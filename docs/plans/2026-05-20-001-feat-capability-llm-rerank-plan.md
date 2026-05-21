---
title: feat: Add LLM re-rank to capability matching
type: feat
status: completed
date: 2026-05-20
implemented: 2026-05-21
origin: docs/brainstorms/2026-05-20-capability-rerank-requirements.md
---

# feat: Add LLM re-rank to capability matching

## Summary

在 capability 向量搜索之后增加 LLM 重排步骤：宽召回 15 个候选（可配置），用一次专门的 LLM Chat 调用评估每个候选与用户原始意图的相关性，过滤无关 API，按相关度排序，返回高质量 Top-N。重排逻辑内置在 InvokerForMatch 中，对主 LLM 透明；LLM 调用失败时降级返回原始向量搜索结果。

---

## Problem Frame

当前 capability 搜索依赖纯向量相似度，同一标签下的 API（如 "集团员工动态分析" 和 "手动发送消息"）在向量空间中难以可靠区分。用户提出宽泛问题时，搜索结果混入大量无关 API，导致主 LLM 可能错误调用无关接口或拒绝回答。

详见 origin document。

---

## Requirements

- R1. 向量搜索候选数量从 6 提升到可配置值（默认 15）
- R2. 召回数量通过配置项控制，不与返回数量绑定
- R3. 向量搜索后，将候选列表和用户原始查询发送给重排 LLM
- R4. 重排 LLM 逐条判断候选相关性，输出保留/排除及排序
- R5. 不相关候选从结果中移除，剩余按相关度降序
- R6. 最终返回数量不超过调用方请求的 limit（默认 6）
- R7. 重排在 InvokerForMatch 内部执行，对主 LLM 透明
- R8. 候选数量 ≤ 返回 limit 时跳过重排
- R9. 重排 LLM 失败或超时时，降级返回原始向量搜索结果
- R10. 重排 LLM 返回格式异常时，降级返回原始结果
- R11. 相同查询的重排结果可缓存（TTL 默认 5 分钟）
- R12. 缓存键基于原始查询文本

**Origin acceptance examples:** AE1 (分析人员构成 → 过滤 sendmsg/export), AE2 (查订单 → 全部保留), AE3 (LLM 故障 → 降级), AE4 (缓存命中)

---

## Scope Boundaries

- 意图拆解（方案 A）不在本次范围
- 能力画像预计算（方案 C）不在本次范围
- GetSubject() 改造不在本次范围
- 新数据库字段不在本次范围

### Deferred to Follow-Up Work

- 重排 prompt 的持续调优 — 上线后根据实际查询效果迭代
- 基于用户反馈的重排质量监控 — 后续迭代

---

## Context & Research

### Relevant Code and Patterns

- `pkg/services/stores/capability_x.go:401` — InvokerForMatch，重排插入点
- `pkg/services/stores/capability_x.go:186` — MatchCapabilities，向量搜索入口
- `pkg/services/stores/corpus_x.go:35` — MatchSpec 定义和 setDefaults
- `pkg/services/stores/llm.go:73` — GetSummary，最接近的 "LLM 做判断" 模式
- `pkg/services/llm/client.go:16` — Client 接口，Chat 方法用于重排调用
- `pkg/services/llm/options.go:132` — 默认 LLM 配置（provider/model/temperature/timeout）
- `pkg/services/stores/rc.go` — Redis 单例 SgtRC()，Set/Get 模式
- `pkg/settings/config.go` — envconfig 配置模式，Provider 嵌套结构
- `pkg/models/aigc/match.go:9` — MatchResult 类型（DocID, Subject, Similarity）

### Institutional Learnings

无相关机构知识 — 这是代码库中首次实现 LLM 重排。

---

## Key Technical Decisions

- **重排使用 Chat 而非 Generate**：Chat 支持 system prompt + user message 结构，更适合 "给定候选列表，输出结构化判断" 的任务；Generate 是纯文本补全，对 JSON 输出控制力弱
- **专用 Rerank Provider 配置**：独立于 Summarize/Embedding，允许为重排选择不同的模型（更便宜/更快），且不耦合到摘要或嵌入的配置
- **温度设为 0**：重排需要确定性输出，不需要创造性
- **降级策略：返回原始结果而非空结果**：宁可多返回噪音也不丢失信号，主 LLM 尚能自行判断一部分
- **重排作为 capabilityStore 的独立方法**：可单独测试，不嵌入 InvokerForMatch 闭包内部

---

## Open Questions

### Resolved During Planning

- 重排 LLM 客户端配置方式 → 新增 Rerank Provider 字段，与现有 Embedding/Summarize 模式一致
- 候选信息传给重排 LLM 时包含哪些字段 → method, endpoint, summary（parameters 太长且对判断帮助有限，不包含）

### Deferred to Implementation

- [Affects R3] 重排 prompt 的最终措辞 — 需要在实际模型上测试 JSON 输出稳定性
- [Affects R11] 缓存 key 是否需要包含候选 ID 集合 — 取决于候选是否因数据变更而不同
- [Needs research] 单次重排调用的候选数量上限 — 与模型上下文窗口相关，需实测

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification.*

### Re-rank flow

```
InvokerForMatch(intent, limit)
  │
  ├─ MatchCapabilities(Query=intent, Limit=recallLimit, SkipKeywords=true)
  │   └─ GetEmbedding(intent) → vector_match_capability_4() → candidates (15-20)
  │
  ├─ [skip if len(candidates) ≤ limit]
  │
  ├─ checkCache(intent)
  │   ├─ hit → return cached result
  │   └─ miss ↓
  │
  ├─ rerankWithLLM(intent, candidates)
  │   ├─ build prompt (system + user message with candidates)
  │   ├─ Chat(ctx, messages, nil) → JSON response
  │   ├─ parse JSON → {relevant: [...], irrelevant: [...]}
  │   ├─ filter & reorder candidates
  │   └─ [on any error] → return original candidates
  │
  ├─ storeCache(intent, result)
  │
  └─ truncate to limit → build tool result JSON
```

### Re-rank prompt shape

```
System: You are an API relevance evaluator. Given a user's intent and a list of candidate APIs, judge whether each API is relevant. An API is relevant if calling it would help answer or fulfill the user's intent. An API is irrelevant if it does something unrelated, even if keywords overlap.

Output only valid JSON, no other text.

User:
Evaluate each candidate for the intent: "{query}"

Candidates:
1. [GET] /api/a1/hr/staff/analysis - 集团员工动态分析
2. [GET] /api/a1/hr/staff/sendmsg - 手动发送消息
...

Return JSON:
{
  "relevant": [{"index": 1, "reason": "directly provides staff analysis data"}],
  "irrelevant": [{"index": 2, "reason": "sends messages, unrelated to analysis"}]
}
```

---

## Implementation Units

### U1. Add re-rank configuration

**Goal:** 在 Config 中增加重排相关的配置项

**Requirements:** R1, R2, R11

**Dependencies:** None

**Files:**
- Modify: `pkg/settings/config.go`

**Approach:**
- 新增 `RerankEnabled bool` (env: `RERANK_ENABLED`, default: `false`)，上线后改为 `true`
- 新增 `RerankRecallLimit int` (env: `RERANK_RECALL_LIMIT`, default: `15`)，控制宽召回数量
- 新增 `RerankCacheTTL int` (env: `RERANK_CACHE_TTL`, default: `300`)，缓存 TTL（秒）
- 新增 `Rerank Provider` (env prefix: `RERANK_`)，重排专用 LLM 配置

**Patterns to follow:**
- `pkg/settings/config.go` 中 VectorThreshold/VectorLimit 的 envconfig 模式
- Provider 嵌套结构参考 Embedding/Summarize 字段

**Test scenarios:**
- 配置默认值验证 — RerankEnabled=false, RecallLimit=15, CacheTTL=300
- 环境变量覆盖默认值

**Verification:**
- `settings.Current.RerankEnabled` 等字段可正常读取
- 环境变量 `RERANK_ENABLED=true` 可覆盖默认值

---

### U2. Implement re-rank core logic

**Goal:** 实现核心重排函数：构建 prompt、调用 LLM Chat、解析 JSON 响应、过滤并排序候选

**Requirements:** R3, R4, R5, R9, R10

**Dependencies:** U1

**Files:**
- Modify: `pkg/services/stores/capability_x.go`
- Modify: `pkg/services/stores/llm.go`

**Approach:**
- 在 `capabilityStore` 上新增 `rerankCapabilities(ctx, query, candidates) (Capabilities, error)` 方法
- 构建 messages：system prompt（角色定义 + 输出格式约束）+ user message（intent + 编号候选列表）
- 候选列表每项包含：序号、method、endpoint、summary
- 初始化 Rerank LLM 客户端（`GetLLMRerankClient()`），温度 0，开启 JSON 模式（如有）
- 调用 `Chat(ctx, messages, nil)`，解析返回的 JSON 中的 `relevant` 数组
- 按 `relevant[].index` 从原始 candidates 中提取并重排
- 任何错误（网络、超时、JSON 解析失败、空结果）返回 (nil, error)，调用方（InvokerForMatch）使用其持有的原始 candidates 执行降级
- 在 `stores/llm.go` 中新增 `GetLLMRerankClient()`，参考 `GetLLMSummarizeClient()` 模式

**Patterns to follow:**
- `GetSummary()` at `stores/llm.go:73` — 错误处理和日志模式（注意：GetSummary 使用 Generate()，重排使用 Chat()，仅错误处理模式可复用）
- `llm.NewClient()` with functional options at `stores/llm.go:48` — 客户端初始化
- logger().Infow() 用于操作日志，logger().Warnw() 用于异常

**Test scenarios:**
- Happy path: 5 个候选（2 相关 + 3 无关），重排后返回 2 个，按相关度排序
- Happy path: 所有候选都相关，全部保留，仅调整排序
- Happy path: 所有候选都无关，返回空列表
- Edge case: 候选列表为空，直接返回空
- Edge case: 候选只有 1 个，跳过重排或直接返回
- Error path: LLM 返回非 JSON 文本，降级返回原始候选
- Error path: LLM 调用超时，降级返回原始候选
- Error path: JSON 中 index 超出候选范围，忽略该条

**Verification:**
- 单元测试覆盖上述所有场景
- 重排函数不修改输入的 candidates 切片

---

### U3. Integrate re-rank into InvokerForMatch

**Goal:** 将重排接入 capability_match 工具的执行流程

**Requirements:** R1, R2, R6, R7, R8, R9

**Dependencies:** U2

**Files:**
- Modify: `pkg/services/stores/capability_x.go`

**Approach:**
- 在 `InvokerForMatch` 闭包中，`MatchCapabilities` 调用时使用 `RerankRecallLimit` 作为 Limit
- 匹配结果返回后，判断 `len(candidates) > 请求的 limit` 且 `RerankEnabled` 为 true
- 满足条件时调用 `rerankCapabilities()`，失败时 logger().Infow() 记录降级
- 截断至请求的 limit 返回
- `SkipKeywords` 保持 true（无需 LLM 提取关键词，重排 LLM 会理解原始意图）
- 构建 tool result 的逻辑不变（已有代码 `capability_x.go:428-441`）

**Patterns to follow:**
- `InvokerForMatch` 现有的错误处理模式 — 错误通过 `BuildToolErrorResult` 返回

**Test scenarios:**
- Integration: RerankEnabled=true，宽泛查询 "分析人员构成"，验证 sendmsg/export 被过滤
- Integration: RerankEnabled=false，行为与现有完全一致
- Integration: 候选数 ≤ limit，跳过重排，不调用 LLM
- Error path: 重排失败，返回原始宽召回结果（不中断请求），日志记录降级

**Verification:**
- 手动测试：开启重排后 "分析人员构成" 的匹配结果不含 sendmsg 和 export API
- 关闭重排时行为与改动前一致

---

### U4. Add Redis caching for re-rank results

**Goal:** 对相同查询的重排结果做短期缓存，减少重复 LLM 调用

**Requirements:** R11, R12

**Dependencies:** U2 (可与 U3 并行)

**Files:**
- Modify: `pkg/services/stores/capability_x.go`

**Approach:**
- 缓存 key：`rerank:<hash(query)>`，对 query 做 SHA256 取前 16 位
- 缓存值：重排后的候选 ID 列表（JSON 序列化）
- TTL：`RerankCacheTTL` 秒
- 在 `rerankCapabilities` 内：先查缓存，命中则按缓存的 ID 顺序从 candidates 中重建结果
- 重排成功后写入缓存
- 缓存读写失败不影响主流程，logger().Infow() 记录

**Patterns to follow:**
- `SgtRC().Set()/Get()` at `pkg/services/stores/state.go` — Redis 操作模式
- 缓存穿透保护：空结果也缓存（短 TTL）

**Test scenarios:**
- Happy path: 首次查询触发 LLM 重排并写缓存，相同查询第二次命中缓存
- Happy path: 缓存过期后重新触发 LLM 重排
- Edge case: 候选集合变化（新增/删除 API），缓存仍按 ID 重建——不存在的 ID 静默跳过
- Error path: Redis 不可用，跳过缓存直接调用 LLM，不影响主流程

**Verification:**
- 单元测试 mock Redis 客户端
- 集成测试验证端到端缓存行为

---

### U5. End-to-end tests

**Goal:** 补齐集成测试，验证完整的 InvokerForMatch → re-rank → result 链路

**Requirements:** R7, R8, R9, AE1-AE4

**Dependencies:** U3, U4

**Files:**
- Create: `pkg/services/stores/capability_rerank_test.go`

**Approach:**
- Mock LLM 客户端，返回预定义的 JSON 响应
- Mock Redis 客户端
- 构造测试用的 capability 数据集
- 覆盖 Acceptance Examples AE1-AE4

**Test scenarios:**
- Covers AE1. 宽泛查询过滤无关 API：输入 "分析公司人员构成"，候选含 analysis/board/sendmsg/export，重排后仅保留 analysis/board
- Covers AE2. 精确查询全部保留：输入 "查询我的订单"，候选均与订单相关，重排后全部保留并排序
- Covers AE3. 重排 LLM 故障降级：LLM 返回错误，降级返回原始 Top-6 向量搜索结果
- Covers AE4. 缓存命中：首次查询写缓存，二次查询命中缓存，LLM 调用次数为 1
- 候选 ≤ limit 时跳过重排：limit=6，仅匹配到 4 个候选，不调用重排 LLM

**Verification:**
- `make test-stores` 全部通过

---

## System-Wide Impact

- **Interaction graph:** 仅影响 `InvokerForMatch` → `MatchCapabilities` → `MatchVectorWith` 链路，不改变 tool executor、registry 或 LLM 交互循环
- **Error propagation:** 重排失败不向上传播，在 InvokerForMatch 内部降级为原始结果
- **State lifecycle risks:** 缓存基于 query hash，不涉及 capability 数据变更的 invalidation（短期 TTL 容忍短暂不一致）
- **Unchanged invariants:** capability_match 工具的输入输出 contract 不变（intent + limit → result array）；MatchSpec / MatchCapabilities 签名不变；向量搜索逻辑不变

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| 重排 LLM JSON 输出不稳定（多输出文字、格式错误） | prompt 强调 "output only JSON"；解析失败时降级返回原始结果 |
| 重排增加延迟影响用户体验 | 使用快速/便宜模型；缓存减少重复调用；RerankEnabled 开关可紧急关闭 |
| 重排 prompt 设计不当导致系统性误杀某类 API | prompt 作为配置项可热更新（后续迭代）；上线初期监控重排过滤率 |
| 候选数量 15-20 超出重排 LLM 上下文窗口 | Deferred to Implementation 实测；必要时减少候选数或分批评估 |

---

## Sources & References

- **Origin document:** [docs/brainstorms/2026-05-20-capability-rerank-requirements.md](../brainstorms/2026-05-20-capability-rerank-requirements.md)
- Related code: `pkg/services/stores/capability_x.go` (InvokerForMatch, MatchCapabilities)
- Related code: `pkg/services/stores/llm.go` (LLM client initialization, GetSummary)
- Related code: `pkg/services/llm/client.go` (Client interface)
- Related code: `pkg/settings/config.go` (Configuration)
- Related code: `pkg/services/stores/rc.go` (Redis singleton)

---

## Implementation Notes

- **xxhash 替换 SHA256**：缓存 key 改用 `github.com/cespare/xxhash/v2`，比 SHA256 更轻量，输出 64-bit hex 足够避免碰撞
- **Provider 扩展**：新增 `Temperature` 和 `TimeoutSeconds` 字段，`NewLLMClient` 条件性应用。重排 client 通过 `initRerankClient()` 独立创建，硬编码 `WithTemperature(0)`
- **空结果缓存**：短 TTL 60s（vs 正常 300s），硬编码在 `rerankCacheSet` 中
- **测试策略**：测试通过直接替换 package-level `llmRe` 变量注入 mock client，未使用接口抽象
