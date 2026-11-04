---
title: feat: 聊天轮次的幂等与状态机
type: feat
status: completed
date: 2026-10-29
origin: docs/reports/2026-10-27-whip-design-review.md
---

# feat: 聊天轮次的幂等与状态机

## Summary

三个接线点（Web SSE、Web 非流式、频道 `MessageHandler`）覆盖四条频道路径，都能收到同一条用户消息的多次投递，而当前没有统一的幂等机制：频道侧的 Redis 去重窗口只有 60 秒，且企业微信 HTTP 回调路径**完全没有接入去重**。重复投递会重复执行模型调用与副作用工具。本计划引入持久化的 `chat_turn` 记录，以幂等键保证"同一次请求只执行一次"，并给轮次一个可查询的状态机。

改动涉及 `docs/convo.yaml` 契约（新增 `ChatTurn` 表）、`make codegen`、一条 PG 迁移 SQL，以及三个接线点的接线。

---

## Problem Frame

### 现状：三个接线点，四条频道路径，三种去重强度

| 入口 | 去重 | 强度 |
| --- | --- | --- |
| Web SSE / 非流式 | 无 | 重复请求重复执行 |
| 频道 WebSocket（企微 / 飞书） | Redis `SetNX`，TTL 60 秒 | 窗口内挡住，窗口外重复 |
| 频道 HTTP 回调（飞书） | Redis `SetNX`，TTL 60 秒 | 同上 |
| 频道 HTTP 回调（**企微**） | **无** | 每次投递都执行 |

企微 HTTP 路径的实现（`pkg/services/channels/wecom/wecom_http.go:157-256`）在解密后立即 `w.WriteHeader(http.StatusOK)`，然后 `go handler(...)` 异步处理，全程没有任何幂等检查。`IsDuplicate` 的调用点只有三处：`wecom/websocket.go:334`、`feishu/websocket.go:242`、`feishu/webhook.go:281`——**唯独漏掉了企微 HTTP**。

企微的官方重试策略是回调未在 5 秒内响应则重试最多三次，每次间隔递增；飞书事件订阅的重试间隔也在分钟级。**60 秒的 Redis 窗口覆盖不了任何一次平台重投**。

### 重复执行的代价不均匀

| 工具 | 重复调用后果 |
| --- | --- |
| `memory_store` | 幂等（同 key 走 update 分支，`pkg/services/stores/convo_x.go:517`） |
| `memory_list` / `memory_recall` | 只读 |
| `memory_forget` | 近似幂等 |
| `KBCreate` | **非幂等**——每次新建文档，重复投递产生重复知识库条目 |
| 外部 MCP 工具 | **非幂等且副作用未知**——发消息、下单、改配置都在此列 |
| 频道回复 | 用户收到两条相同回复 |

### 反方向的问题：静默丢失

现有去重是"先 `SetNX` 再执行"。若进程在 `SetNX` 之后、执行完成之前崩溃，平台重投会在 60 秒内被判定为重复而**直接跳过**——用户的消息消失了，没有回复、没有日志线索。

**当前是"要么重复，要么丢失"，没有第三种状态。** 根因是去重窗口本身就是一个布尔值，无法表达"已接收 / 执行中 / 已完成 / 结果未知"。

### 缺失的持久载体

`docs/convo.yaml` 里已有 `Message` 表（`convo_message`），但 `stores` 声明为 `LGD`——只有 List / Get / Delete，**没有 Create**，全仓也找不到任何写入调用点。表存在但从未被使用。

---

## Requirements

- R1. 每条入口在调用模型之前先持久化一条 `chat_turn` 记录，作为该次执行的唯一锚点。
- R2. 轮次有持久化的幂等键：频道侧用平台 `message_id`，Web 侧用请求携带的幂等键（缺省时按请求摘要派生）。
- R3. 相同幂等键的重复请求**不重复执行**模型调用与副作用工具；已有轮次处于 `succeeded` 终态时直接返回其结果。`failed` / `cancelled` 的轮次允许同键重新执行；`interrupted` 按 R5 不自动重放。
- R4. 轮次状态机：`queued → running → succeeded | failed | cancelled | interrupted`。
- R5. 已产生外部副作用但结果不确定的轮次标记为 `interrupted`，**不自动重放**。重放 = 客户端以**新的幂等键**重发，产生新轮次；原 `interrupted` 轮次保持不变，其已发生的副作用不追溯、不重放。
- R6. 进程重启后：已完成的轮次可查询到最终结果；未完成的轮次可被识别为中断。
- R7. 企微 HTTP 回调路径接入与其它入口一致的幂等判定。
- R8. 幂等键在全局唯一，同键并发请求只有一个能进入执行。
- R9. 不改变现有工具执行语义、模型调用参数与响应格式。
- R10. 有回归测试覆盖重复投递、并发同键、进程重启后状态恢复；`make vet` 与 `make lint` 通过。

---

## Scope Boundaries

### In Scope

- `docs/convo.yaml`：新增 `ChatTurn` 模型与 store 声明
- `make codegen` 产物
- `data/schemas/`：一条 PG 迁移 SQL
- `pkg/services/stores/`：轮次的写入与查询
- 三条入口的接线
- 企微 HTTP 路径的幂等接入

### Out of Scope

- **SSE 事件游标与断线续读**（`after_seq`）：需要事件流落库，属 [计划 004](2026-10-29-004-feat-conversation-persistence-basis-plan.md)
- **原始 transcript 持久化**（turn / message / tool_call / tool_result）：属 [计划 004](2026-10-29-004-feat-conversation-persistence-basis-plan.md)
- **工具结果 handle 与分页读取**：属 [计划 004](2026-10-29-004-feat-conversation-persistence-basis-plan.md)
- **MCP 动态目录**：见 [计划 002](2026-10-29-002-feat-effective-tool-policy-plan.md) 的 Deferred
- **子 agent**：尚在规划阶段，未有实现

### Deferred to Follow-Up Work

- 现有 Redis 60 秒去重在 PG 幂等上线后的处置：**保留，但不再拥有丢弃权**。命中时只记录、不阻断，继续交给 PG 幂等层按轮次状态判定。否则「先 `SetNX` 再执行」的静默丢失缺陷会被原样保留——进程在 `SetNX` 之后、轮次落库之前崩溃时，平台重投被 Redis 直接跳过，既没有执行也没有轮次记录。它仍是通道层的第一道快速过滤（挡掉绝大多数重复而不产生数据库写入），但判定权归 PG。
- 幂等键的对外暴露（Web 侧让客户端显式传 key）：本计划先支持"缺省时按请求摘要派生"，显式 key 作为 API 字段加入。

---

## Context & Research

### Relevant Code and Patterns

- `pkg/services/channels/dedup.go`：`Dedup.IsDuplicate`，Redis `SetNX` + TTL
- `pkg/services/channels/wecom/wecom_http.go:157`：`handleMessage`，缺去重的入口
- `pkg/services/channels/wecom/websocket.go:334`、`feishu/websocket.go:242`、`feishu/webhook.go:281`：已有去重的三个入口
- `pkg/web/api/handle_convo.go:77` `prepareChatRequest`：Web 侧请求准备入口
- `pkg/web/api/handle_platform.go:125` `MessageHandler`：频道侧统一入口，三种频道都汇到这里
- `pkg/services/runner/runner.go:37` `Persist`：现有的事件持久化路径，`historyAdapter` 写 Redis 历史 + `UsageRecord`
- `docs/convo.yaml`：`Message` / `UsageRecord` 的模型声明方式与 store 类型码（`LGCUD` = List/Get/Create/Update/Delete）
- `docs/mcps.yaml:74`、`docs/convo.yaml:210`：契约层唯一索引的写法（`pg: ',notnull,unique,...'` 与 `unique:mm_key_uid_key`）
- `data/schemas/`：迁移 SQL 目录，文件名格式 `20YYYYMMDDHHMMSS_<name>.up.sql`（14 位时间戳，与 `data/schemas/fs.go` 的 embed 规则 `20????????????_*.up.sql` 一致），由 `db.RunMigrations` 执行

### Institutional Learnings

- `docs/solutions/runtime-errors/streaming-reply-multiple-startstream.md` 记录过一次"状态由多处共同管理导致重复副作用"的教训（`StartStream` 被多处调用）。本计划把轮次状态收敛为单一记录，所有入口共享同一判定。
- `docs/plans/2026-05-09-001-feat-event-driven-architecture-plan.md`：事件驱动架构的既有设计，`runner.Persist` 由此而来。

### External References

- 企业微信回调重试策略：官方文档说明回调未在 5 秒内响应会重试最多 3 次，间隔递增
- 飞书事件订阅重试：官方文档说明重试间隔为分钟级

两者均远超现有 60 秒窗口，这是本计划把幂等落到 PG 的直接依据。

---

## Key Technical Decisions

### 幂等放在 PG 而不是延长 Redis TTL

延长 TTL 到分钟级能覆盖平台重试，但解决不了"要么重复要么丢失"——布尔去重无法表达执行中状态。PG 表能表达完整状态机，且天然持久（进程重启后仍可读）。Redis TTL 从 60 秒改到 5 分钟是治标，PG 幂等是治本。

### ChatTurn 模型设计

```yaml
- name: ChatTurn
  comment: '聊天轮次'
  tableTag: 'convo_chat_turn,alias:ct'
  oidcat: event
  fields:
    - type: comm.DefaultModel
    - comment: 会话编号
      name: SessionID
      type: oid.OID
      tags: {bson: 'session_id', json: 'session', pg: ',notnull', binding: 'required'}
      isset: true
      query: 'equal'
    - comment: 幂等键
      name: IdempotencyKey
      type: string
      tags: {bson: 'idempotencyKey', json: 'idempotencyKey', pg: ',notnull,type:varchar(255),unique:uk_turn_idem_key'}
      isset: true
      query: 'equal'
    - comment: 来源 web/channel
      name: Source
      type: string
      tags: {bson: 'source', json: 'source', pg: ',notnull,type:varchar(32)'}
      isset: true
      query: 'equal'
    - comment: 状态 queued/running/succeeded/failed/cancelled/interrupted
      name: Status
      type: string
      tags: {bson: 'status', json: 'status', pg: ',notnull,type:varchar(16)'}
      isset: true
      query: 'equal'
    - comment: 请求摘要
      name: RequestDigest
      type: string
      tags: {bson: 'requestDigest', json: 'requestDigest', pg: ',notnull,type:varchar(64)'}
      isset: true
    - comment: 归属用户
      name: OwnerID
      type: oid.OID
      tags: {bson: 'owner_id', json: 'ownerId', pg: ',notnull'}
      isset: true
      query: 'equal'
    - comment: 最终结果
      name: Result
      type: string
      tags: {bson: 'result', json: 'result', pg: ',notnull,type:text'}
      isset: true
    - type: comm.MetaField
```

关键点：

- **幂等键唯一索引**：`unique:uk_turn_idem_key` 让同键并发插入只有一个成功，第二个拿到唯一约束冲突并转为"已有轮次"。这满足 R8，无需应用层加锁。
- **`Result` 存最终答案**：满足 R3「已有轮次处于 `succeeded` 终态时直接返回其结果」——重复请求不必重新执行就能拿到答案。该字段必须带 `isset: true`，否则生成的 `ChatTurnSet` 里没有它，终态结果无处写入。
- **`OwnerID` 归属用户**：Web 侧的幂等键与最终答案都按登录身份隔离，没有归属字段则跨用户可互相命中（见「幂等键的来源」）。
- **`RequestDigest`**：记录请求内容摘要，用于检测"同键但内容不同"的异常情况。
- **`MetaField`**：预留扩展位，避免后续加字段就要改契约。

**多副本部署**：`ChatTurn` 还需携带实例归属（`LeaseOwner`）与租约到期时刻（`LeaseExpiresAt`）两个字段，类型按 `docs/convo.yaml` 中既有时间字段的写法。启动恢复只回收租约已过期、或归属为本进程且已失联的 `running` 记录；单副本部署下这两个字段恒为空，同样适用。

`Message` 表本轮不动 —— 它的写入路径属 [计划 004](2026-10-29-004-feat-conversation-persistence-basis-plan.md)，那里一并把 store 类型码从 `LGD` 补上 `C`。

### 幂等键的来源

| 入口 | 幂等键 |
| --- | --- |
| 企微 HTTP | `wecom:<msg_id>` |
| 企微 WebSocket | `wecom:<msg_id>` |
| 飞书 WebSocket | `feishu:<message_id>` |
| 飞书 HTTP | `feishu:<message_id>` |
| Web SSE / 非流式 | 客户端传入的幂等键；缺省时为 `sha256(owner_id + session_id + prompt + 请求体摘要)` |

频道侧用 `message_id` 是平台保证的唯一标识，且它已经贯穿频道侧各入口的 `channel.Message` 结构（`pkg/services/channels/`），无需新增字段传递。

**Web 派生键必须包含调用者身份。** 幂等键是全局唯一的（R8），而派生式的输入里原本没有用户标识；首条消息客户端通常还没拿到 csid，`session_id` 分量是空串。两个不同用户发送同一条消息会得到同一把键，后到的一方撞唯一约束后按 R3 直接拿到前一位用户的完整答案——而 `Result` 是模型最终答案，可能含首位用户的记忆召回与知识库内容。派生式因此改为 `sha256(owner_id + session_id + prompt + 请求体摘要)`，`owner_id` 取登录上下文中的用户 OID（无登录身份时退化为 `anon:` 前缀）。

命中终态轮次要返回其 `Result` 之前，必须先校验该轮次的 `owner_id` 与当前调用者一致；不一致则不返回其结果。

**重试与重新生成不参与 PG 幂等。** `ChatRequest` 的 `regen` 标志、以及"与上一条用户消息内容相同"的重试语义（`handle_convo.go` 现在正是靠内容相同识别重试并重新执行）都表示一次**新的**提问。缺省派生式无法区分"同一条消息被网络重投"与"用户主动发的第二条相同内容"，因此这两类请求直接跳过 PG 幂等判定、按新请求执行；需要严格去重的客户端走显式幂等键（见 Deferred）。

Web 侧缺省派生意味着：同一会话内连续发送**内容完全相同**的两条消息会被判为重复，第二次不会重新执行。这是有意接受的行为——内容完全相同的重复提问，其答案本就应当一致，重新执行既浪费调用也没有信息增益。需要区分两次独立提问的客户端，显式传入不同的幂等键即可。

### 状态机与副作用时机

```
入参校验 → 创建 turn(status=queued) → 转为 running → 执行 → 写终态 + Result
```

**转 `running` 必须发生在调用模型之前**，否则重复投递会同时进入执行。

**副作用标记**：`interrupted` 状态用于表达"外部工具的副作用可能已经发生，但结果不确定"。判定规则：工具调用已开始、且执行过程中进程崩溃或 ctx 被取消 → `interrupted`。这类轮次**不自动重放**（R5）。

**`cancelled` 的判定**：ctx 在任何工具副作用发生之前被取消 → `cancelled`；已发生副作用或进程崩溃 → `interrupted`；执行正常结束但结果为失败 → `failed`。三者的边界写死，不留给实施者猜。

**轮次锚点的传递**：轮次创建成功后，其 ID 必须随执行上下文传递（如 `ctx = stores.ContextWithTurnID(ctx, turn.GetID())`），供下游写入路径（`runner.Persist` / `historyAdapter.AppendEvent`）取用——[计划 004](2026-10-29-004-feat-conversation-persistence-basis-plan.md) 的 `ToolRecord.TurnID` 依赖这个约定，否则写入侧拿不到轮次 ID。

### 与现有 Redis 去重的关系

**PG 幂等是唯一的执行仲裁者。** 所有路径都接入 PG 幂等；Redis 去重降级为不阻断执行的提示层——命中时记录日志并继续往下走，最终由 PG 轮次状态决定"跳过"还是"补执行"。

企微 HTTP 路径不再新增 Redis 前置闸门：它由 `MessageHandler` 的 PG 幂等统一覆盖（U3），无需在 `handleMessage` 里再加一道 `SetNX`。若仍保留 Redis 快速过滤，其接入点必须放在 PG 轮次创建**之后**，使其不可能挡掉一条尚无 PG 记录的投递。

### PG 幂等的接入位置

幂等判定放在**执行之前**，而不是 `Registry.Invoke` 里。三个接线点各有各的"开始执行"位置，在入口层判定最直接：

- `handle_convo.go` `postChat` / 非流式分支
- `handle_platform.go` `MessageHandler`

`MessageHandler` 是频道侧四者的汇合点，在此接入即可覆盖企微 HTTP、企微 WS、飞书 HTTP、飞书 WS 全部四条频道路径。

---

## Open Questions

### Resolved During Planning

- **PG 表由谁建？** `db.InitSchemas` 走 `CreateModels` + `NewCreateTable().IfNotExists()`，模型注册后表自动创建。迁移 SQL 只需要处理唯一索引这类 `IfNotExists` 建表不覆盖的情况，且必须写成可重复执行的 `IF NOT EXISTS` 形式，避免与建表时已生成的同名约束冲突。
- **幂等键放全局唯一还是按会话唯一？** 全局。频道的 `message_id` 在不同会话间不重复；Web 侧的派生键含 `owner_id` 与 `session_id`。用全局唯一让并发同键的处理退化为一次唯一约束冲突，最简单。
- **轮次结果存在 `Result` 文本里还是独立字段？** `Result` 文本。轮次结果就是最终答案，与 `UsageRecord` 的职责不同，不需要独立表。
- **Redis 60 秒去重要不要删？** 不删，但不再拥有丢弃权，见 Scope Boundaries。

### Deferred to Implementation

- 幂等键派生摘要的具体拼接方式
- `interrupted` 与 `cancelled` 的判定点的实现细节（判定规则已定，见「状态机与副作用时机」）

### From 2026-11-02 review

- **轮次写入失败时 fail-open 还是 fail-closed** — Risks & Dependencies (P1, coherence, feasibility, adversarial, confidence 100)

  风险表把「轮次记录写入失败」的对冲定为「降级为直接执行」，也就是在拿不到幂等声明的情况下照常跑模型与副作用工具。此时并发同键请求会同时进入执行，R3 与 R8 同时失效，而被重复的正是计划点名的 `KBCreate` 与外部 MCP 工具。计划把 R3/R8 写成无条件要求，又把唯一会打破它们的情形写成了正常的对冲路径。需要决定：轮次写入失败是 fail-closed（不进入模型与副作用工具，向调用方返回可重试错误）还是 fail-open（降级执行并告警）。

  <!-- dedup-key: section="risks dependencies" title="轮次写入失败时 failopen 还是 failclosed" evidence="| 轮次记录写入失败阻塞正常请求 | 写入失败时降级为\"直接执行\"并记 warn 日志，不把数据库问题放大成服务不可用 |" -->

- **RequestDigest 保留还是删除** — Key Technical Decisions / ChatTurn 模型设计 (P2, security-lens, scope-guardian, confidence 75)

  两个方向冲突：一条要求定义「同键但内容不同」的处置规则（该列的用途正是检测这种异常），另一条要求删掉这个没人消费的列。需要定其一——保留则补上检测与拒绝逻辑，删除则接受同键不同内容被静默复用（用户改了 prompt 却拿到旧问题的答案）。

  <!-- dedup-key: section="key technical decisions chatturn 模型设计" title="requestdigest 保留还是删除" evidence="- **`RequestDigest`**：记录请求内容摘要，用于检测\"同键但内容不同\"的异常情况。" -->

- **convo_chat_turn 的保留期与级联清理** — Risks & Dependencies (P2, security-lens, confidence 75)

  把模型最终回答落到 PG，会把本来 24 小时后随 Redis 过期消失的对话内容变成长期留存的数据，而 Risks 表没有保留期/清理一行，模型里也没有 TTL 或过期字段。更实际的问题是现有删除会话路径（`DELETE /api/convo/sessions/{id}` → `DeleteSession`）只删 `convo_session` 行，新表按 `SessionID` 关联却不会被清理——用户主动删除会话后旧答案仍留在库里。保留期与清理归属是产品决策。

  <!-- dedup-key: section="risks dependencies" title="convo chat turn 的保留期与级联清理" evidence="- **`Result` 存最终答案**：满足 R3「已有轮次处于终态时直接返回其结果」——重复请求不必重新执行就能拿到答案。" -->

- **Web 侧显式幂等键的 API 字段名与是否本轮交付** — Requirements / R2；Scope Boundaries / Deferred to Follow-Up Work (P2, feasibility, scope-guardian, confidence 75)

  R2 明确写「Web 侧用请求携带的幂等键」，而 Deferred 段又说「显式 key 作为 API 字段加入」留待后续，U4 也没说明是否新增请求字段。实现者按 R2 读会加字段，按 Scope Boundaries 读则不加，两种做法都能自圆其说却给出不同的 API。

  <!-- dedup-key: section="requirements r2 scope boundaries deferred to followup work" title="web 侧显式幂等键的 api 字段名与是否本轮交付" evidence="- R2. 轮次有持久化的幂等键：频道侧用平台 `message_id`，Web 侧用请求携带的幂等键（缺省时按请求摘要派生）。" -->

### Resolved During Implementation

- **轮次写入失败时 fail-open 还是 fail-closed** → **fail-closed**。拿不到幂等声明就不进入模型与副作用工具，向调用方返回可重试错误（Web 503、频道侧回错误提示）。R3/R8 是无条件要求，Risks 表的「降级为直接执行」会在数据库抖动时同时打破这两条。
- **RequestDigest 保留还是删除** → **保留并真正使用**。同键但摘要不同视为冲突（`ErrTurnConflict`），不复用既有轮次的结果，避免用户改了提问却拿到旧答案。
- **Web 侧显式幂等键的 API 字段** → **本轮不加**。只交付派生键（`sha256(owner_id + session_id + prompt + 请求摘要)`），`ChatRequest` 契约不变，不触发 swagger 重新生成。
- **convo_chat_turn 的保留期与级联清理** → **本轮不做定时清理**。轮次表随会话删除一并清理的归属留作后续；模型最终答案因此在 PG 中长期留存，删除会话时需一并删除对应轮次（本轮未接线）。
- **多副本租约字段（LeaseOwner / LeaseExpiresAt）** → **本轮不实现**。启动时把全部 `queued`/`running` 轮次标记为 `interrupted`，在单副本部署下语义正确；多副本部署时会把其它副本的在途轮次误标为中断，属已知残留。
- **生成的 `CreateChatTurn` 不可直接用于幂等仲裁** → codegen 对带 `unique` 的列生成 `ON CONFLICT DO UPDATE`（同键 upsert），不暴露唯一约束冲突。`stores.CreateTurn` 改为普通插入，用 `errors.As` 取 `PGError` 并比对 SQLSTATE `23505` 判定为 `ErrDuplicate`。


---

## High-Level Technical Design

```go
// pkg/services/stores/convo_x.go —— 三个方法并入 ConvoStoreX 接口
// （sto.Convo() 返回的是内嵌 ConvoStoreX 的生成 store，单独声明 TurnStore 接口
//   会导致 sto.Convo().CreateTurn(...) 不存在）

func (s *convoStore) CreateTurn(ctx context.Context, in convo.ChatTurnBasic) (*convo.ChatTurn, error)
func (s *convoStore) GetTurnByIdempotencyKey(ctx context.Context, key string) (*convo.ChatTurn, error)
func (s *convoStore) UpdateTurnStatus(ctx context.Context, id string, status string, result string) error
```

入口侧的统一流程（以 `MessageHandler` 为例）：

```go
key := "wecom:" + msg.MessageID
turn, err := sto.Convo().CreateTurn(ctx, convo.ChatTurnBasic{
    IdempotencyKey: key, Status: "queued", OwnerID: ownerID, ...,
})
if err != nil {
    // 唯一约束冲突 → 已存在（仅 ErrDuplicate 走本分支，其余错误走降级路径）
    existing, err := sto.Convo().GetTurnByIdempotencyKey(ctx, key)
    if err != nil || existing == nil {
        // 查不到轮次：不进入重复分支（降级方向见 Risks 表）
        return
    }
    switch existing.Status {
    case "succeeded":
        // 直接返回已有结果，不重复执行
        return
    case "failed", "cancelled":
        // 允许同键重试：转为 running 后重新执行
    case "queued", "running", "interrupted":
        // 执行中或中断：不重复执行，也不返回结果
        return
    }
}
// 独占成功：转为 running 并执行
```

---

## Implementation Units

### U1. 契约与表结构

1. 在 `docs/convo.yaml` 增加 `ChatTurn` 模型与 store 声明（store 类型码 `LGCUD`）
2. 执行 `make codegen`
3. `data/schemas/` 的迁移 SQL 只写增量 DDL：`CREATE UNIQUE INDEX IF NOT EXISTS uk_turn_idem_key ON convo_chat_turn (idempotency_key);`（可重复执行）。契约里的 `unique:uk_turn_idem_key` 会被 codegen 写进 bun tag，`InitSchemas` 的 `CREATE TABLE IF NOT EXISTS` 已随建表生成该唯一约束，迁移只用于已建表环境的补齐

验收：`make codegen` 产物入库；`InitDB` 后表与索引存在；重复插入同键被拒。

### U2. 轮次存储

在 `pkg/services/stores/convo_x.go` 增加轮次的创建、按幂等键查询、状态更新三个方法，薄封装生成层。

验收：唯一约束冲突可被识别（返回可区分的错误，而不是通用 DB 错误）。

判定方式：仓库虽已导出 `stores.ErrDuplicate = pgx.ErrDuplicate`，但 `pgx.FilterError` 内部用的是类型断言而非 `errors.As`，且 `pgx.DoInsert` 对插入失败是 `fmt.Errorf("create %s fail: %w", name, err)` 包装——直接套用永远匹配不上。`CreateTurn` 内部应用 `errors.As` 取出 `pgdriver.Error`（即 `pgx.PGError`）并检查 `Field('C') == "23505"`，命中则返回 `stores.ErrDuplicate`；调用方以 `errors.Is(err, stores.ErrDuplicate)` 识别"已存在"，其余错误走降级路径。

### U3. 幂等判定接入频道入口

在 `handle_platform.go` `MessageHandler` 中：

1. 构造幂等键（含 Web 侧的 `owner_id`）
2. 尝试创建轮次
3. 冲突时按既有状态分派：`succeeded` 直接返回已存结果；`failed` / `cancelled` 转为 `running` 后重新执行；`queued` / `running` / `interrupted` 既不执行也不返回结果
4. 成功时转 `running` 并执行，执行后写终态与 `Result`
5. 同时把三条已有 Redis 去重路径（`wecom/websocket.go:334`、`feishu/websocket.go:242`、`feishu/webhook.go:281`）的 `IsDuplicate` 命中行为从"直接返回"改为"只记录不阻断"，判定权交给本单元

验收：同一 `message_id` 连续投递两次，只有一次进入模型调用；第二次拿到相同结果。

### U4. 幂等判定接入 Web 入口

在 `handle_convo.go` 的 SSE 与非流式分支同样接入。Web 侧的终态结果为最终答案文本。

验收：同一幂等键的两次 Web 请求只产生一次模型调用。

### U5. 企微 HTTP 路径的幂等覆盖

企微 HTTP 的重复投递由 U3 在 `MessageHandler` 的 PG 幂等统一覆盖——`MessageHandler` 是频道侧的汇合点，企微 HTTP 的 `go handler(...)` 最终也会走到那里。本单元因此不再在 `wecom_http.go` 单独接入 Redis 前置闸门（`HTTPChannel` 也无需新增 `dedup` 字段）。

验收：企微 HTTP 路径的重复投递不会进入第二次模型调用；判定以 PG 轮次状态为准（见 U7 用例）。

### U6. 中断状态

进程启动时扫描 `queued` 与 `running` 两种状态的轮次，统一标记为 `interrupted`。多副本部署下只回收租约已过期、或归属为本进程且已失联的记录（租约字段见「ChatTurn 模型设计」）。执行过程中 ctx 取消时，若已有工具副作用发生，同样标记 `interrupted`；副作用发生前取消则标记 `cancelled`。

验收：重启后无残留 `queued` / `running` 轮次；中断轮次不自动重放。

### U7. 回归测试

| 用例 | 断言 |
| --- | --- |
| 企微 HTTP 重复投递 | 只有一次进入 handler |
| 同幂等键并发插入 | 一个成功一个冲突 |
| 重复请求命中已完成轮次 | 返回相同 `Result`，无第二次模型调用 |
| 重复请求命中执行中轮次 | 不重复执行 |
| 重复请求命中 `failed` / `cancelled` 轮次 | 转为 `running` 后重新执行 |
| 重启后 `queued` / `running` 轮次 | 被标记为 `interrupted` |
| Web 同幂等键重复请求 | 只产生一次模型调用，第二次返回同一 `Result` |
| Web 重试 / 重新生成（`regen` 或与上一条同内容） | 跳过幂等判定，作为新请求重新执行 |
| 跨用户同内容请求 | 不命中对方的轮次，各自查不到对方的 `Result` |

---

## System-Wide Impact

| Area | Impact |
| --- | --- |
| `docs/convo.yaml` | 新增 `ChatTurn` 模型；触发 `make codegen` |
| `pkg/models/convo` | 新增生成模型 |
| `pkg/services/stores` | 新增轮次存储方法 |
| `pkg/web/api` | 两条 Web 入口接入幂等 |
| `pkg/services/channels` | 企微 HTTP 补去重；频道入口接入 PG 幂等 |
| `data/schemas` | 新增一条迁移 SQL |
| 外部行为 | 重复投递不再重复执行；用户不再收到两条相同回复 |

---

## Risks & Dependencies

| Risk | Mitigation |
| --- | --- |
| 幂等键派生碰撞导致正常请求被误判为重复 | 频道侧用平台唯一 `message_id`；Web 侧派生摘要含 session 与内容，相同内容的重复提问判为重复是有意接受的行为 |
| 轮次记录写入失败阻塞正常请求 | 写入失败时降级为"直接执行"并记 warn 日志，不把数据库问题放大成服务不可用 |
| 进程重启时的中断标记误伤正常轮次 | 启动时 `queued` 与 `running` 状态的轮次均为上次进程遗留，直接标记为 `interrupted`；不做时长筛选——时长筛选会让刚崩溃留下的年轻轮次永远停在非终态，永久占用其幂等键 |
| `interrupted` 轮次不重放导致用户消息确实丢失 | 这是有意取舍（R5）；用户可重新提问，下一次是新的幂等键 |
| 依赖 [计划 002](2026-10-29-002-feat-effective-tool-policy-plan.md)？ | 否。本计划不依赖策略对象；两者可并行 |
| 整体落地顺序 | 001 → 002 → 003 → 004。其中 004 依赖本计划的轮次锚点（`turn_id`），必须在本计划之后 |

---

## Documentation / Operational Notes

- 新增配置项：无（幂等键显式传入的 API 字段名待定）
- 新增 PG 表：`convo_chat_turn`
- 可按 `IdempotencyKey` 查询轮次；按 `Status` 统计异常轮次；按 `OwnerID` 按登录身份收窄查询面

---

## Sources & References

- `docs/reports/2026-10-27-whip-design-review.md` 第 4 节「可查询、可重放的聊天 turn」
- `pkg/services/channels/dedup.go`
- `pkg/services/channels/wecom/wecom_http.go`（缺去重的入口）
- `pkg/web/api/handle_platform.go`（频道侧汇合点）
- `docs/plans/2026-05-09-001-feat-event-driven-architecture-plan.md`
- [计划 002](2026-10-29-002-feat-effective-tool-policy-plan.md)（授权门控）
- [计划 004](2026-10-29-004-feat-conversation-persistence-basis-plan.md)（transcript 与事件游标）
