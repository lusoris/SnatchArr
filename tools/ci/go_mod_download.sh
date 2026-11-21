#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Fetch modules before a scan, retrying only known transient proxy failures:
#
#   tools/ci/go_mod_download.sh
#
# Exit status: go mod download status, 124 attempt deadline, 2 usage.
# Cases: go_mod_download_test.sh.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

HERE="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
backoff="${GO_MOD_DOWNLOAD_BACKOFF-5 15}"
if [ -z "$backoff" ] || [ "$backoff" != "$(printf '%s\n' "$backoff" | LC_ALL=C grep -E '^[0-9]{1,3}[[:blank:]]+[0-9]{1,3}$')" ]; then
  echo 'go_mod_download: usage: GO_MOD_DOWNLOAD_BACKOFF="SECONDS SECONDS" sh go_mod_download.sh' >&2
  exit 2
fi
first_delay="${backoff%%[[:blank:]]*}"
second_delay="${backoff##*[[:blank:]]}"
MAX_ATTEMPTS=3
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' 0
attempt=1
while [ "$attempt" -le "$MAX_ATTEMPTS" ]; do
  rc=0
  python3 -I "$HERE/run_bounded.py" 300 go mod download >"$WORK/output" 2>&1 || rc=$?
  cat "$WORK/output"
  if [ "$rc" -eq 0 ]; then
    exit 0
  fi
  if grep -Fq -e 'SECURITY ERROR' -e 'checksum mismatch' "$WORK/output"; then
    echo 'go_mod_download: module verification failed; not retrying' >&2
    exit "$rc"
  fi
  if [ "$rc" -ne 124 ] && ! grep -Fq \
    -e 'stream error:' \
    -e 'connection reset by peer' \
    -e 'i/o timeout' \
    -e 'TLS handshake timeout' \
    -e 'unexpected EOF' \
    -e 'server misbehaving' \
    -e '502 Bad Gateway' \
    -e '503 Service Unavailable' \
    -e '504 Gateway Timeout' "$WORK/output"; then
    echo 'go_mod_download: go mod download failed (not a transient network error); not retrying' >&2
    exit "$rc"
  fi
  if [ "$attempt" -eq "$MAX_ATTEMPTS" ]; then
    echo 'go_mod_download: gave up after 3 attempts' >&2
    exit "$rc"
  fi
  if [ "$attempt" -eq 1 ]; then
    delay="$first_delay"
  else
    delay="$second_delay"
  fi
  printf 'go_mod_download: attempt %s of 3 failed on a transient network error; retrying in %s s\n' "$attempt" "$delay" >&2
  sleep "$delay"
  attempt=$((attempt + 1))
done
