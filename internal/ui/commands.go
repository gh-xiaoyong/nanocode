package ui

import "strings"

type replCommand struct {
	Name        string
	Usage       string
	Description string
}

func replCommands() []replCommand {
	// 命令注册表同时服务 /help、输入 / 时的提示和 Tab 补全，避免三处列表漂移。
	return []replCommand{
		{Name: "/help", Usage: "/help", Description: "显示帮助"},
		{Name: "/status", Usage: "/status", Description: "查看当前运行状态"},
		{Name: "/mode", Usage: "/mode [name]", Description: "查看或切换模式：default | plan | accept-edits"},
		{Name: "/memory", Usage: "/memory", Description: "查看当前加载记忆的摘要"},
		{Name: "/memory", Usage: "/memory full", Description: "查看当前加载记忆的完整内容"},
		{Name: "/memory", Usage: "/memory status", Description: "只查看记忆来源状态"},
		{Name: "/session", Usage: "/session", Description: "查看当前会话信息"},
		{Name: "/clear", Usage: "/clear", Description: "清空当前会话历史"},
		{Name: "/reload", Usage: "/reload", Description: "重新加载配置和 memory"},
		{Name: "/resume", Usage: "/resume <id>", Description: "恢复指定 session"},
		{Name: "/continue", Usage: "/continue", Description: "继续最近更新的 session"},
		{Name: "/undo", Usage: "/undo", Description: "撤销最近一次成功的文件写入"},
		{Name: "/exit", Usage: "/exit", Description: "退出交互界面"},
		{Name: "/quit", Usage: "/quit", Description: "退出交互界面"},
	}
}

func slashCommandCandidates() []string {
	commands := replCommands()
	seen := map[string]bool{}
	candidates := make([]string, 0, len(commands))
	for _, command := range commands {
		if seen[command.Usage] {
			continue
		}
		seen[command.Usage] = true
		candidates = append(candidates, command.Usage)
	}
	return candidates
}

func completeSlashCommand(line string) []string {
	if !strings.HasPrefix(line, "/") {
		return nil
	}

	matches := make([]string, 0)
	for _, candidate := range slashCommandCandidates() {
		if strings.HasPrefix(candidate, line) {
			matches = append(matches, candidate)
		}
	}
	return matches
}
