---
title: refactor: system prompt 只承载稳定内容
type: refactor
status: completed
date: 2026-09-13
origin: docs/improvements/2026-09-13-hermes-agent-comparison.md, docs/improvements/2026-09-13-prompt-assembly-cache-comparison.md
---

# refactor: system prompt 只承载稳定内容

## Summary

提示组装改为「system 只放稳定内容 + 常驻数据不进任何消息 + 能力走工具」：system 消息由 preset 正文、工具说明、频道说明、记忆引导、技能索引（仅 name/description）构成；时间、SessionID、用户身份、记忆清单、知识库命中全部不再进入发给模型的消息。唯一的逐轮注入是**显式技能激活**（频道 `/skill <name>`），以 user 消息形式放在历史之后、本轮提问之前。

这是 `docs/improvements/2026-09-13-prompt-assembly-cache-comparison.md` 第 4.7 节的 **Q 路线**（对齐 pi-mono 的"prompt 只放静态内容"取向，记忆与技能正文经工具按需取）。上一版计划（把易变块整体搬进一条 user 消息）经实测确认会导致"记忆嵌入新一轮明显影响沟通结果"，本版将其推翻。

---

## Problem Frame

上一版实现把时间、SessionID、用户身份、记忆清单、技能块合并成一条 user 角色的上下文消息，插在历史与本轮提问之间。实测发现：记忆清单以**用户口吻**出现在提问之前，模型会把这段系统注入的数据当成用户本轮发言的一部分，明显影响回复质量；其中 `PrettyTextForOwner` 的祈使句（`you need to use the tool memory_recall again to retrieve the memory content.`）在 user 消息里读起来像"用户要求你调工具"。

对照研究（`docs/improvements/2026-09-13-prompt-assembly-cache-comparison.md`）给出的两条自洽路线中，选了 Q：

- hermes 式（P）：常驻数据留在 system 的 volatile 层，但**按会话冻结**（持久化渲染结果、压缩边界重建）。收益最大，代价是会话内记忆不立即可见 + 需新增会话级渲染缓存与失效策略。
- pi-mono 式（Q）：prompt 里只放静态内容，逐轮/环境数据一概不进 prompt。无需新基础设施，代价是模型必须先"想起"调工具才能拿到记忆。

Q 被选中是因为：上一版的病灶正是"数据以用户口吻出现在提问之前"，而 Q 从根上取消了这类注入；同时它不需要为冻结快照引入缓存与失效语义。

---

## Requirements

- R1. system 消息只由稳定块构成：preset 正文、工具说明、频道说明、记忆引导、技能索引（仅 name/description）。同一会话连续两轮字节级一致。
- R2. 时间、SessionID、用户身份、记忆清单、知识库命中不出现在任何发给模型的消息里。
- R3. 记忆只经工具（`memory_list` / `memory_recall` / `memory_store` / `memory_forget`）访问；稳定块提供一段记忆使用引导，替代被移除的记忆清单。
- R4. 技能只以索引（name + description）出现在 system；技能正文经 `skill_read` 工具或**显式激活**取得。
- R5. 显式技能激活（频道 `/skill <name>`）以 user 消息形式注入技能全文，位置在历史之后、本轮提问之前；无激活时不产生该消息。
- R6. 行为不回归：有工具时 `cs.SetTools` 副作用保留；重试/Regenerate 仍跳过最后一条历史；流式与非流式共用同一序列；用量 `MsgCount` 仍按对话消息口径（排除激活消息）。

---

## Scope Boundaries

- 不引入 provider 级缓存控制（`cache_control` 断点、缓存键、TTL）；本项只确定"发什么"，不确定"如何缓存"。
- 不改记忆的检索、衰减与 tier 策略，不改记忆工具的参数与语义。
- 不改 preset 字段、不改 `docs/*.yaml` 契约、不触发 `make codegen`。
- 不做上下文压缩与历史持久化改造（历史窗口滑动是独立议题，见对照研究 4.6）。
- 不为环境数据新增"会话常量"层：不把 SessionID/身份放进 system 末尾（那是 P 的形态）。

### Deferred to Follow-Up Work

- **时间能力**：已由 `current_time` 工具承接（`ToolNameCurrentTime`，无条件注册，描述一句话）：模型需要时自行调用，prompt 不承担时间。原 `agent.ThisMoment()` 的时辰换算随之迁入 `pkg/services/tools/invokers.go`，`DateInContext` 保持删除。
- **身份可见性**：模型将不再知道用户姓名/UID。若需要，可在 system 末尾放会话常量（P 形态）或提供 `user_profile` 工具——需产品决策。
- **Web 侧技能激活**：`ChatRequest.Skills` 现在是索引范围（元数据），不再触发全文直注；Web 侧若需要强制加载，应新增显式激活入口或依赖 `skill_read`。
- **缓存命中计数暴露**：`prompt_cache_hit_tokens`/`miss` 仍未记录，无法量化稳定块的实际收益。

---

## Context & Research

### Relevant Code and Patterns

- `pkg/services/agent/prompt.go`：`SystemPromptParts` / `BuildSystemMessage`，提示分层的唯一归属。
- `pkg/services/agent/skill_inject.go`：`BuildSkillPrompt`（阈值决定全文/元数据，本版改为恒元数据的 `BuildSkillIndex`）、`SkillPromptByCommand`（单技能全文，本版成为激活路径的唯一实现，此前是死代码）。
- `pkg/web/api/prompt.go`：`promptParts` 槽位映射、`prepareSystemMessage`、`chatMessages` 序列组装。
- `pkg/web/api/handle_platform.go`：`msg.SkillName` 由 `/skill` 指令设置（`commands.go` `handleSkillCommand`），是激活路径的入口。
- `pkg/services/tools/defines.go` / `skills.go`：`memory_*` 与 `skill_*` 工具描述，稳定块引导需与之互补而非重复。
- `docs/improvements/2026-09-13-prompt-assembly-cache-comparison.md`：Q 路线定义、块级对照表（4.7）、顺序与窗口滑动的更正（4.2 / 4.6）。

### Institutional Learnings

- `docs/solutions/` 无直接相关条目。

### External References

- `../pi-mono/packages/coding-agent/src/core/system-prompt.ts`：system 只含身份、工具清单、guidelines、上下文文件、技能索引、cwd。
- `../hermes-agent/agent/system_prompt.py`、`tools/memory_tool.py`：P 路线的证据；其"技能激活走 user 消息"（`AGENTS.md:420`）正是本版 R5 的依据。

---

## Key Technical Decisions

- **常驻数据不进任何消息，而不是换一条消息携带**：上一版仅换了位置（system → user 消息），角色语义仍在；本版直接取消注入，让数据回到工具。
- **技能索引进 system，技能正文走工具/激活**：索引是静态（会话内几乎不变）内容，放 system 既符合两家做法又不产生逐轮变化；正文体积大且按需使用，走 `skill_read` 或显式激活。
- **记忆引导进稳定块**：取消清单后，模型"想起去查记忆"的概率是唯一风险点，用一段静态引导兜住（对齐 hermes 把 `MEMORY_GUIDANCE` 放 stable 层的做法）。
- **激活块保留为 user 消息**：这是 hermes 明文认可的唯一 user 注入场景（斜杠命令激活），语义上也确实是"用户显式要求加载某技能"。
- **不做冻结**：Q 不需要会话级渲染缓存，system 内容完全由"当轮可见的 preset + 工具集 + 技能索引"决定。
- **删除失效配置项**：`DateInContext` 与 `SkillDirectThreshold` 在新形态下没有消费者，随本项一并删除（README 与 .env 示例均未提及，无文档面）。

---

## Open Questions

### Resolved During Planning

- **记忆清单取消后靠什么让模型去查记忆？** 稳定块的记忆引导 + 工具描述；`memory_list`/`memory_recall` 的描述已说明用途，引导补"何时该查"。
- **技能索引放哪里？** system 稳定块（两个参考实现都如此），不是 user 消息。
- **`/skill` 全文注入去哪？** 以 user 消息注入，属显式激活而非常驻数据。
- **无工具分支的知识库命中怎么办？** 取消注入；有 `kb_search` 工具覆盖该能力，且生产路径下工具恒存在，该分支实际不可达。
- **SkillPromptByCommand 与 BuildSkillPrompt 的关系？** 前者成为激活路径唯一实现，后者删除。

### Deferred to Implementation

- 记忆引导与技能索引的文案（中英文、分节标题）在实施时定稿，以一次真实会话观察为准。
- 激活消息是否需要额外的分节标题以区分"用户原话"（当前倾向加一行 `# Activated Skill`）。

---

## High-Level Technical Design

```
上一版（被推翻）

  [ system: preset · 工具说明 · 频道说明 ]
  [ history ... ]
  [ user: 时间 · SessionID · 身份 · 记忆清单 · 技能块 ]   <- 病灶
  [ user: 本轮提问 ]

本版（Q）

  [ system: preset · 工具说明 · 频道说明 · 记忆引导 · 技能索引 ]
  [ history ... ]
  [ user: 激活的技能全文 ]           <- 仅 /skill 显式激活时存在
  [ user: 本轮提问 ]
```

组装器形状：

```
SystemPromptParts {
    Base, Tools, Channel string   // preset 与频道
    Memory, Skills       string   // 稳定引导与技能索引
    Activation           string   // 易变：显式激活的技能全文
}

stable     = render(Base, Tools, Channel, Memory, Skills) -> system 消息
activation = render(Activation)                            -> user 消息（为空则不产生）
```

---

## Implementation Units

### U1. agent 包：稳定块 + 激活块

**Goal:** `SystemPromptParts` 承载稳定块与可选激活块；`BuildSystemMessage` 只产出稳定 system 消息与工具定义；技能索引与激活提示实现分离。

**Requirements:** R1, R3, R4, R5

**Dependencies:** None

**Files:**

- Modify: `pkg/services/agent/prompt.go`
- Modify: `pkg/services/agent/skill_inject.go`
- Modify: `pkg/services/agent/skill_inject_test.go`
- Create: `pkg/services/agent/prompt_test.go`

**Approach:**

- `SystemPromptParts` 字段改为 `Base/Tools/Channel/Memory/Skills/Activation`；`StableMessage()` 按书写顺序拼稳定块，`ActivatedMessage()` 在 `Activation` 非空时产出 user 消息。
- 删除 `Normalize` 中的时间逻辑（`DateInContext` 注入整体移除）。
- `BuildSystemMessage(ctx, toolreg, parts)` 返回 `(llm.Message, []llm.ToolDefinition)`——激活块由调用方按需渲染。
- `BuildSkillPrompt` 改为 `BuildSkillIndex`：恒为 `name + description` 清单，附"用 skill_read 加载全文"提示；`SkillDirectThreshold` 不再参与。
- `SkillPromptByCommand` 保持现状，成为激活路径的实现。

**Patterns to follow:**

- `pkg/services/agent/prompt.go` 现有 `joinBlocks` 与默认值回退风格。
- `pkg/services/agent/skill_inject.go` 的窄接口 `SkillStore`。

**Test scenarios:**

- Happy path：稳定块按 Base → Tools → Channel → Memory → Skills 顺序拼接。
- Happy path：Base 为空回退 `DefaultSystemMsg`；有工具且 Tools 为空回退 `DefaultToolsMsg`；无工具则不含工具说明。
- Edge case：`Activation` 为空 → 不产生激活消息；非空 → user 角色且内容为技能全文。
- Edge case：技能索引不含技能正文，只含 name/description。
- Integration：同一组稳定块渲染两次字节一致；激活内容不出现在稳定块中。

**Verification:**

- `go test ./pkg/services/agent/` 通过；CLI 两处调用点编译通过且不再注入时间。

---

### U2. web/api：序列改为「稳定 system + 历史 + 激活 + 提问」

**Goal:** HTTP 与频道两条入口不再注入任何常驻数据；激活消息只由显式技能激活产生。

**Requirements:** R1, R2, R3, R4, R5, R6

**Dependencies:** U1

**Files:**

- Modify: `pkg/web/api/prompt.go`
- Modify: `pkg/web/api/handle_convo.go`
- Modify: `pkg/web/api/handle_platform.go`
- Test: `pkg/web/api/prompt_test.go`

**Approach:**

- `promptStore` 收窄为 `{Preset(), Skill()}`（记忆与知识库不再经提示层访问）。
- `promptParts` 只填稳定块：preset 三段 + 记忆引导（记忆工具在场时）+ 技能索引（技能工具在场时）。
- `prepareSystemMessage` 返回 `(llm.Message, []llm.ToolDefinition)`，保留 `cs.SetTools` 副作用。
- 频道路径：`msg.SkillName` 经 `SkillPromptByCommand` 取全文，渲染为激活消息；失败仅告警并降级为无激活。
- `chatMessages` 签名与语义不变（`[system][历史][激活][提问]`），`conversationMsgCount` 继续排除注入消息。

**Test scenarios:**

- Happy path：记忆引导与技能索引出现在 system；记忆清单、时间、SessionID、身份不出现在任何消息。
- Edge case：无激活 → 序列为 `[system][历史][提问]`；有激活 → `[system][历史][激活][提问]`。
- Edge case：技能索引为空或全部不可见 → 不产生技能段落，其余稳定块不受影响。
- Edge case：`/skill` 指向不可见技能 → 无激活消息且本轮照常进行。
- Integration：`MsgCount` 与改动前同口径（排除激活消息）。

**Verification:**

- `go test ./pkg/web/api/` 通过；`make vet`、`make lint` 通过。

---

### U3. 配置面清理

**Goal:** 删除失去消费者的配置项。

**Requirements:** R2

**Dependencies:** U1, U2

**Files:**

- Modify: `pkg/settings/config.go`

**Approach:** 删除 `DateInContext` 与 `SkillDirectThreshold`（README、.env 示例、docker-compose 均未引用）。

**Test scenarios:** 无（纯配置删除，编译与现有测试覆盖）。

**Verification:** `go build ./...` 通过；全仓搜索不再有引用。

---

### U4. 文档同步与诊断工具

**Goal:** 内部文档与提示 dump 工具反映新形态。

**Requirements:** R1, R2

**Dependencies:** U2

**Files:**

- Modify: `docs/WORKFLOWS.md`
- Modify: `docs/COMPONENTS.md`
- Modify: `pkg/web/api/prompt_dump_test.go`（临时诊断 harness）

**Approach:** 更新两处序列图/流程描述；dump 场景改为"稳定 system + 历史 + 可选激活 + 提问"，便于前端联调时核对实际注入内容。

**Verification:** `MORRIGAN_DUMP_PROMPT=1 go test -run TestDumpPromptContent -v ./pkg/web/api/` 输出与设计一致。

---

## System-Wide Impact

- **Interaction graph:** `/api/chat`（流式与非流式）、`/api/chat-sse`、企业微信/飞书回调与 WebSocket、CLI `agent` 与 REPL，全部经新组装路径；CLI 不再注入时间。
- **Error propagation:** 技能索引与激活查询失败维持"记日志、降级为空、不阻塞本轮"。
- **State lifecycle risks:** 无新增持久化；激活消息不落历史、不计用量。
- **API surface parity:** HTTP 与频道契约不变；`ChatRequest.Skills` 语义从"可触发全文直注"收敛为"索引范围"（与字段注释一致）。
- **Unchanged invariants:** preset 字段集合、`docs/*.yaml` 契约、工具定义与工具说明文本、记忆检索与衰减策略、重试/Regenerate 语义、频道去重、用量 `MsgCount` 口径。
- **Capability regressions（需产品确认）:** 模型不再知道当前时间、用户身份与 SessionID；记忆需要主动调工具。

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| 取消记忆清单后模型不去查记忆，记忆能力形同虚设 | 稳定块加记忆引导；若观察仍不查，回退到 P（冻结快照）而非恢复逐轮注入 |
| 技能索引放 system 后，技能变更会改变 system 字节 | 技能索引只随"可见技能集/请求参数"变化，属低频；换来的是取消逐轮注入 |
| 模型失去时间/身份感知 | 列入 Deferred：时间走 `current_time` 工具，身份走会话常量或工具，按产品需要再加 |
| Web 客户端依赖 `skills` 触发全文直注 | 字段注释本就声明为索引范围；行为变化写进 Release 说明与计划，必要时新增显式激活入口 |
| 稳定块内容增多导致 prompt 变长 | 索引仅 name/description，引导一段话；相比被移除的每轮清单/全文是净减少 |

---

## Documentation / Operational Notes

- 上一版计划把 `README` 列为"无需改动"；本版删除两个环境变量（`date_in_context`、`SKILL_DIRECT_THRESHOLD`），README 与 .env 示例当前均未提及，无需改文档。
- 上线观察点：模型是否会主动调用 `memory_list`/`memory_recall`（`matches`/工具调用日志）；技能激活路径是否仍按预期工作；system 消息长度是否下降。

---

## Sources & References

- 对照研究：[docs/improvements/2026-09-13-prompt-assembly-cache-comparison.md](docs/improvements/2026-09-13-prompt-assembly-cache-comparison.md)（Q 路线、4.7 块级对照）
- 前序研究：[docs/improvements/2026-09-13-hermes-agent-comparison.md](docs/improvements/2026-09-13-hermes-agent-comparison.md)（B-1 条目）
- 相关代码：`pkg/web/api/prompt.go`、`pkg/web/api/handle_convo.go`、`pkg/web/api/handle_platform.go`、`pkg/services/agent/prompt.go`、`pkg/services/agent/skill_inject.go`、`pkg/services/tools/defines.go`、`main.go`
