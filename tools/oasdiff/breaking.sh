#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# OpenAPI breaking change detector against a base revision (HISS-14). An ERR-level breaking
# change fails the check unless SUBJECT declares a breaking change via conventional-commit
# syntax (e.g. "feat(api)!: ..."). make api-openapi-breaking and CI job api-openapi both call
# this script, so the version and flags live here only; breaking_test.sh plants the cases it
# must accept and refuse.
#
#   tools/oasdiff/breaking.sh BASE_SPEC REVISION_SPEC SUBJECT
#
# OASDIFF names the command runner (default: go -C tools/go tool oasdiff). Exit status: 0 when no
# ERR-level change exists or when a break is declared in SUBJECT, non-zero otherwise.
set -eu

if [ "$#" -ne 3 ]; then
  echo "usage: ${0##*/} BASE_SPEC REVISION_SPEC SUBJECT" >&2
  exit 2
fi

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"

case "$1" in
  /*) BASE="$1" ;;
  *) BASE="$PWD/$1" ;;
esac

case "$2" in
  /*) REVISION="$2" ;;
  *) REVISION="$PWD/$2" ;;
esac

SUBJECT="$3"

OASDIFF="${OASDIFF:-go -C $REPO_ROOT/tools/go tool oasdiff}"

status=0
# shellcheck disable=SC2086 # OASDIFF is a command with its arguments.
out=$($OASDIFF breaking "$BASE" "$REVISION" --fail-on ERR 2>&1) || status=$?

if [ -n "$out" ]; then
  printf '%s\n' "$out"
fi

if [ "$status" -eq 0 ]; then
  exit 0
fi

if [ "$status" -eq 1 ] && printf '%s\n' "$SUBJECT" | grep -Eq '^[a-z]+(\([^)]*\))?!: '; then
  echo "breaking change declared in subject: $SUBJECT"
  exit 0
fi

exit "$status"
