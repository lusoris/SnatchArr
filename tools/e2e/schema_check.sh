# shellcheck shell=bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# schema_after_upgrade_ok OLD CANDIDATE HIGHEST DIRTY: whether the schema version after an
# upgrade is right. The candidate must sit at the highest migration it ships, clean, and never
# below the old release. Equal to the old version is right when no migration landed since that
# release: after v0.8.0 a strict "greater than" failed every upgrade run (2026-10-31). An old
# image started as the candidate is caught by the version and commit check, not here.
schema_after_upgrade_ok() {
    local old=$1 cand=$2 highest=$3 dirty=$4
    [ "$cand" -ge "$old" ] && [ "$cand" -eq "$highest" ] && [ "$dirty" = "f" ]
}
