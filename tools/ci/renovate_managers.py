# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Pure Renovate 44.234.0 manager, preset, record-file and `custom.regex` capture logic.

Split from `renovate_rules.py`: owns `BUILTIN_MANAGER_FILE_PATTERNS`, `MODELED_PRESETS` and
`extends` parsing, `customManagers` validation, exact-path classification and two-pass snapshot
comparison, `record`-file protection, and Docker `custom.regex` `depName` capture extraction.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from collections import defaultdict
from collections.abc import Iterable, Mapping
from dataclasses import dataclass
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from renovate_regex import (
    DOCKER_ALLOWED_GROUPS,
    GLOB_CHARS,
    ConfigError,
    RefusedError,
    ascii_lower,
    check_input_domain,
    compile_match_string,
    compile_regex_literal,
    expect_failure,
    extract_named_groups,
)

ALLOWED_CUSTOM_MANAGER_KEYS = frozenset({
    "autoReplaceStringTemplate",
    "currentValueTemplate",
    "customType",
    "datasourceTemplate",
    "depNameTemplate",
    "depTypeTemplate",
    "description",
    "extractVersionTemplate",
    "managerFilePatterns",
    "matchStrings",
    "packageNameTemplate",
    "registryUrlTemplate",
    "versioningTemplate",
})
DOCKER_FORBIDDEN_MANAGER_KEYS = (
    "packageNameTemplate",
    "depNameTemplate",
    "registryUrlTemplate",
)
# Default `managerFilePatterns` from Renovate 44.234.0 `lib/modules/manager/<name>/index.ts`
# (`defaultConfig`); `"custom.regex"` delegates file matching to `customManagers`.
BUILTIN_MANAGER_FILE_PATTERNS: dict[str, tuple[str, ...]] = {
    "cargo": (r"/(^|/)Cargo\.toml$/",),
    "custom.regex": (),
    "devcontainer": (
        r"/^.devcontainer/devcontainer.json$/",
        r"/^.devcontainer.json$/",
        r"/^.devcontainer/[^/]+/devcontainer.json$/",
    ),
    "docker-compose": (r"/(^|/)(?:docker-)?compose[^/]*\.ya?ml$/",),
    "dockerfile": (
        r"/(^|/|\.)([Dd]ocker|[Cc]ontainer)file$/",
        r"/(^|/)([Dd]ocker|[Cc]ontainer)file[^/]*$/",
    ),
    "github-actions": (
        r"/(^|/)(workflow-templates|\.(?:github|gitea|forgejo)/(?:workflows|actions))/.+\.ya?ml$/",
        r"/(^|/)action\.ya?ml$/",
    ),
    "gomod": (r"/(^|/)go\.mod$/",),
    "helm-values": (r"/(^|/)values\.ya?ml$/",),
    "helmv3": (r"/(^|/)Chart\.ya?ml$/",),
    "npm": (
        r"/(^|/)package\.json$/",
        r"/(^|/)pnpm-workspace\.yaml$/",
        r"/(^|/)\.yarnrc\.yml$/",
    ),
    "pip_requirements": (r"/(^|/)[\w-]*requirements([-._]\w+)?\.(txt|pip)$/",),
    "rust-toolchain": (r"/(^|/)rust-toolchain(\.toml)?$/",),
}
# Internal presets verified in Renovate 44.234.0 `config/presets/internal/*.preset.js`
# (none carries `extends`, managers, `managerFilePatterns`, or age-affecting `packageRules`):
# - `:semanticCommits` = `default:semanticCommits` = `{semanticCommits: "enabled"}`
# - `helpers:pinGitHubActionDigests` = `packageRules: [{matchDepTypes: ["action", "workflow"],
#   pinDigests: true}]`
# - `docker:pinDigests` = `packageRules: [{matchDatasources: ["docker"], pinDigests: true},
#   {matchManagers: ["argocd", "devcontainer", "helmv3", "pyenv"], pinDigests: false}]`
MODELED_PRESETS = frozenset({
    ":semanticCommits",
    "helpers:pinGitHubActionDigests",
    "docker:pinDigests",
})
MAX_MATCH_ITERATIONS = 10000
UNKNOWN_KEY_SUFFIX = (
    "classify it in tools/ci/renovate_rules.py after checking it cannot redirect a Docker "
    "lookup or change release age"
)

SnapshotEntry = tuple[str, bytes | None]


@dataclass(frozen=True)
class ManagerFilePattern:
    """One compiled `customManagers[i].managerFilePatterns` entry."""

    manager_index: int
    raw: str
    kind: str
    regex: re.Pattern[str] | None = None


@dataclass(frozen=True)
class DockerManagerSpec:
    """Validated Docker-capable `customManagers[index]` entry awaiting capture extraction."""

    index: int
    file_patterns: tuple[ManagerFilePattern, ...]
    match_strings: tuple[str, ...]


def no_duplicate_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    """Construct a JSON object from PAIRS while refusing duplicate keys."""
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ConfigError(f"renovate.json: duplicate JSON key {key!r}")
        result[key] = value
    return result


def refuse_unknown_keys(
    obj: dict[str, object],
    allowed: frozenset[str],
    where: str,
    *,
    allow_extra_matchers: bool = False,
) -> None:
    """Refuse any key on OBJ outside ALLOWED (plus `match*`/`exclude*` when enabled)."""
    for key in obj:
        if key in allowed:
            continue
        if allow_extra_matchers and key.startswith(("match", "exclude")):
            continue
        raise RefusedError(f"{where}: unknown key {key}: {UNKNOWN_KEY_SUFFIX}")


def parse_str_list(obj: dict[str, object], key: str, where: str) -> list[str]:
    """Validate optional list-of-strings option KEY on OBJ (default `[]`)."""
    raw = obj.get(key, [])
    if not isinstance(raw, list) or not all(isinstance(item, str) for item in raw):
        raise ConfigError(f"renovate.json: {where}.{key} must be a list of strings")
    return raw


def parse_extends(data: dict[str, object]) -> tuple[str, ...]:
    """Validate optional `extends` on DATA (`allowString: true` in Renovate options)."""
    if "extends" not in data:
        return ()
    raw = data["extends"]
    if isinstance(raw, str):
        return (raw,)
    if isinstance(raw, list) and all(isinstance(item, str) for item in raw):
        return tuple(raw)
    raise ConfigError("renovate.json: extends must be a string or a list of strings")


def first_unmodeled_preset(extends: Iterable[str]) -> str | None:
    """Return the first `extends` entry outside `MODELED_PRESETS`, or `None` when all modeled."""
    for entry in extends:
        if entry not in MODELED_PRESETS:
            return entry
    return None


def is_exact_file_pattern(raw: str) -> bool:
    """Report whether `managerFilePatterns` string RAW is a plain normalized exact path."""
    if not raw or raw.startswith(("/", "!", "#")) or GLOB_CHARS.intersection(raw):
        return False
    return not any(seg in ("", ".", "..") for seg in raw.split("/"))


def compile_manager_file_pattern(index: int, raw: str) -> ManagerFilePattern:
    """Compile one `customManagers[INDEX].managerFilePatterns` string RAW."""
    if raw.startswith("/"):
        return ManagerFilePattern(
            manager_index=index,
            raw=raw,
            kind="regex",
            regex=compile_regex_literal(raw, raw),
        )
    if raw.startswith("!/"):
        compile_regex_literal(raw, raw[1:])
        return ManagerFilePattern(manager_index=index, raw=raw, kind="glob")
    if is_exact_file_pattern(raw):
        return ManagerFilePattern(manager_index=index, raw=raw, kind="exact")
    # Only a plain relative path compares exactly; `./x`, `a//b`, `..` or globs may match after
    # minimatch normalizes them, so they count as `glob`.
    return ManagerFilePattern(manager_index=index, raw=raw, kind="glob")


def manager_exact_entries(mgr: object) -> list[str]:
    """Return ASCII-lowercased exact `managerFilePatterns` entries on one raw JSON manager."""
    if not isinstance(mgr, dict):
        return []
    pats = mgr.get("managerFilePatterns")
    if not isinstance(pats, list):
        return []
    return [ascii_lower(pat) for pat in pats if isinstance(pat, str) and is_exact_file_pattern(pat)]


def wanted_manager_paths(config_text: str) -> set[str]:
    """Extract ASCII-lowercased exact `customManagers[*].managerFilePatterns` from JSON text."""
    try:
        data = json.loads(config_text, object_pairs_hook=no_duplicate_keys)
    except (ValueError, ConfigError):
        return set()
    if not isinstance(data, dict) or not isinstance(data.get("customManagers"), list):
        return set()
    wanted: set[str] = set()
    for mgr in data["customManagers"]:  # type: ignore[union-attr]
        wanted.update(manager_exact_entries(mgr))
    return wanted


def snapshot_entry(path: str, raw: bytes | None) -> SnapshotEntry:
    """Return `(path, sha256_bytes_or_None)` for one `(path, raw)` item from `read_contents`."""
    return (path, None if raw is None else hashlib.sha256(raw).digest())


def check_same_snapshot(pass1: set[SnapshotEntry], pass2: set[SnapshotEntry]) -> None:
    """Refuse when pass 2 over `read_contents` yields a different `(path, sha256)` set than pass 1."""
    if pass1 != pass2:
        raise RefusedError("tree changed during check")


def is_non_docker_manager(mgr: dict[str, object], named_groups: set[str]) -> bool:
    """Report whether customManager MGR definitely targets a non-Docker datasource."""
    ds = mgr.get("datasourceTemplate")
    if not isinstance(ds, str) or "{{" in ds or "datasource" in named_groups:
        return False
    cleaned = ds.strip().lower()
    return bool(cleaned) and cleaned != "docker"


def check_docker_manager(mgr: dict[str, object], named_groups: set[str], where: str) -> None:
    """Refuse unmodeled templates or named groups on a Docker-capable customManager."""
    for key in DOCKER_FORBIDDEN_MANAGER_KEYS:
        if key in mgr:
            raise RefusedError(f"{where}: {key} is not modeled on a Docker customManager")
    extra = sorted(named_groups - DOCKER_ALLOWED_GROUPS)
    if extra:
        bad = extra[0]
        raise RefusedError(
            f"{where}: matchStrings group {bad!r} is not modeled on a Docker customManager"
        )


def validate_custom_manager(
    raw: object, index: int
) -> tuple[list[ManagerFilePattern], DockerManagerSpec | None]:
    """Validate `customManagers[INDEX]` keys, Docker rules and `managerFilePatterns`."""
    where = f"customManagers[{index}]"
    if not isinstance(raw, dict):
        raise ConfigError(f"renovate.json: {where} must be an object")
    refuse_unknown_keys(raw, ALLOWED_CUSTOM_MANAGER_KEYS, where)
    if "customType" in raw and not isinstance(raw["customType"], str):
        raise ConfigError(f"renovate.json: {where}.customType must be a string")
    if raw.get("customType") != "regex":
        raise RefusedError(f"{where}: customType must be 'regex'")
    match_strings = parse_str_list(raw, "matchStrings", where)
    raw_pats = parse_str_list(raw, "managerFilePatterns", where)
    named_groups = extract_named_groups(match_strings, where)
    patterns = [compile_manager_file_pattern(index, pat) for pat in raw_pats]
    if is_non_docker_manager(raw, named_groups):
        return patterns, None
    check_docker_manager(raw, named_groups, where)
    spec = DockerManagerSpec(
        index=index,
        file_patterns=tuple(patterns),
        match_strings=tuple(match_strings),
    )
    return patterns, spec


def parse_enabled_managers(data: dict[str, object]) -> tuple[str, ...] | None:
    """Validate optional `enabledManagers` on DATA; return `None` when every manager is enabled.

    `getEnabledManagersList` (`lib/modules/manager/index.ts`) filters only when
    `enabledManagers?.length` is non-zero, so absent, null and `[]` all enable every manager.
    """
    raw = data.get("enabledManagers")
    if raw is None:
        return None
    if not isinstance(raw, list) or not all(isinstance(item, str) for item in raw):
        raise ConfigError("renovate.json: enabledManagers must be a list of strings")
    return tuple(raw) or None


def parse_custom_managers(
    data: dict[str, object],
) -> tuple[tuple[ManagerFilePattern, ...], tuple[DockerManagerSpec, ...]]:
    """Validate `customManagers` on DATA; return file patterns and Docker manager specs."""
    raw_managers = data.get("customManagers", [])
    if not isinstance(raw_managers, list):
        raise ConfigError("renovate.json: customManagers must be a list")
    patterns: list[ManagerFilePattern] = []
    docker_specs: list[DockerManagerSpec] = []
    for index, raw_mgr in enumerate(raw_managers):
        mgr_pats, docker_spec = validate_custom_manager(raw_mgr, index)
        patterns.extend(mgr_pats)
        if docker_spec is not None:
            docker_specs.append(docker_spec)
    return tuple(patterns), tuple(docker_specs)


def file_pattern_may_match(entry: ManagerFilePattern, record_path: str) -> bool:
    """Report whether `managerFilePatterns` ENTRY may match RECORD_PATH."""
    if entry.kind == "glob":
        return True
    if entry.kind == "exact":
        return ascii_lower(entry.raw) == ascii_lower(record_path)
    if entry.regex is None:
        raise ConfigError("renovate.json: internal file regex state error")
    return bool(entry.regex.search(record_path))


def check_builtin_patterns(mgr: str, sorted_records: list[str]) -> None:
    """Refuse when any default `managerFilePatterns` regex of MGR matches SORTED_RECORDS."""
    for pat in BUILTIN_MANAGER_FILE_PATTERNS[mgr]:
        rx = compile_regex_literal(pat, pat)
        for rec in sorted_records:
            if rx.search(rec):
                raise RefusedError(
                    f"enabledManagers: {mgr} default managerFilePatterns entry {pat} "
                    f"reads record file {rec}: a record pin must be one no updater moves"
                )


def check_records_not_builtin(
    enabled_managers: tuple[str, ...] | None, records: Iterable[str]
) -> None:
    """Refuse missing/unmodeled `enabledManagers` or built-in patterns hitting RECORDS."""
    sorted_records = sorted(set(records))
    if not sorted_records:
        return
    first_rec = sorted_records[0]
    if enabled_managers is None:
        raise RefusedError(
            f"enabledManagers is absent or empty: every built-in Renovate manager may read "
            f"record file {first_rec}; list enabledManagers so the check can rule them out"
        )
    for mgr in sorted(set(enabled_managers)):
        if mgr not in BUILTIN_MANAGER_FILE_PATTERNS:
            raise RefusedError(
                f"enabledManagers entry {mgr} is not modeled: its default "
                f"managerFilePatterns may read record file {first_rec}; add it to "
                "BUILTIN_MANAGER_FILE_PATTERNS in tools/ci/renovate_managers.py"
            )
        check_builtin_patterns(mgr, sorted_records)


def check_records_not_managed(
    file_patterns: Iterable[ManagerFilePattern], records: Iterable[str]
) -> None:
    """Refuse any `customManagers` file pattern that may read a `record`-marked file."""
    sorted_records = sorted(set(records))
    if not sorted_records:
        return
    for entry in file_patterns:
        for rec in sorted_records:
            if file_pattern_may_match(entry, rec):
                raise RefusedError(
                    f"customManagers[{entry.manager_index}]: managerFilePatterns entry "
                    f"{entry.raw} reads record file {rec}: "
                    "a record pin must be one no updater moves"
                )


def decode_managed_content(raw: bytes, path: str, manager_index: int) -> str:
    """Decode RAW of PATH as strict UTF-8 and refuse vertical tab `\\x0b`."""
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise RefusedError(
            f"customManagers[{manager_index}]: {path} is not valid UTF-8 "
            f"({exc.reason} at byte {exc.start}); Renovate would read U+FFFD"
        ) from exc
    if "\x0b" in text:
        raise RefusedError(
            f"customManagers[{manager_index}]: {path} contains '\\x0b' (vertical tab), "
            "where Python \\s and RE2 \\s diverge"
        )
    return text


def extract_regex_matches(
    rx: re.Pattern[str],
    text: str,
    path: str,
    manager_index: int,
    match_index: int,
) -> list[tuple[str, str]]:
    """Run RX over TEXT up to Renovate's 10000-match cap; return `(depName, location)` pairs."""
    pairs: list[tuple[str, str]] = []
    for count, match in enumerate(rx.finditer(text), 1):
        if count >= MAX_MATCH_ITERATIONS:
            raise RefusedError(
                f"customManagers[{manager_index}].matchStrings[{match_index}]: "
                f"{path} reached {MAX_MATCH_ITERATIONS} matches"
            )
        cap = match.group("depName")
        if cap:
            line = 1 + text.count("\n", 0, match.start("depName"))
            pairs.append((cap, f"customManagers[{manager_index}] {path}:{line}"))
    return pairs


def compile_docker_manager(
    spec: DockerManagerSpec, searched_set: set[str]
) -> tuple[set[str], list[re.Pattern[str]]]:
    """Validate exact paths, `matchStrings` and `searched` guard for Docker manager SPEC."""
    for entry in spec.file_patterns:
        if entry.kind != "exact":
            raise RefusedError(
                f"customManagers[{spec.index}]: managerFilePatterns entry {entry.raw} is a "
                "glob or regex; a Docker customManager must name exact paths so the check "
                "can read what it captures"
            )
        # minimatch `nocase` folds non-ASCII letters too (`Ä` matches `ä`); `ascii_lower` does not.
        check_input_domain([entry.raw], f"customManagers[{spec.index}]: managerFilePatterns entry")
    compiled = [
        compile_match_string(ms, f"customManagers[{spec.index}].matchStrings[{j}]")
        for j, ms in enumerate(spec.match_strings)
    ]
    for entry in spec.file_patterns:
        if entry.kind == "exact" and ascii_lower(entry.raw) not in searched_set:
            raise RefusedError(
                f"customManagers[{spec.index}]: managerFilePatterns entry {entry.raw} "
                "was not searched by parent"
            )
    return {
        ascii_lower(entry.raw) for entry in spec.file_patterns if entry.kind == "exact"
    }, compiled


def extract_docker_captures(
    specs: Iterable[DockerManagerSpec],
    searched: Iterable[str],
    files: Mapping[str, bytes],
) -> dict[str, list[str]]:
    """Extract and domain-check `depName` captures across all Docker `customManagers` SPECS."""
    searched_set = {ascii_lower(item) for item in searched}
    sorted_paths = sorted(files)
    found: dict[str, list[str]] = defaultdict(list)
    for spec in specs:
        wanted, regexes = compile_docker_manager(spec, searched_set)
        for path in sorted_paths:
            if ascii_lower(path) not in wanted:
                continue
            text = decode_managed_content(files[path], path, spec.index)
            for j, rx in enumerate(regexes):
                for cap, loc in extract_regex_matches(rx, text, path, spec.index, j):
                    found[cap].append(loc)
    check_input_domain(found, "captured depName")
    return dict(found)


def self_test_builtin_managers() -> None:
    """Compile every built-in manager pattern and test record-file hits and misses."""
    for mgr, pats in BUILTIN_MANAGER_FILE_PATTERNS.items():
        for pat in pats:
            compile_regex_literal(pat, f"{mgr}:{pat}")
    hits = (
        ("cargo", "Cargo.toml"),
        ("devcontainer", ".devcontainer/devcontainer.json"),
        ("docker-compose", "compose.yaml"),
        ("dockerfile", "Dockerfile"),
        ("github-actions", ".github/workflows/a.yml"),
        ("gomod", "go.mod"),
        ("helm-values", "charts/x/values.yaml"),
        ("helmv3", "charts/x/Chart.yaml"),
        ("npm", "package.json"),
        ("pip_requirements", "tools/ci/ruff-requirements.txt"),
        ("rust-toolchain", "rust-toolchain.toml"),
    )
    non_empty = {mgr for mgr, pats in BUILTIN_MANAGER_FILE_PATTERNS.items() if pats}
    if {mgr for mgr, _ in hits} != non_empty:
        raise AssertionError("self_test_builtin_managers must hit every non-empty manager")
    for mgr, rec in hits:
        expect_failure(
            lambda m=mgr, r=rec: check_records_not_builtin((m,), [r]),
            RefusedError,
            f"{mgr} hitting {rec}",
        )
    safe_records = [
        "docs/adr/0019-foreign-title-search.md",
        "tools/e2e/pushtitle/images.yaml",
        "tools/e2e/pushtitle_live.sh",
    ]
    check_records_not_builtin(tuple(sorted(BUILTIN_MANAGER_FILE_PATTERNS)), safe_records)
    expect_failure(
        lambda: check_records_not_builtin(("kubernetes",), ["rec.yml"]),
        RefusedError,
        "unmodeled enabledManagers entry",
    )
    expect_failure(
        lambda: parse_enabled_managers({"enabledManagers": "dockerfile"}),
        ConfigError,
        "bad enabledManagers",
    )


def self_test_extends_and_wanted() -> None:
    """Test `parse_extends`, `first_unmodeled_preset` and `wanted_manager_paths`."""
    if parse_extends({}) != ():
        raise AssertionError("absent extends must return ()")
    if parse_extends({"extends": "github>x/y"}) != ("github>x/y",):
        raise AssertionError("string extends must return 1-tuple")
    modeled = list(MODELED_PRESETS)
    if first_unmodeled_preset(parse_extends({"extends": modeled})) is not None:
        raise AssertionError("MODELED_PRESETS must all count as modeled")
    if first_unmodeled_preset(("default:semanticCommits", "github>x/y")) != "default:semanticCommits":
        raise AssertionError("default:semanticCommits spelling must be reported unmodeled")
    for bad in (None, 1, [1], {"a": "b"}):
        expect_failure(
            lambda b=bad: parse_extends({"extends": b}), ConfigError, f"extends={bad!r}"
        )
    cfg = json.dumps({
        "customManagers": [
            {"managerFilePatterns": ["Tools/X.env", "**/glob.yml", "/^re$/", "./rel.yml"]},
        ]
    })
    if wanted_manager_paths(cfg) != {"tools/x.env"}:
        raise AssertionError("wanted_manager_paths must return only ASCII-lowercased exact paths")
    if wanted_manager_paths("{bad json") != set():
        raise AssertionError("wanted_manager_paths must return empty set on malformed JSON")


def self_test_snapshot_compare() -> None:
    """Assert `check_same_snapshot` accepts equal sets and refuses changed/added/removed paths."""
    base = {snapshot_entry("a.yml", b"one"), snapshot_entry("link.env", None)}
    check_same_snapshot(base, {snapshot_entry("link.env", None), snapshot_entry("a.yml", b"one")})
    changed_hash = {snapshot_entry("a.yml", b"two"), snapshot_entry("link.env", None)}
    none_to_empty = {snapshot_entry("a.yml", b"one"), snapshot_entry("link.env", b"")}
    added_path = base | {snapshot_entry("b.yml", b"new")}
    removed_path = {snapshot_entry("a.yml", b"one")}
    for label, other in (
        ("changed hash", changed_hash),
        ("None vs empty", none_to_empty),
        ("added path", added_path),
        ("removed path", removed_path),
    ):
        expect_failure(
            lambda o=other: check_same_snapshot(base, o), RefusedError, f"snapshot {label}"
        )


def self_test_docker_captures() -> None:
    """Assert `extract_docker_captures` enforces exact paths, `searched`, UTF-8, `\\x0b` and caps."""
    _, specs = parse_custom_managers({
        "customManagers": [{
            "customType": "regex",
            "datasourceTemplate": "docker",
            "managerFilePatterns": ["Tools/X.env"],
            "matchStrings": [r"IMG=(?<depName>[a-z0-9./-]+):(?<currentValue>[0-9]+)\n"],
        }]
    })
    got = extract_docker_captures(
        specs, ["tools/x.env"], {"tools/x.env": b"# c\nIMG=rhysd/actionlint:1\n"}
    )
    if got != {"rhysd/actionlint": ["customManagers[0] tools/x.env:2"]}:
        raise AssertionError(f"unexpected captures: {got!r}")
    expect_failure(
        lambda: extract_docker_captures(specs, [], {"tools/x.env": b"IMG=a:1\n"}),
        RefusedError,
        "unsearched path",
    )
    expect_failure(
        lambda: extract_docker_captures(specs, ["tools/x.env"], {"tools/x.env": b"\xff"}),
        RefusedError,
        "invalid UTF-8",
    )
    expect_failure(
        lambda: extract_docker_captures(specs, ["tools/x.env"], {"tools/x.env": b"a\x0bb\n"}),
        RefusedError,
        "vertical tab",
    )
    pats, _ = parse_custom_managers({
        "customManagers": [{
            "customType": "regex",
            "datasourceTemplate": "docker",
            "managerFilePatterns": ["docs/adr.md"],
            "matchStrings": ["(?<depName>a):(?<currentValue>1)"],
        }]
    })
    check_records_not_managed(pats, ["other.md"])
    for rec in ("docs/adr.md", "DOCS/ADR.MD"):
        expect_failure(
            lambda r=rec: check_records_not_managed(pats, [r]), RefusedError, f"record {rec!r}"
        )


def main(argv: list[str] | None = None) -> int:
    """Run `--self-test` when requested."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--self-test", action="store_true", help="run table-driven unit tests")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test_builtin_managers()
        self_test_extends_and_wanted()
        self_test_snapshot_compare()
        self_test_docker_captures()
        print("ok   renovate_managers self-test")
    return 0


if __name__ == "__main__":
    sys.exit(main())
