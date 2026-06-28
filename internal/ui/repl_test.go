package ui

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"nanocode/internal/permission"
)

type testRunner struct {
	prompts []string
}

func (r *testRunner) Run(ctx context.Context, userPrompt string, output io.Writer) error {
	r.prompts = append(r.prompts, userPrompt)
	_, err := io.WriteString(output, "stub response")
	return err
}

func TestREPLModeSwitchRebuildsRunner(t *testing.T) {
	input := strings.NewReader("/mode plan\nhello\n/exit\n")
	output := &bytes.Buffer{}

	defaultRunner := &testRunner{}
	planRunner := &testRunner{}
	buildModes := make([]permission.Mode, 0, 2)

	repl := NewREPL(input, output, permission.ModeDefault, "test-model", defaultRunner, func(ctx context.Context, mode permission.Mode) (PromptRunner, error) {
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
