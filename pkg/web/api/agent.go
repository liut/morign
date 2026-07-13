package api

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"github.com/liut/morign/pkg/services/agent"
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
	toolExec    *agent.ToolExecutor
	sysPrompt   string
	toolsPrompt string
}

// NewAgent 创建 Agent
func NewAgent(llmClient llm.Client, toolreg *tools.Registry, sysPrompt, toolsPrompt string) *Agent {
	return &Agent{
		llm:         llmClient,
		toolreg:     toolreg,
		toolExec:    agent.NewToolExecutor(toolreg),
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

// Chat 非流式对话，使用 AgentLoop.RunNonStreaming。
func (ag *Agent) Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (string, error) {
	loop := agent.NewAgentLoop(agent.AgentLoopConfig{
		LLM:      ag.llm,
		ToolExec: ag.toolExec,
	}, agent.WithMaxLoop(settings.Current.MaxLoopIterations))

	return loop.RunNonStreaming(ctx, messages, tools)
}

// Run 以 iter.Seq2 方式执行对话，使用 AgentLoop。
func (ag *Agent) Run(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) iter.Seq2[*llm.Event, error] {
	loop := agent.NewAgentLoop(agent.AgentLoopConfig{
		LLM:      ag.llm,
		ToolExec: ag.toolExec,
	}, agent.WithMaxLoop(settings.Current.MaxLoopIterations))

	return func(yield func(*llm.Event, error) bool) {
		for event, err := range loop.Run(ctx, messages, tools) {
			if !yield(event, err) {
				return
			}
		}
	}
}

// StreamChat 流式对话，通过 StreamCallbacks 回调输出，返回最终文本。
func (ag *Agent) StreamChat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition, cb StreamCallbacks) (string, error) {
	var fullAnswer string
	for event, err := range ag.Run(ctx, messages, tools) {
		if err != nil {
			return fullAnswer, fmt.Errorf("stream chat: %w", err)
		}
		fullAnswer += event.Delta
		if event.Delta != "" && cb.OnDelta != nil {
			cb.OnDelta(event.Delta)
		}
		if event.Think != "" && cb.OnThink != nil {
			cb.OnThink(event.Think)
		}
		if event.StopReason == llm.FinishReasonToolCalls {
			cb.OnThink("\n")
		}
	}
	return fullAnswer, nil
}
