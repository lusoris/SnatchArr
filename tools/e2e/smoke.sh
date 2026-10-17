#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# End-to-end smoke test (#19): Postgres, the API, two workers and four fakearr instances
# (Sonarr, Radarr, a Sonarr searched by whole series, and a Radarr for cutoff upgrades). It
# drives the API like a user and proves that lease, stamina and dispatch work together:
#   a. each instance searches exactly its hourly cap, although every snatch wants more;
#   b. /hourly-caps reports what fakearr actually received, per instance;
#   c. both workers leased runs;
#   d. no item is searched twice (afterglow);
#   e. searches happened at all (the checks above are not vacuous);
#   f. a whole-series search that does not fit the cap is not sent and costs no stamina; once
#      the cap fits it, it is sent once and costs exactly its seasons (#142);
#   g. each worker's health listener answers /livez with 200 and /metrics with the API failure
#      counter, and /system/status reports the workers' last contact (A1-01);
#   h. an upgrade snatch searches only cutoff-unmet items up to its cap, records stamina,
#      avoids repeats, and records kind upgrade on completed runs (C10-01);
#   i. with images: each container runs under the memory limit of the chart, and its peak
#      stays at or below 80 % of that limit (C12-01, tools/e2e/mem_limits.sh);
#   j. with SOAK_SECONDS: rounds of runs for that long, with no failed worker API call, no
#      failed run and fresh worker contact in any round (C14-01, `make e2e-soak`).
# `make e2e-smoke` builds the binaries and provides Postgres. Needs curl and jq.
set -euo pipefail

: "${SMOKE_DSN:?set SMOKE_DSN to a Postgres DSN (make e2e-smoke does)}"
: "${SMOKE_FAKEARR_BIN:?path to the fakearr binary}"
if [ -z "${SMOKE_API_IMAGE:-}" ]; then
    : "${SMOKE_API_BIN:?path to the snatcharr binary}"
fi
if [ -z "${SMOKE_WORKER_IMAGE:-}" ]; then
    : "${SMOKE_WORKER_BIN:?path to the snatch-worker binary}"
fi

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
RADARR_UPGRADE_PORT=$(free_port "${SMOKE_RADARR_UPGRADE_PORT:-$((RADARR_PORT + 1))}")
API_PORT=$(free_port "${SMOKE_API_PORT:-18080}")
GRPC_PORT=$(free_port "${SMOKE_GRPC_PORT:-19090}")
# Two workers on one host: each needs its own health listener port.
W1_HTTP_PORT=$(free_port "${SMOKE_WORKER_HTTP_PORT:-$((API_PORT + 1))}")
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
CONTAINERS=()

cleanup() {
    local code=$? pid c
    for c in "${CONTAINERS[@]}"; do
        # --rm containers may already be gone after a stop.
        if docker inspect "$c" >/dev/null 2>&1 && ! docker rm -f "$c" >/dev/null 2>&1; then
            echo "smoke: could not remove container $c" >&2
        fi
    done
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

# check_image_user ROLE IMAGE: verify the distroless image user is 65532.
check_image_user() {
    local role=$1 image=$2 user
    user=$(docker inspect -f '{{.Config.User}}' "$image" 2>/dev/null) || fail "cannot inspect user of $role image $image"
    case "$user" in
        65532 | 65532:65532) ;;
        *) fail "$role image $image user is '$user', want 65532 or 65532:65532" ;;
    esac
    echo "smoke: $role image user is $user"
}
# check_image_ca IMAGE: the api's Go TLS stack reads the image's CA bundle for outbound
# HTTPS (*arr, Seerr, Cleanuparr); the worker embeds webpki roots and needs none.
check_image_ca() {
    local image=$1 c="smoke-api-ca-$$" bundle=/etc/ssl/certs/ca-certificates.crt count
    CONTAINERS+=("$c")
    docker create --name "$c" "$image" >/dev/null 2>&1 || fail "cannot create container from api image $image"
    docker cp "$c:$bundle" "$LOG_DIR/ca.crt" >/dev/null 2>&1 || fail "api image $image has no CA bundle at $bundle"
    count=$(grep -c 'BEGIN CERTIFICATE' "$LOG_DIR/ca.crt") || fail "api image $image CA bundle $bundle holds no certificate"
    echo "smoke: api image CA bundle holds $count certificates"
}
if [ -n "${SMOKE_API_IMAGE:-}" ]; then
    check_image_user api "$SMOKE_API_IMAGE"
    check_image_ca "$SMOKE_API_IMAGE"
fi
if [ -n "${SMOKE_WORKER_IMAGE:-}" ]; then
    check_image_user worker "$SMOKE_WORKER_IMAGE"
fi
MEM="$(dirname "$0")/mem_limits.sh"
if [ -n "${SMOKE_API_IMAGE:-}" ]; then
    API_MEM=$(bash "$MEM" limit api) || fail "i. no memory limit for the api in the chart"
fi
if [ -n "${SMOKE_WORKER_IMAGE:-}" ]; then
    WORKER_MEM=$(bash "$MEM" limit worker) || fail "i. no memory limit for the worker in the chart"
fi

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
start radarr_upgrade "$SMOKE_FAKEARR_BIN" -addr "127.0.0.1:$RADARR_UPGRADE_PORT" -kind radarr -api-key "$FAKE_KEY" -missing 8 -cutoff 8
# Five missing episodes: all of one series, spread over SERIES_COST seasons.
start sonarr_series "$SMOKE_FAKEARR_BIN" -addr "127.0.0.1:$SONARR_SERIES_PORT" -kind sonarr -api-key "$FAKE_KEY" -missing 5
# SMOKE_GRPC_MAX_CONNECTION_AGE shortens the API's gRPC connection age, so the soak (j) sees
# many connection rotations instead of one every 2 minutes. SMOKE_LEASE_WAIT_SECS is the
# workers' lease long-poll: 5 s keeps the checks quick; the soak runs the production 30 s, so
# a long-poll outlives a connection's grace unless the API keeps it to the end.
SMOKE_LEASE_WAIT_SECS=${SMOKE_LEASE_WAIT_SECS:-5}
API_ENV=(APP_SNATCHARR_PROFILE=dev APP_DB_DSN="$SMOKE_DSN" APP_SNATCHARR_WORKER_TOKEN="$TOKEN"
    APP_HTTP_ADDR="127.0.0.1:$API_PORT" APP_GRPC_LISTEN="127.0.0.1:$GRPC_PORT")
if [ -n "${SMOKE_GRPC_MAX_CONNECTION_AGE:-}" ]; then
    API_ENV+=(APP_GRPC_KEEPALIVE_MAX_CONNECTION_AGE="$SMOKE_GRPC_MAX_CONNECTION_AGE")
fi
# No APP_CRYPTO_KEY: the dev profile must start on its development key (#138).
if [ -n "${SMOKE_API_IMAGE:-}" ]; then
    cname_api="smoke-api-$$"
    CONTAINERS+=("$cname_api")
    api_docker_env=()
    for kv in "${API_ENV[@]}"; do
        api_docker_env+=(-e "$kv")
    done
    start api docker run --rm --init --network host --read-only \
        --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges \
        --memory "$API_MEM" --memory-swap "$API_MEM" \
        --name "$cname_api" \
        "${api_docker_env[@]}" \
        "$SMOKE_API_IMAGE"
else
    start api env "${API_ENV[@]}" "$SMOKE_API_BIN"
fi
wait_http "http://127.0.0.1:$SONARR_PORT/healthz"
wait_http "http://127.0.0.1:$RADARR_PORT/healthz"
wait_http "http://127.0.0.1:$RADARR_UPGRADE_PORT/healthz"
wait_http "http://127.0.0.1:$SONARR_SERIES_PORT/healthz"
wait_http "$API/system/status"

if [ -n "${SMOKE_API_IMAGE:-}" ]; then
    spa_headers="$LOG_DIR/spa_headers.txt"
    spa_body="$LOG_DIR/spa_body.html"
    status=$(curl -sS --max-time 10 -D "$spa_headers" -o "$spa_body" -w "%{http_code}" "http://127.0.0.1:$API_PORT/") ||
        fail "SPA root GET failed"
    [ "$status" = "200" ] || fail "SPA root returned HTTP $status, want 200"
    grep -qi '^content-type:[[:space:]]*text/html' "$spa_headers" ||
        fail "SPA root Content-Type is not text/html"
    grep -q 'data-sveltekit-preload-data="hover"' "$spa_body" ||
        fail "SPA root does not contain SvelteKit marker data-sveltekit-preload-data=\"hover\""
    asset_path=$(grep -oE '/_app/immutable/[^"'\'' >]+' "$spa_body" | head -n 1)
    [ -n "$asset_path" ] || fail "SPA root body references no /_app/immutable/ hashed asset"
    asset_status=$(curl -sS --max-time 10 -o /dev/null -w "%{http_code}" "http://127.0.0.1:$API_PORT$asset_path") ||
        fail "failed to GET SPA hashed asset $asset_path"
    [ "$asset_status" = "200" ] || fail "SPA hashed asset $asset_path returned HTTP $asset_status, want 200"
    echo "smoke: SPA root (200, text/html) and hashed asset $asset_path (200) verified"
fi

if [ -n "${SMOKE_WORKER_IMAGE:-}" ]; then
    cname_w1="smoke-w1-$$"
    CONTAINERS+=("$cname_w1")
    start w1 docker run --rm --init --network host --read-only \
        --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges \
        --memory "$WORKER_MEM" --memory-swap "$WORKER_MEM" \
        --name "$cname_w1" \
        -e SNATCH_WORKER_ID=w1 \
        -e SNATCH_API_GRPC="http://127.0.0.1:$GRPC_PORT" \
        -e SNATCH_WORKER_TOKEN="$TOKEN" \
        -e SNATCH_CONCURRENCY=1 \
        -e SNATCH_LEASE_WAIT_SECS="$SMOKE_LEASE_WAIT_SECS" \
        -e SNATCH_HTTP_ADDR="127.0.0.1:$W1_HTTP_PORT" \
        "$SMOKE_WORKER_IMAGE"

    cname_w2="smoke-w2-$$"
    CONTAINERS+=("$cname_w2")
    start w2 docker run --rm --init --network host --read-only \
        --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges \
        --memory "$WORKER_MEM" --memory-swap "$WORKER_MEM" \
        --name "$cname_w2" \
        -e SNATCH_WORKER_ID=w2 \
        -e SNATCH_API_GRPC="http://127.0.0.1:$GRPC_PORT" \
        -e SNATCH_WORKER_TOKEN="$TOKEN" \
        -e SNATCH_CONCURRENCY=1 \
        -e SNATCH_LEASE_WAIT_SECS="$SMOKE_LEASE_WAIT_SECS" \
        -e SNATCH_HTTP_ADDR="127.0.0.1:$W2_HTTP_PORT" \
        "$SMOKE_WORKER_IMAGE"
else
    start w1 env SNATCH_WORKER_ID=w1 SNATCH_API_GRPC="http://127.0.0.1:$GRPC_PORT" \
        SNATCH_WORKER_TOKEN="$TOKEN" SNATCH_CONCURRENCY=1 SNATCH_LEASE_WAIT_SECS="$SMOKE_LEASE_WAIT_SECS" \
        SNATCH_HTTP_ADDR="127.0.0.1:$W1_HTTP_PORT" "$SMOKE_WORKER_BIN"
    start w2 env SNATCH_WORKER_ID=w2 SNATCH_API_GRPC="http://127.0.0.1:$GRPC_PORT" \
        SNATCH_WORKER_TOKEN="$TOKEN" SNATCH_CONCURRENCY=1 SNATCH_LEASE_WAIT_SECS="$SMOKE_LEASE_WAIT_SECS" \
        SNATCH_HTTP_ADDR="127.0.0.1:$W2_HTTP_PORT" "$SMOKE_WORKER_BIN"
fi

# Setup wizard, then every call carries the session cookie and the CSRF token.
token=$(api POST /auth/setup '{"username":"admin","password":"smoke-password-1234"}' | jq -r '.csrf_token // empty')
[ -n "$token" ] || fail "setup returned no csrf_token"
CSRF=(-H "X-CSRF-Token: $token")

if [ -n "${SMOKE_API_IMAGE:-}" ]; then
    # Create validates tz with time.LoadLocation and the binary embeds no tzdata, so this
    # proves the runtime image's zone data. Disabled: a live pause window would make the
    # snatch steps below depend on the clock.
    sched_payload='{"name":"smoke-berlin","days":[1,2,3,4,5],"start":"08:00","end":"17:00","tz":"Europe/Berlin","action":"pause","enabled":false}'
    if ! sched_id=$(api POST /schedules "$sched_payload" | jq -r '.id // empty') || [ -z "$sched_id" ]; then
        fail "failed to create schedule with tz Europe/Berlin"
    fi
    echo "smoke: schedule created with tz Europe/Berlin (id: $sched_id)"
fi

# stream_probe [CURL_ARGS...]: status and media type of GET /events/stream. An open stream
# never ends, so curl stops at its timeout (exit 28) after the headers.
stream_probe() {
    local out rc
    if out=$(curl -sS --max-time 3 -o /dev/null -w '%{http_code} %{content_type}' "$@" "$API/events/stream" 2>/dev/null); then
        rc=0
    else
        rc=$?
    fi
    case $rc in
    0 | 28) printf '%s\n' "$out" ;;
    *) fail "events stream probe: curl exit $rc" ;;
    esac
}

# The event stream is served outside the generated router; it must refuse a caller
# without a session as every other operation does.
probe=$(stream_probe)
case "$probe" in
"401 application/problem+json"*) ;;
*) fail "unauthenticated events stream answered '$probe', want 401 application/problem+json" ;;
esac
probe=$(stream_probe --cookie "$JAR")
case "$probe" in
"200 text/event-stream"*) ;;
*) fail "authenticated events stream answered '$probe', want 200 text/event-stream" ;;
esac
echo "smoke: events stream answers 401 without a session and 200 with one"

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
radarr_upgrade_id=$(instance radarr "$RADARR_UPGRADE_PORT" smoke-radarr-upgrade \
    ".hourly_cap = $CAP | .missing_per_cycle = 0 | .upgrade_per_cycle = $PER_CYCLE")
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
    api POST "/instances/$radarr_upgrade_id/runs" '{"kind":"upgrade"}' >/dev/null
    settle "$sonarr_id" "$radarr_id" "$series_id" "$radarr_upgrade_id"
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

check_upgrade() {
    local ids count unique used runs_json runs_count bad_runs
    ids=$(searched "$RADARR_UPGRADE_PORT")
    count=$(printf '%s\n' "$ids" | sed '/^$/d' | wc -l)
    unique=$(printf '%s\n' "$ids" | sed '/^$/d' | sort -u | wc -l)
    used=$(printf '%s' "$caps" | jq --arg id "$radarr_upgrade_id" '[(. // [])[] | select(.instance_id == $id) | .used] | add // 0')
    runs_json=$(api GET "/runs?instance_id=$radarr_upgrade_id")
    runs_count=$(printf '%s' "$runs_json" | jq '[(. // [])[]] | length')
    [ "$runs_count" -ge "$ROUNDS" ] || fail "h. smoke-radarr-upgrade has $runs_count run(s), want at least $ROUNDS"
    bad_runs=$(printf '%s' "$runs_json" | jq '[(. // [])[] | select(.kind != "upgrade" or .status != "done")] | length')
    [ "$bad_runs" -eq 0 ] || fail "h. smoke-radarr-upgrade has $bad_runs run(s) not kind=upgrade and status=done"
    for id in $(printf '%s\n' "$ids" | sed '/^$/d'); do
        [ "$id" -ge 5000 ] || fail "h. smoke-radarr-upgrade searched item $id in missing range (< 5000)"
        [ "$id" -lt 6000 ] || fail "h. smoke-radarr-upgrade searched item $id outside cutoff range"
    done
    [ "$count" -eq "$CAP" ] || fail "h. smoke-radarr-upgrade searched $count items, want exactly the cap $CAP"
    [ "$unique" -eq "$count" ] || fail "h. smoke-radarr-upgrade searched an item twice ($count searches, $unique distinct)"
    [ "$used" -eq "$count" ] || fail "h. /hourly-caps reports $used used for smoke-radarr-upgrade, fakearr received $count"
    echo "smoke: smoke-radarr-upgrade searched $count cutoff items of cap $CAP; API reports $used used; kind upgrade and status done verified"
}
check_upgrade

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

runs=$(api GET "/runs?instance_id=$sonarr_id")$(api GET "/runs?instance_id=$radarr_id")$(api GET "/runs?instance_id=$series_id")$(api GET "/runs?instance_id=$radarr_upgrade_id")
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
contact=$(api GET /system/status | jq -r '.worker_last_contact_at // empty')
[ -n "$contact" ] || fail "g. /system/status has no worker_last_contact_at"
echo "smoke: last worker contact $contact"

# worker_failures: failed API calls both workers counted so far, all calls summed.
worker_failures() {
    local port n total=0
    for port in "$W1_HTTP_PORT" "$W2_HTTP_PORT"; do
        n=$(curl -fsS --max-time 5 "http://127.0.0.1:$port/metrics" |
            awk '/^snatcharr_worker_api_failures_total\{/ { n += $2 } END { print n + 0 }') ||
            fail "j. worker on $port: /metrics failed"
        total=$((total + n))
    done
    echo "$total"
}

# runs_in STATUS ID...: runs of the instances in STATUS.
runs_in() {
    local status=$1 id n total=0
    shift
    for id in "$@"; do
        n=$(api GET "/runs?instance_id=$id&limit=500" | jq --arg s "$status" '[(. // [])[] | select(.status == $s)] | length')
        total=$((total + n))
    done
    echo "$total"
}

# contact_age: seconds since the newest LeaseRun or Heartbeat of any worker.
contact_age() {
    local at at_s now
    at=$(api GET /system/status | jq -r '.worker_last_contact_at // empty')
    [ -n "$at" ] || fail "j. /system/status lost worker_last_contact_at"
    at_s=$(date -d "$at" +%s) || fail "j. worker_last_contact_at '$at' is no time"
    now=$(date +%s)
    echo $((now - at_s))
}

# soak_round N: one soak round; fails on any failed API call, failed run or stale contact
# since the soak started.
soak_round() {
    local failures failed age
    api POST "/instances/$sonarr_id/runs" '{"kind":"missing"}' >/dev/null
    api POST "/instances/$radarr_id/runs" '{"kind":"missing"}' >/dev/null
    settle "$sonarr_id" "$radarr_id"
    sleep "$SOAK_ROUND_SECONDS"
    failures=$(worker_failures)
    failures=$((failures - soak_failures))
    [ "$failures" -eq 0 ] || fail "j. round $1: the workers counted $failures failed API calls since the soak started"
    failed=$(runs_in failed "$sonarr_id" "$radarr_id")
    failed=$((failed - soak_failed))
    [ "$failed" -eq 0 ] || fail "j. round $1: $failed runs failed since the soak started"
    age=$(contact_age)
    [ "$age" -le "$SOAK_MAX_CONTACT_AGE" ] || fail "j. round $1: last worker contact $age s ago, want at most $SOAK_MAX_CONTACT_AGE s"
}

# j. Soak (C14-01): with SOAK_SECONDS set, keep the system running and trigger a missing run on
# sonarr and radarr every SOAK_ROUND_SECONDS. Each round, no worker API call may have failed,
# no run may have failed and a worker must have reached the API within SOAK_MAX_CONTACT_AGE.
# This is the class of the lease failures that went unseen for 26 h: a long-poll cut at
# connection rotation, or a call refused unsent by the closing connection, counts as a
# failed API call.
SOAK_SECONDS=${SOAK_SECONDS:-0}
SOAK_ROUND_SECONDS=${SOAK_ROUND_SECONDS:-20}
# A worker reaches the API at least once per long-poll; 15 s on top covers a resend.
SOAK_MAX_CONTACT_AGE=${SOAK_MAX_CONTACT_AGE:-$((SMOKE_LEASE_WAIT_SECS + 15))}
if [ "$SOAK_SECONDS" -gt 0 ]; then
    soak_rounds=$((SOAK_SECONDS / SOAK_ROUND_SECONDS))
    # 2 runs per round per 500-run page of /runs: at most 450 rounds keeps every run counted.
    if [ "$soak_rounds" -lt 1 ] || [ "$soak_rounds" -gt 450 ]; then
        fail "j. SOAK_SECONDS / SOAK_ROUND_SECONDS gives $soak_rounds rounds, want 1 to 450"
    fi
    soak_failures=$(worker_failures)
    soak_failed=$(runs_in failed "$sonarr_id" "$radarr_id")
    soak_done=$(runs_in "done" "$sonarr_id" "$radarr_id")
    soak_start=$(date +%s)
    for round in $(seq 1 "$soak_rounds"); do
        soak_round "$round"
    done
    done_runs=$(runs_in "done" "$sonarr_id" "$radarr_id")
    done_runs=$((done_runs - soak_done))
    [ "$done_runs" -ge $((2 * soak_rounds)) ] || fail "j. $done_runs runs done in $soak_rounds rounds, want at least $((2 * soak_rounds))"
    soak_end=$(date +%s)
    echo "smoke: soak passed: $soak_rounds rounds in $((soak_end - soak_start)) s, $done_runs runs done, no failed API call or run"
fi

# mem_peak NAME CONTAINER LIMIT: the container's peak against the chart limit it ran under.
mem_peak() {
    local id peak
    id=$(docker inspect -f '{{.Id}}' "$2") || fail "i. cannot inspect container $2"
    peak=$(bash "$MEM" peak "$id") || fail "i. no memory peak for $1"
    bash "$MEM" check "$1" "$peak" "$3" || fail "i. $1 outgrew its memory budget"
}
if [ -n "${SMOKE_API_IMAGE:-}" ]; then
    mem_peak api "$cname_api" "$API_MEM"
fi
if [ -n "${SMOKE_WORKER_IMAGE:-}" ]; then
    mem_peak w1 "$cname_w1" "$WORKER_MEM"
    mem_peak w2 "$cname_w2" "$WORKER_MEM"
fi
echo "smoke: passed"
