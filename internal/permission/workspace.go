package permission

import (
	"fmt"
	"path/filepath"
	"strings"
)

// WorkspacePolicy 定义当前项目的工作区边界。
// 所有文件工具都先通过它解析路径，默认禁止访问项目根目录之外的文件。
type WorkspacePolicy struct {
	rootDir string
}

type WorkspacePathError struct {
	UserPath string
	RootDir  string
	Cause    error
}

func (e *WorkspacePathError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("path %q escapes workspace root %q: %v", e.UserPath, e.RootDir, e.Cause)
	}
	return fmt.Sprintf("path %q escapes workspace root %q", e.UserPath, e.RootDir)
}

func (e *WorkspacePathError) Unwrap() error {
	return e.Cause
}

func NewWorkspacePolicy(rootDir string) (*WorkspacePolicy, error) {
	cleanRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}

	return &WorkspacePolicy{rootDir: cleanRoot}, nil
}

func (p *WorkspacePolicy) RootDir() string {
	return p.rootDir
}

func (p *WorkspacePolicy) ResolvePath(userPath string) (string, error) {
	// 空路径默认指向 workspace root，便于 ls/grep/glob 这类工具省略 path。
	targetPath := strings.TrimSpace(userPath)
	if targetPath == "" {
		targetPath = "."
	}

	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(p.rootDir, targetPath)
	}

	cleanTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("resolve target path: %w", err)
	}

	relative, err := filepath.Rel(p.rootDir, cleanTarget)
	if err != nil {
		return "", fmt.Errorf("check workspace boundary: %w", err)
	}

	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		// 默认禁止仓库外读写；后续如果支持仓库外授权，应从这里扩展。
		return "", &WorkspacePathError{UserPath: userPath, RootDir: p.rootDir}
	}

	return cleanTarget, nil
}
