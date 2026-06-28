package permission

import (
	"fmt"
	"path/filepath"
	"strings"
)

// WorkspacePolicy 定义当前阶段的工作区边界。
// 阶段 1.5 先把“默认只允许访问当前项目根目录内路径”收敛成显式对象，
// 为后续读写分级、权限确认和仓库外授权打基础。
type WorkspacePolicy struct {
	rootDir string
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
		return "", fmt.Errorf("path %q escapes workspace root %q", userPath, p.rootDir)
	}

	return cleanTarget, nil
}
