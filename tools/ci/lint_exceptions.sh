#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Validates the generated exceptions block in .golangci.yml against a golangci-lint
# JSON report using praetorctl via the pinned engine. Fails on new findings, on
# expired entries, or if the generated block is stale.
#
#   tools/ci/lint_exceptions.sh <config> <report> <issue>
#
# Exit status: 0 valid and current, 1 refused or stale, 2 usage.
set -eu

if [ "$#" -ne 3 ]; then
  echo "usage: ${0##*/} <config> <report> <issue>" >&2
  exit 2
fi

config="$1"
report="$2"
issue="$3"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT HUP INT TERM

cp "$config" "$tmp"

engine="${PRAETOR_ENGINE:-sh .config/lefthook/engine.sh}"
today_flags=""
if [ -n "${LINT_EXCEPTIONS_TODAY:-}" ]; then
  today_flags="--today $LINT_EXCEPTIONS_TODAY"
fi

rc=0
# shellcheck disable=SC2086
$engine linters exceptions generate --config "$tmp" --report "$report" --issue "$issue" --category A --write $today_flags >/dev/null || rc=$?

if [ "$rc" -ne 0 ]; then
  echo "lint_exceptions: generate refused (rc $rc)" >&2
  exit 1
fi

if ! cmp -s "$config" "$tmp"; then
  cmd="$engine linters exceptions generate --config $config --report $report --issue $issue --category A --write"
  if [ -n "$today_flags" ]; then
    cmd="$cmd $today_flags"
  fi
  echo "lint_exceptions: generated block is stale; run: $cmd" >&2
  if diff -u "$config" "$tmp" >&2; then
    echo "lint_exceptions: cmp found a difference that diff does not show" >&2
  fi
  exit 1
fi

count=$(awk '
  /^[[:space:]]*#[[:space:]]*praetor:generated-exceptions begin/,/^[[:space:]]*#[[:space:]]*praetor:generated-exceptions end/ {
    if ($0 ~ /^[[:space:]]*- path:/) n++
  }
  END { print n + 0 }
' "$config")

echo "lint_exceptions: $count exception(s) hold"
