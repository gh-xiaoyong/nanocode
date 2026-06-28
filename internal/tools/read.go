package tools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolu "github.com/cloudwego/eino/components/tool/utils"

	"nanocode/internal/permission"
)

const defaultReadFileLineLimit = 2000

type readFileInput struct {
	FilePath string `json:"file_path" jsonschema:"required" jsonschema_description:"要读取的文件路径，相对路径默认相对于当前项目根目录"`
	Offset   int    `json:"offset,omitempty" jsonschema_description:"从第几行开始读取，1-based；省略或小于 1 时默认从第 1 行开始"`
	Limit    int    `json:"limit,omitempty" jsonschema_description:"最多读取多少行；省略或小于 1 时默认读取 2000 行"`
}

func newReadFileTool(workspace *permission.WorkspacePolicy) (einotool.BaseTool, error) {
	// read_file 是阶段 1 最关键的只读工具。
	// 它按需分段读取文件，避免一次性把大文件塞进上下文。
	return toolu.InferTool(
		"read_file",
		"按需读取工作区内指定文件的文本内容，默认读取前 2000 行；可用 offset 和 limit 分段读取更靠后的内容。",
		func(ctx context.Context, input readFileInput) (string, error) {
			_ = ctx

			resolvedPath, err := resolvePath(workspace, input.FilePath)
			if err != nil {
				return "", err
			}

			fileInfo, err := os.Stat(resolvedPath)
			if err != nil {
				return "", fmt.Errorf("stat file: %w", err)
			}
			if fileInfo.IsDir() {
				return "", fmt.Errorf("path %q is a directory, not a file", input.FilePath)
			}

			result, err := readFileWindow(resolvedPath, input.Offset, input.Limit)
			if err != nil {
				return "", fmt.Errorf("read file: %w", err)
			}

			return result, nil
		},
	)
}

func readFileWindow(filePath string, offset int, limit int) (string, error) {
	startLine := offset
	if startLine < 1 {
		startLine = 1
	}

	lineLimit := limit
	if lineLimit < 1 {
		lineLimit = defaultReadFileLineLimit
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	lines := make([]string, 0, min(lineLimit, 64))
	currentLine := 0
	hasMore := false
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return "", readErr
		}

		if line != "" {
			currentLine++
			if currentLine >= startLine && len(lines) < lineLimit {
				lines = append(lines, strings.TrimRight(line, "\r\n"))
			}
			if currentLine >= startLine && len(lines) >= lineLimit {
				if readErr != io.EOF {
					hasMore = true
				}
				break
			}
		}

		if readErr == io.EOF {
			break
		}
	}

	endLine := startLine + len(lines) - 1
	if len(lines) == 0 {
		endLine = startLine - 1
	}

	header := fmt.Sprintf("[read_file] %s lines %d-%d", filepath.Base(filePath), startLine, endLine)
	if hasMore {
		header += fmt.Sprintf(" (truncated; use offset=%d, limit=%d to continue)", endLine+1, lineLimit)
	}

	if len(lines) == 0 {
		return header + "\n(no content in requested range)", nil
	}

	return header + "\n" + strings.Join(lines, "\n"), nil
}
