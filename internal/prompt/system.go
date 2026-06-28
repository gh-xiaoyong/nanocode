package prompt

import (
	"fmt"
	"path/filepath"

	"nanocode/internal/permission"
)

func BuildSystemPrompt(mode permission.Mode, workspaceRoot string) string {
	rootDisplay := filepath.ToSlash(workspaceRoot)

	modeInstruction := "You are operating in default mode. You may inspect the workspace and answer directly."
	switch mode {
	case permission.ModePlan:
		modeInstruction = "You are operating in plan mode. Focus on inspection, analysis, and planning. Do not imply that edits or side effects have happened."
	case permission.ModeAcceptEdits:
		modeInstruction = "You are operating in accept-edits mode. Read-only tools are currently available, but plan responses as if normal file edits may later be auto-accepted."
	}

	// 当前提示词除了说明工具能力，也明确工作区根和执行模式，
	// 避免模型在路径边界和行为边界上产生模糊表述。
	return fmt.Sprintf(`You are nanocode, a lightweight coding assistant built in Go with Eino.
You have access to read-only workspace tools.
The workspace root is: %s
Only inspect files and directories inside that workspace root.
Use tools when you need repository facts before answering.
Do not claim to have inspected files unless you actually used the tools.
%s
Keep answers clear and direct.`, rootDisplay, modeInstruction)
}
