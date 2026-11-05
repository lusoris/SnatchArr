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
    python3 -I -B - "$HERE" "$repo" "$@" <<'PY'
import subprocess
import sys

sys.path.insert(0, sys.argv[1])
from secret_scan import git_environment

subprocess.run(
    ["git", "-C", sys.argv[2], "-c", "user.name=fixture", "-c", "user.email=fixture@invalid",
     "-c", "commit.gpgsign=false", *sys.argv[3:]],
    env=git_environment(), timeout=60, check=True,
)
PY
}

# update LOCAL REMOTE: emit a complete pre-push ref update.
update() {
    printf 'refs/heads/main %s refs/heads/main %s\n' "$1" "$2"
}

# expect WANT LABEL INPUT NEEDLE [REMOTE]: assert the status and safe diagnostic from the hook.
expect() {
    want="$1" label="$2" input="$3" needle="$4"
    shift 4
    ran=$((ran + 1))
    rc=0
    out="$(printf '%s' "$input" | timeout 660 sh "$check" "$@" 2>&1)" || rc=$?
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
        expect 0 "active exception $target" "$blocked_input" '0 blocking' origin
        printf '# expires 2000-01-01: planted fixture\n%s\n' "$target" > .gitleaksignore
        expect 1 "expired exception $target" "$blocked_input" "$planted:settings.py:generic-api-key:1" origin
    done
    printf '# expires %s: unused fixture\nabsent.py:generic-api-key\n' "$future" > .gitleaksignore
    expect 0 'range omits unused exception diagnostics' "$clean_input" '0 blocking' origin
    printf '%s\n' 'malformed exception' > .gitleaksignore
    expect 2 'malformed exception list fails closed' "$clean_input" 'could not scan' origin
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
    expect 2 'corrupt scanner cache fails closed' "$clean_input" 'scanner setup failed' origin
    check="$HERE/prepush_secret_scan.sh"
}

# check_hook_cases: replay ranges and fixture writes with throwaway hook variables.
check_hook_cases() (
    export GIT_DIR="$hook_gitdir" GIT_INDEX_FILE="$hook_gitdir/index"
    before_failed="$failed"
    expect 1 'hook environment: planted range' "$blocked_input" "$planted:settings.py:generic-api-key:1" origin
    expect 0 'hook environment: clean range' "$clean_input" '0 blocking' origin
    mkdir -p "$work/hook-fixture" || exit 1
    fixture_git -C "$work/hook-fixture" init --quiet --initial-branch main || exit 1
    printf 'fixture\n' > "$work/hook-fixture/README.md"
    fixture_git -C "$work/hook-fixture" add README.md || exit 1
    fixture_git -C "$work/hook-fixture" commit --quiet --message fixture || exit 1
    [ -d "$work/hook-fixture/.git" ] && [ "$failed" -eq "$before_failed" ]
)

# check_hook_environment: hook runs must preserve the outer config, refs and indexes.
check_hook_environment() {
    fixture_git worktree add --quiet -b hook "$work/hook"
    hook_gitdir="$(fixture_git -C "$work/hook" rev-parse --absolute-git-dir)"
    hook_heads="$(fixture_git rev-parse HEAD refs/heads/main refs/heads/hook)"
    cp "$repo/.git/config" "$work/config.before"
    cp "$repo/.git/HEAD" "$work/head.before"
    cp "$repo/.git/index" "$work/index.before"
    cp "$hook_gitdir/HEAD" "$work/hook-head.before"
    cp "$hook_gitdir/index" "$work/hook-index.before"
    ran=$((ran + 3))
    if ! check_hook_cases; then
        echo 'FAIL hook environment: ranges or fixture writes' >&2
        failed=$((failed + 1))
    fi
    if ! cmp -s "$work/config.before" "$repo/.git/config" ||
        [ "$(fixture_git config --get core.bare)" != false ] ||
        ! cmp -s "$work/head.before" "$repo/.git/HEAD" ||
        ! cmp -s "$work/hook-head.before" "$hook_gitdir/HEAD" ||
        [ "$(fixture_git rev-parse HEAD refs/heads/main refs/heads/hook)" != "$hook_heads" ] ||
        ! cmp -s "$work/index.before" "$repo/.git/index" ||
        ! cmp -s "$work/hook-index.before" "$hook_gitdir/index"; then
        echo 'FAIL hook environment: outer config, HEAD, branch or index changed' >&2
        failed=$((failed + 1))
    else
        echo 'ok   hook environment: outer config, HEAD, branches and indexes unchanged'
    fi
}

fixture_git init --quiet --initial-branch main
printf 'fixture\n' > "$repo/README.md"
fixture_git add README.md
fixture_git commit --quiet --message fixture
base="$(fixture_git rev-parse HEAD)"
fixture_git remote add origin "$work/origin.git"
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
expect 1 'planted commit in pushed range' "$blocked_input" "$planted:settings.py:generic-api-key:1" origin
expect 0 'clean pushed range' "$clean_input" '0 blocking' origin
fixture_git update-ref refs/remotes/origin/main "$planted"
expect 0 'planted commit already on remote' "$clean_input" '0 blocking' origin
fixture_git update-ref refs/remotes/origin/main "$base"
expect 1 'new branch containing planted commit' "$(update "$tip" "$zeros")" "$planted:settings.py:generic-api-key:1" origin
fixture_git update-ref refs/remotes/other/main "$planted"
expect 1 'new branch includes commits only on other remote' "$(update "$tip" "$zeros")" "$planted:settings.py:generic-api-key:1" origin
fixture_git update-ref refs/remotes/origin/main "$planted"
expect 0 'new branch excludes destination remote commits' "$(update "$tip" "$zeros")" '0 blocking' origin
expect 2 'missing remote argument fails closed' "$clean_input" 'missing remote argument'
expect 2 'empty remote argument fails closed' "$clean_input" 'missing remote argument' ''
expect 1 'URL destination scans full reachable history' "$(update "$tip" "$zeros")" "$planted:settings.py:generic-api-key:1" 'https://example.invalid/repo.git'
for remote in 'with.dot-underscore_1' 'with space' 'with*glob'; do
    fixture_git config "remote.$remote.url" "$work/remote.git"
done
fixture_git update-ref refs/remotes/with.dot-underscore_1/main "$planted"
expect 0 'safe remote name excludes destination commits' "$(update "$tip" "$zeros")" '0 blocking' 'with.dot-underscore_1'
expect 2 'remote name with space fails closed' "$clean_input" 'unsupported remote name' 'with space'
expect 2 'remote name with glob fails closed' "$clean_input" 'unsupported remote name' 'with*glob'
expect 0 'delete ref' "$(update "$zeros" "$planted")" '' origin
expect 0 'no ref updates' '' 'no stdin ref updates' origin
expect 2 'malformed stdin line' 'refs/heads/main invalid' 'malformed stdin line' origin
expect 2 'extra stdin field' "$(update "$tip" "$planted") extra" 'malformed stdin line' origin
expect 2 'nonhex object ID' "$(update "${tip%?}x" "$planted")" 'malformed stdin line' origin
expect 2 'unknown remote object fails closed' "$(update "$tip" "$missing")" 'could not scan' origin
expect 1 'multiple refs include planted second range' "$clean_input$blocked_input" "$planted:settings.py:generic-api-key:1" origin
expect 1 'multiple refs preserve earlier finding' "$blocked_input$clean_input" "$planted:settings.py:generic-api-key:1" origin
check_exceptions
check_setup_failure
# Full-history scans cannot see uncommitted worktree files: replay this source in git too.
cp "$HERE/prepush_secret_scan_test.sh" "$repo/check-fixture.sh"
fixture_git add check-fixture.sh
fixture_git commit --quiet --message 'test source'
source_tip="$(fixture_git rev-parse HEAD)"
expect 0 'test source contains no matching literal' "$(update "$source_tip" "$tip")" '0 blocking' origin
check_hook_environment

if [ "$failed" -ne 0 ]; then
    echo "pre-push scan tests: $failed of $ran cases failed" >&2
    exit 1
fi
echo "pre-push scan tests: $ran cases passed"
