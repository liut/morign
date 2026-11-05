---
title: feat: Memory 分层衰减重构
type: feat
status: active
date: 2026-07-07
origin: docs/brainstorms/2026-07-04-memory-tier-decay-requirements.md
---

# feat: Memory 分层衰减重构

## Summary

在现有 `convo_memory` 扁平模型上引入三级分层（working/short-term/long-term）和艾宾浩斯时间衰减，通过 `convo.yaml` 代码生成扩展数据模型，在 `MatchMemories` 中插入衰减加权重排逻辑，并更新 MCP 工具输出。实现单元按依赖顺序排列：数据模型 → 评估器 → 衰减引擎 → 生命周期钩子 → 工具/配置 → Bug 修复 → 测试。

---

## Problem Frame

当前 `convo_memory` 是扁平 key-value 存储，检索排序完全由 pgvector cosine 距离决定。随着对话积累，向量召回无法区分重要偏好和过期碎片，`memory_recall` 质量持续下降。参见 origin 文档 Problem Frame。

---

## Requirements

**Origin actors:** A1 (LLM Agent), A2 (Memory Engine), A3 (终端用户)
**Origin flows:** F1 (记忆写入与自动分层), F2 (衰减检索与重排), F3 (访问强化与晋升), F4 (系统提示注入——第一阶段不变)
**Origin acceptance examples:** AE1 (long-term 分配), AE2 (working 分配), AE3 (衰减排序差异), AE4 (时钟重置强化), AE5 (遗忘淡出), AE6 (向量清理)

### 数据模型

- R1. `convo_memory` 增加 `tier` 字段（枚举：`working` / `short-term` / `long-term`），NOT NULL，默认 `working`
- R2. 增加 `importance_score`（float, 0-1）、`last_accessed_at`（timestamp）、`access_count`（int, 默认 0）、`decay_rate`（float）
- R3. `tier` 与现有 `cate` 正交

### 重要性评估

- R4. 记忆写入时自动规则评估 `importance_score`，不依赖 LLM
- R5. 规则基于文本长度、标点、关键词加权打分
- R6. 分层路由：score >= 0.8 → long-term，>= 0.6 → short-term，否则 → working
- R7. 阈值和 decay_rate 比例可配置

### 衰减与检索

- R8. 检索时计算 `R = e^(-t / (24 × S))`，t 为距最近访问的小时数，S 为 decay_rate
- R9. decay_rate 默认比例 working:short-term:long-term = 1:7:60
- R10. `FinalScore = VectorSimilarity × R × ForgottenMultiplier`
- R11. R 低于遗忘阈值时 ForgottenMultiplier = 0.1

### 强化与晋升

- R12. 检索命中时更新 last_accessed_at（时钟重置，下次 CalcRetention 得 R≈1.0）和 access_count
- R13. access_count 达阈值时自动晋升 tier
- R14. 晋升阈值可配置

### 系统提示注入

- R15. 第一阶段 prepareSystemMessage 中 ListMemory 逻辑不变

### MCP 工具

- R16. memory_recall 返回附带 tier
- R17. memory_list 支持 tier 过滤
- R18. memory_store 向后兼容，tier 由系统自动分配

### Bug 修复

- R19. DeleteMemory 时同步删除 corpus_vector_400 对应行

---

## Scope Boundaries

- LLM 驱动的重要性评估（首期规则评估）
- 语义去重和 LLM 记忆压缩
- 图检索和全文检索
- 经验与技能蒸馏
- 第二阶段：系统提示按 tier 过滤、自动清理过期 working 记忆

---

## Context & Research

### Relevant Code and Patterns

| Pattern | Reference | Usage |
|---------|-----------|-------|
| YAML → codegen | `docs/convo.yaml` → `make codegen` | 模型字段添加 |
| Store X-extension | `pkg/services/stores/convo_x.go` | 新方法定义 |
| Invoker 闭包 | `InvokerForMemoryRecall()` | MCP 工具实现 |
| envconfig 配置 | `pkg/settings/config.go` | 可配置参数 |
| MatchVectorWith | `pkg/services/stores/corpus_x.go:259` | 向量匹配入口 |
| DocMatch.Similarity | `pkg/models/corpus/corpus_gen.go:208` | 现有相似度字段 |
| pgvector 匹配 | `data/schemas/pg_10_match_doc.sql` | PostgreSQL 向量函数 |
| 集成测试 | `pkg/services/stores/integration_test.go` | mock embedding 模式 |
| SQL 迁移 | `data/schemas/20????_*.up.sql` | DDL 变更惯例 |
| MetaField 扩展 | `comm.MetaField` on all models | JSONB 元数据存储 |

### Institutional Learnings

`docs/solutions/` 中无 memory 相关记录——本次重构是该领域的首次结构化变更。

### Code Generation Constraint

`_gen.go` 文件由 `make codegen` 从 `docs/*.yaml` 自动生成，不可手动编辑。`make codegen` 依赖 `../scaffold/scripts/codegen` 工具链。模型字段变更流程：编辑 YAML → 运行 codegen → 验证生成结果。

---

## Key Technical Decisions

- **字段新增走 codegen 流程**: 编辑 `docs/convo.yaml` Memory 模型定义 → `make codegen` 重新生成 `convo_gen.go`。若 codegen 工具不可用，备选方案是手动添加字段到 `convo_gen.go`（一次性例外）并创建迁移 SQL
- **衰减重排插入 MatchMemories 中段**: `MatchVectorWith` 返回 `DocMatches`（含 similarity）→ 计算 composite score → 重排 → 再取完整记录。不修改 `MatchVectorWith` 或 `DocMatch` 类型，避免影响知识库检索路径
- **重要性评估为纯函数**: `evaluateImportance(text string) float64`，无副作用，独立可测。便于后续替换为 LLM 评估器
- **强化通过时钟重置实现，不使用 CalcReinforce**: Retention 是 `last_accessed_at` 的派生值，不持久化。将 `last_accessed_at` 更新为 now 即可使下次 `CalcRetention` 得到 R≈1.0，效果等价于完全强化。`CalcReinforce` 保留为库函数供未来使用
- **tier 用 text 存储而非 enum**: PostgreSQL enum 类型的 ALTER 操作复杂，text + 应用层校验更灵活，与现有 `cate` 字段一致
- **decay_rate 存于行而非计算**: 每条记忆在写入/晋升时固化 `decay_rate`，避免每次检索时查配置做映射。晋升时更新为新 tier 的 decay_rate

---

## Open Questions

### Resolved During Planning

- **字段添加方式**: 走 `convo.yaml` codegen，备选方案为手动编辑 + 迁移 SQL
- **decay_rate 存储策略**: 存于行，写入/晋升时固化
- **晋升 access_count 阈值默认值**: working → short-term = 3 次访问，short-term → long-term = 10 次访问
- **tier/cate 是否需要联合索引**: 第一阶段不需要——tier 过滤仅在 `memory_list` 中使用，频率远低于按 owner_id + key 查询
- **`last_accessed_at` 初始值**: 使用 `now()`（与 `created` 在 insert 时等价），迁移 SQL 设置 `DEFAULT now()`，确保初始 R=1.0

### Deferred to Implementation

- [Affects R5] 规则评估的具体关键词列表和权重——需要在实现时结合现有记忆数据分析确定
- [Affects R9] 三层 decay_rate 精确数值（PowerMem 默认 1:7:60 作为起点）——需在真实使用中调参

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

### 检索重排流程（核心变更）

```
memory_recall(query)
  │
  ▼
MatchMemories(ctx, MatchSpec{Query, Limit})
  │
  ├─ 1. GetEmbedding(query) → vec
  ├─ 2. MatchVectorWith(vec, threshold, limit) → DocMatches [{DocID, Subject, Similarity}]
  │
  ├─ 3. [NEW] 加载每条匹配记忆的 decay 元数据 (tier, decay_rate, last_accessed_at)
  │     SELECT id, tier, decay_rate, last_accessed_at
  │     FROM convo_memory WHERE id IN (matched IDs)
  │
  ├─ 4. [NEW] 计算每条记忆的 composite score:
  │     t = hours_since(last_accessed_at)
  │     R = exp(-t / (24 * decay_rate))
  │     forgotten = R < forget_threshold ? 0.1 : 1.0
  │     final_score = similarity * R * forgotten
  │
  ├─ 5. [NEW] 按 final_score DESC 重排匹配列表
  │
  ├─ 6. 取 final_score 前 limit 条 → 查询完整 Memory 记录
  │
  └─ 7. 返回结果
```

### 写入分层流程

```
memory_store(key, content, category)
  │
  ▼
InvokerForMemoryStore
  │
  ├─ 1. 现有 upsert 逻辑（按 key 查找 → 更新或创建）
  │
  ├─ 2. [NEW] 若是新建: evaluateImportance(content) → importance_score
  │     tier = importance_score >= long_term_threshold  → "long-term"
  │          : importance_score >= short_term_threshold → "short-term"
  │          :                                          → "working"
  │     decay_rate = decayRates[tier]
  │     last_accessed_at = now()
  │
  └─ 3. 若是更新已有记忆: 保持现有 tier/decay_rate 不变
        （仅更新 content/cate，不重新评估重要性）
```

### 强化与晋升流程

```
memory_recall 返回结果后
  │
  ▼
[同步] 对每条返回的记忆:
  │
  ├─ 1. access_count += 1, last_accessed_at = now()
  │     (时钟重置策略: 更新 last_accessed_at 后，下次 CalcRetention 得 R≈1.0)
  │
  ├─ 2. [晋升检查]
  │     if tier == "working" && access_count >= promote_w2s → tier = "short-term"
  │     if tier == "short-term" && access_count >= promote_s2l → tier = "long-term"
  │     decay_rate 同步更新为新 tier 的值
  │
  └─ 3. UPDATE convo_memory SET ... WHERE id = ?
```

> **设计决策: 为什么不用 CalcReinforce?** Retention（R）是 `last_accessed_at` 和 `decay_rate` 的派生值（`R = e^(-t/(24×S))`），不持久化。曾设计 `CalcReinforce(R, F)` 计算部分强化值，但无字段可持久化其输出。最终采用时钟重置策略——直接将 `last_accessed_at` 设为 now，等价于将留存率拉回 1.0，比部分强化更简单且效果一致。`CalcReinforce` 保留为库函数，供未来需要部分强化的场景使用。

---

## 交付进度

- **U1–U6：已落地** — `docs/convo.yaml` 的分层字段、`pkg/models/convo/memory_eval.go` 的 `EvaluateImportance`、`pkg/models/convo/memory_decay.go` 的 `CalcRetention` / `RouteTier`、`pkg/services/stores/convo_x.go` 的衰减重排 / 写入分层 / 强化晋升 / 向量清理、`pkg/settings/config.go` 的阈值配置均已实现。
- **U7（集成测试）：未落地** — 缺一条「写入 → 分层 → recall 衰减重排 → access_count 递增 → tier 晋升 → forget 清向量」的端到端链路测试，目前只有 `pkg/models/convo/memory_decay_test.go` 的单元测试。
  - 已有基础设施：`pkg/services/stores/integration_test.go` 的 mock embedding 与 `InitDB()`，可照 `TestIntegration_MemoryCRUD` 扩一条。这套逻辑涉及时钟与阈值配置，无集成测试时调参风险高。

---

## Implementation Units

### U1. 数据模型扩展

**Goal:** 为 `convo_memory` 添加分层衰减所需的全部字段

**Requirements:** R1, R2, R3

**Dependencies:** None

**Files:**
- Modify: `docs/convo.yaml` (Memory model 字段定义)
- Regenerate: `pkg/models/convo/convo_gen.go` (codegen 输出)
- Create: `data/schemas/20260707000000_memory_tier_decay.up.sql`

**Approach:**
- 在 `convo.yaml` 的 Memory model 中添加 5 个新字段，均带 `isset: true`
- 运行 `make codegen` 重新生成 `convo_gen.go`
- 创建迁移 SQL：ALTER TABLE 添加列 + 回填默认值
- 若 codegen 环境不可用，手动在 `convo_gen.go` 的 `MemoryBasic` 和 `MemorySet` 中添加字段，标记为一次性例外

**YAML 变更（在 convo.yaml Memory fields 中）:**
```yaml
      - comment: 分层 (working/short-term/long-term)
        name: Tier
        type: string
        tags: {json: 'tier', pg: ',notnull,type:text,default:working'}
        isset: true
        query: 'match'
      - comment: 重要性评分 (0-1)
        name: ImportanceScore
        type: float64
        tags: {json: 'importanceScore', pg: 'importance_score,notnull,type:float,default:0.5'}
        isset: true
      - comment: 最近访问时间
        name: LastAccessedAt
        type: time.Time
        tags: {json: 'lastAccessedAt', pg: 'last_accessed_at,notnull,type:timestamptz,default:now()'}
        isset: true
      - comment: 访问计数
        name: AccessCount
        type: int
        tags: {json: 'accessCount', pg: 'access_count,notnull,type:int,default:0'}
        isset: true
      - comment: 衰减率
        name: DecayRate
        type: float64
        tags: {json: 'decayRate', pg: 'decay_rate,notnull,type:float,default:1.0'}
        isset: true
```

**Migration SQL:**
```sql
ALTER TABLE convo_memory ADD COLUMN IF NOT EXISTS tier text NOT NULL DEFAULT 'working';
ALTER TABLE convo_memory ADD COLUMN IF NOT EXISTS importance_score float NOT NULL DEFAULT 0.5;
ALTER TABLE convo_memory ADD COLUMN IF NOT EXISTS last_accessed_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE convo_memory ADD COLUMN IF NOT EXISTS access_count int NOT NULL DEFAULT 0;
ALTER TABLE convo_memory ADD COLUMN IF NOT EXISTS decay_rate float NOT NULL DEFAULT 1.0;
```

**Patterns to follow:**
- `docs/convo.yaml` 现有 Memory model 字段格式
- `data/schemas/20260331234900_convo_user.up.sql` 迁移风格
- `pkg/models/convo/convo_gen.go` MemoryBasic / MemorySet 结构

**Test scenarios:**
- Happy path: codegen 后 Memory 结构包含全部新字段，JSON tag 命名正确
- Happy path: 迁移 SQL 在已有数据表上执行成功，现有行回填默认值
- Edge case: 字段在 MemorySet 中可被 SetWith 正确更新

**Verification:**
- `make codegen` 无报错，`convo_gen.go` 包含新字段
- 迁移 SQL 在测试数据库上执行成功
- `go build ./...` 编译通过

---

### U2. 重要性评估器

**Goal:** 实现纯规则的重要性评分函数，输出 0-1 分数

**Requirements:** R4, R5

**Dependencies:** None

**Files:**
- Create: `pkg/services/stores/memory_eval.go`
- Test: `pkg/services/stores/memory_eval_test.go`

**Approach:**
- 导出函数 `EvaluateImportance(text string) float64`
- 评分维度：文本长度（log 归一化）、特殊标点（`?` `!` 加权）、关键词命中（preference/always/never/urgent/password/remember 等加权）
- 各维度加权求和后 clamp 到 [0, 1]
- 关键词列表定义为包级变量，便于后续从配置加载

**Technical design:**
```
baseScore = clamp(log(len(text)+1) / log(500), 0, 1) * 0.3  // 长度因子
punctScore = (count('?') + count('!')) * 0.05               // 标点因子
kwScore = sum(keywordWeights[k] for k in keywords if k in textLower) * 0.15
return clamp(baseScore + punctScore + kwScore, 0, 1)
```

**Patterns to follow:**
- `pkg/services/stores/llm.go` 中的纯函数风格
- 避免外部依赖，保持函数纯度

**Test scenarios:**
- Happy path: "用户偏好黑暗模式，always 使用 dark theme" → score >= 0.8
- Happy path: "今天天气不错" → score < 0.6
- Edge case: 空字符串 → score = 0
- Edge case: 极长文本（>10000 字符）→ score 不超过 1.0
- Edge case: 只有标点无关键词 → score 仅由长度和标点贡献

**Verification:**
- `go test -v ./pkg/services/stores/ -run TestEvaluateImportance` 通过
- AE1/AE2 的场景分数分布符合预期（long-term 候选 >= 0.8，working 候选 < 0.6）

---

### U3. 衰减引擎与检索重排

**Goal:** 实现艾宾浩斯衰减公式，在 MatchMemories 中插入复合评分重排

**Requirements:** R8, R9, R10, R11

**Dependencies:** U1 (需要新字段存在于模型中)

**Files:**
- Create: `pkg/services/stores/memory_decay.go`
- Modify: `pkg/services/stores/convo_x.go` (MatchMemories)
- Test: `pkg/services/stores/memory_decay_test.go`

**Approach:**
- `memory_decay.go` 提供：
  - `calcRetention(lastAccessedAt time.Time, decayRate float64) float64` — 艾宾浩斯公式
  - `calcReinforce(currentR, factor float64) float64` — 强化公式
  - `tierDecayRate(tier string) float64` — 层级→衰减率映射
  - `forgottenMultiplier(retention, forgetThreshold float64) float64` — 遗忘系数
- 修改 `MatchMemories`：在向量匹配后、取完整记录前，查询匹配记忆的 decay 元数据，计算 composite score，重排序，取 top-K

**Technical design (MatchMemories 修改):**
```
// 现有: ps, err = s.w.Corpus().MatchVectorWith(...)
// 现有: spec.IDs = ps.DocumentIDs()

// 新增: 加载 decay 元数据
type memoryDecayMeta struct {
    ID             oid.OID
    Tier           string
    DecayRate      float64
    LastAccessedAt time.Time
}
// SELECT id, tier, decay_rate, last_accessed_at FROM convo_memory WHERE id IN (ps.DocumentIDs())

// 新增: 计算 composite score 并重排
scored := make([]scoredMatch, len(ps))
for i, m := range ps {
    meta := decayMetaByID[m.DocID]
    R := calcRetention(meta.LastAccessedAt, meta.DecayRate)
    fm := forgottenMultiplier(R, forgetThreshold)
    scored[i] = scoredMatch{
        DocID:      m.DocID,
        FinalScore: float64(m.Similarity) * R * fm,
    }
}
sort.Slice(scored, func(i, j int) bool { return scored[i].FinalScore > scored[j].FinalScore })

// 取 top limit
spec.IDs = topDocIDs(scored, ms.Limit)
```

**Patterns to follow:**
- 现有 `MatchMemories` 的双步查询模式（向量匹配 → 取完整记录）
- `MatchSpec.setDefaults()` 的默认值设置模式

**Test scenarios:**
- Happy path: 两条向量相似度相同的记忆，一条 1h 前的 working，一条 24h 前的 long-term → long-term 排前（Covers AE3）
- Happy path: 遗忘阈值以下记忆的 FinalScore 被 ×0.1 压低（Covers AE5）
- Edge case: 所有匹配记忆均已遗忘 → 仍返回原顺序（不丢失结果）
- Edge case: 匹配记忆数 < limit → 不需要截断
- Edge case: last_accessed_at 为零值（新记忆）→ R=1.0
- Edge case: decay_rate 为 0（数据异常）→ 防御性 fallback 为 1.0，记录 warning

**Verification:**
- `go test -v ./pkg/services/stores/ -run TestMemoryDecay` 通过
- 集成测试中 `memory_recall` 返回结果排序符合 AE3 预期

---

### U4. 写入分层与检索强化

**Goal:** 在记忆写入时自动评估分层，检索命中时触发强化与晋升

**Requirements:** R4, R6, R12, R13

**Dependencies:** U1, U2, U3

**Files:**
- Modify: `pkg/services/stores/convo_x.go` (InvokerForMemoryStore, InvokerForMemoryRecall)

**Approach:**
- `InvokerForMemoryStore`: 在新建记忆路径（非 upsert 更新）中，调用 `EvaluateImportance(content)` → 按阈值分配到 tier → 设置 `decay_rate = tierDecayRate(tier)` → 设置 `last_accessed_at = now()`
- 更新已有记忆时不重新评估重要性，保持 tier/decay_rate 不变
- `InvokerForMemoryRecall`: 在返回结果前，对命中的每条记忆执行 reinforce 和晋升检查，批量 UPDATE
- 晋升阈值：`access_count >= PromoteWorkingToShortTerm`（默认 3）→ working→short-term；`access_count >= PromoteShortTermToLongTerm`（默认 10）→ short-term→long-term

**Execution note:** 强化更新在 recall 结果返回给 LLM 之前同步完成——记忆表写入量极低，不需要异步

**Patterns to follow:**
- 现有 `InvokerForMemoryStore` 的 upsert 逻辑（先 GetMyMemoryWithKey → 存在则更新 / 不存在则创建）
- `afterCreatedMemory` 的钩子模式

**Test scenarios:**
- Happy path: 新建记忆 → tier 和 decay_rate 被正确设置（Covers AE1, AE2）
- Happy path: 更新已有记忆的 content → tier 和 decay_rate 不变
- Happy path: recall 命中一条记忆 → last_accessed_at 更新为 now，下次检索时 R≈1.0（Covers AE4）
- Edge case: recall 命中多条记忆 → 每条独立执行 reinforce
- Edge case: access_count 达到晋升阈值 → tier 提升，decay_rate 更新
- Edge case: long-term 记忆达到晋升阈值 → 保持 long-term（已是最高层）

**Verification:**
- 集成测试：写入 → 验证 tier，多次 recall → 验证 access_count 递增和 tier 晋升
- `go test -v -tags=integration ./pkg/services/stores/ -run TestMemoryLifecycle` 通过

---

### U5. MCP 工具更新与配置

**Goal:** memory_recall 返回 tier，memory_list 支持 tier 过滤，添加可配置参数

**Requirements:** R7, R14, R16, R17, R18

**Dependencies:** U1, U4

**Files:**
- Modify: `pkg/services/stores/convo_x.go` (InvokerForMemoryRecall, InvokerForMemoryList)
- Modify: `pkg/services/tools/defines.go` (memoryListDescriptor, memoryRecallDescriptor)
- Modify: `pkg/settings/config.go` (添加 memory 配置项)

**Approach:**
- `InvokerForMemoryRecall`: 返回结果中每条记忆增加 `"tier"` 字段
- `InvokerForMemoryList`: 支持 `tier` 参数过滤，传入 `spec.Tier` 字段（需在 ConvoMemorySpec 中增加）
- `ConvoMemorySpec`: 增加 `Tier` 字段，在 `convo.yaml` 的 Memory model `specExtras` 中追加（参考现有 `IsFull`/`IsOwner` 模式）：
  ```yaml
  - comment: 按分层过滤
    name: Tier
    type: string
    tags: {form: 'tier', json: 'tier'}
  ```
- `ConvoMemorySpec.Sift`: 新增 `siftMatch(q, "tier", spec.Tier, false)` 过滤逻辑
- `memoryListDescriptor`: InputSchema 增加 `tier` 可选参数
- `memoryRecallDescriptor`: Description 更新，说明返回包含 tier
- `settings.Config`: 添加 `MemoryLongTermThreshold`（默认 0.8）、`MemoryShortTermThreshold`（默认 0.6）、`MemoryForgetThreshold`（默认 0.05）、`MemoryPromoteW2S`（默认 3）、`MemoryPromoteS2L`（默认 10）
- `MemoryReinforcementFactor` 原计划作为强化因子配置，因采用时钟重置策略（重置 `last_accessed_at` 即可使 R≈1.0）不再需要，已移除
- `MemoryDecayRateWorking/ShortTerm/LongTerm` 原计划可配置 decay_rate，当前 `TierDecayRate()` 使用硬编码值（1/7/60），待后续需要调参时再改为可配置
- U4 中的路由逻辑引用 `settings.Current.MemoryLongTermThreshold` 等方法而非硬编码

**Patterns to follow:**
- `pkg/settings/config.go` 中现有 `VectorThreshold`/`VectorLimit` 的 envconfig 模式
- `pkg/services/tools/defines.go` 中现有 tool descriptor 的 InputSchema 结构

**Test scenarios:**
- Happy path: memory_recall 返回结果包含 tier 字段
- Happy path: memory_list 带 tier=long-term 过滤 → 只返回长期记忆
- Happy path: 环境变量 `MEMORY_LONG_TERM_THRESHOLD=0.9` → settings.Current.MemoryLongTermThreshold = 0.9
- Edge case: memory_list 不带 tier 参数 → 返回所有记忆（向后兼容）
- Edge case: memory_store 不传 category → 默认 "custom"，tier 仍自动分配

**Verification:**
- `go build ./...` 编译通过
- 集成测试验证 MCP 工具调用返回格式正确

---

### U6. 向量清理 Bug 修复

**Goal:** DeleteMemory 时同步删除 corpus_vector_400 对应行

**Requirements:** R19

**Dependencies:** None

**Files:**
- Modify: `pkg/services/stores/convo_x.go` (InvokerForMemoryForget)

**Approach:**
- 在 `InvokerForMemoryForget` 中，`DeleteMemory` 之前或之后，查询并删除 `corpus_vector_400` 中 `doc_id = memory.ID` 的行
- 参考现有 `dbAfterDeleteCobDocument` 的向量清理逻辑
- 包装为辅助函数 `deleteMemoryVector(ctx, db, memoryID)`

**Technical design:**
```
// 在 InvokerForMemoryForget 中:
existing, err := s.GetMyMemoryWithKey(ctx, key)
// ... 现有逻辑 ...
// 新增: 清理向量
if _, err := s.w.db.NewDelete().Model((*corpus.DocVector)(nil)).
    Where("doc_id = ?", existing.ID).Exec(ctx); err != nil {
    logger().Infow("delete memory vector fail", "id", existing.ID, "err", err)
}
// 再删除记忆本身
if err := s.DeleteMemory(ctx, existing.StringID()); err != nil { ... }
```

**Patterns to follow:**
- `pkg/services/stores/corpus_gen.go` 中 `dbAfterDeleteCobDocument` 的向量清理

**Test scenarios:**
- Happy path: memory_forget → convo_memory 和 corpus_vector_400 均被删除（Covers AE6）
- Edge case: corpus_vector_400 中不存在对应行（已被手动删除或从未生成）→ 删除记忆本身仍成功
- Edge case: 删除 vector 失败 → 记录日志，仍删除记忆本身（不留孤儿记忆）

**Verification:**
- 集成测试：创建记忆 → memory_forget → 查询 corpus_vector_400 确认无对应行

---

### U7. 集成测试

**Goal:** 端到端验证分层衰减全流程

**Requirements:** R1-R19（全覆盖）

**Dependencies:** U1, U2, U3, U4, U5, U6

**Files:**
- Modify: `pkg/services/stores/integration_test.go`

**Approach:**
- 利用现有 mock embedding 客户端和 `TestMain` 的 `InitDB()` 设置
- 新增 `TestIntegration_MemoryTierDecay` 覆盖完整生命周期：
  - 写入两条内容不同的记忆 → 验证 tier 分配不同
  - 向量匹配（mock 返回固定相似度）→ 验证衰减重排结果
  - 多次 recall 同一条记忆 → 验证 access_count 递增
  - 达到晋升阈值 → 验证 tier 晋升
  - memory_forget → 验证向量清理
- 新增 `TestIntegration_MemoryListByTier` 验证 tier 过滤

**Test scenarios:**
- Happy path: 完整生命周期——写入 → 分层 → recall（衰减重排）→ reinforce → 晋升（Covers F1, F2, F3）
- Happy path: memory_list 按 tier 过滤
- Happy path: memory_forget 清理向量（Covers F1 + AE6）
- Edge case: upsert 更新记忆不改变 tier
- Edge case: 多条记忆混合 tier 的衰减排序正确性

**Verification:**
- `go test -v -tags=integration ./pkg/services/stores/ -run TestIntegration_Memory` 全部通过

---

## System-Wide Impact

- **Interaction graph:** `InvokerForMemoryStore` → 新增 `EvaluateImportance` 调用 → 写入 tier/decay。`InvokerForMemoryRecall` → `MatchMemories`（新增衰减重排）→ reinforce 更新。两条路径共享 `memory_decay.go` 中的衰减函数
- **Error propagation:** 衰减重排失败 → 降级为原始向量排序（不阻断检索）。强化更新失败 → 记录日志，不影响结果返回。重要性评估失败 → 默认 score=0.5（working）
- **State lifecycle risks:** `last_accessed_at` 在写入和强化时更新——确保写入时初始化为 now()，避免零值导致 R=0。tier 晋升和 decay_rate 更新在同一事务中——避免 tier 与 decay_rate 不一致
- **API surface parity:** 知识库检索路径（`MatchDocments`）不受影响——衰减逻辑仅插入 `MatchMemories`。`memory_store` upsert 更新路径不重新评估重要性——保持 LLM 手动更新 content 时不意外改变 tier
- **Integration coverage:** U7 中的集成测试覆盖从写入到 recall 到晋升的完整链路，包括向量清理
- **Unchanged invariants:** `prepareSystemMessage` 的 `ListMemory` 调用和 `PrettyTextForOwner` 格式不变（R15/F4）。MCP 工具名称和核心参数语义不变。`DocMatch`/`DocMatches`/`MatchVectorWith` 接口不变。`corpus_vector_400` 表结构不变

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| codegen 工具链不可用（`../scaffold/` 不存在） | 备选方案：手动添加字段到 `_gen.go` 并创建迁移 SQL，标记为一次性例外 |
| 规则评估对非英文内容准确度不足 | 关键词列表包含中英文混合（偏好/preference、总是/always），AE1/AE2 验证基本场景 |
| 线上数据回填默认值后首次衰减行为异常 | 回填时 `last_accessed_at = now()`，确保初始 R≈1.0；`tier = 'working'` 保守默认 |
| 衰减重排增加一次 DB 查询延迟 | 仅查询 id/tier/decay_rate/last_accessed_at 四列，走主键索引，< 1ms |
| `last_accessed_at` 零值导致 R=0 | 迁移 SQL 设置 `DEFAULT now()`，写入时显式赋值，U3 中零值处理 |

---

## Sources & References

- **Origin document:** [docs/brainstorms/2026-07-04-memory-tier-decay-requirements.md](../brainstorms/2026-07-04-memory-tier-decay-requirements.md)
- Related code: `pkg/services/stores/convo_x.go` (MatchMemories, InvokerForMemory*)
- Related code: `pkg/services/stores/corpus_x.go` (MatchVectorWith, GetEmbedding)
- Related code: `pkg/models/convo/convo_gen.go` (Memory model)
- Related code: `docs/convo.yaml` (codegen spec)
- External reference: PowerMem Ebbinghaus decay formula
