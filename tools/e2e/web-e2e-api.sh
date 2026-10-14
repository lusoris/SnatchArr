#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Web end-to-end run against a real API (#34): Postgres, the SnatchArr API and two fakearr
# Sonarr instances (one seeded by the auth setup, one for the UI flow), then Playwright against
# the Vite preview server, which proxies /api to the Go API. `make web-e2e-api` builds the
# binaries and provides Postgres. Needs curl.
set -euo pipefail

: "${SMOKE_DSN:?set SMOKE_DSN to a Postgres DSN}"
: "${SMOKE_API_BIN:?path to the snatcharr binary}"
: "${SMOKE_FAKEARR_BIN:?path to the fakearr binary}"

free_port() {
    local port=$1 tries=0
    while (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; do
        port=$((port + 1))
        tries=$((tries + 1))
        [ "$tries" -lt 50 ] || { echo "web-e2e-api: no free port from $1" >&2; exit 1; }
    done
    echo "$port"
}

SONARR_PORT=$(free_port "${SMOKE_SONARR_PORT:-18989}")
default_flow_port=$((SONARR_PORT >= 18990 ? SONARR_PORT + 1 : 18990))
SONARR_FLOW_PORT=$(free_port "${SMOKE_SONARR_FLOW_PORT:-$default_flow_port}")
API_PORT=$(free_port "${SMOKE_API_PORT:-18080}")
GRPC_PORT=$(free_port "${SMOKE_GRPC_PORT:-19090}")
TOKEN=smoke-worker-token
FAKE_KEY=fake-api-key-0123456789

LOG_DIR=$(mktemp -d)
PIDS=()

cleanup() {
    local code=$? pid
    for pid in "${PIDS[@]}"; do
        if kill "$pid" 2>/dev/null && ! wait "$pid" 2>/dev/null; then
            :
        fi
    done
    if [ "$code" -ne 0 ]; then
        echo "web-e2e-api: FAILED (exit $code); last log lines:" >&2
        if ! tail -n 40 "$LOG_DIR"/*.log >&2; then
            echo "web-e2e-api: no service logs in $LOG_DIR" >&2
        fi
    fi
    rm -rf "$LOG_DIR"
    exit "$code"
}
trap cleanup EXIT

start() {
    local name=$1
    shift
    "$@" >"$LOG_DIR/$name.log" 2>&1 &
    PIDS+=("$!")
}

wait_http() {
    local url=$1 tries=0
    until curl -fsS --max-time 2 -o /dev/null "$url" 2>/dev/null; do
        tries=$((tries + 1))
        [ "$tries" -lt 60 ] || { echo "web-e2e-api: $url not ready after 30 s" >&2; exit 1; }
        sleep 0.5
    done
}

start sonarr "$SMOKE_FAKEARR_BIN" -addr "127.0.0.1:$SONARR_PORT" -kind sonarr -api-key "$FAKE_KEY"
start sonarr_flow "$SMOKE_FAKEARR_BIN" -addr "127.0.0.1:$SONARR_FLOW_PORT" -kind sonarr -api-key "$FAKE_KEY"
# The dev profile keeps the Secure flag on the session cookie, and Playwright's request client drops Secure cookies over plain http://127.0.0.1.
start api env APP_SNATCHARR_SESSION_SECURE=false \
    APP_SNATCHARR_PROFILE=dev APP_DB_DSN="$SMOKE_DSN" APP_SNATCHARR_WORKER_TOKEN="$TOKEN" \
    APP_HTTP_ADDR="127.0.0.1:$API_PORT" APP_GRPC_LISTEN="127.0.0.1:$GRPC_PORT" "$SMOKE_API_BIN"

wait_http "http://127.0.0.1:$SONARR_PORT/healthz"
wait_http "http://127.0.0.1:$SONARR_FLOW_PORT/healthz"
wait_http "http://127.0.0.1:$API_PORT/api/v1/system/status"

cd web
SNATCHARR_E2E_API="http://127.0.0.1:$API_PORT" \
SNATCHARR_E2E_FAKEARR="http://127.0.0.1:$SONARR_PORT" \
SNATCHARR_E2E_FAKEARR_FLOW="http://127.0.0.1:$SONARR_FLOW_PORT" \
pnpm exec playwright test "$@"
