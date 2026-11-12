#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Replays the Opengrep scanner fixtures and kills its mutants offline: listing
# drift, a dropped comparison, a deleted or expired baseline entry, and a
# baseline that grows or extends an expiry against an earlier commit. Then
# holds the Makefile and CI to the scanner runs.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
SCAN="$HERE/codacy_opengrep_scan.py"
FIXTURES="$HERE/codacy_opengrep/testdata"
BASELINE=tools/ci/codacy_opengrep/baseline.json
DAY=2026-11-12
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# expect WANT NAME NEEDLE COMMAND...: run COMMAND and match its result and diagnostic.
expect() {
  want=$1
  name=$2
  needle=$3
  shift 3
  got=0
  out="$("$@" 2>&1)" || got=$?
  check_case "$name" "$want" "$got" "$needle" "$out"
  echo "ok   $name"
}

# make_root DIR: copy the scanner, its config, baseline and fixtures into DIR.
make_root() {
  mkdir -p "$1/tools/ci/codacy_opengrep/testdata"
  cp "$SCAN" "$HERE/tool-versions.env" "$1/tools/ci/"
  cp "$HERE/codacy_opengrep/baseline.json" "$HERE/codacy_opengrep/opengrep-python.config.json" "$1/tools/ci/codacy_opengrep/"
  cp "$FIXTURES/main-report.json" "$FIXTURES/planted-report.json" "$1/tools/ci/codacy_opengrep/testdata/"
}

# edit_baseline FILE PYTHON: rewrite the findings list F of baseline FILE with one Python statement.
edit_baseline() {
  python3 -I -c 'import json,sys
path = sys.argv[1]
data = json.load(open(path, encoding="utf-8"))
F = data["findings"]
exec(sys.argv[2])
open(path, "w", encoding="utf-8").write(json.dumps(data, indent=2) + "\n")' "$1" "$2"
}

expect 0 "recorded report replay" "ok   tool error" python3 -I "$SCAN" --self-test --today "$DAY"
expect 0 "matching listing" "Opengrep listing: exact match" python3 -I "$SCAN" --drift "$FIXTURES/enabled-python-ids.txt"
sed '$d' "$FIXTURES/enabled-python-ids.txt" > "$WORK/listing.txt"
expect 1 "removed listing id" "listing metadata hash or count differs" python3 -I "$SCAN" --drift "$WORK/listing.txt"

make_root "$WORK/compare"
sed -i 's/actual = {fingerprint(issue) for issue in issues}/actual = set()/' "$WORK/compare/tools/ci/codacy_opengrep_scan.py"
expect 1 "mutant without comparison" "FAIL planted finding" python3 -I "$WORK/compare/tools/ci/codacy_opengrep_scan.py" --self-test --today "$DAY"

make_root "$WORK/deleted"
edit_baseline "$WORK/deleted/$BASELINE" 'del F[0]'
expect 1 "mutant deleting one baseline entry" "FAIL main baseline" python3 -I "$WORK/deleted/tools/ci/codacy_opengrep_scan.py" --self-test --today "$DAY"

expect 0 "category A entry holds on its expires day" "ok   main baseline" python3 -I "$SCAN" --self-test --today 2026-11-30
expect 1 "category A entry refused the day after" "expired 2026-11-30" python3 -I "$SCAN" --self-test --today 2026-12-01
expect 1 "category D entry refused the day after" "expired 2027-03-12" python3 -I "$SCAN" --base-ref HEAD --today 2027-03-13

make_root "$WORK/repo"
new_repo "$WORK/repo"
git -C "$WORK/repo" add tools/ci/codacy_opengrep_scan.py tools/ci/tool-versions.env tools/ci/codacy_opengrep/opengrep-python.config.json
git -C "$WORK/repo" commit -q -m "scanner without a baseline"
REPO_SCAN="$WORK/repo/tools/ci/codacy_opengrep_scan.py"
expect 0 "first baseline accepted" "first baseline" python3 -I "$REPO_SCAN" --base-ref HEAD --today "$DAY"
git -C "$WORK/repo" add "$BASELINE"
git -C "$WORK/repo" commit -q -m "baseline"
expect 0 "unchanged baseline accepted" "is a shrink" python3 -I "$REPO_SCAN" --base-ref HEAD --today "$DAY"
expect 1 "unknown base ref refused" "not a commit" python3 -I "$REPO_SCAN" --base-ref no-such-ref --today "$DAY"
cp "$WORK/repo/$BASELINE" "$WORK/baseline.json"
edit_baseline "$WORK/repo/$BASELINE" 'F.append(dict(F[0], contentSha256="1" * 64))'
expect 1 "grown baseline refused" "baseline grew" python3 -I "$REPO_SCAN" --base-ref HEAD --today "$DAY"
cp "$WORK/baseline.json" "$WORK/repo/$BASELINE"
edit_baseline "$WORK/repo/$BASELINE" 'next(e for e in F if e["category"] == "A")["expires"] = "2026-12-15"'
expect 1 "extended expiry refused" "expiry moved from 2026-11-30 to 2026-12-15" python3 -I "$REPO_SCAN" --base-ref HEAD --today "$DAY"
cp "$WORK/baseline.json" "$WORK/repo/$BASELINE"
edit_baseline "$WORK/repo/$BASELINE" 'del F[-1]'
expect 0 "shrunk baseline accepted" "is a shrink" python3 -I "$REPO_SCAN" --base-ref HEAD --today "$DAY"

ROOT="$(cd "$HERE/../.." && pwd)"
for wired in "Makefile:python3 -I tools/ci/codacy_opengrep_scan.py --self-test" \
  "Makefile:python3 -I tools/ci/codacy_opengrep_scan.py --base-ref" \
  ".github/workflows/ci.yml:python3 -I tools/ci/codacy_opengrep_scan.py --self-test" \
  ".github/workflows/ci.yml:python3 -I tools/ci/codacy_opengrep_scan.py --base-ref" \
  ".github/workflows/ci.yml:python3 -I tools/ci/codacy_opengrep_scan.py$"; do
  if ! grep -q -E "^[^#]*${wired#*:}" "$ROOT/${wired%%:*}"; then
    printf "FAIL wiring: %s runs no '%s'\n" "${wired%%:*}" "${wired#*:}" >&2
    exit 1
  fi
done
echo "ok   Makefile and CI run the self-test, the shrink check and the live scan"

echo "codacy-opengrep scan: fixtures, drift, expiry, shrink-only and mutants refused"
