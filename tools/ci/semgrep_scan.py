"""Run the repository's Semgrep rules in a digest-pinned image, fixtures first.

Each rule in .semgrep.yml needs a fixture in tools/ci/semgrep/testdata/ with at
least one `// ruleid:` line it must report and one `// ok:` line it must not.
Every fixture starts with the `//go:build semgrep_fixture` constraint, so no Go
build, vet or gosec run ever loads the code the rules are meant to report.
Semgrep's test mode replays every fixture; only when all pass is the scan of
the Go files trusted: every tracked file still present plus every untracked file
Git does not ignore, so a new file in a working tree is scanned too. The scan
fails on any finding, on any Semgrep error, and when the report does not list
exactly the files that were passed.

`--semgrep PATH` runs a Semgrep binary directly instead of the image, for hosts
without docker such as a lane sandbox. It prints the binary's version, keeps
Semgrep's state files in a temporary directory, and applies the same fixture
replay and scanned-file check; CI keeps the pinned image. A hand-run
`semgrep scan` on a directory skips `*_test.go` files without saying so (PR 1244
round r16), so lane briefs name this mode (.config/lane-brief.yaml).

Copied from tekmora/discord-bot tools/ci/semgrep_scan.py (EUPL-1.2, same author)
at commit 70642249adc9d99375b707cebe0eb756970905cc. Changed here: this paragraph,
the `--semgrep` mode and the untracked-file targets.

Usage: python3 -I tools/ci/semgrep_scan.py --image NAME:TAG@sha256:DIGEST
       python3 -I tools/ci/semgrep_scan.py --semgrep semgrep
"""

import argparse
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile

RULES = ".semgrep.yml"
FIXTURES = "tools/ci/semgrep/testdata"
FIXTURE_CONSTRAINT = "//go:build semgrep_fixture\n"
MAX_FIXTURES = 64
MAX_TARGETS = 4096
TIMEOUT_SECONDS = 600
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


# Keep this call byte-identical: the Opengrep baseline hashes its source text.
# fmt: off
def run(command, root):
    """Run command in root with a bound; a start failure or timeout is a ScanError."""
    try:
        return subprocess.run(command, cwd=root, capture_output=True, text=True,
                              timeout=TIMEOUT_SECONDS, check=False)
    except (OSError, subprocess.TimeoutExpired) as err:
        raise ScanError(f"{command[0]} did not complete: {err}") from err
# fmt: on


def image_runner(docker, image, root):
    """Return a call that runs Semgrep in the pinned image, the tree mounted read-only."""

    def call(args):
        """Run Semgrep with args in a fresh container without network."""
        command = [
            docker,
            "run",
            "--rm",
            "--network",
            "none",
            "--volume",
            f"{root}:/src:ro",
            "--workdir",
            "/src",
            "--entrypoint",
            "semgrep",
            image,
            *args,
        ]
        return run(command, root)

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
            return run([binary, *args], root)
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
    ids = RULE_ID_RE.findall((root / RULES).read_text(encoding="utf-8"))
    if not ids:
        raise ScanError(f"{RULES} declares no rule")
    if len(ids) != len(set(ids)):
        raise ScanError(f"{RULES} declares a rule id twice")
    return set(ids)


def fixtures(root, ids):
    files = sorted((root / FIXTURES).glob("*.go"))
    if not files or len(files) > MAX_FIXTURES:
        raise ScanError(
            f"{FIXTURES}: {len(files)} fixtures, expected 1 to {MAX_FIXTURES}"
        )
    seen = {"ruleid": set(), "ok": set()}
    for path in files:
        text = path.read_text(encoding="utf-8")
        if not text.startswith(FIXTURE_CONSTRAINT):
            raise ScanError(
                f"{path.name}: does not start with {FIXTURE_CONSTRAINT.strip()!r}"
            )
        for kind, rule in ANNOTATION_RE.findall(text):
            if rule not in ids:
                raise ScanError(f"{path.name}: annotation names unknown rule '{rule}'")
            seen[kind].add(rule)
    for kind, covered in sorted(seen.items()):
        missing = sorted(ids - covered)
        if missing:
            raise ScanError(
                f"no `{kind}:` fixture line for rule(s): {', '.join(missing)}"
            )
    return [path.relative_to(root).as_posix() for path in files]


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


def targets(root, allow_empty=False):
    """Tracked Go files still on disk plus untracked ones Git does not ignore, fixtures left out."""
    listed = run(
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
        root,
    )
    if listed.returncode != 0:
        raise ScanError(f"git ls-files failed: {listed.stderr.strip()}")
    names = sorted(
        {
            n
            for n in listed.stdout.split("\0")
            if n and not n.startswith(FIXTURES + "/") and (root / n).is_file()
        }
    )
    if (not names and not allow_empty) or len(names) > MAX_TARGETS:
        raise ScanError(f"{len(names)} Go files to scan, expected 1 to {MAX_TARGETS}")
    return names


def scan(semgrep, root):
    """Scan every target and fail unless the report is clean and names each target as scanned."""
    names = targets(root)
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
            *names,
        ]
    )
    try:
        report = json.loads(result.stdout)
    except json.JSONDecodeError as err:
        sys.stderr.write(result.stderr[-4000:])
        raise ScanError(f"no JSON report (exit {result.returncode}): {err}") from err
    for finding in report.get("results", []):
        print(
            f"{finding['path']}:{finding['start']['line']}: {finding['check_id']}: "
            f"{finding['extra']['message']}",
            file=sys.stderr,
        )
    for error in report.get("errors", []):
        print(
            f"semgrep error: {error.get('type')}: {error.get('message')}",
            file=sys.stderr,
        )
    scanned = set(report.get("paths", {}).get("scanned", []))
    if report.get("results") or report.get("errors"):
        raise ScanError(
            f"{len(report.get('results', []))} finding(s), {len(report.get('errors', []))} error(s)"
        )
    if scanned != set(names):
        skipped = sorted(set(names) - scanned)[:10]
        raise ScanError(
            f"report scanned {len(scanned)} of {len(names)} files; not scanned: {skipped}"
        )
    if result.returncode != 0:
        raise ScanError(f"semgrep exited {result.returncode} with a clean report")
    current = set(targets(root, allow_empty=True))
    added = sorted(current - set(names))
    removed = sorted(set(names) - current)
    if added or removed:
        paths = (added + removed)[:10]
        raise ScanError(
            f"target set changed during the scan: {len(added)} added, "
            f"{len(removed)} removed: {paths}"
        )
    print(f"Semgrep: {len(names)} files scanned, 0 findings")


def checked_inputs(root):
    """Rule ids and fixture paths, refused before any Semgrep call."""
    ids = rule_ids(root)
    return ids, fixtures(root, ids)


def image_scan(docker, image, root):
    """Check the image pin, then replay the fixtures and scan inside the pinned image."""
    if not IMAGE_RE.match(image):
        raise ScanError(f"image '{image}' is not pinned as NAME:TAG@sha256:DIGEST")
    ids, names = checked_inputs(root)
    semgrep = image_runner(docker, image, root)
    self_test(semgrep, names, ids)
    scan(semgrep, root)


def native_scan(name, root):
    """Resolve the local binary, then replay the fixtures and scan with state in a temporary directory."""
    binary = shutil.which(name)
    if binary is None:
        raise ScanError(f"--semgrep '{name}' is not an executable file")
    binary = os.path.abspath(binary)
    ids, names = checked_inputs(root)
    with tempfile.TemporaryDirectory(prefix="semgrep-state-") as state:
        semgrep = native_runner(binary, root, state)
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
        self_test(semgrep, names, ids)
        scan(semgrep, root)


def ci_pin(root):
    """Read the Semgrep image tag as data, never as shell input."""
    try:
        with (root / "tools/ci/tool-versions.env").open(encoding="utf-8") as source:
            lines = [source.readline() for _ in range(256)]
    except OSError, UnicodeError:
        return None
    for line in lines:
        if line.startswith("SEMGREP_IMAGE="):
            match = re.fullmatch(
                r"SEMGREP_IMAGE=[^\s:]+:([A-Za-z0-9._-]+)@sha256:[0-9a-f]{64}\n?", line
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
    root = pathlib.Path(args.root).resolve()
    try:
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
