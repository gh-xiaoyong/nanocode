package ui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"nanocode/internal/memory"
	"nanocode/internal/permission"
	"nanocode/internal/session"
)

type testRunner struct {
	prompts      []string
	systemPrompt string
	modelLabel   string
	toolSummary  map[permission.ToolKind]int
}

func (r *testRunner) Run(ctx context.Context, userPrompt string, output io.Writer) error {
	_ = ctx
	r.prompts = append(r.prompts, userPrompt)
	_, err := io.WriteString(output, "stub response")
	return err
}

func (r *testRunner) ContextUsage(messages []*schema.Message) string {
	return fmt.Sprintf("[已使用%d%%上下文/1M]", len(messages))
}

func (r *testRunner) SystemPrompt() string {
	if r.systemPrompt != "" {
		return r.systemPrompt
	}
	return "test system prompt"
}

func (r *testRunner) ToolSummary() map[permission.ToolKind]int {
	if r.toolSummary != nil {
		return r.toolSummary
	}
	return map[permission.ToolKind]int{
		permission.ToolKindRead:  5,
		permission.ToolKindWrite: 2,
		permission.ToolKindShell: 1,
	}
}

func (r *testRunner) ModelLabel() string {
	if r.modelLabel != "" {
		return r.modelLabel
	}
	return "test-model"
}

type testSessionRunner struct {
	testRunner
}

func (r *testSessionRunner) RunWithMessages(ctx context.Context, messages []*schema.Message, userPrompt string, output io.Writer) ([]*schema.Message, error) {
	_ = ctx
	r.prompts = append(r.prompts, userPrompt)
	_, err := io.WriteString(output, "stub response")
	if err != nil {
		return messages, err
	}
	next := append([]*schema.Message{}, messages...)
	next = append(next, schema.UserMessage(userPrompt), schema.AssistantMessage("stub response", nil))
	return next, nil
}

type testUndoRunner struct {
	testRunner
	message string
}

func (r *testUndoRunner) UndoLast(ctx context.Context) (string, error) {
	_ = ctx
	return r.message, nil
}

type testMemoryRunner struct {
	testRunner
	memory memory.Memory
}

func (r *testMemoryRunner) Memory() memory.Memory {
	return r.memory
}

func TestREPLModeSwitchRebuildsRunner(t *testing.T) {
	input := strings.NewReader("/mode plan\nhello\n/exit\n")
	output := &bytes.Buffer{}

	defaultRunner := &testRunner{}
	planRunner := &testRunner{toolSummary: map[permission.ToolKind]int{permission.ToolKindRead: 5}}
	buildModes := make([]permission.Mode, 0, 2)

	repl := NewREPL(input, output, permission.ModeDefault, "test-model", defaultRunner, func(ctx context.Context, mode permission.Mode, confirmer permission.Confirmer) (PromptRunner, error) {
		_ = ctx
		_ = confirmer
		buildModes = append(buildModes, mode)
		if mode == permission.ModePlan {
			return planRunner, nil
		}
		return defaultRunner, nil
	})

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(buildModes) != 1 || buildModes[0] != permission.ModePlan {
		t.Fatalf("expected runner factory to rebuild for plan mode, got %v", buildModes)
	}
	if len(defaultRunner.prompts) != 0 {
		t.Fatalf("expected default runner to be unused after mode switch, got %v", defaultRunner.prompts)
	}
	if len(planRunner.prompts) != 1 || planRunner.prompts[0] != "hello" {
		t.Fatalf("expected plan runner to receive prompt, got %v", planRunner.prompts)
	}
	if !strings.Contains(output.String(), "已切换模式：default -> plan") {
		t.Fatalf("expected mode transition output, got %q", output.String())
	}
}

func TestREPLModeCommandShowsCurrentModeAndTools(t *testing.T) {
	input := strings.NewReader("/mode\n/exit\n")
	output := &bytes.Buffer{}

	repl := NewREPL(input, output, permission.ModeDefault, "test-model", &testRunner{}, nil)
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := output.String()
	for _, expected := range []string{"Mode: default", "Model: test-model", "Tools: read=5 write=2 shell=1"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected /mode output to contain %q, got %q", expected, rendered)
		}
	}
}

func TestREPLModeSameModeDoesNotRebuild(t *testing.T) {
	input := strings.NewReader("/mode default\n/exit\n")
	output := &bytes.Buffer{}
	builds := 0
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", &testRunner{}, func(ctx context.Context, mode permission.Mode, confirmer permission.Confirmer) (PromptRunner, error) {
		_ = ctx
		_ = mode
		_ = confirmer
		builds++
		return &testRunner{}, nil
	})

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if builds != 0 {
		t.Fatalf("expected same mode not to rebuild runner, got %d builds", builds)
	}
	if !strings.Contains(output.String(), "已在该模式：default") {
		t.Fatalf("expected same mode message, got %q", output.String())
	}
}

func TestREPLModeSwitchFailureKeepsOldRunner(t *testing.T) {
	input := strings.NewReader("/mode plan\nhello\n/exit\n")
	output := &bytes.Buffer{}
	oldRunner := &testRunner{}
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", oldRunner, func(ctx context.Context, mode permission.Mode, confirmer permission.Confirmer) (PromptRunner, error) {
		_ = ctx
		_ = mode
		_ = confirmer
		return nil, fmt.Errorf("build boom")
	})

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(oldRunner.prompts) != 1 || oldRunner.prompts[0] != "hello" {
		t.Fatalf("expected old runner to stay active, got %v", oldRunner.prompts)
	}
	if !strings.Contains(output.String(), "切换模式失败：build boom") {
		t.Fatalf("expected mode failure message, got %q", output.String())
	}
}

func TestREPLHelpCommand(t *testing.T) {
	input := strings.NewReader("/help\n/exit\n")
	output := &bytes.Buffer{}
	runner := &testRunner{}

	repl := NewREPL(input, output, permission.ModeDefault, "test-model", runner, nil)
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := output.String()
	for _, expected := range []string{"/mode [name]", "/status", "/clear", "/reload", "/resume <id>", "/continue", "/session", "/memory", "/memory full", "/memory status"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected help output to mention %s, got %q", expected, rendered)
		}
	}
}

func TestREPLSlashHintCommand(t *testing.T) {
	input := strings.NewReader("/\n/exit\n")
	output := &bytes.Buffer{}
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", &testRunner{}, nil)

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "可用 REPL 命令") || !strings.Contains(rendered, "/status") {
		t.Fatalf("expected slash hints, got %q", rendered)
	}
}

func TestSlashCommandCandidatesUseRegistry(t *testing.T) {
	candidates := slashCommandCandidates()
	for _, expected := range []string{"/help", "/status", "/clear", "/reload", "/resume <id>", "/continue", "/memory full", "/memory status"} {
		if !containsString(candidates, expected) {
			t.Fatalf("expected candidates to contain %s, got %v", expected, candidates)
		}
	}

	matches := completeSlashCommand("/memory f")
	if len(matches) != 1 || matches[0] != "/memory full" {
		t.Fatalf("expected /memory full completion, got %v", matches)
	}
}

func TestREPLWelcomeShowsRuntimeSummary(t *testing.T) {
	rootDir := t.TempDir()
	store := session.NewStore(rootDir)
	current, err := store.Create("default", "test-model", rootDir)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	current.Messages = []*schema.Message{schema.UserMessage("hello")}

	input := strings.NewReader("/exit\n")
	output := &bytes.Buffer{}
	runner := &testMemoryRunner{memory: memory.Memory{Sources: []memory.Source{
		{Kind: memory.SourceProject, Exists: true, Content: "rule"},
		{Kind: memory.SourceUser, Exists: false},
	}}}

	repl := NewREPL(input, output, permission.ModeDefault, "gpt-4.1 (https://api.openai.com/v1)", runner, nil)
	repl.SetSession(store, current)
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := output.String()
	for _, expected := range []string{
		"Model: gpt-4.1 (https://api.openai.com/v1)",
		"Mode: default",
		"Workspace: " + rootDir,
		"Session: " + current.ID,
		"Messages: 1",
		"Memory: 1/2 loaded",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected welcome output to contain %q, got %q", expected, rendered)
		}
	}
}

func TestREPLWelcomeWithoutSessionDoesNotFail(t *testing.T) {
	input := strings.NewReader("/exit\n")
	output := &bytes.Buffer{}

	repl := NewREPL(input, output, permission.ModeDefault, "test-model", &testRunner{}, nil)
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(output.String(), "Session: <none>") {
		t.Fatalf("expected no session welcome output, got %q", output.String())
	}
}

func TestREPLUndoCommand(t *testing.T) {
	input := strings.NewReader("/undo\n/exit\n")
	output := &bytes.Buffer{}
	runner := &testUndoRunner{message: "已恢复文件：hello.txt"}

	repl := NewREPL(input, output, permission.ModeDefault, "test-model", runner, nil)
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(output.String(), "已恢复文件：hello.txt") {
		t.Fatalf("expected undo output, got %q", output.String())
	}
}

func TestREPLConfirmSupportsRichInputs(t *testing.T) {
	input := strings.NewReader("h\np\na\nmaybe\ny\n")
	output := &bytes.Buffer{}
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", &testRunner{}, nil)

	decision, err := repl.Confirm(context.Background(), permission.Request{
		ToolName:  "edit_file",
		Kind:      permission.ToolKindWrite,
		Risk:      "修改工作区文件内容",
		Arguments: `{"path":"a.go"}`,
		Preview:   "--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,1 @@\n-old\n+new\n",
	})
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if decision != permission.DecisionAllow {
		t.Fatalf("expected allow decision, got %q", decision)
	}

	rendered := output.String()
	for _, expected := range []string{"可用输入", "Diff 预览：", `参数：{"path":"a.go"}`, "无法识别输入"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected confirmation output to contain %q, got %q", expected, rendered)
		}
	}
	if strings.Count(rendered, "Diff 预览：") < 2 {
		t.Fatalf("expected preview to be shown initially and again after p, got %q", rendered)
	}
}

func TestREPLConfirmDeniesExpectedInputs(t *testing.T) {
	for _, input := range []string{"\n", "n\n", "no\n", "q\n", "quit\n", "cancel\n"} {
		output := &bytes.Buffer{}
		repl := NewREPL(strings.NewReader(input), output, permission.ModeDefault, "test-model", &testRunner{}, nil)
		decision, err := repl.Confirm(context.Background(), permission.Request{ToolName: "bash", Kind: permission.ToolKindShell, Risk: "执行命令", Arguments: `{"command":"go test ./..."}`})
		if err != nil {
			t.Fatalf("Confirm(%q) error = %v", input, err)
		}
		if decision != permission.DecisionDeny {
			t.Fatalf("expected deny for %q, got %q", input, decision)
		}
	}
}

func TestREPLConfirmEOFDefaultsDeny(t *testing.T) {
	output := &bytes.Buffer{}
	repl := NewREPL(strings.NewReader(""), output, permission.ModeDefault, "test-model", &testRunner{}, nil)

	decision, err := repl.Confirm(context.Background(), permission.Request{ToolName: "bash", Kind: permission.ToolKindShell, Risk: "执行命令", Arguments: `{}`})
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if decision != permission.DecisionDeny {
		t.Fatalf("expected EOF to deny, got %q", decision)
	}
}

func TestREPLConfirmShowsDiffPreviewWithColors(t *testing.T) {
	input := strings.NewReader("n\n")
	output := &bytes.Buffer{}
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", &testRunner{}, nil)

	decision, err := repl.Confirm(context.Background(), permission.Request{
		ToolName:  "edit_file",
		Kind:      permission.ToolKindWrite,
		Risk:      "修改工作区文件内容",
		Arguments: `{"path":"a.go"}`,
		Preview:   "--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,1 @@\n-old\n+new\n",
	})
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if decision != permission.DecisionDeny {
		t.Fatalf("expected deny decision, got %q", decision)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "Diff 预览：") || !strings.Contains(rendered, "```diff") || !strings.Contains(rendered, "+new") {
		t.Fatalf("expected diff preview in confirmation prompt, got %q", rendered)
	}
	if !strings.Contains(rendered, ansiGreen+"+new"+ansiReset) || !strings.Contains(rendered, ansiRed+"-old"+ansiReset) {
		t.Fatalf("expected colored diff lines, got %q", rendered)
	}
	if strings.Contains(rendered, ansiGreen+"+++ b/a.go"+ansiReset) || strings.Contains(rendered, ansiRed+"--- a/a.go"+ansiReset) {
		t.Fatalf("expected diff headers to stay uncolored, got %q", rendered)
	}
}

func TestREPLMemorySummaryCommand(t *testing.T) {
	longContent := strings.Repeat("项目规则\n", 300)
	input := strings.NewReader("/memory\n/exit\n")
	output := &bytes.Buffer{}
	runner := &testMemoryRunner{memory: memory.Memory{Sources: []memory.Source{
		{Kind: memory.SourceUser, Path: "C:/Users/me/.nanocode/NANO.md", Exists: false},
		{Kind: memory.SourceProject, Path: "D:/repo/NANO.md", Exists: true, Content: longContent},
	}}}

	repl := NewREPL(input, output, permission.ModeDefault, "test-model", runner, nil)
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "project") || !strings.Contains(rendered, "已截断") || !strings.Contains(rendered, "/memory full") {
		t.Fatalf("expected summarized memory output, got %q", rendered)
	}
	if strings.Count(rendered, "项目规则") >= 300 {
		t.Fatalf("expected default memory output to be summarized, got %q", rendered)
	}
}

func TestREPLMemoryFullAndStatusCommands(t *testing.T) {
	content := "项目规则完整内容"
	input := strings.NewReader("/memory status\n/memory full\n/exit\n")
	output := &bytes.Buffer{}
	runner := &testMemoryRunner{memory: memory.Memory{Sources: []memory.Source{
		{Kind: memory.SourceProject, Path: "D:/repo/NANO.md", Exists: true, Content: content},
	}}}

	repl := NewREPL(input, output, permission.ModeDefault, "test-model", runner, nil)
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := output.String()
	statusIndex := strings.Index(rendered, "Memory sources:")
	fullIndex := strings.Index(rendered, content)
	if statusIndex < 0 || fullIndex < 0 || fullIndex < statusIndex {
		t.Fatalf("expected status then full memory output, got %q", rendered)
	}
	statusBlock := rendered[statusIndex:fullIndex]
	if strings.Contains(statusBlock, content) {
		t.Fatalf("expected /memory status not to print content, got %q", statusBlock)
	}
}

func TestREPLMemoryNoSources(t *testing.T) {
	input := strings.NewReader("/memory\n/exit\n")
	output := &bytes.Buffer{}
	runner := &testMemoryRunner{memory: memory.Memory{}}

	repl := NewREPL(input, output, permission.ModeDefault, "test-model", runner, nil)
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(output.String(), "Memory: 未配置记忆来源。") {
		t.Fatalf("expected no memory output, got %q", output.String())
	}
}

func TestREPLSavesSessionAfterPrompt(t *testing.T) {
	rootDir := t.TempDir()
	store := session.NewStore(rootDir)
	current, err := store.Create("default", "test-model", rootDir)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	input := strings.NewReader("hello\n/exit\n")
	output := &bytes.Buffer{}
	runner := &testSessionRunner{}
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", runner, nil)
	repl.SetSession(store, current)

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	loaded, err := store.Load(current.ID)
	if err != nil {
		t.Fatalf("Load session: %v", err)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected session messages to be saved, got %d", len(loaded.Messages))
	}
}

func TestREPLSessionCommand(t *testing.T) {
	rootDir := t.TempDir()
	store := session.NewStore(rootDir)
	current, err := store.Create("default", "test-model", rootDir)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	input := strings.NewReader("/session\n/exit\n")
	output := &bytes.Buffer{}
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", &testRunner{}, nil)
	repl.SetSession(store, current)

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := output.String()
	for _, expected := range []string{
		"Session: " + current.ID,
		"Mode: default",
		"Model: test-model",
		"Workspace: " + rootDir,
		"Messages: 0",
		"Created:",
		"Updated:",
		"Path:",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected session output to contain %q, got %q", expected, rendered)
		}
	}
}

func TestREPLSessionCommandWithoutSession(t *testing.T) {
	input := strings.NewReader("/session\n/exit\n")
	output := &bytes.Buffer{}
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", &testRunner{}, nil)

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(output.String(), "当前没有持久化会话。") {
		t.Fatalf("expected no session output, got %q", output.String())
	}
}

func TestREPLStatusCommand(t *testing.T) {
	rootDir := t.TempDir()
	store := session.NewStore(rootDir)
	current, err := store.Create("default", "test-model", rootDir)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	current.Messages = []*schema.Message{schema.SystemMessage("system"), schema.UserMessage("hello")}
	if err := store.Save(current); err != nil {
		t.Fatalf("Save session: %v", err)
	}

	input := strings.NewReader("/status\n/exit\n")
	output := &bytes.Buffer{}
	runner := &testMemoryRunner{
		testRunner: testRunner{},
		memory: memory.Memory{Sources: []memory.Source{
			{Kind: memory.SourceUser, Exists: false},
			{Kind: memory.SourceProject, Exists: true, Content: "rule"},
		}},
	}
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", runner, nil)
	repl.SetSession(store, current)

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := output.String()
	for _, expected := range []string{"Mode: default", "Model: test-model", "Session: " + current.ID, "Messages: 2", "Memory: 1/2 loaded", "Context: [已使用2%上下文/1M]", "Tools: read=5 write=2 shell=1"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected status to contain %q, got %q", expected, rendered)
		}
	}
}

func TestREPLClearClearsCurrentSessionWithoutChangingID(t *testing.T) {
	rootDir := t.TempDir()
	store := session.NewStore(rootDir)
	current, err := store.Create("default", "test-model", rootDir)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	current.Messages = []*schema.Message{schema.SystemMessage("system"), schema.UserMessage("hello")}
	if err := store.Save(current); err != nil {
		t.Fatalf("Save session: %v", err)
	}

	input := strings.NewReader("/clear\n/session\n/exit\n")
	output := &bytes.Buffer{}
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", &testRunner{}, nil)
	repl.SetSession(store, current)

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	loaded, err := store.Load(current.ID)
	if err != nil {
		t.Fatalf("Load session: %v", err)
	}
	if loaded.ID != current.ID || len(loaded.Messages) != 0 {
		t.Fatalf("expected same empty session, got id=%s messages=%d", loaded.ID, len(loaded.Messages))
	}
	if !strings.Contains(output.String(), "Messages: 0") {
		t.Fatalf("expected session command to show empty messages, got %q", output.String())
	}
}

func TestREPLReloadRebuildsRunnerAndPreservesMessages(t *testing.T) {
	rootDir := t.TempDir()
	store := session.NewStore(rootDir)
	current, err := store.Create("default", "old-model", rootDir)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	current.Messages = []*schema.Message{schema.SystemMessage("old system"), schema.UserMessage("hello")}

	oldRunner := &testRunner{systemPrompt: "old system", modelLabel: "old-model"}
	newRunner := &testRunner{systemPrompt: "new system", modelLabel: "new-model"}
	input := strings.NewReader("/reload\n/status\n/exit\n")
	output := &bytes.Buffer{}
	builds := 0
	repl := NewREPL(input, output, permission.ModeDefault, "old-model", oldRunner, func(ctx context.Context, mode permission.Mode, confirmer permission.Confirmer) (PromptRunner, error) {
		_ = ctx
		_ = mode
		_ = confirmer
		builds++
		return newRunner, nil
	})
	repl.SetSession(store, current)

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if builds != 1 {
		t.Fatalf("expected one rebuild, got %d", builds)
	}
	if current.Messages[0].Content != "new system" || len(current.Messages) != 2 {
		t.Fatalf("expected system prompt replaced and messages preserved, got %#v", current.Messages)
	}
	if !strings.Contains(output.String(), "Model: new-model") {
		t.Fatalf("expected status to show reloaded model, got %q", output.String())
	}
}

func TestREPLReloadFailureKeepsOldRunnerAndSession(t *testing.T) {
	rootDir := t.TempDir()
	store := session.NewStore(rootDir)
	current, err := store.Create("default", "old-model", rootDir)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	current.Messages = []*schema.Message{schema.SystemMessage("old system")}

	oldRunner := &testRunner{systemPrompt: "old system", modelLabel: "old-model"}
	input := strings.NewReader("/reload\n/status\n/exit\n")
	output := &bytes.Buffer{}
	repl := NewREPL(input, output, permission.ModeDefault, "old-model", oldRunner, func(ctx context.Context, mode permission.Mode, confirmer permission.Confirmer) (PromptRunner, error) {
		_ = ctx
		_ = mode
		_ = confirmer
		return nil, fmt.Errorf("reload boom")
	})
	repl.SetSession(store, current)

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if current.Messages[0].Content != "old system" {
		t.Fatalf("expected old session to stay unchanged, got %#v", current.Messages)
	}
	if !strings.Contains(output.String(), "reload boom") || !strings.Contains(output.String(), "Model: old-model") {
		t.Fatalf("expected friendly error and old model, got %q", output.String())
	}
}

func TestREPLResumeAndContinueLoadSessions(t *testing.T) {
	rootDir := t.TempDir()
	store := session.NewStore(rootDir)
	first, err := store.Create("default", "test-model", rootDir)
	if err != nil {
		t.Fatalf("Create first session: %v", err)
	}
	first.Messages = []*schema.Message{schema.UserMessage("first")}
	if err := store.Save(first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	second, err := store.Create("plan", "test-model", rootDir)
	if err != nil {
		t.Fatalf("Create second session: %v", err)
	}
	second.Messages = []*schema.Message{schema.UserMessage("second")}
	if err := store.Save(second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	input := strings.NewReader("/resume " + first.ID + "\n/session\n/continue\n/session\n/exit\n")
	output := &bytes.Buffer{}
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", &testRunner{}, func(ctx context.Context, mode permission.Mode, confirmer permission.Confirmer) (PromptRunner, error) {
		_ = ctx
		_ = confirmer
		return &testRunner{toolSummary: map[permission.ToolKind]int{permission.ToolKindRead: 5}, systemPrompt: string(mode)}, nil
	})
	repl.SetSession(store, second)

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "已恢复会话："+first.ID) || !strings.Contains(rendered, "Session: "+first.ID) {
		t.Fatalf("expected resume output for first session, got %q", rendered)
	}
	if !strings.Contains(rendered, "已恢复会话："+second.ID) || !strings.Contains(rendered, "Session: "+second.ID) {
		t.Fatalf("expected continue output for latest session, got %q", rendered)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
