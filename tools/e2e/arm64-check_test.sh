#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Cases for the API config-error allow-list of arm64-check.sh (arm64-check.sh match TEXT).
# The 0.2.0 release failed because the check knew only the 0.1.x error; both are cases here.
set -euo pipefail

check="$(dirname "$0")/arm64-check.sh"
failed=0

expect() {
    local want=$1 name=$2 text=$3 got=refused
    if bash "$check" match "$text" 2>/dev/null; then
        got=accepted
    fi
    if [ "$got" = "$want" ]; then
        echo "ok   $name: $got"
    else
        echo "FAIL $name: $got, want $want" >&2
        failed=1
    fi
}

expect accepted "0.1.x worker token" '[Fx] RUNNING
config: required secret is empty: snatcharr.worker.token'
expect accepted "0.2.0 database DSN" 'failed to build pgx.Options:
received non-nil error from function "github.com/golusoris/golusoris/db/pgx".loadOptions
	github.com/golusoris/golusoris@v0.16.5/db/pgx/pgx.go:153:
db/pgx: DSN is required'
expect refused "panic" 'panic: runtime error: invalid memory address or nil pointer dereference'
expect refused "other secret" 'config: required secret is empty: snatcharr.session.key'
expect refused "empty" ''
exit "$failed"
