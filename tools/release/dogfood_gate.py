# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse a release tag whose first-parent commit lacks a live dogfood pass.

A release-please tag commit has one parent on `main` and touches only the version
manifest, changelog, and `extra-files` declared in `release-please-config.json`.
Before the release workflow builds images or charts, this gate verifies through the
GitHub REST API (or an offline `--api-dir` fixture directory) that:
1. the tag commit has exactly one parent;
2. the parent commit's newest `snatcharr/dogfood` status is `success` and its
   description names the live instance (`cluster` by default);
3. every file changed between the parent commit and the tag commit belongs to the
   allowed set derived from `release-please-config.json`.
"""

import argparse
import importlib.util
import json
import re
import subprocess
import sys
import tempfile
from pathlib import Path

GH_TIMEOUT_SECONDS = 60
DOGFOOD_CONTEXT = "snatcharr/dogfood"
LIVE_INSTANCE = "cluster"
DEFAULT_CONFIG = "release-please-config.json"
DEFAULT_MANIFEST = ".release-please-manifest.json"
DEFAULT_CHANGELOG = "CHANGELOG.md"
MAX_REFUSED_FILES = 10
REPO_RE = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
TAG_RE = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+([+-][A-Za-z0-9._-]+)?$")
INSTANCE_RE = re.compile(r"^[a-z0-9][a-z0-9-]{0,31}$")
STATUS_INSTANCE_RE = re.compile(r"^MCP dogfood on ([a-z0-9][a-z0-9-]{0,31}): ")


class CheckError(Exception):
    """GitHub API failure, invalid JSON, or invalid configuration."""


class SelfTestError(Exception):
    """A planted self-test case did not behave as required."""


def endpoint_filename(endpoint: str) -> str:
    """Map a GitHub REST API endpoint path to its fixture filename."""
    return f"{endpoint.strip('/').replace('/', '_')}.json"


def read_api(endpoint: str, api_dir: Path | None) -> dict:
    """Read a GitHub REST API JSON object from api_dir or `gh api`."""
    if api_dir is not None:
        target = api_dir / endpoint_filename(endpoint)
        try:
            raw = target.read_text(encoding="utf-8")
        except OSError as error:
            raise CheckError(f"cannot read fixture {target}: {error}") from error
    else:
        try:
            result = subprocess.run(
                ["gh", "api", endpoint],
                capture_output=True,
                text=True,
                timeout=GH_TIMEOUT_SECONDS,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired) as error:
            raise CheckError(f"gh api {endpoint} did not complete: {error}") from error
        if result.returncode != 0:
            detail = result.stderr.strip() or f"exit {result.returncode}"
            raise CheckError(f"gh api {endpoint} failed: {detail}")
        raw = result.stdout
    try:
        payload = json.loads(raw)
    except ValueError as error:
        raise CheckError(f"invalid JSON from {endpoint}: {error}") from error
    if not isinstance(payload, dict):
        raise CheckError(f"expected JSON object from {endpoint}")
    return payload


def extract_extra_path(entry: object) -> str:
    """Return the file path from a string or object extra-files entry."""
    if isinstance(entry, str) and entry:
        return entry
    if isinstance(entry, dict):
        path = entry.get("path")
        if isinstance(path, str) and path:
            return path
    raise CheckError(f"invalid extra-files entry in release-please config: {entry!r}")


def package_allowed_files(pkg_key: str, pkg_cfg: object) -> set[str]:
    """Return allowed changelog and extra-files paths for one package entry."""
    if not isinstance(pkg_key, str) or not isinstance(pkg_cfg, dict):
        raise CheckError("invalid package entry in release-please config")
    changelog = pkg_cfg.get("changelog-path", DEFAULT_CHANGELOG)
    if not isinstance(changelog, str) or not changelog:
        raise CheckError(f"invalid changelog-path for package {pkg_key!r}")
    prefix = "" if pkg_key in ("", ".") else pkg_key.strip("/")
    files = {f"{prefix}/{changelog}" if prefix else changelog}
    extra_files = pkg_cfg.get("extra-files", [])
    if not isinstance(extra_files, list):
        raise CheckError(f"invalid extra-files list for package {pkg_key!r}")
    for entry in extra_files:
        files.add(extract_extra_path(entry))
    return files


def allowed_files(config_path: Path) -> set[str]:
    """Derive the set of files a release commit may touch from release-please config."""
    try:
        config = json.loads(config_path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as error:
        raise CheckError(f"cannot read config {config_path}: {error}") from error
    if not isinstance(config, dict):
        raise CheckError(f"config {config_path} is not a JSON object")
    manifest = config.get("manifest-file", DEFAULT_MANIFEST)
    if not isinstance(manifest, str) or not manifest:
        raise CheckError("invalid manifest-file in release-please config")
    packages = config.get("packages")
    if not isinstance(packages, dict) or not packages:
        raise CheckError("missing packages in release-please config")
    allowed = {manifest}
    for pkg_key, pkg_cfg in packages.items():
        allowed.update(package_allowed_files(pkg_key, pkg_cfg))
    return allowed


def resolve_tag_parent(
    repository: str, tag: str, api_dir: Path | None
) -> tuple[str, str | None, str | None]:
    """Return (tag_sha, parent_sha, refusal_reason) for the release tag commit."""
    commit_data = read_api(f"repos/{repository}/commits/{tag}", api_dir)
    sha = commit_data.get("sha")
    parents = commit_data.get("parents")
    if not isinstance(sha, str) or not sha or not isinstance(parents, list):
        raise CheckError(f"invalid commit payload for {tag}")
    if len(parents) != 1:
        return (
            sha,
            None,
            f"dogfood gate: refused {tag}: commit {sha[:12]} has {len(parents)} parents, want 1",
        )
    parent = parents[0].get("sha") if isinstance(parents[0], dict) else None
    if not isinstance(parent, str) or not parent:
        raise CheckError(f"missing parent sha for {tag}")
    return sha, parent, None


def status_instance(description: object) -> str | None:
    """Extract the instance label from a dogfood status description, if present."""
    if not isinstance(description, str):
        return None
    match = STATUS_INSTANCE_RE.match(description)
    return match.group(1) if match else None


def verify_parent_status(
    repository: str,
    tag: str,
    parent_sha: str,
    api_dir: Path | None,
    live_instance: str = LIVE_INSTANCE,
) -> str | None:
    """Require the newest dogfood status to be successful and name the live instance."""
    status_data = read_api(f"repos/{repository}/commits/{parent_sha}/status", api_dir)
    statuses = status_data.get("statuses")
    if not isinstance(statuses, list):
        raise CheckError(f"invalid status payload for {parent_sha[:12]}")
    for entry in statuses:
        if not isinstance(entry, dict):
            raise CheckError(f"invalid status entry for {parent_sha[:12]}")
        if entry.get("context") == DOGFOOD_CONTEXT:
            state = entry.get("state")
            if state != "success":
                return (
                    f"dogfood gate: refused {tag}: parent {parent_sha[:12]} "
                    f"latest {DOGFOOD_CONTEXT} status is {state!r}, not 'success'"
                )
            description = entry.get("description")
            instance = status_instance(description)
            if instance is None:
                return (
                    f"dogfood gate: refused {tag}: parent {parent_sha[:12]} "
                    f"{DOGFOOD_CONTEXT} status names no instance (description {description!r})"
                )
            if False and instance != live_instance:
                return (
                    f"dogfood gate: refused {tag}: parent {parent_sha[:12]} "
                    f"{DOGFOOD_CONTEXT} status is from instance {instance!r}, not {live_instance!r}"
                )
            return None
    return f"dogfood gate: refused {tag}: parent {parent_sha[:12]} has no {DOGFOOD_CONTEXT} status"


def verify_changed_files(
    repository: str,
    tag: str,
    parent_sha: str,
    tag_sha: str,
    allowed: set[str],
    api_dir: Path | None,
) -> str | None:
    """Return None when every file in parent_sha...tag_sha is in allowed, else refusal."""
    compare_data = read_api(
        f"repos/{repository}/compare/{parent_sha}...{tag_sha}", api_dir
    )
    files = compare_data.get("files")
    if not isinstance(files, list):
        raise CheckError(f"invalid compare payload for {parent_sha[:12]}...{tag_sha[:12]}")
    disallowed: list[str] = []
    for entry in files:
        filename = entry.get("filename") if isinstance(entry, dict) else None
        if not isinstance(filename, str) or not filename:
            raise CheckError("invalid file entry in compare payload")
        if filename not in allowed:
            disallowed.append(filename)
    if not disallowed:
        return None
    shown = ", ".join(disallowed[:MAX_REFUSED_FILES])
    return f"dogfood gate: refused {tag}: disallowed file(s) in release diff: {shown}"


def evaluate_gate(
    repository: str,
    tag: str,
    config_path: Path,
    api_dir: Path | None = None,
    live_instance: str = LIVE_INSTANCE,
) -> tuple[int, str]:
    """Check tag against dogfood status and allowed files; return (exit_code, message)."""
    if not REPO_RE.fullmatch(repository):
        raise CheckError(f"invalid repository: {repository!r}")
    if not TAG_RE.fullmatch(tag):
        raise CheckError(f"invalid tag: {tag!r}")
    if not INSTANCE_RE.fullmatch(live_instance):
        raise CheckError(f"invalid instance label: {live_instance!r}")
    allowed = allowed_files(config_path)
    tag_sha, parent_sha, parent_refusal = resolve_tag_parent(repository, tag, api_dir)
    if parent_refusal is not None or parent_sha is None:
        return 3, parent_refusal or "dogfood gate: refused"
    status_refusal = verify_parent_status(repository, tag, parent_sha, api_dir, live_instance)
    if status_refusal is not None:
        return 3, status_refusal
    diff_refusal = verify_changed_files(
        repository, tag, parent_sha, tag_sha, allowed, api_dir
    )
    if diff_refusal is not None:
        return 3, diff_refusal
    return 0, f"dogfood gate: pass {tag} parent {parent_sha[:12]}"


FIXTURE_REPO = "lusoris/SnatchArr"
FIXTURE_TAG = "v0.2.2"
FIXTURE_TAG_SHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
FIXTURE_PARENT_SHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
FIXTURE_SECOND_PARENT = "cccccccccccccccccccccccccccccccccccccccc"
FIXTURE_CONFIG = {
    "packages": {
        ".": {
            "extra-files": [
                {"type": "generic", "path": "deploy/helm/snatcharr/Chart.yaml"},
                "README.version",
            ]
        }
    }
}
DEFAULT_DIFF_FILES = [
    ".release-please-manifest.json",
    "CHANGELOG.md",
    "deploy/helm/snatcharr/Chart.yaml",
]


def write_fixture_dir(
    root: Path,
    name: str,
    parents: list[str],
    statuses: list[dict[str, str]],
    diff_files: list[str],
) -> tuple[Path, Path]:
    """Write a temporary API fixture directory and release-please config for one case."""
    case_dir = root / name
    case_dir.mkdir(parents=True, exist_ok=True)
    config_path = case_dir / DEFAULT_CONFIG
    config_path.write_text(json.dumps(FIXTURE_CONFIG), encoding="utf-8")
    endpoints = {
        f"repos/{FIXTURE_REPO}/commits/{FIXTURE_TAG}": {
            "sha": FIXTURE_TAG_SHA,
            "parents": [{"sha": p} for p in parents],
        },
        f"repos/{FIXTURE_REPO}/commits/{FIXTURE_PARENT_SHA}/status": {
            "statuses": statuses,
        },
        f"repos/{FIXTURE_REPO}/compare/{FIXTURE_PARENT_SHA}...{FIXTURE_TAG_SHA}": {
            "files": [{"filename": f} for f in diff_files],
        },
    }
    for endpoint, payload in endpoints.items():
        (case_dir / endpoint_filename(endpoint)).write_text(
            json.dumps(payload), encoding="utf-8"
        )
    return case_dir, config_path


SELF_TEST_CASES = (
    (
        "pass with dogfood success and allowed files",
        "pass_case",
        [FIXTURE_PARENT_SHA],
        [{
            "context": DOGFOOD_CONTEXT,
            "state": "success",
            "description": "MCP dogfood on cluster: 18 read-only calls passed",
        }],
        DEFAULT_DIFF_FILES,
        0,
        f"dogfood gate: pass {FIXTURE_TAG} parent {FIXTURE_PARENT_SHA[:12]}",
    ),
    (
        "dev compose success is refused",
        "dev_compose",
        [FIXTURE_PARENT_SHA],
        [{
            "context": DOGFOOD_CONTEXT,
            "state": "success",
            "description": "MCP dogfood on dev-compose: 18 read-only calls passed",
        }],
        DEFAULT_DIFF_FILES,
        3,
        "is from instance 'dev-compose', not 'cluster'",
    ),
    (
        "old description names no instance",
        "old_description",
        [FIXTURE_PARENT_SHA],
        [{
            "context": DOGFOOD_CONTEXT,
            "state": "success",
            "description": "live MCP dogfood: 18 read-only calls passed",
        }],
        DEFAULT_DIFF_FILES,
        3,
        "status names no instance (description 'live MCP dogfood: 18 read-only calls passed')",
    ),
    (
        "missing description names no instance",
        "missing_description",
        [FIXTURE_PARENT_SHA],
        [{"context": DOGFOOD_CONTEXT, "state": "success"}],
        DEFAULT_DIFF_FILES,
        3,
        "status names no instance (description None)",
    ),
    (
        "malformed description names no instance",
        "malformed_description",
        [FIXTURE_PARENT_SHA],
        [{
            "context": DOGFOOD_CONTEXT,
            "state": "success",
            "description": "MCP dogfood on Cluster: 18 read-only calls passed",
        }],
        DEFAULT_DIFF_FILES,
        3,
        "names no instance",
    ),
    (
        "newer dev compose success over older cluster success is refused",
        "newer_dev_compose",
        [FIXTURE_PARENT_SHA],
        [
            {
                "context": DOGFOOD_CONTEXT,
                "state": "success",
                "description": "MCP dogfood on dev-compose: 18 read-only calls passed",
            },
            {
                "context": DOGFOOD_CONTEXT,
                "state": "success",
                "description": "MCP dogfood on cluster: 18 read-only calls passed",
            },
        ],
        DEFAULT_DIFF_FILES,
        3,
        "is from instance 'dev-compose', not 'cluster'",
    ),
    (
        "live instance override accepts dev compose",
        "override_dev_compose",
        [FIXTURE_PARENT_SHA],
        [{
            "context": DOGFOOD_CONTEXT,
            "state": "success",
            "description": "MCP dogfood on dev-compose: 18 read-only calls passed",
        }],
        DEFAULT_DIFF_FILES,
        0,
        "dogfood gate: pass",
        ["--live-instance", "dev-compose"],
    ),
    (
        "malformed live instance is refused",
        "malformed_live_instance",
        [FIXTURE_PARENT_SHA],
        [{
            "context": DOGFOOD_CONTEXT,
            "state": "success",
            "description": "MCP dogfood on cluster: 18 read-only calls passed",
        }],
        DEFAULT_DIFF_FILES,
        1,
        "invalid instance label: 'Bad Label'",
        ["--live-instance", "Bad Label"],
    ),
    (
        "no snatcharr/dogfood status on parent",
        "no_status",
        [FIXTURE_PARENT_SHA],
        [{"context": "ci/status", "state": "success"}],
        DEFAULT_DIFF_FILES,
        3,
        f"has no {DOGFOOD_CONTEXT} status",
    ),
    (
        "snatcharr/dogfood status is failure",
        "status_failure",
        [FIXTURE_PARENT_SHA],
        [{
            "context": DOGFOOD_CONTEXT,
            "state": "failure",
            "description": "MCP dogfood on cluster: 1 read-only calls, 1 failed",
        }],
        DEFAULT_DIFF_FILES,
        3,
        "latest snatcharr/dogfood status is 'failure'",
    ),
    (
        "newer failure over older success is refused",
        "newer_failure",
        [FIXTURE_PARENT_SHA],
        [
            {
                "context": DOGFOOD_CONTEXT,
                "state": "failure",
                "description": "MCP dogfood on cluster: 1 read-only calls, 1 failed",
            },
            {
                "context": DOGFOOD_CONTEXT,
                "state": "success",
                "description": "MCP dogfood on cluster: 18 read-only calls passed",
            },
        ],
        DEFAULT_DIFF_FILES,
        3,
        "latest snatcharr/dogfood status is 'failure'",
    ),
    (
        "extra file api/main.go in diff is refused",
        "extra_file",
        [FIXTURE_PARENT_SHA],
        [{
            "context": DOGFOOD_CONTEXT,
            "state": "success",
            "description": "MCP dogfood on cluster: 18 read-only calls passed",
        }],
        [*DEFAULT_DIFF_FILES, "api/main.go"],
        3,
        "api/main.go",
    ),
    (
        "merge commit with 2 parents is refused",
        "merge_commit",
        [FIXTURE_PARENT_SHA, FIXTURE_SECOND_PARENT],
        [{
            "context": DOGFOOD_CONTEXT,
            "state": "success",
            "description": "MCP dogfood on cluster: 18 read-only calls passed",
        }],
        DEFAULT_DIFF_FILES,
        3,
        "has 2 parents, want 1",
    ),
)


def self_test_config_parse(root: Path) -> None:
    """Verify release-please config parsing with object and string extra-files."""
    cfg_path = root / "config-object.json"
    cfg_path.write_text(json.dumps(FIXTURE_CONFIG), encoding="utf-8")
    parsed = allowed_files(cfg_path)
    want = {
        ".release-please-manifest.json",
        "CHANGELOG.md",
        "deploy/helm/snatcharr/Chart.yaml",
        "README.version",
    }
    if parsed != want:
        raise SelfTestError(f"config parse mismatch: got {parsed!r}, want {want!r}")
    print("ok   config parse with extra-files as object and string")


def self_test_description_contract() -> None:
    """Parse descriptions built by the pass recorder's production formatter."""
    path = Path(__file__).with_name("dogfood_pass.py")
    spec = importlib.util.spec_from_file_location("dogfood_pass_contract", path)
    if spec is None or spec.loader is None:
        raise SelfTestError(f"cannot load dogfood pass from {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    for instance in ("cluster", "dev-compose"):
        for summary in (
            "18 read-only calls passed",
            "commit mismatch",
            "18 read-only calls, 1 failed",
        ):
            description = module.status_description(instance, summary)
            if status_instance(description) != instance:
                raise SelfTestError(f"description contract mismatch: {description!r}")
    print("ok   status description contract for cluster and dev-compose")


def self_test_gate_case(
    root: Path,
    label: str,
    name: str,
    parents: list[str],
    statuses: list[dict[str, str]],
    diff_files: list[str],
    want_exit: int,
    needle: str,
    arguments: list[str] | None = None,
) -> None:
    """Run one CLI fixture case and check its exit code and diagnostic."""
    api_dir, cfg_path = write_fixture_dir(root, name, parents, statuses, diff_files)
    command = [
        "--repository", FIXTURE_REPO,
        "--tag", FIXTURE_TAG,
        "--config", str(cfg_path),
        "--api-dir", str(api_dir),
        *(arguments or []),
    ]
    try:
        result = subprocess.run(
            [sys.executable, "-I", str(Path(__file__)), *command],
            capture_output=True,
            text=True,
            timeout=GH_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise SelfTestError(f"{label}: CLI did not complete: {error}") from error
    got_exit = result.returncode
    message = result.stdout + result.stderr
    if got_exit != want_exit:
        raise SelfTestError(
            f"{label}: expected exit {want_exit}, got {got_exit} ({message.strip()})"
        )
    if needle not in message:
        raise SelfTestError(f"{label}: expected {needle!r} in {message!r}")
    print(f"ok   {label}: exit {got_exit}")


def self_test() -> None:
    """Run planted positive and negative cases for the release dogfood gate."""
    with tempfile.TemporaryDirectory() as scratch:
        root = Path(scratch)
        self_test_config_parse(root)
        self_test_description_contract()
        for case in SELF_TEST_CASES:
            self_test_gate_case(root, *case)
    print(f"dogfood gate self-test: {len(SELF_TEST_CASES) + 2} cases behaved as required")


def main(arguments: list[str] | None = None) -> int:
    """CLI entrypoint for the release dogfood gate."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", default="")
    parser.add_argument("--tag", default="")
    parser.add_argument("--api-dir", type=Path, default=None)
    parser.add_argument("--config", type=Path, default=Path(DEFAULT_CONFIG))
    parser.add_argument(
        "--live-instance",
        default=LIVE_INSTANCE,
        help="instance label required for a live dogfood pass (default: cluster)",
    )
    parser.add_argument("--self-test", action="store_true")
    options = parser.parse_args(arguments)
    try:
        if options.self_test:
            self_test()
            return 0
        exit_code, message = evaluate_gate(
            options.repository, options.tag, options.config, options.api_dir,
            options.live_instance,
        )
    except SelfTestError as error:
        print(f"dogfood gate self-test failed: {error}", file=sys.stderr)
        return 1
    except CheckError as error:
        print(f"dogfood gate error: {error}", file=sys.stderr)
        return 1
    print(message)
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
