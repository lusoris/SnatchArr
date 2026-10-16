#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Postgres major upgrade of a homelab compose install (#252): starts deploy/homelab/compose.yaml
# on the previous Postgres major (storetest.PreviousMajorImage), completes the first-run setup
# and logs in, then runs the upgrade steps of docs/install-compose.md ("Postgres major upgrade")
# in order and proves
#   a. the new major serves the database (server major = storetest.Major);
#   b. every table of the public schema holds the same rows as before (row content hash);
#   c. the setup stays complete and the admin logs in with the password set before the upgrade.
#
# Image overrides (SMOKE_API_IMAGE, SMOKE_WORKER_IMAGE, SMOKE_POSTGRES_MIRROR): compose_lib.sh.
# UPGRADE_PLANT skips one documented step, and the test must refuse the result:
#   skip-move     start the new major on the old data directory;
#   skip-restore  start the new major on an empty data directory and never restore.
# Needs docker compose, curl, jq and openssl.
set -euo pipefail

label="compose-pg-upgrade"
# shellcheck source=tools/e2e/compose_lib.sh
. "$(dirname "$0")/compose_lib.sh"

storetest="$root/api/internal/store/storetest/storetest.go"
pin() { sed -n "s/^\t$1 *= *\"\?\([^\"]*\)\"\?\$/\1/p" "$storetest"; }
old_image=$(pin PreviousMajorImage)
old_major=$(pin PreviousMajor)
new_major=$(pin Major)
[ -n "$old_image" ] && [ -n "$old_major" ] && [ -n "$new_major" ] ||
    fail "could not read the Postgres pins from $storetest"

compose_init
# The install before the upgrade: compose.yaml as a previous-major install writes it, with that
# image and the volume at /var/lib/postgresql/data, where images up to 17 keep their data.
sed -e "s#^\( *image: \)postgres:.*#\1$(mirrored "$old_image")#" \
    -e 's#postgres-data:/var/lib/postgresql$#postgres-data:/var/lib/postgresql/data#' \
    "$compose" >"$work/compose-old.yaml"
grep -q "postgres-data:/var/lib/postgresql/data\$" "$work/compose-old.yaml" ||
    fail "no postgres-data volume line in $compose"
new_files=("${files[@]}")
files=(-f "$work/compose-old.yaml")
if [ -f "$work/override.yaml" ]; then
    files+=(-f "$work/override.yaml")
fi

psql_at() { dc exec -T postgres psql -U snatcharr -d snatcharr -v ON_ERROR_STOP=1 -Atc "$1"; }

server_major() { psql_at 'SHOW server_version_num' | awk '{ print int($1 / 10000) }'; }

fingerprint() {
    dc exec -T postgres psql -U snatcharr -d snatcharr -v ON_ERROR_STOP=1 -At \
        <"$root/tools/e2e/fingerprint.sql"
}

login() {
    curl -fsS --max-time 10 -o /dev/null -H 'Content-Type: application/json' \
        -d "{\"username\":\"admin\",\"password\":\"$password\"}" "$api/api/v1/auth/login"
}

# 1. An install on the previous major with a completed setup and one login.
dc config --quiet
dc up -d --quiet-pull --wait --wait-timeout 240 || fail "services did not turn healthy on $old_major"
[ "$(server_major)" = "$old_major" ] || fail "the install runs Postgres $(server_major), want $old_major"
password="upgrade-$(secret)"
complete_setup "$password" || fail "setup on $old_major failed"
login || fail "login on $old_major failed"
dc stop api worker >/dev/null
fingerprint >"$work/before.txt"
grep -q '^schema_migrations 1 ' "$work/before.txt" || fail "no migration row before the upgrade"
grep -q '^users 1 ' "$work/before.txt" || fail "no user row before the upgrade"
echo "ok   Postgres $old_major install: setup done, $(wc -l <"$work/before.txt") tables"

# 2. The documented upgrade steps, in order.
dc exec -T postgres pg_dump -U snatcharr --format=custom snatcharr >"$work/snatcharr.dump" ||
    fail "pg_dump failed"
[ -s "$work/snatcharr.dump" ] || fail "pg_dump wrote an empty dump"
dc down
files=("${new_files[@]}")
if [ "${UPGRADE_PLANT:-}" != skip-move ]; then
    dc run --rm --no-deps --entrypoint sh postgres -c "cd /var/lib/postgresql &&
        mkdir data-pg$old_major &&
        find . -mindepth 1 -maxdepth 1 ! -name data-pg$old_major -exec mv {} data-pg$old_major/ ';'" ||
        fail "moving the old data directory aside failed"
fi
dc up -d --quiet-pull --wait --wait-timeout 240 postgres ||
    fail "the new postgres $new_major did not turn healthy"
if [ "${UPGRADE_PLANT:-}" != skip-restore ]; then
    dc exec -T postgres pg_restore -U snatcharr --no-owner --exit-on-error --dbname=snatcharr \
        <"$work/snatcharr.dump" || fail "pg_restore failed"
fi

# 3. Read back, first the database alone (nothing writes yet), then through the API.
[ "$(server_major)" = "$new_major" ] || fail "the install runs Postgres $(server_major), want $new_major"
echo "ok   Postgres $new_major serves the database"
fingerprint >"$work/after.txt"
diff -u "$work/before.txt" "$work/after.txt" >&2 || fail "data read-back differs after the upgrade"
echo "ok   all $(wc -l <"$work/after.txt") tables read back unchanged"
dc up -d --wait --wait-timeout 240 || fail "services did not turn healthy on $new_major"
status=$(curl -fsS --max-time 10 "$api/api/v1/auth/setup/status") || fail "GET setup/status failed"
jq -e '.setup_complete == true' >/dev/null <<<"$status" ||
    fail "setup/status after the upgrade is $status, want setup_complete true"
login || fail "login with the password set on $old_major failed"
echo "ok   setup complete, admin logs in"

# 4. The documented clean-up of the old data directory leaves the install running.
dc exec -T postgres rm -rf "/var/lib/postgresql/data-pg$old_major" ||
    fail "removing the old data directory failed"
dc restart postgres >/dev/null
dc up -d --wait --wait-timeout 240 || fail "services did not turn healthy after the clean-up"
[ "$(psql_at 'SELECT count(*) FROM users')" = 1 ] || fail "user row missing after the clean-up"
echo "ok   old data directory removed, install still serves the data"
echo "compose Postgres $old_major -> $new_major upgrade passed"
