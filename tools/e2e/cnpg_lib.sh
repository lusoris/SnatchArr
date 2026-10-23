# shellcheck shell=bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Shared steps of the chart tests on CloudNativePG (cnpg_pg_upgrade.sh, cnpg_backup_restore.sh),
# sourced, not run. The caller sets `label` (the prefix of its messages) and `default_ns` before
# sourcing; the namespace and port come from <PREFIX>_NAMESPACE and <PREFIX>_PORT, where the caller
# sets `prefix` (UPGRADE, RESTORE). The caller fills `password` before it calls login.
#
# <PREFIX>_API_IMAGE and <PREFIX>_WORKER_IMAGE (repository:tag, already present on the nodes, for
# example through kind load) run the chart on those images with pullPolicy Never; `images` holds
# the --set arguments. CI sets them to the images built from the checkout: on a release pull
# request the chart already pins the new appVersion, whose images are not published yet. Unset,
# the chart runs the released images of its appVersion. <PREFIX>_KEEP=1 keeps the namespace.

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
ns_var="${prefix:?set prefix before sourcing cnpg_lib.sh}_NAMESPACE"
port_var="${prefix}_PORT"
keep_var="${prefix}_KEEP"
ns="${!ns_var:-${default_ns:?set default_ns before sourcing cnpg_lib.sh}}"
port="${!port_var:-18282}"
release=snatcharr
cluster="$release-db"
api="http://127.0.0.1:$port"
work=$(mktemp -d)
pf=""

# fail, add_image and images.
# shellcheck source=tools/e2e/live_lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/live_lib.sh"

cleanup() {
    status=$?
    if [ -n "$pf" ]; then
        kill "$pf" 2>/dev/null || echo "$label: port-forward already gone" >&2
    fi
    if [ "$status" -ne 0 ]; then
        kubectl -n "$ns" get cluster,backup,pods,jobs -o wide >&2 || echo "$label: no state" >&2
        kubectl -n "$ns" logs "deploy/$release-api" --tail 40 >&2 || echo "$label: no API log" >&2
    fi
    if [ "${!keep_var:-0}" != 1 ]; then
        kubectl delete namespace "$ns" --wait=false >/dev/null 2>&1 ||
            echo "$label: could not delete namespace $ns" >&2
    fi
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT

primary() { kubectl -n "$ns" get cluster "$cluster" -o jsonpath='{.status.currentPrimary}'; }

psql_at() {
    kubectl -n "$ns" exec -i "$(primary)" -c postgres -- psql -d snatcharr -v ON_ERROR_STOP=1 -At "$@"
}

fingerprint() { psql_at -q <"$root/tools/e2e/fingerprint.sql"; }

# wait_healthy waits up to $1 seconds for the operator to report the cluster healthy.
wait_healthy() {
    local i phase
    for ((i = 0; i < $1 / 5; i++)); do
        phase=$(kubectl -n "$ns" get cluster "$cluster" -o jsonpath='{.status.phase}')
        [ "$phase" = "Cluster in healthy state" ] && return 0
        sleep 5
    done
    fail "cluster $cluster is '$phase' after $1 s, want healthy"
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
        kill "$pf" 2>/dev/null || echo "$label: previous port-forward already gone" >&2
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

# setup_admin completes the first-run setup with a new random password and logs in once.
setup_admin() {
    local setup_secret
    password="$label-$(openssl rand -hex 32)"
    setup_secret=$(kubectl -n "$ns" get secret "$release-secrets" -o jsonpath='{.data.setup-secret}' | base64 -d)
    curl -fsS --max-time 10 -H 'Content-Type: application/json' \
        -d "{\"username\":\"admin\",\"password\":\"$password\",\"setup_secret\":\"$setup_secret\"}" \
        "$api/api/v1/auth/setup" >/dev/null || fail "setup failed"
    login || fail "login after the setup failed"
}

# check_app scales the API up and checks that the setup stays complete and the admin logs in
# with the password set before.
check_app() {
    local status
    scale_app 1
    port_forward
    status=$(curl -fsS --max-time 10 "$api/api/v1/auth/setup/status") || fail "GET setup/status failed"
    jq -e '.setup_complete == true' >/dev/null <<<"$status" ||
        fail "setup/status is $status, want setup_complete true"
    login || fail "login with the password set before failed"
}

api_image_var="${prefix}_API_IMAGE"
worker_image_var="${prefix}_WORKER_IMAGE"
add_image api "${!api_image_var:-}"
add_image worker "${!worker_image_var:-}"
echo "$label: api image ${!api_image_var:-released appVersion}, worker image ${!worker_image_var:-released appVersion}"
