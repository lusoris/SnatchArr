#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Unit tests for ruff_check.sh verifying planted lint failures, clean lint,
# requirements validation, and version checking.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
check="$HERE/ruff_check.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0

cache="${1:-$work/cache}"

# new_repo DIR: initialize a throwaway git repository at DIR.
new_repo() {
    local dir=$1
    mkdir -p "$dir"
    git -C "$dir" init -q -b main
    git -C "$dir" config user.email t@example.invalid
    git -C "$dir" config user.name t
    git -C "$dir" config commit.gpgsign false
}

# test_planted_isc004: verify that implicit string concatenation (ISC004) is caught and refused.
test_planted_isc004() {
    local repo="$work/repo-isc004"
    new_repo "$repo"
    # the repository's settings without its own per-file exceptions
    sed '/^\[lint.per-file-ignores\]/,$d' "$REPO_ROOT/ruff.toml" > "$repo/ruff.toml"
    printf '%s\n' 'x = ["a" "b", "c"]' > "$repo/main.py"
    git -C "$repo" add ruff.toml main.py
    git -C "$repo" commit -q -m "initial"

    set +e
    out=$(sh "$check" "$cache" "$repo" 2>&1)
    status=$?
    set -e

    if [ "$status" -eq 0 ]; then
        echo "FAIL isc004: expected non-zero exit status, got 0"
        failed=1
    elif ! echo "$out" | grep -q "ISC004"; then
        echo "FAIL isc004: expected output to mention ISC004, got: $out"
        failed=1
    else
        echo "ok   planted ISC004 refused"
    fi
}

# test_clean: verify that clean code matching ruff.toml rules passes with exit status 0.
test_clean() {
    local repo="$work/repo-clean"
    new_repo "$repo"
    # the repository's settings without its own per-file exceptions
    sed '/^\[lint.per-file-ignores\]/,$d' "$REPO_ROOT/ruff.toml" > "$repo/ruff.toml"
    printf '%s\n' 'x = ["a", "b", "c"]' > "$repo/main.py"
    git -C "$repo" add ruff.toml main.py
    git -C "$repo" commit -q -m "initial"

    set +e
    out=$(sh "$check" "$cache" "$repo" 2>&1)
    status=$?
    set -e

    if [ "$status" -ne 0 ]; then
        echo "FAIL clean: expected exit status 0, got $status: $out"
        failed=1
    else
        echo "ok   clean file passes"
    fi
}

# test_req_without_hash: verify that requirements lacking sha256 hashes are refused.
test_req_without_hash() {
    local bad_req="$work/bad-requirements.txt"
    printf '%s\n' 'ruff==0.17.3' > "$bad_req"

    set +e
    out=$(RUFF_REQ="$bad_req" sh "$check" "$cache" "$work" 2>&1)
    status=$?
    set -e

    if [ "$status" -eq 0 ]; then
        echo "FAIL req-without-hash: expected failure, got 0"
        failed=1
    elif ! echo "$out" | grep -q "not pinned by version and hash"; then
        echo "FAIL req-without-hash: expected hash error message, got: $out"
        failed=1
    else
        echo "ok   requirements without hash refused"
    fi
}

# test_version_mismatch: verify that an installed ruff with mismatched version is refused.
test_version_mismatch() {
    local stub="$work/stub_ruff.sh"
    printf '%s\n' '#!/bin/sh' 'echo "ruff 0.0.1"' > "$stub"
    chmod +x "$stub"

    set +e
    out=$(RUFF_BIN="$stub" sh "$check" "$cache" "$work" 2>&1)
    status=$?
    set -e

    if [ "$status" -eq 0 ]; then
        echo "FAIL version-mismatch: expected failure, got 0"
        failed=1
    elif ! echo "$out" | grep -q "reports 0.0.1"; then
        echo "FAIL version-mismatch: expected version mismatch message, got: $out"
        failed=1
    else
        echo "ok   version mismatch refused"
    fi
}

# exception_repo DIR HEADER RULES [KEY]: throwaway repo whose ruff.toml excepts RULES
# for main.py (which fires F401) under the comment line HEADER.
exception_repo() {
    local dir=$1 header=$2 rules=$3 key=${4:-'"main.py"'}
    new_repo "$dir"
    printf '%s\n' 'target-version = "py314"' '' '[lint.per-file-ignores]' "$header" "$key = [$rules]" > "$dir/ruff.toml"
    printf '%s\n' 'import os' > "$dir/main.py"
    git -C "$dir" add ruff.toml main.py
    git -C "$dir" commit -q -m "initial"
}

# expect_exception_case NAME WANT_RC NEEDLE DIR: run the check on DIR on a fixed
# day (2026-11-01) and compare exit status and output.
expect_exception_case() {
    local name=$1 want=$2 needle=$3 dir=$4 out status
    set +e
    out=$(RUFF_CHECK_TODAY=2026-11-01 sh "$check" "$cache" "$dir" 2>&1)
    status=$?
    set -e
    if [ "$want" -eq 0 ] && [ "$status" -ne 0 ]; then
        echo "FAIL $name: exit $status, want 0: $out"
        failed=1
    elif [ "$want" -ne 0 ] && { [ "$status" -eq 0 ] || ! echo "$out" | grep -q "$needle"; }; then
        echo "FAIL $name: exit $status, want non-zero naming '$needle': $out"
        failed=1
    else
        echo "ok   $name"
    fi
}

# test_exceptions: a valid exception passes; a missing comment, an expired
# entry, a rule that no longer fires, an indented key without its comment and a
# key the check cannot read are each refused.
test_exceptions() {
    local good='# category A, entered 2026-11-01, expires 2026-12-31: planted'
    exception_repo "$work/exc-valid" "$good" '"F401"'
    expect_exception_case "valid exception passes" 0 "" "$work/exc-valid"
    exception_repo "$work/exc-bare" '# planted without category' '"F401"'
    expect_exception_case "exception without category line refused" 1 "lacks its" "$work/exc-bare"
    exception_repo "$work/exc-expired" '# category A, entered 2026-01-01, expires 2026-10-31: planted' '"F401"'
    expect_exception_case "expired exception refused" 1 "expired on 2026-10-31" "$work/exc-expired"
    exception_repo "$work/exc-stale" "$good" '"F401", "E501"'
    expect_exception_case "stale exception refused" 1 "E501 for main.py is stale" "$work/exc-stale"
    exception_repo "$work/exc-indent" '# planted without category' '"F401"' '  "main.py"'
    expect_exception_case "indented key without category line refused" 1 "lacks its" "$work/exc-indent"
    exception_repo "$work/exc-squote" "$good" '"F401"' "'main.py'"
    expect_exception_case "key the check cannot read refused" 1 "the exception check read 0" "$work/exc-squote"
}

test_planted_isc004
test_clean
test_req_without_hash
test_version_mismatch
test_exceptions

if [ "$failed" -ne 0 ]; then
    exit 1
fi
