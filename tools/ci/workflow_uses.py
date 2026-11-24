#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Extract uses references, list composite actions, or emit step text and version inputs.

Reads each file with yaml.compose so YAML anchors, aliases, merge keys and flow
collections resolve while every node keeps its 1-based start line. Default mode
prints one tab-separated line per remote reference (`owner/repo\\tpath:line`) from
`jobs.<job>.uses`, `jobs.<job>.steps[*].uses` and `runs.steps[*].uses`, skipping
`./`, `$/` and `docker://` values and enforcing MAX_REFS across the run.
`--all-uses FILE...` prints `<file>:<line>\\t<value>` for every decoded job-level
and step-level `uses` value, remote and local alike.
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
from workflow_yaml import (
    MAX_FILES,
    MAX_OUTPUT_BYTES,
    OutputBudget,
    UsesError,
    collect_files_uses,
    keep_uses_value,
    yaml,
)


def build_parser() -> argparse.ArgumentParser:
    """Build the command-line argument parser for workflow_uses.py."""
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument(
        "--self-test", action="store_true", help="run planted self-test cases"
    )
    mode.add_argument(
        "--all-uses",
        action="store_true",
        help="print every decoded job and step uses value as file:line\\tvalue",
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


def write_nul_items(items: list[str]) -> None:
    """Write NUL-terminated UTF-8 entries to stdout when items is non-empty."""
    if items:
        sys.stdout.buffer.write(
            b"\0".join(item.encode("utf-8") for item in items) + b"\0"
        )


def format_tsv_mode(
    args: argparse.Namespace, budget: OutputBudget | None = None
) -> list[str]:
    """Collect bounded tab-separated output lines for the selected non-NUL mode."""
    active = budget if budget is not None else OutputBudget(MAX_OUTPUT_BYTES)
    if args.all_uses:
        return [
            active.line((f"{file_path}:{line}", value))
            for value, file_path, line in collect_files_uses(
                args.files, decode=keep_uses_value
            )
        ]
    if args.step_text is not None:
        return [
            active.line((f"{rel_path}:{line_no}", line_text))
            for rel_path, line_no, line_text in collect_files_step_text(
                args.step_text, args.files
            )
        ]
    if args.version_inputs is not None:
        return [
            active.line((f"{rel_path}:{line_no}", repo, version))
            for rel_path, line_no, repo, version in collect_files_version_inputs(
                args.version_inputs, args.files
            )
        ]
    return [
        active.line((repo, f"{file_path}:{line}"))
        for repo, file_path, line in collect_files_uses(args.files)
    ]


def dispatch_mode(args: argparse.Namespace) -> None:
    """Execute --list-actions or print lines from the selected TSV mode."""
    if args.list_actions is not None:
        write_nul_items(list_action_files(args.list_actions, args.files))
        return
    for out_line in format_tsv_mode(args):
        print(out_line)


def main(argv: list[str] | None = None, *, yaml_mod: object = yaml) -> int:
    """Run --self-test, --all-uses, --list-actions, --step-text, --version-inputs or print uses."""
    if yaml_mod is None:
        print(
            "error: PyYAML is required; run tools/ci/pyyaml_venv.sh DIR"
            " and set PYTHON_BIN=DIR/bin/python",
            file=sys.stderr,
        )
        return 2
    args = build_parser().parse_args(argv)
    if args.self_test:
        from workflow_uses_selftest import self_test

        return self_test()
    if not args.files or len(args.files) > MAX_FILES:
        print(
            "usage: workflow_uses.py"
            " [--self-test | --all-uses | --list-actions ROOT | --step-text ROOT"
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
