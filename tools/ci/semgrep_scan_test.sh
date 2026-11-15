#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for semgrep_scan.py: a minimal repository with one rule, one fixture and one
# Go file, unchanged and with one defect planted at a time. A stub stands in for docker, so no
# image is pulled; it logs every call, and a defect in the rules, the fixtures or the image
# reference must be refused before the stub runs. Last: the pinned image comes from
# tools/ci/tool-versions.env and the scan is wired into Makefile, lefthook, CI and Renovate.
#
#   tools/ci/semgrep_scan_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs python3, git and jq.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh
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
  finding) jq -n '{results: [{path: "main.go", start: {line: 1}, check_id: "demo-rule",
                   extra: {message: "planted"}}], errors: [], paths: {scanned: $ARGS.positional}}' --args "$@" ;;
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
  git -C "$1" init --quiet
  git -C "$1" add -A
}

accepted=0
refused=0

# expect WANT NAME MODE CALLED NEEDLE [SH EDIT]: after EDIT runs in a fresh repository, the scan
# must exit WANT, print NEEDLE, and call the stub (CALLED yes) or never call it (CALLED no).
expect() {
  want="$1" name="$2" mode="$3" called="$4" needle="$5" edit="${6:-:}" image="${IMAGE_OVERRIDE:-$IMAGE}"
  root="$WORK/root"
  fresh "$root"
  (cd "$root" && sh -c "$edit")
  : > "$WORK/calls"
  got=0
  out="$(STUB_MODE="$mode" STUB_LOG="$WORK/calls" python3 -I "$SCAN" --root "$root" --docker "$STUB" \
    --image "$image" 2>&1)" || got=$?
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

# Defects only Semgrep's own answer reveals.
expect 1 "fixture replay fails" fixture-fails yes "fixture tools/ci/semgrep/testdata/demo-rule.go did not pass (exit 1)"
expect 1 "scan reports a finding" finding yes "1 finding(s), 0 error(s)"
expect 1 "scan skips a file" skipped yes "report scanned 0 of 1 files; not scanned: ['main.go']"
expect 1 "scan prints no report" no-json yes "no JSON report"
expect 1 "clean report, failing exit" exit-2 yes "semgrep exited 2 with a clean report"
expect 1 "no Go file to scan" clean yes "0 Go files to scan, expected 1 to 4096" 'git rm --quiet --cached main.go'

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
echo "ok   Makefile (verify-all, semgrep-scan), lefthook pre-push, CI semgrep job and Renovate wire semgrep_scan.py and SEMGREP_IMAGE"

echo "semgrep scan: $accepted accepted, $refused refused"
