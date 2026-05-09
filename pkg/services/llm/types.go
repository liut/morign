package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/liut/morign/pkg/utils/words"
)

// FinishReason 聊天完成原因
type FinishReason string

const (
	FinishReasonStop          FinishReason = "stop"
	FinishReasonLength        FinishReason = "length"
	FinishReasonFunctionCall  FinishReason = "function_call"
	FinishReasonToolCalls     FinishReason = "tool_calls"
	FinishReasonContentFilter FinishReason = "content_filter"
	FinishReasonNull          FinishReason = "null"
)

// Message 表示聊天消息
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`                     // 不可省略
	Thinking   string     `json:"reasoning_content,omitempty"` // thinking mode 内容
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // only for role=tool
}

// ToolCall 表示工具调用请求
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

// ToolCallFunc 工具调用函数
type ToolCallFunc struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Results   any             `json:"results,omitempty"`
}

// MarshalJSON 自定义序列化，将 Arguments 转为字符串（OpenAI API 要求）
func (f ToolCallFunc) MarshalJSON() ([]byte, error) {
	type Alias ToolCallFunc
	aux := &struct {
		Arguments any `json:"arguments"`
		*Alias
	}{
		Alias: (*Alias)(&f),
	}
	// 将 json.RawMessage 转为 string
	if len(f.Arguments) > 0 {
		aux.Arguments = string(f.Arguments)
	} else {
		aux.Arguments = "{}"
	}
	return json.Marshal(aux)
}

// ToolDefinition 工具定义
type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition 函数定义
type FunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// Tools 工具定义列表
type Tools []ToolDefinition

// Names 返回工具列表中的函数名
func (z Tools) Names() []string {
	out := make([]string, len(z))
	for i := range z {
		out[i] = z[i].Function.Name
	}
	return out
}

// ChatResult 聊天结果
type ChatResult struct {
	Content   string
	Thinking  string // reasoning_content for DeepSeek thinking mode
	ToolCalls []ToolCall
	Usage     *Usage
}

// HasToolCalls 判断是否有工具调用
func (r *ChatResult) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// Usage token 使用统计
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// Response 完整响应
type Response struct {
	Content          string
	ToolCalls        []ToolCall
	Usage            Usage
	CompletionTokens int
}

// MessageRole 消息角色常量
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// MessagesLogged 是 []Message 的自定义类型，用于日志输出
type MessagesLogged []Message

// String 返回每条消息的前30个字的文本，用于日志记录
func (z MessagesLogged) String() string {
	if len(z) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteString("[")
	for i, msg := range z {
		if i > 0 {
			sb.WriteString(", ")
		}
		text := msg.previewText(37)
		sb.WriteString(text)
	}
	sb.WriteString("]")
	return sb.String()
}

// previewText 返回消息的前n个字的文本，带角色前缀
func (z *Message) previewText(n int) string {
	var prefix string
	switch z.Role {
	case RoleSystem:
		prefix = "S: "
	case RoleUser:
		prefix = "U: "
	case RoleAssistant:
		prefix = "A: "
	case RoleTool:
		prefix = "T: " + z.ToolCallID + ": "
	default:
		prefix = "? "
	}

	text := z.Content
	// 有工具调用时显示工具调用信息
	if len(z.ToolCalls) > 0 {
		var tcNames []string
		for _, tc := range z.ToolCalls {
			tcNames = append(tcNames, tc.Function.Name)
		}
		text = "[tool_calls: " + strings.Join(tcNames, ", ") + "]"
	}
	// tool 角色显示 content 长度
	if z.Role == RoleTool && text != "" {
		text = fmt.Sprintf("[len=%d]", len(text))
	}

	return words.TakeHead(prefix+text, n, "...")
}

// ToolLogs 是 []ToolDefinition 的自定义类型，用于日志输出
type ToolLogs []ToolDefinition

// String 返回工具调用的函数名列表，用于日志记录
func (z ToolLogs) String() string {
	if len(z) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteString("[")
	for i, tc := range z {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(tc.Function.Name)
	}
	sb.WriteString("]")
	return sb.String()
}
