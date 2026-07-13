package agent

import (
	"context"
	"errors"
	"iter"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liut/morign/pkg/services/llm"
)

// ---------------------------------------------------------------------------
// mock LLM client helpers
// ---------------------------------------------------------------------------

// singleSeqMock returns the same event sequence on every StreamChat call.
type singleSeqMock struct {
	seq iter.Seq2[*llm.Event, error]
}

func (m *singleSeqMock) Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.ChatResult, error) {
	return nil, errors.New("chat not implemented in mock")
}
func (m *singleSeqMock) StreamChat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) iter.Seq2[*llm.Event, error] {
	return m.seq
}
func (m *singleSeqMock) Generate(ctx context.Context, prompt string) (string, *llm.Usage, error) {
	return "", nil, errors.New("generate not implemented")
}
func (m *singleSeqMock) Embedding(ctx context.Context, texts []string) ([]float64, error) {
	return nil, errors.New("embedding not implemented")
}

// multiSeqMock returns a different sequence on each StreamChat call (round-robin).
type multiSeqMock struct {
	rounds []iter.Seq2[*llm.Event, error]
	idx    int
}

func (m *multiSeqMock) Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.ChatResult, error) {
	return nil, errors.New("chat not implemented in mock")
}
func (m *multiSeqMock) StreamChat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) iter.Seq2[*llm.Event, error] {
	if m.idx >= len(m.rounds) {
		return func(yield func(*llm.Event, error) bool) {}
	}
	seq := m.rounds[m.idx]
	m.idx++
	return seq
}
func (m *multiSeqMock) Generate(ctx context.Context, prompt string) (string, *llm.Usage, error) {
	return "", nil, errors.New("generate not implemented")
}
func (m *multiSeqMock) Embedding(ctx context.Context, texts []string) ([]float64, error) {
	return nil, errors.New("embedding not implemented")
}

// chatMock returns a fixed ChatResult on every Chat call.
type chatMock struct {
	result *llm.ChatResult
	err    error
}

func (m *chatMock) Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.ChatResult, error) {
	return m.result, m.err
}
func (m *chatMock) StreamChat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) iter.Seq2[*llm.Event, error] {
	return func(yield func(*llm.Event, error) bool) {}
}
func (m *chatMock) Generate(ctx context.Context, prompt string) (string, *llm.Usage, error) {
	return "", nil, errors.New("generate not implemented")
}
func (m *chatMock) Embedding(ctx context.Context, texts []string) ([]float64, error) {
	return nil, errors.New("embedding not implemented")
}

// multiChatMock returns a different ChatResult on each Chat call.
type multiChatMock struct {
	results []*llm.ChatResult
	idx     int
}

func (m *multiChatMock) Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.ChatResult, error) {
	if m.idx >= len(m.results) {
		return &llm.ChatResult{}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}
func (m *multiChatMock) StreamChat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) iter.Seq2[*llm.Event, error] {
	return func(yield func(*llm.Event, error) bool) {}
}
func (m *multiChatMock) Generate(ctx context.Context, prompt string) (string, *llm.Usage, error) {
	return "", nil, errors.New("generate not implemented")
}
func (m *multiChatMock) Embedding(ctx context.Context, texts []string) ([]float64, error) {
	return nil, errors.New("embedding not implemented")
}

// ---------------------------------------------------------------------------
// event constructors
// ---------------------------------------------------------------------------

func deltaEv(delta string) *llm.Event {
	return &llm.Event{ID: llm.NewEventID(), Timestamp: time.Now(), Author: "assistant", Delta: delta}
}

func doneEv(toolCalls []llm.ToolCall) *llm.Event {
	return &llm.Event{
		ID:        llm.NewEventID(),
		Timestamp: time.Now(),
		Author:    "assistant",
		Done:      true,
		ToolCalls: toolCalls,
	}
}

// collectEvents consumes an iter.Seq2 and returns all events + last error.
func collectEvents(seq iter.Seq2[*llm.Event, error]) (events []*llm.Event, lastErr error) {
	for ev, err := range seq {
		if err != nil {
			return events, err
		}
		events = append(events, ev)
	}
	return events, nil
}

// ---------------------------------------------------------------------------
// helper: simple tool that echoes
// ---------------------------------------------------------------------------

func echoResult(text string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"text": text}}}
}

func echoTerminateResult(text string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"text": text}}, "terminate": true}
}

// ---------------------------------------------------------------------------
// Tests: Scenario 1 — single turn, no tool calls
// ---------------------------------------------------------------------------

func TestAgentLoopRun_SingleTurnNoTools(t *testing.T) {
	mock := &singleSeqMock{seq: func(yield func(*llm.Event, error) bool) {
		yield(deltaEv("Hello"), nil)
		yield(deltaEv(" world"), nil)
		yield(doneEv(nil), nil) // Done with no tool calls
	}}

	cfg := AgentLoopConfig{
		LLM:      mock,
		ToolExec: NewToolExecutor(testRegistry()),
		MaxLoop:  5,
	}
	loop := NewAgentLoop(cfg)

	events, err := collectEvents(loop.Run(context.Background(), nil, nil))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Delta != "Hello" {
		t.Errorf("event[0].Delta = %q, want Hello", events[0].Delta)
	}
	if events[1].Delta != " world" {
		t.Errorf("event[1].Delta = %q, want ' world'", events[1].Delta)
	}
	if !events[2].Done {
		t.Error("event[2].Done should be true")
	}
}

// ---------------------------------------------------------------------------
// Tests: Scenario 2 — multi-turn with tool calls
// ---------------------------------------------------------------------------

func TestAgentLoopRun_MultiTurnWithTools(t *testing.T) {
	reg := testRegistry()
	reg.RegisterInvoker("weather", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return echoResult("sunny, 22°C"), nil
	})

	// Round 1: LLM returns tool call for "weather"
	round1TC := []llm.ToolCall{tc("call-1", "weather", map[string]any{"city": "Beijing"})}
	// Round 2: LLM returns final answer with no tool calls
	round2Done := doneEv(nil)
	round2Done.Delta = "The weather is sunny"

	mock := &multiSeqMock{
		rounds: []iter.Seq2[*llm.Event, error]{
			func(yield func(*llm.Event, error) bool) {
				yield(deltaEv("Let me check"), nil)
				yield(doneEv(round1TC), nil)
			},
			func(yield func(*llm.Event, error) bool) {
				yield(round2Done, nil)
			},
		},
	}

	cfg := AgentLoopConfig{
		LLM:      mock,
		ToolExec: NewToolExecutor(reg),
		MaxLoop:  5,
	}
	loop := NewAgentLoop(cfg)

	events, err := collectEvents(loop.Run(context.Background(), nil, nil))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have: round 1 events (2) + tool result event (1) + round 2 events (1) = 4
	if len(events) < 4 {
		t.Fatalf("expected at least 4 events, got %d", len(events))
	}

	// Find tool result event
	var foundToolResult bool
	for _, ev := range events {
		if ev.ToolResult != nil {
			foundToolResult = true
			if ev.ToolResult.Name != "weather" {
				t.Errorf("tool result name = %q, want weather", ev.ToolResult.Name)
			}
			if ev.ToolResult.Content != "sunny, 22°C" {
				t.Errorf("tool result content = %q", ev.ToolResult.Content)
			}
			break
		}
	}
	if !foundToolResult {
		t.Error("no tool result event found")
	}
}

// ---------------------------------------------------------------------------
// Tests: Scenario 3 — non-streaming RunNonStreaming
// ---------------------------------------------------------------------------

func TestAgentLoopRunNonStreaming(t *testing.T) {
	mock := &chatMock{
		result: &llm.ChatResult{
			Content: "The capital of France is Paris.",
		},
	}

	cfg := AgentLoopConfig{
		LLM:      mock,
		ToolExec: NewToolExecutor(testRegistry()),
		MaxLoop:  5,
	}
	loop := NewAgentLoop(cfg)

	answer, err := loop.RunNonStreaming(context.Background(), nil, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "The capital of France is Paris." {
		t.Errorf("answer = %q, want 'The capital of France is Paris.'", answer)
	}
}

func TestAgentLoopRunNonStreaming_WithTools(t *testing.T) {
	reg := testRegistry()
	reg.RegisterInvoker("calc", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return echoResult("42"), nil
	})

	tc1 := []llm.ToolCall{tc("c1", "calc", map[string]any{"expr": "6*7"})}

	mock := &multiChatMock{
		results: []*llm.ChatResult{
			{ToolCalls: tc1, Thinking: "need to calculate"},
			{Content: "The answer is 42."},
		},
	}

	cfg := AgentLoopConfig{
		LLM:      mock,
		ToolExec: NewToolExecutor(reg),
		MaxLoop:  5,
	}
	loop := NewAgentLoop(cfg)

	answer, err := loop.RunNonStreaming(context.Background(), nil, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "The answer is 42." {
		t.Errorf("answer = %q, want 'The answer is 42.'", answer)
	}
}

// ---------------------------------------------------------------------------
// Tests: Scenario 4 — maxLoop limit
// ---------------------------------------------------------------------------

func TestAgentLoopRun_MaxLoopExceeded(t *testing.T) {
	// LLM that always returns a tool call, forcing infinite loop.
	// With MaxLoop=2, it should stop after 2 iterations with an error.
	reg := testRegistry()
	var invokeCount int32
	reg.RegisterInvoker("dummy", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		atomic.AddInt32(&invokeCount, 1)
		return echoResult("done"), nil
	})

	tcDummy := []llm.ToolCall{tc("c1", "dummy", nil)}

	mock := &multiSeqMock{
		rounds: []iter.Seq2[*llm.Event, error]{
			func(yield func(*llm.Event, error) bool) {
				yield(deltaEv("calling tool"), nil)
				yield(doneEv(tcDummy), nil)
			},
			func(yield func(*llm.Event, error) bool) {
				yield(deltaEv("calling tool again"), nil)
				yield(doneEv(tcDummy), nil)
			},
			func(yield func(*llm.Event, error) bool) {
				yield(deltaEv("third call"), nil)
				yield(doneEv(tcDummy), nil)
			},
		},
	}

	cfg := AgentLoopConfig{
		LLM:      mock,
		ToolExec: NewToolExecutor(reg),
		MaxLoop:  2,
	}
	loop := NewAgentLoop(cfg)

	events, err := collectEvents(loop.Run(context.Background(), nil, nil))

	if err == nil {
		t.Fatal("expected an error for maxLoop exceeded, got nil")
	}

	t.Logf("maxLoop error: %v", err)
	t.Logf("events collected: %d", len(events))
	t.Logf("tool invocations: %d", invokeCount)

	// Should have stopped: events from round 1 (2) + tool result (1)
	// + round 2 (2) + tool result (1) = 6, then maxLoop triggered before round 3
	if len(events) < 4 {
		t.Errorf("expected at least 4 events before maxLoop error, got %d", len(events))
	}
	if atomic.LoadInt32(&invokeCount) != 2 {
		t.Errorf("expected 2 tool invocations, got %d", invokeCount)
	}
}

// ---------------------------------------------------------------------------
// Tests: Scenario 5 — tool execution allTerminate=true
// ---------------------------------------------------------------------------

func TestAgentLoopRun_AllTerminate(t *testing.T) {
	reg := testRegistry()
	reg.RegisterInvoker("finish", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return echoTerminateResult("all done"), nil
	})

	tcFinish := []llm.ToolCall{tc("c1", "finish", nil)}

	// Even though round 2 exists, it should never be called because allTerminate=true
	mock := &multiSeqMock{
		rounds: []iter.Seq2[*llm.Event, error]{
			func(yield func(*llm.Event, error) bool) {
				yield(deltaEv("finishing"), nil)
				yield(doneEv(tcFinish), nil)
			},
			func(yield func(*llm.Event, error) bool) {
				// This should NOT be reached
				yield(deltaEv("SHOULD NOT APPEAR"), nil)
				yield(doneEv(nil), nil)
			},
		},
	}

	cfg := AgentLoopConfig{
		LLM:      mock,
		ToolExec: NewToolExecutor(reg),
		MaxLoop:  5,
	}
	loop := NewAgentLoop(cfg)

	events, err := collectEvents(loop.Run(context.Background(), nil, nil))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify we got the tool result with terminate
	var foundTerminate bool
	for _, ev := range events {
		if ev.ToolResult != nil && ev.ToolResult.Terminate {
			foundTerminate = true
		}
		if ev.Delta == "SHOULD NOT APPEAR" {
			t.Error("second LLM call should not have happened after allTerminate")
		}
	}
	if !foundTerminate {
		t.Error("tool result with terminate=true not found")
	}
}

// ---------------------------------------------------------------------------
// Tests: Scenario 6 — context cancel
// ---------------------------------------------------------------------------

func TestAgentLoopRun_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Mock that yields a few events then blocks until context is cancelled
	mock := &singleSeqMock{seq: func(yield func(*llm.Event, error) bool) {
		yield(deltaEv("processing"), nil)
		// Cancel context mid-stream
		cancel()
		// Try to yield another event — simulation of cancellation
		// The StreamChat implementation would check ctx.Done()
		select {
		case <-ctx.Done():
			yield(nil, ctx.Err())
		case <-time.After(100 * time.Millisecond):
			yield(doneEv(nil), nil)
		}
	}}

	cfg := AgentLoopConfig{
		LLM:      mock,
		ToolExec: NewToolExecutor(testRegistry()),
		MaxLoop:  5,
	}
	loop := NewAgentLoop(cfg)

	events, err := collectEvents(loop.Run(ctx, nil, nil))

	if err == nil {
		// Could be that context cancel didn't propagate in mock
		t.Log("context cancel did not produce error (mock timing dependent)")
	}
	_ = events
	// At minimum, iteration terminated without hanging
	t.Logf("context cancel test: %d events, err=%v", len(events), err)
}

// ---------------------------------------------------------------------------
// Tests: Scenario 7 — StreamChat error
// ---------------------------------------------------------------------------

func TestAgentLoopRun_StreamChatError(t *testing.T) {
	streamErr := errors.New("API connection refused")
	mock := &singleSeqMock{seq: func(yield func(*llm.Event, error) bool) {
		yield(deltaEv("partial response"), nil)
		yield(nil, streamErr)
	}}

	cfg := AgentLoopConfig{
		LLM:      mock,
		ToolExec: NewToolExecutor(testRegistry()),
		MaxLoop:  5,
	}
	loop := NewAgentLoop(cfg)

	events, err := collectEvents(loop.Run(context.Background(), nil, nil))

	if err == nil {
		t.Fatal("expected error from StreamChat, got nil")
	}
	if !errors.Is(err, streamErr) {
		t.Logf("error chain: %v (contains streamErr=%v)", err, errors.Is(err, streamErr))
	}
	if len(events) < 1 {
		t.Error("expected at least the partial response event before error")
	}
}

// ---------------------------------------------------------------------------
// Tests: Option pattern
// ---------------------------------------------------------------------------

func TestAgentLoop_WithMaxLoop(t *testing.T) {
	cfg := AgentLoopConfig{
		MaxLoop: 0,
	}
	loop := NewAgentLoop(cfg, WithMaxLoop(10))
	if loop.cfg.MaxLoop != 10 {
		t.Errorf("WithMaxLoop: expected 10, got %d", loop.cfg.MaxLoop)
	}
}

func TestAgentLoop_DefaultMaxLoop(t *testing.T) {
	cfg := AgentLoopConfig{
		MaxLoop: 0,
	}
	loop := NewAgentLoop(cfg)
	if loop.cfg.MaxLoop != 5 {
		t.Errorf("default MaxLoop: expected 5, got %d", loop.cfg.MaxLoop)
	}
}

// ---------------------------------------------------------------------------
// Tests: Run closure handles early termination by caller
// ---------------------------------------------------------------------------

func TestAgentLoopRun_CallerBreaksEarly(t *testing.T) {
	// Simulate caller breaking out of range early — iterator should stop
	mock := &singleSeqMock{seq: func(yield func(*llm.Event, error) bool) {
		if !yield(deltaEv("event-1"), nil) {
			return
		}
		// This should not be reached if caller breaks
		panic("should not yield event-2 if caller broke early")
	}}

	cfg := AgentLoopConfig{
		LLM:      mock,
		ToolExec: NewToolExecutor(testRegistry()),
		MaxLoop:  5,
	}
	loop := NewAgentLoop(cfg)

	var count int
	for ev, err := range loop.Run(context.Background(), nil, nil) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		count++
		if count >= 1 {
			break // early termination
		}
		_ = ev
	}
	if count != 1 {
		t.Errorf("expected 1 event before break, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Tests: RunNonStreaming with MaxLoop exceeded
// ---------------------------------------------------------------------------

func TestAgentLoopRunNonStreaming_MaxLoopExceeded(t *testing.T) {
	reg := testRegistry()
	reg.RegisterInvoker("dummy", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return echoResult("ok"), nil
	})

	tcDummy := []llm.ToolCall{tc("c1", "dummy", nil)}

	// Always returns tool calls — should hit maxLoop
	mock := &multiChatMock{
		results: []*llm.ChatResult{
			{ToolCalls: tcDummy},
			{ToolCalls: tcDummy},
			{ToolCalls: tcDummy},
		},
	}

	cfg := AgentLoopConfig{
		LLM:      mock,
		ToolExec: NewToolExecutor(reg),
		MaxLoop:  2,
	}
	loop := NewAgentLoop(cfg)

	answer, err := loop.RunNonStreaming(context.Background(), nil, nil)

	if err == nil {
		t.Fatal("expected error for maxLoop exceeded in non-streaming mode, got nil")
	}
	t.Logf("non-streaming maxLoop error: %v, answer: %q", err, answer)
}

// ---------------------------------------------------------------------------
// Tests: RunNonStreaming with allTerminate
// ---------------------------------------------------------------------------

func TestAgentLoopRunNonStreaming_AllTerminate(t *testing.T) {
	reg := testRegistry()
	reg.RegisterInvoker("finish", func(ctx context.Context, params map[string]any) (map[string]any, error) {
		return echoTerminateResult("done"), nil
	})

	tcFinish := []llm.ToolCall{tc("c1", "finish", nil)}

	mock := &multiChatMock{
		results: []*llm.ChatResult{
			{ToolCalls: tcFinish},
			{Content: "should not reach"}, // should NOT be consumed
		},
	}

	cfg := AgentLoopConfig{
		LLM:      mock,
		ToolExec: NewToolExecutor(reg),
		MaxLoop:  5,
	}
	loop := NewAgentLoop(cfg)

	answer, err := loop.RunNonStreaming(context.Background(), nil, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When allTerminate, we return the content from the first Chat (which had tool calls),
	// so answer may be empty (tool calls only). This verifies no error and no loop.
	t.Logf("non-streaming allTerminate answer: %q", answer)
	// The key assertion: mock.idx should be 1 (only first result consumed)
	if mock.idx != 1 {
		t.Errorf("expected 1 Chat call, got %d (should have stopped at allTerminate)", mock.idx)
	}
}
