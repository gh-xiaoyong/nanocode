package agent

import (
	"bytes"
	"context"
	"fmt"
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

func TestRunnerRuntimeInfoMethods(t *testing.T) {
	runner := &SingleTurnRunner{
		systemPrompt: "system prompt",
		modelLabel:   "test-model",
		contextMgr:   contextmgr.NewManager(contextmgr.Budget{MaxTokens: 1000000}),
		toolMetadata: map[string]tools.RegisteredTool{
			"read_file": {Kind: permission.ToolKindRead},
			"grep":      {Kind: permission.ToolKindRead},
			"edit_file": {Kind: permission.ToolKindWrite},
			"bash":      {Kind: permission.ToolKindShell},
		},
	}

	if runner.SystemPrompt() != "system prompt" {
		t.Fatalf("expected system prompt, got %q", runner.SystemPrompt())
	}
	if runner.ModelLabel() != "test-model" {
		t.Fatalf("expected model label, got %q", runner.ModelLabel())
	}
	usage := runner.ContextUsage([]*schema.Message{schema.UserMessage("hello")})
	if !strings.Contains(usage, "[已使用") || !strings.Contains(usage, "上下文/1M]") {
		t.Fatalf("expected readable context usage, got %q", usage)
	}
	summary := runner.ToolSummary()
	if summary[permission.ToolKindRead] != 2 || summary[permission.ToolKindWrite] != 1 || summary[permission.ToolKindShell] != 1 {
		t.Fatalf("unexpected tool summary: %#v", summary)
	}
}

func TestToolSummaryForPlanModeExcludesWriteAndShell(t *testing.T) {
	registeredTools := []tools.RegisteredTool{
		{Tool: stubTool{}, Kind: permission.ToolKindRead},
		{Tool: stubTool{}, Kind: permission.ToolKindWrite},
		{Tool: stubTool{}, Kind: permission.ToolKindShell},
	}
	filtered := filterToolsForMode(permission.ModePlan, registeredTools)
	metadata := make(map[string]tools.RegisteredTool, len(filtered))
	for i, tool := range filtered {
		metadata[fmt.Sprintf("tool-%d", i)] = tool
	}
	runner := &SingleTurnRunner{toolMetadata: metadata}

	summary := runner.ToolSummary()
	if summary[permission.ToolKindRead] != 1 || summary[permission.ToolKindWrite] != 0 || summary[permission.ToolKindShell] != 0 {
		t.Fatalf("expected plan mode summary to expose read tools only, got %#v", summary)
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
	}, NoopReporter{}, StartTurnStats())

	if !strings.Contains(message.Content, "tool output truncated") {
		t.Fatalf("expected trimmed tool result, got %q", message.Content)
	}
}

func TestInvokeToolReportsStartAndFinish(t *testing.T) {
	output := &bytes.Buffer{}
	runner := &SingleTurnRunner{
		invokers: map[string]einotool.InvokableTool{
			"read_file": stubInvokableTool{result: "full result that should only go to model"},
		},
		toolMetadata: map[string]tools.RegisteredTool{
			"read_file": {Kind: permission.ToolKindRead},
		},
		mode:       permission.ModeDefault,
		confirmer:  permission.DenyConfirmer{},
		contextMgr: contextmgr.NewDefaultManager(),
	}

	message := runner.invokeTool(context.Background(), schema.ToolCall{
		ID: "call-1",
		Function: schema.FunctionCall{
			Name:      "read_file",
			Arguments: `{"file_path":"README.md"}`,
		},
	}, NewTerminalReporter(output), StartTurnStats())

	rendered := output.String()
	if !strings.Contains(rendered, "● read_file") {
		t.Fatalf("expected tool start output, got %q", rendered)
	}
	if !strings.Contains(rendered, "✓ read_file 完成") {
		t.Fatalf("expected tool finish output, got %q", rendered)
	}
	if !strings.Contains(rendered, "输出 1 行") {
		t.Fatalf("expected tool summary output, got %q", rendered)
	}
	if strings.Contains(rendered, "full result that should only go to model") {
		t.Fatalf("expected full tool result not to be printed, got %q", rendered)
	}
	if message.Content != "full result that should only go to model" {
		t.Fatalf("expected full result in tool message, got %q", message.Content)
	}
}

func TestInvokeToolReportsError(t *testing.T) {
	output := &bytes.Buffer{}
	runner := &SingleTurnRunner{
		invokers: map[string]einotool.InvokableTool{
			"bad_tool": stubInvokableTool{err: fmt.Errorf("boom")},
		},
		toolMetadata: map[string]tools.RegisteredTool{
			"bad_tool": {Kind: permission.ToolKindRead},
		},
		mode:       permission.ModeDefault,
		confirmer:  permission.DenyConfirmer{},
		contextMgr: contextmgr.NewDefaultManager(),
	}

	message := runner.invokeTool(context.Background(), schema.ToolCall{
		ID: "call-1",
		Function: schema.FunctionCall{
			Name:      "bad_tool",
			Arguments: `{}`,
		},
	}, NewTerminalReporter(output), StartTurnStats())

	if !strings.Contains(output.String(), "工具失败：bad_tool") || !strings.Contains(output.String(), "boom") {
		t.Fatalf("expected tool error output, got %q", output.String())
	}
	if !strings.Contains(message.Content, "工具失败：bad_tool") || !strings.Contains(message.Content, "boom") {
		t.Fatalf("expected failure in tool message, got %q", message.Content)
	}
}

func TestInvokeToolReportsDenied(t *testing.T) {
	output := &bytes.Buffer{}
	runner := &SingleTurnRunner{
		invokers: map[string]einotool.InvokableTool{
			"edit_file": stubInvokableTool{result: "edited"},
		},
		toolMetadata: map[string]tools.RegisteredTool{
			"edit_file": {Kind: permission.ToolKindWrite},
		},
		mode:       permission.ModePlan,
		confirmer:  permission.DenyConfirmer{},
		contextMgr: contextmgr.NewDefaultManager(),
	}

	message := runner.invokeTool(context.Background(), schema.ToolCall{
		ID: "call-1",
		Function: schema.FunctionCall{
			Name:      "edit_file",
			Arguments: `{"path":"README.md"}`,
		},
	}, NewTerminalReporter(output), StartTurnStats())

	if !strings.Contains(output.String(), "! edit_file 权限拒绝") {
		t.Fatalf("expected denied output, got %q", output.String())
	}
	if !strings.Contains(output.String(), `path="README.md"`) {
		t.Fatalf("expected denied output to include argument summary, got %q", output.String())
	}
	if !strings.Contains(message.Content, "权限拒绝") || !strings.Contains(message.Content, "plan 模式") {
		t.Fatalf("expected denied tool message, got %q", message.Content)
	}
}

func TestInvokeToolPassesDiffPreviewToConfirmerAndReporter(t *testing.T) {
	output := &bytes.Buffer{}
	confirmer := &allowConfirmer{}
	preview := "--- a/README.md\n+++ b/README.md\n@@ -1,1 +1,1 @@\n-old\n+new\n"
	runner := &SingleTurnRunner{
		invokers: map[string]einotool.InvokableTool{
			"edit_file": stubInvokableTool{result: "edit_file succeeded: README.md (1 replacement(s), checkpoint created)"},
		},
		toolMetadata: map[string]tools.RegisteredTool{
			"edit_file": {
				Kind: permission.ToolKindWrite,
				Preview: func(ctx context.Context, arguments string) (string, error) {
					_ = ctx
					_ = arguments
					return preview, nil
				},
			},
		},
		mode:       permission.ModeDefault,
		confirmer:  confirmer,
		contextMgr: contextmgr.NewDefaultManager(),
	}

	message := runner.invokeTool(context.Background(), schema.ToolCall{
		ID: "call-1",
		Function: schema.FunctionCall{
			Name:      "edit_file",
			Arguments: `{"path":"README.md"}`,
		},
	}, NewTerminalReporter(output), StartTurnStats())

	if confirmer.request.Preview != preview {
		t.Fatalf("expected confirmer to receive diff preview, got %q", confirmer.request.Preview)
	}
	if !strings.Contains(output.String(), "修改 README.md") || !strings.Contains(output.String(), ansiGreen+"+1"+ansiReset) || !strings.Contains(output.String(), ansiRed+"-1"+ansiReset) {
		t.Fatalf("expected reporter to show diff summary, got %q", output.String())
	}
	if strings.Contains(message.Content, "--- a/README.md") {
		t.Fatalf("expected diff preview not to enter tool message, got %q", message.Content)
	}
}

func TestInvokeToolStatsCountsSuccessfulWrite(t *testing.T) {
	runner := &SingleTurnRunner{
		invokers: map[string]einotool.InvokableTool{
			"write_file": stubInvokableTool{result: "write_file succeeded: demo.txt (4 bytes, checkpoint created)"},
		},
		toolMetadata: map[string]tools.RegisteredTool{
			"write_file": {Kind: permission.ToolKindWrite},
		},
		mode:       permission.ModeAcceptEdits,
		confirmer:  permission.DenyConfirmer{},
		contextMgr: contextmgr.NewDefaultManager(),
	}
	stats := StartTurnStats()

	runner.invokeTool(context.Background(), schema.ToolCall{
		ID: "call-1",
		Function: schema.FunctionCall{
			Name:      "write_file",
			Arguments: `{"path":"demo.txt"}`,
		},
	}, NoopReporter{}, stats)

	if stats.ToolCalls != 1 || stats.SuccessfulToolCalls != 1 || stats.WrittenFiles != 1 {
		t.Fatalf("expected successful write stats, got %#v", stats)
	}
}

func TestInvokeToolStatsCountsDeniedWriteWithoutWrittenFile(t *testing.T) {
	runner := &SingleTurnRunner{
		invokers: map[string]einotool.InvokableTool{
			"edit_file": stubInvokableTool{result: "edited"},
		},
		toolMetadata: map[string]tools.RegisteredTool{
			"edit_file": {Kind: permission.ToolKindWrite},
		},
		mode:       permission.ModePlan,
		confirmer:  permission.DenyConfirmer{},
		contextMgr: contextmgr.NewDefaultManager(),
	}
	stats := StartTurnStats()

	runner.invokeTool(context.Background(), schema.ToolCall{
		ID: "call-1",
		Function: schema.FunctionCall{
			Name:      "edit_file",
			Arguments: `{"path":"README.md"}`,
		},
	}, NoopReporter{}, stats)

	if stats.ToolCalls != 1 || stats.DeniedToolCalls != 1 || stats.WrittenFiles != 0 {
		t.Fatalf("expected denied write stats without written file, got %#v", stats)
	}
}

func TestInvokeToolDeniedByDenyConfirmerMentionsNonInteractive(t *testing.T) {
	runner := &SingleTurnRunner{
		invokers: map[string]einotool.InvokableTool{
			"bash": stubInvokableTool{result: "ok"},
		},
		toolMetadata: map[string]tools.RegisteredTool{
			"bash": {Kind: permission.ToolKindShell},
		},
		mode:       permission.ModeDefault,
		confirmer:  permission.DenyConfirmer{},
		contextMgr: contextmgr.NewDefaultManager(),
	}

	message := runner.invokeTool(context.Background(), schema.ToolCall{
		ID: "call-1",
		Function: schema.FunctionCall{
			Name:      "bash",
			Arguments: `{"command":"go test ./..."}`,
		},
	}, NoopReporter{}, StartTurnStats())

	if !strings.Contains(message.Content, "非交互模式") || !strings.Contains(message.Content, "-prompt") {
		t.Fatalf("expected non-interactive denial, got %q", message.Content)
	}
}

func TestInvokeToolStatsCountsFailedBashAsStartedCommand(t *testing.T) {
	runner := &SingleTurnRunner{
		invokers: map[string]einotool.InvokableTool{
			"bash": stubInvokableTool{err: fmt.Errorf("boom")},
		},
		toolMetadata: map[string]tools.RegisteredTool{
			"bash": {Kind: permission.ToolKindShell},
		},
		mode:       permission.ModeDefault,
		confirmer:  &allowConfirmer{},
		contextMgr: contextmgr.NewDefaultManager(),
	}
	stats := StartTurnStats()

	runner.invokeTool(context.Background(), schema.ToolCall{
		ID: "call-1",
		Function: schema.FunctionCall{
			Name:      "bash",
			Arguments: `{"command":"go test ./..."}`,
		},
	}, NoopReporter{}, stats)

	if stats.ToolCalls != 1 || stats.BashCommands != 1 || stats.FailedToolCalls != 1 {
		t.Fatalf("expected failed bash stats, got %#v", stats)
	}
}

func TestWriteTurnFooterAppendsReadableStatusAndStats(t *testing.T) {
	runner := &SingleTurnRunner{
		contextMgr: contextmgr.NewManager(contextmgr.Budget{MaxTokens: 1000000}),
	}
	output := &bytes.Buffer{}
	messages := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("hello"),
		schema.AssistantMessage("answer", nil),
	}
	stats := StartTurnStats()
	stats.RecordModelCall()

	err := runner.writeTurnFooter(output, messages, stats)

	if err != nil {
		t.Fatalf("expected footer write to succeed: %v", err)
	}
	rendered := output.String()
	if !strings.Contains(rendered, "[已使用") {
		t.Fatalf("expected context usage marker, got %q", rendered)
	}
	if !strings.Contains(rendered, "上下文/1M]") {
		t.Fatalf("expected 1M budget marker, got %q", rendered)
	}
	if !strings.Contains(rendered, "[本轮") || !strings.Contains(rendered, "模型 1 次") || !strings.Contains(rendered, "工具 0 次") {
		t.Fatalf("expected turn stats marker, got %q", rendered)
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
	err    error
}

func (t stubInvokableTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	_ = ctx
	_ = argumentsInJSON
	_ = opts
	return t.result, t.err
}

type allowConfirmer struct {
	request permission.Request
}

func (c *allowConfirmer) Confirm(ctx context.Context, request permission.Request) (permission.Decision, error) {
	_ = ctx
	c.request = request
	return permission.DecisionAllow, nil
}
