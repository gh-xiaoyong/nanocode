package prompt

import (
	"fmt"
	"path/filepath"
	"strings"

	"nanocode/internal/memory"
	"nanocode/internal/permission"
)

func BuildSystemPrompt(mode permission.Mode, workspaceRoot string, mem memory.Memory) string {
	// system prompt 由当前 mode、workspace 和长期记忆动态生成，保证模型看到的边界和运行时一致。
	rootDisplay := filepath.ToSlash(workspaceRoot)

	modeInstruction := "You are operating in default mode. You may read the workspace. File writes and shell commands require user confirmation."
	switch mode {
	case permission.ModePlan:
		modeInstruction = "You are operating in plan mode. Only inspect, search, analyze, and propose plans. Write and shell tools are not available. Do not ask the user to confirm execution; explain that they must switch out of plan mode before edits can be made."
	case permission.ModeAcceptEdits:
		modeInstruction = "You are operating in accept-edits mode. Normal file edits may be accepted automatically, but shell commands still require user confirmation."
	}

	memoryBlock := buildMemoryBlock(mem)

	return fmt.Sprintf(`You are nanocode, a lightweight coding assistant built in Go with Eino.
The workspace root is: %s
Only inspect or modify files inside that workspace root.

Available tool categories:
- read tools: read_file, ls, grep, glob, git_status
- write tools: write_file, edit_file (not available in plan mode)
- shell tool: bash (not available in plan mode)

Editing guidance:
- Prefer edit_file for targeted changes.
- Use write_file only when creating a new file or replacing a whole file is clearly appropriate.
- For large files, read only the needed line window first, then use precise edit_file replacements.
- Before using bash, explain the purpose briefly in your assistant text.
- Tool outputs may be truncated by the context manager. If that happens, narrow the query or read smaller file windows with offset/limit.

%s%s
Do not claim to have inspected or modified files unless you actually used the tools.
Keep answers clear and direct.`, rootDisplay, memoryBlock, modeInstruction)
}

func buildMemoryBlock(mem memory.Memory) string {
	loaded := mem.LoadedSources()
	if len(loaded) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("Memory:\n")
	builder.WriteString("The following long-term instructions and project conventions should be followed when relevant.\n")
	for _, source := range loaded {
		builder.WriteString(fmt.Sprintf("\n[%s memory: %s]\n", source.Kind, filepath.ToSlash(source.Path)))
		builder.WriteString(source.Content)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	return builder.String()
}
