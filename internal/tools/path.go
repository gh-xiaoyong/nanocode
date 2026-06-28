package tools

import "nanocode/internal/permission"

func resolvePath(workspace *permission.WorkspacePolicy, userPath string) (string, error) {
	return workspace.ResolvePath(userPath)
}
