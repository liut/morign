# Morign 核心工作流

## 0. Event 驱动架构

```mermaid
graph LR
    LLM[LLM StreamChat] -->|yields| EV[llm.Event]
    EV --> |delta| CLIENT[Client SSE]
    EV --> |Runner.Persist| RS[Runner]
    RS --> |AppendEvent| HS[HistoryStore]
    RS --> |MergeDelta| SS[SessionStore]
    RS --> |CreateUsageRecord| DB[(PostgreSQL)]
```

- **Event**: 统一的事件结构体 (`llm.Event`)，包含 Delta、Think、ToolCalls、Usage、Actions 等
- **Runner.Persist**: 统一持久化入口，同时处理历史追加、状态合并、用量记录
- **HistoryStore / SessionStore**: 通过适配器模式对接 `stores.Storage`

## 1. Web Chat 完整流程

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant MW as OAuth Middleware
    participant API as api.postChat
    participant Prep as prepareChatRequest
    participant LLM as LLM Client
    participant TE as ToolExecutor
    participant TR as Tool Registry
    participant DB as Database

    Client->>MW: POST /api/chat
    MW->>MW: 从 Cookie/Header 提取 Token
    MW->>API: 注入 OAuth Context

    API->>Prep: prepareChatRequest(ctx, param)
    Prep->>Prep: prepareSystemMessage()
    Note over Prep: 1. SystemPrompt<br/>2. DateInContext<br/>3. SessionID<br/>4. User Info + Memories<br/>5. Tools / KB Docs<br/>6. Channel Prompt

    Prep->>DB: ListHistory(sessionID)
    DB-->>Prep: 历史消息
    Prep->>Prep: 构建 messages[] (sys + history + user)
    Prep-->>API: chatRequest{messages, tools, cs}

    alt 流式 (SSE)
        API->>API: iter.Seq2 yield loop
        loop 工具调用循环 (max 12 iterations)
            API->>LLM: StreamChat → iter.Seq2[*Event, error]
            LLM-->>API: Event{Delta, Think, ToolCalls, Done}
            API->>Client: SSE writeEvent(delta)
            alt 有 ToolCalls
                API->>TE: ExecuteToolCalls(messages, toolCalls)
                loop 每个 ToolCall
                    TE->>TR: Invoke(name, params)
                    alt 内置工具
                        TR->>DB: kb_search / memory_*
                        DB-->>TR: results
                    else MCP 工具
                        TR->>TR: callServerTool (SSE/Streamable)
                    end
                    TR-->>TE: tool result
                end
                TE-->>API: updated messages (含 tool results)
            else 无 ToolCalls
                API->>API: break loop
            end
        end
        API->>Runner: Runner.Persist(sessionID, event) for each Event
        API->>DB: AddHistory + Save
    else 非流式
        API->>TE: ExecuteToolCallLoop(messages, tools, exec)
        loop 工具调用循环
            TE->>LLM: Chat(messages, tools)
            LLM-->>TE: {content, toolCalls, usage}
            alt 有 ToolCalls
                TE->>TE: ExecuteToolCalls(messages, toolCalls)
            else 无 ToolCalls
                TE-->>API: final answer
            end
        end
        API->>Runner: Persist final event
        API->>Client: JSON {text}
    end
```

## 2. Platform Channel 消息流程

```mermaid
sequenceDiagram
    participant User as 平台用户 (微信/飞书)
    participant Platform as 平台服务器
    participant CH as Channel Handler
    participant Prep as prepareSystemMessage
    participant LLM as LLM Client
    participant TE as ToolExecutor
    participant DB as Database

    User->>Platform: 发送消息
    Platform->>CH: Webhook Callback
    CH->>CH: MessageHandler(p, msg)

    CH->>DB: GetUserWith(userID)
    alt 用户存在
        DB-->>CH: User + OAuth Token
        CH->>CH: ctx = ContextWithUser + OAuthToken
    else 用户不存在
        CH->>CH: 继续（匿名上下文）
    end

    CH->>CH: DetectCommand(content)
    alt 匹配命令 (/reset 等)
        CH->>CH: 执行命令
        CH->>Platform: Reply("会话已重置")
    else 普通消息
        CH->>Prep: buildChatMessagesAndTools()
        Prep-->>CH: messages + tools

        alt 支持 Streaming (如企业微信 WebSocket)
            CH->>CH: handleStreamingReply()
            CH->>Platform: StartStream("正在思考...")
            loop 工具调用循环
                CH->>LLM: StreamChat(messages, tools)
                LLM-->>CH: stream chunks
                CH->>Platform: AppendStream(content)
                alt 有 ToolCalls
                    CH->>TE: ExecuteToolCalls()
                    TE-->>CH: updated messages
                else 无 ToolCalls
                    CH->>CH: break
                end
            end
            CH->>Platform: FinishStream(finalContent)
        else 普通回复
            CH->>CH: handleRegularReply()
            CH->>TE: ExecuteToolCallLoop()
            TE-->>CH: final answer
            CH->>Platform: Reply(answer)
        end

        CH->>DB: AddHistory + Save
    end
```

## 3. CLI Agent 流程

```mermaid
flowchart TD
    A[CLI: morign agent -m 'msg'] --> B[InitDB]
    B --> C[NewLLMClient]
    C --> D[LoadPreset]
    D --> E[NewRegistry<br/>注册内置工具]
    E --> F[LoadServers<br/>加载活跃的 MCP Server]
    F --> G[NewAgent]
    G --> H{BuildSystemMessage}
    H --> I["system prompt + 时间上下文 + tools"]

    I --> J{交互模式?}
    J -->|是| K[REPL Loop]
    J -->|否| L[单次对话]

    K --> M[读取用户输入]
    M --> N{/exit?}
    N -->|是| Z[退出]
    N -->|否| O{Stream?}
    O -->|是| P[StreamChat + Tool Loop]
    O -->|否| Q[Chat + Tool Loop]

    L --> R{Stream?}
    R -->|是| P
    R -->|否| Q

    P --> S[输出结果]
    Q --> S
    S --> M
```

## 4. 工具调用执行流程

```mermaid
flowchart TD
    Start([LLM 返回 ToolCalls]) --> Check{len(toolCalls) > 0?}
    Check -->|否| Done([返回最终回答])
    Check -->|是| Append[追加 Assistant Message<br/>含 tool_calls + thinking]

    Append --> Loop[遍历每个 ToolCall]
    Loop --> Parse[解析 Arguments JSON]
    Parse --> Invoke[Registry.Invoke(name, params)]

    Invoke --> Match{工具类型?}
    Match -->|kb_search| KB[(知识库<br/>pgvector 相似度匹配)]
    Match -->|kb_create| KBCreate[(写入 Document)]
    Match -->|memory_*| Mem[(用户记忆 CRUD<br/>含向量化)]
    Match -->|fetch| Fetch[HTTP GET<br/>HTML→Markdown]
    Match -->|capability_*| Cap[(API 能力匹配<br/>+ HTTP 调用 Bus)]
    Match -->|MCP Server| MCP[远程 MCP 调用<br/>SSE / Streamable HTTP]

    KB --> Result[formatToolResult]
    KBCreate --> Result
    Mem --> Result
    Fetch --> Result
    Cap --> Result
    MCP --> Result

    Result --> AppendTR[追加 Tool Message<br/>role=tool, content=result]
    AppendTR --> Loop

    Loop --> |所有 ToolCall 处理完| BackToLLM[返回 updated messages]
    BackToLLM --> Start
```

## 5. MCP Server 连接生命周期

```mermaid
flowchart LR
    A[启动时<br/>LoadServers] --> B[查询 active MCP Servers]
    B --> C{TransType?}
    C -->|SSE| D[transport.NewSSE]
    C -->|Streamable| E[transport.NewStreamableHTTP]

    D --> F[client.NewClient]
    E --> F

    F --> G[client.Start]
    G --> H[client.Initialize<br/>MCP 协议握手]
    H --> I[client.ListTools]
    I --> J[注册工具到 Registry<br/>前缀: serverName-toolName]

    J --> K{运行时}
    K --> |Tool Call| L[callServerTool]
    L --> M[server.client.CallTool]
    M --> N[convertMCPToolResult]
    N --> K

    K --> |RemoveServer| O[删除 invokers]
    O --> P[过滤 tools 列表]
    P --> Q[client.Close]
```

## 6. 知识库文档导入与向量化流程

```mermaid
flowchart TD
    A[CLI: morign import docs.csv] --> B[读取 CSV 文件]
    B --> C[逐行解析]
    C --> D{Document 存在?}
    D -->|否| E[CreateDocument]
    D -->|是| F[UpdateDocument]
    E --> G[生成 Subject<br/>title + heading + content]
    F --> G
    G --> H[GetEmbedding<br/>调用 LLM Embedding API]
    H --> I[CreateOrUpdate DocVector<br/>存入 corpus_vector_400]
    I --> J[记录 diff log]
    J --> C
    C --> |EOF| K([完成])

    L[CLI: morign embedding -t doc] --> M[查询未向量化文档]
    M --> N[批量 GetEmbedding]
    N --> O[批量 Upsert DocVector]
    O --> P([完成])
```
