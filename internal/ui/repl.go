package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/schema"

	"nanocode/internal/memory"
	"nanocode/internal/permission"
	"nanocode/internal/session"
)

var ErrExitRequested = errors.New("exit requested")

type PromptRunner interface {
	Run(ctx context.Context, userPrompt string, output io.Writer) error
}

type SessionRunner interface {
	// SessionRunner 在普通 Run 之外返回更新后的 messages，供 REPL 保存会话历史。
	PromptRunner
	RunWithMessages(ctx context.Context, messages []*schema.Message, userPrompt string, output io.Writer) ([]*schema.Message, error)
}

type UndoRunner interface {
	UndoLast(ctx context.Context) (string, error)
}

type MemoryRunner interface {
	Memory() memory.Memory
}

type RunnerFactory func(ctx context.Context, mode permission.Mode, confirmer permission.Confirmer) (PromptRunner, error)

type REPL struct {
	input         *bufio.Reader
	output        io.Writer
	mode          permission.Mode
	modelLabel    string
	runner        PromptRunner
	runnerFactory RunnerFactory
	sessionStore  *session.Store
	session       *session.Session
	sessionPath   string
}

func NewREPL(input io.Reader, output io.Writer, mode permission.Mode, modelLabel string, runner PromptRunner, runnerFactory RunnerFactory) *REPL {
	return &REPL{
		input:         bufio.NewReader(input),
		output:        output,
		mode:          mode,
		modelLabel:    modelLabel,
		runner:        runner,
		runnerFactory: runnerFactory,
	}
}

func (r *REPL) SetRunner(runner PromptRunner) {
	r.runner = runner
}

func (r *REPL) SetSession(store *session.Store, current *session.Session) {
	// session 由 main 创建或加载，REPL 只负责在每轮结束后保存。
	r.sessionStore = store
	r.session = current
	if store != nil && current != nil {
		r.sessionPath = store.Path(current.ID)
	}
}

func (r *REPL) Run(ctx context.Context) error {
	if err := r.renderWelcome(); err != nil {
		return err
	}

	for {
		line, err := r.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				_, writeErr := fmt.Fprintln(r.output)
				return writeErr
			}
			return err
		}

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			if err := r.handleCommand(ctx, line); err != nil {
				if errors.Is(err, ErrExitRequested) {
					return nil
				}
				return err
			}
			continue
		}

		if err := r.runPrompt(ctx, line); err != nil {
			return err
		}
	}
}

func ReadSinglePrompt(input io.Reader, output io.Writer) (string, error) {
	reader := bufio.NewReader(input)

	if _, err := fmt.Fprint(output, ">>> "); err != nil {
		return "", err
	}

	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	prompt := strings.TrimSpace(line)
	if prompt == "" {
		return "", fmt.Errorf("empty prompt")
	}

	return prompt, nil
}

func (r *REPL) readLine() (string, error) {
	if _, err := fmt.Fprintf(r.output, "[%s] >>> ", r.mode); err != nil {
		return "", err
	}

	line, err := r.input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	line = strings.TrimSpace(line)
	if line == "" && errors.Is(err, io.EOF) {
		return "", io.EOF
	}

	return line, err
}

func (r *REPL) handleCommand(ctx context.Context, line string) error {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}

	switch fields[0] {
	case "/help":
		return r.renderHelp()
	case "/mode":
		return r.handleModeCommand(fields)
	case "/memory":
		return r.renderMemory()
	case "/session":
		return r.renderSession()
	case "/undo":
		return r.handleUndoCommand(ctx)
	case "/exit", "/quit":
		_, err := fmt.Fprintln(r.output, "Bye.")
		if err != nil {
			return err
		}
		return ErrExitRequested
	default:
		_, err := fmt.Fprintf(r.output, "Unknown command: %s\n输入 /help 查看可用命令。\n", fields[0])
		return err
	}
}

func (r *REPL) handleModeCommand(fields []string) error {
	if len(fields) == 1 {
		_, err := fmt.Fprintf(r.output, "Current mode: %s\n", r.mode)
		return err
	}

	nextMode, err := permission.ParseMode(fields[1])
	if err != nil {
		_, writeErr := fmt.Fprintf(r.output, "切换模式失败：%v\n", err)
		if writeErr != nil {
			return writeErr
		}
		return nil
	}

	r.mode = nextMode
	if r.session != nil {
		// 模式是会话状态的一部分，恢复会话时会优先沿用这里保存的 mode。
		r.session.Mode = string(nextMode)
		_ = r.saveSession()
	}

	if r.runnerFactory != nil {
		nextRunner, buildErr := r.runnerFactory(context.Background(), nextMode, r)
		if buildErr != nil {
			_, writeErr := fmt.Fprintf(r.output, "切换模式失败：%v\n", buildErr)
			if writeErr != nil {
				return writeErr
			}
			return nil
		}
		r.runner = nextRunner
	}

	_, err = fmt.Fprintf(r.output, "Switched mode to %s.\n", r.mode)
	return err
}

func (r *REPL) handleUndoCommand(ctx context.Context) error {
	undoRunner, ok := r.runner.(UndoRunner)
	if !ok {
		_, err := fmt.Fprintln(r.output, "当前 runner 不支持撤销。")
		return err
	}

	message, err := undoRunner.UndoLast(ctx)
	if err != nil {
		_, writeErr := fmt.Fprintf(r.output, "撤销失败：%v\n", err)
		if writeErr != nil {
			return writeErr
		}
		return nil
	}

	_, err = fmt.Fprintln(r.output, message)
	return err
}

func (r *REPL) Confirm(ctx context.Context, request permission.Request) (permission.Decision, error) {
	_ = ctx
	if _, err := fmt.Fprintf(r.output, "\n准备执行工具：%s\n类型：%s\n风险：%s\n参数：%s\n\n允许执行？ [y/N] ", request.ToolName, request.Kind, request.Risk, request.Arguments); err != nil {
		return permission.DecisionDeny, err
	}

	line, err := r.input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return permission.DecisionDeny, err
	}

	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "y" || answer == "yes" {
		return permission.DecisionAllow, nil
	}

	return permission.DecisionDeny, nil
}

func (r *REPL) runPrompt(ctx context.Context, line string) error {
	if _, err := fmt.Fprintln(r.output, ""); err != nil {
		return err
	}

	if sessionRunner, ok := r.runner.(SessionRunner); ok && r.session != nil {
		// 有 session 时走历史感知执行：上一轮 messages 会参与下一轮模型调用。
		messages, err := sessionRunner.RunWithMessages(ctx, r.session.Messages, line, r.output)
		if err != nil {
			return err
		}
		r.session.Messages = messages
		r.session.Mode = string(r.mode)
		if err := r.saveSession(); err != nil {
			return err
		}
	} else {
		if err := r.runner.Run(ctx, line, r.output); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(r.output); err != nil {
		return err
	}

	_, err := fmt.Fprintln(r.output)
	return err
}

func (r *REPL) saveSession() error {
	if r.sessionStore == nil || r.session == nil {
		return nil
	}
	if err := r.sessionStore.Save(r.session); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func (r *REPL) renderWelcome() error {
	if _, err := fmt.Fprintln(r.output, "nanocode interactive mode"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Current model: %s\n", r.modelLabel); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Current mode: %s\n", r.mode); err != nil {
		return err
	}
	if r.session != nil {
		if _, err := fmt.Fprintf(r.output, "Current session: %s\n", r.session.ID); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(r.output, "输入 /help 查看命令，输入 /exit 退出。")
	return err
}

func (r *REPL) renderHelp() error {
	helpText := `Commands:
  /help                 显示帮助
  /mode                 查看当前模式
  /mode <name>          切换模式：default | plan | accept-edits
  /memory               查看当前加载的长期记忆
  /session              查看当前会话信息
  /undo                 撤销最近一次成功的文件写入
  /exit                 退出交互界面
普通输入会作为一轮用户请求发送给 Agent。`

	_, err := fmt.Fprintln(r.output, helpText)
	return err
}

func (r *REPL) renderMemory() error {
	memoryRunner, ok := r.runner.(MemoryRunner)
	if !ok {
		_, err := fmt.Fprintln(r.output, "当前 runner 不支持记忆查看。")
		return err
	}

	mem := memoryRunner.Memory()
	if len(mem.Sources) == 0 {
		_, err := fmt.Fprintln(r.output, "Memory: 未配置记忆来源。")
		return err
	}

	if _, err := fmt.Fprintln(r.output, "Memory:"); err != nil {
		return err
	}
	for _, source := range mem.Sources {
		status := "missing"
		if source.Exists {
			status = "loaded"
		}
		if _, err := fmt.Fprintf(r.output, "- %s: %s (%s)\n", source.Kind, source.Path, status); err != nil {
			return err
		}
		if source.Exists {
			if strings.TrimSpace(source.Content) == "" {
				if _, err := fmt.Fprintln(r.output, "  <empty>"); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(r.output, "  %s\n", indentMemoryContent(source.Content)); err != nil {
				return err
			}
		}
	}
	return nil
}

func indentMemoryContent(content string) string {
	return strings.ReplaceAll(content, "\n", "\n  ")
}

func (r *REPL) renderSession() error {
	// /session 只展示会话元信息，不打印完整 messages，避免把上下文刷满终端。
	if r.session == nil {
		_, err := fmt.Fprintln(r.output, "当前没有持久化会话。")
		return err
	}

	_, err := fmt.Fprintf(
		r.output,
		"Session: %s\nMessages: %d\nPath: %s\n",
		r.session.ID,
		len(r.session.Messages),
		r.sessionPath,
	)
	return err
}
