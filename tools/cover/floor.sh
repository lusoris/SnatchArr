#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# The coverage floor of make api-cover and make worker-cover, kept apart so that floor_test.sh
# can plant the cases it refuses (gap audit A3-02). PERCENT is the total line coverage without
# the sign (`go tool cover -func`, `cargo llvm-cov report --json`). An empty or non-numeric
# PERCENT fails: the old awk read it as 0 only by accident.
#
#   tools/cover/floor.sh PERCENT MIN
#
# Exit status: 0 PERCENT >= MIN, 1 below MIN or not a number, 2 usage.
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: ${0##*/} PERCENT MIN" >&2
  exit 2
fi
for value in "$1" "$2"; do
  case "$value" in
    '' | *[!0-9.]* | *.*.* | .* | *.)
      echo "coverage floor: '$value' is not a percentage" >&2
      exit 1
      ;;
  esac
done
if awk -v p="$1" -v m="$2" 'BEGIN { exit (p + 0 >= m + 0) ? 0 : 1 }'; then
  echo "coverage $1% (min $2%)"
  exit 0
fi
echo "coverage $1% is below the floor of $2%" >&2
exit 1
