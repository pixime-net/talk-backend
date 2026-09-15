# talk

`talk` is a Go module that exposes two LLM interaction surfaces built on a shared conversation engine:

- **CLI** (`talk-cli`) — an interactive REPL for multi-turn conversations from the terminal
- **AG-UI HTTP server** (`talk serve`) — an SSE-based HTTP server implementing the [AG-UI protocol](https://github.com/ag-ui-protocol/ag-ui) for web frontends

Both surfaces share the same LLM router, MCP tool manager, SQLite session store, and observability pipeline.

---

## Quickstart

```bash
cp .env.example .env
$EDITOR .env        # set at least one provider API key
make build
```

### CLI

```bash
make run                          # default model: haiku-4.5
make run MODEL=sonnet-4.6
make run MODEL=sonnet-4.6 SYSTEM_FILE=./my_prompt.md
```

### AG-UI HTTP server

```bash
make serve                        # listens on :8090
make serve-dev                    # with hot-reload (requires air)
```

The server exposes a single endpoint:

```
POST /agent
```

It accepts an `RunAgentInput` JSON body (AG-UI protocol) and streams `text/event-stream` AG-UI events back to the caller. The model alias is resolved per request from the forwarded properties of the AG-UI input.

---

## Available models

| Alias           | Provider  | API                  | Notes          |
| --------------- | --------- | -------------------- | -------------- |
| `haiku-4.5`     | Anthropic | Anthropic SDK        | Fast, low cost |
| `sonnet-4.6`    | Anthropic | Anthropic SDK        | Balanced       |
| `gpt-5.4`       | OpenAI    | OpenAI-compatible    |                |
| `mistral-small` | Mistral   | OpenAI-compatible    |                |

---

## CLI commands

Once in the REPL:

| Command                             | Description                      |
| ----------------------------------- | -------------------------------- |
| `/help`                             | Show available commands          |
| `/model`                            | Switch LLM model                 |
| `/thinking [off\|low\|medium\|high]` | Set reasoning effort             |
| `/memory`                           | Show session history             |
| `/session [list\|new\|remove]`       | Manage sessions                  |
| `/prompt`                           | Show current system prompt       |
| `/mcp [list\|add\|remove\|refresh]`  | Manage MCP tool servers          |
| `/q`                                | Quit                             |

---

## Environment variables

Copy `.env.example` to `.env` and fill in the relevant keys:

| Variable                 | Required | Default                      | Description                                                                   |
| ------------------------ | -------- | ---------------------------- | ----------------------------------------------------------------------------- |
| `ANTHROPIC_API_KEY`      | yes*     | —                            | Anthropic API key                                                             |
| `OPENAI_API_KEY`         | yes*     | —                            | OpenAI API key                                                                |
| `MISTRAL_API_KEY`        | yes*     | —                            | Mistral API key                                                               |
| `TOOLS_MAX_CONCURRENT`   | optional | `4`                          | Max concurrent tool executions (`1` = sequential)                             |
| `CONTEXT_FULL_TURNS`     | optional | `0`                          | Context mode: `-1` full, `0` lean, `N>0` hybrid with last `N` detailed turns |
| `SERVE_PORT`             | optional | `8090`                       | HTTP server port (`talk serve`)                                               |
| `CORS_ALLOW_ORIGIN`      | optional | `*`                          | Allowed CORS origins (`talk serve`)                                           |
| `CORS_ALLOW_HEADERS`     | optional | `Content-Type, Authorization`| Allowed CORS headers (`talk serve`)                                           |
| `LANGFUSE_PUBLIC_KEY`    | optional | —                            | Langfuse public key                                                           |
| `LANGFUSE_SECRET_KEY`    | optional | —                            | Langfuse secret key                                                           |
| `LANGFUSE_BASE_URL`      | optional | `https://cloud.langfuse.com` | Langfuse base URL (EU cloud default)                                          |
| `CONSOLE_USAGE_REPORTER` | optional | `true`                       | Enable/disable console cost reporter                                          |

*At least one provider key is required depending on the model used.

---

## Make targets

| Target          | Description                                                    |
| --------------- | -------------------------------------------------------------- |
| `make build`    | Build the `talk-cli` binary into `bin/`                        |
| `make run`      | Run the CLI (default: `MODEL=haiku-4.5`)                       |
| `make serve`    | Build and start the AG-UI server (default port: `8090`)        |
| `make serve-dev`| Start the AG-UI server with hot-reload (requires `air`)        |
| `make test`     | Run tests                                                      |
| `make cover`    | Run tests with coverage                                        |
| `make vet`      | Run `go vet`                                                   |
| `make clean`    | Remove build artifacts                                         |
| `make all`      | Run vet, test, and build                                       |

---

## System prompt

Both surfaces load `system_prompt.md` from the working directory by default. Override with `--system-file`:

```bash
go run ./cmd/cli --model sonnet-4.6 --system-file /path/to/prompt.md
```

---

## MCP tool servers

From the CLI REPL, register a running MCP server:

```
/mcp add
Server name: owm
Server URL: http://localhost:8080
Auth type [none/apikey/oauth] (default: apikey): none
```

Registered servers persist in the SQLite store and are automatically reconnected on startup (both CLI and AG-UI server).

---

## Observability

Token usage and latency are reported in parallel through a `MessageEventHandler` pipeline:

- **Console reporter** — prints cost and token count per turn (disable with `CONSOLE_USAGE_REPORTER=false`)
- **Langfuse** — sends traces and usage via OpenTelemetry OTLP/HTTP (enabled when `LANGFUSE_PUBLIC_KEY` is set)

See [`../docs/langfuse.md`](../docs/langfuse.md) for setup details.

---

## Profiling

Enable the built-in pprof server with `--pprof`:

```bash
go run ./cmd/cli --model sonnet-4.6 --pprof
```

Profiling endpoint starts on `localhost:6060`:

```bash
# Memory allocations
go tool pprof -http=:8081 http://localhost:6060/debug/pprof/heap

# CPU profile (30-second sample)
go tool pprof -http=:8081 http://localhost:6060/debug/pprof/profile?seconds=30
```

> Requires [Graphviz](https://graphviz.org/) for the Graph view. The Flame Graph view works without it.

---

## Project structure

```
talk/
├── cmd/cli/                    # Entry point: CLI REPL, serve command, commands
├── internal/
│   ├── agui/                   # AG-UI protocol handler, SSE writer, event emitter
│   ├── config/                 # .env loader
│   ├── domain/                 # Conversation engine, Message, Model, ToolExecutor
│   ├── helpers/                # Shared utilities
│   ├── llm/
│   │   ├── anthropic/          # Anthropic SDK client
│   │   ├── openai/             # OpenAI-compatible client (OpenAI, Mistral, …)
│   │   └── router/             # Model alias → LlmClient router
│   ├── mcp/                    # MCP server manager and SQLite registry
│   ├── memory/                 # SQLite session and message store
│   ├── prompt/                 # File-based and static prompt providers
│   └── observability/           # Console reporter, Langfuse / OTLP reporter
├── system_prompt.md            # Default system prompt
├── .env.example                # Environment variables template
├── Makefile
└── go.mod
```
