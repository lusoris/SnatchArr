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
# DOCS_LOCK_SECONDS bounds the wait for another check holding the environment (default 900).
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
  { sub(/[[:space:]]+#.*/, ""); sub(/;.*/, "") }
  # Exact pins only: name[extras]==version; a wildcard such as ==9.* selects several releases.
  !/^[[:space:]]*[A-Za-z0-9][A-Za-z0-9._-]*(\[[A-Za-z0-9,._ -]*\])?[[:space:]]*==[[:space:]]*[0-9][0-9A-Za-z.+!-]*[[:space:]]*$/ { exit 1 }
' "$requirements"; then
  echo "error: $requirements: every requirement must have an exact == version pin" >&2
  exit 1
fi

mkdir -p "$cache"
cache="$(cd "$cache" && pwd)"
req_hash="$(sha256sum "$requirements" | cut -c1-12)"
venv="$cache/docs-$req_hash"
# One check at a time per environment: a second check on a cold cache would otherwise
# clear the environment while the first installs into it. mkdir is atomic on every
# platform, unlike flock, which macOS lacks.
lock="$venv.lock"
lock_seconds="${DOCS_LOCK_SECONDS:-900}"
waited=0
until mkdir "$lock" 2>/dev/null; do
  if [ "$waited" -ge "$lock_seconds" ]; then
    echo "error: $lock held for ${lock_seconds}s; remove it if no docs check is running" >&2
    exit 1
  fi
  sleep 1
  waited=$((waited + 1))
done
tmp=""
# cleanup releases the environment lock and removes the build output.
cleanup() {
  [ -z "$tmp" ] || rm -rf "$tmp"
  rmdir "$lock"
}
trap cleanup 0
trap 'exit 1' HUP INT TERM
# The marker is written only after pip succeeds and records the interpreter that
# built the environment: an interrupted install, or one made by a different
# python3, is rebuilt from scratch instead of trusted because bin/mkdocs exists.
interpreter="$(python3 -c 'import os, sys; print(sys.version.split()[0], os.path.realpath(sys.executable))')"
built_by=""
if [ -f "$venv/.installed" ]; then
  built_by="$(cat "$venv/.installed")"
fi
if [ -z "${MKDOCS_BIN:-}" ] && [ "$built_by" != "$interpreter" ]; then
  python3 -m venv --clear "$venv"
  "$venv/bin/pip" install --quiet --disable-pip-version-check -r "$requirements"
  printf '%s\n' "$interpreter" > "$venv/.installed"
fi

mkdocs="${MKDOCS_BIN:-$venv/bin/mkdocs}"
if [ ! -x "$mkdocs" ]; then
  echo "error: mkdocs executable not found at $mkdocs" >&2
  exit 1
fi

tmp="$(mktemp -d)"
cd "$root"
NO_MKDOCS_2_WARNING=1 "$mkdocs" build --strict -d "$tmp"
