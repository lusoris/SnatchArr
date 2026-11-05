#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Replay pushed ranges against the pinned scanner in a throwaway repository.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
check="$HERE/prepush_secret_scan.sh"
platform="$(uname -s)/$(uname -m)"
if [ "$platform" != Linux/x86_64 ]; then
    echo "pre-push scan tests: skipped on $platform: the gitleaks pin is the linux x64 archive"
    exit 0
fi
work="$(mktemp -d)"
trap 'rm -rf "$work"' 0
repo="$work/repo"
mkdir -p "$repo"
failed=0
ran=0

# fixture_git ARGS: run git with a disposable author and unsigned fixture commits.
fixture_git() {
    timeout 60 git -C "$repo" -c user.name=fixture -c user.email=fixture@invalid \
        -c commit.gpgsign=false "$@"
}

# update LOCAL REMOTE: emit a complete pre-push ref update.
update() {
    printf 'refs/heads/main %s refs/heads/main %s\n' "$1" "$2"
}

# expect WANT LABEL INPUT NEEDLE: assert the status and safe diagnostic from the hook.
expect() {
    want="$1" label="$2" input="$3" needle="$4"
    ran=$((ran + 1))
    rc=0
    out="$(printf '%s' "$input" | timeout 660 sh "$check" 2>&1)" || rc=$?
    if [ "$rc" -ne "$want" ]; then
        echo "FAIL $label: exit $rc, want $want" >&2
        printf '%s\n' "$out" >&2
        failed=$((failed + 1))
    elif ! printf '%s\n' "$out" | grep -Fq -- "$needle"; then
        echo "FAIL $label: missing diagnostic '$needle'" >&2
        failed=$((failed + 1))
    elif printf '%s\n' "$out" | grep -Fq 'Unused exception'; then
        echo "FAIL $label: range scan reported unused exceptions" >&2
        failed=$((failed + 1))
    else
        echo "ok   $label"
    fi
}

# check_exceptions: prove active/expired path and fingerprint exceptions in range mode.
check_exceptions() {
    future="$(python3 -I -c 'import datetime; print((datetime.datetime.now(datetime.UTC).date() + datetime.timedelta(days=1)).isoformat())')"
    for target in "$planted:settings.py:generic-api-key:1" 'settings.py:generic-api-key'; do
        printf '# expires %s: planted fixture\n%s\n' "$future" "$target" > .gitleaksignore
        expect 0 "active exception $target" "$blocked_input" '0 blocking'
        printf '# expires 2000-01-01: planted fixture\n%s\n' "$target" > .gitleaksignore
        expect 1 "expired exception $target" "$blocked_input" "$planted:settings.py:generic-api-key:1"
    done
    printf '# expires %s: unused fixture\nabsent.py:generic-api-key\n' "$future" > .gitleaksignore
    expect 0 'range omits unused exception diagnostics' "$clean_input" '0 blocking'
    printf '%s\n' 'malformed exception' > .gitleaksignore
    expect 2 'malformed exception list fails closed' "$clean_input" 'could not scan'
    rm .gitleaksignore
}

# check_setup_failure: a corrupt cached archive must produce rc2, never a finding.
check_setup_failure() {
    mkdir -p "$work/broken/tools/ci" "$work/broken/.cache/gitleaks"
    cp "$HERE/prepush_secret_scan.sh" "$HERE/gitleaks_scan.sh" "$work/broken/tools/ci/"
    for archive in "$HERE/../../.cache/gitleaks"/gitleaks_*_linux_x64.tar.gz; do
        printf 'corrupt archive\n' > "$work/broken/.cache/gitleaks/$(basename "$archive")"
    done
    check="$work/broken/tools/ci/prepush_secret_scan.sh"
    expect 2 'corrupt scanner cache fails closed' "$clean_input" 'scanner setup failed'
    check="$HERE/prepush_secret_scan.sh"
}

fixture_git init --quiet --initial-branch main
printf 'fixture\n' > "$repo/README.md"
fixture_git add README.md
fixture_git commit --quiet --message fixture
base="$(fixture_git rev-parse HEAD)"
fixture_git update-ref refs/remotes/origin/main "$base"
# Derive the planted value at runtime; no key-shaped literal lives in this file.
python3 -I -c 'import base64, hashlib, sys
from pathlib import Path
value = base64.urlsafe_b64encode(hashlib.sha256(b"pushed-range fixture").digest()).decode()[:40]
Path(sys.argv[1]).write_text("api_" + "key = \"" + value + "\"\n", encoding="utf-8")' "$repo/settings.py"
fixture_git add settings.py
fixture_git commit --quiet --message planted
planted="$(fixture_git rev-parse HEAD)"
printf 'clean update\n' >> "$repo/README.md"
fixture_git add README.md
fixture_git commit --quiet --message clean
tip="$(fixture_git rev-parse HEAD)"
zeros="$(printf '%040d' 0)"
missing="$(printf '%040d' 1)"
cd "$repo"

# Keep trailing newlines to exercise real multi-line pre-push stdin.
blocked_input="$(update "$planted" "$base")
"
clean_input="$(update "$tip" "$planted")
"
expect 1 'planted commit in pushed range' "$blocked_input" "$planted:settings.py:generic-api-key:1"
expect 0 'clean pushed range' "$clean_input" '0 blocking'
fixture_git update-ref refs/remotes/origin/main "$planted"
expect 0 'planted commit already on remote' "$clean_input" '0 blocking'
fixture_git update-ref refs/remotes/origin/main "$base"
expect 1 'new branch containing planted commit' "$(update "$tip" "$zeros")" "$planted:settings.py:generic-api-key:1"
fixture_git update-ref refs/remotes/origin/main "$planted"
expect 0 'new branch excludes commits on remotes' "$(update "$tip" "$zeros")" '0 blocking'
expect 0 'delete ref' "$(update "$zeros" "$planted")" ''
expect 0 'no ref updates' '' 'no stdin ref updates'
expect 2 'malformed stdin line' 'refs/heads/main invalid' 'malformed stdin line'
expect 2 'extra stdin field' "$(update "$tip" "$planted") extra" 'malformed stdin line'
expect 2 'nonhex object ID' "$(update "${tip%?}x" "$planted")" 'malformed stdin line'
expect 2 'unknown remote object fails closed' "$(update "$tip" "$missing")" 'could not scan'
expect 1 'multiple refs include planted second range' "$clean_input$blocked_input" "$planted:settings.py:generic-api-key:1"
expect 1 'multiple refs preserve earlier finding' "$blocked_input$clean_input" "$planted:settings.py:generic-api-key:1"
check_exceptions
check_setup_failure
# Full-history scans cannot see uncommitted worktree files: replay this source in git too.
cp "$HERE/prepush_secret_scan_test.sh" "$repo/check-fixture.sh"
fixture_git add check-fixture.sh
fixture_git commit --quiet --message 'test source'
source_tip="$(fixture_git rev-parse HEAD)"
expect 0 'test source contains no matching literal' "$(update "$source_tip" "$tip")" '0 blocking'

if [ "$failed" -ne 0 ]; then
    echo "pre-push scan tests: $failed of $ran cases failed" >&2
    exit 1
fi
echo "pre-push scan tests: $ran cases passed"
