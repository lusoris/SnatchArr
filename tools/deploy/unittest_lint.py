# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Require any: true for mapping notContains assertions in Helm unit tests.

Usage: python3 -I tools/deploy/unittest_lint.py [FILE ... | --self-test]
Exit 0 = accepted, 1 = refused or unreadable input, 2 = usage error.
"""

from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
FIXTURES = ROOT / "tools/deploy/testdata/unittest_lint"
MAX_ITEMS = 1024


def check_test(path: Path, test: object) -> int:
    """Refuse mapping notContains without partial matching; bound each assertion list."""
    if not isinstance(test, dict) or not isinstance(test.get("asserts"), list):
        raise ValueError("expected a test with an asserts list")
    if len(test["asserts"]) > MAX_ITEMS:
        raise ValueError(f"expected at most {MAX_ITEMS} assertions per test")
    status = 0
    for assertion in test["asserts"]:
        if not isinstance(assertion, dict):
            raise ValueError("expected an assertion mapping")
        if "notContains" not in assertion:
            continue
        spec = assertion["notContains"]
        if not isinstance(spec, dict):
            raise ValueError("expected a notContains mapping")
        if isinstance(spec.get("content"), dict) and spec.get("any") is not True:
            print(f"{path}: {test.get('it', '<unnamed>')}: {spec.get('path', '<missing path>')}: "
                  "mapping notContains requires any: true")
            status = 1
    return status


def check_file(path: Path) -> int:
    """Print each unsafe assertion's file, test name and assertion path."""
    try:
        suite = yaml.safe_load(path.read_text(encoding="utf-8"))
        if not isinstance(suite, dict) or not isinstance(suite.get("tests"), list):
            raise ValueError("expected a suite with a tests list")
        if len(suite["tests"]) > MAX_ITEMS:
            raise ValueError(f"expected at most {MAX_ITEMS} tests per suite")
        return max((check_test(path, test) for test in suite["tests"]), default=0)
    except (OSError, UnicodeError, ValueError, yaml.YAMLError) as error:
        print(f"{path}: refused: {error}", file=sys.stderr)
        return 1


def self_test() -> int:
    """Replay both fixture CLI exit codes; never accept an unexpected failure."""
    status = 0
    for name, expected in (("good.yaml", 0), ("bad.yaml", 1)):
        try:
            result = subprocess.run(
                [sys.executable, "-I", str(Path(__file__).resolve()), str(FIXTURES / name)],
                capture_output=True, text=True, check=False, timeout=30,
            )
        except (OSError, subprocess.SubprocessError) as error:
            print(f"self-test {name}: {error}", file=sys.stderr)
            status = 1
            continue
        print(f"self-test {name}: exit {result.returncode}, expected {expected}")
        if result.returncode != expected:
            sys.stderr.write(result.stdout + result.stderr)
            status = 1
    return status


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
