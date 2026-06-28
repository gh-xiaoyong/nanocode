package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolu "github.com/cloudwego/eino/components/tool/utils"

	"nanocode/internal/checkpoint"
	"nanocode/internal/permission"
)

type writeFileInput struct {
	Path    string `json:"path" jsonschema:"required" jsonschema_description:"要写入的文件路径，相对路径默认相对于当前项目根目录"`
	Content string `json:"content" jsonschema:"required" jsonschema_description:"要写入文件的完整内容"`
}

func newWriteFileTool(workspace *permission.WorkspacePolicy, checkpoints *checkpoint.Manager) (einotool.BaseTool, error) {
	// write_file 用于创建或覆盖完整文件；执行前的权限确认由 Agent loop 统一处理。
	return toolu.InferTool(
		"write_file",
		"创建或覆盖工作区内的指定文件。父目录必须已存在；写入前会创建最近一次修改 checkpoint，可通过 /undo 撤销。",
		func(ctx context.Context, input writeFileInput) (string, error) {
			_ = ctx

			resolvedPath, err := resolvePath(workspace, input.Path)
			if err != nil {
				return "", err
			}

			parentDir := filepath.Dir(resolvedPath)
			// MVP 不自动创建父目录，避免模型误造一串意外路径。
			parentInfo, err := os.Stat(parentDir)
			if err != nil {
				return "", fmt.Errorf("stat parent directory: %w", err)
			}
			if !parentInfo.IsDir() {
				return "", fmt.Errorf("parent path %q is not a directory", parentDir)
			}

			if info, err := os.Stat(resolvedPath); err == nil && info.IsDir() {
				return "", fmt.Errorf("path %q is a directory, not a file", input.Path)
			} else if err != nil && !os.IsNotExist(err) {
				return "", fmt.Errorf("stat file: %w", err)
			}

			if err := checkpoints.SaveBeforeWrite(resolvedPath); err != nil {
				return "", err
			}

			// 真正写入前已经保存 checkpoint，因此 /undo 能恢复旧文件或删除新文件。
			if err := os.WriteFile(resolvedPath, []byte(input.Content), 0o644); err != nil {
				return "", fmt.Errorf("write file: %w", err)
			}

			relativePath, err := filepath.Rel(workspace.RootDir(), resolvedPath)
			if err != nil {
				relativePath = resolvedPath
			}

			return fmt.Sprintf("write_file succeeded: %s (%d bytes, checkpoint created)", filepath.ToSlash(relativePath), len([]byte(input.Content))), nil
		},
	)
}

type editFileInput struct {
	Path       string `json:"path" jsonschema:"required" jsonschema_description:"要编辑的文件路径，相对路径默认相对于当前项目根目录"`
	OldString  string `json:"old_string" jsonschema:"required" jsonschema_description:"要被替换的精确原始字符串"`
	NewString  string `json:"new_string" jsonschema:"required" jsonschema_description:"替换后的新字符串"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema_description:"是否替换所有匹配；默认 false 时要求 old_string 只匹配一次"`
}

func newEditFileTool(workspace *permission.WorkspacePolicy, checkpoints *checkpoint.Manager) (einotool.BaseTool, error) {
	// edit_file 只做精确字符串替换，避免模型用模糊 patch 误伤相邻代码。
	return toolu.InferTool(
		"edit_file",
		"对工作区内文件执行精确字符串替换。默认要求 old_string 只匹配一次；写入前会创建最近一次修改 checkpoint，可通过 /undo 撤销。",
		func(ctx context.Context, input editFileInput) (string, error) {
			_ = ctx

			if input.OldString == "" {
				return "", fmt.Errorf("old_string must not be empty")
			}

			resolvedPath, err := resolvePath(workspace, input.Path)
			if err != nil {
				return "", err
			}

			info, err := os.Stat(resolvedPath)
			if err != nil {
				return "", fmt.Errorf("stat file: %w", err)
			}
			if info.IsDir() {
				return "", fmt.Errorf("path %q is a directory, not a file", input.Path)
			}

			contentBytes, err := os.ReadFile(resolvedPath)
			if err != nil {
				return "", fmt.Errorf("read file: %w", err)
			}

			content := string(contentBytes)
			matchCount := strings.Count(content, input.OldString)
			if matchCount == 0 {
				return "", fmt.Errorf("old_string was not found in %q", input.Path)
			}
			if matchCount > 1 && !input.ReplaceAll {
				// 默认要求唯一匹配，逼迫模型提供足够具体的上下文。
				return "", fmt.Errorf("old_string matched %d times; provide a more specific old_string or set replace_all=true", matchCount)
			}

			replaceCount := 1
			if input.ReplaceAll {
				replaceCount = -1
			}
			nextContent := strings.Replace(content, input.OldString, input.NewString, replaceCount)

			if err := checkpoints.SaveBeforeWrite(resolvedPath); err != nil {
				return "", err
			}
			if err := os.WriteFile(resolvedPath, []byte(nextContent), 0o644); err != nil {
				return "", fmt.Errorf("write edited file: %w", err)
			}

			relativePath, err := filepath.Rel(workspace.RootDir(), resolvedPath)
			if err != nil {
				relativePath = resolvedPath
			}

			replacements := matchCount
			if !input.ReplaceAll {
				replacements = 1
			}
			return fmt.Sprintf("edit_file succeeded: %s (%d replacement(s), checkpoint created)", filepath.ToSlash(relativePath), replacements), nil
		},
	)
}
