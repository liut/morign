package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/services/llm"
	"github.com/liut/morign/pkg/services/tools"
	"github.com/liut/morign/pkg/settings"
)

func testRegistry() *tools.Registry {
	return tools.NewRegistry(nil)
}

func tc(id, name string, args map[string]any) llm.ToolCall {
	raw, _ := json.Marshal(args)
	return llm.ToolCall{
		ID:   id,
		Type: "function",
		Function: llm.ToolCallFunc{
			Name:      name,
			Arguments: raw,
		},
	}
}

func TestExecuteToolCallsConcurrent(t *testing.T) {
	// Scenario 1: Happy path — two tools execute concurrently, results in call order.
	reg := testRegistry()
	var orderMu sync.Mutex
	var order []string
	ready := make(chan struct{})

	reg.RegisterInvoker("alpha", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		orderMu.Lock()
		order = append(order, "alpha")
		orderMu.Unlock()
		ready <- struct{}{} // signal started
		return textToolResult("alpha-result"), nil
	})
	reg.RegisterInvoker("beta", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		orderMu.Lock()
		order = append(order, "beta")
		orderMu.Unlock()
		<-ready // wait for alpha to also start (proves concurrency)
		return textToolResult("beta-result"), nil
	})

	te := NewToolExecutor(reg)
	calls := []llm.ToolCall{
		tc("c1", "alpha", nil),
		tc("c2", "beta", nil),
	}

	evs, msgs, allTerm := te.ExecuteToolCalls(context.Background(), nil, calls, "")

	if len(order) != 2 {
		t.Errorf("expected 2 invocations, got %d", len(order))
	}
	if len(evs) != 2 {
		t.Fatalf("expected 2 events, got %d", len(evs))
	}
	if evs[0].ToolResult.CallID != "c1" {
		t.Errorf("first event CallID = %q, want c1", evs[0].ToolResult.CallID)
	}
	if evs[1].ToolResult.CallID != "c2" {
		t.Errorf("second event CallID = %q, want c2", evs[1].ToolResult.CallID)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (nil+assistant+2 tool results), got %d", len(msgs))
	}
	if msgs[0].Role != llm.RoleAssistant {
		t.Errorf("first msg role = %q, want assistant", msgs[0].Role)
	}
	if allTerm {
		t.Error("allTerminate should be false when no tool sets terminate")
	}
}

func TestExecuteToolCallsBeforeHookAllow(t *testing.T) {
	// Scenario 2: BeforeToolCall returns block=false, tool executes normally.
	reg := testRegistry()
	reg.RegisterInvoker("echo", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return textToolResult("ok"), nil
	})

	te := NewToolExecutor(reg)
	var beforeCalled bool
	te.BeforeToolCall = func(ctx context.Context, name string, params map[string]any) (bool, string) {
		beforeCalled = true
		return false, ""
	}

	evs, _, _ := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "echo", nil)}, "")

	if !beforeCalled {
		t.Error("BeforeToolCall was not invoked")
	}
	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if evs[0].ToolResult.Content != "ok" {
		t.Errorf("result = %q, want ok", evs[0].ToolResult.Content)
	}
}

func TestExecuteToolCallsBeforeHookBlock(t *testing.T) {
	// Scenario 4: BeforeToolCall returns block=true, tool does not execute.
	reg := testRegistry()
	var invoked bool
	reg.RegisterInvoker("echo", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		invoked = true
		return textToolResult("ok"), nil
	})

	te := NewToolExecutor(reg)
	te.BeforeToolCall = func(ctx context.Context, name string, params map[string]any) (bool, string) {
		return true, "blocked by policy"
	}

	evs, msgs, _ := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "echo", nil)}, "")

	if invoked {
		t.Error("tool should NOT have been invoked when blocked")
	}
	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if evs[0].ToolResult.Content != "blocked by policy" {
		t.Errorf("error content = %q, want 'blocked by policy'", evs[0].ToolResult.Content)
	}
	if evs[0].ToolResult.Status != llm.ToolStatusRejected {
		t.Errorf("status = %q, want %q", evs[0].ToolResult.Status, llm.ToolStatusRejected)
	}
	if evs[0].ToolResult.ErrorCode != llm.ToolErrorRejected {
		t.Errorf("error code = %q, want %q", evs[0].ToolResult.ErrorCode, llm.ToolErrorRejected)
	}
	if evs[0].ToolResult.Terminate {
		t.Error("blocked tool should NOT set Terminate=true by default")
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (assistant + 1 tool result), got %d", len(msgs))
	}
}

func TestExecuteToolCallsAfterHook(t *testing.T) {
	// Scenario 3: AfterToolCall modifies the result.
	reg := testRegistry()
	reg.RegisterInvoker("echo", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return textToolResult("original"), nil
	})

	te := NewToolExecutor(reg)
	te.AfterToolCall = func(ctx context.Context, name string, result map[string]any) map[string]any {
		return textToolResult("modified")
	}

	evs, _, _ := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "echo", nil)}, "")

	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if evs[0].ToolResult.Content != "modified" {
		t.Errorf("after hook should modify content, got %q", evs[0].ToolResult.Content)
	}
}

func TestExecuteToolCallsAllTerminate(t *testing.T) {
	// Scenario 5: All tools return terminate=true → allTerminate=true.
	reg := testRegistry()
	reg.RegisterInvoker("a", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		result := textToolResult("done")
		result["terminate"] = true
		return result, nil
	})
	reg.RegisterInvoker("b", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		result := textToolResult("done")
		result["terminate"] = true
		return result, nil
	})

	te := NewToolExecutor(reg)
	_, _, allTerm := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "a", nil), tc("c2", "b", nil)}, "")

	if !allTerm {
		t.Error("allTerminate should be true when all tools set terminate")
	}
}

func TestExecuteToolCallsPartialTerminate(t *testing.T) {
	// Scenario 6: Only some tools terminate → allTerminate=false.
	reg := testRegistry()
	reg.RegisterInvoker("a", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		result := textToolResult("t")
		result["terminate"] = true
		return result, nil
	})
	reg.RegisterInvoker("b", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return textToolResult("nt"), nil
	})

	te := NewToolExecutor(reg)
	_, _, allTerm := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "a", nil), tc("c2", "b", nil)}, "")

	if allTerm {
		t.Error("allTerminate should be false when not all tools terminate")
	}
}

func TestExecuteToolCallsEmpty(t *testing.T) {
	// Scenario 7: No tool calls → empty results.
	te := NewToolExecutor(testRegistry())
	evs, msgs, allTerm := te.ExecuteToolCalls(context.Background(), nil, nil, "")

	if len(evs) != 0 {
		t.Errorf("expected 0 events, got %d", len(evs))
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages (nil in), got %d", len(msgs))
	}
	if allTerm {
		t.Error("allTerminate should be false for empty calls")
	}
}

func TestExecuteToolCallsInvokeError(t *testing.T) {
	// Tool invocation error: the failing call still yields exactly one error result,
	// so tool_calls and tool messages stay paired.
	reg := testRegistry()
	reg.RegisterInvoker("failer", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return nil, errors.New("boom")
	})
	reg.RegisterInvoker("ok", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return textToolResult("ok"), nil
	})

	te := NewToolExecutor(reg)
	evs, msgs, _ := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "failer", nil), tc("c2", "ok", nil)}, "")

	if len(evs) != 2 {
		t.Fatalf("expected 2 events, got %d", len(evs))
	}
	if evs[0].ToolResult.CallID != "c1" {
		t.Errorf("first event CallID = %q, want c1", evs[0].ToolResult.CallID)
	}
	if evs[0].ToolResult.Status != llm.ToolStatusError {
		t.Errorf("failing call status = %q, want %q", evs[0].ToolResult.Status, llm.ToolStatusError)
	}
	if evs[0].ToolResult.ErrorCode != llm.ToolErrorExecutionFailed {
		t.Errorf("failing call error code = %q, want %q", evs[0].ToolResult.ErrorCode, llm.ToolErrorExecutionFailed)
	}
	if evs[1].ToolResult.CallID != "c2" {
		t.Errorf("second event CallID = %q, want c2", evs[1].ToolResult.CallID)
	}
	if evs[1].ToolResult.Status != llm.ToolStatusOK {
		t.Errorf("successful call status = %q, want %q", evs[1].ToolResult.Status, llm.ToolStatusOK)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (assistant + 2 tool results), got %d", len(msgs))
	}
}

func TestExecuteToolCallsNonFunctionType(t *testing.T) {
	// Non-"function" type tool calls are not executed but still yield one error result.
	reg := testRegistry()
	var invoked bool
	reg.RegisterInvoker("echo", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		invoked = true
		return textToolResult("ok"), nil
	})

	te := NewToolExecutor(reg)
	calls := []llm.ToolCall{
		{ID: "c1", Type: "retrieval", Function: llm.ToolCallFunc{Name: "echo", Arguments: []byte("{}")}},
	}
	evs, msgs, _ := te.ExecuteToolCalls(context.Background(), nil, calls, "")

	if invoked {
		t.Error("non-function tool should be skipped")
	}
	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if evs[0].ToolResult.ErrorCode != llm.ToolErrorUnsupportedType {
		t.Errorf("error code = %q, want %q", evs[0].ToolResult.ErrorCode, llm.ToolErrorUnsupportedType)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (assistant + 1 tool result), got %d", len(msgs))
	}
}

// moved from pkg/web/api/handle_convo_test.go
func TestFormatToolResult(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected string
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: "",
		},
		{
			name:     "empty map",
			input:    map[string]any{},
			expected: "{}",
		},
		{
			name: "normal map",
			input: map[string]any{
				"result": "success",
				"count":  1,
			},
			expected: `{"count":1,"result":"success"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatToolResult(tt.input)
			if result != tt.expected {
				t.Errorf("formatToolResult() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// setToolResultMaxChars 覆盖配置上限，测试结束恢复
func setToolResultMaxChars(t *testing.T, n int) {
	t.Helper()
	orig := settings.Current.ToolResultMaxChars
	settings.Current.ToolResultMaxChars = n
	t.Cleanup(func() { settings.Current.ToolResultMaxChars = orig })
}

// formatToolResult 将工具结果转换为文本字符串，并按配置上限截断
func formatToolResult(result map[string]any) string {
	return truncateToolResult(toolResultText(result))
}

func textToolResult(text string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"text": text}}}
}

func TestFormatToolResultTruncation(t *testing.T) {
	t.Run("within limit kept byte-identical", func(t *testing.T) {
		setToolResultMaxChars(t, 100)
		text := "短结果，原样返回"
		if got := formatToolResult(textToolResult(text)); got != text {
			t.Errorf("got %q, want %q", got, text)
		}
	})

	t.Run("exactly at limit kept", func(t *testing.T) {
		setToolResultMaxChars(t, 5)
		text := "12345"
		if got := formatToolResult(textToolResult(text)); got != text {
			t.Errorf("got %q, want %q", got, text)
		}
	})

	t.Run("over limit keeps head and tail within limit", func(t *testing.T) {
		setToolResultMaxChars(t, 120)
		text := strings.Repeat("头", 200) + strings.Repeat("尾", 200)
		got := formatToolResult(textToolResult(text))
		if !strings.HasPrefix(got, "头") {
			t.Errorf("head missing: %q", got)
		}
		if !strings.HasSuffix(got, "尾") {
			t.Errorf("tail missing: %q", got)
		}
		assertTruncated(t, got, 120)
	})

	t.Run("cjk truncated on rune boundary", func(t *testing.T) {
		setToolResultMaxChars(t, 60)
		got := formatToolResult(textToolResult(strings.Repeat("中文测试内容超出", 20)))
		if !strings.HasPrefix(got, "中文") {
			t.Errorf("head = %q, want prefix 中文", got)
		}
		assertTruncated(t, got, 60)
	})

	t.Run("zero limit keeps long content", func(t *testing.T) {
		setToolResultMaxChars(t, 0)
		text := strings.Repeat("字", 100)
		if got := formatToolResult(textToolResult(text)); got != text {
			t.Errorf("len(got) = %d, want %d", len(got), len(text))
		}
	})

	t.Run("nil and empty results", func(t *testing.T) {
		setToolResultMaxChars(t, 5)
		if got := formatToolResult(nil); got != "" {
			t.Errorf("nil = %q, want empty", got)
		}
		if got := formatToolResult(map[string]any{}); got != "{}" {
			t.Errorf("empty map = %q, want {}", got)
		}
	})

	t.Run("structuredContent capped", func(t *testing.T) {
		setToolResultMaxChars(t, 60)
		got := formatToolResult(map[string]any{"structuredContent": strings.Repeat("中文测试内容超出", 20)})
		if !strings.HasPrefix(got, "中文") {
			t.Errorf("structuredContent not capped: %q", got)
		}
		assertTruncated(t, got, 60)
	})

	t.Run("json fallback capped", func(t *testing.T) {
		setToolResultMaxChars(t, 60)
		got := formatToolResult(map[string]any{"blob": strings.Repeat("x", 50)})
		if !strings.HasPrefix(got, `{"blo`) {
			t.Errorf("json fallback not capped: %q", got)
		}
		assertTruncated(t, got, 60)
	})
}

func TestExecuteToolCallsTruncatesToolResult(t *testing.T) {
	setToolResultMaxChars(t, 120)
	reg := testRegistry()
	reg.RegisterInvoker("big", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		result := textToolResult(strings.Repeat("字", 400))
		result["terminate"] = true
		return result, nil
	})

	te := NewToolExecutor(reg)
	evs, msgs, allTerm := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "big", nil)}, "")

	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if !strings.Contains(evs[0].ToolResult.Content, "[truncated:") {
		t.Errorf("event content not truncated: %q", evs[0].ToolResult.Content)
	}
	if n := utf8.RuneCountInString(evs[0].ToolResult.Content); n > 120 {
		t.Errorf("truncated content length = %d runes, want <= 120", n)
	}
	if !allTerm || !evs[0].ToolResult.Terminate {
		t.Error("terminate semantics should be preserved")
	}
	toolMsg := msgs[len(msgs)-1]
	if toolMsg.Role != llm.RoleTool {
		t.Fatalf("last message role = %q, want tool", toolMsg.Role)
	}
	if !strings.Contains(toolMsg.Content, "[truncated:") {
		t.Errorf("tool message not truncated: %q", toolMsg.Content)
	}
}

// assertTruncated 断言结果带省略说明、总长不超上限，且未截断在破损的 rune 边界上。
func assertTruncated(t *testing.T, got string, limit int) {
	t.Helper()
	if !strings.Contains(got, "[truncated:") {
		t.Errorf("missing truncation notice: %q", got)
	}
	if n := utf8.RuneCountInString(got); n > limit {
		t.Errorf("truncated result length = %d runes, want <= %d", n, limit)
	}
	for _, r := range got {
		if r == utf8.RuneError {
			t.Errorf("truncated at a broken rune boundary: %q", got)
		}
	}
}

func TestExecuteToolCallsInvalidArguments(t *testing.T) {
	// Malformed JSON arguments yield an invalid_arguments result instead of nothing.
	reg := testRegistry()
	reg.RegisterInvoker("echo", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return textToolResult("ok"), nil
	})

	te := NewToolExecutor(reg)
	calls := []llm.ToolCall{
		{ID: "c1", Type: "function", Function: llm.ToolCallFunc{Name: "echo", Arguments: []byte("{not json")}},
	}
	evs, msgs, _ := te.ExecuteToolCalls(context.Background(), nil, calls, "")

	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if evs[0].ToolResult.ErrorCode != llm.ToolErrorInvalidArguments {
		t.Errorf("error code = %q, want %q", evs[0].ToolResult.ErrorCode, llm.ToolErrorInvalidArguments)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (assistant + 1 tool result), got %d", len(msgs))
	}
}

func TestExecuteToolCallsPanic(t *testing.T) {
	// A panicking tool yields a panic result; the other calls in the batch are unaffected.
	reg := testRegistry()
	reg.RegisterInvoker("boom", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		panic("kaboom")
	})
	reg.RegisterInvoker("ok", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return textToolResult("ok"), nil
	})

	te := NewToolExecutor(reg)
	evs, msgs, _ := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "boom", nil), tc("c2", "ok", nil)}, "")

	if len(evs) != 2 {
		t.Fatalf("expected 2 events, got %d", len(evs))
	}
	if evs[0].ToolResult.ErrorCode != llm.ToolErrorPanic {
		t.Errorf("error code = %q, want %q", evs[0].ToolResult.ErrorCode, llm.ToolErrorPanic)
	}
	if evs[1].ToolResult.Status != llm.ToolStatusOK {
		t.Errorf("second call status = %q, want %q", evs[1].ToolResult.Status, llm.ToolStatusOK)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (assistant + 2 tool results), got %d", len(msgs))
	}
}

func TestExecuteToolCallsToolSelfReportedError(t *testing.T) {
	// A tool reporting isError=true is a business error, not a successful call.
	reg := testRegistry()
	reg.RegisterInvoker("denied", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return mcps.BuildToolErrorResult("permission denied"), nil
	})

	te := NewToolExecutor(reg)
	evs, _, _ := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "denied", nil)}, "")

	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if evs[0].ToolResult.Status != llm.ToolStatusError {
		t.Errorf("status = %q, want %q", evs[0].ToolResult.Status, llm.ToolStatusError)
	}
	if evs[0].ToolResult.ErrorCode != llm.ToolErrorToolError {
		t.Errorf("error code = %q, want %q", evs[0].ToolResult.ErrorCode, llm.ToolErrorToolError)
	}
	if evs[0].ToolResult.Content != "permission denied" {
		t.Errorf("content = %q, want %q", evs[0].ToolResult.Content, "permission denied")
	}
}

func TestExecuteToolCallsResultOrderMatchesCallOrder(t *testing.T) {
	// Tool messages must appear in tool_calls order, one per call.
	reg := testRegistry()
	reg.RegisterInvoker("slow", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		time.Sleep(50 * time.Millisecond)
		return textToolResult("slow"), nil
	})
	reg.RegisterInvoker("fast", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return textToolResult("fast"), nil
	})

	te := NewToolExecutor(reg)
	calls := []llm.ToolCall{tc("c1", "slow", nil), tc("c2", "fast", nil)}
	evs, msgs, _ := te.ExecuteToolCalls(context.Background(), nil, calls, "")

	if len(evs) != 2 {
		t.Fatalf("expected 2 events, got %d", len(evs))
	}
	for i, want := range []string{"c1", "c2"} {
		if evs[i].ToolResult.CallID != want {
			t.Errorf("event %d CallID = %q, want %q", i, evs[i].ToolResult.CallID, want)
		}
	}
	toolMsgs := msgs[1:]
	if len(toolMsgs) != len(calls) {
		t.Fatalf("expected %d tool messages, got %d", len(calls), len(toolMsgs))
	}
	for i, want := range []string{"c1", "c2"} {
		if toolMsgs[i].ToolCallID != want {
			t.Errorf("tool message %d ToolCallID = %q, want %q", i, toolMsgs[i].ToolCallID, want)
		}
	}
}

func TestToolResultTextExtractsContentText(t *testing.T) {
	// mcps 构造的 content 是 []map[string]any，JSON 反序列化的结果是 []any，两者都要能取出 text。
	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{
			name:  "mcps content slice",
			input: map[string]any{"content": []map[string]any{{"type": "text", "text": "hello"}}},
			want:  "hello",
		},
		{
			name:  "decoded content slice",
			input: map[string]any{"content": []any{map[string]any{"type": "text", "text": "hello"}}},
			want:  "hello",
		},
		{
			name:  "mcps error result",
			input: mcps.BuildToolErrorResult("permission denied"),
			want:  "permission denied",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolResultText(tt.input); got != tt.want {
				t.Errorf("toolResultText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExecuteToolCallsErrorResultRespectsLimit(t *testing.T) {
	// 错误文本同样受 ToolResultMaxChars 约束。
	setToolResultMaxChars(t, 120)
	reg := testRegistry()
	reg.RegisterInvoker("failer", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return nil, errors.New(strings.Repeat("x", 500))
	})

	te := NewToolExecutor(reg)
	evs, _, _ := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "failer", nil)}, "")

	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if n := utf8.RuneCountInString(evs[0].ToolResult.Content); n > 120 {
		t.Errorf("error content length = %d runes, want <= 120", n)
	}
}

func TestTruncateToolResultRespectsLimit(t *testing.T) {
	// 无论上限大小，截断结果（含省略说明）都不得超过上限。
	text := strings.Repeat("头", 300) + strings.Repeat("尾", 300)
	for _, limit := range []int{5, 10, 20, 40, 60, 100, 200, 20000} {
		setToolResultMaxChars(t, limit)
		got := truncateToolResult(text)
		if n := utf8.RuneCountInString(got); n > limit {
			t.Errorf("limit %d: length = %d runes, want <= %d", limit, n, limit)
		}
	}
}
