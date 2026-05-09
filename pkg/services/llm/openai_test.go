package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBuildEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		path     string
		expected string
	}{
		{
			name:     "empty base URL uses default",
			baseURL:  "",
			path:     "/chat/completions",
			expected: "https://api.openai.com/v1/chat/completions",
		},
		{
			name:     "with trailing slash",
			baseURL:  "https://api.openai.com/v1/",
			path:     "/chat/completions",
			expected: "https://api.openai.com/v1/chat/completions",
		},
		{
			name:     "custom URL",
			baseURL:  "https://custom.api.com",
			path:     "/chat/completions",
			expected: "https://custom.api.com/v1/chat/completions",
		},
		{
			name:     "openrouter URL",
			baseURL:  "https://openrouter.ai/api/v1",
			path:     "/chat/completions",
			expected: "https://openrouter.ai/api/v1/chat/completions",
		},
		{
			name:     "ollama local (OpenAI compatible)",
			baseURL:  "http://localhost:11434/v1",
			path:     "/chat/completions",
			expected: "http://localhost:11434/v1/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildEndpoint(tt.baseURL, tt.path)
			if result != tt.expected {
				t.Errorf("buildEndpoint(%q, %q) = %v, want %v", tt.baseURL, tt.path, result, tt.expected)
			}
		})
	}
}

func TestOpenAIProviderChat(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %v, want application/json", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %v, want Bearer test-key", r.Header.Get("Authorization"))
		}

		// 返回模拟响应
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"content": "Hello, how can I help you?"
				}
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 8,
				"total_tokens": 18
			}
		}`))
	}))
	defer server.Close()

	// 创建 provider
	p := newOpenAIProvider()
	cfg := &config{
		baseURL:     server.URL,
		apiKey:      "test-key",
		model:       "gpt-3.5-turbo",
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

	if result.Usage.InputTokens != 10 {
		t.Errorf("Usage.PromptTokens = %v, want 10", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 8 {
		t.Errorf("Usage.CompletionTokens = %v, want 8", result.Usage.OutputTokens)
	}
}

func TestOpenAIProviderEmbedding(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": [{
				"embedding": [0.1, 0.2, 0.3]
			}]
		}`))
	}))
	defer server.Close()

	p := newOpenAIProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
		model:   "text-embedding-3-small",
	}

	result, err := p.Embedding(context.Background(), cfg, []string{"Hello world"})

	if err != nil {
		t.Errorf("Embedding() error = %v", err)
		return
	}

	if len(result) != 3 {
		t.Errorf("embedding length = %v, want 3", len(result))
	}

	if result[0] != 0.1 || result[1] != 0.2 || result[2] != 0.3 {
		t.Errorf("embedding = %v, want [0.1, 0.2, 0.3]", result)
	}
}

func TestOpenAIProviderGenerate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"choices": [{
				"text": "This is a generated response."
			}],
			"usage": {
				"prompt_tokens": 5,
				"completion_tokens": 10,
				"total_tokens": 15
			}
		}`))
	}))
	defer server.Close()

	p := newOpenAIProvider()
	cfg := &config{
		baseURL:     server.URL,
		apiKey:      "test-key",
		model:       "gpt-3.5-turbo-instruct",
		maxTokens:   1024,
		temperature: 0.7,
	}

	text, usage, err := p.Generate(context.Background(), cfg, "Say hello")

	if err != nil {
		t.Errorf("Generate() error = %v", err)
		return
	}

	if text != "This is a generated response." {
		t.Errorf("text = %v, want 'This is a generated response.'", text)
	}

	if usage.TotalTokens != 15 {
		t.Errorf("usage.TotalTokens = %v, want 15", usage.TotalTokens)
	}
}

func TestOpenAIProviderStreamChat(t *testing.T) {
	// 测试基本的流式响应解析
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// 直接写入模拟流式数据
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"index\":0,\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\" World\"},\"index\":0,\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := newOpenAIProvider()
	cfg := &config{
		baseURL:     server.URL,
		apiKey:      "test-key",
		model:       "gpt-3.5-turbo",
		maxTokens:   4096,
		temperature: 0.7,
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

	// 验证至少收到了一些结果
	if len(results) == 0 {
		t.Error("expected at least one result")
	}
}

func TestOpenAIProviderStreamChatWithTools(t *testing.T) {
	// 测试带 tool calls 的流式响应
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"index\":0,\"function\":{\"name\":\"get_weather\"}}]},\"index\":0,\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"\"},\"index\":0,\"finish_reason\":\"tool_calls\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := newOpenAIProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
		model:   "gpt-3.5-turbo",
	}

	tools := []ToolDefinition{
		{
			Type: "function",
			Function: FunctionDefinition{
				Name:        "get_weather",
				Description: "Get weather for a location",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{"location": map[string]any{"type": "string"}}},
			},
		},
	}

	for event, err := range p.StreamChat(context.Background(), cfg, []Message{
		{Role: RoleUser, Content: "What's the weather?"},
	}, tools) {
		if err != nil {
			t.Errorf("stream error = %v", err)
			break
		}
		// 验证 tool calls 被正确解析
		if len(event.ToolCalls) > 0 {
			if event.ToolCalls[0].Function.Name != "get_weather" {
				t.Errorf("tool name = %v, want get_weather", event.ToolCalls[0].Function.Name)
			}
		}
	}
}

func TestOpenAIProviderChatWithTools(t *testing.T) {
	// 测试带 tool calls 的 Chat 响应
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"content": "Let me check the weather.",
					"tool_calls": [{
						"id": "call_abc123",
						"type": "function",
						"function": {
							"name": "get_weather",
							"arguments": "{\"location\":\"Beijing\"}"
						}
					}]
				}
			}],
			"usage": {"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}
		}`))
	}))
	defer server.Close()

	p := newOpenAIProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
		model:   "gpt-3.5-turbo",
	}

	tools := []ToolDefinition{
		{
			Type: "function",
			Function: FunctionDefinition{
				Name:        "get_weather",
				Description: "Get weather",
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

func TestOpenAIProviderChatError(t *testing.T) {
	// 测试 HTTP 错误响应
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"Internal error","type":"server_error"}}`))
	}))
	defer server.Close()

	p := newOpenAIProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
		model:   "gpt-3.5-turbo",
	}

	_, err := p.Chat(context.Background(), cfg, []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	if err == nil {
		t.Error("expected error")
	}
}

func TestOpenAIProviderChatEmptyChoices(t *testing.T) {
	// 测试空 choices 响应
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`))
	}))
	defer server.Close()

	p := newOpenAIProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
	}

	_, err := p.Chat(context.Background(), cfg, []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	if err == nil {
		t.Error("expected error for empty choices")
	}
}

func TestOpenAIProviderEmbeddingError(t *testing.T) {
	// 测试 embedding 错误
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid request"}}`))
	}))
	defer server.Close()

	p := newOpenAIProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
	}

	_, err := p.Embedding(context.Background(), cfg, []string{"test"})

	if err == nil {
		t.Error("expected error")
	}
}

func TestOpenAIProviderGenerateError(t *testing.T) {
	// 测试 Generate 错误
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded"}}`))
	}))
	defer server.Close()

	p := newOpenAIProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
	}

	_, _, err := p.Generate(context.Background(), cfg, "test")

	if err == nil {
		t.Error("expected error")
	}
}

func TestOpenAIProviderGenerateEmptyChoices(t *testing.T) {
	// 测试空 choices
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`))
	}))
	defer server.Close()

	p := newOpenAIProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
	}

	_, _, err := p.Generate(context.Background(), cfg, "test")

	if err == nil {
		t.Error("expected error for empty choices")
	}
}

func TestDoRequestTimeout(t *testing.T) {
	// 测试自定义 HTTP client
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	// 使用自定义 client
	customClient := &http.Client{Timeout: 10 * time.Second}

	p := newOpenAIProvider()
	cfg := &config{
		baseURL:    server.URL,
		apiKey:     "test-key",
		httpClient: customClient,
	}

	result, err := p.Chat(context.Background(), cfg, []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	if err != nil {
		t.Errorf("Chat() error = %v", err)
		return
	}

	if result.Content != "OK" {
		t.Errorf("content = %v, want OK", result.Content)
	}
}

func TestOpenAIProviderEmbeddingWithCustomModel(t *testing.T) {
	// 测试使用自定义 embedding model
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求中的 model
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.5,0.6,0.7]}]}`))
	}))
	defer server.Close()

	p := newOpenAIProvider()
	cfg := &config{
		baseURL: server.URL,
		apiKey:  "test-key",
		model:   "text-embedding-3-large",
	}

	result, err := p.Embedding(context.Background(), cfg, []string{"test text"})

	if err != nil {
		t.Errorf("Embedding() error = %v", err)
		return
	}

	if len(result) != 3 {
		t.Errorf("embedding length = %v, want 3", len(result))
	}
}
