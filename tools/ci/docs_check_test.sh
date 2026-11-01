#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Real docs builds against clean inputs and planted links; invalid requirements
# must fail before invoking MkDocs. One cache serves every case.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
check="$HERE/docs_check.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cache="${1:-$work/cache}"
root="$work/root"
failed=0

mkdir -p "$root/tools/docs"
# mkdocs.yml currently references only docs/ (including theme assets); no
# external overrides, hooks or inherited config need copying.
cp "$REPO_ROOT/mkdocs.yml" "$root/mkdocs.yml"
cp -R "$REPO_ROOT/docs" "$root/docs"
cp "$REPO_ROOT/tools/docs/requirements.txt" "$root/tools/docs/requirements.txt"

# run_case NAME WANT_RC NEEDLE [BIN [CACHE]]: negative cases accept any nonzero status.
run_case() {
    local name=$1 want=$2 needle=$3 out status=0 result
    out=$(MKDOCS_BIN="${4:-}" sh "$check" "${5:-$cache}" "$root" 2>&1) || status=$?
    result=$status
    if [ "$want" -ne 0 ] && [ "$status" -ne 0 ]; then
        result=1
    fi
    if (check_case "$name" "$want" "$result" "$needle" "$out"); then
        echo "ok   $name"
    else
        failed=1
    fi
}

run_case 'clean copy passes' 0 ''
# An environment whose install stopped after bin/mkdocs appeared has no
# completion marker; the check must rebuild it, not run the stale executable.
stale="$work/stale-cache"
req_hash="$(sha256sum "$root/tools/docs/requirements.txt" | cut -c1-12)"
mkdir -p "$stale/docs-$req_hash/bin"
printf '%s\n' '#!/bin/sh' 'echo "stale environment used"' 'exit 1' > "$stale/docs-$req_hash/bin/mkdocs"
chmod +x "$stale/docs-$req_hash/bin/mkdocs"
run_case 'interrupted install rebuilt' 0 '' '' "$stale"
# A completed environment built by another interpreter must be rebuilt too.
other="$work/other-interpreter-cache"
mkdir -p "$other/docs-$req_hash/bin"
printf '%s\n' '#!/bin/sh' 'echo "stale environment used"' 'exit 1' > "$other/docs-$req_hash/bin/mkdocs"
chmod +x "$other/docs-$req_hash/bin/mkdocs"
printf '%s\n' '3.0.0 /nonexistent/python3' > "$other/docs-$req_hash/.installed"
run_case 'environment from another interpreter rebuilt' 0 '' '' "$other"
printf '\n[x](../../go.mod)\n' >> "$root/docs/index.md"
run_case 'link outside docs refused' 1 'go.mod'
cp "$REPO_ROOT/docs/index.md" "$root/docs/index.md"
printf '\n[x](missing-doc-page.md)\n' >> "$root/docs/index.md"
run_case 'missing doc page refused' 1 'missing-doc-page.md'
cp "$REPO_ROOT/docs/index.md" "$root/docs/index.md"

# A successful stub would make either refusal case fail if a build were reached.
stub="$work/mkdocs-stub"
printf '%s\n' '#!/bin/sh' 'echo "unexpected build reached"' 'exit 0' > "$stub"
chmod +x "$stub"
printf '%s\n' 'unpinned-package # == is only a comment' >> "$root/tools/docs/requirements.txt"
run_case 'unpinned requirements refused before build' 1 'exact == version pin' "$stub"
cp "$REPO_ROOT/tools/docs/requirements.txt" "$root/tools/docs/requirements.txt"
printf '%s\n' 'marker-only>=1; python_version == "3.15"' >> "$root/tools/docs/requirements.txt"
run_case 'marker == is not a version pin' 1 'exact == version pin' "$stub"
cp "$REPO_ROOT/tools/docs/requirements.txt" "$root/tools/docs/requirements.txt"
printf '%s\n' 'wildcard-package==9.*' >> "$root/tools/docs/requirements.txt"
run_case 'wildcard == is not an exact pin' 1 'exact == version pin' "$stub"
cp "$REPO_ROOT/tools/docs/requirements.txt" "$root/tools/docs/requirements.txt"
# A held environment lock blocks a second check until released, and fails it after the bound.
lock="$cache/docs-$req_hash.lock"
mkdir "$lock"
out=$(DOCS_LOCK_SECONDS=1 MKDOCS_BIN="$stub" sh "$check" "$cache" "$root" 2>&1) && status=0 || status=$?
if (check_case 'held lock refused after bound' 1 "$([ "$status" -ne 0 ] && echo 1 || echo 0)" 'held for 1s' "$out"); then
    echo "ok   held lock refused after bound"
else
    failed=1
fi
(sleep 2 && rmdir "$lock") &
out=$(DOCS_LOCK_SECONDS=20 MKDOCS_BIN="$stub" sh "$check" "$cache" "$root" 2>&1) && status=0 || status=$?
wait
if (check_case 'released lock taken and freed' 0 "$status" 'unexpected build reached' "$out") && [ ! -d "$lock" ]; then
    echo "ok   released lock taken and freed"
else
    echo "FAIL released lock taken and freed: lock left or run failed" >&2
    failed=1
fi
rm "$root/tools/docs/requirements.txt"
run_case 'missing requirements refused before build' 1 'requirements file missing' "$stub"

exit "$failed"
