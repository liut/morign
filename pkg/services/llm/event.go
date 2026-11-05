package llm

import (
	"time"

	oid "github.com/cupogo/andvari/models/oid"
)

// NewEventID 生成新的 Event ID (OID)。
func NewEventID() string {
	return oid.NewID(oid.OtEvent).String()
}

// Pusher 是流式事件推送函数类型，供 parseStreamResponse 等底层解析器使用。
type Pusher func(*Event, error) bool

// Event 是系统中 Agent、Tool、Runner、Handler 之间通信的唯一介质。
type Event struct {
	ID        string // OID
	Timestamp time.Time
	Author string // "user" | "assistant" | toolName

	// 对话内容
	Delta      string
	Think      string
	ToolCalls  []ToolCall
	StopReason FinishReason
	UserID     string // 用户标识，用于持久化 HistoryItem 时设置 UID
	UserPrompt string // 用户提问，用于持久化 HistoryItem 时配对
	Done       bool // 流式结束标记，零值 false = 未结束

	// 遥测
	Usage    *Usage
	Model    string
	MsgCount int            // 当前消息数，用于 UsageRecord
	Meta     map[string]any // 透传给 UsageRecord 的元数据

	ResponseID string

	// 工具结果
	ToolResult *ToolResult

	// 副作用
	Actions EventActions
}

// ToolResult 携带单次工具调用的执行结果。
type ToolResult struct {
	CallID     string
	Name       string
	Content    string
	Terminate  bool   // 工具请求终止 agent 循环
	Status     string // ToolStatusOK / ToolStatusError / ToolStatusRejected
	ErrorCode  string // 仅 Status != ToolStatusOK 时有值，取值见 ToolError* 常量
	Arguments  string // 入参原文
	Seq        int    // 轮内序号，还原调用与结果的配对顺序
	DurationMS int    // 执行耗时毫秒
}

// ToolStatus 表示工具调用的执行状态。
const (
	ToolStatusOK       = "ok"       // 工具正常返回
	ToolStatusError    = "error"    // 调用未能完成，或工具自报业务错误
	ToolStatusRejected = "rejected" // 被前置策略拒绝，未进入执行
)

// ToolErrorCode 表示工具调用失败的原因。
const (
	ToolErrorInvalidArguments = "invalid_arguments" // 参数 JSON 解析失败
	ToolErrorUnsupportedType   = "unsupported_type"  // tool call 类型非 function
	ToolErrorPanic             = "panic"             // 工具内部 panic
	ToolErrorExecutionFailed   = "execution_failed"  // Registry.Invoke 返回 error
	ToolErrorToolError         = "tool_error"        // 工具自身返回业务错误
	ToolErrorPermissionDenied  = "permission_denied" // 策略门控拒绝
	ToolErrorRejected          = "rejected"          // BeforeToolCall 拒绝
)

// EventActions 是 Event 携带的副作用指令。
type EventActions struct {
	StateDelta map[string]any // session 级状态增量
}
