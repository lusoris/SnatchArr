#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Builds docs with the pinned toolchain in a requirements-keyed virtual environment.
# Build output is temporary; warnings fail the check.
#
#   tools/ci/docs_check.sh CACHE_DIR [ROOT]
#
# MKDOCS_BIN overrides the virtual environment's executable for tests only.
set -eu

cache="${1:?usage: docs_check.sh CACHE_DIR [ROOT]}"
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
root="$(cd "${2:-$REPO_ROOT}" && pwd)"
requirements="$root/tools/docs/requirements.txt"

if [ ! -f "$requirements" ]; then
  echo "error: $requirements: requirements file missing" >&2
  exit 1
fi
if ! awk '
  /^[[:space:]]*(#|$)/ { next }
  { sub(/[[:space:]]+#.*/, ""); sub(/;.*/, ""); if (index($0, "==") == 0) exit 1 }
' "$requirements"; then
  echo "error: $requirements: every requirement must have a == version pin" >&2
  exit 1
fi

mkdir -p "$cache"
cache="$(cd "$cache" && pwd)"
req_hash="$(sha256sum "$requirements" | cut -c1-12)"
venv="$cache/docs-$req_hash"
# The marker is written only after pip succeeds, so an interrupted install is
# rebuilt from scratch instead of trusted because bin/mkdocs exists.
if [ -z "${MKDOCS_BIN:-}" ] && [ ! -f "$venv/.installed" ]; then
  python3 -m venv --clear "$venv"
  "$venv/bin/pip" install --quiet --disable-pip-version-check -r "$requirements"
  touch "$venv/.installed"
fi

mkdocs="${MKDOCS_BIN:-$venv/bin/mkdocs}"
if [ ! -x "$mkdocs" ]; then
  echo "error: mkdocs executable not found at $mkdocs" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' 0
trap 'exit 1' HUP INT TERM
cd "$root"
NO_MKDOCS_2_WARNING=1 "$mkdocs" build --strict -d "$tmp"
