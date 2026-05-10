# Morign 数据模型与实体关系

## 1. 实体关系总览

```mermaid
erDiagram
    convo_User ||--o{ convo_Session : "owns"
    convo_User ||--o{ convo_ThirdUser : "maps to"
    convo_User ||--o{ convo_Memory : "has"
    convo_Session ||--o{ convo_Message : "contains"
    convo_Session ||--o{ convo_UsageRecord : "tracks"
    convo_Session ||--o{ corpus_ChatLog : "logs (deprecated)"

    corpus_Document ||--o{ corpus_DocVector : "embeds to"
    convo_Memory ||--o{ corpus_DocVector : "embeds to (same table)"
    capability_Capability ||--o{ capability_CapabilityVector : "embeds to"

    mcps_Server ||--o{ mcps_ToolDescriptor : "provides"

    convo_Session {
        bigint id PK "OID"
        string title "标题"
        int msgCount "消息数"
        smallint status "open/closed"
        array tools "工具列表"
        varchar channel "频道"
        jsonb meta "扩展元数据"
        bigint owner_id FK "所有者"
        timestamp created
        timestamp updated
    }

    convo_Message {
        bigint id PK "OID"
        bigint session_id FK "会话ID"
        string role "system/user/assistant/tool"
        text content "消息内容"
        smallint tokenCount "Token数"
        jsonb meta "扩展元数据"
        timestamp created
    }

    convo_User {
        bigint id PK "OID"
        varchar username UK "登录名"
        varchar nickname "昵称"
        varchar avatar "头像"
        varchar email "邮箱"
        varchar phone "电话"
        jsonb meta "扩展元数据"
        timestamp created
        timestamp updated
    }

    convo_ThirdUser {
        bigint id PK "平台+账号"
        bigint owner_id FK "关联User"
        jsonb meta "扩展元数据"
    }

    convo_Memory {
        bigint id PK "OID"
        bigint owner_id FK "所有者"
        text key UK "关键点"
        text cate "分类 core/daily/conversation"
        text content "内容"
        jsonb meta "扩展元数据"
        timestamp created
        timestamp updated
    }

    convo_UsageRecord {
        bigint id PK "OID"
        bigint session_id FK "会话ID"
        smallint msgCount "消息数"
        int inputTokens "输入Token"
        int outputTokens "输出Token"
        int totalTokens "总Token"
        string model "模型名"
        jsonb meta "扩展元数据"
        timestamp created
    }

    corpus_Document {
        bigint id PK "OID"
        text title UK "主标题"
        text heading UK "小节标题"
        text content "内容"
        jsonb meta "扩展元数据"
        timestamp created
        timestamp updated
    }

    corpus_DocVector {
        bigint id PK "OID"
        bigint doc_id FK "文档/记忆ID"
        text subject "向量化文本"
        vector embedding "1024维向量"
        jsonb meta "扩展元数据"
    }

    corpus_ChatLog {
        bigint id PK "OID"
        bigint csid FK "会话ID"
        text question "提问"
        text answer "回答"
        jsonb meta "扩展元数据"
    }

    mcps_Server {
        bigint id PK "OID"
        varchar name UK "名称"
        smallint transType "stdio/sse/streamable/inMemory"
        varchar command "启动命令"
        varchar url "服务URL"
        bool isActive "是否激活"
        smallint status "连接状态"
        varchar remark "备注"
        smallint headerCate "认证头类型"
        jsonb meta "扩展元数据"
    }

    capability_Capability {
        bigint id PK "OID"
        varchar operationID "操作ID"
        varchar endpoint "API路径"
        varchar method "GET/POST/PUT/DELETE"
        text summary "简介"
        text description "详细描述"
        jsonb parameters "参数定义"
        jsonb responses "响应定义"
        jsonb tags "标签"
        jsonb meta "扩展元数据"
    }

    capability_CapabilityVector {
        bigint id PK "OID"
        bigint cap_id FK "Capability ID"
        text subject "向量化文本"
        vector embedding "1024维向量"
    }
```

## 2. 核心实体关联图

```
┌──────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  convo_User  │────→│   convo_Session   │────→│  convo_Message   │
│              │     │                   │     │                  │
│  username    │     │  title            │     │  role            │
│  nickname    │     │  status           │     │  content         │
│  avatar      │     │  tools[]          │     │  tokenCount      │
│  email       │     │  channel          │     │                  │
└──────────────┘     └──────────────────┘     └─────────────────┘
       │                      │
       │                      ├──────────────────┐
       │                      │                  │
       ▼                      ▼                  ▼
┌──────────────┐     ┌──────────────────┐     ┌─────────────────┐
│convo_ThirdUsr│     │convo_UsageRecord │     │  corpus_ChatLog  │
│              │     │                  │     │  (deprecated)    │
│  owner_id ───┼─────│  inputTokens     │     │  csid            │
│  platform+uid│     │  outputTokens    │     │  question        │
└──────────────┘     │  model           │     │  answer          │
                     └──────────────────┘     └─────────────────┘

┌──────────────┐     ┌──────────────────┐
│  convo_Memory│────→│  corpus_DocVector │←────│ corpus_Document │
│              │     │                   │     │                  │
│  owner_id    │     │  doc_id (FK)      │     │  title           │
│  key         │     │  subject          │     │  heading         │
│  cate        │     │  embedding(1024)  │     │  content         │
│  content     │     │                   │     │                  │
└──────────────┘     └──────────────────┘     └──────────────────┘
                              ▲
                              │
                     ┌────────┴─────────┐
                     │capability_CapVector│
                     │                   │
                     │  cap_id (FK)      │
                     │  subject          │
                     │  embedding(1024)  │
                     └───────────────────┘
                              │
                              ▼
                     ┌───────────────────┐
                     │capability_Capability│
                     │                   │
                     │  endpoint         │
                     │  method           │
                     │  parameters       │
                     │  responses        │
                     └───────────────────┘

┌──────────────┐
│  mcps_Server │
│              │
│  name        │──────── 对外 MCP 工具（运行时加载，不持久化到独立表）
│  transType   │
│  url         │
│  isActive    │
│  headerCate  │
└──────────────┘
```

## 3. 关键数据表说明

| 表名 | 用途 | 关键字段 |
|------|------|---------|
| `convo_session` | 会话管理，每次对话一个 Session | `title`, `status`, `tools`, `channel`, `owner_id` |
| `convo_message` | 会话消息（新架构，迁移中） | `session_id`, `role`, `content`, `tokenCount` |
| `convo_user` | 用户信息，OAuth 登录同步 | `username`(唯一), `nickname`, `avatar` |
| `convo_third_user` | 第三方平台用户映射（微信/飞书） | PK=平台前缀+UID, `owner_id`→User |
| `convo_memory` | 用户长期记忆 | `owner_id`+`key`(联合唯一), `cate`, `content` |
| `convo_usage_record` | Token 用量追踪 | `session_id`, `inputTokens`, `outputTokens`, `model` |
| `corpus_document` | 知识库文档 | `title`+`heading`(联合唯一), `content` |
| `corpus_vector_400` | 向量存储（文档+记忆共用） | `doc_id`, `embedding`(1024维), `subject` |
| `mcp_server` | MCP 服务器配置 | `name`(唯一), `transType`, `url`, `isActive`, `headerCate` |
| `api_capability` | OpenAPI/Swagger 导入的 API 能力 | `endpoint`, `method`, `parameters`(jsonb) |
| `api_capability_vector` | API 能力语义向量 | `cap_id`, `embedding`(1024维) |
