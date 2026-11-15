#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse shell and Python functions touched by a change that carry no header comment.

The local form of CodeRabbit's docstring coverage check (#960): every function whose body a
change touches, measured against the merge base with the base ref, must carry a header. A shell
function needs a comment line directly above its definition (shellcheck directives do not
count); a Python function needs a docstring. The working tree is compared, so uncommitted edits
count. Exit 0 = no finding, 1 = findings, 2 = the check could not run.
"""

from __future__ import annotations

import argparse
import ast
import os
import re
import subprocess  # nosec B404  # runs only git with a fixed argument list, never a shell
import sys
from pathlib import Path

GIT_TIMEOUT_SECONDS = 30
MAX_FILE_LINES = 20000
MAX_SHELL_CONTEXTS = 32
SHELL_SUFFIXES = (".sh", ".bash")
SHELL_DEF = re.compile(
    r"^(\s*)(?:function\s+)?([A-Za-z_][A-Za-z0-9_:.-]*)\s*\(\)\s*(?:(\{.*\})(?:\s*$|\s+#.*)|\{?(?:\s*$|\s+#.*))|"
    r"^(\s*)function\s+([A-Za-z_][A-Za-z0-9_:.-]*)\s*(?:(\{.*\})(?:\s*$|\s+#.*)|\{?(?:\s*$|\s+#.*))"
)
HEREDOC_OPERATOR = re.compile(r"(?<!<)<<(?!<)(-?)")
HUNK = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@")
C_ESCAPE = re.compile(rb"\\([0-7]{1,3}|[abtnvfr\"\\])")
ESCAPE_MAP = {
    b"a": 7, b"b": 8, b"t": 9, b"n": 10, b"v": 11, b"f": 12, b"r": 13,
    b"\"": 34, b"\\": 92,
}


class CheckError(Exception):
    """Raised when git cannot answer or a file cannot be read."""


def git(root: Path, *args: str) -> str:
    """Run git in root with a deadline and return stdout, raising CheckError on failure."""
    try:
        res = subprocess.run(["git", *args], cwd=root, capture_output=True, text=True,  # nosec B603  # fixed git argv list, no shell; option-like refs are refused before the call
                             timeout=GIT_TIMEOUT_SECONDS, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise CheckError(f"git {' '.join(args)}: {exc}") from exc
    if res.returncode != 0:
        raise CheckError(f"git {' '.join(args)}: {res.stderr.strip()}")
    return res.stdout


def unquote_git_path(text: str) -> str:
    """Decode a C-quoted git path containing octal and backslash escapes to UTF-8."""
    raw = text.encode("utf-8")
    out = bytearray()
    last = 0
    for m in C_ESCAPE.finditer(raw):
        out.extend(raw[last:m.start()])
        esc = m.group(1)
        if b"0" <= esc[0:1] <= b"7":
            out.append(int(esc, 8) & 0xFF)
        else:
            out.append(ESCAPE_MAP[esc])
        last = m.end()
    out.extend(raw[last:])
    return out.decode("utf-8", errors="surrogateescape")


def changed_lines(diff: str) -> tuple[dict[str, set[int]], dict[str, set[int]]]:
    """Map each file in a -U0 diff to added line numbers and pure-deletion gap positions."""
    touched: dict[str, set[int]] = {}
    gaps: dict[str, set[int]] = {}
    current = None
    for line in diff.splitlines():
        if line.startswith("+++ "):
            target = line[4:].rstrip("\t")
            if target.startswith('"b/') and target.endswith('"'):
                current = unquote_git_path(target[3:-1])
            elif target.startswith("b/"):
                current = target[2:]
            else:
                current = None
            if current is not None:
                touched.setdefault(current, set())
                gaps.setdefault(current, set())
            continue
        m = HUNK.match(line)
        if m and current is not None:
            start, count = int(m.group(1)), int(m.group(2) or "1")
            if count:
                touched[current].update(range(start, start + count))
            else:
                gaps[current].add(start)
    return touched, gaps


def shell_header_ok(lines: list[str], def_idx: int) -> bool:
    """Report whether a comment other than a shellcheck directive sits directly above def_idx."""
    i = def_idx - 1
    while i >= 0 and lines[i].strip().startswith("# shellcheck "):
        i -= 1
    return i >= 0 and lines[i].lstrip().startswith("#") and not lines[i].startswith("#!")


def push_shell_context(contexts: list[str], kind: str, path: str, line_no: int) -> None:
    """Push one bounded shell context or report the source location."""
    if len(contexts) >= MAX_SHELL_CONTEXTS:
        raise CheckError(f"{path}:{line_no}: shell context depth exceeds {MAX_SHELL_CONTEXTS}")
    contexts.append(kind)


def shell_quote_step(line: str, i: int, contexts: list[str], path: str, line_no: int) -> int:
    """Consume one quoted span character, opening shell code inside double quotes."""
    kind = contexts[-1]
    char = line[i]
    if kind in ("ansi", '"') and char == "\\" and i + 1 < len(line):
        return 2
    if (char == "'" and kind in ("'", "ansi")) or (char == '"' and kind == '"'):
        contexts.pop()
    elif kind == '"' and line.startswith("$(", i):
        width = 3 if line.startswith("$((", i) else 2
        push_shell_context(contexts, "arith" if width == 3 else "sub", path, line_no)
        return width
    elif kind == '"' and char == "`":
        push_shell_context(contexts, "backtick", path, line_no)
    return 1


def shell_code_step(line: str, i: int, contexts: list[str], path: str, line_no: int,
                    word_start: bool) -> tuple[int, bool, str]:
    """Consume shell code, returning width, next word boundary, and visible code."""
    char = line[i]
    masked = "arith" in contexts
    if char == "\\" and i + 1 < len(line):
        return 2, False, "  "
    if contexts and contexts[-1] == "arith" and line.startswith("))", i):
        contexts.pop()
        return 2, True, "  "
    if line.startswith("$((", i) or (word_start and not masked and line.startswith("((", i)):
        width = 3 if char == "$" else 2
        push_shell_context(contexts, "arith", path, line_no)
        return width, False, " " * width
    if line.startswith("$(", i):
        push_shell_context(contexts, "sub", path, line_no)
        return 2, True, "  "
    if line.startswith(("$'", '$"'), i):
        push_shell_context(contexts, "ansi" if line[i + 1] == "'" else '"', path, line_no)
        return 2, False, "  "
    if char in ("'", '"', "`"):
        push_shell_context(contexts, "backtick" if char == "`" else char, path, line_no)
        return 1, False, " "
    if char == "(":
        push_shell_context(contexts, "group", path, line_no)
    elif char == ")" and contexts and contexts[-1] in ("group", "sub"):
        contexts.pop()
    return 1, char.isspace() or char in ";&|()", " " if masked else char


def shell_line_state(line: str, contexts: list[str], path: str, line_no: int) -> str:
    """Return unquoted shell code while updating bounded contexts across lines."""
    code = []
    word_start = True
    i = 0
    while i < len(line):
        if contexts and contexts[-1] in ("'", "ansi", '"'):
            width = shell_quote_step(line, i, contexts, path, line_no)
            code.append(" " * width)
        elif contexts and contexts[-1] == "backtick" and line[i] == "`":
            contexts.pop()
            width = 1
            code.append(" ")
        elif line[i] == "#" and word_start:
            code.append(" " * (len(line) - i))
            break
        else:
            width, word_start, visible = shell_code_step(line, i, contexts, path, line_no, word_start)
            code.append(visible)
        i += width
    return "".join(code)


def heredoc_end(line: str, delimiter: str, strip_tabs: bool) -> bool:
    """Return whether line closes a heredoc, applying <<- tab stripping."""
    target = line.lstrip("\t") if strip_tabs else line
    return target == delimiter


def heredoc_word(line: str, start: int) -> str | None:
    """Read the complete shell heredoc word after an operator with quote removal."""
    delimiter = []
    quote = None
    seen = False
    i = start
    while i < len(line) and line[i].isspace():
        i += 1
    while i < len(line):
        char = line[i]
        if quote is None and (char.isspace() or char in ";&|()<>"):
            break
        seen = True
        if char == quote:
            quote = None
        elif char in ("'", '"') and quote is None:
            quote = char
        elif char == "\\" and quote != "'" and i + 1 < len(line):
            i += 1
            delimiter.append(line[i])
        else:
            delimiter.append(char)
        i += 1
    return "".join(delimiter) if seen and quote is None else None


def shell_heredoc(line: str, code: str) -> tuple[bool, str] | None:
    """Find a heredoc opener in visible shell code and read its full delimiter."""
    operator = HEREDOC_OPERATOR.search(code)
    if operator is None:
        return None
    delimiter = heredoc_word(line, operator.end())
    return (bool(operator.group(1)), delimiter) if delimiter is not None else None


def shell_function_end(lines: list[str], idx: int, indent: str, path: str) -> int:
    """Find the closing line index (0-based) for a multiline shell function starting at idx."""
    heredoc: str | None = None
    heredoc_dash = False
    contexts: list[str] = []
    for j in range(idx + 1, min(len(lines), idx + 1 + MAX_FILE_LINES)):
        line = lines[j]
        if heredoc is not None:
            if heredoc_end(line, heredoc, heredoc_dash):
                heredoc = None
            continue
        was_quoted = bool(contexts and contexts[-1] in ("'", "ansi", '"'))
        code = shell_line_state(line, contexts, path, j + 1)
        m_hero = shell_heredoc(line, code)
        if m_hero:
            heredoc_dash, heredoc = m_hero
            continue
        if was_quoted or (contexts and contexts[-1] in ("'", "ansi", '"')):
            continue
        r = line.rstrip()
        if r in (f"{indent}}}", f"{indent}}};") or line.startswith((f"{indent}}} ", f"{indent}}}; ")):
            return j
    return idx


def shell_functions(lines: list[str], path: str) -> list[tuple[str, int, int]]:
    """Return (name, first line, last line), 1-based, for each shell function definition."""
    funcs = []
    heredoc: str | None = None
    heredoc_dash = False
    contexts: list[str] = []
    for idx, text in enumerate(lines):
        if heredoc is not None:
            if heredoc_end(text, heredoc, heredoc_dash):
                heredoc = None
            continue
        was_quoted = bool(contexts and contexts[-1] in ("'", "ansi", '"'))
        code = shell_line_state(text, contexts, path, idx + 1)
        if not was_quoted:
            m = SHELL_DEF.match(text)
            if m:
                indent = m.group(1) if m.group(1) is not None else m.group(4)
                name = m.group(2) or m.group(5)
                is_one_line = bool(m.group(3) or m.group(6))
                end = idx if is_one_line else shell_function_end(lines, idx, indent, path)
                funcs.append((name, idx + 1, end + 1))
        m_hero = shell_heredoc(text, code)
        if m_hero:
            heredoc_dash, heredoc = m_hero
    return funcs


def shell_findings(path: str, lines: list[str], touched: set[int], gaps: set[int]) -> list[str]:
    """List touched shell functions in path that lack a header comment."""
    found = []
    for name, start, end in shell_functions(lines, path):
        is_touched = (
            bool(touched.intersection(range(start, end + 1)))
            or any(start <= s < end for s in gaps)
        )
        if is_touched and not shell_header_ok(lines, start - 1):
            found.append(f"{path}:{start}: shell function {name} has no header comment")
    return found


def python_findings(path: str, source: str, touched: set[int], gaps: set[int]) -> list[str]:
    """List touched Python functions in path that lack a docstring."""
    try:
        tree = ast.parse(source, filename=path)
    except SyntaxError as exc:
        raise CheckError(f"{path}: {exc}") from exc
    found = []
    for node in ast.walk(tree):
        if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            continue
        first = min([node.lineno, *(d.lineno for d in node.decorator_list)])
        last = node.end_lineno or node.lineno
        is_touched = (
            bool(touched.intersection(range(first, last + 1)))
            or any(first <= s < last for s in gaps)
        )
        if is_touched and ast.get_docstring(node) is None:
            found.append(f"{path}:{node.lineno}: Python function {node.name} has no docstring")
    return found


def file_findings(root: Path, path: str, touched: set[int], gaps: set[int]) -> list[str]:
    """Read one changed file from the working tree and check the functions the change touched."""
    full = root / path
    if not full.is_file() or not path.endswith((*SHELL_SUFFIXES, ".py")):
        return []
    try:
        source = full.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as exc:
        raise CheckError(f"{path}: {exc}") from exc
    lines = source.splitlines()[:MAX_FILE_LINES]
    if path.endswith(".py"):
        return python_findings(path, source, touched, gaps)
    return shell_findings(path, lines, touched, gaps)


def run(root: Path, base: str) -> list[str]:
    """Return every finding for the change between the merge base with base and the work tree."""
    if base.startswith("-"):
        raise CheckError(f"base ref {base} starts with '-'")
    merge_base = git(root, "merge-base", base, "HEAD").strip()
    diff = git(root, "diff", "-U0", "--no-color", "--no-ext-diff", merge_base, "--",
               "*.sh", "*.bash", "*.py")
    touched_by_file, gaps_by_file = changed_lines(diff)
    untracked = git(root, "ls-files", "-z", "--others", "--exclude-standard", "--",
                    "*.sh", "*.bash", "*.py")
    for path in untracked.split("\0"):
        if not path:
            continue
        full = root / path
        if not full.is_file():
            continue
        try:
            line_count = len(full.read_text(encoding="utf-8").splitlines()[:MAX_FILE_LINES])
        except (OSError, UnicodeDecodeError) as exc:
            raise CheckError(f"{path}: {exc}") from exc
        touched_by_file.setdefault(path, set()).update(range(1, line_count + 1))
        gaps_by_file.setdefault(path, set())
    findings = []
    for path in sorted(touched_by_file.keys() | gaps_by_file.keys()):
        findings.extend(file_findings(root, path, touched_by_file.get(path, set()),
                                      gaps_by_file.get(path, set())))
    return findings


def main() -> int:
    """Parse arguments, run the check and print findings."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--base", default=os.environ.get("FUNC_HEADERS_BASE", "origin/main"),
                        help="ref whose merge base with HEAD the change is measured from")
    parser.add_argument("--root", default=".", help="repository root")
    args = parser.parse_args()
    try:
        findings = run(Path(args.root), args.base)
    except CheckError as exc:
        print(f"func_headers_check: cannot run: {exc}", file=sys.stderr)
        return 2
    for f in findings:
        print(f)
    if findings:
        print(f"func_headers_check: {len(findings)} touched function(s) without a header", file=sys.stderr)
        return 1
    print(f"func_headers_check: ok (touched shell and Python functions since {args.base} carry a header)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
