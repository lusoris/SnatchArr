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

# expect NAME WANT_STATUS NEEDLE SETUP [SOURCE]: plant a fixture and check output.
expect() {
    local name=$1 want=$2 needle=$3 setup=$4 repo out status=0
    repo="$work/$name"
    new_repo "$repo"
    (cd "$repo" && "$setup" "${5-}")
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

# setup_command SOURCE: track shell text without executing the fixture.
setup_command() {
    printf '%s\n' "$1" > download.sh
    git add download.sh
}

# expect_command NAME STATUS SOURCE [DOWNLOADS]: check one shell command.
expect_command() {
    local name=$1 want=$2 source=$3 downloads=${4:-1} needle
    if [[ $want == 1 ]]; then
        needle='download.sh:1: curl download without --retry'
    elif [[ $want == 2 ]]; then
        needle='download_retry_check: cannot run:'
    else
        needle="download_retry_check: ok ($downloads downloads)"
    fi
    expect "$name" "$want" "$needle" setup_command "$source"
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

expect_command retry-zero 1 'curl --retry 0 -o f https://x'
expect_command retry-equals-zero 1 'curl --retry=0 -o f https://x'
expect_command retry-equals-positive 0 'curl --retry=3 -o f https://x'
expect_command retry-one 0 'curl --retry 1 -o f https://x'
expect_command retry-negative 1 'curl --retry -1 -o f https://x'
expect_command retry-invalid 1 'curl --retry invalid -o f https://x'
expect_command retry-equals-invalid 1 'curl --retry=invalid -o f https://x'
expect_command retry-fraction 1 'curl --retry=1.5 -o f https://x'
expect_command retry-underscores 1 'curl --retry=1_0 -o f https://x'
expect_command retry-explicit-plus 0 'curl --retry=+3 -o f https://x'
expect_command retry-missing-value 1 'curl -o f https://x --retry'
expect_command retry-empty-value 1 'curl --retry= -o f https://x'
expect_command retry-last-zero 1 'curl --retry 3 --retry 0 -o f https://x'

expect_command wrapper-env 1 'env curl -o f https://x'
expect_command wrapper-assignment 1 'FOO=1 curl -o f https://x'
expect_command wrapper-env-assignment 1 'env VAR=1 curl -o f https://x'
expect_command wrapper-env-flag 1 'env -i curl -o f https://x'
expect_command wrapper-sudo-flag 1 'sudo -E curl -o f https://x'
expect_command wrapper-sudo-user 1 'sudo -u root curl -o f https://x'
expect_command wrapper-nice-value 1 'nice -n 10 curl -o f https://x'
expect_command not-a-wrapper 0 'echo curl -o f https://x' 0
expect_command wrapper-env-retry 0 'env curl --retry 3 -o f https://x'
expect_command wrapper-nested 1 'FOO=1 env -i BAR=2 command curl -o f https://x'
for wrapper in sudo exec time nice command; do
    expect_command "wrapper-$wrapper" 1 "$wrapper curl -o f https://x"
done

expect_command quoted-fragment 1 'curl -o f "https://x/a#b"'
expect_command comment-retry 1 'curl -o f https://x # --retry 3'
expect_command fragment-retry 0 'curl --retry 3 -o f https://x/a#b'
expect_command fragment-before-output 1 'curl https://x/a#b -o f'
expect_command fragment-before-retry 0 'curl -o f https://x/a#b --retry 3'
expect_command comment-unclosed-quote 1 'curl -o f https://x # " --retry 3'
expect_command malformed-quote 2 'curl -o f "https://x'

expect_command redirect-attached 1 'curl https://x >f'
expect_command redirect-separate 1 'curl https://x > f'
expect_command redirect-append 1 'curl https://x >>f'
expect_command redirect-fd-one 1 'curl https://x 1>f'
expect_command redirect-fd-one-append 1 'curl https://x 1>>f'
expect_command redirect-both 1 'curl https://x &>f'
expect_command redirect-both-append 1 'curl https://x &>>f'
expect_command redirect-append-separate 1 'curl https://x >> f'
expect_command redirect-fd-one-separate 1 'curl https://x 1> f'
expect_command redirect-fd-one-append-separate 1 'curl https://x 1>> f'
expect_command redirect-both-separate 1 'curl https://x &> f'
expect_command redirect-both-append-separate 1 'curl https://x &>> f'
expect_command redirect-null 0 'curl https://x > /dev/null' 0
expect_command redirect-stderr 0 'curl https://x 2>f' 0
expect_command redirect-stderr-append 0 'curl https://x 2>>f' 0
expect_command redirect-background 0 'curl https://x & > f' 0
expect_command redirect-retry 0 'curl --retry 3 https://x > f'
expect_command redirect-dash 1 'curl https://x > -'
expect_command output-dash 0 'curl https://x -o -' 0
expect_command redirect-adjacent-url 1 'curl https://x>f'
expect_command redirect-spaced-fd 1 'curl https://x 2 > f'
expect_command redirect-after-retried-command 1 'curl --retry 3 https://x; curl https://y > f'

# Quoted and escaped hashes remain words; only shell comments end the line.
expect_command hash-single-quoted 1 "echo '# heading'; curl -o archive https://x/f"
expect_command hash-double-quoted 1 'echo "# heading"; curl -o archive https://x/f'
expect_command hash-escaped 1 'echo \#x; curl -o archive https://x/f'
expect_command hash-empty-quote-prefix 1 'echo ""#x; curl -o archive https://x/f'
expect_command comment-after-separator 0 'echo ready;# curl -o archive https://x/f' 0

# Remote-name defaults yield to explicit per-URL output and the disabling flag.
expect_command remote-name-all 1 'curl --remote-name-all https://x/f'
expect_command remote-name-all-retry 0 'curl --remote-name-all --retry 3 https://x/f'
expect_command remote-name-all-stdout 0 'curl --remote-name-all -o - https://x' 0
expect_command remote-name-all-output-stdout 0 'curl --remote-name-all --output - https://x' 0
expect_command remote-name-all-disabled 0 'curl --remote-name-all --no-remote-name-all https://x' 0
expect_command remote-name-all-null 0 'curl --remote-name-all -o /dev/null https://x' 0
expect_command remote-name-all-mixed 1 'curl --remote-name-all -o - https://x https://y'

# Each curl operation resets retry and output options at either next separator.
expect_command next-missing-retry 1 'curl --retry 3 -o first https://x --next -o second https://y'
expect_command next-both-retry 0 'curl --retry 3 -o first https://x --next --retry 2 -o second https://y' 2
expect_command next-poll 0 'curl --retry 3 -o first https://x --next -o /dev/null https://y'
expect_command next-short-missing-retry 1 'curl --retry 3 -o first https://x -: -o second https://y'
expect_command next-short-both-retry 0 'curl --retry 3 -o first https://x -: --retry 2 -o second https://y' 2
expect_command next-reset-remote-name-all 0 'curl --remote-name-all --retry 3 https://x --next https://y'

# Command paths identify curl by basename without matching a longer command name.
expect_command curl-absolute-path 1 '/usr/bin/curl -o archive https://x/a'
expect_command curl-absolute-retry 0 '/usr/local/bin/curl --retry 2 -o archive https://x/a'
expect_command curl-relative-path 1 './curl -o archive https://x/a'
expect_command curl-variable-path 1 '"$CURL_DIR"/curl -o archive https://x/a'
expect_command curl-longer-name 0 'curlx -o f https://x' 0

# Short clusters consume output/config values; uppercase O remains a plain flag.
expect_command cluster-output-separate 1 'curl -fsSLo archive https://x/a'
expect_command cluster-output-attached 1 'curl -fsSLoarchive https://x/a'
expect_command cluster-remote-name 1 'curl -fsSLO https://x/a'
expect_command cluster-stdout 0 'curl -fsSLo - https://x/a' 0
expect_command cluster-output-retry 0 'curl -fsSLo f --retry 3 https://x/a'
expect_command cluster-output-missing 2 'curl https://x/a -fsSLo'
expect_command cluster-config-attached 0 'curl -fsSKoptions https://x/a' 0
expect_command cluster-config-separate 0 'curl -fsSK options https://x/a' 0

# Value-taking short options end clusters and consume their entire argument.
expect_command user-attached 0 'curl -uroot https://x' 0
expect_command header-attached-null 0 'curl -H"X: foo" -o /dev/null https://x' 0
expect_command request-attached 0 'curl -sXGETo f https://x' 0
expect_command referer-separate-output 1 'curl -e https://ref -o out https://x'
expect_command referer-separate-retry 0 'curl --retry 3 -e https://ref -o out https://x'
expect_command user-separate 0 'curl -u o https://x' 0
expect_command cluster-silent-output 1 'curl -sSo f https://x'
expect_command cluster-silent-stdout 0 'curl -fsSLo - https://x' 0
expect_command referer-only 0 'curl -e https://ref -o out' 0
expect_command referer-before-stdout 0 'curl -e https://ref -o - https://x -o out' 0

echo "download_retry_check: all cases passed"
