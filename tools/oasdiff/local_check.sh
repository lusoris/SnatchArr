#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Run the local OpenAPI breaking change check against a base revision. Branch subjects are collected
# separately so a shallow or otherwise invalid repository stops the check before oasdiff runs.
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: ${0##*/} BASE_REF SPEC_PATH" >&2
  exit 2
fi

HERE="$(cd "$(dirname "$0")" && pwd)"
BASE_REF="$1"
SPEC_PATH="$2"
base="$(mktemp)"
trap 'rm -f "$base"' EXIT

subjects=$(sh "$HERE/branch_subjects.sh" "$BASE_REF")
git show "$BASE_REF:$SPEC_PATH" >"$base"
sh "$HERE/breaking.sh" "$base" "$SPEC_PATH" "$subjects"
