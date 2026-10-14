#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Fetches the pinned gitleaks release into CACHE_DIR, checks its digest and version, then runs
# secret_scan.py: its planted self-test first, then the scan of the history. CI (job
# secret-scan) and `make secret-scan` both call this script, so the pin lives here only.
#
#   tools/ci/gitleaks_scan.sh CACHE_DIR
#
# Locally the scan reads every ref of the clone, so a finding on a local branch that was never
# pushed blocks as well. Exit status: 0 no blocking finding, 1 otherwise.
set -eu

GITLEAKS_VERSION=8.30.1
GITLEAKS_SHA256=551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb

cache="${1:?usage: gitleaks_scan.sh CACHE_DIR}"
HERE="$(cd "$(dirname "$0")" && pwd)"

# The pin is the linux x64 archive, the platform of every CI runner. Elsewhere the scan is
# skipped and says so; CI still runs it on every pull request.
platform="$(uname -s)/$(uname -m)"
if [ "$platform" != "Linux/x86_64" ]; then
	echo "secret-scan: skipped on $platform: the gitleaks pin is the linux x64 archive (CI runs the scan)"
	exit 0
fi

mkdir -p "$cache"
archive="$cache/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz"
if [ ! -f "$archive" ]; then
	release="https://github.com/gitleaks/gitleaks/releases/download"
	curl --fail --silent --show-error --location --proto '=https' --max-time 60 \
		--output "$archive.part" "$release/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz"
	mv "$archive.part" "$archive"
fi
# Checked on every run, so a cached archive that changed is refused as well.
echo "$GITLEAKS_SHA256  $archive" | sha256sum --check --quiet
bindir="$cache/gitleaks-$GITLEAKS_VERSION"
mkdir -p "$bindir"
tar -xzf "$archive" -C "$bindir" gitleaks
gitleaks="$bindir/gitleaks"
version="$("$gitleaks" version)"
if [ "$version" != "$GITLEAKS_VERSION" ]; then
	echo "secret-scan: gitleaks reports version $version, want $GITLEAKS_VERSION" >&2
	exit 1
fi

python3 -I "$HERE/secret_scan.py" --gitleaks "$gitleaks" --self-test
python3 -I "$HERE/secret_scan.py" --gitleaks "$gitleaks"
