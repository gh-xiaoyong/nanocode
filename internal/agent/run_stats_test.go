package agent

import (
	"strings"
	"testing"
	"time"

	"nanocode/internal/permission"
)

func TestTurnStatsFormatsRequiredFields(t *testing.T) {
	stats := StartTurnStats()
	stats.StartedAt = time.Now().Add(-1500 * time.Millisecond)
	stats.RecordModelCall()
	stats.RecordModelCall()
	stats.RecordToolCall("read_file", permission.ToolKindRead)

	rendered := stats.FormatTurnStats()

	if !strings.Contains(rendered, "本轮") || !strings.Contains(rendered, "模型 2 次") || !strings.Contains(rendered, "工具 1 次") {
		t.Fatalf("expected required stats fields, got %q", rendered)
	}
}

func TestTurnStatsHidesZeroOptionalFields(t *testing.T) {
	stats := StartTurnStats()
	stats.RecordModelCall()

	rendered := stats.FormatTurnStats()

	for _, hidden := range []string{"写入", "bash", "拒绝", "失败"} {
		if strings.Contains(rendered, hidden) {
			t.Fatalf("expected %q to be hidden in %q", hidden, rendered)
		}
	}
}

func TestTurnStatsCountsToolOutcomes(t *testing.T) {
	stats := StartTurnStats()

	stats.RecordToolCall("write_file", permission.ToolKindWrite)
	stats.RecordToolSuccess("write_file", permission.ToolKindWrite)
	stats.RecordToolCall("edit_file", permission.ToolKindWrite)
	stats.RecordToolDenied("edit_file", permission.ToolKindWrite)
	stats.RecordToolCall("grep", permission.ToolKindRead)
	stats.RecordToolError("grep", permission.ToolKindRead)

	if stats.ToolCalls != 3 {
		t.Fatalf("expected 3 tool calls, got %d", stats.ToolCalls)
	}
	if stats.SuccessfulToolCalls != 1 || stats.DeniedToolCalls != 1 || stats.FailedToolCalls != 1 {
		t.Fatalf("unexpected outcome counts: %#v", stats)
	}
	if stats.WrittenFiles != 1 {
		t.Fatalf("expected successful write_file to count as one written file, got %d", stats.WrittenFiles)
	}

	rendered := stats.FormatTurnStats()
	if !strings.Contains(rendered, "写入 1 个文件") || !strings.Contains(rendered, "拒绝 1 次") || !strings.Contains(rendered, "失败 1 次") {
		t.Fatalf("expected optional outcome fields, got %q", rendered)
	}
}

func TestTurnStatsCountsBashCommands(t *testing.T) {
	stats := StartTurnStats()

	stats.RecordToolCall("bash", permission.ToolKindShell)
	stats.RecordBashCommand()
	stats.RecordToolError("bash", permission.ToolKindShell)

	if stats.BashCommands != 1 {
		t.Fatalf("expected one bash command, got %d", stats.BashCommands)
	}
	if !strings.Contains(stats.FormatTurnStats(), "bash 1 次") {
		t.Fatalf("expected bash count in formatted stats, got %q", stats.FormatTurnStats())
	}
}
