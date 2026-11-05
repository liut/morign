package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/services/llm"
)

// fakeSpiller 记录外溢落库的入参，返回固定 handle。
type fakeSpiller struct {
	calls  []convo.ToolRecordSpill
	handle string
	err    error
}

func (f *fakeSpiller) SpillToolResult(_ context.Context, in convo.ToolRecordSpill) (string, error) {
	f.calls = append(f.calls, in)
	if f.err != nil {
		return "", f.err
	}
	return f.handle, nil
}

func TestSpillToolResultKeepsFullContent(t *testing.T) {
	setToolResultMaxChars(t, 100)
	full := strings.Repeat("超限内容", 100)

	reg := testRegistry()
	reg.RegisterInvoker("big", func(context.Context, map[string]any) (map[string]any, error) {
		return textToolResult(full), nil
	})
	sp := &fakeSpiller{handle: "ev_handle"}
	te := NewToolExecutor(reg)
	te.Spiller = sp

	evs, _, _ := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "big", map[string]any{"q": "x"})}, "")

	if len(sp.calls) != 1 {
		t.Fatalf("spill calls = %d, want 1", len(sp.calls))
	}
	in := sp.calls[0]
	if in.Content != full {
		t.Errorf("content len = %d, want full %d", len(in.Content), len(full))
	}
	if !in.Spilled {
		t.Error("spilled = false, want true")
	}
	if in.CallID != "c1" {
		t.Errorf("callID = %q, want c1", in.CallID)
	}
	if in.ToolName != "big" {
		t.Errorf("toolName = %q, want big", in.ToolName)
	}
	if !strings.Contains(in.Arguments, `"q"`) {
		t.Errorf("arguments = %q, want raw args", in.Arguments)
	}
	if in.Preview == in.Content {
		t.Error("preview should be the truncated text, not the full content")
	}
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	if !strings.Contains(evs[0].ToolResult.Content, "ev_handle") {
		t.Errorf("model text = %q, want the handle", evs[0].ToolResult.Content)
	}
}

func TestSpillToolResultSkipsShortResult(t *testing.T) {
	setToolResultMaxChars(t, 100)

	reg := testRegistry()
	reg.RegisterInvoker("small", func(context.Context, map[string]any) (map[string]any, error) {
		return textToolResult("短结果"), nil
	})
	sp := &fakeSpiller{handle: "ev_handle"}
	te := NewToolExecutor(reg)
	te.Spiller = sp

	evs, _, _ := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "small", nil)}, "")

	if len(sp.calls) != 0 {
		t.Errorf("spill calls = %d, want 0", len(sp.calls))
	}
	if evs[0].ToolResult.Content != "短结果" {
		t.Errorf("content = %q, want 短结果", evs[0].ToolResult.Content)
	}
}

func TestSpillToolResultDegradesOnStoreFailure(t *testing.T) {
	setToolResultMaxChars(t, 100)

	reg := testRegistry()
	reg.RegisterInvoker("big", func(context.Context, map[string]any) (map[string]any, error) {
		return textToolResult(strings.Repeat("超限内容", 100)), nil
	})
	sp := &fakeSpiller{err: errors.New("db down")}
	te := NewToolExecutor(reg)
	te.Spiller = sp

	evs, _, _ := te.ExecuteToolCalls(context.Background(), nil,
		[]llm.ToolCall{tc("c1", "big", nil)}, "")

	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	if strings.Contains(evs[0].ToolResult.Content, "handle=") {
		t.Errorf("model text should not carry a handle after a store failure: %q", evs[0].ToolResult.Content)
	}
}

func TestExecuteToolCallsAssignsSeqInCallOrder(t *testing.T) {
	reg := testRegistry()
	reg.RegisterInvoker("echo", func(context.Context, map[string]any) (map[string]any, error) {
		return textToolResult("ok"), nil
	})
	te := NewToolExecutor(reg)

	ctx := ContextWithToolSeqCounter(context.Background(), NewToolSeqCounter())
	evs, _, _ := te.ExecuteToolCalls(ctx, nil, []llm.ToolCall{
		tc("c1", "echo", nil),
		tc("c2", "echo", nil),
	}, "")

	if evs[0].ToolResult.Seq != 0 || evs[1].ToolResult.Seq != 1 {
		t.Errorf("seq = %d, %d; want 0, 1", evs[0].ToolResult.Seq, evs[1].ToolResult.Seq)
	}

	// 下一批接着排，序号在轮内单调递增。
	evs2, _, _ := te.ExecuteToolCalls(ctx, nil, []llm.ToolCall{tc("c3", "echo", nil)}, "")
	if evs2[0].ToolResult.Seq != 2 {
		t.Errorf("seq = %d, want 2", evs2[0].ToolResult.Seq)
	}
}

func TestToolSeqCounterAllocatesContiguousRanges(t *testing.T) {
	c := NewToolSeqCounter()
	if got := c.Next(3); got != 0 {
		t.Errorf("first batch = %d, want 0", got)
	}
	if got := c.Next(2); got != 3 {
		t.Errorf("second batch = %d, want 3", got)
	}
}
