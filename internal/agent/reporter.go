package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"nanocode/internal/diffview"
	"nanocode/internal/friendlyerror"
)

const maxReporterPreviewRunes = 120

const (
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
	ansiReset = "\x1b[0m"
)

type Reporter interface {
	ToolStart(toolName string, arguments string) time.Time
	ToolFinish(toolName string, arguments string, result string, preview string, startedAt time.Time)
	ToolDenied(toolName string, arguments string, reason string)
	ToolError(toolName string, arguments string, reason string, startedAt time.Time)
	SystemError(reason string)
}

type TerminalReporter struct {
	output io.Writer
}

type NoopReporter struct{}

type ToolSummary struct {
	ToolName string
	Status   string
	Detail   string
	Duration time.Duration
}

func NewTerminalReporter(output io.Writer) Reporter {
	if output == nil {
		return NoopReporter{}
	}
	return TerminalReporter{output: output}
}

func (r TerminalReporter) ToolStart(toolName string, arguments string) time.Time {
	startedAt := time.Now()
	fmt.Fprintf(r.output, "\n● %s%s\n", toolName, formatArgumentSummary(toolName, arguments))
	return startedAt
}

func (r TerminalReporter) ToolFinish(toolName string, arguments string, result string, preview string, startedAt time.Time) {
	summary := summarizeToolResult(toolName, arguments, result, preview)
	summary.Duration = time.Since(startedAt)
	fmt.Fprintf(r.output, "✓ %s 完成 %s，%s\n", toolName, formatDuration(summary.Duration), summary.Detail)
}

func (r TerminalReporter) ToolDenied(toolName string, arguments string, reason string) {
	fmt.Fprintf(r.output, "! %s 权限拒绝%s - %s\n", toolName, formatArgumentSummary(toolName, arguments), reason)
}

func (r TerminalReporter) ToolError(toolName string, arguments string, reason string, startedAt time.Time) {
	formatted := friendlyerror.FormatToolError(toolName, fmt.Errorf("%s", reason))
	if startedAt.IsZero() {
		fmt.Fprintf(r.output, "%s\n", formatted)
		return
	}
	fmt.Fprintf(r.output, "✗ %s 失败 %s\n%s\n", toolName, formatDuration(time.Since(startedAt)), formatted)
}

func (r TerminalReporter) SystemError(reason string) {
	fmt.Fprintf(r.output, "%s\n", friendlyerror.Format(fmt.Errorf("%s", reason)))
}

func (NoopReporter) ToolStart(toolName string, arguments string) time.Time { return time.Now() }
func (NoopReporter) ToolFinish(toolName string, arguments string, result string, preview string, startedAt time.Time) {
}
func (NoopReporter) ToolDenied(toolName string, arguments string, reason string) {}
func (NoopReporter) ToolError(toolName string, arguments string, reason string, startedAt time.Time) {
}
func (NoopReporter) SystemError(reason string) {}

func summarizeToolResult(toolName string, arguments string, result string, preview string) ToolSummary {
	detail := ""
	switch toolName {
	case "read_file":
		detail = summarizeReadFile(result)
	case "grep":
		detail = summarizeGrep(result)
	case "glob":
		detail = summarizeGlob(result)
	case "git_status":
		detail = summarizeGitStatus(result)
	case "write_file":
		detail = summarizeWriteFile(result, preview)
	case "edit_file":
		detail = summarizeEditFile(result, preview)
	case "bash":
		detail = summarizeBash(result)
	}

	if detail == "" {
		detail = strings.Trim(formatResultSummary(result), "（）")
	}
	return ToolSummary{ToolName: toolName, Status: "success", Detail: detail}
}

func summarizeReadFile(result string) string {
	firstLine := firstNonEmptyLine(result)
	if firstLine == "" {
		return ""
	}
	re := regexp.MustCompile(`^\[read_file\]\s+(.+?)\s+lines\s+(\d+)-(\d+)`)
	matches := re.FindStringSubmatch(firstLine)
	if len(matches) != 4 {
		return ""
	}
	detail := fmt.Sprintf("读取 %s lines %s-%s", matches[1], matches[2], matches[3])
	if strings.Contains(firstLine, "truncated") {
		detail += "，已截断"
	}
	return detail
}

func summarizeGrep(result string) string {
	trimmed := strings.TrimSpace(result)
	if trimmed == "(no matches)" {
		return "无命中"
	}
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("命中 %d 条", countContentLines(trimmed))
}

func summarizeGlob(result string) string {
	trimmed := strings.TrimSpace(result)
	if trimmed == "(no matches)" {
		return "返回 0 个文件"
	}
	if trimmed == "" {
		return ""
	}

	lines := nonEmptyLines(trimmed)
	fileCount := 0
	truncated := false
	for _, line := range lines {
		if strings.HasPrefix(line, "(truncated:") {
			truncated = true
			continue
		}
		fileCount++
	}
	detail := fmt.Sprintf("返回 %d 个文件", fileCount)
	if truncated {
		detail += "，已截断"
	}
	return detail
}

func summarizeGitStatus(result string) string {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "status: clean") {
		return "工作区 clean"
	}
	return fmt.Sprintf("状态摘要 %d 行", countContentLines(trimmed))
}

func summarizeWriteFile(result string, preview string) string {
	re := regexp.MustCompile(`^write_file succeeded:\s+(.+?)\s+\((\d+)\s+bytes,\s+checkpoint created\)`)
	matches := re.FindStringSubmatch(strings.TrimSpace(result))
	if len(matches) != 3 {
		return ""
	}
	if detail := summarizeWriteDiff(matches[1], preview); detail != "" {
		return detail
	}
	return fmt.Sprintf("写入 %s，%s bytes，checkpoint created", matches[1], matches[2])
}

func summarizeEditFile(result string, preview string) string {
	re := regexp.MustCompile(`^edit_file succeeded:\s+(.+?)\s+\((\d+)\s+replacement\(s\),\s+checkpoint created\)`)
	matches := re.FindStringSubmatch(strings.TrimSpace(result))
	if len(matches) != 3 {
		return ""
	}
	if detail := summarizeWriteDiff(matches[1], preview); detail != "" {
		return detail
	}
	return fmt.Sprintf("替换 %s 处：%s，checkpoint created", matches[2], matches[1])
}

func summarizeWriteDiff(path string, preview string) string {
	if strings.TrimSpace(preview) == "" || strings.HasPrefix(preview, "diff 预览不可用：") {
		return ""
	}
	summary := diffview.Summarize(preview)
	action := "修改"
	if summary.IsNewFile {
		action = "新建"
	}
	return fmt.Sprintf("%s %s，%s %s", action, path, colorAddedCount(summary.Added), colorRemovedCount(summary.Removed))
}

func colorAddedCount(count int) string {
	return ansiGreen + fmt.Sprintf("+%d", count) + ansiReset
}

func colorRemovedCount(count int) string {
	return ansiRed + fmt.Sprintf("-%d", count) + ansiReset
}

func summarizeBash(result string) string {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" || trimmed == "bash command succeeded with no output" {
		return "命令无输出"
	}
	return fmt.Sprintf("输出 %d 行，%d 字符", countContentLines(trimmed), utf8.RuneCountInString(trimmed))
}

func formatArgumentSummary(toolName string, arguments string) string {
	var values map[string]any
	if err := json.Unmarshal([]byte(arguments), &values); err != nil {
		return " args=" + quotePreview(arguments)
	}

	keys := argumentSummaryKeys(toolName)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, quotePreview(formatJSONValue(value))))
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

func argumentSummaryKeys(toolName string) []string {
	switch toolName {
	case "read_file":
		return []string{"file_path", "path", "offset", "limit"}
	case "grep":
		return []string{"pattern", "path"}
	case "glob":
		return []string{"pattern"}
	case "git_status":
		return []string{"workspace"}
	case "write_file", "edit_file":
		return []string{"path"}
	case "bash":
		return []string{"command"}
	default:
		return []string{"path", "pattern", "command"}
	}
}

func formatResultSummary(result string) string {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return "（无输出）"
	}

	lineCount := countContentLines(trimmed)
	runeCount := utf8.RuneCountInString(trimmed)
	return fmt.Sprintf("（输出 %d 行，%d 字符）", lineCount, runeCount)
}

func formatDuration(duration time.Duration) string {
	if duration < time.Second {
		return fmt.Sprintf("%dms", maxInt(1, int(duration/time.Millisecond)))
	}
	seconds := duration.Seconds()
	if seconds == float64(int(seconds)) {
		return fmt.Sprintf("%ds", int(seconds))
	}
	return fmt.Sprintf("%.1fs", seconds)
}

func quotePreview(value string) string {
	preview := strings.TrimSpace(value)
	preview = strings.ReplaceAll(preview, "\n", "\\n")
	if utf8.RuneCountInString(preview) > maxReporterPreviewRunes {
		preview = firstRunesForReporter(preview, maxReporterPreviewRunes) + "..."
	}
	return fmt.Sprintf("%q", preview)
}

func formatJSONValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(typed)
	}
}

func firstRunesForReporter(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	var builder strings.Builder
	count := 0
	for _, r := range text {
		if count >= limit {
			break
		}
		builder.WriteRune(r)
		count++
	}
	return builder.String()
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func nonEmptyLines(text string) []string {
	rawLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func countContentLines(text string) int {
	return len(nonEmptyLines(text))
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
