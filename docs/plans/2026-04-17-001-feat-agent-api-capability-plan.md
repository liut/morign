---
title: "feat: Agent API Capability 向量检索"
type: feat
status: completed
date: 2026-04-17
origin: docs/brainstorms/2026-04-10-agent-api-discovery-requirements.md
---

# Agent API Capability 向量检索

## Overview

实现 Agent 项目对各项目 swagger API 的本地同步与向量检索能力，使 LLM 能够根据用户自然语言意图动态匹配相关 API，无需加载完整 swagger 文档。

## Problem Statement

多个后端项目（项目 A 约 272 个 API，项目 C 约 450 个）通过 Bus 项目汇集。用户需要 Agent 根据自然语言意图（如"查订单"）动态发现和调用相关 API，而非一次性加载全部 API 元信息导致 context 膨胀。

## Key Decisions (from origin)

- **不动 Bus**: 所有权限检查由 Bus middleware 执行，Agent 不做预检 (R4)
- **本地向量检索**: 复用现有 embedding 基础设施，按需匹配 (R2)
- **事后权限处理**: 403 时告知用户，而非事前查询 (R4)
- **直接使用 operationId**: operationId 一旦确定稳定不变 (R2)

## Proposed Solution

### Phase 1: 数据模型与存储

**1.1 创建 model 文件**

使用 codegen 生成：

```bash
make codegen MDs=docs/capability.yaml
```

这会在 `pkg/models/capability/` 和 `pkg/services/stores/` 下生成对应的 `*_gen.go` 文件。

在生成的文件基础上进行扩展：

```
pkg/models/capability/
  capability_gen.go    # 自动生成的结构
  capability_x.go      # 扩展方法

pkg/services/stores/
  capability_gen.go    # 自动生成的 Store 接口和基础 CRUD
  capability_x.go      # 扩展方法
```

**api_capability 表结构**（见 origin Data Model）:

| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint | oid.OID (PK) |
| operation_id | varchar | operationId（可为空，公开接口） |
| endpoint | varchar | API 路径，如 `/api/accounts/{id}` |
| method | varchar | GET/POST/PUT/DELETE 等 |
| summary | text | 简短描述 |
| description | text | 详细描述 |
| parameters | jsonb | 参数结构 |
| responses | jsonb | 响应结构 |

**唯一约束**: `method + endpoint`

**api_capability_vector 表**:

| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint | oid.OID (PK) |
| cap_id | bigint | FK to api_capability |
| subject | text | 基于 summary + description 生成 |
| embedding | vector(1024) | 语义向量 |

**1.2 表结构**

表结构通过 codegen 自动生成（bun.AutoMigrate），无需手动迁移文件。

**api_capability 表**：

| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint | oid.OID (PK) |
| operation_id | varchar | operationId（可为空，公开接口） |
| endpoint | varchar | API 路径，如 `/api/accounts/{id}` |
| method | varchar | GET/POST/PUT/DELETE 等 |
| summary | text | 简短描述 |
| description | text | 详细描述 |
| parameters | jsonb | 参数结构 |
| responses | jsonb | 响应结构 |

**唯一约束**: `method + endpoint`

**api_capability_vector 表**：

| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint | oid.OID (PK) |
| cap_id | bigint | FK to api_capability |
| subject | text | 基于 summary + description 生成 |
| embedding | vector(1024) | 语义向量 |

**需创建 stored procedure**:

```sql
CREATE OR REPLACE FUNCTION vector_match_capability_4(query_embedding vector(1024), threshold float4, limit_n int)
RETURNS TABLE(id bigint, cap_id bigint, subject text, similarity float4) AS $$
BEGIN
    RETURN QUERY
    SELECT acv.id, acv.cap_id, acv.subject,
           (acv.embedding <=> query_embedding) AS similarity
    FROM api_capability_vector acv
    WHERE (acv.embedding <=> query_embedding) < threshold
    ORDER BY acv.embedding <=> query_embedding
    LIMIT limit_n;
END;
$$ LANGUAGE plpgsql;
```

### Phase 2: Store 层实现

**2.1 创建 capability store**

使用 codegen 生成后，在生成的文件基础上扩展：

```
pkg/services/stores/
  capability_gen.go    # 自动生成的 Store 接口和基础 CRUD
  capability_x.go      # 导入/导出/向量操作
```

**codegen 配置** `docs/capability.yaml` 中 store 部分：

```yaml
stores:
  - name: capabilityStore
    iname: CapabilityStore
    siname: Capability
    hods:
      - { name: Capability, type: LGCUD }
      - { name: CapabilityVector, type: LGC }
```

**Store 接口设计**:

```go
type CapabilityStore interface {
    CapabilityStoreX

    ListCapability(ctx context.Context, spec *CapabilitySpec) (data Capabilities, total int, err error)
    GetCapability(ctx context.Context, id string) (obj *Capability, err error)
    CreateCapability(ctx context.Context, in CapabilityBasic) (obj *Capability, err error)
    UpdateCapability(ctx context.Context, id string, in CapabilitySet) error
    DeleteCapability(ctx context.Context, id string) error

    GetCapabilityVector(ctx context.Context, id string) (obj *CapabilityVector, err error)
    CreateCapabilityVector(ctx context.Context, in CapabilityVectorBasic) (obj *CapabilityVector, err error)
}

type CapabilityStoreX interface {
    ImportCapabilities(ctx context.Context, r io.Reader, lw io.Writer) error
    ExportCapabilities(ctx context.Context, ea ExportArg) error
    SyncEmbeddingCapabilities(ctx context.Context, spec *CapabilitySpec) error
    MatchCapabilities(ctx context.Context, ms MatchSpec) (data CapabilityMatches, err error)
}
```

**2.2 MatchSpec 扩展**

复用现有的 `stores.MatchSpec`（在 corpus_x.go 中定义），因为语义匹配逻辑相同。

**2.3 向量生成时机**

- 导入时：同步生成向量（`ImportCapabilities` 内调用 `SyncEmbeddingCapabilities`）
- 增量更新：按需调用 `SyncEmbeddingCapabilities`

### Phase 3: CLI 命令

新增命令：

```bash
# 导入 swagger（支持 yaml/json 解析）
./morrigan import-swagger <file_or_dir> [--diff <logfile>]

# 同步向量
./morrigan embedding --target capability [--limit 100]
```

**3.1 Swagger 解析**

输入：swagger.yaml 或 swagger.json 文件
输出：Capability 列表

**Swagger 文档结构**（参考 `scaffold/scripts/sqlgen/swag.go`）:

```go
type swagDoc struct {
    Swagger string `json:"swagger" yaml:"swagger"`
    Info    struct { Title string }
    Paths   paths `json:"paths" yaml:"paths"`
}

type apiEntry struct {
    OperationID string         `json:"operationId" yaml:"operationId"`
    Summary     string         `json:"summary" yaml:"summary"`
    Description string         `json:"description" yaml:"description"`
    Parameters  []any          `json:"parameters" yaml:"parameters"`
    Responses   map[string]any `json:"responses" yaml:"responses"`
    Tags        []string       `json:"tags" yaml:"tags"`
}

func loadDoc(docfile string) (*swagDoc, error) {
    yf, err := os.Open(docfile)
    if err != nil {
        return nil, err
    }
    doc := new(swagDoc)
    err = yaml.NewDecoder(yf).Decode(doc)
    if err != nil {
        return nil, err
    }

    return doc, nil
}

```

解析字段映射：

| swagger 字段 | capability 字段 |
|-------------|-----------------|
| paths.{path}.{method}.operationId | OperationID |
| paths.{path}.{method} | Method + Endpoint |
| paths.{path}.{method}.summary | Summary |
| paths.{path}.{method}.description | Description |
| paths.{path}.{method}.parameters | Parameters (JSON) |
| paths.{path}.{method}.responses | Responses (JSON) |

**冲突处理**: 基于 `method + endpoint` 唯一约束，重复时覆盖。

### Phase 4: Store 注册

**4.1 Wrap 结构扩展**

**不需要手动修订，用 codegen 会自动修订**

```go
// wrap.go
type Wrap struct {
    db *pgx.DB
    corpuStore *corpuStore
    capabilityStore *capabilityStore  // 新增
    // ...
}

// wrap.go NewWithDB
w.capabilityStore = &capabilityStore{w: w}

// stores.go Sgt()
func (s *stores) Capability() CapabilityStore { return s.w.capabilityStore }
```

### Phase 5: Bus 集成

**5.1 HTTP 客户端**

```go
// pkg/services/stores/capability_invoker.go
type CapabilityInvoker struct {
    httpClient *http.Client
    baseURL    string // settings.Current.OAuthPrefix
}

func (inv *CapabilityInvoker) Invoke(ctx context.Context, cap *Capability, params map[string]any) (*http.Response, error) {
    // 构造 URL: baseURL + endpoint
    url := inv.baseURL + cap.Endpoint

    // 根据 method 构造请求
    var body io.Reader
    if cap.Method == "POST" || cap.Method == "PUT" || cap.Method == "PATCH" {
        body = buildBody(params)
    }

    req, err := http.NewRequestWithContext(ctx, cap.Method, url, body)
    if err != nil {
        return nil, err
    }

    // 设置 Header（Content-Type, Authorization 等）
    req.Header.Set("Content-Type", "application/json")

    return inv.httpClient.Do(req)
}
```

**5.2 403 处理**

```go
resp, err := inv.Invoke(ctx, cap, params)
if err != nil {
    return nil, err
}
defer resp.Body.Close()

if resp.StatusCode == 403 {
    return nil, ErrPermissionDenied
}
```

**5.3 MCP Tool 注册**

参考 `pkg/services/tools/registry.go` 的模式：

```go
// pkg/services/tools/defines.go - 定义工具描述
const (
    ToolNameCapabilityMatch = "capability_match" // API 能力匹配
)

var capabilityMatchDescriptor = mcps.ToolDescriptor{
    Name:        ToolNameCapabilityMatch,
    Description: "Match API capabilities by user intent. Returns 3-5 relevant APIs based on semantic similarity.",
    InputSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "intent": map[string]any{
                "type":        "string",
                "description": "User's natural language intent (e.g., '查订单', '创建文章')",
            },
            "limit": map[string]any{
                "type":        "integer",
                "description": "Max results to return (default: 5)",
                "default":     5,
            },
        },
        "required": []string{"intent"},
    },
}
```

**Capability Store Invoker**（在 `capability_x.go` 中实现）:

```go
// pkg/services/stores/capability_x.go
func (s *capabilityStore) InvokerForMatch() mcps.Invoker {
    return func(ctx context.Context, args map[string]any) (map[string]any, error) {
        intent := mcps.StringArg(args, "intent")
        if intent == "" {
            return mcps.BuildToolErrorResult("missing required argument: intent"), nil
        }

        limit, _, _ := mcps.IntArg(args, "limit")
        if limit == 0 {
            limit = 5
        }

        caps, err := s.MatchCapabilities(ctx, stores.MatchSpec{
            Query: intent,
            Limit: limit,
        })
        if err != nil {
            return mcps.BuildToolErrorResult(err.Error()), nil
        }
        if len(caps) == 0 {
            return mcps.BuildToolSuccessResult("No matching APIs found"), nil
        }

        // 返回匹配结果
        result := make([]map[string]any, 0, len(caps))
        for _, cap := range caps {
            result = append(result, map[string]any{
                "id":          cap.StringID(),
                "operation_id": cap.OperationID,
                "endpoint":    cap.Endpoint,
                "method":      cap.Method,
                "summary":     cap.Summary,
                "subject":     cap.Subject,
            })
        }
        return mcps.BuildToolSuccessResult(result), nil
    }
}
```

**注册到 Registry**（在 `initTools` 中）:

```go
// pkg/services/tools/registry.go - initTools()
r.tools = append(r.tools, capabilityMatchDescriptor)
r.invokers[ToolNameCapabilityMatch] = sto.Capability().InvokerForMatch()
```

**capability_invoke Tool**（调用具体 API）:

```go
// pkg/services/tools/defines.go
const (
    ToolNameCapabilityInvoke = "capability_invoke" // API 能力调用
)

var capabilityInvokeDescriptor = mcps.ToolDescriptor{
    Name:        ToolNameCapabilityInvoke,
    Description: "Invoke a specific API capability by ID. Returns the API response from Bus.",
    InputSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "id": map[string]any{
                "type":        "string",
                "description": "Capability ID from capability_match result",
            },
            "params": map[string]any{
                "type":        "object",
                "description": "API parameters (path variables, query params, body, etc.)",
            },
        },
        "required": []string{"id"},
    },
}
```

**CapabilityStore InvokerForInvoke**（在 `capability_x.go` 中实现）:

```go
// pkg/services/stores/capability_x.go
func (s *capabilityStore) InvokerForInvoke(invoker *CapabilityInvoker) mcps.Invoker {
    return func(ctx context.Context, args map[string]any) (map[string]any, error) {
        id := mcps.StringArg(args, "id")
        if id == "" {
            return mcps.BuildToolErrorResult("missing required argument: id"), nil
        }

        cap, err := s.GetCapability(ctx, id)
        if err != nil {
            return mcps.BuildToolErrorResult(err.Error()), nil
        }

        params, _ := args["params"].(map[string]any)
        if params == nil {
            params = make(map[string]any)
        }

        resp, err := invoker.Invoke(ctx, cap, params)
        if err != nil {
            return mcps.BuildToolErrorResult(err.Error()), nil
        }
        defer resp.Body.Close()

        if resp.StatusCode == 403 {
            return mcps.BuildToolErrorResult("Permission denied: no access to this API"), nil
        }

        body, _ := io.ReadAll(resp.Body)
        return mcps.BuildToolSuccessResult(string(body)), nil
    }
}
```

**注册到 Registry**（在 `initTools` 中）:

```go
// pkg/services/tools/registry.go - initTools()
r.tools = append(r.tools, capabilityInvokeDescriptor)
r.invokers[ToolNameCapabilityInvoke] = sto.Capability().InvokerForInvoke(capabilityInvoker)
```

## Technical Considerations

### Bus API 调用

调用 Bus 的目标 URL 格式：
```
settings.Current.OAuthPrefix + endpoint
```

HTTP Method 使用 capability 表中的 method 字段。

### 向量检索 (Stored Procedure)

使用 stored procedure 而非 inline raw SQL，减少参数传递次数：

```go
func (s *capabilityStore) matchVectorWith(ctx context.Context, vec corpus.Vector, threshold float32, limit int) (data CapabilityMatches, err error) {
    if len(vec) != corpus.VectorLen {
        return
    }
    err = s.w.db.NewRaw("SELECT * FROM vector_match_capability_4(?, ?, ?)", vec, threshold, limit).
        Scan(ctx, &data)
    return
}
```

**需创建 stored procedure**:

```sql
CREATE OR REPLACE FUNCTION vector_match_capability_4(query_embedding vector(1024), threshold float4, limit_n int)
RETURNS TABLE(id bigint, cap_id bigint, subject text, similarity float4) AS $$
BEGIN
    RETURN QUERY
    SELECT acv.id, acv.cap_id, acv.subject,
           (acv.embedding <=> query_embedding) AS similarity
    FROM api_capability_vector acv
    WHERE (acv.embedding <=> query_embedding) < threshold
    ORDER BY acv.embedding <=> query_embedding
    LIMIT limit_n;
END;
$$ LANGUAGE plpgsql;
```

### Subject 生成 (增强版)


```go
// models/capable/capab_x.go
func (c *Capability) GetSubject() string {
    // 基础: summary + description
    subject := c.Summary
    if c.Description != "" {
        subject += " " + c.Description
    }
    // 增强: 从 parameters 和 responses 提取关键词（未来扩展）
    // 可通过自定义方法实现，未来可能添加新字段
    return subject
}
```

未来可扩展：parameters/responses 结构解析后提取关键字段加入 subject。

### Swagger 解析库

考虑使用现成的 swagger 解析库，如 `github.com/getkin/kin-openapi` 或 `github.com/go-openapi/spec`。

### 导入格式

支持直接解析 swagger.yaml/json （复用 ImportDocs 逻辑）。

## System-Wide Impact

### Interaction Graph

1. `import-swagger` → 解析 swagger → `UpsertCapability` → `CreateCapabilityVector` → `GetEmbedding`
2. `embedding --target capability` → `SyncEmbeddingCapabilities` → `GetEmbedding` → 更新 `CapabilityVector`
3. 用户请求 → `MatchCapabilities` → SQL 向量匹配 → 返回候选 Capabilities
4. LLM 选择 → `CapabilityInvoker.Invoke` → `OAuthPrefix + endpoint` → Bus → 403/200

### Error & Failure Propagation

- `GetEmbedding` 失败：跳过该条 capability 的向量生成，记录日志
- `CreateCapability` 唯一约束冲突：覆盖已有记录（Upsert 逻辑）
- 向量匹配失败：返回空列表，Agent 降级为关键词匹配

### Integration Test Scenarios

1. 导入包含 500+ API 的 swagger 文件，验证向量生成完整
2. 导入重复 swagger（部分更新），验证覆盖逻辑正确
3. `MatchCapabilities` 查询"查订单"，返回相关 API（不包含无关 API）
4. operation_id 为空的公开 API 也能正常导入和匹配

## Acceptance Criteria

- [ ] `make codegen MDs=docs/capability.yaml` 生成 model 和 store 骨架代码
- [ ] 在生成的 `capability_gen.go` 基础上扩展 `capability_x.go` 实现 `CapabilityStoreX` 接口
- [ ] `import-swagger` 命令能解析 swagger.yaml/json 并导入 capability
- [ ] `embedding --target capability` 能批量生成/更新向量
- [ ] `capability_match` Tool 能返回 3-5 个相关 API
- [ ] `capability_invoke` Tool 能正确调用 Bus API
- [ ] 403 响应能正确转换为用户可理解错误信息
- [ ] operation_id 为空的公开 API 能正常导入
- [ ] 重复导入（相同 method+endpoint）能正确覆盖

## Success Metrics

- 导入 700+ API 的 swagger，耗时 < 30 秒
- 向量生成 700+ 条，耗时 < 2 分钟
- 向量匹配延迟 < 100ms

## Dependencies & Risks

**依赖**:
- PostgreSQL pgvector 扩展
- 现有 `GetEmbedding` 函数
- `settings.Current.OAuthPrefix` 配置

**风险**:
- swagger 解析兼容性（不同项目的 swagger 实现可能有差异）
- 向量匹配 threshold 需要试点调整

## Deferred to Implementation

- [Technical] `embedding --target capability` CLI 参数扩展（在现有 `embedding` 命令中添加 capability 支持）
- [Technical] 公开 API（operation_id 为空）的 Bus 调用处理
- [Technical] 降级策略（零匹配时的 fallback）

## Enhancement Summary

**Deepened on:** 2026-04-17
**Sections enhanced:** 7
**Research agents used:** security-sentinel, performance-oracle, kieran-rails-reviewer, Go patterns researcher

### Key Improvements Discovered

1. **Bug Fix**: `resp.Body.Close()` 在 Invoke 返回 error 时会 panic，需先检查 resp 不为 nil
2. **Input Validation**: `buildBody(params)` 缺少对 capability.parameters schema 的验证
3. **Performance**: Raw SQL 传 vec 参数 4 次，应改用 stored procedure 模式
4. **Codegen**: 使用 `make codegen MDs=docs/mcps.yaml` 生成 model 和 store 骨架代码

## Codegen 参考

Model 和 Store 代码使用现有 codegen 机制生成：

```bash
make codegen MDs=docs/capability.yaml
```

配置参考 `docs/capability.yaml`：
- 生成 `pkg/models/capability/capability_gen.go`
- 生成 `pkg/services/stores/capability_gen.go`
- 在生成的 `*_gen.go` 基础上扩展 `*_x.go`

**注意**：Vector 字段类型使用 `bytes`（对应 `vector(1024)`），具体类型映射参考现有 corpus 模型。

## Input Validation for Params

**Severity: HIGH**

`params map[string]any` 缺少 schema 验证：

```go
func validateParams(params map[string]any, paramSchema jsonb) error {
    // TODO: 实现参数验证逻辑
    // 验证类型、必填、格式等
    return nil
}

func buildBody(params map[string]any) (io.Reader, error) {
    data, err := json.Marshal(params)
    if err != nil {
        return nil, err
    }
    return bytes.NewReader(data), nil
}
```

## Authorization Header 处理

**Needs Clarification**

Plan 未说明如何将 OAuth token 注入到 Bus 请求中。需确认：
- Token 来源：settings 或 session？
- 注入方式：Bearer token 或 Cookie？

## Performance Considerations

### 1. 向量检索：Raw SQL vs Stored Procedure

**问题**: Raw SQL 传 vec 参数 4 次（~16KB 数据），应改用 stored procedure 模式。

```go
// 当前方案（传 4 次 vec）
err = s.w.db.NewRaw(`
    SELECT ... (acv.embedding <=> ?) ...
`, vec, vec, threshold, vec, limit)

// 推荐方案（传 1 次 vec）
err = s.w.db.NewRaw("SELECT * FROM vector_match_capability_4(?, ?, ?)", vec, threshold, limit).
    Scan(ctx, &data)
```

需创建 `vector_match_capability_4()` stored procedure：

```sql
CREATE OR REPLACE FUNCTION vector_match_capability_4(query_embedding vector(1024), threshold float4, limit_n int)
RETURNS TABLE(id bigint, cap_id bigint, subject text, similarity float4) AS $$
BEGIN
    RETURN QUERY
    SELECT acv.id, acv.cap_id, acv.subject,
           (acv.embedding <=> query_embedding) AS similarity
    FROM api_capability_vector acv
    WHERE (acv.embedding <=> query_embedding) < threshold
    ORDER BY acv.embedding <=> query_embedding
    LIMIT limit_n;
END;
$$ LANGUAGE plpgsql;
```

### 2. 批量导入优化

700+ API 在 30 秒内导入需要：

```go
// 批量 upsert（使用多行 INSERT ... ON CONFLICT）
const batchSize = 100
for i := 0; i < len(capabilities); i += batchSize {
    end := min(i+batchSize, len(capabilities))
    batch := capabilities[i:end]
    err = s.bulkUpsertCapabilities(ctx, batch)
}
```

### 3. 数据库索引

迁移文件中需添加 IVFFlat 索引：

```sql
-- 20260417-001-api_capability.up.sql
CREATE INDEX ON api_capability_vector
USING ivfflat (embedding vector_cosine_ops)
WITH (lists = 100);
```

## Bug Fixes Required

### 1. resp.Body.Close() Panic Bug

```go
// 错误：Invoke 返回 error 时 resp 为 nil，defer 会 panic
resp, err := inv.Invoke(ctx, cap, params)
if err != nil {
    return nil, err
}
defer resp.Body.Close()  // BUG: 如果 resp 是 nil 则 panic

// 正确：先检查 resp 不为 nil
resp, err := inv.Invoke(ctx, cap, params)
if err != nil {
    return nil, err
}
if resp != nil {
    defer resp.Body.Close()
}
```

### 2. GetEmbedding 错误处理

Plan 说明 "跳过该条" 但代码实现在 `SyncEmbeddingDocments` 中是立即返回错误：

```go
// 当前行为（失败整个 batch）
vec, err := GetEmbedding(ctx, subject)
if err != nil {
    return err  // 失败整个流程
}

// Capability 应该的行为（跳过并继续）
vec, err := GetEmbedding(ctx, subject)
if err != nil {
    logger().Warnw("skip capability due to embedding fail", "id", cap.ID, "err", err)
    continue  // 跳过这条，继续处理下一条
}
```

### 3. Embedding API 批处理 Bug

**潜在问题**: `openai.go` 的 `Embedding()` 可能只返回第一个结果，批处理失效。实现时需验证：

```go
// 验证 Embedding 返回所有结果
embeddings, err := llmEm.Embedding(ctx, texts)  // texts 是 []string
if len(embeddings) != len(texts) {
    return nil, fmt.Errorf("embedding count mismatch: got %d, want %d", len(embeddings), len(texts))
}
```

## Technical Decisions

### 1. 向量匹配 Stored Procedure 命名

使用 `vector_match_capability_4()` 而非内联 SQL，以：
- 减少参数传递（1 次 vec vs 4 次）
- 与现有 `vector_match_docs_4()` 保持一致

### 2. 错误处理策略

- **导入阶段**: 单条失败跳过并记录，不中断整个流程
- **匹配阶段**: 零匹配返回空列表，Agent 降级为关键词匹配
- **调用阶段**: 403 转换为用户可理解错误

## Sources & References

- **Origin document:** [docs/brainstorms/2026-04-10-agent-api-discovery-requirements.md](docs/brainstorms/2026-04-10-agent-api-discovery-requirements.md)
  - Key decisions: 本地向量检索、复用 GetEmbedding、不动 Bus、事后权限处理
- **Codegen config:** `docs/capability.yaml` - 使用 `make codegen MDs=docs/capability.yaml` 生成代码
- Corpus model pattern: `pkg/models/corpus/corpus_gen.go:19-50`
- Store pattern: `pkg/services/stores/corpus_x.go:74-83`
- CLI pattern: `main.go:36-62`
- Vector matching pattern: `pkg/services/stores/corpus_x.go:258-280`
- OpenAPI parsing: `github.com/getkin/kin-openapi`
- pgvector docs: PostgreSQL pgvector extension`
