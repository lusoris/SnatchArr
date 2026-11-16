#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Install the Codacy-pinned Lizard in a requirements-keyed cache, then check the worktree.
# Usage: codacy_lizard_check.sh CACHE_DIR [check arguments]
set -eu

cache="${1:?usage: codacy_lizard_check.sh CACHE_DIR [check arguments]}"
shift
case "$(uname -s)" in
  MINGW* | MSYS* | CYGWIN*)
    echo "codacy-lizard: skipped: this POSIX wrapper cannot use Windows venv Scripts" >&2
    exit 0
    ;;
esac
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
requirements="$root/tools/lizard/requirements.txt"
mkdir -p "$cache"
cache="$(cd "$cache" && pwd)"
venv_key="$(python3 -I -c 'import hashlib, sys; digest = hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest()[:12]; print(digest + "-" + sys.implementation.name + "-" + str(sys.version_info.major) + "." + str(sys.version_info.minor))' "$requirements")"
venv="$cache/lizard-$venv_key"
if [ ! -x "$venv/bin/python" ]; then
  rm -f "$venv/.installed"
  python3 -m venv "$venv"
fi
if [ ! -f "$venv/.installed" ]; then
  "$venv/bin/pip" install --quiet --disable-pip-version-check --require-hashes -r "$requirements"
  touch "$venv/.installed"
fi
cd "$root"
exec "$venv/bin/python" -I tools/ci/codacy_lizard_check.py "$@"
