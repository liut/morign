---
title: Agent 循环解耦与工具执行增强
type: refactor
status: active
date: 2026-07-13
origin: docs/brainstorms/2026-07-13-agent-loop-extraction-requirements.md
---

# Agent 循环解耦与工具执行增强

## Summary

抽取三处 Handler 中重复的 Agent 循环为独立的 `AgentLoop`，通过 `iter.Seq2` 输出事件解耦传输层；同步加入工具并行执行（goroutine fan-out）、terminate 提前退出、beforeToolCall/afterToolCall 钩子。新建 `pkg/services/agent` 包，扩展 `ToolExecutor`，重构三个 Handler 为纯事件消费者。

---

## Problem Frame

`handle_convo.go`（SSE）、`handle_platform.go`（平台频道 Channel，如 WeCom/Feishu）、`agent.go`（CLI）各有一套相同的循环模板：调 LLM → 收集 tool calls → 串行执行工具 → 追加结果 → loop。修改循环行为需要改三处，工具执行无并行能力。

需求文档 `docs/brainstorms/2026-07-13-agent-loop-extraction-requirements.md` 已完成范围确认，本计划是其实现规划。

---

## Requirements

- R1. 创建 `pkg/services/agent` 包，`AgentLoop` 封装 LLM 调用 + 工具执行 + 循环迭代（origin R1）
- R2. 通过 `iter.Seq2[*llm.Event, error]` 输出事件，无传输层依赖（origin R2）
- R3. 支持流式（`StreamChat`）和非流式（`Chat`）两种模式（origin R3）
- R4. `maxLoopIterations` 保留为安全网（origin R4）
- R5. 三处 Handler 替换为 AgentLoop 消费（origin R5）
- R6. 多个 tool calls 并发执行（origin R6）
- R7. 工具结果保持 LLM 返回顺序（origin R7）
- R8. `ToolResult.Terminate` 支持整批提前退出（origin R8）
- R9. `BeforeToolCall` 钩子（origin R9）
- R10. `AfterToolCall` 钩子（origin R10）

> R11 `ShouldStopAfterTurn` 钩子已延期至后续实现，详见 [Deferred to Follow-Up Work](#deferred-to-follow-up-work)。

**Origin actors:** A1 (HTTP SSE Handler), A2 (Channel Handler), A3 (CLI Agent), A4 (LLM Provider), A5 (Tool Registry)
**Origin flows:** F1 (Agent 循环流式), F2 (工具并行执行), F3 (terminate 提前退出), F4 (工具钩子拦截)
**Origin acceptance examples:** AE1 (SSE 行为不变), AE2 (两工具并发), AE3 (terminate 退出), AE4 (before hook block), AE5 (after hook 审计)

---

## Scope Boundaries

- AgentMessage 分层抽象
- Steering / Follow-up 运行时消息注入
- 事件体系细化为多类型
- Pluggable transport

### Deferred to Follow-Up Work

- R11 `shouldStopAfterTurn`: 规划阶段确认 Channel handler 场景后单独实现

---

## Context & Research

### Relevant Code and Patterns

- `pkg/web/api/handle_convo.go`: SSE 流式循环（`chatStreamResponseLoop` + `doChatStream`，~100 行循环逻辑）
- `pkg/web/api/handle_platform.go`: Channel 流式循环（`handleStreamingReply` + `doChannelStream`，~70 行循环逻辑）
- `pkg/web/api/agent.go`: CLI 流式循环（`Run` 方法，~50 行循环逻辑）
- `pkg/web/api/tool_executor.go`: `ToolExecutor` 结构体及 `ExecuteToolCallLoop` / `ExecuteToolCalls` 方法
- `pkg/services/llm/event.go`: `Event` 结构体及 `ToolResult` 子结构
- `pkg/services/llm/client.go`: `Client` 接口（`Chat` / `StreamChat`）
- `pkg/services/llm/types.go`: `Message`、`ToolCall`、`ToolDefinition` 类型

### External References

- pi-mono `@mariozechner/pi-agent-core` 的 Agent 类、AgentLoop 引擎、工具并行执行和 terminate 机制作为设计参考

---

## Key Technical Decisions

- **AgentLoop 输出 `iter.Seq2`**: `Run()` 方法返回 `iter.Seq2[*llm.Event, error]`，与 LLM 的 `StreamChat` 保持一致的迭代器语义。调用方通过 `for event, err := range loop.Run(ctx, messages, tools)` 消费，无需 goroutine 桥接或 channel 缓冲策略
- **ToolExecutor 迁入 agent 包**: `ToolExecutor` 从 `pkg/web/api` 迁至 `pkg/services/agent`，消除 services→web 跨层反向依赖。原有位置保留 type alias 或直接删除，调用方更新 import 路径
- **ToolExecutor 零侵入扩展**: 新增 `BeforeToolCall` 和 `AfterToolCall` 为可选 func 字段，`ExecuteToolCalls` 内部在关键点调用。不设置这些字段时行为与当前完全一致
- **并行执行编排**: 在 `ExecuteToolCalls` 内部，用 `[]promise` 模式——预分配结果切片、每个工具调用的 goroutine 写入固定索引位置、`sync.WaitGroup` 等待全部完成。保证 LLM 返回顺序的同时并发执行
- **Terminate 放在 `ToolResult` 上**: 新增 `Terminate bool` 字段到 `llm.ToolResult`。工具通过 `Registry.Invoke` 返回的 `map[string]any` 中可选的 `"terminate"` 键（`bool` 类型）设值。提取协议：若 map 含 `"terminate": true` 则 `ToolResult.Terminate = true`，否则为 `false`。`AgentLoop` 在工具批量执行后检查所有结果是否都设了 terminate
- **非流式循环保留兼容路径**: `ExecuteToolCallLoop` 本次重构中保留以供向后兼容，后续可择机废弃。新增 `AgentLoop.RunNonStreaming` 方法作为非流式场景的首选路径

---

## Open Questions

### Resolved During Planning

- AgentLoop 输出 `iter.Seq2`，调用方通过 `for event, err := range` 消费，无需 goroutine 桥接
- 工具并行上限：默认不设上限，`ExecuteToolCalls` 内部全部并发。后续如需要可加 `maxConcurrency` 参数
- `Terminate` 字段放在 `llm.ToolResult` 上，工具实现方通过返回值设标记
- `handle_platform.go` 流控适配（平台频道 Channel）：`StartStream` → 首次收到 AgentLoop 事件时调用；`AppendStream` → 每次收到 delta 事件时调用；`FinishStream` → AgentLoop 迭代结束时调用。流控状态维护在 Handler 侧，AgentLoop 不感知

### Deferred to Implementation

- `ShouldStopAfterTurn` 钩子的接口签名和生效时机
- 并行执行的 goroutine panic recovery 策略
- `ExecuteToolCallLoop` 的废弃时间表

---

## Output Structure

```
pkg/services/agent/          # 新建
├── agent_loop.go            # AgentLoop 结构体 + Run() / RunNonStreaming()
├── tool_executor.go         # 从 pkg/web/api 迁入: 加钩子字段、并行执行、terminate 检查
├── agent_loop_test.go       # 单元测试

pkg/services/llm/event.go    # 修改: ToolResult 加 Terminate 字段
pkg/web/api/tool_executor.go # 删除: ToolExecutor 已迁至 pkg/services/agent
pkg/web/api/handle_convo.go  # 修改: 循环替换为 AgentLoop
pkg/web/api/handle_platform.go # 修改: 循环替换为 AgentLoop
pkg/web/api/agent.go         # 修改: Run 方法替换为 AgentLoop
```

---

## Implementation Units

### U1. 扩展 ToolExecutor：钩子 + 并行 + terminate

**Goal:** 扩展 `ExecuteToolCalls`：加入 before/after 钩子调用、goroutine 并行执行、及 terminate 标记检测。返回值增加 `allTerminate bool`，现有调用方同步适配。

**Requirements:** R6, R7, R8, R9, R10

**Dependencies:** None

**Files:**
- Create: `pkg/services/agent/tool_executor.go`（从 `pkg/web/api/tool_executor.go` 迁入并扩展）
- Modify: `pkg/services/llm/event.go`（`ToolResult` 加 `Terminate`）
- Modify: `pkg/web/api/tool_executor.go`（替换为 re-export 或删除，调用方改为 import 新位置）

**Approach:**
- `ToolExecutor` 新增两个可选字段：`BeforeToolCall`（`func(ctx, name string, params map[string]any) (block bool, reason string)`）和 `AfterToolCall`（`func(ctx, name string, result map[string]any) map[string]any`）
- `ExecuteToolCalls` 内部重构：预分配切片、每个 tool call 启动 `go func()`（含 `defer wg.Done()` + `defer recover()` 防 panic 死锁）、WaitGroup 等待、按原始索引收集结果
- 结果收集后检查所有 `ToolResult.Terminate`，若全部为 true 则返回 `allTerminate=true`
- 当 `BeforeToolCall` block 工具时，生成的 error `ToolResult` 默认 `Terminate=false`，不阻塞正常 terminate 退出
- `ExecuteToolCalls` 返回值增加 `allTerminate bool`，现有调用方同步适配（共 3 处：`tool_executor.go` 的 `ExecuteToolCallLoop` 内部调用需捕获 `allTerminate`、`handle_convo.go` 的 `executeToolCallLoop`、`agent.go` 的 `Run`）
- 搬迁时需同步处理跨包依赖：`formatToolResult` 从 `handle_convo.go` 迁入本文件，`logger()` 替换为 `slog.Default()`

**Execution note:** 先写单元测试覆盖并发 + 钩子 + terminate 场景，再改实现

**Test scenarios:**
- Happy path: 两个工具调用，goroutine 并发执行，结果按调用顺序返回
- Happy path: BeforeToolCall 返回 block=false，工具正常执行
- Happy path: AfterToolCall 修改结果并返回
- Edge case: BeforeToolCall 返回 block=true，工具不执行，生成错误 tool result
- Edge case: 所有工具返回 terminate=true，allTerminate 标记为 true
- Edge case: 部分工具 terminate，allTerminate 为 false
- Edge case: 无 tool calls 时直接返回空结果

---

### U2. 创建 AgentLoop

**Goal:** 在 `pkg/services/agent` 中创建 `AgentLoop`，封装流式和非流式的完整 Agent 循环逻辑。

**Requirements:** R1, R2, R3, R4

**Dependencies:** U1

**Files:**
- Create: `pkg/services/agent/agent_loop.go`
- Create: `pkg/services/agent/agent_loop_test.go`

**Approach:**
- `AgentLoopConfig` 结构体：`LLM llm.Client`、`ToolExec *ToolExecutor`、`MaxLoop int`、`Stream bool`
- `AgentLoop.Run(ctx, messages, tools) iter.Seq2[*llm.Event, error]`：返回迭代器，调用方通过 `for event, err := range` 消费。内部复用 `StreamChat` 的 `iter.Seq2` 语义，不引入 goroutine 桥接
- 流式模式：调 `StreamChat`，将每个 chunk 包装为 Event 发送；done 时收集 tool calls，调用 `ExecuteToolCalls`，将 tool result event 发送，若 allTerminate 则退出，否则下一轮
- 非流式模式：`RunNonStreaming(ctx, messages, tools) (string, error)`——调 `Chat`，收集 tool calls，执行工具，循环直到无 tool calls
- `maxLoopIterations` 检查保留在循环开头，超限时发一条 error event 然后退出

**Patterns to follow:**
- `pkg/services/runner/runner.go` 的简洁结构体 + option 模式
- `pkg/web/api/agent.go` 的 `Run` 方法循环逻辑

**Test scenarios:**
- Happy path: 无工具调用的单轮对话，事件序列正确
- Happy path: 有工具调用的多轮循环，事件包含 tool result
- Happy path: 非流式模式，返回最终答案
- Edge case: 达到 maxLoop 上限，优雅退出
- Edge case: 工具执行后 allTerminate=true，循环退出不发起新 LLM 调用
- Edge case: context cancel 时迭代正常终止
- Edge case: StreamChat 返回错误时迭代器输出 error 事件然后终止

---

### U3. 迁移 SSE Handler

**Goal:** `handle_convo.go` 的 `chatStreamResponseLoop` + `doChatStream` 替换为 AgentLoop 消费。

**Requirements:** R5

**Dependencies:** U2

**Files:**
- Modify: `pkg/web/api/handle_convo.go`

**Approach:**
- 创建 `AgentLoop`，`for event, err := range loop.Run(ctx, messages, tools)` 消费事件
- delta 事件 → SSE writeEvent
- tool call 事件 → SSE writeEvent（tool_calls 信息）
- tool result 事件 → SSE writeEvent（可选，当前未向客户端发送 tool result）
- 循环结束 → 发送 done 事件 + 持久化（`Runner.Persist`）
- 删除 `chatStreamResponseLoop`、`doChatStream`、`chatResponse` 结构体及相关的 `doChatStream` 内部字段
- `chatRequest` 结构体精简，移除不再需要的字段（`hi`、`chunkIdx`、`prompt` 中可合并的部分）

**Patterns to follow:**
- 保持现有 SSE 事件格式不变（`eventsource.WriteEvent`）
- `writeEvent` 辅助函数保留
- `Runner.Persist` 调用位置从 `chatStreamResponseLoop` 末尾移到 `for range` 循环外

**Test scenarios:**
- 回归：SSE 聊天请求产生的事件序列与重构前一致
- 回归：usage 记录和 history 持久化正常

---

### U4. 迁移 Channel Handler

**Goal:** `handle_platform.go` 的 `handleStreamingReply` + `doChannelStream` 替换为 AgentLoop 消费（Channel 指 WeCom/Feishu 等平台频道，非 Go 语言 channel）。

**Requirements:** R5

**Dependencies:** U2

**Files:**
- Modify: `pkg/web/api/handle_platform.go`

**Approach:**
- 创建 `AgentLoop`，`for event, err := range loop.Run(ctx, messages, tools)` 消费事件
- 首次 delta → 调 `sr.StartStream`
- 后续 delta → 调 `sr.AppendStream`（累加模式，WeCom 覆盖语义）
- 迭代结束 → 调 `sr.FinishStream` + 持久化
- 删除 `handleStreamingReply`、`doChannelStream` 中的循环逻辑
- `buildChatMessagesAndTools` 保留

**Patterns to follow:**
- WeCom 的 AppendStream 累加模式：Handler 侧维护 `contentBuilder`，每次 delta 追加后全量发送
- 错误处理：StreamChat 错误 → `sr.FinishStream` 发送错误消息

**Test scenarios:**
- 回归：Channel 聊天流式行为与重构前一致
- Edge case: LLM 返回错误时 FinishStream 正确发送错误消息

---

### U5. 迁移 CLI Agent

**Goal:** `agent.go` 的 `Run` 方法替换为 AgentLoop 调用。

**Requirements:** R5

**Dependencies:** U2

**Files:**
- Modify: `pkg/web/api/agent.go`

**Approach:**
- `Run` 方法内创建 `AgentLoop`，`for event, err := range agentLoop.Run(ctx, messages, tools)` 消费事件
- delta 事件 → yield(event, nil)
- tool result 事件 → yield(event, nil)
- 迭代结束 → return
- `StreamChat` 方法无需修改——它已包装 `Run`，`Run` 替换为 `AgentLoop.Run` 后自动生效
- `Chat` 方法改为调用 `AgentLoop.RunNonStreaming`

**Test scenarios:**
- 回归：CLI 流式输出与重构前一致
- 回归：非流式 Chat 返回正确的完整答案
