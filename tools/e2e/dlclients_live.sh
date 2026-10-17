#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Observes the six download clients SnatchArr supports, each a real release
# (tools/e2e/dlclients/compose.yaml), through the adapters production uses
# (TestLiveDownloadClients in api/internal/dlclients/live_test.go). Two runs: every client
# running, then SABnzbd and NZBGet paused. Each run must pass and must not skip.
#
# SNATCHARR_DLCLIENTS_PLANT plants a defect the test must refuse:
#   secret  every client is handed a secret it does not accept
#   pause   the second run claims SABnzbd and NZBGet are paused without pausing them
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
plant=${SNATCHARR_DLCLIENTS_PLANT:-}
project="snatcharr-dlclients-$$"
work=$(mktemp -d)
TRANSMISSION_PASS=$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')
NZBGET_PASS=$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')
export TRANSMISSION_PASS NZBGET_PASS
qbit="http://127.0.0.1:${QBITTORRENT_PORT:-18101}"
transmission="http://127.0.0.1:${TRANSMISSION_PORT:-18102}"
deluge="http://127.0.0.1:${DELUGE_PORT:-18103}"
rtorrent="http://127.0.0.1:${RTORRENT_PORT:-18104}"
sab="http://127.0.0.1:${SABNZBD_PORT:-18105}"
nzbget="http://127.0.0.1:${NZBGET_PORT:-18106}"

case "$plant" in
'' | secret | pause) ;;
*) echo "dlclients-live: unknown SNATCHARR_DLCLIENTS_PLANT '$plant'" >&2; exit 1 ;;
esac

dc() { docker compose -p "$project" -f "$root/tools/e2e/dlclients/compose.yaml" "$@"; }

cleanup() {
    status=$?
    if [ "$status" -ne 0 ] && ! dc logs --no-color --tail 30; then
        echo "dlclients-live: could not read the client logs" >&2
    fi
    if ! dc down -v --remove-orphans >/dev/null 2>&1; then
        echo "dlclients-live: could not remove project $project" >&2
    fi
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT

fail() {
    echo "dlclients-live: $*" >&2
    exit 1
}

# answers URL: true once URL answers HTTP at all (any status), within 90 s.
answers() {
    local i
    for i in $(seq 1 90); do
        if curl -s -o /dev/null --max-time 2 "$1"; then return 0; fi
        sleep 1
    done
    fail "$1 did not answer within 90 s (tries: $i)"
}

# jsonrpc URL BODY [curl options]: one JSON-RPC call with a deadline.
jsonrpc() {
    local url=$1 body=$2
    shift 2
    curl -sf --max-time 10 -H 'Content-Type: application/json' -d "$body" "$@" "$url"
}

qbit_password() {
    local i pass
    for i in $(seq 1 60); do
        pass=$(dc logs --no-color qbittorrent 2>/dev/null | sed -n 's/.*temporary password is provided for this session: \([^ ]*\).*/\1/p' | tail -1 | tr -d '\r')
        if [ -n "$pass" ]; then printf '%s' "$pass"; return 0; fi
        sleep 1
    done
    fail "qBittorrent printed no temporary password within 60 s (tries: $i)"
}

sab_key() {
    local i key
    for i in $(seq 1 60); do
        key=$(dc exec -T sabnzbd sed -n 's/^api_key = //p' /config/sabnzbd.ini 2>/dev/null | tr -d '\r')
        if [ -n "$key" ]; then printf '%s' "$key"; return 0; fi
        sleep 1
    done
    fail "SABnzbd wrote no API key within 60 s (tries: $i)"
}

# deluge_connect: what a user does once in the web UI's connection manager.
deluge_connect() {
    local jar="$work/deluge.cookies" host
    jsonrpc "$deluge/json" '{"method":"auth.login","params":["deluge"],"id":1}' -c "$jar" >/dev/null ||
        fail "Deluge refused the default web password"
    host=$(jsonrpc "$deluge/json" '{"method":"web.get_hosts","params":[],"id":2}' -b "$jar" |
        sed -n 's/.*"result": *\[\[ *"\([0-9a-f]*\)".*/\1/p')
    [ -n "$host" ] || fail "Deluge lists no daemon host"
    jsonrpc "$deluge/json" "{\"method\":\"web.connect\",\"params\":[\"$host\"],\"id\":3}" -b "$jar" >/dev/null ||
        fail "Deluge did not connect to its daemon"
}

# usenet_paused true|false: set SABnzbd and NZBGet to that state.
usenet_paused() {
    local sab_mode=resume nzbget_method=resumedownload
    if [ "$1" = true ]; then sab_mode=pause nzbget_method=pausedownload; fi
    curl -sf --max-time 10 -o /dev/null "$sab/api?mode=$sab_mode&apikey=$sab_secret&output=json" ||
        fail "SABnzbd refused mode=$sab_mode"
    jsonrpc "$nzbget/jsonrpc" "{\"method\":\"$nzbget_method\",\"params\":[]}" -u "snatch:$NZBGET_PASS" >/dev/null ||
        fail "NZBGet refused $nzbget_method"
}

# clients PAUSED: the test input; plant=secret spoils every secret.
clients() {
    local spoil=''
    if [ "$plant" = secret ]; then spoil=-planted; fi
    cat <<EOF
[
 {"kind":"qbittorrent","url":"$qbit","username":"admin","secret":"$qbit_secret$spoil","auth":true},
 {"kind":"transmission","url":"$transmission/transmission/","username":"snatch","secret":"$TRANSMISSION_PASS$spoil","auth":true},
 {"kind":"deluge","url":"$deluge","secret":"deluge$spoil","auth":true},
 {"kind":"rtorrent","url":"$rtorrent/RPC2$spoil"},
 {"kind":"sabnzbd","url":"$sab","secret":"$sab_secret$spoil","auth":true,"paused":$1},
 {"kind":"nzbget","url":"$nzbget","username":"snatch","secret":"$NZBGET_PASS$spoil","auth":true,"paused":$1}
]
EOF
}

# observe PAUSED: one test run; it must pass and must not skip.
observe() {
    local out
    clients "$1" >"$work/clients.json"
    if ! out=$(cd "$root" && SNATCHARR_DLCLIENTS_LIVE="$work/clients.json" \
        go test -count=1 -run '^TestLiveDownloadClients$' -v ./api/internal/dlclients/ 2>&1); then
        printf '%s\n' "$out" >&2
        fail "TestLiveDownloadClients failed with paused=$1"
    fi
    case "$out" in
    *"--- SKIP"*) printf '%s\n' "$out" >&2; fail "TestLiveDownloadClients skipped" ;;
    *"--- PASS: TestLiveDownloadClients "*) echo "ok   six clients observed, paused=$1" ;;
    *) printf '%s\n' "$out" >&2; fail "TestLiveDownloadClients did not run" ;;
    esac
}

dc up -d --quiet-pull
for url in "$qbit" "$transmission/transmission/rpc" "$deluge/json" "$rtorrent/RPC2" "$sab/api" "$nzbget/jsonrpc"; do
    answers "$url"
done
qbit_secret=$(qbit_password)
sab_secret=$(sab_key)
deluge_connect

usenet_paused false
observe false
if [ "$plant" != pause ]; then usenet_paused true; fi
observe true
