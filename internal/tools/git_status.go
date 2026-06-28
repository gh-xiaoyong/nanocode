package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolu "github.com/cloudwego/eino/components/tool/utils"

	"nanocode/internal/permission"
)

type gitStatusInput struct{}

func newGitStatusTool(workspace *permission.WorkspacePolicy) (einotool.BaseTool, error) {
	// git_status 是阶段 2 的仓库状态感知工具。
	// 它只执行只读 git 命令，让模型在动手前知道当前分支和用户未提交改动。
	return toolu.InferTool(
		"git_status",
		"读取当前工作区的 git 状态，包括当前分支、未提交改动和 diff 摘要；只执行只读 git 命令。",
		func(ctx context.Context, input gitStatusInput) (string, error) {
			_ = input
			return buildGitStatusSummary(ctx, workspace.RootDir())
		},
	)
}

func buildGitStatusSummary(ctx context.Context, workspaceRoot string) (string, error) {
	// 这里只读取 status 和 diff stat，不做 add/commit/checkout 等会改变仓库状态的操作。
	status, err := runGitCommand(ctx, workspaceRoot, "status", "--short", "--branch")
	if err != nil {
		return "", err
	}

	unstagedStat, err := runGitCommand(ctx, workspaceRoot, "diff", "--stat")
	if err != nil {
		return "", err
	}

	stagedStat, err := runGitCommand(ctx, workspaceRoot, "diff", "--cached", "--stat")
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString("Git status\n")
	builder.WriteString("----------\n")

	status = strings.TrimSpace(status)
	if status == "" {
		builder.WriteString("status: clean\n")
	} else {
		builder.WriteString(status)
		builder.WriteString("\n")
	}

	builder.WriteString("\nUnstaged diff stat\n")
	builder.WriteString("------------------\n")
	writeGitSection(&builder, unstagedStat)

	builder.WriteString("\nStaged diff stat\n")
	builder.WriteString("----------------\n")
	writeGitSection(&builder, stagedStat)

	return strings.TrimRight(builder.String(), "\n"), nil
}

func runGitCommand(ctx context.Context, workspaceRoot string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workspaceRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}

	return string(output), nil
}

func writeGitSection(builder *strings.Builder, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		builder.WriteString("(none)\n")
		return
	}

	builder.WriteString(content)
	builder.WriteString("\n")
}
