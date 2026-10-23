# shellcheck shell=bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Shared helpers of the end-to-end tests (dlclients_live.sh, forceimport_live.sh,
# integrations_live.sh, smoke.sh, web-e2e-api.sh, chart_install.sh, cnpg_lib.sh), sourced, not run.
# The caller sets `label` (the prefix of its messages) before sourcing.

fail() {
    echo "${label:?set label before sourcing live_lib.sh}: $*" >&2
    exit 1
}

# add_image adds the --set arguments for an image of repository:tag to `images`.
images=()
add_image() {
    local component=$1 ref=$2
    [ -n "$ref" ] || return 0
    [[ "$ref" == *:* && "${ref##*:}" != */* && "$ref" != *@* ]] ||
        fail "$component image $ref is not repository:tag"
    images+=(--set "image.$component.repository=${ref%:*}" --set "image.$component.tag=${ref##*:}"
        --set "image.$component.pullPolicy=Never")
}

# answers URL [TIMEOUT]: true once URL answers HTTP at all (any status), within TIMEOUT (default 90 s).
answers() {
    local url=$1 timeout=${2:-${ANSWERS_TIMEOUT:-90}} i
    for i in $(seq 1 "$timeout"); do
        if curl -s -o /dev/null --max-time 2 "$url"; then return 0; fi
        sleep 1
    done
    fail "$url did not answer within ${timeout} s (tries: ${i:-0})"
}

# qbit_password [TIMEOUT]: reads the temporary password qBittorrent logged at startup.
qbit_password() {
    local timeout=${1:-${QBIT_PASSWORD_TIMEOUT:-60}} i pass
    for i in $(seq 1 "$timeout"); do
        pass=$(dc logs --no-color qbittorrent 2>/dev/null | sed -n 's/.*temporary password is provided for this session: \([^ ]*\).*/\1/p' | tail -1 | tr -d '\r')
        if [ -n "$pass" ]; then printf '%s' "$pass"; return 0; fi
        sleep 1
    done
    fail "qBittorrent printed no temporary password within ${timeout} s (tries: ${i:-0})"
}

# free_port START [MAX_TRIES]: the first port from START on which nothing listens (default at most 50 tried).
free_port() {
    local port=$1 tries=0 max_tries=${2:-${FREE_PORT_MAX_TRIES:-50}}
    while (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; do
        port=$((port + 1))
        tries=$((tries + 1))
        [ "$tries" -lt "$max_tries" ] || fail "no free port from $1"
    done
    echo "$port"
}
