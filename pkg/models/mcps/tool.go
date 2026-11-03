package mcps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Invoker is the tool invocation function type
type Invoker func(ctx context.Context, params map[string]any) (map[string]any, error)

// ToolDescriptor 是工具的描述符，用于 MCP 工具列表
type ToolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolCallPayload 是 MCP tools/call 的参数
type ToolCallPayload struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// BuildToolSuccessResult 构建标准的 MCP 工具成功结果
// 仅用于内嵌工具，不用兼容标准SDK
func BuildToolSuccessResult(structured any) map[string]any {
	result := map[string]any{}
	if structured != nil {
		if text, ok := structured.(string); ok {
			result["content"] = []map[string]any{
				{
					"type": "text",
					"text": text,
				},
			}
			return result
		}
		result["structuredContent"] = structured
		return result
	}

	result["content"] = []map[string]any{
		{
			"type": "text",
			"text": "ok",
		},
	}

	return result
}

// BuildToolErrorResult 构建标准的 MCP 工具错误结果
func BuildToolErrorResult(message string) map[string]any {
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "tool execution failed"
	}
	return map[string]any{
		"isError": true,
		"content": []map[string]any{
			{
				"type": "text",
				"text": msg,
			},
		},
	}
}

// IsToolError 判断结果是否为 BuildToolErrorResult 构造的业务错误结果
func IsToolError(result map[string]any) bool {
	v, ok := result["isError"].(bool)
	return ok && v
}

// StringArg 从参数中获取字符串值
func StringArg(arguments map[string]any, key string) string {
	if arguments == nil {
		return ""
	}
	raw, ok := arguments[key]
	if !ok {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", raw))
	}
}

// IntArg 从参数中获取整数值
func IntArg(arguments map[string]any, key string) (int, bool, error) {
	if arguments == nil {
		return 0, false, nil
	}
	raw, ok := arguments[key]
	if !ok || raw == nil {
		return 0, false, nil
	}
	switch value := raw.(type) {
	case int:
		return value, true, nil
	case int8:
		return int(value), true, nil
	case int16:
		return int(value), true, nil
	case int32:
		return int(value), true, nil
	case int64:
		return int(value), true, nil
	case uint:
		return int(value), true, nil
	case uint8:
		return int(value), true, nil
	case uint16:
		return int(value), true, nil
	case uint32:
		return int(value), true, nil
	case uint64:
		return int(value), true, nil
	case float32:
		f := float64(value)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, true, fmt.Errorf("%s must be a valid number", key)
		}
		return int(f), true, nil
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, true, fmt.Errorf("%s must be a valid number", key)
		}
		return int(value), true, nil
	case json.Number:
		i, err := value.Int64()
		if err != nil {
			return 0, true, fmt.Errorf("%s must be an integer", key)
		}
		return int(i), true, nil
	default:
		return 0, true, fmt.Errorf("%s must be a number", key)
	}
}

// BoolArg 从参数中获取布尔值
func BoolArg(arguments map[string]any, key string) (bool, bool, error) {
	if arguments == nil {
		return false, false, nil
	}
	raw, ok := arguments[key]
	if !ok || raw == nil {
		return false, false, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, true, fmt.Errorf("%s must be a boolean", key)
	}
	return value, true, nil
}

// ToolNames 是 []ToolDescriptor 的自定义类型，用于日志输出
type ToolNames []ToolDescriptor

// String 返回工具名称列表，用于日志记录
func (z ToolNames) String() string {
	if len(z) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteString("[")
	for i, td := range z {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(td.Name)
	}
	sb.WriteString("]")
	return sb.String()
}
