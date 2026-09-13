# Morign

AI chat backend with knowledge base Q&A, MCP tools, and OAuth authentication.

## Features

- Import knowledge base documents from a CSV, either with the `import` command or through the keeper-only async import API
- Generate document vectors with the Embedding API
- High-quality Q&A based on vector search, with optional LLM re-rank
- Welcome message and preset messages
- Chat history for conversation (based on Redis)
- RESTful API with Swagger documentation, supporting text/event-stream
- Login with OAuth2 client for a general security provider
- Built-in MCP (Model Context Protocol) tool support
- Multi-channel adapter support (WeCom WebSocket/Webhook, Feishu WebSocket/Webhook)
- Preset config `${VAR}` env var expansion for secrets
- Keeper role guarding write operations and admin endpoints
- Skills support: admin API plus per-channel injection
- Memory with tier decay, reinforcement and forgetting

## Supported Frontend

<details>
 <summary>chatgpt-svelte based on Svelte  ⤸</summary>

 ![chatgpt-svelte](./docs/screen-svelte-s.png)

> https://github.com/liut/chatgpt-svelte

</details>

<details>
 <summary>Calisyn based on Vue.js  ⤸</summary>

 ![calisyn](./docs/screen-web-s.png)

> https://github.com/liut/calisyn

</details>


## APIs

> Full reference: [docs/swagger.yaml](./docs/swagger.yaml), regenerated with `make gen-apidoc`.
> Endpoints marked 🔑 require the keeper role.

### Get session information

<details>
 <summary><code>GET</code> <code><b>/api/session</b></code></summary>

##### Parameters

> None

##### Description

> Returns current login status and user info. If not logged in and `auth` is true, redirect to the URI in `data.uri` for OAuth login.

##### Responses

> | http code     | content-type                      | response                                           |
> |---------------|-----------------------------------|---------------------------------------------------------------------|
> | `200`         | `application/json`        | `{"status":"Success","data":{"auth":false,"user":{...},"keeper":false}}` (logged in)                        |
> | `200`         | `application/json`        | `{"status":"Success","data":{"auth":true,"uri":"/api/auth/login"}}` (not logged in)        |


</details>

### Get user information of that has been verified or signed in

<details>
 <summary><code>GET</code> <code><b>/api/me</b></code></summary>

##### Parameters

> None

##### Responses

> | http code     | content-type                      | response                                           |
> |---------------|-----------------------------------|---------------------------------------------------------------------|
> | `200`         | `application/json`        | `{"data": {"avatar": "", "name": "name", "uid": "uid"}}`                                         |
> | `401`         | `application/json`        | `{"error": "", "message": ""}`                                         |


</details>

### Post chat prompt and return Streaming messages

<details>
 <summary><code>POST</code> <code><b>/api/chat-sse</b></code> or <code><b>/api/chat</b></code> with <code>{stream: true}</code></summary>

##### Parameters

> | name       |  type     | data type      | description                         |
> |------------|-----------|----------------|-------------------------------------|
> | `csid`     |  optional | string       | conversation ID        |
> | `prompt`   |  required | string       | message for ask        |
> | `stream`   |  optional |  bool        | enable event-stream, force on <code><b>/api/chat-sse</b></code>       |


##### Responses

> | http code     | content-type               | response                                           |
> |---------------|----------------------------|----------------------------------------------------|
> | `200`         | `text/event-stream`        | `{"delta": "message fragments", "id": "conversation ID"}`                                          |
> | `401`         | `application/json`        | `{"status": "Unauthorized", "message": ""}`                                         |


</details>

### Corpus import 🔑

CSV upload is asynchronous: `POST /api/corpus/imports` takes a multipart `file` field (UTF-8 CSV, header `title,heading,content`, max 10 MiB) and returns a task in `pending` state; a background worker picks up pending tasks one at a time. Poll `GET /api/corpus/imports` (filter by `status`/`filename`, page with `limit`/`page`, sort with `sort`) or `GET /api/corpus/imports/{id}` for counts and failure details. CSV content is stored with the task and never returned by the read endpoints.

See the swagger file for request/response schemas and status codes.

## Getting started

```bash

# Install dependencies (includes forego for env loading)
make deps

# Or manually
go mod tidy
go install github.com/ddollar/forego@latest

# Create .env from example and configure (update PG/Redis URLs, API keys, etc.)
test -e .env || cp .env.example .env

# Start server with environment variables from .env
forego start

# Or directly (for development)
forego run go run . web

```

### Prepare preset data file in YAML

```yaml
welcome: "Hello, I am your virtual assistant. How can I help you?"
systemPrompt: "You are a helpful assistant."
toolsPrompt: "You will select the appropriate tool based on the user's question and call the tool to solve the problem."

# Channel adapters (WeCom, Feishu) with ${VAR} env var support for secrets
channels:
  wecom:
    enable: true
    mode: websocket
    config:
      bot_id: "${WECOM_BOT_ID}"
      bot_secret: "${WECOM_BOT_SECRET}"

# Custom tool descriptions (optional)
tools:
  kb_search: "Search documents in knowledge base with subject. When faced with unknown or uncertain issues, prioritize consulting the knowledge base."
  kb_create: "Create new document of knowledge base, all parameters are required. Note: Unless the user explicitly requests supplementary content, do not invoke it."
  fetch: "Fetches a URL from the internet and optionally extracts its contents as markdown"
  memory_list: "List all stored memories"
  memory_recall: "Search memories by keyword"
  memory_store: "Store a new memory"
  memory_forget: "Delete a memory by key"
```

- `welcome`: Welcome message displayed to users
- `systemPrompt`: System prompt for AI conversation
- `toolsPrompt`: Instructions for tool usage (used when MCP tools are available)
- `channels`: Channel adapter configurations — secret fields support `${VAR}` env var expansion
- `tools`: Custom tool descriptions (optional, overrides built-in defaults)
  Note: Memory tools (memory_*) are bound to and isolated by the logged-in user identity.

See [data/preset.example.yaml](./data/preset.example.yaml) for a complete example.

### Prepare database

```sql
CREATE USER morign WITH LOGIN PASSWORD 'mydbusersecret';
CREATE DATABASE morign WITH OWNER = morign ENCODING = 'UTF8';
GRANT ALL PRIVILEGES ON DATABASE morign to morign;


\c morign

-- install extension from https://github.com/pgvector/pgvector
CREATE EXTENSION vector;

```

### Command line usage

```plan

USAGE:
   morign [global options] command [command options]

COMMANDS:
   usage, env                         show usage
   initdb                             init database schema
   import                             import documents from a csv
   import-swagger, import-capability  import API capabilities from swagger yaml/json
   export, exportDocs                 export documents to a csv
   embedding, embedding-doc-vec       read prompt documents and embedding
   cleanup-missed                     delete capabilities marked as missed
   agent, llm, chat                   test LLM agent
   web, run                           run a web server
   version, ver                       show build version
   help, h                            Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h  show help

```

Run `./morign <command> --help` for the flags of each command, e.g. `import --diff`, `export --format csv|jsonl` or `import-swagger --mark-missing`.

#### Agent Command

Test LLM functionality from command line:

```bash
# Non-streaming chat
./morign agent -m "hello"

# Streaming chat
./morign agent -m "hello" -s

# Show verbose logs
./morign agent -m "hello" -v
```

Parameters:
- `-m, --message`: message to send (required)
- `-s, --stream`: enable streaming response
- `-v, --verbose`: show logs (disabled by default)
- `-i, --interactive`: interactive REPL mode


### Change settings with environment

#### Show all local settings
```bash
./morign usage
```

```bash
# HTTP server
MORIGN_HTTP_LISTEN=:3002

# optional preset data
MORIGN_PRESET_FILE=./data/preset.yaml

# optional OAuth2 login
MORIGN_AUTH_REQUIRED=true
OAUTH_PREFIX=https://portal.my-company.xyz

# optional proxy
HTTPS_PROXY=socks5://proxy.my-company.xyz:1081
```

#### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MORIGN_PG_STORE_DSN` | postgres://morign@localhost/morign?sslmode=disable | PostgreSQL connection string |
| `MORIGN_REDIS_URI` | redis://localhost:6379/1 | Redis connection string |
| `MORIGN_HTTP_LISTEN` | :5001 | HTTP listen address |
| `MORIGN_AUTH_REQUIRED` | false | Enable authentication |
| `MORIGN_KEEPER_ROLE` | keeper | Role required for write operations |
| `MORIGN_KEEPER_UIDS` | | Comma separated uid list that bypasses the role check |
| `MORIGN_VECTOR_THRESHOLD` | 0.47 | Vector similarity threshold (0.39 - 0.65) |
| `MORIGN_VECTOR_LIMIT` | 6 | Number of vector matches |
| `MORIGN_RERANK_ENABLED` | false | Enable LLM re-rank of capability matches |
| `MORIGN_MAX_LOOP_ITERATIONS` | 12 | Max agent tool call loop iterations |
| `MORIGN_TOOL_RESULT_MAX_CHARS` | 20000 | Max characters per tool result before truncation (0 = unlimited) |

Other switches (skills injection, memory tiers, OAuth, Sentry, ...) are listed by `./morign usage`.

#### Provider Configuration (AI Services)

Each provider requires `API_KEY` and `MODEL`, optional `URL` and `TYPE` for custom endpoints:

| Provider | Purpose | Required Variables |
|----------|---------|-------------------|
| `INTERACT` | Chat/completion | `API_KEY`, `MODEL` |
| `EMBEDDING` | Vector embedding | `API_KEY`, `MODEL` |
| `SUMMARIZE` | Text summarization | `API_KEY`, `MODEL` |

Supported Provider Type: `openai`, `anthropic`, `openrouter`, `ollama`

Example:
```
# Interact provider (supports openai/anthropic/openrouter/ollama)
MORIGN_INTERACT_API_KEY=sk-xxx
MORIGN_INTERACT_MODEL=gpt-4o-mini
MORIGN_INTERACT_TYPE=openai  # optional, default openai

# Using Anthropic
MORIGN_INTERACT_TYPE=anthropic
MORIGN_INTERACT_MODEL=claude-3-5-sonnet

# Embedding provider
MORIGN_EMBEDDING_API_KEY=sk-xxx
MORIGN_EMBEDDING_MODEL=text-embedding-3-small

# Summarize provider
MORIGN_SUMMARIZE_API_KEY=sk-xxx
MORIGN_SUMMARIZE_MODEL=gpt-4o-mini
```

> Tip: Run `./morign usage` to view all current configurations

## The operation steps for generating data.

1. Prepare a CSV file for the corpus document.
2. Import documents.
3. Generate Questions and Answers from documents with Completion.
4. Generate Prompts and vector from QAs with Embedding
5. Done and go to chat

Step 2 can also be done over HTTP with the keeper-only import API: upload the CSV to `POST /api/corpus/imports`, then poll `GET /api/corpus/imports/{id}` until the status is `succeeded` or `failed`.

### CSV template of documents

| title      | heading     | content                                   |
|------------|-------------|-------------------------------------------|
| my company | introduction | A great company stems from a genius idea. |
|            |             |                                           |

```bash
./morign initdb
./morign import mycompany.csv
./morign embedding
```


## Attach frontend resources

1. Go to frontend project directory
2. Build frontend pages and accompanying static resources.
3. Copy them into ./htdocs

Example:

```bash
cd ../Calisyn
npm run build
rsync -a --delete dist/* ../morign/htdocs/
cd -
```

During the development and debugging phase, you can still use with proxy to collaborate with the front-end project.
