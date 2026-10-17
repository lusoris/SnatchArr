#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Reads a real Cleanuparr release (tools/e2e/cleanuparr/compose.yaml) through the client
# production uses (TestLiveCleanuparr in api/internal/cleanuparrclient/live_test.go). The
# script sets Cleanuparr up as a user would: admin account, API key, one Sonarr instance.
# Two runs: dry run off, then on. Each run must pass and must not skip.
#
# SNATCHARR_CLEANUPARR_PLANT plants a defect the test must refuse:
#   key     the test is handed an API key Cleanuparr does not accept
#   dryrun  the second run claims dry run is on without switching it on
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
plant=${SNATCHARR_CLEANUPARR_PLANT:-}
project="snatcharr-cleanuparr-$$"
work=$(mktemp -d)
url="http://127.0.0.1:${CLEANUPARR_PORT:-18111}"
version=$(sed -n 's|.*image: ghcr.io/cleanuparr/cleanuparr:\([^@]*\)@.*|\1|p' "$root/tools/e2e/cleanuparr/compose.yaml")
admin_pass="Live-$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')"
sonarr_name=live-sonarr
sonarr_url=http://sonarr.invalid:8989

case "$plant" in
'' | key | dryrun) ;;
*) echo "cleanuparr-live: unknown SNATCHARR_CLEANUPARR_PLANT '$plant'" >&2; exit 1 ;;
esac

# shellcheck source=tools/e2e/pull_retry.sh
. "$root/tools/e2e/pull_retry.sh"
dc() { docker compose -p "$project" -f "$root/tools/e2e/cleanuparr/compose.yaml" "$@"; }

cleanup() {
    status=$?
    if [ "$status" -ne 0 ] && ! dc logs --no-color --tail 30; then
        echo "cleanuparr-live: could not read the Cleanuparr log" >&2
    fi
    if ! dc down -v --remove-orphans >/dev/null 2>&1; then
        echo "cleanuparr-live: could not remove project $project" >&2
    fi
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT

fail() {
    echo "cleanuparr-live: $*" >&2
    exit 1
}

# answers URL: true once URL answers HTTP 200, within 120 s.
answers() {
    local i
    for i in $(seq 1 120); do
        if curl -sf -o /dev/null --max-time 2 "$1"; then return 0; fi
        sleep 1
    done
    fail "$1 did not answer within 120 s (tries: $i)"
}

# call METHOD PATH [BODY]: one admin API call with a deadline; prints the response body.
call() {
    local args=(-X "$1" -H "Authorization: Bearer $token")
    if [ "$#" -gt 2 ]; then args+=(-H 'Content-Type: application/json' -d "$3"); fi
    curl -sf --max-time 10 "${args[@]}" "$url$2"
}

# setup: what a user does on first start: create the admin, sign in, read the API key, and
# add one Sonarr instance.
setup() {
    local creds login
    creds=$(jq -cn --arg p "$admin_pass" '{username: "admin", password: $p}')
    curl -sf --max-time 10 -o /dev/null -H 'Content-Type: application/json' -d "$creds" "$url/api/auth/setup/account" ||
        fail "Cleanuparr refused the admin account"
    curl -sf --max-time 10 -o /dev/null -X POST "$url/api/auth/setup/complete" ||
        fail "Cleanuparr did not complete its setup"
    login=$(curl -sf --max-time 10 -H 'Content-Type: application/json' -d "$creds" "$url/api/auth/login") ||
        fail "Cleanuparr refused the admin login"
    token=$(jq -r '.tokens.accessToken // empty' <<<"$login")
    [ -n "$token" ] || fail "Cleanuparr returned no access token"
    api_key=$(call GET /api/account/api-key | jq -r '.apiKey // empty')
    [ -n "$api_key" ] || fail "Cleanuparr returned no API key"
    # Cleanuparr only stores this key; a fresh random one keeps a key-shaped literal out of the tree.
    sonarr_key=$(od -An -tx1 -N16 /dev/urandom | tr -d ' \n')
    call POST /api/configuration/sonarr/instances "$(jq -cn --arg n "$sonarr_name" --arg u "$sonarr_url" --arg k "$sonarr_key" \
        '{name: $n, url: $u, apiKey: $k, enabled: true, version: 4}')" >/dev/null ||
        fail "Cleanuparr refused the Sonarr instance"
}

# dry_run true|false: set the general dry-run switch, keeping every other general setting.
dry_run() {
    local general
    general=$(call GET /api/configuration/general) || fail "Cleanuparr did not return its general settings"
    call PUT /api/configuration/general "$(jq -c --argjson d "$1" '.dryRun = $d' <<<"$general")" >/dev/null ||
        fail "Cleanuparr refused dryRun = $1"
}

# observe DRYRUN: one test run; it must pass and must not skip. plant=key spoils the API key.
observe() {
    local out key=$api_key
    if [ "$plant" = key ]; then key=$api_key-planted; fi
    jq -n --arg url "$url" --arg key "$key" --arg v "$version" --arg n "$sonarr_name" --arg s "$sonarr_url" \
        --argjson d "$1" '{url: $url, api_key: $key, version: $v, sonarr_name: $n, sonarr_url: $s, dry_run: $d}' \
        >"$work/cleanuparr.json"
    if ! out=$(cd "$root" && SNATCHARR_CLEANUPARR_LIVE="$work/cleanuparr.json" \
        go test -count=1 -run '^TestLiveCleanuparr$' -v ./api/internal/cleanuparrclient/ 2>&1); then
        printf '%s\n' "$out" >&2
        fail "TestLiveCleanuparr failed with dryRun=$1"
    fi
    case "$out" in
    *"--- SKIP"*) printf '%s\n' "$out" >&2; fail "TestLiveCleanuparr skipped" ;;
    *"--- PASS: TestLiveCleanuparr "*) echo "ok   Cleanuparr $version read, dryRun=$1" ;;
    *) printf '%s\n' "$out" >&2; fail "TestLiveCleanuparr did not run" ;;
    esac
}

[ -n "$version" ] || fail "no Cleanuparr release tag in tools/e2e/cleanuparr/compose.yaml"
pull_retry dc pull --quiet
dc up -d --pull never
answers "$url/api/auth/status"
setup

observe false
if [ "$plant" != dryrun ]; then dry_run true; fi
observe true
