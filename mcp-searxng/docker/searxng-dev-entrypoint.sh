#!/bin/sh
# LOCAL DEVELOPMENT ONLY: starts the standalone SearXNG of Dockerfile.dev.
set -eu

# SearXNG refuses to start without a secret key: generate one unless provided.
if [ -z "${SEARXNG_SECRET:-}" ]; then
    SEARXNG_SECRET="$(head -c 32 /dev/urandom | base64 | tr -dc 'a-zA-Z0-9')"
    export SEARXNG_SECRET
fi

exec /usr/local/searxng/entrypoint.sh
