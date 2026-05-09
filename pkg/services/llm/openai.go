package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"strings"
	"time"
)

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// chatRequestBody OpenAI Chat Completion 请求体
type chatRequestBody struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	Stream      bool             `json:"stream"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	ToolChoice  string           `json:"tool_choice,omitempty"`
	// Options for streaming response. Only set this when you set stream: true.
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`  // deepseek
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"` // deepseek
}

func (ou *openaiUsage) toUsage() *Usage {
	if ou == nil {
		return nil
	}
	return &Usage{
		InputTokens:  ou.PromptTokens,
		OutputTokens: ou.CompletionTokens,
		TotalTokens:  ou.TotalTokens,
	}
}

// openAIProvider OpenAI Provider 实现
type openAIProvider struct{}

// newOpenAIProvider 创建 OpenAI Provider
func newOpenAIProvider() *openAIProvider {
	return &openAIProvider{}
}

func (p *openAIProvider) Chat(ctx context.Context, cfg *config, messages []Message, tools []ToolDefinition) (*ChatResult, error) {
	body, err := p.doChatRequest(ctx, cfg, messages, tools, false)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content,omitempty"` // deepseek only
				ToolCalls        []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
						Results   json.RawMessage `json:"results"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage *openaiUsage `json:"usage,omitempty"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	result := &ChatResult{
		Content: resp.Choices[0].Message.Content,
	}
	if resp.Usage != nil {
		result.Usage = resp.Usage.toUsage()
	}

	for _, tc := range resp.Choices[0].Message.ToolCalls {
		args := tc.Function.Arguments
		// 处理 arguments 可能被 JSON 字符串包裹的情况
		if len(args) > 0 && args[0] == '"' {
			var s string
			if err := json.Unmarshal(args, &s); err == nil {
				args = []byte(s)
			}
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: ToolCallFunc{
				Name:      tc.Function.Name,
				Arguments: args,
				Results:   tc.Function.Results,
			},
		})
	}

	// 写入交互日志
	if cfg.logDir != "" {
		var thinkContent string
		if resp.Choices[0].Message.ReasoningContent != "" {
			thinkContent = resp.Choices[0].Message.ReasoningContent
		}
		go LogInteraction(cfg.logDir, "openai", &InteractionLog{
			Model:     cfg.model,
			Messages:  messages,
			Tools:     tools,
			Usage:     result.Usage,
			Response:  result.Content,
			ToolCalls: result.ToolCalls,
			Think:     thinkContent,
		})
	}

	return result, nil
}

func (p *openAIProvider) StreamChat(ctx context.Context, cfg *config, messages []Message, tools []ToolDefinition) iter.Seq2[*Event, error] {

	return func(yield func(*Event, error) bool) {
		endpoint := buildEndpoint(cfg.baseURL, "/chat/completions")

		var toolsOpt []ToolDefinition
		if len(tools) > 0 {
			toolsOpt = tools
		}

		reqBody := chatRequestBody{
			Model:       cfg.model,
			Messages:    toOpenAIMessages(messages),
			MaxTokens:   cfg.maxTokens,
			Temperature: cfg.temperature,
			Stream:      true,
			Tools:       toolsOpt,

			StreamOptions: &StreamOptions{IncludeUsage: true},
		}
		if len(toolsOpt) > 0 {
			reqBody.ToolChoice = "auto"
		}

		logger().Infow("stream start",
			"model", cfg.model,
			"msgs_count", len(messages),
			"tools_count", len(tools),
			"tools", ToolLogs(tools),
			"has_tools", len(tools) > 0,
			"endpoint", endpoint,
			"messages", MessagesLogged(messages),
		)

		reqBodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			yield(nil, fmt.Errorf("marshal request: %w", err))
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBodyBytes))
		if err != nil {
			logger().Warnw("create stream request failed", "err", err, "reqBody", string(reqBodyBytes))
			yield(nil, err)
			return
		}

		req.Header.Set("Content-Type", "application/json")
		if cfg.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
		}
		for k, v := range cfg.headers {
			req.Header.Set(k, v)
		}

		hc := cfg.httpClient
		if hc == nil {
			hc = &http.Client{Timeout: 0}
		}

		resp, err := hc.Do(req)
		if err != nil {
			logger().Warnw("stream request failed", "err", err, "reqBody", string(reqBodyBytes))
			yield(nil, err)
			return
		}

		if resp.StatusCode >= 400 {
			fmt.Fprintf(os.Stderr, "\n%s\n", string(reqBodyBytes))
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			errMsg := fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			logger().Warnw("stream response error",
				"status", resp.StatusCode,
				"respBody", string(respBody))
			yield(nil, errMsg)
			return
		}
		defer resp.Body.Close()

		push := func(event *Event, err error) bool {
			if err != nil {
				return yield(nil, err)
			}
			event.Author = "assistant"
			return yield(event, nil)
		}

		if err := p.parseStreamResponse(resp.Body, push, cfg.debug, cfg.logDir, cfg.model, messages, tools); err != nil {
			yield(nil, err)
		}
	}
}

// parseStreamResponse 解析流式响应，通过 push 直接产出 *Event。
func (p *openAIProvider) parseStreamResponse(body io.Reader, push Pusher, debug bool, logDir, model string, messages []Message, tools []ToolDefinition) error {
	bufReader := bufio.NewReaderSize(body, 1024)

	var currentToolCalls []ToolCall
	var finishReason FinishReason
	var lines int
	var responseText string
	var thinkContent string

	for {
		lines++
		rawLine, err := bufReader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				push(&Event{Done: true}, nil)
			} else {
				return fmt.Errorf("read: %w", err)
			}
			return nil
		}

		noSpaceLine := bytes.TrimSpace(rawLine)
		// 跳过非 data: 开头的行
		if len(noSpaceLine) < 5 || string(noSpaceLine[:5]) != "data:" {
			continue
		}

		if debug {
			fmt.Fprintln(os.Stderr, string(rawLine))
		}

		// 去除 data: 前缀和空格
		noPrefixLine := bytes.TrimLeft(noSpaceLine[5:], " \t")
		if string(noPrefixLine) == "[DONE]" {
			logger().Infow("stream DONE", "lines", lines)
			push(&Event{Done: true}, nil)
			return nil
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content,omitempty"`
					ToolCalls        []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Index    int    `json:"index"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
							Results   any    `json:"results"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason FinishReason `json:"finish_reason"`
			} `json:"choices"`
			Model string       `json:"model"`
			ID    string       `json:"id"`
			Usage *openaiUsage `json:"usage,omitempty"`
		}

		if err := json.Unmarshal(noPrefixLine, &chunk); err != nil {
			logger().Infow("parse chunk fail", "err", err, "rawLine", rawLine)
			return fmt.Errorf("parse chunk: %w", err)
		}

		if len(chunk.Choices) == 0 {
			logger().Debugw("choices is empty", "rawLine", rawLine)
			continue
		}

		delta := chunk.Choices[0].Delta
		finishReason = chunk.Choices[0].FinishReason

		// 累积 tool calls
		for _, tc := range delta.ToolCalls {
			if tc.Index >= len(currentToolCalls) {
				currentToolCalls = append(currentToolCalls, ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
				})
			}
			currentToolCalls[tc.Index].Function.Name += tc.Function.Name
			currentToolCalls[tc.Index].Function.Arguments = append(
				currentToolCalls[tc.Index].Function.Arguments,
				tc.Function.Arguments...,
			)
		}

		// 发送内容，每个 chunk 都带上累积的 tool_calls
		ev := &Event{
			ID:         NewEventID(),
			Timestamp:  time.Now(),
			Delta:      delta.Content,
			Think:      delta.ReasoningContent,
			ToolCalls:  currentToolCalls,
			StopReason: finishReason,
			Model:      chunk.Model,
			ResponseID: chunk.ID,
		}
		if chunk.Usage != nil {
			logger().Debugw("usage from chunk", "usage", chunk.Usage)
			ev.Usage = chunk.Usage.toUsage()
		}

		// 累积响应内容
		responseText += delta.Content
		if delta.ReasoningContent != "" {
			thinkContent += delta.ReasoningContent
		}

		// 检查是否需要结束流：
		// 1. finish_reason 不为空（标准行为）
		// 2. tool_calls_len 为 0 且之前累积了 tool_calls（DeepSeek 行为）
		shouldEndStream := finishReason != "" || (len(delta.ToolCalls) == 0 && len(currentToolCalls) > 0)

		if shouldEndStream {
			ev.Done = true
		}
		if !push(ev, nil) {
			return nil
		}

		if shouldEndStream {
			logger().Infow("stream done", "finish_reason", finishReason,
				"tool_calls_count", len(currentToolCalls), "lines", lines)
			if logDir != "" {
				go LogInteraction(logDir, "openai", &InteractionLog{
					Model:      model,
					Messages:   messages,
					Tools:      tools,
					Usage:      ev.Usage,
					Response:   responseText,
					ToolCalls:  currentToolCalls,
					Think:      thinkContent,
					StopReason: string(finishReason),
				})
			}
			return nil
		}
	}
}

// doChatRequest 发送聊天请求的公共方法
func (p *openAIProvider) doChatRequest(ctx context.Context, cfg *config, messages []Message, tools []ToolDefinition, stream bool) ([]byte, error) {
	endpoint := buildEndpoint(cfg.baseURL, "/chat/completions")

	var toolsOpt []ToolDefinition
	if len(tools) > 0 {
		toolsOpt = tools
	}

	reqBody := chatRequestBody{
		Model:       cfg.model,
		Messages:    toOpenAIMessages(messages),
		MaxTokens:   cfg.maxTokens,
		Temperature: cfg.temperature,
		Stream:      stream,
		Tools:       toolsOpt,
		ToolChoice:  "auto",
	}

	return p.doRequest(ctx, cfg, endpoint, reqBody)
}

// buildEndpoint 构建 API 端点（OpenAI 兼容接口）
func buildEndpoint(baseURL, path string) string {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		logger().Infow("empty baseURL")
		base = "https://api.openai.com/v1"
	}
	// 避免重复添加 /v1
	if strings.HasSuffix(base, "/v1") || strings.HasSuffix(base, "/v1beta") {
		return base + path
	}
	return base + "/v1" + path
}

func (p *openAIProvider) doRequest(ctx context.Context, cfg *config, endpoint string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if cfg.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
	}
	for k, v := range cfg.headers {
		req.Header.Set(k, v)
	}

	hc := cfg.httpClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.timeout}
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return respBody, nil
}

func toOpenAIMessages(messages []Message) []Message {
	return messages
}

// Generate 简单文本生成（使用 Completion API）
func (p *openAIProvider) Generate(ctx context.Context, cfg *config, prompt string) (string, *Usage, error) {
	endpoint := buildEndpoint(cfg.baseURL, "/completions")

	reqBody := map[string]any{
		"model":       cfg.model,
		"prompt":      prompt,
		"max_tokens":  cfg.maxTokens,
		"temperature": cfg.temperature,
	}

	body, err := p.doRequest(ctx, cfg, endpoint, reqBody)
	if err != nil {
		return "", nil, err
	}

	var resp struct {
		Choices []struct {
			Text string `json:"text"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return "", nil, fmt.Errorf("parse response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", nil, fmt.Errorf("no choices in response")
	}

	usage := &Usage{
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		TotalTokens:  resp.Usage.TotalTokens,
	}

	return resp.Choices[0].Text, usage, nil
}

// Embedding 向量化文本
func (p *openAIProvider) Embedding(ctx context.Context, cfg *config, texts []string) ([]float64, error) {
	endpoint := buildEndpoint(cfg.baseURL, "/embeddings")

	// 使用默认的 embedding model
	model := "text-embedding-3-small"
	if cfg.model != "" {
		model = cfg.model
	}

	reqBody := map[string]any{
		"input": texts,
		"model": model,
	}

	body, err := p.doRequest(ctx, cfg, endpoint, reqBody)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no embedding data")
	}

	return resp.Data[0].Embedding, nil
}
