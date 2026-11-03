---
title: fix: 工具调用结果完整性
type: fix
status: completed
date: 2026-10-29
origin: docs/reports/2026-10-27-whip-design-review.md
---

# fix: 工具调用结果完整性

## Summary

`ToolExecutor.ExecuteToolCalls` 在四条错误路径上留下空结果：assistant 消息仍带 `tool_calls`，消息列表却没有对应的 tool result。下一轮请求因此携带孤立 `tool_use`，被 provider 直接拒绝（400），会话中断。本计划让每个 `tool_call` 恰好产生一个结果，并把超大结果从"只留头部"改为头尾保留。

改动集中在 `pkg/services/agent/tool_executor.go` 与 `pkg/services/llm/event.go`，不触碰 `docs/*.yaml` 契约，不触发 `make codegen`。

---

## Problem Frame

`pkg/services/agent/tool_executor.go` 的 `ExecuteToolCalls` 为每个 tool call 启一个 goroutine，用 `results[idx]` 收集结果。收集阶段跳过 `nil` 项：

```go
for i, ev := range results {
    if ev == nil {
        allTerminate = false
        continue          // ← 没有对应的 tool message
    }
    ...
    messages = append(messages, llm.Message{Role: llm.RoleTool, ...})
}
```

跳过 `nil` 项会让后续 tool message 相对 `tool_calls` 前移，顺序与配对同时失真 —— 这正是 R3 要求按调用顺序组装的原因。

而 goroutine 内部有四条路径在写入 `results[idx]` 之前 `return`：

| 路径 | 位置 | 触发条件 |
| --- | --- | --- |
| 类型不是 `function` | `tool_executor.go:97` | provider 返回非 function 类型 |
| 参数 JSON 解析失败 | `tool_executor.go:104` | 模型输出的 arguments 非法 |
| `Registry.Invoke` 返回 error | `tool_executor.go:132` | 底层调用失败 |
| goroutine panic | `tool_executor.go:89` | 工具内部 panic |

panic 路径尤其隐蔽：`recover()` 只记日志，`results[idx]` 保持 `nil`。

**后果不是"模型停滞"，而是请求被拒。** Anthropic 要求每个 `tool_use` 块紧跟一个 `tool_result`；`pkg/services/llm/anthropic.go:520-560` 只做单向消息转换，不校验配对。孤儿 `tool_use` 进入下一轮请求时，Anthropic 与 OpenAI 都会以 `invalid_request_error` 拒绝。`AgentLoop.Run`（`agent_loop.go:85`）在收集后继续下一轮 LLM 调用，于是整轮会话在该点终止。

第二个问题在 `truncateToolResult`（`tool_executor.go:240`）：超限时用 `words.TakeHead` 只保留头部，错误尾部、引用信息与调用结论常常正好落在被丢弃的部分。工具结果从不落历史、也不推送给客户端，只活在当轮 messages 里，被截断后无法补救。

---

## Requirements

- R1. 每个 `tool_call` 产生且只产生一个结果。参数解析失败、类型不符、panic、执行错误四条路径全部转成结构化错误结果，不再留空。工具不存在沿用 `Registry.Invoke` 既有的 `tool not found` 结果，不在本计划范围。
- R2. 结果携带明确的状态与错误码，区别"工具自身返回的业务错误"（如 `permission denied`）与"调用未能完成"（如 panic）。
- R3. 批次内并发执行不变，消息组装按调用顺序（`tool_calls` 顺序与 tool message 顺序一致）。
- R4. 超限结果保留头部与尾部，总长度（含省略说明）不超过 `ToolResultMaxChars`。
- R5. `BeforeToolCall` 拒绝的调用产出 `rejected` 状态结果，其理由文本对模型可见。
- R6. 不改变工具调用的可见集合、执行顺序与 `terminate` 语义。
- R7. 删除 `ToolExecutor.ExecuteToolCallLoop`（死代码），使工具执行只有一处实现。
- R8. 有回归测试覆盖四条错误路径；`make vet` 与 `make lint` 通过。

---

## Scope Boundaries

### In Scope

- `pkg/services/agent/tool_executor.go`：结果收集与错误归一、截断策略；删除 `ExecuteToolCallLoop`
- `pkg/services/llm/event.go`：`ToolResult` 增加状态与错误码字段
- `docs/WORKFLOWS.md`：删除 `ExecuteToolCallLoop` 后同步两处流程图引用
- 对应单元测试

### Out of Scope

- **结果 handle 与分页读取**：需要 PG 持久化载体，属 [计划 004](2026-10-29-004-feat-conversation-persistence-basis-plan.md)
- **执行侧授权门控**：见 [计划 002](2026-10-29-002-feat-effective-tool-policy-plan.md)
- **工具输出的 schema 校验**：不在本计划
- **子 agent 的结果契约**：子 agent 尚在规划阶段，未有实现，不在依赖范围内

### Deferred to Follow-Up Work

无。

---

## Context & Research

### Relevant Code and Patterns

- `pkg/services/agent/tool_executor.go`：唯一的工具执行出口，内置工具、MCP 工具、频道工具都经过 `formatToolResult`
- `pkg/services/llm/event.go:48`：`ToolResult{CallID, Name, Content, Terminate}`，是事件流里唯一的工具结果载体
- `pkg/models/mcps/tool.go:56`：`BuildToolErrorResult` 已产出 `isError` 结果，是"工具业务错误"的既有表达，本计划沿用
- `pkg/utils/words`：`TakeHead` 与 `TakeTail` 均可用（`TakeTail` 已在 `pkg/web/api/handle_convo.go` 使用），头尾截断有现成惯例
- `pkg/services/agent/tool_executor_test.go`：表驱动测试写法参考

### Institutional Learnings

- `docs/solutions/logic-errors/executeToolCallLoop-deduplication.md` 记录过一次"两份近似实现各自演化"的教训。本次删除死代码是该教训的直接延续。

### External References

无。改动方向由 provider 的 tool_use/tool_result 配对要求决定，无取舍空间。

---

## Key Technical Decisions

### 统一结果契约

`llm.ToolResult` 扩展两个字段：

```go
type ToolResult struct {
    CallID    string
    Name      string
    Content   string
    Terminate bool
    Status    string // "ok" | "error" | "rejected"
    ErrorCode string // 仅 Status != "ok" 时有值
}
```

`Status` 的取值与含义：

| Status | ErrorCode | 场景 | Content |
| --- | --- | --- | --- |
| `ok` | — | 工具正常返回 | 工具结果文本 |
| `error` | `invalid_arguments` | 参数 JSON 解析失败 | 可行动的错误说明 |
| `error` | `unsupported_type` | tool call 类型非 `function` | 可行动的错误说明 |
| `error` | `panic` | 工具内部 panic | 工具名 + panic 摘要 |
| `error` | `execution_failed` | `Registry.Invoke` 返回 error | 错误摘要 |
| `error` | `tool_error` | 工具自身返回业务错误（结果含 `isError=true`） | 工具错误原文 |
| `error` | `permission_denied` | 策略门控拒绝（来源：[计划 002](2026-10-29-002-feat-effective-tool-policy-plan.md)） | 拒绝理由原文 |
| `rejected` | `rejected` | `BeforeToolCall` 拒绝 | 拒绝理由原文 |

`rejected` 独立于 `error`：它表示调用被前置策略拒绝而非执行失败，下游统计（[计划 004](2026-10-29-004-feat-conversation-persistence-basis-plan.md) 的 `ToolRecord.Status`）需单独识别，不得计入执行失败。

### 错误归一的位置

错误结果的构造收敛到一个函数，四条 `return` 路径改为"设置结果后返回"，而不是"什么都不做就返回"。panic 路径在 `recover()` 内构造错误结果 —— 这是本计划最关键的一行改动。

该函数通过 `status` 参数区分 `error` 与 `rejected`，因此错误路径与 `BeforeToolCall` 拒绝共用同一个出口，不再需要第二个构造函数。

**工具自身的业务错误**：`Registry.Invoke` 对工具自报失败（如 `permission denied`）返回的是 `error` 为 `nil`、`isError=true` 的结果——它走的是"成功"分支。执行层必须先检查结果中的 `isError` 标记，命中则写 `Status="error"` / `ErrorCode="tool_error"`，否则写 `ok`。否则这类失败会以 `ok` 落库、按 `Status` 检索也查不到。

`Registry.Invoke` 的既有约定是：找不到工具时返回 `BuildToolErrorResult("tool not found")` 且 `error` 为 `nil`（`pkg/services/tools/registry.go:128`）。这条路径**已经**产出结果，不是本计划的修复对象；执行层按 `ok` 处理，本计划不改 `Registry` 的返回约定。

### 上下文取消与超时

本计划不引入独立的工具超时配置。`Registry.Invoke` 接收调用方传入的 ctx，调用方的 deadline 或取消会以 error 形式返回，走 `execution_failed` 路径。超时语义由此免费获得，无需新配置项。

### 截断策略

`truncateToolResult` 从"只留头"改为"留头 + 留尾"，总字符上限仍为 `settings.Current.ToolResultMaxChars`（默认 20000）。

**上限包含省略说明本身**：头部取上限的 3/4，尾部取 1/4，二者相加再加省略说明，整体不超过 `ToolResultMaxChars`。头尾比例取 3:1 —— 头部保留结论铺垫，尾部保留错误详情与引用信息。

与现状的差异：现状是"上限 + 省略说明"（实际长度略超上限），本计划改为"省略说明计入上限"，因此正文载荷比现状少约一行说明的长度。

---

## Open Questions

### Resolved During Planning

- **是否引入独立的工具超时配置？** 否。ctx 取消与 deadline 已覆盖，且不新增配置项符合项目惯例。
- **`ToolResult` 新字段是否会漏到客户端？** 不会。`handle_convo.go:217` 与 `handle_platform.go:226` 都在 `event.ToolResult != nil` 时 `continue`，工具结果事件从不发给客户端。
- **是否删除 `ExecuteToolCallLoop`？** 是。已确认为本计划范围内（U4）。

### Deferred to Implementation

- 错误结果文本的具体措辞，在实现时对齐现有工具错误的可读性标准。

### From 2026-11-02 review

- **Status/ErrorCode 是否随 tool message 发给 provider** — Key Technical Decisions / 统一结果契约 (P1, adversarial, confidence 75)

  本计划给 `llm.ToolResult` 增加了 `Status` 与 `ErrorCode`，但真正发给 provider 的是 `llm.Message{Role: RoleTool, Content: ev.ToolResult.Content}`——新字段不随消息走。对 Anthropic 而言 `tool_result` 块不带 `is_error` 标记，模型无法像今天通过 `BuildToolErrorResult` 的 `isError` 那样区分工具自报失败与成功调用。计划需要说明：模型的 tool message 是否要带机器可读的错误标记，还是 `Status` 只用于内部日志与落库。

  <!-- dedup-key: section="key technical decisions 统一结果契约" title="statuserrorcode 是否随 tool message 发给 provider" evidence="R2. 结果携带明确的状态与错误码，区别\"工具自身返回的业务错误\"（如 `permission denied`）与\"调用未能完成\"（如 panic）。" -->


---

## High-Level Technical Design

```go
// 错误归一后的单一出口
func toolErrorResult(callID string, name string, status string, code string, detail string) *llm.Event {
    return &llm.Event{
        ID:        llm.NewEventID(),
        Timestamp: time.Now(),
        Author:    name,
        ToolResult: &llm.ToolResult{
            CallID:    callID,
            Name:      name,
            Content:   formatToolError(name, code, detail),
            Status:    status,
            ErrorCode: code,
        },
    }
}
```

goroutine 内的处理顺序：

```
defer wg.Done() → defer recover() → 写 toolErrorResult(panic) → 返回
  ↑ 结果写入必须早于 wg.Done()，否则 wg.Wait() 可能先返回、收集阶段仍会读到空槽位
  ↓
type != "function" → 写 toolErrorResult(unsupported_type)
  ↓
参数解析失败 → 写 toolErrorResult(invalid_arguments)
  ↓
BeforeToolCall 拒绝 → 写 rejected 结果
  ↓
Invoke 返回 error → 写 toolErrorResult(execution_failed)
  ↓
成功 → 写 ok 结果
```

收集阶段不再有 `nil`：`results` 长度恒等于 `toolCalls` 长度，每项非空。

---

## Implementation Units

### U1. 扩展工具结果契约

在 `pkg/services/llm/event.go` 的 `ToolResult` 上增加 `Status` 与 `ErrorCode` 字段并附注释说明取值。确认 `llm.NewEventID` 存在且已被现有代码使用。

验收：`pkg/services/llm` 编译通过，无既有调用点需要改动。

### U2. 错误路径归一

在 `pkg/services/agent/tool_executor.go` 中：

1. 新增错误结果构造函数与错误文本格式化函数
2. 四条 `return` 路径改为写入结果后返回
3. panic 的 `recover()` 内写入 `panic` 错误结果；该写入必须发生在 goroutine 的 `wg.Done()` 之前（`defer wg.Done()` 先注册、`recover` 的 defer 后注册）
4. `BeforeToolCall` 分支写入 `rejected` 状态
5. 成功路径先检查结果中的 `isError` 标记：命中写 `Status="error"` / `ErrorCode="tool_error"`，否则写 `ok`
6. 收集阶段移除 `nil` 跳过分支（保留防御性断言即可）
7. 所有错误结果构造均传入 `tc.ID` 作为 `CallID`，与成功路径、`BeforeToolCall` 路径保持一致

验收：四条错误路径各自产生恰好一个 tool message，`tool_calls` 与 tool message 一一配对且顺序一致。

### U3. 头尾截断

改写 `truncateToolResult`：超限时取头部 3/4 与尾部 1/4（均以 `ToolResultMaxChars` 为基数），中间插入省略说明，说明总长度与已展示区间；省略说明计入上限。`words.TakeHead` / `words.TakeTail` 按 rune 计数，与现状一致。

验收：超限结果的文本同时包含首尾内容，且总长度（含省略说明）不超过 `ToolResultMaxChars`。

### U4. 删除死代码

删除 `ToolExecutor.ExecuteToolCallLoop`（`tool_executor.go:40`）。该方法全仓无调用点，且与 `ExecuteToolCalls` 重复同一套工具执行逻辑——本计划改动的是后者的结果收集逻辑，两处并存会形成两个会分叉的实现。

`docs/solutions/logic-errors/executeToolCallLoop-deduplication.md` 记录过"两份近似实现各自演化"导致行为不一致的教训，本次删除是该教训的直接延续。

验收：全仓（含根目录 `main.go`）无对该方法的引用；`go build ./...` 通过；`docs/WORKFLOWS.md` 中引用该方法的两处流程图同步更新为 `AgentLoop.Run` / `AgentLoop.RunNonStreaming`。

### U5. 回归测试

在 `pkg/services/agent/tool_executor_test.go` 补表驱动测试，并同步更新固化旧行为的既有用例：

| 既有用例 | 需更新的断言 |
| --- | --- |
| `TestExecuteToolCallsInvokeError` | 失败调用现在也产生一个结果，期望 1 个事件改为 2 个，其中 1 个为 `execution_failed` |
| `TestExecuteToolCallsNonFunctionType` | 非 function 类型现在产生 1 个结果，期望 0 个改为 1 个 `unsupported_type` |
| `TestFormatToolResultTruncation` | 断言的"只保留头部"前缀与旧截断说明文案改为新的头尾截断形态 |
| `TestExecuteToolCallsTruncatesToolResult` | 同上 |

| 用例 | 断言 |
| --- | --- |
| 参数 JSON 非法 | 产生一个 `invalid_arguments` 结果，tool message 数量等于 tool call 数量 |
| tool call 类型非 function | 产生一个 `unsupported_type` 结果 |
| invoker 返回 error | 产生一个 `execution_failed` 结果 |
| invoker panic | 产生一个 `panic` 结果，且同批其他调用结果不受影响 |
| `BeforeToolCall` 拒绝 | 产生一个 `rejected` 结果，Content 含拒绝理由 |
| 混合成功与失败 | 消息顺序与 `tool_calls` 顺序一致，长度相等 |
| 超长结果 | 同时含首尾内容，长度不超上限 |

---

## System-Wide Impact

| Area | Impact |
| --- | --- |
| `pkg/services/agent` | `tool_executor.go` 行为变更；`ExecuteToolCallLoop` 死代码删除 |
| `pkg/services/llm` | `ToolResult` 增加两个字段，无既有调用点改动 |
| Web / 频道入口 | 无改动。工具结果事件本就不下发客户端 |
| 数据契约 | 无变更，不触发 `make codegen` |
| 外部行为 | 原本会 400 的会话现在能继续；工具错误的可见文本措辞变化 |

---

## Risks & Dependencies

| Risk | Mitigation |
| --- | --- |
| panic 错误结果本身可能再次 panic | 错误结果构造只用字符串拼接，不触碰出错工具的入参结构 |
| 头尾截断可能把中段关键信息丢掉 | 3:1 比例保留结论与错误详情；总预算不变，不会加剧上下文压力 |
| 补齐结果后模型看到更多错误信息，可能反复重试同一工具 | 错误文本包含可行动信息（期望的参数形状、工具名），不是裸栈信息 |
| 删除 `ExecuteToolCallLoop` 影响外部调用方 | 已确认全仓无调用点（含根目录 `main.go`）；`docs/WORKFLOWS.md` 的两处流程图引用同步更新 |

---

## Documentation / Operational Notes

- 无新增配置项，无新增环境变量
- 工具错误会以结构化事件出现在 agent 循环日志中，可按 `ErrorCode` 检索

---

## Sources & References

- `docs/reports/2026-10-27-whip-design-review.md` 第 1 节「工具结果完整性」
- `pkg/services/agent/tool_executor.go`
- `pkg/services/llm/anthropic.go`（消息转换，确认配对要求由 provider 强制）
- `pkg/services/llm/event.go`
- [计划 002](2026-10-29-002-feat-effective-tool-policy-plan.md)（执行侧授权门控）
- [计划 004](2026-10-29-004-feat-conversation-persistence-basis-plan.md)（结果 handle 与持久化）
