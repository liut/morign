# Morign 系统架构文档

## 1. 架构总览

```mermaid
graph TB
    subgraph "入口层"
        CLI[CLI Commands<br/>web / agent / initdb / import]
        HTTP[HTTP Server<br/>chi/v5]
    end

    subgraph "Web 层 (pkg/web)"
        Router[Route Registry<br/>pkg/web/routes]
        API[API Handlers<br/>pkg/web/api]
        Auth[Auth Middleware<br/>OAuth2 / Cookie]
        Resp[Response Helpers<br/>pkg/web/resp]
    end

    subgraph "服务层 (pkg/services)"
        LLM[LLM Client<br/>OpenAI / Anthropic / OpenRouter / Ollama]
        Event[Event 驱动<br/>llm.Event + Runner.Persist]
        ToolReg[Tool Registry<br/>MCP + Built-in Tools]
        Stores[Data Stores<br/>PostgreSQL / Redis]
        Channels[Platform Channels<br/>WeCom / Feishu]
    end

    subgraph "模型层 (pkg/models)"
        Convo[convo<br/>Session / Message / User / Memory]
        Corpus[corpus<br/>Document / DocVector / ChatLog]
        MCPS[mcps<br/>Server / Tool]
        Capability[capability<br/>API Capability / Vector]
        Channel[channel<br/>Message / Attachment]
        AIGC[aigc<br/>Preset / History]
    end

    subgraph "基础设施"
        PG[(PostgreSQL<br/>+ pgvector)]
        Redis[(Redis<br/>Session / Rate Limit)]
        OAuth[OAuth2 Provider<br/>Staffio]
        MCP_EXT[External MCP Servers<br/>SSE / Streamable HTTP]
    end

    CLI --> |"web 命令"| HTTP
    CLI --> |"agent 命令"| LLM
    CLI --> |"数据命令"| Stores

    HTTP --> Router
    Router --> Auth
    Router --> API
    API --> Resp

    API --> LLM
    API --> ToolReg
    API --> Stores
    API --> Channels

    ToolReg --> MCP_EXT
    ToolReg --> Stores

    Stores --> PG
    Stores --> Redis
    Auth --> OAuth
    Auth --> Redis

    LLM --> |Embedding| PG
```

## 2. 分层架构

```
┌─────────────────────────────────────────────────────┐
│                    main.go                          │
│         CLI Entry (urfave/cli) + Web Server          │
├─────────────────────────────────────────────────────┤
│                 Web Layer (pkg/web)                  │
│  ┌──────────┐ ┌──────────┐ ┌────────┐ ┌─────────┐  │
│  │  server  │ │  routes  │ │  api   │ │  resp   │  │
│  │ chi mux  │ │ registry │ │handler │ │ helpers │  │
│  └──────────┘ └──────────┘ └────────┘ └─────────┘  │
├─────────────────────────────────────────────────────┤
│              Service Layer (pkg/services)            │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────┐  │
│  │   llm    │ │  event   │ │  runner  │ │channels│  │
│  │ client   │ │ Event驱动 │ │ Persist  │ │ wecom  │  │
│  │ provider │ │          │ │统一持久化 │ │ feishu │  │
│  └──────────┘ └──────────┘ └──────────┘ └───────┘  │
├─────────────────────────────────────────────────────┤
│               Model Layer (pkg/models)               │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────┐  │
│  │  convo   │ │  corpus  │ │   mcps   │ │ aigc  │  │
│  │ session  │ │ document │ │  server  │ │ preset │  │
│  │ message  │ │ docVector│ │  tool    │ │history │  │
│  │ memory   │ │ chatLog  │ │          │ │ match  │  │
│  │ user     │ │          │ │          │ │       │  │
│  └──────────┘ └──────────┘ └──────────┘ └───────┘  │
│  ┌──────────┐ ┌──────────┐                          │
│  │capability│ │ channel  │                          │
│  │ api spec │ │ message  │                          │
│  │ vector   │ │          │                          │
│  └──────────┘ └──────────┘                          │
├─────────────────────────────────────────────────────┤
│           Infrastructure (pkg/settings)              │
│  ┌──────────┐ ┌──────────┐ ┌──────────────────┐    │
│  │PostgreSQL│ │  Redis   │ │ External Services │    │
│  │+pgvector │ │ cache    │ │ OAuth / MCP / LLM│    │
│  └──────────┘ └──────────┘ └──────────────────┘    │
└─────────────────────────────────────────────────────┘
```

## 3. 路由注册机制

项目使用插件式路由注册：各业务包通过 `init()` 调用 `routes.Register()` 注册自己的路由挂载函数，服务启动时 `routes.Routers()` 按名称排序统一挂载。

```mermaid
sequenceDiagram
    participant PKG as 业务包 (api/ etc.)
    participant REG as routes.Registry
    participant SRV as server.strapRouter
    participant CHI as chi.Mux

    PKG->>REG: init() → Register("api", StrapFunc(strap))
    Note over REG: 存入 map[name]Strapper

    SRV->>REG: Routers(chiRouter)
    REG->>REG: 按名称排序
    loop 遍历注册项
        REG->>PKG: sf.Strap(chiRouter)
        PKG->>CHI: 注册具体路由
    end
```
