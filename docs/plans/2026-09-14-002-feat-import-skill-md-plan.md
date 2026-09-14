---
title: 'feat: 导入单个 SKILL.md'
type: feat
status: completed
date: 2026-09-14
origin: docs/brainstorms/2026-08-01-skill-support-requirements.md
---

# feat: 导入单个 SKILL.md

## Summary

把 origin 里预留的「导入」走出第一步：给一个 SKILL.md，服务端以 frontmatter 为权威解出 name/description 与一期那七列元数据，直接落库成技能。Web 端登录即可导入，CLI 供运维用；同名拒绝、不覆盖。`.skill`/`.zip`、目录整树与 URL 导入留到下一期（见 Scope Boundaries）。

---

## Requirements

- R1. 数据层提供「一段 SKILL.md 正文 → 技能记录」的导入入口，调用方只需给出正文（不再额外传 name/description，name 以 frontmatter 为权威）
- R2. 导入前校验：frontmatter 存在且 name 非空、name 合法（小写字母数字连字符、≤64）、description 非空且 ≤124、七列元数据不超列宽、正文为 UTF-8
- R3. 落库语义：正文进 `agent_skill.content`，七列元数据由 frontmatter 派生，owner 默认取当前用户，Channel 默认未投放（私有）
- R4. 同名不覆盖：name 已存在时返回可识别的冲突错误，既有行与既有资源文件保持不变
- R5. HTTP：登录用户即可导入（`POST /api/skills/import`），支持 JSON `content` 与 multipart `file` 两种入参，返回导入结果
- R6. CLI：新增 `import-skill <path>` 子命令读取本地 SKILL.md，可指定归属与投放频道；失败（校验/冲突）非零退出并说明原因
- R7. 错误可读：结构错误、校验错误、同名冲突三类分别给出明确信息，不静默吞掉

**Origin actors:** A1（Web/API 用户）、A4（Keeper/管理员：CLI 运维使用）
**Origin flows:** 新增一条导入流程（origin 的 F1-F3 覆盖既有创建/可见性/加载路径）

---

## Scope Boundaries

- 不做 `.skill`/`.zip` 压缩包导入，也不做「目录整树导入（SKILL.md + scripts/references/assets 一并入库）」——下一期
- 不做 URL / 远端仓库（含 hub 索引）导入——下一期
- 不做同名覆盖与版本升级（无 `update` 开关、不做 version 比对）
- 不做批量导入（一次一个文件）
- 不做管理面：列出他人技能、审核、分享、配额
- 不做导入历史/任务表（同步导入，无进度上报需求）

### Deferred to Follow-Up Work

- `.skill`/`.zip` 与目录导入：下一期计划，届时一并设计文件数量/大小上限、路径安全（绝对路径、`..`、符号链接）与忽略规则（`__MACOSX`、`.DS_Store`）
- URL / 远端导入：下一期，需处理内网地址与私仓鉴权
- 覆盖/升级语义与 `version` 比对：下一期

---

## Context & Research

- 契约与模型：`docs/skills.yaml`（一期已加七列）、`pkg/models/skills/skills_x.go`（`Frontmatter()` 返回 `SkillMeta`，`ValidateFrontmatter()` 已含列宽校验）
- 写入路径：`pkg/services/stores/skills_x.go` 的 `CreateSkillWithFiles`（事务内「frontmatter 派生七列 + 写资源文件」）、`dbBeforeCreateSkill`（owner 已指定时不覆盖，导入可指定归属）
- 路由与鉴权：`pkg/web/api/handle_skills_x.go` 的 `init()` 中 `regHI(true, "POST", "/skills", ...)` 注册范式、`createSkill` 的登录检查与错误映射
- 上传体量控制范式：`pkg/web/api/handle_corpus_import.go`（`maxImportUploadSize`、`isTooLarge`、`utf8.Valid`、BOM 处理）
- CLI：`main.go` 的 `urfave/cli/v2` 命令表（`initdb` / `import` / `import-swagger` / `embedding` / `agent`），`import-skill` 同风格
- 测试范式：`pkg/web/api/skill_user_test.go`（内存 store + chi router）、`pkg/services/stores/skills_integration_test.go`（真实 PostgreSQL，`TestMain` 已跑 `InitDB`）
- institutional learnings：`docs/solutions/` 下无与技能导入直接相关的条目

---

## Key Technical Decisions

- **一期只收单个 SKILL.md**：不带资源文件，因此完全不触碰 `agent_skill_file` 的写入策略；包/目录的大小与路径安全规则留到下一期随 `.zip` 一并设计，避免现在定一套规则再推翻
- **name 以 frontmatter 为权威**：导入路径不接受请求体里的 name/description。既有 create 的协议是「请求体必须与 frontmatter 一致」，那是为了纠客户端笔误；导入的协议本身就是「只有文件」，两者不冲突，各自成立
- **复用 `CreateSkillWithFiles`**：它已经承载「七列由 frontmatter 派生 + 原子写」的语义，导入只补校验与冲突映射，不新增第二条落库路径
- **同名不覆盖靠唯一约束识别**：直接用 `stores.ErrDuplicate` 判定并映射，不做「先查再插」（避免竞态与额外查询）
- **权限按端分**：Web 端登录即可、owner 强制当前用户（不接受指定 owner，避免越权代建）；CLI 不做角色限制，允许 `--owner` 供运维代建
- **导入即私有**：Web 端导入不提供投放参数，落库为未投放，需要公开时走既有更新接口——避免把「发布」混进「导入」
- **入参双形态**：JSON `content`（前端读文件后直接 POST）与 multipart `file`（curl/后台便利）由同一 handler 分派，省掉在前端形态上做取舍
- **同步导入**：单文件解析 + 一次插入，无 embedding、无外部调用，不需要任务表（与文档导入的异步取舍不同，那里的成本在逐行 embedding）

---

## Implementation Units

### U1. stores：ImportSkillMD

**Goal:** 数据层提供「一段 SKILL.md 正文 → 技能记录」的导入入口，含校验、冲突识别与归属/频道语义。

**Requirements:** R1, R2, R3, R4, R7

**Dependencies:** None

**Files:**

- Modify: `pkg/services/stores/skills_x.go`
- Test: `pkg/services/stores/skills_integration_test.go`

**Approach:**

- 新增导入函数（签名与选项结构实施时定），入参为正文与可选的归属/频道覆盖项；内部先用 `skills.Frontmatter` 解出 name/description，再复用 `CreateSkillWithFiles`
- 校验复用 `skills.ValidName` / `skills.ValidDescription` / `skills.ValidateFrontmatter`（含七列列宽），错误原样向上返回，由调用方映射状态码或退出码
- 同名冲突：透出 `stores.ErrDuplicate`，不吞、不重试
- 归属：未显式指定时取 `UserFromContext`；两者都缺时报错（CLI 无上下文，必须给 `--owner`）

**Patterns to follow:** `CreateSkillWithFiles` 的事务写法与 `dbBeforeCreateSkill`；`LoadForName` 的错误风格（`errors.Join` + 统一错误值）

**Test scenarios:**

- Happy path：合法 SKILL.md → 记录落库；name/description 与七列均来自 frontmatter；owner 为上下文用户；Channel 为未投放
- Happy path：显式指定归属与投放频道（运维代建场景）→ 落库值被尊重
- Edge case：无 frontmatter / name 非法 / description 超长 / version 超列宽 → 各自返回对应错误，且库中无新记录
- Error path：同名已存在 → `ErrDuplicate`，既有行的正文与元数据未被改动
- Integration：导入后经 `LoadForName` 读回，七列与 frontmatter 一致（复用一期的不变量）

**Verification:** `go test -tags=integration ./pkg/services/stores/` 通过；所有失败路径都不留下半条记录。

### U2. web/api：POST /api/skills/import

**Goal:** 登录用户通过 HTTP 导入单个 SKILL.md，成功返回导入结果，失败给出 400 与原因。

**Requirements:** R2, R3, R5, R7

**Dependencies:** U1

**Files:**

- Modify: `pkg/web/api/handle_skills_x.go`
- Test: `pkg/web/api/skill_user_test.go`

**Approach:**

- 在既有 `init()` 里注册 `regHI(true, "POST", "/skills/import", ...)`，与 `/skills` 的 CRUD 同处一层
- handler 先做登录检查（与 `createSkill` 一致），再分派 multipart `file` 或 JSON `content`；请求体上限与 UTF-8 校验沿用文档导入那套（超限 413）
- owner 强制当前用户；一期不接受调用方指定归属与投放频道（导入即私有，公开走更新接口）
- 冲突与校验错误映射为 400 并带回原因；响应给导入结果的必要字段（name/description/id）

**Patterns to follow:** `createSkill` 的登录与错误映射、`postCorpusImport` 的多部分读取与 413 处理、`skill_user_test.go` 的内存 store 与 router 装置

**Test scenarios:**

- Happy path：JSON `content` 合法 → 200，store 中出现该技能，owner 为当前用户，Channel 为未投放
- Happy path：multipart `file` 合法 → 200，结果与 JSON 入参一致
- Error path：未登录 → 401；既无 `file` 也无 `content` → 400
- Error path：非 UTF-8 / 缺 frontmatter / name 非法 / 同名 → 400，且 store 中没有新增
- Edge case：请求体超过上限 → 413
- Integration：失败请求之后，store 中技能数量与既有技能内容不变

**Verification:** `go test ./pkg/web/api/` 通过；`make gen-apidoc` 后新端点出现在 `docs/swagger.*`。

### U3. CLI：import-skill

**Goal:** 运维用一条命令把本地 SKILL.md 导入库。

**Requirements:** R6

**Dependencies:** U1

**Files:**

- Modify: `main.go`

**Approach:**

- 新增 `import-skill` 命令，参数为文件路径，flags 提供归属与投放频道；风格与 `import` / `import-swagger` 一致
- 读文件后调用 U1 的导入函数，成功打印技能名与结果；校验失败或同名时打印原因并非零退出
- 按用户要求不做角色限制；未给归属且无用户上下文时，提示补充归属参数

**Patterns to follow:** `importDocs` / `importSwagger` 的参数读取与日志风格

**Test expectation:** none -- CLI 只是解析路径与 flags 后调用 U1 的薄封装，行为语义由 U1 的集成测试覆盖；该单元的验证是对临时 SKILL.md 手工执行一次，并对同名再导入一次确认错误路径

**Verification:** 对本地 SKILL.md 执行成功并打印技能名；重复执行非零退出且提示同名冲突。

---

## Open Questions

### Resolved During Planning

- 导入源范围：一期只收单个 SKILL.md，`.skill`/`.zip` 与 URL 下一期
- 同名行为：不覆盖，直接拒绝
- 权限：Web 端登录即可，CLI 不限

### Deferred to Implementation

- U1 导入函数的精确签名与是否需要独立的选项结构
- 响应体字段命名与是否需要返回完整技能详情视图
- CLI 的频道参数解析细节（复用 `skills.Channel.Decode` 还是逗号切分后拼位）
- 导入成功/失败各打什么日志字段（沿用 `logger().Warn/Info` 风格）

---

## 参考

- `docs/brainstorms/2026-08-01-skill-support-requirements.md`：R11 预留的导入与导入导出边界
- `docs/plans/2026-08-01-001-feat-skill-support-plan.md`：R1-R13 与既有实现取舍
- `docs/plans/2026-09-14-001-feat-skill-frontmatter-fields-plan.md`：七列元数据契约与派生不变量
- `docs/plans/2026-08-24-001-feat-import-docs-async-plan.md`：导入类功能的既有范式（校验、错误处理、异步取舍）
