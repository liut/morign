package llm

import (
	"encoding/json"
	"testing"
)

func TestToOpenAIMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		expected []Message
	}{
		{
			name:     "empty messages",
			messages: []Message{},
			expected: []Message{},
		},
		{
			name: "user message",
			messages: []Message{
				{Role: RoleUser, Content: "Hello"},
			},
			expected: []Message{
				{Role: "user", Content: "Hello"},
			},
		},
		{
			name: "system message",
			messages: []Message{
				{Role: RoleSystem, Content: "You are helpful."},
			},
			expected: []Message{
				{Role: "system", Content: "You are helpful."},
			},
		},
		{
			name: "assistant message with tool calls",
			messages: []Message{
				{
					Role: RoleAssistant,
					ToolCalls: []ToolCall{
						{
							ID:   "call_123",
							Type: "function",
							Function: ToolCallFunc{
								Name:      "get_weather",
								Arguments: json.RawMessage(`{"location":"Beijing"}`),
							},
						},
					},
				},
			},
			expected: []Message{
				{
					Role: "assistant",
					ToolCalls: []ToolCall{
						{
							ID:   "call_123",
							Type: "function",
							Function: ToolCallFunc{
								Name:      "get_weather",
								Arguments: json.RawMessage(`{"location":"Beijing"}`),
							},
						},
					},
				},
			},
		},
		{
			name: "tool response message",
			messages: []Message{
				{
					Role:       RoleTool,
					ToolCallID: "call_123",
					Content:    "Sunny, 25°C",
				},
			},
			expected: []Message{
				{Role: "tool", ToolCallID: "call_123", Content: "Sunny, 25°C"},
			},
		},
		{
			name: "message with name",
			messages: []Message{
				{
					Role:    RoleUser,
					Content: "Hello",
				},
			},
			expected: []Message{
				{Role: "user", Content: "Hello"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toOpenAIMessages(tt.messages)
			if len(result) != len(tt.expected) {
				t.Fatalf("length mismatch: got %d, want %d", len(result), len(tt.expected))
			}
			for i := range result {
				if result[i].Role != tt.expected[i].Role {
					t.Errorf("messages[%d].Role = %v, want %v", i, result[i].Role, tt.expected[i].Role)
				}
				if result[i].Content != tt.expected[i].Content {
					t.Errorf("messages[%d].Content = %v, want %v", i, result[i].Content, tt.expected[i].Content)
				}
				if result[i].ToolCallID != tt.expected[i].ToolCallID {
					t.Errorf("messages[%d].ToolCallID = %v, want %v", i, result[i].ToolCallID, tt.expected[i].ToolCallID)
				}
			}
		})
	}
}

func TestToolCallFuncMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		function ToolCallFunc
		wantArgs string
	}{
		{
			name: "normal arguments",
			function: ToolCallFunc{
				Name:      "get_weather",
				Arguments: json.RawMessage(`{"location":"Beijing"}`),
			},
			wantArgs: `{"location":"Beijing"}`,
		},
		{
			name: "empty arguments",
			function: ToolCallFunc{
				Name:      "test",
				Arguments: json.RawMessage{},
			},
			wantArgs: "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.function)
			if err != nil {
				t.Fatalf("MarshalJSON failed: %v", err)
			}

			var result map[string]any
			if err := json.Unmarshal(b, &result); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			args, ok := result["arguments"]
			if !ok {
				t.Fatal("arguments field not found")
			}

			if args != tt.wantArgs {
				t.Errorf("arguments = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

func TestChatResultHasToolCalls(t *testing.T) {
	tests := []struct {
		name     string
		toolCalls []ToolCall
		expected  bool
	}{
		{
			name:      "no tool calls",
			toolCalls: nil,
			expected:  false,
		},
		{
			name:      "empty tool calls",
			toolCalls: []ToolCall{},
			expected:  false,
		},
		{
			name: "has tool calls",
			toolCalls: []ToolCall{
				{ID: "call_1", Type: "function"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ChatResult{
				ToolCalls: tt.toolCalls,
			}
			if result := r.HasToolCalls(); result != tt.expected {
				t.Errorf("HasToolCalls() = %v, want %v", result, tt.expected)
			}
		})
	}
}


func TestMessagesLogged_String(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		expected string
	}{
		{
			name:     "empty",
			messages: []Message{},
			expected: "[]",
		},
		{
			name: "system message",
			messages: []Message{
				{Role: RoleSystem, Content: "You are helpful."},
			},
			expected: "[S: You are helpful.]",
		},
		{
			name: "user message",
			messages: []Message{
				{Role: RoleUser, Content: "Hello world"},
			},
			expected: "[U: Hello world]",
		},
		{
			name: "assistant message",
			messages: []Message{
				{Role: RoleAssistant, Content: "I can help you."},
			},
			expected: "[A: I can help you.]",
		},
		{
			name: "tool message",
			messages: []Message{
				{Role: RoleTool, Content: "Sunny, 25C"},
			},
			// Name 字段被忽略，使用 ToolCallID
			expected: "[T: : [len=10]]",
		},
		{
			name: "multiple messages",
			messages: []Message{
				{Role: RoleSystem, Content: "You are helpful."},
				{Role: RoleUser, Content: "Hello"},
				{Role: RoleAssistant, Content: "Hi there!"},
			},
			expected: "[S: You are helpful., U: Hello, A: Hi there!]",
		},
		{
			name: "truncate long content",
			messages: []Message{
				{Role: RoleUser, Content: "This is a very long message that should be truncated"},
			},
			expected: "[U: This is a very long message that s...]",
		},
		{
			name: "tool calls",
			messages: []Message{
				{
					Role: RoleAssistant,
					ToolCalls: []ToolCall{
						{ID: "call_1", Type: "function", Function: ToolCallFunc{Name: "get_weather"}},
					},
				},
			},
			expected: "[A: [tool_calls: get_weather]]",
		},
		{
			name: "tool result empty content",
			messages: []Message{
				{Role: RoleTool, ToolCallID: "call_123", Content: ""},
			},
			expected: "[T: call_123: ]",
		},
		{
			name: "unknown role",
			messages: []Message{
				{Role: "admin", Content: "some content"},
			},
			expected: "[? some content]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logged := MessagesLogged(tt.messages)
			result := logged.String()
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMessage_previewText(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		n        int
		expected string
	}{
		{
			name:     "system truncate",
			msg:      Message{Role: RoleSystem, Content: "This is a very long system message"},
			n:        15,
			expected: "S: This is a ve...",
		},
		{
			name:     "user no truncate",
			msg:      Message{Role: RoleUser, Content: "Hello"},
			n:        20,
			expected: "U: Hello",
		},
		{
			name:     "tool with name",
			msg:      Message{Role: RoleTool, Content: "Result"},
			n:        50,
			// Name 字段被忽略
			expected: "T: : [len=6]",
		},
		{
			name: "assistant tool calls",
			msg: Message{
				Role: RoleAssistant,
				ToolCalls: []ToolCall{
					{Function: ToolCallFunc{Name: "search"}},
				},
			},
			n:        50,
			expected: "A: [tool_calls: search]",
		},
		{
			name:     "tool result",
			msg:      Message{Role: RoleTool, ToolCallID: "call_1", Content: "result data"},
			n:        50,
			expected: "T: call_1: [len=11]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.msg.previewText(tt.n)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestToolLogs_String(t *testing.T) {
	tests := []struct {
		name      string
		toolCalls []ToolDefinition
		expected  string
	}{
		{
			name:      "empty",
			toolCalls: []ToolDefinition{},
			expected:  "[]",
		},
		{
			name: "single tool",
			toolCalls: []ToolDefinition{
				{Type: "function", Function: FunctionDefinition{Name: "get_weather"}},
			},
			expected: "[get_weather]",
		},
		{
			name: "multiple tools",
			toolCalls: []ToolDefinition{
				{Type: "function", Function: FunctionDefinition{Name: "get_weather"}},
				{Type: "function", Function: FunctionDefinition{Name: "search"}},
				{Type: "function", Function: FunctionDefinition{Name: "calculate"}},
			},
			expected: "[get_weather, search, calculate]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logged := ToolLogs(tt.toolCalls)
			result := logged.String()
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}
