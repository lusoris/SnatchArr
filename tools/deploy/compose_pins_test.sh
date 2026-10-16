#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for compose_pins.sh. Refused: the tag latest (also with a digest), a tag
# without a digest, a digest without a tag, a variable, a short or upper-case digest, a file
# without an image line, one bad file among good ones, no argument. Accepted: a quoted
# reference, a trailing comment, a registry host with a port, the repository's compose files.
#
#   tools/deploy/compose_pins_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
failed=0
D=sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873

# expect WANT NAME files...: compose_pins.sh files exits WANT.
expect() {
  want="$1"
  name="$2"
  shift 2
  got=0
  sh "$HERE/compose_pins.sh" "$@" >"$WORK/out" 2>&1 || got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: exit $got, want $want" >&2
    sed 's/^/  /' "$WORK/out" >&2
    failed=1
    return 0
  fi
  echo "ok   $name: exit $got"
}

# compose NAME IMAGE: a compose file with a pinned postgres service and a service on IMAGE.
compose() {
  printf '%s\n' 'services:' '  db:' "    image: postgres:18.6-alpine@$D" '  app:' \
    "    image: $2" '    ports:' '      - "8080:8080"' >"$WORK/$1.yml"
}

compose good "ghcr.io/lusoris/snatcharr-api:0.1.9@$D"
compose quoted "\"grafana/grafana:13.2.4@$D\""
compose comment "prom/prometheus:v3.15.0@$D # pinned"
compose port "registry.local:5000/team/app:1.2.3@$D"
compose latest "grafana/grafana:latest"
compose latest-digest "grafana/grafana:latest@$D"
compose tag-only "grafana/grafana:13.2.4"
compose digest-only "grafana/grafana@$D"
compose variable "\${APP_IMAGE}"
compose short "grafana/grafana:13.2.4@sha256:77f585114c32"
compose upper "grafana/grafana:13.2.4@sha256:77F585114C32FBCA283DC835B0596F4E52B51B4C6662D7810B2F4084F60A1873"
printf '%s\n' 'services:' '  app:' '    build: .' >"$WORK/none.yml"

expect 0 good "$WORK/good.yml"
expect 0 quoted "$WORK/quoted.yml"
expect 0 comment "$WORK/comment.yml"
expect 0 registry-port "$WORK/port.yml"
expect 1 latest "$WORK/latest.yml"
expect 1 latest-with-digest "$WORK/latest-digest.yml"
expect 1 tag-without-digest "$WORK/tag-only.yml"
expect 1 digest-without-tag "$WORK/digest-only.yml"
expect 1 variable "$WORK/variable.yml"
expect 1 short-digest "$WORK/short.yml"
expect 1 upper-case-digest "$WORK/upper.yml"
expect 1 no-image-line "$WORK/none.yml"
expect 1 one-bad-file "$WORK/good.yml" "$WORK/latest.yml" "$WORK/quoted.yml"
expect 1 missing-file "$WORK/absent.yml"
expect 2 no-argument
expect 0 repository "$ROOT/deploy/compose/docker-compose.yml"

exit "$failed"
