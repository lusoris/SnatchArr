#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Extract remote GitHub Actions uses references from workflow and composite action YAML files.

Reads each file on argv with yaml.compose so YAML anchors, aliases, merge keys and flow
collections resolve while every node keeps its 1-based start line. Prints one tab-separated
line per remote reference (`owner/repo\\tpath:line`) from `jobs.<job>.uses`,
`jobs.<job>.steps[*].uses` and `runs.steps[*].uses`, skipping `./` and `docker://` values and
never inspecting `run:` scripts. Exits 2 when PyYAML is missing, a file fails to parse, or a
remote reference is not `owner/repo[/path]@ref` with owner and repo in `[A-Za-z0-9_.-]`.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    yaml = None

MERGE_TAG = "tag:yaml.org,2002:merge"
NULL_TAG = "tag:yaml.org,2002:null"
STR_TAG = "tag:yaml.org,2002:str"
NAME_RE = re.compile(r"^[A-Za-z0-9_.-]+$")
DOT_NAMES = frozenset({".", ".."})

MAX_FILES = 4096
MAX_JOBS = 1024
MAX_STEPS = 4096
MAX_MAPPING_PAIRS = 4096
MAX_MERGE_NODES = 1024


class UsesError(Exception):
    """Raised when a workflow or composite action file fails YAML or uses validation."""


def node_line(key_node: yaml.Node, val_node: yaml.Node) -> int:
    """Return the 1-based source line for a mapping entry's value, or its key when empty."""
    if (
        isinstance(val_node, yaml.ScalarNode)
        and val_node.tag == NULL_TAG
        and val_node.value == ""
    ):
        return key_node.start_mark.line + 1
    return val_node.start_mark.line + 1


def merge_sources(val_node: yaml.Node, path: str) -> list[yaml.MappingNode]:
    """Unpack a YAML merge target (one mapping or a sequence of mappings)."""
    if isinstance(val_node, yaml.MappingNode):
        return [val_node]
    if isinstance(val_node, yaml.SequenceNode):
        if len(val_node.value) > MAX_MERGE_NODES:
            raise UsesError(
                f"{path}:{val_node.start_mark.line + 1}: too many merge items"
            )
        out: list[yaml.MappingNode] = []
        for item in val_node.value:
            if not isinstance(item, yaml.MappingNode):
                raise UsesError(
                    f"{path}:{item.start_mark.line + 1}: merge item is not a mapping"
                )
            out.append(item)
        return out
    raise UsesError(
        f"{path}:{val_node.start_mark.line + 1}: merge value is not a mapping"
    )


def explicit_entries(
    node: yaml.MappingNode, path: str
) -> tuple[dict[str, tuple[yaml.ScalarNode, yaml.Node]], list[yaml.MappingNode]]:
    """Read direct scalar keys (refusing duplicates) and queued merge mappings from node."""
    if len(node.value) > MAX_MAPPING_PAIRS:
        raise UsesError(
            f"{path}:{node.start_mark.line + 1}: mapping exceeds {MAX_MAPPING_PAIRS} entries"
        )
    entries: dict[str, tuple[yaml.ScalarNode, yaml.Node]] = {}
    merges: list[yaml.MappingNode] = []
    seen_keys: set[tuple[str, str]] = set()
    for key_node, val_node in node.value:
        if not isinstance(key_node, yaml.ScalarNode):
            raise UsesError(
                f"{path}:{key_node.start_mark.line + 1}: mapping key is not a scalar"
            )
        if key_node.tag == MERGE_TAG:
            merges.extend(merge_sources(val_node, path))
            continue
        ident = (key_node.tag, key_node.value)
        if ident in seen_keys:
            raise UsesError(
                f"{path}:{key_node.start_mark.line + 1}: duplicate key {key_node.value!r}"
            )
        seen_keys.add(ident)
        if key_node.tag == STR_TAG:
            entries[key_node.value] = (key_node, val_node)
    return entries, merges


def resolve_mapping(
    node: yaml.MappingNode, path: str
) -> dict[str, tuple[yaml.ScalarNode, yaml.Node]]:
    """Resolve a YAML mapping node including merge keys without recursion."""
    resolved, initial_merges = explicit_entries(node, path)
    stack = list(reversed(initial_merges))
    visited: set[int] = {id(node)}
    steps = 0
    while stack:
        steps += 1
        if steps > MAX_MERGE_NODES:
            raise UsesError(
                f"{path}:{node.start_mark.line + 1}: merge depth exceeds {MAX_MERGE_NODES}"
            )
        cur = stack.pop()
        if id(cur) in visited:
            continue
        visited.add(id(cur))
        sub_entries, sub_merges = explicit_entries(cur, path)
        for key, pair in sub_entries.items():
            resolved.setdefault(key, pair)
        stack.extend(reversed(sub_merges))
    return resolved


def valid_repo_part(part: str) -> bool:
    """Report whether an owner or repository name segment uses only allowed characters."""
    return bool(NAME_RE.fullmatch(part)) and part not in DOT_NAMES


def parse_uses_value(raw: str, path: str, line: int) -> str | None:
    """Validate a uses string and return owner/repo, or None for local and docker references."""
    if raw.startswith(("./", "docker://")):
        return None
    target, sep, ref = raw.partition("@")
    if not sep or not ref or "@" in ref or any(ch.isspace() for ch in raw):
        raise UsesError(f"{path}:{line}: invalid uses reference {raw!r}")
    parts = target.split("/")
    if len(parts) < 2 or not all(parts):
        raise UsesError(f"{path}:{line}: invalid uses reference {raw!r}")
    owner, repo = parts[0], parts[1]
    if not valid_repo_part(owner) or not valid_repo_part(repo):
        raise UsesError(f"{path}:{line}: invalid repository in uses reference {raw!r}")
    return f"{owner}/{repo}"


def extract_mapping_uses(
    mapping: yaml.MappingNode, path: str
) -> tuple[dict[str, tuple[yaml.ScalarNode, yaml.Node]], tuple[str, int] | None]:
    """Resolve mapping and parse its optional uses key into (owner/repo, line)."""
    fields = resolve_mapping(mapping, path)
    if "uses" not in fields:
        return fields, None
    key_node, val_node = fields["uses"]
    line = node_line(key_node, val_node)
    if not isinstance(val_node, yaml.ScalarNode) or val_node.tag != STR_TAG:
        raise UsesError(f"{path}:{line}: uses value must be a string")
    repo = parse_uses_value(val_node.value, path, line)
    return fields, (repo, line) if repo is not None else None


def collect_steps_uses(steps_node: yaml.Node, path: str) -> list[tuple[str, int]]:
    """Collect remote (owner/repo, line) pairs from a steps sequence node."""
    if isinstance(steps_node, yaml.ScalarNode) and steps_node.tag == NULL_TAG:
        return []
    if not isinstance(steps_node, yaml.SequenceNode):
        raise UsesError(
            f"{path}:{steps_node.start_mark.line + 1}: steps must be a sequence"
        )
    if len(steps_node.value) > MAX_STEPS:
        raise UsesError(
            f"{path}:{steps_node.start_mark.line + 1}: steps exceeds {MAX_STEPS} entries"
        )
    found: list[tuple[str, int]] = []
    for step_node in steps_node.value:
        if not isinstance(step_node, yaml.MappingNode):
            raise UsesError(
                f"{path}:{step_node.start_mark.line + 1}: step must be a mapping"
            )
        _, hit = extract_mapping_uses(step_node, path)
        if hit is not None:
            found.append(hit)
    return found


def collect_jobs_uses(jobs_node: yaml.Node, path: str) -> list[tuple[str, int]]:
    """Collect remote (owner/repo, line) pairs from jobs.<job>.uses and jobs.<job>.steps[*].uses."""
    if isinstance(jobs_node, yaml.ScalarNode) and jobs_node.tag == NULL_TAG:
        return []
    if not isinstance(jobs_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{jobs_node.start_mark.line + 1}: jobs must be a mapping"
        )
    jobs = resolve_mapping(jobs_node, path)
    if len(jobs) > MAX_JOBS:
        raise UsesError(
            f"{path}:{jobs_node.start_mark.line + 1}: jobs exceeds {MAX_JOBS} entries"
        )
    found: list[tuple[str, int]] = []
    for _, job_node in jobs.values():
        if not isinstance(job_node, yaml.MappingNode):
            raise UsesError(
                f"{path}:{job_node.start_mark.line + 1}: job must be a mapping"
            )
        job_fields, hit = extract_mapping_uses(job_node, path)
        if hit is not None:
            found.append(hit)
        if "steps" in job_fields:
            found.extend(collect_steps_uses(job_fields["steps"][1], path))
    return found


def collect_text_uses(text: str, path: str) -> list[tuple[str, int]]:
    """Compose YAML text for path and return remote (owner/repo, line) pairs."""
    try:
        root = yaml.compose(text, Loader=yaml.SafeLoader)
    except yaml.YAMLError as err:
        raise UsesError(f"{path}: {err}") from err
    if not isinstance(root, yaml.MappingNode):
        raise UsesError(f"{path}: document root is not a YAML mapping")
    top = resolve_mapping(root, path)
    found: list[tuple[str, int]] = []
    if "jobs" in top:
        found.extend(collect_jobs_uses(top["jobs"][1], path))
    if "runs" in top:
        runs_node = top["runs"][1]
        if not isinstance(runs_node, yaml.MappingNode):
            raise UsesError(
                f"{path}:{runs_node.start_mark.line + 1}: runs must be a mapping"
            )
        runs_fields = resolve_mapping(runs_node, path)
        if "steps" in runs_fields:
            found.extend(collect_steps_uses(runs_fields["steps"][1], path))
    return found


def collect_file_uses(path: str) -> list[tuple[str, int]]:
    """Read path as UTF-8 and return remote (owner/repo, line) pairs."""
    try:
        text = Path(path).read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as err:
        raise UsesError(f"{path}: {err}") from err
    return collect_text_uses(text, path)


def run_positive_cases() -> list[str]:
    """Run planted positive self-test cases and return failure descriptions."""
    workflow_doc = (
        "on: push\njobs:\n  call:\n    uses: org.1/reusable/.github/workflows/w.yml@v1\n"
        "  build:\n    runs-on: ubuntu-26.04\n    env:\n      uses: ignored/env@v1\n"
        "    steps:\n      - uses: ./local/action\n      - uses: docker://alpine:3.20\n"
        "      - name: script\n        run: |\n          uses: moved/in-run@v1\n"
        "      - uses: act_1/step-repo/sub@deadbeef # v1\n"
    )
    anchor_doc = (
        "x-ref: &ref a-owner/a-repo@v1\nx-step: &step\n  uses: b.owner/b_repo@v2\n"
        "jobs:\n  j:\n    steps:\n      - uses: *ref\n      - *step\n"
        "      - <<: *step\n        name: merged\n      - {name: f, uses: c/d@v3}\n"
    )
    cases = [
        (
            "workflow-jobs-steps-and-skips",
            workflow_doc,
            [("org.1/reusable", 4), ("act_1/step-repo", 15)],
        ),
        (
            "anchors-aliases-merges-and-flow",
            anchor_doc,
            [
                ("a-owner/a-repo", 1),
                ("b.owner/b_repo", 3),
                ("b.owner/b_repo", 3),
                ("c/d", 11),
            ],
        ),
        (
            "composite-action-runs",
            "name: c\nruns:\n  using: composite\n  steps:\n    - uses: comp/act@v1\n",
            [("comp/act", 5)],
        ),
    ]
    failures: list[str] = []
    for name, text, want in cases:
        try:
            got = collect_text_uses(text, "case.yml")
        except UsesError as err:
            failures.append(f"{name}: unexpected error: {err}")
            continue
        if got != want:
            failures.append(f"{name}: got {got!r}, want {want!r}")
        else:
            print(f"ok   {name}")
    return failures


def run_negative_cases() -> list[str]:
    """Run planted negative self-test cases and return failure descriptions."""
    cases = [
        ("invalid-yaml", "jobs: [\n", "bad.yml"),
        ("non-mapping-root", "- item\n", "list.yml"),
        (
            "duplicate-key",
            "jobs:\n  j:\n    steps:\n      - uses: a/b@v1\n        uses: c/d@v1\n",
            "dup.yml:5",
        ),
        ("missing-ref", "jobs:\n  j:\n    uses: owner/repo\n", "noref.yml:3"),
        ("empty-ref", "jobs:\n  j:\n    uses: owner/repo@\n", "emptyref.yml:3"),
        (
            "bad-owner-charset",
            "jobs:\n  j:\n    uses: bad$owner/repo@v1\n",
            "owner.yml:3",
        ),
        (
            "bad-repo-charset",
            'jobs:\n  j:\n    uses: "owner/bad repo@v1"\n',
            "repo.yml:3",
        ),
        ("empty-subpath", "jobs:\n  j:\n    uses: owner/repo/@v1\n", "sub.yml:3"),
        (
            "dotdot-owner",
            "jobs:\n  j:\n    uses: ../repo@v1\n",
            "dot.yml:3",
        ),
        (
            "dotdot-repo",
            "jobs:\n  j:\n    uses: owner/..@v1\n",
            "dotrepo.yml:3",
        ),
        (
            "non-scalar-uses",
            "jobs:\n  j:\n    uses: [a/b@v1]\n",
            "listuses.yml:3",
        ),
        (
            "null-uses",
            "jobs:\n  j:\n    steps:\n      - uses:\n",
            "null.yml:4",
        ),
    ]
    failures: list[str] = []
    for name, text, needle in cases:
        path = needle.split(":", 1)[0]
        try:
            got = collect_text_uses(text, path)
            failures.append(f"{name}: accepted {got!r}, expected error with {needle!r}")
        except UsesError as err:
            if needle not in str(err):
                failures.append(f"{name}: error {str(err)!r} lacks {needle!r}")
            else:
                print(f"ok   {name}")
    return failures


def self_test() -> int:
    """Run all planted positive and negative cases; exit 1 naming any failed case."""
    failures = [*run_positive_cases(), *run_negative_cases()]
    for msg in failures:
        print(f"FAIL {msg}", file=sys.stderr)
    return 1 if failures else 0


def main(argv: list[str] | None = None) -> int:
    """Run --self-test or print remote uses references for the files on argv."""
    if yaml is None:
        print("error: PyYAML is required", file=sys.stderr)
        return 2
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--self-test", action="store_true", help="run planted self-test cases"
    )
    parser.add_argument(
        "files", nargs="*", help="workflow or composite action YAML files"
    )
    args = parser.parse_args(argv)
    if args.self_test:
        return self_test()
    if not args.files or len(args.files) > MAX_FILES:
        print("usage: workflow_uses.py [--self-test] FILE [FILE ...]", file=sys.stderr)
        return 2
    try:
        for file_path in args.files:
            for repo, line in collect_file_uses(file_path):
                print(f"{repo}\t{file_path}:{line}")
    except UsesError as err:
        print(f"error: {err}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
