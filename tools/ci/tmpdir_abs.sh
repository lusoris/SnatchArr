#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Print the absolute directory that a relative TMPDIR value names, for the TMPDIR block at the
# top of the Makefile. The value arrives as one argument, so a path with spaces or quotes stays
# one path; make's word functions would split it.
#
#   tools/ci/tmpdir_abs.sh <TMPDIR value>
#
# Exit status: 0 with the absolute, symlink-free directory on stdout for a relative value, or
# with nothing on stdout for an absolute value, which the Makefile keeps as it is; 1 when a
# relative value names no directory, or resolves to a path with a newline, which make's $(shell)
# would turn into a space; 2 on a usage error.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
  echo "usage: tmpdir_abs.sh <non-empty TMPDIR value>" >&2
  exit 2
fi
case $1 in
  /*) exit 0 ;;
esac
nl='
'
# "./" keeps cd from reading "-" as OLDPWD and from searching CDPATH; CDPATH is cleared as well,
# since a CDPATH match makes cd print the directory into the substitution. The "." after pwd's
# newline keeps the substitution from stripping newlines that end the path.
dir="$(CDPATH='' cd -- "./$1" 2>/dev/null && pwd -P && echo .)" || {
  printf 'tmpdir_abs.sh: TMPDIR %s names no directory\n' "$1" >&2
  exit 1
}
dir="${dir%"$nl."}"
case $dir in
  *"$nl"*)
    printf 'tmpdir_abs.sh: TMPDIR %s resolves to a path with a newline\n' "$1" >&2
    exit 1
    ;;
esac
printf '%s\n' "$dir"
