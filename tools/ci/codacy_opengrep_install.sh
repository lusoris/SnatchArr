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

NPM_RC=0
(CDPATH='' cd -- "$CACHE" && timeout "$DEADLINE" npm ci --ignore-scripts --no-audit) || NPM_RC=$?
if [ "$NPM_RC" -eq 124 ]; then
  printf 'codacy-opengrep install: npm ci timed out after %s s\n' "$DEADLINE" >&2
  exit 124
fi
if [ "$NPM_RC" -ne 0 ]; then
  printf 'codacy-opengrep install: npm ci failed with exit %s\n' "$NPM_RC" >&2
  exit "$NPM_RC"
fi
