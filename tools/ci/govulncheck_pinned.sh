#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Scan with the module's official Go release and check the scan's own version header:
# distro or GOEXPERIMENT builds can hide standard-library vulnerabilities.
#
#   tools/ci/govulncheck_pinned.sh SCANNER [PATTERN...]
#
# Exit status: scanner status when pinned, 1 version mismatch, 2 usage or missing directive.
# 129/130/143 = interrupted (HUP/INT/TERM), scanner stopped.
# Cases: govulncheck_pinned_test.sh.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

if [ "$#" -eq 0 ] || [ -z "$1" ]; then
  echo 'govulncheck_pinned: usage: govulncheck_pinned.sh SCANNER [PATTERN...]' >&2
  exit 2
fi
SCANNER="$1"
shift
directive="$(go list -m -f '{{.GoVersion}}')" || {
  echo 'govulncheck_pinned: cannot read module Go directive' >&2
  exit 2
}
if [ -z "$directive" ]; then
  echo 'govulncheck_pinned: empty module Go directive' >&2
  exit 2
fi
if [ "$#" -eq 0 ]; then
  set -- ./...
fi
rc=0
# This script never changes directory, so a relative TMPDIR path stays valid.
log="$(mktemp)"
trap 'rm -f "$log"' 0
pid=
# stop_scan STATUS: stop and reap a running scan, then exit with STATUS.
# shellcheck disable=SC2329 # invoked by the HUP, INT and TERM traps below
stop_scan() {
  # A scan that already exited cannot be signalled; a signalled one exits non-zero.
  if [ -n "$pid" ] && kill "$pid" 2>/dev/null && ! wait "$pid" 2>/dev/null; then
    : # expected: it ended on our signal
  fi
  exit "$1"
}
trap 'stop_scan 129' HUP
trap 'stop_scan 130' INT
trap 'stop_scan 143' TERM
# Run in background so a signal to this shell alone stops and reaps the scan.
GOTOOLCHAIN="go$directive" "$SCANNER" -show version "$@" >"$log" 2>&1 &
pid=$!
wait "$pid" || rc=$?
cat "$log"
# Keep the prefix so even a second empty Go header cannot disappear in command substitution.
go_headers="$(sed -n '/^Go: /p' "$log")"
go_seen="$(printf '%s\n' "$go_headers" | sed -n 's/^Go: //p')"
if [ "$go_headers" != "Go: go$directive" ]; then
  printf "govulncheck_pinned: govulncheck read Go '%s', want 'go%s': a distro or GOEXPERIMENT build hides standard library vulnerabilities\n" "$go_seen" "$directive" >&2
  exit 1
fi
exit "$rc"
