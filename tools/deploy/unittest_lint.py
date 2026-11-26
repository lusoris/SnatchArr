# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Require any: true for mapping notContains assertions in Helm unit tests.

Usage: python3 -I tools/deploy/unittest_lint.py [FILE ... | --self-test]
Exit 0 = accepted, 1 = refused or unreadable input, 2 = usage error.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
FIXTURES = ROOT / "tools/deploy/testdata/unittest_lint"
MAX_ITEMS = 1024


def check_test(path: Path, test: object) -> int:
    """Refuse mapping notContains without partial matching; bound each assertion list."""
    if not isinstance(test, dict) or not isinstance(test.get("asserts"), list):
        raise TypeError("expected a test with an asserts list")
    if len(test["asserts"]) > MAX_ITEMS:
        raise ValueError(f"expected at most {MAX_ITEMS} assertions per test")
    status = 0
    for assertion in test["asserts"]:
        if not isinstance(assertion, dict):
            raise TypeError("expected an assertion mapping")
        if "notContains" not in assertion:
            continue
        spec = assertion["notContains"]
        if not isinstance(spec, dict):
            raise TypeError("expected a notContains mapping")
        if isinstance(spec.get("content"), dict) and spec.get("any") is not True:
            print(f"{path}: {test.get('it', '<unnamed>')}: {spec.get('path', '<missing path>')}: "
                  "mapping notContains requires any: true")
            status = 1
    return status


def check_file(path: Path) -> int:
    """Print each unsafe assertion's file, test name and assertion path."""
    try:
        suite = yaml.safe_load(path.read_text(encoding="utf-8"))
        return check_suite(path, suite)
    except (OSError, UnicodeError, TypeError, ValueError, yaml.YAMLError) as error:
        print(f"{path}: refused: {error}", file=sys.stderr)
        return 1


def check_suite(path: Path, suite: object) -> int:
    """Validate a parsed suite and check its tests."""
    if not isinstance(suite, dict) or not isinstance(suite.get("tests"), list):
        raise TypeError("expected a suite with a tests list")
    if len(suite["tests"]) > MAX_ITEMS:
        raise ValueError(f"expected at most {MAX_ITEMS} tests per suite")
    return max((check_test(path, test) for test in suite["tests"]), default=0)


def self_test() -> int:
    """Replay both fixtures in-process; never accept an unexpected failure."""
    fixtures = (
        (
            "good.yaml",
            0,
            (
                "mapping content matches any containing entry",
                "scalar and list content may use whole-item matching",
            ),
        ),
        (
            "bad.yaml",
            1,
            (
                "mapping content lacks any",
                "mapping content has any false",
                "mapping content has a string instead of boolean true",
                "empty mapping content also requires any true",
            ),
        ),
    )
    status = 0
    for name, expected, expected_cases in fixtures:
        path = FIXTURES / name
        result = check_file(path)
        print(f"self-test {name}: exit {result}, expected {expected}")
        if result != expected:
            status = 1
        if _self_test_cases(path, expected_cases, expected):
            status = 1
    return status


def _self_test_cases(
    path: Path, expected_cases: tuple[str, ...], expected: int
) -> bool:
    """Check each fixture test independently; return true on any failure."""
    try:
        suite = yaml.safe_load(path.read_text(encoding="utf-8"))
        if not isinstance(suite, dict) or not isinstance(suite.get("tests"), list):
            raise TypeError("expected a suite with a tests list")
        tests = suite["tests"]
        if len(tests) > MAX_ITEMS:
            raise ValueError(f"expected at most {MAX_ITEMS} tests per suite")
        cases = []
        for test in tests:
            if not isinstance(test, dict) or not isinstance(test.get("it"), str):
                raise TypeError("expected a test with an it string")
            cases.append(test["it"])
    except (OSError, UnicodeError, TypeError, ValueError, yaml.YAMLError) as error:
        print(f"self-test {path.name}: {error}", file=sys.stderr)
        return True
    if tuple(cases) != expected_cases or len(set(cases)) != len(cases):
        missing = [case for case in expected_cases if case not in cases]
        extra = [case for case in cases if case not in expected_cases]
        duplicates = [case for case in dict.fromkeys(cases) if cases.count(case) > 1]
        print(
            f"self-test {path.name}: missing {missing!r}, extra {extra!r}, "
            f"duplicated {duplicates!r}. Expected order {expected_cases!r}, got {cases!r}",
            file=sys.stderr,
        )
        return True
    failed = False
    for number, (case, test) in enumerate(zip(cases, tests, strict=True), start=1):
        exception = False
        try:
            result = check_suite(path, {"tests": [test]})
        except (TypeError, ValueError) as error:
            print(
                f"self-test {path.name} case {number} '{case}' exception: {error}",
                file=sys.stderr,
            )
            result = 1
            exception = True
        print(
            f"self-test {path.name} case {number} '{case}': exit {result}, expected {expected}"
        )
        if exception or result != expected:
            failed = True
    return failed


def main() -> int:
    """Check explicit files, or every chart test by default; replay fixtures on request."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("files", nargs="*", type=Path)
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        if args.files:
            parser.error("--self-test does not accept files")
        return self_test()
    paths = args.files or sorted((ROOT / "deploy/helm/snatcharr/tests").glob("*_test.yaml"))
    if not paths:
        print("refused: no Helm unit test files found", file=sys.stderr)
        return 1
    if len(paths) > MAX_ITEMS:
        print(f"refused: expected at most {MAX_ITEMS} files", file=sys.stderr)
        return 1
    statuses = [check_file(path) for path in paths]
    status = max(statuses)
    if status == 0:
        print(f"Helm unittest lint: {len(paths)} files accepted")
    return status


if __name__ == "__main__":
    sys.exit(main())
