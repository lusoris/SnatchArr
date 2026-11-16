#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Cases for planted.sh: refusal and timeout handling, labelled and traceback reasons, and Go
# test, build, setup, slash-label, and gomod reasons that must survive container-log noise.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0

# The script under test: PLANT=bad fails with "refused bad", PLANT=quiet passes, PLANT=hang and
# HANG_CLEAN=1 sleep past the bound, PLANT=labelled, spaced, traceback, gotest, gobuild, gosetup,
# slashlabel, and gomod print reasons followed by container noise, and PLANT=hanglabelled prints a
# reason before hanging. The clean run passes.
cat >"$work/check.sh" <<'SCRIPT'
case "${PLANT:-}" in
bad) echo "refused bad" >&2; exit 1 ;;
quiet) exit 0 ;;
hang) sleep 120 ;;
hanglabelled) echo "check: the hang reason" >&2; for i in $(seq 30); do echo "api-1 | noise $i"; done; sleep 120 ;;
labelled) echo "check: the real reason" >&2; for i in $(seq 30); do echo "api-1 | noise $i"; done; exit 1 ;;
spaced) echo "pushtitle check: the spaced reason" >&2; for i in $(seq 30); do echo "api-1 | noise $i"; done; exit 1 ;;
gotest)
    printf '%s\n' '=== RUN   TestLive' '    live_test.go:61: the go test reason' '--- FAIL: TestLive (0.01s)' '    --- FAIL: TestLive/sub (0.01s)' 'FAIL' >&2
    printf 'FAIL\texample.com/m/a\t0.001s\n' >&2
    ;;
gobuild)
    printf '%s\n' '# example.com/m/b [example.com/m/b.test]' 'b/b_test.go:5:28: the compile reason' >&2
    printf 'FAIL\texample.com/m/b [build failed]\n' >&2
    ;;
gosetup)
    printf '%s\n' '# example.com/setupshape' 'setup_test.go:2:8: the setup reason' >&2
    printf 'FAIL\texample.com/setupshape [setup failed]\nFAIL\n' >&2
    ;;
slashlabel) echo 'testutil/pg: start shared container: the slash reason' >&2 ;;
gomod)
    printf '%s\n' 'go: errors parsing go.mod:' 'go.mod:3: the go.mod reason' '/work/m/go.mod:5: the absolute reason' 'go: errors parsing go.work:' '../go.work:3: the go.work reason' 'vendor/notgo.mod:4: the lookalike line' >&2
    ;;
traceback)
    printf '%s\n' 'Traceback (most recent call last):' '  File "check.py", line 9, in <module>' '    main()' 'TimeoutError: the traceback reason' >&2
    for i in $(seq 30); do echo "api-1 | noise $i"; done
    exit 1
    ;;
esac
case "${PLANT:-}" in
gotest|gobuild|gosetup|slashlabel|gomod)
    for i in $(seq 30); do
        if [ "$i" -eq 1 ]; then
            echo '2026/11/16 15:52:47 Creating container for image x: y'
        else
            echo "api-1 | noise $i"
        fi
    done
    exit 1
    ;;
esac
[ "${HANG_CLEAN:-0}" = 1 ] && sleep 120
exit 0
SCRIPT

# run_planted [ENV=VALUE...] -- CASE...: run planted.sh and retain its output, status, and time.
run_planted() {
    local status=0 start
    local envs=()
    while [ "$1" != -- ]; do envs+=("$1"); shift; done
    shift
    start=$SECONDS
    RUN_OUTPUT=$(env PLANTED_SECONDS=2 "${envs[@]}" bash "$here/planted.sh" PLANT "$work/check.sh" "$@" 2>&1) || status=$?
    RUN_STATUS=$status
    RUN_TOOK=$((SECONDS - start))
}

# expect NAME WANT_STATUS WANT_TEXT [ENV=VALUE...] -- CASE...: assert refusal/pass and output text.
expect() {
    local name=$1 want=$2 text=$3
    shift 3
    run_planted "$@"
    if [ "$want" = 0 ] && [ "$RUN_STATUS" != 0 ]; then
        echo "FAIL $name: status $RUN_STATUS, want 0: $RUN_OUTPUT" >&2
        failed=1
    elif [ "$want" != 0 ] && [ "$RUN_STATUS" = 0 ]; then
        echo "FAIL $name: passed, want a refusal" >&2
        failed=1
    elif [[ $RUN_OUTPUT != *"$text"* ]]; then
        echo "FAIL $name: output lacks '$text': $RUN_OUTPUT" >&2
        failed=1
    elif [ "$RUN_TOOK" -gt 30 ]; then
        echo "FAIL $name: took ${RUN_TOOK}s, past the 2 s bound plus the kill grace" >&2
        failed=1
    else
        echo "ok   $name"
    fi
}

# expect_absent NAME TEXT [ENV=VALUE...] -- CASE...: require refusal with TEXT absent from output.
expect_absent() {
    local name=$1 text=$2
    shift 2
    run_planted "$@"
    if [ "$RUN_STATUS" = 0 ]; then
        echo "FAIL $name: passed, want a refusal" >&2
        failed=1
    elif [[ $RUN_OUTPUT == *"$text"* ]]; then
        echo "FAIL $name: output contains '$text': $RUN_OUTPUT" >&2
        failed=1
    elif [ "$RUN_TOOK" -gt 30 ]; then
        echo "FAIL $name: took ${RUN_TOOK}s, past the 2 s bound plus the kill grace" >&2
        failed=1
    else
        echo "ok   $name"
    fi
}

expect "refused plant, clean pass" 0 "planted case bad refused: refused bad" -- "bad:refused bad"
expect "a plant that passes is refused" 1 "case quiet passed" -- "quiet:anything"
expect "a wrong message is refused" 1 "failed without 'other text'" -- "bad:other text"
expect "a hung planted case ends at the bound" 1 "case hang of $work/check.sh did not end within 2s" -- "hang:never"
expect "a hung clean run ends at the bound" 1 "clean run of $work/check.sh did not end within 2s" HANG_CLEAN=1 -- "bad:refused bad"
# A script that reports through compose_lib.sh fail() prints "<label>: <reason>"; a wrong
# message must still show that reason, though 30 container log lines push it out of the tail.
expect "a label reason line survives the log tail" 1 "check: the real reason" -- "labelled:other text"
# check.py exits with "pushtitle check: <reason>" (a space in the label) or, on an error it does
# not catch, a Python traceback; both must show though the log tail pushes them out (main
# 4b94304b CI run 43294646878 hid the reason of a failed v3 idonly run).
expect "a spaced label reason line survives the log tail" 1 "pushtitle check: the spaced reason" -- "spaced:other text"
expect "a traceback survives the log tail" 1 "TimeoutError: the traceback reason" -- "traceback:other text"
expect "a traceback keeps its frames" 1 'File "check.py", line 9' -- "traceback:other text"
expect "a traceback keeps its header" 1 "Traceback (most recent call last):" -- "traceback:other text"
# Go test output uses indented failure and test-log lines plus tab-separated package summaries.
expect "Go test log reason survives the log tail" 1 "live_test.go:61: the go test reason" -- "gotest:other text"
expect "Go subtest failure survives the log tail" 1 "--- FAIL: TestLive/sub" -- "gotest:other text"
expect "Go package failure survives the log tail" 1 $'FAIL\texample.com/m/a' -- "gotest:other text"
expect "Go compile reason survives the log tail" 1 "b/b_test.go:5:28: the compile reason" -- "gobuild:other text"
expect "Go build failure survives the log tail" 1 "[build failed]" -- "gobuild:other text"
expect "Go setup reason survives the log tail" 1 "the setup reason" -- "gosetup:other text"
expect "Go setup failure survives the log tail" 1 "[setup failed]" -- "gosetup:other text"
expect "slash label reason survives the log tail" 1 "testutil/pg: start shared container: the slash reason" -- "slashlabel:other text"
# Go module and workspace parse reasons include a module-file path and line number.
expect "Go module reason survives the log tail" 1 "go.mod:3: the go.mod reason" -- "gomod:other text"
expect "absolute Go module reason survives the log tail" 1 "/work/m/go.mod:5: the absolute reason" -- "gomod:other text"
expect "Go workspace reason survives the log tail" 1 "../go.work:3: the go.work reason" -- "gomod:other text"
expect_absent "Go module lookalike stays filtered" "notgo.mod:4" -- "gomod:other text"
# Container noise stays filtered from the reason prelude and falls outside the final log tail.
expect_absent "label noise stays outside the log tail" "api-1 | noise 5" -- "labelled:other text"
expect_absent "Go test noise stays outside the log tail" "api-1 | noise 5" -- "gotest:other text"
expect_absent "testcontainers setup log stays filtered" "Creating container for image x" -- "gotest:other text"
# A case that hangs after its reason line ends at the bound and still shows that reason.
expect "a hung case keeps its reason line" 1 "check: the hang reason" -- "hanglabelled:never"
expect "no planted case is refused" 1 "no planted case" --
exit "$failed"
