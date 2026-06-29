package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"

	"nanocode/internal/friendlyerror"
	"nanocode/internal/memory"
	"nanocode/internal/permission"
	"nanocode/internal/session"
)

var ErrExitRequested = errors.New("exit requested")

const (
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
	ansiReset = "\x1b[0m"

	memorySummaryRunes = 1200
	timeLayout         = "2006-01-02 15:04:05"
)

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

type RuntimeInfoRunner interface {
	ContextUsage(messages []*schema.Message) string
	SystemPrompt() string
	ToolSummary() map[permission.ToolKind]int
}

type ModelLabelRunner interface {
	ModelLabel() string
}

type RunnerFactory func(ctx context.Context, mode permission.Mode, confirmer permission.Confirmer) (PromptRunner, error)

type REPL struct {
	input         LineReader
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
	return NewREPLWithLineReader(newPlainLineReader(input), output, mode, modelLabel, runner, runnerFactory)
}

func NewREPLWithLineReader(input LineReader, output io.Writer, mode permission.Mode, modelLabel string, runner PromptRunner, runnerFactory RunnerFactory) *REPL {
	if input == nil {
		input = newPlainLineReader(strings.NewReader(""))
	}
	return &REPL{
		input:         input,
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

func (r *REPL) SetModelLabel(modelLabel string) {
	r.modelLabel = modelLabel
}

func (r *REPL) SetSession(store *session.Store, current *session.Session) {
	// session 由 main 创建或加载，REPL 负责展示、切换和每轮结束后的保存。
	r.sessionStore = store
	r.session = current
	r.refreshSessionPath()
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
	line, err := r.input.ReadLine(fmt.Sprintf("[%s] >>> ", r.mode))
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
	if fields[0] == "/" {
		return r.renderSlashHints()
	}

	switch fields[0] {
	case "/help":
		return r.renderHelp()
	case "/status":
		return r.renderStatus()
	case "/mode":
		return r.handleModeCommand(ctx, fields)
	case "/memory":
		return r.renderMemory(fields)
	case "/session":
		return r.renderSession()
	case "/clear":
		return r.handleClearCommand()
	case "/reload":
		return r.handleReloadCommand(ctx)
	case "/resume":
		return r.handleResumeCommand(ctx, fields)
	case "/continue":
		return r.handleContinueCommand(ctx)
	case "/undo":
		return r.handleUndoCommand(ctx)
	case "/exit", "/quit":
		_, err := fmt.Fprintln(r.output, "Bye.")
		if err != nil {
			return err
		}
		return ErrExitRequested
	default:
		_, err := fmt.Fprintf(r.output, "未知命令：%s\n输入 /help 查看可用命令。\n", fields[0])
		return err
	}
}

func (r *REPL) handleModeCommand(ctx context.Context, fields []string) error {
	if len(fields) == 1 {
		if _, err := fmt.Fprintf(r.output, "Mode: %s\n", r.mode); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(r.output, "Model: %s\n", r.modelLabel); err != nil {
			return err
		}
		return r.renderToolSummary()
	}

	nextMode, err := permission.ParseMode(fields[1])
	if err != nil {
		_, writeErr := fmt.Fprintf(r.output, "切换模式失败：%v\n", err)
		if writeErr != nil {
			return writeErr
		}
		return nil
	}

	if nextMode == r.mode {
		_, err := fmt.Fprintf(r.output, "已在该模式：%s\n", r.mode)
		return err
	}

	oldMode := r.mode
	oldRunner := r.runner
	nextRunner, buildErr := r.buildRunner(ctx, nextMode)
	if buildErr != nil {
		r.runner = oldRunner
		_, writeErr := fmt.Fprintf(r.output, "切换模式失败：%v\n", buildErr)
		if writeErr != nil {
			return writeErr
		}
		return nil
	}

	r.mode = nextMode
	r.runner = nextRunner
	r.syncModelLabelFromRunner()
	if r.session != nil {
		// 模式是会话状态的一部分，恢复会话时会优先沿用这里保存的 mode。
		r.session.Mode = string(nextMode)
		r.replaceSessionSystemPrompt()
		_ = r.saveSession()
	}

	_, err = fmt.Fprintf(r.output, "已切换模式：%s -> %s\n", oldMode, r.mode)
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

func (r *REPL) handleClearCommand() error {
	if r.session == nil {
		_, err := fmt.Fprintln(r.output, "当前没有持久化会话可清空。")
		return err
	}

	// /clear 只清空当前会话的 messages，不创建新 session，也不删除旧文件。
	r.session.Messages = []*schema.Message{}
	if err := r.saveSession(); err != nil {
		return err
	}

	_, err := fmt.Fprintf(r.output, "已清空当前会话：%s\n", r.session.ID)
	return err
}

func (r *REPL) handleReloadCommand(ctx context.Context) error {
	oldRunner := r.runner
	nextRunner, err := r.buildRunner(ctx, r.mode)
	if err != nil {
		r.runner = oldRunner
		_, writeErr := fmt.Fprintf(r.output, "%s\n", friendlyerror.Format(err))
		if writeErr != nil {
			return writeErr
		}
		return nil
	}

	r.runner = nextRunner
	r.syncModelLabelFromRunner()
	if r.session != nil {
		r.session.Mode = string(r.mode)
		r.session.Model = r.modelLabel
		r.replaceSessionSystemPrompt()
		if err := r.saveSession(); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintln(r.output, "已重新加载配置和 memory，并重建 runner。")
	return err
}

func (r *REPL) handleResumeCommand(ctx context.Context, fields []string) error {
	if len(fields) < 2 {
		_, err := fmt.Fprintln(r.output, "用法：/resume <session-id>")
		return err
	}
	if r.sessionStore == nil {
		_, err := fmt.Fprintln(r.output, "当前没有 session store，无法恢复会话。")
		return err
	}

	loaded, err := r.sessionStore.Load(fields[1])
	if err != nil {
		_, writeErr := fmt.Fprintf(r.output, "%s\n", friendlyerror.Format(err))
		return writeErr
	}
	return r.switchSession(ctx, loaded)
}

func (r *REPL) handleContinueCommand(ctx context.Context) error {
	if r.sessionStore == nil {
		_, err := fmt.Fprintln(r.output, "当前没有 session store，无法继续最近会话。")
		return err
	}

	loaded, err := r.sessionStore.LoadLatest()
	if err != nil {
		_, writeErr := fmt.Fprintf(r.output, "%s\n", friendlyerror.Format(err))
		return writeErr
	}
	return r.switchSession(ctx, loaded)
}

func (r *REPL) switchSession(ctx context.Context, loaded *session.Session) error {
	if loaded == nil {
		return fmt.Errorf("session is nil")
	}

	nextMode := r.mode
	if strings.TrimSpace(loaded.Mode) != "" {
		if parsed, err := permission.ParseMode(loaded.Mode); err == nil {
			nextMode = parsed
		}
	}

	nextRunner, err := r.buildRunner(ctx, nextMode)
	if err != nil {
		_, writeErr := fmt.Fprintf(r.output, "%s\n", friendlyerror.Format(err))
		return writeErr
	}

	r.mode = nextMode
	r.runner = nextRunner
	r.syncModelLabelFromRunner()
	r.session = loaded
	r.refreshSessionPath()
	r.replaceSessionSystemPrompt()

	_, err = fmt.Fprintf(r.output, "已恢复会话：%s（mode=%s，messages=%d）\n", r.session.ID, r.mode, len(r.session.Messages))
	return err
}

func (r *REPL) Confirm(ctx context.Context, request permission.Request) (permission.Decision, error) {
	_ = ctx
	if err := r.renderConfirmRequest(request); err != nil {
		return permission.DecisionDeny, err
	}
	if strings.TrimSpace(request.Preview) != "" {
		if err := r.renderConfirmPreview(request); err != nil {
			return permission.DecisionDeny, err
		}
	}

	for {
		line, err := r.input.ReadLine("\n允许执行？[y/N, h 查看帮助] ")
		if err != nil {
			if errors.Is(err, io.EOF) {
				return permission.DecisionDeny, nil
			}
			return permission.DecisionDeny, err
		}

		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "n", "no", "q", "quit", "cancel":
			return permission.DecisionDeny, nil
		case "y", "yes":
			return permission.DecisionAllow, nil
		case "p", "preview":
			if strings.TrimSpace(request.Preview) == "" {
				if _, err := fmt.Fprintln(r.output, "当前工具调用没有 diff 预览。"); err != nil {
					return permission.DecisionDeny, err
				}
				continue
			}
			if err := r.renderConfirmPreview(request); err != nil {
				return permission.DecisionDeny, err
			}
		case "a", "args":
			if err := r.renderConfirmArgs(request); err != nil {
				return permission.DecisionDeny, err
			}
		case "h", "help", "?":
			if err := r.renderConfirmHelp(); err != nil {
				return permission.DecisionDeny, err
			}
		default:
			if _, err := fmt.Fprintln(r.output, "无法识别输入。输入 h 查看可用选项。"); err != nil {
				return permission.DecisionDeny, err
			}
		}
	}
}

func (r *REPL) renderConfirmRequest(request permission.Request) error {
	_, err := fmt.Fprintf(r.output, "\n! 需要确认工具调用\n工具：%s\n类型：%s\n风险：%s\n参数：%s\n", request.ToolName, request.Kind, request.Risk, request.Arguments)
	return err
}

func (r *REPL) renderConfirmPreview(request permission.Request) error {
	// Diff 预览只展示给用户确认，不写入模型上下文。
	_, err := fmt.Fprintf(r.output, "\nDiff 预览：\n```diff\n%s```\n", ensureTrailingNewline(colorizeDiff(request.Preview)))
	return err
}

func (r *REPL) renderConfirmArgs(request permission.Request) error {
	_, err := fmt.Fprintf(r.output, "参数：%s\n", request.Arguments)
	return err
}

func (r *REPL) renderConfirmHelp() error {
	_, err := fmt.Fprintln(r.output, "可用输入：y/yes 允许一次，n/no/回车 拒绝，p/preview 查看 diff，a/args 查看参数，q/quit/cancel 取消。")
	return err
}

func ensureTrailingNewline(text string) string {
	if strings.HasSuffix(text, "\n") {
		return text
	}
	return text + "\n"
}

func colorizeDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			lines[i] = ansiGreen + line + ansiReset
		case strings.HasPrefix(line, "-"):
			lines[i] = ansiRed + line + ansiReset
		}
	}
	return strings.Join(lines, "\n")
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
		r.session.Model = r.modelLabel
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
	r.refreshSessionPath()
	return nil
}

func (r *REPL) buildRunner(ctx context.Context, mode permission.Mode) (PromptRunner, error) {
	if r.runnerFactory == nil {
		return r.runner, nil
	}
	return r.runnerFactory(ctx, mode, r)
}

func (r *REPL) replaceSessionSystemPrompt() {
	if r.session == nil {
		return
	}
	infoRunner, ok := r.runner.(RuntimeInfoRunner)
	if !ok {
		return
	}
	systemPrompt := infoRunner.SystemPrompt()
	if strings.TrimSpace(systemPrompt) == "" {
		return
	}
	if len(r.session.Messages) > 0 && r.session.Messages[0] != nil && r.session.Messages[0].Role == schema.System {
		r.session.Messages[0] = schema.SystemMessage(systemPrompt)
	}
}

func (r *REPL) syncModelLabelFromRunner() {
	modelRunner, ok := r.runner.(ModelLabelRunner)
	if !ok {
		return
	}
	label := strings.TrimSpace(modelRunner.ModelLabel())
	if label != "" {
		r.modelLabel = label
	}
}

func (r *REPL) refreshSessionPath() {
	r.sessionPath = ""
	if r.sessionStore != nil && r.session != nil {
		r.sessionPath = r.sessionStore.Path(r.session.ID)
	}
}

func (r *REPL) renderWelcome() error {
	if _, err := fmt.Fprintln(r.output, "nanocode interactive mode"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Model: %s\n", r.modelLabel); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Mode: %s\n", r.mode); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Workspace: %s\n", r.workspaceRoot()); err != nil {
		return err
	}
	if r.session != nil {
		if _, err := fmt.Fprintf(r.output, "Session: %s\nMessages: %d\n", r.session.ID, len(r.session.Messages)); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(r.output, "Session: <none>"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Memory: %s\n", r.memoryStatus()); err != nil {
		return err
	}
	_, err := fmt.Fprintln(r.output, "输入 / 查看命令，/help 查看帮助，/exit 退出。")
	return err
}

func (r *REPL) renderHelp() error {
	if _, err := fmt.Fprintln(r.output, "Commands:"); err != nil {
		return err
	}
	for _, command := range replCommands() {
		if _, err := fmt.Fprintf(r.output, "  %-18s %s\n", command.Usage, command.Description); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(r.output, "普通输入会作为一轮用户请求发送给 Agent。")
	return err
}

func (r *REPL) renderSlashHints() error {
	if _, err := fmt.Fprintln(r.output, "可用 REPL 命令："); err != nil {
		return err
	}
	for _, command := range replCommands() {
		if _, err := fmt.Fprintf(r.output, "  %-18s %s\n", command.Usage, command.Description); err != nil {
			return err
		}
	}
	return nil
}

func (r *REPL) renderMemory(fields []string) error {
	memoryRunner, ok := r.runner.(MemoryRunner)
	if !ok {
		_, err := fmt.Fprintln(r.output, "当前 runner 不支持记忆查看。")
		return err
	}

	mode := "summary"
	if len(fields) > 1 {
		mode = fields[1]
	}
	switch mode {
	case "summary":
		return r.renderMemorySummary(memoryRunner.Memory())
	case "full":
		return r.renderMemoryFull(memoryRunner.Memory())
	case "status":
		return r.renderMemoryStatus(memoryRunner.Memory())
	default:
		_, err := fmt.Fprintln(r.output, "用法：/memory、/memory full、/memory status")
		return err
	}
}

func (r *REPL) renderMemorySummary(mem memory.Memory) error {
	if len(mem.Sources) == 0 {
		_, err := fmt.Fprintln(r.output, "Memory: 未配置记忆来源。")
		return err
	}

	if _, err := fmt.Fprintln(r.output, "Memory:"); err != nil {
		return err
	}
	for _, source := range mem.Sources {
		if err := r.renderMemorySourceHeader(source); err != nil {
			return err
		}
		if !source.Exists {
			continue
		}
		content := strings.TrimSpace(source.Content)
		if content == "" {
			if _, err := fmt.Fprintln(r.output, "  <empty>"); err != nil {
				return err
			}
			continue
		}
		summary, truncated := summarizeText(content, memorySummaryRunes)
		if _, err := fmt.Fprintf(r.output, "  %s\n", indentMemoryContent(summary)); err != nil {
			return err
		}
		if truncated {
			if _, err := fmt.Fprintln(r.output, "  <已截断，可用 /memory full 查看完整内容>"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *REPL) renderMemoryFull(mem memory.Memory) error {
	if len(mem.Sources) == 0 {
		_, err := fmt.Fprintln(r.output, "Memory: 未配置记忆来源。")
		return err
	}

	if _, err := fmt.Fprintln(r.output, "Memory:"); err != nil {
		return err
	}
	for _, source := range mem.Sources {
		if err := r.renderMemorySourceHeader(source); err != nil {
			return err
		}
		if !source.Exists {
			continue
		}
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
	return nil
}

func (r *REPL) renderMemoryStatus(mem memory.Memory) error {
	if len(mem.Sources) == 0 {
		_, err := fmt.Fprintln(r.output, "Memory: 未配置记忆来源。")
		return err
	}
	if _, err := fmt.Fprintln(r.output, "Memory sources:"); err != nil {
		return err
	}
	for _, source := range mem.Sources {
		if err := r.renderMemorySourceHeader(source); err != nil {
			return err
		}
	}
	return nil
}

func (r *REPL) renderMemorySourceHeader(source memory.Source) error {
	status := "missing"
	if source.Exists {
		status = "loaded"
	}
	_, err := fmt.Fprintf(r.output, "- %s: %s (%s, %d chars)\n", source.Kind, source.Path, status, utf8.RuneCountInString(source.Content))
	return err
}

func indentMemoryContent(content string) string {
	return strings.ReplaceAll(content, "\n", "\n  ")
}

func summarizeText(content string, limit int) (string, bool) {
	if limit <= 0 {
		return "", strings.TrimSpace(content) != ""
	}
	if utf8.RuneCountInString(content) <= limit {
		return content, false
	}
	var builder strings.Builder
	count := 0
	for _, r := range content {
		if count >= limit {
			break
		}
		builder.WriteRune(r)
		count++
	}
	return builder.String(), true
}

func (r *REPL) renderSession() error {
	// /session 只展示会话元信息，不打印完整 messages，避免把上下文刷满终端。
	if r.session == nil {
		_, err := fmt.Fprintln(r.output, "当前没有持久化会话。")
		return err
	}

	_, err := fmt.Fprintf(
		r.output,
		"Session: %s\nMode: %s\nModel: %s\nWorkspace: %s\nMessages: %d\nCreated: %s\nUpdated: %s\nPath: %s\n",
		r.session.ID,
		r.session.Mode,
		r.session.Model,
		r.session.WorkspaceRoot,
		len(r.session.Messages),
		formatSessionTime(r.session.CreatedAt),
		formatSessionTime(r.session.UpdatedAt),
		r.sessionPath,
	)
	return err
}

func (r *REPL) renderStatus() error {
	messageCount := 0
	sessionID := "<none>"
	sessionPath := "<none>"
	if r.session != nil {
		messageCount = len(r.session.Messages)
		sessionID = r.session.ID
		sessionPath = r.sessionPath
	}

	contextUsage := "[上下文使用情况不可用]"
	if infoRunner, ok := r.runner.(RuntimeInfoRunner); ok {
		contextUsage = infoRunner.ContextUsage(r.currentMessages())
	}

	if _, err := fmt.Fprintf(r.output, "Mode: %s\n", r.mode); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Model: %s\n", r.modelLabel); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Workspace: %s\n", r.workspaceRoot()); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Session: %s\nPath: %s\nMessages: %d\n", sessionID, sessionPath, messageCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Memory: %s\n", r.memoryStatus()); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.output, "Context: %s\n", contextUsage); err != nil {
		return err
	}
	return r.renderToolSummary()
}

func (r *REPL) renderToolSummary() error {
	toolSummary := map[permission.ToolKind]int{}
	if infoRunner, ok := r.runner.(RuntimeInfoRunner); ok {
		toolSummary = infoRunner.ToolSummary()
	}
	_, err := fmt.Fprintf(
		r.output,
		"Tools: read=%d write=%d shell=%d\n",
		toolSummary[permission.ToolKindRead],
		toolSummary[permission.ToolKindWrite],
		toolSummary[permission.ToolKindShell],
	)
	return err
}

func (r *REPL) currentMessages() []*schema.Message {
	if r.session == nil {
		return nil
	}
	return r.session.Messages
}

func (r *REPL) workspaceRoot() string {
	if r.session != nil && strings.TrimSpace(r.session.WorkspaceRoot) != "" {
		return r.session.WorkspaceRoot
	}
	workspace, err := os.Getwd()
	if err != nil {
		return "<unknown>"
	}
	return workspace
}

func (r *REPL) memoryStatus() string {
	memoryRunner, ok := r.runner.(MemoryRunner)
	if !ok {
		return "unknown"
	}
	return summarizeMemory(memoryRunner.Memory())
}

func summarizeMemory(mem memory.Memory) string {
	if len(mem.Sources) == 0 {
		return "0 loaded"
	}

	loaded := 0
	for _, source := range mem.Sources {
		if source.Exists {
			loaded++
		}
	}
	return fmt.Sprintf("%d/%d loaded", loaded, len(mem.Sources))
}

func formatSessionTime(value time.Time) string {
	if value.IsZero() {
		return "<unknown>"
	}
	return value.Local().Format(timeLayout)
}
