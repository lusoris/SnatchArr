#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# The memory limits of the chart against the peaks the image smoke measures (C12-01).
#
#   mem_limits.sh limit ROLE [VALUES]         the limits.memory of api or worker in bytes
#   mem_limits.sh peak CONTAINER_ID [ROOT]    the cgroup v2 memory.peak of a container in bytes
#   mem_limits.sh check NAME PEAK LIMIT       fails when PEAK is above MEM_SHARE % of LIMIT
#
# MEM_SHARE defaults to 80, the share at which the hourly caps warn as well.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

fail() {
    echo "mem-limits: $*" >&2
    exit 1
}

# limit ROLE [VALUES]: reads ROLE.resources.limits.memory from the values file.
limit() {
    local role=$1 values=${2:-$root/deploy/helm/snatcharr/values.yaml} raw
    [ -r "$values" ] || fail "cannot read $values"
    raw=$(awk -v role="$role" '
        /^[^ #]/ { top = $0; sub(/:.*/, "", top); res = 0; lim = 0; next }
        top != role { next }
        /^  [^ ]/ { res = ($0 ~ /^  resources:/); lim = 0; next }
        res && /^    [^ ]/ { lim = ($0 ~ /^    limits:/); next }
        res && lim && /^      memory:/ { print $2; exit }
    ' "$values" | tr -d "\"'")
    if [[ "$raw" =~ ^([1-9][0-9]{0,5})Mi$ ]]; then
        echo $((BASH_REMATCH[1] * 1024 * 1024))
        return
    fi
    if [[ "$raw" =~ ^([1-9][0-9]{0,2})Gi$ ]]; then
        echo $((BASH_REMATCH[1] * 1024 * 1024 * 1024))
        return
    fi
    fail "$role.resources.limits.memory in $values is '$raw', want <n>Mi or <n>Gi"
}

# peak ID [ROOT]: memory.peak of the container's cgroup, systemd or cgroupfs driver.
peak() {
    local id=$1 cg=${2:-/sys/fs/cgroup} file
    for file in "$cg/system.slice/docker-$id.scope/memory.peak" "$cg/docker/$id/memory.peak"; do
        if [ -r "$file" ]; then
            cat "$file"
            return
        fi
    done
    fail "no memory.peak for container $id under $cg (cgroup v2 and Linux 5.19 or newer needed)"
}

# check NAME PEAK LIMIT: PEAK must stay at or below MEM_SHARE % of LIMIT.
check() {
    local name=$1 peak=$2 limit=$3 share=${MEM_SHARE:-80} n
    for n in "$peak" "$limit" "$share"; do
        [[ "$n" =~ ^[0-9]{1,15}$ ]] || fail "$name: peak '$peak', limit '$limit' or share '$share' is not a number"
    done
    [ "$limit" -gt 0 ] || fail "$name: limit is 0"
    if [ $((peak * 100)) -gt $((limit * share)) ]; then
        fail "$name peak $((peak / 1048576)) MiB is above $share % of its chart limit $((limit / 1048576)) MiB"
    fi
    echo "mem-limits: $name peak $((peak / 1048576)) MiB, $((peak * 100 / limit)) % of its chart limit $((limit / 1048576)) MiB"
}

cmd=${1:-}
[ "$#" -gt 0 ] && shift
case "$cmd" in
limit) [ "$#" -ge 1 ] || fail "usage: limit ROLE [VALUES]"; limit "$@" ;;
peak) [ "$#" -ge 1 ] || fail "usage: peak CONTAINER_ID [ROOT]"; peak "$@" ;;
check) [ "$#" -eq 3 ] || fail "usage: check NAME PEAK LIMIT"; check "$@" ;;
*) fail "unknown command '$cmd'; want limit, peak or check" ;;
esac
