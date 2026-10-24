#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for generate.py --check:
# (1) unmodified -> exit 0
# (2) one package entry deleted -> exit 1
# (3) one version changed -> exit 1
# (4) label corrections and URL normalisation: labels_test.py (positive, negative, boundary)
# (5) uncollapsed gobreaker v2 url -> exit 1
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT HUP INT TERM

cd "$REPO_ROOT"

COMMITTED_ORIG="web/static/licences.json"
FIXTURE="$WORK/licences.json"

if [ ! -f "$COMMITTED_ORIG" ]; then
  echo "Missing committed licences file $COMMITTED_ORIG" >&2
  exit 2
fi

cp "$COMMITTED_ORIG" "$FIXTURE"

echo "case 1: unmodified committed file -> --check must exit 0"
python3 tools/licences/generate.py --check --committed "$FIXTURE"
echo "ok   case 1: unmodified accepted"

echo "case 2: one package entry deleted -> --check must exit 1"
python3 -c "
import json
with open('$FIXTURE', 'r') as f:
    d = json.load(f)
d['packages'].pop(0)
with open('$FIXTURE', 'w') as f:
    json.dump(d, f, indent=2, sort_keys=True)
"
set +e
python3 tools/licences/generate.py --check --committed "$FIXTURE" > "$WORK/case2.out" 2> "$WORK/case2.err"
status=$?
set -e
if [ "$status" -ne 1 ]; then
  echo "FAIL case 2: expected exit 1 on deleted package, got $status" >&2
  cat "$WORK/case2.err" >&2
  exit 1
fi
echo "ok   case 2: deleted package entry refused (exit 1)"
cat "$WORK/case2.err"

echo "case 3: one version changed -> --check must exit 1"
cp "$COMMITTED_ORIG" "$FIXTURE"
python3 -c "
import json
with open('$FIXTURE', 'r') as f:
    d = json.load(f)
d['packages'][0]['version'] = '999.999.999'
with open('$FIXTURE', 'w') as f:
    json.dump(d, f, indent=2, sort_keys=True)
"
set +e
python3 tools/licences/generate.py --check --committed "$FIXTURE" > "$WORK/case3.out" 2> "$WORK/case3.err"
status=$?
set -e
if [ "$status" -ne 1 ]; then
  echo "FAIL case 3: expected exit 1 on changed version, got $status" >&2
  cat "$WORK/case3.err" >&2
  exit 1
fi
echo "ok   case 3: changed version refused (exit 1)"
cat "$WORK/case3.err"

echo "case 4: go-licenses label corrections and URL normalisation"
python3 -B tools/licences/labels_test.py
echo "ok   case 4: label corrections and URL normalisation verified"

echo "case 5: uncollapsed gobreaker v2 url -> --check must exit 1"
cp "$COMMITTED_ORIG" "$FIXTURE"
python3 -c "
import json
with open('$FIXTURE', 'r') as f:
    d = json.load(f)
for p in d['packages']:
    if p.get('name') == 'github.com/sony/gobreaker/v2':
        p['url'] = 'https://github.com/sony/gobreaker/blob/v2.4.0/v2/LICENSE'
with open('$FIXTURE', 'w') as f:
    json.dump(d, f, indent=2, sort_keys=True)
"
set +e
python3 tools/licences/generate.py --check --committed "$FIXTURE" > "$WORK/case5.out" 2> "$WORK/case5.err"
status=$?
set -e
if [ "$status" -ne 1 ]; then
  echo "FAIL case 5: expected exit 1 on uncollapsed gobreaker url, got $status" >&2
  cat "$WORK/case5.err" >&2
  exit 1
fi
echo "ok   case 5: uncollapsed gobreaker url refused (exit 1)"
cat "$WORK/case5.err"

echo "generate_test: 5 cases verified (1 clean pass, 3 planted defects refused, label and URL unit tests)"
