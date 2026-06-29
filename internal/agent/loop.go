package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"nanocode/internal/checkpoint"
	"nanocode/internal/config"
	"nanocode/internal/contextmgr"
	"nanocode/internal/friendlyerror"
	"nanocode/internal/memory"
	"nanocode/internal/model"
	"nanocode/internal/permission"
	"nanocode/internal/prompt"
	"nanocode/internal/tools"
)

const maxCoreLoopSteps = 50

type SingleTurnRunner struct {
	// SingleTurnRunner 汇集一次 Agent 执行需要的运行时对象：
	// 模型、工具、权限确认、checkpoint 和上下文预算都从这里进入显式 loop。
	model        einomodel.ToolCallingChatModel
	toolInfos    []*schema.ToolInfo
	invokers     map[string]einotool.InvokableTool
	toolMetadata map[string]tools.RegisteredTool
	systemPrompt string
	modelLabel   string
	mode         permission.Mode
	confirmer    permission.Confirmer
	checkpoints  *checkpoint.Manager
	contextMgr   *contextmgr.Manager
	memory       memory.Memory
}

func NewSingleTurnRunner(ctx context.Context, cfg config.ModelConfig, mode permission.Mode) (*SingleTurnRunner, error) {
	return NewSingleTurnRunnerWithRuntime(ctx, cfg, mode, permission.DenyConfirmer{}, checkpoint.NewManager())
}

func NewSingleTurnRunnerWithRuntime(ctx context.Context, cfg config.ModelConfig, mode permission.Mode, confirmer permission.Confirmer, checkpoints *checkpoint.Manager) (*SingleTurnRunner, error) {
	baseModel, err := model.NewToolCallingChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create tool calling model: %w", err)
	}

	workspace, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}

	workspacePolicy, err := permission.NewWorkspacePolicy(workspace)
	if err != nil {
		return nil, fmt.Errorf("create workspace policy: %w", err)
	}
	memoryLoader, err := memory.NewLoader(workspacePolicy.RootDir())
	if err != nil {
		return nil, fmt.Errorf("create memory loader: %w", err)
	}
	loadedMemory, err := memoryLoader.Load()
	if err != nil {
		return nil, fmt.Errorf("load memory: %w", err)
	}

	if confirmer == nil {
		confirmer = permission.DenyConfirmer{}
	}
	if checkpoints == nil {
		checkpoints = checkpoint.NewManager()
	}
	contextManager := contextmgr.NewManager(contextmgr.Budget{
		MaxTokens: cfg.ContextWindow,
	})

	toolRegistry := tools.NewRegistry(workspacePolicy, checkpoints)
	registeredTools, err := toolRegistry.Build(ctx)
	if err != nil {
		return nil, fmt.Errorf("build tools: %w", err)
	}
	registeredTools = filterToolsForMode(mode, registeredTools)

	toolInfos, invokers, toolMetadata, err := buildToolRuntime(ctx, registeredTools)
	if err != nil {
		return nil, err
	}

	toolBoundModel, err := baseModel.WithTools(toolInfos)
	if err != nil {
		return nil, fmt.Errorf("bind tools to model: %w", err)
	}

	return &SingleTurnRunner{
		model:        toolBoundModel,
		toolInfos:    toolInfos,
		invokers:     invokers,
		toolMetadata: toolMetadata,
		systemPrompt: prompt.BuildSystemPrompt(mode, workspacePolicy.RootDir(), loadedMemory),
		modelLabel:   formatModelLabel(cfg),
		mode:         mode,
		confirmer:    confirmer,
		checkpoints:  checkpoints,
		contextMgr:   contextManager,
		memory:       loadedMemory,
	}, nil
}

func (r *SingleTurnRunner) UndoLast(ctx context.Context) (string, error) {
	return r.checkpoints.UndoLast(ctx)
}

func (r *SingleTurnRunner) Memory() memory.Memory {
	return r.memory
}

func (r *SingleTurnRunner) ContextUsage(messages []*schema.Message) string {
	if r.contextMgr == nil {
		return ""
	}
	return r.contextMgr.FormatUsage(messages)
}

func (r *SingleTurnRunner) SystemPrompt() string {
	return r.systemPrompt
}

func (r *SingleTurnRunner) ModelLabel() string {
	return r.modelLabel
}

func (r *SingleTurnRunner) ToolSummary() map[permission.ToolKind]int {
	summary := map[permission.ToolKind]int{
		permission.ToolKindRead:  0,
		permission.ToolKindWrite: 0,
		permission.ToolKindShell: 0,
	}
	for _, metadata := range r.toolMetadata {
		summary[metadata.Kind]++
	}
	return summary
}

func formatModelLabel(cfg config.ModelConfig) string {
	if cfg.BaseURL != "" {
		return fmt.Sprintf("%s (%s)", cfg.Model, cfg.BaseURL)
	}
	return cfg.Model
}

func (r *SingleTurnRunner) Run(ctx context.Context, userPrompt string, output io.Writer) error {
	_, err := r.RunWithMessages(ctx, nil, userPrompt, output)
	return err
}

func (r *SingleTurnRunner) RunWithMessages(ctx context.Context, history []*schema.Message, userPrompt string, output io.Writer) ([]*schema.Message, error) {
	// REPL 会把历史 messages 传进来；本轮结束后再保存回 session。
	messages := r.prepareMessages(history, userPrompt)
	reporter := NewTerminalReporter(output)
	stats := StartTurnStats()

	for step := 0; step < maxCoreLoopSteps; step++ {
		// 每次调模型前先做预算检查，避免工具结果连续回填后把上下文撑爆。
		messages = r.compactMessages(messages)
		stats.RecordModelCall()
		assistantMsg, err := r.streamAssistantMessage(ctx, messages, output)
		if err != nil {
			reporter.SystemError(err.Error())
			return messages, err
		}

		messages = append(messages, assistantMsg)
		if len(assistantMsg.ToolCalls) == 0 {
			if err := r.writeTurnFooter(output, messages, stats); err != nil {
				return messages, err
			}
			return messages, nil
		}

		for _, toolCall := range assistantMsg.ToolCalls {
			messages = append(messages, r.invokeTool(ctx, toolCall, reporter, stats))
		}
	}

	err := fmt.Errorf("core loop reached max steps %d", maxCoreLoopSteps)
	reporter.SystemError(err.Error())
	return messages, err
}

func (r *SingleTurnRunner) prepareMessages(history []*schema.Message, userPrompt string) []*schema.Message {
	messages := cloneMessages(history)
	if len(messages) == 0 || messages[0].Role != schema.System {
		// 历史为空或缺少 system 时补上当前模式对应的 system prompt。
		messages = append([]*schema.Message{schema.SystemMessage(r.systemPrompt)}, messages...)
	}
	return append(messages, schema.UserMessage(userPrompt))
}

func cloneMessages(messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]*schema.Message, len(messages))
	copy(cloned, messages)
	return cloned
}

func (r *SingleTurnRunner) compactMessages(messages []*schema.Message) []*schema.Message {
	result := r.contextMgr.CompactMessages(messages)
	return result.Messages
}

func (r *SingleTurnRunner) writeTurnFooter(output io.Writer, messages []*schema.Message, stats *TurnStats) error {
	footer := r.contextMgr.FormatUsage(messages)
	if stats != nil {
		footer += " " + stats.FormatTurnStats()
	}
	if _, err := fmt.Fprintf(output, "\n%s", footer); err != nil {
		return fmt.Errorf("write turn footer: %w", err)
	}
	return nil
}

func (r *SingleTurnRunner) streamAssistantMessage(ctx context.Context, messages []*schema.Message, output io.Writer) (*schema.Message, error) {
	stream, err := r.model.Stream(ctx, messages, einomodel.WithTools(r.toolInfos))
	if err != nil {
		return nil, fmt.Errorf("start model stream: %w", err)
	}
	defer stream.Close()

	chunks := make([]*schema.Message, 0, 16)
	for {
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return nil, fmt.Errorf("read model stream chunk: %w", recvErr)
		}

		chunks = append(chunks, chunk)
		if chunk.Content != "" {
			if _, err := io.WriteString(output, chunk.Content); err != nil {
				return nil, fmt.Errorf("write model stream chunk: %w", err)
			}
		}
	}

	if len(chunks) == 0 {
		return schema.AssistantMessage("", nil), nil
	}

	assistantMsg, err := schema.ConcatMessages(chunks)
	if err != nil {
		return nil, fmt.Errorf("concat model stream chunks: %w", err)
	}
	return assistantMsg, nil
}

func buildToolRuntime(ctx context.Context, registeredTools []tools.RegisteredTool) ([]*schema.ToolInfo, map[string]einotool.InvokableTool, map[string]tools.RegisteredTool, error) {
	toolInfos := make([]*schema.ToolInfo, 0, len(registeredTools))
	invokers := make(map[string]einotool.InvokableTool, len(registeredTools))
	toolMetadata := make(map[string]tools.RegisteredTool, len(registeredTools))

	for _, registeredTool := range registeredTools {
		info, err := registeredTool.Tool.Info(ctx)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("get tool info: %w", err)
		}
		toolInfos = append(toolInfos, info)

		invokableTool, ok := registeredTool.Tool.(einotool.InvokableTool)
		if !ok {
			return nil, nil, nil, fmt.Errorf("tool %q does not implement InvokableTool", info.Name)
		}
		invokers[info.Name] = invokableTool
		toolMetadata[info.Name] = registeredTool
	}

	return toolInfos, invokers, toolMetadata, nil
}

func filterToolsForMode(mode permission.Mode, registeredTools []tools.RegisteredTool) []tools.RegisteredTool {
	if mode != permission.ModePlan {
		return registeredTools
	}

	// plan mode 不仅执行时拒绝写工具，也在模型可见工具列表里隐藏写入和 shell 能力。
	filtered := make([]tools.RegisteredTool, 0, len(registeredTools))
	for _, registeredTool := range registeredTools {
		if registeredTool.Kind == permission.ToolKindRead {
			filtered = append(filtered, registeredTool)
		}
	}
	return filtered
}

func (r *SingleTurnRunner) invokeTool(ctx context.Context, toolCall schema.ToolCall, reporter Reporter, stats *TurnStats) *schema.Message {
	if reporter == nil {
		reporter = NoopReporter{}
	}
	toolName := toolCall.Function.Name
	invoker, ok := r.invokers[toolName]
	if !ok {
		err := fmt.Errorf("tool %q is not registered", toolName)
		result := friendlyerror.ShortToolError(toolName, err)
		reporter.ToolError(toolName, toolCall.Function.Arguments, err.Error(), time.Time{})
		if stats != nil {
			stats.RecordToolCall(toolName, "")
			stats.RecordToolError(toolName, "")
		}
		return schema.ToolMessage(result, toolCall.ID, schema.WithToolName(toolName))
	}

	request, err := r.permissionRequest(ctx, toolName, toolCall.Function.Arguments)
	if err != nil {
		result := friendlyerror.ShortToolError(toolName, err)
		reporter.ToolError(toolName, toolCall.Function.Arguments, err.Error(), time.Time{})
		if stats != nil {
			stats.RecordToolCall(toolName, "")
			stats.RecordToolError(toolName, "")
		}
		return schema.ToolMessage(result, toolCall.ID, schema.WithToolName(toolName))
	}
	stats.RecordToolCall(toolName, request.Kind)

	decision, err := permission.Evaluate(r.mode, request)
	if err != nil {
		result := friendlyerror.ShortToolError(toolName, err)
		reporter.ToolError(toolName, toolCall.Function.Arguments, err.Error(), time.Time{})
		stats.RecordToolError(toolName, request.Kind)
		return schema.ToolMessage(result, toolCall.ID, schema.WithToolName(toolName))
	}
	if decision == permission.DecisionAsk {
		// default / shell 场景会进入这里，由 REPL 展示具体风险并等待用户确认。
		decision, err = r.confirmer.Confirm(ctx, request)
		if err != nil {
			result := friendlyerror.ShortToolError(toolName, err)
			reporter.ToolError(toolName, toolCall.Function.Arguments, err.Error(), time.Time{})
			stats.RecordToolError(toolName, request.Kind)
			return schema.ToolMessage(result, toolCall.ID, schema.WithToolName(toolName))
		}
	}
	if decision != permission.DecisionAllow {
		reason := r.permissionDeniedMessage(toolName, request.Kind, decision)
		reporter.ToolDenied(toolName, toolCall.Function.Arguments, reason)
		stats.RecordToolDenied(toolName, request.Kind)
		return schema.ToolMessage(reason, toolCall.ID, schema.WithToolName(toolName))
	}

	if toolName == "bash" {
		// bash 只有真正通过权限并开始执行时才计入运行统计。
		stats.RecordBashCommand()
	}
	startedAt := reporter.ToolStart(toolName, toolCall.Function.Arguments)
	result, err := invoker.InvokableRun(ctx, toolCall.Function.Arguments)
	if err != nil {
		reporter.ToolError(toolName, toolCall.Function.Arguments, err.Error(), startedAt)
		stats.RecordToolError(toolName, request.Kind)
		result = friendlyerror.ShortToolError(toolName, err)
	} else {
		reporter.ToolFinish(toolName, toolCall.Function.Arguments, result, request.Preview, startedAt)
		stats.RecordToolSuccess(toolName, request.Kind)
	}
	// 工具自身可能已经截断，Agent 层仍统一兜底，保证 tool_result 不会无限膨胀。
	result = r.contextMgr.TrimToolResult(toolName, result)
	return schema.ToolMessage(result, toolCall.ID, schema.WithToolName(toolName))
}

func (r *SingleTurnRunner) permissionDeniedMessage(toolName string, kind permission.ToolKind, decision permission.Decision) string {
	_ = decision
	_, nonInteractive := r.confirmer.(permission.DenyConfirmer)
	userDenied := !nonInteractive && r.mode != permission.ModePlan
	return friendlyerror.FormatPermissionDenied(toolName, r.mode, kind, nonInteractive, userDenied).Summary()
}

func (r *SingleTurnRunner) permissionRequest(ctx context.Context, toolName string, arguments string) (permission.Request, error) {
	metadata, ok := r.toolMetadata[toolName]
	if !ok {
		return permission.Request{}, fmt.Errorf("missing permission metadata for tool %q", toolName)
	}
	request := permission.Request{
		ToolName:  toolName,
		Arguments: arguments,
		Kind:      metadata.Kind,
		Risk:      metadata.Risk,
	}
	if r.mode != permission.ModePlan && metadata.Preview != nil {
		// diff preview 是给终端用户看的确认信息，不会进入 messages[]。
		preview, err := metadata.Preview(ctx, arguments)
		if err != nil {
			request.Preview = fmt.Sprintf("diff 预览不可用：%v", err)
		} else {
			request.Preview = preview
		}
	}
	return request, nil
}
