#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Holds a commit on main to the commit-msg policy every pull request commit passes. The commits
# job checks the commits of a pull request, never the squash commit GitHub writes from the body
# the maintainer gives, so a squash that dropped the Signed-off-by trailer (main c2c711e6, #1007)
# reached main green. CI runs this on every push to main; the landing helper squash_merge.sh is
# the barrier before the merge, this one turns main red when it was bypassed.
#
#   tools/ci/main_commit_check.sh <commit>|<before>..<after>
#
# Run the policy first. Only failing commits from the two listed bot accounts, committed by
# GitHub, are exempt. Ranges check up to 200 non-merge commits; a zero or non-ancestor before
# checks only after (new branch or rewritten history).
# Exit status: 0 passed or allowed bot, 1 refused, 2 usage. Cases: squash_merge_test.sh.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

[ "$#" -eq 1 ] || { echo "usage: ${0##*/} <commit>|<before>..<after>" >&2; exit 2; }
case "$1" in '' | -*) echo "main_commit_check: invalid commit or range" >&2; exit 2 ;; esac
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
umask 077
work="$(mktemp -d)"
trap 'rm -rf "$work"' 0
trap 'exit 1' HUP INT TERM

# check_commit COMMIT: apply the policy, then allow only GitHub-committed bot exceptions.
check_commit() {
  git -C "$ROOT" log -1 --format=%B "$1" >"$work/msg" || return 1
  if sh "$ROOT/.config/hooks/commit-msg.sh" "$work/msg" >"$work/policy" 2>&1; then
    echo "main_commit_check: $1 passes the commit-msg policy"
    return 0
  fi
  author="$(git -C "$ROOT" log -1 --format=%ae "$1")" || return 1
  committer="$(git -C "$ROOT" log -1 --format=%ce "$1")" || return 1
  case "$author" in
    '29139614+renovate[bot]@users.noreply.github.com' | '340302165+snatcharr-release[bot]@users.noreply.github.com')
      if [ "$committer" = noreply@github.com ]; then
        echo "main_commit_check: $1 has an allowed bot author ($author) and GitHub committer; no DCO to check"
        return 0
      fi
      ;;
  esac
  cat "$work/policy" >&2
  echo "main_commit_check: $1 fails the commit-msg policy; land through tools/ci/squash_merge.sh, never rewrite main" >&2
  return 1
}

case "$1" in
  *..*)
    before="${1%%..*}"
    after="${1#*..}"
    after="$(git -C "$ROOT" rev-parse --verify "$after^{commit}")" || exit 1
    if [ "$before" = 0000000000000000000000000000000000000000 ] ||
      ! git -C "$ROOT" merge-base --is-ancestor "$before" "$after"; then
      echo "main_commit_check: before is zero or not an ancestor; checking only $after"
      printf '%s\n' "$after" >"$work/commits"
    else
      git -C "$ROOT" rev-list --no-merges --max-count=201 "$before..$after" >"$work/commits" || exit 1
      if [ "$(wc -l <"$work/commits")" -gt 200 ]; then
        echo "main_commit_check: range too long (more than 200 commits)" >&2
        exit 1
      fi
    fi
    ;;
  *) git -C "$ROOT" rev-parse --verify "$1^{commit}" >"$work/commits" || exit 1 ;;
esac
status=0
count=0
while [ "$count" -lt 200 ] && IFS= read -r commit; do
  count=$((count + 1))
  check_commit "$commit" || status=1
done <"$work/commits"
exit "$status"
