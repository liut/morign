# Changelog

## v0.9.0 (2026-09-10)

### 新功能

- **Skills**: 新增 Agent Skills 支持 — SKILL.md 及其资源按技能存库（`agent_skill` / `agent_skill_file` 表），系统提示只注入元数据，全文按需加载：清单低于阈值时直接注入，否则通过 `skill_list`、`skill_read`、`skill_file_list`、`skill_file_read` 内置工具或 `/skill` 指令加载
- **Skills**: 新增技能 REST API — 用户级 `/api/skills`（owner 语义，含描述长度校验）与 keeper 专属 `/api/admin/skills`，可见性按频道位掩码控制；二进制资源只存储、不注入 LLM 上下文
- **Memory**: 新增记忆分层与衰减 — 写入时按规则评分（长度、标点、关键词）路由 `working`/`short-term`/`long-term`，召回时按艾宾浩斯曲线 `R = e^(-t/24S)` 重排，命中后重置强化时钟，按访问次数自动升级层级，低于遗忘阈值的记忆乘 0.1 惩罚
- **Memory**: `memory_list` 支持 tier 过滤，`memory_recall` 输出 tier 字段；阈值与提升次数均可通过环境变量配置
- **Corpus**: 新增文档管理 API — `/api/corpus/documents` 的列表、详情、更新与删除，写操作 keeper 专属
- **Corpus**: 新增异步 CSV 导入 — `POST /api/corpus/imports` 上传 CSV（10 MiB 上限、BOM 处理、表头校验）即创建任务，由串行 worker 逐个处理，支持重复行跳过、失败明细（最多 500 条、原因截断）与崩溃后任务回收；列表与 `GET /api/corpus/imports/{id}` 查询进度，读取接口不返回 CSV 原文
- **Session**: `/api/session` 新增 `keeper` 字段，前端可据此决定是否展示管理功能

### Bug 修复

- **Memory**: `reinforceMemory` 改用 `SetWith` 更新，使 `last_accessed_at`、`access_count`、`tier`、`decay_rate` 的变更可被追踪，并移除多余的 `CalcReinforce` 调用（召回即重置强化时钟）
- **Memory**: 修复 `Tier` 列默认值未加引号，PostgreSQL 会把 `default:working` 当作列引用导致建表失败
- **Memory**: `memory_forget` 同步删除 `corpus_vector_400` 记录
- **Feishu**: 移除 WebSocket `Start` 恒为真的错误判断，改为直接记录返回的错误

### 重构

- **Stores**: 从 `IsKeeper` 拆出 `UserIsKeeper`，无需 context 即可复用
- **Stores**: capability invoker 闭包提取为命名函数与方法（`parseMatchArgs`、`buildMatchResults`、`CapabilityInvoker.InvokeAsTool`）

### 文档

- README / README_CN 同步当前实现：修正向量阈值、匹配数量、DSN 等默认值，更新 CLI 命令列表，补充 keeper、skills、记忆分层等特性，接口细节改为指向 swagger
- AGENTS.md 补充代码生成流程（`docs/*.yaml` → `make codegen`、`make gen-apidoc`）、提交前检查与集成测试依赖说明

### 维护

- 升级 `cupogo/andvari` 依赖，适配 `pgx.In` → `pgx.List`

### 升级说明

- 启动时自动执行 schema 初始化与 migration（新增记忆分层列、skills 与导入任务表），也可手动执行 `./morign initdb`

---

## v0.8.0 (2026-07-28)

### 新功能

- **Channel**: 新增 token fallback 机制 — 频道用户（企业微信/飞书）未登录 Web 前端时，通过 aurora 内部 API 自动获取 token，确保 API 调用携带 Authorization header

### Bug 修复

- **LLM**: 修复翻译后的错误消息未传递到 fallback 回复的问题，并处理 overloaded 错误状态

### 重构

- **Agent**: 提取 AgentLoop，新增并行工具执行和 terminate 支持 (#24)
- **Agent**: 将 Agent 类型从 `pkg/web/api` 移至 `pkg/services/agent`，消除 AgentLoop 职责重叠
- **Lint**: 新增 `.golangci.yml` 配置，修复存量 lint 问题（errcheck、staticcheck 等）
- **CI**: 升级 golangci-lint 至 v2，同步更新 CI action 至 v7

### 维护

- 抑制企业微信 WebSocket 高频心跳/流式调试日志，减少生产环境日志噪音

---

## v0.7.0 (2026-06-04)

### 新功能

- **Preset**: config 字段和 MCP URL 支持 `${VAR}` 环境变量展开，消除 YAML 中的硬编码密钥
- **MCP**: 新增 channel-scoped MCP server 注册，工具按频道隔离

---

## v0.6.0 (2026-06-02)

### 新功能

- **Capability**: 新增 LLM re-rank 能力匹配
- **Capability**: 新增 `--mark-missing` 和 `cleanup-missed` 批量维护命令
- **Agent CLI**: thinking/reasoning 内容以 ANSI dim+italic 样式显示

### 重构

- 事件驱动架构重构 — 统一 Event 和 Runner.Persist
- 移除未使用的 AddInvoker 方法
- 收窄 AddServer 签名为 ServerBasic，提取 HeaderFuncFor
- MCP 修复 HeaderFunc 移至 ServerBasic，修正 tool key 分隔符

---

## v0.5.0 (2026-05-08)

### 新功能

- **Agent CLI**: 新增工具调用支持和交互式 REPL 模式（`-i`），复用 ToolExecutor 工具调用循环
- **Agent CLI**: 流式模式下 thinking/reasoning 内容以 ANSI dim 样式区分显示

### 重构

- 提取 `ExecuteToolCalls` 共享方法，消除三处工具调用重复逻辑
- 移除 api struct 中未使用的 router 字段
- 将 channels 初始化从 `newapi` 移至 `Strap`/`InitChannels`
- Channels (飞书/企业微信) 改为显式注册

---

## v0.4.1 (2026-05-07)

### Bug 修复

- 修复 chi 默认 Recoverer 堆栈跟踪泄漏，改用自定义恢复中间件
- 修复 `use GetLLMEmbeddingClient()` 替代直接访问 `llmEm`
- 修复 provider 配置校验从 config init 移到 stores lazy-init
- 修复 capability 导入时 skipai tag 大小写不敏感匹配
- 修复重复的 corpus 向量索引迁移文件
- 修复 LLM client 初始化重构以支持无 provider 配置时的测试 mock
- 修复 Interact client 初始化移出 stores 懒加载

### 重构

- LLM clients 使用 sync.Once 懒初始化

---

## v0.4.0 (2026-04-29)

### 新功能

- **WeCom**: 新增流式消息支持，通过 StreamReplier 接口实现
- **Channel**: StartStream 移到 StreamChat 之前，改进错误处理
- **LLM**: 新增按 provider 的 debug 流式响应配置
- **LLM**: 新增按 provider 的交互日志写入文件
- **Capability**: 新增 API capability 向量搜索支持
- **Capability**: 新增 afterUpdated hook 在更新时同步 capability embeddings
- **Capability**: 新增 skipai tag 支持，导入时跳过/删除标记为 skipai 的 API

### Bug 修复

- 修复 DeepSeek Anthropic 格式兼容性
- 修复 DeepSeek thinking mode 兼容性
- 修复 BuildToolSuccessResult 简化为仅支持 embedded tools
- 修复 capability API 响应体在状态检查前先解码
- 修复 capability 导入时 skipai tag 大小写不敏感匹配
- 修复重复的 corpus 向量索引迁移文件

### 重构

- OAuthTokenMiddleware 增加 header fallback 逻辑
- Capability Responses 使用 SwaggerSchema 结构
- Capability match 默认 limit 从 5 提高到 6
- SwaggerParam.Required JSON tag 增加 omitempty

---

## v0.3.0 (2026-04-02)

### 新功能

- **平台集成**: 新增 WeCom/Feishu platform adapter layer
- **平台集成**: 新增 ThirdUser 表，优化 WeCom 集成
- **用户模型**: 新增 email等 字段，增强 OAuth 同步，支持 avatar
- **Session**: 新增 session command 系统
- **Session**: 从 sessionKey 提取 channel/chatID 到 Session 结构
- **Storage**: 支持 preset 存储，记忆加载改为仅对已认证用户生效
- **飞书**: WebSocket 客户端集成 slog 结构化日志

### Bug 修复

- 修复图片 URI 相对路径补全
- 修复 signin 前用户数据刷新
- 修复 API 文档注解和 swagger 生成
- 修复 `storeUserAndMeta` 并重命名为 `storeUserWith`

### 重构

- 移除 OAuth MCP 集成，简化用户管理
- 项目更名：morrigan → morign
- 修复测试向量维度
- Makefile 环境变量加载重构

### 文档

- API 文档 `/api/welcome` → `/api/session`
- 新增登录/登出响应示例

---

## v0.2.4 (2026-03-27)

- 新增 OAuth MCP server 注册（改为无需授权，延至请求）

## v0.2.3 (2026-03-26)

- 重构：移除 openai.go 中的 regexp 依赖

## v0.2.2 (2026-03-25)

- 重构：提取文本截断逻辑为可复用工具

## v0.2.1 (2026-03-24)

- 新增 AddHistory 重复检测

## v0.2.0 (2026-03-23)

- 新增 Anthropic provider 和 CLI agent 命令
- 新增统一 LLM service，修复流式 tool calls
- 新增 OAuth SP 作为 MCP 支持
- 新增结构化日志
- 简化 chat API，移除 Full response 格式
- API 启动时加载 preset 和初始化工具注册表

## v0.1.2 (2026-03-xx)

- 初始版本
- 基础 API 功能
- Redis 会话历史
- LLM provider 支持
