#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for lint.sh (gap audit A3-03): each spec under testdata/ breaks one rule of
# tools/spectral.yaml, and lint.sh must fail on it and name that rule. A warning-level rule
# (oas3-unused-component) is among them, so --fail-severity warn is proved as well.
#
#   tools/spectral/lint_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
failed=0

# expect WANT SPEC [RULE]: lint.sh SPEC exits 0 (WANT pass) or non-zero (WANT fail) and, when it
# fails, names RULE.
expect() {
  got=0
  sh "$HERE/lint.sh" "$HERE/testdata/$2" >"$WORK/out" 2>&1 || got=$?
  case "$1:$got" in
    pass:0) echo "ok   $2: passes" ;;
    fail:0 | pass:*)
      echo "FAIL $2: exit $got, want $1" >&2
      sed 's/^/  /' "$WORK/out" >&2
      failed=1
      ;;
    fail:*)
      if grep -qF "$3" "$WORK/out"; then
        echo "ok   $2: fails on $3"
      else
        echo "FAIL $2: exit $got without naming $3" >&2
        sed 's/^/  /' "$WORK/out" >&2
        failed=1
      fi
      ;;
  esac
}

expect pass good.yaml
expect fail no-operation-id.yaml operation-operationId
expect fail no-tags.yaml operation-tags
expect fail unused-component.yaml oas3-unused-component
exit "$failed"
