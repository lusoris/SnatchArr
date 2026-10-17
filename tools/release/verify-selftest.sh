#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Proves that tools/release/verify.sh can refuse: it runs the check against the published
# 0.1.4 artifacts, whose state is known and fixed by digest, and compares each exit status.
# The 0.1.4 chart is signed but was never attested (#163), so it is the standing negative
# case for the attestation; the commit of the 0.1.3 tag is the standing wrong commit. The
# release workflow runs this before it verifies a new release.
#
#   tools/release/verify-selftest.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs cosign and network.
set -eu

verify=$(dirname "$0")/verify.sh
repository=lusoris/SnatchArr
api=ghcr.io/lusoris/snatcharr-api@sha256:fca09d40398d7366b39b517072ec4c8c841ee68bb9c32c00c428dbd4c027f8f1
chart=ghcr.io/lusoris/charts/snatcharr@sha256:6a656c89678deb07277fa072c4715fa1f4e834d1b6681a6663bb5a7cfac583a8
commit=4dada773f3d7e2388cc45d069b901406d8d61a40
other_commit=4fbdc4c1522d4626f8ceaa270f287c86e8464325
failed=0

# expect <status> <case> <arguments of verify.sh...>
expect() {
  want=$1
  name=$2
  shift 2
  got=0
  "$verify" "$@" >/dev/null 2>&1 || got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: exit $got, want $want" >&2
    failed=1
    return 0
  fi
  echo "ok   $name: exit $got"
}

expect 0 "signed and attested image is accepted" "$api" "$repository" refs/tags/v0.1.4 "$commit"
expect 4 "signed chart without an attestation is refused" "$chart" "$repository" refs/tags/v0.1.4 "$commit"
expect 5 "signature and attestation of another commit are refused" "$api" "$repository" refs/tags/v0.1.4 "$other_commit"
expect 5 "signature and attestation of another git ref are refused" "$api" "$repository" refs/tags/v0.1.3 "$commit"
expect 5 "signature and attestation of another repository are refused" "$api" lusoris/other refs/tags/v0.1.4 "$commit"
# The identity is a regular expression (release.yml or release-build.yml): it must be anchored
# at the end, and a dot in the given git ref must match only a dot (v0.1.. would match v0.1.4).
expect 5 "a prefix of the signed git ref is refused" "$api" "$repository" refs/tags/v0.1 "$commit"
expect 5 "a git ref whose dots would match any character is refused" "$api" "$repository" refs/tags/v0.1.. "$commit"
expect 2 "missing commit is a usage error" "$api" "$repository" refs/tags/v0.1.4
expect 2 "abbreviated commit is a usage error" "$api" "$repository" refs/tags/v0.1.4 4dada773
exit "$failed"
