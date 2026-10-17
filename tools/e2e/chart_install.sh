#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Chart install and upgrade on a real cluster (#144). A server-side dry run cannot see a pod that
# never becomes Ready: 0.1.0 shipped a CSRF secret the API refused at start. This test
#   1. planted: installs the chart with an existing Secret that holds the published development
#      crypto key, which the chart renders but the API refuses in the prod profile; wait_ready
#      must refuse the install and name that error from the API log;
#   2. installs the chart with its defaults on CloudNativePG, waits until the API and worker
#      Deployments are Available and GET /readyz answers 200 through a port-forward;
#   3. installs the last published chart (CHART_PREVIOUS_VERSION, default: the newest version in
#      the OCI repository) on its released images, upgrades it to the chart of this checkout and
#      checks the same, plus that the generated Secret survived the upgrade unchanged.
# Each step runs in its own namespace, deleted before the next one starts.
#
# Needs a cluster with the CloudNativePG operator (CI: the kind cluster of the deploy job; on a
# workstation only inside a VM), helm, kubectl and curl. CHART_API_IMAGE and CHART_WORKER_IMAGE
# (repository:tag, already present on the nodes, for example through kind load) run the chart of
# this checkout on those images with pullPolicy Never; unset, it runs the released images of its
# appVersion.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
chart="$root/deploy/helm/snatcharr"
oci=oci://ghcr.io/lusoris/charts/snatcharr
release=snatcharr
port="${CHART_PORT:-18283}"
ready_secs="${CHART_READY_SECS:-480}"
work=$(mktemp -d)
ns=""
pf=""

fail() {
    echo "chart-install: $*" >&2
    exit 1
}

# diagnose prints what an operator needs to see why an install is not Ready.
diagnose() {
    kubectl -n "$1" get pods,deploy,cluster -o wide >&2 || echo "chart-install: no state in $1" >&2
    kubectl -n "$1" get events --sort-by=.lastTimestamp 2>/dev/null | tail -20 >&2 ||
        echo "chart-install: no events in $1" >&2
    kubectl -n "$1" logs "deploy/$release-api" --tail 30 >&2 || echo "chart-install: no API log in $1" >&2
}

drop_namespace() {
    if [ -n "$pf" ]; then
        kill "$pf" 2>/dev/null || echo "chart-install: port-forward already gone" >&2
        pf=""
    fi
    if [ -n "$ns" ] && [ "${CHART_KEEP:-0}" != 1 ]; then
        kubectl delete namespace "$ns" --timeout=180s >/dev/null 2>&1 ||
            echo "chart-install: could not delete namespace $ns" >&2
    fi
    ns=""
}

cleanup() {
    status=$?
    if [ "$status" -ne 0 ] && [ -n "$ns" ]; then
        diagnose "$ns"
    fi
    drop_namespace
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT

new_namespace() {
    drop_namespace
    ns="snatcharr-$1"
    kubectl create namespace "$ns" >/dev/null
}

# api_log prints the last API log lines, with the kubectl logs flags $@. It fails when there is no
# log to read: an error, an empty log, or the kubelet's "unable to retrieve container logs"
# placeholder, which kubectl prints as the log itself with exit status 0.
api_log() {
    local out
    out=$(kubectl -n "$ns" logs "deploy/$release-api" "$@" --tail 20 2>/dev/null) || return 1
    case "$out" in
    "" | "unable to retrieve container logs"*) return 1 ;;
    esac
    printf '%s\n' "$out"
}

# crashing names an API container that restarted at least three times, with its last log lines.
# While the kubelet replaces the container, neither log can be read; that is no verdict yet, and
# the next poll asks again.
crashing() {
    local restarts log
    restarts=$(kubectl -n "$ns" get pods -l app.kubernetes.io/component=api \
        -o jsonpath='{range .items[*]}{.status.containerStatuses[0].restartCount}{"\n"}{end}')
    for r in $restarts; do
        if [ "${r:-0}" -ge 3 ]; then
            log=$(api_log --previous) || log=$(api_log) || return 1
            printf '%s\n' "$log"
            return 0
        fi
    done
    return 1
}

available() {
    [ "$(kubectl -n "$ns" get deploy "$release-$1" -o jsonpath='{.status.conditions[?(@.type=="Available")].status}')" = True ]
}

# wait_ready waits up to ready_secs for the API and worker Deployments to be Available and for
# GET /readyz to answer 200. It fails at once when the API crash-loops, with the API log.
wait_ready() {
    local start=$SECONDS log
    while ((SECONDS - start < ready_secs)); do
        if log=$(crashing); then
            fail "API crash-loops in $ns: $log"
        fi
        if available api && available worker; then
            break
        fi
        sleep 5
    done
    if ! available api || ! available worker; then
        fail "API or worker not Available in $ns after ${ready_secs}s"
    fi
    kubectl -n "$ns" port-forward "deploy/$release-api" "$port:8080" >"$work/pf.log" 2>&1 &
    pf=$!
    for _ in $(seq 30); do
        if curl -fsS --max-time 2 -o /dev/null "http://127.0.0.1:$port/readyz"; then
            echo "ok   $ns Ready in $((SECONDS - start))s: API and worker Available, /readyz 200"
            return 0
        fi
        sleep 1
    done
    fail "GET /readyz in $ns did not answer 200"
}

# secret_keys prints the generated Secret one key per line, sorted (a go-template ranges over a
# map in key order).
secret_keys() {
    # shellcheck disable=SC2016 # a go-template, not shell expansion
    kubectl -n "$ns" get secret "$release-secrets" \
        -o go-template='{{range $k, $v := .data}}{{$k}}={{$v}}{{"\n"}}{{end}}'
}

# add_image adds the --set arguments for a CHART_<component>_IMAGE of repository:tag.
images=()
add_image() {
    local component=$1 ref=$2
    [ -n "$ref" ] || return 0
    [[ "$ref" == *:* && "${ref##*:}" != */* && "$ref" != *@* ]] ||
        fail "$component image $ref is not repository:tag"
    images+=(--set "image.$component.repository=${ref%:*}" --set "image.$component.tag=${ref##*:}"
        --set "image.$component.pullPolicy=Never")
}
add_image api "${CHART_API_IMAGE:-}"
add_image worker "${CHART_WORKER_IMAGE:-}"
echo "chart-install: api image ${CHART_API_IMAGE:-released appVersion}, worker image ${CHART_WORKER_IMAGE:-released appVersion}"
small=(--set postgres.cnpg.storage.size=1Gi)

# 1. Planted: the chart renders an install the API refuses; wait_ready must say so.
new_namespace plant
dev_key=$(sed -n 's/^const DevCryptoKey = "\([0-9a-f]*\)"$/\1/p' "$root/api/internal/config/config.go")
[ -n "$dev_key" ] || fail "planted: DevCryptoKey not found in api/internal/config/config.go"
kubectl -n "$ns" create secret generic planted \
    --from-literal=crypto-key="$dev_key" --from-literal=csrf-secret="$(openssl rand -hex 32)" \
    --from-literal=apikey-secret="$(openssl rand -hex 24)" \
    --from-literal=worker-token="$(openssl rand -hex 24)" \
    --from-literal=setup-secret="$(openssl rand -hex 24)" >/dev/null
helm install "$release" "$chart" -n "$ns" "${images[@]}" "${small[@]}" \
    --set secrets.existingSecret=planted >/dev/null
if out=$( (wait_ready) 2>&1); then
    fail "planted: an install on the published development key became Ready"
fi
case "$out" in
*"crash-loops"*"published development key"*) echo "ok   planted development-key install refused" ;;
*) fail "planted: refused for another reason: $out" ;;
esac

# 2. The default install of this checkout.
new_namespace install
helm install "$release" "$chart" -n "$ns" "${images[@]}" "${small[@]}" >/dev/null
wait_ready

# 3. Upgrade from the last published chart.
previous="${CHART_PREVIOUS_VERSION:-$(helm show chart "$oci" | sed -n 's/^version: //p')}"
[ -n "$previous" ] || fail "no published chart version found at $oci"
new_namespace upgrade
helm install "$release" "$oci" --version "$previous" -n "$ns" "${small[@]}" >/dev/null
wait_ready
echo "ok   published chart $previous installed"
kill "$pf" 2>/dev/null || echo "chart-install: port-forward already gone" >&2
pf=""
secret_keys >"$work/secret-before.txt"
helm upgrade "$release" "$chart" -n "$ns" --reuse-values "${images[@]}" >/dev/null
kubectl -n "$ns" rollout status "deploy/$release-api" --timeout="${ready_secs}s" >/dev/null ||
    fail "API rollout after the upgrade from $previous did not finish"
wait_ready
secret_keys >"$work/secret-after.txt"
cmp -s "$work/secret-before.txt" "$work/secret-after.txt" ||
    fail "the generated Secret changed in the upgrade from $previous"
echo "ok   upgrade from chart $previous to this checkout: Ready, Secret unchanged"
echo "chart install and upgrade passed"
