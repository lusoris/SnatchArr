#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Starts a temporary Postgres container for end-to-end tests if SMOKE_DSN is not set,
# waits for TCP readiness, sets SMOKE_DSN, runs the given command, and removes the
# container on exit (HISS-19).
set -euo pipefail

: "${E2E_POSTGRES:=mirror.gcr.io/library/postgres:18-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873}"

if [ -z "${SMOKE_DSN:-}" ]; then
    cid=$(docker run -d --rm -e POSTGRES_PASSWORD=smoke -p 127.0.0.1::5432 "$E2E_POSTGRES")
    cleanup() {
        if ! docker rm -f "$cid" >/dev/null 2>&1; then
            echo "e2e-smoke: could not remove postgres container $cid" >&2
        fi
    }
    trap cleanup EXIT
    tries=0
    # Over TCP: the image's first-start server listens on the socket only, then restarts.
    until docker exec "$cid" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do
        tries=$((tries + 1))
        [ "$tries" -lt 120 ] || { echo "e2e-smoke: postgres not ready after 60 s" >&2; exit 1; }
        sleep 0.5
    done
    port=$(docker port "$cid" 5432/tcp | head -n 1 | sed 's/.*://')
    export SMOKE_DSN="postgres://postgres:smoke@127.0.0.1:$port/postgres?sslmode=disable"
fi

if [ $# -gt 0 ]; then
    "$@"
fi
