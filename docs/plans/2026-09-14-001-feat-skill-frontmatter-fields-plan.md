---
title: 'feat: Skill 契约补齐 frontmatter 字段'
type: feat
status: completed
date: 2026-09-14
origin: docs/improvements/2026-09-14-skill-frontmatter-comparison.md
---

# feat: Skill 契约补齐 frontmatter 字段

## Summary

对 `~/.hermes/skills` 下 119 个 SKILL.md 的 frontmatter 做了全量普查，逐字段对照现有的 `agent_skill` / `agent_skill_file`。**一期范围已定（2026-09-14）**：只补 7 个字段——`Version`、`Author`、`License`、`Platform`、`Category`、`Homepage`、`RelatedSkills`；`Platform` 用位枚举（列类型 `smallint`），`RelatedSkills` 用技能名数组（`jsonb`），其余为定长 `varchar`。`metadata.hermes` 的语义由既有 `MetaField`（`meta`）承担，不新增字段。

其余字段（`tags`、`requires_toolsets`、`prerequisites.*`、`dependencies`、`supersedes`、`upstream_skill` 等）**一期不加**，清单保留在本文备查。**尚未开工**：本计划未改 `docs/skills.yaml`，也未经 `make codegen`。逐字段普查、覆盖率与取值分布见 origin 文档。

---

## Problem Frame

现状：`Skill` 只有 `Name` / `Description` / `Content` / `Channel` / `Owner`，`File` 只有路径与内容元数据；`pkg/models/skills/skills_x.go` 的 `Frontmatter()` 只解出 name / description，`ValidateFrontmatter()` 也只强校验这两项。其余 frontmatter 字段虽然原样留在 `content` 里，但对索引、检索与门控完全不可见。

两个具体病灶：

1. **可用性门控只补了 OS 维度**：hermes 用 `platforms`（OS / 运行环境）+ `requires_toolsets`（工具集）+ `prerequisites.{commands,env_vars}`（CLI 与凭据）三类表达"这个技能在当前环境能否跑"。一期只让 `Platform` 落数据（且不接门控判定，见 Open Questions 1）；工具集与凭据两个维度仍在正文里，`BuildSkillIndex` 与 `skill_list` 依旧无条件列出全部可见技能，"模型照着一个跑不起来的正文去调工具"是已知遗留。
2. **导入缺少版本与出处**（导入落地时会变成迁移债）：R11（URL / `.zip` 导入）目前后置，但 `version`（95%）、`author`（91%）、`license`（90%）是导入的去重、升级与合规前提。尤其 `license`：语料里既有 MIT 批量，也有 AGPL 的 `obliteratus`（正文明确只能走 CLI、不得作为库引入）这类必须在导入时登记许可与出处的条目。

边界：**不重复 B-4**。生命周期与使用统计（`pinned` / `state` / `use_count` / `patch_count` / `created_by`）不在 frontmatter 内，属 `.usage.json` sidecar，已由 `docs/improvements/2026-09-13-hermes-agent-comparison.md` 的 B-4 覆盖。

---

## 一期字段契约（已定）

长度与取值均以 origin 文档的实测为准（分母 119）：

| 字段 | Go 类型 | pg 列 | frontmatter 来源 | 依据 |
|---|---|---|---|---|
| `Version` | `string` | `varchar(32)` | 顶层 `version` | 实测最长 6（`1.0.0`），覆盖率 113；留预发布与 build 元数据余量，一期只做长度约束、不校验 semver |
| `Author` | `string` | `varchar(128)` | 顶层 `author` | 实测最长 80（`Siqi Chen (@blader, …), ported by Hermes Agent`），覆盖率 108 |
| `License` | `string` | `varchar(32)` | 顶层 `license` | 实测全为 `MIT`（最长 3），覆盖率 107；按 SPDX 标识符留余量（`AGPL-3.0-or-later`） |
| `Platform` | `Platform`（`int8` 位枚举） | `smallint` | 顶层 `platforms` | 覆盖率 88，取值 `linux` / `macos` / `windows`；位枚举写法对齐 `Channel`（`multiple` + `stringer` / `decodable` / `textMarshaler` / `textUnmarshaler`） |
| `Category` | `string` | `varchar(32)` | `metadata.hermes.category` | 实测最长 20（`software-development`），覆盖率 16 |
| `Homepage` | `string` | `varchar(255)` | `metadata.hermes.homepage` | 实测最长 56，覆盖率 16；URL 无上限，长度与 `File.Path` 的 `varchar(255)` 一致 |
| `RelatedSkills` | `[]string` | `jsonb`，`default:'[]'` | `metadata.hermes.related_skills` | 实测 132 个条目、单条最长 33（技能名），覆盖率 65；写法对齐 `capability.yaml` 的 `Tags` |

契约细节：

- 全部 `isset: true`，让部分更新经 `SkillSet` 生效。
- `query` 只给 `Platform`（`equal,decode`，与 `Channel` 一致）与 `Category`（`equal`，管理端筛选）；`Version` / `Author` / `License` / `Homepage` / `RelatedSkills` 不设，避免为未定需求加查询路径。
- `Platform` 的 `0` 表示**未声明**（不参与门控），与 `Channel` 的 `0`（未投放，仅创建者可见）语义不同，需在枚举注释中写明。
- 解析层级不同：`version` / `author` / `license` / `platforms` 在 frontmatter 顶层，`category` / `homepage` / `related_skills` 嵌在 `metadata.hermes` 下。
- 字段全部可空：实测缺 `version` 6、`author` 11、`license` 12、`platforms` 31，`name` / `description` 仍是唯一事实必需项，`ValidateFrontmatter` 的强校验范围不变。
- 解析器同步扩展 `Frontmatter()` / `ValidateFrontmatter()`（`pkg/models/skills/skills_x.go`），否则字段落库了也进不了索引。
- 两个解码形态要在解析时归一化：`platforms` 是 YAML 序列（`platforms: [linux, macos]`），解成 `[]string` 再映射为位掩码；`author` 也出现过序列写法（`creative/comfyui`：`author: [kshitijk4poor, alt-glitch, purzbeats]`），若直接解成 `string` 会解码失败并让整条 frontmatter 判为不可用，需按逗号拼接归一化。

### 一期不加（留档）

`tags`（109 / 119）、`dependencies`（25）、`requires_toolsets`（4）、`prerequisites.commands`（17）/ `env_vars`（6）、`supersedes`、`upstream_skill`，以及他生态约定 `argument-hint`、`triggers`、`title`、`setup`、`compatibility`、`required_credential_files`、`environments`、`related_docs`。`session_platforms`（`[teams, cron]`）是我们的 `Channel` 等价物，不引入。

需要显式记下的两个后果：`tags` 是唯一的语义检索面，一期未收 → 索引与 `skill_list` 仍只有 name + description；`platform` 落列但未接门控 → 新字段暂时只是元数据。

---

## 落库形态

一期结论：7 个字段各自成列，**不引入整列 `jsonb frontmatter`**。字段数量少且各自类型明确（位枚举与数组两个特殊类型都有既成写法可对齐），整列 jsonb 的"改动小"收益不成立，反而会让 `Platform` / `Category` 的筛选走 jsonb 路径查询。将来若收 `tags`，沿用 `capability.yaml` 的 `jsonb + default:'[]'`。

---

## Open Questions

1. **`Platform` 是"过滤"还是"提示"。** 一期只落数据。后续要么在 `BuildSkillIndex` / `skill_list` 前过滤掉不适用的技能（改动集中在注入与工具两处，代价是要定义"当前环境"的输入：部署 OS），要么只在索引里展示平台信息让模型自行判断（零后端改动，但失败模式正是我们要消除的那种）。倾向前者。
2. **`Author` / `License` 是否纳入导入准入校验**（无 license 的第三方技能拒绝导入或强制私有）。这决定字段是"记录"还是"门禁"。
3. **`tags` 何时收。** 它是唯一的语义检索面，一期未收意味着发现能力仍停留在 name + description。
4. **既有记录是否需要回填。** frontmatter 原文就在 `agent_skill.content` 里，回填是一次纯解析，不需要人工；是否值得为本地存量数据加一个一次性命令（或并入 `initdb`）由实施时定。

契约范围（7 个字段）已定，其余字段的取舍不阻塞开工。

---

## Scope Boundaries

- 不改 `Channel` / `Owner` 的可见性语义（频道投放 ∪ 自建），本次只加字段。
- 不实现门控的运行时判定（待问题 1 定夺）；也不引入"技能运行环境探测"。
- 不展开 `metadata.hermes`：它的语义已由 `comm.MetaField` 承担，其余嵌套字段（`tags` 等）一期不落列。
- 不动 `agent_skill_file` 的结构。文件级 `sha256`（curator ledger 用它做变更审计）属导入与 B-4 的范畴。
- 导入（R11）本身不在本计划内，本计划只保证字段先就位。

---

## 参考

- `docs/improvements/2026-09-14-skill-frontmatter-comparison.md`：119 个 SKILL.md 的逐字段普查、取值分布与复现命令
- `docs/skills.yaml`、`pkg/models/skills/skills_x.go`、`pkg/services/agent/skill_inject.go`、`pkg/services/tools/skills.go`
- `docs/plans/2026-08-01-001-feat-skill-support-plan.md`：R1-R13（R11 导入后置、R2 共享字段）
- `docs/improvements/2026-09-13-hermes-agent-comparison.md`：§3.4 技能、B-4 技能生命周期与使用统计

一旦动 `docs/skills.yaml`，按 `AGENTS.md` 需执行 `make codegen` 并同步 `pkg/models/skills` / `pkg/services/stores` 的扩展代码，提交前跑 `make vet` 与 `make lint`。
