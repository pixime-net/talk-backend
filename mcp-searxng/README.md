# mcp-searxng

MCP server providing web search and web page reading, powered by a [SearXNG](https://docs.searxng.org/) metasearch instance.

The Docker image embeds SearXNG: only the (authenticated) MCP endpoint is meant to be exposed, SearXNG stays internal to the container.

## Tools

### `search`

Search the web through SearXNG and return the results in SearXNG JSON format.

**Input:**

| Field   | Type   | Required | Description                     |
|---------|--------|----------|---------------------------------|
| `query` | string | yes      | Search query, in natural language |

**Output:** SearXNG `/search?format=json` response, with:

- `results` sorted by descending `score` and truncated to `SEARCH_MAX_RESULTS`;
- empty attributes omitted (to save tokens);
- `answers`, `infoboxes`, `suggestions`, `corrections` kept when present.

### `scrape`

Fetch a public web page and return its main content as markdown (scripts, styles, navigation, images… are removed). Long pages are paginated.

**Input:**

| Field         | Type    | Required | Description                                                                |
|---------------|---------|----------|----------------------------------------------------------------------------|
| `url`         | string  | yes      | Absolute `http`/`https` URL of the page                                    |
| `start_index` | integer | no       | Character offset to start from (use `next_index` of the previous call)     |
| `max_chars`   | integer | no       | Maximum characters returned, capped by `SCRAPE_MAX_CHARS`                  |

**Output:**

| Field         | Type    | Description                                                      |
|---------------|---------|------------------------------------------------------------------|
| `url`         | string  | Final URL, after redirects                                       |
| `title`       | string  | Page title (when available)                                      |
| `content`     | string  | Markdown chunk, cut at a paragraph/line/word boundary            |
| `total_chars` | integer | Total length of the markdown document                            |
| `next_index`  | integer | Offset of the next chunk, omitted when the end has been reached  |

Only public addresses can be scraped: loopback, private, link-local… addresses are rejected, including after redirects (SSRF protection — the embedded SearXNG cannot be reached through this tool).

## Environment variables

Copy [.env.example](.env.example) to `.env` for local development.

| Variable                     | Default                  | Description                                                                  |
|------------------------------|--------------------------|------------------------------------------------------------------------------|
| `SEARXNG_URL`                | `http://localhost:8888`  | SearXNG base URL (in Docker: derived from `SEARXNG_PORT`)                    |
| `SEARCH_MAX_RESULTS`         | `10`                     | Number of results returned by `search`                                       |
| `SCRAPE_MAX_CHARS`           | `8000`                   | Characters returned per `scrape` call (upper bound of `max_chars`)           |
| `LOG_LEVEL`                  | `INFO`                   | Log level (`DEBUG`, `INFO`, `WARN`, `ERROR`)                                 |
| `X_API_KEY`                  | —                        | Shared secret expected in the `X-API-Key` header                             |
| `BASE_URL`                   | —                        | Public base URL of this server                                               |
| `OAUTH_AUTHORIZATION_SERVER` | —                        | OAuth 2.0 authorization server URL                                           |
| `OAUTH_AUDIENCE`             | —                        | Expected `aud` claim                                                         |
| `OAUTH_SCOPES`               | —                        | Comma-separated required scopes                                              |
| `HTTP_HOST`                  | `localhost` (`0.0.0.0` in Docker) | HTTP bind address                                                   |
| `HTTP_PORT`                  | `8080`                   | HTTP port                                                                    |
| `HTTP_RATE_LIMIT`            | `50`                     | Requests per second per client IP                                            |
| `HTTP_RATE_BURST`            | `50`                     | Burst capacity per client IP                                                 |
| `HTTP_READ_TIMEOUT`          | `10`                     | Read timeout (seconds)                                                       |
| `HTTP_WRITE_TIMEOUT`         | `30`                     | Write timeout (seconds)                                                      |
| `HTTP_IDLE_TIMEOUT`          | `60`                     | Idle timeout (seconds)                                                       |
| `HTTP_TRUSTED_PROXIES`       | —                        | Trusted proxy IPs/CIDRs for `X-Forwarded-For`                                |

Docker image only (embedded SearXNG):

| Variable               | Default      | Description                                                                         |
|------------------------|--------------|-------------------------------------------------------------------------------------|
| `SEARXNG_PORT`         | `8888`       | Port of the embedded SearXNG                                                        |
| `SEARXNG_BIND_ADDRESS` | `127.0.0.1`  | SearXNG bind address. Keep loopback in production; `0.0.0.0` for debugging only     |
| `SEARXNG_SECRET`       | random       | SearXNG secret key, generated at each start when unset                              |

## Build & Run with Docker

The image is built from the `talk-backend` root (it needs `talk-libs`):

```bash
make dockerize                     # = docker build -f Dockerfile ..  (version from git describe)
```

It runs two processes, supervised by [docker/entrypoint.sh](docker/entrypoint.sh): if one stops, the other is stopped and the container exits with a non-zero status (so restart policies apply). A `HEALTHCHECK` probes both SearXNG (`/healthz`) and the MCP port.

| Port   | Usage                                                                 |
|--------|-----------------------------------------------------------------------|
| `8080` | MCP endpoint (`/mcp`, `/sse`) — the only port to publish              |
| `8888` | SearXNG — internal, listens on loopback unless `SEARXNG_BIND_ADDRESS` is changed |

Production (only MCP is reachable, protected by authentication):

```bash
docker run -d -p 8080:8080 -e X_API_KEY=change-me --restart on-failure mcp-searxng
```

Hardened variant (read-only root filesystem, SearXNG cache in memory):

```bash
docker run -d -p 8080:8080 -e X_API_KEY=change-me --restart on-failure \
  --read-only --tmpfs /var/cache/searxng --tmpfs /tmp mcp-searxng
```

Debug (SearXNG UI also reachable at http://localhost:8888):

```bash
make docker-run
# = docker run --rm -p 8080:8080 -p 8888:8888 -e SEARXNG_BIND_ADDRESS=0.0.0.0 mcp-searxng
```

SearXNG settings are in [docker/settings.yml](docker/settings.yml) (JSON format enabled, limiter disabled since access control is done by the MCP server).

## Development

### Local SearXNG (development only)

[Dockerfile.dev](Dockerfile.dev) builds a **standalone SearXNG instance for local development only**: it lets you run mcp-searxng from your machine (`make run`, `make dev`, debugger) against a real SearXNG. It is **not** meant to be deployed — use [Dockerfile](Dockerfile) (MCP server + embedded SearXNG) in production.

- SearXNG listens on port `8888`, the default `SEARXNG_URL` of mcp-searxng (`http://localhost:8888`): no configuration needed.
- Same SearXNG settings as production ([docker/settings.yml](docker/settings.yml)), web UI available at http://localhost:8888.
- Published on `127.0.0.1` only, no authentication, secret key generated at each start by [docker/searxng-dev-entrypoint.sh](docker/searxng-dev-entrypoint.sh) (unless `SEARXNG_SECRET` is set).

```bash
make searxng-dev        # build and start SearXNG on http://localhost:8888
make dev                # run mcp-searxng against it
make searxng-dev-stop   # stop it (the container is removed)
```

If port 8888 is already taken on your machine, pick another one and point mcp-searxng to it:

```bash
make searxng-dev DEV_SEARXNG_PORT=19000
SEARXNG_URL=http://localhost:19000 make dev   # or set SEARXNG_URL in .env
make searxng-dev-stop
```

### Commands

```bash
make test               # Run unit tests
make test-integration   # Run integration tests (SearXNG via testcontainers, needs Docker)
make vet                # Run go vet
make cover              # Run tests with coverage
make dev                # Hot reload with air
```

## Authentication

This server supports **X-API-Key** and **OAuth 2.0** authentication.  
See [docs/mcp-server-authentication.md](../docs/mcp-server-authentication.md) for configuration details.

## Security

This server includes built-in HTTP security hardening (rate limiting, path filtering, security headers, timeouts).  
See [docs/mcp-server-secured.md](../docs/mcp-server-secured.md) for configuration details.
