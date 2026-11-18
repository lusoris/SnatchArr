# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
r"""Keep the workspace Rust MSRV equal to the leading components of the pinned toolchain.

Both values must be fixed numeric releases. Workspace members must inherit the one declared
minimum rather than carrying literal versions that Renovate cannot move with the toolchain.

Exit 0 = ok, 1 = input error, 3 = refused.
"""

from __future__ import annotations

import argparse
import re
import sys
import tomllib
from pathlib import Path

MAX_BYTES = 1 << 20
MAX_MEMBER_MANIFESTS = 64
FIXED_RELEASE = re.compile(r"\d+\.\d+(?:\.\d+)?")
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
    key_pattern = re.compile(rf"\s*{re.escape(key)}\s*=")
    for number, line in enumerate(lines, 1):
        header = TABLE_HEADER.fullmatch(line)
        if header:
            current_table = header.group(1)
        elif current_table == wanted_table and key_pattern.match(line):
            return number
    return 0


def fixed_release(value: object) -> str | None:
    """Return VALUE when it is a fixed numeric Rust release, otherwise none."""
    if isinstance(value, str) and FIXED_RELEASE.fullmatch(value):
        return value
    return None


def member_manifests(root: Path, members: object) -> list[Path]:
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
                found.add(relative)
                if len(found) > MAX_MEMBER_MANIFESTS:
                    raise InputError(
                        f"workspace manifests exceed {MAX_MEMBER_MANIFESTS}"
                    )
    return sorted(found)


def check_members(root: Path, cargo: dict[str, object]) -> int:
    """Refuse literal package rust-version values in workspace member manifests."""
    workspace = table(cargo, "workspace")
    for relative in member_manifests(root, workspace.get("members", [])):
        member, lines = read_toml(root, relative)
        value = table(member, "package").get("rust-version")
        if isinstance(value, str):
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
    msrv = fixed_release(raw_msrv)
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
    msrv_components = msrv.split(".")
    channel_components = channel.split(".")
    if channel_components[: len(msrv_components)] != msrv_components:
        print(
            f"refused: Cargo.toml:{msrv_line} rust-version {msrv} does not match "
            f"rust-toolchain.toml:{channel_line} channel {channel}; move rust-version with "
            "the toolchain, or add a CI job building with the older release and extend this check"
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
