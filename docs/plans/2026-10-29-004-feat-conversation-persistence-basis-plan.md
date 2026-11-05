---
title: feat: 原始对话持久化与上下文投影
type: feat
status: active
date: 2026-10-29
origin: docs/reports/2026-10-27-whip-design-review.md
---

# feat: 原始对话持久化与上下文投影

## Summary

当前对话历史只活在 Redis 里：25 条上限、24 小时 TTL，且只保存用户问题与最终助手文本——工具调用轨迹完全丢失。本计划把原始记录（turn / message / tool call / tool result）持久化到 PG，模型上下文从原始记录投影生成，并让超出内联上限的工具结果可以通过 handle 重新读取。

改动涉及 `docs/convo.yaml` 契约、`make codegen`、一条 PG 迁移 SQL，以及 `pkg/services/stores` 与 `pkg/services/agent` 的写入路径。

---

## Problem Frame

### 原始记录与模型上下文混为一谈

`pkg/services/stores/conversation.go` 把对话历史存进 Redis：

```go
historyMaxLength = 25
historyLifetimeS = time.Second * 86400
```

每个 `HistoryItem` 只保留 `User` 与 `Assistant` 两段文本（`pkg/models/aigc/history.go:11`）。`historyAdapter.AppendEvent`（`pkg/services/stores/event_adapter.go:29`）在落库时只取 `event.Delta` 与 `event.Think`。

后果有三层：

1. **工具调用轨迹不可查。** 一次回答用了哪些工具、返回了什么、中间失败在哪，全部无法回答。事后排查、行为分析、成本归因都失去依据。
2. **24 小时后记忆消失。** 企业微信 / 飞书场景下，用户隔天回来追问"上次那个方案"，模型完全没有上下文。
3. **上下文视图无法重建。** 因为原始记录里没有工具调用，下一轮的上下文只能靠"上一轮的最终文本"拼，无法精确还原"模型当时看到了什么"。

### 上下文预算不是真实 token 预算

`pkg/models/aigc/history.go:28` 的 token 计算：

```go
// TODO: calculate tokens.
c += len(z.ChatItem.User) + len(z.ChatItem.Assistant)
```

统计的是**字节数**。`pkg/web/api/convo_basic.go:4` 用 `historyLimitToken = 50 * 1024` 做预算筛选。字节数与 token 数在中文场景下差 2–3 倍，这个预算既不准确也不可解释。

而真正的上下文压力源不在历史，在**工具结果累积**：

- 单条工具结果上限 20000 字符（`settings.Current.ToolResultMaxChars`）
- 每轮可有多个工具调用，`MaxLoopIterations` 默认 12
- 工具结果进入 messages 后**逐轮累积**，每轮 LLM 请求都重发全部历史

一次异常巨大的 MCP 返回（如抓取整页 HTML）会持续占用后续每次 API 调用的上下文。而工具结果**从不落历史、也不推送给客户端**，截断后无法补救。

### 已就位但未使用的设施

`docs/convo.yaml` 里已有 `Message` 模型（`convo_message`），但 store 声明是 `LGD`——**没有 Create**，全仓找不到写入调用点。表存在，路径缺失。

---

## Requirements

- R1. 原始记录持久化到 PG：turn、message、tool call、tool result 全部可查。
- R2. 每轮对话的完整轨迹可还原：模型请求、工具调用、工具结果、错误。
- R3. 模型上下文从原始记录投影生成，而非直接从 Redis 的扁平文本拼接。
- R4. 超出内联上限的工具结果保存原文，模型收到带 handle 的预览，并能通过工具重新读取完整内容。
- R5. 上下文预算以 provider 返回的真实 usage 为依据，而非字节估算（provider 未返回 usage 时回退字节估算）。
- R6. 裁剪（早期轮替换为占位符）不得留下孤立的 tool call / tool result 配对。
- R7. 连续 turn 的稳定前缀保持字节稳定，不因时间、预算提示或重试状态变化而失效。
- R8. 除新增内置工具 `tool_result_read` 外，不改变模型调用参数、既有工具集合与响应格式。
- R9. 有回归测试覆盖持久化、投影、handle 读取与预算控制；`make vet` 与 `make lint` 通过。

---

## Scope Boundaries

### In Scope

- `docs/convo.yaml`：`Message` 的 store 类型码补 `C`；新增 `ToolRecord` 模型承载工具调用与结果
- `make codegen` 产物与一条 PG 迁移 SQL
- `pkg/services/stores`：原始记录写入与查询
- `pkg/services/agent`：工具调用与结果的写入点、handle 生成
- `pkg/web/api`：上下文投影替换现有的 `RecentlyWithTokens` 拼接

### Out of Scope

- **增量摘要与压缩策略**：本计划提供压缩所需的原始记录与配对保证，但压缩算法本身（摘要合并、覆盖范围记录）属后续
- **跨副本的 agent session 恢复**：需持久 agent runtime，属后续
- **子 agent 的 transcript 与用量归集**：子 agent 尚在规划阶段，未有实现
- **SSE `after_seq` 续读的事件流落库**：见「Deferred」。注意[计划 003](2026-10-29-003-feat-turn-idempotency-state-machine-plan.md) 的 Out of Scope 曾把该能力指向本计划，实际本计划也未纳入——该能力当前无归属计划

### Deferred to Follow-Up Work

- **SSE 事件游标与断线续读**：`ChatTurn`（[计划 003](2026-10-29-003-feat-turn-idempotency-state-machine-plan.md)）已提供轮次锚点，但事件序列本身尚未落库。续读需要把每个 SSE 事件按单调序号持久化并支持按 `after_seq` 查询，是独立的一块工作。**触发条件**：Web 端出现"客户端断线后请求重发导致体验受损"的实际反馈。
- **上下文压缩**：本计划落地后，历史侧有了完整原始记录，压缩才具备实施基础。

---

## Context & Research

### Relevant Code and Patterns

- `pkg/services/stores/conversation.go`：Redis 历史存储，`AddHistory` / `ListHistory`
- `pkg/services/stores/event_adapter.go:29` `AppendEvent`：现有的事件落库路径，**只保留 Delta 与 Think**
- `pkg/services/runner/runner.go:37` `Persist`：统一的事件持久化入口，两条链路都经它
- `pkg/models/aigc/history.go`：`HistoryChatItem{User, Assistant, Think}` 与字节数的 token 估算
- `pkg/web/api/handle_convo.go:87`：`history.RecentlyWithTokens(historyLimitToken)`，当前的上下文裁剪点
- `pkg/web/api/prompt.go:123` `chatMessages`：历史文本 → messages 的拼接（调用点：`handle_convo.go:102`、`handle_platform.go:365`）
- `pkg/services/agent/tool_executor.go:199` `formatToolResult`：工具结果进入上下文的唯一出口，超限截断发生在此
- `pkg/services/llm/event.go`：`Event` 已携带 `ToolCalls` 与 `ToolResult`，是原始记录的天然来源
- `pkg/services/llm/openai.go:340` / `anthropic.go`：`Usage` 已由 provider 返回真实 `prompt_tokens`，R5 的数据来源已就位

### Institutional Learnings

- `docs/plans/2026-07-07-001-feat-memory-tier-decay-plan.md`：记忆分层与衰减的既有设计，本计划的 PG 持久化与其并存（记忆是"提炼后的结论"，原始记录是"事实"，两者职责不同）
- `docs/plans/2026-10-10-001-feat-memory-ownership-plan.md`：归属判定与越权收窄的既有模式（`owner_id` 列 + 存储层钩子 + `ownerAllowed` / `ErrForbidden`），原始记录的查询需遵循同一套归属规则——`Message` 与 `ToolRecord` 都必须带 `OwnerID`，否则该规则无处落地

### External References

- Anthropic prompt caching 要求稳定前缀字节一致，这是 R7 的直接来源

---

## Key Technical Decisions

### 原始记录的表结构

复用已有的 `Message` 表承载消息，补充一张 `ToolRecord` 表承载工具调用：

```yaml
- name: ToolRecord
  comment: '工具调用记录'
  tableTag: 'convo_tool_record,alias:tr'
  specNs: convo
  oidcat: event
  fields:
    - type: comm.DefaultModel
    - comment: 轮次编号
      name: TurnID
      type: oid.OID
      tags: {bson: 'turn_id', json: 'turnId', pg: ',notnull', binding: 'required'}
      isset: true
      query: 'equal'
    - comment: 会话编号
      name: SessionID
      type: oid.OID
      tags: {bson: 'session_id', json: 'session', pg: ',notnull', binding: 'required'}
      isset: true
      query: 'equal'
    - comment: 归属用户
      name: OwnerID
      type: oid.OID
      tags: {bson: 'owner_id', json: 'ownerId', pg: ',notnull'}
      isset: true
      query: 'equal'
    - comment: 轮内顺序
      name: Seq
      type: int
      tags: {bson: 'seq', json: 'seq', pg: ',notnull,type:int'}
      isset: true
    - comment: 工具调用编号
      name: CallID
      type: string
      tags: {bson: 'callId', json: 'callId', pg: ',notnull,type:varchar(64)'}
      isset: true
      query: 'equal'
    - comment: 工具名
      name: ToolName
      type: string
      tags: {bson: 'toolName', json: 'toolName', pg: ',notnull,type:varchar(128)'}
      isset: true
      query: 'equal'
    - comment: 入参原文
      name: Arguments
      type: string
      tags: {bson: 'arguments', json: 'arguments', pg: ',notnull,type:text'}
      isset: true
    - comment: 结果状态
      name: Status
      type: string
      tags: {bson: 'status', json: 'status', pg: ',notnull,type:varchar(16)'}
      isset: true
      query: 'equal'
    - comment: 结果全文
      name: Content
      type: string
      tags: {bson: 'content', json: 'content', pg: ',notnull,type:text'}
      isset: true
    - comment: 发给模型的预览原文
      name: Preview
      type: string
      tags: {bson: 'preview', json: 'preview', pg: ',notnull,type:text'}
      isset: true
    - comment: 是否走 handle 外溢
      name: Spilled
      type: bool
      tags: {bson: 'spilled', json: 'spilled', pg: ',notnull'}
      isset: true
    - comment: 错误码
      name: ErrorCode
      type: string
      tags: {bson: 'errorCode', json: 'errorCode', pg: ',notnull,type:varchar(64)'}
      isset: true
    - comment: 耗时毫秒
      name: DurationMS
      type: int
      tags: {bson: 'durationMs', json: 'durationMs', pg: ',notnull,type:int'}
      isset: true
    - type: comm.MetaField
```

`Message` 表补一个 `TurnID` 字段并把 store 类型码从 `LGD` 改为 `LGCD`。

**为什么不为 tool call 和 tool result 分两张表**：两者是一对一的调用关系，`CallID` 天然把它们绑在一起；拆表会让"配对完整性"的查询变成 join，而完整性正是 R6 要保证的东西。

**字段的三个要点**：

- `SessionID` / `OwnerID`：跨会话读取的守卫要能在存储层判定归属，不能靠工具层比对一个表里不存在的列。`SessionID` 用于同会话约束，`OwnerID` 用于按登录身份收窄查询面。
- `Seq`：工具调用在 `ExecuteToolCalls` 中并发执行，插入顺序不确定；没有轮内序号，投影无法重建 provider 要求的 `tool_use` / `tool_result` 顺序——那正是[计划 001](2026-10-29-001-fix-tool-result-completeness-plan.md) 要消灭的那类失败在持久化路径上被重新引入。
- `Preview` / `Spilled`：存"当时发给模型的预览原文"与"是否走 handle 外溢"。投影直接内联 `Preview`，不重新按当前 `ToolResultMaxChars` 推导——限额或截断措辞一变，历史字节就变，R7 的字节稳定随之失效；若限额被调大，投影还会把当时已截断的结果整段内联回来，重新撑爆上下文。

`Arguments` 存**入参原文**（模型 `tool_calls` 中的 arguments JSON），不是摘要：摘要化入参无法还原 provider 的 `tool_use.input`，重放时会触发与[计划 001](2026-10-29-001-fix-tool-result-completeness-plan.md) 同类的 400。

写入前对 `Arguments` 与 `Content` 做敏感字段脱敏（至少对 token / secret / password / authorization 等常见键名掩码）。

### handle 机制

工具结果超过内联上限时：

1. 全文写入 `ToolRecord.Content`，同时写入 `Preview`（发给模型的预览原文）与 `Spilled=true`
2. 模型收到的是一段预览 + 一个 handle（`ToolRecord` 的 ID）
3. 新增一个内置工具 `tool_result_read(handle, offset, limit)`，模型用它分页读取全文

**唯一写入方是截断处。** `formatToolResult` 决定超限时同步写入该行（并取回行 ID 作为 handle），事件持久化路径按 `(turn_id, call_id)` 更新同一行的 `Status` / `ErrorCode` / `DurationMS`，**不覆盖 `Content`**。若两侧各写一行，handle 会指向其中一行、另一行成为孤儿，配对完整性的查询与"tool call 与 tool result 数量相等"的断言都会失配。

写入失败时降级为纯截断文本、不发 handle（而不是让工具调用失败）。

**分页是必需的**：`formatToolResult` 是所有工具结果的唯一出口，新读工具的结果同样会经过它的长度上限——不分页的话，读回的全文会再次被截断到同一个上限，模型拿到的仍是残缺内容，且形成读—截断—再读的循环。该工具自身的输出不再生成新的 `ToolRecord` / handle。

handle 指向数据库行，因此**天然可重新读取**——这与"截断后无法补救"的现状正好相反。

预览采用头尾保留（与 [计划 001](2026-10-29-001-fix-tool-result-completeness-plan.md) 的截断策略一致）。

### 上下文投影

模型请求的消息不再从 Redis 的扁平文本拼接，而是：

```
[system prompt]                      ← 稳定前缀
[摘要 / 早期轮的占位]                ← 若有压缩
[近期若干完整 turn]                  ← 含 tool call / tool result 配对
[当前轮的 tool result]               ← 大结果替换为 handle 占位符
```

投影的关键约束：

- **配对不被拆散**：一个 tool call 与它的 tool result 必须在同一轮内一起出现或一起被替换为占位符（R6）。
- **稳定前缀字节一致**：system prompt 与不涉及压缩的历史部分保持字节稳定，只有尾部随轮次追加。这让 provider 的 prompt cache 命中不因预算提示或重试状态而失效（R7）。
- **逐轮通知放尾部**：预算提示、重试提示等放在请求尾部，不插入前缀中间。

### 真实 token 预算

`Usage` 已携带 provider 返回的真实 `prompt_tokens`。投影时用它判断是否需要裁剪，而不是字节估算。

但 provider 的 usage 是**上一次请求**的返回值，用它触发本次裁剪存在一轮滞后。这个滞后是可接受的：滞后一轮的裁剪判断远好于字节估算的偏差。裁剪发生时按 `Usage.InputTokens` 与目标窗口的比例决定保留多少轮。

**保留的现状**：`pkg/web/api/convo_basic.go` 的 `historyLimitToken` 与 `pkg/models/aigc/history.go` 的 `calcTokens` 在本计划后成为过渡期配置，Redis 历史仍作为缓存层存在，但不再作为模型上下文的唯一来源。

---

## Open Questions

### Resolved During Planning

- **要不要为 tool call 和 tool result 分两张表？** 不分。`CallID` 把两者绑定在一行，配对完整性的查询不需要 join。
- **Redis 历史要删吗？** 不删。降级为缓存层，PG 为事实源。现有 `RecentlyWithTokens` 的调用点在投影接入后移除。
- **handle 用什么标识？** `ToolRecord` 的数据库 ID。天然唯一，且不泄漏内部结构。
- **`tool_result_read` 这个工具要不要过授权门控？** 要。它是内置公开工具，与 `Fetch` 同级，在 [计划 002](2026-10-29-002-feat-effective-tool-policy-plan.md) 的策略表中登记为 `builtin-public`；但读取范围限于当前会话的轮次，避免跨会话读取——归属判定按 `docs/plans/2026-10-10-001-feat-memory-ownership-plan.md` 的 owner 判定与越权收窄规则执行（`owner_id` 列 + 存储层钩子 + `ownerAllowed` / `ErrForbidden`），并额外约束同会话。

### Deferred to Implementation

- 目标上下文窗口的具体数值来源（模型配置还是 provider 返回）
- 保留多少近期轮的策略参数
- 摘要文本的存储位置（放 `Message` 表还是独立）

### From 2026-11-02 review

- **稳定前缀字节稳定与 usage 裁剪的冲突解法** — Key Technical Decisions / 上下文投影 (P1, product-lens, confidence 75)

  R7 与投影设计都承诺前缀字节在连续两轮间保持一致，但 U4 第 4 步依 `Usage.InputTokens` 裁剪早期轮次，而该信号是上一轮的陈旧值——输入 token 随每轮的工具结果增长，裁剪点会在轮次之间移动，前缀字节随之改变，R7 存在的理由（prompt cache 命中）被抵消。origin 的解法是压缩（用稳定的摘要顶替被裁剪的历史），但压缩被推迟到后续，本轮范围内没有既保前缀稳定又执行预算的机制。

  <!-- dedup-key: section="key technical decisions 上下文投影" title="稳定前缀字节稳定与 usage 裁剪的冲突解法" evidence="- R7. 连续 turn 的稳定前缀保持字节稳定，不因时间、预算提示或重试状态变化而失效。" -->


---

## High-Level Technical Design

```
写入侧（每次事件后）
  Event{UserPrompt}       →  写 Message(role=user, turn_id)
  Event{Done, ToolCalls}  →  写 Message(role=assistant, turn_id)
  Event{ToolResult}       →  写 ToolRecord(turn_id, session_id, owner_id, seq, call_id, status, content)
  Event{Usage}            →  写 UsageRecord（已有）

投影侧（构造模型请求前）
  PG 原始记录
    → 按 turn 分组
    → 早期轮：替换为摘要或占位符
    → 近期轮：完整保留（含 tool call / result 配对）
    → 超限 tool result：替换为 handle 占位符
    → 拼装 messages
```

新增内置工具：

```go
// pkg/services/tools/registry.go initTools
r.tools = append(r.tools, toolResultReadDescriptor)
r.invokers[ToolNameToolResultRead] = r.callToolResultRead
```

---

## Implementation Units

本计划拆为两个可独立交付的增量：

- **增量 A（U1–U3）**：契约 + 写入路径 + handle 机制。全部是增量改动，不触碰线上请求路径，上线即交付"工具调用轨迹可查"与"超限结果可回读"两项能力。
- **增量 B（U4–U6）**：上下文投影与真实 token 预算。替换的是每一条会话都依赖的上下文构造路径，风险最高，在增量 A 进入生产并稳定后再做。Redis 拼接路径保留为开关可回退，直到投影稳定。

#### 交付进度

- **增量 A（U1–U3）：已交付**（分支 `feat/conversation-persistence-basis`）
  - U1：`Message` 补 `TurnID` / `OwnerID` 并挂上归属钩子，store 类型码 `LGD → LGCD`；新增 `ToolRecord` 模型（`LGCUD`）；迁移 SQL 只写增量 DDL，`convo_tool_record` 的建表交给 `InitSchemas`
  - U2：`historyAdapter.AppendEvent` 扩展为原始记录写入路径（用户消息、助手消息、工具记录）；三条入口（Web SSE、Web 非流式、频道）统一注入轮次与会话锚点并转发工具事件；非流式链路经 `AgentLoopConfig.OnToolResult` 回调补齐工具轨迹
  - U3：截断处（`spillToolResult`）是 `ToolRecord` 的唯一写入方，回读 handle 即该行编号；新增内置工具 `tool_result_read(handle, offset, limit)`，读取强制同会话同归属
- **增量 B（U4–U6）：未开始**——上下文投影与真实 token 预算待增量 A 进入生产并稳定后再做

### U1. 契约与表结构

1. `docs/convo.yaml`：`Message` 补 `TurnID` 与 `OwnerID` 字段、store 类型码改 `LGCD`；新增 `ToolRecord` 模型与 store 声明
2. `make codegen`
3. `data/schemas/` 迁移 SQL 只写增量 DDL：`ALTER TABLE IF EXISTS convo_message ADD COLUMN IF NOT EXISTS turn_id ...`（以及需要的索引）。`convo_tool_record` 的建表交给 codegen 后的模型注册（`InitSchemas` → `CreateModels`），不在迁移 SQL 里写 `CREATE TABLE`——否则迁移会因 "relation already exists" 失败，而 `RunMigrations` 会吞掉该错误，schema 处于无人管理状态

验收：codegen 产物入库；表与列在 `InitDB` 后存在。

### U2. 写入路径

1. **接线到事件来源**：工具事件必须从产生处转发给持久化路径，而不是只扩展 `AppendEvent`。当前四处 `runner.Persist` 调用点（`handle_convo.go:252` / `:280`、`handle_platform.go:272` / `:303`）都手工构造只带 Delta / Think / UserPrompt / Usage 的 `llm.Event`，且两处 handler 在 `event.ToolResult != nil` 处 `continue`。需要：Web SSE 与频道流式循环把 `event.ToolResult` 事件、以及携带 `ToolCalls` 的 `Done` 事件送进 `rnr.Persist`；助手消息事件需带上 `ToolCalls`
2. `historyAdapter.AppendEvent` 扩展：除 Delta / Think 外，把 `ToolCalls` 与 `ToolResult` 一并落库
3. `pkg/services/stores/convo_x.go` 增加消息与工具记录的写入、查询方法
4. 写入带上 `turn_id`（由[计划 003](2026-10-29-003-feat-turn-idempotency-state-machine-plan.md) 的轮次锚点经 ctx 传递提供）、`session_id`、`owner_id` 与轮内 `seq`
5. **补齐非流式链路**：Web 非流式入口（`handle_convo.go:148`）与频道非流式入口（`handle_platform.go:293`）目前不调用 `runner.Persist`，需补上——否则这两条链路不产生任何原始记录；`AgentLoop.RunNonStreaming`（`agent_loop.go:109`）目前丢弃 `ExecuteToolCalls` 返回的事件，需要改为产出工具事件（或经回调上报）
6. **归属收窄**：`Message` 与 `ToolRecord` 都带 `OwnerID`，创建时由服务端从登录上下文写入。`convo.yaml` 的 webapi 段里 `Message` 挂着 `GET /convo/messages` 与 `GET /convo/messages/{id}`，它们以 `regHI(true, ..., "")` 注册——只要求登录、不要求 keeper，且 `Sift` 只按 session_id / role 过滤、没有归属收窄。今天 `convo_message` 表是空的所以暴露面为零，一旦开始写入，任意登录用户就能列出并按 ID 读取他人整段对话。因此必须为 `Message` 声明 `hooks`（参照 Memory 的 `dbBeforeCreateMemory` / `afterLoadMemory` / `dbBeforeUpdateMemory` / `dbBeforeDeleteMemory`），在存储层实现列表按 owner 过滤、单条读取与删除的越权拒绝（返回 `stores.ErrForbidden`，接口层映射 403）

验收：一次含两个工具调用的对话，能从 PG 还原出两条 `ToolRecord` 及其状态与内容；跨用户读取他人 `Message` 被拒。

### U3. handle 与重新读取

1. `formatToolResult` 超限时：同步写入 `ToolRecord`（全文 + `Preview` + `Spilled`），取回行 ID 作为 handle，模型侧收到头尾预览 + handle；写入失败则降级为纯截断文本、不发 handle
2. 新增内置工具 `tool_result_read(handle, offset, limit)`，按片段返回全文并回传总长度
3. 三条入口（Web SSE、Web 非流式、频道 `MessageHandler`）在调用 agent 循环前统一注入会话标识（复用 `stores.ContextWithConvoID`，并新增轮次号 ctx 键）——当前只有 Web SSE 一条路径注入了会话号，非流式与频道入口的 ctx 里没有，守卫无从判定
4. 在 stores 层新增按 handle 读取的方法，`WHERE` 条件强制包含 `session_id = 当前会话` 且 `owner_id = 当前登录者`；无法确定归属时拒绝

验收：超限结果的全文可经工具分页重新读取；跨会话读取被拒；跨用户读取被拒。

### U4. 上下文投影

新增投影函数，同时接到 `pkg/web/api/handle_convo.go`（Web）与 `pkg/web/api/handle_platform.go:365` 的 `buildChatMessagesAndTools`（频道），替换 `RecentlyWithTokens` 与 `chatMessages` 的拼接路径：

1. 从 PG 读取近期轮次
2. 按配对规则组装 turn；tool call 与 tool result 按 `(turn_id, seq)` 排序
3. 超限 tool result 替换为 handle 占位符；未超限的结果直接内联 `Preview` 原文（不重新按当前限额推导）
4. 依 `Usage.InputTokens` 与目标窗口比例裁剪早期轮次，裁剪公式为 `(目标窗口 − 本轮预留) / 上一轮 InputTokens`，其中「本轮预留」按单轮工具结果上限估算，并对保留轮数设下限（不少于最近一轮）
5. 保持 system prompt 与早期历史字节稳定
6. 承接既有的重试 / 重新生成语义：`prepareChatRequest` 在 `regen` 为真或本次 prompt 与上一条用户消息相同时跳过最后一个 turn；`AddHistory` 的同问替换规则同样沿用

验收：投影产出的 messages 中 tool call 与 tool result 严格配对；连续两轮的稳定前缀字节一致；Web 与频道两条链路产出同一份投影。

### U5. 真实 token 预算

接入 `Usage.InputTokens` 触发裁剪。上一轮的真实用量从 `convo_usage_record` 读取该会话最近一条 `UsageRecord` 的 `InputTokens`——投影发生在发请求之前，此刻作用域内没有 usage 对象。usage 需在 Web SSE、Web 非流式、频道三条链路都写入 `UsageRecord`（当前只有 Web SSE 写入），否则频道场景永远回退到字节估算。

provider 未返回 usage 时回退字节估算；投影后的 `[]llm.Message` 没有 `HistoryItem` 形状，`pkg/models/aigc/history.go` 的 `calcTokens` 无法直接复用，需另写一个按字节估算消息的函数。

验收：一次超预算的会话触发裁剪，且裁剪后仍保持配对完整。

### U6. 回归测试

| 用例 | 断言 |
| --- | --- |
| 含工具调用的对话落库 | PG 中有对应的 `ToolRecord`，状态与内容正确 |
| 超限工具结果 | 模型收到预览 + handle；`tool_result_read` 分页返回全文 |
| 跨会话读 handle | 被拒 |
| 跨用户读 `Message` / handle | 被拒 |
| 投影配对 | messages 中 tool call 与 tool result 数量相等、顺序一致 |
| 稳定前缀 | 连续两轮请求的公共前缀字节一致 |
| 预算裁剪 | 超预算时裁剪早期轮次且不破坏配对 |
| 非流式链路 | Web 非流式与频道非流式同样产出 `ToolRecord` |

---

## System-Wide Impact

| Area | Impact |
| --- | --- |
| `docs/convo.yaml` | `Message` 改字段与 store 类型码；新增 `ToolRecord`；触发 `make codegen` |
| `pkg/models/convo` | 新增生成模型 |
| `pkg/services/stores` | 新增写入与查询方法；`AppendEvent` 语义扩展 |
| `pkg/services/agent` | `formatToolResult` 增加 handle 分支；新增内置工具 |
| `pkg/web/api` | 上下文构造路径替换（Web 与频道两处） |
| `pkg/services/tools` | 新增一个内置工具（影响可见工具集合） |
| `data/schemas` | 新增一条迁移 SQL（只含增量 DDL） |
| 外部行为 | 工具调用轨迹可查；超限结果可分页重新读取；历史不再受 24 小时限制 |

---

## Risks & Dependencies

| Risk | Mitigation |
| --- | --- |
| 每轮多写几张 PG 行，写入放大 | `runner.Persist` 在 Web 与频道两侧都是在请求 goroutine 内**同步调用**（`handle_convo.go:280`、`handle_platform.go:272`），不是异步路径。写入失败只记日志、不影响响应（沿用调用点现有做法），但每轮新增的 PG 写入会增加请求内耗时，需在实现时确认可接受 |
| 新增内置工具改变模型可见工具集合，影响 prompt 前缀 | 新工具追加在末尾，顺序稳定 |
| 投影替换现有路径引入行为回归 | 旧路径（Redis 拼接）保留为开关可回退，直到投影稳定；两个增量分批上线（见 Implementation Units） |
| 依赖 [计划 003](2026-10-29-003-feat-turn-idempotency-state-machine-plan.md) 的轮次锚点 | 是。`turn_id` 是写入的关联键，顺序上 003 必须先落地，且轮次 ID 需按 003 的约定经 ctx 传递到写入侧 |
| 依赖 [计划 001](2026-10-29-001-fix-tool-result-completeness-plan.md) 的结果状态字段 | 是。`Status` / `ErrorCode` 来自统一结果契约，顺序上 001 先落地 |
| 依赖 [计划 002](2026-10-29-002-feat-effective-tool-policy-plan.md) 的工具授权门控 | 是。`tool_result_read` 须在 `EffectiveToolPolicy` 中登记为 `builtin-public`，否则按注册期类别判定会拒绝该调用。顺序上 002 先落地 |
| 24 小时历史限制解除后存储增长 | 原始记录从"24 小时后随 Redis 过期消失"变为长期留存。本计划先按保守口径落地：写入即脱敏（token / secret / password / authorization 等常见键名掩码），保留期沿用现有 24 小时量级。精确的保留期与清理作业归属见「Open Questions」 |

---

## Documentation / Operational Notes

- 新增 PG 表：`convo_tool_record`；`convo_message` 增加 `turn_id` 与 `owner_id` 列
- 新增内置工具：`tool_result_read(handle, offset, limit)`
- 新增配置项：目标上下文窗口（数值来源待定）
- 可按 `turn_id` 还原一次对话的完整轨迹

---

## Sources & References

- `docs/reports/2026-10-27-whip-design-review.md` 第 3 节「原始记录与模型上下文分离」
- `pkg/services/stores/conversation.go`
- `pkg/services/stores/event_adapter.go`
- `pkg/services/agent/tool_executor.go`
- `pkg/web/api/handle_convo.go`
- [计划 001](2026-10-29-001-fix-tool-result-completeness-plan.md)（统一结果契约）
- [计划 003](2026-10-29-003-feat-turn-idempotency-state-machine-plan.md)（轮次锚点）
