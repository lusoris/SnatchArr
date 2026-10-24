#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Runs an end-to-end script once per planted defect, then once clean. Each case
# "plant:message" sets the environment variable VAR to plant; the run must fail and print
# message, or the check proves nothing. The clean run must pass.
#
# Every run ends after PLANTED_SECONDS (default 600) and then fails with the tail of its
# output. A run that hung once took the whole CI job to its timeout with nothing in the log,
# since the output of a planted case is captured (main, 2026-10-18).
#
#   tools/e2e/planted.sh VAR script.sh "plant:message"...
set -euo pipefail

var=$1
script=$2
shift 2
seconds=${PLANTED_SECONDS:-600}
[ "$#" -gt 0 ] || { echo "planted: no planted case for $script" >&2; exit 1; }

# bounded NAME COMMAND...: runs COMMAND for at most $seconds; a run past it fails as a hang.
# timeout signals the whole process group, and the script's EXIT trap still cleans up.
bounded() {
    local name=$1 status=0
    shift
    timeout --kill-after=60 "$seconds" "$@" || status=$?
    if [ "$status" -eq 124 ] || [ "$status" -eq 137 ]; then
        echo "planted: $name of $script did not end within ${seconds}s" >&2
    fi
    return "$status"
}

for case in "$@"; do
    plant=${case%%:*}
    want=${case#*:}
    status=0
    out=$(bounded "case $plant" env "$var=$plant" bash "$script" 2>&1) || status=$?
    if [ "$status" -eq 0 ]; then
        echo "planted: case $plant passed; $script checks nothing" >&2
        exit 1
    fi
    if [ "$status" -eq 124 ] || [ "$status" -eq 137 ]; then
        # Reason line precedes container logs; print it before the tail.
        printf '%s\n' "$out" | awk '/^FAIL /' >&2
        printf '%s\n' "$out" | tail -20 >&2
        exit 1
    fi
    case "$out" in
    *"$want"*) echo "ok   planted case $plant refused: $want" ;;
    *)
        # Reason line precedes container logs; print it before the tail.
        printf '%s\n' "$out" | awk '/^FAIL /' >&2
        printf '%s\n' "$out" | tail -20 >&2
        echo "planted: case $plant failed without '$want'" >&2
        exit 1
        ;;
    esac
done
bounded "clean run" bash "$script"
