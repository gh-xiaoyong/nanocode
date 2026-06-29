package friendlyerror

import (
	"errors"
	"fmt"
	"strings"

	"nanocode/internal/config"
	"nanocode/internal/permission"
)

type Message struct {
	Title  string
	Detail string
	Hint   string
	Cause  error
}

type PermissionDenied struct {
	ToolName       string
	Mode           permission.Mode
	Kind           permission.ToolKind
	NonInteractive bool
	UserDenied     bool
}

func (e PermissionDenied) Error() string {
	return FormatPermissionDenied(e.ToolName, e.Mode, e.Kind, e.NonInteractive, e.UserDenied).Summary()
}

func Format(err error) string {
	return FromError(err).String()
}

func FormatToolError(toolName string, err error) string {
	return FromToolError(toolName, err).String()
}

func FormatPermissionDenied(toolName string, mode permission.Mode, kind permission.ToolKind, nonInteractive bool, userDenied bool) Message {
	if mode == permission.ModePlan {
		return Message{
			Title:  "权限拒绝：当前是 plan 模式",
			Detail: fmt.Sprintf("工具 %s 属于 %s 能力，plan 模式只允许读取和分析。", toolName, kind),
			Hint:   "如需修改文件或执行命令，请切换到 default 或 accept-edits 模式。",
		}
	}
	if nonInteractive {
		return Message{
			Title:  "权限拒绝：非交互模式不会等待确认",
			Detail: fmt.Sprintf("工具 %s 需要用户确认，但 -prompt 模式默认拒绝需要确认的操作。", toolName),
			Hint:   "请进入终端 UI 确认执行，或切换到 accept-edits 模式允许普通文件写入。",
		}
	}
	if userDenied {
		return Message{
			Title:  "权限拒绝：用户未授权执行",
			Detail: fmt.Sprintf("工具 %s 已被当前确认流程拒绝。", toolName),
			Hint:   "如需继续，请重新发起请求并在确认提示中输入 y。",
		}
	}
	return Message{
		Title:  "权限拒绝：工具未被允许执行",
		Detail: fmt.Sprintf("工具 %s 在当前模式 %s 下不能执行。", toolName, mode),
		Hint:   "请检查当前 mode，或调整请求为只读分析。",
	}
}

func FromError(err error) Message {
	if err == nil {
		return Message{}
	}

	var configErr *config.ConfigError
	if errors.As(err, &configErr) {
		return fromConfigError(configErr)
	}

	var workspaceErr *permission.WorkspacePathError
	if errors.As(err, &workspaceErr) {
		return Message{
			Title:  "路径错误：路径超出工作区",
			Detail: fmt.Sprintf("请求路径 %q 不在工作区 %q 内。", workspaceErr.UserPath, workspaceErr.RootDir),
			Hint:   "请使用项目根目录内的相对路径。",
			Cause:  err,
		}
	}

	if isModelError(err.Error()) {
		return Message{
			Title:  "模型请求错误",
			Detail: err.Error(),
			Hint:   "请检查 settings.json 中的 base_url、model 名称、API key，以及网络或模型服务状态。",
			Cause:  err,
		}
	}

	return Message{
		Title:  "错误",
		Detail: err.Error(),
		Cause:  err,
	}
}

func FromToolError(toolName string, err error) Message {
	if err == nil {
		return Message{}
	}
	msg := FromError(err)
	if msg.Title != "错误" {
		return msg
	}

	text := err.Error()
	switch {
	case strings.Contains(text, "old_string was not found"):
		return Message{Title: "工具失败：精确替换未找到内容", Detail: text, Hint: "请先用 read_file 读取最新文件片段，再提供更准确的 old_string。", Cause: err}
	case strings.Contains(text, "old_string matched"):
		return Message{Title: "工具失败：替换内容匹配多处", Detail: text, Hint: "请扩大 old_string 上下文，或明确设置 replace_all=true。", Cause: err}
	case strings.Contains(text, "stat parent directory"):
		return Message{Title: "工具失败：父目录不存在", Detail: text, Hint: "请检查目标路径；当前 write_file 不会自动创建父目录。", Cause: err}
	case strings.Contains(text, "timed out") || strings.Contains(text, "timeout"):
		return Message{Title: "工具失败：命令超时", Detail: text, Hint: "请拆分命令、缩短执行时间，或提高 timeout_seconds。", Cause: err}
	default:
		return Message{Title: fmt.Sprintf("工具失败：%s", toolName), Detail: text, Hint: "请根据错误原因调整参数后重试。", Cause: err}
	}
}

func Short(err error) string {
	if err == nil {
		return ""
	}
	msg := FromError(err)
	return msg.Summary()
}

func ShortToolError(toolName string, err error) string {
	if err == nil {
		return ""
	}
	return FromToolError(toolName, err).Summary()
}

func (m Message) Summary() string {
	if strings.TrimSpace(m.Detail) == "" {
		return m.Title
	}
	return fmt.Sprintf("%s：%s", m.Title, m.Detail)
}

func (m Message) String() string {
	if strings.TrimSpace(m.Title) == "" {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("✗ ")
	builder.WriteString(m.Title)
	if strings.TrimSpace(m.Detail) != "" {
		builder.WriteString("\n  详情：")
		builder.WriteString(m.Detail)
	}
	if strings.TrimSpace(m.Hint) != "" {
		builder.WriteString("\n  建议：")
		builder.WriteString(m.Hint)
	}
	return builder.String()
}

func fromConfigError(err *config.ConfigError) Message {
	switch err.Kind {
	case config.ConfigErrorMissingAPIKey:
		return Message{Title: "配置错误：缺少 API Key", Detail: "没有找到 settings.json 中的 model.api_key，也没有读取到 NANOCODE_API_KEY / OPENAI_API_KEY。", Hint: "复制 settings-example.json 为 settings.json，或设置 NANOCODE_API_KEY / OPENAI_API_KEY。", Cause: err}
	case config.ConfigErrorMissingModel:
		return Message{Title: "配置错误：缺少模型名称", Detail: "没有找到 settings.json 中的 model.model，也没有读取到 NANOCODE_MODEL / OPENAI_MODEL。", Hint: "请在 settings.json 的 model.model 中填写模型名，或设置 NANOCODE_MODEL。", Cause: err}
	case config.ConfigErrorSettingsParse:
		return Message{Title: "配置错误：settings.json 解析失败", Detail: err.Error(), Hint: "请检查 settings.json 是否是合法 JSON，字段是否放在 model 对象内。", Cause: err}
	case config.ConfigErrorEnvParse:
		return Message{Title: "配置错误：环境变量格式不正确", Detail: err.Error(), Hint: "请确认 temperature 是数字，max_tokens/context_window 是整数。", Cause: err}
	default:
		return Message{Title: "配置错误", Detail: err.Error(), Hint: "请检查 settings.json 和 NANOCODE_* / OPENAI_* 环境变量。", Cause: err}
	}
}

func isModelError(text string) bool {
	return strings.Contains(text, "create tool calling model") ||
		strings.Contains(text, "start model stream") ||
		strings.Contains(text, "read model stream chunk")
}
