# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Require root Go manifests in the deploy (Helm) workflow path filter."""

from __future__ import annotations

import argparse
import fnmatch
import re
import sys
from pathlib import Path

WORKFLOW = Path(".github/workflows/ci.yml")
REQUIRED = ("go.mod", "go.sum")
FILTER_HEADER = re.compile(r"^(\s{12})deploy:\s*$")
PATTERN = re.compile(r"^\s+-\s+['\"]([^'\"]+)['\"]\s*$")


def deploy_patterns(source: str) -> list[str]:
    """Read only the deploy path list inside dorny/paths-filter's literal block."""
    patterns: list[str] = []
    inside = False
    for line in source.splitlines():
        if not inside:
            inside = FILTER_HEADER.match(line) is not None
            continue
        if line.strip() and len(line) - len(line.lstrip()) <= 12:
            break
        match = PATTERN.match(line)
        if match:
            patterns.append(match.group(1))
    return patterns


def missing_manifests(patterns: list[str]) -> list[str]:
    """Return root Go manifests unmatched by these deploy filter patterns."""
    return [path for path in REQUIRED if not any(fnmatch.fnmatchcase(path, pattern) for pattern in patterns)]


def self_test(source: str) -> bool:
    """Plant omission of both Go manifests and require both failure names."""
    patterns = deploy_patterns(source)
    if missing_manifests(patterns):
        print("deploy filter self-test: clean workflow is missing a root Go manifest", file=sys.stderr)
        return False
    planted = [pattern for pattern in patterns if pattern not in REQUIRED]
    missing = missing_manifests(planted)
    if missing != list(REQUIRED):
        print(f"deploy filter self-test: planted omission yielded {missing!r}", file=sys.stderr)
        return False
    print(f"deploy filter self-test: planted omission rejected: {', '.join(missing)}")
    return True


def main() -> int:
    """Check the workflow or its planted negative case."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    try:
        source = WORKFLOW.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as error:
        print(f"deploy filter: read {WORKFLOW}: {error}", file=sys.stderr)
        return 2
    if args.self_test:
        return 0 if self_test(source) else 1
    missing = missing_manifests(deploy_patterns(source))
    if missing:
        print(f"deploy filter: {WORKFLOW}: missing path match for {', '.join(missing)}", file=sys.stderr)
        return 1
    print("deploy filter: root Go manifests match")
    return 0


if __name__ == "__main__":
    sys.exit(main())
