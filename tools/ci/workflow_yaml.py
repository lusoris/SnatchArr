# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""YAML node, mapping, step, job and uses helpers for workflow_uses.py.

Composes workflow and composite action YAML documents with yaml.compose so anchors,
aliases, merge keys and flow collections resolve without recursion while every node
retains its 1-based source line, enforcing fixed upper bounds on files, byte size,
lines, YAML nesting depth, jobs, steps, mapping entries, merge depth and uses references.
"""

from __future__ import annotations

import contextlib
import os
import re
import stat
from collections.abc import Callable, Iterator
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
LOCAL_USES_PREFIXES = ("./", ".\\")

MAX_FILES = 4096
MAX_FILE_BYTES = 8 * 1024 * 1024
MAX_FILE_LINES = 65536
MAX_YAML_DEPTH = 64
MAX_YAML_EVENTS = MAX_FILE_BYTES
MAX_STEP_TEXT_LINES = 262144
MAX_WALK_DIRS = 16384
MAX_JOBS = 1024
MAX_STEPS = 4096
MAX_STEP_VISITS = 65536
MAX_MAPPING_PAIRS = 4096
MAX_MERGE_NODES = 1024
MAX_RESOLVED_ENTRIES = 262144
MAX_REFS = 16384
MAX_OUTPUT_BYTES = 4 * MAX_FILE_BYTES


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


class EntryBudget:
    """Count resolved YAML mapping entries and enforce a fixed upper bound."""

    def __init__(self, limit: int = MAX_RESOLVED_ENTRIES) -> None:
        """Initialize the entry counter with maximum allowed entry count limit."""
        self.limit = limit
        self.count = 0

    def take(self, count: int, path: str, line: int) -> None:
        """Record count resolved entries at path:line, raising UsesError past self.limit."""
        self.count += count
        if self.count > self.limit:
            raise UsesError(
                f"{path}:{line}: more than {self.limit} resolved mapping entries"
            )


class OutputBudget:
    """Count formatted TSV output bytes across a run and enforce a fixed upper bound."""

    def __init__(self, limit: int = MAX_OUTPUT_BYTES) -> None:
        """Initialize the byte counter with maximum allowed output byte limit."""
        self.limit = limit
        self.used = 0

    def line(self, fields: tuple[str, ...], sep: str = "\t") -> str:
        """Join fields with sep and count UTF-8 bytes plus newline within self.limit."""
        sep_chars = (len(fields) - 1) * len(sep) if fields else 0
        min_bytes = sum(len(field) for field in fields) + sep_chars + 1
        if self.used + min_bytes > self.limit:
            raise UsesError(f"output exceeds {self.limit} bytes")
        joined = sep.join(fields)
        line_bytes = len(joined.encode("utf-8")) + 1
        if self.used + line_bytes > self.limit:
            raise UsesError(f"output exceeds {self.limit} bytes")
        self.used += line_bytes
        return joined


class DocWalk:
    """Track step visits and cache resolved YAML mappings within one document."""

    def __init__(
        self,
        limit: int = MAX_STEP_VISITS,
        entry_limit: int = MAX_RESOLVED_ENTRIES,
    ) -> None:
        """Initialize step visit and resolved entry budgets with an empty mapping cache."""
        self.limit = limit
        self.visits = 0
        self.entry_budget = EntryBudget(entry_limit)
        self.cache: dict[
            tuple[int, bool], dict[str, tuple[yaml.ScalarNode, yaml.Node]]
        ] = {}
        self._nodes: list[yaml.MappingNode] = []

    def visit(self, path: str, line: int) -> None:
        """Record one step visit at path:line, raising UsesError past self.limit."""
        self.visits += 1
        if self.visits > self.limit:
            raise UsesError(f"{path}:{line}: more than {self.limit} step visits")

    def resolve(
        self,
        node: yaml.MappingNode,
        path: str,
        *,
        any_scalar_key: bool = False,
    ) -> dict[str, tuple[yaml.ScalarNode, yaml.Node]]:
        """Return cached resolve_mapping(node, path) bounded by self.entry_budget."""
        key = (id(node), any_scalar_key)
        cached = self.cache.get(key)
        if cached is None:
            cached = resolve_mapping(
                node,
                path,
                budget=self.entry_budget,
                any_scalar_key=any_scalar_key,
            )
            self._nodes.append(node)
            self.cache[key] = cached
        return cached


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
    """Read direct scalar keys (refusing duplicates and second <<) and merge sources."""
    if len(node.value) > MAX_MAPPING_PAIRS:
        raise UsesError(
            f"{path}:{node.start_mark.line + 1}: mapping exceeds {MAX_MAPPING_PAIRS} entries"
        )
    entries: dict[str, tuple[yaml.ScalarNode, yaml.Node]] = {}
    merges: list[yaml.MappingNode] = []
    seen_keys: set[tuple[str, str]] = set()
    seen_merge = False
    for key_node, val_node in node.value:
        if not isinstance(key_node, yaml.ScalarNode):
            raise UsesError(
                f"{path}:{key_node.start_mark.line + 1}: mapping key is not a scalar"
            )
        if key_node.tag == MERGE_TAG:
            if seen_merge:
                raise UsesError(
                    f"{path}:{key_node.start_mark.line + 1}: duplicate merge key"
                )
            seen_merge = True
            merges.extend(merge_sources(val_node, path))
            continue
        ident = (
            ("", key_node.value) if any_scalar_key else (key_node.tag, key_node.value)
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
    node: yaml.MappingNode,
    path: str,
    budget: EntryBudget | None = None,
    *,
    any_scalar_key: bool = False,
) -> dict[str, tuple[yaml.ScalarNode, yaml.Node]]:
    """Resolve a YAML mapping node including merge keys within EntryBudget."""
    active = budget if budget is not None else EntryBudget(MAX_RESOLVED_ENTRIES)
    line = node.start_mark.line + 1
    resolved, initial_merges = explicit_entries(
        node, path, any_scalar_key=any_scalar_key
    )
    active.take(len(resolved), path, line)
    stack = list(reversed(initial_merges))
    visited: set[int] = {id(node)}
    for step in range(1, MAX_MERGE_NODES + 2):
        if not stack:
            break
        if step > MAX_MERGE_NODES:
            raise UsesError(f"{path}:{line}: merge depth exceeds {MAX_MERGE_NODES}")
        cur = stack.pop()
        if id(cur) in visited:
            continue
        visited.add(id(cur))
        sub_entries, sub_merges = explicit_entries(
            cur, path, any_scalar_key=any_scalar_key
        )
        before = len(resolved)
        for key, pair in sub_entries.items():
            resolved.setdefault(key, pair)
        active.take(len(resolved) - before, path, line)
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


def is_local_uses(raw: str) -> bool:
    """Report whether raw starts with a relative local-action prefix (./ or .\\)."""
    return raw.startswith(LOCAL_USES_PREFIXES)


def parse_uses_value(raw: str, path: str, line: int) -> str | None:
    """Validate a uses string and return owner/repo, or None for local, self and docker."""
    if is_local_uses(raw) or raw.startswith("docker://"):
        return None
    if raw.startswith("$/"):
        check_self_repo_uses(raw, path, line)
        return None
    parts = split_remote_target(raw, path, line)
    owner, repo = parts[0], parts[1]
    if not valid_repo_part(owner) or not valid_repo_part(repo):
        raise UsesError(f"{path}:{line}: invalid repository in uses reference {raw!r}")
    return f"{owner}/{repo}"


def keep_uses_value(raw: str, path: str, line: int) -> str:
    """Return raw unchanged as the --all-uses decoder."""
    _ = (path, line)
    return raw


def mapping_uses_scalar(
    mapping: yaml.MappingNode, path: str, *, walk: DocWalk | None = None
) -> tuple[dict[str, tuple[yaml.ScalarNode, yaml.Node]], tuple[str, int] | None]:
    """Resolve mapping and return its optional string uses key as (raw, line)."""
    fields = (
        walk.resolve(mapping, path)
        if walk is not None
        else resolve_mapping(mapping, path)
    )
    if "uses" not in fields:
        return fields, None
    key_node, val_node = fields["uses"]
    line = node_line(key_node, val_node)
    if not isinstance(val_node, yaml.ScalarNode) or val_node.tag != STR_TAG:
        raise UsesError(f"{path}:{line}: uses value must be a string")
    return fields, (val_node.value, line)


def extract_mapping_uses(
    mapping: yaml.MappingNode, path: str, *, walk: DocWalk | None = None
) -> tuple[dict[str, tuple[yaml.ScalarNode, yaml.Node]], tuple[str, int] | None]:
    """Resolve mapping and parse its optional uses key into (owner/repo, line)."""
    fields, raw_hit = mapping_uses_scalar(mapping, path, walk=walk)
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


def step_uses_scalar(
    step_node: yaml.Node, path: str, *, walk: DocWalk | None = None
) -> tuple[str, int] | None:
    """Validate one step mapping node and return its optional (uses_raw, line)."""
    if not isinstance(step_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{step_node.start_mark.line + 1}: step must be a mapping"
        )
    _, hit = mapping_uses_scalar(step_node, path, walk=walk)
    return hit


def collect_steps_raw_uses(
    steps_node: yaml.Node,
    path: str,
    budget: RefBudget | None = None,
    *,
    walk: DocWalk | None = None,
) -> list[tuple[str, int]]:
    """Collect raw (uses_value, line) pairs from steps_node within optional budget."""
    return collect_steps_uses(
        steps_node, path, budget, decode=keep_uses_value, walk=walk
    )


def collect_steps_uses(
    steps_node: yaml.Node,
    path: str,
    budget: RefBudget | None,
    *,
    decode: Callable[[str, str, int], str | None] = parse_uses_value,
    walk: DocWalk | None = None,
) -> list[tuple[str, int]]:
    """Collect decoded (uses_value, line) pairs from steps_node within optional budget.

    decode maps each raw uses string to the kept value or None to drop it; budget counts
    only kept references. keep_uses_value keeps every raw string.
    """
    active_walk = walk if walk is not None else DocWalk()
    found: list[tuple[str, int]] = []
    for step_node in check_steps_sequence(steps_node, path):
        active_walk.visit(path, step_node.start_mark.line + 1)
        hit = step_uses_scalar(step_node, path, walk=active_walk)
        if hit is None:
            continue
        decoded = decode(hit[0], path, hit[1])
        if decoded is None:
            continue
        if budget is not None:
            budget.take(path, hit[1])
        found.append((decoded, hit[1]))
    return found


def resolved_job_nodes(
    jobs_node: yaml.Node, path: str, *, walk: DocWalk | None = None
) -> list[yaml.MappingNode]:
    """Validate a jobs mapping node and return its job mapping nodes."""
    if isinstance(jobs_node, yaml.ScalarNode) and jobs_node.tag == NULL_TAG:
        return []
    if not isinstance(jobs_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{jobs_node.start_mark.line + 1}: jobs must be a mapping"
        )
    jobs = (
        walk.resolve(jobs_node, path, any_scalar_key=True)
        if walk is not None
        else resolve_mapping(jobs_node, path, any_scalar_key=True)
    )
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
    jobs_node: yaml.Node,
    path: str,
    budget: RefBudget,
    *,
    decode: Callable[[str, str, int], str | None] = parse_uses_value,
    walk: DocWalk | None = None,
) -> list[tuple[str, int]]:
    """Collect decoded (uses_value, line) pairs from jobs.<job> within budget."""
    active_walk = walk if walk is not None else DocWalk()
    found: list[tuple[str, int]] = []
    for job_node in resolved_job_nodes(jobs_node, path, walk=active_walk):
        job_fields, raw_hit = mapping_uses_scalar(job_node, path, walk=active_walk)
        if raw_hit is not None:
            decoded = decode(raw_hit[0], path, raw_hit[1])
            if decoded is not None:
                budget.take(path, raw_hit[1])
                found.append((decoded, raw_hit[1]))
        if "steps" in job_fields:
            found.extend(
                collect_steps_uses(
                    job_fields["steps"][1],
                    path,
                    budget,
                    decode=decode,
                    walk=active_walk,
                )
            )
    return found


def check_line_limit(text: str, display: str, max_lines: int = MAX_FILE_LINES) -> None:
    """Refuse text when its line count exceeds max_lines."""
    if len(text.splitlines()) > max_lines:
        raise UsesError(f"{display}: exceeds {max_lines} lines")


def read_bounded_fd(fd: int, max_bytes: int) -> bytes:
    """Read at most max_bytes + 1 bytes from open regular-file descriptor fd."""
    target = max(max_bytes, 0) + 1
    chunks: list[bytes] = []
    total = 0
    for _ in range(target):
        remaining = target - total
        if remaining <= 0:
            break
        chunk = os.read(fd, remaining)
        if not chunk:
            break
        chunks.append(chunk)
        total += len(chunk)
    return b"".join(chunks)


def read_workflow_text(
    path: Path, display: str, max_bytes: int = MAX_FILE_BYTES
) -> str:
    """Read regular files only; regular-file check needs no deadline (never waits for a peer)."""
    flags = os.O_RDONLY | getattr(os, "O_NONBLOCK", 0) | getattr(os, "O_BINARY", 0)
    try:
        fd = os.open(path, flags)
    except IsADirectoryError as err:
        raise UsesError(f"{display}: not a regular file") from err
    except OSError as err:
        raise UsesError(f"{display}: {err}") from err
    try:
        try:
            if not stat.S_ISREG(os.fstat(fd).st_mode):
                raise UsesError(f"{display}: not a regular file")
            raw = read_bounded_fd(fd, max_bytes)
        finally:
            os.close(fd)
    except OSError as err:
        raise UsesError(f"{display}: {err}") from err
    if len(raw) > max_bytes:
        raise UsesError(f"{display}: exceeds {max_bytes} bytes")
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError as err:
        raise UsesError(f"{display}: {err}") from err
    check_line_limit(text, display)
    return text


def check_yaml_depth(text: str, path: str, max_depth: int = MAX_YAML_DEPTH) -> None:
    """Walk YAML parse events iteratively and refuse collection depth past max_depth."""
    depth = 0
    try:
        with contextlib.closing(yaml.parse(text, Loader=yaml.SafeLoader)) as events:
            for count, event in enumerate(events, start=1):
                if count > MAX_YAML_EVENTS:
                    raise UsesError(
                        f"{path}:{event.start_mark.line + 1}: exceeds {MAX_YAML_EVENTS} YAML events"
                    )
                if isinstance(event, (yaml.MappingStartEvent, yaml.SequenceStartEvent)):
                    depth += 1
                    if depth > max_depth:
                        raise UsesError(
                            f"{path}:{event.start_mark.line + 1}: YAML nesting depth exceeds {max_depth}"
                        )
                elif isinstance(event, (yaml.MappingEndEvent, yaml.SequenceEndEvent)):
                    depth -= 1
    except yaml.YAMLError as err:
        raise UsesError(f"{path}: {err}") from err


def compose_top_mapping(
    text: str, path: str, *, walk: DocWalk | None = None
) -> dict[str, tuple[yaml.ScalarNode, yaml.Node]]:
    """Compose YAML text for path and return its resolved root mapping."""
    check_line_limit(text, path)
    check_yaml_depth(text, path)
    try:
        root = yaml.compose(text, Loader=yaml.SafeLoader)
    except (yaml.YAMLError, RecursionError) as err:
        raise UsesError(f"{path}: {err}") from err
    if not isinstance(root, yaml.MappingNode):
        raise UsesError(f"{path}: document root is not a YAML mapping")
    return walk.resolve(root, path) if walk is not None else resolve_mapping(root, path)


def runs_steps_node(
    top: dict[str, tuple[yaml.ScalarNode, yaml.Node]],
    path: str,
    *,
    walk: DocWalk | None = None,
) -> yaml.Node | None:
    """Validate optional runs mapping in top and return its steps node when present."""
    if "runs" not in top:
        return None
    runs_node = top["runs"][1]
    if not isinstance(runs_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{runs_node.start_mark.line + 1}: runs must be a mapping"
        )
    runs_fields = (
        walk.resolve(runs_node, path)
        if walk is not None
        else resolve_mapping(runs_node, path)
    )
    return runs_fields["steps"][1] if "steps" in runs_fields else None


def collect_text_uses(
    text: str,
    path: str,
    max_refs: int = MAX_REFS,
    budget: RefBudget | None = None,
    *,
    decode: Callable[[str, str, int], str | None] = parse_uses_value,
    max_step_visits: int = MAX_STEP_VISITS,
) -> list[tuple[str, int]]:
    """Compose YAML text for path and return decoded (uses_value, line) pairs."""
    active = budget if budget is not None else RefBudget(max_refs)
    walk = DocWalk(max_step_visits)
    top = compose_top_mapping(text, path, walk=walk)
    found: list[tuple[str, int]] = []
    if "jobs" in top:
        found.extend(
            collect_jobs_uses(top["jobs"][1], path, active, decode=decode, walk=walk)
        )
    steps_node = runs_steps_node(top, path, walk=walk)
    if steps_node is not None:
        found.extend(
            collect_steps_uses(steps_node, path, active, decode=decode, walk=walk)
        )
    return found


def collect_file_uses(
    path: str,
    max_refs: int = MAX_REFS,
    budget: RefBudget | None = None,
    *,
    decode: Callable[[str, str, int], str | None] = parse_uses_value,
) -> list[tuple[str, int]]:
    """Read path as UTF-8 and return decoded (uses_value, line) pairs."""
    text = read_workflow_text(Path(path), path)
    return collect_text_uses(
        text, path, max_refs=max_refs, budget=budget, decode=decode
    )


def iter_files_uses(
    files: list[str],
    max_refs: int = MAX_REFS,
    *,
    decode: Callable[[str, str, int], str | None] = parse_uses_value,
) -> Iterator[tuple[str, str, int]]:
    """Yield decoded (uses_value, file_path, line) triples across files within max_refs."""
    if len(files) > MAX_FILES:
        raise UsesError(f"more than {MAX_FILES} workflow files")
    budget = RefBudget(max_refs)
    for file_path in files:
        for value, line in collect_file_uses(file_path, budget=budget, decode=decode):
            yield (value, file_path, line)


def collect_files_uses(
    files: list[str],
    max_refs: int = MAX_REFS,
    *,
    decode: Callable[[str, str, int], str | None] = parse_uses_value,
) -> list[tuple[str, str, int]]:
    """Collect decoded (uses_value, file_path, line) triples across files within max_refs."""
    return list(iter_files_uses(files, max_refs, decode=decode))


def filter_local_uses(
    raw_steps: list[tuple[str, int]], path: str
) -> list[tuple[str, int]]:
    """Keep ./, .\\ and $/ step references from raw_steps after validating $/ syntax."""
    local_hits: list[tuple[str, int]] = []
    for raw, line in raw_steps:
        if raw.startswith("$/"):
            check_self_repo_uses(raw, path, line)
            local_hits.append((raw, line))
        elif is_local_uses(raw):
            local_hits.append((raw, line))
    return local_hits
