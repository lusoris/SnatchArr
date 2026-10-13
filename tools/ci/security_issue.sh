#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Opens the issue "Security Scan fails on main", or comments on it while it is open, so that a
# failed nightly or post-merge Security Scan reaches the issue list and not only the run email
# (gap audit A2-02). The job "report" of security.yml calls it after a failed scan on main.
#
#   tools/ci/security_issue.sh RUN_URL
#
# Exit status: 0 issue opened or commented, 1 the forge call failed, 2 usage.
# Needs gh with GH_TOKEN (issues: write) and jq. GH names another gh, for the planted cases.
set -eu

TITLE="Security Scan fails on main"

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
  echo "usage: ${0##*/} RUN_URL" >&2
  exit 2
fi
run_url="$1"
gh="${GH:-gh}"

# A failed listing must not read as "no issue open": the step would then open a duplicate.
if ! open=$($gh issue list --state open --limit 200 --json number,title); then
  echo "error: cannot list the open issues" >&2
  exit 1
fi
# The title is compared as a string; a title that only contains it is another issue.
number=$(printf '%s' "$open" | jq -r --arg t "$TITLE" '[.[] | select(.title == $t)][0].number // empty')

if [ -n "$number" ]; then
  $gh issue comment "$number" --body "The Security Scan failed again on main: $run_url"
  echo "commented on #$number"
else
  $gh issue create --title "$TITLE" --body "The Security Scan failed on main: $run_url

Each further failure on main adds a comment here while this issue is open. Close it once the scan passes again."
  echo "opened \"$TITLE\""
fi
