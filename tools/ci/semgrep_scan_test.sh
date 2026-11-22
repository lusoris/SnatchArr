#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for semgrep_scan.py: a minimal repository with one rule, one fixture and one
# Go file, unchanged and with one defect planted at a time. A stub stands in for docker, so no
# image is pulled; it logs every call, and a defect in the rules, the fixtures or the image
# reference must be refused before the stub runs. The same stub stands in for a local Semgrep
# binary (--semgrep). Last: the pinned image comes from tools/ci/tool-versions.env and the scan
# is wired into Makefile, lefthook, CI, Renovate and the lane brief requirements.
#
#   tools/ci/semgrep_scan_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs python3, git and jq.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh
# shellcheck disable=SC1091 # shellcheck without -x does not follow this local helper.
. "$HERE/test_lib.sh"
SCAN="$HERE/semgrep_scan.py"
ROOT="${SEMGREP_WIRING_ROOT:-$(cd "$HERE/../.." && pwd)}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

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
    if [ "$STUB_MODE" = fixture-fails ]; then echo "1 test failed"; exit 1; fi
    echo "All tests passed"
    exit 0
  fi
done
# Drop every argument up to "--"; what is left are the files to scan.
while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done
shift
case "$STUB_MODE" in
  clean) jq -n '{results: [], errors: [], paths: {scanned: $ARGS.positional}}' --args "$@" ;;
  adds-file)
    printf 'package main\n' > added.go
    jq -n '{results: [], errors: [], paths: {scanned: $ARGS.positional}}' --args "$@"
    ;;
  rewrites-file)
    printf 'package main\n\nfunc main() { println("mutated") }\n' > main.go
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
  want="$1" name="$2" mode="$3" called="$4" needle="$5" edit="${6:-:}" image="${IMAGE_OVERRIDE:-$IMAGE}"
  root="$WORK/root"
  fresh "$root"
  (cd "$root" && sh -c "$edit")
  : > "$WORK/calls"
  got=0
  if [ -n "${NATIVE:-}" ]; then
    out="$(cd "${NATIVE_CWD:-$PWD}" && STUB_MODE="$mode" STUB_LOG="$WORK/calls" \
      timeout 30 python3 -I "$SCAN" --root "$root" --semgrep "$NATIVE" 2>&1)" || got=$?
  else
    out="$(STUB_MODE="$mode" STUB_LOG="$WORK/calls" timeout 30 python3 -I "$SCAN" --root "$root" --docker "$STUB" \
      --image "$image" 2>&1)" || got=$?
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

expect 0 "clean repository" clean yes "Semgrep: 1 files scanned, 0 findings"
assert_scanned_set "clean repository" main.go

# Defects refused before any docker call.
IMAGE_OVERRIDE="${IMAGE%@*}" expect 1 "image pinned by tag only" clean no "is not pinned"
IMAGE_OVERRIDE="${IMAGE%%:*}@${IMAGE#*@}" \
  expect 1 "image pinned by digest without a tag" clean no "is not pinned"
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

# MAX_TARGETS = 4096 and MAX_FIXTURES = 64. fresh() tracks 1 non-fixture Go file (main.go) plus
# 1 excluded fixture (tools/ci/semgrep/testdata/demo-rule.go):
# - 4096 - 1 = 4095 untracked Go files -> 1 + 4095 = 4096 == MAX_TARGETS (accepted at boundary);
# - 4097 untracked Go files -> 1 + 4097 = 4098 > 4096 (refused with MAX_TARGETS in diagnostic);
# - 4159 untracked Go files -> 1 + 1 + 4159 = 4161 > 4160 = MAX_TARGETS + MAX_FIXTURES (streaming cap).
expect 0 "4096 Go files at MAX_TARGETS boundary" clean yes "Semgrep: 4096 files scanned, 0 findings" \
  "i=0; while [ \$i -lt 4095 ]; do printf 'package main\n' > u-\$i.go; i=\$((i + 1)); done"
expect 1 "4097 untracked Go files above MAX_TARGETS" clean no "4098 Go files to scan, expected 1 to 4096" \
  "i=0; while [ \$i -lt 4097 ]; do printf 'package main\n' > u-\$i.go; i=\$((i + 1)); done"
expect 1 "listed Go paths above MAX_TARGETS + MAX_FIXTURES" clean no \
  "git ls-files listed more than 4160 Go files" \
  "i=0; while [ \$i -lt 4159 ]; do printf 'package main\n' > u-\$i.go; i=\$((i + 1)); done"

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
expect 0 "untracked Go file is scanned" clean yes "Semgrep: 2 files scanned, 0 findings" \
  'printf "package main\n" > extra.go'
assert_scanned_set "untracked Go file is scanned" extra.go main.go
expect 0 "ignored Go file is left out" clean yes "Semgrep: 1 files scanned, 0 findings" \
  'printf "ignored.go\n" > .gitignore && printf "package main\n" > ignored.go'
assert_scanned_set "ignored Go file is left out" main.go
expect 0 "deleted tracked file is left out" clean yes "Semgrep: 1 files scanned, 0 findings" \
  'printf "package main\n" > gone.go && git add gone.go && rm gone.go'
assert_scanned_set "deleted tracked file is left out" main.go
expect 1 "target added during scan" adds-file yes \
  "target set changed during the scan: 1 added, 0 removed: ['added.go']"
expect 1 "target rewritten during scan" rewrites-file yes \
  "target contents changed during the scan (1): ['main.go']"

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
  "native preserves caller Git excludes" clean yes "Semgrep: 1 files scanned, 0 findings" \
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
wiring_out="$(python3 -I "$WIRING" "$ROOT/.config/lane-brief.yaml" 2>&1)" || wiring_rc=$?
check_case "lane brief impl and fix wiring" 0 "$wiring_rc" "" "$wiring_out"
python3 -I - "$ROOT/.config/lane-brief.yaml" "$WORK" <<'PY'
import pathlib
import sys

import yaml

source = yaml.safe_load(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
destination = pathlib.Path(sys.argv[2])
line = source["required_by_stage"]["impl"][0]
if not isinstance(line, str) or not line.endswith(" semgrep"):
    raise AssertionError(f"expected ' semgrep' suffix, got {line!r}")
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
  wiring_out="$(python3 -I "$WIRING" "$WORK/$case_name.yaml" 2>&1)" || wiring_rc=$?
  case "$case_name" in
    review-only) missing_stage=impl ;;
    missing-fix) missing_stage=fix ;;
    prefix-only) missing_stage=impl ;;
  esac
  check_case "lane brief $case_name" 1 "$wiring_rc" "missing $missing_stage:" "$wiring_out"
  echo "ok   lane brief $case_name"
done
mkfifo "$WORK/fifo-brief.yaml"
wiring_rc=0
wiring_out="$(timeout 30 python3 -I "$WIRING" "$WORK/fifo-brief.yaml" 2>&1)" || wiring_rc=$?
check_case "lane brief FIFO" 1 "$wiring_rc" "lane brief unreadable" "$wiring_out"
echo "ok   lane brief FIFO"
echo "ok   Makefile (verify-all, semgrep-scan), lefthook pre-push, CI semgrep job, Renovate and lane briefs wire semgrep_scan.py and SEMGREP_IMAGE"

echo "semgrep scan: $accepted accepted, $refused refused"
