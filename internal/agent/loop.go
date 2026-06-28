package agent

import (
	"context"
	"fmt"
	"io"
	"os"

	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"nanocode/internal/checkpoint"
	"nanocode/internal/config"
	"nanocode/internal/contextmgr"
	"nanocode/internal/memory"
	"nanocode/internal/model"
	"nanocode/internal/permission"
	"nanocode/internal/prompt"
	"nanocode/internal/tools"
)

const maxCoreLoopSteps = 8

type SingleTurnRunner struct {
	// SingleTurnRunner 持有一次 Agent 执行需要的运行时对象：
	// 模型、工具、权限确认、checkpoint、上下文预算都在这里汇合。
	model        einomodel.ToolCallingChatModel
	toolInfos    []*schema.ToolInfo
	invokers     map[string]einotool.InvokableTool
	toolMetadata map[string]tools.RegisteredTool
	systemPrompt string
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

func (r *SingleTurnRunner) Run(ctx context.Context, userPrompt string, output io.Writer) error {
	_, err := r.RunWithMessages(ctx, nil, userPrompt, output)
	return err
}

func (r *SingleTurnRunner) RunWithMessages(ctx context.Context, history []*schema.Message, userPrompt string, output io.Writer) ([]*schema.Message, error) {
	// 阶段 5 起，REPL 会把历史 messages 传进来；本轮结束后再把更新后的 messages 保存回 session。
	messages := r.prepareMessages(history, userPrompt)

	for step := 0; step < maxCoreLoopSteps; step++ {
		// 每次调模型前先做预算检查，避免工具结果连续回填后把上下文撑爆。
		messages = r.compactMessages(messages)
		assistantMsg, err := r.streamAssistantMessage(ctx, messages, output)
		if err != nil {
			return messages, err
		}

		messages = append(messages, assistantMsg)
		if len(assistantMsg.ToolCalls) == 0 {
			if err := r.writeContextUsage(output, messages); err != nil {
				return messages, err
			}
			return messages, nil
		}

		for _, toolCall := range assistantMsg.ToolCalls {
			messages = append(messages, r.invokeTool(ctx, toolCall))
		}
	}

	return messages, fmt.Errorf("core loop reached max steps %d", maxCoreLoopSteps)
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

func (r *SingleTurnRunner) writeContextUsage(output io.Writer, messages []*schema.Message) error {
	if _, err := fmt.Fprintf(output, "\n%s", r.contextMgr.FormatUsage(messages)); err != nil {
		return fmt.Errorf("write context usage: %w", err)
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

	// plan mode 不仅执行时拒绝写工具，也在模型可见工具列表里直接隐藏写入和 shell 能力。
	filtered := make([]tools.RegisteredTool, 0, len(registeredTools))
	for _, registeredTool := range registeredTools {
		if registeredTool.Kind == permission.ToolKindRead {
			filtered = append(filtered, registeredTool)
		}
	}
	return filtered
}

func (r *SingleTurnRunner) invokeTool(ctx context.Context, toolCall schema.ToolCall) *schema.Message {
	toolName := toolCall.Function.Name
	invoker, ok := r.invokers[toolName]
	if !ok {
		return schema.ToolMessage(fmt.Sprintf("tool %q is not registered", toolName), toolCall.ID, schema.WithToolName(toolName))
	}

	request, err := r.permissionRequest(toolName, toolCall.Function.Arguments)
	if err != nil {
		return schema.ToolMessage(fmt.Sprintf("tool %q failed permission check: %v", toolName, err), toolCall.ID, schema.WithToolName(toolName))
	}

	decision, err := permission.Evaluate(r.mode, request)
	if err != nil {
		return schema.ToolMessage(fmt.Sprintf("tool %q failed permission check: %v", toolName, err), toolCall.ID, schema.WithToolName(toolName))
	}
	if decision == permission.DecisionAsk {
		// default / shell 场景会进入这里，由 REPL 展示具体风险并等待用户确认。
		decision, err = r.confirmer.Confirm(ctx, request)
		if err != nil {
			return schema.ToolMessage(fmt.Sprintf("tool %q permission confirmation failed: %v", toolName, err), toolCall.ID, schema.WithToolName(toolName))
		}
	}
	if decision != permission.DecisionAllow {
		return schema.ToolMessage(fmt.Sprintf("tool %q was denied by permission policy in %s mode", toolName, r.mode), toolCall.ID, schema.WithToolName(toolName))
	}

	result, err := invoker.InvokableRun(ctx, toolCall.Function.Arguments)
	if err != nil {
		result = fmt.Sprintf("tool %q failed: %v", toolName, err)
	}
	// 工具自身可能已经截断，Agent 层仍统一兜底，保证 tool_result 不会无限膨胀。
	result = r.contextMgr.TrimToolResult(toolName, result)
	return schema.ToolMessage(result, toolCall.ID, schema.WithToolName(toolName))
}

func (r *SingleTurnRunner) permissionRequest(toolName string, arguments string) (permission.Request, error) {
	metadata, ok := r.toolMetadata[toolName]
	if !ok {
		return permission.Request{}, fmt.Errorf("missing permission metadata for tool %q", toolName)
	}
	return permission.Request{
		ToolName:  toolName,
		Arguments: arguments,
		Kind:      metadata.Kind,
		Risk:      metadata.Risk,
	}, nil
}
