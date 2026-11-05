---
title: whip 计划序列的多角色审查与修订记录
type: review
status: complete
date: 2026-11-02
origin: docs/reports/2026-10-27-whip-design-review.md
scope:
  - docs/plans/2026-10-29-001-fix-tool-result-completeness-plan.md
  - docs/plans/2026-10-29-002-feat-effective-tool-policy-plan.md
  - docs/plans/2026-10-29-003-feat-turn-idempotency-state-machine-plan.md
  - docs/plans/2026-10-29-004-feat-conversation-persistence-basis-plan.md
related:
  - docs/solutions/logic-errors/wecom-http-missing-dedup.md
---

# whip 计划序列的多角色审查与修订记录

本文档记录 2026-11-02 对 `docs/plans/2026-10-29-001..004` 四份计划所做的一次多角色文档审查，以及据此做出的全部文档修订。它是本次审查的**过程记录**，不是交付物本身——交付物仍是那四份计划。

## 审查范围与方式

| 文档 | 类型 | 审查角色 |
| --- | --- | --- |
| 001 工具调用结果完整性 | plan | coherence, feasibility, scope-guardian, adversarial |
| 002 统一的工具可见性与执行授权 | plan | coherence, feasibility, security-lens, scope-guardian, adversarial, product-lens |
| 003 聊天轮次的幂等与状态机 | plan | coherence, feasibility, security-lens, scope-guardian, adversarial, product-lens |
| 004 原始对话持久化与上下文投影 | plan | coherence, feasibility, security-lens, scope-guardian, adversarial, product-lens |

共 21 个「文档 × 角色」组合，全部完成。每个组合独立阅读目标文档、并在需要时用 Grep/Read 核对代码库事实后产出结构化发现；跨文档的引用与依赖由各角色一并核对。

角色激活依据：

- **coherence / feasibility** —— 常驻，检查文档内部一致性与技术方案在真实代码库中的可实施性。
- **scope-guardian** —— 四份文档均引入新抽象（结果契约、策略对象、轮次状态机、投影与 handle），需要右尺寸审查。
- **adversarial** —— 002 与 004 引入新抽象，003 触及外部集成与状态机，001 的错误归一策略需要压力测试。
- **security-lens** —— 002 是授权计划，003 持久化用户答案并按幂等键回放，004 落库原始轨迹并暴露 `tool_result_read`。
- **product-lens** —— 002 的「骨架但不改行为」是一个战略选择，003/004 各自承担了不可逆的架构承诺。
- **design-lens** —— 未启用：四份文档不含 UI、交互或视觉设计内容。

## 结果概览

| 文档 | 已应用修正 | 延后决策 | 未处理 |
| --- | --- | --- | --- |
| 001 | 13 | 1 | 0 |
| 002 | 14 | 1 | 0 |
| 003 | 21 | 4 | 1（跳过） |
| 004 | 21 | 1 | 0 |
| **合计** | **69** | **7** | **1** |

审查原始产出经去重与合并后为 67 项可执行发现 + 15 项 FYI 观察。其中 59 项经批量执行确认后落地、7 项因涉及真实取舍写入各计划的 Open Questions、1 项因被其它修正覆盖而跳过；另有 14 组证据充分的机械性修正（行号、计数、可编译性等）在进入批量流程前直接应用。

本次共改动 5 个文件，修订明细如下（69 组，部分组一并覆盖同一发现的多个编辑点）。

**本次审查最有价值的产出不是文字修正，而是暴露了四份计划之间的依赖断裂**：A 计划里写的接口，B 计划调用时根本调不通——`p.dedup` 字段在 `HTTPChannel` 上不存在、`Result` 字段没有 `isset` 所以写不进生成层、`runner.Persist` 收不到工具事件所以 `ToolRecord` 永远是空表、`sto.Convo()` 没有 `CreateTurn` 所以设计片段调不到。这些不是措辞问题，是照文档实施会编译不过或写不出目标行。

## 修订明细

「来源」列记的是提出该发现的角色组合。

### 计划 001 —— 工具调用结果完整性

| # | 位置 | 修订内容 | 来源 |
| --- | --- | --- | --- |
| 1 | Requirements R1 / 统一结果契约 | R1 的「五条错误路径」改为四条（去掉无产生路径的"工具不存在"）；Status 表删除 `tool_not_found` 行；「错误归一的位置」改为"执行层按 `ok` 处理，不改 `Registry` 返回约定" | coherence, feasibility, scope-guardian, adversarial |
| 2 | Requirements R8 / U2 验收 | "五条错误路径"改为"四条" | coherence |
| 3 | 统一结果契约 | "`llm.ToolResult` 扩展三个字段"改为"两个字段" | scope-guardian |
| 4 | High-Level Technical Design / U2 | 错误结果构造函数增加 `callID` 参数并写入 `CallID`；U2 补第 7 步"所有错误结果构造均传入 `tc.ID`" | coherence, feasibility, scope-guardian |
| 5 | U4 / Risks / In Scope | 验收的"含 `cmd/`"改为"含根目录 `main.go`"（仓库无 `cmd/` 目录）；补 `docs/WORKFLOWS.md` 两处流程图引用更新 | coherence, feasibility, adversarial |
| 6 | 统一结果契约 / U2 | 为工具自身业务错误（结果含 `isError=true`）补 `tool_error` 错误码行；U2 成功路径改为先检查 `isError` 标记再决定 `ok` / `error` | feasibility, scope-guardian |
| 7 | 统一结果契约 | 补 `permission_denied` 行，对应计划 002 的门控拒绝 | coherence, security-lens |
| 8 | R4 / 截断策略 / U3 | 明确"上限包含省略说明本身"，头部取上限 3/4、尾部 1/4；R4 改为"总长度（含省略说明）不超过上限"；U3 验收同步 | coherence, feasibility, adversarial |
| 9 | U2 / High-Level Technical Design | 规定 panic 恢复写 `results[idx]` 必须发生在 `wg.Done()` 之前（defer 注册顺序写死） | adversarial |
| 10 | U5 回归测试 | 补表说明需同步更新的 4 个既有用例（`TestExecuteToolCallsInvokeError` 等）——它们固化了旧行为 | feasibility |
| 11 | 错误归一的位置 / High-Level Technical Design | 构造函数增加 `status` 参数，使其能同时产出 `error` 与 `rejected`，不再需要第二个构造函数 | scope-guardian |
| 12 | Problem Frame | 补一句说明跳过 `nil` 项会让后续 tool message 相对 `tool_calls` 前移，顺序与配对同时失真 | coherence |
| 13 | 统一结果契约 | 明确 `rejected` 独立于 `error`，下游统计（004 的 `ToolRecord.Status`）需单独识别 | coherence |

### 计划 002 —— 统一的工具可见性与执行授权

| # | 位置 | 修订内容 | 来源 |
| --- | --- | --- | --- |
| 1 | Problem Frame | `base := copy(r.tools)` 改为可编译写法（原文不是合法 Go） | feasibility |
| 2 | Context & Research | 三处代码行号更正（`initTools` 132-181、`registerToolsLocked` 364-397、KBCreate keeper 检查 625） | feasibility |
| 3 | System-Wide Impact | "非 keeper 用户无法再直接按名调用 KBCreate；其余行为不变"改为说明拒绝点前移与返回文本变化 | feasibility |
| 4 | 可见面与执行面如何共用 / U1 | 工具归属改为**注册期记录**而非解析工具名（`genToolKey` 的 `__` 分隔符不唯一，内置受限与全局 MCP 也无法区分）；判定表增运行时注册工具一行；U1 同步 | coherence, feasibility, security-lens, scope-guardian, adversarial, product-lens |
| 5 | 策略对象的位置与形态 | 结构体 `allowed map[string]bool` 改为 `categories map[string]string`；**取消 ctx 缓存**（签名无法实现、ctx 作全局 map 键不可比较且泄漏、判定本就是 O(1)）；补大小写不敏感语义 | coherence, feasibility, scope-guardian, adversarial |
| 6 | Risks & Dependencies | ctx 缓存风险行改写为"判定只读注册表的类别表，不受调用次数影响" | coherence, scope-guardian |
| 7 | 授权来源的记录 | 日志字段固定为工具名、调用者身份、授权来源、决策结果、拒绝原因；被拒调用至少 Info 级别——只有来源字段无法支撑审计 | coherence, feasibility, security-lens, adversarial, product-lens |
| 8 | High-Level Technical Design | 新增 `Registered(name)` 方法；`Invoke` 先判"是否已注册"再判"是否被允许"，`tool not found` 路径保持可达 | coherence, product-lens |
| 9 | Open Questions | "tool not found 与 permission denied 如何区分"的结论改为"按工具是否已注册区分" | coherence |
| 10 | R3 | 改写为"变化仅在拒绝点前移"，消除与 Problem Frame / Impact 的三处冲突 | coherence, feasibility |
| 11 | R7 | 补"大小写变体的调用仍按现状可执行" | feasibility, scope-guardian, adversarial |
| 12 | Problem Frame | "任何登录用户只要知道工具名，就能调用 KBCreate"改为"能进入 invoker"（原文与同节引用的 keeper 检查自相矛盾） | coherence |
| 13 | U4 回归测试 | 补"隐藏即不可按名调用"用例——原"可见 ⇔ 允许"只验证两面同源，覆盖不到真正的绕过路径 | product-lens |
| 14 | Risks & Dependencies | 依赖 001 的表述改为"其 ErrorCode 表已预留 `permission_denied` 一行"，"两计划可并行"的说法不再成立 | coherence, security-lens, product-lens |

### 计划 003 —— 聊天轮次的幂等与状态机

| # | 位置 | 修订内容 | 来源 |
| --- | --- | --- | --- |
| 1 | Context & Research | 两处代码行号更正（`prepareChatRequest` 77、`MessageHandler` 125） | feasibility |
| 2 | U6 / Risks | 启动扫描范围从 `running` 扩到 `queued` + `running`；风险表的"只标记超过一定时长的轮次"改为"不做时长筛选"——时长筛选会让刚崩溃的年轻轮次永久占用幂等键 | coherence, scope-guardian, adversarial, product-lens |
| 3 | High-Level Technical Design | 设计片段的 `existing, _ := ...` 改为带 `err` / `nil` 守卫，消除 nil 解引用 | feasibility |
| 4 | 幂等键的来源 | Web 派生键改为 `sha256(owner_id + session_id + prompt + 请求体摘要)`；命中终态轮次返回 `Result` 前必须校验 `owner_id` 一致 | feasibility, security-lens, adversarial |
| 5 | 幂等键的来源 | 重试与重新生成（`regen` / 与上一条同内容）跳过 PG 幂等判定——缺省派生式无法区分"网络重投"与"用户主动发的第二条相同内容" | coherence, adversarial, product-lens |
| 6 | Deferred / Key Technical Decisions / U3 / U5 | Redis 去重降级为"只记录不阻断"的提示层，PG 为唯一仲裁者；U3 补第 5 步（三条已有 Redis 路径改为不阻断）；U5 由"补 Redis 去重"改为"由 U3 的 PG 幂等覆盖" | coherence, scope-guardian, adversarial, product-lens |
| 7 | U2 轮次存储 | 补唯一冲突的判定方式（`errors.As` 取出 `pgdriver.Error` 检查 `23505`）——`pgx.FilterError` 用类型断言，直接套用永远匹配不上 | feasibility |
| 8 | U1 / Context / Open Questions | 迁移改为 `CREATE UNIQUE INDEX IF NOT EXISTS`（codegen 建表时已生成同名约束）；文件名格式由 12 位更正为 14 位时间戳（与 embed 规则一致，否则文件被静默排除） | feasibility, coherence, adversarial |
| 9 | ChatTurn 模型设计 | `Result` 补 `isset: true`——否则生成的 `ChatTurnSet` 里没有它，终态结果无处写入 | feasibility, scope-guardian |
| 10 | ChatTurn 模型设计 | 新增 `OwnerID` 字段与租约字段（`LeaseOwner` / `LeaseExpiresAt`）说明 | security-lens, product-lens, adversarial |
| 11 | R3 | 改为"`succeeded` 直接返回结果；`failed` / `cancelled` 允许同键重新执行" | coherence |
| 12 | R5 | 明确重放语义："重放 = 客户端以新的幂等键重发，产生新轮次；原 `interrupted` 轮次保持不变" | coherence |
| 13 | U3 / High-Level Technical Design | 冲突分支改为按状态分派（`succeeded` 返回 / `failed`·`cancelled` 重试 / `queued`·`running`·`interrupted` 不执行也不返回） | product-lens |
| 14 | 状态机与副作用时机 | 补轮次锚点的传递约定（`ctx = stores.ContextWithTurnID(ctx, turn.GetID())`）——004 的 `ToolRecord.TurnID` 依赖它 | feasibility |
| 15 | 状态机与副作用时机 | 补 `cancelled` 的判定规则（副作用发生前取消 → `cancelled`），六值状态机此前只有五个可达 | scope-guardian |
| 16 | U6 中断状态 | 多副本部署下只回收租约已过期或归属本进程且已失联的记录 | adversarial |
| 17 | U7 回归测试 | 补 5 条 Web 侧用例（重复请求 / 重试重新生成 / 跨用户同内容 / `failed`·`cancelled` 重试 / `queued` 回收） | coherence, product-lens |
| 18 | Summary / Problem Frame / 接入位置 | 统一表述为"三个接线点覆盖四条频道路径"——原文"三条入口"与"四条频道路径"混用 | coherence |
| 19 | High-Level Technical Design | 轮次三方法由独立 `TurnStore` 接口改为并入 `ConvoStoreX`（否则 `sto.Convo().CreateTurn(...)` 不存在） | feasibility |
| 20 | Risks & Dependencies | 补"整体落地顺序 001 → 002 → 003 → 004"声明，消除与 004 的"003 必须先落地"冲突 | product-lens |
| 21 | Open Questions / Documentation | Resolved 段三处更新（PG 表由谁建 / 幂等键全局唯一 / Redis 是否删）；Documentation 补"按 `OwnerID` 按登录身份收窄查询面" | feasibility, scope-guardian, adversarial |

### 计划 004 —— 原始对话持久化与上下文投影

| # | 位置 | 修订内容 | 来源 |
| --- | --- | --- | --- |
| 1 | R8 | 改为"除新增内置工具 `tool_result_read` 外，不改变模型调用参数、既有工具集合与响应格式" | coherence, feasibility, scope-guardian |
| 2 | Scope Boundaries | In Scope 的"新增 `ToolCall`（或复用 Message 承载）"改为"新增 `ToolRecord` 模型"，与决策段和 U1 一致 | coherence |
| 3 | Context & Research | 两处 `file:line` 更正（`formatToolResult` 199、`chatMessages` 在 `prompt.go:123` 并补两个调用点） | coherence, feasibility, scope-guardian |
| 4 | ToolRecord 模型 | yaml 补 `SessionID` / `OwnerID` / `Seq` / `Preview` / `Spilled` 五个字段，`Arguments`·`Content`·`ErrorCode`·`DurationMS` 补 `isset`（否则生成层写不进这些列），补 `specNs: convo`，`Arguments` 注释改为"入参原文" | coherence, feasibility, adversarial |
| 5 | 原始记录的表结构 | 补字段三要点说明（归属判定、轮内顺序、预览原文）；补入参存原文而非摘要；补写入前脱敏 | coherence, feasibility, security-lens |
| 6 | handle 机制 | 明确**唯一写入方是截断处**（事件侧只 upsert 状态字段、不覆盖 `Content`）；工具改为 `tool_result_read(handle, offset, limit)` 分页读取；写入失败降级为纯截断文本 | coherence, feasibility, scope-guardian, adversarial, product-lens |
| 7 | High-Level Technical Design | 写入侧补 `Event{UserPrompt} → 写 Message(role=user, turn_id)`——用户提问此前没有落库路径 | coherence, feasibility, product-lens |
| 8 | Institutional Learnings / Open Questions | 归属规则改为引用记忆归属计划的 owner 判定与越权收窄（`owner_id` 列 + 存储层钩子 + `ownerAllowed` / `ErrForbidden`），并明确 `tool_result_read` 在计划 002 的策略表中登记为 `builtin-public` | security-lens |
| 9 | R6 | "压缩不得留下孤立的配对"改为"裁剪（早期轮替换为占位符）"——压缩被排除在范围外 | coherence |
| 10 | R5 | 补"provider 未返回 usage 时回退字节估算"，与 U5 一致 | coherence |
| 11 | Out of Scope | 标注 `after_seq` 事件流落库当前无归属计划（003 与 004 互相指向对方） | coherence, scope-guardian |
| 12 | Implementation Units | 拆为两个可独立交付的增量：A（U1–U3，纯增量、不动线上路径）与 B（U4–U6，替换上下文构造路径） | product-lens |
| 13 | U1 | `Message` 补 `OwnerID`；迁移只写增量 DDL（`ALTER TABLE ... ADD COLUMN IF NOT EXISTS`），`convo_tool_record` 的建表交给 codegen | security-lens, feasibility |
| 14 | U2 | 补第 1 步"接线到事件来源"（工具事件从产生处转发给 `runner.Persist`，四处调用点手工构造的 `llm.Event` 不带工具载荷）；补第 5 步"补齐非流式链路"；补第 6 步"归属收窄"（`Message` 需声明 hooks，越权返回 `ErrForbidden`） | coherence, feasibility, scope-guardian, adversarial, product-lens, security-lens |
| 15 | U3 | 补唯一写入方与降级；补分页参数；补"三条入口统一注入会话标识"（当前只有 Web SSE 注入）；补 stores 层读取方法的强制 `WHERE` 条件 | coherence, feasibility, scope-guardian, adversarial, security-lens |
| 16 | U4 | 投影同时接到 Web 与频道两个调用点；按 `(turn_id, seq)` 排序；直接内联 `Preview` 不重新推导；写死裁剪公式并设保留轮数下限；承接既有的重试 / 重新生成语义 | coherence, feasibility, scope-guardian, adversarial |
| 17 | U5 | 补 usage 来源（`convo_usage_record` 最近一条 `UsageRecord`）；补三条链路都要写 `UsageRecord`；补字节估算需另写函数（`calcTokens` 只认 `HistoryItem`） | feasibility, adversarial, scope-guardian |
| 18 | U6 | 补"跨用户读 `Message` / handle 被拒"与"非流式链路同样产出 `ToolRecord`"两条用例 | coherence, security-lens, feasibility |
| 19 | Risks & Dependencies | 更正"异步路径"为同步调用并评估写入放大；补 002 依赖；补保留期与脱敏的保守口径；补增量分批上线 | feasibility, scope-guardian, security-lens, product-lens |
| 20 | System-Wide Impact | `pkg/web/api` 行改为"Web 与频道两处"；`data/schemas` 行改为"只含增量 DDL"；外部行为行补"分页" | coherence, feasibility, scope-guardian |
| 21 | Documentation | 补 `owner_id` 列、`tool_result_read(handle, offset, limit)` 签名 | feasibility, security-lens |

### 连带修订

| 位置 | 修订内容 | 原因 |
| --- | --- | --- |
| `docs/solutions/logic-errors/wecom-http-missing-dedup.md` 的 Fix 段 | 由"补齐 Redis 层去重（短期）+ PG 轮次幂等（长期）"改为"由计划 003 的 U3 在 `MessageHandler` 统一覆盖，不再单独补 Redis 前置闸门"，并说明 Redis `SetNX` 命中即丢弃正是该文件记录的静默丢失根因 | 计划 003 的 U5 语义变更后，该复盘文档的修复指引已失效 |

## 延后决策

以下 7 项涉及真实取舍（多个角色给出了方向不同、都能自圆其说的方案），未直接改文档，已按各自文档的 `### From 2026-11-02 review` 小节写入 Open Questions：

| 文档 | 事项 | 级别 |
| --- | --- | --- |
| 001 | `Status` / `ErrorCode` 是否随 tool message 发给 provider（模型能否区分工具自报失败与成功） | P1 |
| 002 | 全局 MCP 的授权归属由谁决定、收窄的触发条件与承接计划；策略是否应增加"存在已登录身份"维度 | P1 |
| 003 | 轮次写入失败时 fail-open 还是 fail-closed | P1 |
| 003 | `RequestDigest` 保留（并补检测逻辑）还是删除（接受同键不同内容被静默复用） | P2 |
| 003 | `convo_chat_turn` 的保留期与级联清理（现有 `DeleteSession` 不清理新表） | P2 |
| 003 | Web 侧显式幂等键的 API 字段名，以及是否本轮交付 | P2 |
| 004 | 稳定前缀字节稳定（R7）与 usage 裁剪的冲突解法 | P1 |

## 跳过的发现

1 项：计划 003 的"U5 引用的 `p.dedup` 在 `HTTPChannel` 上不存在"。该项随计划 003 的 Redis 闸门调整（U5 改为由 U3 的 PG 幂等覆盖）一并消解——U5 不再需要 Redis 闸门，`HTTPChannel` 也就不需要新增 `dedup` 字段。

## FYI 观察（未改文档）

以下 15 项为顾问性观察，不构成缺陷，不影响计划正确性，记录于此备查：

- **001**：`Status` / `ErrorCode` 用裸字符串字面量，缺类型常量（`pkg/services/llm` 已有 `FinishReason` / `RoleAssistant` 的常量块惯例）；截断层选择——executor 层、provider 组装层、spill 到存储三种都可行，计划直接选了 executor。
- **002**：拒绝与"未找到"文案可区分，会暴露受限工具的存在性；R4 频道语义收窄其实与现状一致（问题出在注释而非行为）；策略计算的加锁方式与注册点标注要求未定义；Scope 的"MCP 收窄 out of scope"与 R5 措辞冲突；未比较"按需判定谓词"这一更简单的替代形态。
- **003**：频道侧幂等键缺 corp/app 租户判别符；中断扫描的阈值与触发点未定；本计划未交付 origin 所述的"可重放"；"PG 对 Redis"的论证只否定了布尔 `SetNX`，未评估"Redis 存状态机 + 异步 write-behind"的混合方案。
- **004**：保留 Redis 上下文、只从 PG 补工具轨迹的增量方案未被比较；字节估算兜底没有可用的估算函数；Risks 表未列对 002 的依赖；handle 用数据库行 ID 可枚举。

## 遗留问题

以下为审查中注意到、但不足以支撑修订的疑点，留给实施阶段验证：

- 001：`BeforeToolCall` 钩子在生产代码中无装配点，只有测试在用；孤儿 `tool_use` 的 400 来自 provider 契约，仓库内无测试复现。
- 002：全局 MCP 收紧无承接方与触发条件；`HeaderCate=None` 的 MCP server 收不到用户身份头；授权来源只写日志不落库。
- 003：`ChatTurn.Status` 用自由字符串而非契约里的 `enums:`；`Result` 标 `notnull` 但 queued / running 阶段无值；企微 HTTP 先返 200 再异步处理，"已响应但结果未知"的窗口与 `interrupted` 语义是否等价未展开；004 落地后答案同时存在于 `chat_turn.Result` 与 `convo_message.content` 两处。
- 004：`convo_message.turn_id` 为 NOT NULL 且无默认值，`ALTER` 迁移在历史数据非空的部署上会失败；投影读 PG 是走主库还是只读副本未约束；工具输出入库后构成存储型提示注入面，未要求来源标注或指令隔离。

## 复审

本次审查为第 1 轮，无历史决策基线。复审时：

- 已应用的 69 组修正应作为"已落地"基线，同类发现不再重复提出。
- 7 项延后决策按各计划 `### From 2026-11-02 review` 中的 `<!-- dedup-key: ... -->` 做复合键去重，避免同一事项二次提出。
- 15 项 FYI 观察不构成缺陷，除非后续有新的信号支持，否则不必再次提出。
