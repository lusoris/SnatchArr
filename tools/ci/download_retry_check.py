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
import subprocess  # nosec B404  # runs only git with fixed arguments, never a shell
import sys
from collections.abc import Iterator
from pathlib import Path

GIT_TIMEOUT_SECONDS = 60
COMMAND_START = re.compile(r"\bcurl\b")
COMMAND_WRAPPERS = {"env", "sudo", "exec", "time", "nice", "command"}
COMMAND_PREFIXES = {"if", "elif", "while", "until", "then", "do", "!"}
COMMAND_SEPARATORS = {";", "&&", "||", "|", "&"}
ASSIGNMENT = re.compile(r"[A-Za-z_][A-Za-z0-9_]*=")
REDIRECT = re.compile(r"(?:[0-9]+|&)?>>?")
STDOUT_REDIRECTS = {">", ">>", "1>", "1>>", "&>", "&>>"}
# curl 8.23.0: value-taking short options derived from `curl --help all`.
SHORT_VALUE_OPTIONS = frozenset("ACDEFHKPQTUXYbcdehmortuwxyz")


def git_tracked_files(root: Path) -> list[str]:
    """Return tracked shell paths; fail closed if git cannot list them."""
    try:
        proc = subprocess.run(  # nosec B603  # fixed git argv, no shell; fixed path pattern
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


def strip_comment(line: str) -> str:
    """Remove only unquoted, unescaped comments at the start of shell words."""
    quote = ""
    escaped = False
    word_start = True
    for index, char in enumerate(line):
        if escaped:
            escaped = False
            continue
        if char == "\\" and quote != "'":
            escaped = True
            word_start = False
            continue
        if quote:
            if char == quote:
                quote = ""
            continue
        if char in {"'", '"'}:
            quote = char
        elif char == "#" and word_start:
            return line[:index]
        word_start = char in " \t\r\n;&|()<>"
    return line


def shell_words(line: str) -> Iterator[str]:
    """Preserve redirect operators and fragments after stripping shell comments."""
    line = strip_comment(line)
    lexer = shlex.shlex(line, posix=True, punctuation_chars=";&|()>")
    lexer.whitespace_split = True
    lexer.commenters = ""
    for word in lexer:
        # shlex reads one character ahead: only adjacent digits name a descriptor.
        position = lexer.instream.tell()
        if re.fullmatch(r"[0-9]+", word) and line[position - 1:position] == ">":
            word += next(lexer)
        yield word


def curl_arguments(command: list[str]) -> list[str] | None:
    """Skip shell control words, assignments and command wrappers before curl.

    After a wrapper word, its own options and their values (`sudo -u root`,
    `nice -n 10`) come before the wrapped command, so the first `curl` word wins.
    """
    wrapped = False
    for index, word in enumerate(command):
        if Path(word).name == "curl":
            return command[index + 1:]
        if word in COMMAND_WRAPPERS:
            wrapped = True
        elif not (wrapped or word in COMMAND_PREFIXES or ASSIGNMENT.match(word)):
            return None
    return None


def curl_operations(arguments: list[str]) -> Iterator[list[str]]:
    """Separate curl operations whose local options reset at --next or -:."""
    operation: list[str] = []
    for word in arguments:
        if word in {"--next", "-:"}:
            yield operation
            operation = []
        else:
            operation.append(word)
    yield operation


def curl_commands(line: str) -> Iterator[list[str]]:
    """Tokenize standalone curl calls without treating quoted fixture text as code."""
    if not COMMAND_START.search(line):
        return
    command: list[str] = []
    for word in [*shell_words(line), ";"]:
        if word in COMMAND_SEPARATORS:
            arguments = curl_arguments(command)
            if arguments is not None:
                yield from curl_operations(arguments)
            command = []
        else:
            command.append(word)


def short_options(word: str, words: Iterator[str]) -> Iterator[str]:
    """Expand output flags, ending clusters at the first value-taking option."""
    for index, option in enumerate(word[1:], 2):
        if option == "O":
            yield "--remote-name"
        elif option in SHORT_VALUE_OPTIONS:
            value = word[index:] or next(words, None)
            if value is None:
                raise ValueError("curl short option missing value")
            if option == "o":
                yield "--output"
                yield value
            return


def output_arguments(arguments: list[str]) -> Iterator[str]:
    """Normalize short output options without inspecting config file contents."""
    words = iter(arguments)
    for word in words:
        if word.startswith("-") and not word.startswith("--") and word != "-":
            yield from short_options(word, words)
        elif word == "--config":
            next(words, None)
        else:
            yield word
            if word == "--output" or REDIRECT.fullmatch(word):
                value = next(words, None)
                if value is None:
                    raise ValueError("curl output option or redirect missing filename")
                yield value


def output_file(word: str, words: Iterator[str]) -> bool | None:
    """Classify curl outputs and shell redirects, consuming their filenames."""
    if word == "--remote-name":
        return True
    if word.startswith("--output="):
        return word.partition("=")[2] not in {"/dev/null", "-"}
    if word == "--output" or REDIRECT.fullmatch(word):
        target = next(words, None)
        if target is None:
            raise ValueError("curl output option or redirect missing filename")
        if word in STDOUT_REDIRECTS:
            return target != "/dev/null"
        return word == "--output" and target not in {"/dev/null", "-"}
    return None


def is_download(arguments: list[str]) -> bool:
    """Identify file outputs with an HTTPS or variable URL, excluding stdout polls."""
    outputs: list[bool] = []
    redirected = False
    remote_all = False
    urls: list[str] = []
    words = output_arguments(arguments)
    for word in words:
        if word in {"--remote-name-all", "--no-remote-name-all"}:
            remote_all = word == "--remote-name-all"
            continue
        file_output = output_file(word, words)
        if file_output is None:
            if word.startswith(("https://", "$")):
                urls.append(word)
        elif REDIRECT.fullmatch(word):
            redirected |= file_output
        else:
            outputs.append(file_output)
    return bool(urls) and (redirected or any(
        outputs[index] if index < len(outputs) else remote_all
        for index in range(len(urls))
    ))


def has_positive_retry(arguments: list[str]) -> bool:
    """Require a positive decimal integer in the last retry option."""
    value = None
    words = iter(arguments)
    for word in words:
        if word == "--retry":
            value = next(words, "")
        elif word.startswith("--retry="):
            value = word.partition("=")[2]
    try:
        return (
            value is not None
            and re.fullmatch(r"\+?[0-9]+", value) is not None
            and int(value) > 0
        )
    except ValueError:
        return False


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
                    if not has_positive_retry(arguments):
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
