#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Offline unit cases for tools/ci/renovate_image_pull.sh using stub cosign, docker,
# timeout and sleep binaries on PATH in a temporary directory.
#
#   RENOVATE_IMAGE_PULL_SCRIPT=... bash tools/ci/renovate_image_pull_test.sh
#
# Exit status: 0 when all cases pass, 1 on any failure.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PULL_SCRIPT="${RENOVATE_IMAGE_PULL_SCRIPT:-$HERE/renovate_image_pull.sh}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

unset RENOVATE_IMAGE RENOVATE_SIGNED_REPO RENOVATE_SIGNER_ISSUER RENOVATE_SIGNER

DIGEST="247f6652229c20ae77f868905b09f9acd658ede7c498dd94f69ed7268c33b676"
MIRROR_REF="mirror.gcr.io/renovate/renovate:44.234.0@sha256:${DIGEST}"
SIGNED_REPO="ghcr.io/renovatebot/renovate"
GHCR_REF="${SIGNED_REPO}@sha256:${DIGEST}"
SIGNER_ISSUER="https://token.actions.githubusercontent.com"
SIGNER_IDENTITY="https://github.com/renovatebot/renovate/.github/workflows/build.yml@refs/heads/main"
QUOTA_ERR="error from registry: Quota exceeded for quota metric (Read requests to external upstream hosts per organization per host in the US multi-region)"

STUB_BIN="$work/bin"
STUB_DOCKER_LOG="$work/docker.log"
STUB_COSIGN_LOG="$work/cosign.log"
STUB_TIMEOUT_LOG="$work/timeout.log"
STUB_SLEEP_LOG="$work/sleep.log"
export DIGEST MIRROR_REF GHCR_REF QUOTA_ERR
export STUB_DOCKER_LOG STUB_COSIGN_LOG STUB_TIMEOUT_LOG STUB_SLEEP_LOG

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

cat > "$STUB_BIN/cosign" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$STUB_COSIGN_LOG"
if [ "${STUB_COSIGN_MODE:-ok}" = "ok" ]; then
  printf '[{"critical":{}}]\n'
  exit 0
fi
if [ "${STUB_COSIGN_MODE:-ok}" = "refuse" ]; then
  printf 'Error: no matching signatures:\n' >&2
  exit 1
fi
printf 'stub cosign: unexpected mode %s\n' "${STUB_COSIGN_MODE:-}" >&2
exit 2
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
if [ "$ref" = "ghcr.io/a/b/c@sha256:${DIGEST}" ] || [ "$ref" = "registry.example.com:5000/owner/repo@sha256:${DIGEST}" ]; then
  printf '%s\n' "$ref"
  exit 0
fi
printf 'stub docker: unexpected reference %s\n' "$ref" >&2
exit 1
EOF

chmod +x "$STUB_BIN/timeout" "$STUB_BIN/sleep" "$STUB_BIN/cosign" "$STUB_BIN/docker"
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
  : > "$STUB_COSIGN_LOG"
  : > "$STUB_TIMEOUT_LOG"
  : > "$STUB_SLEEP_LOG"
}

# invoke_pull MIRROR_MODE FALLBACK_MODE IMAGE_SET IMAGE_VAL REPO_SET REPO_VAL [COSIGN_MODE] [ISSUER_SET] [SIGNER_SET]:
# run PULL_SCRIPT with stub modes and default signer environment variables.
invoke_pull() {
  local m_mode="$1" f_mode="$2" img_set="$3" img_val="$4" repo_set="$5" repo_val="$6"
  local c_mode="${7:-ok}" iss_set="${8:-1}" sig_set="${9:-1}"
  reset_logs
  RUN_RC=0
  (
    export STUB_MIRROR_MODE="$m_mode" STUB_FALLBACK_MODE="$f_mode" STUB_COSIGN_MODE="$c_mode"
    if [ "$img_set" = "1" ]; then
      export RENOVATE_IMAGE="$img_val"
    fi
    if [ "$repo_set" = "1" ]; then
      export RENOVATE_SIGNED_REPO="$repo_val"
    fi
    if [ "$iss_set" = "1" ]; then
      export RENOVATE_SIGNER_ISSUER="$SIGNER_ISSUER"
    fi
    if [ "$sig_set" = "1" ]; then
      export RENOVATE_SIGNER="$SIGNER_IDENTITY"
    fi
    bash "$PULL_SCRIPT"
  ) > "$work/stdout" 2> "$work/stderr" || RUN_RC=$?
  RUN_OUT="$(cat "$work/stdout")"
  RUN_ERR="$(cat "$work/stderr")"
}

# assert_log_lines CASE EXPECTED_TEXT WANT_SLEEPS: check docker, cosign-inclusive timeout and sleep call logs.
assert_log_lines() {
  local name="$1" expected="$2" want_sleeps="$3" got_docker
  got_docker="$(cat "$STUB_DOCKER_LOG")"
  if [ "$got_docker" != "$expected" ]; then
    fail_case "$name" "docker calls mismatch: got [$got_docker], want [$expected] (stderr: $RUN_ERR)"
  fi
  local docker_count cosign_count timeout_count want_timeouts
  docker_count="$(wc -l < "$STUB_DOCKER_LOG" | tr -d ' ')"
  cosign_count="$(wc -l < "$STUB_COSIGN_LOG" | tr -d ' ')"
  timeout_count="$(wc -l < "$STUB_TIMEOUT_LOG" | tr -d ' ')"
  want_timeouts=$((docker_count + cosign_count))
  if [ "$timeout_count" -ne "$want_timeouts" ]; then
    fail_case "$name" "timeout count $timeout_count != docker+cosign count $want_timeouts"
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

# test_no_digest: references without a full @sha256:<64-hex> digest exit 2 without invoking cosign or docker.
test_no_digest() {
  local bad_ref
  for bad_ref in \
    "mirror.gcr.io/renovate/renovate:44.234.0" \
    "mirror.gcr.io/renovate/renovate:latest" \
    "mirror.gcr.io/renovate/renovate:44.234.0@sha256:short"; do
    invoke_pull "ok" "ok" "1" "$bad_ref" "1" "$SIGNED_REPO"
    if [ "$RUN_RC" -ne 2 ] || [ -n "$RUN_OUT" ] || [ -s "$STUB_COSIGN_LOG" ] || [ -s "$STUB_DOCKER_LOG" ]; then
      fail_case "no-digest" "ref=$bad_ref rc=$RUN_RC (want 2) stdout=[$RUN_OUT]"
    fi
  done
  echo "ok   no-digest"
}

# test_missing_env: missing RENOVATE_IMAGE or RENOVATE_SIGNED_REPO exits 2 without invoking cosign or docker.
test_missing_env() {
  invoke_pull "ok" "ok" "0" "" "1" "$SIGNED_REPO"
  if [ "$RUN_RC" -ne 2 ] || [ -n "$RUN_OUT" ] || [ -s "$STUB_COSIGN_LOG" ] || [ -s "$STUB_DOCKER_LOG" ]; then
    fail_case "missing-env" "missing RENOVATE_IMAGE gave rc=$RUN_RC (want 2)"
  fi
  invoke_pull "ok" "ok" "1" "$MIRROR_REF" "0" ""
  if [ "$RUN_RC" -ne 2 ] || [ -n "$RUN_OUT" ] || [ -s "$STUB_COSIGN_LOG" ] || [ -s "$STUB_DOCKER_LOG" ]; then
    fail_case "missing-env" "missing RENOVATE_SIGNED_REPO gave rc=$RUN_RC (want 2)"
  fi
  echo "ok   missing-env"
}

# test_cosign_verified_before_pull: cosign verifies GHCR_REF with a 300s deadline before mirror pull.
test_cosign_verified_before_pull() {
  invoke_pull "ok" "refuse" "1" "$MIRROR_REF" "1" "$SIGNED_REPO" "ok"
  if [ "$RUN_RC" -ne 0 ] || [ "$RUN_OUT" != "$MIRROR_REF" ]; then
    fail_case "cosign-verified-before-pull" "rc=$RUN_RC stdout=[$RUN_OUT] stderr=[$RUN_ERR]"
  fi
  local want_cosign got_cosign want_timeouts got_timeouts
  want_cosign="verify --certificate-oidc-issuer ${SIGNER_ISSUER} --certificate-identity ${SIGNER_IDENTITY} ${GHCR_REF}"
  got_cosign="$(cat "$STUB_COSIGN_LOG")"
  if [ "$got_cosign" != "$want_cosign" ] || [ "$(wc -l < "$STUB_COSIGN_LOG" | tr -d ' ')" -ne 1 ]; then
    fail_case "cosign-verified-before-pull" "cosign log mismatch: got [$got_cosign], want [$want_cosign]"
  fi
  assert_log_lines "cosign-verified-before-pull" "pull --quiet $MIRROR_REF" 0
  want_timeouts="$(printf '300\n90')"
  got_timeouts="$(cat "$STUB_TIMEOUT_LOG")"
  if [ "$got_timeouts" != "$want_timeouts" ]; then
    fail_case "cosign-verified-before-pull" "timeout order mismatch: got [$got_timeouts], want [$want_timeouts]"
  fi
  echo "ok   cosign-verified-before-pull"
}

# test_cosign_refused_no_pull: failed cosign check exits 3 without pulling any image and names GHCR_REF.
test_cosign_refused_no_pull() {
  invoke_pull "ok" "ok" "1" "$MIRROR_REF" "1" "$SIGNED_REPO" "refuse"
  if [ "$RUN_RC" -ne 3 ] || [ -n "$RUN_OUT" ]; then
    fail_case "cosign-refused-no-pull" "rc=$RUN_RC (want 3) stdout=[$RUN_OUT] stderr=[$RUN_ERR]"
  fi
  if [ -s "$STUB_DOCKER_LOG" ]; then
    fail_case "cosign-refused-no-pull" "docker was called on signature failure: $(cat "$STUB_DOCKER_LOG")"
  fi
  local want_cosign got_cosign
  want_cosign="verify --certificate-oidc-issuer ${SIGNER_ISSUER} --certificate-identity ${SIGNER_IDENTITY} ${GHCR_REF}"
  got_cosign="$(cat "$STUB_COSIGN_LOG")"
  if [ "$got_cosign" != "$want_cosign" ]; then
    fail_case "cosign-refused-no-pull" "cosign log mismatch: got [$got_cosign], want [$want_cosign]"
  fi
  case "$RUN_ERR" in
    *"$GHCR_REF"*) ;;
    *) fail_case "cosign-refused-no-pull" "stderr missing '$GHCR_REF': $RUN_ERR" ;;
  esac
  echo "ok   cosign-refused-no-pull"
}

# test_signer_env_missing: missing RENOVATE_SIGNER or RENOVATE_SIGNER_ISSUER exits 2 with empty logs.
test_signer_env_missing() {
  invoke_pull "ok" "ok" "1" "$MIRROR_REF" "1" "$SIGNED_REPO" "ok" "1" "0"
  if [ "$RUN_RC" -ne 2 ] || [ -n "$RUN_OUT" ] || [ -s "$STUB_COSIGN_LOG" ] || [ -s "$STUB_DOCKER_LOG" ]; then
    fail_case "signer-env-missing" "missing RENOVATE_SIGNER gave rc=$RUN_RC cosign=[$(cat "$STUB_COSIGN_LOG")] docker=[$(cat "$STUB_DOCKER_LOG")]"
  fi
  case "$RUN_ERR" in
    *RENOVATE_SIGNER*) ;;
    *) fail_case "signer-env-missing" "stderr missing RENOVATE_SIGNER: $RUN_ERR" ;;
  esac
  invoke_pull "ok" "ok" "1" "$MIRROR_REF" "1" "$SIGNED_REPO" "ok" "0" "1"
  if [ "$RUN_RC" -ne 2 ] || [ -n "$RUN_OUT" ] || [ -s "$STUB_COSIGN_LOG" ] || [ -s "$STUB_DOCKER_LOG" ]; then
    fail_case "signer-env-missing" "missing RENOVATE_SIGNER_ISSUER gave rc=$RUN_RC cosign=[$(cat "$STUB_COSIGN_LOG")] docker=[$(cat "$STUB_DOCKER_LOG")]"
  fi
  case "$RUN_ERR" in
    *RENOVATE_SIGNER_ISSUER*) ;;
    *) fail_case "signer-env-missing" "stderr missing RENOVATE_SIGNER_ISSUER: $RUN_ERR" ;;
  esac
  echo "ok   signer-env-missing"
}

# test_repo_malformed: malformed RENOVATE_SIGNED_REPO values exit 2 without invoking cosign or docker.
test_repo_malformed() {
  local -a bad_repos=(
    "renovate"
    "ghcr.io/renovate"
    "renovatebot/renovate"
    "ghcr.io/renovatebot/renovate:44"
    "ghcr.io/renovatebot/renovate@sha256:${DIGEST}"
    "Ghcr.io/renovatebot/renovate"
    "ghcr.io//renovate"
  )
  local idx bad_repo
  for ((idx = 0; idx < ${#bad_repos[@]}; idx++)); do
    bad_repo="${bad_repos[$idx]}"
    invoke_pull "ok" "ok" "1" "$MIRROR_REF" "1" "$bad_repo"
    if [ "$RUN_RC" -ne 2 ] || [ -n "$RUN_OUT" ] || [ -s "$STUB_COSIGN_LOG" ] || [ -s "$STUB_DOCKER_LOG" ]; then
      fail_case "repo-malformed" "repo=$bad_repo rc=$RUN_RC (want 2) cosign=[$(cat "$STUB_COSIGN_LOG")] docker=[$(cat "$STUB_DOCKER_LOG")]"
    fi
  done
  echo "ok   repo-malformed"
}

# test_repo_accepted: multi-segment and port-bearing repositories pass validation, cosign and fallback pull.
test_repo_accepted() {
  local -a good_repos=(
    "ghcr.io/a/b/c"
    "registry.example.com:5000/owner/repo"
  )
  local idx repo want_ref want_cosign got_cosign expected_docker
  for ((idx = 0; idx < ${#good_repos[@]}; idx++)); do
    repo="${good_repos[$idx]}"
    want_ref="${repo}@sha256:${DIGEST}"
    invoke_pull "refuse" "ok" "1" "$MIRROR_REF" "1" "$repo" "ok"
    if [ "$RUN_RC" -ne 0 ] || [ "$RUN_OUT" != "$want_ref" ]; then
      fail_case "repo-accepted" "repo=$repo rc=$RUN_RC stdout=[$RUN_OUT] want=[$want_ref] stderr=[$RUN_ERR]"
    fi
    want_cosign="verify --certificate-oidc-issuer ${SIGNER_ISSUER} --certificate-identity ${SIGNER_IDENTITY} ${want_ref}"
    got_cosign="$(cat "$STUB_COSIGN_LOG")"
    if [ "$got_cosign" != "$want_cosign" ]; then
      fail_case "repo-accepted" "repo=$repo cosign log mismatch: got [$got_cosign], want [$want_cosign]"
    fi
    expected_docker="$(printf 'pull --quiet %s\npull --quiet %s\npull --quiet %s\npull --quiet %s' \
      "$MIRROR_REF" "$MIRROR_REF" "$MIRROR_REF" "$want_ref")"
    assert_log_lines "repo-accepted" "$expected_docker" 2
  done
  echo "ok   repo-accepted"
}

SUITE_STATUS=0
( test_mirror_ok ) || SUITE_STATUS=1
( test_mirror_refused_fallback ) || SUITE_STATUS=1
( test_both_refused ) || SUITE_STATUS=1
( test_no_digest ) || SUITE_STATUS=1
( test_missing_env ) || SUITE_STATUS=1
( test_cosign_verified_before_pull ) || SUITE_STATUS=1
( test_cosign_refused_no_pull ) || SUITE_STATUS=1
( test_signer_env_missing ) || SUITE_STATUS=1
( test_repo_malformed ) || SUITE_STATUS=1
( test_repo_accepted ) || SUITE_STATUS=1
exit "$SUITE_STATUS"
