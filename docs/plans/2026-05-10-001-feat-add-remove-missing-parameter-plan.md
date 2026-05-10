---
title: feat: Add --mark-missing parameter for capability batch import
type: feat
status: completed
date: 2026-05-10
origin: docs/brainstorms/2026-05-10-capability-batch-import-delete-missing-requirements.md
---

# feat: Add --remove-missing parameter for capability batch import

## Overview

在 capability 批量导入时增加 `--remove-missing=<path-prefix>` 参数，导入后自动删除满足前缀条件但不在导入文件中的 capability。

## Problem Statement

当前 `import-swagger` 命令是纯增量/更新模式，只处理新文件中的 API。如果某个接口在旧文件中存在但在新文件中已被移除，系统不会检测到并删除该接口。用户需要一种方式清理已过期的 capability。

## Proposed Solution

### Recommended: Direct Deletion (Immediate)

实现简单，适合熟悉 Swagger 文件内容的用户，风险可控（通过前缀前缀匹配、空前缀检查等保护）。

**优点**:
- 实现简单，代码量少
- 一条命令完成所有操作
- 通过 `--remove-missing=""` 空值保护避免误删

**缺点**:
- 不可逆，操作立即生效
- 用户无法预览将被删除的内容

### Alternative A: Soft Delete with Pending Confirmation

不直接删除，而是将待删除的 capability 标记为 `pending_deletion` 状态，通过单独的命令或定时任务确认后才真正删除。

**实现思路**:
1. 添加 `Status` 字段到 `CapabilityBasic`（默认值 `active`）
2. `--remove-missing` 时将待删除项的 `Status` 设为 `pending_deletion`
3. 新增 `confirm-delete` 命令，确认后真正删除

**优点**:
- 可逆，用户可以查看待删除列表并选择恢复
- 支持批量操作前的二次确认

**缺点**:
- 需要修改数据库模型（添加 `Status` 字段）
- 实现复杂度增加约 2 倍
- 需要定期清理 pending 状态数据

### Alternative B: Dry-Run Preview First

分两步执行：第一步预览（只读），第二步才执行删除。

**实现思路**:
1. `--remove-missing` 只做预览，输出将被删除的 capability 列表（不实际删除）
2. 输出中包含确认 token
3. 用户再次执行 `import-swagger --confirm=<token> --remove-missing=...` 才真正删除

**优点**:
- 用户可以完整预览删除内容
- 确认机制防止误操作

**缺点**:
- 需要两次命令执行
- 需要实现 token 生成和验证逻辑
- 实现复杂度中等

### Alternative C: Export Deletion List

不删除，而是导出待删除列表到文件，用户查看后自行决定如何处理。

**实现思路**:
1. `--remove-missing` 时将待删除项输出到指定文件（如 `--delete-list=/tmp/to-delete.json`）
2. 输出内容包含 method、endpoint、capability_id 等信息
3. 用户查看后可以手动删除或执行清理命令

**优点**:
- 最安全，完全可控
- 用户可以编辑删除列表

**缺点**:
- 需要额外的步骤
- 不适合自动化场景

### Recommended Approach

**推荐直接删除方案（Phase 1）**，原因：
1. 实现复杂度最低，风险可控
2. 通过 `--remove-missing=""` 空值检查保护
3. 可在后续迭代中增加 Alternative A/B/C 作为增强选项

## Decision

- **采用软删除方案**：利用 `MetaField` 中的 `meta` 字段，添加 `missed: true` 标记
- 不需要修改数据库模型，利用现有的 meta 扩展字段机制
- 通过单独的命令 `cleanup-missed` 清理被标记的 capability

## Phase 1: CLI 参数扩展

**文件**: `main.go`

1. 在 `import-swagger` 命令的 Flags 中增加:
```go
&cli.StringFlag{
    Name:    "mark-missing",
    Usage:   "mark capabilities whose endpoint matches prefix but not in imported file as missed",
},
```

2. 修改 `importSwagger` 函数，解析 `--mark-missing` 参数并传递给 `ImportCapabilities`

### Phase 2: ImportCapabilities 函数扩展

**文件**: `pkg/services/stores/capability_x.go`

1. 修改函数签名，添加可选参数:
```go
func (s *capabilityStore) ImportCapabilities(ctx context.Context, r io.Reader, lw io.Writer, markMissingPrefix string) error
```

2. 第一阶段：导入/更新（原有逻辑），同时收集所有成功处理的 `(method, endpoint)` 组合到集合

3. 第二阶段（当 `markMissingPrefix != ""` 时）:
   - 查询所有 endpoint 以该前缀开头的 capability
   - 对于每个 capability，检查其 `(method, endpoint)` 是否在已导入集合中
   - 不存在则调用 `UpdateCapability` 更新 `meta["missed"] = "true"`
   - 输出标记日志

### Phase 3: 安全保护

1. 空字符串前缀：不执行标记
2. 前缀必须以 `/` 开头（标准化）

## Technical Considerations

### 软删除机制

利用 `CapabilityBasic` 的 `MetaAddKVs` 方法标记 `missed` 状态：

```go
// CapabilitySet 更新时用法
set := capability.CapabilitySet{}
set.MetaAddKVs("missed", "true")

// CapabilityBasic 创建时用法
basic := capability.CapabilityBasic{}
basic.MetaAddKVs("missed", "true")
```

当实体更新时，`MetaDiff` 会通过 `MetaUp` 方法应用到 `MetaField.Meta` 上。

### 查询 missed 状态

使用 bun 的 JSON 查询（PostgreSQL）：

```go
err := db.NewSelect().Model((*capability.Capability)(nil)).
    Where("meta->>'missed' = ?", "true").
    Scan(ctx, &caps)
```

cleanup-missed 命令时查询 `meta->>'missed' = 'true'` 的 capability。

### 向量数据清理

删除 capability 时，`DeleteCapability` 已通过 `dbBeforeDeleteCapability` 清理关联的 `CapabilityVector`。

### 日志格式

```go
// 标记为 missed
fmt.Fprintf(lw, "%s %s [missed]\n", method, path)
// cleanup 删除
fmt.Fprintf(lw, "%s %s [deleted]\n", method, path)
```

## System-Wide Impact

### Interaction Graph

```
importSwagger (main.go)
  └─> ImportCapabilities (capability_x.go)
        ├─> decodeSwaggerDoc
        ├─> GetCapabilityWith (查询已存在)
        ├─> CreateCapability / UpdateCapability
        │     └─> afterCreatedCapability / afterUpdatedCapability (向量创建/更新)
        └─> MarkMissed: UpdateCapability with meta["missed"]="true" (当 remove-missing 触发)

cleanupMissed (main.go) [新增]
  └─> ListCapabilities (查询 missed=true)
        └─> DeleteCapability (逐条删除)
              └─> dbBeforeDeleteCapability (向量清理)
```

### Error Propagation

- 导入阶段失败：整个操作失败，不进入标记阶段
- 标记阶段单条失败：记录错误日志，继续处理其他 capability
- cleanup 阶段失败：继续处理其他 capability，通过错误日志报告

### State Lifecycle Risks

- **部分删除风险**：删除 N 条后发生错误，导致不完整的删除状态
- **缓解**：删除操作是幂等的，重复导入会重新创建缺失的 capability

## Acceptance Criteria

- [ ] `import-swagger --remove-missing=/api/v1/ swagger.json` 正确删除 `/api/v1/xxx` 形式但不在 swagger.json 中的 capability
- [ ] 不提供 `--remove-missing` 时行为与原来完全一致
- [ ] 删除操作有清晰的日志输出（格式：`METHOD /path [deleted]`）
- [ ] 向量数据同步清理
- [ ] 空前缀不执行删除
- [ ] 连续执行两次结果一致（幂等）
- [ ] `make vet lint` 通过

## Implementation Phases

### Phase 1: CLI 参数 + 软删除标记

#### 1.1 main.go - 添加 CLI 参数

```go
&cli.StringFlag{
    Name:    "remove-missing",
    Usage:   "mark capabilities whose endpoint matches prefix but not in imported file as missed",
},
```

#### 1.2 capability_x.go - ImportCapabilities 修改

1. 修改函数签名添加 `removeMissingPrefix` 参数
2. 收集已导入的 `(method, endpoint)` 集合
3. 当 `removeMissingPrefix != ""` 时：
   - 查询所有 endpoint 以该前缀开头的 capability
   - 对于不在已导入集合中的 capability，调用 `UpdateCapability` 更新 `meta["missed"] = "true"`
   - 使用 `CapabilitySet.MetaAddKVs("missed", "true")` 设置标记
   - 输出标记日志 `[missed]`

#### 1.3 新增 cleanup-missed 命令

```go
{
    Name:   "cleanup-missed",
    Usage:  "delete capabilities marked as missed",
    Action: cleanupMissed,
    Flags: []cli.Flag{
        &cli.StringFlag{Name: "prefix", Usage: "only cleanup capabilities with given prefix"},
        &cli.BoolFlag{Name: "dry-run", Usage: "preview only, do not actually delete"},
    },
}
```

#### 1.4 实现 cleanupMissed 函数

- 查询 `meta->>'missed' = 'true'` 的 capability
- 根据 `--prefix` 过滤（可选）
- `--dry-run` 只输出不删除
- 删除时通过 `dbBeforeDeleteCapability` 清理向量数据

### Phase 2: 测试

- [ ] 手动测试基本流程
- [ ] 验证 `make vet lint` 通过

## Acceptance Criteria

- [ ] `import-swagger --remove-missing=/api/v1/ swagger.json` 正确标记 `/api/v1/xxx` 形式但不在 swagger.json 中的 capability 为 `missed: true`
- [ ] 不提供 `--remove-missing` 时行为与原来完全一致
- [ ] 标记操作有清晰的日志输出
- [ ] `cleanup-missed --prefix=/api/v1/` 可以删除被标记的 capability
- [ ] `cleanup-missed --dry-run` 可以预览将要删除的内容
- [ ] 向量数据在 cleanup 时同步清理
- [ ] 空前缀不执行标记
- [ ] `make vet lint` 通过

## Dependencies & Risks

| 依赖 | 类型 | 说明 |
|------|------|------|
| CapabilitySet.MetaAddKVs | 代码复用 | 设置 `meta["missed"] = "true"` 的正确方法 |
| DeleteCapability | 代码复用 | 已实现的删除方法，含向量清理 |
| dbBeforeDeleteCapability | 代码复用 | 向量清理 hook |
| UpdateCapability | 代码复用 | 用于更新 meta 字段 |

| 风险 | 等级 | 缓解 |
|------|------|------|
| 空前缀标记所有 capability | Critical | 代码中检查空字符串，直接返回错误 |
| missed 标记后长期未清理 | Low | 日志提示用户执行 cleanup-missed |

## Sources & References

### Origin

- **Origin document:** [docs/brainstorms/2026-05-10-capability-batch-import-delete-missing-requirements.md](docs/brainstorms/2026-05-10-capability-batch-import-delete-missing-requirements.md)
- **Key decisions carried forward:**
  - 合并 `--path-prefix` + `--remove-missing` 为单一参数 `--remove-missing=<prefix>`
  - 前缀匹配使用 endpoint 前缀匹配（非精确匹配）
  - 可选参数，不提供时保持原有行为

### Internal References

- `main.go:71-103` - importSwagger 函数
- `main.go:312-320` - CLI 命令注册
- `pkg/services/stores/capability_x.go:248-345` - ImportCapabilities 主体
- `pkg/services/stores/capability_gen.go:112-124` - DeleteCapability
- `pkg/services/stores/capability_x.go:145-149` - dbBeforeDeleteCapability

### External References

- Go CLI 框架: github.com/urfave/cli/v3
