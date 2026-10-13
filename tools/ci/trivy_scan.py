"""Run Trivy in its digest-pinned image over the tracked files, planted case first.

The planted tree holds a go.mod that requires a vulnerable golang.org/x/text, a
Dockerfile that runs as root and an AWS key pair; Trivy must report all three
before the scan of the tracked files is trusted. That scan covers vulnerable Go
modules, Rust crates and pnpm packages, secrets, and misconfigurations in the
Dockerfiles and the Helm chart.

The real scan fails on any HIGH or CRITICAL finding that has a fix, on any
Trivy error, and on a report that leaves out a tracked go.mod, Cargo.lock,
pnpm-lock.yaml or Dockerfile. No .trivyignore is read, so no finding is excepted.

Copied from tekmora/discord-bot tools/ci/trivy_scan.py (EUPL-1.2, same author)
at commit 70642249adc9d99375b707cebe0eb756970905cc. Changed here: this
docstring, the cache volume name, the lockfiles the report must cover, and the
image mode is left out because no job here scans a built image.

Usage: python3 -I tools/ci/trivy_scan.py --trivy NAME:TAG@sha256:DIGEST
"""

import argparse
import io
import json
import pathlib
import re
import subprocess
import sys
import tarfile
import tempfile

IMAGE_RE = re.compile(r"^[a-z0-9./-]+:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$")
CACHE_VOLUME = "snatcharr-trivy-cache"
TIMEOUT_SECONDS = 900
MAX_FILES = 4096
MAX_REPORT_BYTES = 64 * 1024 * 1024
SCAN_FLAGS = ["--scanners", "vuln,secret,misconfig", "--severity", "HIGH,CRITICAL",
              "--ignore-unfixed", "--ignorefile", "", "--skip-version-check",
              "--disable-telemetry", "--format", "json", "--exit-code", "1"]
# Trivy result type of each lockfile the report must cover.
LOCKFILES = {"gomod": "go.mod", "cargo": "Cargo.lock", "pnpm": "pnpm-lock.yaml"}
# Unpacks the tar on stdin and runs trivy on it; arguments come in as "$@".
UNPACK_AND_SCAN = "mkdir -p /scan && tar -xf - -C /scan && cd /scan && exec trivy \"$@\""


class ScanError(Exception):
    """A scan that cannot be trusted or that found a violation."""


def run(command, stdin=b""):
    try:
        return subprocess.run(command, input=stdin, capture_output=True,
                              timeout=TIMEOUT_SECONDS, check=False)
    except (OSError, subprocess.TimeoutExpired) as err:
        raise ScanError(f"{command[0]} did not complete: {err}") from err


def report_of(result):
    """Trivy exits 1 exactly when it reports a finding; anything else is an error."""
    stderr = result.stderr.decode("utf-8", "replace")[-4000:]
    if result.returncode not in (0, 1) or len(result.stdout) > MAX_REPORT_BYTES:
        sys.stderr.write(stderr)
        raise ScanError(f"trivy exited {result.returncode}")
    try:
        report = json.loads(result.stdout)
    except json.JSONDecodeError as err:
        sys.stderr.write(stderr)
        raise ScanError(f"no JSON report (exit {result.returncode}): {err}") from err
    if (result.returncode == 1) != bool(findings(report)):
        sys.stderr.write(stderr)
        raise ScanError(f"trivy exited {result.returncode} with {len(findings(report))} finding(s)")
    return report


def findings(report):
    found = []
    for result in report.get("Results") or []:
        for key in ("Vulnerabilities", "Misconfigurations", "Secrets"):
            for item in result.get(key) or []:
                ident = item.get("VulnerabilityID") or item.get("ID") or item.get("RuleID")
                found.append((result.get("Target"), ident, item.get("Severity"), item.get("Title", "")))
    return found


def trivy_fs(docker, trivy, tar_bytes):
    command = [docker, "run", "--rm", "-i", "--volume", f"{CACHE_VOLUME}:/var/cache/trivy",
               "--entrypoint", "sh", trivy, "-ec", UNPACK_AND_SCAN, "trivy",
               "--cache-dir", "/var/cache/trivy", "fs", *SCAN_FLAGS, "."]
    return report_of(run(command, tar_bytes))


def tar_of(root, names):
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w") as archive:
        for name in names:
            archive.add(root / name, arcname=name, recursive=False)
    return buffer.getvalue()


def by_type(report, kind):
    return {r.get("Target"): r for r in report.get("Results") or [] if r.get("Type") == kind}


def planted_tree(directory):
    # The key pair is joined from pieces here, so that neither the key nor a
    # keyword next to a long literal is in the source for gitleaks to report.
    key_id = "".join(("AKIA", "Z7Q3XWERT5YUIOPA"))
    key_value = "/".join(("wJalrXUtnFEMI", "K7MDENG", "bPxRfiCYZXAMPLEKEYQ"))
    files = {
        "go.mod": "module example.com/planted\n\ngo 1.22\n\nrequire golang.org/x/text v0.3.0\n",
        "Dockerfile": "FROM debian:13\nRUN apt-get update\n",
        "config.env": f"aws_access_key_id = {key_id}\naws_secret_access_key = {key_value}\n",
    }
    for name, text in files.items():
        (directory / name).write_text(text, encoding="utf-8")
    return sorted(files)


def self_test_fs(docker, trivy):
    with tempfile.TemporaryDirectory() as temp:
        directory = pathlib.Path(temp)
        report = trivy_fs(docker, trivy, tar_of(directory, planted_tree(directory)))
    reported = {target for target, _, _, _ in findings(report)}
    missing = sorted({"go.mod", "Dockerfile", "config.env"} - reported)
    if missing:
        raise ScanError(f"planted case(s) not reported: {', '.join(missing)}")
    print(f"Trivy fs self-test: planted module, Dockerfile and key reported "
          f"({len(findings(report))} findings)")


def tracked(root):
    listed = run(["git", "-C", str(root), "ls-files", "-z"])
    if listed.returncode != 0:
        raise ScanError(f"git ls-files failed: {listed.stderr.decode('utf-8', 'replace').strip()}")
    names = [n for n in listed.stdout.decode("utf-8").split("\0") if n and (root / n).is_file()]
    if not names or len(names) > MAX_FILES:
        raise ScanError(f"{len(names)} tracked files, expected 1 to {MAX_FILES}")
    return names


def is_dockerfile(name):
    base = pathlib.PurePosixPath(name).name
    return base == "Dockerfile" or base.startswith("Dockerfile.") or base.endswith(".Dockerfile")


def fail_on(found):
    for target, ident, severity, title in found:
        print(f"{target}: {ident} ({severity}): {title}", file=sys.stderr)
    if found:
        raise ScanError(f"{len(found)} finding(s)")


def scan_fs(docker, trivy, root):
    names = tracked(root)
    report = trivy_fs(docker, trivy, tar_of(root, names))
    fail_on(findings(report))
    expected = {kind: {n for n in names if pathlib.PurePosixPath(n).name == base}
                for kind, base in LOCKFILES.items()}
    expected["dockerfile"] = {n for n in names if is_dockerfile(n)}
    missing = sorted(name for kind, wanted in expected.items()
                     for name in wanted - set(by_type(report, kind)))
    empty = sorted(kind for kind, wanted in expected.items() if not wanted)
    if missing or empty:
        raise ScanError(f"report left out: {missing}; no tracked input of type: {empty}")
    counts = ", ".join(f"{len(wanted)} {kind}" for kind, wanted in sorted(expected.items()))
    print(f"Trivy fs: {len(names)} tracked files ({counts}), 0 findings")



def parse_args():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--trivy", required=True, help="Trivy image pinned by digest")
    parser.add_argument("--docker", default="docker")
    parser.add_argument("--root", default=".")
    return parser.parse_args()


def main():
    args = parse_args()
    try:
        if not IMAGE_RE.match(args.trivy):
            raise ScanError(f"image '{args.trivy}' is not pinned as NAME:TAG@sha256:DIGEST")
        self_test_fs(args.docker, args.trivy)
        scan_fs(args.docker, args.trivy, pathlib.Path(args.root).resolve())
    except ScanError as err:
        print(f"Trivy scan failed: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
