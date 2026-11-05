---
title: "Anthropic provider 的 Usage 落在 message_delta 上，Done 事件不带用量，UsageRecord 从未写入"
category: logic-errors
date: 2026-11-05
tags: [anthropic, streaming, usage, persistence, bug]
issues:
  - "Anthropic provider 路径下 convo_usage_record 表始终为空，用量无法归集"
  - "UsageRecord 的 InputTokens 恒为 0（input_tokens 只在 message_start 给出，从未被解析）"
  - "计划 004 的 U5「真实 token 预算」依赖 Usage.InputTokens，前提不成立"
---

# Anthropic provider 的 Usage 落在 message_delta 上，Done 事件不带用量，UsageRecord 从未写入

## Problem

`convo_usage_record` 表里只有 OpenAI 路径的记录。凡是走 Anthropic provider 的会话，一条用量记录都不会产生。

## Root Cause

### 写入点在 `Done` 分支里

`pkg/web/api/handle_convo.go:300-318` 是唯一创建 `UsageRecord` 的路径（经 `runner.Persist` → `CreateUsageRecord`），它嵌在 `if event.Done` 分支中：

```go
if event.Done {
    ...
    if event.Usage != nil {
        _ = a.rnr.Persist(r.Context(), ccr.cs.GetID(), &llm.Event{
            Author: "assistant",
            Usage:  event.Usage,
            ...
        })
    }
}
```

即：**只有结束事件上带 `Usage`，才会落库**。

### Anthropic 的两个事件字段错开

| 事件 | `Usage` | `Done` |
|---|---|---|
| `message_delta` | ✅ 有（output_tokens） | ❌ |
| `message_stop` | ❌ 无 | ✅ true |

`message_delta` 带着用量但不是结束事件，`message_stop` 是结束事件但不带用量 —— 与 [StopReason 丢失](anthropic-stopreason-lost-tool-calls-sse.md) 是同一个病根：**跨事件拆字段**。

结果：`event.Usage` 在 `Done` 事件上恒为 nil，`if event.Usage != nil` 恒为 false，用量记录一次都不写。

### `input_tokens` 从未被解析

Anthropic 把 `input_tokens` 放在 `message_start` 的 `message.usage` 里，把 `output_tokens` 放在 `message_delta` 的顶层 `usage` 里。而 `streamEvent.Message` 结构体（`pkg/services/llm/anthropic.go:320`）只有 `ID` 与 `Model` 两个字段，**没有 `Usage`**：

```go
Message *struct {
    ID    string `json:"id,omitempty"`
    Model string `json:"model,omitempty"`
} `json:"message,omitempty"`
```

`message_start` 的用量从未被读出。即使修好了上一条，落库的 `InputTokens` 也会恒为 0。

## Solution

### 结束事件自带 `Usage`

把 `stopReason` 与 `usage` 收进一个本轮状态结构体，由 `parseStreamResponse` 持有、指针传入 `handleStreamEvent`（跨事件传递的必要条件——`handleStreamEvent` 每事件调用一次）：

```go
// anthropicStreamState 承载一轮流式调用中需要跨事件传递的状态。
type anthropicStreamState struct {
    stopReason FinishReason
    usage      *Usage
}
```

- `message_delta` 算出 `stopReason` 与 `output_tokens`，写进 `state`
- `message_stop` 事件带上 `state.stopReason` 与 `state.usage`
- `[DONE]` 与 EOF 两个兜底分支同样带上 `state`，避免截断流产生字段不全的结束事件

### 合并 input / output tokens

`streamEvent.Message` 补 `Usage *anthropicUsage` 字段；`message_start` 存下 `input_tokens`，`message_delta` 补上 `output_tokens` 并重算 `TotalTokens`：

```go
case "message_start":
    if se.Message.Usage != nil {
        state.usage = &Usage{
            InputTokens: se.Message.Usage.InputTokens,
            TotalTokens: se.Message.Usage.InputTokens,
        }
    }
    ...
case "message_delta":
    if u := se.Usage.toUsage(); u != nil {
        if state.usage == nil {
            state.usage = &Usage{}
        }
        state.usage.OutputTokens = u.OutputTokens
        state.usage.TotalTokens = state.usage.InputTokens + state.usage.OutputTokens
    }
```

修复后 Anthropic 的结束事件与 OpenAI 的形状一致：`Done` + `StopReason` + `Usage` + `ToolCalls` 全在一个事件上。

## Files Changed

- `pkg/services/llm/anthropic.go` — 结束事件补 `Usage`；解析 `message_start` 的 `input_tokens`
- `pkg/services/llm/anthropic_test.go` — 回归测试：结束事件必须携带完整用量

## Prevention

### Provider 事件契约（补充）

除 `Done` 与 `StopReason` 外，**用量也必须挂在结束事件上**：

| 字段 | 语义 | 必填时机 |
|---|---|---|
| `Usage` | 本轮真实 token 用量 | 结束事件必填 |

下游在 `if event.Done` 分支里读 `event.Usage` 落 `UsageRecord`，这是唯一入口。用量挂在别的事件上，等于没有落库。

**Code Review Checklist 补充项：**

- [ ] provider 的结束事件是否携带 `Usage`？
- [ ] provider 的用量字段是否分散在多个事件里（如 Anthropic 的 input/output tokens 分处 `message_start` / `message_delta`）？合并后 `TotalTokens` 是否自洽？

## Impact

- **受影响路径**：所有走 Anthropic provider 的会话，用量记录缺失、prompt token 计数为 0。
- **下游依赖**：[计划 004](../../plans/2026-10-29-004-feat-conversation-persistence-basis-plan.md) 的 U5「真实 token 预算」以 `Usage.InputTokens` 为裁剪依据，该前提在修复前不成立。
- **未受影响**：OpenAI 兼容路径（`openai.go` 的 `Usage` 与 `Done` 同处一个事件）。

## Related

- [StopReason 丢失](anthropic-stopreason-lost-tool-calls-sse.md) — 同一根因的另一面
- `pkg/services/llm/anthropic.go` - `handleStreamEvent` 的事件形状
- `pkg/services/llm/openai.go:327-357` - 正确的事件形状参照
- `pkg/web/api/handle_convo.go:300-318` - UsageRecord 的唯一写入点
- `pkg/services/runner/runner.go:48` - `CreateUsageRecord` 调用处
