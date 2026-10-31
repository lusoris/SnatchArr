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
import subprocess
import sys
from pathlib import Path

GIT_TIMEOUT_SECONDS = 30
MAX_FILE_LINES = 20000
SHELL_SUFFIXES = (".sh", ".bash")
SHELL_DEF = re.compile(
    r"^(\s*)(?:function\s+)?([A-Za-z_][A-Za-z0-9_:.-]*)\s*\(\)\s*(?:(\{.*\})(?:\s*$|\s+#.*)|\{?(?:\s*$|\s+#.*))|"
    r"^(\s*)function\s+([A-Za-z_][A-Za-z0-9_:.-]*)\s*(?:(\{.*\})(?:\s*$|\s+#.*)|\{?(?:\s*$|\s+#.*))"
)
HEREDOC_START = re.compile(r"(?<!<)<<(?!<)(-?)\s*['\"]?([A-Za-z_][A-Za-z0-9_]*)['\"]?")
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
        res = subprocess.run(["git", *args], cwd=root, capture_output=True, text=True,
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
            target = line[4:]
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


def shell_function_end(lines: list[str], idx: int, indent: str) -> int:
    """Find the closing line index (0-based) for a multiline shell function starting at idx."""
    heredoc: str | None = None
    heredoc_dash = False
    for j in range(idx + 1, min(len(lines), idx + 1 + MAX_FILE_LINES)):
        line = lines[j]
        if heredoc is not None:
            target = line.lstrip("\t") if heredoc_dash else line
            if target == heredoc:
                heredoc = None
            continue
        m_hero = HEREDOC_START.search(line)
        if m_hero:
            heredoc_dash = bool(m_hero.group(1))
            heredoc = m_hero.group(2)
            continue
        r = line.rstrip()
        if r in (f"{indent}}}", f"{indent}}};") or line.startswith((f"{indent}}} ", f"{indent}}}; ")):
            return j
    return idx


def shell_functions(lines: list[str]) -> list[tuple[str, int, int]]:
    """Return (name, first line, last line), 1-based, for each shell function definition."""
    funcs = []
    for idx, text in enumerate(lines):
        m = SHELL_DEF.match(text)
        if not m:
            continue
        indent = m.group(1) if m.group(1) is not None else m.group(4)
        name = m.group(2) or m.group(5)
        is_one_line = bool(m.group(3) or m.group(6))
        end = idx if is_one_line else shell_function_end(lines, idx, indent)
        funcs.append((name, idx + 1, end + 1))
    return funcs


def shell_findings(path: str, lines: list[str], touched: set[int], gaps: set[int]) -> list[str]:
    """List touched shell functions in path that lack a header comment."""
    found = []
    for name, start, end in shell_functions(lines):
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
