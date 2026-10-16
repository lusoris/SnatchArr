#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Every image of a compose file is pinned by a version tag and a digest (#140, HISS-11): the
# reference reads `name:tag@sha256:<64 hex>`, and the tag is not `latest`. A file without an
# image line is refused too, so a check that reads nothing cannot pass.
#
#   tools/deploy/compose_pins.sh deploy/compose/docker-compose.yml...
#
# Exit status: 0 every image pinned, 1 a file holds an unpinned image or none, 2 usage.
set -euf

if [ "$#" -eq 0 ]; then
  echo "usage: $0 compose-file..." >&2
  exit 2
fi

# The value after `image:`, quotes and a trailing comment removed.
images() {
  sed -n -E 's/^[[:space:]]*(-[[:space:]]+)?image:[[:space:]]*//p' "$1" |
    sed -E 's/[[:space:]]+#.*$//; s/^["'\'']//; s/["'\'']$//'
}

pinned='^[a-z0-9][a-z0-9._/-]*(:[0-9]+)?(/[a-z0-9._-]+)*:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}@sha256:[a-f0-9]{64}$'
status=0
for file in "$@"; do
  if [ ! -r "$file" ]; then
    echo "$file: not readable" >&2
    status=1
    continue
  fi
  n=0
  bad=0
  for ref in $(images "$file"); do
    n=$((n + 1))
    tag=${ref%%@*}
    tag=${tag##*:}
    if ! printf '%s\n' "$ref" | grep -E -q "$pinned"; then
      echo "$file: image $ref is not pinned as name:tag@sha256:<digest>" >&2
      bad=$((bad + 1))
    elif [ "$tag" = latest ]; then
      echo "$file: image $ref uses the tag latest" >&2
      bad=$((bad + 1))
    fi
  done
  if [ "$n" -eq 0 ]; then
    echo "$file: no image line found" >&2
    status=1
  elif [ "$bad" -gt 0 ]; then
    status=1
  else
    echo "$file: $n images pinned"
  fi
done
exit "$status"
