#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Lists the package directories api-gosec scans: the packages `go list` resolves for the
# patterns, kept only when git tracks a .go file in them. gosec walks a ./... pattern itself and
# skips only vendor/, so it loaded Go files under the git-ignored .workingdir/ (planted copies of
# a package, #1007) and failed on code that is not in the repository.
#
#   tools/ci/gosec_packages.sh [-tags TAGS] <pattern>...
#
# Prints one ./<dir> per line, sorted. Exit status: 0 listed, 1 nothing listed or go list failed,
# 2 usage. Cases: gosec_packages_test.sh.
set -eu

tags=""
if [ "${1:-}" = "-tags" ]; then
  [ "$#" -ge 3 ] || { echo "usage: ${0##*/} [-tags TAGS] <pattern>..." >&2; exit 2; }
  tags="$2"
  shift 2
fi
[ "$#" -ge 1 ] || { echo "usage: ${0##*/} [-tags TAGS] <pattern>..." >&2; exit 2; }

root="$(git rev-parse --show-toplevel)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
if ! go list -e ${tags:+-tags "$tags"} -f '{{.Dir}}' "$@" >"$work/listed"; then
  echo "gosec_packages: go list failed for $*" >&2
  exit 1
fi
git -C "$root" ls-files -z -- '*.go' | tr '\0' '\n' | sed -e 's|/[^/]*$||' -e 's|^[^/]*\.go$|.|' |
  sort -u >"$work/tracked"
sed -e "s|^$root/*||" -e 's|^$|.|' "$work/listed" | sort -u >"$work/rel"
comm -12 "$work/rel" "$work/tracked" | sed -e 's|^\.$||' -e 's|^|./|' >"$work/out"
if [ ! -s "$work/out" ]; then
  echo "gosec_packages: no tracked package for $*" >&2
  exit 1
fi
cat "$work/out"
