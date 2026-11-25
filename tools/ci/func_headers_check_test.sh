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

# expect NAME WANT_STATUS WANT_TEXT EDIT [BASE] [TOOL_PATH] [MORE_TEXT] [RUN_DIR] [WORKSPACE]:
# EDIT changes $repo; WORKSPACE sets GOWORK to its fixture file for this check only.
expect() {
    local name=$1 want=$2 text=$3 edit=$4 base=${5:-main} tool_path=${6:-} more_text=${7:-} run_dir=${8:-} workspace=${9:-} out status
    repo="$work/$(printf '%s' "$name" | tr -c 'a-z0-9' '-')"
    seed_repo "$repo"
    (cd "$repo" && "$edit")
    set +e
    out=$(
        if [ -n "$workspace" ]; then
            export GOWORK="$repo/go.work"
        fi
        if [ -n "$run_dir" ]; then
            cd "$run_dir" 2>&1 || exit 2
        fi
        if [ -n "$tool_path" ]; then
            PATH="$tool_path" python3 -I "$check" --root "$repo" "--base=$base" 2>&1
        else
            python3 -I "$check" --root "$repo" "--base=$base" 2>&1
        fi
    )
    status=$?
    set -e
    if [ "$status" -ne "$want" ] || ! printf '%s' "$out" | grep -qF -- "$text" ||
        { [ -n "$more_text" ] && ! printf '%s' "$out" | grep -qF -- "$more_text"; }; then
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

# edit_4: deleting a committed body line leaves a valid bare function and is refused.
edit_4() {
    sed -i '/echo bare/a\    :' lib.sh
    git add lib.sh && git commit -q -m extra-body
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

# edit_49: shfmt refuses the unclosed nested substitutions and reports their location.
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

# edit_55: double quotes retain a backslash before a non-special character.
edit_55() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<"END\-TEXT"' 'heredoc body' 'END\-TEXT' \
        'missing_header() { :; }' >double-quoted-backslash.sh
}

# edit_56: a nonmatching unescaped line does not end a double-quoted delimiter.
edit_56() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<"END\-TEXT"' 'END-TEXT' 'fake_dq() { :; }' \
        'END\-TEXT' >double-quoted-nonending.sh
}

# edit_57: shfmt leaves a backslash before a dollar in the delimiter and refuses the heredoc.
edit_57() {
    printf '%s\n' '#!/usr/bin/env bash' "cat <<\"A\\\$B\"" "A\$B" 'missing_dollar() { :; }' >double-quoted-dollar.sh
}

# edit_58: shfmt does not decode ANSI-C delimiter escapes and refuses the heredoc.
edit_58() {
    printf '%s\n' '#!/usr/bin/env bash' "cat <<\$'E\\x41F'" 'EAF' 'missing_ansi() { :; }' >ansi-end.sh
}

# edit_59: shfmt ends at the raw ANSI-C escape line, then finds fake_ansi strictly.
edit_59() {
    printf '%s\n' '#!/usr/bin/env bash' "cat <<\$'E\\x41F'" 'E\x41F' 'fake_ansi() { :; }' 'EAF' >ansi-nonending.sh
}

# edit_60: locale quotes remove their dollar prefix for the delimiter.
edit_60() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<$"EOF"' 'EOF' 'missing_locale() { :; }' >locale-end.sh
}

# edit_61: ANSI-C quote syntax inside double quotes stays literal.
edit_61() {
    printf '%s\n' '#!/usr/bin/env bash' "cat <<\"E\$'O'F\"" 'EOF' 'fake_lit() { :; }' "E\$'O'F" >literal-ansi.sh
}

# edit_62: a comment in a quoted command substitution does not open a quote.
edit_62() {
    cat >comment-sub.sh <<'FIXTURE'
#!/usr/bin/env bash
value="$(# don't open a quote
printf ok
)"
bare() { :; }
FIXTURE
}

# edit_63: a comment in a quoted backtick substitution does not open a quote.
edit_63() {
    cat >comment-tick-dq.sh <<'FIXTURE'
#!/usr/bin/env bash
value="`# don't open a quote
printf ok`"
bare() { :; }
FIXTURE
}

# edit_64: a comment in an unquoted backtick substitution does not open a quote.
edit_64() {
    cat >comment-tick-raw.sh <<'FIXTURE'
#!/usr/bin/env bash
value=`# don't open a quote
printf ok`
bare() { :; }
FIXTURE
}

# edit_65: a case pattern in a substitution does not close it.
edit_65() {
    cat >case-simple.sh <<'FIXTURE'
#!/usr/bin/env bash
value="$(case x in
  x) printf '%s\n' "can't" ;;
esac)"
bare() { :; }
FIXTURE
}

# edit_66: an alternative case pattern in a substitution does not close it.
edit_66() {
    cat >case-alt.sh <<'FIXTURE'
#!/usr/bin/env bash
value="$(case x in
  a|x) printf '%s\n' "can't" ;;
esac)"
bare() { :; }
FIXTURE
}

# edit_67: a parenthesized case pattern in a substitution does not close it.
edit_67() {
    cat >case-paren.sh <<'FIXTURE'
#!/usr/bin/env bash
value="$(case x in
  (x) printf '%s\n' "can't" ;;
esac)"
bare() { :; }
FIXTURE
}

# edit_68: a one-line case pattern in a substitution does not close it.
edit_68() {
    cat >case-one.sh <<'FIXTURE'
#!/usr/bin/env bash
value="$(case x in x) echo "it's" ;; esac)"
bare() { :; }
FIXTURE
}

# edit_69: the word case in an ordinary substitution has no pattern syntax.
edit_69() {
    cat >case-guard.sh <<'FIXTURE'
#!/usr/bin/env bash
value="$(echo case x in)"
printf '%s\n' "$value"
bare() { :; }
FIXTURE
}

# edit_70: an unquoted joined heredoc delimiter makes shfmt fail closed.
edit_70() {
    cat >join-here.sh <<'FIXTURE'
#!/usr/bin/env bash
cat <<EOF2
EO\
F2
bare() { :; }
FIXTURE
}

# edit_71: a tab-stripped joined heredoc delimiter makes shfmt fail closed.
edit_71() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<-EOF2' $'\tEO\\' $'\tF2' 'bare() { :; }' >join-tab.sh
}

# edit_72: a joined body line does not close an unquoted heredoc.
edit_72() {
    cat >join-body.sh <<'FIXTURE'
#!/usr/bin/env bash
cat <<EOF2
body\
EOF2
fake_join() { :; }
EOF2
FIXTURE
}

# edit_73: a quoted heredoc does not join a backslash-newline delimiter.
edit_73() {
    cat >join-quoted.sh <<'FIXTURE'
#!/usr/bin/env bash
cat <<'EOF2'
EO\
F2
fake_quoted() { :; }
EOF2
FIXTURE
}

# edit_74: a tab-stripped heredoc keeps the fake function in its body.
edit_74() {
    printf '%s\n' '#!/usr/bin/env bash' 'cat <<-EOF2' $'\tEO\\' $'\tF2' \
        'fake_tab() { :; }' $'\tEOF2' >join-tab-guard.sh
}

# edit_75: two backslashes leave the later bare function visible.
edit_75() {
    cat >two-slashes.sh <<'FIXTURE'
#!/usr/bin/env bash
cat <<EOF2
body\\
EOF2
bare() { :; }
FIXTURE
}

# edit_76: a subshell-bodied function is a function, including its body lines.
edit_76() {
    cat >subshell.sh <<'FIXTURE'
#!/usr/bin/env bash
# sub_doc runs in a subshell.
sub_doc() (
  :
)
sub_bare() (
  :
)
FIXTURE
}

# edit_77: a header for the first definition does not document a second on the same line.
edit_77() {
    cat >same-line.sh <<'FIXTURE'
#!/usr/bin/env bash
# a is documented.
a() { :; }; b() { :; }
FIXTURE
}

# edit_78: the syntax walk finds nested definitions inside an if clause.
edit_78() {
    cat >nested.sh <<'FIXTURE'
#!/usr/bin/env bash
if true; then
  nested_bare() { :; }
fi
FIXTURE
}

# edit_79: an attached header documents a nested definition.
edit_79() {
    cat >nested-doc.sh <<'FIXTURE'
#!/usr/bin/env bash
if true; then
  # nested_doc is documented.
  nested_doc() { :; }
fi
FIXTURE
}

# edit_80: explicit Bash dialect recognizes function syntax despite a POSIX shebang.
edit_80() {
    cat >keyword-sh.sh <<'FIXTURE'
#!/bin/sh
function kw_bare { :; }
FIXTURE
}

# edit_81: a shell change fails closed when go cannot resolve shfmt.
edit_81() {
    printf '%s\n' '#!/usr/bin/env bash' 'bare_missing_go() { :; }' >missing-go.sh
}

# edit_82: a Python-only change does not resolve shfmt when go is absent.
edit_82() {
    sed -i 's/return 1/return 4/' lib.py
}

# edit_83: a bare hash line above a header comment omits Text in shfmt JSON and still passes.
edit_83() {
    printf '%s\n' '' '#' '# bare_hash_doc is documented.' 'bare_hash_doc() { :; }' >>lib.sh
}

# edit_84: a zsh anonymous subshell function omits Name in shfmt JSON and fails closed.
edit_84() {
    printf '%s\n' '#!/usr/bin/env bash' '() ( : )' >anon-sub.sh
}

# edit_85: a documented function at shfmt's 18-bit line limit (262143) passes.
edit_85() {
    printf '\n%.0s' {1..262141} >line-max.sh
    printf '%s\n' '# line_max_doc is documented.' 'line_max_doc() { :; }' >>line-max.sh
}

# edit_86: a function past shfmt's 18-bit line limit overflows Line to 0 and fails closed.
edit_86() {
    printf '\n%.0s' {1..262143} >line-overflow.sh
    printf '%s\n' '# line_overflow_doc is documented.' 'line_overflow_doc() { :; }' >>line-overflow.sh
}

# edit_87: a documented function followed by && passes.
edit_87() {
    printf '%s\n' '# and_doc is documented.' 'and_doc() {' '    :' '} && and_doc' >and-call.sh
}

# edit_88: a documented function followed by || passes.
edit_88() {
    printf '%s\n' '# or_doc is documented.' 'or_doc() {' '    :' '} || or_doc' >or-call.sh
}

# edit_89: a documented function followed by a pipe passes.
edit_89() {
    printf '%s\n' '# pipe_doc is documented.' 'pipe_doc() {' '    :' '} | cat' >pipe-call.sh
}

# edit_90: a nested function on the header line does not inherit the outer header.
edit_90() {
    printf '%s\n' '# outer_doc is documented.' \
        'outer_doc() { inner_bare() { :; } && inner_bare; } && outer_doc' >nested-and.sh
}

# edit_91: editing the command after && does not touch the chained function.
edit_91() {
    printf '%s\n' '#!/usr/bin/env bash' 'chain_and() { :; } &&' \
        '    printf '\''%s\n'\'' old' >chain-and.sh
    git add chain-and.sh && git commit -q -m chain-and
    sed -i 's/old/new/' chain-and.sh
}

# edit_92: editing the command after || does not touch the chained function.
edit_92() {
    printf '%s\n' '#!/usr/bin/env bash' 'chain_or() { :; } ||' \
        '    printf '\''%s\n'\'' old' >chain-or.sh
    git add chain-or.sh && git commit -q -m chain-or
    sed -i 's/old/new/' chain-or.sh
}

# edit_93: editing the command after a pipe does not touch the chained function.
edit_93() {
    printf '%s\n' '#!/usr/bin/env bash' 'chain_pipe() { :; } |' '    tr a b' >chain-pipe.sh
    git add chain-pipe.sh && git commit -q -m chain-pipe
    sed -i 's/tr a b/tr a c/' chain-pipe.sh
}

# edit_94: editing the body of a chained bare function is refused.
edit_94() {
    printf '%s\n' '#!/usr/bin/env bash' 'chain_body() {' '    echo old' '} &&' \
        '    printf '\''%s\n'\'' done' >chain-body.sh
    git add chain-body.sh && git commit -q -m chain-body
    sed -i 's/echo old/echo new/' chain-body.sh
}

# edit_95: deleting a chained line after the closing brace does not touch the function.
edit_95() {
    printf '%s\n' '#!/usr/bin/env bash' 'chain_gap() {' '    :' '} &&' \
        '    printf '\''%s\n'\'' a &&' '    printf '\''%s\n'\'' b' >chain-gap.sh
    git add chain-gap.sh && git commit -q -m chain-gap
    sed -i '/ a &&$/d' chain-gap.sh
}

# edit_96: a function in the right operand of a chain is still checked.
edit_96() {
    printf '%s\n' '#!/usr/bin/env bash' '# left_fn is documented.' \
        'left_fn() { :; } &&' '    right_fn() { :; }' >chain-two.sh
}

# edit_97: a payload-only edit touches a bare function's trailing quoted heredoc.
edit_97() {
    printf '%s\n' '#!/usr/bin/env bash' '' "f() { cat; } <<'EOF' >/dev/null" \
        'payload' 'EOF' >heredoc.sh
    git add heredoc.sh && git commit -q -m heredoc
    sed -i 's/payload/changed/' heredoc.sh
}

# edit_98: a header permits an edit to a trailing quoted heredoc payload.
edit_98() {
    printf '%s\n' '#!/usr/bin/env bash' '# f is documented.' \
        "f() { cat; } <<'EOF' >/dev/null" 'payload' 'EOF' >heredoc.sh
    git add heredoc.sh && git commit -q -m heredoc
    sed -i 's/payload/changed/' heredoc.sh
}

# edit_99: a payload-only edit touches a heredoc inside a one-line block.
edit_99() {
    printf '%s\n' '#!/usr/bin/env bash' '' 'f() { cat <<A; }' \
        'payload' 'A' >heredoc.sh
    git add heredoc.sh && git commit -q -m heredoc
    sed -i 's/payload/changed/' heredoc.sh
}

# edit_100: the right operand's heredoc payload does not touch the left function.
edit_100() {
    printf '%s\n' '#!/usr/bin/env bash' '' 'g() { :; } <<A && h <<B' \
        'left payload' 'A' 'right payload' 'B' >chain-heredoc.sh
    git add chain-heredoc.sh && git commit -q -m chain-heredoc
    sed -i 's/right payload/right changed/' chain-heredoc.sh
}

# edit_101: the left operand's heredoc payload touches its bare function.
edit_101() {
    printf '%s\n' '#!/usr/bin/env bash' '' 'g() { :; } <<A && h <<B' \
        'left payload' 'A' 'right payload' 'B' >chain-heredoc.sh
    git add chain-heredoc.sh && git commit -q -m chain-heredoc
    sed -i 's/left payload/left changed/' chain-heredoc.sh
}

# edit_102: an inline block comment cannot document the nested function.
edit_102() {
    printf '%s\n' '#!/usr/bin/env bash' '# Set up the environment.' \
        'outer() { # This comment describes the outer block.' \
        '    inner() { :; }' '}' >nested-comment.sh
    git add nested-comment.sh && git commit -q -m nested-comment
    sed -i 's/inner() { :; }/inner() { true; }/' nested-comment.sh
}

# edit_103: a standalone space-indented header documents the inner function.
edit_103() {
    printf '%s\n' '#!/usr/bin/env bash' '# outer is documented.' \
        'outer() {' '    # inner is documented.' '    inner() { :; }' \
        '}' >nested-comment.sh
    git add nested-comment.sh && git commit -q -m nested-comment
    sed -i 's/inner() { :; }/inner() { true; }/' nested-comment.sh
}

# edit_104: a standalone tab-indented header documents the inner function.
edit_104() {
    printf '%s\n' '#!/usr/bin/env bash' '# outer is documented.' \
        'outer() {' $'\t# inner is documented.' $'\tinner() { :; }' \
        '}' >nested-comment.sh
    git add nested-comment.sh && git commit -q -m nested-comment
    sed -i 's/inner() { :; }/inner() { true; }/' nested-comment.sh
}

# edit_105: boundary case where the assignment owns its trailing comment.
edit_105() {
    printf '%s\n' '#!/usr/bin/env bash' 'x=1 # note' \
        'f() { :; }' >inline-comment.sh
    git add inline-comment.sh && git commit -q -m inline-comment
    sed -i 's/f() { :; }/f() { true; }/' inline-comment.sh
}

# edit_106: boundary case where the preceding command owns its trailing comment.
edit_106() {
    printf '%s\n' '#!/usr/bin/env bash' ': "ä" # note' \
        'f() { :; }' >inline-comment.sh
    git add inline-comment.sh && git commit -q -m inline-comment
    sed -i 's/f() { :; }/f() { true; }/' inline-comment.sh
}

# edit_107: an inherited Go workspace permits a documented shell edit.
edit_107() {
    printf '%s\n' 'go 1.27.2' '' 'use ./tools/go' >go.work
    printf '%s\n' '# f is documented.' 'f() { :; }' >>lib.sh
}

# edit_108: an inherited Go workspace still reports a bare shell edit.
edit_108() {
    printf '%s\n' 'go 1.27.2' '' 'use ./tools/go' >go.work
    printf '%s\n' 'f() { :; }' >>lib.sh
}

# edit_109: an if-condition comment cannot document its nested function.
edit_109() {
    printf '%s\n' '#!/usr/bin/env bash' 'if true; then # note' \
        '    f() { :; }' 'fi' >inline-comment.sh
    git add inline-comment.sh && git commit -q -m inline-comment
    sed -i 's/f() { :; }/f() { true; }/' inline-comment.sh
}

# edit_110: a multibyte if-condition comment cannot document its nested function.
edit_110() {
    printf '%s\n' '#!/usr/bin/env bash' 'if [ "ä" ]; then # note' \
        '    f() { :; }' 'fi' >inline-comment.sh
    git add inline-comment.sh && git commit -q -m inline-comment
    sed -i 's/f() { :; }/f() { true; }/' inline-comment.sh
}

# edit_111: a case-pattern comment cannot document its nested function.
edit_111() {
    printf '%s\n' '#!/usr/bin/env bash' 'case x in' 'a) # note' \
        '    f() { :; } ;;' 'esac' >inline-comment.sh
    git add inline-comment.sh && git commit -q -m inline-comment
    sed -i 's/f() { :; }/f() { true; }/' inline-comment.sh
}

# edit_112: a form feed inside a quoted value does not change shfmt line coordinates.
edit_112() {
    printf '%s\n' '#!/usr/bin/env bash' "x='$(printf '\f')'" \
        '# f is documented.' 'f() { :; }' >line-coordinate.sh
    git add line-coordinate.sh && git commit -q -m line-coordinate
    sed -i 's/f() { :; }/f() { true; }/' line-coordinate.sh
}

# edit_113: a Unicode line separator does not change shfmt line coordinates.
edit_113() {
    printf '%s\n' '#!/usr/bin/env bash' "x='$(printf '\342\200\250')'" \
        '# f is documented.' 'f() { :; }' >line-coordinate.sh
    git add line-coordinate.sh && git commit -q -m line-coordinate
    sed -i 's/f() { :; }/f() { true; }/' line-coordinate.sh
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
expect "unclosed substitutions name the parse location" 2 "deep-context.sh:2" edit_49
expect "double-quoted hyphen heredoc delimiter stays whole" 0 "ok (" edit_50
expect "backslash-quoted heredoc delimiter ends at EOF" 1 "shell function bare_after_backslash has no header comment" edit_51
expect "nested arithmetic ends before real heredoc" 0 "ok (" edit_52
expect "quoted empty heredoc word closes on blank line" 0 "ok (" edit_53
expect "closing parenthesis starts comment word" 1 "shell function bare_paren has no header comment" edit_54
expect "double-quoted backslash delimiter finds following function" 1 "shell function missing_header has no header comment" edit_55
expect "double-quoted backslash delimiter ignores early line" 0 "ok (" edit_56
expect "double-quoted dollar delimiter fails closed" 2 "double-quoted-dollar.sh:2" edit_57
expect "ANSI-C delimiter fails closed" 2 "ansi-end.sh:2" edit_58
expect "ANSI-C raw escape line exposes fake function" 1 "shell function fake_ansi has no header comment" edit_59
expect "locale delimiter finds following function" 1 "shell function missing_locale has no header comment" edit_60
expect "ANSI-C syntax in double quotes stays literal" 0 "ok (" edit_61
expect "comment in quoted substitution" 1 "shell function bare has no header comment" edit_62
expect "comment in quoted backticks" 1 "shell function bare has no header comment" edit_63
expect "comment in unquoted backticks" 1 "shell function bare has no header comment" edit_64
expect "case pattern in substitution" 1 "shell function bare has no header comment" edit_65
expect "alternative case pattern in substitution" 1 "shell function bare has no header comment" edit_66
expect "parenthesized case pattern in substitution" 1 "shell function bare has no header comment" edit_67
expect "one-line case pattern in substitution" 1 "shell function bare has no header comment" edit_68
expect "ordinary substitution containing case word" 1 "shell function bare has no header comment" edit_69
expect "joined heredoc delimiter fails closed" 2 "join-here.sh:" edit_70 main "" "here-document"
expect "tab-stripped joined delimiter fails closed" 2 "join-tab.sh:" edit_71 main "" "here-document"
expect "joined heredoc body remains hidden" 0 "ok (" edit_72
expect "quoted heredoc body remains hidden" 0 "ok (" edit_73
expect "tab-stripped heredoc body remains hidden" 0 "ok (" edit_74
expect "two backslashes leave function visible" 1 "shell function bare has no header comment" edit_75
expect "subshell body finds undocumented function" 1 "shell function sub_bare has no header comment" edit_76
expect "second function on header line is undocumented" 1 "shell function b has no header comment" edit_77
expect "nested bare function in if is checked" 1 "shell function nested_bare has no header comment" edit_78
expect "nested documented function in if passes" 0 "ok (" edit_79
expect "Bash keyword syntax under POSIX shebang" 1 "shell function kw_bare has no header comment" edit_80
only_tools="$work/only-tools"
mkdir -p "$only_tools"
ln -s "$(command -v python3)" "$only_tools/python3"
ln -s "$(command -v git)" "$only_tools/git"
expect "missing go fails closed for shell" 2 "go tool" edit_81 main "$only_tools"
expect "missing go allows Python-only change" 0 "ok (" edit_82 main "$only_tools"
outside="$work/outside-go-mod"
mkdir -p "$outside"
expect "shell check from directory outside Go module" 0 "ok (" edit_1 main "" "" "$outside"
expect "bare hash line above header passes" 0 "ok (" edit_83
expect "anonymous subshell function fails closed" 2 "anon-sub.sh:2" edit_84
expect "shell function at shfmt line limit passes" 0 "ok (" edit_85
expect "shell function past shfmt line limit fails closed" 2 "missing or overflowed" edit_86
expect "documented function followed by && passes" 0 "ok (" edit_87
expect "documented function followed by || passes" 0 "ok (" edit_88
expect "documented function followed by a pipe passes" 0 "ok (" edit_89
expect "nested function on header line does not inherit outer header" 1 "shell function inner_bare has no header comment" edit_90
expect "command after && does not touch the chained function" 0 "ok (" edit_91
expect "command after || does not touch the chained function" 0 "ok (" edit_92
expect "command after a pipe does not touch the chained function" 0 "ok (" edit_93
expect "body edit in a chained function is refused" 1 "shell function chain_body has no header comment" edit_94
expect "deletion after the closing brace does not touch the chained function" 0 "ok (" edit_95
expect "function in the right operand is still checked" 1 "shell function right_fn has no header comment" edit_96
expect "trailing quoted heredoc payload touches bare function" 1 "shell function f has no header comment" edit_97
expect "trailing quoted heredoc payload with header passes" 0 "ok (" edit_98
expect "one-line block heredoc payload touches bare function" 1 "shell function f has no header comment" edit_99
expect "right heredoc payload does not touch left function" 0 "ok (" edit_100
expect "left heredoc payload touches bare function" 1 "shell function g has no header comment" edit_101
expect "inline block comment does not document inner function" 1 "shell function inner has no header comment" edit_102
expect "space-indented inner header passes" 0 "ok (" edit_103
expect "tab-indented inner header passes" 0 "ok (" edit_104
expect "assignment inline comment is not a header" 1 "shell function f has no header comment" edit_105
expect "multibyte inline comment is not a header" 1 "shell function f has no header comment" edit_106
expect "Go workspace permits documented shell edit" 0 "ok (" edit_107 main "" "" "" workspace
expect "Go workspace still finds bare shell edit" 1 "shell function f has no header comment" edit_108 main "" "" "" workspace
expect "if-condition inline comment is not a header" 1 "shell function f has no header comment" edit_109
expect "multibyte if-condition inline comment is not a header" 1 "shell function f has no header comment" edit_110
expect "case-pattern inline comment is not a header" 1 "shell function f has no header comment" edit_111
expect "form feed keeps shfmt line coordinates" 0 "ok (" edit_112
expect "Unicode line separator keeps shfmt line coordinates" 0 "ok (" edit_113

if [ "$failed" -ne 0 ]; then
    exit 1
fi
echo "func_headers_check: all cases passed"
