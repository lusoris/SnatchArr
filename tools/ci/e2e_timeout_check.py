#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Check that no Playwright spec waits longer than its test may run.

A spec whose numeric `timeout:` literal (an expect.poll, toPass or request budget) is
longer than the project test timeout ends on the test timeout first: the wait never gets
its full budget and the test fails with a misleading timeout (PR #995 review). Such a
spec must raise its budget with test.setTimeout, test.slow or describe.configure.

The check is per file: a helper that polls is usually defined outside the tests that
call it. Exit 0 = clean, 1 = findings, 2 = the check could not run.
"""

from __future__ import annotations

import argparse
import re
import sys
import tempfile
from pathlib import Path

# Playwright's documented default when the config sets no top-level `timeout`.
PLAYWRIGHT_DEFAULT_TIMEOUT_MS = 30_000

TIMEOUT_LITERAL_RE = re.compile(r"\btimeout:\s*([0-9][0-9_]*)\b")
CONFIG_TIMEOUT_RE = re.compile(r"^\ttimeout:\s*([0-9][0-9_]*)\s*,", re.MULTILINE)
RAISE_RE = re.compile(
    r"\btest\.(setTimeout|slow)\s*\(|\.configure\(\s*\{[^}]*\btimeout\b"
)


def test_timeout_ms(config: Path) -> int:
    """Return the project test timeout: the config's top-level value or the default."""
    match = CONFIG_TIMEOUT_RE.search(config.read_text(encoding="utf-8"))
    if match is None:
        return PLAYWRIGHT_DEFAULT_TIMEOUT_MS
    return int(match.group(1).replace("_", ""))


def file_findings(path: Path, limit_ms: int) -> list[str]:
    """Return one finding per literal above limit_ms when the file raises no budget."""
    text = path.read_text(encoding="utf-8")
    if RAISE_RE.search(text):
        return []
    findings = []
    for match in TIMEOUT_LITERAL_RE.finditer(text):
        value = int(match.group(1).replace("_", ""))
        if value > limit_ms:
            line = text.count("\n", 0, match.start()) + 1
            findings.append(
                f"{path}:{line}: timeout {value} ms exceeds the {limit_ms} ms test timeout; "
                "raise it with test.setTimeout"
            )
    return findings


def scan(root: Path, limit_ms: int) -> list[str]:
    """Scan every spec under root."""
    findings: list[str] = []
    for path in sorted(root.rglob("*.test.ts")):
        findings.extend(file_findings(path, limit_ms))
    return findings


def self_test() -> int:
    """Prove the check: a planted long poll is caught, a raised budget and a short poll pass."""
    cases = {
        "planted.test.ts": ("await expect.poll(f, { timeout: 45_000 }).toBe(1);\n", 1),
        "raised.test.ts": (
            "test.setTimeout(90_000);\nawait expect.poll(f, { timeout: 45_000 });\n",
            0,
        ),
        "short.test.ts": ("await expect.poll(f, { timeout: 15_000 });\n", 0),
        "named.test.ts": (
            "await expect.poll(f, { timeout: REQUEST_TIMEOUT_MS });\n",
            0,
        ),
    }
    with tempfile.TemporaryDirectory() as tmp:
        for name, (body, want) in cases.items():
            path = Path(tmp) / name
            path.write_text(body, encoding="utf-8")
            got = len(file_findings(path, PLAYWRIGHT_DEFAULT_TIMEOUT_MS))
            if got != want:
                print(
                    f"e2e timeout self-test: {name} gave {got} findings, want {want}",
                    file=sys.stderr,
                )
                return 1
    print("E2E timeout self-test: planted long poll caught, 3 clean fixtures pass")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--web", type=Path, default=Path("web"), help="web/ directory")
    args = parser.parse_args()
    if self_test() != 0:
        return 2
    config = args.web / "playwright.config.ts"
    e2e = args.web / "e2e"
    if not config.is_file() or not e2e.is_dir():
        print(f"e2e timeout check: {config} or {e2e} missing", file=sys.stderr)
        return 2
    findings = scan(e2e, test_timeout_ms(config))
    for finding in findings:
        print(finding)
    print(f"E2E timeouts: {len(findings)} findings")
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main())
