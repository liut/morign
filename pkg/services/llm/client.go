package llm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
)

// ErrUnsupportedProvider 不支持的 provider
var ErrUnsupportedProvider = errors.New("unsupported provider")

// Client LLM 客户端接口
type Client interface {
	// Chat 发送聊天请求，返回完整响应
	Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*ChatResult, error)
	// StreamChat 发送流式聊天请求，返回 iter.Seq2 事件流
	StreamChat(ctx context.Context, messages []Message, tools []ToolDefinition) iter.Seq2[*Event, error]
	// Generate 简单文本生成（用于关键词提取等）
	Generate(ctx context.Context, prompt string) (string, *Usage, error)
	// Embedding 向量化文本
	Embedding(ctx context.Context, texts []string) ([]float64, error)
}

// client LLM 客户端默认实现
type client struct {
	cfg      *config
	provider provider
}

// provider 接口定义
type provider interface {
	Chat(ctx context.Context, cfg *config, messages []Message, tools []ToolDefinition) (*ChatResult, error)
	StreamChat(ctx context.Context, cfg *config, messages []Message, tools []ToolDefinition) iter.Seq2[*Event, error]
	Generate(ctx context.Context, cfg *config, prompt string) (string, *Usage, error)
	Embedding(ctx context.Context, cfg *config, texts []string) ([]float64, error)
}

// NewClient 创建新的 LLM 客户端
func NewClient(opts ...Option) (Client, error) {
	cfg := applyOptions(opts...)

	var p provider
	switch strings.ToLower(strings.TrimSpace(cfg.provider)) {
	case "", "openai", "openrouter", "ollama":
		p = newOpenAIProvider()
	case "anthropic":
		p = newAnthropicProvider()
	case "gemini":
		// TODO: 实现 gemini provider
		return nil, fmt.Errorf("%w: gemini not implemented yet", ErrUnsupportedProvider)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, cfg.provider)
	}

	if strings.HasPrefix(cfg.model, "kimi") {
		cfg.temperature = 0
	}

	return &client{
		cfg:      cfg,
		provider: p,
	}, nil
}

// Chat 发送聊天请求
func (c *client) Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*ChatResult, error) {
	return c.provider.Chat(ctx, c.cfg, messages, tools)
}

// StreamChat 发送流式聊天请求，返回 iter.Seq2 事件流
func (c *client) StreamChat(ctx context.Context, messages []Message, tools []ToolDefinition) iter.Seq2[*Event, error] {
	return c.provider.StreamChat(ctx, c.cfg, messages, tools)
}

// Generate 简单文本生成
func (c *client) Generate(ctx context.Context, prompt string) (string, *Usage, error) {
	return c.provider.Generate(ctx, c.cfg, prompt)
}

// Embedding 向量化文本
func (c *client) Embedding(ctx context.Context, texts []string) ([]float64, error) {
	return c.provider.Embedding(ctx, c.cfg, texts)
}
