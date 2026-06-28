package tools

import (
	"context"

	einotool "github.com/cloudwego/eino/components/tool"

	"nanocode/internal/checkpoint"
	"nanocode/internal/permission"
)

// Registry 统一装配工具，并给每个工具标注权限类型和风险说明。
type Registry struct {
	workspace   *permission.WorkspacePolicy
	checkpoints *checkpoint.Manager
}

type RegisteredTool struct {
	Tool einotool.BaseTool
	Kind permission.ToolKind
	Risk string
}

func NewRegistry(workspace *permission.WorkspacePolicy, checkpoints *checkpoint.Manager) *Registry {
	return &Registry{workspace: workspace, checkpoints: checkpoints}
}

func (r *Registry) Build(ctx context.Context) ([]RegisteredTool, error) {
	_ = ctx

	// 工具集中在这里注册，Agent 层只消费统一的 RegisteredTool 列表。
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

	writeFileTool, err := newWriteFileTool(r.workspace, r.checkpoints)
	if err != nil {
		return nil, err
	}

	editFileTool, err := newEditFileTool(r.workspace, r.checkpoints)
	if err != nil {
		return nil, err
	}

	bashTool, err := newBashTool(r.workspace)
	if err != nil {
		return nil, err
	}

	return []RegisteredTool{
		{Tool: readFileTool, Kind: permission.ToolKindRead, Risk: "Read workspace file content"},
		{Tool: listDirectoryTool, Kind: permission.ToolKindRead, Risk: "List workspace directory content"},
		{Tool: grepTool, Kind: permission.ToolKindRead, Risk: "Search workspace file content"},
		{Tool: globTool, Kind: permission.ToolKindRead, Risk: "Search workspace files by path pattern"},
		{Tool: gitStatusTool, Kind: permission.ToolKindRead, Risk: "Read current git status"},
		{Tool: writeFileTool, Kind: permission.ToolKindWrite, Risk: "Create or overwrite a workspace file"},
		{Tool: editFileTool, Kind: permission.ToolKindWrite, Risk: "Modify workspace file content"},
		{Tool: bashTool, Kind: permission.ToolKindShell, Risk: "Run a shell command in the workspace; it may have side effects"},
	}, nil
}
