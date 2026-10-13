#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for floor.sh. Refused: a figure below the floor, an empty figure (a failed
# `go tool cover`), a figure that is not a number, a malformed floor. Accepted: a figure above
# the floor and one exactly at it.
#
#   tools/cover/floor_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
failed=0

# expect WANT NAME args...: floor.sh args exits WANT.
expect() {
  want="$1"
  name="$2"
  shift 2
  got=0
  out=$(sh "$HERE/floor.sh" "$@" 2>&1) || got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: exit $got, want $want" >&2
    printf '  %s\n' "$out" >&2
    failed=1
    return 0
  fi
  echo "ok   $name: exit $got"
}

expect 0 above 41.8 41
expect 0 at_floor 41 41
expect 0 at_floor_decimal 41.0 41
expect 1 below 40.9 41
expect 1 just_below 40.99 41
expect 1 zero 0.0 41
expect 1 empty "" 41
expect 1 not_a_number "[no" 41
expect 1 two_dots 41.8.1 41
expect 1 floor_not_a_number 41.8 x
expect 1 floor_empty 41.8 ""
expect 2 usage 41.8
exit "$failed"
