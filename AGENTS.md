# AGENTS.md - Morign 智能体指南

## 项目概述

Morign 是一个 AI 聊天后端，集知识库问答、MCP 工具与 OAuth 认证于一体。

### 技术栈

- **语言**: Go 1.25
- **Web 框架**: chi/v5
- **数据库**: PostgreSQL (含 pgvector 扩展)
- **缓存/会话**: Redis
- **LLM**: 自定义实现 (pkg/services/llm)，支持 OpenAI/Anthropic/OpenRouter/Ollama

### 目录

- `pkg/models`: 数据模型与枚举
- `pkg/services/stores`: 存储层与后台任务（如导入 worker）
- `pkg/services/agent`: Agent 循环与工具执行
- `pkg/services/channels`: 频道适配器（企业微信、飞书）
- `pkg/web/api`: HTTP 接口
- `pkg/settings`: 环境变量配置

## 代码生成

- `docs/*.yaml`（capability、corpus、convo、mcps、skills）是模型、存储与接口的契约来源
- 修改契约后执行 `make codegen` 重新生成，该命令依赖同级仓库 `../scaffold`
- 接口文档用 `make gen-apidoc` 生成到 `docs/swagger.{json,yaml}`
- 以 `_gen.go` 结尾的是自动生成文件，勿动；扩展逻辑写进同目录的 `*_x.go`

## 编码规范

- 简洁为上
- 假设您所编写代码的维护者和读者都是 Go 专家
- 无需用注释解释显而易见的内容
- 使用自解释的变量和函数名
- 上下文清晰时使用短变量名
- 日志使用 `logger().Infow()` 或 `logger().Warnw()`
- 修改结构体时注意 JSON tag 命名一致


## 常用操作

### 代码检查与测试

```bash
# 提交前必做
make vet
make lint

# 运行测试
make test-models    # models 包测试
make test-stores    # stores 包测试，带 -tags=integration，需要本地 PostgreSQL + Redis
```

## 文档

- 需求与设计文档写入 `docs/plans/`（入库）
- 问题复盘写入 `docs/solutions/`
- `docs/brainstorms/`、`todos/` 已被 .gitignore 忽略，不要作为交付产物

## 注意

- 每次提交之前都要确认并先执行 make vet lint
