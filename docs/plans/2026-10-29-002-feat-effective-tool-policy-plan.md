---
title: feat: 统一的工具可见性与执行授权
type: feat
status: completed
date: 2026-10-29
origin: docs/reports/2026-10-27-whip-design-review.md
---

# feat: 统一的工具可见性与执行授权

## Summary

`Registry.ToolsFor` 决定模型看到哪些工具，`Registry.Invoke` 决定实际能执行哪些工具，两者之间没有任何共享的判定逻辑。本计划引入 `EffectiveToolPolicy` 作为唯一授权源：可见面由它计算，执行面由它强制。隐藏的工具无法通过知道名字绕过可见性直接调用。

改动集中在 `pkg/services/tools`，新增一个策略类型并在 `Registry` 内接线，不触碰 `docs/*.yaml` 契约，不触发 `make codegen`。

---

## Problem Frame

当前的两级权限（keeper / 公开）只作用于**可见面**：

```go
// registry.go:211 ToolsFor —— 模型看到什么
func (r *Registry) ToolsFor(ctx context.Context) []mcps.ToolDescriptor {
    base := make([]mcps.ToolDescriptor, len(r.tools))
    copy(base, r.tools)
    if stores.IsKeeper(ctx) {
        base = append(base, r.privTools...)
    }
    if ch := mcps.ChannelFromContext(ctx); ch != "" {
        base = append(base, r.channelTools[ch].tools...)
    }
    return base
}
```

而**执行面**完全独立：

```go
// registry.go:97 Invoke —— 实际执行什么
func (r *Registry) Invoke(ctx context.Context, name string, params map[string]any) (map[string]any, error) {
    // 频道 invoker 优先，再全局 invoker
    ...
    return mcps.BuildToolErrorResult("tool not found"), nil
}
```

`Invoke` 在全局 `r.invokers` map 里按名字查找。`r.invokers` 同时持有公开工具（KBSearch、Fetch、memory 四个、skill 四个、capability 两个）和受限工具（KBCreate）。因此**任何调用者只要知道工具名，都能进入 KBCreate 的 invoker**——它不在模型的可见工具列表里，但直接调用能进到 invoker（`Invoke` 只查名字，不查权限）。invoker 内部虽有一处 keeper 检查能挡住写入，但判定散落在各 invoker 内、不在门控层，无法统一收窄。

KBCreate 的 keeper 检查写在 invoker 内部（`pkg/services/stores/corpus_x.go:625`）：

```go
if !IsKeeper(ctx) {
    return mcps.BuildToolErrorResult("permission denied: keeper role required"), nil
}
```

这挡住了实际的写入，但判定散落在各个 invoker 内部，不在门控层。两个后果：

1. **MCP 工具零授权。** 所有 MCP 工具的 invoker 都不做权限判定——既不检查 keeper，也不检查频道归属。生产环境若挂了企业内部 MCP server（查订单、查薪资、触发工单），任何登录用户都能直接调用。
2. **判定无法统一收窄。** 权限语义分散在各个 invoker 的实现里，无法回答"这个调用是被哪条规则允许的"。

另有一个隐蔽的不一致：`ToolsFor` 对频道工具的注释写着"始终公开"，但 `Invoke` 里频道 invoker 优先于全局 invoker —— 一个频道的专属工具在另一个频道的上下文里既不可见也调不到，反过来全局工具在频道上下文里可见且可调。这两套规则没有共同来源。

---

## Requirements

- R1. 引入 `EffectiveToolPolicy`，作为工具授权的唯一判定源。同一份策略同时驱动可见面（`ToolsFor`）与执行面（`Invoke`）。
- R2. `Invoke` 在执行前校验当前 ctx 下的策略；不在允许集合内的工具返回"permission denied"错误结果，**不进入 invoker**。
- R3. 保持既有权限语义不变：keeper 可见并可执行受限工具；非 keeper 只可见也不可执行受限工具。变化仅在拒绝点前移——非 keeper 调用受限工具过去由 invoker 内部的 keeper 检查拒绝，现在由门控层在进入 invoker 前拒绝。
- R4. 频道工具仅在其归属频道的上下文中可见且可执行。
- R5. MCP 工具纳入策略判定：至少区分"全局 MCP"与"频道专属 MCP"两类归属，后者仅在对应频道可用。
- R6. 工具调用日志记录本次调用的授权来源，便于事后审计。
- R7. 不改变工具集合本身、工具 schema、工具执行逻辑与返回格式；大小写变体的调用仍按现状可执行。
- R8. 有回归测试覆盖可见面与执行面的一致性；`make vet` 与 `make lint` 通过。

---

## Scope Boundaries

### In Scope

- 新增 `EffectiveToolPolicy` 类型与其判定逻辑
- `Registry.ToolsFor` 与 `Registry.Invoke` 改为共用该策略
- 调用日志增加授权来源字段

### Out of Scope

- **MCP 工具目录化**（`tool_search` / `tool_describe` / `tool_call`）：降低 schema 上下文占用的方案，见「Deferred」
- **按用户、频道或子 agent 收窄 MCP 权限的策略配置语法**：本计划只建立判定骨架与既有两级语义，细化策略表达式属后续
- **子 agent 的权限快照继承**：子 agent 尚在规划阶段，未有实现
- **KBCreate invoker 内部的 keeper 检查**：本计划在门控层拦截后，该内部检查成为纵深防御，保留不动

### Deferred to Follow-Up Work

- **MCP 动态目录**：报告建议模型初始只看到稳定的少量工具入口，MCP schema 按需发现。这解决的是"schema 占用上下文"与"prompt 前缀随 MCP 状态漂移"两个问题，收益取决于生产环境实际 server 数量。**触发条件**：`ToolsFor` 返回的工具数量在典型会话中超过约 30 项时再启动该工作。
- **授权来源的持久化审计**：本计划只写日志，不落库。

---

## Context & Research

### Relevant Code and Patterns

- `pkg/services/tools/registry.go`：`ToolsFor`（:211）与 `Invoke`（:97）是本计划改动的两个点
- `pkg/services/tools/registry.go:132-181` `initTools`：工具注册处，公开工具进 `r.tools`，受限工具进 `r.privTools`
- `pkg/services/tools/registry.go:364-397` `registerToolsLocked`：MCP 工具注册，频道 server 进 `cs.tools`，全局 server 进 `r.tools`
- `pkg/services/tools/connection.go:30` `genToolKey`：工具名格式为 `channel__server__tool` 或 `server__tool`，**自带归属信息**
- `pkg/services/stores/auth.go:52` `IsKeeper`：keeper 判定的既有实现
- `pkg/models/mcps/mcps_x.go:39` `ContextWithChannel` / `ChannelFromContext`：频道归属的既有实现

### Institutional Learnings

`docs/solutions/logic-errors/executeToolCallLoop-deduplication.md` 记录过"两份实现各自演化"的教训。本计划的核心正是消除可见面与执行面两份判定——同一个道理在授权维度上的应用。

### External References

无。授权模型沿用项目既有的 keeper / 公开两级语义，不引入外部依赖。

---

## Key Technical Decisions

### 策略对象的位置与形态

`EffectiveToolPolicy` 放在 `pkg/services/tools` 包内，与 `Registry` 同处。它是**按 ctx 计算的值对象**，不是全局状态：

```go
type EffectiveToolPolicy struct {
    keeper     bool
    channel    string
    categories map[string]string  // 工具名 → 归属类别
}
```

`categories` 由注册表在注册时写入（`builtin-public` / `builtin-keeper` / `mcp-global` / `mcp-channel`），是工具归属的唯一权威来源。允许与否不预先物化成一份布尔集合——它可由「类别 + keeper/频道」直接推导，多存一份只会引入两份可能漂移的状态。

**不做 ctx 缓存。** 缓存需要把派生 ctx 沿调用链传下去，而 `ToolsFor` 只返回描述符切片、其内部派生的 ctx 会被丢弃；要么改签名、要么要求调用方预置可变 holder，两者都把契约推出 `pkg/services/tools` 包外。用 ctx 本身作全局 map 的键则既不可比较也会造成泄漏。判定只读注册表的类别表，开销与工具数量无关，不构成需要缓存的理由。

由 `Registry.PolicyFor(ctx) EffectiveToolPolicy` 计算，供 `ToolsFor` 与 `Invoke` 共用。

**大小写**：`Invoke` 现有的 invoker 查找用 `strings.EqualFold` 匹配工具名，大小写变体今天可以调用。策略判定必须沿用同一语义——`categories` 以小写为键，`Allows` / `Source` / `Registered` 查询前把工具名转小写，否则 `KB_CREATE` 这类变体会从"可执行"变成"permission denied"。

### 可见面与执行面如何共用

`ToolsFor` 从策略的允许集合出发过滤可见工具；`Invoke` 查 invoker 之前先问策略"这个名字在允许集合内吗"。两者读同一个集合，不可能出现"看不见却能调"或"看得见却调不了"。

**工具归属在注册时记录，不从工具名解析。** `genToolKey` 的 `channel__server__tool` 格式只作为可读约定，不作为判定输入：server 名与 MCP 工具名都由外部提供、契约层无字符约束，含 `__` 时按名分段无法唯一确定归属；内置受限工具与全局 MCP 工具都不含 `__` 前缀，同样无法区分。注册表本身已持有权威归属（`r.privTools`、`r.invokers`、`r.channelTools[ch].invokers`）。

判定规则保持简单：

| 工具类别 | 判定 |
| --- | --- |
| 内置公开工具（`initTools` 注册的公开工具：KBSearch、Fetch、memory、skill、capability、CurrentTime，以及 [计划 004](2026-10-29-004-feat-conversation-persistence-basis-plan.md) 新增的 `tool_result_read`） | 始终允许 |
| 内置受限工具（KBCreate） | 仅 keeper |
| 运行时注册工具（`Registry.RegisterInvoker`） | 视为内置公开工具 |
| 全局 MCP 工具（注册于 `r.invokers` 的 `server__tool`） | 始终允许（与现状一致，不改变行为） |
| 频道 MCP 工具（注册于 `r.channelTools[ch].invokers`） | 仅当前 ctx 频道匹配时允许 |

内置工具各自还有既有的可见性收窄，与本计划的工具级策略不重叠：`skill` 工具的可见性由 skill store 的用户可见性判定负责，`capability` 工具在未注册（`CountCapability(ctx) > 10` 不满足）时根本不进入允许集合。

**注意**：全局 MCP 工具在策略里"始终允许"，这与现状一致 —— 本计划不收紧全局 MCP 的可见性。但它**从"无判定"变成"经策略判定"**，即未来收窄策略时只需改一处。这是本计划交付的核心价值：建立骨架，不改行为。

### 为什么不在本计划收紧全局 MCP 权限

收紧需要决定"谁有权调用哪个 server"，这依赖生产环境的 server 归属规则，本计划没有足够信息做出正确判断。凭空加一条"所有 MCP 工具默认对登录用户开放"的规则，等于把无意识的现状固化成显式策略。更好的做法是：先把判定点建好，把策略收紧作为独立决策，届时只需要补充策略表。

### 授权来源的记录

`Invoke` 每次判定后都记录一条日志，字段固定为：工具名、调用者身份（用户 OID，无登录身份时为空）、授权来源（`builtin-public` / `builtin-keeper` / `mcp-global` / `mcp-channel`）、决策结果（`allowed` / `denied`）、拒绝原因（仅 `denied` 时有值）。

只有来源字段不足以支撑审计——被拒的调用同样有来源（它属于哪个类别），若不带决策结果就无法与被放行的调用区分；`Documentation` 段承诺的"可按其检索被拒绝的调用"因此需要 `decision=denied` 这个可检索字段。同理，不记录调用者身份则无法归因，"任何用户都能调用某个 MCP server"这类问题在事后无从回答。

被拒的调用至少以 Info 级别记录（现有 `Invoke` 的调用日志是 Debug，默认级别下不会被留存）。

---

## Open Questions

### Resolved During Planning

- **策略要不要进 `docs/mcps.yaml` 契约？** 不进。本计划的策略由代码中的工具注册信息推导，不需要新的持久化字段。加字段会触发 codegen，且当前没有需要持久化的策略数据。
- **`Invoke` 找不到工具时现在返回 `BuildToolErrorResult("tool not found")`，与新加的"permission denied"如何区分？** 按「工具是否已注册」区分：未注册返回既有的 `tool not found`，已注册但不在允许集合内才返回 `permission denied`。两者都返回错误结果 map、`error` 为 `nil`，保持既有约定。
- **`KBCreate` invoker 内部的 keeper 检查要删掉吗？** 不删。门控层拦截后它成为纵深防御，删除会扩大改动面且降低安全性。

### Deferred to Implementation

- 策略判定在注册期标注类别时的具体落点（`initTools` / `registerToolsLocked` / `RegisterInvoker` 三处）。
- 授权来源的日志字段名。

### From 2026-11-02 review

- **全局 MCP 的授权归属与收窄的承接方** — Key Technical Decisions / 判定规则表 (P1, coherence, security-lens, adversarial, product-lens, confidence 100)

  Problem Frame 把「MCP 工具零授权……任何登录用户都能直接调用企业内部 MCP server」列为首要后果，但交付的策略表把「全局 MCP 工具 → 始终允许」写成正式规则，把无意识的现状升格为显式授权。计划没有回答「谁有权调用哪个 server」——这依赖生产环境的 server 归属规则，本计划信息不足。需要确定：全局 MCP 的授权归属由谁决定（配置项 / 契约字段 / 运维约定）、收窄的触发条件是什么、由哪个计划承接。另外 `/api/chat` 默认挂在 `routes.AuthMw(false)` 上且 `Auth_Required` 默认关闭，策略目前也没有「存在已登录身份」这一维度——未登录调用者同样拿到 keeper=false 的策略并可调用全局 MCP 工具。

  <!-- dedup-key: section="key technical decisions 判定规则表" title="全局 mcp 的授权归属与收窄的承接方" evidence="| 全局 MCP 工具（`server__tool`） | 始终允许（与现状一致，不改变行为） |" -->


---

## High-Level Technical Design

```go
// PolicyFor 计算并缓存当前 ctx 的有效工具策略
func (r *Registry) PolicyFor(ctx context.Context) EffectiveToolPolicy

// Allows 判断工具在当前策略下是否可执行
func (p EffectiveToolPolicy) Allows(name string) bool

// Registered 判断工具是否已注册（用于区分"未找到"与"无权限"）
func (p EffectiveToolPolicy) Registered(name string) bool

// Source 返回工具的授权来源，用于日志
func (p EffectiveToolPolicy) Source(name string) string
```

`Registry` 侧的变化：

```go
func (r *Registry) ToolsFor(ctx context.Context) []mcps.ToolDescriptor {
    policy := r.PolicyFor(ctx)
    // 过滤：只保留 policy.Allows(name) 的工具
}

func (r *Registry) Invoke(ctx context.Context, name string, params map[string]any) (map[string]any, error) {
    policy := r.PolicyFor(ctx)
    if !policy.Registered(name) {
        return mcps.BuildToolErrorResult("tool not found"), nil
    }
    if !policy.Allows(name) {
        return mcps.BuildToolErrorResult("permission denied: tool not available in current context"), nil
    }
    // 原有查找逻辑不变
}
```

判定顺序不可颠倒：先判"是否已注册"再判"是否被允许"，否则未注册的名字会被判为 permission denied，`tool not found` 这条既有路径变得不可达，两类错误在事件流里同形。

---

## Implementation Units

### U1. 策略类型与判定

在 `pkg/services/tools` 新增 `policy.go`：

1. `EffectiveToolPolicy` 结构
2. `PolicyFor(ctx)`：从 ctx 读取 keeper 身份与频道归属，结合注册表推导允许集合
3. `Allows` 与 `Source` 方法
4. 判定规则按「Key Technical Decisions」的表实现

工具归属一律在注册时记录：`initTools` 标注内置公开/内置受限，`registerToolsLocked` 用已有的 `server.Channel` 标注 `mcp-global` / `mcp-channel`，`RegisterInvoker` 标注为内置公开。策略只读这张表，不解析工具名。

验收：keeper 与非 keeper、频道内与频道外四种组合下的允许集合符合预期。

### U2. 接线到可见面

改写 `Registry.ToolsFor`：先取策略，再过滤现有工具列表。保持返回顺序不变（内置工具在前、频道工具在后）。

验收：非 keeper 的 `ToolsFor` 不含受限工具；频道 ctx 下含该频道工具且不含其他频道工具。

### U3. 接线到执行面

改写 `Registry.Invoke`：查 invoker 之前先过策略；被拒时返回错误结果并记日志（含授权来源）。授权通过时同样记一条带来源的日志。

验收：非 keeper 直接以名字调用 KBCreate 时返回 permission denied，且 invoker 未被执行；调用其他频道的工具同样被拒。

### U4. 回归测试

在 `pkg/services/tools` 补测试：

| 用例 | 断言 |
| --- | --- |
| keeper 与非 keeper 的可见工具集合 | 受限工具只在 keeper 集合内 |
| 非 keeper 直接调用受限工具名 | 返回 permission denied，invoker 未执行 |
| 频道 ctx 调用他频道工具名 | 返回 permission denied |
| 频道 ctx 调用本频道工具名 | 正常执行 |
| 可见面与执行面一致 | 对每个工具名，`ToolsFor` 可见 ⇔ `Invoke` 允许 |
| 隐藏即不可按名调用 | 对每个未被允许的工具名，它不在同 ctx 的 `ToolsFor` 结果中，且 `Invoke` 返回 permission denied、invoker 未执行 |

---

## System-Wide Impact

| Area | Impact |
| --- | --- |
| `pkg/services/tools` | 新增 `policy.go`；`registry.go` 的 `ToolsFor` / `Invoke` 行为变更 |
| `pkg/services/agent` | 无直接改动；工具错误结果格式变化由 [计划 001](2026-10-29-001-fix-tool-result-completeness-plan.md) 定义 |
| Web / 频道入口 | 无改动 |
| 数据契约 | 无变更，不触发 `make codegen` |
| 外部行为 | 非 keeper 按名调用 KBCreate 的拒绝点从 invoker 内部前移到门控层，返回文本由 `permission denied: keeper role required` 变为 `permission denied: tool not available in current context`；其余内置工具行为不变 |

---

## Risks & Dependencies

| Risk | Mitigation |
| --- | --- |
| 策略判定遗漏某个工具类别，导致合法工具被误拒 | U4 的"可见面 ⇔ 执行面"一致性测试覆盖全部注册路径；实现在注册处集中标注类别，而非在判定处列举名字 |
| `ToolsFor` 过滤后工具顺序变化，影响 prompt 前缀稳定性 | 明确保持原有顺序：内置工具按注册序、频道工具在后 |
| ctx 缓存策略导致同一请求内身份变化不生效 | 身份在请求生命周期内不变；判定只读注册表的类别表，不受调用次数影响 |
| 依赖 [计划 001](2026-10-29-001-fix-tool-result-completeness-plan.md) 的错误结果字段 | 是。计划 001 的 ErrorCode 表已预留 `permission_denied` 一行对应门控拒绝；两处的拒绝文案需保持一致 |

---

## Documentation / Operational Notes

- 无新增配置项
- 工具调用日志新增授权来源字段，可按其检索被拒绝的调用

---

## Sources & References

- `docs/reports/2026-10-27-whip-design-review.md` 第 2 节「动态工具目录与执行授权」
- `pkg/services/tools/registry.go`
- `pkg/services/tools/connection.go`（`genToolKey` 命名格式）
- `pkg/services/stores/corpus_x.go`（KBCreate 的既有 keeper 检查）
- [计划 001](2026-10-29-001-fix-tool-result-completeness-plan.md)（统一工具结果契约）
- [计划 004](2026-10-29-004-feat-conversation-persistence-basis-plan.md)（持久化与子 agent 权限快照）
