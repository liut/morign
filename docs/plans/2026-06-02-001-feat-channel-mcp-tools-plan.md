---
type: feat
origin: docs/brainstorms/2026-06-02-channel-mcp-tools-requirements.md
status: active
---

# feat: 频道专属 MCP 工具注册

## Summary

为每个频道（WeCom、Feishu）支持声明专属的 MCP Server 列表，Registry 按当前频道上下文返回「全局工具 + 该频道专属工具」，实现频道级工具可见性隔离。

---

## Problem Frame

当前 MCP Server 工具全局可见——WeCom 频道专属的 API 封装工具会出现在飞书频道的工具列表中，造成工具集膨胀。需要频道级作用域机制。详见 origin doc Problem Frame。

---

## Requirements Trace

| Origin | Requirement |
|--------|-------------|
| R1 | Preset ChannelConfig 支持 `mcp_servers` 字段 |
| R2 | 工具名 `{channel}_{server_name}-{tool_name}` 格式 |
| R3, R4 | `ToolsFor(ctx)` 按 context 中频道名返回全局+频道工具；无频道时仅全局 |
| R5 | 频道工具始终公开，不受 keeper 限制 |
| R6, R7, R8 | 频道启动时注册 MCP 工具，停止时清理，连接失败降级 |
| R9, R10 | 全局重名冲突检测；同频道内 server 间重名检测 |
| R11 | 频道 MCP 健康可查询 |
| R12 | 设计决策包含 capability 对比 |

---

## Key Technical Decisions

1. **频道作用域 key 使用频道类型名（`p.Name()`，如 `"wecom"`）而非实例 key（`"wecom-websocket"`）。** 同类型多实例共享 MCP 工具——session key 的频道段也是类型名，保持一致。
2. **频道工具存储为独立 `channelTools map[string]*channelToolSet`**，而非打散到全局列表中再加 tag 过滤。物理隔离避免全局列表膨胀和过滤开销。
3. **频道名通过 context 传递**，遵循现有 `ContextWithServerName` 模式，在 `MessageHandler` 中注入。
4. **MCP 连接失败降级不阻塞频道启动**，记 warning 继续。与现有 `LoadServers` 的容错行为一致。
5. **Preset 配置结构新增 `MCPServerConfig`**，只含 preset 需要的字段（Name、URL、TransType、HeaderCate），不复用 DB 模型 `mcps.Server`。

---

## Implementation Units

### U1. 修复 Registry 现有竞态条件

**Goal:** 修复 `AddServer` 中 check-then-register 的锁覆盖范围和 `ToolsFor` 的读锁缺失，为后续频道作用域改造提供安全基础。

**Requirements:** (prerequisite, no origin R-ID)

**Dependencies:** none

**Files:**
- `pkg/services/tools/registry.go`

**Approach:**
- `AddServer`：将 `serversMu.Lock()` 提前到冲突检测之前，使 check + register 在同一临界区内
- `checkToolNameConflict`：调用方已持锁，移除其内部的 `RLock`（当前函数在 `AddServer` 内被调用时无锁，在 `AddInvoker` 内被调用时也无锁——统一由调用方持锁）
- `ToolsFor`：读取 `r.tools` 和 `r.privTools` 时加 `serversMu.RLock`
- `callServerTool`：将 server 查找和 `CallTool` 放在同一 `RLock` 临界区内，消除 TOCTOU

**Patterns to follow:** 现有 `serversMu` 用法风格

**Test scenarios:**
- 并发 `AddServer` 不产生重名工具（go test -race）
- `ToolsFor` 与 `AddServer` 并发执行不触发 race detector
- `callServerTool` 在 server 被并发 Remove 后返回 "server not found" 而非 panic

**Verification:** `go test -race ./pkg/services/tools/` 通过

---

### U2. 频道 Context 传递

**Goal:** 新增 context key 和 helper 函数，使频道名可从 context 提取；在 `MessageHandler` 中注入频道名。

**Requirements:** R3, R4

**Dependencies:** U1

**Files:**
- `pkg/models/mcps/mcps_x.go`
- `pkg/web/api/handle_platform.go`

**Approach:**
- 在 `mcps_x.go` 新增 `ctxChannelKey`（空 struct）、`ContextWithChannel(ctx, name)`、`ChannelFromContext(ctx)`
- 在 `MessageHandler` 中，用户/OAuth 注入之后（handle_platform.go:117 行附近），调用 `ctx = mcps.ContextWithChannel(ctx, p.Name())`
- `prepareSystemMessage` 现有签名不感知频道——ctx 已携带频道信息，无需改签名

**Patterns to follow:** `ContextWithServerName` / `ServerNameFromContext`（mcps_x.go:24-35），`ContextWithUser`（stores/auth.go:38）

**Test scenarios:**
- `MessageHandler` 调用后 ctx 中可提取 `p.Name()` 值
- HTTP API 路径（无频道上下文）调用 `ToolsFor` 时 `ChannelFromContext` 返回空字符串
- `ChannelFromContext` 在未注入频道名的 context 上返回 ""

**Verification:** 现有频道消息处理测试通过；手动验证 HTTP API 路径不回归

---

### U3. Preset ChannelConfig 扩展

**Goal:** 在 `ChannelConfig` 中新增 `MCPServers` 字段，定义 preset 可声明的 MCP Server 配置结构。

**Requirements:** R1

**Dependencies:** none

**Files:**
- `pkg/models/aigc/preset.go`

**Approach:**
- 新增 `MCPServerConfig` 结构体：`Name`、`URL`、`TransType`（`mcps.TransType`）、`HeaderCate`（`mcps.HeaderCate`）
- 在 `ChannelConfig` 中新增 `MCPServers []MCPServerConfig` 字段，YAML tag `mcp_servers`
- 不复用 `mcps.Server`——preset 不需要 ORM 字段（BaseModel、DefaultModel、MetaField）和运行时字段（HeaderFunc、Status）

**Patterns to follow:** `ChannelConfig` 现有结构风格

**Test scenarios:**
- YAML 解析 `mcp_servers` 列表正确填充 `MCPServers` 字段
- 未配置 `mcp_servers` 时字段为 nil（向后兼容）
- `TransType` 枚举值正确序列化/反序列化

**Verification:** preset 解析单元测试；现有 preset YAML 加载不报错

---

### U4. Registry 频道作用域改造

**Goal:** Registry 支持按频道存储和检索工具；`ToolsFor` 和 `Invoke` 感知频道上下文。

**Requirements:** R2, R3, R4, R5

**Dependencies:** U1, U2

**Files:**
- `pkg/services/tools/registry.go`
- `pkg/services/tools/connection.go`

**Approach:**
- 新增 `channelToolSet` 结构体，包含 `tools []mcps.ToolDescriptor`、`invokers map[string]Invoker`、`servers map[string]*MCPConnection`、`mu sync.RWMutex`
- Registry 新增 `channelTools map[string]*channelToolSet` 字段（key = 频道名）+ `channelMu sync.RWMutex`
- `ToolsFor(ctx)` 从 ctx 提取频道名，若存在则合并 `channelTools[name].tools` + 全局 `r.tools`；无频道名时仅返回全局工具。频道工具不涉及 `privTools`（R5: 始终公开）
- `Invoke(ctx, name, params)` 先从 ctx 提取频道名，若存在则先查频道 invokers，未命中再查全局 invokers
- 新增 `AddChannelServer(ctx, channel string, cfg MCPServerConfig)` 方法：创建 transport → 连接 MCP → 列举工具 → 注册到对应 `channelToolSet`
- 工具名格式：`getToolKey` 改为 `{channel}_{serverName}-{toolName}`（R2），修改 `connection.go`
- 新增 `RemoveChannelServer(channel, serverName string)` 方法
- 新增 `RemoveChannelTools(channel string)` 方法（频道停止时批量清理）

**Patterns to follow:** 现有 `AddServer`、`RemoveServer`、`ToolsFor` 逻辑

**Test scenarios:**
- `ToolsFor(ctx)` with channel context 返回全局+频道工具
- `ToolsFor(ctx)` without channel context 仅返回全局工具
- `Invoke` 正确路由频道专属工具调用
- 频道工具名冲突（同频道内）返回 error
- 不同频道同名 server 不冲突（不同 `channelToolSet`）
- 频道工具不受 keeper 角色影响——`IsKeeper` 为 false 时仍可见

**Verification:** `go test -race ./pkg/services/tools/` 通过；手动集成测试 WeCom 频道调用专属工具

---

### U5. 频道生命周期集成

**Goal:** 在 `InitChannels` 中为每个频道初始化其 MCP Server；在 `StopChannels` 中清理。

**Requirements:** R6, R7, R8

**Dependencies:** U3, U4

**Files:**
- `pkg/web/api/handle_platform.go`

**Approach:**
- `InitChannels` 中，频道 `Start` 成功后，遍历 `cfg.MCPServers`，对每个 server 调用 `toolreg.AddChannelServer(ctx, name, server)`（name = `p.Name()`）
- 若 `AddChannelServer` 失败，记 `logger().Warnw("channel MCP server init failed", ...)`，继续处理下一个 server（R8 降级）
- `StopChannels` 中，`StopAll` 之前，遍历 tracked channels，对每个调用 `toolreg.RemoveChannelTools(name)`
- 频道 MCP 生命周期与频道实例生命周期绑定——不跟随 WebSocket 重连重建（MCP 连接独立于 WS 连接）

**Patterns to follow:** 现有 `InitChannels` 循环结构、`LoadServers` 容错风格

**Test scenarios:**
- 配置 2 个 MCP Server，1 个 URL 不可达——频道正常启动，可达 server 的工具已注册，不可达的记 warning
- `StopChannels` 后频道工具从 `ToolsFor` 中消失
- 频道重启后 MCP 工具重新注册（无 "already exists" 错误）

**Verification:** 集成测试——启动带 MCP 配置的频道，验证工具可见性；停止频道，验证工具移除

---

### U6. 健康查询

**Goal:** 提供频道 MCP 工具状态查询能力。

**Requirements:** R11

**Dependencies:** U4

**Files:**
- `pkg/services/tools/registry.go`
- `pkg/web/api/handle_platform.go`（或新 handler 文件）

**Approach:**
- Registry 新增 `ChannelServerStatus(channel string) []ServerStatus` 方法，返回每个 server 的连接状态和已注册工具列表
- 在 HTTP router 上注册 `/health/channel/{name}/tools` 端点（或复用现有 health 端点扩展）
- 返回 JSON：server name、URL、连接状态、工具数、工具名列表

**Patterns to follow:** 现有 API handler 风格

**Test scenarios:**
- 健康端点返回频道 MCP 连接状态
- MCP 不可达时状态反映为 disconnected 及错误信息
- 无 MCP 配置的频道返回空列表

**Verification:** HTTP 请求 `/health/channel/wecom/tools` 返回正确状态

---

## Scope Boundaries

- 不支持运行时动态增删频道 MCP（preset 配置，重启生效）
- 不支持跨频道 MCP 连接池复用——每个频道独立维护连接
- 不支持 MCP 连接自动重连——连接失败后需重启恢复

### Deferred to Follow-Up Work

- MCP 连接健康检查和自动重连机制
- Preset 热加载（SIGHUP 或 admin API）
- 跨频道共享 MCP 连接的连接池

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| MCP Server 启动枚举时响应慢，阻塞频道初始化 | 频道启动变慢 | `AddChannelServer` 加超时 context（30s）；失败降级不阻塞 |
| 频道工具数量增长导致 LLM function-calling 精度下降 | 工具选择错误率上升 | 当前规模可控；后续可加工具数上限 |
| `Invoke` 需要同时查频道和全局 invoker map，性能影响 | 轻微延迟 | 两级 map 查找，O(1)；频道工具量小（通常 <20） |

---

## Review Notes

### From 2026-06-02 review (resolved by U4)

- **Invoke must be channel-aware for ToolsFor to be useful** — U4 resolves this by making Invoke search channel-scoped invokers first.
