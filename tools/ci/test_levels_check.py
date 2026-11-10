# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse a release test level binding the engine's audit does not pass, or one with no files of its own.

praetorctl audit checks every binding under release.test_levels in .standards.yaml: its paths
exist (and hold its pattern), its job runs in a workflow, and that job runs its command. Outside
a release context the audit reports a broken binding as [WARN] and exits 0, and in a release
context this repository's audit stops at the end-to-end evidence gate before it reaches the
test levels. This check runs "<engine> audit --offline" and refuses every "Release test levels"
line other than [PASS], and a [PASS] from an audit that exits nonzero, so a binding that no
longer matches its paths, job or command fails make governance-verify and the CI governance job
on the pull request that breaks it. Offline, the audit reads no workflow run health from the
forge; the online audit still reports that.

The engine checks each level on its own, so a level whose paths also take in a narrower level's
files (unit's api/**/*_test.go holds integration's *_integration_test.go) passes on those files
alone. Before the audit, this check lists each level's tracked files through git :(glob)
pathspecs and refuses a level whose every file another, narrower level also matches. It reads
only globs whose syntax git and the engine's path.Match segments read alike (literal characters,
"*" inside a segment, "**" as a whole segment) and refuses any other. A directory glob (ending
"/", "/*" or "/**") is read as its whole tracked tree, including for "/*", and refused if it
shares any file with another level. The engine filters directory globs by test file convention,
so their own files cannot be compared here. A directory-glob level with no overlap is left out
of that comparison, and the check says so; overlapping levels must use file globs.

The engine is always the pinned engine through .config/lefthook/engine.sh, the one resolver
git hooks use: it installs the PRAETOR_REF pin once into the engine cache and never falls back
to PATH. Exit 0 = pass, 1 = input error or the engine could not run, 3 = refused.
"""

from __future__ import annotations

import argparse
import re
import subprocess  # nosec B404 -- runs the engine and git with fixed argument lists, never a shell
import sys
from pathlib import Path

import yaml

ENGINE_TIMEOUT = 900
GIT_TIMEOUT = 60
MAX_LINES = 20000
MAX_LEVELS = 32
MAX_GLOBS = 64
LEVELS_LINE = re.compile(r"^\[([A-Z]+)\] Release test levels: (.*)$")
GLOB_SEGMENT = re.compile(r"^(\*\*|[0-9A-Za-z._*-]+)$")
MANIFEST = ".standards.yaml"


class InputError(Exception):
    """The manifest or a glob cannot be read as this check requires."""


def read_levels(root: Path) -> dict[str, list[str]]:
    """Return each release test level's paths from ROOT's manifest.

    A duplicate key keeps its last value here; the engine refuses the manifest in that case, so
    the audit verdict below refuses it too.
    """
    try:
        manifest = yaml.safe_load((root / MANIFEST).read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, yaml.YAMLError) as error:
        raise InputError(f"cannot read {MANIFEST}: {error}") from error
    levels = ((manifest or {}).get("release") or {}).get("test_levels") or {}
    if not isinstance(levels, dict) or len(levels) > MAX_LEVELS:
        raise InputError(f"release.test_levels in {MANIFEST} is not a map of at most {MAX_LEVELS} levels")
    result: dict[str, list[str]] = {}
    for name, spec in levels.items():
        paths = (spec or {}).get("paths") if isinstance(spec, dict) else None
        if not isinstance(paths, list) or not all(isinstance(p, str) for p in paths) or len(paths) > MAX_GLOBS:
            raise InputError(f"release.test_levels.{name}.paths is not a list of at most {MAX_GLOBS} strings")
        result[str(name)] = paths
    return result


def is_directory_glob(glob: str) -> bool:
    """Report whether the engine reads GLOB as a directory tree (then filtered by test file convention)."""
    return glob.endswith(("/**", "/*", "/"))


def check_glob(level: str, glob: str) -> None:
    """Raise InputError unless every segment of GLOB is "**" or literal characters with "*"."""
    if not glob or not all(GLOB_SEGMENT.match(segment) for segment in glob.split("/")):
        raise InputError(
            f"release.test_levels.{level}.paths entry {glob!r}: only literal characters, '*' in a segment"
            " and '**' as a whole segment are read here"
        )


def tracked_matches(root: Path, globs: list[str]) -> set[str]:
    """Return the tracked files under ROOT that any of GLOBS matches, through git :(glob) pathspecs."""
    result = subprocess.run(  # nosec B603 -- fixed git argument list, globs passed as pathspecs, no shell
        ["git", "-C", str(root), "ls-files", "-z", "--", *(f":(glob){g}" for g in globs)],
        capture_output=True,
        check=False,
        timeout=GIT_TIMEOUT,
    )
    if result.returncode != 0:
        raise InputError(f"git ls-files in {root} failed: {result.stderr.decode('utf-8', 'replace').strip()}")
    return {name for name in result.stdout.decode("utf-8", "replace").split("\0") if name}


def comparison_glob(glob: str) -> str:
    """Expand a directory glob to its whole tree for conservative overlap checks."""
    return f"{glob.rsplit('/', 1)[0]}/**" if is_directory_glob(glob) else glob


def directory_overlap(
    name: str, glob: str, mine: set[str], files: dict[str, set[str]]
) -> int:
    """Refuse a directory glob sharing tracked files with any other level."""
    status = 0
    for other, theirs in files.items():
        if other == name:
            continue
        shared = len(mine & theirs)
        if shared:
            print(
                f"refused: level {name} has directory glob {glob} and shares {shared} files with {other}: "
                "the engine filters it by test file convention, so its own files cannot be compared here; write a file glob"
            )
            status = 3
    return status


def judge_directory_globs(
    root: Path, levels: dict[str, list[str]], files: dict[str, set[str]]
) -> int:
    """Judge each directory tree's overlaps; note directory levels with no shared files."""
    status = 0
    for name, globs in levels.items():
        directories = [g for g in globs if is_directory_glob(g)]
        if not directories:
            continue
        overlap = 0
        for glob in directories:
            mine = tracked_matches(root, [comparison_glob(glob)])
            overlap = max(overlap, directory_overlap(name, glob, mine, files))
        if not overlap:
            print(
                f"note: level {name} not compared for own files: directory glob, no other level's files in it"
            )
        status = max(status, overlap)
    return status


def level_files(
    root: Path, levels: dict[str, list[str]]
) -> tuple[dict[str, set[str]], int]:
    """Return file-glob levels' tracked files and the directory-glob overlap verdict."""
    files: dict[str, set[str]] = {}
    for name, globs in levels.items():
        comparisons = [comparison_glob(g) for g in globs]
        for glob in comparisons:
            check_glob(name, glob)
        files[name] = tracked_matches(root, comparisons)
    status = judge_directory_globs(root, levels, files)
    compared = {
        name: files[name]
        for name, globs in levels.items()
        if not any(is_directory_glob(g) for g in globs)
    }
    return compared, status


def judge_own_files(files: dict[str, set[str]]) -> int:
    """Refuse each level whose files all sit in narrower levels; return the exit status."""
    status = 0
    for name, mine in files.items():
        inner = sorted(o for o, theirs in files.items() if o != name and theirs and theirs <= mine)
        if not mine or not inner:
            continue
        own = mine.difference(*(files[o] for o in inner))
        if own:
            print(f"ok: level {name} keeps {len(own)} of its {len(mine)} files apart from {', '.join(inner)}")
            continue
        print(f"refused: level {name} has no file of its own: each of its {len(mine)} files is also matched by {', '.join(inner)}")
        status = 3
    return status


def run_audit(root: Path) -> tuple[int, list[str]]:
    """Run the engine's offline audit in ROOT; return its exit status and output lines."""
    result = subprocess.run(  # nosec B603 -- fixed argument list, pinned resolver, no shell string
        ["sh", ".config/lefthook/engine.sh", "audit", "--offline"],
        cwd=root,
        capture_output=True,
        check=False,
        timeout=ENGINE_TIMEOUT,
    )
    text = (result.stdout + result.stderr).decode("utf-8", "replace")
    return result.returncode, text.splitlines()[:MAX_LINES]


def judge(status: int, lines: list[str]) -> int:
    """Print the test levels verdict; PASS counts only when the audit exits 0; return the exit status."""
    found = [m for m in (LEVELS_LINE.match(line) for line in lines) if m]
    if len(found) != 1:
        print(f"refused: want one 'Release test levels' line in the audit output, found {len(found)} (audit exit {status})")
        for line in lines[-5:]:
            print(f"  {line}")
        return 3
    verdict, detail = found[0].group(1), found[0].group(2)
    if verdict != "PASS":
        print(f"refused: release test levels {verdict}: {detail}")
        return 3
    if status != 0:
        print(f"refused: release test levels pass, but the audit exited {status}: {detail}")
        for line in lines[-5:]:
            print(f"  {line}")
        return 3
    print(f"ok: release test levels pass: {detail}")
    return 0


def main() -> int:
    """Parse the arguments, compare the levels' own files, run the audit and judge both."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--root", type=Path, default=Path("."), help="repository root (default .)")
    args = parser.parse_args()
    try:
        files, directories = level_files(args.root, read_levels(args.root))
        own = max(directories, judge_own_files(files))
    except (InputError, OSError, subprocess.SubprocessError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    if not (args.root / ".config/lefthook/engine.sh").is_file():
        print(f"error: cannot run the engine: {args.root}/.config/lefthook/engine.sh is missing", file=sys.stderr)
        return 1
    try:
        status, lines = run_audit(args.root)
    except (OSError, subprocess.SubprocessError) as error:
        print(f"error: cannot run sh .config/lefthook/engine.sh audit --offline: {error}", file=sys.stderr)
        return 1
    return max(own, judge(status, lines))


if __name__ == "__main__":
    sys.exit(main())
