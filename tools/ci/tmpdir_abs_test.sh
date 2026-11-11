#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Replay tools/ci/tmpdir_abs.sh and the TMPDIR block at the top of the Makefile in a throwaway
# tree: a relative TMPDIR with spaces, a quote, a comma or a `$` must stay one path, and copies
# of the block that split or expand the value must refuse it or resolve a different directory.
# A sub-make in another directory must see the same absolute path when TMPDIR came from the make
# command line, also for a backslash, a tab or a `$` in the name, and copies of the block that
# drop or under-escape the MAKEOVERRIDES entry must show a different path there.
# Every case runs with CDPATH pointing at a directory that holds the same names. It also checks
# that no shell file under tools/, .config/ or .devcontainer/ and no Makefile line changes into a
# TMPDIR value with cd, and replays the TMPDIR line of this test and of govulncheck_pinned_test.sh
# with TMPDIR=-, which a bare cd reads as OLDPWD.
#
#   tools/ci/tmpdir_abs_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs GNU make 4.2 or later
# (.SHELLSTATUS), sed, cmp, env -u, and grep -r with --include.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

HERE="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
ROOT="$(CDPATH='' cd -- "$HERE/../.." && pwd)"
# The cases change directory, so an inherited relative TMPDIR is resolved here first, by
# tools/ci/tmpdir_abs.sh, the one resolver: a bare `cd -- -` would read OLDPWD instead.
case "${TMPDIR:-}" in '' | /*) ;; *) TMPDIR="$(sh "$HERE/tmpdir_abs.sh" "$TMPDIR")"; export TMPDIR ;; esac
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' 0
# pwd -P form, the form tmpdir_abs.sh prints.
R="$(CDPATH='' cd -- "$WORK" && pwd -P)"
nl='
'
mkdir -p "$R/tools/ci" "$R/sub/t" "$R/sub/t space"
cp "$HERE/tmpdir_abs.sh" "$R/tools/ci/tmpdir_abs.sh"
cp "$ROOT/Makefile" "$R/Makefile"
# shellcheck disable=SC2016 # make recipe text: make turns $$TMPDIR into the recipe's $TMPDIR
printf 'show:\n\t@printf "[%%s]\\n" "$$TMPDIR"\nsubshow:\n\t@$(MAKE) --no-print-directory -C child -f ../show.mk show\n' >"$R/show.mk"
# shellcheck disable=SC2016 # a directory name with a literal $x, which make must not expand
dollar_name='t$x'
bs_name='b\s'
tab_name="tab$(printf '\t')x"
mkdir "$R/t" "$R/t space" "$R/it's" "$R/a,b (1)" "$R/$dollar_name" "$R/end " "$R/-" "$R/nl$nl" \
  "$R/$bs_name" "$R/$tab_name" "$R/child"
: >"$R/file"
ln -s "$R/t space" "$R/link"
CDPATH="$R/sub"
export CDPATH

# check_output NAME WANT_RC GOT_RC WANT_OUT NEEDLE: passes when GOT_RC is WANT_RC, stdout is
# WANT_OUT plus a newline (empty when WANT_OUT is empty) and stderr contains NEEDLE; otherwise
# prints the FAIL diagnostic to stderr and exits 1.
check_output() {
  if [ -n "$4" ]; then printf '%s\n' "$4" >"$WORK/want"; else : >"$WORK/want"; fi
  if [ "$3" -ne "$2" ] || ! cmp -s "$WORK/want" "$WORK/out" ||
    { [ -n "$5" ] && ! grep -Fq -- "$5" "$WORK/err"; }; then
    printf 'FAIL %s: exit %s, want %s\nstdout: %s\nwant:   %s\nstderr: %s\n' "$1" "$3" "$2" \
      "$(cat "$WORK/out")" "$4" "$(cat "$WORK/err")" >&2
    exit 1
  fi
  printf 'ok   %s\n' "$1"
}

# abs_case NAME WANT_RC WANT_OUT NEEDLE ARG...: runs tmpdir_abs.sh ARG... in the throwaway tree,
# with OLDPWD at another directory, and checks it with check_output.
abs_case() {
  name=$1 want_rc=$2 want_out=$3 needle=$4
  shift 4
  rc=0
  (cd "$R" && OLDPWD="$R/t" sh tools/ci/tmpdir_abs.sh "$@") >"$WORK/out" 2>"$WORK/err" || rc=$?
  check_output "$name" "$want_rc" "$rc" "$want_out" "$needle"
}

# make_goal_case GOAL NAME WANT_RC WANT_OUT NEEDLE MAKEFILE CMD...: runs CMD... -s -f MAKEFILE
# -f show.mk GOAL in the throwaway tree, without the flags of a calling make, and checks it with
# check_output. The show goal prints the TMPDIR its recipe sees in brackets; subshow runs show in
# a sub-make in child/ that reads no copy of the Makefile, as `$(MAKE) -C api -f ...` does.
make_goal_case() {
  goal=$1 name=$2 want_rc=$3 want_out=$4 needle=$5 mf=$6
  shift 6
  rc=0
  (cd "$R" && MAKEFLAGS='' MFLAGS='' MAKELEVEL='' GNUMAKEFLAGS='' "$@" -s -f "$mf" -f show.mk "$goal") \
    >"$WORK/out" 2>"$WORK/err" || rc=$?
  check_output "$name" "$want_rc" "$rc" "$want_out" "$needle"
}

# make_case NAME WANT_RC WANT_OUT NEEDLE MAKEFILE CMD...: make_goal_case with the show goal.
make_case() { make_goal_case show "$@"; }

# submake_case NAME WANT_RC WANT_OUT NEEDLE MAKEFILE CMD...: make_goal_case with the subshow goal.
submake_case() { make_goal_case subshow "$@"; }

# Positive: one relative directory, printed as one absolute path.
abs_case rel-dir 0 "$R/t" '' t
abs_case rel-space-beside-prefix 0 "$R/t space" '' 't space'
abs_case rel-quote 0 "$R/it's" '' "it's"
abs_case rel-dollar 0 "$R/$dollar_name" '' "$dollar_name"
abs_case rel-symlink 0 "$R/t space" '' link
make_case make-env-space 0 "[$R/t space]" '' Makefile env 'TMPDIR=t space' make
make_case make-cli-space 0 "[$R/t space]" '' Makefile make 'TMPDIR=t space'
make_case make-quote 0 "[$R/it's]" '' Makefile env "TMPDIR=it's" make
make_case make-comma-parens 0 "[$R/a,b (1)]" '' Makefile env 'TMPDIR=a,b (1)' make
make_case make-dollar 0 "[$R/$dollar_name]" '' Makefile env "TMPDIR=$dollar_name" make
submake_case submake-cli-space 0 "[$R/t space]" '' Makefile make 'TMPDIR=t space'
submake_case submake-cli-dollar 0 "[$R/$dollar_name]" '' Makefile make "TMPDIR=$dollar_name"
submake_case submake-cli-backslash 0 "[$R/$bs_name]" '' Makefile make "TMPDIR=$bs_name"
submake_case submake-cli-tab 0 "[$R/$tab_name]" '' Makefile make "TMPDIR=$tab_name"
submake_case submake-env-space 0 "[$R/t space]" '' Makefile env 'TMPDIR=t space' make

# Negative: no directory, a file, a path a $(shell) result would change, usage errors.
abs_case missing 1 '' 'names no directory' missing
abs_case regular-file 1 '' 'names no directory' file
abs_case trailing-newline 1 '' 'path with a newline' "nl$nl"
abs_case no-arg 2 '' usage
abs_case two-args 2 '' usage t space
abs_case empty-arg 2 '' usage ''
make_case make-missing 2 '' "refused TMPDIR 'missing' with exit 1" Makefile env TMPDIR=missing make
submake_case submake-cli-missing 2 '' "refused TMPDIR 'missing' with exit 1" Makefile make TMPDIR=missing

# Boundary: absolute values stay as they are, "-" is a directory name, a trailing space stays.
abs_case absolute-missing 0 '' '' '/no such/dir'
abs_case dash 0 "$R/-" '' -
abs_case trailing-space 0 "$R/end " '' 'end '
make_case make-absolute 0 "[/no such/dir]" '' Makefile env 'TMPDIR=/no such/dir' make
make_case make-unset 0 "[]" '' Makefile env -u TMPDIR make
make_case make-empty 0 "[]" '' Makefile env TMPDIR= make
submake_case submake-cli-absolute 0 "[/no such/dir]" '' Makefile make 'TMPDIR=/no such/dir'
submake_case submake-cli-trailing-space 0 "[$R/end ]" '' Makefile make 'TMPDIR=end '

# Mutation: a block that passes the value unquoted splits "t space" into two arguments, one that
# expands it turns "t$x" into "t", one without the MAKEOVERRIDES entry leaves the sub-make the
# relative value, and one that doubles `$` only once lets the sub-make export "t" for "t$x".
# shellcheck disable=SC2016 # sed program: the $(...) text is the Makefile line, not shell
sed '/^TMPDIR_ABS := /c\
TMPDIR_ABS := $(shell sh tools/ci/tmpdir_abs.sh $(value TMPDIR))' "$R/Makefile" >"$R/Makefile.unquoted"
# shellcheck disable=SC2016 # same: literal sed program text
sed 's/\$(value TMPDIR)/$(TMPDIR)/g' "$R/Makefile" >"$R/Makefile.expanded"
sed '/^MAKEOVERRIDES += TMPDIR=/d' "$R/Makefile" >"$R/Makefile.nooverride"
# shellcheck disable=SC2016 # same: literal sed program text
sed '/^TMPDIR_MAKEFLAG := /s/\$\$\$\$\$\$\$\$/$$$$/' "$R/Makefile" >"$R/Makefile.onedollar"
for mutant in Makefile.unquoted Makefile.expanded Makefile.nooverride Makefile.onedollar; do
  if cmp -s "$R/Makefile" "$R/$mutant"; then
    echo "FAIL $mutant: the Makefile no longer holds the line the mutant changes" >&2
    exit 1
  fi
done
make_case mutant-unquoted 2 '' 'with exit 2' Makefile.unquoted env 'TMPDIR=t space' make
make_case mutant-expanded 0 "[$R/t]" '' Makefile.expanded env "TMPDIR=$dollar_name" make
submake_case mutant-nooverride 0 "[t space]" '' Makefile.nooverride make 'TMPDIR=t space'
submake_case mutant-onedollar 0 "[$R/t]" '' Makefile.onedollar make "TMPDIR=$dollar_name"

# cd_tmpdir_lines PATH...: prints FILE:LINE:text of every line, in the shell files, *.mk files and
# Makefiles at or below PATH, that changes into a TMPDIR value with cd (options such as -- or -P
# between cd and the value included). No match is clean; a grep error fails the test.
cd_tmpdir_lines() {
  scan_rc=0
  grep -rHnE --include='*.sh' --include='*.mk' --include=Makefile \
    'cd([[:space:]]+-[[:alpha:]-]*)*[[:space:]]+"?\$\{?TMPDIR' "$@" || scan_rc=$?
  if [ "$scan_rc" -gt 1 ]; then
    printf 'FAIL cd_tmpdir_lines: grep exit %s\n' "$scan_rc" >&2
    exit 1
  fi
}

# One resolver: a relative TMPDIR goes through tools/ci/tmpdir_abs.sh, never through a cd of its own.
lines="$(cd_tmpdir_lines "$ROOT/tools" "$ROOT/.config" "$ROOT/.devcontainer" "$ROOT/Makefile")"
if [ -n "$lines" ]; then
  printf 'FAIL one-resolver: resolve a relative TMPDIR with tools/ci/tmpdir_abs.sh\n%s\n' "$lines" >&2
  exit 1
fi
printf 'ok   one-resolver\n'

# Planted: the line both test scripts carried before, and a cd -P form. The escaped quotes keep
# this file's own source out of the scan; printf writes the lines verbatim.
old_line="$(printf '%s\n' "case \"\${TMPDIR:-}\" in '' | /*) ;; *) TMPDIR=\"\$(CDPATH='' cd -- \"\$TMPDIR\" && pwd)\"; export TMPDIR ;; esac")"
opt_line="$(printf '%s\n' "cd -P \"\${TMPDIR}\" || exit 1")"
mkdir "$R/plant"
printf '%s\n%s\n' "$old_line" "$opt_line" >"$R/plant/planted.sh"
lines="$(cd_tmpdir_lines "$R/plant")"
if [ "$lines" != "$R/plant/planted.sh:1:$old_line$nl$R/plant/planted.sh:2:$opt_line" ]; then
  printf 'FAIL planted-cd-tmpdir: scan reported\n%s\n' "$lines" >&2
  exit 1
fi
printf 'ok   planted-cd-tmpdir\n'

# preamble_run LINE: runs LINE and then mktemp -d in a child sh started in the throwaway tree with
# TMPDIR=- and OLDPWD at another directory; HERE is the tree's tools/ci, as in the test scripts.
# Sets rc; stdout and stderr land in $WORK/out and $WORK/err.
preamble_run() {
  rc=0
  # shellcheck disable=SC2016 # child-shell program: HERE=$1 reads the child's first argument
  (cd "$R" && OLDPWD="$R/t" TMPDIR=- sh -c 'set -eu; HERE=$1; '"$1"'; mktemp -d' sh "$R/tools/ci") \
    >"$WORK/out" 2>"$WORK/err" || rc=$?
}

# Boundary: the TMPDIR line of this test and of govulncheck_pinned_test.sh, taken from the files as
# written, must read "-" as the directory "-", where the temporary directory then lands.
for script in tmpdir_abs_test.sh govulncheck_pinned_test.sh; do
  # shellcheck disable=SC2016 # sed address: the ${TMPDIR:-} text of the line, not shell
  line="$(sed -n '/^case "\${TMPDIR:-}" in /p' "$HERE/$script")"
  case $line in
    '' | *"$nl"*)
      printf 'FAIL dash-preamble %s: want one TMPDIR line, got\n%s\n' "$script" "$line" >&2
      exit 1
      ;;
  esac
  preamble_run "$line"
  created="$(cat "$WORK/out")"
  case "$rc:$created" in
    "0:$R/-/"*) ;;
    *)
      printf 'FAIL dash-preamble %s: exit %s, directory [%s]\n%s\n' "$script" "$rc" "$created" \
        "$(cat "$WORK/err")" >&2
      exit 1
      ;;
  esac
  rmdir "$created"
  printf 'ok   dash-preamble %s\n' "$script"
done

# Mutation: the old line reads "-" as OLDPWD and captures the path twice, so mktemp fails.
preamble_run "$old_line"
if [ "$rc" -eq 0 ]; then
  printf 'FAIL mutant-old-preamble: the old line accepted TMPDIR=-, directory [%s]\n' "$(cat "$WORK/out")" >&2
  exit 1
fi
printf 'ok   mutant-old-preamble\n'
echo "tmpdir_abs: all cases passed"
