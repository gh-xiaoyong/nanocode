package contextmgr

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestEstimateTokensReturnsPositiveForNonEmptyText(t *testing.T) {
	manager := NewDefaultManager()

	if got := manager.EstimateTokens("hello"); got <= 0 {
		t.Fatalf("expected positive token estimate, got %d", got)
	}
}

func TestFormatUsageUsesReadableBudgetUnit(t *testing.T) {
	manager := NewManager(Budget{MaxTokens: 1000000})
	messages := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage(strings.Repeat("a", 10000)),
	}

	output := manager.FormatUsage(messages)

	if !strings.HasPrefix(output, "[已使用") {
		t.Fatalf("expected Chinese usage prefix, got %q", output)
	}
	if !strings.Contains(output, "上下文/1M]") {
		t.Fatalf("expected readable 1M budget, got %q", output)
	}
}

func TestTrimToolResultTruncatesLongOutput(t *testing.T) {
	manager := NewManager(Budget{ToolOutputChars: 10})

	output := manager.TrimToolResult("grep", strings.Repeat("a", 20))

	if !strings.Contains(output, "tool output truncated") {
		t.Fatalf("expected truncation notice, got %q", output)
	}
	if !strings.Contains(output, "Kept: 10 characters") {
		t.Fatalf("expected kept length, got %q", output)
	}
}

func TestTrimToolResultLeavesShortOutputUnchanged(t *testing.T) {
	manager := NewManager(Budget{ToolOutputChars: 100})
	output := "short result"

	if got := manager.TrimToolResult("grep", output); got != output {
		t.Fatalf("expected short output unchanged, got %q", got)
	}
}

func TestCompactMessagesCreatesSummaryWhenOverBudget(t *testing.T) {
	manager := NewManager(Budget{MaxTokens: 20, CompactionRatio: 0.5, RecentMessageCount: 2})
	messages := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("current user request"),
		schema.AssistantMessage(strings.Repeat("old assistant ", 20), nil),
		schema.ToolMessage(strings.Repeat("old tool ", 20), "call-1", schema.WithToolName("grep")),
		schema.AssistantMessage("recent assistant", nil),
		schema.ToolMessage("recent tool", "call-2", schema.WithToolName("read_file")),
	}

	result := manager.CompactMessages(messages)

	if !result.Compacted {
		t.Fatal("expected messages to be compacted")
	}
	if result.Messages[0].Role != schema.System {
		t.Fatalf("expected first message to remain system, got %s", result.Messages[0].Role)
	}
	if !strings.Contains(result.Messages[1].Content, "Earlier conversation summary") {
		t.Fatalf("expected summary message, got %q", result.Messages[1].Content)
	}
	if result.Messages[len(result.Messages)-1].Content != "recent tool" {
		t.Fatalf("expected recent messages to be preserved, got %q", result.Messages[len(result.Messages)-1].Content)
	}
}

func TestCompactMessagesLeavesSmallInputUnchanged(t *testing.T) {
	manager := NewManager(Budget{MaxTokens: 32000})
	messages := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("hello"),
	}

	result := manager.CompactMessages(messages)

	if result.Compacted {
		t.Fatal("expected small messages not to be compacted")
	}
	if len(result.Messages) != len(messages) {
		t.Fatalf("expected unchanged message count, got %d", len(result.Messages))
	}
}
