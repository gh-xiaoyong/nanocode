package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolu "github.com/cloudwego/eino/components/tool/utils"

	"nanocode/internal/permission"
)

type listDirectoryInput struct {
	Path string `json:"path,omitempty" jsonschema_description:"要列出的目录路径，省略时默认当前项目根目录"`
}

func newListDirectoryTool(workspace *permission.WorkspacePolicy) (einotool.BaseTool, error) {
	// ls 帮模型先摸清目录结构，减少它在文件名未知时盲猜路径。
	return toolu.InferTool(
		"ls",
		"列出工作区内某个目录的直接子项，用于快速了解项目结构。",
		func(ctx context.Context, input listDirectoryInput) (string, error) {
			_ = ctx

			resolvedPath, err := resolvePath(workspace, input.Path)
			if err != nil {
				return "", err
			}

			fileInfo, err := os.Stat(resolvedPath)
			if err != nil {
				return "", fmt.Errorf("stat directory: %w", err)
			}
			if !fileInfo.IsDir() {
				return "", fmt.Errorf("path %q is not a directory", input.Path)
			}

			entries, err := os.ReadDir(resolvedPath)
			if err != nil {
				return "", fmt.Errorf("read directory: %w", err)
			}
			if len(entries) == 0 {
				return "(empty directory)", nil
			}

			lines := make([]string, 0, len(entries))
			for _, entry := range entries {
				displayName := entry.Name()
				if entry.IsDir() {
					displayName += "/"
				}

				relativePath, err := filepath.Rel(workspace.RootDir(), filepath.Join(resolvedPath, entry.Name()))
				if err != nil {
					return "", fmt.Errorf("build relative path: %w", err)
				}

				lines = append(lines, fmt.Sprintf("%s\t%s", displayName, filepath.ToSlash(relativePath)))
			}

			sort.Strings(lines)
			return strings.Join(lines, "\n"), nil
		},
	)
}
