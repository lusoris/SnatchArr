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
# deadline on one command, new_repo for initializing throwaway test git
# repositories, track and run_repo_case for staged-fixture checker cases, and
# assert_wired for holding CI and the Makefile to both readers of a checker.
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

# assert_wired CHECK FILE...: checks that each FILE, relative to the repository
# root above HERE, runs `python3 -I tools/ci/CHECK` twice: over the working tree
# and with --index, each run ending its line or followed by `&&`. On a missing
# run, prints the FAIL wiring diagnostic to stderr and exits 1.
assert_wired() {
  wired_check=$1
  shift
  wired_root=$(cd "$HERE/../.." && pwd)
  wired_pattern=$(printf '%s' "$wired_check" | sed 's/[.]/\\./g')
  for wired_file in "$@"; do
    for wired_reader in '' ' --index'; do
      if ! grep -q -E "python3 -I tools/ci/${wired_pattern}${wired_reader}( &&|\$)" "$wired_root/$wired_file"; then
        printf "FAIL wiring: %s runs no 'python3 -I tools/ci/%s%s'\n" "$wired_file" "$wired_check" "$wired_reader" >&2
        exit 1
      fi
    done
  done
}

# track PATH LINE...: writes LINE... to PATH, one per line, and stages PATH in
# the git repository of the current directory.
track() {
  track_file=$1
  shift
  mkdir -p "$(dirname "$track_file")"
  printf '%s\n' "$@" > "$track_file"
  git add "$track_file"
}

# run_repo_case NAME WANT_RC SETUP NEEDLE...: builds a fresh repository at
# $work/NAME, runs SETUP inside it, runs `python3 -I $check --root` on it, and
# holds the exit code to WANT_RC and the output to each NEEDLE (check_case).
# The caller sets work (scratch directory) and check (the checker script).
run_repo_case() {
  repo_case_name=$1
  repo_case_want=$2
  repo_case_setup=$3
  shift 3
  repo_case_dir="${work:?run_repo_case needs work set}/$repo_case_name"
  repo_case_status=0
  new_repo "$repo_case_dir"
  (cd "$repo_case_dir" && "$repo_case_setup")
  repo_case_out=$(python3 -I "${check:?run_repo_case needs check set}" --root "$repo_case_dir" 2>&1) || repo_case_status=$?
  for repo_case_needle in "$@"; do
    check_case "$repo_case_name" "$repo_case_want" "$repo_case_status" "$repo_case_needle" "$repo_case_out"
  done
  echo "ok   $repo_case_name"
}

# new_repo DIR: initialize a throwaway git repository at DIR.
new_repo() {
  mkdir -p "$1"
  git -C "$1" init -q -b main
  git -C "$1" config user.email t@example.invalid
  git -C "$1" config user.name t
  git -C "$1" config commit.gpgsign false
}
