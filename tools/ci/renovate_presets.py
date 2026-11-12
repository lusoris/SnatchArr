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
import os
import re
import shutil
import subprocess  # nosec B404  # runs docker with a fixed argument list, never a shell
import sys
import threading
from collections import deque
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import image_pin_check
import renovate_presets_judge
import renovate_presets_net
from renovate_managers import (
    is_non_docker_manager,
    parse_enabled_managers,
    parse_extends,
    parse_str_list,
)
from renovate_presets_judge import (
    LITERAL_MATCHER_RE,
    MAX_CONFIG_NODES,
    MAX_RULE_STEPS,
    ClosureState,
    DumpData,
    RepoContext,
    check_nested_directives,
    is_str_list,
    judge_preset_rule,
    judge_top_preset,
    manager_named_groups,
    resolve_preset_ref,
)
from renovate_presets_net import (
    MAX_HTTP_BYTES,
    READ_CHUNK_BYTES,
    fetch_horistes,
    load_json_object,
)
from renovate_regex import ConfigError, RefusedError, ascii_lower

HERE = Path(__file__).resolve().parent
DEFAULT_REPO_ROOT = HERE.parents[1]
DEFAULT_TOOL_VERSIONS = HERE / "tool-versions.env"
DUMP_SCRIPT = HERE / "renovate_presets_dump.mjs"

DOCKER_TIMEOUT_SECONDS = 120
MAX_DUMP_BYTES = 32 << 20
MAX_ENV_LINES = 1024
MAX_MANAGER_RENAMES = 64
MAX_STDERR_READS = 1 << 16

IMAGE_RE = re.compile(r"^[a-z0-9./-]+:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$")

EXPECTED_DEP_TYPE_EMITTERS: dict[str, frozenset[str]] = {
    "github-runner": frozenset({
        "modules/manager/github-actions/dep-types.js",
        "modules/manager/github-actions/extract.js",
    }),
    "php2shell-runtime": frozenset({
        "config/presets/internal/security.preset.js",
    }),
}


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


def run_self_test() -> int:
    """Verify invalid migration names locally, then run judging and network cases."""
    for field in ("matchManagers", "enabledManagers"):
        for renames in ({"__proto__": "dockerfile"}, {"npm": "__proto__"}):
            maps = {"matchManagers": {}, "enabledManagers": {}}
            maps[field] = renames
            try:
                parse_manager_renames(maps, "self-test")
            except ConfigError as exc:
                if "has an invalid manager name" not in str(exc):
                    raise
            else:
                raise AssertionError("prototype-like migration name was accepted")
    renovate_presets_judge.run_self_test()
    renovate_presets_net.run_self_test()
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
