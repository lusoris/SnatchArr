#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Install the pinned Codacy analysis CLI under the repository cache, outside Go's ./tools/... walk.
set -eu

HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
ROOT=${1:-$(CDPATH='' cd -- "$HERE/../.." && pwd)}
SOURCE="$ROOT/tools/ci/codacy_opengrep"
CACHE="$ROOT/.cache/codacy_opengrep"

mkdir -p "$CACHE"
cp "$SOURCE/package.json" "$SOURCE/package-lock.json" "$CACHE/"

NPM_RC=0
(CDPATH='' cd -- "$CACHE" && npm ci --ignore-scripts --no-audit) || NPM_RC=$?
if [ "$NPM_RC" -ne 0 ]; then
  printf 'codacy-opengrep install: npm ci failed with exit %s\n' "$NPM_RC" >&2
  exit "$NPM_RC"
fi
