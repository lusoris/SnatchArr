#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Validates rendered manifests with kubeconform against the oldest Kubernetes version the chart
# claims (kubeVersion in Chart.yaml) and against the newest schemas. kubeconform alone checks the
# newest schemas only, so a field added after the claimed minimum passed (gap audit C13-01).
#
#   tools/deploy/kubeconform.sh CHART_YAML RENDER...
#
# Runs the kubeconform binary when it is installed, else the pinned image CI uses. Resources
# without a schema (the CloudNativePG Cluster, the ServiceMonitor) are skipped.
# Exit status: 0 valid, 1 refused, 2 usage.
set -eu

IMAGE=ghcr.io/yannh/kubeconform:v0.7.0

if [ "$#" -lt 2 ]; then
  echo "usage: ${0##*/} CHART_YAML RENDER..." >&2
  exit 2
fi
chart="$1"
shift

# kubeVersion: '>=1.29.0-0' gives 1.29.0; any other form is refused rather than guessed.
min=$(sed -n "s/^kubeVersion: *['\"]\{0,1\}>=\([0-9]*\.[0-9]*\.[0-9]*\)\(-0\)\{0,1\}['\"]\{0,1\} *$/\1/p" "$chart")
if [ -z "$min" ]; then
  echo "kubeconform: no kubeVersion of the form '>=X.Y.Z-0' in $chart" >&2
  exit 1
fi

kc() {
  if command -v kubeconform >/dev/null 2>&1; then
    kubeconform "$@"
  else
    docker run --rm -i "$IMAGE" "$@"
  fi
}

for render in "$@"; do
  for version in "$min" master; do
    if ! kc -strict -ignore-missing-schemas -summary -kubernetes-version "$version" <"$render"; then
      echo "kubeconform: $render is invalid on Kubernetes $version" >&2
      exit 1
    fi
  done
done
