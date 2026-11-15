#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Cases for func_headers_check.py: each builds a throwaway repository with a base commit, applies
# one change in the working tree and checks the exit status and the named function.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
check=$HERE/func_headers_check.py
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0

# shellcheck source=tools/ci/test_lib.sh
# shellcheck disable=SC1091 # standalone shellcheck does not follow sourced files
. "$HERE/test_lib.sh"

# seed_repo DIR: a repository whose base commit holds lib.sh and lib.py, both with headers.
seed_repo() {
    local dir=$1
    new_repo "$dir"
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
    seed_repo "$repo"
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

# edit_25: committed file with space in name checked when undocumented function touched.
edit_25() {
    printf '%s\n' '#!/usr/bin/env bash' \
        '# doc_fn is documented.' 'doc_fn() {' '    echo doc' '}' \
        '' 'bare_fn() {' '    echo bare' '}' >"my script.sh"
    git add "my script.sh" && git commit -q -m "space"
    sed -i "s/echo bare/echo changed/" "my script.sh"
}

# edit_26: function-shaped Python source inside a multiline double-quoted argument is ignored.
edit_26() {
    printf '%s\n' '#!/usr/bin/env bash' 'python3 -c "' 'bare_double() {' '    :' '}' '"' >quoted-double.sh
}

# edit_27: function-shaped text inside a multiline single-quoted argument is ignored.
edit_27() {
    printf '%s\n' '#!/usr/bin/env bash' "python3 -c '" 'bare_single() {' '    :' '}' "'" >quoted-single.sh
}

# edit_28: function-shaped heredoc content is ignored.
edit_28() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<EOF' 'bare_here() {' '    :' '}' 'EOF' >heredoc.sh
}

# edit_29: function-shaped <<- heredoc content is ignored with tab stripping.
edit_29() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<-EOF' $'\tbare_tabbed() {' $'\t    :' $'\t}' $'\tEOF' >heredoc-tabs.sh
}

# edit_30: real function after a multiline quoted argument is still checked.
edit_30() {
    printf '%s\n' '#!/usr/bin/env bash' 'python3 -c "' 'inside_string() {' '    :' '}' '"' \
        'bare_after_string() {' '    :' '}' >after-string.sh
}

# edit_31: real function after a heredoc is still checked.
edit_31() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<EOF' 'inside_heredoc() {' '    :' '}' 'EOF' \
        'bare_after_heredoc() {' '    :' '}' >after-heredoc.sh
}

# edit_32: escaped quote does not close a multiline double-quoted argument.
edit_32() {
    printf '%s\n' '#!/usr/bin/env bash' 'python3 -c "' '  \" escaped quote' \
        'inside_escaped() {' '    :' '}' '"' >escaped-quote.sh
}

# edit_33: quote characters in comments do not affect quote state.
edit_33() {
    printf '%s\n' '#!/usr/bin/env bash' '# "' "# '" '' 'bare_after_comments() {' '    :' '}' >comment-quotes.sh
}

# edit_34: a brace inside a multiline string does not end the real function early.
edit_34() {
    printf '%s\n' '' 'undoc_string() {' '    python3 -c "' '}' '"' '    echo changed' '}' >>lib.sh
    git add lib.sh && git commit -q -m string
    sed -i 's/echo changed/echo touched/' lib.sh
}

# edit_35: ANSI-C escaped apostrophe closes only at the final quote.
edit_35() {
    printf '%s\n' '#!/usr/bin/env bash' "printf '%s\\n' \$'it\\'s'" 'bare_ansi() { :; }' >ansi.sh
}

# edit_36: an escaped backslash leaves the next apostrophe unescaped.
edit_36() {
    printf '%s\n' '#!/usr/bin/env bash' "printf '%s\\n' \$'a\\\\'" 'bare_ansi_boundary() { :; }' >ansi-boundary.sh
}

# edit_37: command substitution inside double quotes parses its own quotes.
# shellcheck disable=SC2016 # literal command substitution is the fixture text
edit_37() {
    printf '%s\n' '#!/usr/bin/env bash' 'value="$(printf '\''%s'\'' "can'\''t")"' \
        'bare_subst() { :; }' >subst.sh
}

# edit_38: a multiline string inside command substitution masks function text.
# shellcheck disable=SC2016 # literal command substitution is the fixture text
edit_38() {
    printf '%s\n' '#!/usr/bin/env bash' 'x="$(printf '\''%s\n'\'' "' 'fake_sub() { :; }' '")"' >subst-multiline.sh
}

# edit_39: backtick substitution inside double quotes parses its own quotes.
edit_39() {
    printf '%s\n' '#!/usr/bin/env bash' 'value="`printf '\''%s'\'' "can'\''t"`"' \
        'bare_backtick() { :; }' >backtick.sh
}

# edit_40: operator-adjacent hash starts a comment.
edit_40() {
    printf '%s\n' '#!/usr/bin/env bash' "echo a;#'" 'bare_semi() { :; }' >semi-comment.sh
}

# edit_41: hash within a word is literal and the following quote opens.
edit_41() {
    printf '%s\n' '#!/usr/bin/env bash' 'echo a#"' 'fake_hash() { :; }' '"' >literal-hash.sh
}

# edit_42: dollar arithmetic left shift does not start a heredoc.
# shellcheck disable=SC2016 # literal arithmetic expansion is the fixture text
edit_42() {
    printf '%s\n' '#!/usr/bin/env bash' 'echo $((1 << BITS))' 'bare_bits() { :; }' >dollar-shift.sh
}

# edit_43: command arithmetic left shift does not start a heredoc.
edit_43() {
    printf '%s\n' '#!/usr/bin/env bash' '(( y = 1 << BITS ))' 'bare_plain_shift() { :; }' >plain-shift.sh
}

# edit_44: unquoted hyphen belongs to the whole heredoc delimiter.
edit_44() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<END-TEXT' 'END' 'fake_early() { :; }' 'END-TEXT' >hyphen-here.sh
}

# edit_45: quoted hyphen belongs to the whole heredoc delimiter.
edit_45() {
    printf '%s\n' '#!/usr/bin/env bash' "cat <<'END-TEXT'" 'END' 'fake_quoted() { :; }' 'END-TEXT' >quoted-hyphen.sh
}

# edit_46: mixed quoting yields EOF and does not consume the next function.
edit_46() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<E"O"F' 'EOF' 'bare_after_mixed() { :; }' >mixed-here.sh
}

# edit_47: a heredoc after a multiline quote masks top-level function text.
edit_47() {
    printf '%s\n' '#!/usr/bin/env bash' 'printf '\''%s\n'\'' "prefix' \
        'suffix"; cat <<EOF' 'fake_tail() { :; }' 'EOF' >tail-here.sh
}

# edit_48: a heredoc after a multiline quote does not close its enclosing function early.
edit_48() {
    printf '%s\n' '' 'undoc_tail() {' '    printf '\''%s\n'\'' "prefix' \
        'suffix"; cat <<EOF' '}' 'EOF' '    echo a' '}' >>lib.sh
    git add lib.sh && git commit -q -m tail
    sed -i 's/echo a$/echo touched/' lib.sh
}

# edit_49: context nesting above the fixed cap reports a file and line error.
# shellcheck disable=SC2016 # literal command substitution openers are the fixture text
edit_49() {
    printf '%s\n' '#!/usr/bin/env bash' "$(printf '\$(%.0s' {1..33})" >deep-context.sh
}

# edit_50: a double-quoted hyphen delimiter is one word.
edit_50() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<"END-TEXT"' 'END' 'fake_double_here() { :; }' \
        'END-TEXT' >double-hyphen.sh
}

# edit_51: a backslash-quoted delimiter closes at EOF.
edit_51() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<\EOF' 'EOF' 'bare_after_backslash() { :; }' >backslash-here.sh
}

# edit_52: nested ordinary parentheses leave arithmetic before a real heredoc.
# shellcheck disable=SC2016 # literal arithmetic expansion is the fixture text
edit_52() {
    printf '%s\n' '#!/usr/bin/env bash' 'echo $(( ((1 << BITS) + 1) ))' \
        'cat <<EOF' 'fake_nested() { :; }' 'EOF' >nested-shift.sh
}

# edit_53: a quoted empty word is a valid heredoc delimiter.
edit_53() {
    printf '%s\n' '#!/usr/bin/env bash' "cat <<''" 'fake_empty() { :; }' '' >empty-here.sh
}

# edit_54: a closing parenthesis starts a new word before a comment.
edit_54() {
    printf '%s\n' '#!/usr/bin/env bash' "(echo p)#'" 'bare_paren() { :; }' >paren-comment.sh
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
expect "committed file with space in name in diff checked" 1 "my script.sh" edit_25
expect "function text in double-quoted string ignored" 0 "ok (" edit_26
expect "function text in single-quoted string ignored" 0 "ok (" edit_27
expect "function text in heredoc ignored" 0 "ok (" edit_28
expect "function text in tab-stripped heredoc ignored" 0 "ok (" edit_29
expect "function after quoted string still checked" 1 "shell function bare_after_string has no header comment" edit_30
expect "function after heredoc still checked" 1 "shell function bare_after_heredoc has no header comment" edit_31
expect "escaped quote keeps string open" 0 "ok (" edit_32
expect "quotes in comments do not open strings" 1 "shell function bare_after_comments has no header comment" edit_33
expect "brace in string does not end function early" 1 "shell function undoc_string has no header comment" edit_34
expect "ANSI-C escaped quote closes correctly" 1 "shell function bare_ansi has no header comment" edit_35
expect "ANSI-C escaped backslash boundary" 1 "shell function bare_ansi_boundary has no header comment" edit_36
expect "substitution inside double quotes parses shell quotes" 1 "shell function bare_subst has no header comment" edit_37
expect "multiline substitution quote masks function" 0 "ok (" edit_38
expect "backtick substitution parses shell quotes" 1 "shell function bare_backtick has no header comment" edit_39
expect "operator-adjacent hash starts comment" 1 "shell function bare_semi has no header comment" edit_40
expect "hash within a word stays literal" 0 "ok (" edit_41
expect "dollar arithmetic shift is not heredoc" 1 "shell function bare_bits has no header comment" edit_42
expect "command arithmetic shift is not heredoc" 1 "shell function bare_plain_shift has no header comment" edit_43
expect "unquoted hyphen heredoc delimiter stays whole" 0 "ok (" edit_44
expect "quoted hyphen heredoc delimiter stays whole" 0 "ok (" edit_45
expect "mixed quote heredoc delimiter ends at EOF" 1 "shell function bare_after_mixed has no header comment" edit_46
expect "heredoc after closing quote masks function" 0 "ok (" edit_47
expect "heredoc after closing quote in function finds end" 1 "shell function undoc_tail has no header comment" edit_48
expect "context depth cap names file and line" 2 "deep-context.sh:2" edit_49
expect "double-quoted hyphen heredoc delimiter stays whole" 0 "ok (" edit_50
expect "backslash-quoted heredoc delimiter ends at EOF" 1 "shell function bare_after_backslash has no header comment" edit_51
expect "nested arithmetic ends before real heredoc" 0 "ok (" edit_52
expect "quoted empty heredoc word closes on blank line" 0 "ok (" edit_53
expect "closing parenthesis starts comment word" 1 "shell function bare_paren has no header comment" edit_54

if [ "$failed" -ne 0 ]; then
    exit 1
fi
echo "func_headers_check: all cases passed"
