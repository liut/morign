---
title: "Anthropic provider 的 Done 事件丢失 StopReason，导致 tool_calls 事件从未写入 SSE"
category: logic-errors
date: 2026-11-05
tags: [anthropic, streaming, sse, tool-calls, bug]
issues:
  - "客户端（Calisyn）看到两轮 LLM 的正文直接连在一起，没有分隔"
  - "首轮过渡语「I'll gather the tool and skill inventory for you.」紧贴正式回答「## 技能（Skills）...」"
  - "Anthropic provider 路径下 SSE 始终收不到 tool_calls 事件"
---

# Anthropic provider 的 Done 事件丢失 StopReason，导致 tool_calls 事件从未写入 SSE

## Problem

客户端（Calisyn 前端）反馈：一次带工具调用的对话，渲染出来的正文是两段内容直接粘连，中间没有任何分隔：

```
I'll gather the tool and skill inventory for you.## 技能（Skills）

| 技能 | 说明 |
|---|---|
| **rainman-travel** | 深度旅行研究工作流…… |
```

第一句是首轮 LLM 的过渡语，第二句是第二轮（拿到工具结果后）的正式回答。二者本应被一个 tool_calls 事件隔开。

## Root Cause

### 现象定位

`/tmp/morign/interact/anthropic_2026-11-05_17.jsonl` 确认这是**两次独立的 LLM 调用**，不是一次返回里的 thinking + 正文：

```
4  think: ''  resp: "I'll gather the tool and skill inventory for you."   stop_reason: tool_calls
5  think: ''  resp: '## 技能（Skills）...'                                stop_reason: stop
```

两条 `think` 全为空 —— 首轮那句是**普通正文（delta）**，不是思考内容。这一点排除了「thinking 与正文混字段」的假设。

### 断链位置

morrigan 在 `pkg/web/api/handle_convo.go` 的 SSE 写出逻辑里，**只有** `event.Done` 分支才会把 tool_calls 发给客户端：

```go
if event.Done {
    finish = event.StopReason

    if len(event.ToolCalls) > 0 && event.StopReason == llm.FinishReasonToolCalls {
        cm.ToolCalls = convertToolCallsForJSON(event.ToolCalls)
        chunkIdx++
        cm.ConversationID = ccr.cs.GetID()
        cm.FinishReason = string(event.StopReason)
        _ = writeEvent(w, strconv.Itoa(chunkIdx), &cm)   // ← 唯一出口
    }
    ...
}
```

写出条件是 `event.Done` **且** `event.StopReason == tool_calls` 同时成立。

### 两个 provider 的事件形状不一致

Anthropic provider 一轮调用结束会推**两个**事件，字段错开：

| 事件 | 位置 | `Done` | `StopReason` | `ToolCalls` |
|---|---|---|---|---|
| `message_delta` | `pkg/services/llm/anthropic.go:403-409` | ❌ 未设 | ✅ `tool_calls` | ✅ 有 |
| `message_stop` | `pkg/services/llm/anthropic.go:425-432` | ✅ `true` | ❌ **未设（空串）** | ✅ 有 |

- `message_delta` 带了正确的 `StopReason`（`anthropic.go:396-402` 里把 `tool_use` 归一为 `tool_calls`），但**没有 `Done`** → 进不了 `if event.Done` 分支。
- `message_stop` 带了 `Done: true`，但**没有 `StopReason`** → 进得去分支，可 `event.StopReason` 是空串，`event.StopReason == llm.FinishReasonToolCalls` 恒为 false。

两者互斥，**没有任何一个事件同时满足两个条件**。结果：

- `cm.ToolCalls` 从未被赋值
- 那条本该隔开两轮的 tool_calls SSE 事件**一次都没写出去**
- `finish = event.StopReason` 拿到的也是空串（同一 bug 的另一面）

### 为什么 OpenAI 路径不受影响

`pkg/services/llm/openai.go:327-336` 在解析 chunk 时就把 `StopReason: finishReason` 填进事件，随后 `openai.go:355-357` 在**同一个事件**上置 `ev.Done = true`：

```go
ev := &Event{
    ...
    ToolCalls:  currentToolCalls,
    StopReason: finishReason,
    ...
}
if shouldEndStream {
    ev.Done = true
}
```

`Done` 与 `StopReason` 同处一个事件，条件自然成立，tool_calls 正常写出。所以该缺陷**只在 Anthropic provider 路径上暴露**。

### 客户端为何表现为「粘连」而非「丢失」

两轮 LLM 的正文都经由 `cm.Delta` 这一个字段下发（`handle_convo.go:280-281`），中间本应由 tool_calls 事件隔断。既然它没发出去，客户端收到的是一条**连续的 delta 序列**：

```
delta: "I'll gather the tool and skill inventory for you."   ← 首轮
delta: "## 技能（Skills）..."                                ← 次轮
```

客户端按顺序把 delta 累加进同一个 text chunk，渲染出来就是两段紧挨着。**内容一条没丢，只是失去了边界信号** —— 这也是该问题此前未被察觉的原因：正文完整、不报错，只是排版诡异。

## Solution

**让 Anthropic provider 的 `Done` 事件自带 `StopReason`**，与 OpenAI provider 的事件形状对齐。

注意：`handleStreamEvent` 是**每个 SSE 事件调用一次**的函数，函数内的局部变量无法从 `message_delta` 那次调用带到 `message_stop` 那次调用。跨事件传递必须像既有的 `thinkContent *string` 那样，由 `parseStreamResponse` 持有、通过指针参数传入：

```go
// parseStreamResponse 中
var stopReason FinishReason
...
done, toolCalls := p.handleStreamEvent(se, &currentText, currentToolCalls, push, logDir, model,
	messages, tools, &thinkContent, &stopReason)
```

```go
func (p *anthropicProvider) handleStreamEvent(se streamEvent, currentText *strings.Builder, currentToolCalls []ToolCall,
	push Pusher, logDir, model string, messages []Message, tools []ToolDefinition, thinkContent *string,
	stopReason *FinishReason) (
	bool, []ToolCall) {
	switch se.Type {
	...
	case "message_delta":
		sr := FinishReason(se.Delta.StopReason)
		if sr == "end_turn" {
			sr = "stop"
		} else if len(currentToolCalls) > 0 {
			sr = "tool_calls" // 为了兼容 OpenAI
		}
		*stopReason = sr
		if !push(&Event{
			ID:         NewEventID(),
			Timestamp:  time.Now(),
			ToolCalls:  currentToolCalls,
			StopReason: sr,
			Usage:      se.Usage.toUsage(),
		}, nil) {
			return true, currentToolCalls
		}
	...
	case "message_stop":
		push(&Event{
			ID:         NewEventID(),
			Timestamp:  time.Now(),
			Done:       true,
			ToolCalls:  currentToolCalls,
			StopReason: *stopReason,   // ← 新增：补上本轮 stop reason
		}, nil)
		return true, currentToolCalls
	...
	}
}
```

这样 `message_stop` 事件同时具备 `Done` 与 `StopReason`，`handle_convo.go` 的既有判断无需改动即可命中，tool_calls 事件正常写出，两轮正文被正确隔开。`finish = event.StopReason` 也顺带拿到正确值。

### 下游的隐式依赖：`agent/stream.go`

`pkg/services/agent/stream.go` 里有一处只看 `StopReason`、不看 `Done` 的判断：

```go
if event.StopReason == llm.FinishReasonToolCalls {
    cb.OnThink("\n")
}
```

修复后 Anthropic 的两个事件（`message_delta` 与 `message_stop`）**都**带 `tool_calls`，该条件会命中两次，CLI 侧多打一个换行。需补上 `Done` 约束：

```go
if event.Done && event.StopReason == llm.FinishReasonToolCalls {
    cb.OnThink("\n")
}
```

这正是「Provider 事件契约」一节提醒的隐式依赖：**下游不得假设 `StopReason` 只在结束事件上出现**。

### 备选方案（未采纳）

改 `handle_convo.go` 的写出条件，让「有 tool_calls」即可写出，不依赖 `Done`：

```go
if len(event.ToolCalls) > 0 && event.StopReason == llm.FinishReasonToolCalls {
```

不采纳的原因：`message_delta`（带 `StopReason`、无 `Done`）和 `message_stop`（带 `Done`）都会命中该条件，可能**重复写出**两条 tool_calls 事件。方案一让事件形状对齐其它 provider，从根上消除这类条件耦合。

## Files Changed

- `pkg/services/llm/anthropic.go` — 结束事件补上 `StopReason`
- `pkg/services/agent/stream.go` — 判断补上 `event.Done` 约束，避免重复触发
- `pkg/services/llm/anthropic_test.go` — 回归测试：结束事件必须自带 `StopReason`

## Prevention

### Provider 事件契约

所有 provider 的「一轮结束」事件必须同时携带 `Done` 与 `StopReason`，下游才能用单一条件判断。约定：

| 字段 | 语义 | 必填时机 |
|---|---|---|
| `Done` | 本轮 LLM 输出结束 | 结束事件必填 |
| `StopReason` | 本轮结束原因（`stop` / `tool_calls` / `length`） | 结束事件必填 |
| `ToolCalls` | 本轮请求的工具调用 | 有则填 |

下游（`handle_convo.go`、agent loop）依赖的是**同一个事件**上的这两个字段，而非跨事件拼凑。

### Code Review Checklist

- [ ] provider 的结束事件是否同时设置了 `Done` 与 `StopReason`？
- [ ] 新增 provider 时是否比对了 `openai.go` 的事件形状？
- [ ] 下游是否存在「`Done` 和 `StopReason` 分处两个事件」的隐式依赖？

### 反模式

```go
// BAD: 两个事件各带一半字段，下游条件永远不成立
case "message_delta":
    push(&Event{StopReason: stopReason, ToolCalls: currentToolCalls})  // 无 Done
case "message_stop":
    push(&Event{Done: true, ToolCalls: currentToolCalls})              // 无 StopReason
```

```go
// GOOD: 结束事件自包含
case "message_stop":
    push(&Event{Done: true, StopReason: stopReason, ToolCalls: currentToolCalls})
```

## Impact

- **受影响路径**：所有走 Anthropic provider 的 SSE 流式对话（`/api/chat` 带 `stream` 参数）。
- **症状**：工具调用前后两轮正文无分隔，直接粘连。内容完整不丢失。
- **未受影响**：OpenAI 兼容 provider（`openai.go`）；非流式路径（`RunNonStreaming` 不依赖 SSE 事件形状）。
- **附带影响**：`handle_convo.go` 中 `finish` 变量取到空串，轮次终态记录的完成原因缺失。

## Related

- `pkg/services/llm/anthropic.go` - `handleStreamEvent` 的事件形状
- `pkg/services/llm/openai.go:327-357` - 正确的事件形状参照
- `pkg/web/api/handle_convo.go:279-298` - SSE 写出与 tool_calls 分支
- `pkg/services/agent/agent_loop.go:62-95` - 工具调用轮次循环
