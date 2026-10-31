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
    """Return the template's level-two headings and checklist item texts."""
    out = []
    for line in template.splitlines()[:MAX_TEMPLATE_LINES]:
        text = line.strip()
        if text.startswith("## "):
            out.append(text)
        elif text.startswith(("- [ ] ", "- [x] ")):
            out.append(text[6:])
    return out


def missing(config: dict[str, object], template: str) -> list[str]:
    """List template lines that neither the release pull request header nor footer carries."""
    body = f"{config.get('pull-request-header', '')}\n{config.get('pull-request-footer', '')}"
    return [line for line in required_lines(template) if line not in body]


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
    gaps = missing(config, template)
    if not required_lines(template):
        print("release_body_check: cannot run: the template has no heading or checklist item", file=sys.stderr)
        return 2
    for gap in gaps:
        print(f"{args.config}: release pull request body lacks template line: {gap}")
    if gaps:
        return 1
    print("release_body_check: ok (release pull request body carries every template section)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
