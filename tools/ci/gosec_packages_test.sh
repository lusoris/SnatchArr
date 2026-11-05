#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for gosec_packages.sh in a throwaway module: a package under the git-ignored
# .workingdir/, an untracked package directory and a testdata package are left out; tracked
# packages and the module root are listed; a package built only with a tag is listed with the
# tag and left out without it; no pattern is usage.
#
#   tools/ci/gosec_packages_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs go and git.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
M="$WORK/mod"
new_repo "$M"
mkdir -p "$M/tools/ci" "$M/a" "$M/b/c" "$M/tagged" "$M/a/testdata" "$M/.workingdir/evidence" "$M/untracked"
cp "$HERE/gosec_packages.sh" "$M/tools/ci/"
printf 'module example.invalid/m\n\ngo 1.24\n' >"$M/go.mod"
printf 'package m\n' >"$M/m.go"
printf 'package a\n' >"$M/a/a.go"
printf 'package c\n' >"$M/b/c/c.go"
printf '//go:build gate\n\npackage tagged\n' >"$M/tagged/t.go"
printf 'package td\n' >"$M/a/testdata/td.go"
printf '.workingdir/\n' >"$M/.gitignore"
git -C "$M" add go.mod m.go a/a.go b/c/c.go tagged/t.go a/testdata/td.go .gitignore
git -C "$M" commit -q -m init
# The #1007 plant: a package copy under .workingdir, and an untracked package beside a tracked one.
printf 'package prowlarr\n\nfunc f() { var x int }\n' >"$M/.workingdir/evidence/plant.go"
printf 'package untracked\n' >"$M/untracked/u.go"

# run ARGS...: run gosec_packages.sh in the module; sets rc and out.
run() {
  rc=0
  out="$(cd "$M" && sh tools/ci/gosec_packages.sh "$@" 2>&1)" || rc=$?
}
run ./...
check_case "pattern lists tracked packages" 0 "$rc" "./a" "$out"
check_case "pattern lists ./b/c" 0 "$rc" "./b/c" "$out"
case "$(printf '%s\n' "$out" | sed -n '1p')" in
  ./) ;;
  *) printf 'FAIL module root not listed first:\n%s\n' "$out" >&2; exit 1 ;;
esac
# Without the tag the tagged package has no Go file to load; tools-gosec scans it with the tag.
for gone in .workingdir untracked testdata tagged; do
  case "$out" in
    *"$gone"*) printf 'FAIL %s listed:\n%s\n' "$gone" "$out" >&2; exit 1 ;;
  esac
done
run -tags gate ./tagged/...
check_case "tagged pattern" 0 "$rc" "./tagged" "$out"
run ./untracked/...
check_case "only untracked packages" 1 "$rc" "no tracked package" "$out"
run
check_case "no pattern" 2 "$rc" "usage" "$out"

echo "gosec_packages_test: all cases passed"
