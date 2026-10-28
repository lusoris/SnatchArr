#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for fetch.py --check:
# (a) manifest missing a non-exception key -> red naming it
# (b) logo file deleted -> red naming it
# (c) hash changed -> red naming it
# (d) a 7th exception added -> red naming it
# (e) clean -> green
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT HUP INT TERM

cd "$REPO_ROOT"

MANIFEST_ORIG="web/src/lib/logos/logos.json"
LOGOS_ORIG="web/static/logos"

if [ ! -f "$MANIFEST_ORIG" ]; then
  echo "Missing manifest file $MANIFEST_ORIG" >&2
  exit 2
fi

if [ ! -d "$LOGOS_ORIG" ]; then
  echo "Missing logos directory $LOGOS_ORIG" >&2
  exit 2
fi

MANIFEST_TMP="$WORK/logos.json"
LOGOS_TMP="$WORK/logos"

cp "$MANIFEST_ORIG" "$MANIFEST_TMP"
cp -r "$LOGOS_ORIG" "$LOGOS_TMP"

echo "case a: manifest missing a non-exception key -> red naming it"
python3 -c "
import json
with open('$MANIFEST_TMP', 'r') as f:
    d = json.load(f)
d.pop('sonarr')
with open('$MANIFEST_TMP', 'w') as f:
    json.dump(d, f, indent=2)
"
set +e
python3 tools/logos/fetch.py --check --manifest "$MANIFEST_TMP" --logos-dir "$LOGOS_TMP" > "$WORK/case_a.out" 2> "$WORK/case_a.err"
status=$?
set -e
if [ "$status" -ne 1 ]; then
  echo "FAIL case a: expected exit 1 on missing key, got $status" >&2
  cat "$WORK/case_a.err" >&2
  exit 1
fi
if ! grep -q "sonarr" "$WORK/case_a.err"; then
  echo "FAIL case a: error output did not name missing key 'sonarr'" >&2
  cat "$WORK/case_a.err" >&2
  exit 1
fi
echo "ok   case a: missing key refused with key named (exit 1)"
cat "$WORK/case_a.err"

echo "case b: logo file deleted -> red naming it"
cp "$MANIFEST_ORIG" "$MANIFEST_TMP"
rm -f "$LOGOS_TMP/sonarr.svg"
set +e
python3 tools/logos/fetch.py --check --manifest "$MANIFEST_TMP" --logos-dir "$LOGOS_TMP" > "$WORK/case_b.out" 2> "$WORK/case_b.err"
status=$?
set -e
if [ "$status" -ne 1 ]; then
  echo "FAIL case b: expected exit 1 on deleted logo file, got $status" >&2
  cat "$WORK/case_b.err" >&2
  exit 1
fi
if ! grep -q "sonarr" "$WORK/case_b.err"; then
  echo "FAIL case b: error output did not name key 'sonarr'" >&2
  cat "$WORK/case_b.err" >&2
  exit 1
fi
echo "ok   case b: deleted logo file refused with key named (exit 1)"
cat "$WORK/case_b.err"

echo "case c: hash changed -> red naming it"
cp "$MANIFEST_ORIG" "$MANIFEST_TMP"
cp "$LOGOS_ORIG/sonarr.svg" "$LOGOS_TMP/sonarr.svg"
echo "corrupted" > "$LOGOS_TMP/sonarr.svg"
set +e
python3 tools/logos/fetch.py --check --manifest "$MANIFEST_TMP" --logos-dir "$LOGOS_TMP" > "$WORK/case_c.out" 2> "$WORK/case_c.err"
status=$?
set -e
if [ "$status" -ne 1 ]; then
  echo "FAIL case c: expected exit 1 on hash mismatch, got $status" >&2
  cat "$WORK/case_c.err" >&2
  exit 1
fi
if ! grep -q "sonarr" "$WORK/case_c.err"; then
  echo "FAIL case c: error output did not name key 'sonarr'" >&2
  cat "$WORK/case_c.err" >&2
  exit 1
fi
echo "ok   case c: hash mismatch refused with key named (exit 1)"
cat "$WORK/case_c.err"

echo "case d: a 7th exception added -> red naming it"
cp "$MANIFEST_ORIG" "$MANIFEST_TMP"
cp "$LOGOS_ORIG/sonarr.svg" "$LOGOS_TMP/sonarr.svg"
python3 -c "
import json
with open('$MANIFEST_TMP', 'r') as f:
    d = json.load(f)
d['sonarr'] = {'exception': 'chat', 'reason': 'vendor terms bar integration-icon use'}
with open('$MANIFEST_TMP', 'w') as f:
    json.dump(d, f, indent=2)
"
set +e
python3 tools/logos/fetch.py --check --manifest "$MANIFEST_TMP" --logos-dir "$LOGOS_TMP" > "$WORK/case_d.out" 2> "$WORK/case_d.err"
status=$?
set -e
if [ "$status" -ne 1 ]; then
  echo "FAIL case d: expected exit 1 on 7th exception, got $status" >&2
  cat "$WORK/case_d.err" >&2
  exit 1
fi
if ! grep -q "sonarr" "$WORK/case_d.err"; then
  echo "FAIL case d: error output did not name key 'sonarr'" >&2
  cat "$WORK/case_d.err" >&2
  exit 1
fi
echo "ok   case d: 7th exception refused with key named (exit 1)"
cat "$WORK/case_d.err"

echo "case e: clean -> green"
cp "$MANIFEST_ORIG" "$MANIFEST_TMP"
python3 tools/logos/fetch.py --check --manifest "$MANIFEST_TMP" --logos-dir "$LOGOS_TMP"
echo "ok   case e: clean check passed (exit 0)"

echo "fetch_test: all 5 planted cases verified (clean pass + 4 planted mutations refused)"
