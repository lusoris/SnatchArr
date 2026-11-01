#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Proposes a refreshed docs/roadmap.md as a bot pull request. Runs after
# `roadmap_render.py --write` in the scheduled Roadmap refresh workflow: when the rewrite changed
# the file, it points the bot branch at the base commit, writes the file there through the
# contents API (GitHub signs that commit), and opens the pull request unless one is open.
#
#   REPO=owner/name BASE_SHA=<sha> [DOC=docs/roadmap.md] [BRANCH=bot/roadmap-refresh] \
#     [DRY_RUN=1] tools/ci/roadmap_refresh_pr.sh
#
# DRY_RUN=1 runs the reads and prints each write instead of sending it.
# Exit status: 0 nothing to do or the pull request proposed, 1 a call failed, 2 bad input.
set -eu

doc="${DOC:-docs/roadmap.md}"
branch="${BRANCH:-bot/roadmap-refresh}"
title="docs(roadmap): refresh the milestone open-issue counts"
repo="${REPO:-}"
base="${BASE_SHA:-}"

case "$repo" in
  */*) ;;
  *) echo "roadmap_refresh: REPO must be owner/name" >&2; exit 2 ;;
esac
[ -n "$base" ] || { echo "roadmap_refresh: BASE_SHA is empty" >&2; exit 2; }

if git diff --quiet -- "$doc"; then
  echo "roadmap_refresh: $doc unchanged, no pull request"
  exit 0
fi

write() {
  if [ -n "${DRY_RUN:-}" ]; then
    echo "dry-run: gh $*"
  else
    gh "$@" >/dev/null
  fi
}

blob="$(git rev-parse "$base:$doc")" || exit 1
content="$(base64 <"$doc" | tr -d '\n')"

if gh api "repos/$repo/git/ref/heads/$branch" --jq .object.sha >/dev/null 2>&1; then
  write api -X PATCH "repos/$repo/git/refs/heads/$branch" -f "sha=$base" -F force=true || exit 1
else
  write api -X POST "repos/$repo/git/refs" -f "ref=refs/heads/$branch" -f "sha=$base" || exit 1
fi

write api -X PUT "repos/$repo/contents/$doc" -f "message=$title" -f "branch=$branch" \
  -f "sha=$blob" -f "content=$content" || exit 1

open="$(gh pr list --repo "$repo" --head "$branch" --state open --json number --jq '.[0].number // empty')" \
  || exit 1
if [ -n "$open" ]; then
  echo "roadmap_refresh: branch $branch updated, pull request #$open already open"
  exit 0
fi
write pr create --repo "$repo" --base main --head "$branch" --title "$title" \
  --body "The scheduled Roadmap refresh workflow re-rendered the milestone table in $doc from the open GitHub milestones (tools/ci/roadmap_render.py --write)." \
  || exit 1
echo "roadmap_refresh: pull request proposed from $branch"
