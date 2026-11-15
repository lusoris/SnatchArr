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
expect pass removed-path.yaml "$(printf 'chore: merge main into x\nfeat(api)!: drop x')" \
  "breaking change declared in subject"
expect fail removed-path.yaml "$(printf 'chore: merge main into x\nfix(api): drop x')" \
  api-path-removed-without-deprecation

# git_fixture ARGS...: git without user or system config, so fixture commits ignore host signing and hooks.
git_fixture() {
  env -i PATH="$PATH" GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 git "$@"
}

repo="$WORK/repo"
mkdir "$repo"
git_fixture -C "$repo" init -q
git_fixture -C "$repo" config user.name "OASDiff Test"
git_fixture -C "$repo" config user.email "oasdiff-test@example.invalid"
printf 'base\n' >"$repo/base.txt"
git_fixture -C "$repo" add base.txt
git_fixture -C "$repo" -c commit.gpgsign=false commit -q -m "chore: base"
base_ref="$(git_fixture -C "$repo" rev-parse HEAD)"
primary_branch="$(git_fixture -C "$repo" symbolic-ref --short HEAD)"

printf 'breaking\n' >"$repo/breaking.txt"
git_fixture -C "$repo" add breaking.txt
git_fixture -C "$repo" -c commit.gpgsign=false commit -q -m "feat(api)!: drop x"
printf 'follow-up\n' >"$repo/follow-up.txt"
git_fixture -C "$repo" add follow-up.txt
git_fixture -C "$repo" -c commit.gpgsign=false commit -q -m "fix: follow-up"

git_fixture -C "$repo" switch -q -c side "$base_ref"
printf 'side\n' >"$repo/side.txt"
git_fixture -C "$repo" add side.txt
git_fixture -C "$repo" -c commit.gpgsign=false commit -q -m "fix: side change"
git_fixture -C "$repo" switch -q "$primary_branch"
git_fixture -C "$repo" -c commit.gpgsign=false merge -q --no-ff side -m "chore: merge main into x"

got=0
(
  cd "$repo"
  sh "$HERE/branch_subjects.sh" "$base_ref"
) >"$WORK/subjects" 2>"$WORK/subjects.err" || got=$?
if [ "$got" -eq 0 ] && grep -qxF "feat(api)!: drop x" "$WORK/subjects"; then
  echo "ok   branch_subjects includes branch breaking subject"
else
  echo "FAIL branch_subjects: missing branch breaking subject (exit $got)" >&2
  sed 's/^/  /' "$WORK/subjects.err" >&2
  failed=1
fi
if [ "$got" -eq 0 ] && [ "$(tail -n 1 "$WORK/subjects")" = "chore: merge main into x" ]; then
  echo "ok   branch_subjects ends with HEAD subject"
else
  echo "FAIL branch_subjects: last line is not the HEAD subject" >&2
  failed=1
fi

git_fixture -C "$repo" switch -q -c nonbreaking "$base_ref"
printf 'nonbreaking\n' >"$repo/nonbreaking.txt"
git_fixture -C "$repo" add nonbreaking.txt
git_fixture -C "$repo" -c commit.gpgsign=false commit -q -m "fix(api): y"
got=0
(
  cd "$repo"
  sh "$HERE/branch_subjects.sh" "$base_ref"
) >"$WORK/nonbreaking-subjects" 2>"$WORK/nonbreaking-subjects.err" || got=$?
if [ "$got" -eq 0 ] && ! grep -q '!: ' "$WORK/nonbreaking-subjects"; then
  echo "ok   branch_subjects does not invent a breaking declaration"
else
  echo "FAIL branch_subjects: nonbreaking branch contains a breaking declaration (exit $got)" >&2
  sed 's/^/  /' "$WORK/nonbreaking-subjects.err" >&2
  failed=1
fi

got=0
(
  cd "$repo"
  sh "$HERE/branch_subjects.sh" no/such/ref
) >"$WORK/unknown-ref" 2>"$WORK/unknown-ref.err" || got=$?
if [ "$got" -ne 0 ]; then
  echo "ok   branch_subjects rejects an unknown base ref"
else
  echo "FAIL branch_subjects: unknown base ref exits zero" >&2
  failed=1
fi

got=0
(
  cd "$repo"
  sh "$HERE/branch_subjects.sh" HEAD
) >"$WORK/equal-subjects" 2>"$WORK/equal-subjects.err" || got=$?
if [ "$got" -eq 0 ] && [ "$(wc -l <"$WORK/equal-subjects" | tr -d ' ')" -eq 1 ] && \
    [ "$(cat "$WORK/equal-subjects")" = "fix(api): y" ]; then
  echo "ok   branch_subjects prints exactly HEAD when base equals HEAD"
else
  echo "FAIL branch_subjects: base equal to HEAD must print exactly the HEAD subject" >&2
  sed 's/^/  /' "$WORK/equal-subjects.err" >&2
  failed=1
fi

exit "$failed"
