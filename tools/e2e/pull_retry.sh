# shellcheck shell=bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Sourced by the live tests that start real releases from a registry (dlclients_live.sh,
# cleanuparr_live.sh). A registry answers a burst of pulls with a rate limit now and then
# ("error from registry: retry-after ..."), which failed a whole CI job on 2026-10-17. The
# images are pulled before `up`, a bounded number of times with a jittered wait between tries.
#
# PULL_TRIES (default 4) bounds the tries; PULL_WAIT (default 15) is the first wait in seconds,
# doubled after each failed try, plus up to half of it at random.

# pull_retry COMMAND...: runs COMMAND (a pull) until it succeeds or PULL_TRIES tries failed.
pull_retry() {
    local tries=${PULL_TRIES:-4} wait=${PULL_WAIT:-15} try
    for ((try = 1; try <= tries; try++)); do
        if "$@"; then
            return 0
        fi
        if [ "$try" -lt "$tries" ]; then
            local pause=$((wait + RANDOM % (wait / 2 + 1)))
            echo "pull_retry: pull failed (try $try of $tries), next try in ${pause}s" >&2
            sleep "$pause"
            wait=$((wait * 2))
        fi
    done
    echo "pull_retry: pull failed $tries times" >&2
    return 1
}
