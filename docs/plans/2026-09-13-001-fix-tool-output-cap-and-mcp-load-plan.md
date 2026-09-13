---
title: 约束工具结果长度并修复 MCP 服务器加载上限
type: fix
status: completed
date: 2026-09-13
origin: docs/improvements/2026-09-13-hermes-agent-comparison.md
---

# 约束工具结果长度并修复 MCP 服务器加载上限

## Summary

两处独立的小改动：去掉 MCP 服务器加载时硬编码的 2 条上限，让所有活跃的远程 server 都能在启动时加载；在工具结果进入 LLM 上下文前加一道可配置的长度上限，避免单次 MCP 或抓取结果撑爆后续请求。改动集中在两个函数与一个配置项，不触碰代码生成契约。

---

## Problem Frame

`LoadServers` 的注释写的是"加载所有 Running 状态的 MCP Server"，实现里却写死了 `spec.Limit = 2`——第 3 个及以后的活跃 server 永远不会被加载，运维侧表现为"配置了却不生效"，且没有任何日志提示。

另一侧，工具结果格式化没有任何长度约束。工具结果既不写入历史也不推送给客户端（HTTP SSE 与频道流式两条路径都显式跳过 `ToolResult` 事件），它只存在于当轮 LLM 上下文里。一次异常巨大的返回（MCP server 返回整页 HTML、抓取一个超大页面）会持续占用后续每次 API 调用的上下文，且事后无法通过查历史补救。hermes-agent 为每个工具单独声明 `max_result_size_chars` 正是针对这一点。

---

## Requirements

- R1. 所有 `is_active` 且传输类型为远程的 MCP server 都在启动时被加载，不再受数量上限截断。
- R2. 单次工具结果在进入 LLM 上下文前有长度上限；超限时截断，并留下模型可读、可操作的截断说明。
- R3. 上限通过环境变量配置，`0` 表示不限制；默认值开箱生效。
- R4. 既有行为不变：非远程（stdIO/inMemory）server 仍被跳过；单个 server 加载失败不影响其他 server；未超限的结果与改动前逐字节一致。
- R5. 两项变更都有回归测试；不修改 `docs/*.yaml` 契约，不触发 `make codegen`。

---

## Scope Boundaries

- 不引入"按工具声明上限"——那需要改能力契约并重新生成模型与存储层，留给后续。
- 不引入工具结果的持久化或展示（属 origin 文档的 C-1）。
- 不改变非远程 server 的跳过策略，也不改加载的并发模型。
- 不改 `ResultLogs` 的日志格式——日志路径与 LLM 上下文路径互不影响。

---

## Context & Research

### Relevant Code and Patterns

- `pkg/services/tools/registry.go` `LoadServers`：唯一的加载入口，`main.go` 与 `pkg/web/api/api.go` 各调用一次。
- `pkg/services/agent/tool_executor.go` `formatToolResult`：所有工具结果的唯一出口，内置工具、MCP 工具、频道工具都经过它。
- `pkg/utils/words/text.go` `TakeHead`：按 rune 截断并可附省略号，仓库既有习惯（`pkg/models/convo/memory.go`、`pkg/web/api/handle_convo.go` 都在用）。
- `pkg/settings/config.go`：envconfig 前缀为 `MORIGN_`，`MaxLoopIterations`、`VectorLimit` 等同类开关的写法可直接照抄。
- 测试写法：`pkg/services/agent/tool_executor_test.go` 的 `TestFormatToolResult` 为表驱动；`pkg/services/stores/capability_rerank_test.go` 与 `pkg/services/agent/skill_inject_test.go` 演示了 `settings.Current` 的覆盖与恢复。

### Institutional Learnings

- `docs/solutions/` 中无直接相关的条目。

### External References

- 跳过外部研究：仓库内已有明确同类模式可循（配置项 + rune 截断 + 表驱动测试），改动面小且方向已定。

---

## Key Technical Decisions

- **上限实施在 `formatToolResult` 的出口处，而非逐工具声明**：一个漏斗覆盖全部工具类型，且不需要新的工具描述字段。代价是无法对个别工具放宽——这正是 Scope Boundaries 里递延的那一项。
- **按 rune 而非字节截断**：本项目以中文场景为主，按字节截断会把 CJK 内容砍掉约三分之二。
- **MCP 修复采用"去掉限制"而非"分页循环"**：上游分页在 `limit == 0` 且 `page == 0` 时不加 LIMIT 子句，正好等价于全量，无需自写分页。
- **加载循环依赖现有的窄接口 `stores.MCPStore` 而非整个 `Storage`**：这样单测只需一个遵守分页语义的假实现即可覆盖完整加载路径，同时 `LoadServers` 的对外签名保持不变。
- **截断说明对模型可见且可操作**：模型需要知道自己只看到一部分、以及如何取到其余内容，否则会基于残缺内容作答。
- **不新增按工具字段**，因此不触碰 `docs/*.yaml`，不触发 codegen。

---

## Open Questions

### Resolved During Planning

- **`LoadServers` 的 `Limit = 2` 是否有意？** 判定为遗留而非有意约束：该行与函数由同一次提交引入（2026-03-12）后再未改动，且与函数注释"加载所有"直接矛盾；上游分页在 `Limit = 0` 时即为全量，去掉该行即可达成注释描述的行为。
- **截断是否影响用户可见内容？** 不影响。`pkg/web/api/handle_convo.go` 与 `pkg/web/api/handle_platform.go` 两条路径都显式跳过 `ToolResult` 事件，工具结果只进 LLM 上下文。

### Deferred to Implementation

- 默认上限的具体取值：以"不影响内置工具的典型输出"为准，先取 20000 字符，上线后按截断提示出现的频率回调。
- 截断说明的最终措辞。
- 集成测试种子数据的清理方式（现有 integration 测试用的是先建后删的写法，实现时对齐）。

---

## Implementation Units

### U1. 移除 MCP 服务器加载上限

**Goal:** 让所有活跃的远程 MCP server 都在启动时被加载。

**Requirements:** R1, R4, R5

**Dependencies:** None

**Files:**
- Modify: `pkg/services/tools/registry.go`
- Test: `pkg/services/tools/zero_tools_test.go`
- Test: `pkg/services/stores/integration_test.go`

**Approach:**
- 去掉 `spec.Limit = 2`，让分页字段取零值——上游 `QueryPager` 在 `limit == 0 && page == 0` 时不加 LIMIT，即全量返回。
- 把筛选条件抽成一个具名构造（`is_active` 加排序），并在其上写明"加载全部"的意图，避免以后又被加回数量限制。
- 加载日志改为报告实际取到的条数，便于运维确认上限已解除。
- **不要依赖 `ListServer` 返回值里的总数**：无 limit 时上游只做 `Scan` 而不计数，`total` 恒为 0；日志与调用方都以实际取到的切片长度为准。
- 把加载循环抽到一个接收窄接口（`stores.MCPStore`）的内部方法，`LoadServers` 保持现有签名转调它。这样单测可以用一个遵守分页语义的假实现覆盖完整加载路径，而不必伪造整个 `Storage`。
- 保持循环内的既有语义：非远程类型跳过；`AddServer` 失败只告警并继续下一个。

**Patterns to follow:**
- `pkg/services/tools/registry.go` 中 `AddServer` 现有的错误处理与日志风格。

**Test scenarios:**
- Happy path: 用一个遵守分页语义的假 `MCPStore`（按 `spec.Limit` 截断返回）提供 3 条活跃 server，加载路径应触达全部 3 条——这样断言的是行为，重加上限会直接让该用例失败。
- Integration: 真实库中放入 3 条 `is_active` 的 server，以无限定的筛选条件查询应全部返回（钉住"上游 `Limit = 0` 即全量"这一外部依赖）。
- Edge case: 库中没有活跃 server 时，加载流程正常结束且不报错。
- Edge case: 返回的 server 全部为非远程类型时，加载流程正常结束且不注册任何工具。
- Error path: 其中一条 server 连接失败时，其余 server 仍会被尝试加载。

**Verification:**
- 配置 3 个及以上活跃 MCP server 的实例重启后，启动日志报告全部 server 已加载，且 `GET /api/tools` 能看到来自第 3 个及以后 server 的工具。

### U2. 给工具结果加长度上限

**Goal:** 单次工具结果超过上限时被截断，且模型能感知并知道如何取回完整内容。

**Requirements:** R2, R3, R4, R5

**Dependencies:** None

**Files:**
- Modify: `pkg/settings/config.go`
- Modify: `pkg/services/agent/tool_executor.go`
- Modify: `README.md`
- Test: `pkg/services/agent/tool_executor_test.go`

**Approach:**
- `pkg/settings` 新增一个整型开关（`MORIGN_TOOL_RESULT_MAX_CHARS`），默认 20000，`0` 表示不限制；在 `README.md` 的环境变量表补一行。
- 默认值的校准锚点：`fetch` 的 `max_length` 默认 5000 字符，`kb_search` 最多返回 `VectorLimit`（默认 6）篇文档，`memory_recall` 默认 5 条。20000 约为 `fetch` 默认值的四倍，落在内置工具的典型输出之上，只拦截病态返回。
- 在 `formatToolResult` 返回前统一施加一次上限：先取到完整字符串（涵盖 `content[].text` 主分支与 `structuredContent`/JSON 兜底分支），再按 rune 截断并附截断说明。
- 截断说明包含三要素：保留了多少字符、原始多少字符、如何取到其余内容。用方括号包裹，便于日志与测试断言。
- 未超限时返回原字符串，保证 R4 的逐字节一致。

**Technical design:** *(方向性说明，不是实现规范)*

| 条件 | 结果 |
|---|---|
| 上限为 0 | 原样返回 |
| 结果长度 ≤ 上限 | 原样返回 |
| 结果长度 > 上限 | 前 N 个字符 + 截断说明 |

**Patterns to follow:**
- `pkg/utils/words/text.go` 的 `TakeHead`（rune 安全）。
- `pkg/services/stores/capability_rerank_test.go` 的 settings 覆盖/恢复写法。

**Test scenarios:**
- Happy path: 结果短于上限时逐字符等于原始内容。
- Edge case: 恰好等于上限时不被截断。
- Edge case: 超限的 CJK 内容按字符截断，结果不以半个多字节字符结尾。
- Edge case: 上限设为 0 时超长内容原样返回。
- Edge case: 空结果或 `nil` 结果返回空字符串，不附截断说明。
- Error path: 结果只走 `structuredContent` 或 JSON 兜底分支时，上限同样生效。
- Integration: 通过 `ToolExecutor` 执行一个返回超长内容的工具，落进 messages 的 tool 消息内容包含截断说明，且 `terminate` 语义不受影响。

**Verification:**
- 新增的表驱动用例通过，既有 `TestFormatToolResult` 与 `TestExecuteToolCalls*` 全部保持通过。
- 构造一个返回超大文本的工具调用，第二次 LLM 请求中的工具消息不超过上限。

---

## System-Wide Impact

- **Interaction graph:** `formatToolResult` 只在 `ToolExecutor.ExecuteToolCalls` 内被调用一次，两条上层路径（HTTP SSE、频道）都经由它；截断不改变事件结构，也不影响 `terminate` 判定与 `Before`/`AfterToolCall` 钩子。
- **Error propagation:** 两处改动都不新增错误路径——MCP 单点失败仍是告警并继续，截断不返回错误。
- **State lifecycle risks:** 工具结果不落库、不推送，所以截断是**不可恢复**的。这是选择默认值时必须权衡的点，也是截断说明必须给出取回方式的原因。
- **API surface parity:** `GET /api/tools` 的输出会因 U1 变多（出现更多 MCP 工具），这是预期行为，不是破坏性变更。
- **Integration coverage:** 单测覆盖截断逻辑本身；MCP 的"全量返回"依赖上游分页语义，用商店层集成测试钉住。
- **Unchanged invariants:** 非远程 server 仍不加载；未超限结果逐字节不变；`docs/*.yaml` 契约与生成代码不变。

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| 默认上限过低，截断合法知识库答案且不可恢复 | 默认值取在本项目内置工具典型输出之上；截断说明告知模型可缩小查询范围重取；上线后按提示出现频率回调 |
| 活跃 MCP server 很多时启动变慢（串行建连） | 保持既有串行与失败跳过语义；日志报告条数与逐个失败原因，便于运维发现异常配置 |
| 上游分页语义变化导致"全量"失效 | 商店层集成测试钉住 `Limit = 0` 返回全部行；该测试失败即为上游行为变更的信号 |
| 截断说明被模型当作普通内容回显 | 说明使用方括号包裹并置于末尾，便于人工与测试识别 |

---

## Sources & References

- **Origin document:** [docs/improvements/2026-09-13-hermes-agent-comparison.md](../improvements/2026-09-13-hermes-agent-comparison.md)（A-1 条目；第 4.7 节的强化依据）
- Related code: `pkg/services/tools/registry.go`、`pkg/services/agent/tool_executor.go`、`pkg/settings/config.go`
