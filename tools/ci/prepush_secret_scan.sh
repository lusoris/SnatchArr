#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Scan each ref update supplied by git pre-push, using the CI scanner and exceptions.
# Usage: sh tools/ci/prepush_secret_scan.sh REMOTE (pre-push remote name or URL).
# Exit status: 0 clean/skipped, 1 blocking finding, 2 malformed input or unavailable scan.
set -eu

remote="${1:-}"
if [ -z "$remote" ]; then
    echo "pre-push scan: missing remote argument: expected remote name or URL" >&2
    exit 2
fi
named_remote=0
if timeout 60 git remote get-url -- "$remote" >/dev/null 2>&1; then
    # LOG_OPTS is space-split by Gitleaks; glob characters also broaden --remotes.
    case "$remote" in
        *[!A-Za-z0-9._-]*)
            echo "pre-push scan: unsupported remote name: use only A-Z, a-z, 0-9, dot, underscore or hyphen" >&2
            exit 2 ;;
    esac
    named_remote=1
fi

HERE="$(cd "$(dirname "$0")" && pwd)"
platform="$(uname -s)/$(uname -m)"
if [ "$platform" != Linux/x86_64 ]; then
    echo "pre-push scan: skipped on $platform: the gitleaks pin is the linux x64 archive (CI runs the scan)"
    exit 0
fi

# valid_oid VALUE: accept only complete SHA-1 or SHA-256 object IDs, including zeros.
valid_oid() {
    case "$1" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#1}" -eq 40 ] || [ "${#1}" -eq 64 ]
}

seen=0
status=0
while read -r local_ref local_oid remote_ref remote_oid extra ||
    [ -n "$local_ref$local_oid$remote_ref$remote_oid$extra" ]; do
    seen=1
    if [ -z "$local_ref" ] || [ -z "$remote_ref" ] || [ -n "$extra" ] ||
        ! valid_oid "$local_oid" || ! valid_oid "$remote_oid" ||
        [ "${#local_oid}" -ne "${#remote_oid}" ]; then
        echo "pre-push scan: malformed stdin line: expected two refs and two object IDs" >&2
        exit 2
    fi
    case "$local_oid" in *[!0]*) ;; *) continue ;; esac
    case "$remote_oid" in
        *[!0]*) range="$remote_oid..$local_oid"; set -- "$range" ;;
        *)
            if [ "$named_remote" -eq 1 ]; then
                range="$local_oid --not --remotes=$remote"
                set -- "$local_oid" --not --remotes="$remote"
            else
                range="$local_oid"; set -- "$local_oid"
            fi ;;
    esac
    # Gitleaks 8.30.1 can report a clean scan when git rejects an unknown object.
    if ! timeout 60 git rev-list --quiet "$@" >/dev/null 2>&1; then
        echo "pre-push scan: could not scan $local_ref ($range): invalid or unavailable history" >&2
        exit 2
    fi
    rc=0
    timeout 600 sh "$HERE/gitleaks_scan.sh" "$HERE/../../.cache/gitleaks" "$range" </dev/null || rc=$?
    case "$rc" in
        0) ;;
        1) status=1 ;;
        *) echo "pre-push scan: could not scan $local_ref ($range)" >&2; exit 2 ;;
    esac
done
if [ "$seen" -eq 0 ]; then
    echo "pre-push scan: no stdin ref updates; nothing to scan"
fi
exit "$status"
