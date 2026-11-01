#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Unit tests for nosec_check.py verifying inline security suppression validation.
set -euo pipefail

check=$(cd "$(dirname "$0")" && pwd)/nosec_check.py
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0

# new_repo DIR: initialize a throwaway git repository at DIR.
new_repo() {
    local dir=$1
    mkdir -p "$dir"
    git -C "$dir" init -q -b main
    git -C "$dir" config user.email t@example.invalid
    git -C "$dir" config user.name t
    git -C "$dir" config commit.gpgsign false
}

# expect NAME WANT_STATUS SETUP: run SETUP inside a new repository, then run nosec_check.py with cwd there.
expect() {
    local name=$1 want=$2 setup=$3 repo out status
    repo="$work/$(printf '%s' "$name" | tr -c 'a-z0-9' '-')"
    new_repo "$repo"
    (cd "$repo" && "$setup")
    set +e
    out=$(cd "$repo" && python3 -I "$check" 2>&1)
    status=$?
    set -e
    if [ "$status" -ne "$want" ]; then
        echo "FAIL $name: status $status (want $want), output: $out"
        failed=1
    else
        echo "ok   $name"
    fi
}

# setup_clean: valid Python and Go files with reasoned nosec annotations.
setup_clean() {
    printf '%s\n' 'import subprocess  # nosec B404 -- runs only git' > main.py
    printf '%s\n' 'package main' '' 'func f() {} // #nosec G115 -- clamped' > main.go
    git add main.py main.go
    git commit -q -m "initial"
}

# setup_py_bare: Python file with bare # nosec directive.
setup_py_bare() {
    printf '%s\n' 'import subprocess  # nosec' > main.py
    git add main.py
    git commit -q -m "initial"
}

# setup_py_no_reason: Python file with rule ID but missing reason.
setup_py_no_reason() {
    printf '%s\n' 'res = run()  # nosec B603' > main.py
    git add main.py
    git commit -q -m "initial"
}

# setup_py_no_id: Python file with reason but missing rule ID.
setup_py_no_id() {
    printf '%s\n' 'res = run()  # nosec -- why' > main.py
    git add main.py
    git commit -q -m "initial"
}

# setup_go_no_reason: Go file with rule ID but missing reason.
setup_go_no_reason() {
    printf '%s\n' 'package main' '' '// #nosec G304' 'func f() {}' > main.go
    git add main.go
    git commit -q -m "initial"
}

# setup_go_multi_id: Go file with multiple rule IDs and a reason.
setup_go_multi_id() {
    printf '%s\n' 'package main' '' '// #nosec G306 G703 -- why' 'func f() {}' > main.go
    git add main.go
    git commit -q -m "initial"
}

# setup_py_comma_ids: Python file with comma-separated rule IDs and a reason.
setup_py_comma_ids() {
    printf '%s\n' 'res = run()  # nosec B404,B603 -- why' > main.py
    git add main.py
    git commit -q -m "initial"
}

# setup_unrelated_nosecret: string literal containing nosecret.
setup_unrelated_nosecret() {
    printf '%s\n' 'SECRET = "nosecret"' > main.py
    printf '%s\n' 'package main' '' 'const token = "nosecret"' > main.go
    git add main.py main.go
    git commit -q -m "initial"
}

# setup_self: the check's own source, committed, so a comment or pattern in it that reads as a
# reasonless directive is caught before the file lands in the real tree.
setup_self() {
    mkdir -p tools/ci
    cp "$check" tools/ci/nosec_check.py
    git add tools/ci/nosec_check.py
    git commit -q -m "initial"
}

expect "own source committed scans clean" 0 setup_self
expect "clean repo (valid py + valid go)" 0 setup_clean
expect "py # nosec bare" 1 setup_py_bare
expect "py # nosec B603 no reason" 1 setup_py_no_reason
expect "py # nosec -- why no id" 1 setup_py_no_id
expect "go // #nosec G304 no reason" 1 setup_go_no_reason
expect "go valid multi-id // #nosec G306 G703 -- why" 0 setup_go_multi_id
expect "py comma ids # nosec B404,B603 -- why" 0 setup_py_comma_ids
expect "string literal unrelated text nosecret" 0 setup_unrelated_nosecret

if [ "$failed" -ne 0 ]; then
    exit 1
fi
echo "nosec_check: all cases passed"
