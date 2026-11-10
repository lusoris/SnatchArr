# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse release notes that claim an open issue is closed.

Only the newest level-two CHANGELOG.md section is checked. Linked and bare
`closes #N` and `closes owner/repo#N` claims are deduplicated per repository and
checked through `gh api`, or offline JSON fixtures under --api-dir.
Exit 0 = all claims closed (including no claims),
1 = input/API failure, 3 = at least one claim names an open issue.
"""

import argparse
import contextlib
import io
import json
import os
import re
import subprocess  # nosec B404  # runs only gh api with a fixed argument list, never a shell
import sys
import tempfile
from pathlib import Path

GH_TIMEOUT_SECONDS = 60
GIT_TIMEOUT_SECONDS = 60
MAX_CLAIMS = 500
MAX_CHANGELOG_BYTES = 8_388_608
MAX_CHANGELOG_LINES = 100_000
REPO_RE = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
CLAIM_RE = re.compile(
    r"\bcloses[ \t]+(?:\[(?P<linked>(?:[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)?#[0-9]+)\]"
    r"\((?P<url>[^\r\n)]*)\)|(?P<bare>(?:[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)?#[0-9]+)\b)",
    re.IGNORECASE,
)
ISSUE_URL_RE = re.compile(
    r"https://github\.com/([^/\s]+)/([^/\s]+)/(?:issues|pull)/[0-9]+(?:[/?#].*)?",
    re.IGNORECASE,
)
COMMIT_RE = re.compile(
    r"\[([0-9a-f]{7,40})\]\(https://github\.com/[^)\s]+/commit/[0-9a-f]{7,40}\)",
    re.IGNORECASE,
)


def history_commit(reference: str, repo_dir: Path) -> str:
    """Name the newest commit whose message carries the closing claim."""
    pattern = r"closes[[:space:]]+\[?" + reference.replace(".", r"\.") + "([^0-9]|$)"
    try:
        result = subprocess.run(  # nosec B603  # fixed git argv list, no shell; pattern built from a regex-matched issue reference
            [
                "git",
                "-C",
                str(repo_dir),
                "log",
                "-n",
                "1",
                "--format=%h",
                "--regexp-ignore-case",
                "--extended-regexp",
                f"--grep={pattern}",
            ],
            capture_output=True,
            text=True,
            timeout=GIT_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise CheckError(
            f"git log for {reference} did not complete: {error}"
        ) from error
    if result.returncode != 0:
        detail = result.stderr.strip() or f"exit {result.returncode}"
        raise CheckError(f"git log for {reference} failed: {detail}")
    commit = result.stdout.strip()
    if not commit:
        return f"no commit in the history carries 'closes {reference}'"
    return f"commit {commit}"


def line_commit(line: str, reference: str, repo_dir: Path) -> str:
    """Name the commit a changelog line links to, else the commit carrying its claim."""
    match = COMMIT_RE.search(line)
    return f"commit {match.group(1)}" if match else history_commit(reference, repo_dir)


class CheckError(Exception):
    """Unreadable changelog, invalid input, or failed issue-state lookup."""


class SelfTestError(Exception):
    """A planted self-test case did not behave as required."""


def endpoint_filename(endpoint: str) -> str:
    """Map a GitHub REST API endpoint to the dogfood gate fixture convention."""
    return f"{endpoint.strip('/').replace('/', '_')}.json"


def claim_key(match: re.Match[str], default_repository: str) -> tuple[str, int]:
    """Resolve a claim's repository from its text, GitHub URL, or default."""
    reference = match["linked"] or match["bare"]
    explicit_repository, number = reference.rsplit("#", 1)
    repository = explicit_repository or default_repository
    if not explicit_repository and match["url"]:
        url = ISSUE_URL_RE.fullmatch(match["url"])
        if url:
            repository = f"{url[1]}/{url[2]}"
    if not REPO_RE.fullmatch(repository):
        raise CheckError(f"invalid claim repository: {repository!r}")
    return repository, int(number)


def newest_claims(
    changelog: Path, repository: str
) -> dict[tuple[str, int], tuple[str, str]]:
    """Collect bounded, distinct closing claims, their first lines and references."""
    with changelog.open("rb") as source:
        raw = source.read(MAX_CHANGELOG_BYTES + 1)
    if len(raw) > MAX_CHANGELOG_BYTES:
        raise CheckError(f"changelog exceeds {MAX_CHANGELOG_BYTES} bytes")
    lines = raw.decode("utf-8").splitlines()
    if len(lines) > MAX_CHANGELOG_LINES:
        raise CheckError(f"changelog exceeds {MAX_CHANGELOG_LINES} lines")
    in_section = False
    claims: dict[tuple[str, int], tuple[str, str]] = {}
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
            reference = match["linked"] or match["bare"]
            claims.setdefault(claim_key(match, repository), (line.strip(), reference))
    if not in_section:
        raise CheckError("changelog has no level-two version heading")
    return claims


def issue_state(repository: str, number: int, api_dir: Path | None) -> str:
    """Read a strict open/closed state from a JSON fixture or a timed gh call."""
    if not REPO_RE.fullmatch(repository):
        raise CheckError(f"invalid claim repository: {repository!r}")
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
            result = subprocess.run(  # nosec B603  # fixed gh argv list, no shell; endpoint built from regex-matched owner, repo and issue number
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
    claims = newest_claims(changelog, repository)
    if claims and not REPO_RE.fullmatch(repository):
        raise CheckError("--repository or GITHUB_REPOSITORY must name owner/repository")
    offenders = []
    for (claim_repository, number), (line, reference) in claims.items():
        if issue_state(claim_repository, number, api_dir) == "open":
            prefix = "" if claim_repository == repository else claim_repository
            offenders.append(
                f"release closes check: open {prefix}#{number}: {line} ({line_commit(line, reference, changelog.resolve().parent)})"
            )
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
        "bare cross-repository open claim refused",
        "## 1.0.1\ncloses other/lib#7\n",
        {("other/lib", 7): '{"state":"open"}'},
        3,
        "open other/lib#7",
    ),
    (
        "linked URL cross-repository open claim refused",
        "## 1.0.1\ncloses [#7](https://github.com/other/lib/issues/7)\n",
        {("other/lib", 7): '{"state":"open"}'},
        3,
        "open other/lib#7",
    ),
    (
        "linked URL with differently cased host refused",
        "## 1.0.1\ncloses [#7](https://GitHub.com/other/lib/issues/7)\n",
        {("other/lib", 7): '{"state":"open"}', 7: '{"state":"closed"}'},
        3,
        "open other/lib#7",
    ),
    (
        "open claim names the linked commit",
        (
            "## 1.0.1\n* fix, closes #9 ([abc1234](https://github.com/example/project/commit/"
            "abc1234def5678))\n"
        ),
        {9: '{"state":"open"}'},
        3,
        "(commit abc1234)",
    ),
    (
        "linked text cross-repository open claim refused",
        "## 1.0.1\ncloses [other/lib#7](https://example.com/fix)\n",
        {("other/lib", 7): '{"state":"open"}'},
        3,
        "open other/lib#7",
    ),
    (
        "linked text repository takes precedence",
        "## 1.0.1\ncloses [other/lib#7](https://github.com/example/project/issues/7)\n",
        {("other/lib", 7): '{"state":"open"}', 7: '{"state":"closed"}'},
        3,
        "open other/lib#7",
    ),
    (
        "linked pull URL cross-repository claim refused",
        "## 1.0.1\ncloses [#7](https://github.com/other/lib/pull/7)\n",
        {("other/lib", 7): '{"state":"open"}'},
        3,
        "open other/lib#7",
    ),
    (
        "same number in two repositories checked separately",
        "## 1.0.1\ncloses #7\ncloses other/lib#7\n",
        {7: '{"state":"closed"}', ("other/lib", 7): '{"state":"closed"}'},
        0,
        "checked 2 issue(s)",
    ),
    (
        "cross-repository forms deduplicated",
        (
            "## 1.0.1\ncloses other/lib#7\n"
            "closes [#7](https://github.com/other/lib/issues/7)\n"
        ),
        {("other/lib", 7): '{"state":"closed"}'},
        0,
        "checked 1 issue(s)",
    ),
    (
        "text that is no issue reference is not a claim",
        "## 1.0.1\ncloses other!/lib#7 and closes (#8)\n",
        {},
        0,
        "checked 0 issue(s)",
    ),
    (
        "linked text that is no issue reference is not a claim",
        "## 1.0.1\ncloses [other!/lib#7](https://github.com/example/project/issues/7)\n",
        {},
        0,
        "checked 0 issue(s)",
    ),
    (
        "invalid linked URL repository refused",
        "## 1.0.1\ncloses [#7](https://github.com/other!/lib/issues/7)\n",
        {},
        1,
        "invalid claim repository",
    ),
    (
        "linked non-GitHub URL uses default repository",
        "## 1.0.1\ncloses [#42](https://example.com/fix)\n",
        {42: '{"state":"closed"}'},
        0,
        "checked 1 issue(s)",
    ),
    (
        "explicit default repository keeps short diagnostic",
        "## 1.0.1\ncloses example/project#42\n",
        {42: '{"state":"open"}'},
        3,
        "open #42: closes example/project#42",
    ),
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
    (
        "uppercase-only closed claim checked",
        "## 1.0.1\nCLOSES #42\n",
        {42: '{"state":"closed"}'},
        0,
        "checked 1 issue(s)",
    ),
    (
        "uppercase-only open claim refused",
        "## 1.0.1\nCLOSES #42\n",
        {42: '{"state":"open"}'},
        3,
        "open #42: CLOSES #42",
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
        (f"open #42: {LINKED_CLAIM.strip()}", "open #43: closes #43"),
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
    (
        "unlinked open claim names the commit carrying it",
        "## 1.0.1\n* fix, closes #9\n",
        {9: '{"state":"open"}'},
        3,
        "open #9: * fix, closes #9 (commit {commit9})",
    ),
    (
        "unlinked claim lookup ignores a longer issue number",
        "## 1.0.1\n* fix, closes #90\n",
        {90: '{"state":"open"}'},
        3,
        "(commit {commit90})",
    ),
    (
        "unlinked claim without a carrying commit says so",
        "## 1.0.1\n* fix, closes #11\n",
        {11: '{"state":"open"}'},
        3,
        "no commit in the history carries 'closes #11'",
    ),
)

SELF_TEST_COMMITS = (
    ("9", "fix: first\n\ncloses #9"),
    ("90", "fix: second\n\ncloses #90"),
)


def run_git_step(argv: list[str]) -> str:
    """Run one self-test git step and return its stripped standard output."""
    try:
        result = subprocess.run(  # nosec B603  # fixed git argv list in a throwaway self-test repository
            argv,
            capture_output=True,
            text=True,
            timeout=GIT_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise SelfTestError(f"git fixture did not complete: {error}") from error
    if result.returncode != 0:
        raise SelfTestError(f"git fixture failed: {result.stderr.strip()}")
    return result.stdout.strip()


def git_fixture(root: Path) -> dict[str, str]:
    """Create a throwaway repository whose commits carry planted closing claims."""
    base = [
        "git",
        "-C",
        str(root),
        "-c",
        "user.name=self-test",
        "-c",
        "user.email=self-test@example.invalid",
        "-c",
        "commit.gpgsign=false",
    ]
    run_git_step([*base, "init", "-q"])
    commits = {}
    for key, message in SELF_TEST_COMMITS:
        run_git_step([*base, "commit", "-q", "--allow-empty", "-m", message])
        commits[f"commit{key}"] = run_git_step([*base, "log", "-n", "1", "--format=%h"])
    return commits


def self_test_case(
    root: Path,
    label: str,
    changelog: str,
    fixtures: dict[int | tuple[str, int], str],
    want_exit: int,
    needles: str | tuple[str, ...],
) -> None:
    """Run one isolated CLI fixture case and assert exit code and diagnostic."""
    commits = git_fixture(root)
    changelog_path = root / "CHANGELOG.md"
    changelog_path.write_text(changelog, encoding="utf-8")
    api_dir = root / "api"
    api_dir.mkdir()
    for claim, payload in fixtures.items():
        repository, number = (
            claim if isinstance(claim, tuple) else (FIXTURE_REPO, claim)
        )
        endpoint = f"repos/{repository}/issues/{number}"
        (api_dir / endpoint_filename(endpoint)).write_text(payload, encoding="utf-8")
    options = build_parser().parse_args(
        [
            "--repository",
            FIXTURE_REPO,
            "--changelog",
            str(changelog_path),
            "--api-dir",
            str(api_dir),
        ]
    )
    output = io.StringIO()
    with contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
        exit_code = run_check(options)
    message = output.getvalue()
    expected = tuple(
        needle.format_map(commits)
        for needle in ((needles,) if isinstance(needles, str) else needles)
    )
    if exit_code != want_exit or any(needle not in message for needle in expected):
        raise SelfTestError(
            f"{label}: expected exit {want_exit} with {expected!r}, "
            f"got {exit_code} ({message.strip()})"
        )
    print(f"ok   {label}: exit {exit_code}")


def self_test() -> None:
    """Run offline planted positive, negative, and boundary cases."""
    failures = []
    with tempfile.TemporaryDirectory() as scratch:
        for index, case in enumerate(SELF_TEST_CASES):
            root = Path(scratch) / str(index)
            root.mkdir()
            try:
                self_test_case(root, *case)
            except SelfTestError as error:
                failures.append(str(error))
    if failures:
        raise SelfTestError("\n".join(failures))
    print(f"release closes self-test: {len(SELF_TEST_CASES)} cases behaved as required")


def build_parser() -> argparse.ArgumentParser:
    """Return the command-line parser shared by main and the self-test."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--changelog", type=Path, default=Path("CHANGELOG.md"))
    parser.add_argument("--repository", default=os.environ.get("GITHUB_REPOSITORY", ""))
    parser.add_argument("--api-dir", type=Path, default=None)
    parser.add_argument("--self-test", action="store_true")
    return parser


def run_check(options: argparse.Namespace) -> int:
    """Evaluate the changelog and fail closed on changelog or API errors."""
    try:
        exit_code, message = evaluate_check(
            options.repository, options.changelog, options.api_dir
        )
    except (CheckError, OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"release closes check error: {error}", file=sys.stderr)
        return 1
    print(message)
    return exit_code


def main(arguments: list[str] | None = None) -> int:
    """Run the self-test or the check; a failed self-test exits 1."""
    options = build_parser().parse_args(arguments)
    if not options.self_test:
        return run_check(options)
    try:
        self_test()
    except (SelfTestError, OSError) as error:
        print(f"release closes check error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
