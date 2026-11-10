#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for test_levels_check.py.
#   bash tools/ci/test_levels_check_test.sh [ENGINE]
# Stub engines first, on small repositories: each stub prints a planted audit output and the
# check must judge it, and each repository plants a level layout for the own-files rule. With
# ENGINE (an engine executable, as make passes $(PRAETORCTL)), the real engine then audits a copy
# of the tracked tree with one binding broken per case (job, command, path, pattern, unit tests
# left with integration tests only), and the check must refuse each one and pass the unbroken
# copy. Without ENGINE the real-engine cases are not run, and the script says so.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
check=$HERE/test_levels_check.py
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"

PASS_LINE='[PASS] Release test levels: 6 required test level(s) verified (unit, web_unit), 0 baselined'
WARN_LINE='[WARN] Release test levels: 1 unbaselined test level gap(s): web_unit (framework): no runnable workflow job executes this level'
LEVELS='release:
  test_levels:
    unit:
      paths:
        - api/**/*_test.go
    integration:
      paths:
        - api/**/*_integration_test.go'

# repo NAME MANIFEST FILE...: commit MANIFEST as .standards.yaml (none if empty) and each FILE
# in a new repository; print its path.
repo() {
    local dir="$work/repo-$1" manifest=$2
    shift 2
    new_repo "$dir"
    if [ -n "$manifest" ]; then printf '%s\n' "$manifest" > "$dir/.standards.yaml"; fi
    for file in "$@"; do
        mkdir -p "$dir/$(dirname "$file")"
        : > "$dir/$file"
    done
    git -C "$dir" add -A
    git -C "$dir" commit -q --allow-empty -m 'test: planted layout'
    printf '%s\n' "$dir"
}

# stub NAME STATUS LINE...: write an engine stub that records its arguments, prints each LINE
# and exits with STATUS; print its path.
stub() {
    local path="$work/stub-$1" status=$2
    shift 2
    {
        printf '%s\n' '#!/bin/sh' "printf '%s\\n' \"\$*\" > \"$work/args-${path##*/stub-}\""
        for line in "$@"; do printf 'printf %%s\\\\n %q\n' "$line"; done
        printf 'exit %s\n' "$status"
    } > "$path"
    chmod +x "$path"
    printf '%s\n' "$path"
}

# run_check NAME WANT NEEDLE ROOT ENGINE: run the check in ROOT with ENGINE and judge it.
run_check() {
    local out status=0
    out=$(python3 -I "$check" --engine "$5" --root "$4" 2>&1) || status=$?
    check_case "$1" "$2" "$status" "$3" "$out"
    echo "ok   $1"
}

# expect_stub NAME WANT NEEDLE STATUS LINE...: run the check with a stub engine on the base
# repository (unit keeps one file apart from integration) and judge it.
expect_stub() {
    local name=$1 want=$2 needle=$3
    shift 3
    run_check "$name" "$want" "$needle" "$base" "$(stub "$name" "$@")"
}

base=$(repo base "$LEVELS" api/a_test.go api/b_integration_test.go)
expect_stub pass 0 "ok: release test levels pass: 6 required" 0 '[INFO] other gate' "$PASS_LINE"
check_case pass-args 0 0 "audit --offline" "$(cat "$work/args-pass")"
echo "ok   pass-args"
expect_stub warn 3 "refused: release test levels WARN: 1 unbaselined test level gap(s): web_unit" 0 "$WARN_LINE"
expect_stub fail 3 "refused: release test levels FAIL:" 1 '[FAIL] Release test levels: release test levels gate failed'
expect_stub skip 3 "refused: release test levels SKIP:" 0 '[SKIP] Release test levels: release.test_levels not declared'
expect_stub missing 3 "found 0 (audit exit 0)" 0 '[PASS] HISS audit' 'Audit Summary: passed'
expect_stub stopped 3 "found 0 (audit exit 1)" 1 '[FAIL] Release end-to-end evidence: 11 commits'
expect_stub twice 3 "found 2" 0 "$PASS_LINE" "$PASS_LINE"
expect_stub indented 3 "found 0" 0 "  $PASS_LINE"
run_check no-engine 1 "error: cannot run $work/no-such-engine audit --offline" "$base" "$work/no-such-engine"

# The own-files rule, each case with an engine stub whose audit passes.
pass=$(stub own 0 "$PASS_LINE")
run_check own-apart 0 "ok: level unit keeps 1 of its 2 files apart from integration" "$base" "$pass"
run_check own-none 3 "refused: level unit has no file of its own: each of its 1 files is also matched by integration" \
    "$(repo none "$LEVELS" api/b_integration_test.go)" "$pass"
untracked=$(repo untracked "$LEVELS" api/b_integration_test.go)
: > "$untracked/api/a_test.go"
run_check own-untracked 3 "refused: level unit has no file of its own" "$untracked" "$pass"
run_check own-equal 3 "refused: level integration has no file of its own: each of its 1 files is also matched by unit" \
    "$(repo equal "${LEVELS%_integration_test.go}_test.go" api/a_test.go)" "$pass"
run_check own-disjoint 0 "ok: release test levels pass" \
    "$(repo disjoint "${LEVELS/api\/\*\*\/\*_integration_test.go/it/*_test.go}" api/a_test.go it/b_test.go)" "$pass"
run_check own-dirglob 0 "note: level integration not compared for own files: directory glob" \
    "$(repo dirglob "${LEVELS/\*\*\/\*_integration_test.go/**}" api/b_integration_test.go)" "$pass"
run_check own-syntax 1 "paths entry 'api/{a,b}_test.go': only literal characters" \
    "$(repo syntax "${LEVELS/\*\*\/\*_integration_test.go/\{a,b\}_test.go}" api/a_test.go)" "$pass"
run_check own-no-manifest 1 "error: cannot read .standards.yaml" "$(repo nomanifest '' api/a_test.go)" "$pass"
run_check own-not-a-repository 1 "error: git ls-files in $work/plain failed" \
    "$(mkdir -p "$work/plain" && printf '%s\n' "$LEVELS" > "$work/plain/.standards.yaml" && printf '%s' "$work/plain")" "$pass"

engine=${1:-}
if [ -z "$engine" ]; then
    echo "real-engine cases not run: no ENGINE argument"
    exit 0
fi

# A copy of the tracked tree in a fresh repository with one commit and the checkout's origin URL
# (the audit's forge gates read it; --offline fetches nothing), so the audit reads the same
# manifest and workflows as the checkout.
tree="$work/tree"
mkdir -p "$tree"
git -C "$ROOT" ls-files -z | (cd "$ROOT" && tar --null -T - -cf -) | tar -xf - -C "$tree"
new_repo "$tree"
if origin=$(git -C "$ROOT" remote get-url origin 2>/dev/null); then
    git -C "$tree" remote add origin "$origin"
fi
git -C "$tree" add -A
git -C "$tree" commit -q -m 'test: tracked tree'

# expect_tree NAME WANT NEEDLE [SED]: audit a copy of the tree, edited by SED inside the
# web_unit (or load_soak, for a pattern) binding of .standards.yaml, and judge it.
expect_tree() {
    local name=$1 want=$2 needle=$3 edit=${4:-} copy
    copy="$work/$name"
    cp -a "$tree" "$copy"
    if [ -n "$edit" ]; then
        sed -i -e "$edit" "$copy/.standards.yaml"
        if cmp -s "$tree/.standards.yaml" "$copy/.standards.yaml"; then
            printf 'FAIL %s: the plant did not change .standards.yaml\n' "$name" >&2
            exit 1
        fi
    fi
    run_check "$name" "$want" "$needle" "$copy" "$engine"
}

level='/^    web_unit:$/,/^    [a-z_]*:$/'
no_job='web_unit (framework): no runnable workflow job executes this level'
expect_tree tree-unbroken 0 "ok: release test levels pass"
expect_tree tree-job 3 "WARN: 1 unbaselined test level gap(s): $no_job" "$level s/job: web\$/job: web-no-such-job/"
expect_tree tree-command 3 "WARN: 1 unbaselined test level gap(s): $no_job" "$level s/run: pnpm test\$/run: pnpm no-such-command/"
expect_tree tree-path 3 "web_unit (framework): no tracked file matches paths [web/src/**/*.no-such-test.ts]" \
    "$level s|web/src/\*\*/\*.test.ts|web/src/**/*.no-such-test.ts|"
expect_tree tree-pattern 3 'load_soak (app-service): no matched file satisfies pattern "no_such_soak_round"' \
    '/^    load_soak:$/,/^    [a-z_]*:$/ s/pattern: soak_round$/pattern: no_such_soak_round/'

# Every Go unit test removed, integration tests kept: the engine still passes the unit level on
# the integration files, and the own-files rule refuses it.
copy="$work/tree-unit-only-integration"
cp -a "$tree" "$copy"
git -C "$copy" ls-files -z -- ':(glob)api/**/*_test.go' ':(exclude,glob)api/**/*_integration_test.go' |
    xargs -0 git -C "$copy" rm -q --
git -C "$copy" commit -q -m 'test: unit tests removed'
status=0
out=$(python3 -I "$check" --engine "$engine" --root "$copy" 2>&1) || status=$?
check_case tree-unit-only-integration 3 "$status" "refused: level unit has no file of its own" "$out"
check_case tree-unit-only-integration-engine 3 "$status" "ok: release test levels pass" "$out"
echo "ok   tree-unit-only-integration"
