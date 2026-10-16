# shellcheck shell=bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Shared setup of the homelab compose tests (compose_smoke.sh, compose_pg_upgrade.sh), sourced,
# not run. The caller sets `label` (the prefix of its messages) and may set `api_probe` (the API
# health check command, a JSON list body) before calling compose_init. compose_init writes a
# generated .env into a fresh work directory and fills `files` with the compose files to pass:
#
# SMOKE_API_IMAGE / SMOKE_WORKER_IMAGE replace the pinned release images (CI passes the images
# it built from the pull request); the override then adds the API and worker health checks,
# which the pinned 0.1.9 images cannot run. Without them the pinned images run unchanged.
# SMOKE_POSTGRES_MIRROR (e.g. mirror.gcr.io/library) pulls the pinned Postgres through a mirror.

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
compose="$root/deploy/homelab/compose.yaml"
port="${SMOKE_PORT:-18080}"
project="snatcharr-${label:?set label before sourcing compose_lib.sh}-$$"
api="http://127.0.0.1:$port"

dc() {
    docker compose -p "$project" --project-directory "$root/deploy/homelab" \
        --env-file "$work/.env" "${files[@]}" "$@"
}

cleanup() {
    status=$?
    if [ "$status" -ne 0 ] && ! { dc ps -a && dc logs --no-color --tail 80; }; then
        echo "$label: could not read the service state" >&2
    fi
    if ! dc down -v --remove-orphans >/dev/null 2>&1; then
        echo "$label: could not remove project $project" >&2
    fi
    rm -rf "$work"
    exit "$status"
}

fail() {
    echo "$label: $*" >&2
    exit 1
}

secret() { openssl rand -hex 32; }

# mirrored prints a Docker Hub image reference as CI pulls it: through SMOKE_POSTGRES_MIRROR
# when set, same tag and digest.
mirrored() {
    if [ -n "${SMOKE_POSTGRES_MIRROR:-}" ]; then
        printf '%s/%s\n' "$SMOKE_POSTGRES_MIRROR" "$1"
    else
        printf '%s\n' "$1"
    fi
}

write_env() {
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
}

write_image_override() {
    : "${SMOKE_API_IMAGE:?set SMOKE_API_IMAGE together with SMOKE_WORKER_IMAGE}"
    : "${SMOKE_WORKER_IMAGE:?set SMOKE_WORKER_IMAGE together with SMOKE_API_IMAGE}"
    local probe=${api_probe:-'"/snatcharr-api", "healthcheck"'}
    cat >"$work/override.yaml" <<EOF
services:
  api:
    image: $SMOKE_API_IMAGE
    pull_policy: never
    healthcheck:
      test: ["CMD", $probe]
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
}

compose_init() {
    work=$(mktemp -d)
    files=(-f "$compose")
    write_env
    if [ -n "${SMOKE_API_IMAGE:-}" ] || [ -n "${SMOKE_WORKER_IMAGE:-}" ]; then
        write_image_override
    fi
    if [ -n "${SMOKE_POSTGRES_MIRROR:-}" ]; then
        pinned=$(sed -n 's#^ *image: \(postgres:[^@]*@sha256:[0-9a-f]\{64\}\)$#\1#p' "$compose")
        [ -n "$pinned" ] || fail "no digest-pinned postgres image in $compose"
        printf 'services:\n  postgres:\n    image: %s\n' "$(mirrored "$pinned")" >"$work/postgres.yaml"
        files+=(-f "$work/postgres.yaml")
    fi
    trap cleanup EXIT
}

# complete_setup runs the first-run setup with the generated setup secret and user `admin`.
complete_setup() {
    local setup_secret
    setup_secret=$(sed -n 's/^SNATCHARR_SETUP_SECRET=//p' "$work/.env")
    curl -fsS --max-time 10 -H 'Content-Type: application/json' \
        -d "{\"username\":\"admin\",\"password\":\"$1\",\"setup_secret\":\"$setup_secret\"}" \
        "$api/api/v1/auth/setup" >/dev/null
}
