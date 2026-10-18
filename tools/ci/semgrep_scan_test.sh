#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for semgrep_scan.py: a minimal repository with one rule, one fixture and one
# Go file, unchanged and with one defect planted at a time. A stub stands in for docker, so no
# image is pulled; it logs every call, and a defect in the rules, the fixtures or the image
# reference must be refused before the stub runs.
#
#   tools/ci/semgrep_scan_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs python3, git and jq.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
SCAN="$HERE/semgrep_scan.py"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
IMAGE="semgrep/semgrep:1.180.0@sha256:529ee8a277ec8adc5b534d7c74eea0a47e9de21d62852b6ba7ac6ba9566845c3"

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
  if [ "$got" -ne "$want" ]; then
    printf 'FAIL %s: exit %s, want %s\n%s\n' "$name" "$got" "$want" "$out" >&2
    exit 1
  fi
  case "$out" in
    *"$needle"*) ;;
    *)
      printf 'FAIL %s: output lacks "%s"\n%s\n' "$name" "$needle" "$out" >&2
      exit 1
      ;;
  esac
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
IMAGE_OVERRIDE="semgrep/semgrep:1.180.0" expect 1 "image pinned by tag only" clean no "is not pinned"
IMAGE_OVERRIDE="semgrep/semgrep@sha256:529ee8a277ec8adc5b534d7c74eea0a47e9de21d62852b6ba7ac6ba9566845c3" \
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
expect 1 "rule without a ruleid line" clean no 'no `ruleid:` fixture line for rule(s): demo-rule' \
  "sed -i '/ruleid:/d' $FX/demo-rule.go"
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

echo "semgrep scan: $accepted accepted, $refused refused"
