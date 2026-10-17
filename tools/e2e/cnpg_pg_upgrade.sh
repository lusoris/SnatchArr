#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Postgres major upgrade of a chart install on CloudNativePG (#252), the steps of
# docs/deployment.md ("Postgres major upgrade"): installs the chart with
# postgres.cnpg.imageName on the previous major, completes the first-run setup and logs in,
# then sets imageName to the current major and proves
#   a. the operator ran its offline in-place upgrade: the data directory reports the new major
#      and the server answers with it;
#   b. every table of the public schema holds the same rows as before (fingerprint.sql), and
#      the migration version is at least 7;
#   c. the API starts on the upgraded database, the setup stays complete and the admin logs
#      in with the password set before the upgrade.
# Planted case, run first on the same install: a chart upgrade that leaves imageName unchanged
# must be refused as "Postgres 17 ... want 18".
#
# Needs a cluster with the CloudNativePG operator (CI: the kind cluster of the deploy job; on a
# workstation only inside a VM), helm, kubectl, curl, jq and openssl.
# UPGRADE_API_IMAGE and UPGRADE_WORKER_IMAGE (repository:tag, already present on the nodes, for
# example through kind load) run the chart on those images with pullPolicy Never. CI sets them to
# the images built from the checkout: on a release pull request the chart already pins the new
# appVersion, whose images are not published yet. Unset, the chart runs the released images of
# its appVersion.
set -euo pipefail

# Operand images of the same OS distribution, by version and digest; Renovate moves them and
# holds the old one on its major (renovate.json).
OLD_IMAGE=ghcr.io/cloudnative-pg/postgresql:17.10-system-trixie@sha256:74f29d3343f8db89135b33b0b4c55641c415b96d08605034a6d0c2782f929146
NEW_IMAGE=ghcr.io/cloudnative-pg/postgresql:18.6-system-trixie@sha256:5efb66993894055f0aa50bd94337c6c4ff2e2129f4d433c527151aebbf283fb0
old_major=17
new_major=18

root=$(cd "$(dirname "$0")/../.." && pwd)
ns="${UPGRADE_NAMESPACE:-snatcharr-pgup}"
release=snatcharr
cluster="$release-db"
port="${UPGRADE_PORT:-18282}"
api="http://127.0.0.1:$port"
work=$(mktemp -d)
pf=""

fail() {
    echo "cnpg-pg-upgrade: $*" >&2
    exit 1
}

cleanup() {
    status=$?
    if [ -n "$pf" ]; then
        kill "$pf" 2>/dev/null || echo "cnpg-pg-upgrade: port-forward already gone" >&2
    fi
    if [ "$status" -ne 0 ]; then
        kubectl -n "$ns" get cluster,pods,jobs -o wide >&2 || echo "cnpg-pg-upgrade: no state" >&2
        kubectl -n "$ns" logs "deploy/$release-api" --tail 40 >&2 || echo "cnpg-pg-upgrade: no API log" >&2
    fi
    if [ "${UPGRADE_KEEP:-0}" != 1 ]; then
        kubectl delete namespace "$ns" --wait=false >/dev/null 2>&1 ||
            echo "cnpg-pg-upgrade: could not delete namespace $ns" >&2
    fi
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT

primary() { kubectl -n "$ns" get cluster "$cluster" -o jsonpath='{.status.currentPrimary}'; }

psql_at() {
    kubectl -n "$ns" exec -i "$(primary)" -c postgres -- psql -d snatcharr -v ON_ERROR_STOP=1 -At "$@"
}

server_major() { psql_at -c 'SHOW server_version_num' | awk '{ print int($1 / 10000) }'; }

fingerprint() { psql_at <"$root/tools/e2e/fingerprint.sql"; }

# wait_major waits up to $2 seconds for the operator to report data directory major $1 on a
# healthy cluster, then checks that the server answers with that major.
wait_major() {
    local want=$1 i got phase
    for ((i = 0; i < $2 / 5; i++)); do
        got=$(kubectl -n "$ns" get cluster "$cluster" -o jsonpath='{.status.pgDataImageInfo.majorVersion}')
        phase=$(kubectl -n "$ns" get cluster "$cluster" -o jsonpath='{.status.phase}')
        if [ "$got" = "$want" ] && [ "$phase" = "Cluster in healthy state" ]; then
            break
        fi
        sleep 5
    done
    [ "$got" = "$want" ] || fail "data directory reports Postgres $got ($phase), want $want"
    got=$(server_major)
    [ "$got" = "$want" ] || fail "the server answers as Postgres $got, want $want"
}

scale_app() {
    kubectl -n "$ns" scale "deploy/$release-api" "deploy/$release-worker" --replicas="$1" >/dev/null
    if [ "$1" -gt 0 ]; then
        kubectl -n "$ns" rollout status "deploy/$release-api" --timeout=300s >/dev/null
    else
        kubectl -n "$ns" wait pod -l app.kubernetes.io/component=api --for=delete --timeout=120s
    fi
}

port_forward() {
    if [ -n "$pf" ]; then
        kill "$pf" 2>/dev/null || echo "cnpg-pg-upgrade: previous port-forward already gone" >&2
    fi
    kubectl -n "$ns" port-forward "deploy/$release-api" "$port:8080" >"$work/pf.log" 2>&1 &
    pf=$!
    for _ in $(seq 30); do
        if curl -fsS --max-time 2 -o /dev/null "$api/readyz"; then
            return 0
        fi
        sleep 1
    done
    fail "the API did not answer through the port-forward"
}

login() {
    curl -fsS --max-time 10 -o /dev/null -H 'Content-Type: application/json' \
        -d "{\"username\":\"admin\",\"password\":\"$password\"}" "$api/api/v1/auth/login"
}

# chart_upgrade sets imageName and keeps the API and worker scaled to zero, so nothing writes
# between the two fingerprints.
chart_upgrade() {
    helm upgrade "$release" "$root/deploy/helm/snatcharr" -n "$ns" --reuse-values \
        --set "postgres.cnpg.imageName=$1" --set api.replicaCount=0 --set worker.replicaCount=0 \
        >/dev/null
}

# add_image adds the --set arguments for an UPGRADE_<component>_IMAGE of repository:tag.
images=()
add_image() {
    local component=$1 ref=$2
    [ -n "$ref" ] || return 0
    [[ "$ref" == *:* && "${ref##*:}" != */* && "$ref" != *@* ]] ||
        fail "$component image $ref is not repository:tag"
    images+=(--set "image.$component.repository=${ref%:*}" --set "image.$component.tag=${ref##*:}"
        --set "image.$component.pullPolicy=Never")
}
add_image api "${UPGRADE_API_IMAGE:-}"
add_image worker "${UPGRADE_WORKER_IMAGE:-}"
echo "cnpg-pg-upgrade: api image ${UPGRADE_API_IMAGE:-released appVersion}, worker image ${UPGRADE_WORKER_IMAGE:-released appVersion}"

# 1. A chart install on the previous major with a completed setup and one login.
kubectl create namespace "$ns" >/dev/null
helm install "$release" "$root/deploy/helm/snatcharr" -n "$ns" "${images[@]}" \
    --set "postgres.cnpg.imageName=$OLD_IMAGE" --set postgres.cnpg.storage.size=1Gi \
    --wait --timeout 10m >/dev/null || fail "chart install on Postgres $old_major failed"
wait_major "$old_major" 300
port_forward
password="upgrade-$(openssl rand -hex 32)"
setup_secret=$(kubectl -n "$ns" get secret "$release-secrets" -o jsonpath='{.data.setup-secret}' | base64 -d)
curl -fsS --max-time 10 -H 'Content-Type: application/json' \
    -d "{\"username\":\"admin\",\"password\":\"$password\",\"setup_secret\":\"$setup_secret\"}" \
    "$api/api/v1/auth/setup" >/dev/null || fail "setup on Postgres $old_major failed"
login || fail "login on Postgres $old_major failed"
scale_app 0
fingerprint >"$work/before.txt"
grep -q '^users 1 ' "$work/before.txt" || fail "no user row before the upgrade"
echo "ok   Postgres $old_major install: setup done, $(wc -l <"$work/before.txt") tables"

# 2. Planted: a chart upgrade that skips the image change must be refused.
chart_upgrade "$OLD_IMAGE"
if out=$( (wait_major "$new_major" 60) 2>&1); then
    fail "planted: an upgrade without the new imageName passed"
fi
case "$out" in
*"Postgres $old_major"*"want $new_major"*) echo "ok   planted skipped image change refused: $out" ;;
*) fail "planted: refused for another reason: $out" ;;
esac

# 3. The documented upgrade: imageName to the new major, the operator does the rest.
chart_upgrade "$NEW_IMAGE"
wait_major "$new_major" 900
kubectl -n "$ns" get events --field-selector "involvedObject.name=$cluster" -o json |
    jq -r '.items[].message' | grep -i 'major' | tail -5 || echo "(no major upgrade events listed)"
echo "ok   operator upgraded the data directory: Postgres $new_major serves the database"

# 4. Read back, first the database alone (nothing writes yet), then through the API.
fingerprint >"$work/after.txt"
diff -u "$work/before.txt" "$work/after.txt" >&2 || fail "data read-back differs after the upgrade"
echo "ok   all $(wc -l <"$work/after.txt") tables read back unchanged"
version=$(psql_at -c 'SELECT version FROM schema_migrations WHERE NOT dirty')
[ -n "$version" ] && [ "$version" -ge 7 ] || fail "migration version is '$version', want at least 7"
echo "ok   migration version $version, not dirty"
psql_at -c 'ANALYZE' >/dev/null
scale_app 1
port_forward
status=$(curl -fsS --max-time 10 "$api/api/v1/auth/setup/status") || fail "GET setup/status failed"
jq -e '.setup_complete == true' >/dev/null <<<"$status" ||
    fail "setup/status after the upgrade is $status, want setup_complete true"
login || fail "login with the password set on Postgres $old_major failed"
echo "ok   API on Postgres $new_major: setup complete, admin logs in"
echo "chart CloudNativePG Postgres $old_major -> $new_major upgrade passed"
