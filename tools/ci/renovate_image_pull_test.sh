#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Offline unit cases for tools/ci/renovate_image_pull.sh using stub docker, timeout
# and sleep binaries on PATH in a temporary directory.
#
#   RENOVATE_IMAGE_PULL_SCRIPT=... bash tools/ci/renovate_image_pull_test.sh
#
# Exit status: 0 when all cases pass, 1 on any failure.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PULL_SCRIPT="${RENOVATE_IMAGE_PULL_SCRIPT:-$HERE/renovate_image_pull.sh}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

unset RENOVATE_IMAGE RENOVATE_SIGNED_REPO

DIGEST="247f6652229c20ae77f868905b09f9acd658ede7c498dd94f69ed7268c33b676"
MIRROR_REF="mirror.gcr.io/renovate/renovate:44.234.0@sha256:${DIGEST}"
SIGNED_REPO="ghcr.io/renovatebot/renovate"
GHCR_REF="${SIGNED_REPO}@sha256:${DIGEST}"
QUOTA_ERR="error from registry: Quota exceeded for quota metric (Read requests to external upstream hosts per organization per host in the US multi-region)"

STUB_BIN="$work/bin"
STUB_DOCKER_LOG="$work/docker.log"
STUB_TIMEOUT_LOG="$work/timeout.log"
STUB_SLEEP_LOG="$work/sleep.log"
export MIRROR_REF GHCR_REF QUOTA_ERR STUB_DOCKER_LOG STUB_TIMEOUT_LOG STUB_SLEEP_LOG

mkdir -p "$STUB_BIN"

cat > "$STUB_BIN/timeout" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ ! "${1:-}" =~ ^[1-9][0-9]*$ ]]; then
  printf 'stub timeout: expected positive seconds, got %s\n' "${1:-}" >&2
  exit 125
fi
printf '%s\n' "$1" >> "$STUB_TIMEOUT_LOG"
shift
exec "$@"
EOF

cat > "$STUB_BIN/sleep" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ ! "${1:-}" =~ ^[1-9][0-9]*$ ]]; then
  printf 'stub sleep: expected positive integer seconds, got %s\n' "${1:-}" >&2
  exit 2
fi
printf '%s\n' "$1" >> "$STUB_SLEEP_LOG"
EOF

cat > "$STUB_BIN/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$STUB_DOCKER_LOG"
if [ "$#" -ne 3 ] || [ "$1" != "pull" ] || [ "$2" != "--quiet" ]; then
  printf 'stub docker: unexpected argv: %s\n' "$*" >&2
  exit 2
fi
ref="$3"
if [ "$ref" = "$MIRROR_REF" ]; then
  if [ "${STUB_MIRROR_MODE:-ok}" = "ok" ]; then
    printf '%s\n' "$MIRROR_REF"
    exit 0
  fi
  printf '%s\n' "$QUOTA_ERR" >&2
  exit 1
fi
if [ "$ref" = "$GHCR_REF" ]; then
  if [ "${STUB_FALLBACK_MODE:-ok}" = "ok" ]; then
    printf '%s\n' "$GHCR_REF"
    exit 0
  fi
  printf 'error from registry: upstream unavailable on ghcr.io for %s\n' "$GHCR_REF" >&2
  exit 1
fi
printf 'stub docker: unexpected reference %s\n' "$ref" >&2
exit 1
EOF

chmod +x "$STUB_BIN/timeout" "$STUB_BIN/sleep" "$STUB_BIN/docker"
PATH="$STUB_BIN:$PATH"
export PATH

RUN_RC=0
RUN_OUT=""
RUN_ERR=""

# fail_case NAME DETAIL: print FAIL diagnostic to stderr and exit 1.
fail_case() {
  printf 'FAIL %s: %s\n' "$1" "$2" >&2
  exit 1
}

# reset_logs: truncate stub call logs before a test case.
reset_logs() {
  : > "$STUB_DOCKER_LOG"
  : > "$STUB_TIMEOUT_LOG"
  : > "$STUB_SLEEP_LOG"
}

# invoke_pull MIRROR_MODE FALLBACK_MODE IMAGE_SET IMAGE_VAL REPO_SET REPO_VAL: run PULL_SCRIPT with stub modes.
invoke_pull() {
  local m_mode="$1" f_mode="$2" img_set="$3" img_val="$4" repo_set="$5" repo_val="$6"
  reset_logs
  RUN_RC=0
  (
    export STUB_MIRROR_MODE="$m_mode" STUB_FALLBACK_MODE="$f_mode"
    if [ "$img_set" = "1" ]; then
      export RENOVATE_IMAGE="$img_val"
    fi
    if [ "$repo_set" = "1" ]; then
      export RENOVATE_SIGNED_REPO="$repo_val"
    fi
    bash "$PULL_SCRIPT"
  ) > "$work/stdout" 2> "$work/stderr" || RUN_RC=$?
  RUN_OUT="$(cat "$work/stdout")"
  RUN_ERR="$(cat "$work/stderr")"
}

# assert_log_lines CASE LOG_PATH EXPECTED_TEXT WANT_SLEEPS: check docker, timeout and sleep call logs.
assert_log_lines() {
  local name="$1" expected="$2" want_sleeps="$3" got_docker
  got_docker="$(cat "$STUB_DOCKER_LOG")"
  if [ "$got_docker" != "$expected" ]; then
    fail_case "$name" "docker calls mismatch: got [$got_docker], want [$expected] (stderr: $RUN_ERR)"
  fi
  local docker_count timeout_count
  docker_count="$(wc -l < "$STUB_DOCKER_LOG" | tr -d ' ')"
  timeout_count="$(wc -l < "$STUB_TIMEOUT_LOG" | tr -d ' ')"
  if [ "$docker_count" != "$timeout_count" ]; then
    fail_case "$name" "timeout count $timeout_count != docker count $docker_count"
  fi
  local -a sleeps=()
  if [ -s "$STUB_SLEEP_LOG" ]; then
    mapfile -t sleeps < "$STUB_SLEEP_LOG"
  fi
  if [ "${#sleeps[@]}" -ne "$want_sleeps" ]; then
    fail_case "$name" "sleep count ${#sleeps[@]}, want $want_sleeps"
  fi
  local idx val
  for ((idx = 0; idx < want_sleeps; idx++)); do
    val="${sleeps[$idx]}"
    if [ "$val" -lt 6 ] || [ "$val" -gt 18 ]; then
      fail_case "$name" "sleep[$idx]=$val outside jittered backoff range [6, 18]"
    fi
  done
}

# test_mirror_ok: one mirror pull succeeds and prints the mirror reference.
test_mirror_ok() {
  invoke_pull "ok" "refuse" "1" "$MIRROR_REF" "1" "$SIGNED_REPO"
  if [ "$RUN_RC" -ne 0 ] || [ "$RUN_OUT" != "$MIRROR_REF" ]; then
    fail_case "mirror-ok" "rc=$RUN_RC stdout=[$RUN_OUT] stderr=[$RUN_ERR]"
  fi
  if [ "$(wc -l < "$work/stdout" | tr -d ' ')" -ne 1 ]; then
    fail_case "mirror-ok" "expected exactly 1 stdout line"
  fi
  assert_log_lines "mirror-ok" "pull --quiet $MIRROR_REF" 0
  echo "ok   mirror-ok"
}

# test_mirror_refused_fallback: 3 quota-refused mirror attempts fall back to 1 ghcr pull at the same digest.
test_mirror_refused_fallback() {
  invoke_pull "refuse" "ok" "1" "$MIRROR_REF" "1" "$SIGNED_REPO"
  if [ "$RUN_RC" -ne 0 ] || [ "$RUN_OUT" != "$GHCR_REF" ]; then
    fail_case "mirror-refused-fallback" "rc=$RUN_RC stdout=[$RUN_OUT] want=[$GHCR_REF] stderr=[$RUN_ERR]"
  fi
  if [ "$(wc -l < "$work/stdout" | tr -d ' ')" -ne 1 ]; then
    fail_case "mirror-refused-fallback" "expected exactly 1 stdout line"
  fi
  local expected
  expected="$(printf 'pull --quiet %s\npull --quiet %s\npull --quiet %s\npull --quiet %s' \
    "$MIRROR_REF" "$MIRROR_REF" "$MIRROR_REF" "$GHCR_REF")"
  assert_log_lines "mirror-refused-fallback" "$expected" 2
  echo "ok   mirror-refused-fallback"
}

# test_both_refused: 3 mirror + 3 fallback failures exit 1 and name both failures on stderr.
test_both_refused() {
  invoke_pull "refuse" "refuse" "1" "$MIRROR_REF" "1" "$SIGNED_REPO"
  if [ "$RUN_RC" -ne 1 ] || [ -n "$RUN_OUT" ]; then
    fail_case "both-refused" "rc=$RUN_RC (want 1) stdout=[$RUN_OUT]"
  fi
  local needle
  for needle in "$MIRROR_REF" "Quota exceeded for quota metric" "$GHCR_REF" "ghcr.io"; do
    case "$RUN_ERR" in
      *"$needle"*) ;;
      *) fail_case "both-refused" "stderr missing '$needle': $RUN_ERR" ;;
    esac
  done
  local expected
  expected="$(printf 'pull --quiet %s\npull --quiet %s\npull --quiet %s\npull --quiet %s\npull --quiet %s\npull --quiet %s' \
    "$MIRROR_REF" "$MIRROR_REF" "$MIRROR_REF" "$GHCR_REF" "$GHCR_REF" "$GHCR_REF")"
  assert_log_lines "both-refused" "$expected" 4
  echo "ok   both-refused"
}

# test_no_digest: references without a full @sha256:<64-hex> digest exit 2 without invoking docker.
test_no_digest() {
  local bad_ref
  for bad_ref in \
    "mirror.gcr.io/renovate/renovate:44.234.0" \
    "mirror.gcr.io/renovate/renovate:latest" \
    "mirror.gcr.io/renovate/renovate:44.234.0@sha256:short"; do
    invoke_pull "ok" "ok" "1" "$bad_ref" "1" "$SIGNED_REPO"
    if [ "$RUN_RC" -ne 2 ] || [ -n "$RUN_OUT" ] || [ -s "$STUB_DOCKER_LOG" ]; then
      fail_case "no-digest" "ref=$bad_ref rc=$RUN_RC (want 2) stdout=[$RUN_OUT]"
    fi
  done
  echo "ok   no-digest"
}

# test_missing_env: missing RENOVATE_IMAGE or RENOVATE_SIGNED_REPO exits 2 without invoking docker.
test_missing_env() {
  invoke_pull "ok" "ok" "0" "" "1" "$SIGNED_REPO"
  if [ "$RUN_RC" -ne 2 ] || [ -n "$RUN_OUT" ] || [ -s "$STUB_DOCKER_LOG" ]; then
    fail_case "missing-env" "missing RENOVATE_IMAGE gave rc=$RUN_RC (want 2)"
  fi
  invoke_pull "ok" "ok" "1" "$MIRROR_REF" "0" ""
  if [ "$RUN_RC" -ne 2 ] || [ -n "$RUN_OUT" ] || [ -s "$STUB_DOCKER_LOG" ]; then
    fail_case "missing-env" "missing RENOVATE_SIGNED_REPO gave rc=$RUN_RC (want 2)"
  fi
  echo "ok   missing-env"
}

test_mirror_ok
test_mirror_refused_fallback
test_both_refused
test_no_digest
test_missing_env
