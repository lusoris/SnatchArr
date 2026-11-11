#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for image_pin_check.py: each case builds a throwaway git repository with
# container image digest pins and verifies the exit status and diagnostic output.
# Digests are constructed at run time so this script's own source holds no literal pin.
# shellcheck disable=SC2016 # Variable-digest fixtures must remain literal shell text.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
check=$HERE/image_pin_check.py
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"

A=$(printf '%064d' 0 | tr 0 a)
B=$(printf '%064d' 0 | tr 0 b)
B_UPPER=$(printf '%s' "$B" | tr abcdef ABCDEF)

# track PATH LINE...: write LINE... to PATH and stage it in the current test repository.
track() {
    local file=$1
    shift
    mkdir -p "$(dirname "$file")"
    printf '%s\n' "$@" > "$file"
    git add "$file"
}

# run_case NAME WANT_RC SETUP NEEDLE...: build a fixture with SETUP and assert WANT_RC + NEEDLEs.
run_case() {
    local name=$1 want=$2 setup=$3 repo out status=0 needle
    shift 3
    repo="$work/$name"
    new_repo "$repo"
    (cd "$repo" && "$setup")
    out=$(python3 -I "$check" --root "$repo" 2>&1) || status=$?
    for needle in "$@"; do
        check_case "$name" "$want" "$status" "$needle" "$out"
    done
    echo "ok   $name"
}

# setup_aligned: mirror.gcr.io/library/postgres and bare postgres share one tag and digest.
setup_aligned() {
    track .github/workflows/ci.yml "services:" "  image: mirror.gcr.io/library/postgres:18-alpine@sha256:$A"
    track api/storetest.go "package p" "const Image = \"postgres:18-alpine@sha256:$A\""
}

# setup_drift: CI workflow mirror pin and Go test pin carry different digests for one tag.
setup_drift() {
    track .github/workflows/ci.yml "services:" "  image: mirror.gcr.io/library/postgres:18-alpine@sha256:$A"
    track api/storetest.go "package p" "const Image = \"postgres:18-alpine@sha256:$B\""
}

# setup_hub_aliases: Docker Hub host aliases and bare official name collapse to one key.
setup_hub_aliases() {
    track a.yml "image: docker.io/library/x:1.0@sha256:$A"
    track b.yml "image: index.docker.io/x:1.0@sha256:$A"
    track c.yml "image: registry-1.docker.io/x:1.0@sha256:$A"
    track d.yml "image: x:1.0@sha256:$B"
}

# setup_other_registry: ghcr.io/o/postgres is distinct from Docker Hub library/postgres.
setup_other_registry() {
    track a.yml "image: ghcr.io/o/postgres:18-alpine@sha256:$B"
    track b.yml "image: postgres:18-alpine@sha256:$A"
}

# setup_other_tag: distinct tags of the same repository may pin distinct digests.
setup_other_tag() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    track b.yml "image: postgres:18.6-alpine@sha256:$B"
}

# setup_variable_digest: variable digests (${D}, $D) are not literal pins.
setup_variable_digest() {
    track a.sh "img=postgres:18-alpine@sha256:$A" 'var1=postgres:18-alpine@sha256:${D}' 'var2=postgres:18-alpine@sha256:$D'
}

# setup_untracked: drift present only in an untracked file is ignored.
setup_untracked() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    printf '%s\n' "image: postgres:18-alpine@sha256:$B" > scratch.yml
}

# setup_port_registry: a host:port registry prefix stays distinct from Docker Hub.
setup_port_registry() {
    track a.yml "image: localhost:5000/postgres:18-alpine@sha256:$B"
    track b.yml "image: postgres:18-alpine@sha256:$A"
}

# setup_hex_boundary: 65-char hex and uppercase hex are not valid 64-lowercase-hex digests.
setup_hex_boundary() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    track b.yml "bad1: postgres:18-alpine@sha256:${B}f" "bad2: postgres:18-alpine@sha256:$B_UPPER"
}

# setup_shell_default: a pin written as a shell or compose default value (`${IMG:-...}`,
# `${IMG-...}`, `${IMG:=...}`) is a literal pin and drifts against the plain pin.
setup_shell_default() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    track compose.yaml "services:" "  db:" "    image: \${IMG:-postgres:18-alpine@sha256:$B}"
    track run.sh "x=\${IMG-postgres:18-alpine@sha256:$B}" "y=\${IMG:=postgres:18-alpine@sha256:$B}"
}

# setup_dash_boundary: a pin never starts inside a name after `-` (`My-postgres` is no pin,
# `a-postgres` is its own repository), so neither drifts against `postgres`.
setup_dash_boundary() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    track b.sh "x=My-postgres:18-alpine@sha256:$B" "y=a-postgres:18-alpine@sha256:$B"
}

# setup_records_separate: a record file (HTML comment marker) keeps its measured pin while the
# live pin moves on; records are compared only with records.
setup_records_separate() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    track docs/adr.md "<!-- image-pin-check: record -->" "| v1 | \`postgres:18-alpine@sha256:$B\` |"
}

# setup_records_drift: two record files (`#` and `//` markers) that disagree are refused.
setup_records_drift() {
    track r1.sh "#!/bin/sh" "# image-pin-check: record (verified pairs)" "img=postgres:18-alpine@sha256:$A"
    track r2.go "// image-pin-check: record" "const Image = \"postgres:18-alpine@sha256:$B\""
}

# setup_marker_mid_line: the marker counts only as a whole comment line, never inside a string
# or after code on the same line.
setup_marker_mid_line() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    track b.sh 'echo "# image-pin-check: record"' "img=postgres:18-alpine@sha256:$B  # image-pin-check: record"
}

run_case aligned 0 setup_aligned "image_pin_check: ok"
run_case drift 3 setup_drift \
    "image_pin_check: library/postgres:18-alpine is pinned to 2 digests" \
    "  sha256:${A:0:12} .github/workflows/ci.yml:2" \
    "  sha256:${B:0:12} api/storetest.go:2" \
    "move every pin of one image tag in one change; Renovate groups them (renovate.json)"
run_case hub-aliases 3 setup_hub_aliases \
    "image_pin_check: library/x:1.0 is pinned to 2 digests" \
    "  sha256:${A:0:12} a.yml:1, b.yml:1, c.yml:1" \
    "  sha256:${B:0:12} d.yml:1"
run_case other-registry 0 setup_other_registry "image_pin_check: ok"
run_case other-tag 0 setup_other_tag "image_pin_check: ok"
run_case variable-digest 0 setup_variable_digest "image_pin_check: ok"
run_case untracked 0 setup_untracked "image_pin_check: ok"
run_case port-registry 0 setup_port_registry "image_pin_check: ok"
run_case hex-boundary 0 setup_hex_boundary "image_pin_check: ok"
run_case shell-default 3 setup_shell_default \
    "image_pin_check: library/postgres:18-alpine is pinned to 2 digests" \
    "  sha256:${A:0:12} a.yml:1" \
    "  sha256:${B:0:12} compose.yaml:3, run.sh:1, run.sh:2"
run_case dash-boundary 0 setup_dash_boundary "image_pin_check: ok (2 image tags across 2 pins)"
run_case records-separate 0 setup_records_separate "image_pin_check: ok (1 image tags across 2 pins)"
run_case records-drift 3 setup_records_drift \
    "image_pin_check: library/postgres:18-alpine is pinned to 2 digests in record files" \
    "  sha256:${A:0:12} r1.sh:3" \
    "  sha256:${B:0:12} r2.go:2" \
    "a file of measured pins that no updater moves takes an \`image-pin-check: record\` comment line"
run_case marker-mid-line 3 setup_marker_mid_line \
    "image_pin_check: library/postgres:18-alpine is pinned to 2 digests" \
    "  sha256:${B:0:12} b.sh:2"

# Not a repository: git ls-files fails, and the check fails closed with status 1.
mkdir -p "$work/not-a-repo"
status=0
out=$(GIT_CEILING_DIRECTORIES=$work python3 -I "$check" --root "$work/not-a-repo" 2>&1) || status=$?
check_case not-a-repo 1 "$status" "error: git ls-files failed" "$out"
echo "ok   not-a-repo"

# assert_mode NAME MODE WANT_RC NEEDLE...: run the check on fixture NAME over the working tree
# (MODE worktree) or the git index (MODE index), bounded to 20 seconds by run_bounded.
assert_mode() {
    local name=$1 mode=$2 want=$3 out status=0 needle
    shift 3
    local -a args=(--root "$work/$name")
    if [ "$mode" = index ]; then
        args+=(--index)
    fi
    out=$(run_bounded 20 python3 -I "$check" "${args[@]}" 2>&1) || status=$?
    for needle in "$@"; do
        check_case "$name/$mode" "$want" "$status" "$needle" "$out"
    done
}

# fixture NAME SETUP: a fresh repository NAME under $work, prepared by SETUP inside it.
fixture() {
    new_repo "$work/$1"
    (cd "$work/$1" && "$2")
}

# Index snapshot drifts (a.yml staged on $B, b.yml staged on $A); the working tree has both on $B.
setup_index_drift() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    track b.yml "image: postgres:18-alpine@sha256:$A"
    git commit -q -m base
    track a.yml "image: postgres:18-alpine@sha256:$B"
    printf '%s\n' "image: postgres:18-alpine@sha256:$B" > b.yml
}

# Index snapshot stays aligned on $A; b.yml holds unstaged drift on $B.
setup_index_aligned() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    track b.yml "image: postgres:18-alpine@sha256:$A"
    git commit -q -m base
    printf '%s\n' "image: postgres:18-alpine@sha256:$B" > b.yml
}

# Sparse checkout: b.yml is staged on $B with the skip-worktree bit and absent from the working
# tree; the index snapshot must still hold it.
setup_index_skip_worktree() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    track b.yml "image: postgres:18-alpine@sha256:$B"
    git update-index --skip-worktree b.yml
    rm b.yml
}

# OCI path components with repeated separators (`__`, `--`), each pinned to two digests.
setup_repeated_separators() {
    track a.yml "image: ghcr.io/o/foo__bar:1@sha256:$A"
    track b.yml "image: ghcr.io/o/foo__bar:1@sha256:$B"
    track c.yml "image: ghcr.io/o/foo--bar:1@sha256:$A"
    track d.yml "image: ghcr.io/o/foo--bar:1@sha256:$B"
}

# A tracked symlink to an endless device is skipped, never read.
setup_symlink_dev_zero() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    ln -s /dev/zero z.yml
    git add z.yml
}

# A tracked symlink to a file outside the repository is not followed: its conflicting pin is no
# repository pin.
setup_symlink_outside() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    printf '%s\n' "image: postgres:18-alpine@sha256:$B" > "$work/outside.yml"
    ln -s "$work/outside.yml" z.yml
    git add z.yml
}

# Tracked text one byte past MAX_BYTES (4 MiB), no NUL byte: the check fails closed.
setup_oversized_text() {
    python3 -I -c 'from pathlib import Path; Path("big.txt").write_bytes(b"a\n" * 2097152 + b"a")'
    git add big.txt
}

# The `":a" * n + "/"` line that made the old host lookahead quadratic, a `"a-" * n` line that
# a match starting after `-` would make quadratic, and a `"${a:-" * n` line of shell defaults,
# plus one pin.
setup_linear_time() {
    track a.yml "image: postgres:18-alpine@sha256:$A"
    python3 -I -c 'from pathlib import Path
Path("long.txt").write_text(":a" * 500000 + "/\n" + "a-" * 500000 + "\n" + "${a:-" * 200000 + "\n", encoding="utf-8")'
    git add long.txt
}

fixture index-drift setup_index_drift
assert_mode index-drift index 3 \
    "image_pin_check: library/postgres:18-alpine is pinned to 2 digests" \
    "  sha256:${A:0:12} b.yml:1" \
    "  sha256:${B:0:12} a.yml:1"
assert_mode index-drift worktree 0 "image_pin_check: ok"
echo "ok   index-drift"

fixture index-aligned setup_index_aligned
assert_mode index-aligned index 0 "image_pin_check: ok"
assert_mode index-aligned worktree 3 \
    "image_pin_check: library/postgres:18-alpine is pinned to 2 digests" \
    "  sha256:${A:0:12} a.yml:1" \
    "  sha256:${B:0:12} b.yml:1"
echo "ok   index-aligned"

fixture index-skip-worktree setup_index_skip_worktree
assert_mode index-skip-worktree index 3 \
    "image_pin_check: library/postgres:18-alpine is pinned to 2 digests" \
    "  sha256:${B:0:12} b.yml:1"
assert_mode index-skip-worktree worktree 0 "image_pin_check: ok"
echo "ok   index-skip-worktree"

fixture repeated-separators setup_repeated_separators
assert_mode repeated-separators worktree 3 \
    "image_pin_check: ghcr.io/o/foo__bar:1 is pinned to 2 digests" \
    "image_pin_check: ghcr.io/o/foo--bar:1 is pinned to 2 digests"
echo "ok   repeated-separators"

fixture symlink-dev-zero setup_symlink_dev_zero
assert_mode symlink-dev-zero worktree 0 "image_pin_check: ok (1 image tags across 1 pins)"
assert_mode symlink-dev-zero index 0 "image_pin_check: ok (1 image tags across 1 pins)"
echo "ok   symlink-dev-zero"

fixture symlink-outside setup_symlink_outside
assert_mode symlink-outside worktree 0 "image_pin_check: ok (1 image tags across 1 pins)"
assert_mode symlink-outside index 0 "image_pin_check: ok (1 image tags across 1 pins)"
echo "ok   symlink-outside"

fixture oversized-text setup_oversized_text
assert_mode oversized-text worktree 1 "error: big.txt: more than 4194304 bytes of text"
echo "ok   oversized-text"

fixture linear-time setup_linear_time
assert_mode linear-time worktree 0 "image_pin_check: ok (1 image tags across 1 pins)"
echo "ok   linear-time"

# assert_bounded WANT NEEDLE SECONDS CMD...: `run_bounded SECONDS CMD...` must exit WANT with
# NEEDLE in its captured output. run_bounded (test_lib.sh) passes the exit status through, maps
# a fatal signal N to 128+N, ends an overrunning command with 124 and answers a bad call with
# 125, 126 or 127, so the linear-time bound holds without GNU timeout.
assert_bounded() {
    local want=$1 needle=$2 status=0 out
    shift 2
    out=$(run_bounded "$@" 2>&1) || status=$?
    check_case "run-bounded/$want $*" "$want" "$status" "$needle" "$out"
}
assert_bounded 0 '' 5 true
assert_bounded 3 '' 5 sh -c 'exit 3'
assert_bounded 143 '' 5 sh -c 'kill -TERM $$'
assert_bounded 124 '' 1 sleep 10
# Boundary: the smallest positive bound is a valid call; 0 and below are usage errors.
assert_bounded 124 '' 0.001 sleep 5
assert_bounded 125 'usage: run_bounded.py' 0 true
assert_bounded 125 'usage: run_bounded.py' -1 true
assert_bounded 125 'usage: run_bounded.py' nan true
assert_bounded 125 'usage: run_bounded.py' inf true
assert_bounded 125 'usage: run_bounded.py' x true
assert_bounded 125 'usage: run_bounded.py' 5
printf 'echo no\n' > "$work/not-executable"
assert_bounded 126 'not-executable: Permission denied' 5 "$work/not-executable"
assert_bounded 127 'no-such-command: No such file or directory' 5 "$work/no-such-command"

# assert_group_ended NAME BOUND WANT SCRIPT: runs `sh -c SCRIPT _ PIDFILE` under run_bounded
# BOUND with stdout captured. SCRIPT starts a `sleep 15` descendant that holds that stdout and
# writes its pid to PIDFILE. Wants exit WANT within 5 seconds and the descendant gone, which
# holds only when run_bounded ends the whole process group.
assert_group_ended() {
    local name=$1 bound=$2 want=$3 script=$4 pidfile="$work/$1.pid" status=0 start elapsed out pid
    start=$(date +%s)
    out=$(run_bounded "$bound" sh -c "$script" _ "$pidfile") || status=$?
    elapsed=$(($(date +%s) - start))
    check_case "run-bounded/$name" "$want" "$status" "" "$out"
    if [ "$elapsed" -gt 5 ]; then
        printf 'FAIL run-bounded/%s: returned after %ss, bound %ss\n' "$name" "$elapsed" "$bound" >&2
        exit 1
    fi
    if [ ! -s "$pidfile" ]; then
        printf 'FAIL run-bounded/%s: no descendant pid in %s\n' "$name" "$pidfile" >&2
        exit 1
    fi
    pid=$(cat "$pidfile")
    # A killed descendant stays a zombie until its new parent reaps it: allow up to 2 seconds.
    for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
        kill -0 "$pid" 2>/dev/null || return 0
        sleep 0.1
    done
    printf 'FAIL run-bounded/%s: descendant %s still running\n' "$name" "$pid" >&2
    exit 1
}
assert_group_ended timeout-descendant 1 124 'sleep 15 & echo $! > "$1"; wait'
assert_group_ended term-forwarded 10 143 'sleep 15 & echo $! > "$1"; kill -TERM $PPID; wait'
echo "ok   run-bounded"
