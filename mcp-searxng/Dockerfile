# syntax=docker/dockerfile:1
# Self-contained image: mcp-searxng + the SearXNG instance it queries.
# Build from the talk-backend root (see `make dockerize`):
#   docker build -f mcp-searxng/Dockerfile -t mcp-searxng .
# The version is derived from git (git describe), like the other MCP servers.

ARG SEARXNG_IMAGE=docker.io/searxng/searxng:2026.10.28-c360868e5

FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY .git/ ./.git/
COPY talk-libs/ ./talk-libs/
COPY mcp-searxng/ ./mcp-searxng/

WORKDIR /src/mcp-searxng

ENV GOWORK=off \
    CGO_ENABLED=0

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev") && \
    go build -trimpath -buildvcs=false \
        -ldflags "-s -w -X github.com/pixime-net/talk-libs/version.Version=${VERSION}" \
        -o /out/mcp-searxng \
        ./cmd

FROM ${SEARXNG_IMAGE}

COPY --from=builder --chmod=0755 /out/mcp-searxng /usr/local/bin/mcp-searxng
COPY --chmod=0755 mcp-searxng/docker/entrypoint.sh /usr/local/bin/mcp-searxng-entrypoint.sh
COPY --chmod=0444 mcp-searxng/docker/settings.yml /etc/searxng/settings.yml

USER searxng

# HTTP_* configure mcp-searxng; SEARXNG_* configure the embedded SearXNG.
# SEARXNG_BIND_ADDRESS=127.0.0.1 keeps SearXNG internal: set it to 0.0.0.0 and
# publish SEARXNG_PORT only to reach SearXNG directly while debugging.
ENV HTTP_HOST=0.0.0.0 \
    HTTP_PORT=8080 \
    SEARXNG_PORT=8888 \
    SEARXNG_BIND_ADDRESS=127.0.0.1

# 8080: MCP endpoint (public). 8888: SearXNG (internal, debug only).
EXPOSE 8080 8888

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["python3", "-c", "import os, socket, urllib.request; urllib.request.urlopen('http://127.0.0.1:%s/healthz' % os.environ['SEARXNG_PORT'], timeout=3); socket.create_connection(('127.0.0.1', int(os.environ['HTTP_PORT'])), timeout=3).close()"]

ENTRYPOINT ["/usr/local/bin/mcp-searxng-entrypoint.sh"]
