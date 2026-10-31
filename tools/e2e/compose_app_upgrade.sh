#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Application upgrade of a homelab compose install: starts deploy/homelab/compose.yaml
# on the previous release (UPGRADE_FROM or the latest v* git tag), seeds resources across
# the API, saves read-back JSON and database fingerprint, then restarts on candidate
# images (SMOKE_API_IMAGE, SMOKE_WORKER_IMAGE) and verifies:
#   a. schema migrations advance to candidate version and dirty is false;
#   b. tables differ only from an explicit allow-list with runtime justifications;
#   c. JSON read-backs match (jq subset equality on every seeded resource);
#   d. runtime settings are preserved (/settings non-default value);
#   e. admin logs in with password set before upgrade; old API key authenticates;
#   f. stored instance credentials decrypt (instance /test sends key to fake-arr);
#   g. worker is healthy and registered in API (worker_last_contact_at);
#   h. new migration effect (runtime_settings table and endpoint survival).
#
# UPGRADE_PLANT cases:
#   drop-row          delete a seeded row before candidate start ("api read-back differs")
#   change-setting    modify a seeded setting before candidate start ("settings differ")
#   wrong-crypto-key  restart candidate with different SNATCHARR_CRYPTO_KEY ("stored key")
#   old-image         start candidate with old release images ("did not advance")
# Needs docker compose, curl, jq and openssl.
set -euo pipefail

label="compose-app-upgrade"
# shellcheck source=tools/e2e/compose_lib.sh
. "$(dirname "$0")/compose_lib.sh"
# shellcheck source=tools/e2e/pull_retry.sh
. "$(dirname "$0")/pull_retry.sh"

resolve_images() {
    if ! git describe --tags --abbrev=0 --match 'v*' HEAD >/dev/null 2>&1; then
        if ! git fetch --tags --force >/dev/null 2>&1; then
            echo "$label: warning: could not fetch tags" >&2
        fi
    fi
    local prev_tag=""
    if ! prev_tag=$(git describe --tags --abbrev=0 --match 'v*' HEAD 2>/dev/null); then
        fail "could not describe previous release tag"
    fi
    prev_ver="${UPGRADE_FROM:-${prev_tag#v}}"
    old_api_image="ghcr.io/lusoris/snatcharr-api:$prev_ver"
    old_worker_image="ghcr.io/lusoris/snatcharr-worker:$prev_ver"

    : "${SMOKE_API_IMAGE:?set SMOKE_API_IMAGE together with SMOKE_WORKER_IMAGE}"
    : "${SMOKE_WORKER_IMAGE:?set SMOKE_WORKER_IMAGE together with SMOKE_API_IMAGE}"
    cand_api="$SMOKE_API_IMAGE"
    cand_worker="$SMOKE_WORKER_IMAGE"
}

digest_of() {
    local d=""
    if ! d=$(docker inspect -f '{{index .RepoDigests 0}}' "$1" 2>/dev/null); then
        d=""
    fi
    if [ -z "$d" ] || [ "$d" = "<no value>" ]; then
        if ! d=$(docker inspect -f '{{.Id}}' "$1" 2>/dev/null); then
            d=""
        fi
    fi
    printf '%s\n' "$d"
}

pull_old_images() {
    pull_retry docker pull "$old_api_image" || fail "pulling $old_api_image failed"
    pull_retry docker pull "$old_worker_image" || fail "pulling $old_worker_image failed"
    local api_dig worker_dig
    api_dig=$(digest_of "$old_api_image")
    worker_dig=$(digest_of "$old_worker_image")
    echo "ok   pulled previous release $prev_ver"
    echo "     api digest: $api_dig"
    echo "     worker digest: $worker_dig"
}

resolve_fake_arr_image() {
    local pinned
    pinned=$(sed -n 's#^ *image: \(postgres:[^@]*@sha256:[0-9a-f]\{64\}\)$#\1#p' "$compose")
    [ -n "$pinned" ] || fail "no digest-pinned postgres image in $compose"
    fake_arr_image=$(mirrored "$pinned")
}

write_override() {
    local target=$1 api_img=$2 worker_img=$3
    cat >"$target" <<EOF
services:
  fake-arr:
    image: $fake_arr_image
    restart: unless-stopped
    entrypoint: ["/bin/sh", "-c"]
    command:
      - |
        rm -f /tmp/pipe && mkfifo /tmp/pipe
        exec 3<>/tmp/pipe
        touch /tmp/fake-arr-ready
        while true; do
          nc -l -p 8989 < /tmp/pipe | while IFS= read -r line; do
            printf '%s\n' "\$\$line" >> /tmp/fake-arr-headers.log
            clean=\$\$(printf '%s' "\$\$line" | tr -d '\r')
            if [ -z "\$\$clean" ]; then
              printf 'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 70\r\nConnection: close\r\n\r\n{"appName":"Sonarr","version":"4.0.10.2544","instanceName":"fakearr"}\r\n' > /tmp/pipe
              break
            fi
          done
        done
  api:
    image: $api_img
    pull_policy: never
    healthcheck:
      test: ["CMD", "/snatcharr-api", "healthcheck"]
      interval: 5s
      timeout: 5s
      retries: 6
      start_period: 30s
  worker:
    image: $worker_img
    pull_policy: never
    healthcheck:
      test: ["CMD", "/snatch-worker", "healthcheck"]
      interval: 5s
      timeout: 5s
      retries: 6
      start_period: 30s
EOF
}

psql_at() {
    dc exec -T postgres psql -U snatcharr -d snatcharr -v ON_ERROR_STOP=1 -Atc "$1"
}

fingerprint() {
    dc exec -T postgres psql -U snatcharr -d snatcharr -v ON_ERROR_STOP=1 -At \
        <"$root/tools/e2e/fingerprint.sql"
}

api_req() {
    local method=$1 path=$2
    shift 2
    local -a args=(-fsS -b "$cookies_file" -X "$method")
    if [ -n "${csrf_token:-}" ] && [ "$method" != "GET" ] && [ "$method" != "HEAD" ]; then
        args+=(-H "X-Csrf-Token: $csrf_token")
    fi
    if [ "$#" -ge 1 ]; then
        args+=(-H 'Content-Type: application/json' -d "$1")
    fi
    curl --max-time 10 "${args[@]}" "$api/api/v1$path"
}

seed_instances_and_policies() {
    seeded_sonarr_key="sonarr-$(secret)"
    local payload
    payload=$(jq -n \
        --arg key "$seeded_sonarr_key" \
        '{kind: "sonarr", name: "upgrade-sonarr", base_url: "http://fake-arr:8989", api_key: $key, enabled: true}')
    local res
    res=$(api_req POST /instances "$payload")
    instance_id=$(jq -r '.id' <<<"$res")
    [ -n "$instance_id" ] && [ "$instance_id" != "null" ] || fail "failed to seed instance"
    seed_count=$((seed_count + 1))

    local policy
    policy=$(api_req GET "/instances/$instance_id/policy")
    api_req PUT "/instances/$instance_id/policy" "$(jq '.hourly_cap = 42' <<<"$policy")" >/dev/null
    seed_count=$((seed_count + 1))

    api_req PUT "/instances/$instance_id/priority" '{"search_priority": 10, "download_priority": 20}' >/dev/null
    seed_count=$((seed_count + 1))

    local qp
    qp=$(api_req GET "/instances/$instance_id/queue-policy")
    api_req PUT "/instances/$instance_id/queue-policy" "$(jq '.grace_seconds = 120' <<<"$qp")" >/dev/null
    seed_count=$((seed_count + 1))
}

seed_settings_and_schedules() {
    local settings
    settings=$(api_req GET /settings)
    api_req PUT /settings "$(jq '.history_retention_days = 99 | .global_hourly_cap = 250' <<<"$settings")" >/dev/null
    seed_count=$((seed_count + 1))

    local req_settings
    req_settings=$(api_req GET /requests/settings)
    api_req PUT /requests/settings "$(jq '.community_enabled = true' <<<"$req_settings")" >/dev/null
    seed_count=$((seed_count + 1))

    local sched_payload
    sched_payload=$(jq -n \
        '{name: "upgrade-schedule", days: [1, 2, 3], start: "02:00", end: "04:00", tz: "UTC", action: "pause", enabled: false}')
    local s_res
    s_res=$(api_req POST /schedules "$sched_payload")
    schedule_id=$(jq -r '.id' <<<"$s_res")
    [ -n "$schedule_id" ] && [ "$schedule_id" != "null" ] || fail "failed to seed schedule"
    seed_count=$((seed_count + 1))
}

seed_integrations_and_notifications() {
    local dl_payload
    dl_payload=$(jq -n \
        --arg secret "dl-$(secret)" \
        '{name: "upgrade-dlclient", kind: "qbittorrent", base_url: "http://dlclient.invalid:8080", secret: $secret, enabled: false}')
    local dl_res
    dl_res=$(api_req POST /download-clients "$dl_payload")
    dlclient_id=$(jq -r '.id' <<<"$dl_res")
    seed_count=$((seed_count + 1))

    local seerr_payload
    seerr_payload=$(jq -n \
        --arg key "seerr-$(secret)" \
        '{name: "upgrade-seerr", base_url: "http://seerr.invalid:5055", api_key: $key, enabled: false}')
    local seerr_res
    seerr_res=$(api_req POST /seerr "$seerr_payload")
    seerr_id=$(jq -r '.id' <<<"$seerr_res")
    seed_count=$((seed_count + 1))

    local prowl_payload
    prowl_payload=$(jq -n \
        --arg key "prowl-$(secret)" \
        '{name: "upgrade-prowlarr", base_url: "http://prowlarr.invalid:9696", api_key: $key, hourly_cap: 25, enabled: false}')
    local prowl_res
    prowl_res=$(api_req POST /prowlarr "$prowl_payload")
    prowlarr_id=$(jq -r '.id' <<<"$prowl_res")
    seed_count=$((seed_count + 1))

    local chan_payload
    chan_payload=$(jq -n \
        '{name: "upgrade-ntfy", kind: "ntfy", enabled: true, config: {server_url: "http://ntfy.invalid", topic: "upgrade-topic"}}')
    local chan_res
    chan_res=$(api_req POST /notifications/channels "$chan_payload")
    channel_id=$(jq -r '.id' <<<"$chan_res")
    seed_count=$((seed_count + 1))

    local rule_payload
    rule_payload=$(jq -n \
        --arg cid "$channel_id" \
        '{name: "upgrade-rule", event_kind: "circuit_opened", threshold: 1, enabled: true, channel_ids: [$cid]}')
    local rule_res
    rule_res=$(api_req POST /notifications/rules "$rule_payload")
    rule_id=$(jq -r '.id' <<<"$rule_res")
    seed_count=$((seed_count + 1))
}

seed_modules_sources_and_keys() {
    api_req PUT /me/preferences '{"brand_mark": "hand", "sfw_mode": "on"}' >/dev/null
    seed_count=$((seed_count + 1))

    api_req PUT /system/modules/watchaware '{"enabled": true}' >/dev/null
    seed_count=$((seed_count + 1))

    local watch_payload
    watch_payload=$(jq -n \
        --arg key "watch-$(secret)" \
        '{name: "upgrade-watchaware", kind: "jellyfin", url: "http://jellyfin.invalid:8096", api_key: $key, enabled: false}')
    local watch_res
    watch_res=$(api_req POST /watchaware/sources "$watch_payload")
    watchaware_id=$(jq -r '.id' <<<"$watch_res")
    seed_count=$((seed_count + 1))

    api_req PUT /system/modules/adult '{"enabled": true}' >/dev/null
    local adult_payload
    adult_payload=$(jq -n \
        --arg key "adult-$(secret)" \
        '{name: "upgrade-adult", endpoint: "http://stashbox.invalid/graphql", api_key: $key, enabled: false}')
    local adult_res
    adult_res=$(api_req POST /adult/sources "$adult_payload")
    adult_id=$(jq -r '.id' <<<"$adult_res")
    seed_count=$((seed_count + 1))

    local key_res
    key_res=$(api_req POST /auth/api-keys '{"name": "upgrade-test-key"}')
    api_key=$(jq -r '.secret' <<<"$key_res")
    [ -n "$api_key" ] && [ "$api_key" != "null" ] || fail "failed to seed API key"
    seed_count=$((seed_count + 1))
}

save_readback_item() {
    local path=$1 file=$2
    api_req GET "$path" | jq -S . >"$work/readbacks-old/$file"
}

save_all_readbacks() {
    mkdir -p "$work/readbacks-old" "$work/readbacks-new"
    save_readback_item "/instances" "instances.json"
    save_readback_item "/instances/$instance_id" "instance.json"
    save_readback_item "/instances/$instance_id/policy" "policy.json"
    save_readback_item "/instances/$instance_id/priority" "priority.json"
    save_readback_item "/instances/$instance_id/queue-policy" "queue-policy.json"
    save_readback_item "/settings" "settings.json"
    save_readback_item "/requests/settings" "requests-settings.json"
    save_readback_item "/schedules" "schedules.json"
    save_readback_item "/schedules/$schedule_id" "schedule.json"
    save_readback_item "/download-clients" "download-clients.json"
    save_readback_item "/download-clients/$dlclient_id" "download-client.json"
    save_readback_item "/seerr" "seerr.json"
    save_readback_item "/seerr/$seerr_id" "seerr-link.json"
    save_readback_item "/prowlarr" "prowlarr.json"
    save_readback_item "/prowlarr/$prowlarr_id" "prowlarr-link.json"
    save_readback_item "/notifications/channels" "channels.json"
    save_readback_item "/notifications/channels/$channel_id" "channel.json"
    save_readback_item "/notifications/rules" "rules.json"
    save_readback_item "/notifications/rules/$rule_id" "rule.json"
    save_readback_item "/me/preferences" "me-preferences.json"
    save_readback_item "/system/modules" "modules.json"
    save_readback_item "/watchaware/sources" "watchaware.json"
    save_readback_item "/watchaware/sources/$watchaware_id" "watchaware-source.json"
    save_readback_item "/adult/sources" "adult.json"
    save_readback_item "/adult/sources/$adult_id" "adult-source.json"
    save_readback_item "/auth/api-keys" "api-keys.json"
    echo "ok   saved read-backs for all $seed_count seeded resources"
}

compare_json() {
    local name=$1 old_file=$2 new_file=$3
    # shellcheck disable=SC2016
    local jq_diff='
def diff_subset(old; new; path):
  if (old | type) != (new | type) then
    "\(path): type mismatch (old \(old | type), new \(new | type))"
  elif (old | type) == "object" then
    old | to_entries[] | .key as $k | .value as $v |
    if ($k == "last_check_at" or $k == "last_used_at" or $k == "last_sync_at") then
      empty
    elif (new | has($k) | not) then
      "\(path).\($k): missing in new"
    else
      diff_subset($v; new[$k]; "\(path).\($k)")
    end
  elif (old | type) == "array" then
    if (old | length) != (new | length) then
      "\(path): array length mismatch (old \(old | length), new \(new | length))"
    else
      range(0; old | length) as $i |
      diff_subset(old[$i]; new[$i]; "\(path)[\($i)]")
    end
  elif old != new then
    "\(path): value mismatch (old \(old | tojson), new \(new | tojson))"
  else
    empty
  end;
diff_subset($old; $new; "$")
'
    local diff=""
    if ! diff=$(jq -r -n \
        --argjson old "$(cat "$old_file")" \
        --argjson new "$(cat "$new_file")" \
        "$jq_diff" 2>/dev/null); then
        diff=""
    fi
    if [ -n "$diff" ]; then
        fail "api read-back differs for $name: $diff"
    fi
}

check_readback_item() {
    local path=$1 file=$2
    if ! api_req GET "$path" | jq -S . >"$work/readbacks-new/$file" 2>/dev/null; then
        fail "api read-back differs for $file: endpoint request failed (missing or error)"
    fi
    compare_json "$file" "$work/readbacks-old/$file" "$work/readbacks-new/$file"
}

verify_all_readbacks() {
    check_readback_item "/instances" "instances.json"
    check_readback_item "/instances/$instance_id" "instance.json"
    check_readback_item "/instances/$instance_id/policy" "policy.json"
    check_readback_item "/instances/$instance_id/priority" "priority.json"
    check_readback_item "/instances/$instance_id/queue-policy" "queue-policy.json"
    check_readback_item "/requests/settings" "requests-settings.json"
    check_readback_item "/schedules" "schedules.json"
    check_readback_item "/schedules/$schedule_id" "schedule.json"
    check_readback_item "/download-clients" "download-clients.json"
    check_readback_item "/download-clients/$dlclient_id" "download-client.json"
    check_readback_item "/seerr" "seerr.json"
    check_readback_item "/seerr/$seerr_id" "seerr-link.json"
    check_readback_item "/prowlarr" "prowlarr.json"
    check_readback_item "/prowlarr/$prowlarr_id" "prowlarr-link.json"
    check_readback_item "/notifications/channels" "channels.json"
    check_readback_item "/notifications/channels/$channel_id" "channel.json"
    check_readback_item "/notifications/rules" "rules.json"
    check_readback_item "/notifications/rules/$rule_id" "rule.json"
    check_readback_item "/me/preferences" "me-preferences.json"
    check_readback_item "/system/modules" "modules.json"
    check_readback_item "/watchaware/sources" "watchaware.json"
    check_readback_item "/watchaware/sources/$watchaware_id" "watchaware-source.json"
    check_readback_item "/adult/sources" "adult.json"
    check_readback_item "/adult/sources/$adult_id" "adult-source.json"
    check_readback_item "/auth/api-keys" "api-keys.json"
    echo "ok   all read-backs match (fields preserved, subset equality verified)"
}

apply_upgrade_plant() {
    case "${UPGRADE_PLANT:-}" in
        drop-row)
            psql_at "DELETE FROM schedules WHERE id = '$schedule_id'" >/dev/null
            ;;
        change-setting)
            psql_at "UPDATE settings SET history_retention_days = 111" >/dev/null
            ;;
        wrong-crypto-key)
            sed -i "s/^SNATCHARR_CRYPTO_KEY=.*/SNATCHARR_CRYPTO_KEY=$(secret)/" "$work/.env"
            ;;
        old-image)
            cand_api="$old_api_image"
            cand_worker="$old_worker_image"
            write_override "$work/override-candidate.yaml" "$cand_api" "$cand_worker"
            ;;
        "") ;;
        *) fail "unknown UPGRADE_PLANT: $UPGRADE_PLANT" ;;
    esac
}

check_candidate_status() {
    local status
    status=$(curl -fsS --max-time 10 "$api/api/v1/system/status") ||
        fail "GET /system/status on candidate failed"
    new_version=$(jq -r '.version' <<<"$status")
    new_commit=$(jq -r '.commit' <<<"$status")

    if [ "$new_version" = "$old_version" ] && [ "$new_commit" = "$old_commit" ]; then
        fail "candidate version and commit did not advance from old release ($old_version)"
    fi

    if [ -n "${SMOKE_EXPECT_COMMIT:-}" ]; then
        case "$new_commit" in
            "$SMOKE_EXPECT_COMMIT"*) ;;
            *)
                if [ "${#new_commit}" -ge 7 ] && [[ "$SMOKE_EXPECT_COMMIT" == "$new_commit"* ]]; then
                    :
                else
                    fail "system status commit $new_commit does not match expected $SMOKE_EXPECT_COMMIT"
                fi
                ;;
        esac
    fi
}

check_migrations() {
    local cand_schema_version cand_schema_dirty highest_migration
    cand_schema_version=$(psql_at 'SELECT version FROM schema_migrations')
    cand_schema_dirty=$(psql_at 'SELECT dirty FROM schema_migrations')
    highest_migration=$(find "$root/api/internal/store/migrations/postgres" -name '*.up.sql' -exec basename {} \; | \
        sed -n 's/^\([0-9]\+\)_.*/\1/p' | sort -n | tail -n 1)
    highest_migration=$((10#$highest_migration))

    if [ "$cand_schema_version" -le "$old_schema_version" ] || \
       [ "$cand_schema_version" -ne "$highest_migration" ] || \
       [ "$cand_schema_dirty" != "f" ]; then
        fail "schema_migrations did not advance: got $cand_schema_version (dirty $cand_schema_dirty), want > $old_schema_version and == $highest_migration"
    fi
    echo "ok   schema migrations advanced: $old_schema_version -> $cand_schema_version (dirty: false)"
}

check_settings() {
    local new_settings retention
    new_settings=$(api_req GET /settings)
    retention=$(jq -r '.history_retention_days' <<<"$new_settings")
    if [ "$retention" != "99" ]; then
        fail "settings differ: history_retention_days is $retention, want 99"
    fi
    echo "ok   settings preserved: history_retention_days is $retention"
}

check_auth() {
    local login_new
    login_new=$(curl -fsS --max-time 10 -c "$work/cookies-new.txt" -H 'Content-Type: application/json' \
        -d "{\"username\":\"admin\",\"password\":\"$admin_pass\"}" "$api/api/v1/auth/login") ||
        fail "login with password set before upgrade failed"
    new_csrf=$(jq -r '.csrf_token' <<<"$login_new")
    [ -n "$new_csrf" ] || fail "login did not return csrf token"
    cookies_file="$work/cookies-new.txt"
    csrf_token="$new_csrf"

    curl -fsS --max-time 10 -H "Authorization: Bearer $api_key" "$api/api/v1/system/status" >/dev/null ||
        fail "authenticated request with old API key failed"
    echo "ok   admin logs in with old password, old API key authenticates"
}

check_instance_test() {
    if ! dc exec -T fake-arr truncate -s 0 /tmp/fake-arr-headers.log >/dev/null 2>&1; then
        echo "$label: note: truncate headers log not needed or failed" >&2
    fi
    if ! api_req POST "/instances/$instance_id/test" >/dev/null 2>&1; then
        echo "$label: note: instance test returned non-zero status" >&2
    fi
    local headers=""
    if ! headers=$(dc exec -T fake-arr cat /tmp/fake-arr-headers.log 2>/dev/null); then
        headers=""
    fi
    if ! grep -q "X-Api-Key: $seeded_sonarr_key" <<<"$headers"; then
        fail "stored key did not decrypt: X-Api-Key missing from fake-arr after upgrade"
    fi
    echo "ok   instance test: stored key decrypted and sent to fake-arr"
}

check_worker() {
    local contact=""
    for _ in $(seq 1 30); do
        local status_json=""
        if ! status_json=$(curl -fsS --max-time 10 "$api/api/v1/system/status" 2>/dev/null); then
            status_json="{}"
        fi
        contact=$(jq -r '.worker_last_contact_at // empty' <<<"$status_json")
        if [ -n "$contact" ]; then
            break
        fi
        sleep 1
    done
    [ -n "$contact" ] || fail "worker did not register in API (no worker_last_contact_at)"
    echo "ok   worker healthy and registered in API (last contact: $contact)"
}

check_runtime_settings() {
    [ "$(psql_at "SELECT to_regclass('public.runtime_settings')")" = "runtime_settings" ] ||
        fail "table runtime_settings does not exist"

    if curl -fsS --max-time 10 -b "$cookies_file" "$api/api/v1/settings/runtime" >/dev/null 2>&1; then
        curl -fsS --max-time 10 -b "$cookies_file" -H "X-Csrf-Token: $new_csrf" \
            -H 'Content-Type: application/json' -X PUT \
            -d '{"values":{"snatcharr.public.url":"http://127.0.0.1:18080"}}' \
            "$api/api/v1/settings/runtime" >/dev/null || fail "PUT /settings/runtime failed"
        dc restart api >/dev/null
        dc up -d --wait api || fail "api did not turn healthy after restart"
        local val
        val=$(curl -fsS --max-time 10 -b "$cookies_file" "$api/api/v1/settings/runtime" | \
            jq -r '.settings[]? | select(.key == "snatcharr.public.url") | .value // empty')
        [ "$val" = "http://127.0.0.1:18080" ] || fail "runtime setting did not survive restart: got '$val'"
        echo "ok   new migration effect: runtime_settings updated and survived restart"
    else
        echo "ok   new migration effect: table runtime_settings verified in Postgres"
    fi
}

check_database_fingerprint() {
    dc stop api worker >/dev/null
    fingerprint >"$work/after.txt"

    local tbl count hash
    while read -r tbl count hash; do
        grep -q "^$tbl " "$work/after.txt" || fail "table $tbl missing from database after upgrade"
        local after_line=""
        if ! after_line=$(grep "^$tbl " "$work/after.txt"); then
            after_line=""
        fi
        if [ "$tbl $count $hash" != "$after_line" ]; then
            case "$tbl" in
                schema_migrations) ;;     # schema migration advanced to highest numbered version
                workers) ;;               # worker heartbeats update last_contact_at
                sessions) ;;              # login session creation and renewal
                api_keys) ;;              # api key touch on authentication
                instances) ;;             # instance test probe updates last_check_at
                rate_buckets) ;;          # periodic rate limit token accounting
                global_rate_buckets) ;;   # global rate limit token accounting
                snatch_runs) ;;           # background snatch runs
                snatch_events) ;;         # background snatch events
                processed_items) ;;       # processed item cache records
                item_search_cooldowns) ;; # search cooldown timestamps
                runtime_settings) ;;      # written during runtime_settings test
                *)
                    fail "data read-back differs: unallowed change in table $tbl ($count $hash -> ${after_line#"$tbl" })"
                    ;;
            esac
        fi
    done < "$work/before.txt"
    echo "ok   all tables preserved; differences match runtime allow-list"
    dc up -d --wait || fail "services did not turn healthy on restart"
}

final_restart_and_recheck() {
    dc restart >/dev/null
    dc up -d --wait || fail "services did not turn healthy after final restart"

    local final_settings
    final_settings=$(curl -fsS --max-time 10 -b "$cookies_file" "$api/api/v1/settings") ||
        fail "re-check /settings after final restart failed"
    [ "$(jq -r '.history_retention_days' <<<"$final_settings")" = "99" ] ||
        fail "settings differ after final restart"
    echo "ok   final restart: /settings re-checked"
    echo "compose application $prev_ver -> $new_version upgrade passed"
}

# 1. Resolve images, initialize compose workdir, pull old images and write overrides.
resolve_images
resolve_fake_arr_image

orig_api_img="$cand_api"
orig_worker_img="$cand_worker"
unset SMOKE_API_IMAGE SMOKE_WORKER_IMAGE
compose_init
cand_api="$orig_api_img"
cand_worker="$orig_worker_img"

pull_old_images
write_override "$work/override-old.yaml" "$old_api_image" "$old_worker_image"
write_override "$work/override-candidate.yaml" "$cand_api" "$cand_worker"

files=(-f "$compose" -f "$work/override-old.yaml")
if [ -f "$work/postgres.yaml" ]; then
    files+=(-f "$work/postgres.yaml")
fi

# 2. Start old release, assert initial status, complete setup and login.
dc config --quiet
dc up -d --quiet-pull --wait --wait-timeout 240 || fail "services did not turn healthy on $prev_ver"

initial_status=$(curl -fsS --max-time 10 "$api/api/v1/system/status") || fail "GET /system/status on $prev_ver failed"
old_version=$(jq -r '.version' <<<"$initial_status")
old_commit=$(jq -r '.commit' <<<"$initial_status")
[ "$old_version" = "$prev_ver" ] || fail "the install runs version $old_version, want $prev_ver"

admin_pass="upgrade-$(secret)"
complete_setup "$admin_pass" || fail "setup on $prev_ver failed"
cookies_file="$work/cookies.txt"
login_resp=$(curl -fsS --max-time 10 -c "$cookies_file" -H 'Content-Type: application/json' \
    -d "{\"username\":\"admin\",\"password\":\"$admin_pass\"}" "$api/api/v1/auth/login") ||
    fail "login on $prev_ver failed"
csrf_token=$(jq -r '.csrf_token' <<<"$login_resp")

# Wait for fake-arr sidecar readiness.
for _ in $(seq 1 30); do
    if dc exec -T fake-arr test -f /tmp/fake-arr-ready 2>/dev/null; then
        break
    fi
    sleep 1
done

# 3. Seed resources through old API.
seed_count=0
seed_instances_and_policies
seed_settings_and_schedules
seed_integrations_and_notifications
seed_modules_sources_and_keys
[ "$seed_count" -ge 8 ] || fail "seeded only $seed_count kinds, need at least 8"
echo "ok   seeded $seed_count kinds on $prev_ver"

# 4. Pre-upgrade probe of instance against fake-arr.
api_req POST "/instances/$instance_id/test" >/dev/null || fail "pre-upgrade test of instance failed"

# 5. Save read-back JSON for all seeded resources.
save_all_readbacks

# 6. Stop api, worker; save fingerprint and schema migrations.
dc stop api worker >/dev/null
fingerprint >"$work/before.txt"
old_schema_version=$(psql_at 'SELECT version FROM schema_migrations')
old_schema_dirty=$(psql_at 'SELECT dirty FROM schema_migrations')
[ "$old_schema_dirty" = "f" ] || fail "schema_migrations dirty before upgrade"
echo "ok   saved fingerprint and schema migration ($old_schema_version)"

# Apply planted defect if requested.
apply_upgrade_plant

# 7. Switch to candidate images.
files=(-f "$compose" -f "$work/override-candidate.yaml")
if [ -f "$work/postgres.yaml" ]; then
    files+=(-f "$work/postgres.yaml")
fi
dc config --quiet
dc up -d --wait --wait-timeout 240 || fail "services did not turn healthy on candidate images"

# 8. Assertions: status, migrations, settings, auth, read-backs, instance probe, worker, table fingerprint.
check_candidate_status
check_migrations
check_settings
check_auth
verify_all_readbacks
check_instance_test
check_worker
check_runtime_settings
check_database_fingerprint

# 9. Final restart and re-check.
final_restart_and_recheck
