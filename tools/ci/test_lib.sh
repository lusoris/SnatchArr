#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Shared test helpers for tools/ci test scripts. Sourced by test scripts,
# never executed directly:
#
#   . "$HERE/test_lib.sh"
#
# Provides check_case for exit code and needle assertions, run_bounded for a
# deadline on one command, and new_repo for initializing throwaway test git
# repositories.
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

# run_bounded SECONDS CMD...: runs CMD through tools/ci/run_bounded.py, so
# callers need no GNU timeout (absent on stock macOS). Returns the exit
# status of CMD, 124 when SECONDS pass first (as GNU timeout does), 128+N
# when signal N ends CMD, 125 on a usage error, 126 when CMD cannot be run
# and 127 when it is not found. At the deadline the whole process group of
# CMD is killed, so no descendant keeps a captured stdout open; the script's
# docstring states the limits. Reads HERE (tools/ci), which every caller sets
# before sourcing this lib; set -u fails a caller that does not.
run_bounded() {
  python3 -I "$HERE/run_bounded.py" "$@"
}

# new_repo DIR: initialize a throwaway git repository at DIR.
new_repo() {
  mkdir -p "$1"
  git -C "$1" init -q -b main
  git -C "$1" config user.email t@example.invalid
  git -C "$1" config user.name t
  git -C "$1" config commit.gpgsign false
}
