#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Spectral over an OpenAPI document with the repository ruleset (tools/spectral.yaml). A warning
# fails as an error does. make api-verify and the CI job api-openapi both call this script, so
# the version and flags live here only; lint_test.sh plants the cases it must refuse.
#
#   tools/spectral/lint.sh SPEC
#
# SPECTRAL names the command (default: the pinned spectral-cli through npx). Exit status: 0 no
# finding, non-zero otherwise.
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: ${0##*/} SPEC" >&2
  exit 2
fi
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck disable=SC2086 # SPECTRAL is a command with its arguments.
exec ${SPECTRAL:-npx --yes @stoplight/spectral-cli@6.15.0} lint "$1" \
  --ruleset "$HERE/../spectral.yaml" --fail-severity warn
