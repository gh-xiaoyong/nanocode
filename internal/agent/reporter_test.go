package agent

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTerminalReporterToolStartIncludesArgumentSummary(t *testing.T) {
	output := &bytes.Buffer{}
	reporter := NewTerminalReporter(output)

	reporter.ToolStart("read_file", `{"file_path":"internal/agent/loop.go","offset":3,"limit":5}`)

	rendered := output.String()
	if !strings.Contains(rendered, "● read_file") {
		t.Fatalf("expected tool start line, got %q", rendered)
	}
	if !strings.Contains(rendered, `file_path="internal/agent/loop.go"`) || !strings.Contains(rendered, `offset="3"`) {
		t.Fatalf("expected argument summary, got %q", rendered)
	}
}

func TestTerminalReporterToolFinishShowsDurationAndSummary(t *testing.T) {
	output := &bytes.Buffer{}
	reporter := NewTerminalReporter(output)

	reporter.ToolFinish("grep", `{"pattern":"nanocode"}`, "main.go:1: nanocode\nREADME.md:3: nanocode", "", time.Now().Add(-20*time.Millisecond))

	rendered := output.String()
	if !strings.Contains(rendered, "✓ grep 完成") {
		t.Fatalf("expected tool finish line, got %q", rendered)
	}
	if !strings.Contains(rendered, "命中 2 条") {
		t.Fatalf("expected grep summary, got %q", rendered)
	}
	if strings.Contains(rendered, "main.go:1") {
		t.Fatalf("expected full result to stay hidden, got %q", rendered)
	}
}

func TestTerminalReporterToolErrorIncludesDurationAndReason(t *testing.T) {
	output := &bytes.Buffer{}
	reporter := NewTerminalReporter(output)

	reporter.ToolError("bash", `{"command":"go test ./..."}`, "timeout", time.Now().Add(-2*time.Second))

	rendered := output.String()
	if !strings.Contains(rendered, "✗ bash 失败 2s") || !strings.Contains(rendered, "工具失败：命令超时") || !strings.Contains(rendered, "timeout_seconds") {
		t.Fatalf("expected tool error line, got %q", rendered)
	}
}

func TestTerminalReporterSystemErrorUsesFriendlyMessage(t *testing.T) {
	output := &bytes.Buffer{}
	reporter := NewTerminalReporter(output)

	reporter.SystemError("start model stream: unauthorized")

	rendered := output.String()
	if !strings.Contains(rendered, "模型请求错误") || !strings.Contains(rendered, "base_url") {
		t.Fatalf("expected friendly model error, got %q", rendered)
	}
}

func TestTerminalReporterToolDeniedIncludesArguments(t *testing.T) {
	output := &bytes.Buffer{}
	reporter := NewTerminalReporter(output)

	reporter.ToolDenied("edit_file", `{"path":"README.md"}`, "denied in plan mode")

	rendered := output.String()
	if !strings.Contains(rendered, `! edit_file 权限拒绝 path="README.md" - denied in plan mode`) {
		t.Fatalf("expected denied line, got %q", rendered)
	}
}

func TestSummarizeReadFileResult(t *testing.T) {
	summary := summarizeToolResult("read_file", `{}`, "[read_file] loop.go lines 1-200 (truncated; use offset=201, limit=200 to continue)\ncontent", "")

	if summary.Detail != "读取 loop.go lines 1-200，已截断" {
		t.Fatalf("unexpected read_file summary: %q", summary.Detail)
	}
}

func TestSummarizeGrepResult(t *testing.T) {
	summary := summarizeToolResult("grep", `{}`, "main.go:1: nanocode\nREADME.md:3: nanocode", "")

	if summary.Detail != "命中 2 条" {
		t.Fatalf("unexpected grep summary: %q", summary.Detail)
	}
}

func TestSummarizeGrepNoMatches(t *testing.T) {
	summary := summarizeToolResult("grep", `{}`, "(no matches)", "")

	if summary.Detail != "无命中" {
		t.Fatalf("unexpected grep no-match summary: %q", summary.Detail)
	}
}

func TestSummarizeGlobResult(t *testing.T) {
	summary := summarizeToolResult("glob", `{}`, "a.tsx\nb.tsx\n(truncated: showing first 100 matches, refine pattern to see more)", "")

	if summary.Detail != "返回 2 个文件，已截断" {
		t.Fatalf("unexpected glob summary: %q", summary.Detail)
	}
}

func TestSummarizeWriteFileResult(t *testing.T) {
	summary := summarizeToolResult("write_file", `{}`, "write_file succeeded: README.md (123 bytes, checkpoint created)", "")

	if summary.Detail != "写入 README.md，123 bytes，checkpoint created" {
		t.Fatalf("unexpected write_file summary: %q", summary.Detail)
	}
}

func TestSummarizeWriteFileResultWithDiffPreview(t *testing.T) {
	preview := "--- /dev/null\n+++ b/demo.txt\n@@ -0,0 +1,2 @@\n+one\n+two\n"

	summary := summarizeToolResult("write_file", `{}`, "write_file succeeded: demo.txt (8 bytes, checkpoint created)", preview)

	if !strings.Contains(summary.Detail, "新建 demo.txt") || !strings.Contains(summary.Detail, ansiGreen+"+2"+ansiReset) || !strings.Contains(summary.Detail, ansiRed+"-0"+ansiReset) {
		t.Fatalf("unexpected write_file diff summary: %q", summary.Detail)
	}
}

func TestSummarizeEditFileResult(t *testing.T) {
	summary := summarizeToolResult("edit_file", `{}`, "edit_file succeeded: internal/foo.go (2 replacement(s), checkpoint created)", "")

	if summary.Detail != "替换 2 处：internal/foo.go，checkpoint created" {
		t.Fatalf("unexpected edit_file summary: %q", summary.Detail)
	}
}

func TestSummarizeEditFileResultWithDiffPreview(t *testing.T) {
	preview := "--- a/internal/foo.go\n+++ b/internal/foo.go\n@@ -1,1 +1,1 @@\n-old\n+new\n"

	summary := summarizeToolResult("edit_file", `{}`, "edit_file succeeded: internal/foo.go (1 replacement(s), checkpoint created)", preview)

	if !strings.Contains(summary.Detail, "修改 internal/foo.go") || !strings.Contains(summary.Detail, ansiGreen+"+1"+ansiReset) || !strings.Contains(summary.Detail, ansiRed+"-1"+ansiReset) {
		t.Fatalf("unexpected edit_file diff summary: %q", summary.Detail)
	}
}

func TestSummarizeBashResult(t *testing.T) {
	summary := summarizeToolResult("bash", `{}`, "one\ntwo", "")

	if summary.Detail != "输出 2 行，7 字符" {
		t.Fatalf("unexpected bash summary: %q", summary.Detail)
	}
}

func TestFormatDurationUsesMillisecondsAndSeconds(t *testing.T) {
	if got := formatDuration(18 * time.Millisecond); got != "18ms" {
		t.Fatalf("expected ms duration, got %q", got)
	}
	if got := formatDuration(30 * time.Second); got != "30s" {
		t.Fatalf("expected s duration, got %q", got)
	}
}

func TestSummarizeFallbackToGenericSummary(t *testing.T) {
	summary := summarizeToolResult("unknown_tool", `{}`, "line one\nline two", "")

	if summary.Detail != "输出 2 行，17 字符" {
		t.Fatalf("unexpected fallback summary: %q", summary.Detail)
	}
}

func TestFormatArgumentSummaryFallsBackToRawJSONPreview(t *testing.T) {
	summary := formatArgumentSummary("grep", `{"pattern":`)

	if !strings.Contains(summary, `args="{\"pattern\":"`) {
		t.Fatalf("expected raw args fallback, got %q", summary)
	}
}
