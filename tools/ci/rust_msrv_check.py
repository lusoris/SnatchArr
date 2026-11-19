# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
r"""Keep the workspace Rust MSRV equal to the fixed release of the pinned toolchain.

The workspace minimum may omit the patch component, but the toolchain must name an exact
three-component release. Workspace members, including in-root path dependencies that
Cargo makes members, must inherit the declared minimum rather than carrying literal
versions that Renovate cannot move with the toolchain.

Exit 0 = ok, 1 = input error, 3 = refused.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
import tomllib
from pathlib import Path

MAX_BYTES = 1 << 20
MAX_MEMBER_MANIFESTS = 64
DEPENDENCY_TABLES = ("dependencies", "dev-dependencies", "build-dependencies")
MSRV_RELEASE = re.compile(r"\d+\.\d+(?:\.\d+)?")
FIXED_RELEASE = re.compile(r"\d+\.\d+\.\d+")
TABLE_HEADER = re.compile(r"\s*\[([A-Za-z0-9_.-]+)]\s*(?:#.*)?")


class InputError(ValueError):
    """Raised when a required manifest cannot be read or validated as input."""


def read_toml(root: Path, relative: Path) -> tuple[dict[str, object], list[str]]:
    """Read and parse one bounded TOML file below ROOT."""
    path = root / relative
    try:
        data = path.read_bytes()
    except OSError as error:
        raise InputError(f"cannot read {relative.as_posix()}: {error}") from error
    if len(data) > MAX_BYTES:
        raise InputError(f"{relative.as_posix()} exceeds the {MAX_BYTES}-byte limit")
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError as error:
        raise InputError(
            f"cannot decode {relative.as_posix()} as UTF-8: {error}"
        ) from error
    try:
        parsed = tomllib.loads(text)
    except tomllib.TOMLDecodeError as error:
        raise InputError(f"cannot parse {relative.as_posix()}: {error}") from error
    return parsed, text.splitlines()


def table(data: dict[str, object], *keys: str) -> dict[str, object]:
    """Return nested TOML table KEYS, or an empty table when it is absent or not a table."""
    current = data
    for key in keys:
        value = current.get(key)
        if not isinstance(value, dict):
            return {}
        current = value
    return current


def key_line(lines: list[str], wanted_table: str, key: str) -> int:
    """Return the one-based line of KEY in WANTED_TABLE, or zero when absent."""
    current_table = ""
    key_pattern = re.compile(rf"\s*{re.escape(key)}(?:\.workspace)?\s*=")
    for number, line in enumerate(lines, 1):
        header = TABLE_HEADER.fullmatch(line)
        if header:
            current_table = header.group(1)
        elif current_table == wanted_table and key_pattern.match(line):
            return number
    return 0


def table_line(lines: list[str], wanted_table: str) -> int:
    """Return the one-based line of WANTED_TABLE, or zero when absent."""
    for number, line in enumerate(lines, 1):
        header = TABLE_HEADER.fullmatch(line)
        if header and header.group(1) == wanted_table:
            return number
    return 0


def fixed_release(value: object) -> str | None:
    """Return VALUE when it is a fixed numeric Rust release, otherwise none."""
    if isinstance(value, str) and FIXED_RELEASE.fullmatch(value):
        return value
    return None


def minimum_release(value: object) -> str | None:
    """Return VALUE when it is a two- or three-component Rust minimum, otherwise none."""
    if isinstance(value, str) and MSRV_RELEASE.fullmatch(value):
        return value
    return None


def exclude_paths(exclude: object) -> list[Path]:
    """Validate workspace EXCLUDE and return its relative paths."""
    if not isinstance(exclude, list) or any(
        not isinstance(item, str) for item in exclude
    ):
        raise InputError(
            "Cargo.toml [workspace].exclude must be a list of relative paths"
        )
    paths = []
    for item in exclude:
        path = Path(item)
        if path.is_absolute():
            raise InputError(f"Cargo.toml workspace exclude path is absolute: {item}")
        paths.append(path)
    return paths


def excluded(directory: Path, excludes: list[Path], members: list[str]) -> bool:
    """Return whether EXCLUDES holds DIRECTORY and no literal MEMBERS entry does."""
    literal = [Path(item) for item in members if not any(c in item for c in "*?[")]
    held = any(directory.is_relative_to(item) for item in excludes)
    return held and not any(directory.is_relative_to(item) for item in literal)


def path_dependencies(
    relative: Path, manifest: dict[str, object], shared: dict[str, object]
) -> list[Path]:
    """Return RELATIVE's in-root path-dependency manifests (implicit Cargo members)."""
    targets = table(manifest, "target")
    scopes = [manifest, *(table(targets, spec) for spec in targets)]
    tables = [table(scope, kind) for scope in scopes for kind in DEPENDENCY_TABLES]
    found = []
    for name, spec in (pair for deps in tables for pair in deps.items()):
        base = relative.parent
        if isinstance(spec, dict) and spec.get("workspace") is True:
            base, spec = Path(), table(shared, name)
        path = spec.get("path") if isinstance(spec, dict) else None
        if not isinstance(path, str):
            continue
        target = Path(os.path.normpath(base / path))
        if target.is_absolute():
            raise InputError(f"{relative.as_posix()}: absolute path dependency {name}")
        if target.parts[:1] != ("..",):
            found.append(target / "Cargo.toml")
    return found


def member_manifests(root: Path, members: object, excludes: list[Path]) -> list[Path]:
    """Expand workspace MEMBERS below ROOT, refusing more than 64 manifest matches."""
    if not isinstance(members, list) or any(
        not isinstance(item, str) for item in members
    ):
        raise InputError("Cargo.toml [workspace].members must be a list of paths")
    found: set[Path] = set()
    matches = 0
    for pattern in members:
        if Path(pattern).is_absolute():
            raise InputError(
                f"Cargo.toml workspace member pattern is absolute: {pattern}"
            )
        for candidate in root.glob(pattern):
            matches += 1
            if matches > MAX_MEMBER_MANIFESTS:
                raise InputError(
                    f"workspace member matches exceed {MAX_MEMBER_MANIFESTS}"
                )
            manifest = (
                candidate
                if candidate.name == "Cargo.toml"
                else candidate / "Cargo.toml"
            )
            if manifest.is_file():
                try:
                    relative = manifest.relative_to(root)
                except ValueError as error:
                    raise InputError(
                        f"workspace member escapes repository root: {manifest}"
                    ) from error
                if excluded(relative.parent, excludes, members):
                    continue
                found.add(relative)
                if len(found) > MAX_MEMBER_MANIFESTS:
                    raise InputError(
                        f"workspace manifests exceed {MAX_MEMBER_MANIFESTS}"
                    )
    return sorted(found)


def check_members(root: Path, cargo: dict[str, object]) -> int:
    """Require workspace member manifests to inherit package rust-version exactly."""
    workspace = table(cargo, "workspace")
    members = workspace.get("members", [])
    excludes = exclude_paths(workspace.get("exclude", []))
    shared = table(workspace, "dependencies")
    pending = member_manifests(root, members, excludes)
    seen = set(pending)
    while pending:
        relative = pending.pop(0)
        member, lines = read_toml(root, relative)
        for found in path_dependencies(relative, member, shared):
            if found not in seen and not excluded(found.parent, excludes, members):
                seen.add(found)
                pending.append(found)
        if len(seen) > MAX_MEMBER_MANIFESTS:
            raise InputError(f"workspace manifests exceed {MAX_MEMBER_MANIFESTS}")
        value = table(member, "package").get("rust-version")
        if value is None:
            line = table_line(lines, "package")
            print(
                f"refused: {relative.as_posix()}:{line} has no rust-version; "
                "use rust-version.workspace = true"
            )
            return 3
        if value != {"workspace": True}:
            line = key_line(lines, "package", "rust-version")
            print(
                f"refused: {relative.as_posix()}:{line} has literal rust-version {value}; "
                "use rust-version.workspace = true"
            )
            return 3
    return 0


def check(root: Path) -> int:
    """Check the root manifests and member inheritance; return the documented exit status."""
    cargo, cargo_lines = read_toml(root, Path("Cargo.toml"))
    toolchain, toolchain_lines = read_toml(root, Path("rust-toolchain.toml"))
    raw_msrv = table(cargo, "workspace", "package").get("rust-version")
    if raw_msrv is None:
        print("refused: Cargo.toml is missing [workspace.package].rust-version")
        return 3
    msrv = minimum_release(raw_msrv)
    msrv_line = key_line(cargo_lines, "workspace.package", "rust-version")
    if msrv is None:
        print(
            f"refused: Cargo.toml:{msrv_line} rust-version {raw_msrv} is no fixed release"
        )
        return 3
    raw_channel = table(toolchain, "toolchain").get("channel")
    channel_line = key_line(toolchain_lines, "toolchain", "channel")
    channel = fixed_release(raw_channel)
    if channel is None:
        print(
            f"refused: rust-toolchain.toml:{channel_line} channel {raw_channel} "
            "is no fixed release and proves no minimum"
        )
        return 3
    msrv_components = tuple(int(component) for component in msrv.split("."))
    if len(msrv_components) == 2:
        msrv_components += (0,)
    channel_components = tuple(int(component) for component in channel.split("."))
    if channel_components != msrv_components:
        print(
            f"refused: Cargo.toml:{msrv_line} rust-version {msrv} does not match "
            f"rust-toolchain.toml:{channel_line} channel {channel}; move rust-version with "
            "the exact toolchain release, or add a CI job building with the older release "
            "and extend this check"
        )
        return 3
    member_status = check_members(root, cargo)
    if member_status:
        return member_status
    print(
        f"ok: Cargo.toml rust-version {msrv} matches rust-toolchain.toml channel {channel}"
    )
    return 0


def main() -> int:
    """Parse arguments and run the Rust MSRV check."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--root", type=Path, default=Path("."), help="repository root (default .)"
    )
    args = parser.parse_args()
    try:
        return check(args.root)
    except (InputError, OSError, ValueError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
