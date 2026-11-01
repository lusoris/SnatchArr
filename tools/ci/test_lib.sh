#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Shared test helpers for tools/ci test scripts. Sourced by test scripts,
# never executed directly:
#
#   . "$HERE/test_lib.sh"
#
# Provides check_case for exit code and needle assertions, and new_repo for
# initializing throwaway test git repositories.
#
# Callers run under set -eu already; the lib states it for its own lines.
set -eu

# check_case NAME WANT_RC GOT_RC NEEDLE OUT [MISSING]: checks that GOT_RC
# matches WANT_RC and OUT contains NEEDLE. On failure, prints the FAIL
# diagnostic to stderr and exits 1; MISSING words the missing-needle line
# (default "output did not contain"). On success, returns 0.
check_case() {
  missing="${6:-output did not contain}"
  if [ "$3" -ne "$2" ]; then
    printf 'FAIL %s: exit %s, want %s\n%s\n' "$1" "$3" "$2" "$5" >&2
    exit 1
  fi

  case "$5" in
    *"$4"*) ;;
    *)
      printf 'FAIL %s: %s "%s"\n%s\n' "$1" "$missing" "$4" "$5" >&2
      exit 1
      ;;
  esac
}

# new_repo DIR: initialize a throwaway git repository at DIR.
new_repo() {
  mkdir -p "$1"
  git -C "$1" init -q -b main
  git -C "$1" config user.email t@example.invalid
  git -C "$1" config user.name t
  git -C "$1" config commit.gpgsign false
}
