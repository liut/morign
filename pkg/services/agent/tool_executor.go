package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/tools"
	"github.com/liut/morign/pkg/settings"
	"github.com/liut/morign/pkg/utils/words"
)

// ToolExecutor 封装工具调用循环逻辑
type ToolExecutor struct {
	toolreg *tools.Registry

	// BeforeToolCall 在每个工具调用前执行。返回 block=true 时阻止工具执行，
	// reason 作为 error 内容写入 ToolResult。可选，nil 时跳过。
	// 同一批次的工具并发执行，钩子会被并发调用。
	BeforeToolCall func(ctx context.Context, name string, params map[string]any) (block bool, reason string)

	// AfterToolCall 在每个工具成功执行后调用，可修改 result。
	// 返回的 map 会替代原 result 用于后续格式化。可选，nil 时跳过。
	// 同一批次的工具并发执行，钩子会被并发调用。
	AfterToolCall func(ctx context.Context, name string, result map[string]any) map[string]any
}

// NewToolExecutor 创建 ToolExecutor
func NewToolExecutor(toolreg *tools.Registry) *ToolExecutor {
	return &ToolExecutor{toolreg: toolreg}
}

// ExecuteToolCalls 执行单轮工具调用（并发），返回事件列表、更新后的消息和 allTerminate。
// allTerminate 在本次所有 tool 都标记 terminate 时为 true。
// 每个 tool call 恰好产出一个结果，失败的调用也产出结构化错误结果，保证
// assistant 消息里的 tool_calls 与后续 tool message 一一配对。
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
			results[idx] = e.callTool(ctx, tc)
		}(i, tc)
	}

	wg.Wait()

	// Collect results in call order, build messages
	events := make([]*llm.Event, 0, n)
	allTerminate := true
	for i, ev := range results {
		if ev == nil {
			// 工具 goroutine 未落结果（如 runtime.Goexit），补一个错误结果维持与 tool_calls 的配对。
			ev = toolErrorResult(toolCalls[i].ID, toolCalls[i].Function.Name,
				llm.ToolErrorExecutionFailed, "tool call did not complete")
		}
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

	return events, messages, allTerminate
}

// formatToolResult 将工具结果转换为文本字符串，并按配置上限截断
func formatToolResult(result map[string]any) string {
	return truncateToolResult(toolResultText(result))
}

// toolResultText 优先提取 content 数组中的 text，否则使用 structuredContent，最后序列化为 JSON
func toolResultText(result map[string]any) string {
	if result == nil {
		return ""
	}
	// 优先提取 content 数组中的 text
	if text := contentText(result["content"]); text != "" {
		return text
	}
	// 备选：使用 structuredContent
	if sc, ok := result["structuredContent"].(string); ok {
		return sc
	}
	if sc, ok := result["structuredContent"].(map[string]any); ok {
		if s, ok := sc["text"].(string); ok {
			return s
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

// truncateToolResult 按 rune 截断超长结果，保留头部 3/4 与尾部 1/4 并附截断说明；
// 上限为 0 或未超限时原样返回。省略说明计入上限，总长度不超过上限。
func truncateToolResult(text string) string {
	limit := settings.Current.ToolResultMaxChars
	if limit <= 0 {
		return text
	}
	size := utf8.RuneCountInString(text)
	if size <= limit {
		return text
	}
	headLen, tailLen, notice := truncSplit(limit, size)
	if notice == "" {
		// 上限容不下任何省略说明，只保留头部
		return words.TakeHead(text, limit)
	}
	return words.TakeHead(text, headLen) + notice + words.TakeTail(text, tailLen)
}

// callTool 执行单次工具调用，任何失败都转成结构化错误结果，保证每个 tool call 恰好一个结果。
func (e *ToolExecutor) callTool(ctx context.Context, tc llm.ToolCall) (ev *llm.Event) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("tool call panic recovered", "tool", tc.Function.Name, "panic", r)
			ev = toolErrorResult(tc.ID, tc.Function.Name, llm.ToolErrorPanic, fmt.Sprintf("%v", r))
		}
	}()

	slog.Info("chat", "toolCallID", tc.ID, "toolCallType", tc.Type, "toolCallName", tc.Function.Name)

	if tc.Type != "function" {
		return toolErrorResult(tc.ID, tc.Function.Name, llm.ToolErrorUnsupportedType, tc.Type)
	}

	var parameters map[string]any
	args := string(tc.Function.Arguments)
	if args != "" && args != "{}" {
		if err := json.Unmarshal(tc.Function.Arguments, &parameters); err != nil {
			slog.Info("chat", "toolCallID", tc.ID, "args", args, "err", err)
			return toolErrorResult(tc.ID, tc.Function.Name, llm.ToolErrorInvalidArguments, err.Error())
		}
	}
	if parameters == nil {
		parameters = make(map[string]any)
	}

	// Before hook
	if e.BeforeToolCall != nil {
		block, reason := e.BeforeToolCall(ctx, tc.Function.Name, parameters)
		if block {
			return toolErrorResult(tc.ID, tc.Function.Name, llm.ToolErrorRejected, reason)
		}
	}

	content, err := e.toolreg.Invoke(ctx, tc.Function.Name, parameters)
	if err != nil {
		slog.Info("invokeTool fail", "toolCallName", tc.Function.Name, "err", err)
		return toolErrorResult(tc.ID, tc.Function.Name, llm.ToolErrorExecutionFailed, err.Error())
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

	status, code := llm.ToolStatusOK, ""
	if mcps.IsToolError(content) {
		status, code = llm.ToolStatusError, llm.ToolErrorToolError
	}
	return toolResultEvent(tc.ID, tc.Function.Name, formatToolResult(content), terminate, status, code)
}

// toolResultEvent 构造一个工具结果事件。
func toolResultEvent(callID, name, content string, terminate bool, status, code string) *llm.Event {
	return &llm.Event{
		ID:        llm.NewEventID(),
		Timestamp: time.Now(),
		Author:    name,
		ToolResult: &llm.ToolResult{
			CallID:    callID,
			Name:      name,
			Content:   content,
			Terminate: terminate,
			Status:    status,
			ErrorCode: code,
		},
	}
}

// toolErrorResult 构造一个失败调用的工具结果事件，错误码决定执行状态。
// 错误文本同样受 ToolResultMaxChars 约束，避免超长错误撑爆上下文。
func toolErrorResult(callID, name, code, detail string) *llm.Event {
	status := llm.ToolStatusError
	if code == llm.ToolErrorRejected {
		status = llm.ToolStatusRejected
	}
	return toolResultEvent(callID, name, truncateToolResult(formatToolError(name, code, detail)), false, status, code)
}

// formatToolError 生成对模型可见的错误文本，给出可行动的信息而非裸错误串。
// rejected 与 permission_denied 保留拒绝理由原文，其余给出带上下文的说明。
func formatToolError(name, code, detail string) string {
	switch code {
	case llm.ToolErrorRejected, llm.ToolErrorPermissionDenied:
		return detail
	case llm.ToolErrorInvalidArguments:
		return fmt.Sprintf("tool %q was called with invalid arguments: %s. Arguments must be a JSON object matching the tool's input schema.", name, detail)
	case llm.ToolErrorUnsupportedType:
		return fmt.Sprintf("tool %q was called with unsupported type %q. Only \"function\" tool calls are executed.", name, detail)
	case llm.ToolErrorPanic:
		return fmt.Sprintf("tool %q panicked during execution: %s. The call did not complete.", name, detail)
	case llm.ToolErrorExecutionFailed:
		return fmt.Sprintf("tool %q failed to execute: %s", name, detail)
	default:
		return fmt.Sprintf("tool %q failed: %s", name, detail)
	}
}

// contentText 提取 content 数组里首个非空 text 字段。
// mcps.BuildTool*Result 构造的 content 是 []map[string]any，JSON 反序列化的结果是 []any。
func contentText(content any) string {
	switch items := content.(type) {
	case []map[string]any:
		for _, item := range items {
			if text, ok := item["text"].(string); ok && text != "" {
				return text
			}
		}
	case []any:
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := m["text"].(string); ok && text != "" {
				return text
			}
		}
	}
	return ""
}

// truncSplit 计算头部与尾部的字符数及省略说明，使三者总长不超过 limit。
// 省略说明的长度随其中数字的位数变化，按上限预留只会偏保守，故不会超限。
// 省略说明过长时逐级降级为更短的说明。
func truncSplit(limit, size int) (headLen, tailLen int, notice string) {
	for _, format := range []truncNoticeFormat{truncNoticeFull, truncNoticeShort, truncNoticeMinimal} {
		payload := limit - utf8.RuneCountInString(format(limit, limit, size))
		if payload <= 0 {
			continue
		}
		headLen = payload * 3 / 4
		tailLen = payload - headLen
		return headLen, tailLen, format(headLen, tailLen, size)
	}
	return 0, 0, ""
}

// truncNoticeFormat 是省略说明的格式，%d 依次为头部字符数、尾部字符数、总字符数。
type truncNoticeFormat func(headLen, tailLen, size int) string

func truncNoticeFull(headLen, tailLen, size int) string {
	return fmt.Sprintf(
		"\n[truncated: showing the first %d and last %d of %d characters; narrow the query or page through the source to get the rest]",
		headLen, tailLen, size)
}

func truncNoticeShort(headLen, tailLen, size int) string {
	return fmt.Sprintf("\n[truncated: first %d + last %d of %d chars]", headLen, tailLen, size)
}

func truncNoticeMinimal(headLen, tailLen, size int) string {
	return "\n[truncated]"
}
