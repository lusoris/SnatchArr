#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Extract remote uses references or list local composite actions from Actions YAML.

Reads each file with yaml.compose so YAML anchors, aliases, merge keys and flow
collections resolve while every node keeps its 1-based start line. Default mode
prints one tab-separated line per remote reference (`owner/repo\\tpath:line`) from
`jobs.<job>.uses`, `jobs.<job>.steps[*].uses` and `runs.steps[*].uses`, skipping `./`
and `docker://` values and never inspecting `run:` scripts. `--list-actions ROOT FILE`
prints NUL-separated repo-relative paths of every action.yml / action.yaml under
ROOT/.github/actions at any depth plus every composite action reached from step-level
`uses: ./<dir>` through an iterative worklist. Exits 2 on missing PyYAML, walk/read
or YAML errors, missing or root-escaping local actions, or invalid remote references.
"""

from __future__ import annotations

import argparse
import os
import re
import stat
import sys
import tempfile
from collections import deque
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
ACTION_FILENAMES = frozenset({"action.yml", "action.yaml"})

MAX_FILES = 4096
MAX_WALK_DIRS = 16384
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


def mapping_uses_scalar(
    mapping: yaml.MappingNode, path: str
) -> tuple[dict[str, tuple[yaml.ScalarNode, yaml.Node]], tuple[str, int] | None]:
    """Resolve mapping and return its optional string uses key as (raw, line)."""
    fields = resolve_mapping(mapping, path)
    if "uses" not in fields:
        return fields, None
    key_node, val_node = fields["uses"]
    line = node_line(key_node, val_node)
    if not isinstance(val_node, yaml.ScalarNode) or val_node.tag != STR_TAG:
        raise UsesError(f"{path}:{line}: uses value must be a string")
    return fields, (val_node.value, line)


def extract_mapping_uses(
    mapping: yaml.MappingNode, path: str
) -> tuple[dict[str, tuple[yaml.ScalarNode, yaml.Node]], tuple[str, int] | None]:
    """Resolve mapping and parse its optional uses key into (owner/repo, line)."""
    fields, raw_hit = mapping_uses_scalar(mapping, path)
    if raw_hit is None:
        return fields, None
    repo = parse_uses_value(raw_hit[0], path, raw_hit[1])
    return fields, (repo, raw_hit[1]) if repo is not None else None


def collect_steps_raw_uses(steps_node: yaml.Node, path: str) -> list[tuple[str, int]]:
    """Collect raw (uses_value, line) pairs from a steps sequence node."""
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
        _, hit = mapping_uses_scalar(step_node, path)
        if hit is not None:
            found.append(hit)
    return found


def collect_steps_uses(steps_node: yaml.Node, path: str) -> list[tuple[str, int]]:
    """Collect remote (owner/repo, line) pairs from a steps sequence node."""
    found: list[tuple[str, int]] = []
    for raw, line in collect_steps_raw_uses(steps_node, path):
        repo = parse_uses_value(raw, path, line)
        if repo is not None:
            found.append((repo, line))
    return found


def resolved_job_nodes(jobs_node: yaml.Node, path: str) -> list[yaml.MappingNode]:
    """Validate a jobs mapping node and return its job mapping nodes."""
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
    out: list[yaml.MappingNode] = []
    for _, job_node in jobs.values():
        if not isinstance(job_node, yaml.MappingNode):
            raise UsesError(
                f"{path}:{job_node.start_mark.line + 1}: job must be a mapping"
            )
        out.append(job_node)
    return out


def collect_jobs_uses(jobs_node: yaml.Node, path: str) -> list[tuple[str, int]]:
    """Collect remote (owner/repo, line) pairs from jobs.<job>.uses and jobs.<job>.steps[*].uses."""
    found: list[tuple[str, int]] = []
    for job_node in resolved_job_nodes(jobs_node, path):
        job_fields, hit = extract_mapping_uses(job_node, path)
        if hit is not None:
            found.append(hit)
        if "steps" in job_fields:
            found.extend(collect_steps_uses(job_fields["steps"][1], path))
    return found


def compose_top_mapping(
    text: str, path: str
) -> dict[str, tuple[yaml.ScalarNode, yaml.Node]]:
    """Compose YAML text for path and return its resolved root mapping."""
    try:
        root = yaml.compose(text, Loader=yaml.SafeLoader)
    except yaml.YAMLError as err:
        raise UsesError(f"{path}: {err}") from err
    if not isinstance(root, yaml.MappingNode):
        raise UsesError(f"{path}: document root is not a YAML mapping")
    return resolve_mapping(root, path)


def runs_steps_node(
    top: dict[str, tuple[yaml.ScalarNode, yaml.Node]], path: str
) -> yaml.Node | None:
    """Validate optional runs mapping in top and return its steps node when present."""
    if "runs" not in top:
        return None
    runs_node = top["runs"][1]
    if not isinstance(runs_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{runs_node.start_mark.line + 1}: runs must be a mapping"
        )
    runs_fields = resolve_mapping(runs_node, path)
    return runs_fields["steps"][1] if "steps" in runs_fields else None


def collect_text_uses(text: str, path: str) -> list[tuple[str, int]]:
    """Compose YAML text for path and return remote (owner/repo, line) pairs."""
    top = compose_top_mapping(text, path)
    found: list[tuple[str, int]] = []
    if "jobs" in top:
        found.extend(collect_jobs_uses(top["jobs"][1], path))
    steps_node = runs_steps_node(top, path)
    if steps_node is not None:
        found.extend(collect_steps_uses(steps_node, path))
    return found


def collect_file_uses(path: str) -> list[tuple[str, int]]:
    """Read path as UTF-8 and return remote (owner/repo, line) pairs."""
    try:
        text = Path(path).read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as err:
        raise UsesError(f"{path}: {err}") from err
    return collect_text_uses(text, path)


def collect_local_step_uses(full_path: Path, rel_path: str) -> list[tuple[str, int]]:
    """Read full_path and return step-level `./<dir>` (raw, line) references."""
    try:
        text = full_path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as err:
        raise UsesError(f"{rel_path}: {err}") from err
    top = compose_top_mapping(text, rel_path)
    raw_steps: list[tuple[str, int]] = []
    if "jobs" in top:
        for job_node in resolved_job_nodes(top["jobs"][1], rel_path):
            job_fields, _ = mapping_uses_scalar(job_node, rel_path)
            if "steps" in job_fields:
                raw_steps.extend(
                    collect_steps_raw_uses(job_fields["steps"][1], rel_path)
                )
    steps_node = runs_steps_node(top, rel_path)
    if steps_node is not None:
        raw_steps.extend(collect_steps_raw_uses(steps_node, rel_path))
    return [(raw, line) for raw, line in raw_steps if raw.startswith("./")]


def resolve_local_action(root: Path, raw: str, path: str, line: int) -> str:
    """Resolve step `./<dir>` under root to its repo-relative action file."""
    sub = raw[2:]
    if (
        any(ch in raw for ch in ("\x00", "\r", "\n"))
        or sub.startswith(("/", "\\"))
        or Path(sub).is_absolute()
    ):
        raise UsesError(f"{path}:{line}: invalid local action {raw!r}")
    try:
        target_dir = (root / sub).resolve()
    except OSError as err:
        raise UsesError(f"{path}:{line}: local action {raw!r}: {err}") from err
    if not target_dir.is_relative_to(root):
        raise UsesError(f"{path}:{line}: local action {raw!r} escapes repository root")
    for filename in ("action.yml", "action.yaml"):
        candidate = target_dir / filename
        try:
            st = candidate.stat()
        except FileNotFoundError:
            continue
        except OSError as err:
            raise UsesError(f"{path}:{line}: local action {raw!r}: {err}") from err
        if not stat.S_ISREG(st.st_mode):
            continue
        resolved = candidate.resolve()
        if not resolved.is_relative_to(root):
            raise UsesError(
                f"{path}:{line}: local action {raw!r} escapes repository root"
            )
        return resolved.relative_to(root).as_posix()
    raise UsesError(
        f"{path}:{line}: local action {raw!r} has neither action.yml nor action.yaml"
    )


def raise_walk_error(err: OSError) -> None:
    """Abort os.walk on any unreadable directory under .github/actions."""
    raise UsesError(f".github/actions: {err}") from err


def actions_dir_if_present(root: Path) -> Path | None:
    """Return root/.github/actions when it exists as an in-root directory, else None."""
    actions_root = root / ".github" / "actions"
    try:
        st = actions_root.stat()
    except FileNotFoundError:
        return None
    except OSError as err:
        raise UsesError(f".github/actions: {err}") from err
    if not stat.S_ISDIR(st.st_mode):
        return None
    if not actions_root.resolve().is_relative_to(root):
        raise UsesError(".github/actions: escapes repository root")
    return actions_root


def register_walk_entry(root: Path, dirpath: str, name: str, out: set[str]) -> None:
    """Validate and record a discovered action.yml/action.yaml entry under root."""
    if name not in ACTION_FILENAMES:
        return
    candidate = (Path(dirpath) / name).resolve()
    if not candidate.is_relative_to(root):
        raise UsesError(f".github/actions: {name} escapes repository root")
    if candidate.is_file():
        out.add(candidate.relative_to(root).as_posix())
        if len(out) > MAX_FILES:
            raise UsesError(
                f"more than {MAX_FILES} composite action files under {root}"
            )


def walk_github_actions(root: Path) -> list[str]:
    """Return sorted repo-relative action files under root/.github/actions."""
    actions_root = actions_dir_if_present(root)
    if actions_root is None:
        return []
    found: set[str] = set()
    for dir_count, (dirpath, dirnames, filenames) in enumerate(
        os.walk(actions_root, onerror=raise_walk_error), start=1
    ):
        if dir_count > MAX_WALK_DIRS or len(filenames) > MAX_FILES:
            raise UsesError(f".github/actions: exceeds {MAX_WALK_DIRS} walk entries")
        dirnames.sort()
        for name in sorted(filenames):
            register_walk_entry(root, dirpath, name, found)
    return sorted(found)


def normalize_input_file(root: Path, file_arg: str) -> str:
    """Resolve file_arg against root and return its repo-relative POSIX path."""
    raw_path = Path(file_arg)
    try:
        resolved = (
            raw_path.resolve()
            if raw_path.is_absolute()
            else (root / raw_path).resolve()
        )
    except OSError as err:
        raise UsesError(f"{file_arg}: {err}") from err
    if not resolved.is_relative_to(root):
        raise UsesError(f"{file_arg}: escapes repository root")
    return resolved.relative_to(root).as_posix()


def seed_worklist(
    root: Path, root_arg: str, workflow_files: list[str]
) -> tuple[deque[str], set[str], set[str]]:
    """Seed worklist with workflow_files and walked .github/actions files."""
    if len(workflow_files) > MAX_FILES:
        raise UsesError(f"more than {MAX_FILES} workflow files under {root_arg}")
    worklist: deque[str] = deque()
    seen: set[str] = set()
    actions: set[str] = set()
    for wf in workflow_files:
        rel = normalize_input_file(root, wf)
        if rel not in seen:
            seen.add(rel)
            worklist.append(rel)
    for act_rel in walk_github_actions(root):
        actions.add(act_rel)
        if act_rel not in seen:
            seen.add(act_rel)
            worklist.append(act_rel)
    if len(seen) > MAX_FILES:
        raise UsesError(
            f"more than {MAX_FILES} workflow and composite action files"
            f" under {root_arg}"
        )
    return worklist, seen, actions


def list_action_files(root_arg: str, workflow_files: list[str]) -> list[str]:
    """List repo-relative composite actions under root_arg via worklist."""
    try:
        root = Path(root_arg).resolve()
        is_dir = root.is_dir()
    except OSError as err:
        raise UsesError(f"{root_arg}: {err}") from err
    if not is_dir:
        raise UsesError(f"{root_arg}: not a directory")
    worklist, seen, actions = seed_worklist(root, root_arg, workflow_files)
    for _ in range(MAX_FILES):
        if not worklist:
            break
        current = worklist.popleft()
        for raw_uses, line in collect_local_step_uses(root / current, current):
            act_rel = resolve_local_action(root, raw_uses, current, line)
            actions.add(act_rel)
            if act_rel not in seen:
                if len(seen) >= MAX_FILES:
                    raise UsesError(
                        f"more than {MAX_FILES} workflow and composite action"
                        f" files under {root_arg}"
                    )
                seen.add(act_rel)
                worklist.append(act_rel)
    return sorted(actions)


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


def run_list_positive_case() -> list[str]:
    """Verify --list-actions finds deep .github/actions and ./<dir> actions."""
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        wf_dir = root / ".github" / "workflows"
        wf_dir.mkdir(parents=True)
        (wf_dir / "ci.yml").write_text(
            "on: push\njobs:\n  call:\n"
            "    uses: ./.github/workflows/reusable.yml\n"
            "  build:\n    runs-on: ubuntu-26.04\n    steps:\n"
            "      - uses: ./tools/actions/x\n",
            encoding="utf-8",
        )
        (wf_dir / "reusable.yml").write_text(
            "on: workflow_call\njobs:\n  r:\n    runs-on: ubuntu-26.04\n"
            "    steps:\n      - uses: actions/checkout@v4\n",
            encoding="utf-8",
        )
        act_x = root / "tools" / "actions" / "x"
        act_x.mkdir(parents=True)
        (act_x / "action.yml").write_text(
            "runs:\n  using: composite\n  steps:\n    - uses: ./tools/actions/y\n"
            "    - uses: ./tools/actions/x\n    - uses: moved/outside@v1\n",
            encoding="utf-8",
        )
        act_y = root / "tools" / "actions" / "y"
        act_y.mkdir(parents=True)
        (act_y / "action.yaml").write_text(
            "runs:\n  using: composite\n  steps:\n    - uses: moved/chained@v1\n",
            encoding="utf-8",
        )
        deep_rel = ".github/actions/d1/d2/d3/d4/d5/d6/d7/d8/d9"
        deep_dir = root / deep_rel
        deep_dir.mkdir(parents=True)
        (deep_dir / "action.yml").write_text(
            "runs:\n  using: composite\n  steps:\n    - uses: moved/deep@v1\n",
            encoding="utf-8",
        )
        want = [
            f"{deep_rel}/action.yml",
            "tools/actions/x/action.yml",
            "tools/actions/y/action.yaml",
        ]
        try:
            got = list_action_files(
                str(root),
                [".github/workflows/ci.yml", ".github/workflows/reusable.yml"],
            )
        except UsesError as err:
            return [f"list-actions-discovery: unexpected error: {err}"]
        if got != want:
            return [f"list-actions-discovery: got {got!r}, want {want!r}"]
        print("ok   list-actions-discovery")
    return []


def check_list_refusal(name: str, root: Path, wf_rel: str, needle: str) -> list[str]:
    """Assert list_action_files raises UsesError containing needle."""
    try:
        got = list_action_files(str(root), [wf_rel])
        return [f"{name}: accepted {got!r}, expected error with {needle!r}"]
    except UsesError as err:
        if needle not in str(err):
            return [f"{name}: error {str(err)!r} lacks {needle!r}"]
        print(f"ok   {name}")
        return []


def run_list_negative_cases() -> list[str]:
    """Verify --list-actions refuses missing actions, escapes, and walk errors."""
    failures: list[str] = []
    wf_rel = ".github/workflows/planted.yml"
    wf_head = "on: push\njobs:\n  j:\n    runs-on: ubuntu-26.04\n    steps:\n"
    with tempfile.TemporaryDirectory() as tmp:
        base = Path(tmp)
        outside = base / "outside"
        outside.mkdir()
        (outside / "action.yml").write_text(
            "runs:\n  using: composite\n  steps: []\n", encoding="utf-8"
        )
        missing_root = base / "missing-case"
        (missing_root / ".github" / "workflows").mkdir(parents=True)
        (missing_root / wf_rel).write_text(
            f"{wf_head}      - uses: ./missing\n", encoding="utf-8"
        )
        failures.extend(
            check_list_refusal(
                "list-actions-missing", missing_root, wf_rel, f"{wf_rel}:6"
            )
        )
        escape_root = base / "escape-case"
        (escape_root / ".github" / "workflows").mkdir(parents=True)
        (escape_root / wf_rel).write_text(
            f"{wf_head}      - uses: ./../outside\n", encoding="utf-8"
        )
        failures.extend(
            check_list_refusal(
                "list-actions-escape", escape_root, wf_rel, f"{wf_rel}:6"
            )
        )
        walk_root = base / "walk-error-case"
        (walk_root / ".github" / "workflows").mkdir(parents=True)
        (walk_root / wf_rel).write_text(
            f"{wf_head}      - run: echo ok\n", encoding="utf-8"
        )
        locked = walk_root / ".github" / "actions" / "locked"
        locked.mkdir(parents=True)
        locked.chmod(0o000)
        try:
            failures.extend(
                check_list_refusal(
                    "list-actions-unreadable-dir",
                    walk_root,
                    wf_rel,
                    ".github/actions",
                )
            )
        finally:
            locked.chmod(0o700)
    return failures


def self_test() -> int:
    """Run all planted positive and negative cases; exit 1 naming any failed case."""
    failures = [
        *run_positive_cases(),
        *run_negative_cases(),
        *run_list_positive_case(),
        *run_list_negative_cases(),
    ]
    for msg in failures:
        print(f"FAIL {msg}", file=sys.stderr)
    return 1 if failures else 0


def main(argv: list[str] | None = None) -> int:
    """Run --self-test, --list-actions, or print remote uses for argv files."""
    if yaml is None:
        print("error: PyYAML is required", file=sys.stderr)
        return 2
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
    parser.add_argument(
        "files", nargs="*", help="workflow or composite action YAML files"
    )
    args = parser.parse_args(argv)
    if args.self_test:
        return self_test()
    if not args.files or len(args.files) > MAX_FILES:
        print(
            "usage: workflow_uses.py [--self-test | --list-actions ROOT]"
            " FILE [FILE ...]",
            file=sys.stderr,
        )
        return 2
    try:
        if args.list_actions is not None:
            listed = list_action_files(args.list_actions, args.files)
            if listed:
                sys.stdout.buffer.write(
                    b"\0".join(item.encode("utf-8") for item in listed) + b"\0"
                )
            return 0
        for file_path in args.files:
            for repo, line in collect_file_uses(file_path):
                print(f"{repo}\t{file_path}:{line}")
    except UsesError as err:
        print(f"error: {err}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
