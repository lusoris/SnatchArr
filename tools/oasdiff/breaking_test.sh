#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for breaking.sh: each spec under testdata/ represents a change scenario
# against base.yaml. breaking.sh must fail on undeclared breaking changes (removed path,
# removed required response property, new required request param) and pass on additive
# changes or breaking changes declared via a breaking conventional commit subject.
#
#   tools/oasdiff/breaking_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
failed=0

# expect WANT REVISION_SPEC SUBJECT [CHECK_RULE]:
# breaking.sh base.yaml REVISION_SPEC SUBJECT exits 0 (WANT pass) or non-zero (WANT fail).
# When CHECK_RULE is given:
#   for fail: CHECK_RULE must be named in the output.
#   for pass: CHECK_RULE must be named in the output (e.g. break declaration confirmation).
expect() {
  want="$1"
  rev="$2"
  subj="$3"
  rule="${4:-}"
  got=0
  sh "$HERE/breaking.sh" "$HERE/testdata/base.yaml" "$HERE/testdata/$rev" "$subj" >"$WORK/out" 2>&1 || got=$?
  case "$want:$got" in
    pass:0)
      if [ -n "$rule" ]; then
        if grep -qF "$rule" "$WORK/out"; then
          echo "ok   $rev ($subj): passes, found '$rule'"
        else
          echo "FAIL $rev ($subj): passed without expected text '$rule'" >&2
          sed 's/^/  /' "$WORK/out" >&2
          failed=1
        fi
      else
        echo "ok   $rev ($subj): passes as expected"
      fi
      ;;
    fail:0 | pass:*)
      echo "FAIL $rev ($subj): exit $got, want $want" >&2
      sed 's/^/  /' "$WORK/out" >&2
      failed=1
      ;;
    fail:*)
      if [ -n "$rule" ]; then
        if grep -qF "$rule" "$WORK/out"; then
          echo "ok   $rev ($subj): fails on $rule"
        else
          echo "FAIL $rev ($subj): exit $got without naming '$rule'" >&2
          sed 's/^/  /' "$WORK/out" >&2
          failed=1
        fi
      else
        echo "ok   $rev ($subj): fails as expected"
      fi
      ;;
  esac
}

expect fail removed-path.yaml "chore(api): drop status" api-path-removed-without-deprecation
expect fail removed-required-response-property.yaml "fix(api): drop item name" response-required-property-removed
expect fail new-required-request-param.yaml "feat(api): require filter" request-parameter-became-required
expect pass added-path.yaml "feat(api): add ping endpoint"
expect pass removed-path.yaml "feat(api)!: drop x" "breaking change declared in subject"

exit "$failed"
