#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Print local subjects that may declare an OpenAPI break. The squash subject on main comes from
# the pull request title, which a local run cannot read, so any branch commit that declares the
# break stands in for it. CI keeps checking the pull request title.
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: ${0##*/} BASE_REF" >&2
  exit 2
fi

base_ref="$1"
if ! git rev-parse --verify -q "$base_ref^{commit}" >/dev/null; then
  echo "${0##*/}: base ref does not resolve: $base_ref" >&2
  exit 1
fi

is_shallow="$(git rev-parse --is-shallow-repository)"
if [ "$is_shallow" = true ]; then
  echo "${0##*/}: repository is shallow; run git fetch --unshallow" >&2
  exit 1
fi

git log --no-merges --format=%s "$base_ref..HEAD"
git log -1 --format=%s HEAD
