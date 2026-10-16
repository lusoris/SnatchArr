#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Homelab compose smoke test (#41): starts deploy/homelab/compose.yaml the way
# docs/install-compose.md says, with a generated .env, and proves
#   a. every service turns healthy (`docker compose up --wait`), the API and worker through
#      their `healthcheck` subcommands;
#   b. the first-run setup screen answers: /setup serves the web UI;
#   c. /api/v1/auth/setup/status reports setup not complete and the setup secret required;
#   d. the documented backup and restore commands round-trip the database.
#
# SMOKE_API_IMAGE / SMOKE_WORKER_IMAGE replace the pinned release images (CI passes the images
# it built from the pull request); the override then adds the API and worker health checks,
# which the pinned 0.1.9 images cannot run. Without them the pinned images run unchanged.
# SMOKE_POSTGRES_MIRROR (e.g. mirror.gcr.io/library) pulls the pinned Postgres through a mirror.
# SMOKE_PLANT=unhealthy or SMOKE_PLANT=setup-done plants a defect the smoke must refuse.
# Needs docker compose, curl, jq and openssl.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
compose="$root/deploy/homelab/compose.yaml"
port="${SMOKE_PORT:-18080}"
project="snatcharr-smoke-$$"
work=$(mktemp -d)

dc() {
    docker compose -p "$project" --project-directory "$root/deploy/homelab" \
        --env-file "$work/.env" "${files[@]}" "$@"
}

cleanup() {
    status=$?
    if [ "$status" -ne 0 ] && ! { dc ps -a && dc logs --no-color --tail 80; }; then
        echo "compose smoke: could not read the service state" >&2
    fi
    if ! dc down -v --remove-orphans >/dev/null 2>&1; then
        echo "compose smoke: could not remove project $project" >&2
    fi
    rm -rf "$work"
    exit "$status"
}

fail() {
    echo "compose smoke: $*" >&2
    exit 1
}

secret() { openssl rand -hex 32; }

cat >"$work/.env" <<EOF
SNATCHARR_DB_PASSWORD=$(secret)
SNATCHARR_CRYPTO_KEY=$(secret)
SNATCHARR_CSRF_SECRET=$(secret)
SNATCHARR_APIKEY_SECRET=$(secret)
SNATCHARR_WORKER_TOKEN=$(secret)
SNATCHARR_SETUP_SECRET=$(secret)
SNATCHARR_PUBLIC_URL=http://127.0.0.1:$port
SNATCHARR_SESSION_SECURE=false
SNATCHARR_BIND=127.0.0.1
SNATCHARR_PORT=$port
EOF

files=(-f "$compose")
if [ -n "${SMOKE_API_IMAGE:-}" ] || [ -n "${SMOKE_WORKER_IMAGE:-}" ]; then
    : "${SMOKE_API_IMAGE:?set SMOKE_API_IMAGE together with SMOKE_WORKER_IMAGE}"
    : "${SMOKE_WORKER_IMAGE:?set SMOKE_WORKER_IMAGE together with SMOKE_API_IMAGE}"
    api_probe='"/snatcharr-api", "healthcheck"'
    if [ "${SMOKE_PLANT:-}" = unhealthy ]; then
        api_probe='"/snatcharr-api-missing"'
    fi
    cat >"$work/override.yaml" <<EOF
services:
  api:
    image: $SMOKE_API_IMAGE
    pull_policy: never
    healthcheck:
      test: ["CMD", $api_probe]
      interval: 5s
      timeout: 5s
      retries: 6
      start_period: 30s
  worker:
    image: $SMOKE_WORKER_IMAGE
    pull_policy: never
    healthcheck:
      test: ["CMD", "/snatch-worker", "healthcheck"]
      interval: 5s
      timeout: 5s
      retries: 6
      start_period: 30s
EOF
    files+=(-f "$work/override.yaml")
fi
if [ -n "${SMOKE_POSTGRES_MIRROR:-}" ]; then
    # CI pulls Docker Hub images through a mirror: same tag, same digest as compose.yaml pins.
    pinned=$(sed -n 's#^ *image: \(postgres:[^@]*@sha256:[0-9a-f]\{64\}\)$#\1#p' "$compose")
    [ -n "$pinned" ] || fail "no digest-pinned postgres image in $compose"
    printf 'services:\n  postgres:\n    image: %s/%s\n' "$SMOKE_POSTGRES_MIRROR" "$pinned" \
        >"$work/postgres.yaml"
    files+=(-f "$work/postgres.yaml")
fi
trap cleanup EXIT

dc config --quiet
dc up -d --quiet-pull --wait --wait-timeout 240 || fail "services did not turn healthy"

for svc in postgres api worker; do
    health=$(dc ps --format '{{.Health}}' "$svc")
    if [ -n "${SMOKE_API_IMAGE:-}" ] || [ "$svc" = postgres ]; then
        [ "$health" = healthy ] || fail "$svc health is '$health', want healthy"
    fi
    echo "ok   $svc running${health:+, $health}"
done

api="http://127.0.0.1:$port"
if [ "${SMOKE_PLANT:-}" = setup-done ]; then
    # Complete the setup first: the status check below must then refuse.
    setup_secret=$(sed -n 's/^SNATCHARR_SETUP_SECRET=//p' "$work/.env")
    curl -fsS --max-time 10 -H 'Content-Type: application/json' \
        -d "{\"username\":\"admin\",\"password\":\"smoke-$(secret)\",\"setup_secret\":\"$setup_secret\"}" \
        "$api/api/v1/auth/setup" >/dev/null || fail "planted setup failed"
fi

page=$(curl -sS --max-time 10 -o "$work/setup.html" -w '%{http_code} %{content_type}' "$api/setup") ||
    fail "GET /setup failed"
case "$page" in
"200 text/html"*) echo "ok   /setup serves the web UI ($page)" ;;
*) fail "GET /setup answered '$page', want 200 text/html" ;;
esac
grep -qi '<html' "$work/setup.html" || fail "/setup body is not an HTML page"

status=$(curl -fsS --max-time 10 "$api/api/v1/auth/setup/status") || fail "GET setup/status failed"
jq -e '.setup_complete == false and .secret_required == true' >/dev/null <<<"$status" ||
    fail "setup/status is $status, want setup_complete false and secret_required true"
echo "ok   setup/status: $status"

# d. The backup and restore commands of docs/install-compose.md, verbatim.
dc exec -T postgres pg_dump -U snatcharr --format=custom snatcharr >"$work/snatcharr.dump" ||
    fail "pg_dump failed"
[ -s "$work/snatcharr.dump" ] || fail "pg_dump wrote an empty dump"
dc stop api worker >/dev/null
dc exec -T postgres pg_restore -U snatcharr --clean --if-exists --no-owner \
    --exit-on-error --dbname=snatcharr <"$work/snatcharr.dump" || fail "pg_restore failed"
dc up -d --wait --wait-timeout 240 || fail "services did not turn healthy after the restore"
status=$(curl -fsS --max-time 10 "$api/api/v1/auth/setup/status") ||
    fail "GET setup/status failed after the restore"
jq -e '.setup_complete == false' >/dev/null <<<"$status" ||
    fail "setup/status after the restore is $status"
echo "ok   backup and restore round trip"
echo "compose smoke passed"
