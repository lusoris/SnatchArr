#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse shell and Python functions touched by a change that carry no header comment.

The local form of CodeRabbit's docstring coverage check (#960): every function whose body a
change touches, measured against the merge base with the base ref, must carry a header. Shell
functions come from the shfmt syntax tree (mvdan/sh, pinned in tools/go/go.mod); a file shfmt
cannot parse stops the check with exit 2 naming the file and line. A shell function needs a
comment directly above its definition (shellcheck directives do not count); a Python function
needs a docstring. The working tree is compared, so uncommitted edits count. Exit 0 = no finding,
1 = findings, 2 = the check could not run.
"""

from __future__ import annotations

import argparse
import ast
import json
import os
import re
import subprocess  # nosec B404  # fixed argv lists, never a shell
import sys
from pathlib import Path

GIT_TIMEOUT_SECONDS = 30
SHFMT_RESOLVE_TIMEOUT_SECONDS = 300
SHELL_PARSE_TIMEOUT_SECONDS = 60
MAX_FILE_LINES = 20000
MAX_SHELL_NODES = 500000
SHELL_SUFFIXES = (".sh", ".bash")
HUNK = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@")
C_ESCAPE = re.compile(rb"\\([0-7]{1,3}|[abtnvfr\"\\])")
ESCAPE_MAP = {
    b"a": 7,
    b"b": 8,
    b"t": 9,
    b"n": 10,
    b"v": 11,
    b"f": 12,
    b"r": 13,
    b'"': 34,
    b"\\": 92,
}


class CheckError(Exception):
    """Raised when git, shfmt, or a source file cannot be read."""


def git(root: Path, *args: str) -> str:
    """Run git in root with a deadline and return stdout, raising CheckError on failure."""
    try:
        res = subprocess.run(
            ["git", *args],
            cwd=root,
            capture_output=True,
            text=True,  # nosec B603  # fixed git argv list, no shell; option-like refs are refused before the call
            timeout=GIT_TIMEOUT_SECONDS,
            check=False,
        )
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
        out.extend(raw[last : m.start()])
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


def build_shfmt() -> None:
    """Build the pinned shfmt tool from this checker's repository once per run."""
    root = Path(__file__).resolve().parents[2]
    try:
        res = subprocess.run(
            [
                "go",
                "tool",
                f"-modfile={root / 'tools/go/go.mod'}",
                "shfmt",
                "--version",
            ],
            cwd=root,
            capture_output=True,
            text=True,  # nosec B603  # pinned go tool argv, no shell
            timeout=SHFMT_RESOLVE_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise CheckError(f"go tool -modfile=tools/go/go.mod shfmt: {exc}") from exc
    if res.returncode != 0:
        detail = res.stderr.strip() or res.stdout.strip() or f"exit {res.returncode}"
        raise CheckError(f"go tool -modfile=tools/go/go.mod shfmt: {detail}")


def parse_shell(path: str, source: str) -> dict:
    """Parse full Bash source with shfmt and fail closed on parser errors."""
    root = Path(__file__).resolve().parents[2]
    try:
        res = subprocess.run(
            [
                "go",
                "tool",
                f"-modfile={root / 'tools/go/go.mod'}",
                "shfmt",
                "--to-json",
                "--language-dialect",
                "bash",
                "--filename",
                path,
            ],
            cwd=root,
            input=source,
            capture_output=True,
            text=True,  # nosec B603  # pinned go tool argv, no shell
            timeout=SHELL_PARSE_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise CheckError(f"{path}: go tool shfmt: {exc}") from exc
    if res.returncode != 0:
        raise CheckError(
            f"{path}: go tool shfmt: {res.stderr.strip() or f'exit {res.returncode}'}"
        )
    try:
        tree = json.loads(res.stdout)
    except (ValueError, RecursionError) as exc:
        raise CheckError(f"{path}: shfmt JSON: {exc}") from exc
    if not isinstance(tree, dict) or tree.get("Type") != "File":
        raise CheckError(f"{path}: shfmt JSON: expected File node")
    return tree


def shell_line(node: dict, key: str, path: str) -> int:
    """Read a 1-based line number from a shfmt position field, failing closed on overflow."""
    pos = node.get(key)
    line = pos.get("Line", 0) if isinstance(pos, dict) else 0
    if line < 1:
        raise CheckError(f"{path}: shfmt position {key} missing or overflowed")
    return line


def shell_func_entry(
    node: dict, cmd: dict, path: str
) -> tuple[str, int, int, list[dict]]:
    """Read one FuncDecl statement node and fail closed on anonymous or overflowed nodes."""
    start = shell_line(cmd, "Pos", path)
    end = shell_line(cmd, "End", path)
    name = cmd.get("Name")
    if not isinstance(name, dict) or not name.get("Value"):
        raise CheckError(f"{path}:{start}: anonymous functions are a zsh feature")
    comments = node.get("Comments", [])
    for comment in comments:
        shell_line(comment, "Hash", path)
    return name["Value"], start, end, comments


def shell_functions(tree: dict, path: str) -> list[tuple[str, int, int, list[dict]]]:
    """Find every shell FuncDecl, including nested declarations, with statement comments."""
    found = []
    stack = [tree]
    visited = 0
    while stack:
        visited += 1
        if visited > MAX_SHELL_NODES:
            raise CheckError(
                f"{path}: shfmt syntax tree exceeds {MAX_SHELL_NODES} nodes"
            )
        node = stack.pop()
        if isinstance(node, list):
            stack.extend(node)
        elif isinstance(node, dict):
            cmd = node.get("Cmd")
            if isinstance(cmd, dict) and cmd.get("Type") == "FuncDecl":
                found.append(shell_func_entry(node, cmd, path))
            stack.extend(node.values())
    return found


def shell_header_ok(comments: list[dict], first: int) -> bool:
    """Check attached comments immediately above a function, skipping shellcheck directives."""
    by_line = {comment["Hash"]["Line"]: comment.get("Text", "") for comment in comments}
    line = first - 1
    for _ in range(len(comments) + 1):
        text = by_line.get(line)
        if text is None:
            return False
        if text.lstrip().startswith("shellcheck "):
            line -= 1
            continue
        return not text.startswith("!")
    return False


def shell_findings(
    path: str, source: str, touched: set[int], gaps: set[int]
) -> list[str]:
    """List touched shell functions in path that lack an attached header comment."""
    found = []
    for name, start, end, comments in shell_functions(parse_shell(path, source), path):
        is_touched = bool(touched.intersection(range(start, end + 1))) or any(
            start <= s < end for s in gaps
        )
        if is_touched and not shell_header_ok(comments, start):
            found.append(f"{path}:{start}: shell function {name} has no header comment")
    return found


def python_findings(
    path: str, source: str, touched: set[int], gaps: set[int]
) -> list[str]:
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
        is_touched = bool(touched.intersection(range(first, last + 1))) or any(
            first <= s < last for s in gaps
        )
        if is_touched and ast.get_docstring(node) is None:
            found.append(
                f"{path}:{node.lineno}: Python function {node.name} has no docstring"
            )
    return found


def file_findings(
    root: Path, path: str, touched: set[int], gaps: set[int]
) -> list[str]:
    """Read one changed file from the working tree and check the functions the change touched."""
    full = root / path
    if not full.is_file() or not path.endswith((*SHELL_SUFFIXES, ".py")):
        return []
    try:
        source = full.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as exc:
        raise CheckError(f"{path}: {exc}") from exc
    if path.endswith(".py"):
        return python_findings(path, source, touched, gaps)
    return shell_findings(path, source, touched, gaps)


def add_untracked(
    root: Path, touched_by_file: dict[str, set[int]], gaps_by_file: dict[str, set[int]]
) -> None:
    """Add every line of each untracked shell or Python file to the touched maps."""
    untracked = git(
        root,
        "ls-files",
        "-z",
        "--others",
        "--exclude-standard",
        "--",
        "*.sh",
        "*.bash",
        "*.py",
    )
    for path in untracked.split("\0"):
        if not path:
            continue
        full = root / path
        if not full.is_file():
            continue
        try:
            line_count = len(
                full.read_text(encoding="utf-8").splitlines()[:MAX_FILE_LINES]
            )
        except (OSError, UnicodeDecodeError) as exc:
            raise CheckError(f"{path}: {exc}") from exc
        touched_by_file.setdefault(path, set()).update(range(1, line_count + 1))
        gaps_by_file.setdefault(path, set())


def run(root: Path, base: str) -> list[str]:
    """Return every finding for the change between the merge base with base and the work tree."""
    if base.startswith("-"):
        raise CheckError(f"base ref {base} starts with '-'")
    merge_base = git(root, "merge-base", base, "HEAD").strip()
    diff = git(
        root,
        "diff",
        "-U0",
        "--no-color",
        "--no-ext-diff",
        merge_base,
        "--",
        "*.sh",
        "*.bash",
        "*.py",
    )
    touched_by_file, gaps_by_file = changed_lines(diff)
    add_untracked(root, touched_by_file, gaps_by_file)
    findings = []
    paths = sorted(touched_by_file.keys() | gaps_by_file.keys())
    shell_paths = [
        path
        for path in paths
        if path.endswith(SHELL_SUFFIXES) and (root / path).is_file()
    ]
    if shell_paths:
        build_shfmt()
    for path in paths:
        findings.extend(
            file_findings(
                root,
                path,
                touched_by_file.get(path, set()),
                gaps_by_file.get(path, set()),
            )
        )
    return findings


def main() -> int:
    """Parse arguments, run the check and print findings."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--base",
        default=os.environ.get("FUNC_HEADERS_BASE", "origin/main"),
        help="ref whose merge base with HEAD the change is measured from",
    )
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
        print(
            f"func_headers_check: {len(findings)} touched function(s) without a header",
            file=sys.stderr,
        )
        return 1
    print(
        f"func_headers_check: ok (touched shell and Python functions since {args.base} carry a header)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
