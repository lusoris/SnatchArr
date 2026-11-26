#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Unit tests for tools/ci/clones_check.py.
# Verifies expected exit status for clean, planted, native, missing scanner,
# sibling-file, directory-entry and empty clone-free list fixtures, plus live
# scan PRAETORCTL resolution cases (relative path, wrong basename, non-exec
# file, bare sh, empty value, directory named praetorctl, executable
# praetorctl/standardsctl absolute paths, PATH lookup when PRAETORCTL is unset,
# and case 18 allowed bare name missing from PATH).
set -eu

script_dir="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH='' cd -- "$script_dir/../.." && pwd)"
checker="$script_dir/clones_check.py"
testdata="$script_dir/testdata/clones"
clone_free="$script_dir/clone-free.txt"
python_bin=""
if resolved_python="$(command -v python3 2>/dev/null)"; then
  python_bin="$resolved_python"
fi
case "$python_bin" in
  /*) ;;
  "")
    echo "FAIL python3 not on PATH" >&2
    exit 2
    ;;
  *)
    python_bin="$(CDPATH='' cd -- "$(dirname -- "$python_bin")" && pwd)/$(basename -- "$python_bin")"
    ;;
esac

failed=0

# Run clones_check.py against $repo_root with "$@" and verify exit status $1.
expect_exit() {
  want="$1"
  shift
  status=0
  out=$("$python_bin" "$checker" --root "$repo_root" "$@" 2>&1) || status=$?
  if [ "$status" -eq "$want" ]; then
    echo "ok   $* -> exit $status"
  else
    echo "FAIL $*: want exit $want, got $status (output: $out)" >&2
    failed=1
  fi
}

# 1. clean.json: clone elsewhere only -> exit 0
expect_exit 0 --report "$testdata/clean.json" --clone-free "$clone_free"

# 2. planted.json: foreign_duplicates touching web/src/routes/about/+page.ts -> exit 1
expect_exit 1 --report "$testdata/planted.json" --clone-free "$clone_free"

# 3. native.json: duplicates touching web/src/lib/legacyRedirect.ts -> exit 1
expect_exit 1 --report "$testdata/native.json" --clone-free "$clone_free"

# 4. noscanner.json: foreign_scanner empty -> exit 2
expect_exit 2 --report "$testdata/noscanner.json" --clone-free "$clone_free"

# 5. nopassed.json: report without the passed field -> exit 2
expect_exit 2 --report "$testdata/nopassed.json" --clone-free "$clone_free"

# 6. sibling.json: a clone in ActivityFeed.svelte.test.ts, next to the
# listed ActivityFeed.svelte, is not that file -> exit 0
expect_exit 0 --report "$testdata/sibling.json" --clone-free "$clone_free"

# 7. directory entry: planted.json against a list naming web/src/routes/about/ -> exit 1
expect_exit 1 --report "$testdata/planted.json" --clone-free "$testdata/dir-clone-free.txt"

# 8. empty clone-free list -> exit 2
expect_exit 2 --report "$testdata/clean.json" --clone-free "$testdata/empty-clone-free.txt"

# Write an executable stub binary at $1 that logs its arguments and emits the stub report.
make_stub() {
  mkdir -p "$(dirname -- "$1")"
  cat >"$1" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$CLONES_STUB_LOG"
cat "$CLONES_STUB_REPORT"
EOF
  chmod 755 "$1"
}

# Verify stub log presence ($1=yes|no) and optional exact single-line args ($3) for case $2.
check_stub_log() {
  got_ran="no"
  if [ -f "$CLONES_STUB_LOG" ]; then
    got_ran="yes"
  fi
  if [ "$got_ran" != "$1" ]; then
    echo "FAIL $2: want stub ran=$1, got $got_ran" >&2
    return 1
  fi
  if [ -n "${3-}" ]; then
    want_body=$(printf '%s\n.' "$3")
    got_body=$(cat "$CLONES_STUB_LOG"; printf '.')
    if [ "$got_body" != "$want_body" ]; then
      echo "FAIL $2: stub log mismatch: got $(cat "$CLONES_STUB_LOG")" >&2
      return 1
    fi
  fi
  return 0
}

# Invoke clones_check.py in $fixture under mode $1 (unset, bare, or set) with value $2.
run_scan_mode() {
  cd "$fixture" || exit 99
  case "$1" in
    unset)
      unset PRAETORCTL
      PATH="$2" "$python_bin" "$checker" --root "$fixture" --clone-free "$clone_free" 2>&1
      ;;
    bare)
      PRAETORCTL="$2" PATH="$fixture/empty" "$python_bin" "$checker" --root "$fixture" --clone-free "$clone_free" 2>&1
      ;;
    set)
      PRAETORCTL="$2" "$python_bin" "$checker" --root "$fixture" --clone-free "$clone_free" 2>&1
      ;;
    *)
      echo "FAIL unknown expect_scan mode: $1" >&2
      exit 98
      ;;
  esac
}

# Run clones_check.py in $fixture and check exit status, output needle, and stub execution.
expect_scan() {
  want="$1"
  needle="$2"
  ran="$3"
  label="$4"
  mode="$5"
  val="${6-}"
  want_log="${7-}"
  rm -f "$CLONES_STUB_LOG"
  status=0
  out=$(run_scan_mode "$mode" "$val") || status=$?
  if [ "$status" -ne "$want" ]; then
    echo "FAIL $label: want exit $want, got $status (output: $out)" >&2
    failed=1
    return
  fi
  case "$out" in
    *"$needle"*) ;;
    *)
      echo "FAIL $label: output missing '$needle' (output: $out)" >&2
      failed=1
      return
      ;;
  esac
  if ! check_stub_log "$ran" "$label" "$want_log"; then
    failed=1
    return
  fi
  echo "ok   $label -> exit $status"
}

raw_fixture="$(mktemp -d)"
trap 'rm -rf "$raw_fixture"' EXIT
fixture="$(CDPATH='' cd -- "$raw_fixture" && pwd -P)"
CLONES_STUB_LOG="$fixture/stub.log"
CLONES_STUB_REPORT="$testdata/clean.json"
export CLONES_STUB_LOG CLONES_STUB_REPORT

mkdir -p "$fixture/web/node_modules/.bin"
printf '#!/bin/sh\nexit 0\n' >"$fixture/web/node_modules/.bin/jscpd"
chmod 755 "$fixture/web/node_modules/.bin/jscpd"

mkdir -p "$fixture/dirs/praetorctl"
chmod 755 "$fixture/dirs/praetorctl"
mkdir -p "$fixture/empty"

make_stub "$fixture/bin/praetorctl"
make_stub "$fixture/bin/evil"
make_stub "$fixture/noexec/praetorctl"
chmod 644 "$fixture/noexec/praetorctl"
make_stub "$fixture/engine/standardsctl"

# 9. negative: relative PRAETORCTL=bin/praetorctl -> exit 2, reports invalid PRAETORCTL=, stub never ran
expect_scan 2 "invalid PRAETORCTL=" no "9 relative PRAETORCTL=bin/praetorctl" set "bin/praetorctl"

# 10. negative: disallowed basename PRAETORCTL=<fixture>/bin/evil -> exit 2, reports invalid PRAETORCTL=, stub never ran
expect_scan 2 "invalid PRAETORCTL=" no "10 disallowed basename PRAETORCTL=<fixture>/bin/evil" set "$fixture/bin/evil"

# 11. negative: non-executable PRAETORCTL=<fixture>/noexec/praetorctl -> exit 2, reports invalid PRAETORCTL=, stub never ran
expect_scan 2 "invalid PRAETORCTL=" no "11 non-executable PRAETORCTL=<fixture>/noexec/praetorctl" set "$fixture/noexec/praetorctl"

# 12. negative: disallowed bare name PRAETORCTL=sh -> exit 2, reports invalid PRAETORCTL=, nothing ran
expect_scan 2 "invalid PRAETORCTL=" no "12 disallowed bare name PRAETORCTL=sh" set "sh"

# 13. boundary: empty PRAETORCTL= -> exit 2, reports invalid PRAETORCTL=, stub never ran
expect_scan 2 "invalid PRAETORCTL=" no "13 empty PRAETORCTL=" set ""

# 14. boundary: directory PRAETORCTL=<fixture>/dirs/praetorctl has an allowed basename and executable bit, so only the regular-file check refuses it -> exit 2, stub never ran
expect_scan 2 "invalid PRAETORCTL=" no "14 directory named praetorctl PRAETORCTL=<fixture>/dirs/praetorctl" set "$fixture/dirs/praetorctl"

# 15. positive twin: PRAETORCTL=<fixture>/bin/praetorctl -> exit 0, exact stub args
expect_scan 0 "" yes "15 executable PRAETORCTL=<fixture>/bin/praetorctl" set "$fixture/bin/praetorctl" "dedupe scan --json $fixture"

# 16. positive: PRAETORCTL=<fixture>/engine/standardsctl -> exit 0, stub ran
expect_scan 0 "" yes "16 executable PRAETORCTL=<fixture>/engine/standardsctl" set "$fixture/engine/standardsctl"

# 17. positive: PRAETORCTL unset and <fixture>/bin on PATH -> exit 0, stub ran
expect_scan 0 "" yes "17 unset PRAETORCTL resolves praetorctl on PATH" unset "$fixture/bin:$PATH"

# 18. negative: allowed bare name PRAETORCTL=standardsctl missing from PATH -> exit 2, reports not found on PATH, stub never ran
expect_scan 2 "not found on PATH" no "18 allowed bare name not on PATH PRAETORCTL=standardsctl" bare "standardsctl"

exit "$failed"
