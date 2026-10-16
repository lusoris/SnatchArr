#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Runs an end-to-end script once per planted defect, then once clean. Each case
# "plant:message" sets the environment variable VAR to plant; the run must fail and print
# message, or the check proves nothing. The clean run must pass.
#
#   tools/e2e/planted.sh VAR script.sh "plant:message"...
set -euo pipefail

var=$1
script=$2
shift 2
[ "$#" -gt 0 ] || { echo "planted: no planted case for $script" >&2; exit 1; }
for case in "$@"; do
    plant=${case%%:*}
    want=${case#*:}
    if out=$(env "$var=$plant" bash "$script" 2>&1); then
        echo "planted: case $plant passed; $script checks nothing" >&2
        exit 1
    fi
    case "$out" in
    *"$want"*) echo "ok   planted case $plant refused: $want" ;;
    *)
        printf '%s\n' "$out" | tail -20 >&2
        echo "planted: case $plant failed without '$want'" >&2
        exit 1
        ;;
    esac
done
bash "$script"
