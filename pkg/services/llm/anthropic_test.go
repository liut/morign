package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicProviderChat(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %v, want application/json", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %v, want test-key", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != anthropicVersion {
			t.Errorf("anthropic-version = %v, want %v", r.Header.Get("anthropic-version"), anthropicVersion)
		}

		// 返回模拟响应
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"content": [{
				"type": "text",
				"text": "Hello, how can I help you?"
			}],
			"usage": {
				"input_tokens": 10,
				"output_tokens": 8
			}
		}`))
	}))
	defer server.Close()

	// 创建 provider
	p := newAnthropicProvider()
	cfg := &config{
		baseURL:     server.URL,
		apiKey:      "test-key",
		model:       "claude-3-5-sonnet-20241022",
		maxTokens:   4096,
		temperature: 0.7,
	}

	// 测试 Chat
	result, err := p.Chat(context.Background(), cfg, []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	if err != nil {
		t.Errorf("Chat() error = %v", err)
		return
	}

	if result == nil {
		t.Error("expected non-nil result")
		return
	}

	if result.Content != "Hello, how can I help you?" {
		t.Errorf("Content = %v, want 'Hello, how can I help you?'", result.Content)
	}
}

func TestAnthropicProviderChatWithTools(t *testing.T) {
	// 测试带 tool calls 的响应
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"content": [
				{
					"type": "text",
					"text": "Let me check the weather."
				},
				{
					"type": "tool_use",
					"id": "toolu_1",
					"name": "get_weather",
					"input": {"location": "Beijing"}
				}
			],
			"usage": {"input_tokens": 20, "output_tokens": 30}
		}`))
	}))
	defer server.Close()

	p := newAnthropicProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
		model:   "claude-3-5-sonnet-20241022",
	}

	tools := []ToolDefinition{
		{
			Type: "function",
			Function: FunctionDefinition{
				Name:        "get_weather",
				Description: "Get weather for a location",
			},
		},
	}

	result, err := p.Chat(context.Background(), cfg, []Message{
		{Role: RoleUser, Content: "Weather in Beijing?"},
	}, tools)

	if err != nil {
		t.Errorf("Chat() error = %v", err)
		return
	}

	if !result.HasToolCalls() {
		t.Error("expected tool calls")
	}

	if len(result.ToolCalls) != 1 {
		t.Errorf("tool calls length = %v, want 1", len(result.ToolCalls))
	}

	if result.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("tool name = %v, want get_weather", result.ToolCalls[0].Function.Name)
	}
}

func TestAnthropicProviderChatError(t *testing.T) {
	// 测试 HTTP 错误响应
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"type":"server_error","message":"Internal error"}}`))
	}))
	defer server.Close()

	p := newAnthropicProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
		model:   "claude-3-5-sonnet-20241022",
	}

	_, err := p.Chat(context.Background(), cfg, []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	if err == nil {
		t.Error("expected error")
	}
}

func TestAnthropicProviderChatEmptyContent(t *testing.T) {
	// 测试空 content 响应
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[]}`))
	}))
	defer server.Close()

	p := newAnthropicProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
		model:   "claude-3-5-sonnet-20241022",
	}

	_, err := p.Chat(context.Background(), cfg, []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestAnthropicProviderStreamChat(t *testing.T) {
	// 测试流式响应
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-3-5-sonnet-20241022\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"id\":\"block_1\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" World\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"usage\":{\"output_tokens\":5}},\"usage\":{\"output_tokens\":5}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := newAnthropicProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
		model:   "claude-3-5-sonnet-20241022",
	}

	var results []*Event
	for event, err := range p.StreamChat(context.Background(), cfg, []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil) {
		if err != nil {
			t.Errorf("stream error = %v", err)
			break
		}
		results = append(results, event)
	}

	if len(results) == 0 {
		t.Error("expected at least one result")
	}
}

func TestAnthropicProviderEmbedding(t *testing.T) {
	// 测试 Embedding 不支持
	p := newAnthropicProvider()
	cfg := &config{
		apiKey: "test-key",
		model:  "claude-3-5-sonnet-20241022",
	}

	_, err := p.Embedding(context.Background(), cfg, []string{"test"})

	if err == nil {
		t.Error("expected error for embedding not supported")
	}
}

func TestAnthropicProviderGenerate(t *testing.T) {
	// 测试 Generate 使用 Chat 代替
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"content": [{"type": "text", "text": "Generated text"}],
			"usage": {"input_tokens": 5, "output_tokens": 10}
		}`))
	}))
	defer server.Close()

	p := newAnthropicProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
		model:   "claude-3-5-sonnet-20241022",
	}

	text, usage, err := p.Generate(context.Background(), cfg, "Say hello")

	if err != nil {
		t.Errorf("Generate() error = %v", err)
		return
	}

	if text != "Generated text" {
		t.Errorf("text = %v, want 'Generated text'", text)
	}

	if usage.TotalTokens == 0 {
		t.Errorf("expected non-zero usage")
	}
}

func TestAnthropicMessagesEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		expected string
	}{
		{
			name:     "empty base URL uses default",
			baseURL:  "",
			expected: "https://api.anthropic.com/v1/messages",
		},
		{
			name:     "custom URL",
			baseURL:  "https://api.anthropic.com",
			expected: "https://api.anthropic.com/v1/messages",
		},
		{
			name:     "custom URL with v1",
			baseURL:  "https://api.anthropic.com/v1",
			expected: "https://api.anthropic.com/v1/messages",
		},
		{
			name:     "custom URL with trailing slash",
			baseURL:  "https://api.anthropic.com/",
			expected: "https://api.anthropic.com/v1/messages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := anthropicMessagesEndpoint(tt.baseURL)
			if result != tt.expected {
				t.Errorf("anthropicMessagesEndpoint(%q) = %v, want %v", tt.baseURL, result, tt.expected)
			}
		})
	}
}

func TestToAnthropicMessages(t *testing.T) {
	tests := []struct {
		name           string
		messages       []Message
		wantMsgCount   int
		wantSystemText string
	}{
		{
			name: "system message",
			messages: []Message{
				{Role: RoleSystem, Content: "You are a helpful assistant."},
				{Role: RoleUser, Content: "Hello"},
			},
			wantMsgCount:   1,
			wantSystemText: "You are a helpful assistant.",
		},
		{
			name: "multiple system messages",
			messages: []Message{
				{Role: RoleSystem, Content: "System 1"},
				{Role: RoleSystem, Content: "System 2"},
				{Role: RoleUser, Content: "Hello"},
			},
			wantMsgCount:   1,
			wantSystemText: "System 1\n\nSystem 2",
		},
		{
			name: "tool result message",
			messages: []Message{
				{Role: RoleUser, Content: "Hello"},
				{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{
					{ID: "call_1", Type: "function", Function: ToolCallFunc{Name: "get_weather", Arguments: json.RawMessage(`{}`)}},
				}},
				{Role: RoleTool, Content: "Sunny", ToolCallID: "call_1"},
			},
			wantMsgCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, systemText := toAnthropicMessages(tt.messages)
			if len(msgs) != tt.wantMsgCount {
				t.Errorf("message count = %v, want %v", len(msgs), tt.wantMsgCount)
			}
			if systemText != tt.wantSystemText {
				t.Errorf("system text = %v, want %v", systemText, tt.wantSystemText)
			}
		})
	}
}

func TestParseArgsToRawJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "{}",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "{}",
		},
		{
			name:     "valid JSON",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "plain text",
			input:    `plain text`,
			expected: `"plain text"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseArgsToRawJSON(tt.input)
			expected := json.RawMessage(tt.expected)
			if string(result) != string(expected) {
				t.Errorf("parseArgsToRawJSON(%q) = %v, want %v", tt.input, string(result), tt.expected)
			}
		})
	}
}
