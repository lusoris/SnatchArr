#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for semgrep_scan.py: a minimal repository with one rule, one fixture and one
# Go file, unchanged and with one defect planted at a time. A stub stands in for docker, so no
# image is pulled; it logs every call, and a defect in the rules, the fixtures or the image
# reference must be refused before the stub runs. Invalid --docker and --root values are refused
# before the stub runs too. The same stub stands in for a local Semgrep binary (--semgrep). Last:
# the pinned image comes from tools/ci/tool-versions.env and the scan is wired into Makefile,
# lefthook, CI, Renovate and the lane brief requirements.
#
#   tools/ci/semgrep_scan_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs python3, git and jq.
# Wiring cases need a PyYAML-capable python. ensure_pyyaml_python (test_lib.sh) takes an explicit
# PYTHON_BIN, then host python3 if it imports yaml, else .cache/pyyaml-venv built by
# tools/ci/pyyaml_venv.sh (needs the package index).
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh
# shellcheck disable=SC1091 # shellcheck without -x does not follow this local helper.
. "$HERE/test_lib.sh"
PY3="$(command -v python3 2>/dev/null)" || PY3=""
if [ -z "$PY3" ]; then
  printf 'semgrep_scan_test: python3 not on PATH\n' >&2
  exit 1
fi
ensure_pyyaml_python
SCAN="$HERE/semgrep_scan.py"
ROOT="${SEMGREP_WIRING_ROOT:-$(cd "$HERE/../.." && pwd)}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
: > "$WORK/gitconfig"
mkdir -p "$WORK/xdg-empty"
export GIT_CONFIG_GLOBAL="$WORK/gitconfig" GIT_CONFIG_NOSYSTEM=1 XDG_CONFIG_HOME="$WORK/xdg-empty"

# read_semgrep_image FILE: print the SEMGREP_IMAGE value from FILE without sourcing shell text.
read_semgrep_image() {
  if [ ! -f "$1" ] || [ ! -r "$1" ]; then
    printf 'FAIL %s: not readable\n' "$1" >&2
    exit 1
  fi
  grep_rc=0
  pin_line="$(grep '^SEMGREP_IMAGE=' "$1")" || grep_rc=$?
  case "$grep_rc" in
    0) ;;
    1)
      printf 'FAIL %s: missing unique SEMGREP_IMAGE= line\n' "$1" >&2
      exit 1
      ;;
    *)
      printf 'FAIL %s: grep exited %s\n' "$1" "$grep_rc" >&2
      exit 1
      ;;
  esac
  case "$pin_line" in
    ''|*'
'*)
      printf 'FAIL %s: missing unique SEMGREP_IMAGE= line\n' "$1" >&2
      exit 1
      ;;
  esac
  pin_value="${pin_line#SEMGREP_IMAGE=}"
  if [ -z "$pin_value" ]; then
    printf 'FAIL %s: empty SEMGREP_IMAGE= line\n' "$1" >&2
    exit 1
  fi
  printf '%s\n' "$pin_value"
}

IMAGE="$(read_semgrep_image "$ROOT/tools/ci/tool-versions.env")"

grep -v '^SEMGREP_IMAGE=' "$ROOT/tools/ci/tool-versions.env" > "$WORK/no-semgrep-image.env"
missing_rc=0
missing_out="$(read_semgrep_image "$WORK/no-semgrep-image.env" 2>&1)" || missing_rc=$?
check_case "missing SEMGREP_IMAGE in tool-versions.env" 1 "$missing_rc" "FAIL " "$missing_out" "output lacks"
echo "ok   missing SEMGREP_IMAGE in tool-versions.env"

nonexistent_rc=0
nonexistent_out="$(read_semgrep_image "$WORK/does-not-exist.env" 2>&1)" || nonexistent_rc=$?
check_case "nonexistent tool-versions.env" 1 "$nonexistent_rc" "not readable" "$nonexistent_out" "output lacks"
echo "ok   nonexistent tool-versions.env"

sed '/^SEMGREP_IMAGE=/p' "$ROOT/tools/ci/tool-versions.env" > "$WORK/dup-semgrep-image.env"
dup_rc=0
dup_out="$(read_semgrep_image "$WORK/dup-semgrep-image.env" 2>&1)" || dup_rc=$?
check_case "duplicate SEMGREP_IMAGE in tool-versions.env" 1 "$dup_rc" "missing unique" "$dup_out" "output lacks"
echo "ok   duplicate SEMGREP_IMAGE in tool-versions.env"

sed 's/^SEMGREP_IMAGE=.*/SEMGREP_IMAGE=/' "$ROOT/tools/ci/tool-versions.env" > "$WORK/empty-semgrep-image.env"
empty_rc=0
empty_out="$(read_semgrep_image "$WORK/empty-semgrep-image.env" 2>&1)" || empty_rc=$?
check_case "empty SEMGREP_IMAGE in tool-versions.env" 1 "$empty_rc" "empty" "$empty_out" "output lacks"
echo "ok   empty SEMGREP_IMAGE in tool-versions.env"

# The stub answers like Semgrep: STUB_MODE picks the answer, STUB_LOG records each call.
STUB="$WORK/docker"
cat > "$STUB" <<'EOF'
#!/bin/sh
set -eu
echo "$*" >> "$STUB_LOG"
if [ -n "${STUB_PWD_LOG:-}" ]; then
  pwd -P >> "$STUB_PWD_LOG"
fi
# Called as a native binary (no docker "run"): Semgrep state must sit in the scan's temporary
# directory, as it would fail on a read-only home.
if [ "$1" != run ]; then
  # check_state_var NAME VALUE: check that VALUE matches */semgrep-state-*, or print an error and exit 3.
  check_state_var() {
    case "$2" in
      */semgrep-state-*) ;;
      *) echo "$1 is outside semgrep-state directory"; exit 3 ;;
    esac
  }
  check_state_var XDG_CONFIG_HOME "${XDG_CONFIG_HOME:-}"
  check_state_var SEMGREP_SETTINGS_FILE "${SEMGREP_SETTINGS_FILE:-}"
  check_state_var SEMGREP_LOG_FILE "${SEMGREP_LOG_FILE:-}"
  check_state_var SEMGREP_VERSION_CACHE_PATH "${SEMGREP_VERSION_CACHE_PATH:-}"
fi
if [ "$1" = --version ]; then echo "semgrep stub"; echo "9.9.9"; exit 0; fi
for arg in "$@"; do
  if [ "$arg" = "--test" ]; then
    case "$STUB_MODE" in
      fixture-fails) echo "1 test failed"; exit 1 ;;
      edits-rules-in-test) printf '# edited\n' >> "$STUB_LIVE_ROOT/.semgrep.yml" ;;
      edits-fixture-in-test) printf '# edited\n' >> "$STUB_LIVE_ROOT/tools/ci/semgrep/testdata/demo-rule.go" ;;
    esac
    echo "All tests passed"
    exit 0
  fi
done
# Drop every argument up to "--"; what is left are the files to scan.
while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done
shift
case "$STUB_MODE" in
  clean | edits-rules-in-test | edits-fixture-in-test)
    jq -n '{results: [], errors: [], paths: {scanned: $ARGS.positional}}' --args "$@"
    ;;
  adds-file)
    printf 'package main\n' > "$STUB_LIVE_ROOT/added.go"
    jq -n '{results: [], errors: [], paths: {scanned: $ARGS.positional}}' --args "$@"
    ;;
  rewrites-file)
    printf 'package main\n\nfunc main() { println("mutated") }\n' > "$STUB_LIVE_ROOT/main.go"
    jq -n '{results: [], errors: [], paths: {scanned: $ARGS.positional}}' --args "$@"
    ;;
  swap-restore)
    original="$STUB_LOG.original"
    cp "$STUB_LIVE_ROOT/main.go" "$original"
    printf 'package main\n\nfunc main() { println("mutated") }\n' > "$STUB_LIVE_ROOT/main.go"
    same=0
    cmp -s main.go "$original" || same=$?
    cp "$original" "$STUB_LIVE_ROOT/main.go"
    if [ "$same" -ne 0 ]; then
      echo 'scan saw the temporary live rewrite' >&2
      exit 3
    fi
    jq -n '{results: [], errors: [], paths: {scanned: $ARGS.positional}}' --args "$@"
    ;;
  finding) jq -n '{results: [{path: "main.go", start: {line: 1}, check_id: "demo-rule",
                   extra: {message: "planted"}}], errors: [], paths: {scanned: $ARGS.positional}}' --args "$@" ;;
  finding-ansi-long)
    long_msg="$(python3 -I -c 'import sys; sys.stdout.write("\x1b[31m" + "x" * 995)')"
    jq -n --arg msg "$long_msg" '{results: [{path: "main.go", start: {line: 1}, check_id: "demo-rule",
      extra: {message: $msg}}], errors: [], paths: {scanned: $ARGS.positional}}' --args "$@"
    ;;
  skipped) jq -n '{results: [], errors: [], paths: {scanned: []}}' ;;
  no-json) echo "not a report" ;;
  exit-2) jq -n '{results: [], errors: [], paths: {scanned: $ARGS.positional}}' --args "$@"; exit 2 ;;
esac
EOF
chmod +x "$STUB"

RULES='rules:
  - id: demo-rule
    languages: [go]
    severity: ERROR
    message: planted
    pattern: panic(...)
'
FIXTURE='//go:build semgrep_fixture

package fixture

func f() {
	// ruleid: demo-rule
	panic("x")
	// ok: demo-rule
	println("x")
}
'

# fresh DIR: a clean repository the scan accepts.
fresh() {
  rm -rf "$1"
  mkdir -p "$1/tools/ci/semgrep/testdata"
  printf '%s' "$RULES" > "$1/.semgrep.yml"
  printf '%s' "$FIXTURE" > "$1/tools/ci/semgrep/testdata/demo-rule.go"
  printf 'package main\n\nfunc main() {}\n' > "$1/main.go"
  mkdir -p "$1/tools/ci"
  printf 'SEMGREP_IMAGE=%s\n' "$IMAGE" > "$1/tools/ci/tool-versions.env"
  git -C "$1" init --quiet
  git -C "$1" add -A
}

accepted=0
refused=0

# assert_scanned_set CASE_NAME TARGET...: check that $WORK/calls holds exactly one scan call and
# that its targets after "--" equal the sorted TARGET arguments (nothing else, fixture absent).
assert_scanned_set() {
  set_case="$1"
  shift
  want_set="$(printf '%s\n' "$@" | LC_ALL=C sort)"
  got_set="$(awk '
    index($0, " -- ") {
      scans++
      rest = substr($0, index($0, " -- ") + 4)
      n = split(rest, items, " ")
      for (i = 1; i <= n; i++) {
        if (items[i] != "") print items[i]
      }
    }
    END {
      if (scans != 1) {
        printf "SCAN_CALL_COUNT=%d\n", scans + 0
      }
    }
  ' "$WORK/calls" | LC_ALL=C sort)"
  if [ "$got_set" != "$want_set" ]; then
    printf 'FAIL %s: scanned set %s, want %s\n' "$set_case" "$got_set" "$want_set" >&2
    exit 1
  fi
}

# expect WANT NAME MODE CALLED NEEDLE [SH EDIT]: after EDIT runs in a fresh repository, the scan
# must exit WANT, print NEEDLE, and call the stub (CALLED yes) or never call it (CALLED no).
expect() {
  want="$1" name="$2" mode="$3" called="$4" needle="$5" edit="${6:-:}"
  image="${IMAGE_OVERRIDE:-$IMAGE}" docker_arg="${DOCKER_OVERRIDE-$STUB}"
  root="$WORK/root"
  scan_root="${ROOT_OVERRIDE:-$root}"
  fresh "$root"
  (cd "$root" && sh -c "$edit")
  : > "$WORK/calls"
  got=0
  if [ -n "${NATIVE:-}" ]; then
    out="$(cd "${NATIVE_CWD:-$PWD}" && run_bounded 30 env \
      PATH="${EXPECT_PATH:-$PATH}" XDG_CONFIG_HOME="$XDG_CONFIG_HOME" \
      STUB_MODE="$mode" STUB_LOG="$WORK/calls" STUB_LIVE_ROOT="$root" \
      "$PY3" -I "$SCAN" --root "$scan_root" --semgrep "$NATIVE" 2>&1)" || got=$?
  else
    out="$(run_bounded 30 env \
      PATH="${EXPECT_PATH:-$PATH}" XDG_CONFIG_HOME="$XDG_CONFIG_HOME" \
      STUB_MODE="$mode" STUB_LOG="$WORK/calls" STUB_LIVE_ROOT="$root" \
      "$PY3" -I "$SCAN" --root "$scan_root" --docker "$docker_arg" --image "$image" 2>&1)" || got=$?
  fi
  check_case "$name" "$want" "$got" "$needle" "$out" "output lacks"
  if [ "$called" = no ] && [ -s "$WORK/calls" ]; then
    printf 'FAIL %s: refused only after running docker\n' "$name" >&2
    exit 1
  fi
  if [ "$called" = yes ] && [ ! -s "$WORK/calls" ]; then
    printf 'FAIL %s: docker never ran\n' "$name" >&2
    exit 1
  fi
  if [ "$want" -eq 0 ]; then accepted=$((accepted + 1)); else refused=$((refused + 1)); fi
  echo "ok   $name"
}

FX=tools/ci/semgrep/testdata

regression_failed=0
# Report every planted regression even when replayed against the old scanner.
check_regression() {
  if (check_case "$@"); then
    if [ "$2" -eq 0 ]; then accepted=$((accepted + 1)); else refused=$((refused + 1)); fi
    echo "ok   $1"
  else
    regression_failed=1
  fi
}

root="$WORK/regressions"
fresh "$root"
fingerprint="$(python3 -I - "$root" <<'PY'
import hashlib
from pathlib import Path
import sys

root = Path(sys.argv[1])
manifest = b"".join(
    name.encode() + b"\0" + hashlib.sha256((root / name).read_bytes()).digest()  # Binary manifest.
    for name in (".semgrep.yml", "main.go", "tools/ci/semgrep/testdata/demo-rule.go")
)
print(hashlib.sha256(manifest).hexdigest()[:12])
PY
)"
got=0
out="$(run_bounded 30 env STUB_MODE=swap-restore STUB_LOG="$WORK/swap" STUB_LIVE_ROOT="$root" \
  python3 -I "$SCAN" --root "$root" --semgrep "$STUB" 2>&1)" || got=$?
check_regression "snapshot survives a temporary rewrite of the live target" 0 "$got" \
  "Semgrep: 1 files scanned (snapshot sha256:$fingerprint), 0 findings" "$out"

got=0
out="$(run_bounded 30 env STUB_MODE=clean STUB_LOG="$WORK/late" python3 -I - "$SCAN" "$root" "$STUB" 2>&1 <<'PY'
from pathlib import Path
import runpy
import sys

scanner, root, stub = sys.argv[1:]
module = runpy.run_path(scanner)
original = module["changed_targets"]

def add_target(root, before):
    (Path(root) / "late.go").write_text("package main\n", encoding="utf-8")
    return original(root, before)

original.__globals__["changed_targets"] = add_target
sys.argv = [scanner, "--root", root, "--semgrep", stub]
sys.exit(module["main"]())
PY
)" || got=$?
check_regression "target added during the final content check" 1 "$got" \
  "target set changed during the scan: 1 added, 0 removed: ['late.go']" "$out"

ln -s main.go "$root/symlink"
mkfifo "$root/fifo"
got=0
out="$(run_bounded 30 python3 -I - "$SCAN" "$root" 2>&1 <<'PY'
import os
from pathlib import Path
import runpy
import sys
from unittest.mock import patch

module = runpy.run_path(sys.argv[1])
root = Path(sys.argv[2])
del os.O_NONBLOCK, os.O_NOFOLLOW
read_bytes = module["read_bounded_bytes"]
if read_bytes(root / "main.go", 1024) != (root / "main.go").read_bytes():
    raise AssertionError("regular file bytes differ")
for name in ("symlink", "fifo", "tools"):
    try:
        read_bytes(root / name, 1024)
    except module["BoundedReadError"] as err:
        if "not a regular file" not in str(err):
            raise
    else:
        raise AssertionError(f"{name} was accepted")

original_open = os.open
replacement = root / "replacement"
replacement.write_bytes(b"replacement")

def replace_then_open(path, flags):
    replacement.replace(path)
    return original_open(path, flags)

with patch.object(os, "open", replace_then_open):
    try:
        read_bytes(root / "main.go", 1024)
    except module["BoundedReadError"] as err:
        if "file changed before open" not in str(err):
            raise
    else:
        raise AssertionError("a swapped file was accepted")
print("regular file read; symlink, FIFO, directory and replacement refused")
PY
)" || got=$?
check_regression "bounded open without O_NONBLOCK and O_NOFOLLOW" 0 "$got" \
  "regular file read; symlink, FIFO, directory and replacement refused" "$out"

mkdir "$WORK/git-stub"
cat > "$WORK/git-stub/git" <<'EOF'
#!/bin/sh
exec python3 -I - <<'PY'
import os
import sys

mode = os.environ["GIT_STUB_MODE"]
if mode == "raw-records":
    sys.stdout.buffer.write(b"main.go\0" * 16641)
else:
    warning = {
        "warning": b"warning: unreadable directory\x1b[31m\n",
        "loud": b"warning: large stderr " + b"x" * (1024 * 1024 - 22),
        "stderr-cap": b"x" * (2 * 1024 * 1024 + 1),
    }
    sys.stderr.buffer.write(warning[mode]); sys.stderr.buffer.flush()  # Flush before stdout.
    sys.stdout.buffer.write(b"main.go\0")
PY
EOF
chmod +x "$WORK/git-stub/git"
for git_mode in warning loud stderr-cap raw-records; do
  got=0
  out="$(run_bounded 30 env PATH="$WORK/git-stub:$PATH" GIT_STUB_MODE="$git_mode" \
    STUB_MODE=clean STUB_LOG="$WORK/git-calls" \
    python3 -I "$SCAN" --root "$root" --semgrep "$STUB" 2>&1)" || got=$?
  case "$git_mode" in
    warning)
      check_regression "Git warning with exit zero" 1 "$got" \
        'git ls-files failed (exit 0): warning: unreadable directory\x1b[31m\n' "$out" ;;
    loud)
      check_regression "Git writes 1 MiB to stderr before stdout" 1 "$got" \
        "git ls-files failed (exit 0): warning: large stderr " "$out"
      check_regression "Git stderr excerpt truncated" 1 "$got" '... [truncated]' "$out" ;;
    stderr-cap)
      check_regression "Git stderr above the byte cap" 1 "$got" \
        'git ls-files failed: stderr exceeds 2097152 bytes' "$out" ;;
    raw-records)
      check_regression "Git listing above the raw record bound" 1 "$got" \
        'git ls-files exceeded the raw listing bound of 16640 records' "$out" ;;
  esac
done

mkdir "$WORK/git-hang-stub"
cat > "$WORK/git-hang-stub/git" <<'EOF'
#!/bin/sh
printf 'main.go\0'
sleep 25 &
exit 0
EOF
chmod +x "$WORK/git-hang-stub/git"
got=0
out="$(run_bounded 60 env PATH="$WORK/git-hang-stub:$PATH" python3 -I - "$SCAN" "$root" 2>&1 <<'PY'
from pathlib import Path
import runpy
import sys
import time

scanner, root = sys.argv[1:]
module = runpy.run_path(scanner)
targets_fn = module["targets"]
targets_fn.__globals__["TIMEOUT_SECONDS"] = 2
started = time.monotonic()  # Measure wall-clock deadline.
try:
    targets_fn(Path(root))
except module["ScanError"] as err:
    elapsed = time.monotonic() - started
    if elapsed >= 10:
        print(f"targets took {elapsed:.2f}s, expected under 10s", file=sys.stderr)
        sys.exit(3)
    sys.exit(str(err))
sys.exit(2)
PY
)" || got=$?
check_regression "Git child holding stdout is killed at the deadline" 1 "$got" \
  "timed out" "$out"

checkout_root="$WORK/image-cwd-root"
fresh "$checkout_root"
checkout_pwd="$(cd "$checkout_root" && pwd -P)"
mkdir -p "$WORK/rel-bin"
cp "$STUB" "$WORK/rel-bin/docker"
: > "$WORK/image-calls"
: > "$WORK/image-pwds"
got=0
out="$(cd "$WORK" && run_bounded 30 env \
  PATH="rel-bin:$PATH" STUB_MODE=clean STUB_LOG="$WORK/image-calls" \
  STUB_PWD_LOG="$WORK/image-pwds" STUB_LIVE_ROOT="$checkout_root" \
  "$PY3" -I "$SCAN" --root "$checkout_root" --docker docker --image "$IMAGE" 2>&1)" || got=$?
verify_rc=0
verify_out="$(run_bounded 30 python3 -I - "$checkout_pwd" "$WORK/image-calls" "$WORK/image-pwds" 2>&1 <<'PY'
from pathlib import Path
import sys

checkout, calls_path, pwds_path = sys.argv[1:]
calls = Path(calls_path).read_text(encoding="utf-8").splitlines()[:10]
pwds = Path(pwds_path).read_text(encoding="utf-8").splitlines()[:10]
if len(calls) != 2 or len(pwds) != 2:
    sys.exit(f"expected 2 image calls and 2 pwds, got {len(calls)} and {len(pwds)}")
if "--test" not in calls[0] or "--json" not in calls[1]:
    sys.exit(f"unexpected call sequence: {calls}")
if any(pwd != checkout for pwd in pwds):
    sys.exit(f"image cwd mismatch: want {checkout}, got {pwds}")
volumes = []
for line in calls:
    if "--volume " not in line or " --workdir " not in line:
        sys.exit(f"missing volume or workdir in call: {line}")
    spec = line.split("--volume ", 1)[1].split(" --workdir ", 1)[0]
    source, sep, tail = spec.partition(":/src:ro")
    if not sep or tail:
        sys.exit(f"unexpected volume spec: {spec}")
    if source == checkout or not Path(source).name.startswith("semgrep-snapshot-"):
        sys.exit(f"bad volume source: {source}")
    volumes.append(source)
if len(set(volumes)) != 1:
    sys.exit(f"replay and scan used different snapshots: {volumes}")
print("image cwd and snapshot volume verified")
PY
)" || verify_rc=$?
if [ "$got" -eq 0 ] && [ "$verify_rc" -ne 0 ]; then
  got="$verify_rc"
fi
out="$out
$verify_out"
check_regression "image calls run in the checkout with the snapshot mounted" 0 "$got" \
  "image cwd and snapshot volume verified" "$out"

# Print the live Go target count or report a discovery error.
list_target_count() {
  run_bounded 30 python3 -I - "$SCAN" "$1" <<'PY'
from pathlib import Path
import runpy
import sys

module = runpy.run_path(sys.argv[1])
try:
    print(len(module["targets"](Path(sys.argv[2]))), "live Go targets")
except module["ScanError"] as err:
    sys.exit(str(err))
PY
}

root="$WORK/target-boundary"
fresh "$root"
mkdir "$root/deleted"
for i in $(seq 1 64); do printf 'package main\n' > "$root/deleted/$i.go"; done
git -C "$root" add -- deleted
rm -rf "$root/deleted"
for i in $(seq 1 4095); do printf 'package main\n' > "$root/u-$i.go"; done
got=0
out="$(list_target_count "$root" 2>&1)" || got=$?
check_regression "4096 live Go files despite 64 deleted indexed paths and a fixture" 0 "$got" \
  '4096 live Go targets' "$out"
printf 'package main\n' > "$root/over.go"
got=0
out="$(list_target_count "$root" 2>&1)" || got=$?
check_regression "4097 live Go files refused at the target cap" 1 "$got" \
  '4097 Go files to scan, expected 1 to 4096' "$out"
[ "$regression_failed" -eq 0 ] || exit 1

expect 0 "clean repository" clean yes "Semgrep: 1 files scanned (snapshot sha256:"
assert_scanned_set "clean repository" main.go

# Defects refused before any docker call.
IMAGE_OVERRIDE="${IMAGE%@*}" expect 1 "image pinned by tag only" clean no "is not pinned"
IMAGE_OVERRIDE="${IMAGE%%:*}@${IMAGE#*@}" \
  expect 1 "image pinned by digest without a tag" clean no "is not pinned"
cp "$STUB" "$WORK/evil"
DOCKER_OVERRIDE="$WORK/evil" expect 1 "docker with another basename" clean no "--docker"
DOCKER_OVERRIDE=bin/docker expect 1 "relative docker path" clean no "--docker"
mkdir -p "$WORK/dir/docker"
DOCKER_OVERRIDE="$WORK/dir/docker" expect 1 "directory named docker" clean no "--docker"
mkdir -p "$WORK/noexec"
cp "$STUB" "$WORK/noexec/docker"
chmod 644 "$WORK/noexec/docker"
DOCKER_OVERRIDE="$WORK/noexec/docker" \
  expect 1 "docker without the executable bit" clean no "--docker"
DOCKER_OVERRIDE="" expect 1 "empty docker" clean no "--docker"
mkdir -p "$WORK/empty"
DOCKER_OVERRIDE=docker EXPECT_PATH="$WORK/empty" \
  expect 1 "bare docker not on PATH" clean no "not found on PATH"
mkdir -p "$WORK/bin"
cp "$STUB" "$WORK/bin/docker"
DOCKER_OVERRIDE=docker EXPECT_PATH="$WORK/bin:$PATH" \
  expect 0 "bare docker on PATH" clean yes "Semgrep: 1 files scanned (snapshot sha256:"
ROOT_OVERRIDE="$WORK/root/main.go" expect 1 "root is a file" clean no "is not a directory"
ROOT_OVERRIDE="$WORK/no-such-root" expect 1 "root does not exist" clean no "is not a directory"
NATIVE="$STUB" ROOT_OVERRIDE="$WORK/no-such-root" \
  expect 1 "native binary, root does not exist" clean no "is not a directory"
expect 1 "no rule declared" clean no "declares no rule" 'printf "rules: []\n" > .semgrep.yml'
expect 1 "rule id declared twice" clean no "declares a rule id twice" \
  'printf "  - id: demo-rule\n    pattern: x\n" >> .semgrep.yml'
expect 1 "no fixture" clean no "0 fixtures, expected 1 to 64" "rm $FX/demo-rule.go"
expect 1 "65 fixtures" clean no "65 fixtures, expected 1 to 64" \
  "i=0; while [ \$i -lt 64 ]; do cp $FX/demo-rule.go $FX/copy-\$i.go; i=\$((i + 1)); done"
expect 1 "fixture without the build constraint" clean no "does not start with '//go:build semgrep_fixture'" \
  "sed -i 1d $FX/demo-rule.go"
expect 1 "constraint not on the first line" clean no "does not start with" \
  "sed -i '1i // leading comment' $FX/demo-rule.go"
expect 1 "annotation names an unknown rule" clean no "annotation names unknown rule 'other-rule'" \
  "printf '// ok: other-rule\n' >> $FX/demo-rule.go"
# shellcheck disable=SC2016 # literal backticks in semgrep_scan.py's diagnostic
expect 1 "rule without a ruleid line" clean no 'no `ruleid:` fixture line for rule(s): demo-rule' \
  "sed -i '/ruleid:/d' $FX/demo-rule.go"
# shellcheck disable=SC2016 # same: literal backticks in semgrep_scan.py's diagnostic
expect 1 "rule without an ok line" clean no 'no `ok:` fixture line for rule(s): demo-rule' \
  "sed -i '/ok:/d' $FX/demo-rule.go"
expect 1 "second rule without a fixture" clean no "fixture line for rule(s): second-rule" \
  'printf "  - id: second-rule\n    pattern: x\n" >> .semgrep.yml'
expect 1 "no Go file to scan" clean no "0 Go files to scan, expected 1 to 4096" \
  'git rm --quiet --cached main.go && rm main.go'
printf 'package outside\n' > "$WORK/outside.go"
expect 1 "untracked symlink outside repository root" clean no \
  "refused 1 symlink or outside-root target(s): ['link.go']" 'ln -s ../outside.go link.go'
expect 1 "FIFO rules file" clean no "not a regular file" \
  'rm .semgrep.yml && mkfifo .semgrep.yml'
expect 1 "FIFO fixture" clean no "not a regular file" \
  "rm $FX/demo-rule.go && mkfifo $FX/demo-rule.go"
expect 0 "rules file at the 65536-byte limit" clean yes \
  "Semgrep: 1 files scanned (snapshot sha256:" \
  'python3 -I -c '\''from pathlib import Path; p = Path(".semgrep.yml"); b = p.read_bytes(); p.write_bytes(b + b"#" * (65536 - len(b) - 1) + b"\n")'\'''
expect 1 "rules file above the 65536-byte limit" clean no "exceeds 65536 bytes" \
  'python3 -I -c '\''from pathlib import Path; p = Path(".semgrep.yml"); b = p.read_bytes(); p.write_bytes(b + b"#" * (65537 - len(b) - 1) + b"\n")'\'''
expect 1 "fixture above the 65536-byte limit" clean no "exceeds 65536 bytes" \
  'python3 -I -c '\''from pathlib import Path; p = Path("tools/ci/semgrep/testdata/demo-rule.go"); b = p.read_bytes(); p.write_bytes(b + b"/" * (65537 - len(b) - 1) + b"\n")'\'''

# Defects only Semgrep's own answer reveals.
expect 1 "fixture replay fails" fixture-fails yes "fixture tools/ci/semgrep/testdata/demo-rule.go did not pass (exit 1)"
expect 1 "scan reports a finding" finding yes "1 finding(s), 0 error(s)"
expect 1 "finding message with ANSI escape and 1000 chars" finding-ansi-long yes 'demo-rule: \x1b[31m'
check_case "finding message cut marker" 0 0 "... [truncated]" "$out"
esc_char="$(printf '\033')"
case "$out" in
  *"$esc_char"*)
    echo "FAIL finding message with ANSI escape and 1000 chars: raw ESC in output" >&2
    exit 1
    ;;
esac
echo "ok   finding message cut marker and escaped control bytes"
expect 1 "scan skips a file" skipped yes "report scanned 0 of 1 files; not scanned: ['main.go']"
expect 1 "scan prints no report" no-json yes "no JSON report"
expect 1 "clean report, failing exit" exit-2 yes "semgrep exited 2 with a clean report"

# Targets: what a working tree holds, not only what the index lists.
expect 0 "untracked Go file is scanned" clean yes "Semgrep: 2 files scanned (snapshot sha256:" \
  'printf "package main\n" > extra.go'
assert_scanned_set "untracked Go file is scanned" extra.go main.go
expect 0 "ignored Go file is left out" clean yes "Semgrep: 1 files scanned (snapshot sha256:" \
  'printf "ignored.go\n" > .gitignore && printf "package main\n" > ignored.go'
assert_scanned_set "ignored Go file is left out" main.go
expect 0 "deleted tracked file is left out" clean yes "Semgrep: 1 files scanned (snapshot sha256:" \
  'printf "package main\n" > gone.go && git add gone.go && rm gone.go'
assert_scanned_set "deleted tracked file is left out" main.go
expect 1 "target added during scan" adds-file yes \
  "target set changed during the scan: 1 added, 0 removed: ['added.go']"
expect 1 "target rewritten during scan" rewrites-file yes \
  "target contents changed during the scan (1): ['main.go']"
expect 1 "live rules edited during the fixture replay" edits-rules-in-test yes \
  "target contents changed during the scan (1): ['.semgrep.yml']"
expect 1 "live fixture edited during the fixture replay" edits-fixture-in-test yes \
  "target contents changed during the scan (1): ['tools/ci/semgrep/testdata/demo-rule.go']"

# Native binary (--semgrep): same replay and scanned-file check, state outside the home directory.
NATIVE="$STUB" expect 0 "native binary, clean repository" clean yes \
  "Semgrep native: $STUB version 9.9.9; CI runs the pinned image"
CI_TAG="${IMAGE%@*}"
CI_TAG="${CI_TAG##*:}"
check_case "native version differs from CI pin" 0 0 \
  "Semgrep native version 9.9.9 differs from the CI pin $CI_TAG; the CI scan decides" "$out"
echo "ok   native version differs from CI pin"
NATIVE=./docker NATIVE_CWD="$WORK" expect 0 "relative native binary with separate root" clean yes \
  "Semgrep native: $STUB version 9.9.9; CI runs the pinned image"
mkdir -p "$WORK/xdg/git"
printf 'ignored.go\n' > "$WORK/xdg/git/ignore"
XDG_CONFIG_HOME="$WORK/xdg" NATIVE="$STUB" expect 0 \
  "native preserves caller Git excludes" clean yes "Semgrep: 1 files scanned (snapshot sha256:" \
  'printf "package main\n" > ignored.go'
if grep -q 'ignored.go' "$WORK/calls"; then
  echo 'FAIL native preserves caller Git excludes: ignored.go reached stub' >&2
  exit 1
fi
NATIVE="$STUB" expect 0 "native without CI pin" clean yes "CI pin unknown" \
  'rm tools/ci/tool-versions.env'
NATIVE="$STUB" expect 0 "native with missing CI tag" clean yes "CI pin unknown" \
  "sed -i 's/:[^:@]*@/@/' tools/ci/tool-versions.env"
NATIVE="$STUB" expect 0 "native with FIFO tool-versions.env" clean yes "CI pin unknown" \
  'rm tools/ci/tool-versions.env && mkfifo tools/ci/tool-versions.env'
NATIVE="$STUB" expect 0 "native version differs from a CRLF CI pin" clean yes \
  "differs from the CI pin" \
  "printf 'SEMGREP_IMAGE=%s\\r\\n' '$IMAGE' > tools/ci/tool-versions.env"
NATIVE="$STUB" expect 1 "native binary, live rules edited during the fixture replay" \
  edits-rules-in-test yes \
  "target contents changed during the scan (1): ['.semgrep.yml']"
NATIVE="$STUB" expect 1 "native binary, scan skips a file" skipped yes \
  "report scanned 0 of 1 files; not scanned: ['main.go']"
NATIVE="$STUB" expect 1 "native binary, scan reports a finding" finding yes "1 finding(s), 0 error(s)"
# shellcheck disable=SC2016 # literal backticks in semgrep_scan.py's diagnostic
NATIVE="$STUB" expect 1 "native binary, rule without an ok line" clean no 'no `ok:` fixture line for rule(s): demo-rule' \
  "sed -i '/ok:/d' $FX/demo-rule.go"
NATIVE="$WORK/no-such-semgrep" expect 1 "native binary missing" clean no "is not an executable file"

SEMGREP_JOB="$WORK/ci-semgrep-job.yml"
if [ ! -f "$ROOT/.github/workflows/ci.yml" ]; then
  printf "FAIL wiring: .github/workflows/ci.yml is missing\n" >&2
  exit 1
fi
awk '
  /^  semgrep:[[:space:]]*(#.*)?$/ { in_job = 1; next }
  in_job && /^  [A-Za-z0-9_-]+:/ { exit }
  in_job && /^[A-Za-z0-9_-]+:/ { exit }
  in_job { print }
' "$ROOT/.github/workflows/ci.yml" > "$SEMGREP_JOB"

for wired in \
  "Makefile:^verify-all:.* semgrep-scan" \
  "Makefile:python3 -I tools/ci/semgrep_scan.py --image" \
  "lefthook.yml:make semgrep-scan" \
  ".github/workflows/ci.yml#semgrep:tools/ci/tool-versions.env" \
  ".github/workflows/ci.yml#semgrep:python3 -I tools/ci/semgrep_scan.py --image" \
  "renovate.json:SEMGREP_IMAGE="; do
  label="${wired%%:*}"
  pattern="${wired#*:}"
  case "$label" in
    .github/workflows/ci.yml#semgrep) target="$SEMGREP_JOB" ;;
    *) target="$ROOT/$label" ;;
  esac
  case "$pattern" in
    ^*) pattern="${pattern#^}" ; anchor="^" ;;
    *) anchor="^[^#]*" ;;
  esac
  if [ ! -f "$target" ]; then
    printf 'FAIL wiring: %s is missing\n' "$label" >&2
    exit 1
  fi
  if ! grep -q -E "${anchor}${pattern}" "$target"; then
    printf "FAIL wiring: %s runs no '%s'\n" "$label" "${wired#*:}" >&2
    exit 1
  fi
done
WIRING="$HERE/lane_brief_wiring.py"
wiring_rc=0
wiring_out="$("$PYTHON_BIN" -I "$WIRING" "$ROOT/.config/lane-brief.yaml" 2>&1)" || wiring_rc=$?
check_case "lane brief impl and fix wiring" 0 "$wiring_rc" "" "$wiring_out"
reordered_brief="$WORK/preceding-command.yaml"
"$PYTHON_BIN" -I - "$ROOT/.config/lane-brief.yaml" "$reordered_brief" <<'PY'
import pathlib
import sys

import yaml

source = yaml.safe_load(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
for stage in ("impl", "fix"):
    source["required_by_stage"][stage].insert(0, "ruff check tools/ci")
pathlib.Path(sys.argv[2]).write_text(yaml.safe_dump(source), encoding="utf-8")
PY
wiring_rc=0
wiring_out="$("$PYTHON_BIN" -I "$WIRING" "$reordered_brief" 2>&1)" || wiring_rc=$?
check_case "lane brief with a preceding command in impl and fix" 0 "$wiring_rc" "" "$wiring_out"
"$PYTHON_BIN" -I - "$reordered_brief" "$WORK" <<'PY'
import pathlib
import sys

import yaml

source = yaml.safe_load(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
destination = pathlib.Path(sys.argv[2])
line = next(
    (entry for entry in source["required_by_stage"]["impl"]
     if isinstance(entry, str) and entry.endswith(" semgrep")),
    None,
)
if line is None:
    raise AssertionError("impl has no command with the ' semgrep' suffix")
prefix = line.removesuffix(" semgrep")
review_only = {"required_by_stage": {"impl": [], "fix": [], "review": [line]}}
missing_fix = {"required_by_stage": {"impl": [line], "fix": []}}
prefix_only = {"required_by_stage": {"impl": [prefix], "fix": [prefix]}}
(destination / "review-only.yaml").write_text(yaml.safe_dump(review_only), encoding="utf-8")
(destination / "missing-fix.yaml").write_text(yaml.safe_dump(missing_fix), encoding="utf-8")
(destination / "prefix-only.yaml").write_text(yaml.safe_dump(prefix_only), encoding="utf-8")
PY
for case_name in review-only missing-fix prefix-only; do
  wiring_rc=0
  wiring_out="$("$PYTHON_BIN" -I "$WIRING" "$WORK/$case_name.yaml" 2>&1)" || wiring_rc=$?
  case "$case_name" in
    review-only) missing_stage=impl ;;
    missing-fix) missing_stage=fix ;;
    prefix-only) missing_stage=impl ;;
  esac
  check_case "lane brief $case_name" 1 "$wiring_rc" "missing $missing_stage:" "$wiring_out"
  echo "ok   lane brief $case_name"
done
echo "ok   lane brief with a preceding command in impl and fix generates three negative briefs"
mkfifo "$WORK/fifo-brief.yaml"
wiring_rc=0
wiring_out="$(run_bounded 30 "$PYTHON_BIN" -I "$WIRING" "$WORK/fifo-brief.yaml" 2>&1)" || wiring_rc=$?
check_case "lane brief FIFO" 1 "$wiring_rc" "lane brief unreadable" "$wiring_out"
echo "ok   lane brief FIFO"
wiring_rc=0
wiring_out="$(run_bounded 30 python3 -I -S "$WIRING" "$ROOT/.config/lane-brief.yaml" 2>&1)" || wiring_rc=$?
check_case "lane brief without PyYAML" 2 "$wiring_rc" "PyYAML not importable" "$wiring_out"
echo "ok   lane brief without PyYAML"
echo "ok   Makefile (verify-all, semgrep-scan), lefthook pre-push, CI semgrep job, Renovate and lane briefs wire semgrep_scan.py and SEMGREP_IMAGE"

echo "semgrep scan: $accepted accepted, $refused refused"
