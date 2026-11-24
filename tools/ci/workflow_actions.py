# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Local composite action discovery, step scalar text, and step version input readers.

Implements --list-actions, --step-text and --version-inputs over workflow_yaml's
bounded YAML composer without recursion.
"""

from __future__ import annotations

import os
import stat
import sys
from collections import deque
from collections.abc import Callable, Iterable
from pathlib import Path

try:
    import yaml
except ImportError:
    yaml = None

sys.path.insert(0, str(Path(__file__).resolve().parent))

from workflow_yaml import (
    ACTION_FILENAMES,
    BAD_PATH_CHARS,
    DOCKER_FILENAMES,
    MAX_FILE_LINES,
    MAX_FILES,
    MAX_REFS,
    MAX_STEP_TEXT_LINES,
    MAX_WALK_DIRS,
    NULL_TAG,
    DocWalk,
    RefBudget,
    UsesError,
    check_self_repo_uses,
    check_steps_sequence,
    collect_steps_raw_uses,
    compose_top_mapping,
    extract_mapping_uses,
    filter_local_uses,
    mapping_uses_scalar,
    node_line,
    read_workflow_text,
    resolve_mapping,
    resolved_job_nodes,
    runs_steps_node,
)


def collect_local_step_uses(
    full_path: Path, rel_path: str, budget: RefBudget | None = None
) -> list[tuple[str, int]]:
    """Read full_path and return step-level ./<dir>, .\\<dir> and $/<dir> (raw, line) pairs."""
    active = budget if budget is not None else RefBudget(MAX_REFS)
    walk = DocWalk()
    text = read_workflow_text(full_path, rel_path)
    top = compose_top_mapping(text, rel_path, walk=walk)
    raw_steps: list[tuple[str, int]] = []
    if "jobs" in top:
        for job_node in resolved_job_nodes(top["jobs"][1], rel_path, walk=walk):
            job_fields, job_hit = mapping_uses_scalar(job_node, rel_path, walk=walk)
            if job_hit is not None and job_hit[0].startswith("$/"):
                check_self_repo_uses(job_hit[0], rel_path, job_hit[1])
            if "steps" in job_fields:
                raw_steps.extend(
                    collect_steps_raw_uses(
                        job_fields["steps"][1], rel_path, active, walk=walk
                    )
                )
    steps_node = runs_steps_node(top, rel_path, walk=walk)
    if steps_node is not None:
        raw_steps.extend(
            collect_steps_raw_uses(steps_node, rel_path, active, walk=walk)
        )
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
    """Validate ./<dir>, .\\<dir> or $/<dir> syntax and return its resolved in-root Path."""
    if raw.startswith("$/"):
        check_self_repo_uses(raw, path, line)
    sub = raw[2:]
    if (
        any(ch in raw for ch in ("\x00", "\r", "\n"))
        or sub.startswith(("/", "\\"))
        or Path(sub).is_absolute()
    ):
        raise UsesError(f"{path}:{line}: invalid local action {raw!r}")
    if raw.startswith(".\\"):
        sub = sub.replace("\\", "/")
    try:
        target_dir = (root / sub).resolve()
    except OSError as err:
        raise UsesError(f"{path}:{line}: local action {raw!r}: {err}") from err
    if not target_dir.is_relative_to(root):
        raise UsesError(f"{path}:{line}: local action {raw!r} escapes repository root")
    return target_dir


def resolve_local_action(root: Path, raw: str, path: str, line: int) -> str | None:
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


def file_step_sequences(
    text: str, rel_path: str, seen_nodes: set[int]
) -> list[yaml.Node]:
    """Compose text for rel_path and return job and composite steps sequence nodes."""
    top = compose_top_mapping(text, rel_path)
    seqs: list[yaml.Node] = []
    if "jobs" in top:
        for job_node in resolved_job_nodes(top["jobs"][1], rel_path):
            if id(job_node) in seen_nodes:
                continue
            seen_nodes.add(id(job_node))
            job_fields = resolve_mapping(job_node, rel_path)
            if "steps" in job_fields:
                seqs.append(job_fields["steps"][1])
    steps_node = runs_steps_node(top, rel_path)
    if steps_node is not None:
        seqs.append(steps_node)
    return seqs


def scalar_line_number(val_node: yaml.ScalarNode, idx: int) -> int:
    """Compute the 1-based source line for non-folded scalar val_node at line idx."""
    if val_node.style == "|":
        return val_node.start_mark.line + 2 + idx
    return val_node.start_mark.line + 1


def folded_body_lines(val_node: yaml.ScalarNode, path: str) -> list[str]:
    """Extract raw body lines after the header from a folded scalar's mark buffer."""
    mark = val_node.start_mark
    end_mark = val_node.end_mark
    if mark is None or end_mark is None or not isinstance(mark.buffer, str):
        line = mark.line + 1 if mark is not None else 1
        raise UsesError(f"{path}:{line}: folded scalar has no source buffer")
    body = mark.buffer[mark.index : end_mark.index].splitlines()[1:]
    if len(body) > MAX_FILE_LINES:
        raise UsesError(
            f"{path}:{mark.line + 1}: scalar spans more than {MAX_FILE_LINES} lines"
        )
    return body


def next_folded_body_idx(
    body: list[str], start_idx: int, val_stripped: str, path: str, base_line: int
) -> int:
    """Find the first body index from start_idx matching val_stripped within MAX_FILE_LINES."""
    limit = min(len(body), MAX_FILE_LINES)
    for idx in range(start_idx, limit):
        body_stripped = body[idx].lstrip(" ")
        if not val_stripped:
            if not body_stripped:
                return idx
        elif body_stripped and val_stripped.startswith(body_stripped):
            return idx
    raise UsesError(f"{path}:{base_line}: folded scalar value does not match source")


def consume_folded_rest(body: list[str], start_idx: int, rest: str) -> int:
    """Advance past folded continuation lines in body matching rest within MAX_FILE_LINES."""
    limit = min(len(body), MAX_FILE_LINES)
    offset = 0
    after = start_idx
    for idx in range(start_idx, limit):
        nxt = body[idx].lstrip(" ")
        if not nxt or not rest.startswith(" " + nxt, offset):
            break
        offset += len(nxt) + 1
        after = idx + 1
    return after


def folded_line_numbers(
    val_node: yaml.ScalarNode, parts: list[str], path: str
) -> list[int]:
    """Map each value line in parts of a folded scalar to its 1-based source line."""
    body = folded_body_lines(val_node, path)
    base_line = val_node.start_mark.line + 1
    cursor = 0
    lines: list[int] = []
    for raw_line in parts[:MAX_FILE_LINES]:
        val_stripped = raw_line.lstrip(" ")
        matched = next_folded_body_idx(body, cursor, val_stripped, path, base_line)
        lines.append(val_node.start_mark.line + 2 + matched)
        body_stripped = body[matched].lstrip(" ")
        cursor = consume_folded_rest(
            body, matched + 1, val_stripped[len(body_stripped) :]
        )
    return lines


def scalar_value_lines(val_node: yaml.ScalarNode, path: str) -> list[tuple[int, str]]:
    """Split scalar val_node into 1-based (line_no, cleaned_text) pairs."""
    parts = val_node.value.split("\n")
    if parts and parts[-1] == "":
        parts.pop()
    if len(parts) > MAX_FILE_LINES:
        raise UsesError(
            f"{path}:{val_node.start_mark.line + 1}: scalar spans more than {MAX_FILE_LINES} lines"
        )
    if val_node.style == ">":
        line_nos = folded_line_numbers(val_node, parts, path)
    else:
        line_nos = [scalar_line_number(val_node, idx) for idx in range(len(parts))]
    return [
        (
            line_no,
            raw_line.replace("\x00", " ").replace("\r", " "),
        )
        for line_no, raw_line in zip(line_nos, parts, strict=True)
    ]


def append_scalar_lines(
    val_node: yaml.ScalarNode,
    path: str,
    out: list[tuple[int, str]],
    seen_nodes: set[int],
) -> None:
    """Expand val_node at most once per file into out within MAX_STEP_TEXT_LINES."""
    if id(val_node) in seen_nodes:
        return
    seen_nodes.add(id(val_node))
    for line_no, clean_line in scalar_value_lines(val_node, path):
        if len(out) >= MAX_STEP_TEXT_LINES:
            raise UsesError(
                f"{path}:{line_no}: more than {MAX_STEP_TEXT_LINES} step text lines"
            )
        out.append((line_no, clean_line))


def collect_with_lines(
    with_node: yaml.Node,
    path: str,
    out: list[tuple[int, str]],
    seen_nodes: set[int],
) -> None:
    """Append scalar value lines for every scalar entry inside a step with mapping."""
    if isinstance(with_node, yaml.ScalarNode) and with_node.tag == NULL_TAG:
        return
    if not isinstance(with_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{with_node.start_mark.line + 1}: with must be a mapping"
        )
    if id(with_node) in seen_nodes:
        return
    seen_nodes.add(id(with_node))
    for _, val_node in resolve_mapping(with_node, path).values():
        if isinstance(val_node, yaml.ScalarNode):
            append_scalar_lines(val_node, path, out, seen_nodes)


def collect_step_lines(
    step_node: yaml.Node,
    path: str,
    out: list[tuple[int, str]],
    seen_nodes: set[int],
) -> None:
    """Append value lines of run and scalar with entries from one step mapping."""
    if not isinstance(step_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{step_node.start_mark.line + 1}: step must be a mapping"
        )
    for key, (key_node, val_node) in resolve_mapping(step_node, path).items():
        if key == "run":
            if not isinstance(val_node, yaml.ScalarNode):
                raise UsesError(
                    f"{path}:{node_line(key_node, val_node)}: run value must be a scalar"
                )
            append_scalar_lines(val_node, path, out, seen_nodes)
        elif key == "with":
            collect_with_lines(val_node, path, out, seen_nodes)


def collect_steps_seq_lines(
    steps_node: yaml.Node,
    path: str,
    out: list[tuple[int, str]],
    seen_nodes: set[int],
) -> None:
    """Append step run and with scalar lines for every step in steps_node."""
    if id(steps_node) in seen_nodes:
        return
    seen_nodes.add(id(steps_node))
    for step_node in check_steps_sequence(steps_node, path):
        if id(step_node) in seen_nodes:
            continue
        seen_nodes.add(id(step_node))
        collect_step_lines(step_node, path, out, seen_nodes)


def collect_text_step_lines(text: str, rel_path: str) -> list[tuple[int, str]]:
    """Return 1-based (line_no, scalar_line) pairs in document order for rel_path."""
    out: list[tuple[int, str]] = []
    seen_nodes: set[int] = set()
    for steps_node in file_step_sequences(text, rel_path, seen_nodes):
        collect_steps_seq_lines(steps_node, rel_path, out, seen_nodes)
    return out


def append_step_records(
    rel_path: str,
    pairs: list[tuple[int, str]],
    seen_records: set[tuple[str, int, str]],
    records: list[tuple[str, int, str]],
) -> None:
    """Append distinct (rel_path, line_no, text) records within MAX_STEP_TEXT_LINES."""
    for line_no, line_text in pairs:
        rec = (rel_path, line_no, line_text)
        if rec in seen_records:
            continue
        if len(records) >= MAX_STEP_TEXT_LINES:
            raise UsesError(
                f"{rel_path}:{line_no}: more than {MAX_STEP_TEXT_LINES} step text lines"
            )
        seen_records.add(rec)
        records.append(rec)


def collect_files_step_text(
    root_arg: str, files: list[str]
) -> list[tuple[str, int, str]]:
    """Return distinct (rel_path, line_no, text) records in document order under root_arg."""
    if len(files) > MAX_FILES:
        raise UsesError(f"more than {MAX_FILES} workflow files under {root_arg}")
    root = resolve_root_dir(root_arg)
    seen_files: set[str] = set()
    seen_records: set[tuple[str, int, str]] = set()
    records: list[tuple[str, int, str]] = []
    for file_arg in files:
        rel_path = normalize_step_file(root, file_arg)
        if rel_path in seen_files:
            continue
        seen_files.add(rel_path)
        text = read_workflow_text(root / rel_path, rel_path)
        append_step_records(
            rel_path, collect_text_step_lines(text, rel_path), seen_records, records
        )
    return records


def clean_version_scalar(ver_node: yaml.Node) -> str:
    """Return raw text of scalar ver_node, or empty when non-scalar or holding control chars."""
    if not isinstance(ver_node, yaml.ScalarNode):
        return ""
    if any(ch in ver_node.value for ch in BAD_PATH_CHARS):
        return ""
    return ver_node.value


def extract_with_version(
    fields: dict[str, tuple[yaml.ScalarNode, yaml.Node]],
    path: str,
    *,
    walk: DocWalk | None = None,
) -> str:
    """Return raw scalar with.version text, or empty when absent, non-scalar or unsafe.

    The runner hands a with key to the action as INPUT_<key upper-cased>, so Version and
    VERSION name the same input as version; two such keys in one with mapping raise UsesError.
    """
    if "with" not in fields:
        return ""
    with_node = fields["with"][1]
    if isinstance(with_node, yaml.ScalarNode) and with_node.tag == NULL_TAG:
        return ""
    if not isinstance(with_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{with_node.start_mark.line + 1}: with must be a mapping"
        )
    with_fields = (
        walk.resolve(with_node, path)
        if walk is not None
        else resolve_mapping(with_node, path)
    )
    named = [
        (key, value)
        for name, (key, value) in with_fields.items()
        if name.upper() == "VERSION"
    ]
    if not named:
        return ""
    if len(named) > 1:
        lines = sorted(key.start_mark.line + 1 for key, _ in named)
        raise UsesError(
            f"{path}:{lines[-1]}: with names the version input twice"
            f" (lines {', '.join(map(str, lines))})"
        )
    return clean_version_scalar(named[0][1])


def collect_step_version_input(
    step_node: yaml.Node,
    path: str,
    budget: RefBudget,
    *,
    walk: DocWalk | None = None,
) -> tuple[int, str, str] | None:
    """Extract (line, owner/repo, version) from one remote-uses step mapping."""
    if not isinstance(step_node, yaml.MappingNode):
        raise UsesError(
            f"{path}:{step_node.start_mark.line + 1}: step must be a mapping"
        )
    fields, hit = extract_mapping_uses(step_node, path, walk=walk)
    if hit is None:
        return None
    repo, line = hit
    budget.take(path, line)
    return line, repo, extract_with_version(fields, path, walk=walk)


def collect_steps_version_inputs(
    steps_node: yaml.Node,
    path: str,
    budget: RefBudget,
    out: list[tuple[int, str, str]],
    *,
    walk: DocWalk | None = None,
) -> None:
    """Append (line, owner/repo, version) triples for remote-uses steps in steps_node."""
    active_walk = walk if walk is not None else DocWalk()
    for step_node in check_steps_sequence(steps_node, path):
        active_walk.visit(path, step_node.start_mark.line + 1)
        entry = collect_step_version_input(step_node, path, budget, walk=active_walk)
        if entry is not None:
            out.append(entry)


def collect_text_version_inputs(
    text: str, rel_path: str, budget: RefBudget
) -> list[tuple[int, str, str]]:
    """Return (line, owner/repo, version) triples for remote step uses in text."""
    walk = DocWalk()
    top = compose_top_mapping(text, rel_path, walk=walk)
    out: list[tuple[int, str, str]] = []
    if "jobs" in top:
        for job_node in resolved_job_nodes(top["jobs"][1], rel_path, walk=walk):
            job_fields = walk.resolve(job_node, rel_path)
            if "steps" in job_fields:
                collect_steps_version_inputs(
                    job_fields["steps"][1], rel_path, budget, out, walk=walk
                )
    steps_node = runs_steps_node(top, rel_path, walk=walk)
    if steps_node is not None:
        collect_steps_version_inputs(steps_node, rel_path, budget, out, walk=walk)
    return out


def collect_files_version_inputs(
    root_arg: str, files: list[str], max_refs: int = MAX_REFS
) -> list[tuple[str, int, str, str]]:
    """Return distinct (rel_path, line, owner/repo, version) records under root_arg."""
    if len(files) > MAX_FILES:
        raise UsesError(f"more than {MAX_FILES} workflow files under {root_arg}")
    root = resolve_root_dir(root_arg)
    budget = RefBudget(max_refs)
    seen_files: set[str] = set()
    seen_records: set[tuple[str, int, str, str]] = set()
    records: list[tuple[str, int, str, str]] = []
    for file_arg in files:
        rel_path = normalize_step_file(root, file_arg)
        if rel_path in seen_files:
            continue
        seen_files.add(rel_path)
        text = read_workflow_text(root / rel_path, rel_path)
        for line, repo, version in collect_text_version_inputs(text, rel_path, budget):
            rec = (rel_path, line, repo, version)
            if rec not in seen_records:
                seen_records.add(rec)
                records.append(rec)
    return records
