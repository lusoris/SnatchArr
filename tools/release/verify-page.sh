#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Verifies the files of one release page, downloaded into a directory, with cosign alone and
# no forge login. The directory must hold exactly the files the checksum file lists, the
# checksum file and the provenance bundle; every listed file must match its checksum, and
# every file, the checksum file included, must be a subject of the bundle signed by the
# release workflow at the given commit (tools/release/verify.sh --bundle).
#
#   gh release download v0.1.10 --repo lusoris/SnatchArr --dir page   # or the web page
#   tools/release/verify-page.sh page 0.1.10 lusoris/SnatchArr refs/tags/v0.1.10 <commit>
#
# Exit status: 0 verified, 2 usage, 4 a file is unlisted, mismatched or not attested.
set -eu

if [ "$#" -ne 5 ] || [ ! -d "$1" ] || [ -z "$2" ]; then
  echo "usage: $0 <directory> <version> <owner/repository> <git ref> <commit>" >&2
  exit 2
fi
directory=$1
version=$2
shift 2
verify=$(cd "$(dirname "$0")" && pwd)/verify.sh
sums=snatcharr-$version-SHA256SUMS
bundle=snatcharr-$version-provenance.sigstore.json
cd "$directory"

for required in "$sums" "$bundle"; do
  if [ ! -f "$required" ]; then
    echo "$directory: $required is missing" >&2
    exit 4
  fi
done
# check_sums <file>: sha256sum where it exists (Linux, Git Bash), shasum elsewhere (macOS).
check_sums() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum --check --strict --quiet -- "$1"
  else
    shasum -a 256 --check --strict --quiet "$1"
  fi
}
if ! check_sums "$sums"; then
  echo "$directory: a file does not match $sums" >&2
  exit 4
fi

listed=$(awk '{ print $2 }' "$sums" | sort)
present=$(find . -maxdepth 1 -type f ! -name "$sums" ! -name "$bundle" | sed 's|^\./||' | sort)
if [ "$listed" != "$present" ]; then
  echo "$directory: files present differ from $sums (listed: $listed; present: $present)" >&2
  exit 4
fi

refused=0
for file in $listed "$sums"; do
  "$verify" --bundle "$bundle" "$file" "$@" || refused=4
done
exit "$refused"
