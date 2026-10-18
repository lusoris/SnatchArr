#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Cases for pull_retry.sh: a pull that succeeds at once, one that succeeds on the last allowed
# try, one that never succeeds (refused after exactly PULL_TRIES tries, not more), and a
# docker compose call that hangs, ended by compose at DC_SECONDS.
set -euo pipefail

# shellcheck source=tools/e2e/pull_retry.sh
. "$(dirname "$0")/pull_retry.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0
export PULL_WAIT=0

# fake_pull N: fails until it has been called N times, then succeeds; counts calls in $work/n.
# shellcheck disable=SC2329 # called through pull_retry
fake_pull() {
    local n
    n=$(($(cat "$work/n") + 1))
    echo "$n" >"$work/n"
    [ "$n" -ge "$1" ]
}

# expect NAME WANT_STATUS WANT_CALLS TRIES SUCCEED_ON
expect() {
    local name=$1 want=$2 calls=$3 status=0
    echo 0 >"$work/n"
    PULL_TRIES=$4 pull_retry fake_pull "$5" 2>/dev/null || status=$?
    if [ "$status" = "$want" ] && [ "$(cat "$work/n")" = "$calls" ]; then
        echo "ok   $name"
    else
        echo "FAIL $name: status $status after $(cat "$work/n") calls, want $want after $calls" >&2
        failed=1
    fi
}

expect "first try succeeds" 0 1 4 1
expect "last allowed try succeeds" 0 4 4 4
expect "never succeeds: refused after the bound" 1 4 4 99
expect "one try allowed, it fails" 1 1 1 2
# A docker compose call that hangs ends at DC_SECONDS with status 124 (a fake docker sleeps).
mkdir "$work/bin"
printf '#!/bin/sh\nexec sleep 30\n' >"$work/bin/docker"
chmod +x "$work/bin/docker"
start=$SECONDS
status=0
PATH="$work/bin:$PATH" DC_SECONDS=1 compose pull --quiet || status=$?
if [ "$status" = 124 ] && [ $((SECONDS - start)) -lt 10 ]; then
    echo "ok   a hung compose call ends at the bound"
else
    echo "FAIL a hung compose call: status $status after $((SECONDS - start))s, want 124 within 10s" >&2
    failed=1
fi
exit "$failed"
