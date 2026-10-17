#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for chart_checks.sh. Refused: an egress `to: []` on the rule line, `to: []` and
# `to: null` after `ports`, a `to:` with no entry (also as the file's last line), a summary
# other than WANT, a chart with three markers, a stale image, two api images and no worker
# image, a version or registry host that only matches as a pattern, an object without a
# top-level namespace (also when one sits below spec), an object in another namespace, a render
# with no object. Accepted: the rule shapes the chart renders, an empty ingress `from`,
# `to: []` inside a ConfigMap, a good chart, a render whose objects all sit in the namespace.
#
#   tools/deploy/chart_checks_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
failed=0

# expect WANT NAME args...: chart_checks.sh args exits WANT.
expect() {
  want="$1"
  name="$2"
  shift 2
  got=0
  sh "$HERE/chart_checks.sh" "$@" >"$WORK/out" 2>&1 || got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: exit $got, want $want" >&2
    sed 's/^/  /' "$WORK/out" >&2
    failed=1
    return 0
  fi
  echo "ok   $name: exit $got"
}

# policy NAME EGRESS: a NetworkPolicy file whose egress section is EGRESS, after a DNS rule
# (ports, no to) and an ingress section with an empty `from`.
policy() {
  {
    printf '%s\n' '---' 'apiVersion: networking.k8s.io/v1' 'kind: NetworkPolicy' 'metadata:' \
      '  name: x' 'spec:' '  policyTypes: [Ingress, Egress]' '  ingress:' '    - from: []' \
      '      ports:' '        - port: http' '  egress:' '    - ports:' '        - port: 53' \
      '          protocol: UDP'
    printf '%s\n' "$2"
  } >"$WORK/$1.yaml"
}

policy good '    - to:
        - podSelector:
            matchLabels:
              cnpg.io/cluster: db
      ports:
        - port: 5432
    # a comment between rules
    - ports:
      - port: 8686
        protocol: TCP
      to:
      - podSelector:
          matchLabels:
            app.kubernetes.io/name: lidarr'
policy open_peer '    - to:
      - namespaceSelector:
          matchLabels:
            x: "y"'
policy to_inline_empty '    - to: []'
policy ports_then_empty '    - ports:
      - port: 8686
      to: []'
policy ports_then_null '    - ports:
      - port: 8686
      to: null'
policy to_no_entry '    - ports:
      - port: 8686
      to:
    - to:
        - podSelector: {}'
# The last line of the file is a `to:` with no entry.
policy to_no_entry_at_end '    - ports:
      - port: 8686
      to:'
# A ConfigMap that carries YAML of its own: its `to: []` is data, not an egress rule.
policy other_kind '    - to:
        - podSelector: {}
      ports:
        - port: 5432
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: y
data:
  config.yml: |
    - to: []'

expect 0 good netpol "$WORK/good.yaml" "2 with a to, 0 of them without ports, 0 with an empty to"
expect 0 good_no_want netpol "$WORK/good.yaml"
expect 1 good_other_want netpol "$WORK/good.yaml" "3 with a to, 0 of them without ports, 0 with an empty to"
expect 1 open_peer_with_want netpol "$WORK/open_peer.yaml" "1 with a to, 0 of them without ports, 0 with an empty to"
expect 1 to_inline_empty netpol "$WORK/to_inline_empty.yaml"
expect 1 ports_then_empty netpol "$WORK/ports_then_empty.yaml"
expect 1 ports_then_null netpol "$WORK/ports_then_null.yaml"
expect 1 to_no_entry netpol "$WORK/to_no_entry.yaml"
expect 1 to_no_entry_at_end netpol "$WORK/to_no_entry_at_end.yaml"
expect 0 other_kind netpol "$WORK/other_kind.yaml" "1 with a to, 0 of them without ports, 0 with an empty to"
expect 1 netpol_missing netpol "$WORK/absent.yaml"
expect 2 netpol_usage netpol

# chart NAME MARKERS API_TAG SECOND: a Chart.yaml with MARKERS release-please markers, the api
# image at API_TAG and a second image line snatcharr-SECOND.
chart() {
  {
    printf '%s\n' 'apiVersion: v2' 'name: snatcharr' 'version: 0.1.5 # x-release-please-version' \
      'appVersion: "0.1.5"' 'annotations:' '  artifacthub.io/images: |'
    i=1
    while [ "$i" -lt "$2" ]; do
      echo "    # x-release-please-start-version $i"
      i=$((i + 1))
    done
    printf '%s\n' '    - name: api' "      image: ghcr.io/lusoris/snatcharr-api:$3" \
      '    - name: worker' "      image: ghcr.io/lusoris/snatcharr-$4"
  } >"$WORK/$1.yaml"
}

chart good 4 0.1.5 worker:0.1.5
chart three_markers 3 0.1.5 worker:0.1.5
chart stale_worker 4 0.1.5 worker:0.1.4
chart two_api 4 0.1.5 api:0.1.5
chart pattern_only 4 0.1.5 worker:0x1x5
# The registry host with a dot that only a pattern matches.
sed 's#ghcr\.io/lusoris/snatcharr-api#ghcrXio/lusoris/snatcharr-api#' "$WORK/good.yaml" >"$WORK/host_pattern.yaml"

expect 0 chart_good chart "$WORK/good.yaml" 0.1.5
expect 1 chart_three_markers chart "$WORK/three_markers.yaml" 0.1.5
expect 1 chart_stale_worker chart "$WORK/stale_worker.yaml" 0.1.5
expect 1 chart_two_api chart "$WORK/two_api.yaml" 0.1.5
expect 1 chart_pattern_only chart "$WORK/pattern_only.yaml" 0.1.5
expect 1 chart_host_pattern chart "$WORK/host_pattern.yaml" 0.1.5
expect 1 chart_no_app chart "$WORK/good.yaml" ""
expect 2 chart_usage chart "$WORK/good.yaml"
# objects NAME DOCS...: a rendered file of the given documents, each led by `---`.
objects() {
  name="$1"
  shift
  for doc in "$@"; do printf '%s\n' '---' "$doc"; done >"$WORK/$name.yaml"
}

placed='apiVersion: v1
kind: ConfigMap
metadata:
  name: a
  namespace: snatcharr'
quoted='apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: b
  name: b
  namespace: "snatcharr"
spec:
  template:
    metadata:
      namespace: other'
ns_object='apiVersion: v1
kind: Namespace
metadata:
  name: snatcharr'
# The namespace sits only below spec, as a ServiceMonitor's namespaceSelector does.
nested='apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: c
spec:
  namespaceSelector:
    matchNames:
      - snatcharr
  selector:
    metadata:
      namespace: snatcharr'
elsewhere='apiVersion: v1
kind: Secret
metadata:
  name: d
  namespace: default'

objects ns_good "$ns_object" "$placed" "$quoted"
objects ns_missing "$placed" "$nested"
objects ns_other "$placed" "$elsewhere"
objects ns_only_namespace "$ns_object"
: >"$WORK/ns_empty.yaml"

expect 0 ns_good namespace "$WORK/ns_good.yaml" snatcharr
expect 1 ns_good_other_want namespace "$WORK/ns_good.yaml" media
expect 1 ns_missing namespace "$WORK/ns_missing.yaml" snatcharr
expect 1 ns_other namespace "$WORK/ns_other.yaml" snatcharr
expect 1 ns_only_namespace namespace "$WORK/ns_only_namespace.yaml" snatcharr
expect 1 ns_empty namespace "$WORK/ns_empty.yaml" snatcharr
expect 1 ns_unreadable namespace "$WORK/absent.yaml" snatcharr
expect 2 ns_usage namespace "$WORK/ns_good.yaml"
expect 2 ns_empty_want namespace "$WORK/ns_good.yaml" ""
expect 2 unknown_command nonsense
exit "$failed"
