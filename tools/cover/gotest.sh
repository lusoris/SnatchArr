#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Run a test command with its output kept in LOG. Quiet when it passes; when it fails, print the
# whole log and then the names of the failing tests, and exit with the command's status. A
# coverage gate that sends go test to /dev/null fails without saying which test broke.
#
#   tools/cover/gotest.sh LOG -- go test ./...
#
# Exit status: the command's, or 2 on a usage error.
set -eu

if [ "$#" -lt 3 ] || [ "$2" != "--" ]; then
  echo "usage: gotest.sh LOG -- COMMAND [ARG...]" >&2
  exit 2
fi
log="$1"
shift 2

rc=0
"$@" >"$log" 2>&1 || rc=$?
if [ "$rc" -eq 0 ]; then
  exit 0
fi
echo "test command failed (exit $rc): $*" >&2
cat "$log" >&2
echo "failing tests:" >&2
grep -E -e '--- FAIL:|^panic:|^FAIL' "$log" >&2 || echo "  (none named; see the log above)" >&2
exit "$rc"
