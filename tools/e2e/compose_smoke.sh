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
# Image overrides (SMOKE_API_IMAGE, SMOKE_WORKER_IMAGE, SMOKE_POSTGRES_MIRROR): compose_lib.sh.
# SMOKE_PLANT=unhealthy or SMOKE_PLANT=setup-done plants a defect the smoke must refuse.
# Needs docker compose, curl, jq and openssl.
set -euo pipefail

label="compose-smoke"
if [ "${SMOKE_PLANT:-}" = unhealthy ]; then
    api_probe='"/snatcharr-api-missing"'
fi
# shellcheck source=tools/e2e/compose_lib.sh
. "$(dirname "$0")/compose_lib.sh"
compose_init

dc config --quiet
dc up -d --quiet-pull --wait --wait-timeout 240 || fail "services did not turn healthy"

for svc in postgres api worker; do
    health=$(dc ps --format '{{.Health}}' "$svc")
    if [ -n "${SMOKE_API_IMAGE:-}" ] || [ "$svc" = postgres ]; then
        [ "$health" = healthy ] || fail "$svc health is '$health', want healthy"
    fi
    echo "ok   $svc running${health:+, $health}"
done

if [ "${SMOKE_PLANT:-}" = setup-done ]; then
    # Complete the setup first: the status check below must then refuse.
    complete_setup "smoke-$(secret)" || fail "planted setup failed"
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
