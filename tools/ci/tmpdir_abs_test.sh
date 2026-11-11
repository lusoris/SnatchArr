#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Replay tools/ci/tmpdir_abs.sh and the TMPDIR block at the top of the Makefile in a throwaway
# tree: a relative TMPDIR with spaces, a quote, a comma or a `$` must stay one path, and copies
# of the block that split or expand the value must refuse it or resolve a different directory.
# Every case runs with CDPATH pointing at a directory that holds the same names.
#
#   tools/ci/tmpdir_abs_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs GNU make 4.2 or later
# (.SHELLSTATUS), sed, cmp and env -u.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

HERE="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
ROOT="$(CDPATH='' cd -- "$HERE/../.." && pwd)"
# The cases change directory, so an inherited relative TMPDIR is resolved here first.
case "${TMPDIR:-}" in '' | /*) ;; *) TMPDIR="$(CDPATH='' cd -- "$TMPDIR" && pwd)"; export TMPDIR ;; esac
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
printf 'show:\n\t@printf "[%%s]\\n" "$$TMPDIR"\n' >"$R/show.mk"
# shellcheck disable=SC2016 # a directory name with a literal $x, which make must not expand
dollar_name='t$x'
mkdir "$R/t" "$R/t space" "$R/it's" "$R/a,b (1)" "$R/$dollar_name" "$R/end " "$R/-" "$R/nl$nl"
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

# make_case NAME WANT_RC WANT_OUT NEEDLE MAKEFILE CMD...: runs CMD... -s -f MAKEFILE -f show.mk
# show in the throwaway tree, without the flags of a calling make, and checks it with
# check_output; the show target prints the TMPDIR its recipe sees in brackets.
make_case() {
  name=$1 want_rc=$2 want_out=$3 needle=$4 mf=$5
  shift 5
  rc=0
  (cd "$R" && MAKEFLAGS='' MFLAGS='' MAKELEVEL='' GNUMAKEFLAGS='' "$@" -s -f "$mf" -f show.mk show) \
    >"$WORK/out" 2>"$WORK/err" || rc=$?
  check_output "$name" "$want_rc" "$rc" "$want_out" "$needle"
}

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

# Negative: no directory, a file, a path a $(shell) result would change, usage errors.
abs_case missing 1 '' 'names no directory' missing
abs_case regular-file 1 '' 'names no directory' file
abs_case trailing-newline 1 '' 'path with a newline' "nl$nl"
abs_case no-arg 2 '' usage
abs_case two-args 2 '' usage t space
abs_case empty-arg 2 '' usage ''
make_case make-missing 2 '' "refused TMPDIR 'missing' with exit 1" Makefile env TMPDIR=missing make

# Boundary: absolute values stay as they are, "-" is a directory name, a trailing space stays.
abs_case absolute-missing 0 '' '' '/no such/dir'
abs_case dash 0 "$R/-" '' -
abs_case trailing-space 0 "$R/end " '' 'end '
make_case make-absolute 0 "[/no such/dir]" '' Makefile env 'TMPDIR=/no such/dir' make
make_case make-unset 0 "[]" '' Makefile env -u TMPDIR make
make_case make-empty 0 "[]" '' Makefile env TMPDIR= make

# Mutation: a block that passes the value unquoted splits "t space" into two arguments, and one
# that expands it turns "t$x" into "t".
# shellcheck disable=SC2016 # sed program: the $(...) text is the Makefile line, not shell
sed '/^TMPDIR_ABS := /c\
TMPDIR_ABS := $(shell sh tools/ci/tmpdir_abs.sh $(value TMPDIR))' "$R/Makefile" >"$R/Makefile.unquoted"
# shellcheck disable=SC2016 # same: literal sed program text
sed 's/\$(value TMPDIR)/$(TMPDIR)/g' "$R/Makefile" >"$R/Makefile.expanded"
for mutant in Makefile.unquoted Makefile.expanded; do
  if cmp -s "$R/Makefile" "$R/$mutant"; then
    echo "FAIL $mutant: the Makefile no longer holds the line the mutant changes" >&2
    exit 1
  fi
done
make_case mutant-unquoted 2 '' 'with exit 2' Makefile.unquoted env 'TMPDIR=t space' make
make_case mutant-expanded 0 "[$R/t]" '' Makefile.expanded env "TMPDIR=$dollar_name" make
echo "tmpdir_abs: all cases passed"
