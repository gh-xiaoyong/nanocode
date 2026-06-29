package friendlyerror

import (
	"fmt"
	"strings"
	"testing"

	"nanocode/internal/config"
	"nanocode/internal/permission"
)

func TestFormatMissingAPIKey(t *testing.T) {
	output := Format(&config.ConfigError{Kind: config.ConfigErrorMissingAPIKey})

	if !strings.Contains(output, "配置错误：缺少 API Key") || !strings.Contains(output, "NANOCODE_API_KEY") {
		t.Fatalf("unexpected formatted error: %q", output)
	}
}

func TestFormatMissingModel(t *testing.T) {
	output := Format(&config.ConfigError{Kind: config.ConfigErrorMissingModel})

	if !strings.Contains(output, "配置错误：缺少模型名称") || !strings.Contains(output, "model.model") {
		t.Fatalf("unexpected formatted error: %q", output)
	}
}

func TestFormatSettingsParse(t *testing.T) {
	output := Format(&config.ConfigError{Kind: config.ConfigErrorSettingsParse, Path: "D:/repo/settings.json", Cause: fmt.Errorf("bad json")})

	if !strings.Contains(output, "settings.json 解析失败") || !strings.Contains(output, "D:/repo/settings.json") {
		t.Fatalf("unexpected formatted error: %q", output)
	}
}

func TestFormatWorkspacePathError(t *testing.T) {
	output := Format(&permission.WorkspacePathError{UserPath: "../secret.txt", RootDir: "D:/projects/nanocode"})

	if !strings.Contains(output, "路径超出工作区") || !strings.Contains(output, "../secret.txt") || !strings.Contains(output, "D:/projects/nanocode") {
		t.Fatalf("unexpected formatted error: %q", output)
	}
}

func TestFormatPermissionDeniedPlanMode(t *testing.T) {
	msg := FormatPermissionDenied("edit_file", permission.ModePlan, permission.ToolKindWrite, false, false)

	if !strings.Contains(msg.String(), "plan 模式") || !strings.Contains(msg.String(), "只允许读取和分析") {
		t.Fatalf("unexpected permission message: %q", msg.String())
	}
}

func TestFormatPermissionDeniedNonInteractive(t *testing.T) {
	msg := FormatPermissionDenied("bash", permission.ModeDefault, permission.ToolKindShell, true, false)

	if !strings.Contains(msg.String(), "非交互模式") || !strings.Contains(msg.String(), "-prompt") {
		t.Fatalf("unexpected permission message: %q", msg.String())
	}
}

func TestFormatToolErrorHints(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "old string missing", err: fmt.Errorf("old_string was not found in file"), want: "read_file"},
		{name: "multiple matches", err: fmt.Errorf("old_string matched 2 times"), want: "replace_all=true"},
		{name: "parent missing", err: fmt.Errorf("stat parent directory: no such file or directory"), want: "不会自动创建父目录"},
		{name: "timeout", err: fmt.Errorf("bash command timed out after 1 seconds"), want: "timeout_seconds"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := FormatToolError("edit_file", tt.err)
			if !strings.Contains(output, tt.want) {
				t.Fatalf("expected %q in %q", tt.want, output)
			}
		})
	}
}

func TestFormatFallback(t *testing.T) {
	output := Format(fmt.Errorf("plain failure"))

	if !strings.Contains(output, "✗ 错误") || !strings.Contains(output, "plain failure") {
		t.Fatalf("unexpected fallback: %q", output)
	}
}
