package agent

import (
	"context"
	"sync/atomic"
)

// toolSeqKey 轮内序号计数器在上下文中的键。
type toolSeqKey struct{}

// ToolSeqCounter 轮内工具调用序号计数器。
// 同一批次的工具并发执行，插入顺序不确定；序号由计数器统一分配，
// 用来还原 provider 要求的「调用与结果同序」配对关系。
type ToolSeqCounter struct {
	n atomic.Int64
}

// NewToolSeqCounter 创建序号计数器。
func NewToolSeqCounter() *ToolSeqCounter {
	return &ToolSeqCounter{}
}

// ContextWithToolSeqCounter 把序号计数器挂到上下文。
func ContextWithToolSeqCounter(ctx context.Context, c *ToolSeqCounter) context.Context {
	return context.WithValue(ctx, toolSeqKey{}, c)
}

// Next 预占 count 个序号，返回本批次的起始序号。
func (c *ToolSeqCounter) Next(count int) int {
	return int(c.n.Add(int64(count)) - int64(count))
}
