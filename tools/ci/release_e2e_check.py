# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Gate release pull requests on Playwright feature e2e coverage for feat/fix commits.

Every non-merge `feat` or `fix` commit between the previous `v*` tag and HEAD must
either add or modify a `web/e2e/features/*.test.ts` file in its own diff, or be
named by a `covers: ... #N` line in a `web/e2e/features/*.test.ts` file at HEAD
that contains at least one `test(` call. A `fix(deps)` commit has no feature of its
own: it counts as covered by the suite only when the Playwright JSON report of the full
web e2e suite shows every configured project passed on the release head commit.
"""

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

GIT_TIMEOUT_SECONDS = 60
COUNTED_RE = re.compile(r"^(feat|fix)(\([^)]*\))?!?: ")
PR_NUMBER_RE = re.compile(r"\(#([0-9]+)\)")
FEATURE_TEST_RE = re.compile(r"^web/e2e/features/[^/]+\.test\.ts$")
DEPS_RE = re.compile(r"^fix\(deps\)!?: ")
NO_SUITE_REPORT = "no full e2e suite report"
MAX_REPORT_SUITES = 100_000
PASSED_TEST_STATUSES = ("expected", "flaky")


class CheckError(Exception):
    """Git failure or invalid ref passed to the release e2e check."""


class SelfTestError(Exception):
    """A planted self-test case did not behave as required."""


def run_git(repository: Path, *arguments: str) -> str:
    """Run git in repository with a timeout and return stdout."""
    if not repository.is_dir():
        raise CheckError(f"repository directory does not exist: {repository}")
    try:
        result = subprocess.run(
            ["git", *arguments],
            cwd=repository,
            capture_output=True,
            text=True,
            timeout=GIT_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise CheckError(f"git {' '.join(arguments)} did not complete: {error}") from error
    if result.returncode != 0:
        detail = result.stderr.strip() or f"exit {result.returncode}"
        raise CheckError(f"git {' '.join(arguments)} failed: {detail}")
    return result.stdout


def resolve_ref(repository: Path, ref: str) -> str:
    """Validate a git ref and verify that it resolves to a commit."""
    if not ref or ref.startswith("-"):
        raise CheckError(f"invalid git ref: {ref!r}")
    run_git(repository, "rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}")
    return ref


def default_base_tag(repository: Path) -> str:
    """Return the latest v* tag reachable from HEAD."""
    tag = run_git(
        repository, "describe", "--tags", "--abbrev=0", "--match", "v*", "HEAD"
    ).strip()
    if not tag:
        raise CheckError("git describe returned an empty tag")
    return tag


def load_head_feature_tests(repository: Path, head: str) -> list[tuple[str, list[str]]]:
    """Return (path, covers_Fragments) for head feature tests containing test(."""
    listing = run_git(repository, "ls-tree", "--name-only", head, "--", "web/e2e/features/")
    paths = sorted(
        line.strip()
        for line in listing.splitlines()
        if FEATURE_TEST_RE.fullmatch(line.strip())
    )
    loaded: list[tuple[str, list[str]]] = []
    for path in paths:
        content = run_git(repository, "show", f"{head}:{path}")
        if "test(" not in content:
            continue
        covers = [
            line.split("covers:", 1)[1]
            for line in content.splitlines()
            if "covers:" in line
        ]
        loaded.append((path, covers))
    return loaded


def own_diff_feature_test(repository: Path, commit: str) -> str | None:
    """Return the first web/e2e/features/*.test.ts added or modified in commit."""
    changed = run_git(
        repository,
        "diff-tree",
        "--root",
        "--no-commit-id",
        "--name-only",
        "-r",
        "--diff-filter=AMR",
        commit,
    )
    for raw in changed.splitlines():
        path = raw.strip()
        if FEATURE_TEST_RE.fullmatch(path):
            return path
    return None


def find_covering_file(
    pr_number: str, feature_tests: list[tuple[str, list[str]]]
) -> str | None:
    """Return the first feature test file whose covers: line names #pr_number."""
    token_re = re.compile(rf"#{re.escape(pr_number)}\b")
    for path, covers_lines in feature_tests:
        if any(token_re.search(line) for line in covers_lines):
            return path
    return None


def report_tests(report: dict) -> list[dict]:
    """Return every test entry of a Playwright JSON report, walking suites without recursion."""
    pending = list(report.get("suites") or [])
    tests: list[dict] = []
    for _ in range(MAX_REPORT_SUITES):
        if not pending:
            return tests
        suite = pending.pop()
        for spec in suite.get("specs") or []:
            tests.extend(spec.get("tests") or [])
        pending.extend(suite.get("suites") or [])
    raise CheckError("e2e suite report nests more suites than the check reads")


def suite_verdict(report_path: Path | None, head_commit: str | None) -> str | None:
    """Return None when the full e2e suite passed on head_commit, else why it does not count."""
    if report_path is None or not head_commit:
        return NO_SUITE_REPORT
    try:
        report = json.loads(report_path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as error:
        return f"e2e suite report unreadable: {error}"
    if not isinstance(report, dict):
        return "e2e suite report is not a JSON object"
    config = report.get("config") or {}
    commit = (config.get("metadata") or {}).get("commit") or ""
    if commit != head_commit:
        return f"e2e suite ran on {commit[:12] or 'no commit'}, not on {head_commit[:12]}"
    if report.get("errors") or (report.get("stats") or {}).get("unexpected", 1) != 0:
        return "e2e suite has failed tests or errors"
    passed = {
        test.get("projectName")
        for test in report_tests(report)
        if test.get("status") in PASSED_TEST_STATUSES
    }
    projects = [project.get("name") for project in config.get("projects") or []]
    skipped = [name for name in projects if name not in passed]
    if not projects or skipped:
        return f"e2e suite skipped project(s): {', '.join(skipped) or 'none configured'}"
    return None


def classify_commit(
    repository: Path,
    sha: str,
    subject: str,
    feature_tests: list[tuple[str, list[str]]],
    suite_reason: str | None,
) -> tuple[bool, str]:
    """Return (covered, output line) for one counted feat/fix commit."""
    sha12 = sha[:12]
    prs = PR_NUMBER_RE.findall(subject)
    pr_label = f"#{prs[-1]}" if prs else "#-"
    diff_test = own_diff_feature_test(repository, sha)
    if diff_test is not None:
        return True, f"covered {sha12} {pr_label} {diff_test}"
    marker_test = find_covering_file(prs[-1], feature_tests) if prs else None
    if marker_test is not None:
        return True, f"covered {sha12} {pr_label} {marker_test}"
    if DEPS_RE.match(subject):
        if suite_reason is None:
            return True, f"covered-by-suite {sha12} {pr_label} full e2e suite passed on head"
        return False, f"UNCOVERED {sha12} {subject} ({suite_reason})"
    if not prs:
        return False, f"UNCOVERED {sha12} {subject} (no PR number)"
    return False, f"UNCOVERED {sha12} {subject}"


def check_range(
    repository: Path,
    base: str | None = None,
    head: str = "HEAD",
    suite_reason: str | None = NO_SUITE_REPORT,
) -> tuple[int, list[str]]:
    """Check commits in base..head and return (uncovered_count, output_lines)."""
    resolved_head = resolve_ref(repository, head)
    resolved_base = resolve_ref(
        repository, base if base is not None else default_base_tag(repository)
    )
    log_out = run_git(
        repository,
        "log",
        "--no-merges",
        "--format=%H%x1f%s",
        f"{resolved_base}..{resolved_head}",
    )
    feature_tests = load_head_feature_tests(repository, resolved_head)
    lines: list[str] = []
    covered = 0
    uncovered = 0
    for entry in log_out.splitlines():
        if not entry:
            continue
        if "\x1f" not in entry:
            raise CheckError(f"unexpected git log line: {entry!r}")
        sha, subject = entry.split("\x1f", 1)
        if not COUNTED_RE.match(subject):
            continue
        is_covered, line = classify_commit(
            repository, sha, subject, feature_tests, suite_reason
        )
        lines.append(line)
        if is_covered:
            covered += 1
        else:
            uncovered += 1
    total = covered + uncovered
    lines.append(
        f"release e2e check: {total} feat/fix commit(s) in {resolved_base}..{resolved_head}, "
        f"{covered} covered, {uncovered} uncovered"
    )
    return uncovered, lines


def git_fixture(repository: Path, *arguments: str) -> None:
    """Run git in a temporary self-test fixture repository."""
    identity = [
        "-c",
        "user.name=fixture",
        "-c",
        "user.email=fixture@invalid",
        "-c",
        "commit.gpgsign=false",
        "-c",
        "tag.gpgsign=false",
    ]
    run_git(repository, *identity, *arguments)


def create_fixture_repo(root: Path, name: str) -> Path:
    """Create a temporary git repo initialized with tag v0.0.1."""
    repo = root / name
    repo.mkdir()
    git_fixture(repo, "init", "--quiet", "--initial-branch", "main")
    (repo / "README.md").write_text("fixture\n", encoding="utf-8")
    git_fixture(repo, "add", "README.md")
    git_fixture(repo, "commit", "--quiet", "--message", "chore: initial")
    git_fixture(repo, "tag", "v0.0.1")
    return repo


def write_commit(repo: Path, rel_path: str, content: str, message: str) -> None:
    """Write rel_path and commit it in a fixture repository."""
    target = repo / rel_path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")
    git_fixture(repo, "add", rel_path)
    git_fixture(repo, "commit", "--quiet", "--message", message)


def write_suite_report(repo: Path, suite: str) -> str | None:
    """Write a planted Playwright JSON report for repo and return the suite verdict."""
    head = run_git(repo, "rev-parse", "HEAD").strip()
    commit = "0" * 40 if suite == "older" else head
    tests = [{"projectName": "public", "status": "expected"}]
    if suite != "skipped":
        tests.append({"projectName": "features", "status": "expected"})
    report = {
        "config": {
            "metadata": {"commit": commit},
            "projects": [{"name": "public"}, {"name": "features"}],
        },
        "suites": [{"specs": [], "suites": [{"specs": [{"tests": tests}]}]}],
        "errors": [],
        "stats": {"expected": len(tests), "unexpected": 0, "flaky": 0, "skipped": 0},
    }
    path = repo.parent / f"{repo.name}-report.json"
    path.write_text(json.dumps(report), encoding="utf-8")
    return suite_verdict(path, head)


def expect_case(repo: Path, want_exit: int, needle: str, suite: str | None) -> None:
    """Run check_range on repo and verify exit code and output substring."""
    suite_reason = write_suite_report(repo, suite) if suite else NO_SUITE_REPORT
    uncovered, lines = check_range(repo, suite_reason=suite_reason)
    got_exit = 1 if uncovered else 0
    output = "\n".join(lines)
    if got_exit != want_exit:
        raise SelfTestError(f"expected exit {want_exit}, got {got_exit}:\n{output}")
    if needle not in output:
        raise SelfTestError(f"expected {needle!r} in output:\n{output}")


def check_case(
    root: Path,
    name: str,
    commits: list[tuple[str, str, str]],
    want_exit: int,
    needle: str,
    suite: str | None = None,
) -> None:
    """Create a fixture repo, apply commits, and assert check_range verdict."""
    repo = create_fixture_repo(root, name)
    for rel_path, content, message in commits:
        write_commit(repo, rel_path, content, message)
    expect_case(repo, want_exit, needle, suite)


FEAT_COMMIT = ("api/a.go", "package a\n", "feat(api): add endpoint (#1)")
DEPS_COMMIT = ("go.sum", "example v2\n", "fix(deps): update module example to v2 (#7)")
SELF_TEST_CASES = (
    ("feat without test", "feat_untested", [FEAT_COMMIT], 1, "UNCOVERED "),
    (
        "feat with covers marker file",
        "feat_covered",
        [
            FEAT_COMMIT,
            (
                "web/e2e/features/1-endpoint.test.ts",
                "// covers: #1\ntest('endpoint', async () => {});\n",
                "test(web): cover endpoint (#2)",
            ),
        ],
        0,
        "covered ",
    ),
    (
        "fix touching features test in own diff",
        "fix_own_diff",
        [
            (
                "web/e2e/features/3-repair.test.ts",
                "test('repair', async () => {});\n",
                "fix(web): repair flow (#3)",
            )
        ],
        0,
        "covered ",
    ),
    (
        "covers #12 must NOT cover #1",
        "token_boundary",
        [
            FEAT_COMMIT,
            (
                "web/e2e/features/12-other.test.ts",
                "// covers: #12\ntest('other', async () => {});\n",
                "test(web): cover 12 (#13)",
            ),
        ],
        1,
        "UNCOVERED ",
    ),
    (
        "chore/docs ignored",
        "chore_docs",
        [
            ("README.md", "docs\n", "docs: clarify readme (#4)"),
            ("Makefile", "all:\n", "chore(ci): tidy (#5)"),
        ],
        0,
        "0 feat/fix commit(s)",
    ),
    (
        "marker file without test(",
        "marker_no_test",
        [
            FEAT_COMMIT,
            (
                "web/e2e/features/1-empty.test.ts",
                "// covers: #1\n",
                "test(web): add empty marker (#2)",
            ),
        ],
        1,
        "UNCOVERED ",
    ),
    (
        "fix(deps) with full suite green",
        "deps_suite_green",
        [DEPS_COMMIT],
        0,
        "covered-by-suite ",
        "green",
    ),
    (
        "fix(deps) with one suite project skipped",
        "deps_suite_skipped",
        [DEPS_COMMIT],
        1,
        "skipped project(s): features",
        "skipped",
    ),
    (
        "fix(deps) with suite on an older commit",
        "deps_suite_older",
        [DEPS_COMMIT],
        1,
        "e2e suite ran on 000000000000",
        "older",
    ),
    ("fix(deps) without suite report", "deps_no_report", [DEPS_COMMIT], 1, NO_SUITE_REPORT),
    ("feat not covered by green suite", "feat_suite_green", [FEAT_COMMIT], 1, "UNCOVERED ", "green"),
)


def self_test() -> None:
    """Verify the release e2e check against planted positive and negative cases."""
    with tempfile.TemporaryDirectory() as scratch:
        root = Path(scratch)
        for label, name, commits, want_exit, needle, *suite in SELF_TEST_CASES:
            try:
                check_case(root, name, commits, want_exit, needle, *suite)
            except (CheckError, SelfTestError) as error:
                raise SelfTestError(f"self-test '{label}': {error}") from error
    print(f"release e2e self-test: {len(SELF_TEST_CASES)} cases behaved as required")


def main(arguments: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path("."))
    parser.add_argument("--base", default=None)
    parser.add_argument("--head", default="HEAD")
    parser.add_argument("--self-test", action="store_true")
    report = os.environ.get("RELEASE_E2E_SUITE_REPORT")
    parser.add_argument("--suite-report", type=Path, default=Path(report) if report else None)
    parser.add_argument("--suite-commit", default=os.environ.get("RELEASE_E2E_SUITE_COMMIT"))
    options = parser.parse_args(arguments)
    try:
        if options.self_test:
            self_test()
            return 0
        suite_reason = suite_verdict(options.suite_report, options.suite_commit)
        uncovered, lines = check_range(
            options.repo.resolve(), options.base, options.head, suite_reason
        )
    except SelfTestError as error:
        print(f"release e2e self-test failed: {error}", file=sys.stderr)
        return 1
    except (CheckError, OSError, subprocess.SubprocessError) as error:
        print(f"release e2e check failed: {error}", file=sys.stderr)
        return 2
    for line in lines:
        print(line)
    return 1 if uncovered else 0


if __name__ == "__main__":
    sys.exit(main())
