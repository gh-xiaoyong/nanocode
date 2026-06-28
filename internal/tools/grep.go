package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolu "github.com/cloudwego/eino/components/tool/utils"

	"nanocode/internal/permission"
)

const defaultGrepMaxResults = 50

type grepInput struct {
	Pattern       string `json:"pattern" jsonschema:"required" jsonschema_description:"要搜索的文本或正则模式"`
	Path          string `json:"path,omitempty" jsonschema_description:"要搜索的文件或目录路径，省略时默认当前项目根目录"`
	CaseSensitive bool   `json:"case_sensitive,omitempty" jsonschema_description:"是否区分大小写，默认 false"`
	IsRegexp      bool   `json:"is_regexp,omitempty" jsonschema_description:"是否按正则表达式搜索，默认 false"`
	MaxResults    int    `json:"max_results,omitempty" jsonschema_description:"最多返回多少条命中，默认 50"`
}

type grepBackend func(workspace *permission.WorkspacePolicy, resolvedPath string, input grepInput) (string, error)

func newGrepTool(workspace *permission.WorkspacePolicy) (einotool.BaseTool, error) {
	return newGrepToolWithBackend(workspace, runBestEffortGrep)
}

func newGrepToolWithBackend(workspace *permission.WorkspacePolicy, backend grepBackend) (einotool.BaseTool, error) {
	// grep 是阶段 2 的仓库搜索能力；底层优先走 ripgrep，提高大仓库下的搜索性能。
	// 如果本机没有 rg 或执行失败，再回退到纯 Go 实现，保证工具接口稳定可用。
	return toolu.InferTool(
		"grep",
		"在工作区内递归搜索文本内容，返回匹配到的相对路径、行号和行内容。",
		func(ctx context.Context, input grepInput) (string, error) {
			_ = ctx

			if strings.TrimSpace(input.Pattern) == "" {
				return "", fmt.Errorf("pattern must not be empty")
			}

			resolvedPath, err := resolvePath(workspace, input.Path)
			if err != nil {
				return "", err
			}

			if input.MaxResults <= 0 {
				input.MaxResults = defaultGrepMaxResults
			}

			return backend(workspace, resolvedPath, input)
		},
	)
}

func runBestEffortGrep(workspace *permission.WorkspacePolicy, resolvedPath string, input grepInput) (string, error) {
	output, err := runRipgrep(workspace, resolvedPath, input)
	if err == nil {
		return output, nil
	}

	// ripgrep 不可用或当前调用失败时，保留纯 Go 回退路径，
	// 避免工具能力完全依赖外部命令。
	return runGoGrep(workspace, resolvedPath, input)
}

func runRipgrep(workspace *permission.WorkspacePolicy, resolvedPath string, input grepInput) (string, error) {
	// rg 输出 JSON 后再转回统一文本格式，避免模型感知到底层实现差异。
	if _, err := exec.LookPath("rg"); err != nil {
		return "", fmt.Errorf("ripgrep not available: %w", err)
	}

	args := []string{
		"--json",
		"--color", "never",
		"--max-count", fmt.Sprintf("%d", input.MaxResults),
	}

	if !input.CaseSensitive {
		args = append(args, "--ignore-case")
	}
	if !input.IsRegexp {
		args = append(args, "--fixed-strings")
	}

	args = append(args, input.Pattern)

	fileInfo, err := os.Stat(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("stat grep path: %w", err)
	}

	if fileInfo.IsDir() {
		args = append(args, resolvedPath)
	} else {
		args = append(args, resolvedPath)
	}

	cmd := exec.Command("rg", args...)
	cmd.Dir = workspace.RootDir()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "(no matches)", nil
		}
		return "", fmt.Errorf("run ripgrep: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	results, err := parseRipgrepJSONOutput(workspace.RootDir(), stdout.Bytes())
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "(no matches)", nil
	}

	return strings.Join(results, "\n"), nil
}

type ripgrepEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
	} `json:"data"`
}

func parseRipgrepJSONOutput(workspaceRoot string, raw []byte) ([]string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}

	lines := strings.Split(trimmed, "\n")
	results := make([]string, 0, len(lines))
	for _, line := range lines {
		var event ripgrepEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("parse ripgrep json output: %w", err)
		}
		if event.Type != "match" {
			continue
		}

		relativePath, err := filepath.Rel(workspaceRoot, event.Data.Path.Text)
		if err != nil {
			return nil, fmt.Errorf("build relative path from ripgrep output: %w", err)
		}

		content := strings.TrimRight(event.Data.Lines.Text, "\r\n")
		results = append(results, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(relativePath), event.Data.LineNumber, content))
	}

	return results, nil
}

func runGoGrep(workspace *permission.WorkspacePolicy, resolvedPath string, input grepInput) (string, error) {
	matcher, err := buildLineMatcher(input)
	if err != nil {
		return "", err
	}

	fileInfo, err := os.Stat(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("stat grep path: %w", err)
	}

	results := make([]string, 0, min(input.MaxResults, 8))
	appendMatches := func(filePath string) error {
		remaining := input.MaxResults - len(results)
		if remaining <= 0 {
			return nil
		}

		matches, err := grepFile(workspace.RootDir(), filePath, matcher, remaining)
		if err != nil {
			return err
		}

		results = append(results, matches...)
		return nil
	}

	if !fileInfo.IsDir() {
		if err := appendMatches(resolvedPath); err != nil {
			return "", err
		}
	} else {
		err = filepath.WalkDir(resolvedPath, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if len(results) >= input.MaxResults {
				return fs.SkipAll
			}
			return appendMatches(path)
		})
		if err != nil && err != fs.SkipAll {
			return "", fmt.Errorf("walk grep path: %w", err)
		}
	}

	if len(results) == 0 {
		return "(no matches)", nil
	}

	return strings.Join(results, "\n"), nil
}

type lineMatcher func(string) bool

func buildLineMatcher(input grepInput) (lineMatcher, error) {
	if input.IsRegexp {
		pattern := input.Pattern
		if !input.CaseSensitive {
			pattern = "(?i)" + pattern
		}

		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile regexp: %w", err)
		}

		return func(line string) bool {
			return compiled.MatchString(line)
		}, nil
	}

	pattern := input.Pattern
	if !input.CaseSensitive {
		pattern = strings.ToLower(pattern)
	}

	return func(line string) bool {
		candidate := line
		if !input.CaseSensitive {
			candidate = strings.ToLower(candidate)
		}
		return strings.Contains(candidate, pattern)
	}, nil
}

func grepFile(workspaceRoot string, filePath string, matcher lineMatcher, maxResults int) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file for grep: %w", err)
	}
	defer file.Close()

	relativePath, err := filepath.Rel(workspaceRoot, filePath)
	if err != nil {
		return nil, fmt.Errorf("build relative path: %w", err)
	}

	scanner := bufio.NewScanner(file)
	// Scanner 默认 64K，源码或日志行可能更长，这里先把上限抬高到 1MB。
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)

	results := make([]string, 0, min(maxResults, 4))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if !matcher(line) {
			continue
		}

		results = append(results, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(relativePath), lineNumber, line))
		if len(results) >= maxResults {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan file for grep: %w", err)
	}

	return results, nil
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
