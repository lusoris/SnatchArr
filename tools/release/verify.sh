#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Verifies one published artifact (an image or the Helm chart) the way a consumer does: its
# cosign signature and its build provenance attestation must both come from the release
# workflow of the given repository, run on the given git ref.
#
#   tools/release/verify.sh <oci reference> <owner/repository> <git ref>
#   tools/release/verify.sh ghcr.io/lusoris/charts/snatcharr:0.1.5 lusoris/SnatchArr refs/tags/v0.1.5
#
# Both checks always run, so one call reports everything that is missing. Exit status:
# 0 verified, 2 usage, 3 signature refused, 4 attestation refused, 5 both refused.
# Needs cosign and gh on the PATH; POSIX sh (on Windows: Git Bash or WSL).
set -eu

if [ "$#" -ne 3 ] || [ -z "$1" ] || [ -z "$2" ] || [ -z "$3" ]; then
  echo "usage: $0 <oci reference> <owner/repository> <git ref>" >&2
  exit 2
fi
reference=$1
repository=$2
git_ref=$3
workflow="$repository/.github/workflows/release.yml"
issuer=https://token.actions.githubusercontent.com

refused=0
if ! cosign verify "$reference" \
  --certificate-identity "https://github.com/$workflow@$git_ref" \
  --certificate-oidc-issuer "$issuer" >/dev/null; then
  echo "$reference: no cosign signature from $workflow@$git_ref" >&2
  refused=3
fi
if ! gh attestation verify "oci://$reference" --repo "$repository" \
  --signer-workflow "$workflow" --source-ref "$git_ref" >/dev/null; then
  echo "$reference: no build provenance attestation from $workflow@$git_ref" >&2
  if [ "$refused" -eq 3 ]; then refused=5; else refused=4; fi
fi
if [ "$refused" -ne 0 ]; then
  exit "$refused"
fi
echo "$reference: signature and build provenance verified ($workflow@$git_ref)"
