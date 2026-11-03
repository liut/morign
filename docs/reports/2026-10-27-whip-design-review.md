---
title: Morign 可借鉴的 whip 设计
type: report
status: completed
date: 2026-10-27
---

# Morign 可借鉴的 whip 设计

## 结论

whip 最值得借鉴的是长会话 agent runtime 的约束，而不是它的完整技术栈。对 Morign 当前形态而言，优先价值集中在六个方面：

1. 工具调用结果必须完整、可恢复。
2. 动态工具目录与执行授权必须分离并共用同一套判定。
3. 原始对话记录与模型上下文必须是两个不同的概念。
4. 一次聊天需要有可查询、可重放的 turn 生命周期。
5. 子 agent 应先解决一次性委派，再考虑持久化递归会话。
6. 上下文成本、失败路径和恢复行为必须可观测、可评测。

以下六点按独立设计主题记录。

---

## 1. 工具结果完整性

### whip 的做法

whip 把工具执行视为受控操作：每次调用有稳定 identity，最终形成成功、失败、拒绝或中断结果。大结果不完整塞回模型，而是保存为不可变内容并通过 handle 引用。模型拿到的是有界 preview 和继续读取的方法。

相关实现与说明：

- [capability dispatcher](../../../whip/internal/capability/dispatcher.go)
- [工具与授权说明](../../../whip/docs/tools.md)

### Morign 当前状态

[tool_executor.go](../../pkg/services/agent/tool_executor.go) 在以下路径会留下空的工具结果：

- 参数 JSON 无法解析；
- 工具执行发生 panic；
- `Registry.Invoke` 返回 error；
- 工具类型不是预期值。

assistant 消息仍然包含 `tool_calls`，但消息列表没有对应的 `tool` result。这会产生不完整的下一轮上下文，严重时模型只会停滞或重复调用。

超大结果目前由 [truncateToolResult](../../pkg/services/agent/tool_executor.go) 只保留头部。结论、错误尾部和引用信息可能正好位于被截断的部分。

### 建议的 Morign 形态

为工具执行定义统一结果：

```text
status
content
error_code
metadata
duration
content_handle?
```

必须满足：

- 每个 `tool_call` 产生且只产生一个结果；
- 参数解析失败、工具不存在、panic、超时和执行错误都转换成结果；
- 批量执行可以并发，但消息组装保持调用顺序；
- 执行前校验输入 schema；
- 需要结构化结果的工具校验输出 schema；
- 超过内联上限的结果保存原文，返回 head-tail preview 和可读取 handle；
- 模型可以用现有或新增工具分页读取完整内容。

### 验收标准

- 任意工具错误路径之后，assistant 的每个 tool call 都有对应 tool result；
- 工具 panic 不会导致整批结果缺失；
- 大输出不会只保留开头；
- 完整输出可以从 handle 重新读取。

---

## 2. 动态工具目录与执行授权

### whip 的做法

whip 不把所有 MCP schema 永久塞进模型工具列表，而是提供稳定的发现和调用入口：

- `mcp.search`
- `mcp.describe`
- `mcp.list_servers`
- `mcp.list_tools`
- `mcp.call`

工具是否可见、是否允许调用、调用哪个 server，以及权限怎么变化，始终由同一套 capability 和 grant 规则决定。MCP 重连或新增工具不会扩大已有子节点的授权。

### Morign 当前状态

[registry.go](../../pkg/services/tools/registry.go) 的 `ToolsFor` 会把所有已加载的全局 MCP 工具直接合并进模型工具列表。MCP 增删或重连会改变模型看到的工具定义，同时带来：

- 工具 schema 占用大量上下文；
- 稳定 prompt 前缀随 MCP 状态变化；
- 当前 keeper/公开两级权限无法表达按用户、频道或子 agent 收窄的 MCP 权限；
- 可见性和实际调用没有统一的授权对象。

### 建议的 Morign 形态

内置工具可以继续直接暴露，动态 MCP 和外部能力使用统一目录入口：

```text
tool_search(query, source?, limit?)
tool_describe(tool)
tool_call(tool, arguments)
```

模型初始只看到稳定的少量工具说明，按需发现完整 schema。执行时重新验证：

- 当前用户和 keeper 身份；
- 频道来源；
- MCP server；
- 原始 tool 名称；
- 工具定义版本或 digest；
- 当前 skill 或子 agent 的授权范围。

增加统一对象，例如 `EffectiveToolPolicy`，同时驱动：

- 模型可见的工具列表；
- 执行阶段的硬门控；
- 子 agent 可继承的权限上限；
- 工具调用日志中的授权来源。

### 验收标准

- MCP 工具的 schema 不直接进入初始工具列表；
- 新增 MCP 工具不会静默扩大已有会话或子 agent 的权限；
- 从工具列表隐藏的工具无法通过知道名字直接调用；
- 可见面和执行面使用同一份授权判定；
- MCP 状态变化不会无条件改变稳定 prompt 前缀。

---

## 3. 原始记录与模型上下文分离

### whip 的做法

whip 保存 append-only transcript，模型实际看到的是由以下内容组成的派生视图：

- 稳定 system prompt；
- 增量维护的摘要；
- 最近若干完整 turn；
- 需要时通过 `context.inspect/search/read` 查询的原始内容。

压缩使用 provider 返回的真实 prompt token，而不是只依赖字符估算。旧的大工具结果会退化成指向原文的占位符；最近指令和正在执行的工具对不会被拆散。逐轮的预算提示、重试提示和 hook 通知放在请求尾部，避免破坏前缀缓存。

相关说明：

- [agent loop](../../../whip/docs/agent-loop.md)
- [context cost handoff](../../../whip/docs/context-cost-handoff.md)

### Morign 当前状态

[conversation.go](../../pkg/services/stores/conversation.go) 将对话历史保存在 Redis，并限制为 25 条、24 小时。历史项只保存用户问题和最终 assistant 文本，不保存完整工具调用轨迹。

[history.go](../../pkg/models/aigc/history.go) 的 `calcTokens` 实际统计字符字节数；[handle_convo.go](../../pkg/web/api/handle_convo.go) 使用 50KB 字符预算筛选历史。这既不是模型真实 token 预算，也无法判断图片、工具结果和大型知识库片段对上下文的影响。

### 建议的 Morign 形态

将事实数据和模型视图分开：

- PostgreSQL 保存 append-only 的 turn、message、tool call、tool result 和 compaction；
- Redis 负责缓存、短期状态和跨请求索引；
- 每次模型请求从原始记录生成独立上下文视图；
- 记录模型配置中的 context window；
- 优先使用 provider 返回的 `prompt_tokens` 触发压缩；
- 压缩保留摘要、当前用户原始请求和近期完整 turn；
- 摘要增量合并，并记录其覆盖的原始记录范围；
- 旧的大型工具输出替换为带 handle 的占位符；
- 逐轮通知放在请求尾部，稳定 system prompt 和已有历史前缀保持字节稳定。

### 验收标准

- 长会话不会只因为 Redis 的 25 条上限而丢失可查询历史；
- 压缩依据来自真实 provider usage 或明确的模型窗口配置；
- 压缩不会留下孤立的 tool call/tool result；
- 模型仍能通过 handle 读取被压缩前的完整工具输出；
- 连续 turn 的稳定前缀不会因时间、预算提示或重试状态变化而失效。

---

## 4. 可查询、可重放的聊天 turn

### whip 的做法

whip 在接受用户命令前先持久化 command：

- `client_id`
- `command_id`
- `request_digest`
- 当前状态
- 最终 outcome

重复请求返回已有状态或结果，不会再次执行。输出事件有单调递增序号，客户端先读取 snapshot 和 cursor，再订阅 cursor 之后的事件。外部副作用已经发送但结果不确定时，标记为 interrupted/outcome unknown，并明确不自动重放。

相关实现：

- [command admission](../../../whip/internal/session/command.go)
- [event replay](../../../whip/internal/session/event.go)

### Morign 当前状态

Web SSE、非流式接口和频道处理路径都将一次执行主要绑定在请求或回调生命周期内。频道去重是 Redis 的短窗口，不能表达以下差异：

- 消息已接收但尚未开始；
- 模型正在执行；
- 工具副作用已经发生；
- 执行中断；
- 已完成并产生最终结果。

客户端断线、进程重启或 webhook 重试后，缺少统一入口查询这次 turn 的真实状态。

### 建议的 Morign 形态

增加轻量的 `chat_turn`：

```text
turn_id
session_id
source
idempotency_key
request_digest
status
last_event_seq
result
created_at
updated_at
```

建议状态机：

```text
queued -> running -> succeeded
                  -> failed
                  -> cancelled
                  -> interrupted
```

配套约束：

- 调用模型前先持久化 turn；
- 事件写入带单调递增序号；
- SSE 支持从 `after_seq` 继续读取；
- 频道平台 `message_id` 用作持久幂等键；
- 工具操作记录自己的 admission 和 outcome；
- 已发送但结果不确定的外部调用标记为 interrupted，不自动重试；
- 只有客户端明确发起新请求时，才重新执行用户操作。

该方案不需要复制 whip 的 daemon 和 JSON-RPC，只需要数据库状态机、稳定 identity 和事件游标。

### 验收标准

- 相同幂等请求不会重复执行模型或副作用工具；
- 客户端断线后可以查询 turn 状态并补读事件；
- 进程中断后，已完成的 turn 可恢复，未完成的 turn 明确标记为 interrupted；
- 外部工具结果不确定时不会自动重放。

---

## 5. 子 agent 的一次性委派与持久化边界

### whip 的做法

whip 明确区分：

- 独立、无状态的模型调用，用于一次性分析或批量推理；
- 有 identity、transcript、budget、mailbox 和后续轮次的 agent session。

只有任务需要独立生命周期或后续交互时，才创建保留型子 agent。父子间通过显式、有界的消息和结果通信，子 agent 的普通 assistant 输出不会隐式注入父上下文。

相关实现：

- [mailbox](../../../whip/internal/session/mailbox.go)
- [递归 runtime 说明](../../../whip/docs/rlm-runtime.md)

### Morign 当前状态

现有 [子 agent 计划](../plans/2026-09-15-001-feat-web-subagent-mechanism-plan.md) 采用同步、进程内、一次性嵌套执行。这个边界适合第一阶段，但还缺少：

- 明确的结构化完成结果；
- 子 agent 用量归集；
- 从父上下文有效授权派生的工具权限；
- 明确的中断和结果不确定性；
- 后台执行或跨请求恢复能力。

### 建议的 Morign 形态

第一阶段继续使用同步一次性委派，并补齐契约：

- 输入包含任务和调用方显式提供的上下文；
- 输出使用结构化结果，例如 `summary`、`findings`、`evidence_refs`；
- 子 agent 的 usage 计入父 turn 的用量记录或独立 cost entry；
- 子工具权限从父上下文的 `EffectiveToolPolicy` 快照收窄，不维护独立的名字白名单；
- 父请求取消时，子执行立即停止；
- 子 agent 失败作为结构化工具结果返回，不破坏父循环；
- 默认不递归，也不允许无限并发。

只有出现以下需求时，才引入持久化 child task：

- 任务需要脱离当前 HTTP 请求继续运行；
- 需要后续追问；
- 需要跨副本恢复；
- 需要独立预算、配额或人工审批；
- 需要向父节点异步投递多个进度或结果。

### 验收标准

- 父上下文只增加一条有界、结构化的委派结果；
- 子 agent 的工具调用不会进入父 transcript；
- 子 agent 的 token 和费用可见；
- 子 agent 无法获得父级未授权的工具或 MCP server；
- 请求取消后没有继续运行的孤儿任务。

---

## 6. 观测与确定性评测

### whip 的做法

whip 将一次 turn 拆成可追踪的 span，记录：

- 模型请求和重试；
- prompt、cache、output token；
- 延迟；
- 工具调用；
- compaction；
- budget reservation 和 settlement；
- 子 agent 和外部能力。

同时使用长上下文、递归、超大工具输出和恢复场景进行确定性评测，使成本和失败行为可以被重复验证。

### Morign 当前状态

已有 usage record，但主要停留在请求级 input/output/total token。上下文决策、工具错误、重试、子 agent 和中断路径缺少统一 trace，因此很难回答：

- 这次为什么压缩；
- 哪个工具消耗了最多上下文；
- 缓存命中是否下降；
- 是否发生重复工具调用；
- 子 agent 花了多少 token；
- 客户端断线后哪些副作用已经发生。

### 建议的 Morign 形态

每个 chat turn 记录一条结构化 trace：

```text
turn_id
model_requests
tool_calls
prompt_tokens
cached_tokens
output_tokens
compaction_count
retry_count
child_agent_usage
latency
error_code
```

不要求第一版接入完整可视化，但应保证日志可以按 `turn_id`、`session_id` 和 `tool_call_id` 聚合。

建议补充确定性测试场景：

- 100 轮对话后上下文仍受 token 预算控制；
- 连续超大工具结果可以被 handle 替换和重新读取；
- 工具参数错误、panic、超时均产生完整 result；
- MCP 断连、重连和新增工具不扩大授权；
- 客户端在流式输出中断线后可以恢复状态；
- 频道重复 webhook 不会重复执行副作用；
- 子 agent 超时、取消和失败不会破坏父循环；
- 稳定 prompt 前缀在普通 turn 之间保持字节一致。

### 验收标准

- 能按一个 turn 还原所有模型调用、工具调用和错误；
- 能区分已知使用量、未知使用量和重试产生的使用量；
- 长会话和故障场景有自动化回归测试；
- 修改压缩、工具目录或授权逻辑时，成本和恢复行为有可重复验证。

---

## 推荐落地顺序

| 阶段 | 工作 | 目标 |
| --- | --- | --- |
| P0 | 工具结果完整性 | 消除缺失 tool result 和静默失败 |
| P0 | EffectiveToolPolicy 与 MCP 动态目录 | 收敛权限、稳定工具面、减少 schema 上下文 |
| P0 | provider usage + 真实 token 预算 | 阻止长会话无界增长 |
| P1 | 原始 turn 持久化与上下文投影 | 支持长期历史、压缩和内容 handle |
| P1 | chat turn 状态机与事件游标 | 支持断线恢复和 webhook 幂等 |
| P2 | 子 agent 结果契约、权限快照和用量归集 | 控制委派成本和副作用 |
| P2 | trace 与确定性评测 | 固化成本、恢复和安全行为 |

## 不建议整体复制

以下能力适合 whip 的本地长期 coding agent 形态，但不应作为 Morign 当前阶段的目标：

- Starlark/QuickJS execution engine；
- 独立 kernel worker 和 checkpoint 体系；
- 完整 daemon、Unix socket 和 JSON-RPC 协议；
- 默认递归的持久化 agent tree；
- 以单一 `rlm_exec` 替换现有工具面。

Morign 应吸收这些设计背后的约束，而不是承担它们的实现复杂度。
