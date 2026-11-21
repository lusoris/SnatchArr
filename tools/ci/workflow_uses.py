#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Extract remote uses references, list composite actions, or emit step text lines.

Reads each file with yaml.compose so YAML anchors, aliases, merge keys and flow
collections resolve while every node keeps its 1-based start line. Default mode
prints one tab-separated line per remote reference (`owner/repo\\tpath:line`) from
`jobs.<job>.uses`, `jobs.<job>.steps[*].uses` and `runs.steps[*].uses`, skipping
`./`, `$/` and `docker://` values and enforcing MAX_REFS across the run.
`--list-actions ROOT FILE...` prints NUL-separated repo-relative paths of every
action.yml / action.yaml under ROOT/.github/actions plus composite actions reached
from step-level `./<dir>` and `$/<dir>`, skipping Docker-only local actions.
`--step-text ROOT FILE...` prints `<file>:<line>\\t<source line>` for lines covered
by step `run` and scalar `with` values. Exits 2 on missing PyYAML or validation error.
"""

from __future__ import annotations

import argparse
import os
import re
import stat
import sys
import tempfile
from collections import deque
from collections.abc import Callable, Iterable
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
ACTION_FILENAMES = ("action.yml", "action.yaml")
DOCKER_FILENAMES = ("Dockerfile", "dockerfile")
BAD_PATH_CHARS = ("\t", "\r", "\n", "\x00")

MAX_FILES = 4096
MAX_FILE_LINES = 65536
MAX_STEP_TEXT_LINES = 262144
MAX_WALK_DIRS = 16384
MAX_JOBS = 1024
MAX_STEPS = 4096
MAX_MAPPING_PAIRS = 4096
MAX_MERGE_NODES = 1024
MAX_REFS = 16384


class UsesError(Exception):
    """Raised when a workflow or composite action file fails YAML or uses validation."""


class RefBudget:
    """Count uses references across a run and enforce a fixed upper bound."""

    def __init__(self, limit: int = MAX_REFS) -> None:
        """Initialize the counter with maximum allowed reference count limit."""
        self.limit = limit
        self.count = 0

    def take(self, path: str, line: int) -> None:
        """Record one reference at path:line, raising UsesError past self.limit."""
        self.count += 1
        if self.count > self.limit:
            raise UsesError(f"{path}:{line}: more than {self.limit} uses references")


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
    node: yaml.MappingNode, path: str, *, any_scalar_key: bool = False
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
        ident = (
            ("", key_node.value)
            if any_scalar_key
            else (key_node.tag, key_node.value)
        )
        if ident in seen_keys:
            raise UsesError(
                f"{path}:{key_node.start_mark.line + 1}: duplicate key {key_node.value!r}"
            )
        seen_keys.add(ident)
        if any_scalar_key or key_node.tag == STR_TAG:
            entries[key_node.value] = (key_node, val_node)
    return entries, merges


def resolve_mapping(
    node: yaml.MappingNode, path: str, *, any_scalar_key: bool = False
) -> dict[str, tuple[yaml.ScalarNode, yaml.Node]]:
    """Resolve a YAML mapping node including merge keys without recursion."""
    resolved, initial_merges = explicit_entries(
        node, path, any_scalar_key=any_scalar_key
    )
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
        sub_entries, sub_merges = explicit_entries(
            cur, path, any_scalar_key=any_scalar_key
        )
        for key, pair in sub_entries.items():
            resolved.setdefault(key, pair)
        stack.extend(reversed(sub_merges))
    return resolved


def valid_repo_part(part: str) -> bool:
    """Report whether an owner or repository name segment uses only allowed characters."""
    return bool(NAME_RE.fullmatch(part)) and part not in DOT_NAMES


def check_self_repo_uses(raw: str, path: str, line: int) -> None:
    """Validate a $/ uses reference, refusing @ref suffixes and bare $/."""
    if "@" in raw:
        raise UsesError(
            f"{path}:{line}: self-repository reference {raw!r} must not include @ref"
        )
    if len(raw) == 2:
        raise UsesError(f"{path}:{line}: invalid uses reference {raw!r}")


def split_remote_target(raw: str, path: str, line: int) -> list[str]:
    """Split a remote uses@ref value on @ and return non-empty target segments."""
    target, sep, ref = raw.partition("@")
    if not sep or not ref or "@" in ref or any(ch.isspace() for ch in raw):
        raise UsesError(f"{path}:{line}: invalid uses reference {raw!r}")
    parts = target.split("/")
    if len(parts) < 2 or not all(parts):
        raise UsesError(f"{path}:{line}: invalid uses reference {raw!r}")
    return parts


def parse_uses_value(raw: str, path: str, line: int) -> str | None:
    """Validate a uses string and return owner/repo, or None for local, self and docker."""
    if raw.startswith(("./", "docker://")):
        return None
    if raw.startswith("$/"):
        check_self_repo_uses(raw, path, line)
        return None
    parts = split_remote_target(raw, path, line)
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


def check_steps_sequence(steps_node: yaml.Node, path: str) -> list[yaml.Node]:
    """Validate a steps sequence node and return its raw item list."""
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
    return steps_node.value


def step_uses_scalar(step_node: yaml.Node, path: str) -> tuple[str, int] | None:
    """Validate one step mapping node and return its optional (uses_raw, line)."""
    if not isinstance(step_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{step_node.start_mark.line + 1}: step must be a mapping"
        )
    _, hit = mapping_uses_scalar(step_node, path)
    return hit


def collect_steps_raw_uses(
    steps_node: yaml.Node, path: str, budget: RefBudget | None = None
) -> list[tuple[str, int]]:
    """Collect raw (uses_value, line) pairs from steps_node within optional budget."""
    found: list[tuple[str, int]] = []
    for step_node in check_steps_sequence(steps_node, path):
        hit = step_uses_scalar(step_node, path)
        if hit is not None:
            if budget is not None:
                budget.take(path, hit[1])
            found.append(hit)
    return found


def collect_steps_uses(
    steps_node: yaml.Node, path: str, budget: RefBudget
) -> list[tuple[str, int]]:
    """Collect remote (owner/repo, line) pairs from steps_node within budget."""
    found: list[tuple[str, int]] = []
    for step_node in check_steps_sequence(steps_node, path):
        hit = step_uses_scalar(step_node, path)
        if hit is None:
            continue
        repo = parse_uses_value(hit[0], path, hit[1])
        if repo is not None:
            budget.take(path, hit[1])
            found.append((repo, hit[1]))
    return found


def resolved_job_nodes(jobs_node: yaml.Node, path: str) -> list[yaml.MappingNode]:
    """Validate a jobs mapping node and return its job mapping nodes."""
    if isinstance(jobs_node, yaml.ScalarNode) and jobs_node.tag == NULL_TAG:
        return []
    if not isinstance(jobs_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{jobs_node.start_mark.line + 1}: jobs must be a mapping"
        )
    jobs = resolve_mapping(jobs_node, path, any_scalar_key=True)
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


def collect_jobs_uses(
    jobs_node: yaml.Node, path: str, budget: RefBudget
) -> list[tuple[str, int]]:
    """Collect remote (owner/repo, line) pairs from jobs.<job> within budget."""
    found: list[tuple[str, int]] = []
    for job_node in resolved_job_nodes(jobs_node, path):
        job_fields, hit = extract_mapping_uses(job_node, path)
        if hit is not None:
            budget.take(path, hit[1])
            found.append(hit)
        if "steps" in job_fields:
            found.extend(collect_steps_uses(job_fields["steps"][1], path, budget))
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


def collect_text_uses(
    text: str,
    path: str,
    max_refs: int = MAX_REFS,
    budget: RefBudget | None = None,
) -> list[tuple[str, int]]:
    """Compose YAML text for path and return remote (owner/repo, line) pairs."""
    active = budget if budget is not None else RefBudget(max_refs)
    top = compose_top_mapping(text, path)
    found: list[tuple[str, int]] = []
    if "jobs" in top:
        found.extend(collect_jobs_uses(top["jobs"][1], path, active))
    steps_node = runs_steps_node(top, path)
    if steps_node is not None:
        found.extend(collect_steps_uses(steps_node, path, active))
    return found


def collect_file_uses(
    path: str, max_refs: int = MAX_REFS, budget: RefBudget | None = None
) -> list[tuple[str, int]]:
    """Read path as UTF-8 and return remote (owner/repo, line) pairs."""
    try:
        text = Path(path).read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as err:
        raise UsesError(f"{path}: {err}") from err
    return collect_text_uses(text, path, max_refs=max_refs, budget=budget)


def collect_files_uses(
    files: list[str], max_refs: int = MAX_REFS
) -> list[tuple[str, str, int]]:
    """Collect remote (owner/repo, file_path, line) triples across files within max_refs."""
    if len(files) > MAX_FILES:
        raise UsesError(f"more than {MAX_FILES} workflow files")
    budget = RefBudget(max_refs)
    out: list[tuple[str, str, int]] = []
    for file_path in files:
        for repo, line in collect_file_uses(file_path, budget=budget):
            out.append((repo, file_path, line))
    return out


def filter_local_uses(
    raw_steps: list[tuple[str, int]], path: str
) -> list[tuple[str, int]]:
    """Keep ./ and $/ step references from raw_steps after validating $/ syntax."""
    local_hits: list[tuple[str, int]] = []
    for raw, line in raw_steps:
        if raw.startswith("$/"):
            check_self_repo_uses(raw, path, line)
            local_hits.append((raw, line))
        elif raw.startswith("./"):
            local_hits.append((raw, line))
    return local_hits


def collect_local_step_uses(
    full_path: Path, rel_path: str, budget: RefBudget | None = None
) -> list[tuple[str, int]]:
    """Read full_path and return step-level ./<dir> and $/<dir> (raw, line) pairs."""
    active = budget if budget is not None else RefBudget(MAX_REFS)
    try:
        text = full_path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as err:
        raise UsesError(f"{rel_path}: {err}") from err
    top = compose_top_mapping(text, rel_path)
    raw_steps: list[tuple[str, int]] = []
    if "jobs" in top:
        for job_node in resolved_job_nodes(top["jobs"][1], rel_path):
            job_fields, job_hit = mapping_uses_scalar(job_node, rel_path)
            if job_hit is not None and job_hit[0].startswith("$/"):
                check_self_repo_uses(job_hit[0], rel_path, job_hit[1])
            if "steps" in job_fields:
                raw_steps.extend(
                    collect_steps_raw_uses(job_fields["steps"][1], rel_path, active)
                )
    steps_node = runs_steps_node(top, rel_path)
    if steps_node is not None:
        raw_steps.extend(collect_steps_raw_uses(steps_node, rel_path, active))
    return filter_local_uses(raw_steps, rel_path)


def regular_in_root(
    root: Path, target_dir: Path, filename: str, raw: str, path: str, line: int
) -> Path | None:
    """Return resolved path when target_dir/filename is an in-root regular file."""
    candidate = target_dir / filename
    try:
        st = candidate.stat()
    except FileNotFoundError:
        return None
    except OSError as err:
        raise UsesError(f"{path}:{line}: local action {raw!r}: {err}") from err
    if not stat.S_ISREG(st.st_mode):
        return None
    resolved = candidate.resolve()
    if not resolved.is_relative_to(root):
        raise UsesError(f"{path}:{line}: local action {raw!r} escapes repository root")
    return resolved


def resolve_local_dir(root: Path, raw: str, path: str, line: int) -> Path:
    """Validate ./<dir> or $/<dir> syntax and return its resolved in-root Path."""
    if raw.startswith("$/"):
        check_self_repo_uses(raw, path, line)
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
    return target_dir


def resolve_local_action(
    root: Path, raw: str, path: str, line: int
) -> str | None:
    """Resolve step ./<dir> or $/<dir> to its repo-relative YAML action or None for Docker."""
    target_dir = resolve_local_dir(root, raw, path, line)
    for filename in ACTION_FILENAMES:
        resolved = regular_in_root(root, target_dir, filename, raw, path, line)
        if resolved is not None:
            return resolved.relative_to(root).as_posix()
    for filename in DOCKER_FILENAMES:
        if regular_in_root(root, target_dir, filename, raw, path, line) is not None:
            return None
    raise UsesError(
        f"{path}:{line}: local action {raw!r} has no action.yml, action.yaml or Dockerfile"
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


def walk_github_actions(
    root: Path,
    walk: Callable[..., Iterable[tuple[str, list[str], list[str]]]] = os.walk,
) -> list[str]:
    """Return sorted repo-relative action files under root/.github/actions."""
    actions_root = actions_dir_if_present(root)
    if actions_root is None:
        return []
    found: set[str] = set()
    for dir_count, (dirpath, dirnames, filenames) in enumerate(
        walk(actions_root, onerror=raise_walk_error), start=1
    ):
        if dir_count > MAX_WALK_DIRS or len(filenames) > MAX_FILES:
            raise UsesError(f".github/actions: exceeds {MAX_WALK_DIRS} walk entries")
        dirnames.sort()
        for name in sorted(filenames):
            register_walk_entry(root, dirpath, name, found)
    return sorted(found)


def resolve_root_dir(root_arg: str) -> Path:
    """Resolve root_arg to a directory Path or raise UsesError."""
    try:
        root = Path(root_arg).resolve()
        is_dir = root.is_dir()
    except OSError as err:
        raise UsesError(f"{root_arg}: {err}") from err
    if not is_dir:
        raise UsesError(f"{root_arg}: not a directory")
    return root


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


def list_action_files(
    root_arg: str, workflow_files: list[str], max_refs: int = MAX_REFS
) -> list[str]:
    """List repo-relative composite actions under root_arg via worklist."""
    root = resolve_root_dir(root_arg)
    budget = RefBudget(max_refs)
    worklist, seen, actions = seed_worklist(root, root_arg, workflow_files)
    for _ in range(MAX_FILES):
        if not worklist:
            break
        current = worklist.popleft()
        for raw_uses, line in collect_local_step_uses(root / current, current, budget):
            act_rel = resolve_local_action(root, raw_uses, current, line)
            if act_rel is None:
                continue
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


def normalize_step_file(root: Path, file_arg: str) -> str:
    """Validate repo-relative file_arg (refusing control chars) and normalize it."""
    if any(ch in file_arg for ch in BAD_PATH_CHARS):
        raise UsesError(f"invalid file path {file_arg!r}")
    if file_arg.startswith(("/", "\\")) or Path(file_arg).is_absolute():
        raise UsesError(f"{file_arg}: path must be relative to repository root")
    rel = normalize_input_file(root, file_arg)
    if any(ch in rel for ch in BAD_PATH_CHARS):
        raise UsesError(f"invalid file path {rel!r}")
    return rel


def record_scalar_lines(
    val_node: yaml.ScalarNode, line_count: int, covered: set[int]
) -> None:
    """Add 0-based source line indices covered by scalar val_node into covered."""
    start = val_node.start_mark.line
    end = val_node.end_mark.line
    if val_node.end_mark.column == 0 and end > start:
        end -= 1
    upper = min(end + 1, line_count)
    if upper - start > MAX_FILE_LINES:
        raise UsesError(f"scalar spans more than {MAX_FILE_LINES} lines")
    for idx in range(max(0, start), upper):
        covered.add(idx)


def collect_with_lines(
    with_node: yaml.Node, path: str, line_count: int, covered: set[int]
) -> None:
    """Record source lines for every scalar value inside a step with mapping."""
    if isinstance(with_node, yaml.ScalarNode) and with_node.tag == NULL_TAG:
        return
    if not isinstance(with_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{with_node.start_mark.line + 1}: with must be a mapping"
        )
    for _, val_node in resolve_mapping(with_node, path).values():
        if isinstance(val_node, yaml.ScalarNode):
            record_scalar_lines(val_node, line_count, covered)


def collect_step_lines(
    step_node: yaml.Node, path: str, line_count: int, covered: set[int]
) -> None:
    """Record source lines of run and scalar with values from one step mapping."""
    if not isinstance(step_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{step_node.start_mark.line + 1}: step must be a mapping"
        )
    fields = resolve_mapping(step_node, path)
    if "run" in fields:
        run_key, run_val = fields["run"]
        if not isinstance(run_val, yaml.ScalarNode):
            raise UsesError(
                f"{path}:{node_line(run_key, run_val)}: run value must be a scalar"
            )
        record_scalar_lines(run_val, line_count, covered)
    if "with" in fields:
        collect_with_lines(fields["with"][1], path, line_count, covered)


def collect_steps_seq_lines(
    steps_node: yaml.Node,
    path: str,
    line_count: int,
    covered: set[int],
    seen_nodes: set[int],
) -> None:
    """Record step run and with lines for every step in steps_node."""
    if id(steps_node) in seen_nodes:
        return
    seen_nodes.add(id(steps_node))
    for step_node in check_steps_sequence(steps_node, path):
        if id(step_node) in seen_nodes:
            continue
        seen_nodes.add(id(step_node))
        collect_step_lines(step_node, path, line_count, covered)


def collect_text_step_lines(text: str, rel_path: str) -> list[tuple[int, str]]:
    """Return sorted 1-based (line_no, source_line) pairs for step run/with in text."""
    lines = text.splitlines()
    if len(lines) > MAX_FILE_LINES:
        raise UsesError(f"{rel_path}: exceeds {MAX_FILE_LINES} lines")
    top = compose_top_mapping(text, rel_path)
    covered: set[int] = set()
    seen_nodes: set[int] = set()
    if "jobs" in top:
        for job_node in resolved_job_nodes(top["jobs"][1], rel_path):
            if id(job_node) in seen_nodes:
                continue
            seen_nodes.add(id(job_node))
            job_fields = resolve_mapping(job_node, rel_path)
            if "steps" in job_fields:
                collect_steps_seq_lines(
                    job_fields["steps"][1], rel_path, len(lines), covered, seen_nodes
                )
    steps_node = runs_steps_node(top, rel_path)
    if steps_node is not None:
        collect_steps_seq_lines(steps_node, rel_path, len(lines), covered, seen_nodes)
    return [(idx + 1, lines[idx]) for idx in sorted(covered)]


def collect_files_step_text(
    root_arg: str, files: list[str]
) -> list[tuple[str, int, str]]:
    """Return distinct (rel_path, line_no, source_line) records for files under root_arg."""
    if len(files) > MAX_FILES:
        raise UsesError(f"more than {MAX_FILES} workflow files under {root_arg}")
    root = resolve_root_dir(root_arg)
    seen_files: set[str] = set()
    records: list[tuple[str, int, str]] = []
    for file_arg in files:
        rel_path = normalize_step_file(root, file_arg)
        if rel_path in seen_files:
            continue
        seen_files.add(rel_path)
        try:
            text = (root / rel_path).read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError) as err:
            raise UsesError(f"{rel_path}: {err}") from err
        for line_no, source_line in collect_text_step_lines(text, rel_path):
            if len(records) >= MAX_STEP_TEXT_LINES:
                raise UsesError(
                    f"{rel_path}:{line_no}: more than {MAX_STEP_TEXT_LINES} step text lines"
                )
            records.append((rel_path, line_no, source_line))
    return records


def run_positive_cases() -> list[str]:
    """Run planted positive self-test cases and return failure descriptions."""
    workflow_doc = (
        "on: push\njobs:\n  call:\n    uses: org.1/reusable/.github/workflows/w.yml@v1\n"
        "  build:\n    runs-on: ubuntu-26.04\n    env:\n      uses: ignored/env@v1\n"
        "    steps:\n      - uses: ./local/action\n      - uses: $/.github/actions/x\n"
        "      - uses: docker://alpine:3.20\n"
        "      - name: script\n        run: |\n          uses: moved/in-run@v1\n"
        "      - uses: act_1/step-repo/sub@deadbeef # v1\n"
    )
    anchor_doc = (
        "x-ref: &ref a-owner/a-repo@v1\nx-step: &step\n  uses: b.owner/b_repo@v2\n"
        "jobs:\n  j:\n    steps:\n      - uses: *ref\n      - *step\n"
        "      - <<: *step\n        name: merged\n      - {name: f, uses: c/d@v3}\n"
    )
    cases = [
        ("workflow-jobs-steps-and-skips", workflow_doc, [("org.1/reusable", 4), ("act_1/step-repo", 16)]),
        (
            "anchors-aliases-merges-and-flow",
            anchor_doc,
            [("a-owner/a-repo", 1), ("b.owner/b_repo", 3), ("b.owner/b_repo", 3), ("c/d", 11)],
        ),
        (
            "composite-action-runs",
            "name: c\nruns:\n  using: composite\n  steps:\n    - uses: comp/act@v1\n",
            [("comp/act", 5)],
        ),
        ("bool-job-id", "jobs:\n  yes:\n    steps:\n      - uses: a/b@c\n", [("a/b", 4)]),
        (
            "merged-bool-job-id",
            "x-jobs: &j\n  on:\n    steps:\n      - uses: d/e@f\njobs:\n  <<: *j\n",
            [("d/e", 4)],
        ),
        ("self-repo-uses-no-lookup", "jobs:\n  j:\n    steps:\n      - uses: $/.github/actions/x\n", []),
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
        (
            "duplicate-bool-str-job-id",
            'jobs:\n  yes:\n    steps: []\n  "yes":\n    steps: []\n',
            "dupjob.yml:4: duplicate key 'yes'",
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
        ("dotdot-owner", "jobs:\n  j:\n    uses: ../repo@v1\n", "dot.yml:3"),
        ("dotdot-repo", "jobs:\n  j:\n    uses: owner/..@v1\n", "dotrepo.yml:3"),
        (
            "non-scalar-uses",
            "jobs:\n  j:\n    uses: [a/b@v1]\n",
            "listuses.yml:3",
        ),
        ("null-uses", "jobs:\n  j:\n    steps:\n      - uses:\n", "null.yml:4"),
        (
            "self-repo-with-ref",
            "jobs:\n  j:\n    steps:\n      - uses: $/.github/actions/x@v1\n",
            "selfref.yml:4: self-repository reference '$/.github/actions/x@v1' must not include @ref",
        ),
        (
            "bare-self-repo",
            "jobs:\n  j:\n    steps:\n      - uses: $/\n",
            "bareself.yml:4: invalid uses reference '$/'",
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


def check_max_refs_refusal(
    name: str, text: str, path: str, max_refs: int, needle: str
) -> list[str]:
    """Assert collect_text_uses at max_refs raises UsesError containing needle."""
    try:
        got = collect_text_uses(text, path, max_refs=max_refs)
        return [
            f"{name}: accepted {len(got)} refs, expected error with {needle!r}"
        ]
    except UsesError as err:
        if needle not in str(err):
            return [f"{name}: error {str(err)!r} lacks {needle!r}"]
        print(f"ok   {name}")
        return []


def run_max_refs_cases() -> list[str]:
    """Verify MAX_REFS bounds extract and --list-actions modes cleanly."""
    failures: list[str] = []
    three_doc = (
        "jobs:\n  j:\n    steps:\n      - uses: a/b@v1\n"
        "      - uses: c/d@v1\n      - uses: e/f@v1\n"
    )
    four_doc = f"{three_doc}      - uses: g/h@v1\n"
    try:
        got = collect_text_uses(three_doc, "three.yml", max_refs=3)
        if len(got) != 3:
            failures.append(f"max-refs-exact-3: got {got!r}, want 3 entries")
        else:
            print("ok   max-refs-exact-3")
    except UsesError as err:
        failures.append(f"max-refs-exact-3: unexpected error: {err}")
    failures.extend(
        check_max_refs_refusal(
            "max-refs-over-3",
            four_doc,
            "four.yml",
            3,
            "four.yml:7: more than 3 uses references",
        )
    )
    big_doc = (
        "x-steps: &shared\n"
        + "".join("  - uses: a/b@c\n" for _ in range(MAX_STEPS))
        + "jobs:\n"
        + "".join(f"  j{idx}:\n    steps: *shared\n" for idx in range(5))
    )
    failures.extend(
        check_max_refs_refusal(
            "max-refs-anchored-jobs",
            big_doc,
            "big.yml",
            MAX_REFS,
            "big.yml:2: more than 16384 uses references",
        )
    )
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        wf_dir = root / ".github" / "workflows"
        wf_dir.mkdir(parents=True)
        (wf_dir / "four.yml").write_text(four_doc, encoding="utf-8")
        failures.extend(
            check_list_refusal(
                "list-actions-max-refs",
                root,
                ".github/workflows/four.yml",
                ".github/workflows/four.yml:7: more than 3 uses references",
                max_refs=3,
            )
        )
    return failures


def write_list_positive_fixture(root: Path) -> list[str]:
    """Populate root with workflows, composite actions and a Docker-only action."""
    wf_dir = root / ".github" / "workflows"
    wf_dir.mkdir(parents=True)
    (wf_dir / "ci.yml").write_text(
        "on: push\njobs:\n  call:\n"
        "    uses: ./.github/workflows/reusable.yml\n"
        "  build:\n    runs-on: ubuntu-26.04\n    steps:\n"
        "      - uses: ./tools/actions/x\n"
        "      - uses: $/.github/actions/x\n"
        "      - uses: $/tools/actions/z\n"
        "      - uses: ./tools/actions/docker-only\n",
        encoding="utf-8",
    )
    (wf_dir / "reusable.yml").write_text(
        "on: workflow_call\njobs:\n  r:\n    runs-on: ubuntu-26.04\n"
        "    steps:\n      - uses: actions/checkout@v4\n",
        encoding="utf-8",
    )
    for rel, body in (
        (
            "tools/actions/x/action.yml",
            (
                "runs:\n  using: composite\n  steps:\n    - uses: ./tools/actions/y\n"
                "    - uses: ./tools/actions/x\n    - uses: moved/outside@v1\n"
            ),
        ),
        (
            "tools/actions/y/action.yaml",
            "runs:\n  using: composite\n  steps:\n    - uses: moved/chained@v1\n",
        ),
        (
            "tools/actions/z/action.yml",
            "runs:\n  using: composite\n  steps:\n    - uses: moved/self-out@v1\n",
        ),
        (
            ".github/actions/x/action.yml",
            "runs:\n  using: composite\n  steps:\n    - uses: moved/self@v1\n",
        ),
        (
            ".github/actions/d1/d2/d3/d4/d5/d6/d7/d8/d9/action.yml",
            "runs:\n  using: composite\n  steps:\n    - uses: moved/deep@v1\n",
        ),
        ("tools/actions/docker-only/Dockerfile", "FROM scratch\n"),
    ):
        full = root / rel
        full.parent.mkdir(parents=True, exist_ok=True)
        full.write_text(body, encoding="utf-8")
    return [
        ".github/actions/d1/d2/d3/d4/d5/d6/d7/d8/d9/action.yml",
        ".github/actions/x/action.yml",
        "tools/actions/x/action.yml",
        "tools/actions/y/action.yaml",
        "tools/actions/z/action.yml",
    ]


def run_list_positive_case() -> list[str]:
    """Verify --list-actions finds ./, $/ and deep actions while skipping Docker-only."""
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        want = write_list_positive_fixture(root)
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


def check_list_refusal(
    name: str, root: Path, wf_rel: str, needle: str, max_refs: int = MAX_REFS
) -> list[str]:
    """Assert list_action_files raises UsesError containing needle."""
    try:
        got = list_action_files(str(root), [wf_rel], max_refs=max_refs)
        return [f"{name}: accepted {got!r}, expected error with {needle!r}"]
    except UsesError as err:
        if needle not in str(err):
            return [f"{name}: error {str(err)!r} lacks {needle!r}"]
        print(f"ok   {name}")
        return []


def check_walk_error_case(base: Path) -> list[str]:
    """Verify walk_github_actions propagates an injected onerror PermissionError."""
    walk_root = (base / "walk-error-case").resolve()
    locked = walk_root / ".github" / "actions" / "locked"
    locked.mkdir(parents=True)

    def fake_walk(
        _top: Path, *, onerror: Callable[[OSError], None]
    ) -> Iterable[tuple[str, list[str], list[str]]]:
        """Invoke onerror with a simulated PermissionError and yield nothing."""
        onerror(PermissionError(13, "Permission denied", str(locked)))
        return ()

    try:
        got = walk_github_actions(walk_root, walk=fake_walk)
        return [f"list-actions-unreadable-dir: accepted {got!r}, expected UsesError"]
    except UsesError as err:
        if not str(err).startswith(".github/actions:"):
            msg = (
                f"list-actions-unreadable-dir: error {str(err)!r}"
                " does not start with '.github/actions:'"
            )
            return [msg]
        print("ok   list-actions-unreadable-dir")
        return []


def run_list_negative_cases() -> list[str]:
    """Verify --list-actions refuses missing actions, escapes, @ref and walk errors."""
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
        for case_name, uses_val, needle in (
            (
                "list-actions-missing",
                "./missing",
                f"{wf_rel}:6: local action './missing' has no action.yml, action.yaml or Dockerfile",
            ),
            ("list-actions-escape", "./../outside", f"{wf_rel}:6: local action './../outside' escapes repository root"),
            (
                "list-actions-self-repo-escape",
                "$/../outside",
                f"{wf_rel}:6: local action '$/../outside' escapes repository root",
            ),
            (
                "list-actions-self-repo-with-ref",
                "$/.github/actions/x@v1",
                f"{wf_rel}:6: self-repository reference '$/.github/actions/x@v1' must not include @ref",
            ),
        ):
            case_root = base / case_name
            (case_root / ".github" / "workflows").mkdir(parents=True)
            (case_root / wf_rel).write_text(
                f"{wf_head}      - uses: {uses_val}\n", encoding="utf-8"
            )
            failures.extend(check_list_refusal(case_name, case_root, wf_rel, needle))
        failures.extend(check_walk_error_case(base))
    return failures


def run_step_text_cases() -> list[str]:
    """Verify --step-text emits only step run/with lines and refuses control-char paths."""
    failures: list[str] = []
    act_rel = ".github/actions/sample/action.yml"
    act_doc = (
        "name: sample\n"
        "description: go install golang.org/x/vuln/cmd/govulncheck@latest\n"
        "inputs:\n"
        "  x:\n"
        "    description: go install golang.org/x/vuln/cmd/govulncheck@latest\n"
        "runs:\n"
        "  using: composite\n"
        "  steps:\n"
        "    - name: block\n"
        "      shell: bash\n"
        "      run: |\n"
        "        echo first\n"
        "        go install example.com/cmd@v1.2.3\n"
        "    - name: single\n"
        "      shell: bash\n"
        "      run: go install x@v1.0.0\n"
        "    - uses: actions/checkout@v4\n"
        "      with:\n"
        "        ref: main\n"
    )
    want = [
        (act_rel, 11, "      run: |"),
        (act_rel, 12, "        echo first"),
        (act_rel, 13, "        go install example.com/cmd@v1.2.3"),
        (act_rel, 16, "      run: go install x@v1.0.0"),
        (act_rel, 19, "        ref: main"),
    ]
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        act_path = root / act_rel
        act_path.parent.mkdir(parents=True)
        act_path.write_text(act_doc, encoding="utf-8")
        try:
            got = collect_files_step_text(str(root), [act_rel, act_rel])
            if got != want:
                failures.append(f"step-text-records: got {got!r}, want {want!r}")
            else:
                print("ok   step-text-records")
        except UsesError as err:
            failures.append(f"step-text-records: unexpected error: {err}")
        for label, bad_arg in (("tab", "a\tb.yml"), ("lf", "a\nb.yml"), ("nul", "a\x00b.yml")):
            try:
                collect_files_step_text(str(root), [bad_arg])
                failures.append(f"step-text-bad-{label}: accepted {bad_arg!r}")
            except UsesError:
                print(f"ok   step-text-bad-{label}")
    return failures


def self_test() -> int:
    """Run all planted positive and negative cases; exit 1 naming any failed case."""
    failures = [
        *run_positive_cases(),
        *run_negative_cases(),
        *run_max_refs_cases(),
        *run_list_positive_case(),
        *run_list_negative_cases(),
        *run_step_text_cases(),
    ]
    for msg in failures:
        print(f"FAIL {msg}", file=sys.stderr)
    return 1 if failures else 0


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
        help="print step run and with source lines under ROOT as file:line\\ttext",
    )
    parser.add_argument(
        "files", nargs="*", help="workflow or composite action YAML files"
    )
    return parser


def dispatch_mode(args: argparse.Namespace) -> None:
    """Execute --list-actions, --step-text or default remote uses extraction."""
    if args.list_actions is not None:
        listed = list_action_files(args.list_actions, args.files)
        if listed:
            sys.stdout.buffer.write(
                b"\0".join(item.encode("utf-8") for item in listed) + b"\0"
            )
        return
    if args.step_text is not None:
        for rel_path, line_no, source_line in collect_files_step_text(
            args.step_text, args.files
        ):
            print(f"{rel_path}:{line_no}\t{source_line}")
        return
    for repo, file_path, line in collect_files_uses(args.files):
        print(f"{repo}\t{file_path}:{line}")


def main(argv: list[str] | None = None) -> int:
    """Run --self-test, --list-actions, --step-text or print remote uses."""
    if yaml is None:
        print("error: PyYAML is required", file=sys.stderr)
        return 2
    args = build_parser().parse_args(argv)
    if args.self_test:
        return self_test()
    if not args.files or len(args.files) > MAX_FILES:
        print(
            "usage: workflow_uses.py"
            " [--self-test | --list-actions ROOT | --step-text ROOT]"
            " FILE [FILE ...]",
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
