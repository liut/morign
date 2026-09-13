# Morign

AI 聊天后端，集知识库问答、MCP 工具与 OAuth 认证于一体。

## 功能特性

- 从 CSV 导入知识库文档：`import` 命令，或 keeper 专属的异步导入 API
- 使用 Embedding API 生成文档向量
- 基于向量搜索实现高质量问答，可选 LLM 重排
- 欢迎消息和预设消息
- 基于 Redis 的对话历史
- 提供 Swagger 文档的 RESTful API，支持 text/event-stream 流式响应
- OAuth2 客户端登录认证
- 内置 MCP（Model Context Protocol）工具支持
- 多频道适配器（企业微信 WebSocket/Webhook，飞书 WebSocket/Webhook）
- 预设配置 `${VAR}` 环境变量展开
- keeper 角色守卫写操作与管理端点
- Skills 支持：管理 API 与按频道注入
- 记忆分层衰减、强化与遗忘

## 支持的前端

<details>
 <summary>chatgpt-svelte 基于 Svelte  ⤸</summary>

 ![chatgpt-svelte](./docs/screen-svelte-s.png)

> forked: https://github.com/liut/chatgpt-svelte

</details>

<details>
 <summary>Calisyn 基于 Vue.js  ⤸</summary>

 ![calisyn](./docs/screen-web-s.png)

> https://github.com/liut/calisyn

</details>

## API 接口

> 完整接口文档：[docs/swagger.yaml](./docs/swagger.yaml)，用 `make gen-apidoc` 重新生成。
> 标记 🔑 的端点需要 keeper 角色。

### 获取会话信息

<details>
 <summary><code>GET</code> <code><b>/api/session</b></code></summary>

##### 参数

> 无

##### 说明

> 返回当前登录状态和用户信息。若未登录且 `auth` 为 true，前端应跳转到 `data.uri` 进行 OAuth 登录。

##### 响应

> | http 状态码 | content-type                      | 响应                                           |
> |---------------|-----------------------------------|---------------------------------------------------------------------|
> | `200`         | `application/json`        | `{"status":"Success","data":{"auth":false,"user":{...},"keeper":false}}` (已登录)                          |
> | `200`         | `application/json`        | `{"status":"Success","data":{"auth":true,"uri":"/api/auth/login"}}` (未登录)                |


</details>

### 获取已验证或已登录用户的信息

<details>
 <summary><code>GET</code> <code><b>/api/me</b></code></summary>

##### 参数

> 无

##### 响应

> | http 状态码 | content-type                      | 响应                                           |
> |---------------|-----------------------------------|---------------------------------------------------------------------|
> | `200`         | `application/json`        | `{"data": {"avatar": "", "name": "name", "uid": "uid"}}`                                         |
> | `401`         | `application/json`        | `{"error": "", "message": ""}`                                         |


</details>

### 发送聊天消息并返回流式响应

<details>
 <summary><code>POST</code> <code><b>/api/chat-sse</b></code> 或 <code><b>/api/chat</b></code> 搭配 <code>{stream: true}</code></summary>

##### 参数

> | 名称       |  类型     | 数据类型      | 描述                         |
> |------------|-----------|----------------|-------------------------------------|
> | `csid`     |  可选 | string       | 会话 ID        |
> | `prompt`   |  必填 | string       | 提问消息        |
> | `stream`   |  可选 |  bool        | 启用 event-stream，在 <code><b>/api/chat-sse</b></code> 中强制开启       |


##### 响应

> | http 状态码 | content-type               | 响应                                           |
> |---------------|----------------------------|----------------------------------------------------|
> | `200`         | `text/event-stream`        | `{"delta": "消息片段", "id": "会话 ID"}`                                          |
> | `401`         | `application/json`        | `{"status": "Unauthorized", "message": ""}`                                         |


</details>

### 语料导入 🔑

CSV 上传是异步的：`POST /api/corpus/imports` 接收 `multipart/form-data` 的 `file` 字段（UTF-8 CSV，表头 `title,heading,content`，最大 10 MiB），返回 `pending` 状态的任务；后台 worker 逐个处理。之后用 `GET /api/corpus/imports`（可按 `status`/`filename` 过滤，用 `limit`/`page` 分页，`sort` 排序）或 `GET /api/corpus/imports/{id}` 查询进度、计数和失败详情。CSV 原文随任务保存，读取接口不会返回。

请求/响应结构和状态码见 swagger 文件。

## 快速开始

```bash

# 安装依赖（包含 forego 用于加载环境变量）
make deps

# 或手动执行
go mod tidy
go install github.com/ddollar/forego@latest

# 从示例创建 .env 并配置（更新 PG/Redis URL、API Key 等）
test -e .env || cp .env.example .env

# 使用 .env 中的环境变量启动服务器
forego start

# 或直接运行（开发模式）
forego run go run . web

```

### 准备 YAML 预设数据文件

```yaml
welcome: "你好，我是你的虚拟助手。有什么可以帮助你的吗？"
systemPrompt: "你是一位有用的助手。"
toolsPrompt: "根据用户问题选择合适的工具并调用来解决问题。"

# 频道适配器（企业微信、飞书），密钥字段支持 ${VAR} 环境变量
channels:
  wecom:
    enable: true
    mode: websocket
    config:
      bot_id: "${WECOM_BOT_ID}"
      bot_secret: "${WECOM_BOT_SECRET}"

# 自定义工具描述（可选）
tools:
  kb_search: "在知识库中搜索相关内容。当遇到未知或不确定的问题时，优先查阅知识库。"
  kb_create: "创建新的知识库文档，所有参数必填。注意：除非用户明确要求补充内容，否则不要调用。"
  fetch: "从互联网获取 URL 内容并可选地提取为 markdown 格式"
  memory_list: "列出所有已存储的记忆"
  memory_recall: "通过关键词搜索记忆"
  memory_store: "存储新的记忆"
  memory_forget: "根据 key 删除记忆"
```

- `welcome`: 显示给用户的欢迎消息
- `systemPrompt`: AI 对话的系统提示
- `toolsPrompt`: 工具使用说明（当 MCP 工具可用时使用）
- `channels`: 频道适配器配置，密钥字段支持 `${VAR}` 环境变量展开
- `tools`: 自定义工具描述（可选，覆盖内置默认值）
  注意：记忆工具 (memory_*) 与登录身份绑定并隔离。

完整示例请参考 [data/preset.example.yaml](./data/preset.example.yaml)

### 准备数据库

```sql
CREATE USER morign WITH LOGIN PASSWORD 'mydbusersecret';
CREATE DATABASE morign WITH OWNER = morign ENCODING = 'UTF8';
GRANT ALL PRIVILEGES ON DATABASE morign to morign;


\c morign

# 安装扩展 https://github.com/pgvector/pgvector
CREATE EXTENSION vector;

```

### 命令行用法

```plan

USAGE:
   morign [全局选项] 命令 [命令选项]

COMMANDS:
   usage, env                         显示用法
   initdb                             初始化数据库模式
   import                             从 csv 导入文档
   import-swagger, import-capability  从 swagger yaml/json 导入 API 能力
   export, exportDocs                 导出文档到 csv
   embedding, embedding-doc-vec       读取提示文档并生成嵌入
   cleanup-missed                     删除已标记缺失的能力
   agent, llm, chat                   测试 LLM 功能
   web, run                           运行 Web 服务器
   version, ver                       显示版本
   help, h                            显示命令帮助

GLOBAL OPTIONS:
   --help, -h  显示帮助

```

用 `./morign <命令> --help` 查看各命令的参数，例如 `import --diff`、`export --format csv|jsonl`、`import-swagger --mark-missing`。

#### Agent 命令

测试 LLM 功能的命令行工具：

```bash
# 非流式对话
./morign agent -m "你好"

# 流式对话
./morign agent -m "你好" -s

# 显示详细日志
./morign agent -m "你好" -v
```

参数：
- `-m, --message`: 发送的消息 (必填)
- `-s, --stream`: 启用流式响应
- `-v, --verbose`: 显示日志 (默认关闭)
- `-i, --interactive`: 交互式 REPL 模式

### 使用环境变量配置

#### 查看所有本地配置
```bash
./morign usage
```

```bash
# HTTP 服务器
MORIGN_HTTP_LISTEN=:3002

# 可选：预设数据
MORIGN_PRESET_FILE=./data/preset.yaml

# 可选：OAuth2 登录
MORIGN_AUTH_REQUIRED=true
OAUTH_PREFIX=https://portal.my-company.xyz

# 可选：代理
HTTPS_PROXY=socks5://proxy.my-company.xyz:1081
```

#### 环境变量列表

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `MORIGN_PG_STORE_DSN` | postgres://morign@localhost/morign?sslmode=disable | PostgreSQL 连接串 |
| `MORIGN_REDIS_URI` | redis://localhost:6379/1 | Redis 连接串 |
| `MORIGN_HTTP_LISTEN` | :5001 | HTTP 监听地址 |
| `MORIGN_AUTH_REQUIRED` | false | 是否启用认证 |
| `MORIGN_KEEPER_ROLE` | keeper | 写操作工具需要的角色 |
| `MORIGN_KEEPER_UIDS` | | 逗号分隔的 uid 列表，跳过角色检查 |
| `MORIGN_VECTOR_THRESHOLD` | 0.47 | 向量相似度阈值（0.39 - 0.65） |
| `MORIGN_VECTOR_LIMIT` | 6 | 向量匹配数量 |
| `MORIGN_RERANK_ENABLED` | false | 是否启用能力匹配的 LLM 重排 |
| `MORIGN_MAX_LOOP_ITERATIONS` | 12 | Agent 工具调用循环次数上限 |
| `MORIGN_TOOL_RESULT_MAX_CHARS` | 20000 | 单次工具结果长度上限（字符数），超出即截断，0 表示不限制 |

其他开关（skill 注入、记忆分层、OAuth、Sentry 等）可用 `./morign usage` 查看。

#### Provider 配置（AI 服务）

每个 Provider 需要 `API_KEY` 和 `MODEL`，可选 `URL` 和 `TYPE` 用于自定义端点：

| Provider | 用途 | 必需变量 |
|----------|------|----------|
| `INTERACT` | 对话/补全 | `API_KEY`, `MODEL` |
| `EMBEDDING` | 向量嵌入 | `API_KEY`, `MODEL` |
| `SUMMARIZE` | 文本摘要 | `API_KEY`, `MODEL` |

支持的 Provider Type: `openai`, `anthropic`, `openrouter`, `ollama`

示例：
```
# Interact provider (支持 openai/anthropic/openrouter/ollama)
MORIGN_INTERACT_API_KEY=sk-xxx
MORIGN_INTERACT_MODEL=gpt-4o-mini
MORIGN_INTERACT_TYPE=openai  # 可选，默认 openai

# 使用 Anthropic
MORIGN_INTERACT_TYPE=anthropic

MORIGN_EMBEDDING_API_KEY=sk-xxx
MORIGN_EMBEDDING_MODEL=text-embedding-3-small

MORIGN_SUMMARIZE_API_KEY=sk-xxx
MORIGN_SUMMARIZE_MODEL=gpt-4o-mini
```

> 提示：运行 `./morign usage` 可查看当前所有配置

## 数据生成步骤

1. 准备 CSV 文件作为语料库文档
2. 导入文档
3. 使用 Completion 从文档生成问答
4. 使用 Embedding 从问答生成提示和向量
5. 完成，开始聊天

第 2 步也可以走 HTTP：把 CSV 上传到 `POST /api/corpus/imports`（仅 keeper 角色），然后轮询 `GET /api/corpus/imports/{id}`，直到状态变成 `succeeded` 或 `failed`。

### 文档 CSV 模板

| title      | heading     | content                                   |
|------------|-------------|-------------------------------------------|
| 我的公司 | 简介 | 一家伟大的公司源于一个天才的想法。 |
|            |             |                                           |

```bash
./morign initdb
./morign import mycompany.csv
./morign embedding
```

## 挂载前端资源

1. 进入前端项目目录
2. 构建前端页面和静态资源
3. 复制到 ./htdocs

示例：

```bash
cd ../Calisyn
npm run build
rsync -a --delete dist/* ../morign/htdocs/
cd -
```

在开发和调试阶段，你仍可以使用代理与前端项目协作。
