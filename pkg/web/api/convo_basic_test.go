package api

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/liut/morign/pkg/services/llm"
)

// 工具调用字段对外协议：驼峰为主键，过渡期保留下划线别名给旧客户端。
func TestChatMessageToolCallsJSON(t *testing.T) {
	calls := []map[string]any{
		{
			"id":   "call_1",
			"type": "function",
			"function": map[string]any{
				"name":      "strata__exec",
				"arguments": `{"command":"ls"}`,
			},
		},
	}

	var msg ChatMessage
	msg.setToolCalls(calls)

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal ChatMessage: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal ChatMessage: %v", err)
	}

	for _, key := range []string{"toolCalls", "tool_calls"} {
		got, ok := decoded[key]
		if !ok {
			t.Fatalf("payload missing %q: %s", key, data)
		}
		gotCalls, ok := got.([]any)
		if !ok || len(gotCalls) != len(calls) {
			t.Fatalf("%q = %v, want %d tool calls", key, got, len(calls))
		}
	}

	// 数组内部结构不变：id / type / function
	call, ok := decoded["toolCalls"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatalf("toolCalls[0] is not an object: %s", data)
	}
	if call["id"] != "call_1" || call["type"] != "function" {
		t.Errorf("toolCalls[0] = %v, want id=call_1 type=function", call)
	}
	fn, ok := call["function"].(map[string]any)
	if !ok || fn["name"] != "strata__exec" {
		t.Errorf("toolCalls[0].function = %v, want name=strata__exec", call["function"])
	}
}

// 过渡期的两个键必须内容一致，客户端只应依赖 toolCalls。
func TestChatMessageToolCallsLegacyMatches(t *testing.T) {
	var msg ChatMessage
	msg.setToolCalls([]map[string]any{{"id": "call_1", "type": "function"}})

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal ChatMessage: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal ChatMessage: %v", err)
	}

	newCalls, _ := json.Marshal(decoded["toolCalls"])
	legacyCalls, _ := json.Marshal(decoded["tool_calls"])
	if string(newCalls) != string(legacyCalls) {
		t.Errorf("toolCalls = %s, tool_calls = %s; want identical content", newCalls, legacyCalls)
	}
}

// 没有工具调用时，两个键都不出现。
func TestChatMessageToolCallsJSONOmitEmpty(t *testing.T) {
	data, err := json.Marshal(ChatMessage{Delta: "hi"})
	if err != nil {
		t.Fatalf("marshal ChatMessage: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal ChatMessage: %v", err)
	}

	for _, key := range []string{"toolCalls", "tool_calls"} {
		if _, ok := decoded[key]; ok {
			t.Errorf("payload should omit %q when there is no tool call: %s", key, data)
		}
	}
}

// toolCallID 走驼峰，与同结构体的其余字段一致。
func TestChatMessageToolCallIDJSON(t *testing.T) {
	data, err := json.Marshal(ChatMessage{ToolCallID: "call_1"})
	if err != nil {
		t.Fatalf("marshal ChatMessage: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal ChatMessage: %v", err)
	}

	if decoded["toolCallID"] != "call_1" {
		t.Errorf("toolCallID = %v, want call_1: %s", decoded["toolCallID"], data)
	}
	if _, ok := decoded["tool_call_id"]; ok {
		t.Errorf("payload should not contain tool_call_id: %s", data)
	}
}

// SSE 实际写出的报文：驼峰为主键，下划线别名同时在场。
func TestWriteEventToolCallsPayload(t *testing.T) {
	var cm ChatMessage
	cm.setToolCalls([]map[string]any{{"id": "call_1", "type": "function"}})
	cm.FinishReason = "tool_calls"

	var buf bytes.Buffer
	if !writeEvent(&buf, "1", &cm) {
		t.Fatal("writeEvent failed")
	}

	var payload string
	for _, line := range strings.Split(buf.String(), "\n") {
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			payload = after
			break
		}
	}
	if payload == "" {
		t.Fatalf("no data field in SSE output: %q", buf.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("unmarshal SSE payload: %v", err)
	}
	for _, key := range []string{"toolCalls", "tool_calls"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("SSE payload missing %q: %s", key, payload)
		}
	}
}

// 生产装配点：工具调用事件必须同时落到驼峰主键与下划线别名。
func TestApplyToolCalls(t *testing.T) {
	ev := &llm.Event{
		Done:       true,
		StopReason: llm.FinishReasonToolCalls,
		ToolCalls: []llm.ToolCall{{
			ID:       "call_1",
			Type:     "function",
			Function: llm.ToolCallFunc{Name: "strata__exec", Arguments: json.RawMessage(`{"command":"ls"}`)},
		}},
	}

	var cm ChatMessage
	applyToolCalls(&cm, ev, "csid_1")

	if cm.ConversationID != "csid_1" {
		t.Errorf("ConversationID = %q, want csid_1", cm.ConversationID)
	}
	if cm.FinishReason != string(llm.FinishReasonToolCalls) {
		t.Errorf("FinishReason = %q, want %q", cm.FinishReason, llm.FinishReasonToolCalls)
	}
	if len(cm.ToolCalls) != 1 || len(cm.ToolCallsLegacy) != 1 {
		t.Fatalf("ToolCalls = %v, ToolCallsLegacy = %v; want one call each", cm.ToolCalls, cm.ToolCallsLegacy)
	}

	data, err := json.Marshal(cm)
	if err != nil {
		t.Fatalf("marshal ChatMessage: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal ChatMessage: %v", err)
	}
	for _, key := range []string{"toolCalls", "tool_calls"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("payload missing %q: %s", key, data)
		}
	}
}
