#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for gotest.sh. A planted red go test must fail the wrapper and show its name
# and its message; a green one must pass with no output; a bad call is a usage error.
#
#   tools/cover/gotest_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
failed=0

# expect WANT NAME DIR NEEDLE...: gotest.sh over go test in DIR exits WANT and prints every NEEDLE
# (no NEEDLE: prints nothing).
expect() {
  want="$1"
  name="$2"
  dir="$3"
  shift 3
  got=0
  out=$(cd "$HERE/testdata/$dir" && GOWORK=off GOFLAGS=-mod=mod sh "$HERE/gotest.sh" "$tmp/$name.log" -- go test -count=1 ./... 2>&1) || got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: exit $got, want $want" >&2
    printf '  %s\n' "$out" >&2
    failed=1
    return 0
  fi
  if [ "$#" -eq 0 ] && [ -n "$out" ]; then
    echo "FAIL $name: printed output on a pass" >&2
    printf '  %s\n' "$out" >&2
    failed=1
    return 0
  fi
  for needle in "$@"; do
    case "$out" in
    *"$needle"*) ;;
    *)
      echo "FAIL $name: output lacks \"$needle\"" >&2
      printf '  %s\n' "$out" >&2
      failed=1
      return 0
      ;;
    esac
  done
  echo "ok   $name: exit $got"
}

expect 1 planted_red red "--- FAIL: TestPlantedRed" "planted failure: this line must reach the gate log"
expect 0 green green

got=0
sh "$HERE/gotest.sh" "$tmp/usage.log" go test >/dev/null 2>&1 || got=$?
if [ "$got" -ne 2 ]; then
  echo "FAIL usage: exit $got, want 2" >&2
  failed=1
else
  echo "ok   usage: exit $got"
fi
exit "$failed"
