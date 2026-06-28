package ui

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"nanocode/internal/memory"
	"nanocode/internal/permission"
	"nanocode/internal/session"
)

type testRunner struct {
	prompts []string
}

func (r *testRunner) Run(ctx context.Context, userPrompt string, output io.Writer) error {
	_ = ctx
	r.prompts = append(r.prompts, userPrompt)
	_, err := io.WriteString(output, "stub response")
	return err
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
	planRunner := &testRunner{}
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
	if !strings.Contains(rendered, "/mode <name>") {
		t.Fatalf("expected help output to mention mode command, got %q", rendered)
	}
	if !strings.Contains(rendered, "/session") {
		t.Fatalf("expected help output to mention session command, got %q", rendered)
	}
	if !strings.Contains(rendered, "/memory") {
		t.Fatalf("expected help output to mention memory command, got %q", rendered)
	}
}

func TestREPLWelcomeShowsCurrentModel(t *testing.T) {
	input := strings.NewReader("/exit\n")
	output := &bytes.Buffer{}
	runner := &testRunner{}

	repl := NewREPL(input, output, permission.ModeDefault, "gpt-4.1 (https://api.openai.com/v1)", runner, nil)
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "Current model: gpt-4.1 (https://api.openai.com/v1)") {
		t.Fatalf("expected welcome output to show current model, got %q", rendered)
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

func TestREPLConfirmAllowsOnlyExplicitYes(t *testing.T) {
	input := strings.NewReader("y\nn\n")
	output := &bytes.Buffer{}
	repl := NewREPL(input, output, permission.ModeDefault, "test-model", &testRunner{}, nil)

	decision, err := repl.Confirm(context.Background(), permission.Request{ToolName: "edit_file", Kind: permission.ToolKindWrite, Risk: "修改工作区文件内容", Arguments: `{"path":"a.go"}`})
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if decision != permission.DecisionAllow {
		t.Fatalf("expected allow decision, got %q", decision)
	}

	decision, err = repl.Confirm(context.Background(), permission.Request{ToolName: "bash", Kind: permission.ToolKindShell, Risk: "执行命令", Arguments: `{"command":"go test ./..."}`})
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if decision != permission.DecisionDeny {
		t.Fatalf("expected deny decision, got %q", decision)
	}
}

func TestREPLMemoryCommand(t *testing.T) {
	input := strings.NewReader("/memory\n/exit\n")
	output := &bytes.Buffer{}
	runner := &testMemoryRunner{memory: memory.Memory{Sources: []memory.Source{
		{Kind: memory.SourceUser, Path: "C:/Users/me/.nanocode/NANO.md", Exists: false},
		{Kind: memory.SourceProject, Path: "D:/repo/NANO.md", Exists: true, Content: "项目规则"},
	}}}

	repl := NewREPL(input, output, permission.ModeDefault, "test-model", runner, nil)
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "project") || !strings.Contains(rendered, "项目规则") {
		t.Fatalf("expected memory output, got %q", rendered)
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
	if !strings.Contains(rendered, current.ID) || !strings.Contains(rendered, "Messages: 0") {
		t.Fatalf("expected session output, got %q", rendered)
	}
}
