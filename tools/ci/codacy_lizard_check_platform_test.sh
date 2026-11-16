#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Simulate Windows uname and require the Lizard cache test to skip before fixture creation.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
real_uname=$(command -v uname)
real_mkdir=$(command -v mkdir)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/shims"

cat > "$work/shims/uname" <<'SH'
#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = -s ]; then
  printf '%s\n' 'MINGW64_NT-10.0-19045'
else
  exec "$REAL_UNAME" "$@"
fi
SH
cat > "$work/shims/mkdir" <<'SH'
#!/bin/sh
case " $* " in
  *lizard-*) printf '%s\n' "$*" >> "$LIZARD_MKDIR_LOG" ;;
esac
exec "$REAL_MKDIR" "$@"
SH
chmod +x "$work/shims/uname" "$work/shims/mkdir"
: > "$work/mkdir.log"

status=0
PATH="$work/shims:$PATH" REAL_UNAME="$real_uname" REAL_MKDIR="$real_mkdir" \
  LIZARD_MKDIR_LOG="$work/mkdir.log" \
  bash "$here/codacy_lizard_check_test.sh" > "$work/stdout" 2> "$work/stderr" || status=$?
if [ "$status" -ne 0 ]; then
  echo "codacy_lizard_check_platform_test: expected exit 0, got $status" >&2
  cat "$work/stderr" >&2
  exit 1
fi
expected='codacy-lizard: skipped: this POSIX wrapper cannot use Windows venv Scripts'
if [ "$(cat "$work/stderr")" != "$expected" ]; then
  echo 'codacy_lizard_check_platform_test: missing Windows skip reason on stderr' >&2
  cat "$work/stderr" >&2
  exit 1
fi
if [ -s "$work/mkdir.log" ]; then
  echo 'codacy_lizard_check_platform_test: created lizard-* cache directory' >&2
  cat "$work/mkdir.log" >&2
  exit 1
fi
echo 'ok   Windows POSIX test skips before creating lizard cache'
