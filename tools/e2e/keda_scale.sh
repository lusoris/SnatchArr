#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# KEDA scales the worker on queued runs (#43). On the kind cluster of `make kind-up` this test
#   1. installs KEDA and the minimal Prometheus of deploy/kustomize/kind/prometheus.yaml, then
#      applies the chart with values-kind.yaml and values-keda.yaml;
#   2. planted: points the ScaledObject at a query that reads 0, queues 50 runs against *arr
#      instances on an unroutable address (each run waits out its *arr timeout), and requires the
#      worker to stay at minReplicas while Prometheus still reports at least 40 queued runs;
#   3. restores the chart's query and requires more than minReplicas within SCALE_UP_SECS (120);
#   4. cancels every run and requires minReplicas again within SCALE_DOWN_SECS (600: the HPA
#      scale-down stabilization window of 300 s plus the metric scrape and poll delays).
#
# Needs KUBECONFIG for that cluster (kind runs only inside the incus VM), helm, kubectl, curl and
# jq. Expects a fresh install: the admin is created through the setup wizard, or logged in with
# KEDA_PROOF_PASSWORD when the wizard already ran.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
kind_dir="$root/deploy/kustomize/kind"
ns=snatcharr
keda_version="${KEDA_VERSION:-2.21.0}"
port="${KEDA_PROOF_PORT:-18284}"
password="${KEDA_PROOF_PASSWORD:-keda-proof-password-1234}"
up_secs="${SCALE_UP_SECS:-120}"
down_secs="${SCALE_DOWN_SECS:-600}"
min_replicas=1
runs_wanted=50
work=$(mktemp -d)
pf_pid=""
cleanup() {
    if [ -n "$pf_pid" ] && kill -0 "$pf_pid" 2>/dev/null; then
        kill "$pf_pid"
    fi
    rm -rf "$work"
}
trap cleanup EXIT

say() { printf '%s %s\n' "$(date +%H:%M:%S)" "$*"; }
fail() {
    say "FAIL: $*" >&2
    exit 1
}

replicas() { kubectl -n "$ns" get deploy/snatcharr-worker -o jsonpath='{.spec.replicas}'; }

# queued: the gauge the default query reads, as Prometheus reports it (empty result = 0).
queued() {
    kubectl -n monitoring exec deploy/prometheus -- promtool query instant http://localhost:9090 \
        'max(snatcharr_runs{status="queued",namespace="snatcharr"})' 2>/dev/null |
        sed -n 's/.*=> \([0-9.]*\) @.*/\1/p' | cut -d. -f1 | grep . || echo 0
}

set_query() {
    kubectl -n "$ns" patch scaledobject/snatcharr-worker --type=json \
        -p "$(jq -cn --arg q "$1" '[{op:"replace",path:"/spec/triggers/0/metadata/query",value:$q}]')" >/dev/null
}

say "1. KEDA $keda_version, Prometheus, chart with values-keda.yaml"
kubectl apply --server-side -f "https://github.com/kedacore/keda/releases/download/v$keda_version/keda-$keda_version.yaml" >/dev/null
kubectl -n keda rollout status deploy/keda-operator --timeout=180s
kubectl -n keda rollout status deploy/keda-metrics-apiserver --timeout=180s
kubectl apply --server-side -f "$kind_dir/prometheus.yaml" >/dev/null
kubectl -n monitoring rollout status deploy/prometheus --timeout=180s
# helm template cannot look up the stored secret: pass the running setup secret, or the render
# writes a fresh random one that the running api never sees.
setup_secret=$(kubectl -n "$ns" get secret snatcharr-secrets -o jsonpath='{.data.setup-secret}' | base64 -d)
[ -n "$setup_secret" ] || fail "no setup-secret in secret/snatcharr-secrets: run make kind-up first"
helm template snatcharr "$root/deploy/helm/snatcharr" --namespace "$ns" \
    -f "$kind_dir/values-kind.yaml" -f "$kind_dir/values-keda.yaml" \
    --set-string "secrets.setupSecret=$setup_secret" >"$work/chart.yaml"
kubectl -n "$ns" apply --server-side --force-conflicts -f "$work/chart.yaml" >/dev/null
kubectl -n "$ns" rollout status deploy/snatcharr-api --timeout=300s
kubectl -n "$ns" wait scaledobject/snatcharr-worker --for=condition=Ready --timeout=180s
default_query=$(kubectl -n "$ns" get scaledobject/snatcharr-worker -o jsonpath='{.spec.triggers[0].metadata.query}')
say "ScaledObject ready, query: $default_query"

kubectl -n "$ns" port-forward svc/snatcharr-api "$port:8080" >"$work/pf.log" 2>&1 &
pf_pid=$!
api_base="http://127.0.0.1:$port/api/v1"
for _ in $(seq 1 30); do
    curl -fsS --max-time 2 -o /dev/null "http://127.0.0.1:$port/livez" 2>/dev/null && break
    sleep 1
done
jar="$work/cookies.txt"
csrf=()
api() {
    local args=(-X "$1" --cookie "$jar" --cookie-jar "$jar" "${csrf[@]}")
    [ $# -lt 3 ] || args+=(-H 'Content-Type: application/json' -d "$3")
    curl -fsS --max-time 10 "${args[@]}" "$api_base$2"
}
creds=$(jq -cn --arg p "$password" '{username:"admin",password:$p}')
setup=$(jq -c --arg s "$setup_secret" '. + {setup_secret:$s}' <<<"$creds")
if session=$(api POST /auth/setup "$setup" 2>/dev/null); then
    token=$(jq -r '.csrf_token // empty' <<<"$session")
else
    token=$(api POST /auth/login "$creds" | jq -r '.csrf_token // empty')
fi
[ -n "$token" ] || fail "neither setup nor login returned a csrf_token"
csrf=(-H "X-CSRF-Token: $token")

say "2. planted: query reads 0 while $runs_wanted runs are queued"
# A rerun on the same cluster starts from its own leftovers removed (names are unique).
for id in $(api GET /instances | jq -r '.[] | select(.name | startswith("keda-proof-")) | .id'); do
    api DELETE "/instances/$id" >/dev/null || fail "leftover instance $id was not deleted"
done
set_query 'max(snatcharr_runs{status="queued",namespace="planted-absent"}) or vector(0)'
# The planted phase starts from minReplicas (an earlier run may have left the worker scaled up).
for _ in $(seq 1 $((down_secs / 10))); do
    [ "$(replicas)" -le "$min_replicas" ] && break
    sleep 10
done
[ "$(replicas)" -le "$min_replicas" ] || fail "worker not at $min_replicas replicas before the planted phase"
for i in $(seq 1 $((runs_wanted / 2))); do
    key=$(od -An -tx1 -N16 /dev/urandom | tr -d " \n")
    body=$(jq -cn --arg n "keda-proof-$i" --arg u "http://10.255.255.1:$((9000 + i))" --arg k "$key" \
        '{kind:"sonarr",name:$n,base_url:$u,api_key:$k,enabled:true}')
    id=$(api POST /instances "$body" | jq -r '.id // empty')
    [ -n "$id" ] || fail "instance $i was not created"
    api POST "/instances/$id/runs" '{"kind":"missing"}' >/dev/null
    api POST "/instances/$id/runs" '{"kind":"upgrade"}' >/dev/null
done
for _ in $(seq 1 24); do
    [ "$(queued)" -ge 40 ] && break
    sleep 5
done
[ "$(queued)" -ge 40 ] || fail "planted: Prometheus never reported 40 queued runs (got $(queued))"
for _ in $(seq 1 18); do
    r=$(replicas)
    [ "$r" -eq "$min_replicas" ] || fail "planted: worker scaled to $r on a query that reads 0"
    sleep 5
done
q=$(queued)
[ "$q" -ge 40 ] || fail "planted: queue drained to $q during the hold, the hold proves nothing"
say "planted: worker held $min_replicas replica for 90 s with $q runs queued"

say "3. chart query restored: scale up within $up_secs s"
set_query "$default_query"
start=$(date +%s)
until [ "$(replicas)" -gt "$min_replicas" ]; do
    [ $(($(date +%s) - start)) -lt "$up_secs" ] || fail "no scale-up within $up_secs s (queued $(queued))"
    sleep 2
done
say "scaled up to $(replicas) after $(($(date +%s) - start)) s with $(queued) runs queued"

say "4. cancel every run: back to $min_replicas within $down_secs s"
api GET '/runs?limit=500' | jq -r '.[] | select(.status == "queued" or .status == "leased") | .id' >"$work/runs.txt"
while read -r id; do
    # A run that finished between the list and the cancel refuses it; the scale-down still counts.
    if ! api POST "/runs/$id/cancel" >/dev/null 2>&1; then
        say "run $id: cancel refused (finished meanwhile)"
    fi
done <"$work/runs.txt"
start=$(date +%s)
until [ "$(replicas)" -eq "$min_replicas" ]; do
    [ $(($(date +%s) - start)) -lt "$down_secs" ] || fail "no scale-down within $down_secs s (replicas $(replicas), queued $(queued))"
    sleep 5
done
say "scaled down to $min_replicas after $(($(date +%s) - start)) s"
say "PASS"
