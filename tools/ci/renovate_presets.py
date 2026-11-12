#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Judge the Renovate `extends` closure of `renovate.json` for Docker release-age bypasses.

Presets in `extends` merge before `renovate.json`, so `image_release_age_check.py` covers preset
`minimumReleaseAge` / `minimumReleaseAgeBehaviour` rules, while a preset rule with `enabled: false`
or a lookup redirect/override on a Docker-reachable rule, a Docker-capable preset `customManager`,
a top-level redirect/disable/manager block, or an `ignorePaths` segment matching a pinned or
Docker-managed path would bypass coverage. Built-in presets and Docker-capable manager names come
offline from the digest-pinned `RENOVATE_IMAGE` (`tools/ci/tool-versions.env`); `horistes` is
fetched at a single default-branch commit SHA with a per-call deadline.

Exit 0 = ok, 1 = input error, 3 = refused.
"""

from __future__ import annotations

import argparse
import http.client
import io
import json
import os
import re
import shutil
import subprocess  # nosec B404  # runs docker with a fixed argument list, never a shell
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from collections import Counter, deque
from collections.abc import Callable
from dataclasses import dataclass, field
from pathlib import Path
from typing import Protocol, Self

sys.path.insert(0, str(Path(__file__).resolve().parent))

import image_pin_check
from renovate_managers import (
    is_exact_file_pattern,
    is_non_docker_manager,
    no_duplicate_keys,
    parse_enabled_managers,
    parse_extends,
    parse_str_list,
)
from renovate_regex import ConfigError, RefusedError, ascii_lower, extract_named_groups
from renovate_rules import (
    RULE_REDIRECT_KEYS,
    TOP_REDIRECT_KEYS,
    UNSUPPORTED_OVERRIDES,
    is_age_disabled,
)

HERE = Path(__file__).resolve().parent
DEFAULT_REPO_ROOT = HERE.parents[1]
DEFAULT_TOOL_VERSIONS = HERE / "tool-versions.env"
DUMP_SCRIPT = HERE / "renovate_presets_dump.mjs"

DOCKER_TIMEOUT_SECONDS = 120
HTTP_TIMEOUT_SECONDS = 20
MAX_DUMP_BYTES = 32 << 20
MAX_HTTP_BYTES = 1 << 20
MAX_PRESETS = 4096
MAX_CONFIG_NODES = 1 << 20
MAX_RULE_STEPS = MAX_PRESETS * 8
MAX_ENV_LINES = 1024
MAX_MANAGER_RENAMES = 64
MAX_STDERR_READS = 1 << 16
READ_CHUNK_BYTES = 65536

HORISTES_REF = "github>plurano/horistes"
GITHUB_API_HOST = "api.github.com"
HORISTES_COMMITS_URL = "https://api.github.com/repos/plurano/horistes/commits/HEAD"
HORISTES_CONTENTS_URL = "https://api.github.com/repos/plurano/horistes/contents"

IMAGE_RE = re.compile(r"^[a-z0-9./-]+:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$")
SHA_RE = re.compile(r"^[0-9a-f]{40}$")
LITERAL_MATCHER_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
IGNORE_SEGMENT_RE = re.compile(r"^\*\*/([A-Za-z0-9._-]+)/\*\*$")

NON_DOCKER_DEP_TYPES = frozenset({"github-runner", "php2shell-runtime"})
EXPECTED_DEP_TYPE_EMITTERS: dict[str, frozenset[str]] = {
    "github-runner": frozenset({
        "modules/manager/github-actions/dep-types.js",
        "modules/manager/github-actions/extract.js",
    }),
    "php2shell-runtime": frozenset({
        "config/presets/internal/security.preset.js",
    }),
}

ALLOWED_PRESET_TOP_KEYS = frozenset({
    "$schema",
    "automerge",
    "commitTrailers",
    "customManagers",
    "dependencyDashboard",
    "description",
    "extends",
    "gitIgnoredAuthors",
    "ignorePaths",
    "minimumReleaseAge",
    "nuget",
    "osvVulnerabilityAlerts",
    "overrideDescription",
    "packageRules",
    "postUpdateOptions",
    "prHeader",
    "semanticCommits",
    "vulnerabilityAlerts",
})
ALLOWED_DASHBOARD_KEYS = frozenset({"enabled"})
ALLOWED_NUGET_KEYS = frozenset({"ignorePaths"})
ALLOWED_PRESET_VULN_KEYS = frozenset({
    "addLabels",
    "enabled",
    "groupName",
    "labels",
    "minimumReleaseAge",
    "minimumReleaseAgeBehaviour",
    "rebaseWhen",
    "schedule",
})

MEASURED_RULE_KEYS = frozenset({
    "allowedVersions",
    "changelogUrl",
    "commitMessageExtra",
    "commitMessageTopic",
    "description",
    "enabled",
    "extends",
    "extractVersion",
    "groupName",
    "groupSlug",
    "isVulnerabilityAlert",
    "matchCurrentValue",
    "matchCurrentVersion",
    "matchDatasources",
    "matchDepNames",
    "matchDepTypes",
    "matchJsonata",
    "matchManagers",
    "matchPackageNames",
    "matchSourceUrls",
    "matchUpdateTypes",
    "minimumReleaseAge",
    "minimumReleaseAgeBehaviour",
    "pinDigests",
    "prBodyColumns",
    "prBodyDefinitions",
    "prBodyNotes",
    "replacementName",
    "replacementNameTemplate",
    "replacementVersion",
    "schedule",
    "semanticCommitType",
    "versionCompatibility",
    "versioning",
})
FORBIDDEN_DOCKER_RULE_KEYS: tuple[str, ...] = (
    "overridePackageName",
    *sorted(UNSUPPORTED_OVERRIDES),
    *RULE_REDIRECT_KEYS,
)
KNOWN_RULE_KEYS = MEASURED_RULE_KEYS | frozenset(FORBIDDEN_DOCKER_RULE_KEYS)
EXCLUSION_MATCHER_KEYS = ("matchDatasources", "matchManagers", "matchDepTypes")
SKIP_WALK_KEYS = frozenset({"content", "onboardingConfig"})

OpenerFunc = Callable[[urllib.request.Request, float], bytes]


@dataclass(frozen=True)
class DumpData:
    """Validated built-in preset groups, Docker-capable managers and emitter metadata."""

    renovate_version: str
    groups: dict[str, dict[str, dict[str, object]]]
    docker_capable_managers: frozenset[str]
    manager_renames: dict[str, dict[str, str]]


@dataclass(frozen=True)
class RepoContext:
    """Validated `renovate.json` state and tracked image-pin paths for closure judgement."""

    top_extends: tuple[str, ...]
    repo_rule_extends: tuple[tuple[int, tuple[str, ...]], ...]
    enabled_managers: frozenset[str] | None
    has_top_minimum_release_age: bool
    repo_docker_deptypes: frozenset[str]
    repo_has_dynamic_docker_deptype: bool
    repo_docker_manager_patterns: tuple[str, ...]
    pin_paths: tuple[str, ...]


@dataclass(frozen=True)
class ResolvedRule:
    """Merged rule dictionary and union of exclusion-matcher values across its `extends` chain."""

    merged: dict[str, object]
    union_matchers: dict[str, tuple[str, ...]]


@dataclass
class ClosureState:
    """Mutable counters and memoization state for an iterative closure walk."""

    dump: DumpData
    horistes: dict[str, object]
    repo: RepoContext
    reached_presets: set[str] = field(default_factory=set)
    visited_top: set[str] = field(default_factory=set)
    resolved_rule_presets: dict[str, ResolvedRule] = field(default_factory=dict)
    rules_judged: int = 0
    excluded_reasons: Counter[str] = field(default_factory=Counter)


class Readable(Protocol):
    """Minimal byte-stream interface used by bounded response readers."""

    def read(self, size: int = -1) -> bytes:
        """Read up to SIZE bytes."""


class HttpsRedirectHandler(urllib.request.HTTPRedirectHandler):
    """Allow only HTTPS redirects and strip `Authorization` on any cross-host redirect."""

    def redirect_request(  # type: ignore[override]
        self,
        req: urllib.request.Request,
        fp: object,
        code: int,
        msg: str,
        headers: object,
        newurl: str,
    ) -> urllib.request.Request | None:
        """Validate redirect scheme and drop credentials when leaving `api.github.com`."""
        parsed = urllib.parse.urlsplit(newurl)
        if parsed.scheme != "https" or not parsed.hostname:
            raise urllib.error.URLError(f"refused non-https redirect to {parsed.scheme!r}")
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)  # type: ignore[arg-type]
        if redirected is not None and parsed.hostname.lower() != GITHUB_API_HOST:
            redirected.headers.pop("Authorization", None)
            redirected.unredirected_hdrs.pop("Authorization", None)
        return redirected


def read_bounded_file(path: Path, cap: int, label: str) -> bytes:
    """Read regular file PATH up to CAP bytes, raising ConfigError when missing or oversized."""
    try:
        with path.open("rb") as handle:
            raw = handle.read(cap + 1)
    except OSError as exc:
        raise ConfigError(f"{label}: {exc}") from exc
    if b"\0" in raw:
        raise ConfigError(f"{label}: binary content is not allowed")
    if len(raw) > cap:
        raise ConfigError(f"{label}: {len(raw)} bytes exceeds cap of {cap}")
    return raw


def load_json_object(raw: bytes, label: str) -> dict[str, object]:
    """Decode UTF-8 JSON bytes RAW with duplicate-key refusal and require a top-level object."""
    try:
        text = raw.decode("utf-8")
        parsed = json.loads(text, object_pairs_hook=no_duplicate_keys)
    except (UnicodeDecodeError, ValueError) as exc:
        raise ConfigError(f"{label}: invalid JSON: {exc}") from exc
    if not isinstance(parsed, dict):
        raise ConfigError(f"{label}: top-level JSON value must be an object")
    return parsed


def read_renovate_image(tool_versions: Path) -> str:
    """Read and validate `RENOVATE_IMAGE` from `tools/ci/tool-versions.env`."""
    lines = read_bounded_file(tool_versions, MAX_HTTP_BYTES, str(tool_versions)).decode(
        "utf-8"
    ).splitlines()
    if len(lines) > MAX_ENV_LINES:
        raise ConfigError(f"{tool_versions}: exceeds {MAX_ENV_LINES} lines")
    image: str | None = None
    for line in lines:
        stripped = line.strip()
        if stripped and not stripped.startswith("#"):
            key, sep, value = stripped.partition("=")
            if sep and key == "RENOVATE_IMAGE":
                image = value.strip()
    if not image or IMAGE_RE.fullmatch(image) is None:
        raise RefusedError(
            f"{tool_versions}: RENOVATE_IMAGE={image!r} is not pinned by version and sha256 digest"
        )
    return image


def drain_process_stderr(
    proc: subprocess.Popen[bytes], err_chunks: list[bytes], overflow: threading.Event
) -> None:
    """Drain bounded child stderr while retaining only its first READ_CHUNK_BYTES."""
    if proc.stderr is None:
        return
    kept = 0
    for _ in range(MAX_STDERR_READS):
        chunk = proc.stderr.read(READ_CHUNK_BYTES)
        if not chunk:
            return
        if kept < READ_CHUNK_BYTES:
            saved = chunk[: READ_CHUNK_BYTES - kept]
            err_chunks.append(saved)
            kept += len(saved)
    overflow.set()
    proc.kill()


def run_docker_dump(image: str, dump_script: Path) -> bytes:
    """Run `renovate_presets_dump.mjs` offline inside IMAGE with a 120 s / 32 MiB bound."""
    docker = shutil.which("docker")
    if docker is None or not dump_script.is_file():
        raise ConfigError(f"missing docker executable or dump script ({dump_script})")
    cmd = [
        docker, "run", "--rm", "--network", "none", "--read-only",
        "-v", f"{dump_script.resolve()}:/probe/dump.mjs:ro",
        "--entrypoint", "node", image, "/probe/dump.mjs",
    ]
    timed_out = threading.Event()
    stderr_overflow = threading.Event()
    err_chunks: list[bytes] = []
    try:
        proc = subprocess.Popen(  # nosec B603  # fixed docker argv list, no shell
            cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE
        )
    except OSError as exc:
        raise ConfigError(f"docker run failed to start: {exc}") from exc

    def on_timeout() -> None:
        """Kill the docker child if DOCKER_TIMEOUT_SECONDS elapses."""
        timed_out.set()
        proc.kill()

    def drain_stderr() -> None:
        """Read bounded stderr so the child cannot block on pipe buffer space."""
        drain_process_stderr(proc, err_chunks, stderr_overflow)

    timer = threading.Timer(DOCKER_TIMEOUT_SECONDS, on_timeout)
    err_thread = threading.Thread(target=drain_stderr, daemon=True)
    timer.start()
    err_thread.start()
    try:
        out = proc.stdout.read(MAX_DUMP_BYTES + 1) if proc.stdout is not None else b""
        if len(out) > MAX_DUMP_BYTES:
            proc.kill()
            raise ConfigError(f"docker dump stdout exceeds {MAX_DUMP_BYTES} bytes")
        rc = proc.wait()
        err_thread.join(timeout=5)
    finally:
        timer.cancel()
    if timed_out.is_set():
        raise ConfigError(f"docker dump timed out after {DOCKER_TIMEOUT_SECONDS}s")
    if stderr_overflow.is_set():
        raise ConfigError(
            f"docker dump stderr exceeded bounded drain of {MAX_STDERR_READS} reads"
        )
    if rc != 0:
        detail = b"".join(err_chunks).decode("utf-8", errors="replace").strip()
        raise ConfigError(f"docker dump exited {rc}: {detail}")
    return out


def read_with_deadline(
    resp: Readable,
    limit: int,
    timeout: float,
    clock: Callable[[], float] = time.monotonic,
) -> bytes:
    """Read at most LIMIT + 1 bytes from RESP within one total TIMEOUT."""
    deadline = clock() + timeout
    chunks: list[bytes] = []
    remaining = limit + 1
    max_reads = limit // READ_CHUNK_BYTES + 2
    for _ in range(max_reads):
        if clock() > deadline:
            raise TimeoutError(f"response read exceeded {timeout}s")
        chunk = resp.read(min(READ_CHUNK_BYTES, remaining))
        if clock() > deadline:
            raise TimeoutError(f"response read exceeded {timeout}s")
        if not chunk:
            return b"".join(chunks)
        saved = chunk[:remaining]
        chunks.append(saved)
        remaining -= len(saved)
        if remaining == 0:
            return b"".join(chunks)
    raise TimeoutError(f"response read exceeded {max_reads} reads")


def open_https_response(
    opener: urllib.request.OpenerDirector,
    req: urllib.request.Request,
    timeout: float,
    bodies: list[bytes],
    errors: list[Exception],
) -> None:
    """Open REQ and read its capped body, recording one result or the original exception."""
    try:
        with opener.open(req, timeout=timeout) as resp:  # nosec B310  # fixed https://api.github.com URL verified above; redirects restricted to https
            bodies.append(read_with_deadline(resp, MAX_HTTP_BYTES, timeout))
    except (OSError, http.client.HTTPException, ValueError) as exc:  # what urllib raises; default_https_opener re-raises it unchanged
        errors.append(exc)


def default_https_opener(
    req: urllib.request.Request,
    timeout: float,
    build_opener: Callable[..., urllib.request.OpenerDirector] = urllib.request.build_opener,
) -> bytes:
    """Execute HTTPS request REQ against `api.github.com` and read up to MAX_HTTP_BYTES + 1."""
    parsed = urllib.parse.urlsplit(req.full_url)
    if parsed.scheme != "https" or parsed.hostname != GITHUB_API_HOST:
        raise ConfigError(f"refused non-GitHub-API URL: {req.full_url!r}")
    opener = build_opener(HttpsRedirectHandler())
    bodies: list[bytes] = []
    errors: list[Exception] = []
    worker = threading.Thread(
        target=open_https_response,
        args=(opener, req, timeout, bodies, errors),
        daemon=True,
    )
    worker.start()
    worker.join(timeout=timeout)
    if worker.is_alive():
        raise TimeoutError(f"response read exceeded {timeout}s")
    if errors:
        raise errors[0]
    if not bodies:
        raise ConfigError("HTTPS response worker ended without a body or a urllib error; its traceback is on stderr")
    return bodies[0]


def github_api_get(
    url: str, accept: str, token: str | None, opener: OpenerFunc
) -> bytes:
    """GET one `https://api.github.com` URL with deadline, byte cap and scoped bearer header."""
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != "https" or parsed.hostname != GITHUB_API_HOST:
        raise ConfigError(f"refused URL outside https://{GITHUB_API_HOST}: {url!r}")
    headers = {
        "Accept": accept,
        "User-Agent": "snatcharr-renovate-presets",
    }
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, headers=headers, method="GET")
    data = opener(req, HTTP_TIMEOUT_SECONDS)
    if len(data) > MAX_HTTP_BYTES:
        raise ConfigError(f"{url}: response exceeds {MAX_HTTP_BYTES} bytes")
    return data


def format_net_exc(exc: BaseException) -> str:
    """Format a network/HTTP exception without exposing headers or request metadata."""
    return f"HTTP {exc.code}" if isinstance(exc, urllib.error.HTTPError) else str(exc)


def fetch_horistes_head(token: str | None, opener: OpenerFunc) -> str:
    """Resolve `plurano/horistes` default-branch HEAD commit SHA."""
    try:
        raw_sha = github_api_get(
            HORISTES_COMMITS_URL, "application/vnd.github.sha", token, opener
        )
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise ConfigError(f"horistes HEAD lookup failed: {format_net_exc(exc)}") from exc
    sha = raw_sha.decode("utf-8", errors="replace").strip()
    if SHA_RE.fullmatch(sha) is None:
        raise ConfigError(f"horistes HEAD returned non-SHA {sha!r}")
    return sha


def fetch_horistes(
    token: str | None = None, opener: OpenerFunc = default_https_opener
) -> tuple[str, dict[str, object]]:
    """Resolve `plurano/horistes` HEAD SHA, then read `default.json` (404 -> `renovate.json`)."""
    sha = fetch_horistes_head(token, opener)
    default_url = f"{HORISTES_CONTENTS_URL}/default.json?ref={sha}"
    fallback_url = f"{HORISTES_CONTENTS_URL}/renovate.json?ref={sha}"
    try:
        raw_body = github_api_get(
            default_url, "application/vnd.github.raw+json", token, opener
        )
    except urllib.error.HTTPError as exc:
        code = exc.code
        exc.close()
        if code != 404:
            raise ConfigError(f"horistes default.json failed: HTTP {code}") from exc
        try:
            raw_body = github_api_get(
                fallback_url, "application/vnd.github.raw+json", token, opener
            )
        except (urllib.error.URLError, TimeoutError, OSError) as exc2:
            raise ConfigError(
                f"horistes renovate.json fallback failed: {format_net_exc(exc2)}"
            ) from exc2
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise ConfigError(f"horistes default.json failed: {exc}") from exc

    return sha, load_json_object(raw_body, HORISTES_REF)


def is_str_list(value: object) -> bool:
    """Report whether VALUE is a `list` whose elements are all `str`."""
    if not isinstance(value, list):
        return False
    for item in value:
        if not isinstance(item, str):
            return False
    return True


def validate_emitters(raw_emitters: object) -> None:
    """Verify `depTypeEmitters` matches the measured Renovate dist emitter sets (rule 3g)."""
    if not isinstance(raw_emitters, dict):
        raise ConfigError("dump: depTypeEmitters must be an object")
    if set(raw_emitters.keys()) != set(EXPECTED_DEP_TYPE_EMITTERS.keys()):
        raise RefusedError(
            f"dump depTypeEmitters keys {sorted(raw_emitters.keys())} != "
            f"{sorted(EXPECTED_DEP_TYPE_EMITTERS.keys())}"
        )
    for dep_type, expected in sorted(EXPECTED_DEP_TYPE_EMITTERS.items()):
        got_raw = raw_emitters[dep_type]
        if not is_str_list(got_raw):
            raise ConfigError(f"dump: depTypeEmitters[{dep_type!r}] must be a list of strings")
        got = frozenset(got_raw)  # type: ignore[arg-type]
        if got != expected:
            raise RefusedError(
                f"dump depTypeEmitters[{dep_type!r}] mismatch: got {sorted(got)}, "
                f"expected {sorted(expected)}"
            )


def parse_group_presets(
    group_name: str, raw_presets: object, counter: list[int]
) -> dict[str, dict[str, object]]:
    """Validate one `{preset_name: dict}` map inside `dump.groups`."""
    if not isinstance(raw_presets, dict):
        raise ConfigError("dump: each groups entry must map string to object")
    group_dict: dict[str, dict[str, object]] = {}
    for preset_name, body in raw_presets.items():
        counter[0] += 1
        if counter[0] > MAX_CONFIG_NODES:
            raise ConfigError("dump: preset count exceeds node cap")
        if not isinstance(preset_name, str) or not isinstance(body, dict):
            raise ConfigError(f"dump: preset {group_name}:{preset_name} must be an object")
        group_dict[preset_name] = body
    return group_dict


def parse_groups_map(raw_groups: object) -> dict[str, dict[str, dict[str, object]]]:
    """Validate `dump.groups` (`{group: {preset: object}}`)."""
    if not isinstance(raw_groups, dict):
        raise ConfigError("dump: groups must be an object")
    groups: dict[str, dict[str, dict[str, object]]] = {}
    counter = [0]
    for group_name, raw_presets in raw_groups.items():
        if not isinstance(group_name, str):
            raise ConfigError("dump: group key must be a string")
        groups[group_name] = parse_group_presets(group_name, raw_presets, counter)
    return groups


def parse_docker_managers(raw_mgrs: object, label: str) -> frozenset[str]:
    """Validate `dump.dockerManagers` and add custom manager names."""
    if not is_str_list(raw_mgrs) or not raw_mgrs:
        raise ConfigError(f"{label}: dockerManagers must be a non-empty list of strings")
    lowered: set[str] = {"custom.regex", "custom.jsonata"}
    for mgr in raw_mgrs:  # type: ignore[union-attr]
        if not mgr:
            raise ConfigError(f"{label}: dockerManagers contains an empty name")
        lowered.add(ascii_lower(mgr))
    return frozenset(lowered)


def parse_manager_rename_map(raw_map: object, field: str, label: str) -> dict[str, str]:
    """Validate one bounded manager rename map from the Renovate dump."""
    if not isinstance(raw_map, dict):
        raise ConfigError(f"{label}: managerRenames[{field!r}] must be an object")
    if len(raw_map) > MAX_MANAGER_RENAMES:
        raise ConfigError(
            f"{label}: managerRenames[{field!r}] exceeds {MAX_MANAGER_RENAMES} entries"
        )
    renames: dict[str, str] = {}
    for old, new in raw_map.items():
        if (
            not isinstance(old, str)
            or not isinstance(new, str)
            or LITERAL_MATCHER_RE.fullmatch(old) is None
            or LITERAL_MATCHER_RE.fullmatch(new) is None
        ):
            raise ConfigError(
                f"{label}: managerRenames[{field!r}] has an invalid manager name"
            )
        renames[old] = new
    return renames


def parse_manager_renames(raw_renames: object, label: str) -> dict[str, dict[str, str]]:
    """Validate the matchManagers and enabledManagers migration maps."""
    if not isinstance(raw_renames, dict):
        raise ConfigError(f"{label}: managerRenames must be an object")
    expected = {"matchManagers", "enabledManagers"}
    if set(raw_renames) != expected:
        raise ConfigError(f"{label}: managerRenames must contain {sorted(expected)}")
    return {
        field: parse_manager_rename_map(raw_renames[field], field, label)
        for field in sorted(expected)
    }


def parse_dump(raw: bytes, label: str) -> DumpData:
    """Validate JSON bytes from `renovate_presets_dump.mjs` or `--dump`."""
    data = load_json_object(raw, label)
    version = data.get("renovateVersion")
    if not isinstance(version, str) or not version:
        raise ConfigError(f"{label}: renovateVersion must be a non-empty string")
    capable = parse_docker_managers(data.get("dockerManagers"), label)
    validate_emitters(data.get("depTypeEmitters"))
    groups = parse_groups_map(data.get("groups"))
    manager_renames = parse_manager_renames(data.get("managerRenames"), label)
    return DumpData(
        renovate_version=version,
        groups=groups,
        docker_capable_managers=capable,
        manager_renames=manager_renames,
    )


def inspect_dict_children(
    node: dict[str, object],
    path: str,
    label: str,
    *,
    allow_rule_extends: bool,
) -> list[tuple[object, str]]:
    """Validate keys of one dict node for `ignorePresets` / `extends` and return child nodes."""
    children: list[tuple[object, str]] = []
    for key, val in node.items():
        child_path = f"{path}.{key}" if path else key
        if key == "ignorePresets":
            raise RefusedError(f"{label}: key 'ignorePresets' at {child_path} is refused")
        if key == "extends":
            is_rule_ext = allow_rule_extends and child_path == "packageRules[*].extends"
            if child_path != "extends" and not is_rule_ext:
                raise RefusedError(
                    f"{label}: key 'extends' at {child_path} is outside allowed contexts"
                )
            continue
        if key not in SKIP_WALK_KEYS:
            children.append((val, child_path))
    return children


def check_nested_directives(
    root_obj: dict[str, object], label: str, *, allow_rule_extends: bool
) -> None:
    """Iteratively refuse `ignorePresets` anywhere and `extends` outside modeled positions."""
    stack: list[tuple[object, str]] = [(root_obj, "")]
    visited = 0
    while stack:
        node, path = stack.pop()
        visited += 1
        if visited > MAX_CONFIG_NODES:
            raise ConfigError(f"{label}: JSON structure exceeds {MAX_CONFIG_NODES} nodes")
        if isinstance(node, dict):
            stack.extend(
                inspect_dict_children(node, path, label, allow_rule_extends=allow_rule_extends)
            )
        elif isinstance(node, list):
            elem_path = f"{path}[*]"
            for item in node:
                stack.append((item, elem_path))


def manager_named_groups(mgr: dict[str, object], where: str) -> set[str]:
    """Extract `matchStrings` named groups on `customType == 'regex'` customManager MGR."""
    custom_type = mgr.get("customType", "regex")
    if not isinstance(custom_type, str):
        raise ConfigError(f"{where}: customType must be a string")
    match_strings = parse_str_list(mgr, "matchStrings", where) if "matchStrings" in mgr else []
    return extract_named_groups(match_strings, where) if custom_type == "regex" else set()


def classify_manager_deptype(
    mgr: dict[str, object], groups: set[str], where: str
) -> tuple[str | None, bool]:
    """Return `(static_deptype_lower, is_dynamic)` for a repo Docker-capable customManager."""
    dynamic = "depType" in groups
    raw_dt = mgr.get("depTypeTemplate")
    if raw_dt is None:
        return None, dynamic
    if not isinstance(raw_dt, str):
        raise ConfigError(f"renovate.json: {where}.depTypeTemplate must be a string")
    cleaned = raw_dt.strip()
    if "{{" in cleaned or not cleaned:
        return None, True
    return ascii_lower(cleaned), dynamic


def inspect_repo_custom_managers(
    repo_cfg: dict[str, object],
) -> tuple[frozenset[str], bool, tuple[str, ...]]:
    """Extract Docker `depTypeTemplate` usage and `managerFilePatterns` from repo customManagers."""
    raw_mgrs = repo_cfg.get("customManagers", [])
    if not isinstance(raw_mgrs, list):
        raise ConfigError("renovate.json: customManagers must be a list")
    deptypes: set[str] = set()
    dynamic_deptype = False
    file_patterns: list[str] = []
    for idx, mgr in enumerate(raw_mgrs):
        where = f"customManagers[{idx}]"
        if not isinstance(mgr, dict):
            raise ConfigError(f"renovate.json: {where} must be an object")
        groups = manager_named_groups(mgr, f"renovate.json {where}")
        if is_non_docker_manager(mgr, groups):
            continue
        file_patterns.extend(parse_str_list(mgr, "managerFilePatterns", where))
        static_dt, is_dyn = classify_manager_deptype(mgr, groups, where)
        dynamic_deptype = dynamic_deptype or is_dyn
        if static_dt is not None:
            deptypes.add(static_dt)
    return frozenset(deptypes), dynamic_deptype, tuple(file_patterns)


def extract_repo_rule_extends(
    repo_cfg: dict[str, object],
) -> tuple[tuple[int, tuple[str, ...]], ...]:
    """Collect any `packageRules[i].extends` entries present on `renovate.json` itself."""
    raw_rules = repo_cfg.get("packageRules", [])
    if not isinstance(raw_rules, list):
        raise ConfigError("renovate.json: packageRules must be a list")
    collected: list[tuple[int, tuple[str, ...]]] = []
    for idx, rule in enumerate(raw_rules):
        if not isinstance(rule, dict):
            raise ConfigError(f"renovate.json: packageRules[{idx}] must be an object")
        exts = parse_extends(rule)
        if exts:
            collected.append((idx, exts))
    return tuple(collected)


def load_non_record_pin_paths(repo_root: Path) -> tuple[str, ...]:
    """Return sorted relative file paths holding non-record literal image pins in REPO_ROOT."""
    pins_by_key = image_pin_check.load_pins(repo_root)
    paths: set[str] = set()
    for (_canonical, _tag, is_record), digests in pins_by_key.items():
        if is_record:
            continue
        for locs in digests.values():
            for loc in locs:
                paths.add(loc.rsplit(":", 1)[0])
    return tuple(sorted(paths))


def load_repo_context(
    repo_root: Path, manager_renames: dict[str, dict[str, str]]
) -> RepoContext:
    """Load `renovate.json` and tracked non-record image pin paths from REPO_ROOT."""
    cfg_path = repo_root / "renovate.json"
    repo_cfg = load_json_object(
        read_bounded_file(cfg_path, MAX_HTTP_BYTES, str(cfg_path)), str(cfg_path)
    )
    check_nested_directives(repo_cfg, "renovate.json", allow_rule_extends=True)
    top_extends = parse_extends(repo_cfg)
    rule_extends = extract_repo_rule_extends(repo_cfg)
    raw_enabled = parse_enabled_managers(repo_cfg)
    enabled_renames = manager_renames["enabledManagers"]
    enabled = (
        None
        if raw_enabled is None
        else frozenset(ascii_lower(enabled_renames.get(m, m)) for m in raw_enabled)
    )
    deptypes, dynamic_dt, mgr_patterns = inspect_repo_custom_managers(repo_cfg)
    pin_paths = load_non_record_pin_paths(repo_root)
    return RepoContext(
        top_extends=top_extends,
        repo_rule_extends=rule_extends,
        enabled_managers=enabled,
        has_top_minimum_release_age="minimumReleaseAge" in repo_cfg,
        repo_docker_deptypes=deptypes,
        repo_has_dynamic_docker_deptype=dynamic_dt,
        repo_docker_manager_patterns=mgr_patterns,
        pin_paths=pin_paths,
    )


def lookup_internal_preset(
    raw_ref: str, groups: dict[str, dict[str, dict[str, object]]], where: str
) -> tuple[str, dict[str, object]]:
    """Resolve `group:name` or `:name` against `dump.groups`."""
    if ":" not in raw_ref:
        raise RefusedError(f"{where}: preset {raw_ref!r} must use group:name or :name")
    raw_group, _, name = raw_ref.partition(":")
    group = raw_group or "default"
    if not name or ":" in name:
        raise RefusedError(f"{where}: malformed preset reference {raw_ref!r}")
    group_map = groups.get(group)
    if group_map is None or name not in group_map:
        raise RefusedError(f"{where}: unresolved preset {raw_ref!r}")
    return f"{group}:{name}", group_map[name]


def resolve_preset_ref(
    raw_ref: str, state: ClosureState, where: str
) -> tuple[str, dict[str, object]]:
    """Validate preset reference RAW_REF and resolve its `(canonical_ref, body)`."""
    if not raw_ref or raw_ref != raw_ref.strip():
        raise RefusedError(f"{where}: invalid whitespace or empty preset {raw_ref!r}")
    if any(tok in raw_ref for tok in ("{{", "(", ")")):
        raise RefusedError(f"{where}: templated or parameterized preset {raw_ref!r} is refused")
    if raw_ref == HORISTES_REF:
        canonical, body = HORISTES_REF, state.horistes
    elif ">" in raw_ref or "://" in raw_ref:
        raise RefusedError(f"{where}: non-built-in preset {raw_ref!r} is refused")
    else:
        canonical, body = lookup_internal_preset(raw_ref, state.dump.groups, where)

    state.reached_presets.add(canonical)
    if len(state.reached_presets) > MAX_PRESETS:
        raise ConfigError(f"preset closure exceeds MAX_PRESETS ({MAX_PRESETS})")
    return canonical, body


def validate_rule_keys(where: str, obj: dict[str, object]) -> None:
    """Refuse unknown keys on a rule-context preset or `packageRules[i]` object (rule 3a)."""
    for key in obj:
        if key not in KNOWN_RULE_KEYS:
            raise RefusedError(f"{where}: unknown rule key {key!r}")
    if "enabled" in obj and not isinstance(obj["enabled"], bool):
        raise ConfigError(f"{where}: key 'enabled' must be a boolean")


def append_unique_strings(bucket: list[str], items: object) -> None:
    """Append unique `str` entries from list ITEMS into BUCKET."""
    if not isinstance(items, list) and not isinstance(items, tuple):
        return
    for item in items:
        if isinstance(item, str) and item not in bucket:
            bucket.append(item)


def merge_rule_parts(
    parts: list[tuple[dict[str, object], dict[str, tuple[str, ...]]]]
) -> ResolvedRule:
    """Merge ordered `(rule_dict, matchers)` parts into one `ResolvedRule`."""
    merged: dict[str, object] = {}
    unions: dict[str, list[str]] = {}
    for part_dict, part_unions in parts:
        for key, val in part_dict.items():
            if key == "extends":
                continue
            merged[key] = val
            if key in EXCLUSION_MATCHER_KEYS:
                append_unique_strings(unions.setdefault(key, []), val)
        for mkey, mvals in part_unions.items():
            append_unique_strings(unions.setdefault(mkey, []), mvals)
    return ResolvedRule(
        merged=merged,
        union_matchers={k: tuple(v) for k, v in unions.items()},
    )


def expand_rule_preset_children(
    canon: str,
    body: dict[str, object],
    in_progress: set[str],
    state: ClosureState,
) -> tuple[tuple[str, ...], list[tuple[str, dict[str, object]]]]:
    """Validate rule-preset BODY and resolve its non-cyclic `extends` children."""
    check_nested_directives(body, canon, allow_rule_extends=False)
    validate_rule_keys(canon, body)
    resolved_children: list[str] = []
    pending: list[tuple[str, dict[str, object]]] = []
    for ext_ref in parse_extends(body):
        c_canon, c_body = resolve_preset_ref(ext_ref, state, f"{canon} extends")
        if c_canon in in_progress:
            continue
        resolved_children.append(c_canon)
        if c_canon not in state.resolved_rule_presets:
            pending.append((c_canon, c_body))
    return tuple(resolved_children), pending


def resolve_rule_preset(raw_ref: str, state: ClosureState, where: str) -> ResolvedRule:
    """Iteratively resolve rule-context preset RAW_REF with cycle guard and memoization."""
    root_canon, root_body = resolve_preset_ref(raw_ref, state, where)
    if root_canon in state.resolved_rule_presets:
        return state.resolved_rule_presets[root_canon]

    stack: list[tuple[str, dict[str, object], bool, tuple[str, ...]]] = [
        (root_canon, root_body, False, ())
    ]
    in_progress: set[str] = set()
    steps = 0
    while stack:
        steps += 1
        if steps > MAX_RULE_STEPS:
            raise ConfigError(f"rule preset expansion exceeded {MAX_RULE_STEPS} steps")
        canon, body, expanded, child_canons = stack.pop()
        if canon in state.resolved_rule_presets:
            continue
        if not expanded:
            in_progress.add(canon)
            children, pending = expand_rule_preset_children(canon, body, in_progress, state)
            stack.append((canon, body, True, children))
            for c_canon, c_body in reversed(pending):
                stack.append((c_canon, c_body, False, ()))
        else:
            in_progress.discard(canon)
            parts = [
                (
                    state.resolved_rule_presets[c].merged,
                    state.resolved_rule_presets[c].union_matchers,
                )
                for c in child_canons
            ]
            parts.append((body, {}))
            state.resolved_rule_presets[canon] = merge_rule_parts(parts)

    return state.resolved_rule_presets[root_canon]


def literal_matcher_values(
    rule: dict[str, object], union_matchers: dict[str, tuple[str, ...]], key: str
) -> list[str] | None:
    """Return literal matcher entries for KEY on RULE, or `None` when absent/empty/non-literal."""
    raw = rule.get(key)
    if not isinstance(raw, list) or not raw:
        return None
    combined: list[str] = []
    for item in (*raw, *union_matchers.get(key, ())):
        if not isinstance(item, str) or LITERAL_MATCHER_RE.fullmatch(item) is None:
            return None
        if item not in combined:
            combined.append(item)
    return combined


def is_manager_non_docker_or_disabled(mgr: str, state: ClosureState) -> bool:
    """Report whether manager MGR is either non-Docker-capable or disabled in `renovate.json`."""
    lowered = ascii_lower(mgr)
    if lowered not in state.dump.docker_capable_managers:
        return True
    enabled = state.repo.enabled_managers
    return enabled is not None and lowered not in enabled


def is_deptype_non_docker(dep_type: str, repo: RepoContext) -> bool:
    """Report whether DEP_TYPE is a proven non-Docker depType unused by repo Docker managers."""
    if repo.repo_has_dynamic_docker_deptype or dep_type not in NON_DOCKER_DEP_TYPES:
        return False
    return ascii_lower(dep_type) not in repo.repo_docker_deptypes


def rule_exclusion_reason(
    rule: dict[str, object],
    union_matchers: dict[str, tuple[str, ...]],
    state: ClosureState,
) -> str | None:
    """Return the Docker-exclusion reason (`datasource`, `manager`, `depType`) or `None` (3d)."""
    datasources = literal_matcher_values(rule, union_matchers, "matchDatasources")
    if datasources is not None and all(ascii_lower(ds) != "docker" for ds in datasources):
        return "datasource"

    managers = literal_matcher_values(rule, union_matchers, "matchManagers")
    if managers is not None:
        renames = state.dump.manager_renames["matchManagers"]
        managers = [renames.get(manager, manager) for manager in managers]
    if managers is not None and all(
        is_manager_non_docker_or_disabled(m, state) for m in managers
    ):
        return "manager"

    dep_types = literal_matcher_values(rule, union_matchers, "matchDepTypes")
    if dep_types is not None and all(is_deptype_non_docker(dt, state.repo) for dt in dep_types):
        return "depType"

    return None


def judge_preset_rule(
    preset_ref: str, rule_index: int, raw_rule: object, state: ClosureState
) -> None:
    """Resolve nested `extends` on `packageRules[rule_index]` and enforce rules 3a, 3c and 3d."""
    where = f"{preset_ref} packageRules[{rule_index}]"
    if not isinstance(raw_rule, dict):
        raise ConfigError(f"{where}: rule must be an object")
    validate_rule_keys(where, raw_rule)
    parts: list[tuple[dict[str, object], dict[str, tuple[str, ...]]]] = []
    for ext_ref in parse_extends(raw_rule):
        resolved = resolve_rule_preset(ext_ref, state, f"{where} extends")
        parts.append((resolved.merged, resolved.union_matchers))
    parts.append((raw_rule, {}))
    final_rule = merge_rule_parts(parts)
    merged = final_rule.merged
    state.rules_judged += 1

    has_disable = merged.get("enabled") is False
    bad_keys = [k for k in FORBIDDEN_DOCKER_RULE_KEYS if k in merged]
    if not has_disable and not bad_keys:
        return

    reason = rule_exclusion_reason(merged, final_rule.union_matchers, state)
    if reason is not None:
        state.excluded_reasons[reason] += 1
        return
    if has_disable:
        raise RefusedError(f"{where}: key 'enabled' is false on a Docker-reachable rule")
    raise RefusedError(f"{where}: key {bad_keys[0]!r} is not allowed on a Docker-reachable rule")


def judge_preset_custom_managers(preset_ref: str, raw_managers: object) -> None:
    """Refuse any preset `customManager` unless `is_non_docker_manager` holds (rule 3e)."""
    if not isinstance(raw_managers, list):
        raise ConfigError(f"{preset_ref}: customManagers must be a list")
    for idx, mgr in enumerate(raw_managers):
        where = f"{preset_ref} customManagers[{idx}]"
        if not isinstance(mgr, dict):
            raise ConfigError(f"{where}: entry must be an object")
        groups = manager_named_groups(mgr, where)
        if not is_non_docker_manager(mgr, groups):
            ds = mgr.get("datasourceTemplate")
            raise RefusedError(
                f"{where}: key 'datasourceTemplate' ({ds!r}) may create Docker dependencies"
            )


def path_has_segment(rel_path: str, seg_lower: str) -> bool:
    """Report whether `/`-delimited REL_PATH contains ASCII-lowercased segment SEG_LOWER."""
    for part in rel_path.split("/"):
        if ascii_lower(part) == seg_lower:
            return True
    return False


def judge_one_ignore_pattern(where: str, pat: str, repo: RepoContext) -> None:
    """Validate one `ignorePaths` entry against `**/<segment>/**` and repo paths."""
    match = IGNORE_SEGMENT_RE.fullmatch(pat)
    if match is None or match.group(1) in (".", ".."):
        raise RefusedError(f"{where}: key 'ignorePaths' entry {pat!r} is not '**/<segment>/**'")
    seg = ascii_lower(match.group(1))
    for pin_path in repo.pin_paths:
        if path_has_segment(pin_path, seg):
            raise RefusedError(
                f"{where}: key 'ignorePaths' entry {pat!r} matches image pin path {pin_path!r}"
            )
    for mfp in repo.repo_docker_manager_patterns:
        if not is_exact_file_pattern(mfp):
            raise RefusedError(
                f"{where}: repo Docker customManager pattern {mfp!r} is not an exact path"
            )
        if path_has_segment(mfp, seg):
            raise RefusedError(
                f"{where}: key 'ignorePaths' entry {pat!r} matches manager path {mfp!r}"
            )


def judge_ignore_paths(preset_ref: str, raw_paths: object, repo: RepoContext) -> None:
    """Enforce `**/<literal-segment>/**` form and check pin / manager path segments (rule 3f)."""
    if not is_str_list(raw_paths):
        raise ConfigError(f"{preset_ref}: ignorePaths must be a list of strings")
    for idx, pat in enumerate(raw_paths):  # type: ignore[arg-type]
        judge_one_ignore_pattern(f"{preset_ref} ignorePaths[{idx}]", pat, repo)


def require_allowed_subdict(
    preset_ref: str, key: str, value: object, allowed: frozenset[str]
) -> dict[str, object]:
    """Require VALUE on PRESET_REF.KEY to be a dict whose keys lie in ALLOWED."""
    if not isinstance(value, dict) or not set(value.keys()).issubset(allowed):
        raise RefusedError(f"{preset_ref}: key {key!r} has unmodeled subkeys")
    return value


def judge_top_subobjects(preset_ref: str, body: dict[str, object], repo: RepoContext) -> None:
    """Validate nested dictionaries allowed at top level of a preset (`nuget`, etc.)."""
    if "dependencyDashboard" in body:
        require_allowed_subdict(
            preset_ref, "dependencyDashboard", body["dependencyDashboard"], ALLOWED_DASHBOARD_KEYS
        )
    if "vulnerabilityAlerts" in body:
        require_allowed_subdict(
            preset_ref, "vulnerabilityAlerts", body["vulnerabilityAlerts"], ALLOWED_PRESET_VULN_KEYS
        )
    if "nuget" in body:
        nuget = require_allowed_subdict(preset_ref, "nuget", body["nuget"], ALLOWED_NUGET_KEYS)
        if "ignorePaths" in nuget:
            judge_ignore_paths(f"{preset_ref}.nuget", nuget["ignorePaths"], repo)


def check_top_disable_and_redirects(preset_ref: str, body: dict[str, object]) -> None:
    """Refuse top-level `enabled: false`, `TOP_REDIRECT_KEYS` and `enabledManagers` (rule 3b)."""
    if "enabled" in body:
        if not isinstance(body["enabled"], bool):
            raise ConfigError(f"{preset_ref}: key 'enabled' must be a boolean")
        if body["enabled"] is False:
            raise RefusedError(f"{preset_ref}: top-level key 'enabled' is false")
    for redirect_key in TOP_REDIRECT_KEYS:
        if redirect_key in body:
            raise RefusedError(f"{preset_ref}: top-level key {redirect_key!r} is not allowed")
    if "enabledManagers" in body:
        raise RefusedError(f"{preset_ref}: top-level key 'enabledManagers' is not allowed")


def is_forbidden_top_manager_block(key: str, state: ClosureState) -> bool:
    """Report whether top-level KEY is a forbidden Docker-capable built-in manager block."""
    lowered = ascii_lower(key)
    if lowered not in state.dump.docker_capable_managers:
        return False
    enabled = state.repo.enabled_managers
    return enabled is None or lowered in enabled or key != "nuget"


def check_top_prohibited_keys(
    preset_ref: str, body: dict[str, object], state: ClosureState
) -> None:
    """Refuse top-level disable/redirect/manager blocks and keys outside `ALLOWED_PRESET_TOP_KEYS`."""
    check_top_disable_and_redirects(preset_ref, body)
    for key in body:
        if is_forbidden_top_manager_block(key, state):
            raise RefusedError(
                f"{preset_ref}: top-level Docker-capable manager block {key!r} is not allowed"
            )
        if key not in ALLOWED_PRESET_TOP_KEYS:
            raise RefusedError(f"{preset_ref}: unknown top-level key {key!r}")


def judge_top_preset(preset_ref: str, body: dict[str, object], state: ClosureState) -> tuple[str, ...]:
    """Judge one top-context preset body against rules 3a–3f and return its `extends` list."""
    check_nested_directives(body, preset_ref, allow_rule_extends=True)
    check_top_prohibited_keys(preset_ref, body, state)
    if "minimumReleaseAge" in body:
        age = body["minimumReleaseAge"]
        if age is not None and not isinstance(age, str):
            raise ConfigError(f"{preset_ref}: top-level minimumReleaseAge must be string or null")
        if not state.repo.has_top_minimum_release_age and is_age_disabled(age):
            raise RefusedError(
                f"{preset_ref}: top-level minimumReleaseAge {age!r} disables the release wait"
            )
    judge_top_subobjects(preset_ref, body, state.repo)
    if "ignorePaths" in body:
        judge_ignore_paths(preset_ref, body["ignorePaths"], state.repo)
    if "customManagers" in body:
        judge_preset_custom_managers(preset_ref, body["customManagers"])
    if "packageRules" in body:
        raw_rules = body["packageRules"]
        if not isinstance(raw_rules, list):
            raise ConfigError(f"{preset_ref}: packageRules must be a list")
        for idx, raw_rule in enumerate(raw_rules):
            judge_preset_rule(preset_ref, idx, raw_rule, state)
    return parse_extends(body)


def walk_closure(state: ClosureState) -> None:
    """Walk top-level and rule-level `extends` iteratively and judge every reached preset."""
    queue: deque[tuple[str, str]] = deque(
        (ref, "renovate.json extends") for ref in state.repo.top_extends
    )
    steps = 0
    while queue:
        steps += 1
        if steps > MAX_RULE_STEPS:
            raise ConfigError(f"top-level closure walk exceeded {MAX_RULE_STEPS} edges")
        raw_ref, where = queue.popleft()
        canon, body = resolve_preset_ref(raw_ref, state, where)
        if canon in state.visited_top:
            continue
        state.visited_top.add(canon)
        for child_ref in judge_top_preset(canon, body, state):
            queue.append((child_ref, f"{canon} extends"))

    for rule_idx, rule_exts in state.repo.repo_rule_extends:
        stub_rule: dict[str, object] = {"extends": list(rule_exts)}
        judge_preset_rule("renovate.json", rule_idx, stub_rule, state)


def format_summary(horistes_sha: str, state: ClosureState) -> str:
    """Format the single-line pass summary naming versions, counts and exclusion reasons."""
    excluded_total = sum(state.excluded_reasons.values())
    if excluded_total:
        parts = ", ".join(
            f"{reason}={count}" for reason, count in sorted(state.excluded_reasons.items())
        )
        excluded_str = f"{excluded_total} ({parts})"
    else:
        excluded_str = "0"
    return (
        f"renovate_presets: ok ("
        f"renovate={state.dump.renovate_version}, "
        f"horistes={horistes_sha}, "
        f"presets={len(state.reached_presets)}, "
        f"rules={state.rules_judged}, "
        f"excluded={excluded_str})"
    )


def make_http_error(url: str, code: int) -> urllib.error.HTTPError:
    """Construct an `HTTPError` with an empty closable body for `--self-test`."""
    return urllib.error.HTTPError(url, code, f"HTTP {code}", hdrs=None, fp=io.BytesIO(b""))  # type: ignore[arg-type]


def self_test_default_json(sha: str) -> None:
    """Verify `default.json` HTTP 200 path and bearer header attachment."""
    calls: list[tuple[str, str | None]] = []

    def ok_opener(req: urllib.request.Request, _timeout: int) -> bytes:
        """Serve `HEAD` and `default.json` while recording `(url, Authorization)`."""
        calls.append((req.full_url, req.get_header("Authorization")))
        if req.full_url == HORISTES_COMMITS_URL:
            return f"{sha}\n".encode("ascii")
        if req.full_url == f"{HORISTES_CONTENTS_URL}/default.json?ref={sha}":
            return b'{"extends": ["config:recommended"]}'
        raise AssertionError(f"unexpected URL {req.full_url}")

    got_sha, got_body = fetch_horistes(token="tok-test", opener=ok_opener)
    if got_sha != sha or got_body != {"extends": ["config:recommended"]}:
        raise AssertionError("default.json body mismatch")
    if len(calls) != 2 or any(auth != "Bearer tok-test" for _, auth in calls):
        raise AssertionError("Authorization header mismatch on default.json path")


def self_test_renovate_json_fallback(sha: str) -> None:
    """Verify `default.json` HTTP 404 -> `renovate.json` fallback with anonymous headers."""
    calls: list[tuple[str, str | None]] = []

    def fallback_opener(req: urllib.request.Request, _timeout: int) -> bytes:
        """Return 404 on `default.json` and 200 on `renovate.json`."""
        calls.append((req.full_url, req.get_header("Authorization")))
        if req.full_url == HORISTES_COMMITS_URL:
            return sha.encode("ascii")
        if req.full_url == f"{HORISTES_CONTENTS_URL}/default.json?ref={sha}":
            raise make_http_error(req.full_url, 404)
        if req.full_url == f"{HORISTES_CONTENTS_URL}/renovate.json?ref={sha}":
            return b'{"description": "fallback"}'
        raise AssertionError(f"unexpected URL {req.full_url}")

    got_sha, got_body = fetch_horistes(token=None, opener=fallback_opener)
    if got_sha != sha or got_body != {"description": "fallback"}:
        raise AssertionError("renovate.json fallback body mismatch")
    if len(calls) != 3 or any(auth is not None for _, auth in calls):
        raise AssertionError("anonymous fallback call mismatch")


def self_test_horistes_errors(sha: str) -> None:
    """Verify non-404 HTTP errors, oversized responses, bad SHAs and redirect credential stripping."""
    def http_500_opener(req: urllib.request.Request, _timeout: int) -> bytes:
        """Return SHA on HEAD and HTTP 500 on `default.json`."""
        if req.full_url == HORISTES_COMMITS_URL:
            return sha.encode("ascii")
        raise make_http_error(req.full_url, 500)

    def oversized_opener(req: urllib.request.Request, _timeout: int) -> bytes:
        """Return SHA on HEAD and MAX_HTTP_BYTES + 1 bytes on `default.json`."""
        if req.full_url == HORISTES_COMMITS_URL:
            return sha.encode("ascii")
        return b" " * (MAX_HTTP_BYTES + 1)

    def bad_sha_opener(_req: urllib.request.Request, _timeout: int) -> bytes:
        """Return a non-hex ref instead of a 40-char SHA."""
        return b"not-a-40-hex-sha"

    for bad_opener in (http_500_opener, oversized_opener, bad_sha_opener):
        try:
            fetch_horistes(token=None, opener=bad_opener)
        except ConfigError:
            pass
        else:
            raise AssertionError(f"{bad_opener.__name__} did not raise ConfigError")


def self_test_redirects() -> None:
    """Verify `HttpsRedirectHandler` strips cross-host auth and refuses plain HTTP redirects."""
    handler = HttpsRedirectHandler()
    orig = urllib.request.Request(
        HORISTES_COMMITS_URL, headers={"Authorization": "Bearer secret"}
    )
    redir = handler.redirect_request(
        orig, None, 302, "Found", {}, "https://objects.githubusercontent.com/x"
    )
    if redir is None or "Authorization" in redir.headers:
        raise AssertionError("cross-host redirect retained Authorization header")
    try:
        handler.redirect_request(orig, None, 302, "Found", {}, "http://api.github.com/x")
    except urllib.error.URLError:
        return
    raise AssertionError("http redirect was not refused")


def self_test_read_with_deadline() -> None:
    """Verify response read success, total-deadline refusal and LIMIT + 1 boundary."""
    class FakeResponse:
        """Return caller-provided chunks while respecting the requested read size."""

        def __init__(self, chunks: list[bytes]) -> None:
            """Queue the chunks the fake body returns in order."""
            self.chunks = deque(chunks)

        def read(self, size: int = -1) -> bytes:
            """Return the next chunk, split at `size` like a real response."""
            chunk = self.chunks.popleft() if self.chunks else b""
            if size >= 0 and len(chunk) > size:
                self.chunks.appendleft(chunk[size:])
                return chunk[:size]
            return chunk

    if read_with_deadline(FakeResponse([b"abc"]), 8, 1, clock=lambda: 0.0) != b"abc":
        raise AssertionError("under-deadline response mismatch")
    times = iter((0.0, 0.0, 0.0, 2.0))
    try:
        read_with_deadline(
            FakeResponse([b"a", b"b"]), 8, 1, clock=lambda: next(times)
        )
    except TimeoutError as exc:
        if str(exc) != "response read exceeded 1s":
            raise
    else:
        raise AssertionError("mid-body deadline was not refused")
    boundary = read_with_deadline(
        FakeResponse([b"abcdef"]), 4, 1, clock=lambda: 0.0
    )
    if boundary != b"abcde":
        raise AssertionError("LIMIT + 1 boundary mismatch")
    try:
        read_with_deadline(FakeResponse([b"a"] * 4), 8, 1, clock=lambda: 0.0)
    except TimeoutError as exc:
        if str(exc) != "response read exceeded 2 reads":
            raise
    else:
        raise AssertionError("read count bound was not refused")


def self_test_https_opener() -> None:
    """Verify the live opener reads the body through `read_with_deadline`, not one short read."""
    class TrickleResponse:
        """Return one byte per read, like a body arriving in small pieces."""

        def __init__(self) -> None:
            """Queue a three-byte body delivered one byte per read."""
            self.left = deque([b"a", b"b", b"c"])

        def __enter__(self) -> Self:
            """Act as its own context manager, like `urlopen` responses."""
            return self

        def __exit__(self, *_exc: object) -> None:
            """Hold no resource, so leaving the context does nothing."""

        def read(self, _size: int = -1) -> bytes:
            """Return one byte, then an empty read at end of body."""
            return self.left.popleft() if self.left else b""

    class FakeOpener:
        """Stand in for `urllib.request.OpenerDirector` without network access."""

        def open(self, _req: object, timeout: float) -> TrickleResponse:
            """Refuse a call without a deadline, else return the trickling body."""
            if timeout <= 0:
                raise AssertionError("opener called without a deadline")
            return TrickleResponse()

    req = urllib.request.Request(f"https://{GITHUB_API_HOST}/repos/x/y")
    body = default_https_opener(req, 5, build_opener=lambda *_h: FakeOpener())
    if body != b"abc":
        raise AssertionError(f"live opener read {body!r}, want b'abc'")


def self_test_https_opener_deadline() -> None:
    """Verify one deadline bounds blocked opener and response reads."""
    class BlockingResponse:
        """Block its only body read until the daemon worker is abandoned."""

        def __enter__(self) -> Self:
            """Act as its own context manager."""
            return self

        def __exit__(self, *_exc: object) -> None:
            """Hold no resource after the timed-out daemon worker exits."""

        def read(self, _size: int = -1) -> bytes:
            """Block beyond the outer response deadline."""
            threading.Event().wait()
            return b""

    class BlockingOpen:
        """Block opening the response beyond the outer response deadline."""

        def open(self, _req: object, timeout: float) -> BlockingResponse:
            """Block before a response can be returned."""
            if timeout <= 0:
                raise AssertionError("opener called without a deadline")
            threading.Event().wait()
            return BlockingResponse()

    class BlockingRead:
        """Return a response whose body read blocks beyond the deadline."""

        def open(self, _req: object, timeout: float) -> BlockingResponse:
            """Return a blocking response."""
            if timeout <= 0:
                raise AssertionError("opener called without a deadline")
            return BlockingResponse()

    req = urllib.request.Request(f"https://{GITHUB_API_HOST}/repos/x/y")
    for opener in (BlockingOpen(), BlockingRead()):
        started = time.monotonic()
        try:
            default_https_opener(req, 0.2, build_opener=lambda *_h, o=opener: o)
        except TimeoutError as exc:
            if str(exc) != "response read exceeded 0.2s":
                raise
            if time.monotonic() - started > 1.2:
                raise AssertionError("blocked response outlived deadline tolerance")
        else:
            raise AssertionError("blocked response did not time out")


def self_test_https_opener_exception() -> None:
    """Verify worker HTTP errors retain their identity and message."""
    req = urllib.request.Request(f"https://{GITHUB_API_HOST}/repos/x/y")
    expected = make_http_error(req.full_url, 503)

    class FailingOpener:
        """Raise the prebuilt HTTP error from the worker thread."""

        def open(self, _req: object, timeout: float) -> Readable:
            """Propagate the exact HTTP error object."""
            if timeout <= 0:
                raise AssertionError("opener called without a deadline")
            raise expected

    try:
        default_https_opener(req, 1, build_opener=lambda *_h: FailingOpener())
    except urllib.error.HTTPError as exc:
        if exc is not expected:
            raise AssertionError("worker HTTP error identity changed") from exc
    else:
        raise AssertionError("worker HTTP error was not propagated")


def run_self_test() -> int:
    """Execute offline unit tests for `horistes` HTTP fetch and redirect handling."""
    sha = "1075f6323bffb41e599fae6d7113a19029b33f2d"
    self_test_default_json(sha)
    self_test_renovate_json_fallback(sha)
    self_test_horistes_errors(sha)
    self_test_redirects()
    self_test_read_with_deadline()
    self_test_https_opener()
    self_test_https_opener_deadline()
    self_test_https_opener_exception()
    print("renovate_presets: self-test ok")
    return 0


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    """Parse CLI options for live mode, offline fixture mode and `--self-test`."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--dump", type=Path, default=None, help="offline dump JSON path")
    parser.add_argument("--horistes", type=Path, default=None, help="offline horistes JSON path")
    parser.add_argument(
        "--repo-root", type=Path, default=DEFAULT_REPO_ROOT, help="repository root to judge"
    )
    parser.add_argument(
        "--tool-versions",
        type=Path,
        default=DEFAULT_TOOL_VERSIONS,
        help="path to tool-versions.env in live mode",
    )
    parser.add_argument(
        "--self-test", action="store_true", help="run offline horistes fetch unit tests"
    )
    return parser.parse_args(argv)


def run_check(args: argparse.Namespace) -> int:
    """Execute live or offline Renovate preset closure judgement and return exit code."""
    if (args.dump is None) != (args.horistes is None):
        raise ConfigError("--dump and --horistes must be passed together")
    if args.dump is not None and args.horistes is not None:
        dump = parse_dump(
            read_bounded_file(args.dump, MAX_DUMP_BYTES, str(args.dump)), str(args.dump)
        )
        horistes = load_json_object(
            read_bounded_file(args.horistes, MAX_HTTP_BYTES, str(args.horistes)),
            str(args.horistes),
        )
        horistes_sha = "offline"
    else:
        image = read_renovate_image(args.tool_versions)
        dump = parse_dump(run_docker_dump(image, DUMP_SCRIPT), "docker dump")
        token = os.environ.get("GH_TOKEN") or None
        horistes_sha, horistes = fetch_horistes(token=token)

    repo = load_repo_context(args.repo_root.resolve(), dump.manager_renames)
    state = ClosureState(dump=dump, horistes=horistes, repo=repo)
    walk_closure(state)
    print(format_summary(horistes_sha, state))
    return 0


def main(argv: list[str] | None = None) -> int:
    """CLI entrypoint mapping `ConfigError` -> exit 1 and `RefusedError` -> exit 3."""
    args = parse_args(argv)
    try:
        if args.self_test:
            return run_self_test()
        return run_check(args)
    except RefusedError as exc:
        print(f"renovate_presets: refused: {exc}", file=sys.stderr)
        return 3
    except (ConfigError, ValueError, subprocess.SubprocessError) as exc:
        print(f"renovate_presets: error: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
