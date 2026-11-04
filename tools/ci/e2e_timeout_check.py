#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Check that no Playwright test waits longer than it may run.

A wait whose `timeout:` (a numeric literal or a file constant) is longer than the project
test timeout ends on the test timeout first: the wait never gets its full budget and the
test fails with a misleading timeout (PR #995 review). The test that runs such a wait must
raise its own budget with test.setTimeout or test.slow, or the file must raise it for all
tests with test.describe.configure({ timeout }) or a beforeEach that calls test.setTimeout.

Scope follows Playwright's: a long wait inside a top-level test needs a raise in that test;
a long wait inside a top-level helper needs a raise in every top-level test that calls the
helper, directly or through other helpers. A top-level test.describe counts as one test.
Exit 0 = clean, 1 = findings, 2 = the check could not run.
"""

from __future__ import annotations

import argparse
import re
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

# Playwright's documented default when the config sets no top-level `timeout`.
PLAYWRIGHT_DEFAULT_TIMEOUT_MS = 30_000

TIMEOUT_RE = re.compile(r"\btimeout:\s*([0-9][0-9_]*|[A-Za-z_$][\w$]*)\b")
CONST_RE = re.compile(
    r"^(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*=\s*([0-9][0-9_]*)\s*;",
    re.MULTILINE,
)
CONFIG_TIMEOUT_RE = re.compile(
    r"^(?:\t| {2})timeout:\s*([0-9][0-9_]*)\s*,?\s*$", re.MULTILINE
)
TOP_CONFIGURE_RE = re.compile(
    r"^test\.describe\.configure\(\s*\{[^}]*?\btimeout:\s*([0-9][0-9_]*|[A-Za-z_$][\w$]*)\b",
    re.MULTILINE,
)
TOP_CONFIGURE_ANY_RE = re.compile(
    r"^test\.describe\.configure\(\s*\{[^}]*?\btimeout\b",
    re.MULTILINE,
)
SEG_CONFIGURE_RE = re.compile(
    r"\btest\.describe\.configure\(\s*\{[^}]*?\btimeout:\s*([0-9][0-9_]*|[A-Za-z_$][\w$]*)\b"
)
SEG_CONFIGURE_ANY_RE = re.compile(
    r"\btest\.describe\.configure\(\s*\{[^}]*?\btimeout\b"
)
SET_TIMEOUT_RE = re.compile(
    r"\b(?:test|testInfo)\.setTimeout\s*\(\s*([0-9][0-9_]*|[A-Za-z_$][\w$]*)\s*\)"
)
SET_TIMEOUT_ANY_RE = re.compile(r"\b(?:test|testInfo)\.setTimeout\s*\(")
SLOW_RE = re.compile(r"\b(?:test|testInfo)\.slow\s*\(")
SLOW_PLAIN_RE = re.compile(r"\b(?:test|testInfo)\.slow\s*\(\s*\)")
TEST_START_RE = re.compile(
    r"^test(?:\.describe)?(?:\.(?:serial|parallel|only|skip|fixme))?\s*\("
)
HELPER_START_RE = re.compile(
    r"^(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)"
    r"|^(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?\("
)
COMMENT_RE = re.compile(r"/\*[\s\S]*?\*/|(^|[\s;{}(),])//[^\n]*")


@dataclass
class Segment:
    """One top-level construct: a test (name None) or a helper (its name)."""

    start_line: int
    helper: str | None
    is_test: bool
    text: str


def strip_comments(text: str) -> str:
    """Blank out comments, keeping line numbers."""

    def blank(match: re.Match[str]) -> str:
        """Replace a comment body with spaces, newlines kept."""
        lead = match.group(1) or ""
        body = match.group(0)[len(lead) :]
        return lead + re.sub(r"[^\n]", " ", body)

    return COMMENT_RE.sub(blank, text)


def segments(text: str) -> list[Segment]:
    """Split a spec into top-level tests, helpers and other code."""
    found: list[Segment] = []
    lines = text.split("\n")
    for number, line in enumerate(lines, start=1):
        helper = HELPER_START_RE.match(line)
        is_test = TEST_START_RE.match(line) is not None
        if is_test or helper or (line[:1].strip() and line[:1] not in "})]"):
            name = (helper.group(1) or helper.group(2)) if helper else None
            found.append(Segment(number, name, is_test, ""))
    for index, seg in enumerate(found):
        end = found[index + 1].start_line - 1 if index + 1 < len(found) else len(lines)
        seg.text = "\n".join(lines[seg.start_line - 1 : end])
    return found


def long_waits(text: str, limit_ms: int) -> list[tuple[int, int]]:
    """Return (line, ms) for every timeout above limit_ms."""
    consts = {
        name: int(value.replace("_", "")) for name, value in CONST_RE.findall(text)
    }
    waits = []
    for match in TIMEOUT_RE.finditer(text):
        token = match.group(1)
        value = int(token.replace("_", "")) if token[0].isdigit() else consts.get(token)
        if value is not None and value > limit_ms:
            waits.append((text.count("\n", 0, match.start()) + 1, value))
    return waits


def callers(segs: list[Segment], helper: str) -> list[Segment]:
    """Return the tests that reach helper directly or through other helpers."""
    reached, frontier = {helper}, [helper]
    for _ in range(len(segs)):
        if not frontier:
            break
        name = frontier.pop()
        call = re.compile(rf"\b{re.escape(name)}\s*\(")
        for seg in segs:
            if seg.helper and seg.helper not in reached and call.search(seg.text):
                reached.add(seg.helper)
                frontier.append(seg.helper)
    calls = [re.compile(rf"\b{re.escape(n)}\s*\(") for n in reached]
    return [s for s in segs if s.is_test and any(c.search(s.text) for c in calls)]


def resolve_token(token: str, consts: dict[str, int]) -> int | None:
    """Resolve a numeric literal or constant identifier to milliseconds."""
    if token and token[0].isdigit():
        return int(token.replace("_", ""))
    return consts.get(token)


def scan_raises(
    text: str,
    any_re: re.Pattern[str],
    val_re: re.Pattern[str],
    consts: dict[str, int],
) -> tuple[int, bool]:
    """Return the highest resolved timeout raise in text and whether any call was unresolvable."""
    matches = list(val_re.finditer(text))
    unresolvable = len(any_re.findall(text)) > len(matches)
    raised = 0
    for match in matches:
        val = resolve_token(match.group(1), consts)
        if val is not None:
            raised = max(raised, val)
        else:
            unresolvable = True
    return raised, unresolvable


def file_level_budget(
    text: str, segs: list[Segment], consts: dict[str, int], limit_ms: int
) -> tuple[int, bool]:
    """Resolve top-level file budget from describe.configure or beforeEach."""
    cfg_val, unresolvable = scan_raises(
        text, TOP_CONFIGURE_ANY_RE, TOP_CONFIGURE_RE, consts
    )
    budget = max(limit_ms, cfg_val)
    for seg in segs:
        if seg.text.startswith("test.beforeEach("):
            be_val, be_unres = scan_raises(
                seg.text, SET_TIMEOUT_ANY_RE, SET_TIMEOUT_RE, consts
            )
            budget = max(budget, be_val)
            unresolvable = unresolvable or be_unres
    return budget, unresolvable


def resolve_test_budget(
    seg: Segment,
    consts: dict[str, int],
    file_budget: int,
    file_unresolvable: bool,
    limit_ms: int,
) -> tuple[int, bool]:
    """Resolve effective budget for a test segment, taking the maximum raise."""
    if file_unresolvable:
        return file_budget, True

    cfg_val, cfg_unres = scan_raises(
        seg.text, SEG_CONFIGURE_ANY_RE, SEG_CONFIGURE_RE, consts
    )
    to_val, to_unres = scan_raises(seg.text, SET_TIMEOUT_ANY_RE, SET_TIMEOUT_RE, consts)
    budget = max(file_budget, cfg_val, to_val)
    unresolvable = cfg_unres or to_unres

    slow_any = len(SLOW_RE.findall(seg.text))
    slow_plain = len(SLOW_PLAIN_RE.findall(seg.text))
    if slow_any > slow_plain:
        unresolvable = True
    elif slow_plain > 0:
        budget = max(budget, 3 * limit_ms)

    return budget, unresolvable


def file_findings(path: Path, limit_ms: int) -> list[str]:
    """Return one finding per long wait whose test budget is not raised."""
    text = strip_comments(path.read_text(encoding="utf-8"))
    consts = {
        name: int(value.replace("_", "")) for name, value in CONST_RE.findall(text)
    }
    segs = segments(text)
    file_budget, file_unresolvable = file_level_budget(text, segs, consts, limit_ms)
    findings = []
    for line, value in long_waits(text, limit_ms):
        owner = next((s for s in reversed(segs) if s.start_line <= line), None)
        if owner is None or not (owner.is_test or owner.helper):
            continue
        tests = [owner] if owner.is_test else callers(segs, owner.helper or "")
        for test in tests:
            budget, unresolvable = resolve_test_budget(
                test, consts, file_budget, file_unresolvable, limit_ms
            )
            if unresolvable:
                findings.append(
                    f"{path}:{line}: test budget not resolvable in the test at line {test.start_line}"
                )
            elif value > budget:
                findings.append(
                    f"{path}:{line}: timeout {value} ms exceeds the {budget} ms test timeout "
                    f"in the test at line {test.start_line}; raise it with test.setTimeout"
                )
    return findings


def test_timeout_ms(config: Path) -> int:
    """Return the project test timeout: the config's top-level value or the default."""
    match = CONFIG_TIMEOUT_RE.search(config.read_text(encoding="utf-8"))
    if match is None:
        return PLAYWRIGHT_DEFAULT_TIMEOUT_MS
    return int(match.group(1).replace("_", ""))


POLL = "\tawait expect.poll(f, { timeout: 45_000 });\n"
SELF_TEST_CASES = {
    "planted": ("test('a', async () => {\n" + POLL + "});\n", 1),
    "planted-helper": (
        "async function wait() {\n" + POLL + "}\n"
        "test('a', async () => {\n\ttest.setTimeout(90_000);\n\tawait wait();\n});\n"
        "test('b', async () => {\n\tawait wait();\n});\n",
        1,
    ),
    "planted-const": (
        (
            "const LONG = 45_000;\ntest('a', async () => {\n"
            "\tawait expect.poll(f, { timeout: LONG });\n});\n"
        ),
        1,
    ),
    "planted-expect-configure": (
        "expect.configure({ timeout: 45_000 });\ntest('a', async () => {\n"
        + POLL
        + "});\n",
        1,
    ),
    "raised": (
        "test('a', async () => {\n\ttest.setTimeout(90_000);\n" + POLL + "});\n",
        0,
    ),
    "describe-configure": (
        "test.describe.configure({ timeout: 90_000 });\ntest('a', async () => {\n"
        + POLL
        + "});\n",
        0,
    ),
    "short": (
        "test('a', async () => {\n\tawait expect.poll(f, { timeout: 15_000 });\n});\n",
        0,
    ),
    "comment": ("// removed timeout: 45_000\ntest('a', async () => {});\n", 0),
    "unknown-name": (
        "test('a', async () => {\n\tawait expect.poll(f, { timeout: REQUEST_TIMEOUT_MS });\n});\n",
        0,
    ),
    "describe-scoped": (
        "test.describe('A', () => {\n"
        "\ttest.describe.configure({ timeout: 90_000 });\n"
        "\ttest('sub', async () => {\n" + POLL + "\t});\n"
        "});\n"
        "test('b', async () => {\n" + POLL + "});\n",
        1,
    ),
    "raised-too-low": (
        "test('a', async () => {\n\ttest.setTimeout(35_000);\n" + POLL + "});\n",
        1,
    ),
    "slow": (
        "test('a', async () => {\n\ttest.slow();\n" + POLL + "});\n",
        0,
    ),
    "raised-const": (
        "const B = 120_000;\ntest('a', async () => {\n\ttest.setTimeout(B);\n"
        + POLL
        + "});\n",
        0,
    ),
    "unresolvable": (
        "test('a', async () => {\n\ttest.setTimeout(UNKNOWN_TIMEOUT);\n"
        + POLL
        + "});\n",
        1,
    ),
    "configure-unresolvable": (
        "test.describe('A', () => {\n"
        "\ttest.describe.configure({ timeout: (B) });\n"
        "\ttest('sub', async () => {\n" + POLL + "\t});\n"
        "});\n",
        1,
    ),
    "before-each-testinfo": (
        "test.beforeEach(async ({ page }, testInfo) => {\n"
        "\ttestInfo.setTimeout(90_000);\n"
        "});\n"
        "test('a', async () => {\n" + POLL + "});\n",
        0,
    ),
    "before-each-boundary": (
        "test.beforeEach(async () => {\n"
        "\tawait setup();\n"
        "});\n"
        "test('a', async () => {\n"
        "\ttest.setTimeout(90_000);\n"
        "});\n"
        "test('b', async () => {\n" + POLL + "});\n",
        1,
    ),
    "before-each-raised": (
        "test.beforeEach(async () => {\n"
        "\ttest.setTimeout(90_000);\n"
        "});\n"
        "test('a', async () => {\n" + POLL + "});\n",
        0,
    ),
    "slow-conditional": (
        "test('a', async () => {\n"
        "\ttest.slow(browserName === 'webkit');\n" + POLL + "});\n",
        1,
    ),
}

UNRESOLVABLE_CASES = ("unresolvable", "configure-unresolvable", "slow-conditional")


def self_test() -> int:
    """Prove the check on planted and clean fixtures, and the config indent forms."""
    with tempfile.TemporaryDirectory() as tmp:
        for name, (body, want) in SELF_TEST_CASES.items():
            path = Path(tmp) / f"{name}.test.ts"
            path.write_text(body, encoding="utf-8")
            findings = file_findings(path, PLAYWRIGHT_DEFAULT_TIMEOUT_MS)
            got = len(findings)
            if got != want:
                print(
                    f"e2e timeout self-test: {name} gave {got} findings, want {want}",
                    file=sys.stderr,
                )
                return 1
            if name in UNRESOLVABLE_CASES and not any(
                "test budget not resolvable" in f for f in findings
            ):
                print(
                    f"e2e timeout self-test: {name} finding text missing",
                    file=sys.stderr,
                )
                return 1
        config = Path(tmp) / "playwright.config.ts"
        config.write_text(
            "export default defineConfig({\n  timeout: 60_000\n});\n", encoding="utf-8"
        )
        if test_timeout_ms(config) != 60_000:
            print(
                "e2e timeout self-test: space-indented config timeout not read",
                file=sys.stderr,
            )
            return 1
    print(
        f"E2E timeout self-test: {len(SELF_TEST_CASES)} fixtures and the config forms verified"
    )
    return 0


def main() -> int:
    """Self-test, then scan web/e2e against the project test timeout."""
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
    limit = test_timeout_ms(config)
    findings = [
        f for path in sorted(e2e.rglob("*.test.ts")) for f in file_findings(path, limit)
    ]
    for finding in findings:
        print(finding)
    print(f"E2E timeouts: {len(findings)} findings")
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main())
