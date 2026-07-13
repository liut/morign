package api

import (
	"encoding/json"
	"testing"

	"github.com/liut/morign/pkg/services/llm"
)


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

