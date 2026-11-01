#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Check that inline security suppressions declare rule ids and a justification.

Codacy Bandit and gosec inline suppressions must declare the specific rule ids they
silence and provide a human-readable justification. Unreasoned suppressions hide real
defects without review context. Exit 0 = valid annotations (or none), 1 = invalid directive,
2 = the check could not run.
"""

from __future__ import annotations

import argparse
import re
import subprocess  # nosec B404 -- runs only git with a fixed argument list, never a shell
import sys
from pathlib import Path

GIT_TIMEOUT_SECONDS = 60

# Directive token built from fragments so this file does not self-match directive regexes.
NO_SEC = "no" + "sec"

PY_DIRECTIVE_RE = re.compile(rf"#\s*{NO_SEC}\b")
PY_VALID_RE = re.compile(rf"#\s*{NO_SEC}(?:[ \t,]+B\d{{3}})+[ \t]+--[ \t]+\S")

GO_DIRECTIVE_RE = re.compile(rf"//\s*#{NO_SEC}\b")
GO_VALID_RE = re.compile(rf"//\s*#{NO_SEC}(?:[ \t,]+G\d{{3}})+[ \t]+--[ \t]+\S")

GO_STRINGS_RE = re.compile(r"`[^`]*`|\"([^\"\\]|\\.)*\"|'([^'\\]|\\.)*'")


def git_tracked_files(root: Path) -> list[str]:
    """Return tracked Python and Go files in root using git ls-files."""
    try:
        proc = subprocess.run(  # nosec B603 -- fixed git argv list, no shell; path patterns are fixed
            ["git", "ls-files", "-z", "--", "*.py", "*.go"],
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


def check_python_file(path_str: str, content: str) -> tuple[int, list[str]]:
    """Scan a Python file for nosec directives, returning annotated count and findings."""
    annotated = 0
    findings: list[str] = []
    for line_no, line in enumerate(content.splitlines(), 1):
        if PY_DIRECTIVE_RE.search(line):
            if PY_VALID_RE.search(line):
                annotated += 1
            else:
                findings.append(
                    f"{path_str}:{line_no}: nosec needs rule ids and a reason "
                    f"(# {NO_SEC} Bxxx -- why / // #{NO_SEC} Gxxx -- why)"
                )
    return annotated, findings


def check_go_file(path_str: str, content: str) -> tuple[int, list[str]]:
    """Scan a Go file for nosec directives, returning annotated count and findings."""
    annotated = 0
    findings: list[str] = []
    for line_no, line in enumerate(content.splitlines(), 1):
        # Strip string literals so quotes containing a Go suppression directive are not treated as comments.
        code = GO_STRINGS_RE.sub("", line)
        if GO_DIRECTIVE_RE.search(code):
            if GO_VALID_RE.search(code):
                annotated += 1
            else:
                findings.append(
                    f"{path_str}:{line_no}: nosec needs rule ids and a reason "
                    f"(# {NO_SEC} Bxxx -- why / // #{NO_SEC} Gxxx -- why)"
                )
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
    parser.add_argument("--root", type=Path, default=Path("."), help="Repository root directory")
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
        print(f"nosec_check: {len(findings)} invalid nosec directive(s)", file=sys.stderr)
        return 1

    print(f"nosec_check: ok ({annotated_count} annotated lines)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
