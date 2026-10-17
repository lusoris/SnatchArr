#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for kubeconform.sh. Refused: a Service with spec.trafficDistribution (added in
# Kubernetes 1.30) under a chart that claims 1.29, a Chart.yaml without kubeVersion, an invalid
# resource. Accepted: the same Service under a chart that claims 1.30, a plain Service under 1.29.
#
#   tools/deploy/kubeconform_test.sh
#
# Needs the network for the schemas. Exit status: 0 every case behaved as recorded, 1 otherwise.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
failed=0

# expect WANT NAME args...: kubeconform.sh args exits WANT.
expect() {
  want="$1"
  name="$2"
  shift 2
  got=0
  sh "$HERE/kubeconform.sh" "$@" >"$WORK/out" 2>&1 || got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: exit $got, want $want" >&2
    sed 's/^/  /' "$WORK/out" >&2
    failed=1
    return 0
  fi
  echo "ok   $name: exit $got"
}

# service NAME [EXTRA]: a Service, with EXTRA as one more line under spec.
service() {
  printf '%s\n' 'apiVersion: v1' 'kind: Service' 'metadata:' '  name: planted' 'spec:' \
    '  selector:' '    app: planted' '  ports:' '    - port: 80' ${2:+"  $2"} >"$WORK/$1.yaml"
}

printf '%s\n' 'apiVersion: v2' 'name: planted' "kubeVersion: '>=1.29.0-0'" >"$WORK/chart-129.yaml"
printf '%s\n' 'apiVersion: v2' 'name: planted' "kubeVersion: '>=1.30.0-0'" >"$WORK/chart-130.yaml"
printf '%s\n' 'apiVersion: v2' 'name: planted' >"$WORK/chart-none.yaml"
service plain
service traffic 'trafficDistribution: PreferClose'
service unknown 'nonsense: true'

expect 0 plain-service-on-1.29 "$WORK/chart-129.yaml" "$WORK/plain.yaml"
expect 1 newer-field-on-1.29 "$WORK/chart-129.yaml" "$WORK/plain.yaml" "$WORK/traffic.yaml"
expect 0 newer-field-on-1.30 "$WORK/chart-130.yaml" "$WORK/traffic.yaml"
expect 1 unknown-field "$WORK/chart-130.yaml" "$WORK/unknown.yaml"
expect 1 no-kube-version "$WORK/chart-none.yaml" "$WORK/plain.yaml"
expect 2 no-render "$WORK/chart-129.yaml"

exit "$failed"
