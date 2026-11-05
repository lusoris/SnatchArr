#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Lists the package directories api-gosec scans: the packages `go list` resolves for the
# patterns, kept when a tracked or untracked, non-ignored .go file sits in them.
# gosec walks a ./... pattern itself and skips only vendor/, so it loaded Go files under the git-ignored .workingdir/ (planted copies of
# a package, #1007) and failed on code that is not in the repository.
#
#   tools/ci/gosec_packages.sh [-tags TAGS] <pattern>...
#
# Refuses ignored .go files directly inside listed directories: gosec would load them too.
# Keep go list -e: packages with errors must reach gosec and fail there.
# Prints one ./<dir> per line, sorted. Exit status: 0 listed, 1 no package, ignored file or list failure,
# 2 usage. Cases: gosec_packages_test.sh.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
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
trap 'rm -rf "$work"' 0
# The dir= prefix keeps a line for every package: go list prints nothing for an empty template
# result, so a missing package (empty Dir under -e) would drop out of the scan unseen.
if ! go list -e ${tags:+-tags "$tags"} -f 'dir={{.Dir}}' "$@" >"$work/raw"; then
  echo "gosec_packages: go list failed for $*" >&2
  exit 1
fi
if grep -qx 'dir=' "$work/raw"; then
  echo "gosec_packages: go list returned a package without a directory for $*" >&2
  exit 1
fi
sed -n 's|^dir=||p' "$work/raw" >"$work/listed"
git -C "$root" ls-files -z --cached --others --exclude-standard -- '*.go' >"$work/kept-files"
tr '\0' '\n' <"$work/kept-files" | sed -e 's|/[^/]*$||' -e 's|^[^/]*\.go$|.|' |
  sort -u >"$work/kept"
sed -e "s|^$root/*||" -e 's|^$|.|' "$work/listed" | sort -u >"$work/rel"
comm -12 "$work/rel" "$work/kept" | sed -e 's|^\.$||' -e 's|^|./|' >"$work/out"
if [ ! -s "$work/out" ]; then
  echo "gosec_packages: no non-ignored package for $*" >&2
  exit 1
fi
git -C "$root" ls-files -z --others --ignored --exclude-standard -- '*.go' >"$work/ignored-files"
tr '\0' '\n' <"$work/ignored-files" >"$work/ignored"
remaining="$(wc -l <"$work/ignored")"
status=0
while [ "$remaining" -gt 0 ] && IFS= read -r ignored; do
  remaining=$((remaining - 1))
  case "$ignored" in
    */*) dir="./${ignored%/*}" ;;
    *) dir=./ ;;
  esac
  if grep -Fxq "$dir" "$work/out"; then
    echo "gosec would load git-ignored $ignored; move or delete it" >&2
    status=1
  fi
done <"$work/ignored"
[ "$status" -eq 0 ] || exit "$status"
cat "$work/out"
