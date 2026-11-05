#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Unit tests for download_retry_check.py; fixtures never execute curl.
# shellcheck disable=SC2016 # Fixture variables must remain literal shell source.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
check=$HERE/download_retry_check.py
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"

# expect NAME WANT_STATUS NEEDLE SETUP: plant a fixture and assert status plus output.
expect() {
    local name=$1 want=$2 needle=$3 setup=$4 repo out status=0
    repo="$work/$name"
    new_repo "$repo"
    (cd "$repo" && "$setup")
    out=$(cd "$repo" && python3 -I "$check" 2>&1) || status=$?
    check_case "$name" "$want" "$status" "$needle" "$out"
    echo "ok   $name"
}

# setup_missing: tracked HTTPS file download without retry, on physical line 2.
setup_missing() {
    printf '%s\n' '#!/bin/sh' 'curl --output archive.tar.gz https://example.invalid/archive' > download.sh
    git add download.sh
}

# setup_continued: retry on second physical line protects a variable-URL download.
setup_continued() {
    printf '%s\\\n%s\n' 'curl --output "$archive" ' '  --retry 5 "$url"' > download.sh
    git add download.sh
}

# setup_poll: /dev/null output is a health poll, not a file download.
setup_poll() {
    printf '%s\n' 'curl -o /dev/null "$url"' > poll.sh
    git add poll.sh
}

# setup_remote: remote-name output is a download even without a local filename.
setup_remote() {
    printf '%s\n' 'curl -O https://example.invalid/archive' > remote.sh
    git add remote.sh
}

# setup_short: short file-output option with a variable URL still requires retry.
setup_short() {
    printf '%s\n' 'curl -o "$archive" "$url"' > short.sh
    git add short.sh
}

# setup_remote_long: long remote-name option also requires retry.
setup_remote_long() {
    printf '%s\n' 'curl --remote-name "$url"' > remote.sh
    git add remote.sh
}

# setup_retry_all_errors: retry modifiers alone do not enable retries.
setup_retry_all_errors() {
    printf '%s\n' 'curl --retry-all-errors -O "$url"' > modifier.sh
    git add modifier.sh
}

# setup_separate: a later command's retry cannot protect an earlier download.
setup_separate() {
    printf '%s\n' 'curl -O "$url"; curl --retry 5 "$other"' > separate.sh
    git add separate.sh
}

# setup_untracked: an untracked download must not enter the scan.
setup_untracked() {
    printf '%s\n' 'curl -o archive https://example.invalid/archive' > untracked.sh
}

# setup_clean: tracked script without downloads.
setup_clean() {
    printf '%s\n' '#!/bin/sh' 'echo ready' > clean.sh
    git add clean.sh
}

# setup_capture: response captures are not standalone file downloads.
setup_capture() {
    printf '%s\n' 'body=$(curl "$url")' 'status=$(curl -o "$page" -w "%{http_code}" "$url")' > capture.sh
    git add capture.sh
}

# setup_testdata: planted downloads below tools/**/testdata/** stay excluded.
setup_testdata() {
    mkdir -p tools/ci/testdata/nested
    printf '%s\n' 'curl -O "$url"' > tools/ci/testdata/nested/fixture.sh
    git add tools/ci/testdata
}

# setup_self: quoted fixtures in this test script must not become executable commands.
setup_self() {
    cp "$HERE/download_retry_check_test.sh" self.sh
    git add self.sh
}

# setup_read_error: a missing tracked file must fail the check, never silently pass.
setup_read_error() {
    setup_clean
    rm clean.sh
}

expect missing 1 'download.sh:2: curl download without --retry' setup_missing
expect continued 0 'download_retry_check: ok (1 downloads)' setup_continued
expect poll 0 'download_retry_check: ok (0 downloads)' setup_poll
expect remote 1 'remote.sh:1: curl download without --retry' setup_remote
expect short 1 'short.sh:1: curl download without --retry' setup_short
expect remote-long 1 'remote.sh:1: curl download without --retry' setup_remote_long
expect retry-all-errors 1 'modifier.sh:1: curl download without --retry' setup_retry_all_errors
expect separate 1 'separate.sh:1: curl download without --retry' setup_separate
expect untracked 0 'download_retry_check: ok (0 downloads)' setup_untracked
expect clean 0 'download_retry_check: ok (0 downloads)' setup_clean
expect capture 0 'download_retry_check: ok (0 downloads)' setup_capture
expect testdata 0 'download_retry_check: ok (0 downloads)' setup_testdata
expect self 0 'download_retry_check: ok (0 downloads)' setup_self
expect read-error 2 'download_retry_check: cannot run:' setup_read_error

echo "download_retry_check: all cases passed"
