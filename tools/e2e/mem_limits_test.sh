#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Cases for mem_limits.sh: the limits of the chart read, a peak found under both cgroup
# drivers, and a peak above the share refused, so the image smoke cannot pass on a check that
# reads nothing.
set -euo pipefail

script="$(dirname "$0")/mem_limits.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0

# expect WANT NAME COMMAND...: WANT is "refused" or the output the command must print.
expect() {
    local want=$1 name=$2 got
    shift 2
    if ! got=$(MEM_SHARE=80 bash "$script" "$@" 2>/dev/null); then
        got=refused
    fi
    if [ "$got" = "$want" ]; then
        echo "ok   $name"
    else
        echo "FAIL $name: got '$got', want '$want'" >&2
        failed=1
    fi
}

cat >"$work/values.yaml" <<'EOF'
api:
  resources:
    requests:
      memory: 64Mi
    limits:
      memory: 256Mi
worker:
  service:
    limits:
      memory: 1Mi
  resources:
    limits:
      memory: "1Gi"
odd:
  resources:
    limits:
      memory: 256M
none:
  resources: {}
EOF
mib=$((1024 * 1024))
expect $((256 * mib)) "limit of api, not its request" limit api "$work/values.yaml"
expect $((1024 * mib)) "limit of worker under resources only, quoted Gi" limit worker "$work/values.yaml"
expect refused "limit in a unit other than Mi or Gi" limit odd "$work/values.yaml"
expect refused "no limit set" limit none "$work/values.yaml"
expect refused "role missing" limit absent "$work/values.yaml"
# The chart itself: both roles carry a limit the smoke can apply.
for role in api worker; do
    if bash "$script" limit "$role" >/dev/null 2>&1; then
        echo "ok   chart limit of $role readable"
    else
        echo "FAIL chart limit of $role not readable" >&2
        failed=1
    fi
done

mkdir -p "$work/cg/system.slice/docker-aaa.scope" "$work/cg/docker/bbb"
echo 1234 >"$work/cg/system.slice/docker-aaa.scope/memory.peak"
echo 5678 >"$work/cg/docker/bbb/memory.peak"
expect 1234 "peak under the systemd driver" peak aaa "$work/cg"
expect 5678 "peak under the cgroupfs driver" peak bbb "$work/cg"
expect refused "no cgroup for the container" peak ccc "$work/cg"

expect "mem-limits: api peak 100 MiB, 39 % of its chart limit 256 MiB" "peak well below" check api $((100 * mib)) $((256 * mib))
expect "mem-limits: api peak 80 MiB, 80 % of its chart limit 100 MiB" "peak at the share" check api $((80 * mib)) $((100 * mib))
expect refused "peak one byte above the share" check api $((80 * mib + 1)) $((100 * mib))
expect refused "peak not a number" check api "" $((100 * mib))
expect refused "limit zero" check api 1 0
exit "$failed"
