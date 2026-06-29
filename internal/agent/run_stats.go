package agent

import (
	"fmt"
	"strings"
	"time"

	"nanocode/internal/permission"
)

type TurnStats struct {
	StartedAt           time.Time
	ModelCalls          int
	ToolCalls           int
	SuccessfulToolCalls int
	DeniedToolCalls     int
	FailedToolCalls     int
	WrittenFiles        int
	BashCommands        int
}

func StartTurnStats() *TurnStats {
	return &TurnStats{StartedAt: time.Now()}
}

func (s *TurnStats) RecordModelCall() {
	if s == nil {
		return
	}
	s.ModelCalls++
}

func (s *TurnStats) RecordToolCall(toolName string, kind permission.ToolKind) {
	if s == nil {
		return
	}
	_ = toolName
	_ = kind
	s.ToolCalls++
}

func (s *TurnStats) RecordToolSuccess(toolName string, kind permission.ToolKind) {
	if s == nil {
		return
	}
	_ = kind
	s.SuccessfulToolCalls++
	if isWriteFileTool(toolName) {
		s.WrittenFiles++
	}
}

func (s *TurnStats) RecordToolDenied(toolName string, kind permission.ToolKind) {
	if s == nil {
		return
	}
	_ = toolName
	_ = kind
	s.DeniedToolCalls++
}

func (s *TurnStats) RecordToolError(toolName string, kind permission.ToolKind) {
	if s == nil {
		return
	}
	_ = toolName
	_ = kind
	s.FailedToolCalls++
}

func (s *TurnStats) RecordBashCommand() {
	if s == nil {
		return
	}
	s.BashCommands++
}

func (s *TurnStats) FormatTurnStats() string {
	if s == nil {
		return ""
	}
	parts := []string{
		fmt.Sprintf("本轮 %s", formatDuration(time.Since(s.StartedAt))),
		fmt.Sprintf("模型 %d 次", s.ModelCalls),
		fmt.Sprintf("工具 %d 次", s.ToolCalls),
	}
	if s.WrittenFiles > 0 {
		parts = append(parts, fmt.Sprintf("写入 %d 个文件", s.WrittenFiles))
	}
	if s.BashCommands > 0 {
		parts = append(parts, fmt.Sprintf("bash %d 次", s.BashCommands))
	}
	if s.DeniedToolCalls > 0 {
		parts = append(parts, fmt.Sprintf("拒绝 %d 次", s.DeniedToolCalls))
	}
	if s.FailedToolCalls > 0 {
		parts = append(parts, fmt.Sprintf("失败 %d 次", s.FailedToolCalls))
	}
	return "[" + strings.Join(parts, " · ") + "]"
}

func isWriteFileTool(toolName string) bool {
	return toolName == "write_file" || toolName == "edit_file"
}
