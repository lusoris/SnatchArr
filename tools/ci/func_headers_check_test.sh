#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Cases for func_headers_check.py: each builds a throwaway repository with a base commit, applies
# one change in the working tree and checks the exit status and the named function.
set -euo pipefail

check=$(cd "$(dirname "$0")" && pwd)/func_headers_check.py
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0

# new_repo DIR: a repository whose base commit holds lib.sh and lib.py, both with headers.
new_repo() {
    local dir=$1
    mkdir -p "$dir"
    git -C "$dir" init -q -b main
    git -C "$dir" config user.email t@example.invalid
    git -C "$dir" config user.name t
    git -C "$dir" config commit.gpgsign false
    printf '%s\n' '#!/usr/bin/env bash' '# old_fn says hi.' 'old_fn() {' '    echo hi' '}' \
        '' 'bare_fn() {' '    echo bare' '}' >"$dir/lib.sh"
    printf '%s\n' 'def old():' '    """Say hi."""' '    return 1' '' 'def bare():' '    return 2' >"$dir/lib.py"
    git -C "$dir" add -A
    git -C "$dir" commit -q -m base
}

# expect NAME WANT_STATUS WANT_TEXT EDIT [BASE]: the function EDIT changes the repository in $repo, then the check runs.
expect() {
    local name=$1 want=$2 text=$3 edit=$4 base=${5:-main} out status
    repo="$work/$(printf '%s' "$name" | tr -c 'a-z0-9' '-')"
    new_repo "$repo"
    (cd "$repo" && "$edit")
    set +e
    out=$(python3 -I "$check" --root "$repo" "--base=$base" 2>&1)
    status=$?
    set -e
    if [ "$status" -ne "$want" ] || ! printf '%s' "$out" | grep -qF -- "$text"; then
        echo "FAIL $name: status $status (want $want), output: $out"
        failed=1
    else
        echo "ok   $name"
    fi
}

# edit_1: untouched bare function passes.
edit_1() {
    printf "%s\n" "# new_fn is documented." "new_fn() {" "    :" "}" >>lib.sh
}

# edit_2: new shell function without header refused.
edit_2() {
    printf "%s\n" "" "new_fn() {" "    :" "}" >>lib.sh
}

# edit_3: edit inside a bare shell function refused.
edit_3() {
    sed -i "s/echo bare/echo changed/" lib.sh
}

# edit_4: deletion inside a bare shell function refused.
edit_4() {
    sed -i "/echo bare/d" lib.sh
}

# edit_5: shellcheck directive is no header.
edit_5() {
    printf "%s\n" "" "# shellcheck disable=SC2034" "new_fn() {" "    :" "}" >>lib.sh
}

# edit_6: header above a directive passes.
edit_6() {
    printf "%s\n" "" "# new_fn is documented." "# shellcheck disable=SC2034" "new_fn() {" "    :" "}" >>lib.sh
}

# edit_7: function keyword form refused.
edit_7() {
    printf "%s\n" "" "function kw_fn {" "    :" "}" >>lib.sh
}

# edit_8: edit inside a documented shell function passes.
edit_8() {
    sed -i "s/echo hi/echo hello/" lib.sh
}

# edit_9: python function without docstring refused.
edit_9() {
    sed -i "s/return 2/return 3/" lib.py
}

# edit_10: python function with docstring passes.
edit_10() {
    sed -i "s/return 1/return 4/" lib.py
}

# edit_11: unknown base cannot run.
edit_11() {
    git branch -q -m main other
}

# edit_12: one-line shell function without header refused.
edit_12() {
    printf "%s\n" "" "new_fn() { :; }" >>lib.sh
}

# edit_13: one-line shell function with header passes.
edit_13() {
    printf "%s\n" "" "# new_fn is documented." "new_fn() { :; }" >>lib.sh
}

# edit_14: deleting blank line before bare function passes.
edit_14() {
    sed -i "6{/^$/d}" lib.sh
}

# edit_15: untracked shell file with bare function refused.
edit_15() {
    printf "%s\n" "#!/usr/bin/env bash" "bare_untracked() {" "    :" "}" >new.sh
}

# edit_16: untracked shell file with header passes.
edit_16() {
    printf "%s\n" "#!/usr/bin/env bash" "# documented." "good_untracked() {" "    :" "}" >new.sh
}

# edit_17: shell function closed with semicolon does not run on into next function.
edit_17() {
    printf "%s\n" "" "undoc() {" "    :" "};" "" "# doc_fn is documented." "doc_fn() {" "    echo doc_hi" "}" >>lib.sh
    git add -A && git commit -q -m base2
    sed -i "s/echo doc_hi/echo doc_hello/" lib.sh
}

# edit_18: option-like base ref refused.
edit_18() {
    :
}

# edit_19: shell function with comment after opening brace recognized.
edit_19() {
    printf "%s\n" "" "new_fn() { # why" "    :" "}" >>lib.sh
}

# edit_20: committed file with quoted non-ASCII name in diff checked.
edit_20() {
    printf '%s\n' '#!/usr/bin/env bash' 'bare_naive() {' '    echo naive' '}' >"naïve.sh"
    git add "naïve.sh" && git commit -q -m naive
    sed -i "s/echo naive/echo changed/" "naïve.sh"
}

# edit_21: untracked file with quoted non-ASCII name checked.
edit_21() {
    printf '%s\n' '#!/usr/bin/env bash' 'bare_uber() {' '    echo uber' '}' >"über.sh"
}

# edit_22: closing brace inside heredoc body does not end function early.
edit_22() {
    printf '%s\n' '' 'undoc() {' "    cat <<'EOF'" '}' 'EOF' '    echo changed' '}' >>lib.sh
    git add lib.sh && git commit -q -m undoc
    sed -i "s/echo changed/echo touched/" lib.sh
}

# edit_23: arithmetic left shift is no heredoc, so the function end is still found.
# shellcheck disable=SC2016 # literal $((...)) is the fixture text
edit_23() {
    printf '%s\n' '' 'undoc_shift() {' '    echo $((1 << 2))' '    echo a' '}' >>lib.sh
    git add lib.sh && git commit -q -m shift
    sed -i "s/echo a$/echo b/" lib.sh
}

# edit_24: untracked file whose name starts with a space keeps its name.
edit_24() {
    printf '%s\n' '#!/usr/bin/env bash' 'bare_space() {' '    echo space' '}' >" lead.sh"
}

expect "untouched bare function passes" 0 "ok (" edit_1
expect "new shell function without header refused" 1 "shell function new_fn has no header comment" edit_2
expect "edit inside a bare shell function refused" 1 "shell function bare_fn has no header comment" edit_3
expect "deletion inside a bare shell function refused" 1 "shell function bare_fn has no header comment" edit_4
expect "shellcheck directive is no header" 1 "shell function new_fn has no header comment" edit_5
expect "header above a directive passes" 0 "ok (" edit_6
expect "function keyword form refused" 1 "shell function kw_fn has no header comment" edit_7
expect "edit inside a documented shell function passes" 0 "ok (" edit_8
expect "python function without docstring refused" 1 "Python function bare has no docstring" edit_9
expect "python function with docstring passes" 0 "ok (" edit_10
expect "unknown base cannot run" 2 "cannot run" edit_11
expect "one-line shell function without header refused" 1 "shell function new_fn has no header comment" edit_12
expect "one-line shell function with header passes" 0 "ok (" edit_13
expect "deleting blank line before bare function passes" 0 "ok (" edit_14
expect "untracked shell file with bare function refused" 1 "new.sh:2: shell function bare_untracked has no header comment" edit_15
expect "untracked shell file with header passes" 0 "ok (" edit_16
expect "shell function closed with semicolon does not run on" 0 "ok (" edit_17
expect "option-like base ref refused" 2 "starts with '-'" edit_18 "--output=/dev/null"
expect "comment after opening brace recognized as shell function" 1 "shell function new_fn has no header comment" edit_19
expect "committed file with quoted non-ASCII name in diff checked" 1 "shell function bare_naive has no header comment" edit_20
expect "untracked file with quoted non-ASCII name checked" 1 "shell function bare_uber has no header comment" edit_21
expect "closing brace inside heredoc body does not end function early" 1 "shell function undoc has no header comment" edit_22
expect "arithmetic shift is not a heredoc" 1 "shell function undoc_shift has no header comment" edit_23
expect "untracked name with leading space checked" 1 "shell function bare_space has no header comment" edit_24

if [ "$failed" -ne 0 ]; then
    exit 1
fi
echo "func_headers_check: all cases passed"
