#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Cases for release_body_check.py: the real files, then copies with one part removed.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
check="$root/tools/ci/release_body_check.py"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0

# variant NAME PYTHON: writes $work/NAME.json from the real config with PYTHON applied to dict d.
variant() {
    python3 -I -c "import json,sys
d=json.load(open(sys.argv[1]))
$2
json.dump(d,open(sys.argv[2],'w'))" "$root/release-please-config.json" "$work/$1.json"
}

# expect NAME WANT_STATUS WANT_TEXT CONFIG [TEMPLATE]: one check run.
expect() {
    local name=$1 want=$2 text=$3 config=$4 template=${5:-$root/.github/pull_request_template.md} out status
    set +e
    out=$(python3 -I "$check" --config "$config" --template "$template" 2>&1)
    status=$?
    set -e
    if [ "$status" -ne "$want" ] || ! printf '%s' "$out" | grep -qF -- "$text"; then
        echo "FAIL $name: status $status (want $want), output: $out"
        failed=1
    else
        echo "ok   $name"
    fi
}

variant no-footer 'd.pop("pull-request-footer")'
variant no-header 'd.pop("pull-request-header")'
variant one-item 'd["pull-request-footer"]=d["pull-request-footer"].replace("Commit messages adhere","Commits follow")'
variant checked-item 'd["pull-request-footer"]=d["pull-request-footer"].replace("- [ ] Commit messages adhere","- [x] Commit messages adhere")'
variant prose-item 'd["pull-request-footer"]=d["pull-request-footer"].replace("- [ ] Commit messages adhere","Commit messages adhere")'
printf '{"a": 1, "a": 2}\n' >"$work/dup.json"
printf '[]\n' >"$work/array.json"
printf 'no headings here\n' >"$work/empty.md"

expect "real config covers the template" 0 "ok (" "$root/release-please-config.json"
expect "missing footer refused" 1 "lacks template line: ## Pre-Merge Verification Checklist" "$work/no-footer.json"
expect "missing header refused" 1 "lacks template line: ## Description" "$work/no-header.json"
expect "one reworded checklist item refused" 1 "Commit messages adhere to Conventional Commits format" "$work/one-item.json"
expect "checked checklist item refused" 1 "Commit messages adhere to Conventional Commits format" "$work/checked-item.json"
expect "prose checklist item refused" 1 "Commit messages adhere to Conventional Commits format" "$work/prose-item.json"
expect "duplicate key cannot run" 2 "duplicate key" "$work/dup.json"
expect "array config cannot run" 2 "not a JSON object" "$work/array.json"
expect "template without sections cannot run" 2 "no heading or checklist item" "$root/release-please-config.json" "$work/empty.md"

if [ "$failed" -ne 0 ]; then
    exit 1
fi
echo "release_body_check: all cases passed"
