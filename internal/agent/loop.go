package agent

import (
	"context"
	"fmt"
	"io"
	"os"

	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"nanocode/internal/config"
	"nanocode/internal/model"
	"nanocode/internal/permission"
	"nanocode/internal/prompt"
	"nanocode/internal/tools"
)

const maxCoreLoopSteps = 8

type SingleTurnRunner struct {
	model        einomodel.ToolCallingChatModel
	toolInfos    []*schema.ToolInfo
	invokers     map[string]einotool.InvokableTool
	systemPrompt string
}

func NewSingleTurnRunner(ctx context.Context, cfg config.ModelConfig, mode permission.Mode) (*SingleTurnRunner, error) {
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

	toolRegistry := tools.NewRegistry(workspacePolicy)
	registeredTools, err := toolRegistry.Build(ctx)
	if err != nil {
		return nil, fmt.Errorf("build tools: %w", err)
	}

	toolInfos, invokers, err := buildToolRuntime(ctx, registeredTools)
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
		systemPrompt: prompt.BuildSystemPrompt(mode, workspacePolicy.RootDir()),
	}, nil
}

func (r *SingleTurnRunner) Run(ctx context.Context, userPrompt string, output io.Writer) error {
	// 这里显式实现 Claude Code 风格的核心循环：
	// messages -> model -> tool_calls? -> 执行工具 -> 追加 tool_result -> 回到 messages。
	messages := []*schema.Message{
		schema.SystemMessage(r.systemPrompt),
		schema.UserMessage(userPrompt),
	}

	for step := 0; step < maxCoreLoopSteps; step++ {
		assistantMsg, err := r.streamAssistantMessage(ctx, messages, output)
		if err != nil {
			return err
		}

		messages = append(messages, assistantMsg)

		if len(assistantMsg.ToolCalls) == 0 {
			return nil
		}

		for _, toolCall := range assistantMsg.ToolCalls {
			toolResult := r.invokeTool(ctx, toolCall)
			messages = append(messages, toolResult)
		}
	}

	return fmt.Errorf("core loop reached max steps %d", maxCoreLoopSteps)
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

func buildToolRuntime(ctx context.Context, registeredTools []einotool.BaseTool) ([]*schema.ToolInfo, map[string]einotool.InvokableTool, error) {
	toolInfos := make([]*schema.ToolInfo, 0, len(registeredTools))
	invokers := make(map[string]einotool.InvokableTool, len(registeredTools))

	for _, baseTool := range registeredTools {
		info, err := baseTool.Info(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("get tool info: %w", err)
		}
		toolInfos = append(toolInfos, info)

		invokableTool, ok := baseTool.(einotool.InvokableTool)
		if !ok {
			return nil, nil, fmt.Errorf("tool %q does not implement InvokableTool", info.Name)
		}
		invokers[info.Name] = invokableTool
	}

	return toolInfos, invokers, nil
}

func (r *SingleTurnRunner) invokeTool(ctx context.Context, toolCall schema.ToolCall) *schema.Message {
	toolName := toolCall.Function.Name
	invoker, ok := r.invokers[toolName]
	if !ok {
		return schema.ToolMessage(fmt.Sprintf("tool %q is not registered", toolName), toolCall.ID, schema.WithToolName(toolName))
	}

	result, err := invoker.InvokableRun(ctx, toolCall.Function.Arguments)
	if err != nil {
		result = fmt.Sprintf("tool %q failed: %v", toolName, err)
	}

	return schema.ToolMessage(result, toolCall.ID, schema.WithToolName(toolName))
}
