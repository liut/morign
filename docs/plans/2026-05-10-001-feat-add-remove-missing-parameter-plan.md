---
title: feat: Add --mark-missing parameter for capability batch import
type: feat
status: completed
date: 2026-05-10
origin: docs/brainstorms/2026-05-10-capability-batch-import-delete-missing-requirements.md
---

# feat: Add --mark-missing parameter for capability batch import

## Overview

在 capability 批量导入时增加 `--mark-missing=<path-prefix>` 参数，导入后将满足前缀条件但不在导入文件中的 capability 标记为 `missed`。

## Problem Statement

当前 `import-swagger` 命令是纯增量/更新模式，只处理新文件中的 API。如果某个接口在旧文件中存在但在新文件中已被移除，系统不会检测到。

## Solution

1. `import-swagger --mark-missing=PREFIX`：标记已删除的 capability
2. `cleanup-missed [--prefix=PREFIX] [--dry-run]`：删除或预览被标记的 capability
3. 日志输出包含 `missed` 数量

使用 `MetaAddKVs("missed", "true")` 标记，无需修改数据库模型。

## Implementation

### Files

- `main.go`: 添加 `--mark-missing` flag 和 `cleanup-missed` 命令
- `pkg/services/stores/capability_x.go`:
  - `ImportCapabilities` 增加 `markMissingPrefix` 参数
  - `markMissingCapabilities`: 标记 missed 能力
  - `CleanupMissedCapabilities`: 清理被标记的能力

### Interface Changes

```go
type CapabilityStoreX interface {
    ImportCapabilities(ctx context.Context, r io.Reader, lw io.Writer, markMissingPrefix string) error
    CleanupMissedCapabilities(ctx context.Context, lw io.Writer, prefix string, dryRun bool) error
    // ...
}
```

## Acceptance Criteria

- [x] `import-swagger --mark-missing=/api/v1/` 正确标记 `/api/v1/xxx` 形式但不在 swagger.json 中的 capability
- [x] 不提供 `--mark-missing` 时行为与原来完全一致
- [x] 标记操作有清晰的日志输出 `[missed]`
- [x] `cleanup-missed --dry-run` 预览将要删除的内容
- [x] `cleanup-missed` 删除被标记的 capability
- [x] 向量数据在 cleanup 时同步清理
- [x] 日志包含 missed 数量统计
