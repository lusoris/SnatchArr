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
#   tools/ci/main_commit_check.sh <commit>
#
# A bot author (release-please, Renovate) runs no commit-msg hook and cannot certify a DCO; its
# commit is reported and passes, as the commits job skips bot pull requests.
# Exit status: 0 passed or bot author, 1 refused, 2 usage. Cases: squash_merge_test.sh.
set -eu

[ "$#" -eq 1 ] || { echo "usage: ${0##*/} <commit>" >&2; exit 2; }
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
email="$(git -C "$ROOT" log -1 --format=%ae "$1")"
case "$email" in
  *"[bot]@users.noreply.github.com")
    echo "main_commit_check: $1 has a bot author ($email); no DCO to check"
    exit 0
    ;;
esac
msg="$(mktemp)"
trap 'rm -f "$msg"' EXIT
git -C "$ROOT" log -1 --format=%B "$1" >"$msg"
if ! sh "$ROOT/.config/hooks/commit-msg.sh" "$msg"; then
  echo "main_commit_check: $1 fails the commit-msg policy; land through tools/ci/squash_merge.sh, never rewrite main" >&2
  exit 1
fi
echo "main_commit_check: $1 passes the commit-msg policy"
