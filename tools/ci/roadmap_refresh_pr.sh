#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Proposes a refreshed docs/roadmap.md as a bot pull request. Runs after
# `roadmap_render.py --write` in the scheduled Roadmap refresh workflow, on a checkout of the
# default branch at BASE_SHA.
#
# When the rewrite changed the file, it writes the file through the contents API onto a temporary
# branch at BASE_SHA (GitHub signs that commit), points the bot branch at the new commit, deletes
# the temporary branch, and opens the pull request unless the bot's own one is open. The bot
# branch never points at a commit already on the base branch, so an open pull request is never
# marked merged without its change. When the file did not change, it closes the bot's open pull
# request, whose counts are then stale.
#
#   REPO=owner/name BASE_SHA=<sha> BASE_BRANCH=<default branch> [RUN_ID=<id>] \
#     [DOC=docs/roadmap.md] [BRANCH=bot/roadmap-refresh] [DRY_RUN=1] tools/ci/roadmap_refresh_pr.sh
#
# DRY_RUN=1 runs the reads and prints each write instead of sending it.
# Exit status: 0 nothing to do or the pull request proposed, 1 a call failed, 2 bad input.
set -eu

doc="${DOC:-docs/roadmap.md}"
branch="${BRANCH:-bot/roadmap-refresh}"
title="docs(roadmap): refresh the milestone open-issue counts"
repo="${REPO:-}"
base="${BASE_SHA:-}"
base_branch="${BASE_BRANCH:-}"
tmp="$branch-tmp-${RUN_ID:-$$}"

case "$repo" in
  */*) ;;
  *) echo "roadmap_refresh: REPO must be owner/name" >&2; exit 2 ;;
esac
[ -n "$base" ] || { echo "roadmap_refresh: BASE_SHA is empty" >&2; exit 2; }
[ -n "$base_branch" ] || { echo "roadmap_refresh: BASE_BRANCH is empty" >&2; exit 2; }

# send runs a write; a dry run prints it on stderr and stands in a placeholder for its output.
send() {
  if [ -n "${DRY_RUN:-}" ]; then
    echo "dry-run: gh $*" >&2
    echo dry-run
  else
    gh "$@"
  fi
}

# Only the bot's own pull request: head in this repository, into the base branch. A fork pull
# request from a branch of the same name does not match.
open="$(gh api "repos/$repo/pulls?head=${repo%%/*}:$branch&base=$base_branch&state=open" \
  --jq '.[0].number // empty')" || exit 1

if git diff --quiet -- "$doc"; then
  if [ -n "$open" ]; then
    send pr close "$open" --repo "$repo" \
      --comment "The milestone table on $base_branch matches the open milestones again; this refresh is no longer needed." \
      >/dev/null || exit 1
    echo "roadmap_refresh: $doc unchanged, stale pull request #$open closed"
  else
    echo "roadmap_refresh: $doc unchanged, no pull request"
  fi
  exit 0
fi

blob="$(git rev-parse "$base:$doc")" || exit 1
content="$(base64 <"$doc" | tr -d '\n')"

# drop_tmp deletes the temporary branch the file was written on.
drop_tmp() {
  send api -X DELETE "repos/$repo/git/refs/heads/$tmp" >/dev/null
}

# A rerun attempt may find the temporary branch an earlier attempt left; reset it to the base.
send api -X POST "repos/$repo/git/refs" -f "ref=refs/heads/$tmp" -f "sha=$base" >/dev/null \
  || send api -X PATCH "repos/$repo/git/refs/heads/$tmp" -f "sha=$base" -F force=true >/dev/null \
  || exit 1
commit="$(send api -X PUT "repos/$repo/contents/$doc" -f "message=$title" -f "branch=$tmp" \
  -f "sha=$blob" -f "content=$content" --jq .commit.sha)" || { drop_tmp; exit 1; }
[ -n "$commit" ] || { echo "roadmap_refresh: contents API returned no commit" >&2; drop_tmp; exit 1; }

if gh api "repos/$repo/git/ref/heads/$branch" --jq .object.sha >/dev/null 2>&1; then
  send api -X PATCH "repos/$repo/git/refs/heads/$branch" -f "sha=$commit" -F force=true >/dev/null \
    || { drop_tmp; exit 1; }
else
  send api -X POST "repos/$repo/git/refs" -f "ref=refs/heads/$branch" -f "sha=$commit" >/dev/null \
    || { drop_tmp; exit 1; }
fi
drop_tmp || exit 1

if [ -n "$open" ]; then
  echo "roadmap_refresh: branch $branch updated, pull request #$open already open"
  exit 0
fi
send pr create --repo "$repo" --base "$base_branch" --head "$branch" --title "$title" \
  --body "The scheduled Roadmap refresh workflow re-rendered the milestone table in $doc from the open GitHub milestones (tools/ci/roadmap_render.py --write)." \
  >/dev/null || exit 1
echo "roadmap_refresh: pull request proposed from $branch"
