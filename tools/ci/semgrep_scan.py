"""Run the repository's Semgrep rules in a digest-pinned image, fixtures first.

Each rule in .semgrep.yml needs a fixture in tools/ci/semgrep/testdata/ with at
least one `// ruleid:` line it must report and one `// ok:` line it must not.
Every fixture starts with the `//go:build semgrep_fixture` constraint, so no Go
build, vet or gosec run ever loads the code the rules are meant to report.
Semgrep's test mode replays every fixture; only when all pass is the scan of
the Go files trusted: every tracked file still present plus every untracked file
Git does not ignore, so a new file in a working tree is scanned too. A clean
result covers the rules and file bytes in the snapshot taken at scan start.
After the scan, the snapshot contents and target set are re-checked against
the live tree. Any finding, Semgrep error, or scanned-file mismatch fails.

`--semgrep PATH` runs a Semgrep binary directly instead of the image, for hosts
without docker such as a lane sandbox. It prints the binary's version, keeps
Semgrep's state files in a temporary directory, and applies the same fixture
replay and scanned-file check; CI keeps the pinned image. A hand-run
`semgrep scan` on a directory skips `*_test.go` files without saying so (PR 1244
round r16), so lane briefs name this mode (.config/lane-brief.yaml).

`--docker` is the fixed name `docker` on PATH or an absolute path to an
executable file named docker, `--root` must be an existing directory, and both
are checked before any process starts.

Copied from tekmora/discord-bot tools/ci/semgrep_scan.py (EUPL-1.2, same author)
at commit 70642249adc9d99375b707cebe0eb756970905cc. Changed here: this paragraph,
the `--semgrep` mode and the untracked-file targets.

Usage: python3 -I tools/ci/semgrep_scan.py --image NAME:TAG@sha256:DIGEST
       python3 -I tools/ci/semgrep_scan.py --semgrep semgrep
"""

import argparse
import hashlib
import json
import os
import pathlib
import re
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import threading
import time
from functools import partial

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import run_bounded

RULES = ".semgrep.yml"
FIXTURES = "tools/ci/semgrep/testdata"
FIXTURE_CONSTRAINT = "//go:build semgrep_fixture\n"
MAX_FIXTURES = 64
MAX_TARGETS = 4096
TIMEOUT_SECONDS = 600
MAX_CONFIG_BYTES = 64 * 1024
MAX_TARGET_BYTES = 8 * 1024 * 1024
MAX_LISTED_RECORDS = 4 * (MAX_TARGETS + MAX_FIXTURES)
MAX_LISTED_BYTES = MAX_LISTED_RECORDS * 1024
MAX_GIT_STDERR_BYTES = 2 * 1024 * 1024
GIT_STDERR_EXCERPT_BYTES = 4096
READ_CHUNK_BYTES = 8 * 1024
MAX_MESSAGE_CHARS = 300
TRUNCATED_MARKER = "... [truncated]"
ALLOWED_DOCKER = ("docker",)
IMAGE_RE = re.compile(r"^[a-z0-9./-]+:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$")
RULE_ID_RE = re.compile(r"^  - id: ([a-z0-9-]+)$", re.MULTILINE)
ANNOTATION_RE = re.compile(r"//\s*(ruleid|ok):\s*([a-z0-9-]+)\s*$", re.MULTILINE)
SEMGREP_FLAGS = [
    "--metrics=off",
    "--disable-version-check",
    "--oss-only",
    "--jobs",
    "1",
]


class ScanError(Exception):
    """A scan that cannot be trusted or that found a violation."""


def resolve_docker(value) -> str:
    """Resolve --docker on PATH when allowed by name, else require an executable '/.../docker' file."""
    if value in ALLOWED_DOCKER:
        found = shutil.which(value)
        if found is None:
            raise ScanError(f"--docker {value!r} not found on PATH")
        return os.path.abspath(found)
    if (
        os.path.isabs(value)
        and os.path.basename(value) in ALLOWED_DOCKER
        and pathlib.Path(value).is_file()
        and os.access(value, os.X_OK)
    ):
        return value
    raise ScanError(
        f"--docker {value!r} must be 'docker' on PATH or an executable '/.../docker' file"
    )


def checked_root(value) -> pathlib.Path:
    """Resolve --root and require an existing directory."""
    try:
        resolved = pathlib.Path(value).resolve()
        is_dir = resolved.is_dir()
    except OSError as err:
        raise ScanError(f"--root '{value}' is not a directory: {err}") from err
    if not is_dir:
        raise ScanError(f"--root '{value}' is not a directory")
    return resolved


def run_docker(docker, args, root):
    """Run docker at its resolved path in root with a bound, raising ScanError on failure."""
    try:
        return subprocess.run(
            ["docker", *args],
            executable=docker,
            cwd=root,
            capture_output=True,
            text=True,
            timeout=TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as err:
        raise ScanError(f"{docker} did not complete: {err}") from err


def run_semgrep(binary, args, root):
    """Run semgrep at its resolved path in root with a bound, raising ScanError on failure."""
    try:
        return subprocess.run(
            ["semgrep", *args],
            executable=binary,
            cwd=root,
            capture_output=True,
            text=True,
            timeout=TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as err:
        raise ScanError(f"{binary} did not complete: {err}") from err


def image_runner(docker, image, root, snapshot):
    """Return a call that runs Semgrep in the pinned image, mounting snapshot read-only from root."""

    def call(args):
        """Run Semgrep with args in a fresh container without network."""
        docker_args = [
            "run",
            "--rm",
            "--network",
            "none",
            "--volume",
            f"{snapshot}:/src:ro",
            "--workdir",
            "/src",
            "--entrypoint",
            "semgrep",
            image,
            *args,
        ]
        return run_docker(docker, docker_args, root)

    return call


def native_runner(binary, root, state):
    """Return a call that runs a local Semgrep binary, its state files kept under state.

    The variable names are the ones semgrep/env.py reads (checked on 1.183.0)."""
    state_env = {
        "XDG_CONFIG_HOME": state,
        "SEMGREP_SETTINGS_FILE": os.path.join(state, "settings.yml"),
        "SEMGREP_LOG_FILE": os.path.join(state, "semgrep.log"),
        "SEMGREP_VERSION_CACHE_PATH": os.path.join(state, "version"),
    }

    def call(args):
        """Run the local binary with args and the state environment."""
        previous = {key: os.environ.get(key) for key in state_env}
        try:
            os.environ.update(state_env)
            return run_semgrep(binary, args, root)
        finally:
            for key, value in previous.items():
                if value is None:
                    os.environ.pop(key, None)
                else:
                    os.environ[key] = value

    return call


def native_version(semgrep):
    """Return the version line of the local binary, refusing a failed or empty answer."""
    result = semgrep(["--version"])
    lines = [line.strip() for line in result.stdout.splitlines() if line.strip()]
    if result.returncode != 0 or not lines:
        raise ScanError(
            f"semgrep --version exited {result.returncode} without a version"
        )
    return lines[-1]


def rule_ids(root):
    """Read RULES under root with a byte bound and return its unique rule ids."""
    try:
        text = read_bounded_text(root / RULES, MAX_CONFIG_BYTES)
    except BoundedReadError as err:
        raise ScanError(f"cannot read {RULES}: {err}") from err
    ids = RULE_ID_RE.findall(text)
    if not ids:
        raise ScanError(f"{RULES} declares no rule")
    if len(ids) != len(set(ids)):
        raise ScanError(f"{RULES} declares a rule id twice")
    return set(ids)


def fixture_paths(root):
    """Return sorted root-relative POSIX fixture paths; require 1..MAX_FIXTURES."""
    files = sorted((root / FIXTURES).glob("*.go"))
    if not files or len(files) > MAX_FIXTURES:
        raise ScanError(
            f"{FIXTURES}: {len(files)} fixtures, expected 1 to {MAX_FIXTURES}"
        )
    return [path.relative_to(root).as_posix() for path in files]


def read_fixture(root, name, ids, seen):
    """Read one fixture with a byte bound and record its ruleid and ok annotations."""
    path = root / name
    try:
        text = read_bounded_text(path, MAX_CONFIG_BYTES)
    except BoundedReadError as err:
        raise ScanError(f"cannot read fixture {name}: {err}") from err
    if not text.startswith(FIXTURE_CONSTRAINT):
        raise ScanError(
            f"{path.name}: does not start with {FIXTURE_CONSTRAINT.strip()!r}"
        )
    for kind, rule in ANNOTATION_RE.findall(text):
        if rule not in ids:
            raise ScanError(f"{path.name}: annotation names unknown rule '{rule}'")
        seen[kind].add(rule)


def check_fixtures(root, names, ids):
    """Verify every fixture's leading build constraint and two-way rule coverage."""
    seen = {"ruleid": set(), "ok": set()}
    for name in names:
        read_fixture(root, name, ids, seen)
    for kind, covered in sorted(seen.items()):
        missing = sorted(ids - covered)
        if missing:
            raise ScanError(
                f"no `{kind}:` fixture line for rule(s): {', '.join(missing)}"
            )


def self_test(semgrep, names, ids):
    """Replay every rule fixture; a fixture that does not pass is a ScanError."""
    for name in names:
        result = semgrep(["scan", "--test", "--config", RULES, name, *SEMGREP_FLAGS])
        if result.returncode != 0 or "All tests passed" not in result.stdout:
            sys.stderr.write(result.stdout[-4000:] + result.stderr[-4000:])
            raise ScanError(f"fixture {name} did not pass (exit {result.returncode})")
    print(
        f"Semgrep self-test: {len(names)} fixtures passed, {len(ids)} rules covered both ways"
    )


class BoundedReadError(OSError):
    """Raised when a bounded regular-file read is refused."""


def _read_regular_fd(fd, path, limit, before):
    """Verify fd is a regular file within limit and read at most limit + 1 bytes."""
    info = os.fstat(fd)
    if not stat.S_ISREG(info.st_mode):
        raise BoundedReadError(f"{path}: not a regular file")
    if before is not None and not os.path.samestat(before, info):
        raise BoundedReadError(f"{path}: file changed before open")
    if info.st_size > limit:
        raise BoundedReadError(f"{path}: size {info.st_size} exceeds {limit} bytes")
    chunks = []
    remaining = limit + 1
    for _ in range(limit + 2):
        if remaining <= 0:
            break
        chunk = os.read(fd, min(remaining, READ_CHUNK_BYTES))
        if not chunk:
            break
        chunks.append(chunk)
        remaining -= len(chunk)
    return b"".join(chunks)


def read_bounded_bytes(path, limit):
    """Refuse symlinks and read at most limit bytes from a verified regular file."""
    if limit < 0:
        raise BoundedReadError(f"{path}: negative byte limit {limit}")
    flags = os.O_RDONLY | getattr(os, "O_BINARY", 0)
    before = None
    try:
        if hasattr(os, "O_NONBLOCK") and hasattr(os, "O_NOFOLLOW"):
            flags |= os.O_NONBLOCK | os.O_NOFOLLOW
        else:
            before = os.lstat(path)
            if not stat.S_ISREG(before.st_mode):
                raise BoundedReadError(f"{path}: not a regular file")
        fd = os.open(path, flags)
        try:
            data = _read_regular_fd(fd, path, limit, before)
        finally:
            os.close(fd)
    except BoundedReadError:
        raise
    except OSError as err:
        raise BoundedReadError(f"{path}: {err}") from err
    if len(data) > limit:
        raise BoundedReadError(f"{path}: content exceeds {limit} bytes")
    return data


def read_bounded_text(path, limit):
    """Read regular UTF-8 text file path up to limit bytes, or raise BoundedReadError."""
    try:
        return read_bounded_bytes(path, limit).decode("utf-8")
    except UnicodeDecodeError as err:
        raise BoundedReadError(f"{path}: {err}") from err


def collect_nul_names(carry, names):
    """Extract NUL-terminated UTF-8 names from carry into names, refusing above cap."""
    pos = 0
    for _ in range(len(carry) + 1):
        nul = carry.find(b"\0", pos)
        if nul < 0:
            break
        raw = carry[pos:nul]
        pos = nul + 1
        if len(names) >= MAX_LISTED_RECORDS:
            raise ScanError(
                f"git ls-files exceeded the raw listing bound of {MAX_LISTED_RECORDS} records"
            )
        try:
            names.append(raw.decode("utf-8"))
        except UnicodeDecodeError as err:
            raise ScanError(f"git ls-files returned non-UTF-8 path: {err}") from err
    return carry[pos:]


def start_git_ls_files(root):
    """Start git ls-files for tracked and non-ignored untracked Go files under root."""
    try:
        return subprocess.Popen(  # nosec B603, B607  # fixed git ls-files argv, no shell
            [
                "git",
                "ls-files",
                "-z",
                "--cached",
                "--others",
                "--exclude-standard",
                "--",
                "*.go",
            ],
            cwd=root,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            start_new_session=run_bounded.GROUP,
        )
    except OSError as err:
        raise ScanError(f"git did not complete: {err}") from err


def read_git_stdout(stream):
    """Read Git's NUL-terminated paths with raw byte and record bounds."""
    names = []
    carry = b""
    total_bytes = 0
    for _ in range(MAX_LISTED_BYTES + 2):
        chunk = stream.read(READ_CHUNK_BYTES)
        if not chunk:
            break
        total_bytes += len(chunk)
        if total_bytes > MAX_LISTED_BYTES:
            raise ScanError(
                f"git ls-files exceeded the raw listing bound of {MAX_LISTED_BYTES} bytes"
            )
        carry = collect_nul_names(carry + chunk, names)
    if carry:
        raise ScanError("git ls-files output was not NUL-terminated")
    return names


def drain_git_stderr(proc, excerpt, failures):
    """Drain stderr in one reader, retaining only an excerpt and killing Git past the cap."""
    total_bytes = 0
    try:
        with proc.stderr as stream:
            for _ in range(MAX_GIT_STDERR_BYTES + 2):
                chunk = stream.read(READ_CHUNK_BYTES)
                if not chunk:
                    return
                total_bytes += len(chunk)
                if total_bytes > MAX_GIT_STDERR_BYTES:
                    raise ScanError(f"stderr exceeds {MAX_GIT_STDERR_BYTES} bytes")
                excerpt.extend(chunk[: GIT_STDERR_EXCERPT_BYTES - len(excerpt)])
    except (OSError, ScanError) as err:
        failures.append(err)
        run_bounded.stop(proc, signal.SIGKILL)


def finish_git_listing(proc, reader, deadline):
    """Stop Git and join its stderr reader within the original deadline."""
    if proc.returncode is None:
        run_bounded.stop(proc, signal.SIGKILL)
    proc.stdout.close()
    reader.join(max(0.0, deadline - time.monotonic()))
    proc.wait(timeout=max(0.0, deadline - time.monotonic()))
    if reader.is_alive():
        raise ScanError(f"git ls-files timed out after {TIMEOUT_SECONDS}s")


def list_git_targets(root):
    """Read both Git pipes under one deadline, refusing incomplete discovery."""
    deadline = time.monotonic() + TIMEOUT_SECONDS
    timed_out = threading.Event()
    proc = start_git_ls_files(root)

    def on_timeout():
        """Mark timeout and kill Git to unblock the pipe readers."""
        timed_out.set()
        run_bounded.stop(proc, signal.SIGKILL)

    timer = threading.Timer(max(0.0, deadline - time.monotonic()), on_timeout)
    excerpt = bytearray()
    failures = []
    reader = threading.Thread(
        target=drain_git_stderr, args=(proc, excerpt, failures), daemon=True
    )
    timer.start()
    reader.start()
    try:
        try:
            names = read_git_stdout(proc.stdout)
            proc.wait(timeout=max(0.0, deadline - time.monotonic()))
        finally:
            finish_git_listing(proc, reader, deadline)
    except subprocess.TimeoutExpired as err:
        raise ScanError(f"git ls-files timed out after {TIMEOUT_SECONDS}s") from err
    except OSError as err:
        raise ScanError(f"git ls-files failed: {err}") from err
    finally:
        timer.cancel()
    if timed_out.is_set():
        raise ScanError(f"git ls-files timed out after {TIMEOUT_SECONDS}s")
    if failures:
        raise ScanError(f"git ls-files failed: {failures[0]}") from failures[0]
    if proc.returncode != 0 or excerpt:
        message = format_message(excerpt.decode("utf-8", errors="replace"))
        raise ScanError(f"git ls-files failed (exit {proc.returncode}): {message}")
    return names


def has_symlink_component(resolved_root, name):
    """Report whether name is non-relative or traverses a symlink under resolved_root."""
    if name.startswith("/"):
        return True
    current = resolved_root
    for part in pathlib.PurePosixPath(name).parts:
        if part in ("", ".", ".."):
            return True
        current = current / part
        try:
            if stat.S_ISLNK(os.lstat(current).st_mode):
                return True
        except FileNotFoundError, NotADirectoryError:
            return False
        except OSError as err:
            raise ScanError(f"cannot stat target {name}: {err}") from err
    return False


def inspect_target(resolved_root, name):
    """Classify repo-relative name under resolved_root as 'bad', 'keep', or 'skip'."""
    candidate = resolved_root / name
    try:
        info = os.lstat(candidate)
    except FileNotFoundError, NotADirectoryError:
        info = None
    except OSError as err:
        raise ScanError(f"cannot stat target {name}: {err}") from err
    try:
        resolved = candidate.resolve()
    except OSError as err:
        raise ScanError(f"cannot resolve target {name}: {err}") from err
    if (
        has_symlink_component(resolved_root, name)
        or (info is not None and stat.S_ISLNK(info.st_mode))
        or resolved == resolved_root
        or not resolved.is_relative_to(resolved_root)
    ):
        return "bad"
    if info is not None and stat.S_ISREG(info.st_mode):
        return "keep"
    return "skip"


def targets(root, allow_empty=False):
    """Tracked Go files still on disk plus untracked ones Git does not ignore, fixtures left out."""
    resolved_root = pathlib.Path(root).resolve()
    listed = sorted(set(list_git_targets(resolved_root)))
    bad = []
    names = []
    for name in listed:
        if not name or name.startswith(FIXTURES + "/"):
            continue
        kind = inspect_target(resolved_root, name)
        if kind == "bad":
            bad.append(name)
        elif kind == "keep":
            names.append(name)
    if bad:
        raise ScanError(
            f"refused {len(bad)} symlink or outside-root target(s): {bad[:10]}"
        )
    if (not names and not allow_empty) or len(names) > MAX_TARGETS:
        raise ScanError(f"{len(names)} Go files to scan, expected 1 to {MAX_TARGETS}")
    return names


def is_config_input(name):
    """Report whether name is the rules file or a rule fixture."""
    return name == RULES or name.startswith(FIXTURES + "/")


def read_scan_input(root, name):
    """Read a rule configuration, fixture, or Go target using its byte limit."""
    limit = MAX_CONFIG_BYTES if is_config_input(name) else MAX_TARGET_BYTES
    return read_bounded_bytes(root / name, limit)


def write_snapshot(root, snapshot, names):
    """Copy scan inputs and return the SHA-256 digest of each copy."""
    digests = {}
    for name in (RULES, *names):
        try:
            raw = read_scan_input(root, name)
        except BoundedReadError as err:
            raise ScanError(f"cannot read target {name}: {err}") from err
        path = snapshot / name
        try:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(raw)
        except OSError as err:
            raise ScanError(f"cannot snapshot {name}: {err}") from err
        digests[name] = hashlib.sha256(raw).digest()
    return digests


def changed_targets(root, before):
    """Return sorted snapshot input paths whose live bytes changed or vanished."""
    changed = []
    for name, digest in before.items():
        try:
            raw = read_scan_input(root, name)
        except BoundedReadError:
            changed.append(name)
            continue
        if hashlib.sha256(raw).digest() != digest:
            changed.append(name)
    return sorted(changed)


def snapshot_fingerprint(digests):
    """Hash sorted UTF-8 paths, each followed by NUL and its fixed-width digest."""
    fingerprint = hashlib.sha256()
    for name, digest in sorted(digests.items()):
        fingerprint.update(name.encode("utf-8") + b"\0" + digest)
    return fingerprint.hexdigest()[:12]


def format_message(message):
    """Escape control characters in a finding message and cut past MAX_MESSAGE_CHARS."""
    escaped = (
        str(message)[: MAX_MESSAGE_CHARS + 1].encode("unicode_escape").decode("ascii")
    )
    if len(escaped) > MAX_MESSAGE_CHARS:
        keep = MAX_MESSAGE_CHARS - len(TRUNCATED_MARKER)
        return f"{escaped[:keep]}{TRUNCATED_MARKER}"
    return escaped


def report_findings(report):
    """Print findings and errors from Semgrep's JSON report, raising ScanError if any exist."""
    results = report.get("results", [])
    errors = report.get("errors", [])
    for finding in results:
        message = format_message(finding.get("extra", {}).get("message", ""))
        print(
            f"{finding['path']}:{finding['start']['line']}: {finding['check_id']}: "
            f"{message}",
            file=sys.stderr,
        )
    for error in errors:
        print(
            f"semgrep error: {error.get('type')}: {format_message(error.get('message', ''))}",
            file=sys.stderr,
        )
    if results or errors:
        raise ScanError(f"{len(results)} finding(s), {len(errors)} error(s)")


def check_scan_report(result, names):
    """Require a clean JSON report covering every snapshot target."""
    try:
        report = json.loads(result.stdout)
    except json.JSONDecodeError as err:
        sys.stderr.write(result.stderr[-4000:])
        raise ScanError(f"no JSON report (exit {result.returncode}): {err}") from err
    report_findings(report)
    scanned = set(report.get("paths", {}).get("scanned", []))
    if scanned != set(names):
        skipped = sorted(set(names) - scanned)[:10]
        raise ScanError(
            f"report scanned {len(scanned)} of {len(names)} files; not scanned: {skipped}"
        )
    if result.returncode != 0:
        raise ScanError(f"semgrep exited {result.returncode} with a clean report")


def check_target_set(root, names):
    """Require the live target set to match the set selected for the snapshot."""
    current = set(targets(root, allow_empty=True))
    expected = set(names)
    added = sorted(current - expected)
    removed = sorted(expected - current)
    if added or removed:
        paths = (added + removed)[:10]
        raise ScanError(
            f"target set changed during the scan: {len(added)} added, "
            f"{len(removed)} removed: {paths}"
        )


def prepare_snapshot(root, snapshot):
    """Copy rules, fixtures and targets into snapshot and validate rule coverage."""
    fixture_names = fixture_paths(root)
    target_names = targets(root)
    before = write_snapshot(root, snapshot, [*fixture_names, *target_names])
    ids = rule_ids(snapshot)
    check_fixtures(snapshot, fixture_names, ids)
    return ids, fixture_names, target_names, before


def scan(runner_factory, root, on_ready=None):
    """Replay fixtures and scan Go targets in one snapshot, then re-check the live tree."""
    with tempfile.TemporaryDirectory(prefix="semgrep-snapshot-") as directory:
        snapshot = pathlib.Path(directory)
        ids, fixture_names, target_names, before = prepare_snapshot(root, snapshot)
        semgrep = runner_factory(snapshot)
        if on_ready is not None:
            on_ready(semgrep)
        self_test(semgrep, fixture_names, ids)
        result = semgrep(
            [
                "scan",
                "--config",
                RULES,
                "--error",
                "--strict",
                "--json",
                *SEMGREP_FLAGS,
                "--",
                *target_names,
            ]
        )
        check_scan_report(result, target_names)
    fingerprint = snapshot_fingerprint(before)
    changed = changed_targets(root, before)
    if changed:
        raise ScanError(
            f"target contents changed during the scan ({len(changed)}): {changed[:10]}"
        )
    check_target_set(root, target_names)
    print(
        f"Semgrep: {len(target_names)} files scanned (snapshot sha256:{fingerprint}), 0 findings"
    )


def image_scan(docker, image, root):
    """Check the image pin and docker binary, then replay the fixtures and scan inside the pinned image."""
    if not IMAGE_RE.match(image):
        raise ScanError(f"image '{image}' is not pinned as NAME:TAG@sha256:DIGEST")
    resolved_docker = resolve_docker(docker)

    def runner_factory(snapshot):
        """Create the pinned image runner mounting snapshot while running docker in root."""
        return image_runner(resolved_docker, image, root, snapshot)

    scan(runner_factory, root)


def report_native_version(binary, semgrep, root):
    """Print the local Semgrep version and warn when it differs from the CI pin."""
    version = native_version(semgrep)
    print(f"Semgrep native: {binary} version {version}; CI runs the pinned image")
    pin = ci_pin(root)
    if pin is None:
        print("CI pin unknown", file=sys.stderr)
    elif version != pin:
        print(
            f"Semgrep native version {version} differs from the CI pin {pin}; "
            "the CI scan decides",
            file=sys.stderr,
        )


def native_scan(name, root):
    """Resolve the local binary, then replay the fixtures and scan with state in a temporary directory."""
    binary = shutil.which(name)
    if binary is None:
        raise ScanError(f"--semgrep '{name}' is not an executable file")
    binary = os.path.abspath(binary)
    with tempfile.TemporaryDirectory(prefix="semgrep-state-") as state:
        runner_factory = partial(native_runner, binary, state=state)
        on_ready = partial(report_native_version, binary, root=root)
        scan(runner_factory, root, on_ready=on_ready)


def ci_pin(root):
    """Read the Semgrep image tag as data, never as shell input."""
    try:
        lines = read_bounded_text(
            root / "tools/ci/tool-versions.env", MAX_CONFIG_BYTES
        ).splitlines()[:256]
    except OSError, UnicodeError:
        return None
    for line in lines:
        if line.startswith("SEMGREP_IMAGE="):
            match = re.fullmatch(
                r"SEMGREP_IMAGE=[^\s:]+:([A-Za-z0-9._-]+)@sha256:[0-9a-f]{64}", line
            )
            return match.group(1) if match else None
    return None


def main():
    """Parse the mode flags, run the scan and return the exit status."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument(
        "--image", help="Semgrep image pinned by digest (CI, hosts with docker)"
    )
    mode.add_argument(
        "--semgrep",
        help="Semgrep binary run directly (hosts without docker, lane sandboxes)",
    )
    parser.add_argument("--docker", default="docker")
    parser.add_argument("--root", default=".")
    args = parser.parse_args()
    try:
        root = checked_root(args.root)
        if args.image is not None:
            image_scan(args.docker, args.image, root)
        else:
            native_scan(args.semgrep, root)
    except ScanError as err:
        print(f"Semgrep scan failed: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
