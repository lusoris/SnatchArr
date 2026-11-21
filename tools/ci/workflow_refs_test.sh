#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Offline planted cases for workflow_refs.sh and workflow_uses.py (no Docker, no network, stub
# GraphQL curl on every call, fixed placeholder token).
# Accepted: one current repository (clean), two current repositories across a workflow and a
# composite action (two-current), uses-shaped text inside a run script block (run-block-uses), a
# Docker-only local action directory holding only Dockerfile (docker-local-action), and three
# distinct repositories batched into one GraphQL POST (three-repos).
# Refused: a reusable workflow under a moved repository name (moved-workflow), a composite action
# step under a moved repository name (moved-action), a failed GraphQL HTTP call and an empty JSON
# object on clean, a moved repository reached via YAML anchor and alias (moved-anchor), a
# flow-style step under a moved repository name (moved-flow), an unquoted boolean job id `yes`
# under a moved repository name (moved-bool-job-id), a nested composite action under a moved
# repository name (moved-nested-action), a composite action outside .github/actions reached via
# `uses: ./tools/actions/x` (moved-outside-action), a composite action reached via self-repository
# `uses: $/.github/actions/planted` under a moved repository name (moved-self-repo-action), a
# self-repository reference carrying `@ref` (self-repo-with-ref), a composite action nested 9
# directories under .github/actions (moved-deep-action), a missing local action `uses: ./missing`
# (missing-local-action), a local action escaping ROOT `uses: ./../outside` (escape-local-action),
# an unreadable directory under .github/actions when unprivileged (unreadable-actions-dir), a
# GraphQL errors array (gql-errors), a null GraphQL alias for a private or missing repository
# (null-alias), 101 repositories split across 2 GraphQL POSTs with a moved repository in the
# second batch (over-100-repos), a missing token (missing-token), and workflow_uses.py without
# PyYAML (no-pyyaml).
#
#   tools/ci/workflow_refs_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs python3 with PyYAML and jq.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
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

# Supply a fixed placeholder token so no caller token ever reaches a stub.
export GH_TOKEN=stub-token
unset GITHUB_TOKEN

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

# A fake GitHub GraphQL API: golusoris/<repo> moved to plurano/<repo> and moved/<repo> moved to
# current/<repo>; any other repository answers its own name. Optionally counts invocations in
# CURL_COUNT_FILE. Every stub runs under a 5 s bound with bounded argument loops.
cat > "$WORK/curl-moved" <<'SH'
#!/bin/sh
set -eu
timeout 5 head -c 65536 > /dev/null
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
printf '%s\n' "$payload" | timeout 5 jq -c '
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
printf '#!/bin/sh\ntimeout 5 head -c 65536 > /dev/null\nexit 22\n' > "$WORK/curl-fails"
printf '#!/bin/sh\ntimeout 5 head -c 65536 > /dev/null\necho "{}"\n' > "$WORK/curl-nameless"
cat > "$WORK/curl-gql-errors" <<'SH'
#!/bin/sh
timeout 5 head -c 65536 > /dev/null
printf '%s\n' '{"data":{"r0":{"nameWithOwner":"actions/checkout"}},"errors":[{"message":"API rate limit exceeded"}]}'
SH
cat > "$WORK/curl-null-alias" <<'SH'
#!/bin/sh
timeout 5 head -c 65536 > /dev/null
printf '%s\n' '{"data":{"r0":null}}'
SH
chmod +x "$WORK/curl-moved" "$WORK/curl-fails" "$WORK/curl-nameless" "$WORK/curl-gql-errors" "$WORK/curl-null-alias"

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

# A composite action reached via `uses: $/.github/actions/planted` is scanned and refused.
mkdir -p "$WORK/moved-self-repo-action/.github/workflows" "$WORK/moved-self-repo-action/.github/actions/planted"
cat > "$WORK/moved-self-repo-action/.github/actions/planted/action.yml" <<'YAML'
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
      - uses: \$/.github/actions/planted
YAML
expect_refusal moved-self-repo-action "refused .github/actions/planted/action.yml:8: moved/repo moved to current/repo" env CURL_BIN="$WORK/curl-moved"

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

# Missing GH_TOKEN and GITHUB_TOKEN is refused before any lookup.
plant missing-token "$CHECKOUT # v7.0.1"
expect_refusal missing-token "is required" env -u GH_TOKEN -u GITHUB_TOKEN CURL_BIN="$WORK/curl-moved"

# workflow_uses.py under `python3 -I -S` (no site packages -> no PyYAML) exits 2 with `PyYAML is required`.
pyyaml_rc=0
pyyaml_out="$(python3 -I -S "$HERE/workflow_uses.py" "$WORK/clean/.github/workflows/planted.yml" 2>&1)" || pyyaml_rc=$?
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

echo "workflow refs: $accepted accepted, $refused refused"
