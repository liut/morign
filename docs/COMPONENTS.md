# Morign 组件交互与依赖

## 1. 核心组件依赖图

```mermaid
graph TD
    subgraph "入口"
        MAIN[main.go]
    end

    subgraph "CLI Commands"
        WEB[web/run]
        AGENT[agent/chat]
        INITDB[initdb]
        IMPORT[import]
        EMBED[embedding]
    end

    subgraph "Web Server"
        SRV[web.server]
        ROUTES[routes.Routers]
        API[api.strap]
        CH_INIT[InitChannels]
    end

    subgraph "API Layer"
        CHAT[postChat]
        CONFIG[getConfig]
        HISTORY[getHistory]
        TOOLS[getTools]
        SUMMARY[postSummary]
        WELCOME[getWelcome]
        TITLE[patchConversationTitle]
    end

    subgraph "Core Services"
        LLM_CLI[llm.Client]
        EVENT[llm.Event]
        RUNNER[runner.Runner]
        TOOL_REG[tools.Registry]
        TOOL_EXEC[ToolExecutor]
        AGENT_SVC[Agent]
    end

    subgraph "Data Access"
        STO[stores.Storage]
        CONVO[ConvoStore]
        CORPUS[CorpusStore]
        MCP_STO[MCPStore]
        CAP_STO[CapabilityStore]
        STATE[StateStore]
    end

    subgraph "External"
        PG[(PostgreSQL)]
        REDIS[(Redis)]
        OAUTH[Staffio OAuth]
        LLM_API[LLM API]
        MCP_SRV[External MCP]
        WECOM[企业微信]
        FEISHU[飞书]
    end

    MAIN --> WEB
    MAIN --> AGENT
    MAIN --> INITDB
    MAIN --> IMPORT
    MAIN --> EMBED

    WEB --> SRV
    SRV --> ROUTES
    ROUTES --> API
    ROUTES --> CH_INIT

    API --> CHAT
    API --> CONFIG
    API --> HISTORY
    API --> TOOLS
    API --> SUMMARY
    API --> WELCOME
    API --> TITLE

    CHAT --> LLM_CLI
    LLM_CLI --> EVENT[Event]
    EVENT --> RUNNER[Runner.Persist]
    RUNNER --> STO
    CHAT --> TOOL_REG
    CHAT --> TOOL_EXEC
    CH_INIT --> LLM_CLI
    CH_INIT --> TOOL_REG
    CH_INIT --> TOOL_EXEC
    CH_INIT --> STO

    AGENT --> AGENT_SVC
    AGENT_SVC --> LLM_CLI
    AGENT_SVC --> TOOL_REG
    AGENT_SVC --> TOOL_EXEC

    STO --> CONVO
    STO --> CORPUS
    STO --> MCP_STO
    STO --> CAP_STO
    STO --> STATE

    CONVO --> PG
    CORPUS --> PG
    MCP_STO --> PG
    CAP_STO --> PG
    STATE --> REDIS

    TOOL_REG --> CONVO
    TOOL_REG --> CORPUS
    TOOL_REG --> MCP_STO
    TOOL_REG --> CAP_STO
    TOOL_REG --> MCP_SRV

    LLM_CLI --> LLM_API
    API --> OAUTH

    CH_INIT --> WECOM
    CH_INIT --> FEISHU
```

## 2. 请求处理管道

```
HTTP Request
    │
    ▼
┌─────────────────┐
│  chi.Mux        │ ← middleware.RealIP, recoverer
└────────┬────────┘
         │
    ┌────▼────────────────────────────┐
    │  OAuthTokenMiddleware           │ ← 注入 OAuth Token 到 Context
    └────┬────────────────────────────┘
         │
    ┌────▼────────────────────────────┐
    │  routes.AuthMw(false)           │ ← 提取 User / Keeper 信息
    └────┬────────────────────────────┘
         │
    ┌────▼────────────────────────────┐
    │  Rate Limiter (opt.)            │ ← Redis 限流 (仅 /chat)
    └────┬────────────────────────────┘
         │
    ┌────▼────────────────────────────┐
    │  Handler (postChat etc.)        │
    └────────────────────────────────┘
```

## 3. Context 注入链

```mermaid
flowchart LR
    REQ[HTTP Request] --> OT[OAuthTokenMiddleware]
    OT --> |"ctx = WithToken(ctx, tok)"| CTX1[Context: token]
    CTX1 --> AM[AuthMw]
    AM --> |"验证 token → User"| CTX2[Context: token + User]
    CTX2 --> |"检查 Keeper role"| CTX3[Context: token + User + Keeper]
    CTX3 --> HANDLER[Handler]

    HANDLER --> |prepareSystemMessage| SYS[读取 User Info + Memories]
    HANDLER --> |ToolsFor| TOOLS[根据 Keeper 角色返回工具集]
    HANDLER --> |ExecuteToolCalls| INVOKE[Memory 操作需验证 User]
```

## 4. 工具注册与调用全景

```
┌──────────────────────────────────────────────────────────────┐
│                    tools.Registry                             │
│                                                              │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────────────┐  │
│  │   tools[]    │  │  invokers{}  │  │  servers{}         │  │
│  │ ToolDescriptor│  │ name→Invoker│  │ name→MCPConnection │  │
│  └──────┬───────┘  └──────┬───────┘  └─────────┬──────────┘  │
│         │                 │                     │             │
└─────────┼─────────────────┼─────────────────────┼─────────────┘
          │                 │                     │
          ▼                 ▼                     ▼
┌──────────────────────────────────────────────────────────────┐
│                      内置工具 (Built-in)                       │
├──────────────┬──────────────┬──────────────┬─────────────────┤
│  kb_search   │  kb_create   │    fetch     │   memory_*      │
│  pgvector    │  INSERT Doc  │  HTTP GET    │   CRUD Memory   │
│  相似度匹配   │  (Keeper)    │  HTML→MD     │   + 向量化       │
├──────────────┴──────────────┼──────────────┼─────────────────┤
│        capability_match     │capability_invoke              │
│        向量匹配 API          │ HTTP 调用 Bus API              │
└─────────────────────────────┴────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│                    MCP 工具 (Remote)                          │
├─────────────────────┬────────────────────────────────────────┤
│    OAuth MCP        │        Strata MCP (Shell)              │
│    Streamable HTTP  │        Streamable HTTP                 │
│    Authorization头   │        OwnerID + SessionID 头          │
└─────────────────────┴────────────────────────────────────────┘
```

## 5. LLM Provider 适配

```mermaid
classDiagram
    class Client {
        <<interface>>
        +Chat(ctx, messages, tools) ChatResult
        +StreamChat(ctx, messages, tools) iter.Seq2[*Event, error]
        +Generate(ctx, prompt) string
        +Embedding(ctx, texts) []float64
    }

    class client {
        -cfg *config
        -provider provider
        +Chat()
        +StreamChat()
        +Generate()
        +Embedding()
    }

    class provider {
        <<interface>>
        +Chat()
        +StreamChat()
        +Generate()
        +Embedding()
    }

    class openAIProvider {
        +Chat() → OpenAI API
        +StreamChat() → OpenAI Stream
        +Generate()
        +Embedding()
    }

    class anthropicProvider {
        +Chat() → Anthropic API
        +StreamChat() → Anthropic Stream
        +Generate() → Anthropic API
        +Embedding() → OpenAI API
    }

    class Event {
        +ID string
        +Delta string
        +Think string
        +ToolCalls []ToolCall
        +Done bool
        +Usage *Usage
        +Actions EventActions
    }

    class Runner {
        +Persist(ctx, sessionID, *Event) error
    }

    Client <|.. client
    client --> provider
    provider <|.. openAIProvider
    provider <|.. anthropicProvider

    Event --> Runner : yields to
    Runner --> HistoryStore : appends to
    Runner --> SessionStore : merges state to

    note for openAIProvider "支持: OpenAI / OpenRouter / Ollama / Kimi"
    note for anthropicProvider "Embedding 复用 OpenAI API"
```
