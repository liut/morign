---
title: feat: Event-driven architecture for chat pipeline
type: feat
status: done
date: 2026-05-09
origin: docs/brainstorms/2026-05-09-event-driven-architecture-requirements.md
---

# feat: Event-driven architecture for chat pipeline

## Overview

将当前基于 `<-chan StreamResult` 的流式管道重构为以 `Event` 为核心原语的事件驱动架构。统一 Agent、Tool、Runner、Handler 之间的通信路径，集中持久化逻辑，为后续多 Agent 编排预留扩展点。

## Problem Statement

当前聊天管道存在 5 个结构性问题：

1. **StreamResult 只是传输层 DTO** — LLM 响应、工具结果、状态变更、持久化各自走不同路径
2. **持久化逻辑散落三处** — `handle_convo.go`（3 处：AddHistory+Save、CreateUsageRecord、CreateChatLog）、`handle_platform.go`（2 处：streaming + regular 各自 AddHistory+Save）
3. **状态变更无迹可寻** — 工具结果直接拼入 messages 数组，无法追溯"谁在什么时候做了什么"
4. **channel 作为流式抽象有局限性** — 生产者必须开 goroutine，无法对事件流做组合包装
5. **CLI/Web/Channel 三条路径各自重复 Agent 逻辑** — `Agent`(CLI-only)、`api` 结构体(web-only)、`channelHandler`(channel-only) 各自持有相同的 `llm.Client` + `ToolExecutor`

## Key Decisions (from origin)

- **D1.** 不引入 ADK-Go 的完整 Session 接口，仅定义 `SessionStore`（`MergeDelta`）和 `HistoryStore`（`AppendEvent` + `CreateUsageRecord`）两个最小接口 (see origin)
- **D2.** StateDelta 仅支持 session 级 key，不引入 app/user/temp 多级作用域 (see origin)
- **D3.** Event 直接放在 `pkg/services/llm` 包，与 `Message` 同包 (see origin; StreamResult 已删除)
- **D4.** 字段范围：`ID`/`Timestamp`/`Author`/`Delta`/`Think`/`ToolCalls`/`StopReason`/`Done`/`ToolResult`/`UserID`/`UserPrompt`/`Actions.StateDelta`/`Usage`/`Model`/`MsgCount`/`Meta`/`ResponseID` 实现 (see origin; InvocationID/Error/Branch/TransferToAgent/ArtifactDelta removed per review)
- **D5.** ~~Event 补充 `Usage`/`Model`/`ResponseID`/`Error` 字段以覆盖 StreamResult 的全部语义 (SpecFlow Q1)~~ → 已合并到 D4
- **D6.** StateDelta 是**补充**而非**替代** — 工具结果仍通过 messages 返回给 LLM，StateDelta 仅用于状态性副作用 (SpecFlow Q2)
- **D7.** ~~新增 `SessionStateStore` 接口~~ → 简化为 `SessionStore`（仅 `MergeDelta`），去掉 Get/Set (SpecFlow Q3)
- **D8.** ~~InvocationID 对应一次 Agent.Run() 调用~~ → InvocationID 已删除（零读取），当前 SessionID 标识对话、Event.ID 标识单事件 (SpecFlow Q4)
- **D9.** Runner 放在新包 `pkg/services/runner`，避免循环依赖 (SpecFlow Q7)

## Technical Approach

### Architecture

```
Handler (SSE / Channel)
    │ 直接调用 llm.StreamChat() 消费 iter.Seq2
    │ 通过 runner.Persist() 手动持久化
    ▼
Agent (LLM 调用 + Tool 执行循环)
    │ 实现 Run() → iter.Seq2[*Event, error]
    │ 内部: llm.StreamChat + toolExec.ExecuteToolCalls
    ▼
Runner (统一持久化入口)
    │ Persist(ctx, sessionID, event) → AppendEvent + MergeDelta + CreateUsageRecord
    ▼
stores/event_adapter (Runner 接口 → 现有 stores 适配)
```

**Event 结构**（`pkg/services/llm/event.go`）：

```go
type Event struct {
    ID        string
    Timestamp time.Time
    Author    string        // "user" | "assistant" | toolName

    Delta      string
    Think      string
    ToolCalls  []ToolCall
    StopReason FinishReason
    UserID     string
    UserPrompt string
    Done       bool

    Usage    *Usage
    Model    string
    MsgCount int
    Meta     map[string]any
    ResponseID string

    ToolResult *ToolResult
    Actions    EventActions
}

type ToolResult struct {
    CallID  string
    Name    string
    Content string
}

type EventActions struct {
    StateDelta map[string]any   // session 级状态增量
}
```

**Pusher**（流式事件推送回调）：

```go
type Pusher func(*Event, error) bool
```

**Runner**（`pkg/services/runner/runner.go`，60 行）：

```go
type Runner struct {
    sessionStore SessionStore
    historyStore HistoryStore
}

func (r *Runner) Persist(ctx context.Context, sessionID string, event *llm.Event) error
```

**接口**（`pkg/services/runner/runner.go`）：

```go
type SessionStore interface {
    MergeDelta(ctx context.Context, sessionID string, delta map[string]any) error
}

type HistoryStore interface {
    AppendEvent(ctx context.Context, sessionID string, event *llm.Event) error
    CreateUsageRecord(ctx context.Context, sessionID string, event *llm.Event) error
}
```

### Implementation Phases

#### Phase 1: Event 类型定义 + LLM Client 适配 ✅

**目标**：定义 Event 类型，将 `StreamChat` 的返回从 `<-chan StreamResult` 改为 `iter.Seq2[*Event, error]`。

**变更文件**：
- `pkg/services/llm/event.go` — 新增 `Event`、`EventActions`、`ToolResult`、`Pusher` 类型
- `pkg/services/llm/client.go` — `Client` 接口 `StreamChat` 签名改为 `iter.Seq2[*Event, error]`
- `pkg/services/llm/openai.go` — `StreamChat` 消除 goroutine+channel，改用 `Pusher` 同步推送
- `pkg/services/llm/anthropic.go` — 同上，`parseStreamResponse`/`handleStreamEvent` 直接构造 `*Event`
- `pkg/services/llm/types.go` — 删除 `StreamResult` 类型

**实际实现调整**：
- 未新增 `Run()` 方法，直接升级 `StreamChat` 签名（用户要求）
- `Pusher` 类型替代 `chan StreamResult`，`parseStreamResponse` 同步调用
- Event 字段从 `Partial` 改为 `Done`（审查建议：零值安全）
- 删除了规划中的预留字段 `Branch`/`Error`/`TransferToAgent`/`ArtifactDelta`（审查建议：YAGNI）

**验收**：
- [x] `Event` 类型包含全部实现字段
- [x] `StreamChat` 返回 `iter.Seq2[*Event, error]`
- [x] 现有测试更新并通过

#### Phase 2: Runner 实现 + 统一持久化 ✅

**目标**：创建 `pkg/services/runner` 包，实现 Runner 统一持久化入口。

**变更文件**：
- `pkg/services/runner/runner.go` — `Runner` 结构体 + `Persist()` 方法 + `SessionStore`/`HistoryStore` 接口
- `pkg/services/stores/event_adapter.go` — `HistoryStore`/`SessionStore` 的适配实现

**实际实现调整**：
- 简化为只保留 `Persist()`（`Runner.Run()` 未实现 —— 审查发现 handler 各有不同的输出格式，统一循环不合适）
- `SessionStore` 从 `Get/Set/MergeDelta` 简化为只有 `MergeDelta`（当前无 Get/Set 消费方）
- `CreateUsageRecord` 参数从 `runner.UsageRecord` 改为直接接收 `*llm.Event`（消除冗余中间结构）
- 接口定义放在 `runner/runner.go`（非 `stores/interfaces.go`）

**验收**：
- [x] Runner 实现持久化流程（AppendEvent + MergeDelta + CreateUsageRecord）
- [x] HistoryStore/SessionStore 通过 event_adapter 适配现有 stores
- [x] 错误通过 `errors.Join` 收集返回

#### Phase 3: Agent 重构 + ToolExecutor 迁移 ✅

**目标**：重构 `Agent` 使用 `iter.Seq2`，ToolExecutor 通过 Event 返回结果。

**变更文件**：
- `pkg/web/api/agent.go` — `Agent.Run()` 返回 `iter.Seq2[*Event, error]`；`Chat()` 使用非流式 `llm.Chat()`；`StreamChat()` 通过 StreamCallbacks 消费 `Run()`
- `pkg/web/api/tool_executor.go` — `ExecuteToolCalls` 返回 `([]*Event, []Message)`，Event 携带 `ToolResult`

**实际实现调整**：
- `Agent.Chat()` 使用 `llm.Chat()` + `ExecuteToolCallLoop`（非流式路径，避免浪费）
- `Agent.StreamChat()` 通过 `Run()` + `StreamCallbacks` 实现（CLI 终端输出）
- `ExecuteToolCalls` 返回 `len(evs)==0` 替代原 `hasToolCall` bool（语义等价）
- 工具结果尚未写入 StateDelta（等待实际需求驱动）

**验收**：
- [x] Agent.Run() 返回 iter.Seq2
- [x] 工具调用结果通过 Event.ToolResult 传递
- [x] CLI agent 功能无回归

#### Phase 4: Handler 适配 ✅

**目标**：Web API 和 Channel Handler 用 `runner.Persist()` 替代直接持久化调用。

**变更文件**：
- `pkg/web/api/handle_convo.go` — `chatStreamResponseLoop`/`doChatStream` 用 `runner.Persist()` 替代 `AddHistory`/`Save`/`CreateUsageRecord`
- `pkg/web/api/handle_platform.go` — `handleStreamingReply`/`handleRegularReply` 同上
- `pkg/web/api/api.go` — `api` 结构体持有 `*runner.Runner`

**实际实现调整**：
- Handler 保持自己的工具调用循环（各自输出格式不同：SSE/Channel Stream/CLI）
- `runner.Persist()` 用于手动持久化点（历史、用量）
- 删除了 `gatherUsage`、手动 `AddHistory+Save`、手动 `CreateUsageRecord` 调用
- 补充了曾丢失的 `UserID`/`UserPrompt`/`MsgCount`/`Meta` 信息

**验收**：
- [x] `/api/chat` SSE 响应格式不变
- [x] Channel webhook 行为不变
- [x] Handler 代码中不再出现 `AddHistory`/`Save`/`CreateUsageRecord` 调用
- [x] 无 goroutine 泄漏（消除了 goroutine+channel 模式）

#### Phase 5: 清理与测试 ✅

**目标**：清理废弃代码，补充集成测试，修复回归 bug。

**变更文件**：
- `pkg/services/llm/types.go` — 删除 `StreamResult` 类型
- `pkg/services/llm/types_test.go` — 删除 `TestStreamResultString`
- `pkg/services/stores/integration_test.go` — 新增 `event_adapter` 集成测试

**Bug 修复（Phase 5 期间）**：
- `message_start` Partial 未设导致 panic（→ 改为 `Done` 零值安全）
- think 内容不输出（`isEmpty` check 漏了 `Think` 字段）
- `hasToolCall` 检查被删导致死循环（改用 `len(evs)==0`）
- `gatherUsage` 信息丢失（`MsgCount`/`Meta`/`UserPrompt` 在 Persist 中恢复）
- `anthropic.go`/`openai.go` 去 channel 时保留原有注释

**验收**：
- [x] `make vet lint` 通过（0 告警）
- [x] `make test-models` 通过
- [x] `make test-stores` 通过（含 event_adapter 集成测试）

### Persistence Contract (AppendEvent)

```
AppendEvent(ctx, sessionID, event):
  1. 将 event 转换为 aigc.HistoryItem:
     - Role=user 的 event → HistoryItem.ChatItem.User
     - Role=assistant 的 event → HistoryItem.ChatItem.Assistant + Think
  2. 写入 Redis: RPUSH convs-<sessionID> <json>
  3. 如果有 StateDelta → MergeDelta(ctx, sessionID, event.Actions.StateDelta)
     - MergeDelta 写入 convo_session.meta jsonb 列
     - key 冲突时后者覆盖前者
  4. 如果有 Usage → CreateUsageRecord
  5. (future) 可选写入 convo_message 表（PostgreSQL）
```

## System-Wide Impact

### Interaction Graph

```
POST /api/chat
  → postChat (handler)
    → prepareChatRequest (构建 messages + tools)
    → chatStreamResponseLoop
      → doChatStream → llm.StreamChat (iter.Seq2)
        → openAIProvider/anthropicProvider: SSE parse → push(*Event)
      → 每个 Event → SSE chunk → writeEvent
      → 工具调用: toolExec.ExecuteToolCalls → []Event (含 ToolResult)
      → runner.Persist(event) → AppendEvent + MergeDelta + CreateUsageRecord
    → GetHistorySummary → title
```

### Error Propagation

| 层级 | 错误类型 | 处理方式 |
|------|---------|---------|
| LLM API | HTTP error / timeout | `yield(nil, err)` → Handler 展示错误 |
| Tool invoke | tool not found / exec fail | `Event{Author: toolName, Content: error text}` → 作为 tool result 返回 LLM |
| Redis write | connection error | Runner 日志告警，不阻断事件流 |
| Context cancel | client disconnect | iter.Seq2 在下次 yield 前检查 `ctx.Done()`，停止迭代 |

### State Lifecycle Risks

- **StateDelta 与 messages 不一致**：工具先写 StateDelta（Set user preference），但后续 LLM 调用失败。此时 StateDelta 已在 Redis session.meta 中，但对话未完成。**缓解**：StateDelta 在 AppendEvent 时与 HistoryItem 同事务写入，只有完整 Event 才写 StateDelta
- **Runner 中途崩溃**：已持久化的事件在 Redis 中，未持久化的丢失。与当前行为一致（当前也是逐个 AddHistory）
- **StateDelta 合并竞争**：并行 Agent 场景（未来）可能引发 key 覆盖。当前单 Agent 无此问题

### API Surface Parity

| 路径 | 当前 | 重构后 |
|------|------|--------|
| POST /api/chat | 直接调 llm.StreamChat | 通过 runner.Run() |
| POST /api/chat-sse | 同上 | 同上 |
| Channel WeCom | channelHandler.handleStreamingReply | 通过 runner.Run() |
| Channel Feishu | channelHandler.handleRegularReply | 通过 runner.Run() |
| CLI agent | Agent.StreamChat | 通过 Agent.Run() |
| GET /api/history/{cid} | ListHistory from Redis | 不变 |

### Integration Test Scenarios

1. **正常流式对话**：User Message → 3 个 delta Event → 1 个 done Event → 验证 Redis history + UsageRecord
2. **工具调用链**：User Message → delta + tool_call Event → tool result Event → 第二轮 delta → done → 验证 messages 列表正确、StateDelta 已合并
3. **客户端断开**：发送 2 个 delta → 关闭连接 → 验证 iter.Seq2 停止、无 goroutine 泄漏、已发事件已持久化
4. **LLM 错误恢复**：LLM 返回错误 Event → 验证 Runner 持久化错误事件 → Handler 展示错误给用户
5. **StateDelta 合并**：工具写入 `{"last_query": "..."}` → 验证 `sessionStore.Get(sessionID, "last_query")` 返回正确值

## Acceptance Criteria

### Functional Requirements
- [x] R1. `Event` 类型定义在 `pkg/services/llm`，含全部实现字段
- [x] R2. `Agent.Run()` 返回 `iter.Seq2[*Event, error]`；`Runner.Persist()` 为持久化入口
- [x] R3. Handler 中无直接 `AddHistory`/`Save`/`CreateUsageRecord` 调用（统一通过 `Runner.Persist()`）
- [ ] R4. 工具可通过 `Event.Actions.StateDelta` 写入状态（接口就绪，待实际需求驱动）
- [x] R5. `Event.Done` 标记流结束（零值安全，中间事件无需显式设置）
- [x] R6. `/api/chat` SSE 和 Channel webhook 对外行为不变

### Non-Functional Requirements
- [x] 无 goroutine 泄漏（消除 goroutine+channel 模式）
- [x] `make vet lint` 通过（0 告警）
- [x] 现有测试全部通过
- [x] event_adapter 集成测试覆盖

## Dependencies & Risks

| 依赖/风险 | 影响 | 缓解 |
|----------|------|------|
| Go 1.25 `iter.Seq2` 稳定 | 低 — 1.23+ 已稳定 | 已在 go.mod 中确认 1.25 |
| Redis history 路径保持不变 | 确保 ListHistory 兼容 | AppendEvent 写同一 Redis key 格式 |
| `SessionStateStore` 避免与 OAuth `StateStore` 命名冲突 | 关键 — 见 SpecFlow Q3 | 使用独立接口名 `SessionStateStore` |
| `convo.Message` 表当前未使用 | 低 — 后续单独迁移 | 本次不涉及，保留现状 |
| 无 goroutine 泄漏 | 高 — 当前 channel 模式有泄漏风险 | iter.Seq2 同步模型天然消除 |

## Sources & References

### Origin
- [docs/brainstorms/2026-05-09-event-driven-architecture-requirements.md](../brainstorms/2026-05-09-event-driven-architecture-requirements.md) — 需求文档
  - 携带决策：D1-D9（字段范围、包位置、StateDelta 语义、Runner 架构）

### Internal References
- `pkg/services/llm/types.go:103` — 当前 StreamResult 定义
- `pkg/web/api/tool_executor.go:47` — ToolExecutor.ExecuteToolCalls 当前实现
- `pkg/web/api/handle_convo.go:334-500` — 当前流式响应循环 + 持久化
- `pkg/web/api/handle_platform.go:157-345` — Channel streaming + regular handler
- `pkg/services/stores/conversation.go:131` — Conversation.Save 实现

### Institutional Learnings
- [executeToolCallLoop-deduplication.md](../solutions/logic-errors/executeToolCallLoop-deduplication.md) — ToolExecutor 提取模式，Runner 的直接前身
- [streaming-reply-multiple-startstream.md](../solutions/runtime-errors/streaming-reply-multiple-startstream.md) — 流式生命周期规则：Start/Finish 必须由单层管理

### External References
- [Go 1.23 iter.Seq2 文档](https://pkg.go.dev/iter#Seq2)
- ADK-Go session/event 模型: `google.golang.org/adk/session`
