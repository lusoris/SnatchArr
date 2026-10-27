#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Collect changed generated files and write a plurano/horistes artifact manifest.

Reads .github/generated-files, compares each listed path in the working tree
against git show HEAD:<path>, and writes changed files to <out>/files/<path>
alongside <out>/manifest.json (schema horistes.generated-files.v1). When no
listed file changed, nothing is written to <out>.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any

SCHEMA = "horistes.generated-files.v1"
DEFAULT_LIST = ".github/generated-files"
GIT_TIMEOUT_SECONDS = 30
MAX_LIST_LINES = 1024


class CheckError(Exception):
    """Raised when an allowlist path or git lookup is invalid."""


def validate_listed_path(path_str: str, seen: set[str]) -> str:
    """Refuse absolute, parent-traversal, backslash, or duplicate paths."""
    if (
        "\\" in path_str
        or path_str.startswith("/")
        or os.path.isabs(path_str)
        or ".." in path_str
    ):
        raise CheckError(f"unsafe listed path: {path_str!r}")
    if path_str in seen:
        raise CheckError(f"duplicate listed path: {path_str!r}")
    seen.add(path_str)
    return path_str


def read_allowlist(list_file: Path) -> list[str]:
    """Parse non-empty, non-comment paths from .github/generated-files."""
    try:
        lines = list_file.read_text(encoding="utf-8").splitlines()
    except OSError as err:
        raise CheckError(f"cannot read allowlist {list_file}: {err}") from err
    if len(lines) > MAX_LIST_LINES:
        raise CheckError(f"allowlist {list_file} exceeds {MAX_LIST_LINES} lines")
    paths: list[str] = []
    seen: set[str] = set()
    for line in lines[:MAX_LIST_LINES]:
        entry = line.split("#", 1)[0].strip()
        if not entry:
            continue
        paths.append(validate_listed_path(entry, seen))
    return paths


def run_git_bytes(
    args: list[str], root: Path, timeout: int = GIT_TIMEOUT_SECONDS
) -> bytes:
    """Run a git command with a deadline and return its raw stdout bytes."""
    try:
        proc = subprocess.run(
            ["git", *args],
            cwd=root,
            capture_output=True,
            timeout=timeout,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as err:
        raise CheckError(f"git {' '.join(args)} failed: {err}") from err
    if proc.returncode != 0:
        stderr = proc.stderr.decode("utf-8", errors="replace").strip()
        raise CheckError(f"git {' '.join(args)} exited {proc.returncode}: {stderr}")
    return proc.stdout


def head_commit(root: Path) -> str:
    """Return the 40-character hexadecimal SHA of HEAD."""
    head = (
        run_git_bytes(["rev-parse", "HEAD"], root)
        .decode("utf-8", errors="replace")
        .strip()
    )
    if len(head) != 40 or not all(c in "0123456789abcdefABCDEF" for c in head):
        raise CheckError(f"git rev-parse HEAD returned invalid commit: {head!r}")
    return head


def collect_changed(root: Path, paths: list[str]) -> list[tuple[str, bytes]]:
    """Compare each listed path's working-tree bytes with git show HEAD:<path>."""
    changed: list[tuple[str, bytes]] = []
    for rel in paths:
        work_file = root / rel
        if not work_file.is_file() or work_file.is_symlink():
            raise CheckError(f"listed path is missing: {rel}")
        try:
            work_bytes = work_file.read_bytes()
        except OSError as err:
            raise CheckError(f"cannot read {rel}: {err}") from err
        head_bytes = run_git_bytes(["show", f"HEAD:{rel}"], root)
        if work_bytes != head_bytes:
            changed.append((rel, work_bytes))
    changed.sort(key=lambda item: item[0])
    return changed


def write_artifact(out: Path, head: str, changed: list[tuple[str, bytes]]) -> None:
    """Write <out>/files/<path> and <out>/manifest.json for changed files."""
    files_meta: list[dict[str, Any]] = []
    for rel, data in changed:
        dst = out / "files" / rel
        dst.parent.mkdir(parents=True, exist_ok=True)
        dst.write_bytes(data)
        files_meta.append(
            {
                "bytes": len(data),
                "path": rel,
                "sha256": hashlib.sha256(data).hexdigest(),
            }
        )
    manifest = {
        "files": files_meta,
        "head": head,
        "schema": SCHEMA,
    }
    out.mkdir(parents=True, exist_ok=True)
    manifest_text = json.dumps(manifest, indent=2, sort_keys=True) + "\n"
    (out / "manifest.json").write_text(manifest_text, encoding="utf-8")


def parse_args() -> argparse.Namespace:
    default_root = Path(__file__).resolve().parent.parent.parent
    parser = argparse.ArgumentParser(description=(__doc__ or "").splitlines()[0])
    parser.add_argument(
        "--out",
        type=Path,
        required=True,
        help="Output directory for manifest.json and files/",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=default_root,
        help="Repository root directory",
    )
    parser.add_argument(
        "--list",
        dest="list_file",
        type=Path,
        default=None,
        help="Path to generated-files allowlist (default: <root>/.github/generated-files)",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    root = args.root.resolve()
    out = args.out.resolve()
    list_file = (
        args.list_file.resolve()
        if args.list_file is not None
        else root / DEFAULT_LIST
    )
    try:
        paths = read_allowlist(list_file)
        if not paths:
            print("nothing to regenerate")
            return 0
        head = head_commit(root)
        changed = collect_changed(root, paths)
        if not changed:
            print("nothing to regenerate")
            return 0
        write_artifact(out, head, changed)
        names = ", ".join(rel for rel, _ in changed)
        print(f"regenerated {len(changed)} file(s) at {head}: {names}")
        return 0
    except CheckError as err:
        print(f"generated_files: {err}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
