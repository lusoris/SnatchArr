#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Renders the chart into a file for kubeconform. `helm template | kubeconform` passed whatever
# helm did: without pipefail the pipe's status is kubeconform's, and kubeconform accepts empty
# input ("0 resource found"). Here a failing render and a render without a resource both fail.
#
#   tools/deploy/render.sh OUT RELEASE CHART [helm template flags...]
#
# HELM names the helm binary (default: helm). Exit status: 0 rendered, 1 refused, 2 usage.
set -eu

if [ "$#" -lt 3 ]; then
  echo "usage: ${0##*/} OUT RELEASE CHART [helm template flags...]" >&2
  exit 2
fi
out="$1"
shift
if ! "${HELM:-helm}" template "$@" >"$out"; then
  rm -f "$out"
  echo "render: helm template $1 $2 failed" >&2
  exit 1
fi
if ! grep -q '^kind:' "$out"; then
  echo "render: helm template $1 $2 rendered no resource" >&2
  exit 1
fi
