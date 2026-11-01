#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse a release-please configuration whose pull request body lacks the template sections.

CodeRabbit's description check compares every pull request body with
.github/pull_request_template.md; release-please writes its own body, which warned on #957.
This check reads the template's level-two headings and checklist items and requires each one in
the release-please pull-request-header plus pull-request-footer. Exit 0 = covered, 1 = missing,
2 = a file could not be read.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

MAX_TEMPLATE_LINES = 500


def unique_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    """Build a dict from JSON pairs, refusing a duplicate key."""
    seen: dict[str, object] = {}
    for key, value in pairs:
        if key in seen:
            raise ValueError(f"duplicate key {key!r}")
        seen[key] = value
    return seen


def required_lines(template: str) -> list[str]:
    """Return the template's level-two headings and checklist items."""
    out = []
    for line in template.splitlines()[:MAX_TEMPLATE_LINES]:
        text = line.strip()
        if text.startswith(("## ", "- [ ] ", "- [x] ")):
            out.append(text)
    return out


def effective(config: dict[str, object], pkg: str | None, key: str) -> str:
    """Return the effective header or footer string for pkg, falling back to root config."""
    if pkg is not None:
        packages = config.get("packages")
        if isinstance(packages, dict):
            pkg_config = packages.get(pkg)
            if isinstance(pkg_config, dict) and key in pkg_config and pkg_config[key] is not None:
                return str(pkg_config[key])
    return str(config.get(key) or "")


def validate_packages(config: dict[str, object], config_path: str) -> str | None:
    """Validate that packages is a dict of dicts if present."""
    packages = config.get("packages")
    if packages is not None and (
        not isinstance(packages, dict) or any(not isinstance(v, dict) for v in packages.values())
    ):
        return f"{config_path} packages must be a dict of dicts"
    return None


def missing(config: dict[str, object], template: str) -> list[tuple[str | None, str]]:
    """List (package or None, template line) for template lines missing from header or footer."""
    packages = config.get("packages")
    req = required_lines(template)
    target_pkgs: list[str | None] = list(packages.keys()) if isinstance(packages, dict) and packages else [None]
    gaps: list[tuple[str | None, str]] = []
    for pkg in target_pkgs:
        header = effective(config, pkg, "pull-request-header")
        footer = effective(config, pkg, "pull-request-footer")
        body_lines = {line.strip() for text in (header, footer) for line in text.splitlines()}
        for line in req:
            if line not in body_lines:
                gaps.append((pkg, line))
    return gaps


def main() -> int:
    """Parse arguments, compare config and template, print what is missing."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--config", default="release-please-config.json")
    parser.add_argument("--template", default=".github/pull_request_template.md")
    args = parser.parse_args()
    try:
        config = json.loads(Path(args.config).read_text(encoding="utf-8"), object_pairs_hook=unique_keys)
        template = Path(args.template).read_text(encoding="utf-8")
    except (OSError, ValueError) as exc:
        print(f"release_body_check: cannot run: {exc}", file=sys.stderr)
        return 2
    if not isinstance(config, dict):
        print(f"release_body_check: cannot run: {args.config} is not a JSON object", file=sys.stderr)
        return 2
    err = validate_packages(config, args.config)
    if err:
        print(f"release_body_check: cannot run: {err}", file=sys.stderr)
        return 2
    if not required_lines(template):
        print("release_body_check: cannot run: the template has no heading or checklist item", file=sys.stderr)
        return 2
    gaps = missing(config, template)
    for pkg, gap in gaps:
        scope = f" (package {pkg})" if pkg is not None else ""
        print(f"{args.config}: release pull request body lacks template line{scope}: {gap}")
    if gaps:
        return 1
    print("release_body_check: ok (release pull request body carries every template section)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
