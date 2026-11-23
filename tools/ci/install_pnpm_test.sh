#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
set -euo pipefail

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH='' cd -- "$SCRIPT_DIR/../.." && pwd)
INSTALLER="${INSTALL_PNPM_SCRIPT:-$REPO_ROOT/web/scripts/install-pnpm.sh}"

WORK_DIR=""
STUB_NPM=""
FIXTURE_TGZ=""
CASE_DIR=""
LAST_OUT=""
LAST_STATUS=0

# Remove the temporary test workspace on exit.
cleanup_work() {
  if [[ -n "${WORK_DIR:-}" ]]; then
    rm -rf -- "$WORK_DIR"
  fi
}

# Report a failed test case to stderr and exit 1.
fail_case() {
  printf 'FAIL %s: %s\n' "$1" "$2" >&2
  exit 1
}

# Write an offline stub npm binary that packs the fixture tarball and logs installs.
write_stub_npm() {
  cat <<'EOF' >"$STUB_NPM"
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$STUB_CALLS_LOG"
if [[ "$#" -eq 0 ]]; then
  printf 'stub-npm: missing subcommand\n' >&2
  exit 2
fi
subcmd="$1"
shift
case "$subcmd" in
  pack)
    if [[ "${STUB_PACK_EXIT:-0}" -ne 0 ]]; then
      printf 'stub-npm: simulated pack failure\n' >&2
      exit "$STUB_PACK_EXIT"
    fi
    spec=""
    dest=""
    for ((i = 0; i < 16 && $# > 0; i++)); do
      if [[ "$1" == "--pack-destination" && "$#" -ge 2 ]]; then
        dest="$2"
        shift 2
      else
        spec="$1"
        shift
      fi
    done
    if [[ -z "$spec" || -z "$dest" || ! -d "$dest" ]]; then
      printf 'stub-npm: invalid pack args\n' >&2
      exit 2
    fi
    printf '%s\n' "$spec" >"$STUB_PACK_SPEC_LOG"
    printf '%s\n' "$dest" >"$STUB_PACK_DEST_LOG"
    base=$(basename -- "$STUB_TARBALL")
    cp -- "$STUB_TARBALL" "${dest}/${base}"
    printf '%s\n' "$base"
    ;;
  install)
    printf '%s\n' "$@" >"$STUB_INSTALL_LOG"
    ;;
  *)
    printf 'stub-npm: unexpected subcommand %s\n' "$subcmd" >&2
    exit 2
    ;;
esac
EOF
  chmod +x "$STUB_NPM"
}

# Run install-pnpm.sh in a fresh fixture directory for one case.
run_installer() {
  local name="$1"
  local pm="$2"
  local pack_exit="$3"
  shift 3
  CASE_DIR="${WORK_DIR}/${name}"
  mkdir -p "${CASE_DIR}/web"
  printf '{"name":"fixture","private":true,"packageManager":"%s"}\n' "$pm" >"${CASE_DIR}/web/package.json"
  LAST_STATUS=0
  LAST_OUT=$(
    cd "${CASE_DIR}/web"
    NPM="$STUB_NPM" \
      STUB_TARBALL="$FIXTURE_TGZ" \
      STUB_CALLS_LOG="${CASE_DIR}/calls.log" \
      STUB_PACK_SPEC_LOG="${CASE_DIR}/pack_spec.log" \
      STUB_PACK_DEST_LOG="${CASE_DIR}/pack_dest.log" \
      STUB_INSTALL_LOG="${CASE_DIR}/install.log" \
      STUB_PACK_EXIT="$pack_exit" \
      bash "$INSTALLER" "$@" 2>&1
  ) || LAST_STATUS=$?
}

# Assert that stub npm packed pnpm@12.15.0, cleaned up mktemp, and installed the tarball.
assert_installed_tarball() {
  local name="$1"
  local prefix="${2:-}"
  if [[ ! -f "${CASE_DIR}/pack_spec.log" || ! -f "${CASE_DIR}/pack_dest.log" ]]; then
    fail_case "$name" "npm pack was not recorded"
  fi
  local spec dest want_install got_install
  spec=$(cat "${CASE_DIR}/pack_spec.log")
  dest=$(cat "${CASE_DIR}/pack_dest.log")
  if [[ "$spec" != "pnpm@12.15.0" ]]; then
    fail_case "$name" "pack spec '${spec}', want pnpm@12.15.0"
  fi
  if [[ -e "$dest" ]]; then
    fail_case "$name" "temp pack dir '${dest}' was not removed on exit"
  fi
  if [[ ! -f "${CASE_DIR}/install.log" ]]; then
    fail_case "$name" "npm install was not recorded"
  fi
  if [[ -n "$prefix" ]]; then
    want_install=$(printf '%s\n' --global --no-audit --no-fund --loglevel=error --prefix "$prefix" "${dest}/pnpm-12.15.0.tgz")
  else
    want_install=$(printf '%s\n' --global --no-audit --no-fund --loglevel=error "${dest}/pnpm-12.15.0.tgz")
  fi
  got_install=$(cat "${CASE_DIR}/install.log")
  if [[ "$got_install" != "$want_install" ]]; then
    fail_case "$name" "install args '${got_install}' != '${want_install}'"
  fi
}

# Verify a valid digest-pinned packageManager installs the packed tarball.
expect_digest_match() {
  local name="$1"
  local pm="$2"
  run_installer "$name" "$pm" 0
  if [[ "$LAST_STATUS" -ne 0 ]]; then
    fail_case "$name" "exit ${LAST_STATUS}, want 0 (${LAST_OUT})"
  fi
  assert_installed_tarball "$name"
  printf 'ok   %s\n' "$name"
}

# Verify a malformed packageManager exits 2 naming the value without calling npm.
expect_malformed_pm() {
  local name="$1"
  local pm="$2"
  local needle="$3"
  run_installer "$name" "$pm" 0
  if [[ "$LAST_STATUS" -ne 2 ]]; then
    fail_case "$name" "exit ${LAST_STATUS}, want 2 (${LAST_OUT})"
  fi
  if [[ "$LAST_OUT" != *"$needle"* ]]; then
    fail_case "$name" "output missing '${needle}': ${LAST_OUT}"
  fi
  if [[ -e "${CASE_DIR}/calls.log" ]]; then
    fail_case "$name" "npm was called on malformed packageManager"
  fi
  printf 'ok   %s\n' "$name"
}

# Verify a digest mismatch exits 1, names both digests, and performs no install.
test_digest_mismatch() {
  local actual_sha256="$1"
  local first_char="0"
  if [[ "${actual_sha256:0:1}" == "0" ]]; then
    first_char="1"
  fi
  local wrong_sha256="${first_char}${actual_sha256:1}"
  run_installer "digest-mismatch" "pnpm@12.15.0+sha256.${wrong_sha256}" 0
  if [[ "$LAST_STATUS" -ne 1 ]]; then
    fail_case "digest-mismatch" "exit ${LAST_STATUS}, want 1 (${LAST_OUT})"
  fi
  if [[ "$LAST_OUT" != *"$wrong_sha256"* || "$LAST_OUT" != *"$actual_sha256"* ]]; then
    fail_case "digest-mismatch" "output did not name expected and actual digests: ${LAST_OUT}"
  fi
  if [[ -e "${CASE_DIR}/install.log" ]]; then
    fail_case "digest-mismatch" "npm install ran despite digest mismatch"
  fi
  local dest
  dest=$(cat "${CASE_DIR}/pack_dest.log")
  if [[ -e "$dest" ]]; then
    fail_case "digest-mismatch" "temp pack dir '${dest}' was not removed on exit"
  fi
  printf 'ok   digest-mismatch\n'
}

# Verify packageManager without a digest prints the no-digest notice and installs.
test_no_digest() {
  run_installer "no-digest" "pnpm@12.15.0" 0
  if [[ "$LAST_STATUS" -ne 0 ]]; then
    fail_case "no-digest" "exit ${LAST_STATUS}, want 0 (${LAST_OUT})"
  fi
  if [[ "$LAST_OUT" != *"declares no digest"* ]]; then
    fail_case "no-digest" "output missing no-digest line: ${LAST_OUT}"
  fi
  assert_installed_tarball "no-digest"
  printf 'ok   no-digest\n'
}

# Verify `--prefix DIR` forwards `--prefix DIR` to npm install.
test_prefix() {
  local sha256_hex="$1"
  local target_prefix="${WORK_DIR}/prefix-target"
  run_installer "prefix" "pnpm@12.15.0+sha256.${sha256_hex}" 0 --prefix "$target_prefix"
  if [[ "$LAST_STATUS" -ne 0 ]]; then
    fail_case "prefix" "exit ${LAST_STATUS}, want 0 (${LAST_OUT})"
  fi
  assert_installed_tarball "prefix" "$target_prefix"
  printf 'ok   prefix\n'
}

# Verify unknown CLI arguments exit 2 without calling npm.
test_unknown_argument() {
  run_installer "malformed-unknown-arg" "pnpm@12.15.0" 0 --bogus
  if [[ "$LAST_STATUS" -ne 2 ]]; then
    fail_case "malformed-unknown-arg" "exit ${LAST_STATUS}, want 2 (${LAST_OUT})"
  fi
  if [[ -e "${CASE_DIR}/calls.log" ]]; then
    fail_case "malformed-unknown-arg" "npm was called on unknown argument"
  fi
  printf 'ok   malformed-unknown-arg\n'
}

# Verify a failing npm pack exits non-zero and never invokes npm install.
test_pack_failure() {
  local sha256_hex="$1"
  run_installer "pack-failure" "pnpm@12.15.0+sha256.${sha256_hex}" 7
  if [[ "$LAST_STATUS" -eq 0 ]]; then
    fail_case "pack-failure" "expected non-zero exit when npm pack fails"
  fi
  if [[ -e "${CASE_DIR}/install.log" ]]; then
    fail_case "pack-failure" "npm install ran after npm pack failed"
  fi
  printf 'ok   pack-failure\n'
}

# Build offline fixtures and execute all install-pnpm.sh test cases.
# Skip with a reason off Linux when a GNU coreutils tool is missing; fail on Linux.
require_tools() {
  local tool
  for tool in sha512sum sha256sum sha224sum sha1sum timeout; do
    if command -v "$tool" >/dev/null 2>&1; then
      continue
    fi
    if [[ "$(uname -s)" == Linux ]]; then
      fail_case "preflight" "${tool} not found"
    fi
    printf 'skip install_pnpm_test on %s: %s not found (GNU coreutils required)\n' "$(uname -s)" "$tool"
    exit 0
  done
}

main() {
  require_tools
  trap cleanup_work EXIT
  WORK_DIR=$(mktemp -d)
  STUB_NPM="${WORK_DIR}/stub-npm.sh"
  write_stub_npm

  mkdir -p "${WORK_DIR}/pkg/package" "${WORK_DIR}/tgz"
  printf '{"name":"pnpm","version":"12.15.0"}\n' >"${WORK_DIR}/pkg/package/package.json"
  FIXTURE_TGZ="${WORK_DIR}/tgz/pnpm-12.15.0.tgz"
  tar -czf "$FIXTURE_TGZ" -C "${WORK_DIR}/pkg" package

  local sha512_out sha256_out sha224_out sha1_out sha512_hex sha256_hex sha224_hex sha1_hex
  sha512_out=$(sha512sum <"$FIXTURE_TGZ")
  sha256_out=$(sha256sum <"$FIXTURE_TGZ")
  sha224_out=$(sha224sum <"$FIXTURE_TGZ")
  sha1_out=$(sha1sum <"$FIXTURE_TGZ")
  sha512_hex="${sha512_out%% *}"
  sha256_hex="${sha256_out%% *}"
  sha224_hex="${sha224_out%% *}"
  sha1_hex="${sha1_out%% *}"

  local upper_sha512=""
  local i
  for ((i = 0; i < 64; i++)); do
    upper_sha512+="AB"
  done

  expect_digest_match "digest-match-sha512" "pnpm@12.15.0+sha512.${sha512_hex}"
  expect_digest_match "digest-match-sha256" "pnpm@12.15.0+sha256.${sha256_hex}"
  expect_digest_match "digest-match-sha224" "pnpm@12.15.0+sha224.${sha224_hex}"
  expect_digest_match "digest-match-sha1" "pnpm@12.15.0+sha1.${sha1_hex}"
  test_digest_mismatch "$sha256_hex"
  test_no_digest
  test_prefix "$sha256_hex"
  expect_malformed_pm "malformed-latest" "pnpm@latest" "pnpm@latest"
  expect_malformed_pm "malformed-yarn" "yarn@4.0.0" "yarn@4.0.0"
  expect_malformed_pm "malformed-md5" "pnpm@1.2.3+md5.0123456789abcdef0123456789abcdef" "pnpm@1.2.3+md5.0123456789abcdef0123456789abcdef"
  expect_malformed_pm "malformed-sha512-127" "pnpm@1.2.3+sha512.${sha512_hex:0:127}" "pnpm@1.2.3+sha512.${sha512_hex:0:127}"
  expect_malformed_pm "malformed-sha512-upper" "pnpm@1.2.3+sha512.${upper_sha512}" "pnpm@1.2.3+sha512.${upper_sha512}"
  expect_malformed_pm "malformed-empty" "" "invalid packageManager"
  test_unknown_argument
  test_pack_failure "$sha256_hex"
}

main "$@"
