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

const anthropicVersion = "2023-06-01"

// anthropicProvider Anthropic Provider 实现
type anthropicProvider struct{}

// newAnthropicProvider 创建 Anthropic Provider
func newAnthropicProvider() *anthropicProvider {
	return &anthropicProvider{}
}

func (p *anthropicProvider) Chat(ctx context.Context, cfg *config, messages []Message, tools []ToolDefinition) (*ChatResult, error) {
	endpoint := anthropicMessagesEndpoint(cfg.baseURL)

	anthropicMessages, systemText := toAnthropicMessages(messages)

	reqBody := struct {
		System      string          `json:"system,omitempty"`
		Model       string          `json:"model"`
		Messages    []anthropicMsg  `json:"messages"`
		Tools       []anthropicTool `json:"tools,omitempty"`
		MaxTokens   int             `json:"max_tokens"`
		Temperature *float64        `json:"temperature,omitempty"`
	}{
		Model:       cfg.model,
		Messages:    anthropicMessages,
		System:      systemText,
		MaxTokens:   cfg.maxTokens,
		Temperature: float64Ptr(cfg.temperature),
	}
	if len(tools) > 0 {
		converted, err := toAnthropicTools(tools)
		if err != nil {
			return nil, err
		}
		reqBody.Tools = converted
	}
	logger().Infow("chat start",
		"model", cfg.model,
		"msgs_count", len(messages),
		"tools_count", len(tools),
		"tools", ToolLogs(tools),
		"has_tools", len(tools) > 0,
		"endpoint", endpoint,
	)

	b, err := json.Marshal(reqBody)
	if err != nil {
		logger().Infow("marshal chat request failed", "err", err)
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		logger().Infow("create chat request failed", "err", err, "endpoint", endpoint)
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if cfg.apiKey != "" {
		req.Header.Set("x-api-key", cfg.apiKey)
	}
	req.Header.Set("anthropic-version", anthropicVersion)
	for k, v := range cfg.headers {
		req.Header.Set(k, v)
	}

	hc := cfg.httpClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.timeout}
	}

	resp, err := hc.Do(req)
	if err != nil {
		logger().Warnw("anthropic request failed", "err", err, "endpoint", endpoint)
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errMsg := fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		logger().Warnw("anthropic response error", "status", resp.StatusCode, "body", string(body))
		return nil, errMsg
	}

	result, err := parseAnthropicResponse(body)
	if err != nil {
		return nil, err
	}
	// 写入交互日志
	if cfg.logDir != "" {
		go LogInteraction(cfg.logDir, "anthropic", &InteractionLog{
			Model:     cfg.model,
			Messages:  messages,
			Tools:     tools,
			Usage:     result.Usage,
			Response:  result.Content,
			ToolCalls: result.ToolCalls,
		})
	}
	return result, nil
}

func (p *anthropicProvider) StreamChat(ctx context.Context, cfg *config, messages []Message, tools []ToolDefinition) iter.Seq2[*Event, error] {

	return func(yield func(*Event, error) bool) {
		endpoint := anthropicMessagesEndpoint(cfg.baseURL)
		anthropicMessages, systemText := toAnthropicMessages(messages)

		reqBody := struct {
			Model       string          `json:"model"`
			Messages    []anthropicMsg  `json:"messages"`
			System      string          `json:"system,omitempty"`
			Tools       []anthropicTool `json:"tools,omitempty"`
			MaxTokens   int             `json:"max_tokens"`
			Temperature *float64        `json:"temperature,omitempty"`
			Stream      bool            `json:"stream"`
		}{
			Model:       cfg.model,
			Messages:    anthropicMessages,
			System:      systemText,
			MaxTokens:   cfg.maxTokens,
			Temperature: float64Ptr(cfg.temperature),
			Stream:      true,
		}
		if len(tools) > 0 {
			converted, err := toAnthropicTools(tools)
			if err != nil {
				yield(nil, err)
				return
			}
			reqBody.Tools = converted
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
			logger().Warnw("marshal stream request failed", "err", err)
			yield(nil, err)
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
			req.Header.Set("x-api-key", cfg.apiKey)
		}
		req.Header.Set("anthropic-version", anthropicVersion)
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

		if cfg.debug || resp.StatusCode >= 300 {
			fmt.Fprintf(os.Stderr, "\n%s\n%d bytes\n", string(reqBodyBytes), len(reqBodyBytes))
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			errMsg := fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			logger().Warnw("stream response error",
				"status", resp.StatusCode,
				"respBody", string(respBody))
			yield(nil, errMsg)
			return
		}
		defer func() { _ = resp.Body.Close() }()

		// pusher 适配：补全 author 后直接 yield
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
func (p *anthropicProvider) parseStreamResponse(body io.Reader, push Pusher, debug bool, logDir, model string, messages []Message, tools []ToolDefinition) error {
	var currentToolCalls []ToolCall
	var currentText strings.Builder
	var thinkContent string

	bufReader := bufio.NewReaderSize(body, 1024)

	for {
		line, err := bufReader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				push(&Event{Done: true}, nil)
			} else {
				logger().Infow("read stream response failed", "err", err)
				return fmt.Errorf("read: %w", err)
			}
			return nil
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// 跳过非 data: 开头的行
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}

		if debug {
			fmt.Fprintln(os.Stderr, string(line))
		}

		data := bytes.TrimSpace(line[5:])
		if string(data) == "[DONE]" {
			push(&Event{Done: true}, nil)
			return nil
		}

		se, err := p.parseStreamEvent(data)
		if err != nil {
			continue
		}

		done, toolCalls := p.handleStreamEvent(se, &currentText, currentToolCalls, push, logDir, model, messages, tools, &thinkContent)
		currentToolCalls = toolCalls

		if done {
			logger().Infow("stream done", "event_type", se.Type, "tool_calls_count", len(currentToolCalls))
			return nil
		}
	}
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (au *anthropicUsage) toUsage() *Usage {
	if au == nil {
		return nil
	}
	return &Usage{
		InputTokens:  au.InputTokens,
		OutputTokens: au.OutputTokens,
		TotalTokens:  au.InputTokens + au.OutputTokens,
	}
}

// streamEvent Anthropic 流事件
type streamEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock *struct {
		Type  string          `json:"type"`
		Text  string          `json:"text,omitempty"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content_block,omitempty"`
	Delta struct {
		Text        string `json:"text,omitempty"`
		Thinking    string `json:"thinking,omitempty"`
		Type        string `json:"type,omitempty"`
		ID          string `json:"id,omitempty"`
		Name        string `json:"name,omitempty"`
		Input       any    `json:"input,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
		StopReason  string `json:"stop_reason,omitempty"`
	} `json:"delta,omitempty"`
	Message *struct {
		ID    string `json:"id,omitempty"`
		Model string `json:"model,omitempty"`
	} `json:"message,omitempty"`
	Usage *anthropicUsage `json:"usage,omitempty"`
}

// parseStreamEvent 解析单个流事件
func (p *anthropicProvider) parseStreamEvent(data []byte) (streamEvent, error) {
	var event streamEvent
	if err := json.Unmarshal(data, &event); err != nil {
		logger().Infow("parse anthropic stream event fail", "err", err, "data", string(data))
		return event, err
	}
	return event, nil
}

// handleStreamEvent 处理流事件，通过 push 直接产出 *Event。返回是否结束及更新后的 toolCalls。
func (p *anthropicProvider) handleStreamEvent(se streamEvent, currentText *strings.Builder, currentToolCalls []ToolCall,
	push Pusher, logDir, model string, messages []Message, tools []ToolDefinition, thinkContent *string) (
	bool, []ToolCall) {
	switch se.Type {
	case "content_block_start":
			// 开始新的内容块，检查是否是 tool_use 类型
		if se.ContentBlock != nil && se.ContentBlock.Type == "tool_use" {
			toolID := se.ContentBlock.ID
			if toolID == "" {
				toolID = fmt.Sprintf("toolu_%d", se.Index)
			}
			currentToolCalls = append(currentToolCalls, ToolCall{
				ID:   toolID,
				Type: "function",
				Function: ToolCallFunc{
					Name: se.ContentBlock.Name,
				},
			})
			logger().Debugw("tool_use started", "id", toolID, "name", se.ContentBlock.Name)
		}
	case "content_block_delta":
		switch se.Delta.Type {
		case "text_delta":
			currentText.WriteString(se.Delta.Text)
			if !push(&Event{
				ID:        NewEventID(),
				Timestamp: time.Now(),
				Delta:     se.Delta.Text,
				ToolCalls: currentToolCalls,

			}, nil) {
				return true, currentToolCalls
			}
		case "thinking_delta":
			*thinkContent += se.Delta.Thinking
			// thinking 独立于 tool_use，不附带正在构建的 tool_calls
			if !push(&Event{
				ID:        NewEventID(),
				Timestamp: time.Now(),
				Think:     se.Delta.Thinking,

			}, nil) {
				return true, currentToolCalls
			}
		case "input_json_delta":
			// 处理 tool_use 的参数，直接取最后一个 tool_call
			if len(currentToolCalls) > 0 && se.Delta.PartialJSON != "" {
				lastIdx := len(currentToolCalls) - 1
				// 跳过 thinking 相关字段（thinking_delta 伴随 input_json_delta 出现，但不属于 tool_use 参数）
				if !strings.HasPrefix(strings.TrimSpace(se.Delta.PartialJSON), "\"thinking") {
					currentToolCalls[lastIdx].Function.Arguments = append(
						currentToolCalls[lastIdx].Function.Arguments,
						se.Delta.PartialJSON...,
					)
				}
			}
		}
	case "content_block_stop":
		// 内容块结束
	case "message_delta":
		stopReason := FinishReason(se.Delta.StopReason)
		if stopReason == "end_turn" {
			stopReason = "stop"
		} else if len(currentToolCalls) > 0 {
			stopReason = "tool_calls" // 为了兼容 OpenAI
		}
		if !push(&Event{
			ID:         NewEventID(),
			Timestamp:  time.Now(),
			ToolCalls:  currentToolCalls,
			StopReason: stopReason,
			Usage:      se.Usage.toUsage(),

		}, nil) {
			return true, currentToolCalls
		}
		if logDir != "" {
			go LogInteraction(logDir, "anthropic", &InteractionLog{
				Model:      model,
				Messages:   messages,
				Tools:      tools,
				Usage:      se.Usage.toUsage(),
				Response:   currentText.String(),
				ToolCalls:  currentToolCalls,
				Think:      *thinkContent,
				StopReason: string(stopReason),
			})
		}
	case "message_stop": // 在 message_delta 后会跟一个message_stop，里面没有实际信息
		push(&Event{
			ID:        NewEventID(),
			Timestamp: time.Now(),
			Done: true,
			ToolCalls: currentToolCalls,
		}, nil)
		return true, currentToolCalls
	case "message_start":
		if se.Message != nil {
			push(&Event{
				ID:         NewEventID(),
				Timestamp:  time.Now(),
				Model:      se.Message.Model,
				ResponseID: se.Message.ID,

			}, nil)
		}
	case "ping":
		// 忽略
	default:
		logger().Infow("unknown anthropic event type", "type", se.Type)
	}
	return false, currentToolCalls
}

func (p *anthropicProvider) Generate(ctx context.Context, cfg *config, prompt string) (string, *Usage, error) {
	// Anthropic 不支持 Completion API，使用 Chat 代替
	messages := []Message{{Role: RoleUser, Content: prompt}}
	result, err := p.Chat(ctx, cfg, messages, nil)
	if err != nil {
		return "", nil, err
	}
	return result.Content, result.Usage, nil
}

func (p *anthropicProvider) Embedding(ctx context.Context, cfg *config, texts []string) ([]float64, error) {
	// Anthropic 不支持 Embedding API，返回错误
	return nil, fmt.Errorf("embedding not supported for anthropic provider")
}

// anthropicMsg Anthropic 消息格式
type anthropicMsg struct {
	Role    string                 `json:"role"`
	Content []anthropicContentPart `json:"content"`
}

// anthropicContentPart Anthropic 内容块
type anthropicContentPart struct {
	Type      string           `json:"type"`
	Text      string           `json:"text,omitempty"`
	Thinking  string           `json:"thinking,omitempty"`
	Source    *anthropicSource `json:"source,omitempty"`
	ID        string           `json:"id,omitempty"`
	Name      string           `json:"name,omitempty"`
	Input     json.RawMessage  `json:"input,omitempty"`
	ToolUseID string           `json:"tool_use_id,omitempty"`
	Content   string           `json:"content,omitempty"`
}

// anthropicSource Anthropic 图片源
type anthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

// anthropicTool Anthropic 工具定义
type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// toAnthropicTools 将 ToolDefinition 转换为 Anthropic 工具格式
func toAnthropicTools(tools []ToolDefinition) ([]anthropicTool, error) {
	out := make([]anthropicTool, 0, len(tools))
	for _, t := range tools {
		params, err := schemaToRawJSON(t.Function.Parameters)
		if err != nil {
			logger().Infow("convert tool to anthropic format failed", "err", err,
				"tool", t.Function.Name)
			return nil, fmt.Errorf("anthropic tool schema %s: %w", t.Function.Name, err)
		}
		out = append(out, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: params,
		})
	}
	return out, nil
}

// toAnthropicMessages 将 Message 列表转换为 Anthropic 消息格式
func toAnthropicMessages(messages []Message) ([]anthropicMsg, string) {
	out := make([]anthropicMsg, 0, len(messages))
	systemParts := make([]string, 0, 1)

	for _, m := range messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		switch role {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				systemParts = append(systemParts, m.Content)
			}

		case "tool":
			// Tool result — 尝试合并到前一个 user message
			toolResultBlock := anthropicContentPart{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}
			if len(out) > 0 {
				last := &out[len(out)-1]
				if last.Role == "user" {
					last.Content = append(last.Content, toolResultBlock)
					continue
				}
			}
			// 无法合并，创建新的 user message
			out = append(out, anthropicMsg{
				Role:    "user",
				Content: []anthropicContentPart{toolResultBlock},
			})

		case "user":
			if m.ToolCallID != "" {
				// Tool result via user message with ToolCallID
				toolResultBlock := anthropicContentPart{
					Type:      "tool_result",
					ToolUseID: m.ToolCallID,
					Content:   m.Content,
				}
				if len(out) > 0 {
					last := &out[len(out)-1]
					if last.Role == "user" {
						last.Content = append(last.Content, toolResultBlock)
						continue
					}
				}
				out = append(out, anthropicMsg{
					Role:    "user",
					Content: []anthropicContentPart{toolResultBlock},
				})
			} else {
				// Regular user message
				parts := toAnthropicInputParts(m)
				if len(parts) > 0 {
					out = append(out, anthropicMsg{
						Role:    "user",
						Content: parts,
					})
				}
			}

		case "assistant":
			content := toAnthropicInputParts(m)
			// Add tool_use blocks
			for i, tc := range m.ToolCalls {
				toolID := strings.TrimSpace(tc.ID)
				if toolID == "" {
					toolID = fmt.Sprintf("toolu_%d", i+1)
				}
				content = append(content, anthropicContentPart{
					Type:  "tool_use",
					ID:    toolID,
					Name:  tc.Function.Name,
					Input: parseArgsToRawJSON(string(tc.Function.Arguments)),
				})
			}
			if len(content) > 0 {
				out = append(out, anthropicMsg{
					Role:    "assistant",
					Content: content,
				})
			}
		}
	}

	return out, strings.Join(systemParts, "\n\n")
}

// toAnthropicInputParts 将 Message 转换为 Anthropic 输入内容块
// 注意：这个函数不处理 tool_calls，tool_calls 由 toAnthropicMessages 处理
func toAnthropicInputParts(m Message) []anthropicContentPart {
	if strings.TrimSpace(m.Content) == "" && strings.TrimSpace(m.Thinking) == "" {
		return nil
	}
	var parts []anthropicContentPart
	if strings.TrimSpace(m.Content) != "" {
		parts = append(parts, anthropicContentPart{
			Type: "text",
			Text: m.Content,
		})
	}
	if strings.TrimSpace(m.Thinking) != "" {
		parts = append(parts, anthropicContentPart{
			Type:     "thinking",
			Thinking: m.Thinking,
		})
	}
	return parts
}

// anthropicMessagesEndpoint 构建 Anthropic Messages API 端点
func anthropicMessagesEndpoint(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

// parseArgsToRawJSON 解析参数为 RawMessage
func parseArgsToRawJSON(s string) json.RawMessage {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	b := []byte(trimmed)
	if json.Valid(b) {
		return b
	}
	quoted, _ := json.Marshal(trimmed)
	return quoted
}

// parseAnthropicResponse 解析 Anthropic 响应
func parseAnthropicResponse(body []byte) (*ChatResult, error) {
	var parsed struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text,omitempty"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		} `json:"content"`
		Usage *anthropicUsage `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		logger().Warnw("parse anthropic response failed", "err", err, "body", string(body))
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}
	if len(parsed.Content) == 0 {
		logger().Warnw("anthropic response has empty content", "body", string(body))
		return nil, fmt.Errorf("anthropic response: empty content")
	}

	out := &ChatResult{
		Usage: parsed.Usage.toUsage(),
	}
	var textParts []string
	for i, part := range parsed.Content {
		switch part.Type {
		case "text":
			if strings.TrimSpace(part.Text) != "" {
				textParts = append(textParts, part.Text)
			}
		case "tool_use":
			toolID := strings.TrimSpace(part.ID)
			if toolID == "" {
				toolID = fmt.Sprintf("toolu_%d", i+1)
			}
			args := part.Input
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:   toolID,
				Type: "function",
				Function: ToolCallFunc{
					Name:      part.Name,
					Arguments: args,
				},
			})
		}
	}
	out.Content = strings.Join(textParts, "\n")
	return out, nil
}

// schemaToRawJSON 将参数 schema 转换为 RawMessage
func schemaToRawJSON(params any) (json.RawMessage, error) {
	if params == nil {
		return json.RawMessage(`{"type": "object", "properties": {}}`), nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		logger().Infow("schema to raw JSON failed", "err", err, "params", params)
		return nil, err
	}
	return json.RawMessage(b), nil
}

// float64Ptr 返回 float64 指针
func float64Ptr(v float64) *float64 {
	return &v
}
