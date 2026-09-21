#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# commit-msg hook: Conventional Commits subject + DCO Signed-off-by trailer.
# Usage: sh .config/hooks/commit-msg.sh <path-to-COMMIT_EDITMSG>
set -eu

msgfile="$1"
subject="$(sed -e '/^#/d' "$msgfile" | sed -n '1p')"

if ! printf '%s' "$subject" | grep -Eq '^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9/_-]+\))?!?: .{1,72}$'; then
  echo "commit-msg: subject must be a Conventional Commit (type(scope)?: summary <= 72 chars)" >&2
  echo "  got: $subject" >&2
  exit 1
fi

if ! sed -e '/^#/d' "$msgfile" | grep -Eq '^Signed-off-by: .+ <.+@.+>$'; then
  echo "commit-msg: missing DCO trailer; commit with 'git commit -s'" >&2
  exit 1
fi
