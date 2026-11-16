#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Plant stale and incomplete Lizard cache entries; the wrapper must replace both.
set -euo pipefail

# Keep the Windows POSIX patterns in sync with codacy_lizard_check.sh.
case "$(uname -s)" in
  MINGW* | MSYS* | CYGWIN*)
    echo "codacy-lizard: skipped: this POSIX wrapper cannot use Windows venv Scripts" >&2
    exit 0
    ;;
esac

HERE=$(cd "$(dirname "$0")" && pwd)
wrapper=$HERE/codacy_lizard_check.sh
requirements=$HERE/../lizard/requirements.txt
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

requirements_hash=$(python3 -I -c 'import hashlib, sys; print(hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest()[:12])' "$requirements")

# Old requirements-only cache contains a broken interpreter that must never run.
old_cache=$work/old-cache
old_venv=$old_cache/lizard-$requirements_hash
mkdir -p "$old_venv/bin"
printf '%s\n' '#!/bin/sh' 'exit 1' > "$old_venv/bin/python"
chmod +x "$old_venv/bin/python"
touch "$old_venv/.installed"
if output=$(sh "$wrapper" "$old_cache" --self-test 2>&1); then
    printf '%s\n' "$output"
    echo 'ok   requirements-only cache ignored'
else
    status=$?
    printf '%s\n' "$output"
    echo "codacy_lizard_check_test: stale cache case failed ($status)" >&2
    exit "$status"
fi

set -- "$old_cache"/lizard-*
if [ "$#" -ne 2 ]; then
    echo "codacy_lizard_check_test: expected one current cache, found: $*" >&2
    exit 1
fi
old_name=lizard-$requirements_hash
first_name=${1##*/}
second_name=${2##*/}
if [ "$first_name" = "$old_name" ]; then
    discovered_venv=$2
elif [ "$second_name" = "$old_name" ]; then
    discovered_venv=$1
else
    echo "codacy_lizard_check_test: expected $old_name and one current cache, found: $first_name $second_name" >&2
    exit 1
fi
if [ ! -d "$discovered_venv" ] || [ ! -x "$discovered_venv/bin/python" ] || [ ! -f "$discovered_venv/.installed" ]; then
    echo "codacy_lizard_check_test: incomplete current cache: ${discovered_venv##*/}" >&2
    exit 1
fi
cache_key=${discovered_venv##*/}
cache_key=${cache_key#lizard-}

# Current cache key with an install marker but no interpreter must be rebuilt.
incomplete_cache=$work/incomplete-cache
incomplete_venv=$incomplete_cache/lizard-$cache_key
mkdir -p "$incomplete_venv"
touch "$incomplete_venv/.installed"
if output=$(sh "$wrapper" "$incomplete_cache" --self-test 2>&1); then
    printf '%s\n' "$output"
    echo 'ok   incomplete current cache recreated'
else
    status=$?
    printf '%s\n' "$output"
    echo "codacy_lizard_check_test: incomplete cache case failed ($status)" >&2
    exit "$status"
fi
