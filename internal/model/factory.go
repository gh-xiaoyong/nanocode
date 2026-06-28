package model

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"

	"nanocode/internal/config"
)

func NewChatModel(ctx context.Context, cfg config.ModelConfig) (einomodel.BaseChatModel, error) {
	temperature := cfg.Temperature
	maxTokens := cfg.MaxTokens

	// 这里统一收敛 OpenAI-compatible 配置，后续扩展 provider 时只需要替换这一层工厂。
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL:             cfg.BaseURL,
		APIKey:              cfg.APIKey,
		Model:               cfg.Model,
		Temperature:         &temperature,
		MaxCompletionTokens: &maxTokens,
	})
}

func NewToolCallingChatModel(ctx context.Context, cfg config.ModelConfig) (einomodel.ToolCallingChatModel, error) {
	chatModel, err := NewChatModel(ctx, cfg)
	if err != nil {
		return nil, err
	}

	toolCallingModel, ok := chatModel.(einomodel.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("configured chat model does not support tool calling")
	}

	return toolCallingModel, nil
}
