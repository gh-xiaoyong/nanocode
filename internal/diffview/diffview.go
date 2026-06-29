package diffview

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Summary struct {
	Added     int
	Removed   int
	IsNewFile bool
}

type diffOp struct {
	kind string
	text string
}

// UnifiedDiff 生成给终端用户查看的 unified diff。MVP 先输出整文件 hunk，
// 这样实现简单稳定，也足够支撑权限确认前的变更预览。
func UnifiedDiff(path string, oldContent string, newContent string, oldExists bool) string {
	if oldExists && oldContent == newContent {
		return ""
	}

	slashPath := filepath.ToSlash(path)
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	oldName := "a/" + slashPath
	if !oldExists {
		oldName = "/dev/null"
	}
	newName := "b/" + slashPath

	var builder strings.Builder
	fmt.Fprintf(&builder, "--- %s\n", oldName)
	fmt.Fprintf(&builder, "+++ %s\n", newName)
	fmt.Fprintf(&builder, "@@ -%s +%s @@\n", rangeHeader(len(oldLines), oldExists), rangeHeader(len(newLines), true))

	for _, op := range buildOps(oldLines, newLines) {
		builder.WriteString(op.kind)
		builder.WriteString(op.text)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func Summarize(diff string) Summary {
	summary := Summary{IsNewFile: strings.Contains(diff, "--- /dev/null")}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			summary.Added++
		case strings.HasPrefix(line, "-"):
			summary.Removed++
		}
	}
	return summary
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.TrimSuffix(normalized, "\n")
	if normalized == "" {
		return nil
	}
	return strings.Split(normalized, "\n")
}

func rangeHeader(lineCount int, exists bool) string {
	if !exists || lineCount == 0 {
		return "0,0"
	}
	return fmt.Sprintf("1,%d", lineCount)
}

func buildOps(oldLines []string, newLines []string) []diffOp {
	table := lcsTable(oldLines, newLines)
	ops := make([]diffOp, 0, len(oldLines)+len(newLines))
	i, j := 0, 0
	for i < len(oldLines) && j < len(newLines) {
		switch {
		case oldLines[i] == newLines[j]:
			ops = append(ops, diffOp{kind: " ", text: oldLines[i]})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			ops = append(ops, diffOp{kind: "-", text: oldLines[i]})
			i++
		default:
			ops = append(ops, diffOp{kind: "+", text: newLines[j]})
			j++
		}
	}
	for i < len(oldLines) {
		ops = append(ops, diffOp{kind: "-", text: oldLines[i]})
		i++
	}
	for j < len(newLines) {
		ops = append(ops, diffOp{kind: "+", text: newLines[j]})
		j++
	}
	return ops
}

func lcsTable(oldLines []string, newLines []string) [][]int {
	table := make([][]int, len(oldLines)+1)
	for i := range table {
		table[i] = make([]int, len(newLines)+1)
	}
	for i := len(oldLines) - 1; i >= 0; i-- {
		for j := len(newLines) - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				table[i][j] = table[i+1][j+1] + 1
				continue
			}
			table[i][j] = maxInt(table[i+1][j], table[i][j+1])
		}
	}
	return table
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
