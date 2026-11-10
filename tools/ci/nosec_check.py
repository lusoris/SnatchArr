#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Check that inline security suppressions declare rule ids and a justification.

Python: bandit 1.9.4 consumes the first directive in a COMMENT token up to the
next ``#``, so validate that directive and keep reasons behind a second hash.
Go: gosec v2.30.0 consumes the first line-start tag in line or block comment
groups up to ``--``, so validate the first directive and require a line comment.

Codacy Bandit and gosec inline suppressions must declare the specific rule ids they
silence and provide a human-readable justification. Unreasoned suppressions hide real
defects without review context. Exit 0 = valid annotations (or none), 1 = invalid directive,
2 = the check could not run.
"""

from __future__ import annotations

import argparse
import io
import re
import shutil
import subprocess  # nosec B404  # runs only git with a fixed argument list, never a shell
import sys
import tokenize
from collections.abc import Iterator
from pathlib import Path

GIT_TIMEOUT_SECONDS = 60

# Directive token built from fragments so this file does not self-match directive regexes.
NO_SEC = "no" + "sec"

PY_DIRECTIVE_RE = re.compile(rf"#\s*{NO_SEC}")
PY_VALID_RE = re.compile(
    rf"#\s*{NO_SEC}[ \t]+B[0-9]{{3}}(?:[ \t,]+B[0-9]{{3}})*[ \t]+#[ \t]+\S"
)
PY_DASH_RE = re.compile(rf"#\s*{NO_SEC}(?:[ \t,]+B\d{{3}})+[ \t]+--")

GO_DIRECTIVE_RE = re.compile(rf"#{NO_SEC}")
GO_VALID_RE = re.compile(
    rf"//[ \t]*#{NO_SEC}[ \t]+G[0-9]{{3}}(?:[ \t,]+G[0-9]{{3}})*[ \t]+--[ \t]+\S"
)
GO_SECOND_SLASH_RE = re.compile(rf"//\s*#{NO_SEC}(?:[ \t,]+G\d{{3}})+[ \t]+//")


def git_tracked_files(root: Path) -> list[str]:
    """Return tracked Python and Go files in root using git ls-files."""
    git = shutil.which("git")
    if git is None:
        raise RuntimeError("git ls-files failed: git executable not found")
    try:
        proc = subprocess.run(  # nosec B603  # fixed git argv list, no shell; path patterns are fixed
            [git, "ls-files", "-z", "--", "*.py", "*.go"],
            cwd=root,
            capture_output=True,
            timeout=GIT_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise RuntimeError(f"git ls-files failed: {exc}") from exc

    if proc.returncode != 0:
        err = proc.stderr.decode("utf-8", errors="replace").strip()
        raise RuntimeError(f"git ls-files failed (rc {proc.returncode}): {err}")

    raw = proc.stdout
    if not raw:
        return []
    parts = raw.split(b"\x00")
    files: list[str] = []
    for part in parts:
        if part:
            files.append(part.decode("utf-8", errors="replace"))
    return files


def check_python_comment(comment: str) -> tuple[int, list[str]]:
    """Validate the first Bandit directive and reject additional directives."""
    first = PY_DIRECTIVE_RE.search(comment)
    if first is None:
        return 0, []
    directive = comment[first.start() :]
    findings: list[str] = []
    if not PY_VALID_RE.match(directive):
        if PY_DASH_RE.match(directive):
            findings.append(
                f"nosec reason after -- widens the suppression in bandit; write # {NO_SEC} Bxxx  # why"
            )
        else:
            findings.append(
                f"nosec needs rule ids and a reason (# {NO_SEC} Bxxx  # why / // #{NO_SEC} Gxxx -- why)"
            )
    if PY_DIRECTIVE_RE.search(comment, first.end()):
        findings.append(
            f"one nosec directive per comment; write # {NO_SEC} Bxxx  # why"
        )
    return int(not findings), findings


def check_python_file(path_str: str, content: str) -> tuple[int, list[str]]:
    """Scan only Python comment tokens, failing closed on tokenization errors."""
    annotated = 0
    findings: list[str] = []
    try:
        for token in tokenize.generate_tokens(io.StringIO(content).readline):
            if token.type != tokenize.COMMENT:
                continue
            count, errors = check_python_comment(token.string)
            annotated += count
            findings.extend(f"{path_str}:{token.start[0]}: {error}" for error in errors)
    except tokenize.TokenError as exc:
        findings.append(f"{path_str}:{exc.args[1][0]}: cannot tokenize: {exc.args[0]}")
    except SyntaxError as exc:
        findings.append(f"{path_str}:{exc.lineno or 1}: cannot tokenize: {exc.msg}")
    return annotated, findings


def go_literal_end(content: str, start: int) -> int:
    """Skip a Go quoted string, rune, or raw string, including escaped quotes."""
    quote = content[start]
    escaped = False
    for index in range(start + 1, len(content)):
        char = content[index]
        if quote != "`" and not escaped and char == "\\":
            escaped = True
        elif not escaped and char == quote:
            return index + 1
        else:
            escaped = False
    return len(content)


def go_comments(content: str) -> Iterator[tuple[int, str, str]]:
    """Yield Go line and block comments while advancing past every literal."""
    index, line = 0, 1
    while index < len(content):
        if content[index] in "\"'`":
            end = go_literal_end(content, index)
        elif content.startswith("//", index):
            stop = content.find("\n", index + 2)
            end = len(content) if stop < 0 else stop
            yield line, "line", content[index:end]
        elif content.startswith("/*", index):
            stop = content.find("*/", index + 2)
            end = len(content) if stop < 0 else stop + 2
            yield line, "block", content[index:end]
        else:
            end = index + 1
        line += content.count("\n", index, end)
        index = end


def check_go_comment(kind: str, comment: str) -> tuple[int, list[str]]:
    """Require a single scoped directive at the start of a Go line comment."""
    first = GO_DIRECTIVE_RE.search(comment)
    if first is None:
        return 0, []
    # Gosec ignores a tag preceded by prose on the same comment-text line.
    if kind == "line" and comment[2 : first.start()].strip():
        return 0, []
    findings: list[str] = []
    if kind == "block":
        findings.append(f"#{NO_SEC} in a block comment; write // #{NO_SEC} Gxxx -- why")
    elif GO_SECOND_SLASH_RE.match(comment):
        findings.append(
            f"second // after #{NO_SEC} widens the suppression in gosec; write // #{NO_SEC} Gxxx -- why"
        )
    elif not GO_VALID_RE.match(comment):
        findings.append(
            f"nosec needs rule ids and a reason (# {NO_SEC} Bxxx  # why / // #{NO_SEC} Gxxx -- why)"
        )
    if GO_DIRECTIVE_RE.search(comment, first.end()):
        findings.append("one nosec directive per comment")
    return int(not findings), findings


def check_go_file(path_str: str, content: str) -> tuple[int, list[str]]:
    """Scan actual Go comments for nosec directives, returning count and findings."""
    annotated = 0
    findings: list[str] = []
    for line_no, kind, comment in go_comments(content):
        count, errors = check_go_comment(kind, comment)
        annotated += count
        findings.extend(f"{path_str}:{line_no}: {error}" for error in errors)
    return annotated, findings


def scan_repository(root: Path) -> tuple[int, list[str]]:
    """Scan all tracked Python and Go files in root for nosec directives."""
    file_list = git_tracked_files(root)
    total_annotated = 0
    all_findings: list[str] = []

    for rel_path in file_list:
        file_path = root / rel_path
        if not file_path.is_file():
            continue
        content = file_path.read_text(encoding="utf-8", errors="replace")
        if rel_path.endswith(".py"):
            annotated, findings = check_python_file(rel_path, content)
        elif rel_path.endswith(".go"):
            annotated, findings = check_go_file(rel_path, content)
        else:
            continue

        total_annotated += annotated
        all_findings.extend(findings)

    return total_annotated, all_findings


def main() -> int:
    """Run the nosec check across tracked files and report results."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--root", type=Path, default=Path("."), help="Repository root directory"
    )
    args = parser.parse_args()

    root = args.root.resolve()
    try:
        annotated_count, findings = scan_repository(root)
    except RuntimeError as exc:
        print(f"nosec_check: cannot run: {exc}", file=sys.stderr)
        return 2

    if findings:
        for finding in findings:
            print(finding, file=sys.stderr)
        print(
            f"nosec_check: {len(findings)} invalid nosec directive(s)", file=sys.stderr
        )
        return 1

    print(f"nosec_check: ok ({annotated_count} annotated lines)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
