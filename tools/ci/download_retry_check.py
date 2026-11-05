#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Require retries on curl downloads in tracked shell scripts.

Ignore tools/**/testdata/**, /dev/null polls and command-substitution captures.
Exit 0 = clean, 1 = downloads missing retries, 2 = the check could not run.
"""

from __future__ import annotations

import argparse
import re
import shlex
import subprocess  # nosec B404 -- runs only git with fixed arguments, never a shell
import sys
from collections.abc import Iterator
from pathlib import Path

GIT_TIMEOUT_SECONDS = 60
COMMAND_START = re.compile(r"(?:^|[;&|])\s*(?:(?:if|elif|while|until|then|do|!|command)\s+)*curl\b")
COMMAND_PREFIXES = {"if", "elif", "while", "until", "then", "do", "!", "command"}
COMMAND_SEPARATORS = {";", "&&", "||", "|", "&"}


def git_tracked_files(root: Path) -> list[str]:
    """Return tracked shell paths; fail closed if git cannot list them."""
    try:
        proc = subprocess.run(  # nosec B603 -- fixed git argv, no shell; fixed path pattern
            ["git", "ls-files", "-z", "--", "*.sh"],
            cwd=root,
            capture_output=True,
            timeout=GIT_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise RuntimeError(f"git ls-files failed: {exc}") from exc
    if proc.returncode != 0:
        error = proc.stderr.decode("utf-8", errors="replace").strip()
        raise RuntimeError(f"git ls-files failed (rc {proc.returncode}): {error}")
    return [part.decode("utf-8") for part in proc.stdout.split(b"\0") if part]


def logical_lines(content: str) -> Iterator[tuple[int, str]]:
    """Join backslash continuations, retaining the first physical line number."""
    pending = ""
    start = 1
    for line_no, line in enumerate(content.splitlines(), 1):
        if not pending:
            start = line_no
        if line.endswith("\\"):
            pending += line[:-1]
            continue
        yield start, pending + line
        pending = ""
    if pending:
        yield start, pending


def curl_commands(line: str) -> Iterator[list[str]]:
    """Tokenize standalone curl calls without treating quoted fixture text as code."""
    if not COMMAND_START.search(line):
        return
    lexer = shlex.shlex(line, posix=True, punctuation_chars=";&|()")
    lexer.whitespace_split = True
    command: list[str] = []
    for token in [*lexer, ";"]:
        if token in COMMAND_SEPARATORS:
            if command and command[0] == "curl":
                yield command[1:]
            command = []
        elif not command and token in COMMAND_PREFIXES:
            continue
        else:
            command.append(token)


def is_download(arguments: list[str]) -> bool:
    """Identify file outputs with an HTTPS or variable URL, excluding stdout polls."""
    output = False
    urls: list[str] = []
    tokens = iter(arguments)
    for token in tokens:
        if token in {"--output", "-o"}:
            target = next(tokens, None)
            if target is None:
                raise ValueError("curl output option missing filename")
            output |= target not in {"/dev/null", "-"}
        elif token.startswith("--output="):
            output |= token.partition("=")[2] not in {"/dev/null", "-"}
        elif token.startswith("-o"):
            output |= token[2:] not in {"/dev/null", "-"}
        elif token in {"--remote-name", "-O"}:
            output = True
        else:
            urls.append(token)
    return output and any(url.startswith(("https://", "$")) for url in urls)


def scan_repository(root: Path) -> tuple[int, list[str]]:
    """Scan tracked scripts, skipping fixture directories, and collect retry findings."""
    downloads = 0
    findings: list[str] = []
    for rel_path in git_tracked_files(root):
        path = Path(rel_path)
        if path.parts[0] == "tools" and "testdata" in path.parts[1:-1]:
            continue
        content = (root / path).read_text(encoding="utf-8")
        for line_no, line in logical_lines(content):
            for arguments in curl_commands(line):
                if is_download(arguments):
                    downloads += 1
                    if not any(arg == "--retry" or arg.startswith("--retry=") for arg in arguments):
                        findings.append(f"{rel_path}:{line_no}: curl download without --retry")
    return downloads, findings


def main() -> int:
    """Run the download retry check and report findings or an operational error."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path("."), help="Repository root directory")
    args = parser.parse_args()
    try:
        downloads, findings = scan_repository(args.root.resolve())
    except (RuntimeError, OSError, ValueError) as exc:
        print(f"download_retry_check: cannot run: {exc}", file=sys.stderr)
        return 2
    if findings:
        for finding in findings:
            print(finding, file=sys.stderr)
        print(f"download_retry_check: {len(findings)} download(s) missing --retry", file=sys.stderr)
        return 1
    print(f"download_retry_check: ok ({downloads} downloads)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
