#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Replay scan headers and exit statuses in a throwaway module; a removed guard must
# accept the planted distro header that the real guard refuses.
#
#   tools/ci/govulncheck_pinned_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs go and sed.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

# CDPATH is cleared for each cd below: a CDPATH match makes cd print the directory, and the
# command substitution would capture that line in front of the path pwd prints.
HERE="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
# run_in changes into the module, so an inherited relative TMPDIR is resolved here first.
case "${TMPDIR:-}" in '' | /*) ;; *) TMPDIR="$(CDPATH='' cd -- "$TMPDIR" && pwd)"; export TMPDIR ;; esac
WORK="$(mktemp -d)"
guard_pid=
scanner_pid=
# Scanner first: a guard that waits on its scan in the foreground then returns at once.
cleanup() {
  if [ -f "$WORK/scanner.pid" ]; then
    scanner_pid="$(cat "$WORK/scanner.pid")"
  fi
  # A process that already exited cannot be signalled; a signalled guard exits non-zero.
  if [ -n "$scanner_pid" ] && ! kill "$scanner_pid" 2>/dev/null; then
    : # expected: the scan already ended
  fi
  if [ -n "$guard_pid" ] && kill "$guard_pid" 2>/dev/null && ! wait "$guard_pid" 2>/dev/null; then
    : # expected: it ended on our signal
  fi
  rm -rf "$WORK"
}
trap cleanup 0
# Two platform declarations, bounded by the two-element literal list.
platforms="Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only."
for script in "$HERE/govulncheck_pinned.sh" "$HERE/govulncheck_pinned_test.sh"; do
  grep -Fxq "# $platforms" "$script" || { echo "FAIL platform reason missing: $script" >&2; exit 1; }
done
mkdir -p "$WORK/mod"
printf 'module example.invalid/m\n\ngo 1.27.0\n' >"$WORK/mod/go.mod"
cat >"$WORK/scanner" <<'EOF'
#!/bin/sh
set -eu
if [ "${SLOW:-0}" = 1 ]; then
  printf '%s\n' "$$" >"$SCANNER_PIDFILE"
  exec sleep 30
fi
printf '%s\n' "$GOTOOLCHAIN" "$@" >>"$RECORD"
cat "$HEADER"
if [ -n "${PROBE_TMP:-}" ]; then
  printf 'tmp entries: %s\n' "$(ls -A "$PROBE_TMP" | wc -l | tr -d ' ')"
fi
printf 'scan body\n'
printf 'scanner stderr\n' >&2
exit "$SCAN_RC"
EOF
chmod +x "$WORK/scanner"
RECORD="$WORK/record"
HEADER="$WORK/header"
SCAN_RC=0
export RECORD HEADER SCAN_RC
GUARD="$HERE/govulncheck_pinned.sh"

# run_in DIR [PATTERN...]: scan the throwaway module, the guard's TMPDIR set to DIR (empty:
# inherited) and the scanner counting the entries there; sets rc, stdout and combined out.
run_in() {
  dir="$1"
  shift
  : >"$RECORD"
  rc=0
  stdout="$(
    cd "$WORK/mod" || exit 2
    if [ -n "$dir" ]; then
      TMPDIR="$dir" PROBE_TMP="$dir"
      export TMPDIR PROBE_TMP
    fi
    sh "$GUARD" "$WORK/scanner" "$@" 2>"$WORK/stderr"
  )" || rc=$?
  out="$(printf '%s\n' "$stdout"; cat "$WORK/stderr")"
}
# run [PATTERN...]: run_in with the inherited TMPDIR.
run() {
  run_in '' "$@"
}
# check_empty DIR: fail the run when DIR still holds an entry after the guard ended.
check_empty() {
  entries="$(ls -A "$1")"
  if [ -n "$entries" ]; then
    printf 'FAIL temporary directory not empty: %s\n' "$1" >&2
    exit 1
  fi
}

# signal_case NAME STATE: TERM goes to the guard shell alone, never a process group.
signal_case() {
  (cd "$WORK/mod" && SLOW=1 SCANNER_PIDFILE="$WORK/scanner.pid" TMPDIR="$WORK/t" exec sh "$GUARD" "$WORK/scanner") >"$WORK/signal.out" 2>&1 &
  guard_pid=$!
  attempts=0
  # Bounded startup wait: at most 100 sleeps of 0.1 seconds.
  while [ ! -s "$WORK/scanner.pid" ] && [ "$attempts" -lt 100 ]; do
    sleep 0.1
    attempts=$((attempts + 1))
  done
  if [ ! -s "$WORK/scanner.pid" ]; then
    echo 'FAIL scanner did not publish its pid' >&2
    exit 1
  fi
  scanner_pid="$(cat "$WORK/scanner.pid")"
  kill -TERM "$guard_pid"
  if [ "$2" = stopped ]; then
    attempts=0
    # Bounded stop wait: at most 50 sleeps of 0.1 seconds, far below the planted 30-second
    # scan, so a scan the guard runs in the foreground fails here instead of passing late.
    while kill -0 "$scanner_pid" 2>/dev/null && [ "$attempts" -lt 50 ]; do
      sleep 0.1
      attempts=$((attempts + 1))
    done
    if kill -0 "$scanner_pid" 2>/dev/null; then
      echo 'FAIL scanner survived TERM to guard' >&2
      exit 1
    fi
  fi
  rc=0
  wait "$guard_pid" 2>/dev/null || rc=$?
  guard_pid=
  check_case "$1 signal status" 143 "$rc" '' "$(cat "$WORK/signal.out")"
  if [ "$2" = stopped ]; then
    check_empty "$WORK/t"
  else
    if ! kill -0 "$scanner_pid" 2>/dev/null; then
      echo 'FAIL removed traps did not leave scanner alive' >&2
      exit 1
    fi
    kill "$scanner_pid"
  fi
  scanner_pid=
  rm -f "$WORK/scanner.pid"
}
printf 'Go: go1.27.0\n' >"$HEADER"
run
check_case 'exact release' 0 "$rc" 'scan body' "$out"
expected="$(printf 'Go: go1.27.0\nscan body\nscanner stderr\n')"
if [ "$stdout" != "$expected" ]; then
  printf 'FAIL output changed:\n%s\n' "$stdout" >&2
  exit 1
fi
# One recorded call: the guard reads the header of the scan itself, never a second probe.
record="$(cat "$RECORD")"
expected="$(printf 'go1.27.0\n-show\nversion\n./...\n')"
if [ "$record" != "$expected" ]; then
  printf 'FAIL toolchain, arguments or scanner invocation count:\n%s\n' "$record" >&2
  exit 1
fi
run ./x
check_case 'explicit pattern' 0 "$rc" "$(printf 'go1.27.0\n-show\nversion\n./x\n')" "$(cat "$RECORD")"
SCAN_RC=3
run
check_case 'findings status retained' 3 "$rc" 'scan body' "$out"
SCAN_RC=1
run
check_case 'scanner error retained' 1 "$rc" 'scanner stderr' "$out"
SCAN_RC=0
# Six mismatches, bounded by the six-element literal list.
for seen in go1.27.0-X:nodwarf5 go1.27.1 go1.27.00 go1.27.0rc1 go1.27 ''; do
  if [ -z "$seen" ]; then
    : >"$HEADER"
  else
    printf 'Go: %s\n' "$seen" >"$HEADER"
  fi
  run
  check_case "reject '$seen'" 1 "$rc" "govulncheck_pinned: govulncheck read Go '$seen', want 'go1.27.0': a distro or GOEXPERIMENT build hides standard library vulnerabilities" "$out"
done
printf 'Go: go1.27.1\nGo: go1.27.0\n' >"$HEADER"
run
check_case 'two Go headers' 1 "$rc" "want 'go1.27.0'" "$out"
printf 'Go: go1.27.0\nGo: \n' >"$HEADER"
run
check_case 'second empty Go header' 1 "$rc" "want 'go1.27.0'" "$out"
rc=0
out="$(cd "$WORK/mod" && sh "$GUARD" 2>&1)" || rc=$?
check_case 'no scanner' 2 "$rc" 'govulncheck_pinned: usage:' "$out"

mkdir "$WORK/mod/rtmp" "$WORK/t"
printf 'Go: go1.27.0\n' >"$HEADER"
# The scan output lands in one file under TMPDIR while the scan runs, and is gone afterwards.
run_in rtmp
check_case 'relative TMPDIR' 0 "$rc" 'tmp entries: 1' "$out"
check_empty "$WORK/mod/rtmp"
run_in "$WORK/t"
check_case 'temporary log on pass' 0 "$rc" 'tmp entries: 1' "$out"
check_empty "$WORK/t"
printf 'Go: go1.27.1\n' >"$HEADER"
run_in "$WORK/t"
check_case 'temporary log on mismatch' 1 "$rc" 'tmp entries: 1' "$out"
check_case 'temporary log on mismatch' 1 "$rc" "want 'go1.27.0'" "$out"
check_empty "$WORK/t"
signal_case 'guard' stopped

# Remove signal traps only: the same TERM must leave the planted scanner alive.
sed '/^trap .* HUP$/d; /^trap .* INT$/d; /^trap .* TERM$/d' "$GUARD" >"$WORK/untrapped.sh"
rc=0
cmp -s "$GUARD" "$WORK/untrapped.sh" || rc=$?
if [ "$rc" -ne 1 ]; then
  echo 'FAIL signal-trap mutation did not produce a different copy' >&2
  exit 1
fi
GUARD="$WORK/untrapped.sh"
signal_case 'removed traps' alive

# Remove the comparison block in a copy: the same distro plant must then pass.
sed '/^if \[ .*go_headers.* != /,/^fi$/d' "$HERE/govulncheck_pinned.sh" >"$WORK/unguarded.sh"
GUARD="$WORK/unguarded.sh"
printf 'Go: go1.27.0-X:nodwarf5\n' >"$HEADER"
run
check_case 'removed guard accepts planted distro header' 0 "$rc" 'Go: go1.27.0-X:nodwarf5' "$out"
echo 'govulncheck_pinned_test: all cases passed (removed guard accepted distro plant)'
