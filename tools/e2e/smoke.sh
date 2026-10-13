#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# End-to-end smoke test (#19): Postgres, the API, two workers and three fakearr instances
# (Sonarr, Radarr and a Sonarr searched by whole series). It drives the API like a user and
# proves that lease, stamina and dispatch work together:
#   a. each instance searches exactly its hourly cap, although every snatch wants more;
#   b. /hourly-caps reports what fakearr actually received, per instance;
#   c. both workers leased runs;
#   d. no item is searched twice (afterglow);
#   e. searches happened at all (the checks above are not vacuous);
#   f. a whole-series search that does not fit the cap is not sent and costs no stamina; once
#      the cap fits it, it is sent once and costs exactly its seasons (#142);
#   g. each worker's health listener answers /livez with 200 and /metrics with the API failure
#      counter (A1-01).
# `make e2e-smoke` builds the binaries and provides Postgres. Needs curl and jq.
set -euo pipefail

: "${SMOKE_DSN:?set SMOKE_DSN to a Postgres DSN (make e2e-smoke does)}"
: "${SMOKE_API_BIN:?path to the snatcharr binary}"
: "${SMOKE_WORKER_BIN:?path to the snatch-worker binary}"
: "${SMOKE_FAKEARR_BIN:?path to the fakearr binary}"

# free_port START: the first port from START on which nothing listens (at most 50 tried).
free_port() {
    local port=$1 tries=0
    while (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; do
        port=$((port + 1))
        tries=$((tries + 1))
        [ "$tries" -lt 50 ] || { echo "smoke: no free port from $1" >&2; exit 1; }
    done
    echo "$port"
}
SONARR_PORT=$(free_port "${SMOKE_SONARR_PORT:-18989}")
SONARR_SERIES_PORT=$(free_port "${SMOKE_SONARR_SERIES_PORT:-18990}")
RADARR_PORT=$(free_port "${SMOKE_RADARR_PORT:-17878}")
API_PORT=$(free_port "${SMOKE_API_PORT:-18080}")
GRPC_PORT=$(free_port "${SMOKE_GRPC_PORT:-19090}")
# Two workers on one host: each needs its own health listener port.
W1_HTTP_PORT=$(free_port "${SMOKE_WORKER_HTTP_PORT:-18081}")
W2_HTTP_PORT=$(free_port "$((W1_HTTP_PORT + 1))")
CAP=3       # hourly cap per instance
PER_CYCLE=5 # items one snatch wants: more than the cap, so the cap binds
ROUNDS=3    # run triggers per instance
SERIES_COST=3 # seasons of the one series the whole-series instance wants: its search cost
API="http://127.0.0.1:$API_PORT/api/v1"
FAKE_KEY=fake-api-key-0123456789
TOKEN=smoke-worker-token

LOG_DIR=$(mktemp -d)
PIDS=()

cleanup() {
    local code=$? pid
    for pid in "${PIDS[@]}"; do
        # A service that already exited cannot be signalled; a signalled one exits non-zero.
        if kill "$pid" 2>/dev/null && ! wait "$pid" 2>/dev/null; then
            : # expected: it ended on our signal
        fi
    done
    if [ "$code" -ne 0 ]; then
        echo "smoke: FAILED (exit $code); last log lines:" >&2
        if ! tail -n 40 "$LOG_DIR"/*.log >&2; then
            echo "smoke: no service logs in $LOG_DIR" >&2
        fi
    fi
    rm -rf "$LOG_DIR"
    exit "$code"
}
trap cleanup EXIT

fail() {
    echo "smoke: assertion failed: $*" >&2
    exit 1
}

# start NAME COMMAND...: run a service in the background, logging to $LOG_DIR/NAME.log.
start() {
    local name=$1
    shift
    "$@" >"$LOG_DIR/$name.log" 2>&1 &
    PIDS+=("$!")
}

# wait_http URL: poll until URL answers 2xx, at most 60 times, 0.5 s apart.
wait_http() {
    local url=$1 tries=0
    until curl -fsS --max-time 2 -o /dev/null "$url" 2>/dev/null; do
        tries=$((tries + 1))
        [ "$tries" -lt 60 ] || fail "$url not ready after 30 s"
        sleep 0.5
    done
}

JAR="$LOG_DIR/cookies.txt"
CSRF=()
# api METHOD PATH [JSON]: one authenticated API call; non-2xx fails the test.
api() {
    local method=$1 path=$2
    local args=(-X "$method" --cookie "$JAR" --cookie-jar "$JAR" "${CSRF[@]}")
    if [ $# -ge 3 ]; then
        args+=(-H 'Content-Type: application/json' -d "$3")
    fi
    curl -fsS --max-time 10 "${args[@]}" "$API$path"
}

# searched PORT: item ids fakearr on PORT was asked to search, one per line.
searched() {
    curl -fsS --max-time 10 "http://127.0.0.1:$1/_fake/commands" |
        jq -r '(. // [])[] | .body | (.episodeIds // .movieIds // .albumIds // .bookIds // [])[]'
}

start sonarr "$SMOKE_FAKEARR_BIN" -addr "127.0.0.1:$SONARR_PORT" -kind sonarr -api-key "$FAKE_KEY"
start radarr "$SMOKE_FAKEARR_BIN" -addr "127.0.0.1:$RADARR_PORT" -kind radarr -api-key "$FAKE_KEY"
# Five missing episodes: all of one series, spread over SERIES_COST seasons.
start sonarr_series "$SMOKE_FAKEARR_BIN" -addr "127.0.0.1:$SONARR_SERIES_PORT" -kind sonarr -api-key "$FAKE_KEY" -missing 5
# No APP_CRYPTO_KEY: the dev profile must start on its development key (#138).
start api env APP_SNATCHARR_PROFILE=dev APP_DB_DSN="$SMOKE_DSN" APP_SNATCHARR_WORKER_TOKEN="$TOKEN" \
    APP_HTTP_ADDR="127.0.0.1:$API_PORT" APP_GRPC_LISTEN="127.0.0.1:$GRPC_PORT" "$SMOKE_API_BIN"
wait_http "http://127.0.0.1:$SONARR_PORT/healthz"
wait_http "http://127.0.0.1:$RADARR_PORT/healthz"
wait_http "http://127.0.0.1:$SONARR_SERIES_PORT/healthz"
wait_http "$API/system/status"
start w1 env SNATCH_WORKER_ID=w1 SNATCH_API_GRPC="http://127.0.0.1:$GRPC_PORT" \
    SNATCH_WORKER_TOKEN="$TOKEN" SNATCH_CONCURRENCY=1 SNATCH_LEASE_WAIT_SECS=5 \
    SNATCH_HTTP_ADDR="127.0.0.1:$W1_HTTP_PORT" "$SMOKE_WORKER_BIN"
start w2 env SNATCH_WORKER_ID=w2 SNATCH_API_GRPC="http://127.0.0.1:$GRPC_PORT" \
    SNATCH_WORKER_TOKEN="$TOKEN" SNATCH_CONCURRENCY=1 SNATCH_LEASE_WAIT_SECS=5 \
    SNATCH_HTTP_ADDR="127.0.0.1:$W2_HTTP_PORT" "$SMOKE_WORKER_BIN"

# Setup wizard, then every call carries the session cookie and the CSRF token.
token=$(api POST /auth/setup '{"username":"admin","password":"smoke-password-1234"}' | jq -r '.csrf_token // empty')
[ -n "$token" ] || fail "setup returned no csrf_token"
CSRF=(-H "X-CSRF-Token: $token")

# instance KIND PORT [NAME [POLICY]]: create an instance on fakearr and change its policy with
# the jq filter POLICY (default: the smoke cap and per-cycle count); prints its id.
instance() {
    local kind=$1 port=$2 name=${3:-smoke-$1} id policy
    local filter=${4:-".hourly_cap = $CAP | .missing_per_cycle = $PER_CYCLE"}
    id=$(api POST /instances "{\"kind\":\"$kind\",\"name\":\"$name\",\"base_url\":\"http://127.0.0.1:$port\",\"api_key\":\"$FAKE_KEY\",\"enabled\":true}" | jq -r '.id // empty')
    [ -n "$id" ] || fail "no id for the $name instance"
    policy=$(api GET "/instances/$id/policy" | jq -c "$filter")
    api PUT "/instances/$id/policy" "$policy" >/dev/null
    echo "$id"
}
sonarr_id=$(instance sonarr "$SONARR_PORT")
radarr_id=$(instance radarr "$RADARR_PORT")
# One stamina short of the series, so its search has to wait.
series_id=$(instance sonarr "$SONARR_SERIES_PORT" smoke-sonarr-series \
    ".hourly_cap = $((SERIES_COST - 1)) | .missing_per_cycle = $PER_CYCLE | .sonarr_missing_mode = \"shows\"")

# active ID: runs of the instance that are queued or leased.
active() {
    api GET "/runs?instance_id=$1" | jq '[(. // [])[] | select(.status == "queued" or .status == "leased")] | length'
}

# settle ID...: wait until none of the instances has a queued or leased run, at most 60 s.
settle() {
    local tries=0 id busy=1
    while [ "$busy" -eq 1 ]; do
        busy=0
        for id in "$@"; do
            [ "$(active "$id")" -eq 0 ] || busy=1
        done
        tries=$((tries + 1))
        [ "$busy" -eq 0 ] || [ "$tries" -lt 60 ] || fail "runs still active after 60 s"
        [ "$busy" -eq 0 ] || sleep 1
    done
}

# Trigger the instances together so both workers have a run to lease, then let them finish.
for _ in $(seq 1 "$ROUNDS"); do
    api POST "/instances/$sonarr_id/runs" '{"kind":"missing"}' >/dev/null
    api POST "/instances/$radarr_id/runs" '{"kind":"missing"}' >/dev/null
    api POST "/instances/$series_id/runs" '{"kind":"missing"}' >/dev/null
    settle "$sonarr_id" "$radarr_id" "$series_id"
done

caps=$(api GET /hourly-caps)
check_instance() {
    local name=$1 id=$2 port=$3 ids count unique used
    ids=$(searched "$port")
    # sed, not grep: no searches at all must reach the assertion, not trip pipefail.
    count=$(printf '%s\n' "$ids" | sed '/^$/d' | wc -l)
    unique=$(printf '%s\n' "$ids" | sed '/^$/d' | sort -u | wc -l)
    used=$(printf '%s' "$caps" | jq --arg id "$id" '[(. // [])[] | select(.instance_id == $id) | .used] | add // 0')
    [ "$count" -eq "$CAP" ] || fail "a. $name searched $count items, want exactly the cap $CAP (each snatch wants $PER_CYCLE)"
    [ "$used" -eq "$count" ] || fail "b. /hourly-caps reports $used used for $name, fakearr received $count"
    [ "$unique" -eq "$count" ] || fail "d. $name searched an item twice ($count searches, $unique distinct)"
    echo "smoke: $name searched $count of cap $CAP; API reports $used used; no repeats"
}
check_instance sonarr "$sonarr_id" "$SONARR_PORT"
check_instance radarr "$radarr_id" "$RADARR_PORT"

# used ID: stamina /hourly-caps reports as spent by the instance, read now.
used() {
    api GET /hourly-caps | jq --arg id "$1" '[(. // [])[] | select(.instance_id == $id) | .used] | add // 0'
}

# commands PORT: names of the search commands fakearr on PORT received, one per line.
commands() {
    curl -fsS --max-time 10 "http://127.0.0.1:$1/_fake/commands" | jq -r '(. // [])[] | .name'
}

# check_series WANT_COMMANDS WANT_USED WHY: what the whole-series fakearr received and what
# the API charged for it.
check_series() {
    local names count spent
    names=$(commands "$SONARR_SERIES_PORT")
    count=$(printf '%s\n' "$names" | sed '/^$/d' | wc -l)
    spent=$(used "$series_id")
    [ "$count" -eq "$1" ] || fail "f. the series instance received $count command(s), want $1: $3"
    [ "$spent" -eq "$2" ] || fail "f. /hourly-caps reports $spent used for the series instance, want $2: $3"
    [ "$count" -eq 0 ] || [ "$(printf '%s\n' "$names" | sort -u)" = SeriesSearch ] ||
        fail "f. the series instance received $names, want SeriesSearch only"
    echo "smoke: series instance received $count command(s); API reports $spent used ($3)"
}
check_series 0 0 "a series of $SERIES_COST seasons does not fit a cap of $((SERIES_COST - 1)) and waits for free"

# The same series once the cap fits it: one command, charged its cost, and that spends the cap.
policy=$(api GET "/instances/$series_id/policy" | jq -c ".hourly_cap = $SERIES_COST")
api PUT "/instances/$series_id/policy" "$policy" >/dev/null
for _ in 1 2; do
    api POST "/instances/$series_id/runs" '{"kind":"missing"}' >/dev/null
    settle "$series_id"
done
check_series 1 "$SERIES_COST" "a cap of $SERIES_COST fits the series exactly once"

runs=$(api GET "/runs?instance_id=$sonarr_id")$(api GET "/runs?instance_id=$radarr_id")$(api GET "/runs?instance_id=$series_id")
for worker in w1 w2; do
    leases=$(printf '%s' "$runs" | jq -s --arg w "$worker" '[.[][] | select(.leased_by == $w)] | length')
    [ "$leases" -gt 0 ] || fail "c. worker $worker leased no run"
    echo "smoke: $worker leased $leases run(s)"
done
for port in "$W1_HTTP_PORT" "$W2_HTTP_PORT"; do
    live=$(curl -fsS --max-time 5 "http://127.0.0.1:$port/livez") || fail "g. worker on $port: /livez did not answer 200"
    case "$live" in
        "ok: last successful API call "*) ;;
        *) fail "g. worker on $port: /livez says '$live'" ;;
    esac
    metrics=$(curl -fsS --max-time 5 "http://127.0.0.1:$port/metrics") || fail "g. worker on $port: /metrics failed"
    case "$metrics" in
        *'snatcharr_worker_api_failures_total{call="lease_run"} '*) ;;
        *) fail "g. worker on $port: /metrics has no lease_run failure counter" ;;
    esac
    echo "smoke: worker on $port: $live"
done
echo "smoke: passed"
