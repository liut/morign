package convo

// ToolRecordSpill 是工具结果落库的输入。
// 轮次、会话与归属不放在这里：它们一律由服务端从请求上下文推导，不接受调用方指定。
type ToolRecordSpill struct {
	// Seq 是工具调用在轮内的序号，用于还原 provider 要求的调用与结果顺序。
	Seq int
	// CallID 是模型给出的工具调用编号。
	CallID string
	// ToolName 是工具名。
	ToolName string
	// Arguments 是入参原文。
	Arguments string
	// Status 是结果状态。
	Status string
	// Content 是结果全文。
	Content string
	// Preview 是当时发给模型的预览原文。
	Preview string
	// Spilled 表示结果是否走 handle 外溢。
	Spilled bool
	// ErrorCode 是错误码，非错误时为空。
	ErrorCode string
	// DurationMS 是执行耗时毫秒。
	DurationMS int
}
