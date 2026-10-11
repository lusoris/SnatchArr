#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Proves that tools/release/verify.sh can refuse: it runs the check against the published
# 0.1.4 artifacts, whose state is known and fixed by digest, and compares each exit status.
# The 0.1.4 chart is signed but was never attested (#163), so it is the standing negative
# case for the attestation. The release workflow runs this before it verifies a new release.
#
#   tools/release/verify-selftest.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs cosign, gh and network.
set -eu

verify=$(dirname "$0")/verify.sh
repository=lusoris/SnatchArr
api=ghcr.io/lusoris/snatcharr-api@sha256:fca09d40398d7366b39b517072ec4c8c841ee68bb9c32c00c428dbd4c027f8f1
chart=ghcr.io/lusoris/charts/snatcharr@sha256:6a656c89678deb07277fa072c4715fa1f4e834d1b6681a6663bb5a7cfac583a8
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

expect 0 "signed and attested image is accepted" "$api" "$repository" refs/tags/v0.1.4
expect 4 "signed chart without an attestation is refused" "$chart" "$repository" refs/tags/v0.1.4
expect 5 "signature and attestation of another git ref are refused" "$api" "$repository" refs/tags/v0.1.3
expect 5 "signature and attestation of another repository are refused" "$api" lusoris/other refs/tags/v0.1.4
expect 2 "missing argument is a usage error" "$api" "$repository"
exit "$failed"
