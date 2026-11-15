package api

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/liut/morign/pkg/models/skills"
	"github.com/liut/morign/pkg/services/llm"
)

type fakeSkillStore struct {
	byName map[string]*skills.Skill
	recent []skills.Skill
	err    error
}

func (f *fakeSkillStore) TopRecent(ctx context.Context, limit int) (skills.Skills, error) {
	if f.err != nil {
		return nil, f.err
	}
	if limit <= 0 || limit > len(f.recent) {
		limit = len(f.recent)
	}
	return f.recent[:limit], nil
}

func (f *fakeSkillStore) LoadForName(ctx context.Context, name string) (*skills.Skill, error) {
	if f.err != nil {
		return nil, f.err
	}
	obj, ok := f.byName[name]
	if !ok {
		return nil, errSkillNotFound
	}
	return obj, nil
}

var errSkillNotFound = errFakeNotFound{}

type errFakeNotFound struct{}

func (errFakeNotFound) Error() string { return "skill not found" }

func TestConvertToolCallsForJSON(t *testing.T) {
	tests := []struct {
		name  string
		input []llm.ToolCall
	}{
		{
			name:  "nil input",
			input: nil,
		},
		{
			name:  "empty slice",
			input: []llm.ToolCall{},
		},
		{
			name: "single tool call",
			input: []llm.ToolCall{
				{
					ID:   "call_123",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "get_weather",
						Arguments: json.RawMessage(`{"location":"Beijing"}`),
					},
				},
			},
		},
		{
			name: "multiple tool calls",
			input: []llm.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "tool1",
						Arguments: json.RawMessage(`{"a":1}`),
					},
				},
				{
					ID:   "call_2",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "tool2",
						Arguments: json.RawMessage(`{"b":2}`),
					},
				},
			},
		},
		{
			name: "empty arguments",
			input: []llm.ToolCall{
				{
					ID:   "call_empty",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "empty_tool",
						Arguments: json.RawMessage{},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToolCallsForJSON(tt.input)
			// 测试序列化不报错
			jsonBytes, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			// nil 应该序列化为 null
			if len(tt.input) == 0 {
				if string(jsonBytes) != "null" {
					t.Errorf("expected null, got %s", jsonBytes)
				}
			} else {
				// 非空情况，确保是数组
				if jsonBytes[0] != '[' {
					t.Errorf("expected array, got %s", jsonBytes)
				}
			}
		})
	}
}

func TestConvertToolCallsForJSON_Serialize(t *testing.T) {
	// 专门测试序列化不报错
	input := []llm.ToolCall{
		{
			ID:   "call_123",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "get_weather",
				Arguments: json.RawMessage(`{"location":"Beijing"}`),
			},
		},
	}

	result := convertToolCallsForJSON(input)
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// 验证序列化结果可以反序列化回来
	var parsed []map[string]any
	err = json.Unmarshal(jsonBytes, &parsed)
	if err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if len(parsed) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(parsed))
	}

	if parsed[0]["id"] != "call_123" {
		t.Errorf("expected id 'call_123', got %v", parsed[0]["id"])
	}
}

func TestSkillIndexIsMetadataOnly(t *testing.T) {
	sto := newFakePromptStore()
	sto.skill = &fakeSkillStore{byName: map[string]*skills.Skill{
		"a": skillFor("a", "A", "content-a"),
		"b": skillFor("b", "B", "content-b"),
	}}
	got := skillIndex(context.Background(), sto, []string{"a", "b"})
	for _, name := range []string{"a", "b"} {
		if !strings.Contains(got, "- "+name+": ") {
			t.Errorf("index missing %s, got %q", name, got)
		}
		if strings.Contains(got, "content-"+name) {
			t.Errorf("index leaked skill body for %s, got %q", name, got)
		}
	}
}

func TestSkillIndexManySkills(t *testing.T) {
	f := &fakeSkillStore{byName: map[string]*skills.Skill{}}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		f.byName[name] = skillFor(name, "desc-"+name, "content-"+name)
	}
	sto := newFakePromptStore()
	sto.skill = f
	out := skillIndex(context.Background(), sto, []string{"a", "b", "c", "d", "e"})
	if !strings.Contains(out, "desc-a") || strings.Contains(out, "content-a") {
		t.Errorf("expected metadata only, got %q", out)
	}
	if !strings.Contains(out, "skill_read") {
		t.Errorf("metadata should hint skill_read, got %q", out)
	}
}

func TestSkillIndexDegrade(t *testing.T) {
	sto := newFakePromptStore()
	sto.skill = &fakeSkillStore{err: errFakeNotFound{}}
	if got := skillIndex(context.Background(), sto, []string{"a"}); got != "" {
		t.Errorf("store error should degrade to empty, got %q", got)
	}
}

func TestSkillIndexEmpty(t *testing.T) {
	sto := newFakePromptStore()
	sto.skill = &fakeSkillStore{}
	if got := skillIndex(context.Background(), sto, nil); got != "" {
		t.Errorf("no skills should inject nothing, got %q", got)
	}
}

func skillFor(name, desc, content string) *skills.Skill {
	return &skills.Skill{SkillBasic: skills.SkillBasic{Name: name, Description: desc, Content: content}}
}

func TestConvertToolCallsForJSON_IncompleteJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     []llm.ToolCall
		wantPanic bool
	}{
		{
			name: "incomplete JSON - missing closing brace",
			input: []llm.ToolCall{
				{
					ID:   "call_incomplete",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "test",
						Arguments: json.RawMessage(`{"location":`),
					},
				},
			},
			wantPanic: false, // 应该不 panic，只是转成空对象
		},
		{
			name: "partial JSON key",
			input: []llm.ToolCall{
				{
					ID:   "call_partial",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "test",
						Arguments: json.RawMessage(`{"l`),
					},
				},
			},
			wantPanic: false,
		},
		{
			name: "empty raw message",
			input: []llm.ToolCall{
				{
					ID:   "call_empty",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "test",
						Arguments: json.RawMessage(``),
					},
				},
			},
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil && !tt.wantPanic {
					t.Errorf("convertToolCallsForJSON panicked: %v", r)
				}
			}()
			result := convertToolCallsForJSON(tt.input)
			// 验证序列化不报错
			_, err := json.Marshal(result)
			if err != nil {
				t.Errorf("json.Marshal failed: %v", err)
			}
		})
	}
}

// 工具结果的 SSE 报文：role 标记类型，元信息收在 toolResult 里。
func TestWriteToolResultPayload(t *testing.T) {
	ev := &llm.Event{ToolResult: &llm.ToolResult{
		CallID:     "call_1",
		Name:       "strata__exec",
		Seq:        0,
		Status:     llm.ToolStatusError,
		ErrorCode:  llm.ToolErrorExecutionFailed,
		DurationMS: 132,
		RuneCount:  4096,
	}}

	var buf bytes.Buffer
	if !writeToolResult(&buf, "1", ev) {
		t.Fatal("writeToolResult failed")
	}

	var decoded map[string]any
	payloads := ssePayloads(t, buf.String())
	if len(payloads) != 1 {
		t.Fatalf("expected exactly one SSE payload, got %d: %q", len(payloads), buf.String())
	}
	if err := json.Unmarshal([]byte(payloads[0]), &decoded); err != nil {
		t.Fatalf("unmarshal SSE payload: %v", err)
	}

	if decoded["role"] != llm.RoleTool {
		t.Errorf("role = %v, want %q", decoded["role"], llm.RoleTool)
	}
	tr, ok := decoded["toolResult"].(map[string]any)
	if !ok {
		t.Fatalf("toolResult is not an object: %v", decoded)
	}
	want := map[string]any{
		"callId": "call_1",
		"name":   "strata__exec",
		"seq":    float64(0),
		"status": llm.ToolStatusError,
		"err":    llm.ToolErrorExecutionFailed,
		"dur":    float64(132),
		"clen":   float64(4096),
	}
	for key, value := range want {
		if tr[key] != value {
			t.Errorf("toolResult[%s] = %v, want %v", key, tr[key], value)
		}
	}
}

// 结果正文不进协议：报文里只有元信息。
func TestWriteToolResultOmitsContent(t *testing.T) {
	ev := &llm.Event{ToolResult: &llm.ToolResult{
		CallID:  "call_1",
		Name:    "strata__exec",
		Status:  llm.ToolStatusOK,
		Content: "secret payload",
	}}

	var buf bytes.Buffer
	if !writeToolResult(&buf, "1", ev) {
		t.Fatal("writeToolResult failed")
	}

	payloads := ssePayloads(t, buf.String())
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}
	if strings.Contains(payloads[0], "secret payload") {
		t.Errorf("tool result content must not be sent to the client: %s", payloads[0])
	}
}

// 一批工具的结果逐条下发：序号与调用编号按调用顺序。
func TestWriteToolResultOrder(t *testing.T) {
	events := []*llm.Event{
		{ToolResult: &llm.ToolResult{CallID: "c1", Name: "a", Seq: 0, Status: llm.ToolStatusOK}},
		{ToolResult: &llm.ToolResult{CallID: "c2", Name: "b", Seq: 1, Status: llm.ToolStatusError, ErrorCode: llm.ToolErrorToolError}},
	}

	var buf bytes.Buffer
	for i, ev := range events {
		if !writeToolResult(&buf, strconv.Itoa(i+1), ev) {
			t.Fatalf("writeToolResult %d failed", i)
		}
	}

	payloads := ssePayloads(t, buf.String())
	if len(payloads) != 2 {
		t.Fatalf("expected 2 payloads, got %d: %q", len(payloads), buf.String())
	}
	for i, want := range []struct {
		callID string
		seq    float64
	}{{"c1", 0}, {"c2", 1}} {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(payloads[i]), &decoded); err != nil {
			t.Fatalf("unmarshal payload %d: %v", i, err)
		}
		tr, ok := decoded["toolResult"].(map[string]any)
		if !ok {
			t.Fatalf("payload %d has no toolResult: %s", i, payloads[i])
		}
		if tr["callId"] != want.callID || tr["seq"] != want.seq {
			t.Errorf("payload %d toolResult = %v, want callId=%s seq=%v", i, tr, want.callID, want.seq)
		}
	}
}

// 工具调用消息先于工具结果消息：前端按调用编号把结果对回调用。
func TestToolResultFollowsToolCall(t *testing.T) {
	var buf bytes.Buffer

	var callMsg ChatMessage
	applyToolCalls(&callMsg, &llm.Event{
		Done:       true,
		StopReason: llm.FinishReasonToolCalls,
		ToolCalls: []llm.ToolCall{{
			ID:       "call_1",
			Type:     "function",
			Function: llm.ToolCallFunc{Name: "strata__exec", Arguments: json.RawMessage(`{"command":"ls"}`)},
		}},
	}, "csid_1")
	if !writeEvent(&buf, "1", &callMsg) {
		t.Fatal("writeEvent failed")
	}

	ev := &llm.Event{ToolResult: &llm.ToolResult{CallID: "call_1", Name: "strata__exec", Status: llm.ToolStatusOK}}
	if !writeToolResult(&buf, "2", ev) {
		t.Fatal("writeToolResult failed")
	}

	payloads := ssePayloads(t, buf.String())
	if len(payloads) != 2 {
		t.Fatalf("expected 2 payloads, got %d", len(payloads))
	}

	var callPayload, resultPayload map[string]any
	if err := json.Unmarshal([]byte(payloads[0]), &callPayload); err != nil {
		t.Fatalf("unmarshal tool call payload: %v", err)
	}
	if err := json.Unmarshal([]byte(payloads[1]), &resultPayload); err != nil {
		t.Fatalf("unmarshal tool result payload: %v", err)
	}

	calls, ok := callPayload["toolCalls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool call payload has no toolCalls: %s", payloads[0])
	}
	call, _ := calls[0].(map[string]any)
	if call["id"] != "call_1" {
		t.Errorf("toolCalls[0].id = %v, want call_1", call["id"])
	}

	tr, ok := resultPayload["toolResult"].(map[string]any)
	if !ok {
		t.Fatalf("tool result payload has no toolResult: %s", payloads[1])
	}
	if tr["callId"] != call["id"] {
		t.Errorf("toolResult.callId = %v, want %v", tr["callId"], call["id"])
	}
}

// ssePayloads 取出 SSE 输出里的全部 data 字段。
func ssePayloads(t *testing.T, out string) []string {
	t.Helper()
	var payloads []string
	for _, line := range strings.Split(out, "\n") {
		if payload, ok := strings.CutPrefix(line, "data: "); ok {
			payloads = append(payloads, payload)
		}
	}
	return payloads
}
