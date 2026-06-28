package agent

import (
	"bytes"
	"context"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"nanocode/internal/contextmgr"
	"nanocode/internal/permission"
	"nanocode/internal/tools"
)

func TestFilterToolsForPlanModeKeepsOnlyReadTools(t *testing.T) {
	registeredTools := []tools.RegisteredTool{
		{Tool: stubTool{}, Kind: permission.ToolKindRead},
		{Tool: stubTool{}, Kind: permission.ToolKindWrite},
		{Tool: stubTool{}, Kind: permission.ToolKindShell},
	}

	filtered := filterToolsForMode(permission.ModePlan, registeredTools)

	if len(filtered) != 1 {
		t.Fatalf("expected one read tool in plan mode, got %d", len(filtered))
	}
	if filtered[0].Kind != permission.ToolKindRead {
		t.Fatalf("expected plan mode to keep only read tools, got %q", filtered[0].Kind)
	}
}

func TestFilterToolsForDefaultModeKeepsAllTools(t *testing.T) {
	registeredTools := []tools.RegisteredTool{
		{Tool: stubTool{}, Kind: permission.ToolKindRead},
		{Tool: stubTool{}, Kind: permission.ToolKindWrite},
		{Tool: stubTool{}, Kind: permission.ToolKindShell},
	}

	filtered := filterToolsForMode(permission.ModeDefault, registeredTools)

	if len(filtered) != len(registeredTools) {
		t.Fatalf("expected default mode to keep all tools, got %d", len(filtered))
	}
}

type stubTool struct {
	einotool.BaseTool
}

func TestInvokeToolTrimsToolResult(t *testing.T) {
	runner := &SingleTurnRunner{
		invokers: map[string]einotool.InvokableTool{
			"long_tool": stubInvokableTool{result: strings.Repeat("x", 20)},
		},
		toolMetadata: map[string]tools.RegisteredTool{
			"long_tool": {Kind: permission.ToolKindRead},
		},
		mode:       permission.ModeDefault,
		confirmer:  permission.DenyConfirmer{},
		contextMgr: contextmgr.NewManager(contextmgr.Budget{ToolOutputChars: 10}),
	}

	message := runner.invokeTool(context.Background(), schema.ToolCall{
		ID: "call-1",
		Function: schema.FunctionCall{
			Name:      "long_tool",
			Arguments: `{}`,
		},
	})

	if !strings.Contains(message.Content, "tool output truncated") {
		t.Fatalf("expected trimmed tool result, got %q", message.Content)
	}
}

func TestWriteContextUsageAppendsReadableStatus(t *testing.T) {
	runner := &SingleTurnRunner{
		contextMgr: contextmgr.NewManager(contextmgr.Budget{MaxTokens: 1000000}),
	}
	output := &bytes.Buffer{}
	messages := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("hello"),
		schema.AssistantMessage("answer", nil),
	}

	err := runner.writeContextUsage(output, messages)

	if err != nil {
		t.Fatalf("expected usage write to succeed: %v", err)
	}
	rendered := output.String()
	if !strings.Contains(rendered, "[已使用") {
		t.Fatalf("expected context usage marker, got %q", rendered)
	}
	if !strings.Contains(rendered, "上下文/1M]") {
		t.Fatalf("expected 1M budget marker, got %q", rendered)
	}
}

func TestPrepareMessagesKeepsHistoryAndAppendsUserPrompt(t *testing.T) {
	runner := &SingleTurnRunner{systemPrompt: "system prompt"}
	history := []*schema.Message{
		schema.SystemMessage("existing system"),
		schema.UserMessage("first user"),
		schema.AssistantMessage("first answer", nil),
	}

	messages := runner.prepareMessages(history, "second user")

	if len(messages) != 4 {
		t.Fatalf("expected four messages, got %d", len(messages))
	}
	if messages[0].Content != "existing system" {
		t.Fatalf("expected existing system message to be preserved, got %q", messages[0].Content)
	}
	if messages[len(messages)-1].Content != "second user" {
		t.Fatalf("expected new user prompt appended, got %q", messages[len(messages)-1].Content)
	}
	if len(history) != 3 {
		t.Fatalf("expected history slice not to be mutated, got %d", len(history))
	}
}

func TestPrepareMessagesInjectsSystemForEmptyHistory(t *testing.T) {
	runner := &SingleTurnRunner{systemPrompt: "system prompt"}

	messages := runner.prepareMessages(nil, "hello")

	if len(messages) != 2 {
		t.Fatalf("expected system and user messages, got %d", len(messages))
	}
	if messages[0].Role != schema.System || messages[0].Content != "system prompt" {
		t.Fatalf("expected injected system message, got %#v", messages[0])
	}
}

func TestPrepareMessagesInjectsMemoryAwareSystemPrompt(t *testing.T) {
	runner := &SingleTurnRunner{systemPrompt: "system prompt with project memory"}

	messages := runner.prepareMessages(nil, "hello")

	if !strings.Contains(messages[0].Content, "project memory") {
		t.Fatalf("expected memory-aware system prompt, got %q", messages[0].Content)
	}
}

type stubInvokableTool struct {
	einotool.InvokableTool
	result string
}

func (t stubInvokableTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	_ = ctx
	_ = argumentsInJSON
	_ = opts
	return t.result, nil
}
