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
	CallID  string
	Name    string
	Content string
}

// EventActions 是 Event 携带的副作用指令。
type EventActions struct {
	StateDelta map[string]any // session 级状态增量
}
