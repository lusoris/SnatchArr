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

expected_head_subject="$(git_fixture -C "$repo" log -1 --format=%s HEAD)"
git_fixture clone -q --depth 1 "file://$repo" "$WORK/shallow"

got=0
(
  cd "$WORK/shallow"
  sh "$HERE/branch_subjects.sh" HEAD
) >"$WORK/shallow-subjects" 2>"$WORK/shallow-subjects.err" || got=$?
if [ "$got" -ne 0 ] && grep -qF shallow "$WORK/shallow-subjects.err" && \
    grep -qF "git fetch --unshallow" "$WORK/shallow-subjects.err"; then
  echo "ok   branch_subjects refuses a shallow repository"
else
  echo "FAIL branch_subjects: shallow repository was not refused as expected (exit $got)" >&2
  sed 's/^/  /' "$WORK/shallow-subjects.err" >&2
  failed=1
fi

git_fixture -C "$WORK/shallow" fetch -q --unshallow
got=0
(
  cd "$WORK/shallow"
  sh "$HERE/branch_subjects.sh" HEAD
) >"$WORK/unshallowed-subjects" 2>"$WORK/unshallowed-subjects.err" || got=$?
if [ "$got" -eq 0 ] && [ "$(tail -n 1 "$WORK/unshallowed-subjects")" = "$expected_head_subject" ]; then
  echo "ok   branch_subjects accepts an unshallowed repository"
else
  echo "FAIL branch_subjects: unshallowed repository did not print the HEAD subject (exit $got)" >&2
  sed 's/^/  /' "$WORK/unshallowed-subjects.err" >&2
  failed=1
fi

check_repo="$WORK/check-repo"
mkdir -p "$check_repo/api/openapi"
git_fixture -C "$check_repo" init -q -b main
git_fixture -C "$check_repo" config user.name "OASDiff Test"
git_fixture -C "$check_repo" config user.email "oasdiff-test@example.invalid"
printf 'openapi: placeholder\n' >"$check_repo/api/openapi/openapi.yaml"
git_fixture -C "$check_repo" add api/openapi/openapi.yaml
git_fixture -C "$check_repo" -c commit.gpgsign=false commit -q -m "chore: base"

git_fixture clone -q --depth 1 "file://$check_repo" "$WORK/local-check-shallow"
got=0
(
  cd "$WORK/local-check-shallow"
  OASDIFF=true sh "$HERE/local_check.sh" origin/main api/openapi/openapi.yaml
) >"$WORK/local-check-shallow-no-break" 2>"$WORK/local-check-shallow-no-break.err" || got=$?
if [ "$got" -ne 0 ] && grep -qF "git fetch --unshallow" "$WORK/local-check-shallow-no-break.err"; then
  echo "ok   local_check refuses shallow no-break repository"
else
  echo "FAIL local_check: shallow no-break repository was not refused (exit $got)" >&2
  sed 's/^/  /' "$WORK/local-check-shallow-no-break.err" >&2
  failed=1
fi

got=0
(
  cd "$WORK/local-check-shallow"
  OASDIFF=false sh "$HERE/local_check.sh" origin/main api/openapi/openapi.yaml
) >"$WORK/local-check-shallow-break" 2>"$WORK/local-check-shallow-break.err" || got=$?
if [ "$got" -ne 0 ]; then
  echo "ok   local_check refuses shallow break repository"
else
  echo "FAIL local_check: shallow break repository exited zero" >&2
  failed=1
fi

git_fixture clone -q "file://$check_repo" "$WORK/local-check-full"
got=0
(
  cd "$WORK/local-check-full"
  OASDIFF=true sh "$HERE/local_check.sh" origin/main api/openapi/openapi.yaml
) >"$WORK/local-check-full-no-break" 2>"$WORK/local-check-full-no-break.err" || got=$?
if [ "$got" -eq 0 ]; then
  echo "ok   local_check accepts full no-break repository"
else
  echo "FAIL local_check: full no-break repository exited $got" >&2
  sed 's/^/  /' "$WORK/local-check-full-no-break.err" >&2
  failed=1
fi

printf 'feature\n' >"$WORK/local-check-full/feature.txt"
git_fixture -C "$WORK/local-check-full" config user.name "OASDiff Test"
git_fixture -C "$WORK/local-check-full" config user.email "oasdiff-test@example.invalid"
git_fixture -C "$WORK/local-check-full" add feature.txt
git_fixture -C "$WORK/local-check-full" -c commit.gpgsign=false commit -q -m "feat(api)!: drop x"
got=0
(
  cd "$WORK/local-check-full"
  OASDIFF=false sh "$HERE/local_check.sh" origin/main api/openapi/openapi.yaml
) >"$WORK/local-check-full-break" 2>"$WORK/local-check-full-break.err" || got=$?
if [ "$got" -eq 0 ] && grep -qF "breaking change declared in subject" "$WORK/local-check-full-break"; then
  echo "ok   local_check accepts declared full break repository"
else
  echo "FAIL local_check: declared full break repository was not accepted (exit $got)" >&2
  sed 's/^/  /' "$WORK/local-check-full-break.err" >&2
  failed=1
fi

got=0
(
  cd "$WORK/local-check-full"
  OASDIFF=true sh "$HERE/local_check.sh" no/such/ref api/openapi/openapi.yaml
) >"$WORK/local-check-unknown" 2>"$WORK/local-check-unknown.err" || got=$?
if [ "$got" -ne 0 ] && grep -qF "base ref does not resolve" "$WORK/local-check-unknown.err"; then
  echo "ok   local_check rejects an unknown base ref"
else
  echo "FAIL local_check: unknown base ref was not rejected clearly (exit $got)" >&2
  sed 's/^/  /' "$WORK/local-check-unknown.err" >&2
  failed=1
fi

makefile="$HERE/../../Makefile"
expected_recipe_line="$(printf '\tsh tools/oasdiff/local_check.sh origin/main api/openapi/openapi.yaml')"
if awk -v expected="$expected_recipe_line" '
  /^api-openapi-breaking:/ { in_target=1; next }
  in_target && /^[^[:space:]]/ { exit }
  in_target && $0 == expected { found=1 }
  END { exit !found }
' "$makefile"; then
  echo "ok   api-openapi-breaking runs local_check"
else
  echo "FAIL api-openapi-breaking: recipe does not run local_check" >&2
  failed=1
fi

exit "$failed"
