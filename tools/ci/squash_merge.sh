#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# The landing path for a maintainer pull request: squash-merges it only when the squash commit
# will pass the commit-msg policy that every pull request commit already passes.
#
#   tools/ci/squash_merge.sh <pr> <head-sha> <subject> <body-file> [--dry-run]
#
# gh pr merge --body-file replaces the body GitHub builds from the squashed commits, so a body
# written by hand drops their Signed-off-by trailers (main c2c711e6, #1007). This script refuses
# such a body instead of merging it:
#
#   rc 2  usage, or no git user.name / user.email to sign off with
#   rc 3  the body's last non-blank line is not "Signed-off-by: <user.name> <user.email>"
#   rc 4  subject plus body fail .config/hooks/commit-msg.sh, or the subject lacks "(#<pr>)"
#   rc 5  mergeStateStatus is not CLEAN, or the head moved from <head-sha>
#   rc 6  merged, but the commit on main does not carry the trailer
#
# GH names the forge client (default gh), for the planted cases in squash_merge_test.sh.
set -eu

GH="${GH:-gh}"
REPO="${SQUASH_MERGE_REPO:-lusoris/SnatchArr}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# usage: print the call shape and exit 2.
usage() {
  echo "usage: ${0##*/} <pr> <head-sha> <subject> <body-file> [--dry-run]" >&2
  exit 2
}

[ "$#" -eq 4 ] || [ "$#" -eq 5 ] || usage
pr="$1" sha="$2" subject="$3" body="$4" dry="${5:-}"
case "$pr" in '' | *[!0-9]*) usage ;; esac
# An empty head (a failed gh read) would make --match-head-commit a no-op.
printf '%s' "$sha" | grep -Eq '^[0-9a-f]{40}$' || {
  echo "refusing: head-sha must be 40 hex characters, got '$sha'" >&2
  exit 2
}
[ -z "$dry" ] || [ "$dry" = "--dry-run" ] || usage
[ -f "$body" ] || { echo "refusing: no body file $body" >&2; exit 2; }

if ! name="$(git -C "$ROOT" config user.name)" || ! email="$(git -C "$ROOT" config user.email)" ||
  [ -z "$name" ] || [ -z "$email" ]; then
  echo "refusing: git config user.name and user.email are required for the Signed-off-by trailer" >&2
  exit 2
fi
trailer="Signed-off-by: $name <$email>"

last="$(sed -n '/^[[:space:]]*$/!p' "$body" | tail -n 1)"
if [ "$last" != "$trailer" ]; then
  echo "refusing: the squash body must end with '$trailer'" >&2
  echo "  last line: $last" >&2
  exit 3
fi

case "$subject" in
  *" (#$pr)") ;;
  *) echo "refusing: the subject must end with ' (#$pr)'" >&2; exit 4 ;;
esac
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
{ printf '%s\n\n' "$subject"; cat "$body"; } >"$work/msg"
sh "$ROOT/.config/hooks/commit-msg.sh" "$work/msg" || exit 4

view="$("$GH" pr view "$pr" -R "$REPO" --json mergeStateStatus,headRefOid \
  --jq '[.mergeStateStatus, .headRefOid] | @tsv')" || {
  echo "refusing: #$pr state unreadable" >&2
  exit 5
}
state="$(printf '%s' "$view" | cut -f1)"
head="$(printf '%s' "$view" | cut -f2)"
[ "$state" = CLEAN ] || { echo "refusing: #$pr mergeStateStatus '$state', merge needs CLEAN" >&2; exit 5; }
[ "$head" = "$sha" ] || { echo "refusing: #$pr head is $head, not $sha" >&2; exit 5; }

if [ "$dry" = "--dry-run" ]; then
  echo "dry-run: #$pr would squash at $sha with the body ending '$trailer'"
  exit 0
fi

"$GH" pr merge "$pr" -R "$REPO" --squash --match-head-commit "$sha" \
  --subject "$subject" --body-file "$body"
merged="$("$GH" pr view "$pr" -R "$REPO" --json mergeCommit --jq '.mergeCommit.oid')"
message="$("$GH" api "repos/$REPO/commits/$merged" --jq '.commit.message')"
if ! printf '%s\n' "$message" | grep -Fxq "$trailer"; then
  echo "refusing: main $merged lacks '$trailer'; report it, never rewrite main" >&2
  exit 6
fi
echo "merged #$pr as $merged with '$trailer'"
