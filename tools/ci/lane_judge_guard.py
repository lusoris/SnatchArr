# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse staged lane output edited after the lane ended, allowing 120 seconds.

The launcher owns <git-dir>/lane-judge.json; this check only reads it. A missing
marker permits ordinary worktrees, and MERGE_HEAD permits judge conflict resolution.
The staged set is read from the index git records (GIT_INDEX_FILE when git sets it),
so the hook wiring keeps it. Staged deletions (D) pass: they have no worktree file
whose mtime can be checked. Exit 0 = permitted, 1 = refused, 2 = invalid input or
failed inspection.
"""

from __future__ import annotations

import json
import os
import subprocess  # nosec B404  # fixed git argument lists, never a shell
import sys
from datetime import UTC, datetime
from pathlib import Path

MAX_RUN_ROWS = 200000
MAX_STAGED_PATHS = 5000
GRACE_SECONDS = 120
REFUSAL = "send the change back to a lane fix round; the judge does not edit lane output"


def git(args: list[str], root: Path | None = None) -> subprocess.CompletedProcess:
    """Run one bounded Git inspection, leaving status interpretation to its caller."""
    return subprocess.run(  # nosec B603  # fixed git executable and argument lists
        ["git", "--literal-pathspecs", *args], cwd=root, capture_output=True, check=False, timeout=30
    )


def git_output(args: list[str], root: Path | None = None) -> bytes:
    """Return successful Git output or fail closed with its diagnostic."""
    result = git(args, root)
    if result.returncode != 0:
        error = result.stderr.decode("utf-8", errors="replace").strip()
        raise ValueError(f"git {' '.join(args)} failed ({result.returncode}): {error}")
    return result.stdout


def parse_time(raw: str, where: str) -> datetime:
    """Require an ISO-8601 timestamp with an explicit UTC offset."""
    try:
        value = datetime.fromisoformat(raw)
    except ValueError as error:
        raise ValueError(f"{where}: bad time {raw!r}") from error
    if value.utcoffset() is None:
        raise ValueError(f"{where}: bad time {raw!r}: UTC offset required")
    return value


def unique_object(pairs: list[tuple[str, object]]) -> dict:
    """Reject duplicate JSON keys rather than silently replacing marker data."""
    result = dict(pairs)
    if len(result) != len(pairs):
        raise ValueError("lane marker has duplicate keys")
    return result


def read_marker(path: Path) -> dict | None:
    """Validate the launcher's exact marker shape, preserving missing-marker silence."""
    try:
        path.lstat()
    except FileNotFoundError:
        return None
    marker = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique_object)
    if not isinstance(marker, dict) or set(marker) != {"label", "runs", "launched_at"}:
        raise ValueError("lane marker must have exactly keys: label, runs, launched_at")
    if not all(isinstance(value, str) for value in marker.values()):
        raise ValueError("lane marker label, runs and launched_at must be strings")
    if not Path(marker["runs"]).is_absolute():
        raise ValueError("lane marker runs must be an absolute path")
    parse_time(marker["launched_at"], "lane marker launched_at")
    if not Path(marker["runs"]).is_file():
        raise ValueError(f"lane runs file missing or not a file: {marker['runs']}")
    return marker


def latest_exit(marker: dict) -> datetime | None:
    """Find the latest matching exit at or after launch within the row bound."""
    launched = parse_time(marker["launched_at"], "lane marker launched_at")
    latest = None
    with Path(marker["runs"]).open(encoding="utf-8") as runs:
        for number in range(MAX_RUN_ROWS):
            row = runs.readline()
            if not row:
                return latest
            columns = row.rstrip("\r\n").split("\t")
            if len(columns) < 3 or columns[1:3] != [marker["label"], "exit"]:
                continue
            ended = parse_time(columns[0], f"lane runs row {number + 1}")
            if ended >= launched and (latest is None or ended > latest):
                latest = ended
        if runs.read(1):
            raise ValueError(f"lane runs file exceeds {MAX_RUN_ROWS} rows")
    return latest


def staged_paths(root: Path) -> list[str]:
    """List staged non-deletions with NUL delimiters and a scalar path bound."""
    output = git_output(["diff", "--cached", "--name-only", "-z", "--diff-filter=ACMRT"], root)
    paths = [os.fsdecode(path) for path in output.split(b"\0") if path]
    if len(paths) > MAX_STAGED_PATHS:
        raise ValueError(f"staged paths exceed {MAX_STAGED_PATHS}")
    return paths


def check_path(root: Path, path: str, ended: datetime, label: str) -> bool:
    """Refuse unstaged changes or a worktree mtime beyond the exit grace period."""
    result = git(["diff", "--quiet", "--", path], root)
    if result.returncode == 1:
        print(f"refused: lane {label} path {path!r} has an unstaged change; {REFUSAL}", file=sys.stderr)
        return False
    if result.returncode != 0:
        error = result.stderr.decode("utf-8", errors="replace").strip()
        raise ValueError(f"git diff failed for {path!r} ({result.returncode}): {error}")
    mtime = (root / path).lstat().st_mtime
    if mtime > ended.timestamp() + GRACE_SECONDS:
        modified = datetime.fromtimestamp(mtime, UTC).isoformat()
        print(
            f"refused: lane {label} path {path!r} mtime {modified} exceeds exit time {ended.isoformat()}"
            f" + {GRACE_SECONDS}s; {REFUSAL}",
            file=sys.stderr,
        )
        return False
    return True


def check() -> int:
    """Inspect the per-worktree marker and staged paths without mutating either."""
    git_dir = Path(os.fsdecode(git_output(["rev-parse", "--git-dir"]).rstrip(b"\n")))
    marker = read_marker(git_dir / "lane-judge.json")
    if marker is None:
        return 0
    if (git_dir / "MERGE_HEAD").exists():
        print("lane judge guard: merge commit, conflict resolution is the judge's job", file=sys.stderr)
        return 0
    ended = latest_exit(marker)
    if ended is None:
        print(
            f"lane {marker['label']} has no exit row after {marker['launched_at']}; wait for it to end; {REFUSAL}",
            file=sys.stderr,
        )
        return 1
    root = Path(os.fsdecode(git_output(["rev-parse", "--show-toplevel"]).rstrip(b"\n")))
    permitted = True
    for path in staged_paths(root):
        if not check_path(root, path, ended, marker["label"]):
            permitted = False
    return 0 if permitted else 1


def main() -> int:
    """Report invalid or unreadable inputs as exit 2, never an implicit pass."""
    try:
        return check()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"lane judge guard: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
