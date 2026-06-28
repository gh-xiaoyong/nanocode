package tools

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	toolu "github.com/cloudwego/eino/components/tool/utils"

	"nanocode/internal/permission"
)

const (
	defaultBashTimeoutSeconds = 30
	maxBashTimeoutSeconds     = 120
	maxBashOutputBytes        = 12000
)

type bashInput struct {
	Command        string `json:"command" jsonschema:"required" jsonschema_description:"要在工作区根目录执行的非交互式命令"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema_description:"命令超时时间，默认 30 秒，最大 120 秒"`
}

func newBashTool(workspace *permission.WorkspacePolicy) (einotool.BaseTool, error) {
	// bash 是 shell 类工具，即使 accept-edits 模式也需要用户确认后才执行。
	return toolu.InferTool(
		"bash",
		"在工作区根目录执行非交互式 shell 命令。该工具可能产生副作用，执行前通常需要用户确认。",
		func(ctx context.Context, input bashInput) (string, error) {
			command := strings.TrimSpace(input.Command)
			if command == "" {
				return "", fmt.Errorf("command must not be empty")
			}

			timeoutSeconds := input.TimeoutSeconds
			if timeoutSeconds <= 0 {
				timeoutSeconds = defaultBashTimeoutSeconds
			}
			if timeoutSeconds > maxBashTimeoutSeconds {
				timeoutSeconds = maxBashTimeoutSeconds
			}

			cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
			defer cancel()

			cmd := shellCommand(cmdCtx, command)
			cmd.Dir = workspace.RootDir()

			output, err := cmd.CombinedOutput()
			rendered := truncateBashOutput(string(output))
			if cmdCtx.Err() == context.DeadlineExceeded {
				return rendered, fmt.Errorf("bash command timed out after %d seconds", timeoutSeconds)
			}
			if err != nil {
				return rendered, fmt.Errorf("bash command failed: %w", err)
			}
			if strings.TrimSpace(rendered) == "" {
				return "bash command succeeded with no output", nil
			}
			return rendered, nil
		},
	)
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	// Windows 下使用 PowerShell，其他平台使用 sh，调用方统一只传 command 字符串。
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

func truncateBashOutput(output string) string {
	// bash 自身先做一次截断；Agent 层 context manager 还会再做统一兜底。
	if len(output) <= maxBashOutputBytes {
		return output
	}
	return output[:maxBashOutputBytes] + "\n[truncated: bash output exceeded 12000 bytes]"
}
