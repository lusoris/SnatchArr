#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Install the pinned Codacy analysis CLI under the repository cache, outside Go's ./tools/... walk,
# bounded by a 300 s timeout (overridden by CODACY_OPENGREP_NPM_DEADLINE_S, 1..3600).
set -eu

DEADLINE=${CODACY_OPENGREP_NPM_DEADLINE_S-300}
case "$DEADLINE" in
  '' | 0?* | *[!0-9]* | ?????*)
    printf 'codacy-opengrep install: CODACY_OPENGREP_NPM_DEADLINE_S must be a whole number from 1 to 3600 (got %s)\n' "$DEADLINE" >&2
    exit 2
    ;;
esac
if [ "$DEADLINE" -lt 1 ] || [ "$DEADLINE" -gt 3600 ]; then
  printf 'codacy-opengrep install: CODACY_OPENGREP_NPM_DEADLINE_S must be a whole number from 1 to 3600 (got %s)\n' "$DEADLINE" >&2
  exit 2
fi
if ! command -v timeout >/dev/null 2>&1; then
  printf 'codacy-opengrep install: timeout not found on PATH\n' >&2
  exit 2
fi

HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
ROOT=${1:-$(CDPATH='' cd -- "$HERE/../.." && pwd)}
SOURCE="$ROOT/tools/ci/codacy_opengrep"
CACHE="$ROOT/.cache/codacy_opengrep"

mkdir -p "$CACHE"
cp "$SOURCE/package.json" "$SOURCE/package-lock.json" "$CACHE/"

TIMEOUT_RC=0
# Keep the wrapper alive through the KILL grace period, even if npm exits on TERM.
# Report ordinary exits separately without capturing npm output or giving it fd 3.
{
# The quoted script is expanded by the inner sh.
# shellcheck disable=SC2016
NPM_STATUS=$(
  CDPATH='' cd -- "$CACHE" &&
    timeout --kill-after=1s "$DEADLINE" sh -c '
      # Never return to the normal path after TERM. Ignore repeat deliveries
      # before starting the bounded wait, so sleep inherits the ignore action.
      trap "trap \"\" TERM; sleep 2; exit 124" TERM
      rc=0
      npm ci --ignore-scripts --no-audit 3>&- || rc=$?
      printf "%s\n" "$rc" >&3 || exit 1
      exit 0
    ' 3>&1 >&4 4>&-
) || TIMEOUT_RC=$?
} 4>&1
case "$TIMEOUT_RC" in
  124 | 137)
    printf 'codacy-opengrep install: npm ci timed out after %s s\n' "$DEADLINE" >&2
    exit 124
    ;;
  0) ;;
  *)
    printf 'codacy-opengrep install: npm ci failed with exit %s\n' "$TIMEOUT_RC" >&2
    exit "$TIMEOUT_RC"
    ;;
esac
case "$NPM_STATUS" in
  '' | *[!0-9]* | ????* | 0?*)
    printf 'codacy-opengrep install: missing or invalid npm ci exit status\n' >&2
    exit 1
    ;;
esac
if [ "$NPM_STATUS" -gt 255 ]; then
  printf 'codacy-opengrep install: missing or invalid npm ci exit status\n' >&2
  exit 1
fi
if [ "$NPM_STATUS" -ne 0 ]; then
  printf 'codacy-opengrep install: npm ci failed with exit %s\n' "$NPM_STATUS" >&2
  exit "$NPM_STATUS"
fi
