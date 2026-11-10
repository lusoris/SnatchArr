# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse a Praetor engine reference that differs from the engine pin of CI.

The pin is the PRAETOR_REF (or PRAETOR_COMMIT) declared in a tracked workflow under
.github/workflows, the value .config/lefthook/engine.sh and the Standards job install the
engine from. Every other tracked file that names a Praetor package or workflow with a literal
ref ("cordanaLLM/praetor/<path>@<ref>", such as the devcontainer's go install line) must use
that same ref, so moving the pin in one place and not the other fails here instead of leaving
a devcontainer on an older engine than CI. A literal ref starts with a letter or digit and
holds only letters, digits and "._+-"; a variable ("${PRAETOR_REF}", also escaped inside a
quoted test fixture) or a comment placeholder ("<ref>") is no literal ref and is not read.

Exit 0 = ok, 1 = input error, 3 = refused.
"""

from __future__ import annotations

import argparse
import re
import subprocess  # nosec B404 -- runs only git with a fixed argument list, never a shell
import sys
from pathlib import Path

MAX_FILES = 20000
MAX_BYTES = 1 << 20
MAX_LINES = 20000
GIT_TIMEOUT = 60
WORKFLOW_DIR = ".github/workflows/"
PIN_DECLARATION = re.compile(r"^\s*(PRAETOR_REF|PRAETOR_COMMIT)\s*[:=]\s*(\S+)")
# A literal ref is an allow-list of git ref characters starting with a letter or digit, so a
# variable ("${PRAETOR_REF}", also escaped), a comment placeholder ("<ref>") and this pattern's
# own source never match.
PRAETOR_REFERENCE = re.compile(r"cordanaLLM/praetor/[^@\s\"'`]+@([0-9A-Za-z][0-9A-Za-z._+-]*)", re.IGNORECASE)


def tracked_files(root: Path) -> list[str]:
    """Return the repository's tracked paths (index included), at most MAX_FILES of them."""
    result = subprocess.run(  # nosec B603 -- fixed git argv, no shell
        ["git", "ls-files", "-z"],
        cwd=root,
        capture_output=True,
        check=False,
        timeout=GIT_TIMEOUT,
    )
    if result.returncode != 0:
        error = result.stderr.decode("utf-8", errors="replace").strip()
        raise ValueError(f"git ls-files failed in {root}: {error}")
    paths = [p for p in result.stdout.decode("utf-8", "surrogateescape").split("\0") if p]
    if len(paths) > MAX_FILES:
        raise ValueError(f"{len(paths)} tracked files exceed the bound of {MAX_FILES}")
    return paths


def read_lines(path: Path) -> list[str]:
    """Return the text lines of PATH, or none for a binary, missing or oversized file."""
    try:
        data = path.read_bytes()
    except FileNotFoundError:
        return []
    if len(data) > MAX_BYTES or b"\0" in data:
        return []
    return data.decode("utf-8", "replace").splitlines()[:MAX_LINES]


def clean_value(raw: str) -> str:
    """Strip quotes and a trailing comment from a declared PRAETOR_REF value."""
    return raw.split("#", 1)[0].strip().strip("\"'")


def declared_pins(root: Path, paths: list[str]) -> list[tuple[str, int, str]]:
    """Return (path, line, value) for every PRAETOR_REF or PRAETOR_COMMIT in a workflow."""
    pins = []
    for rel in paths:
        if not rel.startswith(WORKFLOW_DIR) or not rel.endswith((".yml", ".yaml")):
            continue
        for number, line in enumerate(read_lines(root / rel), 1):
            match = PIN_DECLARATION.match(line)
            if match and clean_value(match.group(2)):
                pins.append((rel, number, clean_value(match.group(2))))
    return pins


def literal_references(root: Path, paths: list[str]) -> list[tuple[str, int, str]]:
    """Return (path, line, ref) for every literal cordanaLLM/praetor/<path>@<ref>."""
    found = []
    for rel in paths:
        for number, line in enumerate(read_lines(root / rel), 1):
            found.extend((rel, number, match.group(1)) for match in PRAETOR_REFERENCE.finditer(line))
    return found


def check(root: Path) -> int:
    """Print every reference that differs from the CI pin; return the exit status."""
    paths = tracked_files(root)
    pins = declared_pins(root, paths)
    values = sorted({value for _, _, value in pins})
    if len(values) != 1:
        where = ", ".join(f"{p}:{n} {v}" for p, n, v in pins) or "none"
        print(f"refused: want exactly one PRAETOR_REF value under {WORKFLOW_DIR}, found: {where}")
        return 3
    pin = values[0]
    source = f"{pins[0][0]}:{pins[0][1]}"
    refused = 0
    for rel, number, ref in literal_references(root, paths):
        if ref != pin:
            print(f"refused {rel}:{number}: Praetor ref {ref} differs from the engine pin {pin} ({source}); move both together")
            refused += 1
    if refused:
        return 3
    print(f"ok: every literal Praetor ref equals the engine pin {pin} ({source})")
    return 0


def main() -> int:
    """Parse the arguments and run the check."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--root", type=Path, default=Path("."), help="repository root (default .)")
    args = parser.parse_args()
    try:
        return check(args.root)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
