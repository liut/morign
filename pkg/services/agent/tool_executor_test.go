package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

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
		return map[string]any{"content": []any{map[string]any{"text": "alpha-result"}}}, nil
	})
	reg.RegisterInvoker("beta", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		orderMu.Lock()
		order = append(order, "beta")
		orderMu.Unlock()
		<-ready // wait for alpha to also start (proves concurrency)
		return map[string]any{"content": []any{map[string]any{"text": "beta-result"}}}, nil
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
		return map[string]any{"content": []any{map[string]any{"text": "ok"}}}, nil
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
		return map[string]any{"content": []any{map[string]any{"text": "ok"}}}, nil
	})

	te := NewToolExecutor(reg)
	te.BeforeToolCall = func(ctx context.Context, name string, params map[string]any) (bool, string) {
		return true, "blocked by policy"
	}

	evs, _, _ := te.ExecuteToolCalls(context.Background(), nil,
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
	if evs[0].ToolResult.Terminate {
		t.Error("blocked tool should NOT set Terminate=true by default")
	}
}

func TestExecuteToolCallsAfterHook(t *testing.T) {
	// Scenario 3: AfterToolCall modifies the result.
	reg := testRegistry()
	reg.RegisterInvoker("echo", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return map[string]any{"content": []any{map[string]any{"text": "original"}}}, nil
	})

	te := NewToolExecutor(reg)
	te.AfterToolCall = func(ctx context.Context, name string, result map[string]any) map[string]any {
		result["content"] = []any{map[string]any{"text": "modified"}}
		return result
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
		return map[string]any{"content": []any{map[string]any{"text": "done"}}, "terminate": true}, nil
	})
	reg.RegisterInvoker("b", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return map[string]any{"content": []any{map[string]any{"text": "done"}}, "terminate": true}, nil
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
		return map[string]any{"content": []any{map[string]any{"text": "t"}}, "terminate": true}, nil
	})
	reg.RegisterInvoker("b", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return map[string]any{"content": []any{map[string]any{"text": "nt"}}}, nil
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
	// Tool invocation error: tool call returns error → no event for that tool.
	reg := testRegistry()
	reg.RegisterInvoker("failer", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return nil, errors.New("boom")
	})
	reg.RegisterInvoker("ok", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return map[string]any{"content": []any{map[string]any{"text": "ok"}}}, nil
	})

	te := NewToolExecutor(reg)
	evs, _, _ := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "failer", nil), tc("c2", "ok", nil)}, "")

	// Only "ok" succeeds; failer produces no event
	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if evs[0].ToolResult.CallID != "c2" {
		t.Errorf("event CallID = %q, want c2", evs[0].ToolResult.CallID)
	}
}

func TestExecuteToolCallsNonFunctionType(t *testing.T) {
	// Non-"function" type tool calls are skipped.
	reg := testRegistry()
	var invoked bool
	reg.RegisterInvoker("echo", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		invoked = true
		return map[string]any{"content": []any{map[string]any{"text": "ok"}}}, nil
	})

	te := NewToolExecutor(reg)
	calls := []llm.ToolCall{
		{ID: "c1", Type: "retrieval", Function: llm.ToolCallFunc{Name: "echo", Arguments: []byte("{}")}},
	}
	evs, _, _ := te.ExecuteToolCalls(context.Background(), nil, calls, "")

	if invoked {
		t.Error("non-function tool should be skipped")
	}
	if len(evs) != 0 {
		t.Errorf("expected 0 events, got %d", len(evs))
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

func textToolResult(text string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"text": text}}}
}

func TestFormatToolResultTruncation(t *testing.T) {
	const notice = "[truncated: showing the first 5 of 8 characters"

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

	t.Run("cjk truncated on rune boundary", func(t *testing.T) {
		setToolResultMaxChars(t, 5)
		got := formatToolResult(textToolResult("中文测试内容超出"))
		if !strings.HasPrefix(got, "中文测试内") {
			t.Errorf("head = %q, want prefix 中文测试内", got)
		}
		if !strings.Contains(got, notice) {
			t.Errorf("missing truncation notice: %q", got)
		}
		if !utf8.ValidString(got) {
			t.Errorf("result is not valid utf-8: %q", got)
		}
		if r := []rune(got); r[5] == utf8.RuneError {
			t.Errorf("truncated at a broken rune boundary: %q", got)
		}
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
		setToolResultMaxChars(t, 5)
		got := formatToolResult(map[string]any{"structuredContent": "中文测试内容超出"})
		if !strings.HasPrefix(got, "中文测试内") || !strings.Contains(got, notice) {
			t.Errorf("structuredContent not capped: %q", got)
		}
	})

	t.Run("json fallback capped", func(t *testing.T) {
		setToolResultMaxChars(t, 5)
		got := formatToolResult(map[string]any{"blob": strings.Repeat("x", 50)})
		if !strings.HasPrefix(got, `{"blo`) || !strings.Contains(got, "of ") {
			t.Errorf("json fallback not capped: %q", got)
		}
	})
}

func TestExecuteToolCallsTruncatesToolResult(t *testing.T) {
	setToolResultMaxChars(t, 20)
	reg := testRegistry()
	reg.RegisterInvoker("big", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		result := textToolResult(strings.Repeat("字", 100))
		result["terminate"] = true
		return result, nil
	})

	te := NewToolExecutor(reg)
	evs, msgs, allTerm := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "big", nil)}, "")

	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if !strings.Contains(evs[0].ToolResult.Content, "showing the first 20 of 100 characters") {
		t.Errorf("event content not truncated: %q", evs[0].ToolResult.Content)
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
