#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Extract remote uses, list composite actions, or emit step text and version inputs.

Reads each file with yaml.compose so YAML anchors, aliases, merge keys and flow
collections resolve while every node keeps its 1-based start line. Default mode
prints one tab-separated line per remote reference (`owner/repo\\tpath:line`) from
`jobs.<job>.uses`, `jobs.<job>.steps[*].uses` and `runs.steps[*].uses`, skipping
`./`, `$/` and `docker://` values and enforcing MAX_REFS across the run.
`--list-actions ROOT FILE...` prints NUL-separated repo-relative paths of every
action.yml / action.yaml under ROOT/.github/actions plus composite actions reached
from step-level `./<dir>` and `$/<dir>`, skipping Docker-only local actions.
`--step-text ROOT FILE...` prints `<file>:<line>\\t<text>` for scalar value lines
of step `run` and scalar `with` entries. `--version-inputs ROOT FILE...` prints
`<file>:<line>\\t<owner/repo>\\t<version>` for remote step `uses` references.
Exits 2 on missing PyYAML or validation error.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from workflow_actions import (
    collect_files_step_text,
    collect_files_version_inputs,
    list_action_files,
)
from workflow_yaml import MAX_FILES, UsesError, collect_files_uses, yaml


def build_parser() -> argparse.ArgumentParser:
    """Build the command-line argument parser for workflow_uses.py."""
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument(
        "--self-test", action="store_true", help="run planted self-test cases"
    )
    mode.add_argument(
        "--list-actions",
        metavar="ROOT",
        help="print NUL-separated composite action paths under ROOT",
    )
    mode.add_argument(
        "--step-text",
        metavar="ROOT",
        help="print step run and with scalar lines under ROOT as file:line\\ttext",
    )
    mode.add_argument(
        "--version-inputs",
        metavar="ROOT",
        help="print step uses version inputs under ROOT as file:line\\trepo\\tversion",
    )
    parser.add_argument(
        "files", nargs="*", help="workflow or composite action YAML files"
    )
    return parser


def dispatch_mode(args: argparse.Namespace) -> None:
    """Execute --list-actions, --step-text, --version-inputs or remote uses output."""
    if args.list_actions is not None:
        listed = list_action_files(args.list_actions, args.files)
        if listed:
            sys.stdout.buffer.write(
                b"\0".join(item.encode("utf-8") for item in listed) + b"\0"
            )
        return
    if args.step_text is not None:
        for rel_path, line_no, line_text in collect_files_step_text(
            args.step_text, args.files
        ):
            print(f"{rel_path}:{line_no}\t{line_text}")
        return
    if args.version_inputs is not None:
        for rel_path, line_no, repo, version in collect_files_version_inputs(
            args.version_inputs, args.files
        ):
            print(f"{rel_path}:{line_no}\t{repo}\t{version}")
        return
    for repo, file_path, line in collect_files_uses(args.files):
        print(f"{repo}\t{file_path}:{line}")


def main(argv: list[str] | None = None) -> int:
    """Run --self-test, --list-actions, --step-text, --version-inputs or print uses."""
    if yaml is None:
        print("error: PyYAML is required", file=sys.stderr)
        return 2
    args = build_parser().parse_args(argv)
    if args.self_test:
        from workflow_uses_selftest import self_test

        return self_test()
    if not args.files or len(args.files) > MAX_FILES:
        print(
            "usage: workflow_uses.py"
            " [--self-test | --list-actions ROOT | --step-text ROOT"
            " | --version-inputs ROOT] FILE [FILE ...]",
            file=sys.stderr,
        )
        return 2
    try:
        dispatch_mode(args)
    except UsesError as err:
        print(f"error: {err}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
