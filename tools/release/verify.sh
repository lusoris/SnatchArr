#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Verifies one published artifact the way a consumer does, with cosign alone and no forge
# login. The signing certificate must name a release workflow of the given repository, run on
# the given git ref at the given commit; an artifact built from any other commit is refused.
# Images and the chart are signed by release-build.yml after 0.2.1 (#42) and by release.yml
# before; the files of the release page by release.yml. Both are accepted, no other workflow.
#
#   tools/release/verify.sh <oci reference> <owner/repository> <git ref> <commit>
#   tools/release/verify.sh --bundle <bundle> <file> <owner/repository> <git ref> <commit>
#
# An image or the Helm chart in the registry needs a cosign signature and a build provenance
# attestation; both checks always run, so one call reports everything that is missing. A file
# from the release page needs the provenance bundle published next to it, which must name the
# file's digest as one of its subjects.
#
#   tools/release/verify.sh ghcr.io/lusoris/charts/snatcharr:0.1.10 lusoris/SnatchArr \
#     refs/tags/v0.1.10 <commit of v0.1.10>
#
# Exit status: 0 verified, 2 usage, 3 signature refused, 4 attestation refused, 5 both refused.
# Needs cosign 2.4 or later on the PATH; POSIX sh (on Windows: Git Bash or WSL).
set -eu

usage() {
  echo "usage: $0 <oci reference> <owner/repository> <git ref> <commit>" >&2
  echo "       $0 --bundle <bundle> <file> <owner/repository> <git ref> <commit>" >&2
  exit 2
}

bundle=
if [ "${1:-}" = --bundle ]; then
  [ "$#" -eq 6 ] || usage
  bundle=$2
  shift 2
fi
[ "$#" -eq 4 ] || usage
subject=$1
repository=$2
git_ref=$3
commit=$4
for argument in "$subject" "$repository" "$git_ref"; do
  [ -n "$argument" ] || usage
done
case "$commit" in
  *[!0-9a-f]*) usage ;;
esac
[ "${#commit}" -eq 40 ] || usage

# quote <text>: <text> with every character a regular expression reads as syntax escaped.
quote() {
  printf '%s' "$1" | sed 's/[][\\.*+?^$(){}|]/\\&/g'
}

identity="https://github.com/$repository/.github/workflows/release(-build)?.yml@$git_ref"
identity_regexp="^https://github\\.com/$(quote "$repository")/\\.github/workflows/release(-build)?\\.yml@$(quote "$git_ref")\$"
provenance=https://slsa.dev/provenance/v1
log=$(mktemp)
trap 'rm -f "$log"' EXIT

# refuse <what>: reports one refused check with the last line cosign printed.
refuse() {
  echo "$subject: $1 refused for $identity at commit $commit: $(tail -n 1 "$log")" >&2
}

# cosign_check <subcommand> <arguments...>: runs one cosign check against the identity and commit.
cosign_check() {
  subcommand=$1
  shift
  cosign "$subcommand" "$@" \
    --certificate-identity-regexp "$identity_regexp" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    --certificate-github-workflow-sha "$commit" >/dev/null 2>"$log"
}

refused=0
if [ -n "$bundle" ]; then
  if ! cosign_check verify-blob-attestation --new-bundle-format --bundle "$bundle" \
    --type "$provenance" "$subject"; then
    refuse "build provenance in $bundle"
    exit 4
  fi
  echo "$subject: build provenance verified ($identity, commit $commit)"
  exit 0
fi

if ! cosign_check verify "$subject"; then
  refuse "cosign signature"
  refused=3
fi
if ! cosign_check verify-attestation --new-bundle-format --type "$provenance" "$subject"; then
  refuse "build provenance attestation"
  if [ "$refused" -eq 3 ]; then refused=5; else refused=4; fi
fi
if [ "$refused" -ne 0 ]; then
  exit "$refused"
fi
echo "$subject: signature and build provenance verified ($identity, commit $commit)"
