# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse release notes that claim an open issue is closed.

Only the newest level-two CHANGELOG.md section is checked. Linked and bare
`closes #N` claims are deduplicated and checked through `gh api`, or offline
JSON fixtures under --api-dir. Exit 0 = all claims closed (including no claims),
1 = input/API failure, 3 = at least one claim names an open issue.
"""

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

GH_TIMEOUT_SECONDS = 60
MAX_CLAIMS = 500
MAX_CHANGELOG_BYTES = 8_388_608
MAX_CHANGELOG_LINES = 100_000
REPO_RE = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
CLAIM_RE = re.compile(
    r"\bcloses[ \t]+(?:\[#([0-9]+)\]\([^\r\n)]*\)|#([0-9]+)\b)",
    re.IGNORECASE,
)


class CheckError(Exception):
    """Unreadable changelog, invalid input, or failed issue-state lookup."""


class SelfTestError(Exception):
    """A planted self-test case did not behave as required."""


def endpoint_filename(endpoint: str) -> str:
    """Map a GitHub REST API endpoint to the dogfood gate fixture convention."""
    return f"{endpoint.strip('/').replace('/', '_')}.json"


def newest_claims(changelog: Path) -> dict[int, str]:
    """Collect bounded, distinct closing claims and their first changelog lines."""
    with changelog.open("rb") as source:
        raw = source.read(MAX_CHANGELOG_BYTES + 1)
    if len(raw) > MAX_CHANGELOG_BYTES:
        raise CheckError(f"changelog exceeds {MAX_CHANGELOG_BYTES} bytes")
    lines = raw.decode("utf-8").splitlines()
    if len(lines) > MAX_CHANGELOG_LINES:
        raise CheckError(f"changelog exceeds {MAX_CHANGELOG_LINES} lines")
    in_section = False
    claims: dict[int, str] = {}
    count = 0
    for line in lines:
        if line.startswith("## "):
            if in_section:
                break
            in_section = True
        if not in_section:
            continue
        for match in CLAIM_RE.finditer(line):
            count += 1
            if count > MAX_CLAIMS:
                raise CheckError(f"newest section exceeds {MAX_CLAIMS} closing claims")
            number = int(match.group(1) or match.group(2))
            claims.setdefault(number, line.strip())
    if not in_section:
        raise CheckError("changelog has no level-two version heading")
    return claims


def issue_state(repository: str, number: int, api_dir: Path | None) -> str:
    """Read a strict open/closed state from a JSON fixture or a timed gh call."""
    endpoint = f"repos/{repository}/issues/{number}"
    if api_dir is not None:
        target = api_dir / endpoint_filename(endpoint)
        try:
            payload = json.loads(target.read_text(encoding="utf-8"))
        except (OSError, ValueError) as error:
            raise CheckError(f"cannot read API fixture {target}: {error}") from error
        if not isinstance(payload, dict):
            raise CheckError(f"expected JSON object from {endpoint}")
        state = payload.get("state")
    else:
        try:
            result = subprocess.run(
                ["gh", "api", endpoint, "--jq", ".state"],
                capture_output=True,
                text=True,
                timeout=GH_TIMEOUT_SECONDS,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired) as error:
            raise CheckError(f"gh api {endpoint} did not complete: {error}") from error
        if result.returncode != 0:
            detail = result.stderr.strip() or f"exit {result.returncode}"
            raise CheckError(f"gh api {endpoint} failed: {detail}")
        state = result.stdout.strip()
    if state not in ("open", "closed"):
        raise CheckError(f"invalid issue state from {endpoint}: {state!r}")
    return state


def evaluate_check(
    repository: str, changelog: Path, api_dir: Path | None
) -> tuple[int, str]:
    """Check each distinct issue once and report every open-issue offender."""
    claims = newest_claims(changelog)
    if claims and not REPO_RE.fullmatch(repository):
        raise CheckError("--repository or GITHUB_REPOSITORY must name owner/repository")
    offenders = []
    for number, line in claims.items():
        if issue_state(repository, number, api_dir) == "open":
            offenders.append(f"release closes check: open #{number}: {line}")
    if offenders:
        return 3, "\n".join(offenders)
    return 0, f"release closes check: checked {len(claims)} issue(s), all closed"


FIXTURE_REPO = "example/project"
LINKED_CLAIM = (
    "  * Fixed something, closes [#42](https://github.com/example/project/issues/42)  "
)
NEWEST_SECTION = f"# Changelog\n\n## 1.0.1\n{LINKED_CLAIM}\n"
SELF_TEST_CASES = (
    (
        "open claim refused",
        NEWEST_SECTION,
        {42: '{"state":"open"}'},
        3,
        f"open #42: {LINKED_CLAIM.strip()}",
    ),
    (
        "all closed",
        NEWEST_SECTION + "closes #43\n",
        {42: '{"state":"closed"}', 43: '{"state":"closed"}'},
        0,
        "checked 2 issue(s)",
    ),
    ("no claims", "## 1.0.1\n* Updated documentation\n", {}, 0, "checked 0 issue(s)"),
    ("API error", NEWEST_SECTION, {}, 1, "cannot read API fixture"),
    (
        "older section ignored",
        "## 1.0.1\n* Docs\n## 1.0.0\ncloses #42\n",
        {42: '{"state":"open"}'},
        0,
        "checked 0 issue(s)",
    ),
    (
        "bare open claim refused",
        "## 1.0.1\n  * Fixed, closes #42  \n",
        {42: '{"state":"open"}'},
        3,
        "open #42: * Fixed, closes #42",
    ),
    (
        "case insensitive and deduplicated",
        NEWEST_SECTION + "CLOSES #42\n",
        {42: '{"state":"closed"}'},
        0,
        "checked 1 issue(s)",
    ),
    ("preamble ignored", "closes #42\n## 1.0.1\n* Docs\n", {}, 0, "checked 0 issue(s)"),
    ("invalid API JSON", NEWEST_SECTION, {42: "{"}, 1, "cannot read API fixture"),
    ("invalid API shape", NEWEST_SECTION, {42: "[]"}, 1, "expected JSON object"),
    ("missing state", NEWEST_SECTION, {42: "{}"}, 1, "invalid issue state"),
    (
        "unknown state",
        NEWEST_SECTION,
        {42: '{"state":"unknown"}'},
        1,
        "invalid issue state",
    ),
    (
        "multiple open claims",
        NEWEST_SECTION + "closes #43\n",
        {42: '{"state":"open"}', 43: '{"state":"open"}'},
        3,
        "open #43: closes #43",
    ),
    (
        "claim cap accepted",
        "## 1.0.1\n" + "closes #42\n" * MAX_CLAIMS,
        {42: '{"state":"closed"}'},
        0,
        "checked 1 issue(s)",
    ),
    (
        "claim cap exceeded",
        "## 1.0.1\n" + "closes #42\n" * (MAX_CLAIMS + 1),
        {},
        1,
        "exceeds 500 closing claims",
    ),
    ("missing section refused", "# Changelog\n", {}, 1, "no level-two version heading"),
)


def self_test_case(
    root: Path,
    label: str,
    changelog: str,
    fixtures: dict[int, str],
    want_exit: int,
    needle: str,
) -> None:
    """Run one isolated CLI fixture case and assert exit code and diagnostic."""
    changelog_path = root / "CHANGELOG.md"
    changelog_path.write_text(changelog, encoding="utf-8")
    api_dir = root / "api"
    api_dir.mkdir()
    for number, payload in fixtures.items():
        endpoint = f"repos/{FIXTURE_REPO}/issues/{number}"
        (api_dir / endpoint_filename(endpoint)).write_text(payload, encoding="utf-8")
    result = subprocess.run(
        [
            sys.executable,
            "-I",
            str(Path(__file__).resolve()),
            "--repository",
            FIXTURE_REPO,
            "--changelog",
            str(changelog_path),
            "--api-dir",
            str(api_dir),
        ],
        capture_output=True,
        text=True,
        timeout=GH_TIMEOUT_SECONDS,
        check=False,
    )
    message = result.stdout + result.stderr
    if result.returncode != want_exit or needle not in message:
        raise SelfTestError(
            f"{label}: expected exit {want_exit} with {needle!r}, "
            f"got {result.returncode} ({message.strip()})"
        )
    print(f"ok   {label}: exit {result.returncode}")


def self_test() -> None:
    """Run offline planted positive, negative, and boundary cases."""
    with tempfile.TemporaryDirectory() as scratch:
        for index, case in enumerate(SELF_TEST_CASES):
            root = Path(scratch) / str(index)
            root.mkdir()
            self_test_case(root, *case)
    print(f"release closes self-test: {len(SELF_TEST_CASES)} cases behaved as required")


def main(arguments: list[str] | None = None) -> int:
    """Parse arguments and fail closed on changelog, API, or self-test errors."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--changelog", type=Path, default=Path("CHANGELOG.md"))
    parser.add_argument("--repository", default=os.environ.get("GITHUB_REPOSITORY", ""))
    parser.add_argument("--api-dir", type=Path, default=None)
    parser.add_argument("--self-test", action="store_true")
    options = parser.parse_args(arguments)
    try:
        if options.self_test:
            self_test()
            return 0
        exit_code, message = evaluate_check(
            options.repository, options.changelog, options.api_dir
        )
    except (
        CheckError,
        SelfTestError,
        OSError,
        ValueError,
        subprocess.SubprocessError,
    ) as error:
        print(f"release closes check error: {error}", file=sys.stderr)
        return 1
    print(message)
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
