#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Offline planted cases for workflow_refs.sh and workflow_uses.py (no Docker, no network, stub
# GraphQL curl on every call, fixed placeholder token).
# Accepted: workflow_uses.py --self-test (workflow-uses-selftest), one current repository (clean),
# two current repositories across a workflow and a composite action (two-current), uses-shaped text
# inside a run script block (run-block-uses), a Docker-only local action directory holding only
# Dockerfile (docker-local-action), three distinct repositories batched into one GraphQL POST
# (three-repos), GITHUB_GRAPHQL_URL overriding the REST v3 URL variable (graphql-url-override),
# default GraphQL URL when only the REST v3 URL variable is set (graphql-url-ignores-v3-url),
# workflow-refs.yml carrying no uses entry (guard-job-no-uses) or uses-shaped scalar text in run or
# step name (guard-job-run-uses-text, guard-job-name-uses-text), bash versions 4.4, 5.0 and 5.2
# (bash-version-4-4, bash-version-5-0, bash-version-5-2), stubs in this file and in
# workflow-check_test.sh that run no GNU timeout (stubs-no-gnu-timeout), curl bounded by
# --max-time 20 and --retry-max-time 55 to 60 under the default lookup deadline
# (lookup-max-time-default) and by 1 to 5 under a 5 s deadline (lookup-max-time-short),
# WORKFLOW_REFS_DEADLINE_SECONDS at its bounds 1 and 9999 (deadline-env-1, deadline-env-9999), and
# workflow-refs.yml running on a schedule with a non-empty cron string (guard-job-schedule), and a
# copy whose on key is quoted, which PyYAML reads as the string "on" (guard-job-quoted-on).
# Refused: a reusable workflow under a moved repository name (moved-workflow), a composite action
# step under a moved repository name (moved-action), a failed GraphQL HTTP call and an empty JSON
# object on clean, a moved repository reached via YAML anchor and alias (moved-anchor), a
# flow-style step under a moved repository name (moved-flow), an unquoted boolean job id `yes`
# under a moved repository name (moved-bool-job-id), a leading-dot workflow file under a moved
# repository name (moved-hidden-workflow), a nested composite action under a moved
# repository name (moved-nested-action), a composite action outside .github/actions reached via
# `uses: ./tools/actions/x` (moved-outside-action), a composite action outside .github/actions
# reached only via self-repository `uses: $/tools/self-repo/planted` under a moved repository name
# (moved-self-repo-action), a
# self-repository reference carrying `@ref` (self-repo-with-ref), a composite action nested 9
# directories under .github/actions (moved-deep-action), a missing local action `uses: ./missing`
# (missing-local-action), a local action escaping ROOT `uses: ./../outside` (escape-local-action),
# an unreadable directory under .github/actions when unprivileged (unreadable-actions-dir), a
# GraphQL errors array (gql-errors), a null GraphQL alias for a private or missing repository
# (null-alias), a mismatched GITHUB_GRAPHQL_URL (graphql-url-mismatch), two JSON objects masking a
# GraphQL errors member (gql-two-objects-errors) or duplicating data (gql-two-data-objects), a
# nameWithOwner holding an embedded or trailing newline (gql-newline-name, gql-trailing-newline-name),
# 101 repositories split across 2 GraphQL POSTs with a moved repository in the
# second batch (over-100-repos), a lookup stalled past a 2 s deadline in batch 1 of 2
# (lookup-deadline-batch-1), WORKFLOW_REFS_DEADLINE_SECONDS 0, 10000, 05 and abc (deadline-env-0,
# deadline-env-10000, deadline-env-05, deadline-env-abc), a missing token (missing-token),
# workflow_uses.py without PyYAML
# (no-pyyaml), pyyaml_venv.sh without a DIR argument (pyyaml-venv-no-arg), with the deadline word
# abc (pyyaml-venv-deadline-word) or against a package index that never answers past a 12 s
# deadline (pyyaml-venv-stalled-index), workflow-refs.yml copies with no schedule
# (guard-job-no-schedule), a schedule with no cron line (guard-job-empty-schedule), both an
# unquoted and a quoted on key (guard-job-on-twice), a planted
# block uses step (guard-job-block-uses), flow uses step
# (guard-job-flow-uses), local ./ uses step (guard-job-local-uses), escaped-key "\x75ses" step
# (guard-job-escaped-key-uses), escaped-key local uses step (guard-job-escaped-local-uses), or
# uses key outside the step path (guard-job-nested-uses-key),
# and unsupported bash versions 3.2, 4.3, 4.x and empty (bash-version-3-2, bash-version-4-3,
# bash-version-4-x, bash-version-empty).
# Platform (HISS-21): runs on Linux and macOS with bash 4.4 or newer (workflow_refs.sh uses
# mapfile -d, associative arrays and ${var,,}); skips with a stated reason and exits 0 on older
# bash such as macOS stock bash 3.2.
#
#   tools/ci/workflow_refs_test.sh
#
# Exit status: 0 every case behaved as recorded (or skipped on missing prerequisite), 1 otherwise.
# Needs bash 4.4 or newer (mapfile -d), python3 (with PyYAML or venv + network for
# pyyaml_venv.sh) and jq.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Stubs run their reads under this portable deadline runner instead of GNU timeout.
export RUN_BOUNDED_PY="$HERE/run_bounded.py"
# shellcheck source=tools/ci/bash_version.sh
. "$HERE/bash_version.sh"
if ! bash_version_supported "${BASH_VERSINFO[0]}" "${BASH_VERSINFO[1]}"; then
  echo "skip workflow_refs_test.sh: needs bash 4.4 or newer (mapfile -d), found $BASH_VERSION"
  exit 0
fi

REPO_ROOT="$(cd "$HERE/../.." && pwd)"

# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
ensure_pyyaml_python

WORK="$(mktemp -d)"
# cleanup: restore the mode of the unreadable-actions-dir fixture when it exists (a failed restore goes to stderr), then remove $WORK.
cleanup() {
  local locked="$WORK/unreadable-actions-dir/.github/actions/locked"
  if [ -d "$locked" ] && ! chmod 700 "$locked"; then
    printf 'cleanup: failed to restore mode on %s\n' "$locked" >&2
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

# Every input workflow_refs.sh reads is fixed (GH_TOKEN) or cleared here except PYTHON_BIN,
# passed through on purpose so callers can supply the PyYAML interpreter.
export GH_TOKEN=stub-token
rest_url_var="GITHUB_API"
rest_url_var+="_URL"
unset GITHUB_TOKEN GITHUB_GRAPHQL_URL "$rest_url_var" WORKFLOW_REFS_DEADLINE_SECONDS CURL_BIN

# Commit-pinned actions from ci.yml used in fixture workflows.
CHECKOUT=actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1
SETUP_GO=actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e
COSIGN=sigstore/cosign-installer@6f9f17788090df1f26f669e9d70d6ae9567deba6

accepted=0
refused=0

# plant CASE USES: one workflow whose only step uses USES.
plant() {
  mkdir -p "$WORK/$1/.github/workflows"
  cat > "$WORK/$1/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - uses: $2
        with:
          persist-credentials: false
YAML
}

# plant_action CASE USES [DIR]: one composite action under .github/actions/DIR/action.yml and one calling workflow.
plant_action() {
  local dir="${3:-planted}"
  mkdir -p "$WORK/$1/.github/workflows" "$WORK/$1/.github/actions/$dir"
  cat > "$WORK/$1/.github/actions/$dir/action.yml" <<YAML
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
name: planted
description: planted action
runs:
  using: composite
  steps:
    - uses: $2
YAML
  cat > "$WORK/$1/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - uses: $CHECKOUT # v7.0.1
        with:
          persist-credentials: false
      - uses: ./.github/actions/$dir
YAML
}

# plant_call CASE USES: one workflow whose only job calls the reusable workflow USES.
plant_call() {
  mkdir -p "$WORK/$1/.github/workflows"
  cat > "$WORK/$1/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    uses: $2
YAML
}

# expect_accepted CASE NEEDLE [COMMAND PREFIX...]: the check must pass and its output must name NEEDLE.
expect_accepted() {
  local name="$1" needle="$2" out
  shift 2
  if ! out="$("$@" bash "$HERE/workflow_refs.sh" "$WORK/$name" 2>&1)"; then
    printf 'FAIL %s: refused\n%s\n' "$name" "$out" >&2
    exit 1
  fi
  case "$out" in
    *"$needle"*)
      accepted=$((accepted + 1))
      echo "ok   $name: accepted ($needle)"
      ;;
    *)
      printf 'FAIL %s: accepted without "%s"\n%s\n' "$name" "$needle" "$out" >&2
      exit 1
      ;;
  esac
}

# expect_refusal CASE NEEDLE [COMMAND PREFIX...]: the check must fail and name NEEDLE.
expect_refusal() {
  local name="$1" needle="$2" out
  shift 2
  if out="$("$@" bash "$HERE/workflow_refs.sh" "$WORK/$name" 2>&1)"; then
    printf 'FAIL %s: accepted\n%s\n' "$name" "$out" >&2
    exit 1
  fi
  case "$out" in
    *"$needle"*)
      refused=$((refused + 1))
      echo "ok   $name: refused ($needle)"
      ;;
    *)
      printf 'FAIL %s: refused without "%s"\n%s\n' "$name" "$needle" "$out" >&2
      exit 1
      ;;
  esac
}

# guard_has_no_uses FILE: succeed when workflow_uses.py --all-uses and --uses-keys both exit 0 with empty output on FILE.
guard_has_no_uses() {
  local file="$1" uses_out keys_out
  uses_out="$("${PYTHON_BIN:-python3}" -I "$HERE/workflow_uses.py" --all-uses "$file")" || return 1
  [ -z "$uses_out" ] || return 1
  keys_out="$("${PYTHON_BIN:-python3}" -I "$HERE/workflow_uses.py" --uses-keys "$file")" || return 1
  [ -z "$keys_out" ] || return 1
  return 0
}

# plant_guard_step CASE STEP: copy workflow-refs.yml to $WORK/CASE.yml with STEP inserted after its single steps: line.
plant_guard_step() {
  local case_name="$1" step="$2" src="$REPO_ROOT/.github/workflows/workflow-refs.yml"
  local dst="$WORK/$case_name.yml" line line_n=0 steps_count=0
  [[ "$step" != *$'\n'* ]] || return 1
  while IFS= read -r line || [ -n "$line" ]; do
    line_n=$((line_n + 1))
    [ "$line_n" -le 65536 ] || return 1
    printf '%s\n' "$line"
    if [ "$line" = "    steps:" ]; then
      steps_count=$((steps_count + 1))
      printf '%s\n' "$step"
    fi
  done < "$src" > "$dst"
  [ "$steps_count" -eq 1 ] || return 1
}

# guard_has_schedule FILE: succeed when the on mapping of FILE holds a schedule list with a
# mapping whose cron is a non-empty string. PyYAML reads an unquoted on key as True and a quoted
# one as the string "on"; both serve. Both keys at once print `on key given twice`, no schedule
# prints `no schedule entry with a cron string`; either exits 1. Reads under a 10 s bound.
guard_has_schedule() {
  python3 -I "$RUN_BOUNDED_PY" 10 "${PYTHON_BIN:-python3}" -I -c '
import sys

import yaml

with open(sys.argv[1], encoding="utf-8") as handle:
    doc = yaml.safe_load(handle)
keys = [key for key in (True, "on") if isinstance(doc, dict) and key in doc]
if len(keys) > 1:
    print("on key given twice")
    sys.exit(1)
on = doc[keys[0]] if keys else None
entries = on.get("schedule") if isinstance(on, dict) else None
if not isinstance(entries, list):
    entries = []
crons = [entry.get("cron") for entry in entries if isinstance(entry, dict)]
if not any(isinstance(cron, str) and cron.strip() for cron in crons):
    print("no schedule entry with a cron string")
    sys.exit(1)
' "$1"
}

# plant_guard_unscheduled CASE MODE: copy workflow-refs.yml to $WORK/CASE.yml without its single
# cron line (MODE cron) or without its single schedule: and cron lines (MODE schedule).
plant_guard_unscheduled() {
  local mode="$2" src="$REPO_ROOT/.github/workflows/workflow-refs.yml"
  local dst="$WORK/$1.yml" line line_n=0 cron_n=0 schedule_n=0
  local cron_re='^[[:space:]]+-[[:space:]]+cron:'
  [ "$mode" = cron ] || [ "$mode" = schedule ] || return 1
  while IFS= read -r line || [ -n "$line" ]; do
    line_n=$((line_n + 1))
    [ "$line_n" -le 65536 ] || return 1
    if [[ "$line" =~ $cron_re ]]; then
      cron_n=$((cron_n + 1))
      continue
    fi
    if [ "$mode" = schedule ] && [ "$line" = "  schedule:" ]; then
      schedule_n=$((schedule_n + 1))
      continue
    fi
    printf '%s\n' "$line"
  done < "$src" > "$dst"
  [ "$cron_n" -eq 1 ] || return 1
  [ "$mode" = cron ] || [ "$schedule_n" -eq 1 ]
}

# plant_guard_quoted_on CASE MODE: copy workflow-refs.yml to $WORK/CASE.yml with its single
# top-level `on:` line written `"on":` (MODE quoted), or keep it and append a top-level
# `"on": {}` line (MODE twice).
plant_guard_quoted_on() {
  local mode="$2" src="$REPO_ROOT/.github/workflows/workflow-refs.yml"
  local dst="$WORK/$1.yml" line line_n=0 on_n=0
  [ "$mode" = quoted ] || [ "$mode" = twice ] || return 1
  while IFS= read -r line || [ -n "$line" ]; do
    line_n=$((line_n + 1))
    [ "$line_n" -le 65536 ] || return 1
    if [ "$line" = "on:" ]; then
      on_n=$((on_n + 1))
      [ "$mode" = twice ] || line='"on":'
    fi
    printf '%s\n' "$line"
  done < "$src" > "$dst"
  [ "$on_n" -eq 1 ] || return 1
  [ "$mode" = quoted ] || printf '%s\n' '"on": {}' >> "$dst"
}

# A fake GitHub GraphQL API: golusoris/<repo> moved to plurano/<repo> and moved/<repo> moved to
# current/<repo>; any other repository answers its own name. Optionally counts invocations in
# CURL_COUNT_FILE. Every stub runs under a 5 s bound with bounded argument loops.
cat > "$WORK/curl-moved" <<'SH'
#!/bin/sh
set -eu
python3 -I "$RUN_BOUNDED_PY" 5 head -c 65536 > /dev/null
if [ -n "${CURL_COUNT_FILE:-}" ]; then
  count=0
  if [ -f "$CURL_COUNT_FILE" ]; then
    read -r count < "$CURL_COUNT_FILE"
  fi
  printf '%s\n' "$((count + 1))" > "$CURL_COUNT_FILE"
fi
payload=""
prev=""
arg_n=0
for arg in "$@"; do
  arg_n=$((arg_n + 1))
  [ "$arg_n" -le 64 ] || exit 2
  if [ "$prev" = "--data-raw" ] || [ "$prev" = "-d" ]; then
    payload="$arg"
    break
  fi
  prev="$arg"
done
[ -n "$payload" ] || exit 22
printf '%s\n' "$payload" | python3 -I "$RUN_BOUNDED_PY" 5 jq -c '
  .query as $q
  | reduce ([ $q | match("(?<a>r[0-9]+):\\s*repository\\(owner:\\s*\"(?<o>[A-Za-z0-9_.-]+)\",\\s*name:\\s*\"(?<n>[A-Za-z0-9_.-]+)\"\\)"; "g") ][:200][]) as $m (
      {};
      ($m.captures | map({(.name): .string}) | add) as $c
      | (if $c.o == "golusoris" then "plurano/\($c.n)"
         elif $c.o == "moved" then "current/\($c.n)"
         else "\($c.o)/\($c.n)" end) as $full
      | .[$c.a] = {nameWithOwner: $full}
    )
  | {data: .}
'
SH
cat > "$WORK/curl-want-url" <<'SH'
#!/bin/sh
set -eu
last=""
arg_n=0
for arg in "$@"; do
  arg_n=$((arg_n + 1))
  [ "$arg_n" -le 64 ] || exit 2
  last="$arg"
done
if [ -z "${STUB_WANT_URL:-}" ] || [ "$last" != "$STUB_WANT_URL" ]; then
  python3 -I "$RUN_BOUNDED_PY" 5 head -c 65536 > /dev/null
  exit 22
fi
exec "$(dirname "$0")/curl-moved" "$@"
SH
cat > "$WORK/curl-fails" <<'SH'
#!/bin/sh
set -eu
python3 -I "$RUN_BOUNDED_PY" 5 head -c 65536 > /dev/null
exit 22
SH
cat > "$WORK/curl-nameless" <<'SH'
#!/bin/sh
set -eu
python3 -I "$RUN_BOUNDED_PY" 5 head -c 65536 > /dev/null
echo "{}"
SH
cat > "$WORK/curl-gql-errors" <<'SH'
#!/bin/sh
set -eu
python3 -I "$RUN_BOUNDED_PY" 5 head -c 65536 > /dev/null
printf '%s\n' '{"data":{"r0":{"nameWithOwner":"actions/checkout"}},"errors":[{"message":"API rate limit exceeded"}]}'
SH
cat > "$WORK/curl-null-alias" <<'SH'
#!/bin/sh
set -eu
python3 -I "$RUN_BOUNDED_PY" 5 head -c 65536 > /dev/null
printf '%s\n' '{"data":{"r0":null}}'
SH
cat > "$WORK/curl-two-objects-errors" <<'SH'
#!/bin/sh
set -eu
python3 -I "$RUN_BOUNDED_PY" 5 head -c 65536 > /dev/null
printf '%s\n' '{"errors":[{"message":"boom"}],"data":{"r0":{"nameWithOwner":"actions/checkout"}}} {}'
SH
cat > "$WORK/curl-two-data-objects" <<'SH'
#!/bin/sh
set -eu
python3 -I "$RUN_BOUNDED_PY" 5 head -c 65536 > /dev/null
printf '%s\n' '{"data":{"r0":{"nameWithOwner":"actions/checkout"}}} {"data":{"r0":{"nameWithOwner":"actions/checkout"}}}'
SH
cat > "$WORK/curl-newline-name" <<'SH'
#!/bin/sh
set -eu
python3 -I "$RUN_BOUNDED_PY" 5 head -c 65536 > /dev/null
printf '%s\n' '{"data":{"r0":{"nameWithOwner":"actions/checkout\nother/repo"}}}'
SH
cat > "$WORK/curl-trailing-newline-name" <<'SH'
#!/bin/sh
set -eu
python3 -I "$RUN_BOUNDED_PY" 5 head -c 65536 > /dev/null
printf '%s\n' '{"data":{"r0":{"nameWithOwner":"actions/checkout\n"}}}'
SH
# curl-slow: counts its call in CURL_COUNT_FILE like curl-moved, stalls 10 s, then answers as
# curl-moved without counting again; the lookup deadline is expected to cut it first
# (lookup-deadline-batch-1).
cat > "$WORK/curl-slow" <<'SH'
#!/bin/sh
set -eu
if [ -n "${CURL_COUNT_FILE:-}" ]; then
  count=0
  if [ -f "$CURL_COUNT_FILE" ]; then
    read -r count < "$CURL_COUNT_FILE"
  fi
  printf '%s\n' "$((count + 1))" > "$CURL_COUNT_FILE"
fi
sleep 10
unset CURL_COUNT_FILE
exec "$(dirname "$0")/curl-moved" "$@"
SH
# curl-argv: writes each argument on its own line to CURL_ARGV_FILE (at most 64 arguments), then
# answers as curl-moved with the same arguments and stdin.
cat > "$WORK/curl-argv" <<'SH'
#!/bin/sh
set -eu
: > "$CURL_ARGV_FILE"
arg_n=0
for arg in "$@"; do
  arg_n=$((arg_n + 1))
  [ "$arg_n" -le 64 ] || exit 2
  printf '%s\n' "$arg" >> "$CURL_ARGV_FILE"
done
exec "$(dirname "$0")/curl-moved" "$@"
SH
chmod +x "$WORK/curl-moved" "$WORK/curl-want-url" "$WORK/curl-fails" "$WORK/curl-nameless" \
  "$WORK/curl-gql-errors" "$WORK/curl-null-alias" "$WORK/curl-two-objects-errors" \
  "$WORK/curl-two-data-objects" "$WORK/curl-newline-name" "$WORK/curl-trailing-newline-name" \
  "$WORK/curl-slow" "$WORK/curl-argv"

if ! selftest_out="$("${PYTHON_BIN:-python3}" -I "$HERE/workflow_uses.py" --self-test 2>&1)"; then
  printf 'FAIL workflow-uses-selftest: self-test failed\n%s\n' "$selftest_out" >&2
  exit 1
fi
accepted=$((accepted + 1))
echo "ok   workflow-uses-selftest: accepted"

plant clean "$CHECKOUT # v7.0.1"
expect_accepted clean "all 1 used by name are under their current name" env CURL_BIN="$WORK/curl-moved"

plant_action two-current "$SETUP_GO # v7.0.0"
expect_accepted two-current "all 2 used by name are under their current name" env CURL_BIN="$WORK/curl-moved"

plant_call moved-workflow "golusoris/sveltesentio/.github/workflows/ci-sveltekit.yml@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da # main"
expect_refusal moved-workflow "refused .github/workflows/planted.yml:8: golusoris/sveltesentio moved to plurano/sveltesentio" env CURL_BIN="$WORK/curl-moved"

plant_action moved-action "golusoris/goenvoy/.github/actions/setup@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da # main"
expect_refusal moved-action "refused .github/actions/planted/action.yml:8: golusoris/goenvoy moved to plurano/goenvoy" env CURL_BIN="$WORK/curl-moved"

expect_refusal clean "cannot read repository actions/checkout" env CURL_BIN="$WORK/curl-fails"
expect_refusal clean "names no repository the token can read for actions/checkout: private and missing repositories are not supported; reference a public repository" env CURL_BIN="$WORK/curl-nameless"

# A `run: |` block whose script text contains `uses: moved/repo@<sha>` is ignored and passes.
mkdir -p "$WORK/run-block-uses/.github/workflows"
cat > "$WORK/run-block-uses/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - uses: $CHECKOUT # v7.0.1
        with:
          persist-credentials: false
      - name: script containing uses text
        run: |
          cat <<'EOF'
            - uses: moved/repo@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da
          EOF
YAML
expect_accepted run-block-uses "all 1 used by name are under their current name" env CURL_BIN="$WORK/curl-moved"

# A moved repository reached through a YAML anchor and alias is refused.
mkdir -p "$WORK/moved-anchor/.github/workflows"
cat > "$WORK/moved-anchor/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
env:
  ANCHORED_ACTION: &moved-ref moved/repo@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - uses: *moved-ref
YAML
expect_refusal moved-anchor "refused .github/workflows/planted.yml:7: moved/repo moved to current/repo" env CURL_BIN="$WORK/curl-moved"

# A flow-style step `- {name: x, uses: moved/repo@<sha>}` is refused.
mkdir -p "$WORK/moved-flow/.github/workflows"
cat > "$WORK/moved-flow/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - {name: x, uses: moved/repo@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da}
YAML
expect_refusal moved-flow "refused .github/workflows/planted.yml:10: moved/repo moved to current/repo" env CURL_BIN="$WORK/curl-moved"

# An unquoted boolean job id `yes` is kept and its moved step reference is refused.
mkdir -p "$WORK/moved-bool-job-id/.github/workflows"
cat > "$WORK/moved-bool-job-id/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  yes:
    runs-on: ubuntu-26.04
    steps:
      - uses: moved/repo@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da
YAML
expect_refusal moved-bool-job-id "refused .github/workflows/planted.yml:10: moved/repo moved to current/repo" env CURL_BIN="$WORK/curl-moved"

# A leading-dot workflow file `.github/workflows/.hidden.yml` is matched by dotglob and refused.
mkdir -p "$WORK/moved-hidden-workflow/.github/workflows"
cat > "$WORK/moved-hidden-workflow/.github/workflows/.hidden.yml" <<'YAML'
name: hidden
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - uses: moved/repo@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da
YAML
expect_refusal moved-hidden-workflow "refused .github/workflows/.hidden.yml:10: moved/repo moved to current/repo" env CURL_BIN="$WORK/curl-moved"

# A moved reference in a nested composite action `.github/actions/group/inner/action.yml` is refused.
plant_action moved-nested-action "moved/repo@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da # v1.0.0" "group/inner"
expect_refusal moved-nested-action "refused .github/actions/group/inner/action.yml:8: moved/repo moved to current/repo" env CURL_BIN="$WORK/curl-moved"

# A composite action outside .github/actions reached by `uses: ./tools/actions/x` is inspected and refused.
mkdir -p "$WORK/moved-outside-action/.github/workflows" "$WORK/moved-outside-action/tools/actions/x"
cat > "$WORK/moved-outside-action/tools/actions/x/action.yml" <<'YAML'
name: outside-x
description: composite action outside .github/actions
runs:
  using: composite
  steps:
    - uses: moved/outside-repo@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da # v1.0.0
YAML
cat > "$WORK/moved-outside-action/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - uses: $CHECKOUT # v7.0.1
        with:
          persist-credentials: false
      - uses: ./tools/actions/x
YAML
expect_refusal moved-outside-action "refused tools/actions/x/action.yml:6: moved/outside-repo moved to current/outside-repo" env CURL_BIN="$WORK/curl-moved"

# A composite action nested 9 directories under .github/actions is walked and refused.
deep_dir="d1/d2/d3/d4/d5/d6/d7/d8/d9"
mkdir -p "$WORK/moved-deep-action/.github/workflows" "$WORK/moved-deep-action/.github/actions/$deep_dir"
cat > "$WORK/moved-deep-action/.github/actions/$deep_dir/action.yml" <<'YAML'
name: deep-action
description: composite action nested 9 directories under .github/actions
runs:
  using: composite
  steps:
    - uses: moved/deep-repo@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da # v1.0.0
YAML
cat > "$WORK/moved-deep-action/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - uses: $CHECKOUT # v7.0.1
        with:
          persist-credentials: false
YAML
expect_refusal moved-deep-action "refused .github/actions/$deep_dir/action.yml:6: moved/deep-repo moved to current/deep-repo" env CURL_BIN="$WORK/curl-moved"

# A composite action outside .github/actions, reached only via `uses: $/tools/self-repo/planted`,
# is scanned and refused: no directory walk finds it, so the refusal proves workflow discovery.
mkdir -p "$WORK/moved-self-repo-action/.github/workflows" "$WORK/moved-self-repo-action/tools/self-repo/planted"
cat > "$WORK/moved-self-repo-action/tools/self-repo/planted/action.yml" <<'YAML'
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
name: planted
description: planted action
runs:
  using: composite
  steps:
    - uses: moved/repo@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da # v1.0.0
YAML
cat > "$WORK/moved-self-repo-action/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - uses: $CHECKOUT # v7.0.1
        with:
          persist-credentials: false
      - uses: \$/tools/self-repo/planted
YAML
expect_refusal moved-self-repo-action "refused tools/self-repo/planted/action.yml:8: moved/repo moved to current/repo" env CURL_BIN="$WORK/curl-moved"

# A self-repository reference carrying `@ref` is refused before any directory lookup.
plant self-repo-with-ref '$/.github/actions/planted@v1'
expect_refusal self-repo-with-ref "error: .github/workflows/planted.yml:10: self-repository reference '\$/.github/actions/planted@v1' must not include @ref" env CURL_BIN="$WORK/curl-moved"

# A Docker-only local action directory holding only Dockerfile is accepted and not listed.
mkdir -p "$WORK/docker-local-action/.github/workflows" "$WORK/docker-local-action/docker-action"
printf 'FROM scratch\n' > "$WORK/docker-local-action/docker-action/Dockerfile"
cat > "$WORK/docker-local-action/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - uses: $CHECKOUT # v7.0.1
        with:
          persist-credentials: false
      - uses: ./docker-action
YAML
expect_accepted docker-local-action "all 1 used by name are under their current name" env CURL_BIN="$WORK/curl-moved"

# A step `uses: ./missing` with no action.yml, action.yaml or Dockerfile is refused naming path:line.
plant missing-local-action "./missing"
expect_refusal missing-local-action "error: .github/workflows/planted.yml:10: local action './missing' has no action.yml, action.yaml or Dockerfile" env CURL_BIN="$WORK/curl-moved"

# A step `uses: ./../outside` escaping ROOT is refused naming path:line even when ../outside/action.yml exists.
mkdir -p "$WORK/outside"
cat > "$WORK/outside/action.yml" <<YAML
name: outside
description: action outside root
runs:
  using: composite
  steps:
    - uses: $CHECKOUT # v7.0.1
YAML
plant escape-local-action "./../outside"
expect_refusal escape-local-action "error: .github/workflows/planted.yml:10: local action './../outside' escapes repository root" env CURL_BIN="$WORK/curl-moved"

# An unreadable directory under .github/actions is refused rather than silently skipped.
plant unreadable-actions-dir "$CHECKOUT # v7.0.1"
locked_dir="$WORK/unreadable-actions-dir/.github/actions/locked"
mkdir -p "$locked_dir"
chmod 000 "$locked_dir"
if ls "$locked_dir" > /dev/null 2>&1; then
  echo "skip unreadable-actions-dir: process reads mode-000 directories (root or CAP_DAC_OVERRIDE)"
else
  expect_refusal unreadable-actions-dir "error: .github/actions:" env CURL_BIN="$WORK/curl-moved"
fi
chmod 700 "$locked_dir"

# GraphQL answer with an `errors` member is refused naming path:line.
plant gql-errors "$CHECKOUT # v7.0.1"
expect_refusal gql-errors "error: .github/workflows/planted.yml:10: GraphQL returned errors for actions/checkout" env CURL_BIN="$WORK/curl-gql-errors"

# GraphQL answer with a null alias is refused naming path:line and explaining public-only support.
plant null-alias "$CHECKOUT # v7.0.1"
expect_refusal null-alias "error: .github/workflows/planted.yml:10: names no repository the token can read for actions/checkout: private and missing repositories are not supported; reference a public repository" env CURL_BIN="$WORK/curl-null-alias"

# GITHUB_GRAPHQL_URL overrides the REST v3 URL variable (stripping one trailing slash); the REST v3 URL variable alone leaves the default endpoint.
plant graphql-url-override "$CHECKOUT # v7.0.1"
expect_accepted graphql-url-override "all 1 used by name are under their current name" \
  env GITHUB_GRAPHQL_URL="https://ghe.example.test/api/graphql/" \
  "$rest_url_var=https://ghe.example.test/api/v3" \
  STUB_WANT_URL="https://ghe.example.test/api/graphql" \
  CURL_BIN="$WORK/curl-want-url"

plant graphql-url-ignores-v3-url "$CHECKOUT # v7.0.1"
expect_accepted graphql-url-ignores-v3-url "all 1 used by name are under their current name" \
  env "$rest_url_var=https://ghe.example.test/api/v3" \
  STUB_WANT_URL="https://api.github.com/graphql" \
  CURL_BIN="$WORK/curl-want-url"

plant graphql-url-mismatch "$CHECKOUT # v7.0.1"
expect_refusal graphql-url-mismatch "cannot read repository actions/checkout from https://wrong.example.test/api/graphql" \
  env GITHUB_GRAPHQL_URL="https://wrong.example.test/api/graphql/" \
  STUB_WANT_URL="https://ghe.example.test/api/graphql" \
  CURL_BIN="$WORK/curl-want-url"

# Multiple JSON values in a GraphQL HTTP body are refused as invalid JSON response.
plant gql-two-objects-errors "$CHECKOUT # v7.0.1"
expect_refusal gql-two-objects-errors "invalid JSON response" env CURL_BIN="$WORK/curl-two-objects-errors"

plant gql-two-data-objects "$CHECKOUT # v7.0.1"
expect_refusal gql-two-data-objects "invalid JSON response" env CURL_BIN="$WORK/curl-two-data-objects"

# A nameWithOwner containing an embedded or trailing newline is rejected as naming no readable repository.
plant gql-newline-name "$CHECKOUT # v7.0.1"
expect_refusal gql-newline-name "names no repository the token can read for actions/checkout: private and missing repositories are not supported; reference a public repository" env CURL_BIN="$WORK/curl-newline-name"

plant gql-trailing-newline-name "$CHECKOUT # v7.0.1"
expect_refusal gql-trailing-newline-name "names no repository the token can read for actions/checkout: private and missing repositories are not supported; reference a public repository" env CURL_BIN="$WORK/curl-trailing-newline-name"

# 3 distinct repositories are looked up in a single GraphQL POST (1 stub call).
mkdir -p "$WORK/three-repos/.github/workflows"
cat > "$WORK/three-repos/.github/workflows/planted.yml" <<YAML
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - uses: $CHECKOUT # v7.0.1
        with:
          persist-credentials: false
      - uses: $SETUP_GO # v7.0.0
        with:
          go-version: stable
          cache: false
      - uses: $COSIGN # v4.1.2
YAML
rm -f "$WORK/curl.count"
expect_accepted three-repos "all 3 used by name are under their current name" env CURL_BIN="$WORK/curl-moved" CURL_COUNT_FILE="$WORK/curl.count"
if [ "$(cat "$WORK/curl.count")" != "1" ]; then
  echo "FAIL three-repos: expected 1 GraphQL call, got $(cat "$WORK/curl.count")" >&2
  exit 1
fi

# > 100 distinct repositories split into 1 GraphQL POST per 100 (101 -> 2 calls).
mkdir -p "$WORK/over-100-repos/.github/workflows"
{
  cat <<'YAML'
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
YAML
  for ((repo_i = 0; repo_i < 100; repo_i++)); do
    printf '      - uses: current/repo-%03d@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da # v1.0.0\n' "$repo_i"
  done
  printf '      - uses: moved/z-last@5b3b01ef8b0bd664acfa621eeb84a1c7e626e7da # v1.0.0\n'
} > "$WORK/over-100-repos/.github/workflows/planted.yml"
rm -f "$WORK/curl.count"
expect_refusal over-100-repos "refused .github/workflows/planted.yml:110: moved/z-last moved to current/z-last" env CURL_BIN="$WORK/curl-moved" CURL_COUNT_FILE="$WORK/curl.count"
if [ "$(cat "$WORK/curl.count")" != "2" ]; then
  echo "FAIL over-100-repos: expected 2 GraphQL calls, got $(cat "$WORK/curl.count")" >&2
  exit 1
fi

# The same 101 repositories with curl stalled 10 s and a 2 s lookup deadline: batch 1 of 2 is cut
# by run_bounded.py (exit 1, deadline message) after 1 GraphQL call and within 8 s.
cp -R "$WORK/over-100-repos" "$WORK/lookup-deadline-batch-1"
rm -f "$WORK/curl.count"
deadline_t0=$SECONDS
expect_refusal lookup-deadline-batch-1 \
  "repository lookup deadline of 2 s passed at batch 1 of 2 (first repository current/repo-000)" \
  env WORKFLOW_REFS_DEADLINE_SECONDS=2 CURL_BIN="$WORK/curl-slow" CURL_COUNT_FILE="$WORK/curl.count"
deadline_elapsed=$((SECONDS - deadline_t0))
deadline_calls="$(cat "$WORK/curl.count" 2> /dev/null || echo 0)"
if [ "$deadline_calls" != "1" ] || [ "$deadline_elapsed" -gt 8 ]; then
  printf 'FAIL lookup-deadline-batch-1: want 1 GraphQL call within 8 s, got %s in %s s\n' \
    "$deadline_calls" "$deadline_elapsed" >&2
  exit 1
fi

# curl_arg_after FILE FLAG: print the line after the first FLAG line in FILE (at most 64 lines).
curl_arg_after() {
  local file="$1" flag="$2" line n=0 take=0
  while IFS= read -r line || [ -n "$line" ]; do
    n=$((n + 1))
    if [ "$n" -gt 64 ]; then
      return 1
    fi
    if [ "$take" -eq 1 ]; then
      printf '%s\n' "$line"
      return 0
    fi
    if [ "$line" = "$flag" ]; then
      take=1
    fi
  done < "$file"
  return 1
}

# in_range VALUE LOW HIGH: succeed when VALUE is a whole number from LOW to HIGH.
in_range() {
  [[ "$1" =~ ^[0-9]{1,5}$ ]] && [ "$1" -ge "$2" ] && [ "$1" -le "$3" ]
}

# expect_curl_bounds CASE DEADLINE MAX_LOW MAX_HIGH RETRY_LOW RETRY_HIGH: run a copy of clean under
# curl-argv with WORKFLOW_REFS_DEADLINE_SECONDS=DEADLINE (unset when DEADLINE is empty) and require
# the values after --max-time and --retry-max-time inside the given ranges.
expect_curl_bounds() {
  local name="$1" max_time retry_time
  local -a deadline_env=(-u WORKFLOW_REFS_DEADLINE_SECONDS)
  if [ -n "$2" ]; then
    deadline_env=("WORKFLOW_REFS_DEADLINE_SECONDS=$2")
  fi
  cp -R "$WORK/clean" "$WORK/$name"
  expect_accepted "$name" "all 1 used by name are under their current name" \
    env "${deadline_env[@]}" CURL_BIN="$WORK/curl-argv" CURL_ARGV_FILE="$WORK/$name.argv"
  max_time="$(curl_arg_after "$WORK/$name.argv" --max-time)" || max_time=""
  retry_time="$(curl_arg_after "$WORK/$name.argv" --retry-max-time)" || retry_time=""
  if ! in_range "$max_time" "$3" "$4" || ! in_range "$retry_time" "$5" "$6"; then
    printf 'FAIL %s: --max-time %s (want %s to %s), --retry-max-time %s (want %s to %s)\n' \
      "$name" "$max_time" "$3" "$4" "$retry_time" "$5" "$6" >&2
    exit 1
  fi
}

expect_curl_bounds lookup-max-time-default "" 20 20 55 60
expect_curl_bounds lookup-max-time-short 5 1 5 1 5

# WORKFLOW_REFS_DEADLINE_SECONDS outside 1 to 9999 or not a plain whole number exits 2 before any
# lookup (curl-fails would exit 1); 1 passes on a fixture with no repository (no lookup, so no
# clock tick decides it) and 9999 on a copy of clean.
for deadline_value in 0 10000 05 abc; do
  deadline_rc=0
  deadline_out="$(env WORKFLOW_REFS_DEADLINE_SECONDS="$deadline_value" CURL_BIN="$WORK/curl-fails" \
    bash "$HERE/workflow_refs.sh" "$WORK/clean" 2>&1)" || deadline_rc=$?
  deadline_needle="WORKFLOW_REFS_DEADLINE_SECONDS must be a whole number of seconds"
  deadline_needle+=" from 1 to 9999, got '$deadline_value'"
  if [ "$deadline_rc" -ne 2 ]; then
    printf 'FAIL deadline-env-%s: want exit 2, got %s\n%s\n' \
      "$deadline_value" "$deadline_rc" "$deadline_out" >&2
    exit 1
  fi
  case "$deadline_out" in
    *"$deadline_needle"*)
      refused=$((refused + 1))
      echo "ok   deadline-env-$deadline_value: refused (exit 2)"
      ;;
    *)
      printf 'FAIL deadline-env-%s: exit 2 without "%s"\n%s\n' \
        "$deadline_value" "$deadline_needle" "$deadline_out" >&2
      exit 1
      ;;
  esac
done
mkdir -p "$WORK/deadline-env-1/.github/workflows"
cat > "$WORK/deadline-env-1/.github/workflows/planted.yml" <<'YAML'
name: planted
on:
  pull_request:
permissions:
  contents: read
jobs:
  planted:
    runs-on: ubuntu-26.04
    steps:
      - run: echo planted
YAML
expect_accepted deadline-env-1 "all 0 used by name are under their current name" \
  env WORKFLOW_REFS_DEADLINE_SECONDS=1 CURL_BIN="$WORK/curl-fails"
cp -R "$WORK/clean" "$WORK/deadline-env-9999"
expect_accepted deadline-env-9999 "all 1 used by name are under their current name" \
  env WORKFLOW_REFS_DEADLINE_SECONDS=9999 CURL_BIN="$WORK/curl-moved"

# Missing GH_TOKEN and GITHUB_TOKEN is refused before any lookup.
plant missing-token "$CHECKOUT # v7.0.1"
expect_refusal missing-token "is required" env -u GH_TOKEN -u GITHUB_TOKEN CURL_BIN="$WORK/curl-moved"

# workflow_uses.py under `-I -S` (no site packages -> no PyYAML) exits 2 with `PyYAML is required`.
pyyaml_rc=0
pyyaml_out="$("${PYTHON_BIN:-python3}" -I -S "$HERE/workflow_uses.py" "$WORK/clean/.github/workflows/planted.yml" 2>&1)" || pyyaml_rc=$?
if [ "$pyyaml_rc" -ne 2 ]; then
  printf 'FAIL no-pyyaml: expected exit 2, got %s\n%s\n' "$pyyaml_rc" "$pyyaml_out" >&2
  exit 1
fi
case "$pyyaml_out" in
  *"PyYAML is required"*)
    refused=$((refused + 1))
    echo "ok   no-pyyaml: refused (exit 2, PyYAML is required)"
    ;;
  *)
    printf 'FAIL no-pyyaml: exit 2 without "PyYAML is required"\n%s\n' "$pyyaml_out" >&2
    exit 1
    ;;
esac

# pyyaml_venv.sh without arguments exits 2 and prints its usage.
venv_rc=0
venv_out="$(bash "$HERE/pyyaml_venv.sh" 2>&1)" || venv_rc=$?
if [ "$venv_rc" -ne 2 ]; then
  printf 'FAIL pyyaml-venv-no-arg: expected exit 2, got %s\n%s\n' "$venv_rc" "$venv_out" >&2
  exit 1
fi
case "$venv_out" in
  *"usage: pyyaml_venv.sh DIR"*)
    refused=$((refused + 1))
    echo "ok   pyyaml-venv-no-arg: refused (exit 2, usage)"
    ;;
  *)
    printf 'FAIL pyyaml-venv-no-arg: exit 2 without usage\n%s\n' "$venv_out" >&2
    exit 1
    ;;
esac

# pyyaml_venv.sh refuses the deadline word abc with exit 2 before it creates DIR.
venv_rc=0
venv_out="$(PYYAML_VENV_DEADLINE_SECONDS=abc bash "$HERE/pyyaml_venv.sh" "$WORK/venv-word" 2>&1)" ||
  venv_rc=$?
if [ "$venv_rc" -ne 2 ] || [ -e "$WORK/venv-word" ]; then
  printf 'FAIL pyyaml-venv-deadline-word: want exit 2 and no DIR, got %s\n%s\n' \
    "$venv_rc" "$venv_out" >&2
  exit 1
fi
venv_needle="PYYAML_VENV_DEADLINE_SECONDS must be a whole number of seconds from 1 to 9999, got"
case "$venv_out" in
  *"$venv_needle 'abc'"*)
    refused=$((refused + 1))
    echo "ok   pyyaml-venv-deadline-word: refused (exit 2)"
    ;;
  *)
    printf 'FAIL pyyaml-venv-deadline-word: exit 2 without the deadline message\n%s\n' \
      "$venv_out" >&2
    exit 1
    ;;
esac

# start_stalled_index PORT_FILE: start in the background a loopback listener that accepts
# connections and never answers (bounded to 60 s by run_bounded.py), set stall_pid, and wait at
# most 10 s for PORT_FILE to hold its port; exit 1 when it does not.
start_stalled_index() {
  local wait_i
  : > "$1"
  python3 -I "$RUN_BOUNDED_PY" 60 python3 -I -c '
import socket
import time

listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
listener.bind(("127.0.0.1", 0))
listener.listen(16)
print(listener.getsockname()[1], flush=True)
time.sleep(60)
' > "$1" 2> "$1.err" &
  stall_pid=$!
  for ((wait_i = 0; wait_i < 100; wait_i++)); do
    if [[ "$(head -n 1 "$1")" =~ ^[0-9]{1,5}$ ]]; then
      return 0
    fi
    sleep 0.1
  done
  return 1
}

# pyyaml_venv.sh stops at its deadline against a package index that never answers.
stall_pid=""
if ! start_stalled_index "$WORK/stall.port"; then
  printf 'FAIL pyyaml-venv-stalled-index: loopback listener did not start\n%s\n' \
    "$(head -c 4096 "$WORK/stall.port.err")" >&2
  exit 1
fi
stall_port="$(head -n 1 "$WORK/stall.port")"
stall_t0=$SECONDS
stall_rc=0
stall_out="$(env -u PIP_EXTRA_INDEX_URL -u PIP_FIND_LINKS -u PIP_NO_INDEX \
  PYYAML_VENV_DEADLINE_SECONDS=12 PIP_INDEX_URL="http://127.0.0.1:$stall_port/simple/" \
  PIP_CONFIG_FILE=/dev/null NO_PROXY=127.0.0.1 no_proxy=127.0.0.1 \
  bash "$HERE/pyyaml_venv.sh" "$WORK/venv-stalled" 2>&1)" || stall_rc=$?
stall_elapsed=$((SECONDS - stall_t0))
stall_stop="stopped"
if ! kill "$stall_pid" 2> /dev/null; then
  stall_stop="already gone"
fi
stall_wait_rc=0
wait "$stall_pid" || stall_wait_rc=$?
if [ "$stall_rc" -ne 1 ] || [ "$stall_elapsed" -gt 25 ]; then
  printf 'FAIL pyyaml-venv-stalled-index: want exit 1 within 25 s, got %s after %s s\n' \
    "$stall_rc" "$stall_elapsed" >&2
  printf 'listener %s (wait rc %s)\n%s\n' "$stall_stop" "$stall_wait_rc" "$stall_out" >&2
  exit 1
fi
case "$stall_out" in
  *"PyYAML setup deadline of 12 s passed"*)
    refused=$((refused + 1))
    echo "ok   pyyaml-venv-stalled-index: refused (PyYAML setup deadline of 12 s passed)"
    ;;
  *)
    printf 'FAIL pyyaml-venv-stalled-index: exit 1 without the deadline message\n%s\n' \
      "$stall_out" >&2
    exit 1
    ;;
esac

# workflow-refs.yml carries no uses entry, ignores uses-shaped scalar text, and refuses planted block, flow, local, escaped-key and nested uses keys.
if ! guard_has_no_uses "$REPO_ROOT/.github/workflows/workflow-refs.yml"; then
  echo "FAIL guard-job-no-uses: workflow-refs.yml carried a uses entry" >&2
  exit 1
fi
accepted=$((accepted + 1))
echo "ok   guard-job-no-uses: accepted (no uses in workflow-refs.yml)"

for guard_case in guard-job-run-uses-text guard-job-name-uses-text; do
  case "$guard_case" in
    guard-job-run-uses-text)
      guard_label="uses text in run scalar"
      guard_step='      - run: "echo '\''uses: x/y@v1'\''"'
      ;;
    guard-job-name-uses-text)
      guard_label="uses text in step name"
      guard_step="      - {name: 'uses: x/y@v1', run: echo ok}"
      ;;
  esac
  if ! plant_guard_step "$guard_case" "$guard_step"; then
    printf 'FAIL %s: failed to plant step in workflow-refs.yml\n' "$guard_case" >&2
    exit 1
  fi
  if ! guard_has_no_uses "$WORK/$guard_case.yml"; then
    printf 'FAIL %s: refused planted %s\n' "$guard_case" "$guard_label" >&2
    exit 1
  fi
  accepted=$((accepted + 1))
  echo "ok   $guard_case: accepted ($guard_label)"
done

guard_refused_cases=(
  "guard-job-block-uses|block uses|      - uses: $CHECKOUT # v7.0.1"
  "guard-job-flow-uses|flow uses|      - {uses: $CHECKOUT, with: {persist-credentials: false}}"
  "guard-job-local-uses|local uses|      - uses: ./.github/actions/planted"
  'guard-job-escaped-key-uses|escaped-key uses|      - {"\x75ses": '"$CHECKOUT"'}'
  'guard-job-escaped-local-uses|escaped-key local uses|      - {"\x75ses": ./.github/actions/planted}'
  "guard-job-nested-uses-key|uses key outside the step path|      - {run: echo ok, with: {uses: x/y@v1}}"
)
for ((guard_i = 0; guard_i < 6 && guard_i < ${#guard_refused_cases[@]}; guard_i++)); do
  guard_entry="${guard_refused_cases[$guard_i]}"
  guard_case="${guard_entry%%|*}"
  guard_rest="${guard_entry#*|}"
  guard_label="${guard_rest%%|*}"
  guard_step="${guard_rest#*|}"
  if ! plant_guard_step "$guard_case" "$guard_step"; then
    printf 'FAIL %s: failed to plant step in workflow-refs.yml\n' "$guard_case" >&2
    exit 1
  fi
  if guard_has_no_uses "$WORK/$guard_case.yml"; then
    printf 'FAIL %s: accepted planted %s\n' "$guard_case" "$guard_label" >&2
    exit 1
  fi
  refused=$((refused + 1))
  echo "ok   $guard_case: refused ($guard_label)"
done

# workflow-refs.yml runs on a schedule with a cron string; copies without the schedule or its
# cron line are refused.
if ! guard_has_schedule "$REPO_ROOT/.github/workflows/workflow-refs.yml" > /dev/null; then
  echo "FAIL guard-job-schedule: workflow-refs.yml has no schedule entry with a cron string" >&2
  exit 1
fi
accepted=$((accepted + 1))
echo "ok   guard-job-schedule: accepted (schedule with a cron string)"

if ! plant_guard_quoted_on guard-job-quoted-on quoted; then
  echo "FAIL guard-job-quoted-on: failed to quote the on line of workflow-refs.yml" >&2
  exit 1
fi
if ! guard_has_schedule "$WORK/guard-job-quoted-on.yml" > /dev/null; then
  echo "FAIL guard-job-quoted-on: quoted on key has no schedule entry with a cron string" >&2
  exit 1
fi
accepted=$((accepted + 1))
echo "ok   guard-job-quoted-on: accepted (quoted on key with a cron string)"

if ! plant_guard_quoted_on guard-job-on-twice twice; then
  echo "FAIL guard-job-on-twice: failed to append a quoted on key to workflow-refs.yml" >&2
  exit 1
fi
on_twice_rc=0
on_twice_out="$(guard_has_schedule "$WORK/guard-job-on-twice.yml")" || on_twice_rc=$?
if [ "$on_twice_rc" -ne 1 ] || [ "$on_twice_out" != "on key given twice" ]; then
  printf 'FAIL guard-job-on-twice: want exit 1 and on key given twice, got %s: %s\n' \
    "$on_twice_rc" "$on_twice_out" >&2
  exit 1
fi
refused=$((refused + 1))
echo "ok   guard-job-on-twice: refused (on key given twice)"

for schedule_mode in schedule cron; do
  schedule_case="guard-job-no-schedule"
  [ "$schedule_mode" = schedule ] || schedule_case="guard-job-empty-schedule"
  if ! plant_guard_unscheduled "$schedule_case" "$schedule_mode"; then
    printf 'FAIL %s: failed to drop the %s line from workflow-refs.yml\n' \
      "$schedule_case" "$schedule_mode" >&2
    exit 1
  fi
  schedule_rc=0
  schedule_out="$(guard_has_schedule "$WORK/$schedule_case.yml")" || schedule_rc=$?
  if [ "$schedule_rc" -ne 1 ] || [ "$schedule_out" != "no schedule entry with a cron string" ]; then
    printf 'FAIL %s: want exit 1 and no schedule entry, got %s: %s\n' \
      "$schedule_case" "$schedule_rc" "$schedule_out" >&2
    exit 1
  fi
  refused=$((refused + 1))
  echo "ok   $schedule_case: refused (no schedule entry with a cron string)"
done

# bash_version.sh refuses < 4.4, non-integers and empty args, and accepts 4.4 (boundary) and 5.x.
bash_ver_refused=(
  "3-2|3|2|bash 3.2"
  "4-3|4|3|bash 4.3"
  "4-x|4|x|bash 4 x"
  "empty|||empty version"
)
for ((ver_i = 0; ver_i < 4 && ver_i < ${#bash_ver_refused[@]}; ver_i++)); do
  ver_entry="${bash_ver_refused[$ver_i]}"
  ver_label="${ver_entry%%|*}"
  ver_rest="${ver_entry#*|}"
  ver_major="${ver_rest%%|*}"
  ver_rest="${ver_rest#*|}"
  ver_minor="${ver_rest%%|*}"
  ver_desc="${ver_rest#*|}"
  if (
    # shellcheck source=tools/ci/bash_version.sh
    . "$HERE/bash_version.sh"
    bash_version_supported "$ver_major" "$ver_minor"
  ); then
    printf 'FAIL bash-version-%s: accepted %s\n' "$ver_label" "$ver_desc" >&2
    exit 1
  fi
  refused=$((refused + 1))
  echo "ok   bash-version-$ver_label: refused ($ver_desc)"
done

bash_ver_accepted=(
  "4-4|4|4|bash 4.4 boundary"
  "5-0|5|0|bash 5.0"
  "5-2|5|2|bash 5.2"
)
for ((ver_i = 0; ver_i < 3 && ver_i < ${#bash_ver_accepted[@]}; ver_i++)); do
  ver_entry="${bash_ver_accepted[$ver_i]}"
  ver_label="${ver_entry%%|*}"
  ver_rest="${ver_entry#*|}"
  ver_major="${ver_rest%%|*}"
  ver_rest="${ver_rest#*|}"
  ver_minor="${ver_rest%%|*}"
  ver_desc="${ver_rest#*|}"
  if ! (
    # shellcheck source=tools/ci/bash_version.sh
    . "$HERE/bash_version.sh"
    bash_version_supported "$ver_major" "$ver_minor"
  ); then
    printf 'FAIL bash-version-%s: refused %s\n' "$ver_label" "$ver_desc" >&2
    exit 1
  fi
  accepted=$((accepted + 1))
  echo "ok   bash-version-$ver_label: accepted ($ver_desc)"
done

# stub_scripts_have_no_gnu_timeout FILE...: fail naming the first line of FILE (at most 4 files,
# 65536 lines each) outside a comment that runs GNU timeout, gtimeout or the retired stub variable.
stub_scripts_have_no_gnu_timeout() {
  local re='(^|[|;&(]|[[:space:]])"?\$?\{?(g?time' file line n
  re+='out|STUB_TIME'
  re+='OUT)\}?"?[[:space:]]'
  if [ "$#" -gt 4 ]; then
    echo "stub_scripts_have_no_gnu_timeout: at most 4 files, got $#"
    return 1
  fi
  for file in "$@"; do
    n=0
    while IFS= read -r line || [ -n "$line" ]; do
      n=$((n + 1))
      if [ "$n" -gt 65536 ]; then
        echo "$file: more than 65536 lines"
        return 1
      fi
      if [[ $line =~ ^[[:space:]]*# ]]; then
        continue
      fi
      if [[ $line =~ $re ]]; then
        echo "$file:$n: $line"
        return 1
      fi
    done < "$file"
  done
}

stub_files=("$HERE/workflow_refs_test.sh" "$HERE/workflow-check_test.sh")
if ! hit="$(stub_scripts_have_no_gnu_timeout "${stub_files[@]}")"; then
  printf 'FAIL stubs-no-gnu-timeout: %s\n' "$hit" >&2
  exit 1
fi
accepted=$((accepted + 1))
echo "ok   stubs-no-gnu-timeout: accepted (stubs read under run_bounded.py)"

echo "workflow refs: $accepted accepted, $refused refused"
