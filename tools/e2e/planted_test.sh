#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Cases for planted.sh: a check that refuses its plant and passes clean is accepted; a plant
# that passes, a plant refused with the wrong message, and a run that hangs (in a planted case
# or in the clean run) are each refused, the hang within the PLANTED_SECONDS bound.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0

# The script under test: PLANT=bad fails with "refused bad", PLANT=quiet passes, PLANT=hang and
# HANG_CLEAN=1 sleep far past the bound; the clean run passes.
cat >"$work/check.sh" <<'SCRIPT'
case "${PLANT:-}" in
bad) echo "refused bad" >&2; exit 1 ;;
quiet) exit 0 ;;
hang) sleep 120 ;;
esac
[ "${HANG_CLEAN:-0}" = 1 ] && sleep 120
exit 0
SCRIPT

# expect NAME WANT_STATUS WANT_TEXT [ENV=VALUE...] -- CASE...: one planted.sh run.
expect() {
    local name=$1 want=$2 text=$3 status=0 out start took
    shift 3
    local envs=()
    while [ "$1" != -- ]; do envs+=("$1"); shift; done
    shift
    start=$SECONDS
    out=$(env PLANTED_SECONDS=2 "${envs[@]}" bash "$here/planted.sh" PLANT "$work/check.sh" "$@" 2>&1) || status=$?
    took=$((SECONDS - start))
    if [ "$want" = 0 ] && [ "$status" != 0 ]; then
        echo "FAIL $name: status $status, want 0: $out" >&2
        failed=1
    elif [ "$want" != 0 ] && [ "$status" = 0 ]; then
        echo "FAIL $name: passed, want a refusal" >&2
        failed=1
    elif [[ $out != *"$text"* ]]; then
        echo "FAIL $name: output lacks '$text': $out" >&2
        failed=1
    elif [ "$took" -gt 30 ]; then
        echo "FAIL $name: took ${took}s, past the 2 s bound plus the kill grace" >&2
        failed=1
    else
        echo "ok   $name"
    fi
}

expect "refused plant, clean pass" 0 "planted case bad refused: refused bad" -- "bad:refused bad"
expect "a plant that passes is refused" 1 "case quiet passed" -- "quiet:anything"
expect "a wrong message is refused" 1 "failed without 'other text'" -- "bad:other text"
expect "a hung planted case ends at the bound" 1 "case hang of $work/check.sh did not end within 2s" -- "hang:never"
expect "a hung clean run ends at the bound" 1 "clean run of $work/check.sh did not end within 2s" HANG_CLEAN=1 -- "bad:refused bad"
expect "no planted case is refused" 1 "no planted case" --
exit "$failed"
