package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/tools"
	"github.com/liut/morign/pkg/settings"
)

// StreamCallbacks 流式响应回调
type StreamCallbacks struct {
	OnDelta func(delta string)
	OnThink func(think string)
}

// Agent 封装 CLI agent 的对话能力，复用 ToolExecutor 的工具调用循环
type Agent struct {
	llm         llm.Client
	toolreg     *tools.Registry
	toolExec    *ToolExecutor
	sysPrompt   string
	toolsPrompt string
}

// NewAgent 创建 Agent
func NewAgent(llmClient llm.Client, toolreg *tools.Registry, sysPrompt, toolsPrompt string) *Agent {
	return &Agent{
		llm:         llmClient,
		toolreg:     toolreg,
		toolExec:    NewToolExecutor(toolreg),
		sysPrompt:   sysPrompt,
		toolsPrompt: toolsPrompt,
	}
}

// BuildSystemMessage 构建 system message，包含 tools 列表
func (ag *Agent) BuildSystemMessage(ctx context.Context) (llm.Message, []llm.ToolDefinition) {
	var sb strings.Builder

	sysPrompt := ag.sysPrompt
	if sysPrompt == "" {
		sysPrompt = dftSystemMsg
	}
	sb.WriteString(sysPrompt)

	if settings.Current.DateInContext {
		sb.WriteString("\n")
		sb.WriteString(thisMoment())
	}

	tools := convertMCPToolsToLLMTools(ag.toolreg.ToolsFor(ctx))
	if len(tools) > 0 {
		toolsPrompt := ag.toolsPrompt
		if toolsPrompt == "" {
			toolsPrompt = dftToolsMsg
		}
		sb.WriteString("\n")
		sb.WriteString(toolsPrompt)
	}

	return llm.Message{Role: llm.RoleSystem, Content: sb.String()}, tools
}

// Chat 非流式对话，支持工具调用循环
func (ag *Agent) Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (string, error) {
	exec := func(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (string, []llm.ToolCall, *llm.Usage, error) {
		result, err := ag.llm.Chat(ctx, messages, tools)
		if err != nil {
			return "", nil, nil, err
		}
		return result.Content, result.ToolCalls, result.Usage, nil
	}

	answer, _, _, err := ag.toolExec.ExecuteToolCallLoop(ctx, messages, tools, exec)
	return answer, err
}

// StreamChat 流式对话，支持工具调用循环
// 通过 StreamCallbacks 回调输出 delta 和 think，返回最终完整回答文本
func (ag *Agent) StreamChat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition, cb StreamCallbacks) (string, error) {
	maxLoop := settings.Current.MaxLoopIterations
	if maxLoop <= 0 {
		maxLoop = 5
	}

	var fullAnswer string
	var fullThink string

	for iter := 0; iter < maxLoop; iter++ {
		stream, err := ag.llm.StreamChat(ctx, messages, tools)
		if err != nil {
			return fullAnswer, fmt.Errorf("stream chat: %w", err)
		}

		var roundAnswer string
		var roundThink string
		var toolCalls []llm.ToolCall

		for result := range stream {
			if result.Error != nil {
				return fullAnswer, result.Error
			}
			if result.Delta != "" {
				roundAnswer += result.Delta
				if cb.OnDelta != nil {
					cb.OnDelta(result.Delta)
				}
			}
			if result.Think != "" {
				roundThink += result.Think
				if cb.OnThink != nil {
					cb.OnThink(result.Think)
				}
			}
			if result.Done {
				toolCalls = result.ToolCalls
			}
		}

		fullAnswer += roundAnswer
		fullThink += roundThink

		if len(toolCalls) == 0 {
			break
		}

		messages, _ = ag.toolExec.ExecuteToolCalls(ctx, messages, toolCalls, roundThink)
	}

	return fullAnswer, nil
}
