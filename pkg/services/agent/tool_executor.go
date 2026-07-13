package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/tools"
)

// ChatExecutor 定义聊天执行函数类型，支持流式/非流式
type ChatExecutor func(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (string, []llm.ToolCall, *llm.Usage, error)

// ToolExecutor 封装工具调用循环逻辑
type ToolExecutor struct {
	toolreg *tools.Registry

	// BeforeToolCall 在每个工具调用前执行。返回 block=true 时阻止工具执行，
	// reason 作为 error 内容写入 ToolResult。可选，nil 时跳过。
	BeforeToolCall func(ctx context.Context, name string, params map[string]any) (block bool, reason string)

	// AfterToolCall 在每个工具成功执行后调用，可修改 result。
	// 返回的 map 会替代原 result 用于后续格式化。可选，nil 时跳过。
	AfterToolCall func(ctx context.Context, name string, result map[string]any) map[string]any
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
	exec ChatExecutor,
) (string, []llm.ToolCall, *llm.Usage, error) {
	for {
		answer, toolCalls, usage, err := exec(ctx, messages, tools)
		if err != nil {
			return "", nil, nil, err
		}

		if len(toolCalls) == 0 {
			return answer, nil, usage, nil
		}

		evs, msgs, allTerminate := e.ExecuteToolCalls(ctx, messages, toolCalls, "")
		messages = msgs
		if allTerminate {
			return answer, toolCalls, usage, nil
		}
		if len(evs) == 0 {
			// 没有成功执行任何工具，跳出循环
			return answer, toolCalls, usage, nil
		}
	}
}

// ExecuteToolCalls 执行单轮工具调用（并发），返回事件列表、更新后的消息和 allTerminate。
// allTerminate 在本次所有成功执行的 tool 都标记 terminate 时为 true。
func (e *ToolExecutor) ExecuteToolCalls(ctx context.Context, messages []llm.Message, toolCalls []llm.ToolCall, think string) ([]*llm.Event, []llm.Message, bool) {
	if len(toolCalls) == 0 {
		return nil, messages, false
	}

	messages = append(messages, llm.Message{
		Role:      llm.RoleAssistant,
		Thinking:  think,
		ToolCalls: toolCalls,
	})

	n := len(toolCalls)
	results := make([]*llm.Event, n)
	var wg sync.WaitGroup
	wg.Add(n)

	for i, tc := range toolCalls {
		go func(idx int, tc llm.ToolCall) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("tool call panic recovered", "tool", tc.Function.Name, "panic", r)
				}
			}()

			slog.Info("chat", "toolCallID", tc.ID, "toolCallType", tc.Type, "toolCallName", tc.Function.Name)

			if tc.Type != "function" {
				return
			}

			var parameters map[string]any
			args := string(tc.Function.Arguments)
			if args != "" && args != "{}" {
				if err := json.Unmarshal(tc.Function.Arguments, &parameters); err != nil {
					slog.Info("chat", "toolCallID", tc.ID, "args", args, "err", err)
					return
				}
			}
			if parameters == nil {
				parameters = make(map[string]any)
			}

			// Before hook
			if e.BeforeToolCall != nil {
				block, reason := e.BeforeToolCall(ctx, tc.Function.Name, parameters)
				if block {
					results[idx] = &llm.Event{
						ID:        llm.NewEventID(),
						Timestamp: time.Now(),
						Author:    tc.Function.Name,
						ToolResult: &llm.ToolResult{
							CallID:  tc.ID,
							Name:    tc.Function.Name,
							Content: reason,
						},
					}
					return
				}
			}

			content, err := e.toolreg.Invoke(ctx, tc.Function.Name, parameters)
			if err != nil {
				slog.Info("invokeTool fail", "toolCallName", tc.Function.Name, "err", err)
				return
			}

			slog.Info("invokeTool ok", "toolCallName", tc.Function.Name,
				"content", tools.ResultLogs(content))

			// After hook
			if e.AfterToolCall != nil {
				content = e.AfterToolCall(ctx, tc.Function.Name, content)
			}

			// Extract terminate from result
			terminate := false
			if t, ok := content["terminate"]; ok {
				if tb, ok := t.(bool); ok {
					terminate = tb
				}
			}

			toolResult := formatToolResult(content)
			results[idx] = &llm.Event{
				ID:        llm.NewEventID(),
				Timestamp: time.Now(),
				Author:    tc.Function.Name,
				ToolResult: &llm.ToolResult{
					CallID:    tc.ID,
					Name:      tc.Function.Name,
					Content:   toolResult,
					Terminate: terminate,
				},
			}
		}(i, tc)
	}

	wg.Wait()

	// Collect results in call order, build messages
	var events []*llm.Event
	allTerminate := true
	hasResult := false
	for i, ev := range results {
		if ev == nil {
			allTerminate = false
			continue
		}
		hasResult = true
		if !ev.ToolResult.Terminate {
			allTerminate = false
		}
		events = append(events, ev)
		messages = append(messages, llm.Message{
			Role:       llm.RoleTool,
			Content:    ev.ToolResult.Content,
			ToolCallID: toolCalls[i].ID,
		})
	}

	if !hasResult {
		allTerminate = false
	}

	return events, messages, allTerminate
}

// formatToolResult 将工具结果转换为文本字符串
// 优先提取 content 数组中的 text，否则使用 structuredContent
func formatToolResult(result map[string]any) string {
	if result == nil {
		return ""
	}
	// 优先提取 content 数组中的 text
	if content, ok := result["content"].([]any); ok {
		for _, c := range content {
			if cMap, ok := c.(map[string]any); ok {
				if text, ok := cMap["text"].(string); ok && text != "" {
					return text
				}
			}
		}
	}
	// 备选：使用 structuredContent
	if sc, ok := result["structuredContent"].(string); ok {
		return sc
	}
	if sc, ok := result["structuredContent"].(map[string]any); ok {
		for k, v := range sc {
			if s, ok := v.(string); ok && k == "text" {
				return s
			}
		}
		if b, err := json.Marshal(sc); err == nil {
			return string(b)
		}
	}
	// 最后：序列化为 JSON
	if b, err := json.Marshal(result); err == nil {
		return string(b)
	}
	return ""
}
