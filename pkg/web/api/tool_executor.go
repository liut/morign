package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/tools"
	toolsvc "github.com/liut/morign/pkg/services/tools"
)

// chatExecutor 定义聊天执行函数类型，支持流式/非流式
type chatExecutor func(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (string, []llm.ToolCall, *llm.Usage, error)

// ToolExecutor 封装工具调用循环逻辑
type ToolExecutor struct {
	toolreg *tools.Registry
}

// NewToolExecutor 创建 ToolExecutor
func NewToolExecutor(toolreg *tools.Registry) *ToolExecutor {
	return &ToolExecutor{toolreg: toolreg}
}

// ExecuteToolCallLoop 执行工具调用循环，直到无 tool calls
func (e *ToolExecutor) ExecuteToolCallLoop(
	ctx context.Context,
	messages []llm.Message,
	tools []llm.ToolDefinition,
	exec chatExecutor,
) (string, []llm.ToolCall, *llm.Usage, error) {
	for {
		answer, toolCalls, usage, err := exec(ctx, messages, tools)
		if err != nil {
			return "", nil, nil, err
		}

		if len(toolCalls) == 0 {
			return answer, nil, usage, nil
		}

		evs, msgs := e.ExecuteToolCalls(ctx, messages, toolCalls, "")
		messages = msgs
		if len(evs) == 0 {
			// 没有成功执行任何工具，跳出循环
			return answer, toolCalls, usage, nil
		}
	}
}

// ExecuteToolCalls 执行单轮工具调用，返回事件列表和更新后的消息。
func (e *ToolExecutor) ExecuteToolCalls(ctx context.Context, messages []llm.Message, toolCalls []llm.ToolCall, think string) ([]*llm.Event, []llm.Message) {
	if len(toolCalls) == 0 {
		return nil, messages
	}

	messages = append(messages, llm.Message{
		Role:      llm.RoleAssistant,
		Thinking:  think,
		ToolCalls: toolCalls,
	})

	var events []*llm.Event
	for _, tc := range toolCalls {
		logger().Infow("chat", "toolCallID", tc.ID, "toolCallType", tc.Type, "toolCallName", tc.Function.Name)

		if tc.Type != "function" {
			continue
		}

		var parameters map[string]any
		args := string(tc.Function.Arguments)
		if args != "" && args != "{}" {
			if err := json.Unmarshal(tc.Function.Arguments, &parameters); err != nil {
				logger().Infow("chat", "toolCallID", tc.ID, "args", args, "err", err)
				continue
			}
		}
		if parameters == nil {
			parameters = make(map[string]any)
		}

		content, err := e.toolreg.Invoke(ctx, tc.Function.Name, parameters)
		if err != nil {
			logger().Infow("invokeTool fail", "toolCallName", tc.Function.Name, "err", err)
			continue
		}

		logger().Infow("invokeTool ok", "toolCallName", tc.Function.Name,
			"content", toolsvc.ResultLogs(content))
		toolResult := formatToolResult(content)
		messages = append(messages, llm.Message{
			Role:       llm.RoleTool,
			Content:    toolResult,
			ToolCallID: tc.ID,
		})

		events = append(events, &llm.Event{
			ID:        llm.NewEventID(),
			Timestamp: time.Now(),
			Author:    tc.Function.Name,
			ToolResult: &llm.ToolResult{
				CallID:  tc.ID,
				Name:    tc.Function.Name,
				Content: toolResult,
			},
		})
	}

	return events, messages
}
