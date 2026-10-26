#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Tests the dependency snapshot check function without running Docker:
# - one accepting fixture
# - one refusing fixture (missing direct dependency)
# - one boundary fixture (empty manifests)
#
#   tools/ci/dependency_snapshot_test.sh
#
# Exit status: 0 all cases pass, 1 failure.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
SCRIPT="$HERE/dependency_snapshot.py"
FIXTURES="$HERE/testdata/dependency_snapshot"

cd "$REPO_ROOT"

run_case() {
  name="$1"
  want_rc="$2"
  needle="$3"
  snap="$4"
  pkg="$5"

  got_rc=0
  out="$(python3 -I "$SCRIPT" --check-fixture "$snap" "$pkg" 2>&1)" || got_rc=$?

  if [ "$got_rc" -ne "$want_rc" ]; then
    printf 'FAIL %s: exit %s, want %s\n%s\n' "$name" "$got_rc" "$want_rc" "$out" >&2
    exit 1
  fi

  case "$out" in
    *"$needle"*) ;;
    *)
      printf 'FAIL %s: output did not contain "%s"\n%s\n' "$name" "$needle" "$out" >&2
      exit 1
      ;;
  esac

  printf 'ok   %s\n' "$name"
}

# 1. Accepting fixture
run_case "accepting fixture" 0 "Fixture check ok" \
  "$FIXTURES/accept_snapshot.json" "$FIXTURES/accept_package.json"

# 2. Refusing fixture (missing direct dep)
run_case "refusing fixture (missing direct dep)" 1 "direct dependencies missing from resolved snapshot: missing-dep" \
  "$FIXTURES/refuse_snapshot.json" "$FIXTURES/refuse_package.json"

# 3. Boundary fixture (empty manifests)
run_case "boundary fixture (empty manifests)" 1 "snapshot missing manifest for pnpm-lock.yaml" \
  "$FIXTURES/empty_snapshot.json" "$FIXTURES/empty_package.json"
