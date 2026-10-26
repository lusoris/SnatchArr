# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Generate a GitHub Dependency Graph snapshot from web/pnpm-lock.yaml using Trivy.

pnpm 12 writes a two-document lockfile when packageManager is pinned: document 1 is
the boot document (pnpm@12.14.0 and @pnpm/exe.*), while document 2 contains the
project dependencies. GitHub's dependency graph reader parses only the first YAML
document, leaving all 442 project packages invisible to Dependabot.

Trivy fs --format github resolves the full second document when run with
--include-dev-deps and --scanners license.

A planted case runs first: a synthetic package with a lockfile holding only the
boot document must be refused by the dependency check before the real scan is
performed. If the check accepts the planted case, this script exits with status 2
("check not proven").

The check verifies that:
1. The snapshot contains a manifest for pnpm-lock.yaml.
2. Every direct dependency and devDependency from web/package.json appears in the
   resolved package_url purls (pkg:npm/<urlencoded name>@ver).

On success, the manifest key and source_location are rewritten to web/pnpm-lock.yaml
so the dependency graph shows the repository path, the snapshot is written to --out,
and package counts are printed.

Usage:
    python3 -I tools/ci/dependency_snapshot.py --trivy IMAGE:TAG@sha256:DIGEST --out PATH
"""

import argparse
import io
import json
import os
import pathlib
import re
import subprocess
import sys
import tarfile
import tempfile
import urllib.parse

IMAGE_RE = re.compile(r"^[a-z0-9./-]+:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$")
TIMEOUT_SECONDS = 900
MAX_REPORT_BYTES = 64 * 1024 * 1024
UNPACK_AND_SCAN = "mkdir -p /scan && tar -xf - -C /scan && cd /scan && exec trivy \"$@\""
BOOT_DOC_LOCKFILE = """lockfileVersion: '9.0'

importers:

  .:
    configDependencies: {}
    packageManagerDependencies:
      pnpm:
        specifier: 12.14.0
        version: 12.14.0

packages:

  '@pnpm/exe.linux-x64@12.14.0':
    resolution: {integrity: sha512-qT+0LqEr2wBVaxcG2T8cX/R8SSRz2Rvpp8nRR4fh8tnGUZRch87gFmsHUAoSQh7w6j1h0qTjrA384j4xi8frvw==}
    cpu: [x64]
    os: [linux]
"""


class ScanError(Exception):
    """A scan that cannot be trusted or that found a violation."""


class CheckNotProvenError(ScanError):
    """A planted check that was accepted instead of being refused."""


def parse_args():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--trivy", help="Trivy image pinned by digest (NAME:TAG@sha256:DIGEST)")
    parser.add_argument("--out", help="Output file path for the snapshot JSON")
    parser.add_argument("--sha", default=os.environ.get("GITHUB_SHA"), help="Commit SHA (default: $GITHUB_SHA)")
    parser.add_argument("--ref", default=os.environ.get("GITHUB_REF"), help="Git ref (default: $GITHUB_REF)")
    parser.add_argument("--docker", default="docker", help="Docker binary (default: docker)")
    parser.add_argument("--root", default=".", help="Repository root path (default: .)")
    parser.add_argument(
        "--check-fixture",
        nargs=2,
        metavar=("SNAPSHOT", "PACKAGE_JSON"),
        help="Test check_snapshot on fixture files without running Docker",
    )
    return parser.parse_args()


def tar_of_files(files_dict):
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w") as archive:
        for arcname, file_path in files_dict.items():
            archive.add(file_path, arcname=arcname, recursive=False)
    return buffer.getvalue()


def run_trivy(docker, trivy, tar_bytes, sha, ref, env_overrides=None):
    env_args = [
        "-e", f"GITHUB_SHA={sha}",
        "-e", f"GITHUB_REF={ref}",
    ]
    env = env_overrides if env_overrides is not None else os.environ
    for var in ("GITHUB_WORKFLOW", "GITHUB_JOB", "GITHUB_RUN_ID"):
        val = env.get(var)
        if val:
            env_args.extend(["-e", f"{var}={val}"])

    command = [
        docker, "run", "--rm", "-i",
        *env_args,
        "--entrypoint", "sh", trivy, "-ec", UNPACK_AND_SCAN, "trivy",
        "fs", "--format", "github", "--scanners", "license",
        "--skip-version-check", "--disable-telemetry", "--include-dev-deps", ".",
    ]
    try:
        proc = subprocess.run(
            command,
            input=tar_bytes,
            capture_output=True,
            timeout=TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as err:
        raise ScanError(f"{docker} did not complete: {err}") from err

    stderr_text = proc.stderr.decode("utf-8", "replace")[-4000:]
    if proc.returncode != 0:
        sys.stderr.write(stderr_text)
        raise ScanError(f"trivy exited {proc.returncode}")

    if len(proc.stdout) > MAX_REPORT_BYTES:
        raise ScanError(f"report length ({len(proc.stdout)}) exceeds {MAX_REPORT_BYTES} bytes")

    stdout_text = proc.stdout.decode("utf-8", "replace").strip()
    idx = stdout_text.find("{")
    if idx == -1:
        sys.stderr.write(stderr_text)
        raise ScanError("no JSON report found in trivy output")

    try:
        report = json.loads(stdout_text[idx:])
    except json.JSONDecodeError as err:
        sys.stderr.write(stderr_text)
        raise ScanError(f"invalid JSON report: {err}") from err

    return report


def check_snapshot(snapshot, package_json_data, manifest_key="pnpm-lock.yaml"):
    manifests = snapshot.get("manifests")
    if not isinstance(manifests, dict) or manifest_key not in manifests:
        raise ScanError(f"snapshot missing manifest for {manifest_key}")

    manifest = manifests[manifest_key]
    if not isinstance(manifest, dict):
        raise ScanError(f"manifest '{manifest_key}' is not an object")

    resolved = manifest.get("resolved")
    if not isinstance(resolved, dict):
        raise ScanError(f"manifest '{manifest_key}' missing resolved dictionary")

    resolved_names = set()
    for item in resolved.values():
        if not isinstance(item, dict):
            continue
        purl = item.get("package_url") or ""
        if purl.startswith("pkg:npm/"):
            name_part, _, _ = purl[len("pkg:npm/"):].rpartition("@")
            resolved_names.add(urllib.parse.unquote(name_part))

    deps = package_json_data.get("dependencies") or {}
    dev_deps = package_json_data.get("devDependencies") or {}
    direct_names = set(deps.keys()) | set(dev_deps.keys())

    missing = sorted(direct_names - resolved_names)
    if missing:
        raise ScanError(f"direct dependencies missing from resolved snapshot: {', '.join(missing)}")

    return len(resolved), len(direct_names)


def rewrite_manifest_location(snapshot, old_key="pnpm-lock.yaml", new_key="web/pnpm-lock.yaml"):
    manifests = snapshot.get("manifests", {})
    if old_key in manifests:
        manifest = manifests.pop(old_key)
        manifest.setdefault("file", {})["source_location"] = new_key
        manifests[new_key] = manifest


def self_test_planted(docker, trivy):
    with tempfile.TemporaryDirectory() as temp:
        temp_dir = pathlib.Path(temp)
        pkg_json = temp_dir / "package.json"
        lock_yaml = temp_dir / "pnpm-lock.yaml"
        pkg_json.write_text(
            json.dumps({"name": "planted-boot-only", "dependencies": {"svelte": "^5.0.0"}}, indent=2) + "\n",
            encoding="utf-8",
        )
        lock_yaml.write_text(BOOT_DOC_LOCKFILE, encoding="utf-8")
        tar_bytes = tar_of_files({"package.json": pkg_json, "pnpm-lock.yaml": lock_yaml})
        snapshot = run_trivy(docker, trivy, tar_bytes, sha="0" * 40, ref="refs/heads/main", env_overrides={})
        pkg_data = json.loads(pkg_json.read_text(encoding="utf-8"))
        try:
            check_snapshot(snapshot, pkg_data, manifest_key="pnpm-lock.yaml")
        except ScanError:
            # Expected refusal: boot-document-only lockfile cannot satisfy direct dependencies.
            return
        raise CheckNotProvenError("planted case accepted: check not proven")


def run_fixture_check(check_fixture_args):
    snap_path = pathlib.Path(check_fixture_args[0])
    pkg_path = pathlib.Path(check_fixture_args[1])
    try:
        snap_data = json.loads(snap_path.read_text(encoding="utf-8"))
        pkg_data = json.loads(pkg_path.read_text(encoding="utf-8"))
        resolved_count, direct_count = check_snapshot(snap_data, pkg_data)
        print(f"Fixture check ok: {resolved_count} resolved packages, {direct_count} direct dependencies checked")
        return 0
    except ScanError as err:
        print(f"Fixture check refused: {err}", file=sys.stderr)
        return 1


def scan_and_write(args, sha, ref):
    root = pathlib.Path(args.root).resolve()
    web_dir = root / "web"
    pkg_json_path = web_dir / "package.json"
    lock_yaml_path = web_dir / "pnpm-lock.yaml"
    if not pkg_json_path.is_file() or not lock_yaml_path.is_file():
        raise ScanError(f"missing web/package.json or web/pnpm-lock.yaml under {root}")

    self_test_planted(args.docker, args.trivy)

    tar_bytes = tar_of_files({"package.json": pkg_json_path, "pnpm-lock.yaml": lock_yaml_path})
    snapshot = run_trivy(args.docker, args.trivy, tar_bytes, sha, ref)
    pkg_data = json.loads(pkg_json_path.read_text(encoding="utf-8"))

    resolved_count, direct_count = check_snapshot(snapshot, pkg_data, manifest_key="pnpm-lock.yaml")
    rewrite_manifest_location(snapshot, old_key="pnpm-lock.yaml", new_key="web/pnpm-lock.yaml")

    out_path = pathlib.Path(args.out).resolve()
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(json.dumps(snapshot, indent=2) + "\n", encoding="utf-8")

    print(f"Dependency snapshot: {resolved_count} resolved packages, {direct_count} direct dependencies checked")
    return 0


def main():
    args = parse_args()
    if args.check_fixture:
        return run_fixture_check(args.check_fixture)

    if not args.trivy:
        print("error: --trivy is required", file=sys.stderr)
        return 1
    if not args.out:
        print("error: --out is required", file=sys.stderr)
        return 1

    try:
        if not IMAGE_RE.match(args.trivy):
            raise ScanError(f"image '{args.trivy}' is not pinned as NAME:TAG@sha256:DIGEST")

        sha = args.sha
        if not sha:
            raise ScanError("--sha or GITHUB_SHA environment variable is required")
        ref = args.ref
        if not ref:
            raise ScanError("--ref or GITHUB_REF environment variable is required")

        return scan_and_write(args, sha, ref)
    except CheckNotProvenError as err:
        print(f"{err}", file=sys.stderr)
        return 2
    except ScanError as err:
        print(f"Dependency snapshot failed: {err}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
