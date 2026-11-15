---
title: 'fix: 工具调用区分客户端取消与工具故障，并为 MCP 调用加超时兜底'
type: fix
status: completed
date: 2026-11-13
origin: docs/plans/2026-10-29-001-fix-tool-result-completeness-plan.md
---

# fix: 工具调用区分客户端取消与工具故障，并为 MCP 调用加超时兜底

## Summary

排查「前端 `regen=true` 时会忽略最后一次历史、重新发起 LLM 请求并执行相应 tools」这一行为时，确认 **regen 本身是正常设计**，但在排查过程中暴露了两个真实缺陷：

1. **客户端取消被伪装成工具故障**：regen 会先中止同一会话的旧流，被取消的那一轮的 MCP 工具调用随之收到 `context canceled`，该错误被当作普通工具失败喂给模型，模型据此重新规划、反复调用工具。
2. **MCP 调用没有超时兜底**：`transport.NewStreamableHTTP` 未设 timeout，`CallTool` 只依赖调用方 ctx；而 Web 链路的 ctx 直接来自 HTTP 请求（无 deadline），一旦某次响应读取出岔子，轮次永久停在 `status=2`（执行中），`finishTurn` 永不调用。

**不动 regen 的语义**：忽略最后一次历史、重新发起 LLM、重新执行工具，都是既定设计（见下方「regen 行为核查」）。

---

## Problem Frame

### regen 行为核查（结论：正常）

用户反馈的现象逐条对照代码：

| 现象 | 代码依据 | 判定 |
|------|----------|------|
| 忽略最后一次历史 | `handle_convo.go:92-103` 设 `skipLast`；`prompt.go:142` 的 `historyMessages` 用 `break` 丢弃最后一轮 | 符合设计 |
| 重新发起 LLM 请求 | `handle_turn.go:245-257` `webTurnKey` 在 `retry` 时追加 nonce，regen 绕过幂等新建一轮 | 符合设计 |
| 重新执行相应 tools | 工具调用由模型每轮重新决定；轨迹按 `turn_id` 分列于 `convo_tool_record`，旧轮次不被覆盖 | 符合设计 |

配套的写侧同样自洽：`stores/conversation.go:148-186` 的 `AddHistory` 发现「最后一条 `User` == 本轮 prompt」时先 `RPop` 再 push —— **regen 替换旧回答而非堆积**。

SSE 链路一轮只写一条历史（`handle_convo.go:338-346`，`UserPrompt` 只在整轮结束时设置），所以 `skipLast` 跳过的正是上一轮完整的 (user, assistant) 对。

### 缺陷一：客户端取消被伪装成工具故障

现场数据（session `719117618944090112`，2026-11-13 11:39）：

```
turn 719183008252309652  seq=4  status=error
  tool 'exec' call failed: transport error: failed to send request:
    failed to send request: Post "http://192.168.64.3:2280/mcp/": context canceled
```

成因链：

1. 前端点 regen → `chatStore.abortByCsid(startedCsid)` 中止同一会话的所有旧流（Calisyn `src/views/chat/index.vue:478`）
2. 旧轮次的 HTTP 请求断开 → 后端 `r.Context()` 被取消
3. 旧轮次正在执行的 MCP 工具调用随之取消 → `Post ...: context canceled`
4. `agent/tool_executor.go:226-231` 的 `callTool` 捕获该 error → `toolErrorResult(...)`，构造出一条「工具执行失败」的结果
5. 该结果作为正常的 tool message 喂回模型 → 模型看到「工具坏了」，于是重新规划、再次调用工具

第 4 步是问题所在：**「客户端主动取消」与「工具真的执行失败」被归成了同一类**。前者不该产生任何工具结果。

### 缺陷二：MCP 调用无超时兜底

`pkg/services/tools/registry.go:314` 用 `transport.NewStreamableHTTP` 建连，**未设置 timeout**；`client.CallTool` 的 ctx 直接来自 `agent_loop.go` 的调用方 ctx。

而 Web 链路的 ctx 是 `r.Context()` —— 只有客户端断开才取消，**没有 deadline**。这与 `docs/plans/2026-10-29-001-fix-tool-result-completeness-plan.md` 中「不引入独立的工具超时配置，ctx 取消与 deadline 已覆盖」的决策前提不符：那条决策假定调用方 ctx 带 deadline，Web 链路不满足。

后果：某次 MCP 响应读取出岔子时（响应已在服务端生成但客户端未消费，见 `netstat` 中 `RecvQ=6740` 而 `SendQ` 归零的连接），请求无限期挂起：

- 轮次永久停在 `status=2`（执行中），`updated` 停在创建那一刻
- `finishTurn` 永不调用 → 状态永远不变
- SSE 流不再产出任何事件 → 客户端表现为「发完工具调用后就没有后续反馈」

对照：`seq=3` 那条工具结果是 `exec failed: command timeout` —— **工具自身**有超时保护，但 **MCP 传输层**没有。

---

## Requirements

**取消语义**

- R1. 工具执行期间调用方 ctx 被取消时，不再构造「工具执行失败」的工具结果。取消是请求终止，不是工具故障。
- R2. 取消导致的终止沿用既有中断路径：轮次以 `TurnStatusInterrupted`（6）收尾，不产生模型可见的错误文本。
- R3. 工具自身确实失败（超时、返回错误、参数非法等）时，行为不变 —— 仍构造结构化错误结果喂给模型。

**超时兜底**

- R4. MCP 工具调用具备超时上限，超时后按工具失败处理（构造错误结果），不让轮次无限期停在 `status=2`。
- R5. 超时上限可配置，默认值取一个与工具实际耗时相称的量级（现有 `strata__exec` 实测 130ms–3.2s，`command timeout` 出现在约 9 分钟档）。
- R6. 不引入「全局工具超时配置项」以外的额外配置面：超时作用于 MCP 传输层，不改 `Registry.Invoke` 的既有返回约定。

**不变项**

- R7. regen 的语义不变：仍然忽略最后一次历史、重新发起 LLM、重新执行工具、替换而非追加历史。
- R8. `Registry.Invoke` 的返回约定不变（工具自报失败返回 `error=nil` + `isError=true`，走「成功」分支）。
- R9. ~~工具结果事件仍不发给客户端~~ —— **已修订**（2026-11-15，见 `docs/plans/2026-11-15-001-feat-tool-result-feedback-plan.md`）：Web SSE 链路改为下发工具执行结果的元信息（`role=tool` + `toolResult`），供前端把工具卡片从「已发起」更新到终态。保留的语义：客户端取消时整批结果丢弃，取消既不外发也不伪装成失败；频道链路（`handle_platform.go`）仍不下发工具结果。

---

## Scope Boundaries

- 不改 regen 的触发条件、幂等键派生与历史写入策略。
- 不改工具执行器的并发模型（一批工具并发执行、序号还原配对关系）。
- 不改 `convo_tool_record` 的表结构与字段。
- 不改 provider（Anthropic / OpenAI / Ollama）的请求体与响应解析。
- 不改工具结果的截断策略（`ToolResultMaxChars`）。

---

## Context & Research

### Relevant Code and Patterns

- 取消被吞的入口：`pkg/services/agent/tool_executor.go:226-231`（`callTool` 的 `err != nil` 分支）
- 错误结果构造：`pkg/services/agent/tool_executor.go:281-287`（`toolErrorResult`）
- ctx 传递链：`pkg/web/api/handle_convo.go:262` → `pkg/services/agent/agent_loop.go:60` → `pkg/services/agent/tool_executor.go:226`
- MCP 传输层建连：`pkg/services/tools/registry.go:314`（`transport.NewStreamableHTTP`）
- 工具调用出口：`pkg/services/tools/registry.go:508-526`（`callServerToolWithClient`）
- 轮次收尾：`pkg/web/api/handle_turn.go:96-116`（`finishTurn`）—— `ctx.Err() != nil` 且工具跑过 → `TurnStatusInterrupted`
- 既有「超时由 ctx 免费获得」的决策：`docs/plans/2026-10-29-001-fix-tool-result-completeness-plan.md` 第 150–153 行
- 既有中断状态语义：`docs/plans/2026-10-29-003-feat-turn-idempotency-state-machine-plan.md`

### Institutional Learnings

- **`AGENTS.md`**：「日志基于 `log/slog`」—— 新增的取消/超时分支须留日志，否则线上无法区分「取消」与「故障」。
- **`docs/plans/2026-10-29-001`**：错误结果的构造应收敛到单一出口（`toolErrorResult`），新增路径复用它而非另起炉灶。
- **本仓库既有模式**：超时通过 `context.WithTimeout` 施加在具体调用上（如 `handle_convo.go:365` 的 4s），而非改全局配置。

---

## Key Technical Decisions

- **取消不构造错误结果，而是终止整轮**：在 `callTool` 里先判 `ctx.Err() != nil`，是则直接返回取消态（不产出 tool result）。执行器需要在「取消」与「失败」之间做出区分 —— 取消时该批工具的后续调用同样应立即返回，整轮随之结束。
- **取消走既有中断路径**：`finishTurn` 已经把 `ctx.Err() != nil && toolRan` 映射为 `TurnStatusInterrupted`，无需新增状态。R2 由此免费获得。
- **超时施加在 MCP 传输层**：给 `transport.NewStreamableHTTP` 的 http client 设 timeout，或在 `callServerToolWithClient` 用 `context.WithTimeout` 包一层。前者覆盖所有 MCP 请求（含 `tools/list` 等初始化请求），后者更精准地只覆盖工具调用。
- **超时按工具失败处理而非取消**：超时是工具自己的问题（服务端没响应），模型应当知道并重新规划。这与「客户端取消」语义相反，两条路径必须分开。
- **默认超时要够宽**：现有 `strata__exec` 最慢 3.2s，但 `command timeout` 出现在分钟级 —— 默认值应取分钟级，否则会把正常的长任务误判为超时。

---

## Output Structure

    pkg/services/agent/
      tool_executor.go                     # 修改：区分 ctx 取消与工具失败
      tool_executor_test.go                # 修改：补取消与超时的用例
    pkg/services/tools/
      registry.go                          # 修改：MCP 调用加超时兜底
    pkg/settings/
      config.go                            # 修改：新增超时配置项
    docs/
      plans/
        2026-11-13-001-fix-mcp-tool-call-cancel-and-timeout-plan.md   # 本计划

---

## Implementation Units

### U1. 取消与失败分流

**Goal:** 工具执行期间的客户端取消不再伪装成工具故障。

**Requirements:** R1, R2, R3, R9

**Dependencies:** None

**Files:**
- Modify: `pkg/services/agent/tool_executor.go`
- Modify: `pkg/services/agent/tool_executor_test.go`

**Approach:**
- `callTool` 中 `e.toolreg.Invoke` 返回 error 后，先判 `ctx.Err() != nil`：是取消则不构造错误结果，返回一个可识别的「已取消」状态。
- 执行器聚合该批工具结果时，遇到取消态立即停止本批剩余工具，并让整轮随 ctx 取消而结束（agent loop 的 `iter.Seq2` 以 ctx 错误收束）。
- 沿用 `finishTurn` 现有的 `ctx.Err() != nil` 分支落到 `TurnStatusInterrupted`，不新增状态值。
- 工具自身失败（超时、error 返回、参数非法）的路径完全不动，仍走 `toolErrorResult`。

**Patterns to follow:**
- `pkg/services/agent/tool_executor.go` 现有的 `toolErrorResult` 单一出口
- `pkg/services/agent/agent_loop.go:60-108` 的 `iter.Seq2` 错误收束方式

**Test scenarios:**
- Happy path: 工具正常执行，行为不变。
- Edge case: ctx 在工具执行中途被取消 —— 不产生 tool result，不产生模型可见的错误文本，轮次以 `TurnStatusInterrupted` 收尾。
- Error path: 工具自身失败（返回 error）—— 仍构造结构化错误结果喂给模型，模型能继续下一轮。
- Integration: 取消后 agent loop 结束，不再发起新的 LLM 请求。

**Verification:** 单测覆盖取消与失败两条路径的分流；`make vet` / `make lint` 通过。

---

### U2. MCP 调用超时兜底

**Goal:** MCP 工具调用有超时上限，轮次不会无限期停在 `status=2`。

**Requirements:** R4, R5, R6, R8

**Dependencies:** None（与 U1 可并行，但建议 U1 先落地）

**Files:**
- Modify: `pkg/services/tools/registry.go`
- Modify: `pkg/settings/config.go`

**Approach:**
- 在 `callServerToolWithClient` 用 `context.WithTimeout` 包一层 ctx，超时上限取自新增配置项。
- 超时后返回 error，调用侧按普通工具失败处理（构造错误结果喂给模型），与取消路径区分。
- 配置项命名与默认值参考现有 settings 风格；默认值取分钟级，避免误杀正常长任务。
- 注意 `transport.NewStreamableHTTP` 建的 http client 无 timeout，初始化类请求（`initialize` / `tools/list`）也应一并覆盖 —— 建连阶段就用带超时的 client。

**Patterns to follow:**
- `pkg/services/tools/registry.go:314` 现有的 transport 构造方式
- `pkg/settings/config.go` 现有配置项的命名与 `envconfig` 标注风格

**Test scenarios:**
- Happy path: MCP 服务正常响应，工具结果照常返回。
- Edge case: MCP 服务接受连接但不返回响应 —— 超时后构造错误结果，轮次正常收尾（不再是 `status=2` 永久挂起）。
- Error path: MCP 服务返回错误 —— 行为不变。
- Integration: 超时场景下轮次最终状态为「已完成」或「失败」，而非永久「执行中」。

**Verification:** 单测覆盖超时路径；`make vet` / `make lint` 通过。

---

### U3. 回归验证与文档同步

**Goal:** 确认 regen 行为未受影响，取消与超时在真实链路上表现符合预期。

**Requirements:** R7, R9

**Dependencies:** U1, U2

**Files:**
- Modify: `docs/plans/2026-10-29-001-fix-tool-result-completeness-plan.md`（更新其中「超时由 ctx 免费获得」的结论，注明本计划新增的兜底）

**Approach:**
- 跑一次带工具调用的会话，再点 regen：确认旧轮次以「中断」收尾且不产生模型可见的工具错误文本，新轮次正常执行工具。
- 确认 `convo_tool_record` 中旧轮次的工具记录不再出现 `context canceled` 伪装成 `execution_failed` 的形态。
- 更新 `2026-10-29-001` 计划里那条已被本计划修正的决策，避免两份计划结论冲突。

**Test scenarios:**
- Happy path: regen 后新轮次正常出结果，历史中旧回答被替换而非堆积。
- Edge case: 旧轮次仍在执行工具时 regen —— 旧轮次干净地中断，新轮次不受影响。

**Verification:** `make vet` / `make lint` / `go test ./...` 通过；实测 regen 行为符合预期。

**Outcome:** 已同步 `2026-10-29-001` 的「超时由 ctx 免费获得」结论（加注 2026-11-13 修正）。`make vet`、`make lint`、`go test ./...` 全绿。真机 regen 实测待部署后复核（单测已覆盖取消整批丢弃与 `agent loop` 在取消态停止迭代）。

---

## System-Wide Impact

- **Interaction graph:** 前端 regen → `abortByCsid` → 旧 HTTP 请求断开 → 后端 ctx 取消 → MCP 调用取消 → （当前）伪装成工具错误 → 模型重新规划 → 反复调工具。U1 在「MCP 调用取消」与「工具错误结果」之间插入正确分流。
- **Error propagation:** 取消从「模型可见的工具错误」变为「轮次级中断」。模型不再看到假的工具故障，也就不会因它而重复规划。
- **State lifecycle risks:** 轮次状态从「永久执行中」变为可收敛的终态。`convo_chat_turn` 不再留悬挂记录。
- **API surface parity:** 无对外协议变化 —— 工具结果事件本就不发给客户端（R9）。
- **Unchanged invariants:** provider 请求体、持久化表结构与数据、SSE 事件格式与顺序、regen 语义、工具结果的截断策略。

---

## Risks & Dependencies

| 风险 | 缓解 |
|------|------|
| 取消路径误伤正常工具失败，导致工具不再报错 | 判据用 `ctx.Err() != nil`（确切的取消信号），而非 error 文本匹配；U1 单测覆盖两条路径 |
| 超时默认值过小，误杀正常的长工具调用 | 默认值取分钟级；实测 `strata__exec` 最慢 3.2s、`command timeout` 在约 9 分钟档 |
| 超时把「客户端取消」也吞成工具失败 | 超时用独立的 `context.WithTimeout` ctx，与调用方 ctx 取消可区分；两者分别在 U1 / U2 处理 |
| 与 `2026-10-29-001` 计划的既有结论冲突 | U3 显式更新那份计划的决策，两份计划结论一致 |
| 取消后仍有工具在跑，浪费沙箱资源 | 执行器在取消态立即停止本批剩余工具（U1） |

---

## Documentation / Operational Notes

- 本计划入库 `docs/plans/`；`docs/brainstorms/` 与 `todos/` 在本仓库被 gitignore，不作为交付产物。
- 代码变更走分支与 PR；提交前执行 `make vet` 与 `make lint`。
- 排查过程的可复用结论（regen 语义、取消被吞的现场特征、连接队列的读法）建议另写一篇 `docs/solutions/` 复盘。

---

## Open Questions

### Deferred to Implementation

三项均已在实现时定案（2026-11-13）：

- **超时配置项命名与默认值**：`MORIGN_MCP_TOOL_TIMEOUT_SECONDS`（字段 `settings.Current.MCPToolTimeoutSeconds`），默认 900s，`0` 表示不限制；量级高于 `strata__exec` 的 `command timeout`（约 9 分钟）。
- **取消时是否保留同批已完成结果**：整批丢弃。`ExecuteToolCalls` 在 `wg.Wait()` 后检查 `ctx.Err()`，取消则丢弃本批全部 events 与 tool messages（含已完成的），避免模型看到「部分成功」而被诱导重新规划；assistant 消息也在取消时撤回，保证 `tool_calls` 与 tool message 配对不残缺。
- **SSE 终止信号**：不改。取消沿用既有流收尾路径，不新增协议事件（R9 未含协议变更）。

- 超时配置项的具体命名与默认值，需在实现时对齐 `pkg/settings/config.go` 现有风格，并确认与工具自身的 `command timeout` 处于同一量级、不重复设限。
- 取消时是否要保留已完成的同批工具结果（部分结果 + 一条取消标记），还是整批丢弃 —— 取决于模型对「部分成功」的理解成本，实现时以错误结果文本的可读性为准。
- 取消是否需要在 SSE 层给客户端发一个明确的终止信号（如 `[DONE]`），需与前端协议一并确认。
