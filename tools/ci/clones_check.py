# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Check that no codebase clone touches any declared clone-free path.

Scans the repository with praetorctl dedupe scan --json, verifying that neither
native Go duplicates nor foreign (jscpd) duplicates overlap with files declared
in tools/ci/clone-free.txt. Resolves PRAETORCTL (default praetorctl) either by
fixed allowed name on PATH or as an executable absolute path with that basename.
"""

import argparse
import json
import os
import pathlib
import shutil
import subprocess
import sys
import threading

TIMEOUT_SECONDS = 60
LOOKUP_TIMEOUT_SECONDS = 5
ALLOWED_NAMES = ("praetorctl", "standardsctl")


class CheckError(Exception):
    """Raised when the check or scan encounters a fatal issue."""

    def __init__(self, message: str, exit_code: int = 2) -> None:
        super().__init__(message)
        self.message = message
        self.exit_code = exit_code


def strip_lang_suffix(path_str: str) -> str:
    """Strip a trailing :<lang> suffix such as :typescript, :css, :html."""
    if ":" in path_str:
        prefix, suffix = path_str.rsplit(":", 1)
        if suffix.isalnum():
            return prefix
    return path_str


def load_clone_free_paths(clone_free_file: pathlib.Path) -> list[str]:
    """Load non-empty, non-comment paths from the clone-free file."""
    if not clone_free_file.is_file():
        raise CheckError(
            f"clones_check: clone-free file not found: {clone_free_file}",
            exit_code=2,
        )
    try:
        lines = clone_free_file.read_text(encoding="utf-8").splitlines()
    except Exception as err:
        raise CheckError(
            f"clones_check: error reading clone-free file {clone_free_file}: {err}",
            exit_code=2,
        ) from err

    paths: list[str] = []
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        paths.append(stripped)

    if not paths:
        raise CheckError(
            f"clones_check: clone-free file has no entries: {clone_free_file}",
            exit_code=2,
        )
    return paths


def fetch_report_from_file(report_file: str) -> str:
    """Read scan report JSON content from a specified file."""
    report_path = pathlib.Path(report_file).resolve()
    if not report_path.is_file():
        raise CheckError(
            f"clones_check: report file not found: {report_path}",
            exit_code=2,
        )
    try:
        return report_path.read_text(encoding="utf-8")
    except Exception as err:
        raise CheckError(
            f"clones_check: cannot read report file {report_path}: {err}",
            exit_code=2,
        ) from err


def _find_praetorctl(value: str) -> str:
    """Filesystem-only lookup of VALUE; may block, so run it on a worker thread."""
    if value in ALLOWED_NAMES:
        resolved = shutil.which(value)
        if not resolved:
            raise CheckError(
                f"clones_check: praetorctl binary not found on PATH: {value}",
                exit_code=2,
            )
        if not os.path.isabs(resolved):
            # A relative PATH entry yields a relative result; anchor it to the
            # caller's cwd now, before the scan changes directory. Do not
            # normalize or resolve symlinks, so the invoked name is preserved.
            try:
                resolved = os.path.join(os.getcwd(), resolved)
            except OSError as err:
                raise CheckError(
                    f"clones_check: cannot make {resolved!r} absolute: {err}",
                    exit_code=2,
                ) from err
        return resolved
    candidate = pathlib.Path(value)
    if (
        os.path.isabs(value)
        and os.path.basename(value) in ALLOWED_NAMES
        and candidate.is_file()
        and os.access(candidate, os.X_OK)
    ):
        return value
    raise CheckError(
        f"clones_check: invalid PRAETORCTL={value!r}: not an allowed executable",
        exit_code=2,
    )


def _record_lookup(value: str, results: list[str], errors: list[CheckError]) -> None:
    """Run the lookup, recording one result or one CheckError.

    Expected OS/path failures (OSError, ValueError) are converted to a named
    CheckError with exit code 2 that includes and chains the cause. An existing
    CheckError is recorded unchanged.
    """
    try:
        results.append(_find_praetorctl(value))
    except CheckError as err:
        errors.append(err)
    except (OSError, ValueError) as err:
        wrapped = CheckError(
            f"clones_check: praetorctl lookup failed: {type(err).__name__}: {err}",
            exit_code=2,
        )
        wrapped.__cause__ = err
        errors.append(wrapped)


def resolve_praetorctl(timeout: float | None = None) -> str:
    """Resolve PRAETORCTL to an allowed executable path or raise CheckError.

    The filesystem lookup runs on a daemon worker and is bounded by TIMEOUT
    (default LOOKUP_TIMEOUT_SECONDS). The worker never starts a process.
    """
    limit = LOOKUP_TIMEOUT_SECONDS if timeout is None else timeout
    value = os.environ.get("PRAETORCTL", "praetorctl")
    results: list[str] = []
    errors: list[CheckError] = []
    worker = threading.Thread(
        target=_record_lookup,
        args=(value, results, errors),
        daemon=True,
    )
    worker.start()
    worker.join(timeout=limit)
    if worker.is_alive():
        raise CheckError(
            f"clones_check: praetorctl lookup timed out after {limit}s",
            exit_code=2,
        )
    if errors:
        raise errors[0]
    if not results:
        raise CheckError(
            "clones_check: praetorctl lookup ended without a result",
            exit_code=2,
        )
    return results[0]


def run_scan_report(root: pathlib.Path) -> str:
    """Execute praetorctl dedupe scan --json and return raw output."""
    resolved = resolve_praetorctl()

    jscpd_bin = root / "web" / "node_modules" / ".bin" / "jscpd"
    if not jscpd_bin.is_file() or not os.access(jscpd_bin, os.X_OK):
        raise CheckError(
            f"clones_check: jscpd binary missing or not executable at {jscpd_bin}",
            exit_code=2,
        )

    env = os.environ.copy()
    env["PRAETOR_JSCPD_BIN"] = str(jscpd_bin)
    try:
        proc = subprocess.run(
            ["praetorctl", "dedupe", "scan", "--json", str(root)],
            executable=resolved,
            cwd=str(root),
            env=env,
            capture_output=True,
            text=True,
            timeout=TIMEOUT_SECONDS,
            check=False,
        )
    except subprocess.TimeoutExpired as err:
        raise CheckError(
            f"clones_check: scan timed out after {TIMEOUT_SECONDS}s",
            exit_code=2,
        ) from err
    except Exception as err:
        raise CheckError(
            f"clones_check: failed to execute scan: {err}",
            exit_code=2,
        ) from err

    # Exit 1 is the scan's own verdict on findings elsewhere in the tree; the JSON report
    # is still complete then (fetch_report checks `passed`). Any other exit is an error.
    if proc.returncode not in (0, 1):
        raise CheckError(
            f"clones_check: scan exited {proc.returncode}: {proc.stderr.strip()}",
            exit_code=2,
        )
    return proc.stdout


def fetch_report(root: pathlib.Path, report_file: str | None) -> dict:
    """Fetch and validate dedupe scan report structure."""
    if report_file:
        content = fetch_report_from_file(report_file)
    else:
        content = run_scan_report(root)

    try:
        data = json.loads(content)
    except Exception as err:
        raise CheckError(
            f"clones_check: scan report is not valid JSON: {err}",
            exit_code=2,
        ) from err

    if not isinstance(data, dict) or "passed" not in data:
        raise CheckError(
            "clones_check: scan report has no 'passed' field",
            exit_code=2,
        )

    foreign_scanner = data.get("foreign_scanner")
    if not foreign_scanner or not str(foreign_scanner).strip():
        raise CheckError(
            "clones_check: foreign_scanner is empty in dedupe report",
            exit_code=2,
        )

    return data


def is_clone_free(path: str, clone_free_paths: list[str]) -> bool:
    """Match an exact file entry, or a directory entry that ends in a slash."""
    return any(
        path.startswith(entry) if entry.endswith("/") else path == entry
        for entry in clone_free_paths
    )


def check_clones(report: dict, clone_free_paths: list[str]) -> int:
    """Verify that no duplicate group touches any clone-free path."""
    matching_clones: list[dict] = []
    duplicate_groups = report.get("duplicates", []) + report.get(
        "foreign_duplicates", []
    )

    for group in duplicate_groups:
        locs = group.get("locations", [])
        touches = False
        for loc in locs:
            path_str = loc.get("path", "")
            clean_path = strip_lang_suffix(path_str)
            if is_clone_free(clean_path, clone_free_paths):
                touches = True
                break
        if touches:
            matching_clones.append(group)

    if matching_clones:
        messages: list[str] = []
        for group in matching_clones:
            messages.append(
                f"clones_check: clone touches clone-free path: hash={group.get('hash')} loc={group.get('loc')}"
            )
            for loc in group.get("locations", []):
                loc_path = loc.get("path", "")
                line = loc.get("line")
                func_name = loc.get("func_name")
                line_str = f":{line}" if line is not None else ""
                func_str = f" in {func_name}()" if func_name else ""
                messages.append(f"  - {loc_path}{line_str}{func_str}")
        raise CheckError("\n".join(messages), exit_code=1)

    print(
        f"clones_check: {len(clone_free_paths)} clone-free paths, 0 clones touch them"
    )
    return 0


def main() -> int:
    default_root = pathlib.Path(__file__).resolve().parent.parent.parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--root",
        default=str(default_root),
        help="Repository root directory (default: parent of tools/)",
    )
    parser.add_argument(
        "--report",
        default=None,
        help="Path to a pre-generated dedupe scan JSON report file",
    )
    parser.add_argument(
        "--clone-free",
        default=None,
        help="Path to clone-free path list file (default: <root>/tools/ci/clone-free.txt)",
    )

    args = parser.parse_args()
    root = pathlib.Path(args.root).resolve()
    clone_free_file = (
        pathlib.Path(args.clone_free).resolve()
        if args.clone_free
        else root / "tools" / "ci" / "clone-free.txt"
    )

    try:
        clone_free_paths = load_clone_free_paths(clone_free_file)
        report = fetch_report(root, args.report)
        return check_clones(report, clone_free_paths)
    except CheckError as err:
        print(err.message, file=sys.stderr)
        return err.exit_code


if __name__ == "__main__":
    sys.exit(main())
