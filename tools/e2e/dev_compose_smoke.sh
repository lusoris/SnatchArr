#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Dev compose smoke test (gap audit C9-08): starts deploy/compose/docker-compose.yml with both
# profiles, as `make dev` and docs/metrics.md use it, and proves
#   a. Postgres turns healthy and Sonarr and Radarr answer /ping;
#   b. Prometheus scrapes the OTel collector (target up);
#   c. Grafana has the Prometheus data source working and the provisioned SnatchArr dashboard.
#
# DEV_PLANT=no-dashboards (empty dashboard folder) or DEV_PLANT=bad-target (collector target
# on a closed port) plants a defect the smoke must refuse. Needs docker compose, curl and jq.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
project="snatcharr-devsmoke-$$"
files=(-f "$root/deploy/compose/docker-compose.yml")

case "${DEV_PLANT:-}" in
"") ;;
no-dashboards)
    mkdir "$work/empty"
    cat >"$work/plant.yaml" <<EOF
services:
  grafana:
    volumes:
      - $work/empty:/var/lib/grafana/dashboards:ro
EOF
    files+=(-f "$work/plant.yaml")
    ;;
bad-target)
    sed 's/otel-collector:8889/otel-collector:9999/' "$root/deploy/compose/prometheus.yml" >"$work/prometheus.yml"
    cat >"$work/plant.yaml" <<EOF
services:
  prometheus:
    volumes:
      - $work/prometheus.yml:/etc/prometheus/prometheus.yml:ro
EOF
    files+=(-f "$work/plant.yaml")
    ;;
*)
    echo "dev-compose-smoke: unknown DEV_PLANT '$DEV_PLANT'" >&2
    exit 2
    ;;
esac

dc() {
    docker compose -p "$project" "${files[@]}" --profile arr --profile observability "$@"
}

fail() {
    echo "FAIL dev-compose-smoke: $*" >&2
    dc ps -a >&2 || echo "dev-compose-smoke: docker compose ps failed" >&2
    dc logs --tail 30 >&2 || echo "dev-compose-smoke: docker compose logs failed" >&2
    exit 1
}

cleanup() {
    dc down -v --remove-orphans >/dev/null 2>&1 ||
        echo "dev-compose-smoke: docker compose down failed; remove project $project by hand" >&2
    rm -rf "$work"
}
trap cleanup EXIT

# until SECONDS CMD...: retry CMD once a second for at most SECONDS seconds.
until_ok() {
    local limit=$1 i
    shift
    for ((i = 0; i < limit; i++)); do
        if "$@" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    return 1
}

get() {
    curl -fsS --max-time 5 "$@"
}

dc config --quiet
dc up -d --quiet-pull --wait --wait-timeout 240 || fail "services did not start"

health=$(dc ps --format '{{.Health}}' postgres)
[ "$health" = healthy ] || fail "postgres health is '$health', want healthy"
echo "ok   postgres healthy"

for arr in sonarr:8989 radarr:7878; do
    until_ok 120 get "http://127.0.0.1:${arr#*:}/ping" || fail "${arr%%:*} did not answer /ping"
    echo "ok   ${arr%%:*} answers /ping"
done

collector_up() {
    get http://127.0.0.1:9091/api/v1/targets |
        jq -e '.data.activeTargets[] | select(.labels.job == "otel-collector") | select(.health == "up")'
}
until_ok 60 collector_up || fail "the otel-collector target is not up in Prometheus"
echo "ok   prometheus scrapes the otel-collector"

until_ok 90 get http://127.0.0.1:3000/api/health || fail "grafana did not answer /api/health"
ds=$(get http://127.0.0.1:3000/api/datasources/uid/prometheus/health) ||
    fail "the grafana Prometheus data source check failed"
jq -e '.status == "OK"' >/dev/null <<<"$ds" || fail "the grafana Prometheus data source reports $ds"
echo "ok   grafana Prometheus data source OK"

dashboards() {
    get 'http://127.0.0.1:3000/api/search?type=dash-db' | jq -e 'length > 0'
}
until_ok 30 dashboards || fail "grafana provisioned 0 dashboards"
echo "ok   grafana provisioned $(get 'http://127.0.0.1:3000/api/search?type=dash-db' | jq -r 'map(.title) | join(", ")')"
echo "dev-compose-smoke: ok"
