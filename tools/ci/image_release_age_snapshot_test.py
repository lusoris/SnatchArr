# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Plant deterministic changed-snapshot cases for both release-age read modes."""

from __future__ import annotations

import argparse
import contextlib
import io
import sys
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))

import image_release_age_check as check_module
from renovate_rules import JudgeResult

CONFIG = b'{"customManagers": [{"managerFilePatterns": ["managed.env"]}]}'


def expect(condition: bool, detail: object) -> None:
    """Fail a planted case with DETAIL when CONDITION is false."""
    if not condition:
        raise AssertionError(detail)


def check_outcome(
    case: str,
    index: bool,
    status: int,
    reads: list[bool],
    sleeps: list[float],
    judged: list[check_module.JudgeRequest],
    stdout: str,
    stderr: str,
) -> None:
    """Verify status, fresh reads, retry delays, judge count and diagnostics."""
    expected_reads = {"P1": 4, "P2": 6, "P3": 2}[case]
    expected_sleeps = {"P1": [2.0], "P2": [2.0, 2.0], "P3": []}[case]
    expect(status == (3 if case == "P2" else 0), (case, status, stdout))
    expect(reads == [index] * expected_reads, (case, index, reads))
    expect(sleeps == expected_sleeps, (case, sleeps))
    expect(len(judged) == (0 if case == "P2" else 1), (case, len(judged)))
    if judged:
        expect(judged[0].files == {"managed.env": b"stable"}, judged[0].files)
    retry_count = {"P1": 1, "P2": 2, "P3": 0}[case]
    expect(stderr.count("tree changed during check, attempt") == retry_count, stderr)
    if case == "P1":
        expect("attempt 1 of 3, reading again in 2 s" in stderr, stderr)
    if case == "P2":
        expect("tree changed during check (3 attempts)" in stdout, stdout)


def run_case(case: str, index: bool) -> None:
    """Exercise one stable or changed two-pass snapshot with a stub reader and sleep."""
    reads: list[bool] = []
    sleeps: list[float] = []
    judged: list[check_module.JudgeRequest] = []
    stdout = io.StringIO()
    stderr = io.StringIO()

    def reader(_root: Path, *, index: bool = False):
        """Yield an unrelated changed file on selected second passes only."""
        reads.append(index)
        pass2 = len(reads) % 2 == 0
        changed = pass2 and (case == "P2" or (case == "P1" and len(reads) == 2))
        yield "renovate.json", CONFIG
        yield "managed.env", b"stable"
        yield "unrelated.txt", b"partial" if changed else b"stable"

    def sleep(seconds: float) -> None:
        """Record retry delays without waiting."""
        sleeps.append(seconds)

    def judge(request: check_module.JudgeRequest) -> JudgeResult:
        """Record the single request sent to the judge after a stable read."""
        judged.append(request)
        return JudgeResult((), ("postgres",), None, {})

    # The origin/main checker has no retry helper; patch its reader directly so P1 tests
    # the old snapshot refusal rather than failing because a new API is absent.
    with contextlib.ExitStack() as stack:
        stack.enter_context(
            mock.patch.object(check_module.image_pin_check, "read_contents", reader)
        )
        stack.enter_context(mock.patch.object(check_module, "judge_in_child", judge))
        if hasattr(check_module, "read_stable_tree"):
            original = check_module.read_stable_tree

            def injected(root: Path, mode: bool):
                """Supply the stub reader and sleep to the production retry helper."""
                return original(root, mode, reader=reader, sleep=sleep)

            stack.enter_context(
                mock.patch.object(check_module, "read_stable_tree", injected)
            )
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            if case == "P2":
                argv = ["--root", "."] + (["--index"] if index else [])
                status = check_module.main(argv)
            else:
                status = check_module.check(Path("."), index=index)

    check_outcome(
        case, index, status, reads, sleeps, judged, stdout.getvalue(), stderr.getvalue()
    )
    print(f"ok   snapshot-{case}-{('index' if index else 'worktree')}")


def main() -> None:
    """Run all planted cases, or one selected case for the origin/main red check."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--case", choices=("P1", "P2", "P3"))
    args = parser.parse_args()
    cases = (args.case,) if args.case else ("P1", "P2", "P3")
    for case in cases:
        for index in (False, True):
            run_case(case, index)


if __name__ == "__main__":
    main()
