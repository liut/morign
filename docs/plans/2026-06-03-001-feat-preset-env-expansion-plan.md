---
date: 2026-06-03
type: feat
topic: preset-env-expansion
status: active
origin: docs/brainstorms/2026-06-03-preset-env-expansion-requirements.md
---

# feat: Preset 环境变量展开

## Summary

Channel 配置中的密钥字段支持 `${VAR}` 语法引用环境变量，使用 `os.Expand(s, os.Getenv)` 在消费点展开。`Preset` 结构体始终持有占位符，密钥不长期驻留内存。

---

## Problem Frame

`preset.yaml` 中 channel 配置密钥（`bot_secret`、`app_secret` 等）和 MCP Server URL token 目前以明文存储。部署依赖 `.gitignore` + 手工修订，存在泄露风险且操作繁琐。应用级配置已通过 `envconfig` 支持环境变量，preset 层缺少等价机制。

---

## Requirements Trace

| Origin ID | Requirement |
|---|---|
| R1 | 展开 `channels.*.config` 中所有 string 值的 `${VAR}` |
| R2 | 展开 `channels.*.mcpServers[*].url` 中的 `${VAR}` |
| R3 | 环境变量未设置时展开为空字符串 |
| R4 | 非 string 类型的 config 值不参与展开 |
| R5 | `MCPServerConfig` 的 `Name`、`TransType`、`HeaderCate` 不展开 |
| R6 | Preset 顶层字段不展开 |
| R7 | 不含 `${` 的值保持原样 |

---

## Key Technical Decisions

- **消费点展开而非 `LoadPreset` 展开**: 展开分散在 `InitChannels`（MCP URL）和各 channel adapter 构造函数（config string 值）。`Preset` 结构体始终持有 `${VAR}` 占位符，调试/日志时不会泄露真实密钥
- **直接使用 `os.ExpandEnv`**: 标准库一行搞定，同时支持 `${VAR}` 和 `$VAR`，无需自定义 helper。消费点只对已知的 secret 字段调 expand，`allow_from` 等非 secret 字段不调，自然满足 R7
- **不新增文件**: 改动只涉及现有的 `handle_platform.go` 和 4 个 channel adapter 文件，零新文件

---

## Implementation Units

### U1. MCP Server URL 展开

- **Goal**: 在 `InitChannels` 中注册 MCP Server 前展开 URL 中的 `${VAR}`
- **Requirements**: R2, R3
- **Dependencies**: 无
- **Files**:
  - `pkg/web/api/handle_platform.go` — 修改 `InitChannels` 函数
- **Approach**:
  在 `mcps.ServerBasic{...}` 构造时，对 `mcpCfg.URL` 调 `os.ExpandEnv`：
  ```go
  URL: os.ExpandEnv(mcpCfg.URL),
  ```
- **Verification**: 含 `${VAR}` 的 MCP URL 正确展开，不含 `${}` 的 URL 不变

### U2. Channel Config 值展开

- **Goal**: 在各 channel adapter 构造函数中展开 secret 字段
- **Requirements**: R1, R3, R4, R7
- **Dependencies**: 无
- **Files**:
  - `pkg/services/channels/wecom/websocket.go` — `bot_secret` 取值处
  - `pkg/services/channels/wecom/wecom_http.go` — `corp_secret`、`callback_token`、`callback_aes_key` 取值处
  - `pkg/services/channels/feishu/websocket.go` — `app_secret` 取值处
  - `pkg/services/channels/feishu/webhook.go` — `app_secret`、`encrypt_key` 取值处
- **Approach**:
  每个 secret 字段取值后包一层 `os.ExpandEnv`。例如 wecom websocket.L103：
  ```go
  secret := os.ExpandEnv(opts["bot_secret"].(string))
  ```
  非 secret 字段（`allow_from`、`proxy`、`callback_path`、`enable_markdown`）不调 expand。`bot_id`、`app_id`、`corp_id`、`agent_id` 等非机密标识符也不调。
- **Verification**: 含 `${VAR}` 的字段正确展开，不含 `${}` 的字段不变

### U3. 测试

- **Goal**: 覆盖展开逻辑
- **Requirements**: R1, R2, R3, R7
- **Dependencies**: U1, U2
- **Files**:
  - `pkg/utils/expand_test.go` — 可选：如果团队偏好有集中的单元测试，对 `os.Expand(s, os.Getenv)` 行为做表驱动验证
- **Approach**:
  `os.ExpandEnv` 是标准库，本身不需要测试。如果加测试，用 `t.Setenv` + 表驱动覆盖 `${VAR}` 存在/不存在/多个/空字符串等场景
- **Test scenarios**:
  - `${VAR}` 存在 → 展开为环境变量值 (Covers AE1)
  - `${VAR}` 不存在 → 展开为空字符串 (Covers AE2)
  - 不含 `${}` 的值 → 原样保留 (Covers AE3)
  - 同一字符串中多个 `${VAR}` → 全部展开 (Covers AE5)
- **Verification**: `go test -v ./pkg/utils/` 通过

---

## Scope Boundaries

### Deferred for later

- `preset.yaml` 和 `preset.example.yaml` 中实际替换 `${VAR}` 占位符 — 属于部署配置变更，不在此次代码改动范围

---

## Success Criteria

- 部署者可将 `preset.yaml` 安全提交到版本控制，密钥通过环境变量注入
- 不含 `${}` 的现有 preset 配置行为不变
- `Preset` 结构体在内存中始终持有 `${VAR}` 占位符
- 变量缺失时 adapter 层现有 required 校验正常报错
- `make vet lint` 通过
