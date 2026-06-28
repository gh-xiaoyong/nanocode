package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	einotool "github.com/cloudwego/eino/components/tool"
	toolu "github.com/cloudwego/eino/components/tool/utils"

	"nanocode/internal/permission"
)

const globMaxResults = 100

type globInput struct {
	Pattern string `json:"pattern" jsonschema:"required" jsonschema_description:"文件路径匹配模式，例如 **/*.tsx 或 internal/**/*.go"`
	Path    string `json:"path,omitempty" jsonschema_description:"搜索起点目录，省略时默认当前项目根目录"`
}

type globMatch struct {
	relativePath string
	modTimeUnix  int64
}

func newGlobTool(workspace *permission.WorkspacePolicy) (einotool.BaseTool, error) {
	// glob 解决的是“只知道文件名/扩展名模式，但没有内容关键字”的场景。
	// 结果按修改时间倒序排列，让最近改过的文件优先进入模型视野。
	return toolu.InferTool(
		"glob",
		"按文件名或路径模式查找工作区内文件，例如 **/*.tsx；结果按修改时间倒序排列，最多返回 100 个文件。",
		func(ctx context.Context, input globInput) (string, error) {
			_ = ctx

			pattern := strings.TrimSpace(input.Pattern)
			if pattern == "" {
				return "", fmt.Errorf("pattern must not be empty")
			}

			searchRoot, err := resolvePath(workspace, input.Path)
			if err != nil {
				return "", err
			}

			rootInfo, err := os.Stat(searchRoot)
			if err != nil {
				return "", fmt.Errorf("stat glob path: %w", err)
			}
			if !rootInfo.IsDir() {
				return "", fmt.Errorf("path %q is not a directory", input.Path)
			}

			matches, err := collectGlobMatches(workspace.RootDir(), searchRoot, pattern)
			if err != nil {
				return "", err
			}
			if len(matches) == 0 {
				return "(no matches)", nil
			}

			sort.Slice(matches, func(i, j int) bool {
				if matches[i].modTimeUnix == matches[j].modTimeUnix {
					return matches[i].relativePath < matches[j].relativePath
				}
				return matches[i].modTimeUnix > matches[j].modTimeUnix
			})

			truncated := len(matches) > globMaxResults
			if truncated {
				matches = matches[:globMaxResults]
			}

			lines := make([]string, 0, len(matches)+1)
			for _, match := range matches {
				lines = append(lines, match.relativePath)
			}
			if truncated {
				lines = append(lines, fmt.Sprintf("(truncated: showing first %d matches, refine pattern to see more)", globMaxResults))
			}

			return strings.Join(lines, "\n"), nil
		},
	)
}

func collectGlobMatches(workspaceRoot string, searchRoot string, pattern string) ([]globMatch, error) {
	// doublestar 提供 ** 支持；这里再过滤目录，只把文件路径返回给模型。
	matches, err := doublestar.FilepathGlob(filepath.Join(searchRoot, filepath.FromSlash(pattern)))
	if err != nil {
		return nil, fmt.Errorf("glob files: %w", err)
	}

	results := make([]globMatch, 0, len(matches))
	for _, matchPath := range matches {
		info, err := os.Stat(matchPath)
		if err != nil {
			return nil, fmt.Errorf("stat glob match: %w", err)
		}
		if info.IsDir() {
			continue
		}

		relativePath, err := filepath.Rel(workspaceRoot, matchPath)
		if err != nil {
			return nil, fmt.Errorf("build relative path: %w", err)
		}

		results = append(results, globMatch{
			relativePath: filepath.ToSlash(relativePath),
			modTimeUnix:  info.ModTime().UnixNano(),
		})
	}

	return results, nil
}
