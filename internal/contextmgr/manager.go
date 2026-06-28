package contextmgr

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
)

const (
	DefaultMaxTokens          = 32000
	DefaultToolOutputChars    = 12000
	DefaultCompactionRatio    = 0.8
	DefaultRecentMessageCount = 6
)

type Budget struct {
	// Budget 是阶段 4 的上下文预算配置；当前先用代码默认值，后续可下沉到 settings.json。
	MaxTokens          int
	ToolOutputChars    int
	CompactionRatio    float64
	RecentMessageCount int
}

type Manager struct {
	budget Budget
}

type Usage struct {
	UsedTokens int
	MaxTokens  int
	Percent    int
}

type CompactionResult struct {
	Messages  []*schema.Message
	Compacted bool
	Before    int
	After     int
}

func NewDefaultManager() *Manager {
	return NewManager(Budget{
		MaxTokens:          DefaultMaxTokens,
		ToolOutputChars:    DefaultToolOutputChars,
		CompactionRatio:    DefaultCompactionRatio,
		RecentMessageCount: DefaultRecentMessageCount,
	})
}

func NewManager(budget Budget) *Manager {
	if budget.MaxTokens <= 0 {
		budget.MaxTokens = DefaultMaxTokens
	}
	if budget.ToolOutputChars <= 0 {
		budget.ToolOutputChars = DefaultToolOutputChars
	}
	if budget.CompactionRatio <= 0 || budget.CompactionRatio > 1 {
		budget.CompactionRatio = DefaultCompactionRatio
	}
	if budget.RecentMessageCount < 1 {
		budget.RecentMessageCount = DefaultRecentMessageCount
	}
	return &Manager{budget: budget}
}

func (m *Manager) EstimateTokens(text string) int {
	return EstimateTokens(text)
}

func EstimateTokens(text string) int {
	// MVP 先用字符数粗估 token，避免阶段 4 引入真实 tokenizer 的额外复杂度。
	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}
	return (runes + 3) / 4
}

func (m *Manager) EstimateMessages(messages []*schema.Message) int {
	total := 0
	for _, message := range messages {
		if message == nil {
			continue
		}
		total += EstimateTokens(string(message.Role))
		total += EstimateTokens(message.Content)
		total += EstimateTokens(message.Name)
		total += EstimateTokens(message.ToolCallID)
		total += EstimateTokens(message.ToolName)
		for _, toolCall := range message.ToolCalls {
			total += EstimateTokens(toolCall.ID)
			total += EstimateTokens(toolCall.Function.Name)
			total += EstimateTokens(toolCall.Function.Arguments)
		}
	}
	return total
}

func (m *Manager) Usage(messages []*schema.Message) Usage {
	used := m.EstimateMessages(messages)
	percent := 0
	if m.budget.MaxTokens > 0 {
		percent = (used*100 + m.budget.MaxTokens - 1) / m.budget.MaxTokens
	}
	return Usage{
		UsedTokens: used,
		MaxTokens:  m.budget.MaxTokens,
		Percent:    percent,
	}
}

func (m *Manager) FormatUsage(messages []*schema.Message) string {
	usage := m.Usage(messages)
	return fmt.Sprintf("[已使用%d%%上下文/%s]", usage.Percent, formatTokenBudget(usage.MaxTokens))
}

func (m *Manager) TrimToolResult(toolName string, result string) string {
	// 所有工具结果在回填模型前都走这里，形成统一的输出预算兜底。
	limit := m.budget.ToolOutputChars
	if utf8.RuneCountInString(result) <= limit {
		return result
	}

	trimmed := firstRunes(result, limit)
	return fmt.Sprintf(`%s

[nanocode context manager: tool output truncated]
Tool: %s
Original output length: %d characters
Kept: %d characters
Next step: narrow the query, use a more specific grep/glob pattern, or read the file in smaller offset/limit windows.`,
		trimmed,
		toolName,
		utf8.RuneCountInString(result),
		limit,
	)
}

func (m *Manager) CompactMessages(messages []*schema.Message) CompactionResult {
	// 压缩只在超过阈值时触发；没超预算时保持原始 messages，减少信息损失。
	before := m.EstimateMessages(messages)
	threshold := int(float64(m.budget.MaxTokens) * m.budget.CompactionRatio)
	if before <= threshold || len(messages) <= 3 {
		return CompactionResult{Messages: messages, Compacted: false, Before: before, After: before}
	}

	systemMessages, userMessage, middleMessages, recentMessages := splitMessages(messages, m.budget.RecentMessageCount)
	if len(middleMessages) == 0 {
		return CompactionResult{Messages: messages, Compacted: false, Before: before, After: before}
	}

	compacted := make([]*schema.Message, 0, len(systemMessages)+2+len(recentMessages))
	compacted = append(compacted, systemMessages...)
	compacted = append(compacted, schema.SystemMessage(buildSummary(middleMessages)))
	if userMessage != nil {
		compacted = append(compacted, userMessage)
	}
	compacted = append(compacted, recentMessages...)

	after := m.EstimateMessages(compacted)
	return CompactionResult{Messages: compacted, Compacted: true, Before: before, After: after}
}

func splitMessages(messages []*schema.Message, recentCount int) ([]*schema.Message, *schema.Message, []*schema.Message, []*schema.Message) {
	// 压缩策略：system 和最近消息保留，较早消息进入规则型 summary。
	systemMessages := make([]*schema.Message, 0, 1)
	nonSystem := make([]*schema.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		if message.Role == schema.System {
			systemMessages = append(systemMessages, message)
			continue
		}
		nonSystem = append(nonSystem, message)
	}

	userIndex := -1
	for i := len(nonSystem) - 1; i >= 0; i-- {
		if nonSystem[i].Role == schema.User {
			userIndex = i
			break
		}
	}

	var userMessage *schema.Message
	if userIndex >= 0 {
		userMessage = nonSystem[userIndex]
		nonSystem = append(nonSystem[:userIndex], nonSystem[userIndex+1:]...)
	}

	recentStart := len(nonSystem) - recentCount
	if recentStart < 0 {
		recentStart = 0
	}

	middleMessages := nonSystem[:recentStart]
	recentMessages := nonSystem[recentStart:]
	return systemMessages, userMessage, middleMessages, recentMessages
}

func buildSummary(messages []*schema.Message) string {
	// 当前 summary 不调用模型，只记录角色、工具和内容片段，保证可测试且无额外成本。
	var builder strings.Builder
	builder.WriteString("Earlier conversation summary generated by nanocode context manager.\n")
	builder.WriteString("This is a rule-based compacted summary of older messages; recent messages remain verbatim.\n")
	for i, message := range messages {
		builder.WriteString(fmt.Sprintf("- %s message", message.Role))
		if message.ToolName != "" {
			builder.WriteString(fmt.Sprintf(" from tool %s", message.ToolName))
		}
		if len(message.ToolCalls) > 0 {
			names := make([]string, 0, len(message.ToolCalls))
			for _, toolCall := range message.ToolCalls {
				names = append(names, toolCall.Function.Name)
			}
			builder.WriteString(fmt.Sprintf(" requesting tools: %s", strings.Join(names, ", ")))
		}
		if message.Content != "" {
			builder.WriteString(": ")
			builder.WriteString(firstRunes(strings.ReplaceAll(message.Content, "\n", " "), 240))
		}
		if i < len(messages)-1 {
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func formatTokenBudget(tokens int) string {
	if tokens >= 1000000 && tokens%1000000 == 0 {
		return fmt.Sprintf("%dM", tokens/1000000)
	}
	if tokens >= 1000 && tokens%1000 == 0 {
		return fmt.Sprintf("%dK", tokens/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

func firstRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}

	var builder strings.Builder
	builder.Grow(limit)
	count := 0
	for _, r := range text {
		if count >= limit {
			break
		}
		builder.WriteRune(r)
		count++
	}
	return builder.String()
}
