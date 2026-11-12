# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse one container image tag pinned to more than one digest across tracked files.

Rule: every literal `name:tag@sha256:<digest>` pin in the repository's tracked working-tree
files (default) or git index snapshot (`--index`) that resolves to the same `(canonical name,
tag)` key must carry the same digest.

Grammar (allow-list):
- not preceded by `[A-Za-z0-9._/-]`, or directly after a shell default operator `${NAME:-` or
  `${NAME-` (`NAME` = `[A-Za-z_][A-Za-z0-9_]*`), as in `image: ${IMG:-postgres:18@sha256:...}`;
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

Record files: a file holding a comment line `image-pin-check: record` (after `#`, `//` or
`<!--`) records measured pins that no updater moves, such as an ADR verification table or a
verified test matrix. Its pins are compared only with pins in other record files, so a
Renovate digest update of the live copies is never refused for leaving a record behind.

Reading:
- working tree: each tracked path is read as a regular file on disk. A path below a link,
  junction or other reparse point counts as missing, as git sees it (deleted), so a file
  outside the tracked tree never enters the scan. Bytes are read as they are on disk, so a
  `working-tree-encoding` file is not decoded; CI and `make workflows-verify` therefore run
  both modes;
- index: each stage-0 entry of mode 100644 or 100755 is read as its raw staged blob
  (`git ls-files -s`). `git cat-file --batch-check` gives each blob's size first; blobs of at
  most MAX_BYTES stream through one `git cat-file --batch` pipe, and a larger blob is read only
  up to MAX_BYTES + 1 bytes before its git is stopped, so no blob is written to temporary
  storage and the rest of a large blob is never read. Nothing is checked out, so no line-ending
  conversion, `working-tree-encoding` or smudge filter changes or fails the snapshot; links,
  submodules and unmerged stages hold no file content and are left out.

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
import threading
from collections import defaultdict
from collections.abc import Iterable, Iterator
from contextlib import contextmanager
from pathlib import Path
from typing import IO, cast

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
    r"(?:\$\{[A-Za-z_][A-Za-z0-9_]*:?-|(?<![A-Za-z0-9._/-]))"
    rf"(?P<name>(?:{HOST_PREFIX})?{PATH_COMPONENT}(?:/{PATH_COMPONENT})*)"
    r":(?P<tag>[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})"
    r"@sha256:(?P<digest>[0-9a-f]{64})(?![0-9A-Fa-f])"
)
RECORD_MARKER = re.compile(r"\s*(?:#|//|<!--)\s*image-pin-check: record\b")

# Index modes that hold file content: regular and executable files (no link 120000, no submodule 160000).
REGULAR_MODES = frozenset({"100644", "100755"})
# `git cat-file --batch` record header and `--batch-check` line of a blob: SHA-1 or SHA-256
# object id, type, size.
BATCH_HEADER = re.compile(rb"(?P<oid>[0-9a-f]{40}|[0-9a-f]{64}) blob (?P<size>[0-9]{1,20})\n")

# (canonical name, tag, record file) -> {digest: [file:line, ...]}
PinMap = dict[tuple[str, str, bool], dict[str, list[str]]]
# (repository path, first MAX_BYTES + 1 bytes of its content, or None when it holds no file)
Content = tuple[str, bytes | None]


def run_git(root: Path, args: list[str], stdin: IO[bytes] | None = None, stdout: IO[bytes] | None = None) -> bytes:
    """Run git ARGS in ROOT under GIT_TIMEOUT and return its stdout (b"" when STDOUT takes it).

    A failing git raises ValueError with its stderr, so main() reports it and exits 1.
    """
    proc = subprocess.run(  # nosec B603  # fixed git argv, no shell
        ["git", *args],
        cwd=root,
        stdin=stdin,
        stdout=subprocess.PIPE if stdout is None else stdout,
        stderr=subprocess.PIPE,
        check=False,
        timeout=GIT_TIMEOUT,
    )
    if proc.returncode != 0:
        detail = proc.stderr.decode("utf-8", errors="replace").strip()
        raise ValueError(f"git {args[0]} failed in {root}: {detail}")
    return proc.stdout or b""


def git_entries(root: Path, args: list[str]) -> list[str]:
    """Split the NUL-separated output of git ARGS in ROOT into entries, bounded by MAX_FILES."""
    out = run_git(root, args).decode("utf-8", "surrogateescape")
    entries = [item for item in out.split("\0") if item]
    if len(entries) > MAX_FILES:
        raise ValueError(f"{len(entries)} tracked files exceed the bound of {MAX_FILES}")
    return entries


def staged_blobs(root: Path) -> list[tuple[str, str]]:
    """List `(path, blob id)` of each stage-0 entry of mode 100644 or 100755 in ROOT's git index.

    `git ls-files -s` lists entries with the skip-worktree bit (sparse checkout) as well.
    """
    blobs = []
    for entry in git_entries(root, ["ls-files", "-s", "-z"]):
        meta, _, path = entry.partition("\t")
        fields = meta.split(" ")
        if len(fields) != 3 or not path:
            raise ValueError(f"git ls-files -s: unexpected entry {entry!r}")
        mode, oid, stage = fields
        if stage == "0" and mode in REGULAR_MODES:
            blobs.append((path, oid))
    return blobs


@contextmanager
def request_file(oids: list[str]) -> Iterator[IO[bytes]]:
    """Yield a rewound temporary file listing OIDS one per line, as `git cat-file` batch input."""
    with tempfile.TemporaryFile() as request:
        request.write("".join(f"{oid}\n" for oid in oids).encode("ascii"))
        request.seek(0)
        yield request


def record_size(header: bytes, oid: str, mode: str) -> int:
    """Return the size in HEADER, a `git cat-file MODE` record header that must name blob OID."""
    match = BATCH_HEADER.fullmatch(header)
    if match is None or match.group("oid").decode("ascii") != oid:
        raise ValueError(f"git cat-file {mode}: no blob record for {oid}")
    return int(match.group("size"))


def blob_sizes(root: Path, oids: list[str]) -> list[int]:
    """Return the size of each blob in OIDS, in order, from one `git cat-file --batch-check` call."""
    with request_file(oids) as request:
        lines = run_git(root, ["cat-file", "--batch-check"], stdin=request).splitlines(keepends=True)
    if len(lines) != len(oids):
        raise ValueError(f"git cat-file --batch-check: {len(lines)} records for {len(oids)} blobs")
    return [record_size(line, oid, "--batch-check") for line, oid in zip(lines, oids, strict=True)]


@contextmanager
def git_stream(root: Path, args: list[str], stdin: IO[bytes] | None = None) -> Iterator[IO[bytes]]:
    """Run git ARGS in ROOT and yield its stdout pipe; git is killed on exit or after GIT_TIMEOUT.

    The reader takes only what it needs from the pipe, so no output is stored in full; a failing
    git or the deadline shows up to the reader as a record cut short.
    """
    with subprocess.Popen(  # nosec B603  # fixed git argv, no shell
        ["git", *args],
        cwd=root,
        stdin=subprocess.DEVNULL if stdin is None else stdin,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
    ) as proc:
        deadline = threading.Timer(GIT_TIMEOUT, proc.kill)
        deadline.start()
        try:
            yield cast("IO[bytes]", proc.stdout)  # stdout=PIPE always opens it
        finally:
            deadline.cancel()
            proc.kill()


def batch_blob(out: IO[bytes], oid: str) -> bytes:
    """Read the next `git cat-file --batch` record from OUT: blob OID of at most MAX_BYTES bytes."""
    size = record_size(out.readline(160), oid, "--batch")
    if size > MAX_BYTES:
        raise ValueError(f"git cat-file --batch: blob {oid} holds {size} bytes, over {MAX_BYTES}")
    data = out.read(size)
    if len(data) != size or out.read(1) != b"\n":
        raise ValueError(f"git cat-file --batch: record of {oid} is cut short (git failed or passed {GIT_TIMEOUT} s)")
    return data


def blob_prefix(root: Path, oid: str) -> bytes:
    """Read the first MAX_BYTES + 1 bytes of blob OID, which holds more, then stop its git.

    The rest of the blob is never read, so its size costs neither time nor storage.
    """
    with git_stream(root, ["cat-file", "blob", oid]) as out:
        data = out.read(MAX_BYTES + 1)
    if len(data) != MAX_BYTES + 1:
        raise ValueError(f"git cat-file blob {oid}: cut short at {len(data)} bytes (git failed or passed {GIT_TIMEOUT} s)")
    return data


def index_contents(root: Path) -> Iterator[Content]:
    """Yield each staged regular file of ROOT's git index with the start of its raw blob.

    Blobs of at most MAX_BYTES stream through one `git cat-file --batch` pipe, larger ones follow
    through blob_prefix(); no checkout, filter or conversion takes part. GIT_TIMEOUT bounds the
    whole stream, the scan of its blobs included.
    """
    blobs = staged_blobs(root)
    sizes = blob_sizes(root, [oid for _, oid in blobs])
    small = [blob for blob, size in zip(blobs, sizes, strict=True) if size <= MAX_BYTES]
    large = [blob for blob, size in zip(blobs, sizes, strict=True) if size > MAX_BYTES]
    with request_file([oid for _, oid in small]) as request, git_stream(root, ["cat-file", "--batch"], request) as out:
        for path, oid in small:
            yield path, batch_blob(out, oid)
    for path, oid in large:
        yield path, blob_prefix(root, oid)


def plain_parents(root: Path, rel: str) -> bool:
    """Tell whether each directory between ROOT and REL is a plain directory.

    A link, junction or other reparse point is refused, as git refuses a path beyond a
    symbolic link; ROOT itself is taken as given.
    """
    parent = root
    for part in rel.split("/")[:-1]:
        parent = parent / part
        try:
            info = os.lstat(parent)
        except (FileNotFoundError, NotADirectoryError):
            return False
        reparse = getattr(info, "st_file_attributes", 0) & stat.FILE_ATTRIBUTE_REPARSE_POINT
        if reparse or not stat.S_ISDIR(info.st_mode):
            return False
    return True


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


def worktree_contents(root: Path) -> Iterator[Content]:
    """Yield each tracked path of ROOT with the start of its working-tree file.

    The content is None for a path that is missing, no regular file, or below a link.
    """
    for rel in git_entries(root, ["ls-files", "-z"]):
        yield rel, read_regular(root / rel) if plain_parents(root, rel) else None


def text_lines(raw: bytes | None, rel: str) -> list[str]:
    """Decode RAW, the first MAX_BYTES + 1 bytes of REL, into lines; [] when absent or binary."""
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


def collect_pins(contents: Iterable[Content]) -> PinMap:
    """Map each `(canonical name, tag, record file)` in CONTENTS to its digest locations."""
    found: PinMap = defaultdict(lambda: defaultdict(list))
    for rel, raw in contents:
        lines = text_lines(raw, rel)
        record = any(RECORD_MARKER.match(line) for line in lines)
        for lineno, line in enumerate(lines, 1):
            for match in IMAGE_PIN.finditer(line):
                key = (canonical_name(match.group("name")), match.group("tag"), record)
                found[key][match.group("digest")].append(f"{rel}:{lineno}")
    return found


def load_pins(root: Path, *, index: bool = False) -> PinMap:
    """Collect image pins from ROOT's git index (when INDEX is set) or working tree."""
    return collect_pins(index_contents(root) if index else worktree_contents(root))


def check(root: Path, *, index: bool = False) -> int:
    """Refuse any `(canonical name, tag)` pinned to multiple digests; return the exit status."""
    pins_by_key = load_pins(root, index=index)
    refused = 0
    total_refs = 0
    for (canonical, tag, record), digests in sorted(pins_by_key.items()):
        total_refs += sum(len(locs) for locs in digests.values())
        if len(digests) <= 1:
            continue
        refused += 1
        scope = " in record files" if record else ""
        print(f"image_pin_check: {canonical}:{tag} is pinned to {len(digests)} digests{scope}")
        for digest, locs in sorted(digests.items()):
            where = ", ".join(dict.fromkeys(locs))
            print(f"  sha256:{digest[:12]} {where}")
    if refused:
        print("move every pin of one image tag in one change; Renovate groups them (renovate.json)")
        print("a file of measured pins that no updater moves takes an `image-pin-check: record` comment line")
        return 3
    tags = len({key[:2] for key in pins_by_key})
    print(f"image_pin_check: ok ({tags} image tags across {total_refs} pins)")
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
