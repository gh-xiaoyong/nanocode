package tools

import (
	"context"

	einotool "github.com/cloudwego/eino/components/tool"

	"nanocode/internal/permission"
)

// Registry 负责集中装配当前阶段可用的工具。
// 后面接权限、风险等级或更多工具时，可以继续沿用这个入口扩展。
type Registry struct {
	workspace *permission.WorkspacePolicy
}

func NewRegistry(workspace *permission.WorkspacePolicy) *Registry {
	return &Registry{workspace: workspace}
}

func (r *Registry) Build(ctx context.Context) ([]einotool.BaseTool, error) {
	readFileTool, err := newReadFileTool(r.workspace)
	if err != nil {
		return nil, err
	}

	listDirectoryTool, err := newListDirectoryTool(r.workspace)
	if err != nil {
		return nil, err
	}

	grepTool, err := newGrepTool(r.workspace)
	if err != nil {
		return nil, err
	}

	globTool, err := newGlobTool(r.workspace)
	if err != nil {
		return nil, err
	}

	gitStatusTool, err := newGitStatusTool(r.workspace)
	if err != nil {
		return nil, err
	}

	return []einotool.BaseTool{readFileTool, listDirectoryTool, grepTool, globTool, gitStatusTool}, nil
}
