#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
set -euo pipefail

readonly NPM_TIMEOUT_SECONDS=120

TMP_DIR=""
PREFIX=""
PNPM_VERSION=""
DIGEST_ALGO=""
EXPECTED_HEX=""

# Remove the temporary pack directory on script exit when one was created.
cleanup() {
  if [[ -n "${TMP_DIR:-}" ]]; then
    rm -rf -- "$TMP_DIR"
  fi
}

# Parse command-line arguments: accept none or `--prefix DIR`, else exit 2.
parse_args() {
  case "$#" in
    0)
      PREFIX=""
      ;;
    2)
      if [[ "$1" != "--prefix" || -z "$2" ]]; then
        printf 'install-pnpm: usage: %s [--prefix DIR] (got %s %s)\n' "$0" "$1" "$2" >&2
        exit 2
      fi
      PREFIX="$2"
      ;;
    *)
      printf 'install-pnpm: usage: %s [--prefix DIR] (got %s)\n' "$0" "$*" >&2
      exit 2
      ;;
  esac
}

# Allow-list parse packageManager: `pnpm@X.Y.Z[+<algo>.<hex>]` per corepack format.
parse_package_manager() {
  local pm="$1"
  local LC_ALL=C
  if [[ "$pm" == *$'\n'* || "$pm" == *$'\r'* ]]; then
    printf "install-pnpm: invalid packageManager '%s'\n" "$pm" >&2
    exit 2
  fi
  if [[ ! "$pm" =~ ^pnpm@([0-9]+\.[0-9]+\.[0-9]+)(\+([a-z0-9]+)\.([0-9a-f]+))?$ ]]; then
    printf "install-pnpm: invalid packageManager '%s': expected pnpm@<x.y.z>[+<algo>.<hex>]\n" "$pm" >&2
    exit 2
  fi
  PNPM_VERSION="${BASH_REMATCH[1]}"
  DIGEST_ALGO="${BASH_REMATCH[3]:-}"
  EXPECTED_HEX="${BASH_REMATCH[4]:-}"
  case "${DIGEST_ALGO}:${#EXPECTED_HEX}" in
    :0 | sha1:40 | sha224:56 | sha256:64 | sha512:128) ;;
    *)
      printf "install-pnpm: invalid packageManager '%s': unsupported digest %s (%d hex chars)\n" \
        "$pm" "$DIGEST_ALGO" "${#EXPECTED_HEX}" >&2
      exit 2
      ;;
  esac
}

# Verify the packed tarball against a declared digest, or report that none is declared.
verify_digest() {
  local tarball="$1"
  local pm="$2"
  local sum_out=""
  local actual_hex=""
  if [[ -z "$DIGEST_ALGO" ]]; then
    printf 'install-pnpm: packageManager %s declares no digest; continuing without digest check\n' "$pm"
    return 0
  fi
  case "$DIGEST_ALGO" in
    sha1) sum_out=$(sha1sum <"$tarball") ;;
    sha224) sum_out=$(sha224sum <"$tarball") ;;
    sha256) sum_out=$(sha256sum <"$tarball") ;;
    sha512) sum_out=$(sha512sum <"$tarball") ;;
    *)
      printf 'install-pnpm: unsupported digest algorithm: %s\n' "$DIGEST_ALGO" >&2
      exit 2
      ;;
  esac
  actual_hex="${sum_out%% *}"
  if [[ "$actual_hex" != "$EXPECTED_HEX" ]]; then
    printf 'install-pnpm: %s digest mismatch for pnpm@%s: expected %s, got %s\n' \
      "$DIGEST_ALGO" "$PNPM_VERSION" "$EXPECTED_HEX" "$actual_hex" >&2
    exit 1
  fi
}

# Read packageManager from ./package.json, pack pnpm, verify digest, and install globally.
main() {
  parse_args "$@"
  if [[ ! -f ./package.json ]]; then
    printf 'install-pnpm: ./package.json not found in %s\n' "$PWD" >&2
    exit 2
  fi
  local pm
  pm=$(node -p 'require("./package.json").packageManager')
  parse_package_manager "$pm"

  local npm_bin="${NPM:-npm}"
  trap cleanup EXIT
  TMP_DIR=$(mktemp -d)

  local packed
  packed=$(timeout "$NPM_TIMEOUT_SECONDS" "$npm_bin" pack "pnpm@${PNPM_VERSION}" --pack-destination "$TMP_DIR")
  if [[ -z "$packed" || "$packed" == */* || "$packed" == *$'\n'* || "$packed" == "." || "$packed" == ".." ]]; then
    printf "install-pnpm: unexpected npm pack output: '%s'\n" "$packed" >&2
    exit 1
  fi
  local tarball="${TMP_DIR}/${packed}"
  if [[ ! -f "$tarball" || -L "$tarball" ]]; then
    printf 'install-pnpm: packed tarball missing: %s\n' "$tarball" >&2
    exit 1
  fi

  verify_digest "$tarball" "$pm"

  local -a install_args=(install --global --no-audit --no-fund --loglevel=error)
  if [[ -n "$PREFIX" ]]; then
    install_args+=(--prefix "$PREFIX")
  fi
  install_args+=("$tarball")
  timeout "$NPM_TIMEOUT_SECONDS" "$npm_bin" "${install_args[@]}"
}

main "$@"
