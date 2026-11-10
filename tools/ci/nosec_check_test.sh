#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Unit tests for nosec_check.py verifying inline security suppression validation.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
check=${NOSEC_CHECK:-$HERE/nosec_check.py}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0

# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"

NOSEC_FIXTURE_TIMEOUT=${NOSEC_FIXTURE_TIMEOUT:-60}
NOSEC_CHECK_TIMEOUT=${NOSEC_CHECK_TIMEOUT:-120}

# git ARGS...: run the git binary under the fixture deadline (HISS-02).
git() { timeout "$NOSEC_FIXTURE_TIMEOUT" git "$@"; }

# expect NAME WANT_STATUS SETUP [NEEDLE]: check status and optional diagnostic in a new repository.
expect() {
    local name=$1 want=$2 setup=$3 needle=${4-} repo out status
    repo="$work/$(printf '%s' "$name" | tr -c 'a-z0-9' '-')"
    set +e
    (
        set -e
        new_repo "$repo"
        cd "$repo"
        "$setup"
    )
    status=$?
    set -e
    if [ "$status" -ne 0 ]; then
        echo "FAIL $name: setup $setup status $status"
        failed=1
        return
    fi
    set +e
    out=$(cd "$repo" && timeout "$NOSEC_CHECK_TIMEOUT" python3 -I "$check" 2>&1)
    status=$?
    set -e
    if [ "$status" -ne "$want" ] || [[ "$out" != *"$needle"* ]]; then
        echo "FAIL $name: status $status (want $want), expected diagnostic: $needle, output: $out"
        failed=1
    else
        echo "ok   $name"
    fi
}

# setup_clean: valid Python and Go files with reasoned nosec annotations.
setup_clean() {
    printf '%s\n' 'import subprocess  # nosec B404  # runs only git' > main.py
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
    printf '%s\n' 'res = run()  # nosec B404,B603  # why' > main.py
    git add main.py
    git commit -q -m "initial"
}

# setup_py_dash_reason: Python directive with the unsafe dash reason separator.
setup_py_dash_reason() {
    printf '%s\n' 'res = run()  # nosec B603 -- why' > main.py
    git add main.py
    git commit -q -m "initial"
}

# setup_py_dash_names_id: Python dash reason names an additional suppression ID.
setup_py_dash_names_id() {
    printf '%s\n' 'y = eval(x)  # nosec B404 -- B307 safe' > main.py
    git add main.py
    git commit -q -m "initial"
}

# setup_py_second_hash_no_reason: Python directive has a separator but no reason.
setup_py_second_hash_no_reason() {
    printf '%s\n' 'res = run()  # nosec B603  #' > main.py
    git add main.py
    git commit -q -m "initial"
}

# setup_go_second_slash: Go directive with the unsafe second comment separator.
setup_go_second_slash() {
    printf '%s\n' 'package main' '' '// #nosec G304  // why' 'func f() {}' > main.go
    git add main.go
    git commit -q -m "initial"
}

# setup_go_dash: Go directive keeps its safe dash reason separator.
setup_go_dash() {
    printf '%s\n' 'package main' '' '// #nosec G304 -- why' 'func f() {}' > main.go
    git add main.go
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

# setup_py_string_marker: string marker must not validate the real directive.
setup_py_string_marker() {
    printf '%s\n' 'marker = "# nosec B404  # example"; y = eval(x)  # nosec B404 -- B307 safe' > main.py
    git add main.py
}

# setup_py_double_directive: invalid first directive followed by a valid one.
setup_py_double_directive() {
    printf '%s\n' 'y = eval(x)  # nosec B404 -- B307 # nosec B404  # why' > main.py
    git add main.py
}

# setup_py_second_valid_directive: two individually valid directives in one comment.
setup_py_second_valid_directive() {
    printf '%s\n' 'y = eval(x)  # nosec B307  # why # nosec B404  # why' > main.py
    git add main.py
}

# setup_py_string_only: string contents are not directives.
setup_py_string_only() {
    printf '%s\n' 's = "# nosec B307"' > main.py
    git add main.py
}

# setup_py_multiline_string: a directive-looking line inside a string is ignored.
setup_py_multiline_string() {
    printf '%s\n' 's = """' '# nosec' '"""' > main.py
    git add main.py
}

# setup_py_tokenize_error: tokenization failure must fail closed.
setup_py_tokenize_error() {
    printf '%s\n' 's = """unterminated' > main.py
    git add main.py
}

# setup_go_unscoped_first: valid second directive must not hide the unscoped first.
setup_go_unscoped_first() {
    printf '%s\n' 'package main' '// #nosec -- why // #nosec G104 -- why' > main.go
    git add main.go
}

# setup_go_block_bare: block comments can suppress all gosec rules.
setup_go_block_bare() {
    printf '%s\n' 'package main' '/* #nosec */' > main.go
    git add main.go
}

# setup_go_block_scoped: scoped block directives still require line-comment form.
setup_go_block_scoped() {
    printf '%s\n' 'package main' '/* #nosec G104 -- why */' > main.go
    git add main.go
}

# setup_go_raw_string: a raw string may contain comment syntax across lines.
setup_go_raw_string() {
    printf '%s\n' 'package main' 'const s = `first' '// #nosec`' > main.go
    git add main.go
}

# setup_go_string: quoted comment syntax is not a directive.
setup_go_string() {
    printf '%s\n' 'package main' 'func f() { s := "// #nosec"; _ = s }' > main.go
    git add main.go
}

# setup_go_second_directive: one comment must contain only one directive.
setup_go_second_directive() {
    printf '%s\n' 'package main' '// #nosec G104 -- why #nosec' > main.go
    git add main.go
}

# setup_go_prose: gosec ignores a tag inside explanatory comment text.
setup_go_prose() {
    printf '%s\n' 'package main' '// Example: "// #nosec <rule> -- <reason>"' > main.go
    git add main.go
}

# fixture_deadline: a stalled fixture git must fail before the outer deadline.
fixture_deadline() {
    local stub="$work/stub" out status start=$SECONDS elapsed
    mkdir -p "$stub"
    printf '%s\n' '#!/usr/bin/env bash' 'sleep 30' > "$stub/git"
    chmod +x "$stub/git"
    set +e
    out=$(PATH="$stub:$PATH" NOSEC_FIXTURE_TIMEOUT=1 \
        timeout 15 bash "$0" --fixture-deadline 2>&1)
    status=$?
    set -e
    elapsed=$((SECONDS - start))
    if [ "$status" -ne 1 ] || [ "$elapsed" -ge 10 ] || \
        [[ "$out" != *'FAIL fixture-deadline: setup setup_clean status 124'* ]]; then
        echo "FAIL fixture-deadline: status $status (want 1), elapsed ${elapsed}s (want <10), output: $out"
        failed=1
    else
        echo "ok   fixture-deadline"
    fi
}

if [ "${1-}" = "--fixture-deadline" ]; then
    expect "fixture-deadline" 0 setup_clean
    exit "$failed"
fi

fixture_deadline
expect "own source committed scans clean" 0 setup_self
expect "clean repo (valid py + valid go)" 0 setup_clean
expect "py # nosec bare" 1 setup_py_bare
expect "py # nosec B603 no reason" 1 setup_py_no_reason
expect "py # nosec -- why no id" 1 setup_py_no_id
expect "go // #nosec G304 no reason" 1 setup_go_no_reason
expect "go valid multi-id // #nosec G306 G703 -- why" 0 setup_go_multi_id
expect "py comma ids # nosec B404,B603  # why" 0 setup_py_comma_ids
expect "py-dash-reason" 1 setup_py_dash_reason \
    'main.py:1: nosec reason after -- widens the suppression in bandit; write # nosec Bxxx  # why'
expect "py-dash-names-id" 1 setup_py_dash_names_id \
    'main.py:1: nosec reason after -- widens the suppression in bandit; write # nosec Bxxx  # why'
expect "py-second-hash-no-reason" 1 setup_py_second_hash_no_reason \
    '(# nosec Bxxx  # why / // #nosec Gxxx -- why)'
expect "go-second-slash" 1 setup_go_second_slash \
    'main.go:3: second // after #nosec widens the suppression in gosec; write // #nosec Gxxx -- why'
expect "go-dash" 0 setup_go_dash
expect "string literal unrelated text nosecret" 0 setup_unrelated_nosecret
expect "py-string-marker" 1 setup_py_string_marker 'main.py:1: nosec reason after -- widens the suppression in bandit'
expect "py-double-directive" 1 setup_py_double_directive 'main.py:1: one nosec directive per comment; write # nosec Bxxx  # why'
expect "py-second-valid-directive" 1 setup_py_second_valid_directive 'main.py:1: one nosec directive per comment; write # nosec Bxxx  # why'
expect "py-nosec-in-string-only" 0 setup_py_string_only 'nosec_check: ok (0 annotated lines)'
expect "py-multiline-string" 0 setup_py_multiline_string 'nosec_check: ok (0 annotated lines)'
expect "py-tokenize-error" 1 setup_py_tokenize_error 'main.py:1: cannot tokenize:'
expect "go-unscoped-first" 1 setup_go_unscoped_first 'main.go:2: one nosec directive per comment'
expect "go-block-bare" 1 setup_go_block_bare 'main.go:2: #nosec in a block comment; write // #nosec Gxxx -- why'
expect "go-block-scoped" 1 setup_go_block_scoped 'main.go:2: #nosec in a block comment; write // #nosec Gxxx -- why'
expect "go-nosec-in-raw-string" 0 setup_go_raw_string 'nosec_check: ok (0 annotated lines)'
expect "go-nosec-in-string" 0 setup_go_string 'nosec_check: ok (0 annotated lines)'
expect "go-second-directive" 1 setup_go_second_directive 'main.go:2: one nosec directive per comment'
expect "go-prose-marker" 0 setup_go_prose 'nosec_check: ok (0 annotated lines)'

if [ "$failed" -ne 0 ]; then
    exit 1
fi
echo "nosec_check: all cases passed"
