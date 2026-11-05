#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for gosec_packages.sh in a throwaway module: a package under the git-ignored
# .workingdir/ and a testdata package are left out; tracked and untracked non-ignored
# packages and the module root are listed; ignored Go files in listed packages are refused.
# A package built only with a tag is listed with the tag and left out without it; no pattern
# is usage.
#
#   tools/ci/gosec_packages_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs go and git.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' 0
# Two platform declarations, bounded by the two-element literal list.
platforms="Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only."
for script in "$HERE/gosec_packages.sh" "$HERE/gosec_packages_test.sh"; do
  grep -Fxq "# $platforms" "$script" || { echo "FAIL platform reason missing: $script" >&2; exit 1; }
done
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
# Three excluded directory names: explicit bound = 3.
for gone in .workingdir testdata tagged; do
  case "$out" in
    *"$gone"*) printf 'FAIL %s listed:\n%s\n' "$gone" "$out" >&2; exit 1 ;;
  esac
done
run -tags gate ./tagged/...
check_case "tagged pattern" 0 "$rc" "./tagged" "$out"
run ./untracked/...
check_case "untracked non-ignored package" 0 "$rc" "./untracked" "$out"
# A tracked package with a load error must reach gosec, not disappear from its input.
printf 'package a\nimport _ "example.invalid/missing"\n' >"$M/a/a.go"
run ./a/...
check_case "tracked error package retained" 0 "$rc" "./a" "$out"
# Ignore files directly inside two listed packages: both diagnostics must name their files.
printf 'a/ignored.go\nm-ignored.go\n' >>"$M/.gitignore"
printf 'package a\n' >"$M/a/ignored.go"
printf 'package m\n' >"$M/m-ignored.go"
run ./...
check_case "ignored file inside listed package" 1 "$rc" "gosec would load git-ignored a/ignored.go; move or delete it" "$out"
check_case "all ignored files named" 1 "$rc" "gosec would load git-ignored m-ignored.go; move or delete it" "$out"
rm "$M/a/ignored.go" "$M/m-ignored.go"
run
check_case "no pattern" 2 "$rc" "usage" "$out"

echo "gosec_packages_test: all cases passed"
