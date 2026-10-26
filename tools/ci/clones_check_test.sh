#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Unit tests for tools/ci/clones_check.py.
# Verifies expected exit status for clean, planted, native, missing scanner,
# and empty clone-free list fixtures.
set -eu

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "$script_dir/../.." && pwd)"
checker="$script_dir/clones_check.py"
testdata="$script_dir/testdata/clones"
clone_free="$script_dir/clone-free.txt"

failed=0

expect_exit() {
  want="$1"
  shift
  status=0
  out=$(python3 "$checker" --root "$repo_root" "$@" 2>&1) || status=$?
  if [ "$status" -eq "$want" ]; then
    echo "ok   $* -> exit $status"
  else
    echo "FAIL $*: want exit $want, got $status (output: $out)" >&2
    failed=1
  fi
}

# 1. clean.json: clone elsewhere only -> exit 0
expect_exit 0 --report "$testdata/clean.json" --clone-free "$clone_free"

# 2. planted.json: foreign_duplicates touching web/src/routes/about/+page.ts -> exit 1
expect_exit 1 --report "$testdata/planted.json" --clone-free "$clone_free"

# 3. native.json: duplicates touching web/src/lib/legacyRedirect.ts -> exit 1
expect_exit 1 --report "$testdata/native.json" --clone-free "$clone_free"

# 4. noscanner.json: foreign_scanner empty -> exit 2
expect_exit 2 --report "$testdata/noscanner.json" --clone-free "$clone_free"

# 5. nopassed.json: report without the passed field -> exit 2
expect_exit 2 --report "$testdata/nopassed.json" --clone-free "$clone_free"

# 6. empty clone-free list -> exit 2
expect_exit 2 --report "$testdata/clean.json" --clone-free "$testdata/empty-clone-free.txt"

exit "$failed"
