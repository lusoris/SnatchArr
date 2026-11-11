# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse one container image tag pinned to more than one digest across tracked files.

Rule: every literal `name:tag@sha256:<digest>` pin in the repository's tracked working-tree
files (default) or git index snapshot (`--index`) that resolves to the same `(canonical name,
tag)` key must carry the same digest.

Grammar (allow-list):
- not preceded by `[A-Za-z0-9._/-]`;
- `name`: optional first component in host form (`[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[0-9]+)?/`),
  followed by one or more `/`-separated path components
  `[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*` (OCI `distribution-spec` `spec.md` line 151 at commit
  `97274622c111`);
- `:` + `tag` matching `[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}`;
- `@sha256:` + 64 lowercase hex digits, not followed by a hex digit (`[0-9A-Fa-f]`).
A digest written as a shell or template variable (`${D}`, `$D`) is no literal pin and is ignored.

Canonical form:
- strip at most one leading Docker Hub host in `{docker.io, index.docker.io,
  registry-1.docker.io, mirror.gcr.io}` (`mirror.gcr.io` mirrors Docker Hub under the same
  manifest digests);
- prepend `library/` when the remaining name holds no `/`;
- keep every other registry host intact (`ghcr.io/o/postgres` stays `ghcr.io/o/postgres`).

Exit 0 = ok, 1 = input error, 3 = refused.
"""

from __future__ import annotations

import argparse
import os
import re
import stat
import subprocess  # nosec B404  # runs only git with a fixed argument list, never a shell
import sys
import tempfile
from collections import defaultdict
from pathlib import Path

MAX_FILES = 20000
MAX_BYTES = 4 << 20
GIT_TIMEOUT = 60

DOCKER_HUB_HOSTS = frozenset({
    "docker.io",
    "index.docker.io",
    "registry-1.docker.io",
    "mirror.gcr.io",
})

HOST_PREFIX = r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[0-9]+)?/"
PATH_COMPONENT = r"[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*"
IMAGE_PIN = re.compile(
    r"(?<![A-Za-z0-9._/-])"
    rf"(?P<name>(?:{HOST_PREFIX})?{PATH_COMPONENT}(?:/{PATH_COMPONENT})*)"
    r":(?P<tag>[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})"
    r"@sha256:(?P<digest>[0-9a-f]{64})(?![0-9A-Fa-f])"
)


def list_tracked_paths(root: Path) -> list[str]:
    """List tracked repository paths under ROOT via git ls-files, bounded by MAX_FILES."""
    proc = subprocess.run(  # nosec B603  # fixed git argv, no shell
        ["git", "ls-files", "-z"],
        cwd=root,
        capture_output=True,
        check=False,
        timeout=GIT_TIMEOUT,
    )
    if proc.returncode != 0:
        detail = proc.stderr.decode("utf-8", errors="replace").strip()
        raise ValueError(f"git ls-files failed in {root}: {detail}")
    entries = [item for item in proc.stdout.decode("utf-8", "surrogateescape").split("\0") if item]
    if len(entries) > MAX_FILES:
        raise ValueError(f"{len(entries)} tracked files exceed the bound of {MAX_FILES}")
    return entries


def checkout_index(root: Path, dest: Path) -> None:
    """Materialize the git index of ROOT into DEST via git checkout-index."""
    proc = subprocess.run(  # nosec B603  # fixed git argv, no shell
        ["git", "checkout-index", "-a", f"--prefix={dest}/"],
        cwd=root,
        capture_output=True,
        check=False,
        timeout=GIT_TIMEOUT,
    )
    if proc.returncode != 0:
        detail = proc.stderr.decode("utf-8", errors="replace").strip()
        raise ValueError(f"git checkout-index failed in {root}: {detail}")


def read_regular(path: Path) -> bytes | None:
    """Read at most MAX_BYTES + 1 bytes of PATH; None when it is missing or no regular file.

    A link, directory, fifo or device is never read: `lstat` refuses it before the open, and
    `O_NOFOLLOW` plus `fstat` on the open descriptor refuse a swap between the two calls
    (`O_NOFOLLOW` and `O_NONBLOCK` are absent on Windows, where the `lstat` check remains).
    """
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0)
    try:
        if not stat.S_ISREG(os.lstat(path).st_mode):
            return None
        fd = os.open(path, flags)
    except FileNotFoundError:
        return None
    with os.fdopen(fd, "rb") as handle:
        if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
            return None
        return handle.read(MAX_BYTES + 1)


def file_lines(path: Path, rel: str) -> list[str]:
    """Read UTF-8 text lines from regular file PATH; skip missing, non-regular or binary files."""
    raw = read_regular(path)
    if raw is None or b"\0" in raw:
        return []
    if len(raw) > MAX_BYTES:
        raise ValueError(
            f"{rel}: more than {MAX_BYTES} bytes of text; raise MAX_BYTES in tools/ci/image_pin_check.py"
        )
    return raw.decode("utf-8", "replace").splitlines()


def canonical_name(name: str) -> str:
    """Normalize Docker Hub host aliases (mirror.gcr.io included) and bare library names."""
    head, sep, rest = name.partition("/")
    if sep and head in DOCKER_HUB_HOSTS:
        name = rest
    return name if "/" in name else f"library/{name}"


def collect_pins(root: Path, paths: list[str]) -> dict[tuple[str, str], dict[str, list[str]]]:
    """Map each `(canonical name, tag)` in PATHS under ROOT to `{digest: [file:line, ...]}`."""
    found: dict[tuple[str, str], dict[str, list[str]]] = defaultdict(lambda: defaultdict(list))
    for rel in paths:
        for lineno, line in enumerate(file_lines(root / rel, rel), 1):
            for match in IMAGE_PIN.finditer(line):
                key = (canonical_name(match.group("name")), match.group("tag"))
                found[key][match.group("digest")].append(f"{rel}:{lineno}")
    return found


def load_pins(root: Path, *, index: bool = False) -> dict[tuple[str, str], dict[str, list[str]]]:
    """Collect image pins from ROOT's git index (when INDEX is set) or working tree."""
    paths = list_tracked_paths(root)
    if not index:
        return collect_pins(root, paths)
    with tempfile.TemporaryDirectory() as tmp:
        dest = Path(tmp).resolve()
        checkout_index(root, dest)
        return collect_pins(dest, paths)


def check(root: Path, *, index: bool = False) -> int:
    """Refuse any `(canonical name, tag)` pinned to multiple digests; return the exit status."""
    pins_by_key = load_pins(root, index=index)
    refused = 0
    total_refs = 0
    for (canonical, tag), digests in sorted(pins_by_key.items()):
        total_refs += sum(len(locs) for locs in digests.values())
        if len(digests) <= 1:
            continue
        refused += 1
        print(f"image_pin_check: {canonical}:{tag} is pinned to {len(digests)} digests")
        for digest, locs in sorted(digests.items()):
            where = ", ".join(dict.fromkeys(locs))
            print(f"  sha256:{digest[:12]} {where}")
    if refused:
        print("move every pin of one image tag in one change; Renovate groups them (renovate.json)")
        return 3
    print(f"image_pin_check: ok ({len(pins_by_key)} image tags across {total_refs} pins)")
    return 0


def main() -> int:
    """Parse CLI arguments and run the container image digest check."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--root", type=Path, default=Path("."), help="repository root (default .)")
    parser.add_argument(
        "--index",
        action="store_true",
        help="scan git index content instead of working-tree files",
    )
    args = parser.parse_args()
    try:
        return check(args.root, index=args.index)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"image_pin_check: error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
