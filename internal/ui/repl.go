package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"nanocode/internal/permission"
)

var ErrExitRequested = errors.New("exit requested")

type PromptRunner interface {
	Run(ctx context.Context, userPrompt string, output io.Writer) error
}

type RunnerFactory func(ctx context.Context, mode permission.Mode) (PromptRunner, error)

// REPL 把终端交互层和 Agent 执行层隔开：
// 它只负责读用户输入、处理少量内建命令、展示结果，
// 不直接关心模型、工具或 ReAct 细节。
type REPL struct {
	input         *bufio.Reader
	output        io.Writer
	mode          permission.Mode
	modelLabel    string
	runner        PromptRunner
	runnerFactory RunnerFactory
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
			if err := r.handleCommand(line); err != nil {
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

func (r *REPL) handleCommand(line string) error {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}

	switch fields[0] {
	case "/help":
		return r.renderHelp()
	case "/mode":
		return r.handleModeCommand(fields)
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
	if r.runnerFactory != nil {
		nextRunner, buildErr := r.runnerFactory(context.Background(), nextMode)
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

func (r *REPL) runPrompt(ctx context.Context, line string) error {
	if _, err := fmt.Fprintln(r.output, ""); err != nil {
		return err
	}

	if err := r.runner.Run(ctx, line, r.output); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(r.output); err != nil {
		return err
	}

	_, err := fmt.Fprintln(r.output)
	return err
}

func (r *REPL) renderWelcome() error {
	if _, err := fmt.Fprintln(r.output, "nanocode interactive mode"); err != nil {
		return err
	}
	// 模型信息属于终端展示层关心的上下文，让用户进入 UI 后立刻知道当前到底连的是哪个模型。
	if _, err := fmt.Fprintf(r.output, "Current model: %s\n", r.modelLabel); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Current mode: %s\n", r.mode); err != nil {
		return err
	}
	_, err := fmt.Fprintln(r.output, "输入 /help 查看命令，输入 /exit 退出。")
	return err
}

func (r *REPL) renderHelp() error {
	helpText := `Commands:
  /help                 显示帮助
  /mode                 查看当前模式
  /mode <name>          切换模式：default | plan | accept-edits
  /exit                 退出交互界面

普通输入会作为一轮用户请求发送给 Agent。`

	_, err := fmt.Fprintln(r.output, helpText)
	return err
}
