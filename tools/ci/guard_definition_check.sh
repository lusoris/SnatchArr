#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Commit-msg entry of the workflow-refs guard check. For a squash message whose subject ends in
# (#N), fetches refs/pull/N/merge from origin into a temporary bare repository and runs
# guard_definition_check.py on that merge commit and its first parent (the base tip).
# Platform (HISS-21): runs on Linux in CI and on maintainer hosts and on macOS; requires bash,
# git, python3 and PyYAML for PYTHON_BIN.
#
#   tools/ci/guard_definition_check.sh MSGFILE
#
# The subject is the first line of MSGFILE that is neither empty nor a '#' comment; one without
# (#N) passes silently. GUARD_DEFINITION_FETCH_SECONDS (default 60, whole seconds from 1 to
# 9999) bounds the fetch; every other git call gets 10 s and the comparison 60 s. PYTHON_BIN
# (default python3) runs guard_definition_check.py; GUARD_DEFINITION_ACK passes through to it.
# The GIT_* variables git exports to hooks are dropped after origin is read, so nothing is
# fetched into the calling repository.
# Exit status: 0 no (#N), guard unchanged or change acknowledged; 1 change refused, no origin,
# fetch or comparison failed, or a deadline passed; 2 usage, unreadable MSGFILE, a bad
# GUARD_DEFINITION_FETCH_SECONDS or PYTHON_BIN, or PyYAML missing.
set -euo pipefail

if [ $# -ne 1 ] || [ -z "$1" ]; then
  echo "usage: ${0##*/} MSGFILE" >&2
  exit 2
fi
MSGFILE="$1"
if ! [ -f "$MSGFILE" ] || ! [ -r "$MSGFILE" ]; then
  echo "guard-definition: cannot read $MSGFILE" >&2
  exit 2
fi
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PR_SUBJECT_RE='\(#([1-9][0-9]{0,6})\)$'

# bounded SECONDS CMD...: run CMD under tools/ci/run_bounded.py, which exits 124 at the deadline.
bounded() {
  python3 -I "$HERE/run_bounded.py" "$@"
}

# subject_of FILE: print the first of the first 4096 lines of FILE that is neither empty nor a
# '#' comment, without a trailing carriage return; print nothing when there is none.
subject_of() {
  local line count=0
  while [ "$count" -lt 4096 ] && { IFS= read -r line || [ -n "$line" ]; }; do
    count=$((count + 1))
    line="${line%$'\r'}"
    case "$line" in
      '' | '#'*) continue ;;
    esac
    if [[ "$line" =~ ^[[:space:]]*$ ]]; then
      continue
    fi
    printf '%s\n' "$line"
    return 0
  done < "$1"
}

subject="$(subject_of "$MSGFILE")"
if ! [[ "$subject" =~ $PR_SUBJECT_RE ]]; then
  exit 0
fi
PR="${BASH_REMATCH[1]}"

FETCH_SECONDS="${GUARD_DEFINITION_FETCH_SECONDS:-60}"
if ! [[ "$FETCH_SECONDS" =~ ^[1-9][0-9]{0,3}$ ]]; then
  echo "error: GUARD_DEFINITION_FETCH_SECONDS must be a whole number of seconds from 1 to 9999, got '$FETCH_SECONDS'" >&2
  exit 2
fi
PYTHON_BIN="${PYTHON_BIN:-python3}"
if ! [[ "$PYTHON_BIN" =~ ^[A-Za-z0-9_./+-]{1,255}$ ]]; then
  echo "error: PYTHON_BIN must be a command name or path of letters, digits and _ . / + -, got '$PYTHON_BIN'" >&2
  exit 2
fi

if ! url="$(bounded 10 git remote get-url origin 2>/dev/null)" || [ -z "$url" ]; then
  echo "guard-definition: no origin remote"
  exit 1
fi
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES \
  GIT_COMMON_DIR GIT_NAMESPACE GIT_PREFIX GIT_CEILING_DIRECTORIES GIT_DISCOVERY_ACROSS_FILESYSTEM

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
bounded 10 git init -q --bare "$tmp"

rc=0
out="$(bounded "$FETCH_SECONDS" git --git-dir "$tmp" fetch --no-tags --quiet --depth=2 "$url" \
  "refs/pull/$PR/merge" 2>&1)" || rc=$?
if [ "$rc" -eq 124 ]; then
  echo "guard-definition: fetch of refs/pull/$PR/merge passed the $FETCH_SECONDS s deadline"
  exit 1
fi
if [ "$rc" -ne 0 ]; then
  echo "guard-definition: cannot fetch refs/pull/$PR/merge from origin; the merge ref is missing when the pull request has conflicts"
  printf '%s\n' "$out" | tail -n 5
  exit 1
fi

merge="$(bounded 10 git --git-dir "$tmp" rev-parse --verify --quiet 'FETCH_HEAD^{commit}')"
read -r -a parents <<< "$(bounded 10 git --git-dir "$tmp" rev-list --parents -n 1 "$merge")"
if [ "${#parents[@]}" -ne 3 ]; then
  echo "guard-definition: refs/pull/$PR/merge is not a merge commit"
  exit 1
fi

rc=0
bounded 60 "$PYTHON_BIN" -I "$HERE/guard_definition_check.py" --git-dir "$tmp" \
  --base "${parents[1]}" --merge "$merge" --pr "$PR" || rc=$?
case "$rc" in
  0 | 1 | 2) exit "$rc" ;;
  124) echo "guard-definition: comparison passed the 60 s deadline" ;;
  *) echo "guard-definition: comparison could not run (exit $rc)" ;;
esac
exit 1
