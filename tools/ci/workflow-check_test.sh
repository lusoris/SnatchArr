#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for workflow-check.sh. Accepted: a commit pin whose comment names its tag, and
# a commit pin whose comment names a branch (printed as a note). Refused: a tag pin, a comment
# that names another tag, a missing comment, a broken workflow, a missing token, a repository
# without workflow files, a zizmor run that fails or writes no report, and a gate image without
# a digest. Last: Renovate reads both image pins. Adapted from github.com/golusoris/goenvoy.
#
#   tools/ci/workflow-check_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Same needs as the check.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# The commit of the v7.0.1 tag. The master branch of that repository is frozen on another commit.
CHECKOUT=actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1

# plant CASE USES [RUNS_ON_KEY]: one workflow whose only step uses USES.
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
    ${3:-runs-on}: ubuntu-26.04
    steps:
      - uses: $2
        with:
          persist-credentials: false
YAML
}

# expect_accepted CASE NEEDLE: the check must pass and its output must name NEEDLE.
expect_accepted() {
  local name="$1" needle="$2" out
  if ! out="$(bash "$HERE/workflow-check.sh" "$WORK/$name" 2>&1)"; then
    printf 'FAIL %s: refused\n%s\n' "$name" "$out" >&2
    exit 1
  fi
  case "$out" in
    *"$needle"*) echo "ok   $name: accepted ($needle)" ;;
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
  if out="$("$@" bash "$HERE/workflow-check.sh" "$WORK/$name" 2>&1)"; then
    printf 'FAIL %s: accepted\n%s\n' "$name" "$out" >&2
    exit 1
  fi
  case "$out" in
    *"$needle"*) echo "ok   $name: refused ($needle)" ;;
    *)
      printf 'FAIL %s: refused without "%s"\n%s\n' "$name" "$needle" "$out" >&2
      exit 1
      ;;
  esac
}

plant clean "$CHECKOUT # v7.0.1"
expect_accepted clean "no version comment names another tag"

plant branch-comment "$CHECKOUT # master"
expect_accepted branch-comment "note .github/workflows/planted.yml:10: ref-version-mismatch: branch points to commit"

plant tag-pin "actions/checkout@v7"
expect_refusal tag-pin "refused .github/workflows/planted.yml:10: unpinned-uses"

plant branch-pin "actions/checkout@main"
expect_refusal branch-pin "refused .github/workflows/planted.yml:10: unpinned-uses"

plant stale-comment "$CHECKOUT # v4.0.0"
expect_refusal stale-comment "refused .github/workflows/planted.yml:10: ref-version-mismatch"

plant no-comment "$CHECKOUT"
expect_refusal no-comment "refused .github/workflows/planted.yml:10: ref-version-mismatch"

plant broken "$CHECKOUT # v7.0.1" runs-in
expect_refusal broken "syntax-check"

expect_refusal clean "is required" env -u GH_TOKEN -u GITHUB_TOKEN

mkdir -p "$WORK/empty"
expect_refusal empty "no workflow file"

# A stand-in for docker: actionlint passes, zizmor ends with the given exit status, no report.
stub() {
  printf '#!/bin/sh\ncase "$*" in *zizmor*) exit %s ;; esac\n' "$2" > "$WORK/$1"
  chmod +x "$WORK/$1"
}
stub docker-zizmor-fails 3
expect_refusal clean "zizmor exit 3" env DOCKER_BIN="$WORK/docker-zizmor-fails"
stub docker-zizmor-silent 0
expect_refusal clean "zizmor wrote no report" env DOCKER_BIN="$WORK/docker-zizmor-silent"

# A copy of the check whose zizmor image carries no digest.
mkdir -p "$WORK/copy/tools/ci" "$WORK/copy/.github"
cp "$HERE/workflow-check.sh" "$WORK/copy/tools/ci/"
sed 's/^\(ZIZMOR_IMAGE=.*\)@sha256:.*$/\1/' "$HERE/tool-versions.env" > "$WORK/copy/tools/ci/tool-versions.env"
HERE="$WORK/copy/tools/ci" expect_refusal clean "is not an image pinned by version and digest"

# renovate_reads PINS: how many patterns of the Renovate manager for the pin file match PINS
# exactly once. A pattern that matches nothing stops the updates without an error.
renovate_reads() {
  jq -r --rawfile pins "$1" '
    [ .customManagers[]
      | select(.managerFilePatterns == ["tools/ci/tool-versions.env"])
      | .matchStrings[] as $pattern
      | [ $pins | match($pattern; "g") ] | select(length == 1) ]
    | length
  ' "$HERE/../../renovate.json"
}
if [ "$(renovate_reads "$HERE/tool-versions.env")" != 2 ]; then
  echo "FAIL renovate: renovate.json does not read both image pins of tools/ci/tool-versions.env" >&2
  exit 1
fi
echo "ok   renovate: both image pins are read"
if [ "$(renovate_reads "$WORK/copy/tools/ci/tool-versions.env")" != 1 ]; then
  echo "FAIL renovate: the pin without a digest is still read" >&2
  exit 1
fi
echo "ok   renovate: a pin without a digest is not read"

echo "workflow check: 2 accepted, 10 refused; Renovate reads 2 pins"
