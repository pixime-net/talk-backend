#!/bin/sh
# Runs SearXNG and mcp-searxng side by side in the same container.
# When either process stops, the other one is stopped too and the container
# exits with the status of the first one, so the orchestrator can restart it.
set -eu

: "${SEARXNG_PORT:=8888}"
# SearXNG listens on loopback only by default: it is reachable by mcp-searxng
# but not from outside the container, even if its port is published.
: "${SEARXNG_BIND_ADDRESS:=127.0.0.1}"
: "${SEARXNG_URL:=http://127.0.0.1:${SEARXNG_PORT}}"
export SEARXNG_PORT SEARXNG_URL
export GRANIAN_HOST="$SEARXNG_BIND_ADDRESS" GRANIAN_PORT="$SEARXNG_PORT"

if [ -z "${SEARXNG_SECRET:-}" ]; then
    SEARXNG_SECRET="$(head -c 32 /dev/urandom | base64 | tr -dc 'a-zA-Z0-9')"
    export SEARXNG_SECRET
fi

/usr/local/searxng/entrypoint.sh &
searxng_pid=$!

mcp-searxng --transport http &
mcp_pid=$!

stopping=false
stop_all() {
    kill -TERM "$searxng_pid" "$mcp_pid" 2>/dev/null || true
}
trap 'stopping=true; stop_all' INT TERM

# "wait" on a short sleep lets the shell reap exited children (so kill -0
# fails for them) and run the trap as soon as a signal arrives.
while kill -0 "$searxng_pid" 2>/dev/null && kill -0 "$mcp_pid" 2>/dev/null; do
    sleep 1 &
    wait $! || true
done

status=0
if kill -0 "$mcp_pid" 2>/dev/null; then
    wait "$searxng_pid" || status=$?
    echo "searxng exited with status $status, stopping mcp-searxng" >&2
else
    wait "$mcp_pid" || status=$?
    echo "mcp-searxng exited with status $status, stopping searxng" >&2
fi

stop_all
wait || true

# An unexpected stop must look like a failure so that restart policies apply,
# even if the process exited cleanly.
if [ "$stopping" = false ] && [ "$status" -eq 0 ]; then
    status=1
fi
exit "$status"
