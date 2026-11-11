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
output="$(GOTOOLCHAIN="go$directive" "$SCANNER" -show version "$@" 2>&1)" || rc=$?
printf '%s\n' "$output"
# Keep the prefix so even a second empty Go header cannot disappear in command substitution.
go_headers="$(printf '%s\n' "$output" | sed -n '/^Go: /p')"
go_seen="$(printf '%s\n' "$go_headers" | sed -n 's/^Go: //p')"
if [ "$go_headers" != "Go: go$directive" ]; then
  printf "govulncheck_pinned: govulncheck read Go '%s', want 'go%s': a distro or GOEXPERIMENT build hides standard library vulnerabilities\n" "$go_seen" "$directive" >&2
  exit 1
fi
exit "$rc"
