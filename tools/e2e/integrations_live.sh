#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Drives SnatchArr API end to end against real application containers
# (tools/e2e/integrations/compose.yaml), exercising setup, instances, Prowlarr,
# Seerr, and download clients across all supported integrations.
#
# SNATCHARR_INTEGRATIONS_PLANT plants a defect the test must refuse:
#   badkey       wrong Prowlarr API key
#   seerrpath    point Seerr link at Sonarr port
#   badclient    wrong qBittorrent secret
#   badinstance  wrong Sonarr API key
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

if [ -z "${SMOKE_DSN:-}" ]; then
    exec bash "$root/tools/e2e/postgres.sh" bash "$0" "$@"
fi

plant=${SNATCHARR_INTEGRATIONS_PLANT:-}
case "$plant" in
'' | badkey | seerrpath | badclient | badinstance) ;;
*) echo "integrations-live: unknown SNATCHARR_INTEGRATIONS_PLANT '$plant'" >&2; exit 1 ;;
esac

# shellcheck source=tools/e2e/pull_retry.sh
. "$root/tools/e2e/pull_retry.sh"
project="snatcharr-integrations-$$"
dc() { compose -p "$project" -f "$root/tools/e2e/integrations/compose.yaml" "$@"; }

work=$(mktemp -d)
INTEGRATIONS_CONFIG_DIR="$work/config"
mkdir -p "$INTEGRATIONS_CONFIG_DIR/whisparr_v2" "$INTEGRATIONS_CONFIG_DIR/seerr"
INTEGRATIONS_UID=$(id -u)
INTEGRATIONS_GID=$(id -g)
LOG_DIR="$work/logs"
mkdir -p "$LOG_DIR"
COOKIE_JAR="$work/cookies.txt"

api_pid=""

cleanup() {
    status=$?
    if [ -n "$api_pid" ]; then
        if kill "$api_pid" 2>/dev/null && ! wait "$api_pid" 2>/dev/null; then
            :
        fi
    fi
    if [ "$status" -ne 0 ] && [ -z "$plant" ]; then
        echo "integrations-live: reading container logs on failure..." >&2
        if ! dc logs --no-color --tail 40 >&2; then
            :
        fi
        if [ -d "$LOG_DIR" ]; then
            echo "integrations-live: service logs:" >&2
            if ! tail -n 40 "$LOG_DIR"/*.log >&2; then
                :
            fi
        fi
    fi
    if ! dc down -v --remove-orphans >/dev/null 2>&1; then
        :
    fi
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT

fail() {
    echo "integrations-live: $*" >&2
    exit 1
}

free_port() {
    local port=$1 tries=0
    while (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; do
        port=$((port + 1))
        tries=$((tries + 1))
        [ "$tries" -lt 100 ] || fail "no free port from $1"
    done
    echo "$port"
}

base_port=${INTEGRATIONS_BASE_PORT:-18300}
allocate_port() {
    base_port=$(free_port "$base_port")
    local p=$base_port
    base_port=$((base_port + 1))
    printf -v "$1" '%s' "$p"
}

allocate_port API_PORT
allocate_port GRPC_PORT
allocate_port SONARR_PORT
allocate_port RADARR_PORT
allocate_port LIDARR_PORT
allocate_port READARR_PORT
allocate_port PROWLARR_PORT
allocate_port WHISPARR_V2_PORT
allocate_port WHISPARR_V3_PORT
allocate_port SEERR_PORT
allocate_port SABNZBD_PORT
allocate_port QBITTORRENT_PORT

SONARR_API_KEY=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
RADARR_API_KEY=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
LIDARR_API_KEY=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
READARR_API_KEY=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
PROWLARR_API_KEY=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
WHISPARR_V2_API_KEY=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
WHISPARR_V3_API_KEY=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
SEERR_API_KEY=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')

export SONARR_PORT RADARR_PORT LIDARR_PORT READARR_PORT PROWLARR_PORT \
       WHISPARR_V2_PORT WHISPARR_V3_PORT SEERR_PORT SABNZBD_PORT QBITTORRENT_PORT \
       INTEGRATIONS_UID INTEGRATIONS_GID INTEGRATIONS_CONFIG_DIR \
       SONARR_API_KEY RADARR_API_KEY LIDARR_API_KEY READARR_API_KEY \
       PROWLARR_API_KEY WHISPARR_V3_API_KEY

cat <<EOF > "$INTEGRATIONS_CONFIG_DIR/whisparr_v2/config.xml"
<Config>
  <Port>6969</Port>
  <ApiKey>$WHISPARR_V2_API_KEY</ApiKey>
  <AuthenticationMethod>None</AuthenticationMethod>
  <AuthenticationRequired>Enabled</AuthenticationRequired>
  <Branch>master</Branch>
  <LogLevel>debug</LogLevel>
  <UrlBase></UrlBase>
</Config>
EOF

cat <<EOF > "$INTEGRATIONS_CONFIG_DIR/seerr/settings.json"
{
  "main": {
    "apiKey": "$SEERR_API_KEY",
    "mediaServerType": 4
  },
  "public": {
    "initialized": true
  }
}
EOF

pull_retry dc pull --quiet
dc up -d --pull never

answers() {
    local url=$1 timeout=${2:-60} tries
    for tries in $(seq 1 "$timeout"); do
        if curl -s -o /dev/null --max-time 2 "$url"; then return 0; fi
        sleep 1
    done
    fail "$url did not answer within ${timeout}s"
}

answers "http://127.0.0.1:$SONARR_PORT"
answers "http://127.0.0.1:$RADARR_PORT"
answers "http://127.0.0.1:$LIDARR_PORT"
answers "http://127.0.0.1:$READARR_PORT"
answers "http://127.0.0.1:$PROWLARR_PORT"
answers "http://127.0.0.1:$WHISPARR_V2_PORT"
answers "http://127.0.0.1:$WHISPARR_V3_PORT"
answers "http://127.0.0.1:$SEERR_PORT"
answers "http://127.0.0.1:$SABNZBD_PORT"
answers "http://127.0.0.1:$QBITTORRENT_PORT"

python3 -I - <<EOF
import os
import sqlite3
import sys
import time

db_path = "$INTEGRATIONS_CONFIG_DIR/seerr/db/db.sqlite3"
deadline = time.time() + 60
while time.time() < deadline:
    if os.path.exists(db_path) and os.path.getsize(db_path) > 0:
        try:
            conn = sqlite3.connect(db_path)
            conn.execute("INSERT OR REPLACE INTO user (id, email, username, permissions, userType, avatar) VALUES (1, 'admin@example.com', 'admin', 2, 1, '')")
            conn.commit()
            conn.close()
            break
        except Exception:
            pass
    time.sleep(0.5)
else:
    sys.exit("Failed to insert admin user in Seerr db within 60s")
EOF

sab_key() {
    local i key cid="${project}-sabnzbd-1"
    for i in $(seq 1 60); do
        key=$(docker exec "$cid" sed -n 's/^api_key = //p' /config/sabnzbd.ini 2>/dev/null | tr -d '\r')
        if [ -n "$key" ]; then printf '%s' "$key"; return 0; fi
        sleep 1
    done
    fail "SABnzbd wrote no API key within 60 s (tries: $i)"
}

qbit_password() {
    local i pass cid="${project}-qbittorrent-1"
    for i in $(seq 1 60); do
        pass=$(docker logs "$cid" 2>/dev/null | sed -n 's/.*temporary password is provided for this session: \([^ ]*\).*/\1/p' | tail -1 | tr -d '\r')
        if [ -n "$pass" ]; then printf '%s' "$pass"; return 0; fi
        sleep 1
    done
    fail "qBittorrent printed no temporary password within 60 s (tries: $i)"
}

SABNZBD_API_KEY=$(sab_key)
QBITTORRENT_PASSWORD=$(qbit_password)

if [ -z "${SMOKE_API_BIN:-}" ] || [ ! -x "${SMOKE_API_BIN:-}" ]; then
    mkdir -p "$root/api/tmp/e2e"
    go build -o "$root/api/tmp/e2e/snatcharr" "$root/api/cmd/snatcharr"
    SMOKE_API_BIN="$root/api/tmp/e2e/snatcharr"
fi

env APP_SNATCHARR_SESSION_SECURE=false \
    APP_SNATCHARR_PROFILE=dev \
    APP_DB_DSN="$SMOKE_DSN" \
    APP_SNATCHARR_WORKER_TOKEN="integrations-token" \
    APP_HTTP_ADDR="127.0.0.1:$API_PORT" \
    APP_GRPC_LISTEN="127.0.0.1:$GRPC_PORT" \
    "$SMOKE_API_BIN" >"$LOG_DIR/api.log" 2>&1 &
api_pid=$!

wait_api() {
    local url=$1 timeout=${2:-60} tries=0
    until curl -fsS --max-time 2 -o /dev/null "$url" 2>/dev/null; do
        tries=$((tries + 1))
        [ "$tries" -lt "$((timeout * 2))" ] || fail "$url not ready after ${timeout}s"
        sleep 0.5
    done
}
wait_api "http://127.0.0.1:$API_PORT/api/v1/system/status"

CSRF_TOKEN=""

api_req() {
    local step=$1 method=$2 path=$3 want_status=$4 body=${5:-}
    local headers=(-H "Accept: application/json")
    if [ -n "$CSRF_TOKEN" ]; then
        headers+=(-H "X-CSRF-Token: $CSRF_TOKEN")
    fi
    if [ -n "$body" ]; then
        headers+=(-H "Content-Type: application/json" -d "$body")
    fi
    local res status resp_body
    if ! res=$(curl -sS --max-time 15 -X "$method" --cookie "$COOKIE_JAR" --cookie-jar "$COOKIE_JAR" \
        "${headers[@]}" -w "\n%{http_code}" "http://127.0.0.1:$API_PORT/api/v1$path" 2>&1); then
        fail "$step: curl failed: $res"
    fi
    status=$(printf '%s\n' "$res" | tail -n 1)
    resp_body=$(printf '%s\n' "$res" | sed '$d')
    if [ "$status" != "$want_status" ]; then
        echo "$step: status $status (want $want_status); body: $resp_body" >&2
        fail "$step failed"
    fi
    printf '%s' "$resp_body"
}

# 1. Setup admin
setup_res=$(api_req "setup admin" POST /auth/setup 201 '{"username":"admin","password":"smoke-password-1234"}')
CSRF_TOKEN=$(printf '%s' "$setup_res" | jq -r '.csrf_token // empty')
[ -n "$CSRF_TOKEN" ] || fail "setup returned empty csrf_token"

# 2. App instances
apps=(
    "sonarr:$SONARR_PORT:$SONARR_API_KEY"
    "radarr:$RADARR_PORT:$RADARR_API_KEY"
    "lidarr:$LIDARR_PORT:$LIDARR_API_KEY"
    "readarr:$READARR_PORT:$READARR_API_KEY"
    "whisparr_v2:$WHISPARR_V2_PORT:$WHISPARR_V2_API_KEY"
    "whisparr_v3:$WHISPARR_V3_PORT:$WHISPARR_V3_API_KEY"
)

for entry in "${apps[@]}"; do
    kind="${entry%%:*}"
    rest="${entry#*:}"
    port="${rest%%:*}"
    key="${rest#*:}"

    if [ "$plant" = "badinstance" ] && [ "$kind" = "sonarr" ]; then
        key="wrong-sonarr-key-00000000000000"
    fi

    input=$(jq -n \
        --arg kind "$kind" \
        --arg name "int-$kind" \
        --arg base_url "http://127.0.0.1:$port" \
        --arg api_key "$key" \
        '{kind: $kind, name: $name, base_url: $base_url, api_key: $api_key, enabled: true}')

    probe_res=$(api_req "instance test" POST /instances/test 200 "$input")
    app_name=$(printf '%s' "$probe_res" | jq -r '.app_name // empty')
    version=$(printf '%s' "$probe_res" | jq -r '.version // empty')
    [ -n "$app_name" ] && [ -n "$version" ] || fail "instance test $kind: app_name or version empty: $probe_res"

    create_res=$(api_req "create instance" POST /instances 201 "$input")
    inst_id=$(printf '%s' "$create_res" | jq -r '.id // empty')
    [ -n "$inst_id" ] || fail "create instance $kind: id empty: $create_res"

    test_res=$(api_req "test stored instance" POST "/instances/$inst_id/test" 200)
    stored_name=$(printf '%s' "$test_res" | jq -r '.app_name // empty')
    [ -n "$stored_name" ] || fail "test stored instance $kind: app_name empty: $test_res"

    filters_res=$(api_req "filter options" GET "/instances/$inst_id/policy/filter-options" 200)
    printf '%s' "$filters_res" | jq -e '.tags | type == "array"' >/dev/null || fail "filter options $kind: tags not array: $filters_res"
    printf '%s' "$filters_res" | jq -e '.quality_profiles | type == "array"' >/dev/null || fail "filter options $kind: quality_profiles not array: $filters_res"

    preview_res=$(api_req "preview run" POST "/instances/$inst_id/preview?kind=missing" 202)
    run_id=$(printf '%s' "$preview_res" | jq -r '.run_id // empty')
    [ -n "$run_id" ] || fail "preview run $kind: run_id empty: $preview_res"
done

# 3. Prowlarr
prowlarr_key="$PROWLARR_API_KEY"
if [ "$plant" = "badkey" ]; then
    prowlarr_key="bad-prowlarr-key-00000000000000"
fi
prowlarr_input=$(jq -n \
    --arg name "int-prowlarr" \
    --arg base_url "http://127.0.0.1:$PROWLARR_PORT" \
    --arg api_key "$prowlarr_key" \
    '{name: $name, base_url: $base_url, api_key: $api_key, enabled: true}')

prow_probe=$(api_req "prowlarr test" POST /prowlarr/test 200 "$prowlarr_input")
prow_name=$(printf '%s' "$prow_probe" | jq -r '.app_name // empty')
[ -n "$prow_name" ] || fail "prowlarr test: app_name empty: $prow_probe"

prow_create=$(api_req "create prowlarr" POST /prowlarr 201 "$prowlarr_input")
prow_id=$(printf '%s' "$prow_create" | jq -r '.id // empty')
[ -n "$prow_id" ] || fail "create prowlarr: id empty: $prow_create"

prow_test=$(api_req "test stored prowlarr" POST "/prowlarr/$prow_id/test" 200)
prow_stored_name=$(printf '%s' "$prow_test" | jq -r '.app_name // empty')
[ -n "$prow_stored_name" ] || fail "test stored prowlarr: app_name empty: $prow_test"

prow_status=$(api_req "prowlarr status" GET /prowlarr/status 200)
prow_reachable=$(printf '%s' "$prow_status" | jq -r '.[0].reachable // empty')
[ "$prow_reachable" = "true" ] || fail "prowlarr status: reachable is $prow_reachable, want true: $prow_status"

# 4. Seerr
seerr_url="http://127.0.0.1:$SEERR_PORT"
if [ "$plant" = "seerrpath" ]; then
    seerr_url="http://127.0.0.1:$SONARR_PORT"
fi
seerr_input=$(jq -n \
    --arg name "int-seerr" \
    --arg base_url "$seerr_url" \
    --arg api_key "$SEERR_API_KEY" \
    '{name: $name, base_url: $base_url, api_key: $api_key, enabled: true}')

seerr_probe=$(api_req "seerr test" POST /seerr/test 200 "$seerr_input")
seerr_name=$(printf '%s' "$seerr_probe" | jq -r '.app_name // empty')
[ -n "$seerr_name" ] || fail "seerr test: app_name empty: $seerr_probe"

seerr_create=$(api_req "create seerr" POST /seerr 201 "$seerr_input")
seerr_id=$(printf '%s' "$seerr_create" | jq -r '.id // empty')
[ -n "$seerr_id" ] || fail "create seerr: id empty: $seerr_create"

seerr_test=$(api_req "test stored seerr" POST "/seerr/$seerr_id/test" 200)
seerr_stored_name=$(printf '%s' "$seerr_test" | jq -r '.app_name // empty')
[ -n "$seerr_stored_name" ] || fail "test stored seerr: app_name empty: $seerr_test"

seerr_sync=$(api_req "seerr sync" POST "/seerr/$seerr_id/sync" 200)
printf '%s' "$seerr_sync" | jq -e 'has("seen")' >/dev/null || fail "seerr sync: seen field missing: $seerr_sync"

seerr_reqs=$(api_req "seerr requests" GET /seerr/requests 200)
printf '%s' "$seerr_reqs" | jq -e 'type == "array"' >/dev/null || fail "seerr requests: not array: $seerr_reqs"

seerr_import=$(api_req "seerr import instances" POST "/seerr/$seerr_id/import-instances" 200)
printf '%s' "$seerr_import" | jq -e '.created | type == "array"' >/dev/null || fail "seerr import: created not array: $seerr_import"

# 5. Download clients (qbittorrent + sabnzbd)
qbit_secret="$QBITTORRENT_PASSWORD"
if [ "$plant" = "badclient" ]; then
    qbit_secret="wrong-qbit-password"
fi

qbit_input=$(jq -n \
    --arg kind "qbittorrent" \
    --arg name "int-qbittorrent" \
    --arg base_url "http://127.0.0.1:$QBITTORRENT_PORT" \
    --arg username "admin" \
    --arg secret "$qbit_secret" \
    '{kind: $kind, name: $name, base_url: $base_url, username: $username, secret: $secret, enabled: true}')

qbit_probe=$(api_req "download client test" POST /download-clients/test 200 "$qbit_input")
qbit_reachable=$(printf '%s' "$qbit_probe" | jq -r '.reachable')
if [ "$qbit_reachable" != "true" ]; then
    echo "download client test: qbittorrent reachable is $qbit_reachable, want true; body: $qbit_probe" >&2
    fail "download client test failed"
fi

qbit_create=$(api_req "create download client" POST /download-clients 201 "$qbit_input")
qbit_id=$(printf '%s' "$qbit_create" | jq -r '.id // empty')
[ -n "$qbit_id" ] || fail "create qbittorrent: id empty: $qbit_create"

qbit_test=$(api_req "test stored download client" POST "/download-clients/$qbit_id/test" 200)
qbit_test_reachable=$(printf '%s' "$qbit_test" | jq -r '.reachable')
[ "$qbit_test_reachable" = "true" ] || fail "test stored qbittorrent: reachable is $qbit_test_reachable, want true: $qbit_test"

sab_input=$(jq -n \
    --arg kind "sabnzbd" \
    --arg name "int-sabnzbd" \
    --arg base_url "http://127.0.0.1:$SABNZBD_PORT" \
    --arg secret "$SABNZBD_API_KEY" \
    '{kind: $kind, name: $name, base_url: $base_url, username: "", secret: $secret, enabled: true}')

sab_probe=$(api_req "download client test" POST /download-clients/test 200 "$sab_input")
sab_reachable=$(printf '%s' "$sab_probe" | jq -r '.reachable')
if [ "$sab_reachable" != "true" ]; then
    echo "download client test: sabnzbd reachable is $sab_reachable, want true; body: $sab_probe" >&2
    fail "download client test failed"
fi

sab_create=$(api_req "create download client" POST /download-clients 201 "$sab_input")
sab_id=$(printf '%s' "$sab_create" | jq -r '.id // empty')
[ -n "$sab_id" ] || fail "create sabnzbd: id empty: $sab_create"

sab_test=$(api_req "test stored download client" POST "/download-clients/$sab_id/test" 200)
sab_test_reachable=$(printf '%s' "$sab_test" | jq -r '.reachable')
[ "$sab_test_reachable" = "true" ] || fail "test stored sabnzbd: reachable is $sab_test_reachable, want true: $sab_test"

dl_status=$(api_req "download clients status" GET /download-clients/status 200)
printf '%s' "$dl_status" | jq -e 'length >= 2 and all(.[]; .reachable == true)' >/dev/null || \
    fail "download clients status: not all reachable: $dl_status"

echo "ok   every integration driven end to end against real application containers"
