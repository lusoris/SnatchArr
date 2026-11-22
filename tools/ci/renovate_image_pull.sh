#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Pull the digest-pinned Renovate container image from mirror.gcr.io, falling back
# to the cosign-verified ghcr.io repository at the exact same @sha256 digest when
# mirror.gcr.io refuses the pull (for example on upstream quota exhaustion).
# mirror.gcr.io carries no signatures, so the identical digest is verified on RENOVATE_SIGNED_REPO before either source is pulled.
#
# Usage:
#   RENOVATE_IMAGE=<name>:<tag>@sha256:<64-hex> \
#   RENOVATE_SIGNED_REPO=<host>/<owner>/<repo> \
#   RENOVATE_SIGNER_ISSUER=<oidc-issuer-url> \
#   RENOVATE_SIGNER=<certificate-identity-uri> \
#     bash tools/ci/renovate_image_pull.sh
#
# Stdout:
#   Exactly one line on exit 0: the local image reference that succeeded.
#
# Exit status:
#   0  mirror or signed fallback succeeded
#   1  both sources failed after 3 attempts each (both failures named on stderr)
#   2  usage error (positional args, missing env, invalid repo, or ref without @sha256:)
#   3  signature verification failed (no pull attempted)
#
# Worst-case runtime arithmetic against the 25 min (1500 s) workflows job timeout:
#   - pull timeouts: 2 sources * 3 attempts * 90 s = 540 s
#   - jittered sleeps: 2 sources * ((6 + 3) s + (12 + 6) s) = 54 s
#   - cosign verify (run inside this helper before any pull): 300 s
#   - renovate_presets.py: 120 s
#   Total: 540 + 54 + 300 + 120 = 1014 s (16 min 54 s) < 1500 s (25 min).
#
# Platforms: bash with GNU coreutils timeout, cosign and docker on Linux CI and maintainer hosts;
# macOS and Windows skipped where GNU timeout, cosign or Docker is absent.
set -euo pipefail

readonly PULL_SOURCES=2
readonly PULL_MAX_ATTEMPTS=3
readonly PULL_TIMEOUT_SECONDS=90
readonly PULL_BASE_WAIT_SECONDS=6
readonly COSIGN_TIMEOUT_SECONDS=300

LAST_FAILURE=""

# usage_error MESSAGE: print a usage diagnostic to stderr and exit 2.
usage_error() {
  printf 'renovate_image_pull: %s\n' "$1" >&2
  exit 2
}

# validate_inputs ARGC: require zero positional args and valid env pins.
validate_inputs() {
  if [ "$1" -ne 0 ]; then
    usage_error "usage: RENOVATE_IMAGE=<name>:<tag>@sha256:<64-hex> RENOVATE_SIGNED_REPO=<host>/<owner>/<repo> RENOVATE_SIGNER_ISSUER=<issuer> RENOVATE_SIGNER=<identity> renovate_image_pull.sh"
  fi
  if [ -z "${RENOVATE_IMAGE:-}" ]; then
    usage_error "missing environment variable: RENOVATE_IMAGE"
  fi
  if [ -z "${RENOVATE_SIGNED_REPO:-}" ]; then
    usage_error "missing environment variable: RENOVATE_SIGNED_REPO"
  fi
  if [ -z "${RENOVATE_SIGNER_ISSUER:-}" ]; then
    usage_error "missing environment variable: RENOVATE_SIGNER_ISSUER"
  fi
  if [ -z "${RENOVATE_SIGNER:-}" ]; then
    usage_error "missing environment variable: RENOVATE_SIGNER"
  fi
  local image_re='^[a-z0-9][a-z0-9./-]*(:[A-Za-z0-9._-]+)?@sha256:[0-9a-f]{64}$'
  if [[ ! "$RENOVATE_IMAGE" =~ $image_re ]]; then
    usage_error "RENOVATE_IMAGE must carry @sha256:<64-hex>: $RENOVATE_IMAGE"
  fi
  local repo_re='^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+(:[0-9]{1,5})?(/[a-z0-9]+([._-][a-z0-9]+)*){2,}$'
  if [[ ! "$RENOVATE_SIGNED_REPO" =~ $repo_re ]]; then
    usage_error "RENOVATE_SIGNED_REPO must name <host>/<owner>/<repo> without tag or digest: $RENOVATE_SIGNED_REPO"
  fi
}

# verify_signature REF: verify the keyless Sigstore signature on REF before any image pull.
verify_signature() {
  local ref="$1" rc=0 err
  err="$(timeout "$COSIGN_TIMEOUT_SECONDS" cosign verify \
    --certificate-oidc-issuer "$RENOVATE_SIGNER_ISSUER" \
    --certificate-identity "$RENOVATE_SIGNER" \
    "$ref" 2>&1 > /dev/null)" || rc=$?
  if [ "$rc" -ne 0 ]; then
    printf 'renovate_image_pull: signature verification failed for %s (exit %s): %s\n' \
      "$ref" "$rc" "${err:-no output}" >&2
    return 3
  fi
}

# pull_source LABEL TARGET EXPECTED_DIGEST: try TARGET up to PULL_MAX_ATTEMPTS times with jittered backoff.
pull_source() {
  local label="$1" target="$2" expected_digest="$3"
  if [[ "$target" != *"@$expected_digest" ]]; then
    LAST_FAILURE="$label ($target): refused reference not pinned to $expected_digest"
    printf 'renovate_image_pull: %s\n' "$LAST_FAILURE" >&2
    return 2
  fi
  local prefix="${target%"@$expected_digest"}"
  local prefix_re='^[a-z0-9][a-z0-9.-]*(:[0-9]{1,5})?(/[a-z0-9]+([._-][a-z0-9]+)*)+(:[A-Za-z0-9._-]+)?$'
  if [[ ! "$prefix" =~ $prefix_re ]]; then
    LAST_FAILURE="$label ($target): refused invalid image prefix '$prefix'"
    printf 'renovate_image_pull: %s\n' "$LAST_FAILURE" >&2
    return 2
  fi
  local wait="$PULL_BASE_WAIT_SECONDS" attempt rc out pause
  for ((attempt = 1; attempt <= PULL_MAX_ATTEMPTS; attempt++)); do
    rc=0
    out="$(timeout "$PULL_TIMEOUT_SECONDS" docker pull --quiet "$target" 2>&1)" || rc=$?
    if [ "$rc" -eq 0 ]; then
      return 0
    fi
    LAST_FAILURE="$label ($target) attempt $attempt/$PULL_MAX_ATTEMPTS exited $rc: ${out:-no output}"
    if [ "$attempt" -lt "$PULL_MAX_ATTEMPTS" ]; then
      pause=$((wait + RANDOM % (wait / 2 + 1)))
      printf 'renovate_image_pull: %s; retrying in %ss\n' "$LAST_FAILURE" "$pause" >&2
      sleep "$pause"
      wait=$((wait * 2))
    else
      printf 'renovate_image_pull: %s\n' "$LAST_FAILURE" >&2
    fi
  done
  return 1
}

# main ARGS...: verify signature on fallback ref, pull from mirror or fallback, and print the ref.
main() {
  validate_inputs "$#"
  local digest="sha256:${RENOVATE_IMAGE##*@sha256:}"
  local fallback_ref="${RENOVATE_SIGNED_REPO}@${digest}"
  local verify_rc=0
  verify_signature "$fallback_ref" || verify_rc=$?
  if [ "$verify_rc" -ne 0 ]; then
    return "$verify_rc"
  fi
  local -a labels=("mirror" "signed-fallback")
  local -a refs=("$RENOVATE_IMAGE" "$fallback_ref")
  local -a failures=()
  local idx rc
  for ((idx = 0; idx < PULL_SOURCES; idx++)); do
    rc=0
    pull_source "${labels[$idx]}" "${refs[$idx]}" "$digest" || rc=$?
    if [ "$rc" -eq 0 ]; then
      printf '%s\n' "${refs[$idx]}"
      return 0
    fi
    if [ "$rc" -eq 2 ]; then
      return 2
    fi
    failures+=("$LAST_FAILURE")
  done
  printf 'renovate_image_pull: both sources failed:\n  1. %s\n  2. %s\n' \
    "${failures[0]}" "${failures[1]}" >&2
  return 1
}

main "$@"
