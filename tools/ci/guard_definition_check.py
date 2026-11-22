# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse a pull request that changes the workflow-refs guard unless the merger acknowledges it.

Usage: python3 -I guard_definition_check.py --git-dir DIR --base REV --merge REV --pr N

GitHub runs a pull_request workflow from the pull request's merge commit, so a pull request can
edit .github/workflows/workflow-refs.yml, or a script its steps run, and still report the
required check. This check compares the merge commit (--merge) with its base (--base) in the git
directory DIR and labels every changed guard item:

- the workflow as a YAML event stream: comments, indentation and flow or block layout do not
  count; scalar text, plain or quoted, tags, anchors, key order and duplicate keys do;
- the validator files: every tools/ci path the run steps of job workflow-refs name, then every
  tools/ci path, $HERE/<name> path or Python import those files name, by mode and object id;
- this check itself (SELF) and its commit-msg command in lefthook.yml.

A change passes only with GUARD_DEFINITION_ACK='<N>: <reason>' in the environment, never from a
commit message. Every git call runs under a 10 s timeout. Messages go to stdout; usage errors and
a missing PyYAML go to stderr.

Exit status: 0 = guard unchanged, first introduction or change acknowledged; 1 = change refused,
a git read failed or a limit passed; 2 = usage error or PyYAML missing.
"""

from __future__ import annotations

import argparse
import collections
import os
import re
import subprocess  # nosec B404  # runs only git with a fixed argument list, never a shell
import sys
from types import ModuleType

WORKFLOW = ".github/workflows/workflow-refs.yml"
JOB = "workflow-refs"
SELF = ("tools/ci/guard_definition_check.sh", "tools/ci/guard_definition_check.py")
HOOK_FILE = "lefthook.yml"
HOOK_COMMAND = "guard-definition"
MAX_FILES = 64
MAX_CANDIDATES = 512
MAX_BYTES = 1024 * 1024
MAX_EVENTS = 200_000
GIT_TIMEOUT_SECONDS = 10
PR_RE = re.compile(r"[1-9][0-9]{0,6}")
ACK_RE = re.compile(r"([1-9][0-9]{0,6}): ([ -~]{20,200})")
ACK_FORM = "'<N>: <reason, 20 to 200 printable characters>'"
CI_TOKEN = re.compile(r"tools/ci/([A-Za-z0-9_.-]+)")
HERE_TOKEN = re.compile(r"\$\{?HERE\}?/([A-Za-z0-9_.-]+)")
PY_IMPORT = re.compile(r"^\s*(?:from|import)\s+([A-Za-z_][A-Za-z0-9_]*)", re.MULTILINE)
EVENT_FIELDS = ("anchor", "tag", "implicit", "value", "version", "tags")


class GuardError(Exception):
    """A git read failed or a limit passed; main prints the message and exits 1."""


def git(git_dir: str, *args: str) -> bytes:
    """Return the stdout of `git --git-dir git_dir args`; raise GuardError on failure or timeout."""
    try:
        res = subprocess.run(  # nosec B603  # fixed git argv list, no shell, revisions checked
            ["git", "--literal-pathspecs", "--git-dir", git_dir, *args],
            capture_output=True,
            timeout=GIT_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise GuardError(f"git {' '.join(args)}: {exc}") from exc
    if res.returncode != 0:
        detail = res.stderr.decode("utf-8", "replace").strip()
        raise GuardError(f"git {' '.join(args)} exited {res.returncode}: {detail}")
    return res.stdout


def commit(git_dir: str, rev: str) -> str:
    """Return the commit id that rev names in git_dir; raise GuardError when it names none."""
    try:
        out = git(git_dir, "rev-parse", "--verify", "--quiet", f"{rev}^{{commit}}")
    except GuardError as exc:
        raise GuardError(f"revision {rev} names no commit") from exc
    return out.decode("ascii", "replace").strip()


def entry(git_dir: str, rev: str, path: str) -> tuple[str, str, str] | None:
    """Return (mode, type, object id) of path at rev, or None when rev has no such path."""
    out = git(git_dir, "ls-tree", "--full-tree", "-z", rev, "--", path)
    for record in out.split(b"\0")[:2]:
        meta, _, name = record.partition(b"\t")
        fields = meta.decode("ascii", "replace").split(" ")
        if name.decode("utf-8", "replace") == path and len(fields) == 3:
            return fields[0], fields[1], fields[2]
    return None


def read_text(git_dir: str, rev: str, path: str) -> str | None:
    """Return the text of path at rev ('' for a non-blob), None when absent; refuse > MAX_BYTES."""
    found = entry(git_dir, rev, path)
    if found is None:
        return None
    if found[1] != "blob":
        return ""
    size = int(
        git(git_dir, "cat-file", "-s", found[2]).decode("ascii", "replace").strip()
    )
    if size > MAX_BYTES:
        raise GuardError(f"{path} at {rev[:12]} is larger than MAX_BYTES ({MAX_BYTES})")
    return git(git_dir, "cat-file", "blob", found[2]).decode("utf-8", "replace")


def events(yaml: ModuleType, text: str | None) -> object:
    """Return the YAML event stream of text without layout, None for None.

    An unparsable document becomes a tuple holding its text, so it equals only the same text.
    """
    if text is None:
        return None
    stream = yaml.parse(text, Loader=yaml.SafeLoader)
    out: list[tuple] = []
    try:
        for _, event in zip(range(MAX_EVENTS + 1), stream, strict=False):
            out.append(
                (type(event).__name__, *(getattr(event, f, None) for f in EVENT_FIELDS))
            )
    except yaml.YAMLError:
        return ("unparsable", text)
    if len(out) > MAX_EVENTS:
        raise GuardError(f"{WORKFLOW} passed MAX_EVENTS ({MAX_EVENTS})")
    return out


def load(yaml: ModuleType, text: str | None) -> object:
    """Return yaml.safe_load of text, None for None or an unparsable document."""
    if text is None:
        return None
    try:
        return yaml.safe_load(text)
    except yaml.YAMLError:
        return None


def seeds(doc: object) -> list[str]:
    """Return the tools/ci paths in the run strings of jobs[JOB].steps of the workflow doc."""
    jobs = doc.get("jobs") if isinstance(doc, dict) else None
    job = jobs.get(JOB) if isinstance(jobs, dict) else None
    steps = job.get("steps") if isinstance(job, dict) else None
    paths: list[str] = []
    for step in steps if isinstance(steps, list) else []:
        run = step.get("run") if isinstance(step, dict) else None
        if isinstance(run, str):
            paths.extend(f"tools/ci/{name}" for name in CI_TOKEN.findall(run))
    return paths


def references(path: str, text: str) -> list[str]:
    """Return the tools/ci paths that text (the file at path) names by path, $HERE or import."""
    names = CI_TOKEN.findall(text) + HERE_TOKEN.findall(text)
    if path.endswith(".py"):
        names.extend(f"{module}.py" for module in PY_IMPORT.findall(text))
    return [f"tools/ci/{name}" for name in names]


def enqueue(queue: collections.deque[str], queued: set[str], paths: list[str]) -> None:
    """Append each path not queued before; raise GuardError past MAX_CANDIDATES queued paths."""
    for path in paths:
        if path in queued:
            continue
        queued.add(path)
        queue.append(path)
        if len(queued) > MAX_CANDIDATES:
            raise GuardError(
                f"validator candidates passed MAX_CANDIDATES ({MAX_CANDIDATES})"
            )


def validator_files(git_dir: str, rev: str, doc: object) -> set[str]:
    """Return the validator files present at rev: seeds from the workflow, then their references."""
    found: set[str] = set()
    queued: set[str] = set()
    queue: collections.deque[str] = collections.deque()
    enqueue(queue, queued, seeds(doc))
    for _ in range(MAX_CANDIDATES + 1):
        if not queue:
            return found
        path = queue.popleft()
        text = read_text(git_dir, rev, path)
        if text is None:
            continue
        found.add(path)
        if len(found) > MAX_FILES:
            raise GuardError(f"validator file set passed MAX_FILES ({MAX_FILES})")
        enqueue(queue, queued, references(path, text))
    raise GuardError(f"validator candidates passed MAX_CANDIDATES ({MAX_CANDIDATES})")


def hook_entry(doc: object) -> object:
    """Return the commit-msg command HOOK_COMMAND of a lefthook doc, or None when it has none."""
    hook = doc.get("commit-msg") if isinstance(doc, dict) else None
    commands = hook.get("commands") if isinstance(hook, dict) else None
    return commands.get(HOOK_COMMAND) if isinstance(commands, dict) else None


def changes(
    yaml: ModuleType, git_dir: str, base: str, merge: str
) -> tuple[list[str], int]:
    """Return (sorted labels of changed guard items, number of files compared)."""
    labels: list[str] = []
    texts = [read_text(git_dir, rev, WORKFLOW) for rev in (base, merge)]
    if events(yaml, texts[0]) != events(yaml, texts[1]):
        labels.append(WORKFLOW)
    files: set[str] = set()
    for rev, text in zip((base, merge), texts, strict=True):
        files |= validator_files(git_dir, rev, load(yaml, text))
    compared = sorted(files.union(SELF))
    for path in compared:
        if entry(git_dir, base, path) != entry(git_dir, merge, path):
            labels.append(path)
    hooks = [
        hook_entry(load(yaml, read_text(git_dir, rev, HOOK_FILE)))
        for rev in (base, merge)
    ]
    if hooks[0] is not None and hooks[0] != hooks[1]:
        labels.append(f"{HOOK_FILE}: commit-msg {HOOK_COMMAND}")
    return sorted(labels), len(compared) + 2


def acknowledged(pr: str) -> bool:
    """Report whether GUARD_DEFINITION_ACK acknowledges pull request pr; print why it does not."""
    ack = os.environ.get("GUARD_DEFINITION_ACK")
    if ack is None:
        return False
    match = ACK_RE.fullmatch(ack)
    if match is None:
        print(
            f"guard-definition: GUARD_DEFINITION_ACK ignored: not in the form {ACK_FORM}"
        )
        return False
    if match.group(1) != pr:
        print(
            f"guard-definition: GUARD_DEFINITION_ACK ignored: "
            f"names pull request {match.group(1)}, not {pr}"
        )
        return False
    print(
        f"guard-definition: change acknowledged for pull request {pr}: {match.group(2)}"
    )
    return True


def parse_args(argv: list[str]) -> argparse.Namespace:
    """Parse the command line; argparse exits 2 on a usage error."""
    parser = argparse.ArgumentParser(
        description="Compare the workflow-refs guard of a merge commit with its base."
    )
    parser.add_argument("--git-dir", required=True)
    parser.add_argument("--base", required=True)
    parser.add_argument("--merge", required=True)
    parser.add_argument("--pr", required=True)
    args = parser.parse_args(argv)
    if PR_RE.fullmatch(args.pr) is None:
        parser.error(f"--pr must be a whole number from 1 to 9999999, got {args.pr!r}")
    for rev in (args.base, args.merge):
        if not rev or rev.startswith("-"):
            parser.error(
                f"a revision must be non-empty and not start with '-', got {rev!r}"
            )
    return args


def load_yaml() -> ModuleType | None:
    """Return the PyYAML module, or None after printing that it is missing."""
    try:
        import yaml
    except ImportError:
        print("guard-definition: PyYAML is required", file=sys.stderr)
        return None
    return yaml


def refuse(pr: str, labels: list[str]) -> int:
    """Print the change labels; return 0 when GUARD_DEFINITION_ACK acknowledges them, else 1."""
    for label in labels:
        print(f"guard-definition: {label}: changed by pull request {pr}")
    if acknowledged(pr):
        return 0
    print(
        f"refusing: pull request {pr} changes the workflow-refs guard; review the change, "
        f"then merge with GUARD_DEFINITION_ACK='{pr}: <reason, 20 to 200 printable characters>'"
    )
    return 1


def main(argv: list[str] | None = None) -> int:
    """Run the check; see the module docstring for usage and exit status."""
    args = parse_args(sys.argv[1:] if argv is None else argv)
    yaml = load_yaml()
    if yaml is None:
        return 2
    try:
        base, merge = commit(args.git_dir, args.base), commit(args.git_dir, args.merge)
        if entry(args.git_dir, base, WORKFLOW) is None:
            print(
                f"guard-definition: default branch has no {WORKFLOW}; first introduction passes"
            )
            return 0
        labels, compared = changes(yaml, args.git_dir, base, merge)
    except GuardError as exc:
        print(f"guard-definition: {exc}")
        return 1
    if labels:
        return refuse(args.pr, labels)
    print(
        f"guard-definition: pull request {args.pr} leaves the workflow-refs guard unchanged "
        f"({compared} files compared)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
