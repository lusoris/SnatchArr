#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Replays the Opengrep scanner fixtures and kills its mutants offline: listing
# hash and ID-set drift, a dropped comparison, lost multiplicity, collapsed
# literal whitespace, an ignored tool error, an unbounded download, a canary
# error that ends the scan, a deleted or expired baseline entry, and a baseline
# that grows, raises a count, extends an expiry or rewrites a kept entry against
# an earlier commit. Then holds the Makefile, lefthook and CI to the scanner runs.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
SCAN="$HERE/codacy_opengrep_scan.py"
FIXTURES="$HERE/codacy_opengrep/testdata"
BASELINE=tools/ci/codacy_opengrep/baseline.json
CONFIG=tools/ci/codacy_opengrep/opengrep-python.config.json
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
  case "$out" in
    *Traceback*)
      printf 'FAIL %s: the scanner raised instead of reporting\n%s\n' "$name" "$out" >&2
      exit 1
      ;;
  esac
  echo "ok   $name"
}

# make_root DIR: copy the scanner, its config, baseline and fixtures into DIR.
make_root() {
  mkdir -p "$1/tools/ci/codacy_opengrep/testdata"
  cp "$SCAN" "$HERE/tool-versions.env" "$1/tools/ci/"
  cp "$HERE/codacy_opengrep/baseline.json" "$HERE/codacy_opengrep/opengrep-python.config.json" "$1/tools/ci/codacy_opengrep/"
  cp "$FIXTURES/main-report.json" "$FIXTURES/planted-report.json" "$1/tools/ci/codacy_opengrep/testdata/"
}

# mutate DIR FROM TO: copy a root into DIR whose scanner has the literal line part FROM replaced by
# TO; fails when FROM is not in the scanner, so a renamed line cannot turn a mutant into the original.
mutate() {
  make_root "$1"
  python3 -I -c 'import sys
from pathlib import Path
path, old, new = Path(sys.argv[1]), sys.argv[2], sys.argv[3]
text = path.read_text(encoding="utf-8")
if text.count(old) != 1:
    sys.exit(f"mutant source not found exactly once: {old}")
path.write_text(text.replace(old, new), encoding="utf-8")' "$1/tools/ci/codacy_opengrep_scan.py" "$2" "$3"
}

# edit_json FILE OP [ARG...]: apply one allow-listed edit to a baseline or mirror config file.
# Baseline: drop-first, drop-last, drop-all, append-new-sha, set-first FIELD VALUE,
# set-first-a FIELD VALUE (first category A entry), drop-rule RULE. Config: drop-rule RULE,
# set-listing LISTING (metadata hash and count of LISTING). first-rule prints the first entry's rule.
edit_json() {
  python3 -I -c 'import hashlib, json, sys
from pathlib import Path
path, op, args = Path(sys.argv[1]), sys.argv[2], sys.argv[3:]
data = json.loads(path.read_text(encoding="utf-8"))
FIELDS = {"patternId", "filePath", "contentSha256", "count", "category", "entered", "expires", "issue", "reason"}

def findings():
    return data["findings"]

def set_first(entries, field, value):
    if field not in FIELDS or not entries:
        sys.exit(f"edit_json: no field {field} to set")
    entries[0][field] = value

def drop_rule(rule):
    if "findings" in data:
        data["findings"] = [item for item in findings() if item["patternId"] != rule]
    else:
        for tool in data["tools"]:
            tool["patterns"] = [item for item in tool["patterns"] if item["patternId"] != rule]

def set_listing(listing):
    raw = Path(listing).read_bytes()
    data["metadata"]["listingSha256"] = hashlib.sha256(raw).hexdigest()
    data["metadata"]["listingCount"] = len(raw.decode("utf-8").splitlines())

OPS = {
    "drop-first": (0, lambda: findings().pop(0)),
    "drop-last": (0, lambda: findings().pop()),
    "drop-all": (0, lambda: findings().clear()),
    "append-new-sha": (0, lambda: findings().append(dict(findings()[0], contentSha256="1" * 64))),
    "set-first": (2, lambda: set_first(findings(), *args)),
    "set-first-a": (2, lambda: set_first([item for item in findings() if item["category"] == "A"], *args)),
    "drop-rule": (1, lambda: drop_rule(*args)),
    "set-listing": (1, lambda: set_listing(*args)),
    "first-rule": (0, lambda: print(findings()[0]["patternId"])),
}
if op not in OPS or len(args) != OPS[op][0]:
    sys.exit(f"edit_json: unknown operation or argument count: {op} {args}")
OPS[op][1]()
if op != "first-rule":
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")' "$@"
}

expect 0 "recorded report replay" "ok   tool error" python3 -I "$SCAN" --self-test --today "$DAY"
expect 0 "matching listing" "Opengrep listing: exact match" python3 -I "$SCAN" --drift "$FIXTURES/enabled-python-ids.txt"
sed '$d' "$FIXTURES/enabled-python-ids.txt" > "$WORK/listing.txt"
expect 1 "removed listing id, stale metadata" "listing metadata hash or count differs" python3 -I "$SCAN" --drift "$WORK/listing.txt"
make_root "$WORK/drift"
edit_json "$WORK/drift/$CONFIG" set-listing "$WORK/listing.txt"
expect 1 "removed listing id, refreshed metadata" "listing drift: added=[], removed=['$(tail -n 1 "$FIXTURES/enabled-python-ids.txt")']" python3 -I "$WORK/drift/tools/ci/codacy_opengrep_scan.py" --drift "$WORK/listing.txt"

mutate "$WORK/compare" "actual = Counter(fingerprint(issue) for issue in issues)" "actual = Counter()"
expect 1 "mutant without comparison" "FAIL planted finding" python3 -I "$WORK/compare/tools/ci/codacy_opengrep_scan.py" --self-test --today "$DAY"
mutate "$WORK/multiplicity" "actual = Counter(fingerprint(issue) for issue in issues)" "actual = Counter({fingerprint(issue) for issue in issues})"
expect 1 "mutant counting each key once" "FAIL second occurrence" python3 -I "$WORK/multiplicity/tools/ci/codacy_opengrep_scan.py" --self-test --today "$DAY"
mutate "$WORK/literal" "LITERAL_RE.finditer(text)" 'LITERAL_RE.finditer("")'
expect 1 "mutant collapsing literal whitespace" "FAIL whitespace inside a string literal" python3 -I "$WORK/literal/tools/ci/codacy_opengrep_scan.py" --self-test --today "$DAY"
mutate "$WORK/errors" 'f"tool error: {item}" for item in errors]' 'f"tool error: {item}" for item in ()]'
expect 1 "mutant ignoring tool errors" "FAIL tool error refused" python3 -I "$WORK/errors/tools/ci/codacy_opengrep_scan.py" --self-test --today "$DAY"
mutate "$WORK/deadline" "worker.join(deadline)" "worker.join()"
expect 1 "mutant without a download deadline" "FAIL download trickling past its deadline refused" python3 -I "$WORK/deadline/tools/ci/codacy_opengrep_scan.py" --self-test --today "$DAY"
mutate "$WORK/kept" 'KEPT_FIELDS = ("entered", "category", "issue", "reason")' 'KEPT_FIELDS = ("entered", "category")'
expect 1 "mutant ignoring issue and reason changes" "FAIL changed issue refused" python3 -I "$WORK/kept/tools/ci/codacy_opengrep_scan.py" --self-test --today "$DAY"
mutate "$WORK/canary" "except ScanError as err:  # the tree scan still runs" "except OSError as err:  # the tree scan still runs"
expect 1 "mutant letting a canary error end the scan" "canary: cannot read" python3 -I "$WORK/canary/tools/ci/codacy_opengrep_scan.py" --self-test --today "$DAY"

make_root "$WORK/deleted"
edit_json "$WORK/deleted/$BASELINE" drop-first
expect 1 "mutant deleting one baseline entry" "FAIL main baseline" python3 -I "$WORK/deleted/tools/ci/codacy_opengrep_scan.py" --self-test --today "$DAY"
make_root "$WORK/empty"
edit_json "$WORK/empty/$BASELINE" drop-all
expect 1 "empty baseline: every main finding new, no crash" "FAIL main baseline" python3 -I "$WORK/empty/tools/ci/codacy_opengrep_scan.py" --self-test --today "$DAY"

expect 0 "category A entry holds on its expires day" "ok   main baseline" python3 -I "$SCAN" --self-test --today 2026-11-30
expect 1 "category A entry refused the day after" "expired 2026-11-30" python3 -I "$SCAN" --self-test --today 2026-12-01
expect 1 "category D entry refused the day after" "expired 2027-03-12" python3 -I "$SCAN" --base-ref HEAD --today 2027-03-13

make_root "$WORK/repo"
new_repo "$WORK/repo"
git -C "$WORK/repo" add tools/ci/codacy_opengrep_scan.py tools/ci/tool-versions.env "$CONFIG"
git -C "$WORK/repo" commit -q -m "scanner without a baseline"
REPO_SCAN="$WORK/repo/tools/ci/codacy_opengrep_scan.py"
expect 0 "first baseline accepted" "first baseline" python3 -I "$REPO_SCAN" --base-ref HEAD --today "$DAY"
git -C "$WORK/repo" add "$BASELINE"
git -C "$WORK/repo" commit -q -m "baseline"
expect 0 "unchanged baseline accepted" "is a shrink" python3 -I "$REPO_SCAN" --base-ref HEAD --today "$DAY"
expect 1 "unknown base ref refused" "not a commit" python3 -I "$REPO_SCAN" --base-ref no-such-ref --today "$DAY"
expect 1 "option-like base ref refused" "not a branch, tag or commit name" python3 -I "$REPO_SCAN" --base-ref=--output=x --today "$DAY"
cp "$WORK/repo/$BASELINE" "$WORK/baseline.json"
cp "$WORK/repo/$CONFIG" "$WORK/config.json"
# repo_case WANT NAME NEEDLE OP [ARG...]: edit the committed baseline, compare it with HEAD, restore it.
repo_case() {
  want=$1
  name=$2
  needle=$3
  shift 3
  edit_json "$WORK/repo/$BASELINE" "$@"
  expect "$want" "$name" "$needle" python3 -I "$REPO_SCAN" --base-ref HEAD --today "$DAY"
  cp "$WORK/baseline.json" "$WORK/repo/$BASELINE"
}
repo_case 1 "grown baseline refused" "baseline grew" append-new-sha
repo_case 1 "raised count refused" "baseline grew: count 1 -> 2" set-first count 2
repo_case 1 "extended expiry refused" "expiry moved from 2026-11-30 to 2026-12-15" set-first-a expires 2026-12-15
repo_case 1 "rewritten reason refused" "reason changed from" set-first reason "Rewritten after review; the shrink-only check refuses this edit."
repo_case 1 "renumbered issue refused" "issue changed from" set-first issue 9999
repo_case 0 "shrunk baseline accepted" "is a shrink" drop-last
RULE="$(edit_json "$WORK/repo/$BASELINE" first-rule)"
edit_json "$WORK/repo/$CONFIG" drop-rule "$RULE"
edit_json "$WORK/repo/$BASELINE" drop-rule "$RULE"
expect 0 "rule dropped from the mirror with its entries accepted" "is a shrink" python3 -I "$REPO_SCAN" --base-ref HEAD --today "$DAY"
cp "$WORK/config.json" "$WORK/repo/$CONFIG"
cp "$WORK/baseline.json" "$WORK/repo/$BASELINE"

ROOT="$(cd "$HERE/../.." && pwd)"
for wired in "Makefile:python3 -I tools/ci/codacy_opengrep_scan.py --self-test" \
  "Makefile:python3 -I tools/ci/codacy_opengrep_scan.py --base-ref" \
  "Makefile:python3 -I tools/ci/codacy_opengrep_scan.py$" \
  "Makefile:npm ci --ignore-scripts" \
  "Makefile:^tools-verify:.* tools-opengrep" \
  "lefthook.yml:make tools-opengrep" \
  ".github/workflows/ci.yml:python3 -I tools/ci/codacy_opengrep_scan.py --self-test" \
  ".github/workflows/ci.yml:python3 -I tools/ci/codacy_opengrep_scan.py --base-ref" \
  ".github/workflows/ci.yml:python3 -I tools/ci/codacy_opengrep_scan.py$" \
  ".github/workflows/ci.yml:git fetch --no-tags --quiet origin \"[\$]BEFORE_SHA\""; do
  pattern="${wired#*:}"
  case "$pattern" in
    ^*) pattern="${pattern#^}" ; anchor="^" ;;
    *) anchor="^[^#]*" ;;
  esac
  if ! grep -q -E "${anchor}${pattern}" "$ROOT/${wired%%:*}"; then
    printf "FAIL wiring: %s runs no '%s'\n" "${wired%%:*}" "${wired#*:}" >&2
    exit 1
  fi
done
echo "ok   Makefile, lefthook and CI run the self-test, the shrink check, the install and the live scan"

echo "codacy-opengrep scan: fixtures, drift, expiry, shrink-only and mutants refused"
