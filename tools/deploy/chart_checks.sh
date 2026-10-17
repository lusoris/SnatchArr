#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# The chart checks of make deploy-verify, moved out of the Makefile so that chart_checks_test.sh
# can plant the cases each one refuses (gap audit A3-01).
#
#   tools/deploy/chart_checks.sh netpol RENDER [WANT]
#   tools/deploy/chart_checks.sh chart CHART_YAML APP_VERSION
#   tools/deploy/chart_checks.sh namespace RENDER NAMESPACE
#
# netpol reads the egress rules of every NetworkPolicy in a rendered file and prints
# "<n> with a to, <m> of them without ports, <k> with an empty to". An egress rule whose `to` is
# empty (`to: []`, `to: null`, or `to:` with no entry) allows every destination (#120), so k > 0
# fails whatever WANT says. With WANT, the line must equal WANT (#145: an arrPeers entry with
# `ports` is reachable on those ports only).
#
# chart checks Chart.yaml: the four x-release-please markers are still there (a yaml updater
# drops them), and artifacthub.io/images lists the api and worker image at APP_VERSION (#122).
#
# namespace reads every object of a rendered file and refuses one whose top-level
# metadata.namespace is missing or other than NAMESPACE. Kustomize 5.8 no longer moves helmCharts
# objects without a namespace into the overlay namespace (kubernetes-sigs/kustomize#6058), so the
# chart sets it on every object itself (#346). Namespace objects are the only kind exempt; a file
# with no object is refused.
#
# Exit status: 0 holds, 1 refused, 2 usage.
set -eu

usage() {
  echo "usage: ${0##*/} netpol RENDER [WANT] | chart CHART_YAML APP_VERSION | namespace RENDER NAMESPACE" >&2
  exit 2
}

# Egress rules sit at four spaces under `  egress:`; a rule's `to` is either on the rule line
# (`    - to:`) or below it (`      to:`), and its entries are list items indented past the key.
netpol_summary() {
  awk '
    function rule_end() {
      if (pending) { empty++ }
      if (to) { rules++; if (!ports) open++ }
      to = 0; ports = 0; pending = 0
    }
    /^[[:space:]]*(#|$)/ { next }
    pending {
      pending = 0
      if ($0 !~ /^      +- /) { empty++ }
    }
    /^---/ || /^kind:/ { rule_end(); egress = 0; next }
    /^  [a-zA-Z]+:/ { rule_end(); egress = ($0 ~ /^  egress:/); next }
    !egress { next }
    /^    - / { rule_end() }
    /^    (- |  )to:/ {
      to = 1
      value = $0
      sub(/^[^:]*:[[:space:]]*/, "", value)
      if (value == "") { pending = 1 }
      else if (value ~ /^(\[\]|null|~|\{\})$/) { empty++ }
    }
    /^    (- |  )ports:/ { ports = 1 }
    END { rule_end(); printf "%d with a to, %d of them without ports, %d with an empty to\n", rules, open, empty }
  ' "$1"
}

netpol() {
  [ -r "$1" ] || { echo "netpol: cannot read $1" >&2; exit 1; }
  summary=$(netpol_summary "$1")
  echo "networkPolicy egress rules: $summary"
  case "$summary" in
    *", 0 with an empty to") ;;
    *)
      echo "networkPolicy: an egress rule with an empty 'to' allows every destination" >&2
      exit 1
      ;;
  esac
  if [ "$#" -eq 2 ] && [ "$summary" != "$2" ]; then
    echo "networkPolicy: want '$2'" >&2
    exit 1
  fi
}

chart() {
  [ -r "$1" ] || { echo "chart: cannot read $1" >&2; exit 1; }
  app="$2"
  # awk counts: `grep -c` exits 1 on a count of 0.
  markers=$(awk '/x-release-please/ { n++ } END { print n + 0 }' "$1")
  if [ -z "$app" ] || [ "$markers" -ne 4 ]; then
    echo "Chart.yaml: appVersion '$app', $markers of 4 x-release-please markers" >&2
    exit 1
  fi
  # The image pattern reaches awk through the environment: `awk -v` would turn `\.` into `.`.
  re='^ +image: ghcr\.io/lusoris/snatcharr-(api|worker):'
  export re
  images=$(awk '$0 ~ ENVIRON["re"] { n++ } END { print n + 0 }' "$1")
  # The tag is compared as a string, not a pattern: the dots of a version are regex wildcards.
  # api and worker count once each, so two api lines do not stand in for the worker.
  current=$(awk -v want="$app" '
    $0 !~ ENVIRON["re"] { next }
    { name = $0; sub(/^.*snatcharr-/, "", name); sub(/:.*$/, "", name)
      tag = $0; sub(/^.*:/, "", tag)
      if (tag == want) seen[name] = 1 }
    END { print seen["api"] + seen["worker"] }' "$1")
  if [ "$images" -ne 2 ] || [ "$current" -ne 2 ]; then
    echo "Chart.yaml: artifacthub.io/images must list both images at appVersion $app ($images found, $current current)" >&2
    exit 1
  fi
  echo "Chart.yaml: appVersion $app, 4 markers, both images current"
}

# Only the top-level `metadata:` block counts: a pod template's metadata or a ServiceMonitor's
# namespaceSelector sits deeper. Each object out of place goes to stderr by kind and name.
namespace() {
  [ -r "$1" ] || { echo "namespace: cannot read $1" >&2; exit 1; }
  summary=$(awk -v want="$2" '
    function unquote(v) { gsub(/^["\047]|["\047]$/, "", v); return v }
    function doc_end() {
      if (kind != "" && kind != "Namespace") {
        objects++
        if (ns != want) {
          bad++
          printf "%s/%s: namespace %s\n", kind, name, (ns == "" ? "(none)" : ns) > "/dev/stderr"
        }
      }
      kind = ""; name = ""; ns = ""; meta = 0
    }
    /^---/ { doc_end(); next }
    /^kind:/ { kind = unquote($2); next }
    /^metadata:/ { meta = 1; next }
    /^[^ #]/ { meta = 0 }
    meta && /^  name:/ { name = unquote($2) }
    meta && /^  namespace:/ { ns = unquote($2) }
    END { doc_end(); printf "%d objects, %d outside namespace %s\n", objects, bad, want }
  ' "$1")
  echo "namespace: $summary"
  case "$summary" in
    "0 objects"*) echo "namespace: no object in $1" >&2; exit 1 ;;
    *", 0 outside namespace $2") ;;
    *) exit 1 ;;
  esac
}

[ "$#" -ge 1 ] || usage
cmd="$1"
shift
case "$cmd" in
  netpol) { [ "$#" -eq 1 ] || [ "$#" -eq 2 ]; } || usage; netpol "$@" ;;
  chart) [ "$#" -eq 2 ] || usage; chart "$@" ;;
  namespace) { [ "$#" -eq 2 ] && [ -n "$2" ]; } || usage; namespace "$@" ;;
  *) usage ;;
esac
