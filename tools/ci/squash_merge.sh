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
#   rc 2  usage, a non-canonical <pr> number, or no git user.name / user.email to sign off with
#   rc 3  the body's last non-blank line is not "Signed-off-by: <user.name> <user.email>"
#   rc 4  subject plus body fail .config/hooks/commit-msg.sh, or the subject lacks "(#<pr>)"
#   rc 5  mergeStateStatus is not CLEAN, or the head moved from <head-sha>
#   rc 6  merged, but the commit on main does not carry the trailer
#   rc 7  merge failed and the pull request is not confirmed MERGED
#   rc 8  merged, but the main commit read-back failed
#   rc 9  origin is missing or is not github.com/<REPO>, or tools/ci/guard_definition_check.sh
#         refuses the squash message: guard changed without GUARD_DEFINITION_ACK,
#         refs/pull/<pr>/merge merges a head other than <head-sha>, or the check could not run
#
# The guard check runs here as well as from lefthook commit-msg: the squash commit is built on
# the forge, so no local commit-msg hook sees its message. GUARD_DEFINITION_ACK passes through.
#
# GH names the forge client (default gh), for the planted cases in squash_merge_test.sh.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

GH="${GH:-gh}"
REPO="${SQUASH_MERGE_REPO:-lusoris/SnatchArr}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# usage: print the call shape and exit 2.
usage() {
  echo "usage: ${0##*/} <pr> <head-sha> <subject> <body-file> [--dry-run]" >&2
  exit 2
}

# check_origin: exit 9 unless the origin remote of ROOT names REPO on github.com.
check_origin() {
  if ! origin="$(git -C "$ROOT" remote get-url origin 2>/dev/null)" || [ -z "$origin" ]; then
    echo "refusing: no origin remote; the guard check would have no github.com/$REPO remote to read" >&2
    exit 9
  fi
  origin_lc="$(printf '%s' "$origin" | LC_ALL=C tr '[:upper:]' '[:lower:]')"
  repo_lc="$(printf '%s' "$REPO" | LC_ALL=C tr '[:upper:]' '[:lower:]')"
  case "$origin_lc" in
    "https://github.com/$repo_lc" | "https://github.com/$repo_lc.git" | \
      "git@github.com:$repo_lc" | "git@github.com:$repo_lc.git" | \
      "ssh://git@github.com/$repo_lc" | "ssh://git@github.com/$repo_lc.git") ;;
    *)
      echo "refusing: origin '$origin' is not github.com/$REPO; the guard check would read another repository" >&2
      exit 9
      ;;
  esac
}

[ "$#" -eq 4 ] || [ "$#" -eq 5 ] || usage
pr="$1" sha="$2" subject="$3" body="$4" dry="${5:-}"
case "$pr" in '' | *[!0-9]*) usage ;; esac
# Match PR_SUBJECT_RE in tools/ci/guard_definition_check.sh so every accepted <pr> runs the guard.
printf '%s' "$pr" | grep -Eq '^[1-9][0-9]{0,6}$' || {
  echo "refusing: <pr> must be a pull request number of 1 to 7 digits without a leading zero, got '$pr'" >&2
  exit 2
}
# An empty head (a failed gh read) would make --match-head-commit a no-op.
printf '%s' "$sha" | grep -Eq '^[0-9a-f]{40}$' || {
  echo "refusing: head-sha must be 40 hex characters, got '$sha'" >&2
  exit 2
}
[ -z "$dry" ] || [ "$dry" = "--dry-run" ] || usage
[ -f "$body" ] || { echo "refusing: no body file $body" >&2; exit 2; }

# Snapshot before validation: the caller may change its file while gh reads the PR.
umask 077
work="$(mktemp -d)"
trap 'rm -rf "$work"' 0
trap 'exit 1' HUP INT TERM
body="$(mktemp "$work/body.XXXXXX")"
cat <"$4" >"$body"

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
{ printf '%s\n\n' "$subject"; cat "$body"; } >"$work/msg"
sh "$ROOT/.config/hooks/commit-msg.sh" "$work/msg" || exit 4
check_origin
(cd "$ROOT" && GUARD_DEFINITION_HEAD="$sha" bash tools/ci/guard_definition_check.sh "$work/msg") || exit 9

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

if "$GH" pr merge "$pr" -R "$REPO" --squash --match-head-commit "$sha" \
  --subject "$subject" --body-file "$body"; then
  merged="$("$GH" pr view "$pr" -R "$REPO" --json mergeCommit --jq '.mergeCommit.oid')" || {
    echo "merged as unknown, read-back failed: check main_commit_check" >&2
    exit 8
  }
else
  view="$("$GH" pr view "$pr" -R "$REPO" --json state,mergeCommit \
    --jq '[.state, .mergeCommit.oid] | @tsv')" || {
    echo "merge failed: #$pr state unreadable" >&2
    exit 7
  }
  state="$(printf '%s' "$view" | cut -f1)"
  [ "$state" = MERGED ] || { echo "merge failed: #$pr state '$state'" >&2; exit 7; }
  merged="$(printf '%s' "$view" | cut -f2)"
  echo "merge completed despite the client error: #$pr as $merged"
fi
message="$("$GH" api "repos/$REPO/commits/$merged" --jq '.commit.message')" || {
  echo "merged as $merged, read-back failed: check main_commit_check" >&2
  exit 8
}
if ! printf '%s\n' "$message" | grep -Fxq "$trailer"; then
  echo "refusing: main $merged lacks '$trailer'; report it, never rewrite main" >&2
  exit 6
fi
echo "merged #$pr as $merged with '$trailer'"
