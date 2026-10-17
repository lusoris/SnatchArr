#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Asserts that the built container images execute on linux/arm64 under QEMU
# without runtime or architecture defects (no "exec format error", no exit 126/127/139).
# For api: executes with no config and verifies the binary's own config error (exit 1,
# stderr carries one of api_config_errors below).
# For worker: executes with no config and verifies the binary's startup log
# ("snatch-worker starting").
# CHECK_ARCH=amd64 runs the same checks on amd64 images: CI's image smoke does, so a changed
# startup error fails a pull request instead of the release.
set -euo pipefail

arch=${CHECK_ARCH:-arm64}
case "$arch" in
    arm64) machine=" b7 00 " ;;
    amd64) machine=" 3e 00 " ;;
    *) echo "arm64-check: CHECK_ARCH=$arch, want arm64 or amd64" >&2; exit 2 ;;
esac

# The config errors the API stops on when it starts without configuration. Which one comes
# first depends on the order fx builds the graph: 0.1.x stopped on the worker token, 0.2.0
# on the database DSN. Anything else, a panic included, is refused.
api_config_errors=(
    "config: required secret is empty: snatcharr.worker.token"
    "db/pgx: DSN is required"
)

# names_config_error reports whether its argument carries one of api_config_errors.
names_config_error() {
    local want
    for want in "${api_config_errors[@]}"; do
        case "$1" in
            *"$want"*) return 0 ;;
        esac
    done
    return 1
}

fail() {
    echo "arm64-check: assertion failed: $*" >&2
    exit 1
}

# drop NAME: remove a container if it still exists (--rm containers may already be gone).
drop() {
    if docker inspect "$1" >/dev/null 2>&1 && ! docker rm -f "$1" >/dev/null 2>&1; then
        echo "arm64-check: could not remove container $1" >&2
    fi
}

# Both Dockerfiles cross-compile on $BUILDPLATFORM, so an image labelled arm64 can carry an
# amd64 binary that the amd64 runner executes natively. Read the entrypoint's ELF header
# (magic, then e_machine at offset 18: b7 00 = aarch64, 3e 00 = x86-64) instead of trusting
# the label.
check_elf() {
    local role=$1 image=$2 entry cname dir header
    entry=$(docker inspect -f '{{index .Config.Entrypoint 0}}' "$image" 2>/dev/null) || fail "cannot read $role image $image entrypoint"
    cname="smoke-$role-elf-$$"
    dir=$(mktemp -d)
    docker create --name "$cname" --platform "linux/$arch" "$image" >/dev/null 2>&1 || fail "cannot create $role container from $image"
    if ! docker cp "$cname:$entry" "$dir/entry" >/dev/null 2>&1; then
        drop "$cname"
        rm -rf "$dir"
        fail "cannot copy $role entrypoint $entry out of $image"
    fi
    drop "$cname"
    header=$(od -An -tx1 -N20 "$dir/entry" | tr -s ' \n' ' ')
    rm -rf "$dir"
    case "$header" in
        " 7f 45 4c 46 "*"$machine") ;;
        *) fail "$role image $image entrypoint $entry is not an $arch ELF binary (header:$header)" ;;
    esac
}

check_api() {
    local image=$1 got out rc=0
    got=$(docker inspect -f '{{.Architecture}}' "$image" 2>/dev/null) || fail "cannot inspect api image $image"
    [ "$got" = "$arch" ] || fail "api image $image architecture is '$got', want $arch"
    check_elf api "$image"

    out=$(docker run --rm --platform "linux/$arch" "$image" 2>&1) || rc=$?
    case "$rc" in
        126|127|139) fail "api image $image failed to execute (exit $rc): $out" ;;
        1) ;;
        *) fail "api image $image exited with $rc, want 1 (config error): $out" ;;
    esac
    case "$out" in
        *"exec format error"*) fail "api image $image execution error: exec format error" ;;
    esac
    names_config_error "$out" || fail "api image $image stderr does not name missing config: $out"
    echo "arm64-check: api image $image executed cleanly (config error exit 1 verified)"
}

check_worker() {
    local image=$1 got out cname tries=0 found=0
    got=$(docker inspect -f '{{.Architecture}}' "$image" 2>/dev/null) || fail "cannot inspect worker image $image"
    [ "$got" = "$arch" ] || fail "worker image $image architecture is '$got', want $arch"
    check_elf worker "$image"

    cname="smoke-worker-$arch-$$"
    docker run -d --rm --name "$cname" --platform "linux/$arch" "$image" >/dev/null 2>&1 || fail "failed to start worker container"
    cleanup() {
        drop "$cname"
    }
    trap cleanup EXIT

    while [ "$tries" -lt 20 ]; do
        # A failed read means the container is gone; $out then carries docker's error.
        if ! out=$(docker logs "$cname" 2>&1); then
            break
        fi
        if echo "$out" | grep -q "snatch-worker starting"; then
            found=1
            break
        fi
        case "$out" in
            *"exec format error"*) break ;;
        esac
        tries=$((tries + 1))
        sleep 0.5
    done
    drop "$cname"
    trap - EXIT

    case "$out" in
        *"exec format error"*) fail "worker image $image execution error: exec format error" ;;
    esac
    [ "$found" -eq 1 ] || fail "worker image $image did not log startup message: $out"
    echo "arm64-check: worker image $image executed cleanly (startup message verified)"
}

if [ $# -eq 1 ]; then
    fail "usage: ${0##*/} <api-image> <worker-image> OR ${0##*/} api <api-image> OR ${0##*/} worker <worker-image>"
elif [ $# -eq 2 ] && [ "$1" = "match" ]; then
    # match TEXT: exit 0 when TEXT names an expected API config error (arm64-check_test.sh).
    names_config_error "$2"
elif [ $# -eq 2 ] && [ "$1" = "api" ]; then
    check_api "$2"
elif [ $# -eq 2 ] && [ "$1" = "worker" ]; then
    check_worker "$2"
elif [ $# -eq 2 ]; then
    check_api "$1"
    check_worker "$2"
else
    fail "usage: ${0##*/} <api-image> <worker-image>"
fi
