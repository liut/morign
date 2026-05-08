# Changelog

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
