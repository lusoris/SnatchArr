#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Replay module download results through a local go stub, including a reduced-bound plant:
#
#   tools/ci/go_mod_download_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs sed and cmp.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

HERE="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
# shellcheck source=/dev/null
. "$HERE/test_lib.sh"
case "${TMPDIR:-}" in '' | /*) ;; *) TMPDIR="$(sh "$HERE/tmpdir_abs.sh" "$TMPDIR")"; export TMPDIR ;; esac
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' 0
SCENARIO="$WORK/scenario"
COUNTER="$WORK/counter"
export SCENARIO COUNTER
PATH="$WORK:$PATH"
export PATH
cat >"$WORK/go" <<'EOF'
#!/bin/sh
set -eu
if [ "$#" -ne 2 ] || [ "$1" != mod ] || [ "$2" != download ]; then
  echo 'FAIL go stub arguments' >&2
  exit 2
fi
count="$(cat "$COUNTER")"
count=$((count + 1))
printf '%s\n' "$count" >"$COUNTER"
line="$(sed -n "${count}p" "$SCENARIO")"
case "$line" in
  *'|'*) ;;
  *) echo 'FAIL go stub scenario exhausted' >&2; exit 2 ;;
esac
printf '%s\n' "${line#*|}"
exit "${line%%|*}"
EOF
chmod +x "$WORK/go"

# run_case NAME WANT_RC NEEDLE WANT_CALLS BACKOFF SCRIPT SCENARIO_LINE...
run_case() {
  case_name="$1"
  want_rc="$2"
  needle="$3"
  want_calls="$4"
  case_backoff="$5"
  script="$6"
  shift 6
  printf '%s\n' "$@" >"$SCENARIO"
  printf '0\n' >"$COUNTER"
  rc=0
  out="$(GO_MOD_DOWNLOAD_BACKOFF="$case_backoff" run_bounded 60 sh "$script" 2>&1)" || rc=$?
  check_case "$case_name" "$want_rc" "$rc" "$needle" "$out"
  calls="$(cat "$COUNTER")"
  if [ "$calls" -ne "$want_calls" ]; then
    printf 'FAIL %s: go called %s times, want %s\n%s\n' "$case_name" "$calls" "$want_calls" "$out" >&2
    exit 1
  fi
  printf 'ok %s\n' "$case_name"
}

SCRIPT="$HERE/go_mod_download.sh"
run_case first_ok 0 'download ok' 1 '0 0' "$SCRIPT" '0|download ok'
run_case transient_then_ok 0 'attempt 2 of 3 failed on a transient network error' 3 '0 0' "$SCRIPT" \
  '1|stream error: stream ID 135; INTERNAL_ERROR; received from peer' \
  '1|stream error: stream ID 135; INTERNAL_ERROR; received from peer' '0|download ok'
run_case transient_exhausted 1 'gave up after 3 attempts' 3 '0 0' "$SCRIPT" \
  '1|503 Service Unavailable' '1|503 Service Unavailable' '1|503 Service Unavailable'
run_case hard_fail 1 'not a transient network error' 1 '0 0' "$SCRIPT" '1|unknown revision v9.9.9'
run_case verify_fail 1 'module verification failed' 1 '0 0' "$SCRIPT" \
  '1|verifying example.com/m@v1.0.0: checksum mismatch; stream error:'
run_case bad_backoff 2 'usage:' 0 '5' "$SCRIPT" '0|download ok'
run_case empty_backoff 2 'usage:' 0 '' "$SCRIPT" '0|download ok'
run_case blank_backoff 2 'usage:' 0 ' ' "$SCRIPT" '0|download ok'
run_case multiline_backoff 2 'usage:' 0 '5 15
x y' "$SCRIPT" '0|download ok'
run_case two_valid_lines_backoff 2 'usage:' 0 '0 0
999 999' "$SCRIPT" '0|download ok'
run_case tab_backoff 0 'download ok' 1 '0	0' "$SCRIPT" '0|download ok'

cp "$SCRIPT" "$WORK/one_attempt.sh"
cp "$HERE/run_bounded.py" "$WORK/run_bounded.py"
sed 's/^MAX_ATTEMPTS=3$/MAX_ATTEMPTS=1/' "$WORK/one_attempt.sh" >"$WORK/mutated.sh"
if cmp -s "$WORK/one_attempt.sh" "$WORK/mutated.sh"; then
  echo 'FAIL planted attempt-bound mutation did not change the copy' >&2
  exit 1
fi
run_case planted 1 'gave up after 3 attempts' 1 '0 0' \
  "$WORK/mutated.sh" '1|stream error: stream ID 135; INTERNAL_ERROR; received from peer' \
  '1|stream error: stream ID 135; INTERNAL_ERROR; received from peer' '0|download ok'
echo 'go_mod_download_test: ok (12 cases)'
