#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Cases for schema_check.sh: an upgrade that adds migrations and one that adds none (the
# release after v0.8.0) are accepted; a candidate below the old version, one short of its own
# highest migration, and a dirty schema are refused.
set -euo pipefail

# shellcheck source=tools/e2e/schema_check.sh
. "$(dirname "$0")/schema_check.sh"
failed=0

# expect NAME WANT OLD CANDIDATE HIGHEST DIRTY: WANT is ok or refused.
expect() {
    local name=$1 want=$2 got=refused
    shift 2
    if schema_after_upgrade_ok "$@"; then got=ok; fi
    if [ "$got" = "$want" ]; then
        echo "ok   $name"
    else
        echo "FAIL $name: $got, want $want" >&2
        failed=1
    fi
}

expect "migrations added" ok 59 61 61 f
expect "no migration since the old release" ok 61 61 61 f
expect "candidate below the old release" refused 61 60 60 f
expect "candidate short of its highest migration" refused 59 60 61 f
expect "dirty schema" refused 59 61 61 t
exit "$failed"
