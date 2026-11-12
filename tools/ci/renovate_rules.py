# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Pure Renovate 44.234.0 Docker release-age coverage evaluator.

Coordinates `renovate_regex.py` (RE2 matchers and `custom.regex` compilation) and
`renovate_managers.py` (`enabledManagers`, `customManagers` capture extraction, `extends` presets
and `record`-file checks) with top-level/`packageRules` config allow-lists, zero-duration
`minimumReleaseAge` parsing (`util/pretty-time.js` + `ms`), `getDepFromImageRef` `depName`
normalization (`lib/modules/manager/dockerfile/extract.ts`), `overridePackageName` templates and
ordered `packageRules` state evaluation over pins and captures without file I/O.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections.abc import Iterable, Mapping
from dataclasses import dataclass
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from renovate_managers import (
    DockerManagerSpec,
    ManagerFilePattern,
    check_records_not_builtin,
    check_records_not_managed,
    extract_docker_captures,
    first_unmodeled_preset,
    no_duplicate_keys,
    parse_custom_managers,
    parse_enabled_managers,
    parse_extends,
    refuse_unknown_keys,
)
from renovate_regex import (
    CompiledPattern,
    ConfigError,
    RefusedError,
    check_input_domain,
    compile_pattern_list,
    compile_re2_body,
    conjoin_three_valued,
    expect_failure,
    match_compiled,
)

__all__ = [
    "ConfigError", "CoverageRule", "JudgeResult", "ParsedConfig", "RefusedError", "RuleState",
    "docker_dep_name", "is_age_disabled", "is_dated", "is_name_covered", "is_pin_covered",
    "judge", "judge_full", "parse_config",
]

DATED_HOSTS = frozenset({"docker.io", "index.docker.io", "registry-1.docker.io"})
SUPPORTED_MATCHERS = frozenset({"matchDatasources", "matchDepNames", "matchPackageNames"})
UNSUPPORTED_OVERRIDES = frozenset({"overrideDepName", "overrideDatasource"})
MODELED_RULE_KEYS = SUPPORTED_MATCHERS | frozenset({
    "minimumReleaseAge", "minimumReleaseAgeBehaviour", "overridePackageName",
})
# `enabled` stays inert: the check answers whether the age wait holds a pin back once Renovate
# looks it up. `enabled: false` skips the lookup on purpose (an opt-out, not a silent wait), so it
# never counts as coverage; a pin only a disable rule matches is still refused. A `matchFileNames`
# disable (the praetor-managed files rule in renovate.json) may match any pin here, so refusing a
# matching disable would refuse the live config.
INERT_RULE_KEYS = frozenset({
    "allowedVersions", "automerge", "description", "enabled", "groupName", "labels",
})
RULE_REDIRECT_KEYS = (
    "registryUrls", "defaultRegistryUrls", "registryAliases", "registryUrlTemplate",
)
ALLOWED_RULE_KEYS = (
    MODELED_RULE_KEYS | UNSUPPORTED_OVERRIDES | INERT_RULE_KEYS | frozenset(RULE_REDIRECT_KEYS)
)
TOP_REDIRECT_KEYS = ("registryAliases", "registryUrls", "defaultRegistryUrls")
ALLOWED_TOP_KEYS = frozenset(TOP_REDIRECT_KEYS) | frozenset({
    "$schema", "customManagers", "enabledManagers", "extends", "lockFileMaintenance",
    "minimumReleaseAge", "minimumReleaseAgeBehaviour", "packageRules", "platformCommit",
    "postUpdateOptions", "schedule", "timezone", "vulnerabilityAlerts",
})
ALLOWED_VULN_KEYS = frozenset({
    "enabled", "groupName", "minimumReleaseAge", "minimumReleaseAgeBehaviour", "schedule",
})
ALLOWED_LOCK_KEYS = frozenset({"automerge", "enabled", "schedule"})
DOCKER_DEP_PREFIXES = ("amd64", "arm64", "library")
QUAY_DEP_RE = re.compile(r"^quay\.io(?::[1-9][0-9]{0,4})?", re.IGNORECASE | re.ASCII)
REDIRECT_SUFFIX = "is not modeled: it can redirect a Docker lookup to an undated registry"

MAX_STATES = 64
MAX_CONFIG_NODES = 1 << 20
TEMPLATE_TRIPLE = re.compile(r"^\{\{\{\s*replace\s+'([^']*)'\s+'([^']*)'\s+depName\s*\}\}\}$")
TEMPLATE_DOUBLE = re.compile(r"^\{\{\s*replace\s+'([^']*)'\s+'([^']*)'\s+depName\s*\}\}$")
ZERO_AGE_RE = re.compile(
    r"\s*-?0*\.?0+\s*(?:years?|yrs?|y|months?|weeks?|w|days?|d|hours?|hrs?|h|"
    r"minutes?|mins?|m|seconds?|secs?|s|milliseconds?|msecs?|ms)?\s*",
    re.IGNORECASE | re.ASCII,
)

# Implicit alias, no `type` statement: the hook floor (.config/lefthook/python.sh) is Python 3.10.
OverrideSpec = tuple[re.Pattern[str], str] | str | None


@dataclass(frozen=True)
class CoverageRule:
    """One `packageRules` entry that affects Docker release-age coverage or unsupported overrides."""

    index: int
    datasources: tuple[CompiledPattern, ...] | None
    dep_patterns: tuple[CompiledPattern, ...] | None
    pkg_patterns: tuple[CompiledPattern, ...] | None
    has_extra_matcher: bool
    has_unsupported_override: bool
    has_age: bool
    age: str | None
    has_behaviour: bool
    behaviour: str | None
    has_override: bool
    override: OverrideSpec

    @property
    def has_coverage_key(self) -> bool:
        """Report whether this rule modifies release-age settings or `packageName`."""
        return self.has_age or self.has_behaviour or self.has_override


@dataclass(frozen=True)
class ParsedConfig:
    """Validated `renovate.json` state used to judge pins, record files and captures."""

    age_null: bool
    behaviour_optional: bool
    rules: tuple[CoverageRule, ...]
    manager_patterns: tuple[ManagerFilePattern, ...]
    docker_managers: tuple[DockerManagerSpec, ...] = ()
    enabled_managers: tuple[str, ...] | None = None
    extends: tuple[str, ...] = ()


@dataclass(frozen=True)
class RuleState:
    """Accumulated `(age_null, behaviour_optional, package_name)` state for one dependency."""

    age_null: bool = False
    behaviour_optional: bool = False
    package_name: str | None = None

    def is_covered(self) -> bool:
        """Report whether Renovate can propose updates in this accumulated state."""
        return self.age_null or self.behaviour_optional or is_dated(self.package_name)


@dataclass(frozen=True)
class JudgeResult:
    """Outcome of judging live pins, record pins and `custom.regex` captures."""

    uncovered: tuple[str, ...]
    judged_pins: tuple[str, ...]
    unmodeled_preset: str | None
    uncovered_captures: dict[str, tuple[str, ...]]


def registry_host(name: str) -> str | None:
    """Return the registry host of Docker reference NAME, or None when NAME has no host."""
    first, sep, rest = name.partition("/")
    if sep and rest and ("." in first or ":" in first or first == "localhost"):
        return first
    return None


def is_dated(name: str | None) -> bool:
    """Report whether Renovate's Docker datasource dates NAME via Docker Hub (case-sensitive)."""
    if not name or name.startswith("/") or name.endswith("/") or any(c.isspace() for c in name):
        return False
    host = registry_host(name)
    return (host is None or host in DATED_HOSTS) and name == name.lower()


def docker_dep_name(name: str) -> str:
    """Normalize Docker `depName` per Renovate 44.234.0 `dockerfile/extract.ts` `getDepFromImageRef`."""
    dep_name = name
    for prefix in DOCKER_DEP_PREFIXES:
        dep_name = dep_name.removeprefix(f"{prefix}/")
    if QUAY_DEP_RE.search(dep_name):
        dep_name = QUAY_DEP_RE.sub("quay.io", dep_name, count=1)
    return dep_name


def is_age_disabled(age: str | None) -> bool:
    """Report whether `minimumReleaseAge` value AGE disables the release wait in Renovate."""
    return age is None or age == "" or ZERO_AGE_RE.fullmatch(age) is not None


def parse_optional_patterns(
    rule: dict[str, object], key: str, index: int
) -> tuple[CompiledPattern, ...] | None:
    """Compile optional matcher list KEY on `packageRules[INDEX]`."""
    if key not in rule:
        return None
    return compile_pattern_list(rule[key], f"packageRules[{index}].{key}")


def parse_optional_str(obj: dict[str, object], key: str, label: str) -> tuple[bool, str | None]:
    """Return `(present, value)` for string-or-null option KEY on OBJ."""
    if key not in obj:
        return False, None
    value = obj[key]
    if value is not None and not isinstance(value, str):
        raise ConfigError(f"renovate.json: {label}.{key} must be string or null")
    return True, value


def check_json_bound(data: object) -> None:
    """Refuse JSON trees exceeding `MAX_CONFIG_NODES` values."""
    stack: list[object] = [data]
    for _ in range(MAX_CONFIG_NODES):
        if not stack:
            return
        node = stack.pop()
        if isinstance(node, dict):
            stack.extend(node.values())
        elif isinstance(node, list):
            stack.extend(node)
    raise ConfigError(f"renovate.json: more than {MAX_CONFIG_NODES} JSON values")


def is_redirect_nonempty(obj: dict[str, object], key: str, where: str) -> bool:
    """Validate redirect option KEY on OBJ and report whether its value is non-empty."""
    if key not in obj or obj[key] is None:
        return False
    val = obj[key]
    if key == "registryAliases":
        if not isinstance(val, dict):
            raise ConfigError(f"renovate.json: {where}.{key} must be an object or null")
        return bool(val)
    if key == "registryUrlTemplate":
        if not isinstance(val, str):
            raise ConfigError(f"renovate.json: {where}.{key} must be a string or null")
        return bool(val)
    if not isinstance(val, list) or not all(isinstance(item, str) for item in val):
        raise ConfigError(f"renovate.json: {where}.{key} must be a list of strings or null")
    return bool(val)


def check_redirect_keys(
    obj: dict[str, object], keys: tuple[str, ...], where: str, *, enforce: bool = True
) -> None:
    """Validate types of redirect KEYS on OBJ; refuse non-empty values when ENFORCE is True."""
    for key in keys:
        nonempty = is_redirect_nonempty(obj, key, where)
        if enforce and nonempty:
            raise RefusedError(f"{where}: {key} {REDIRECT_SUFFIX}")


def validate_sub_object(data: dict[str, object], key: str, allowed: frozenset[str]) -> None:
    """Validate optional nested config object KEY against ALLOWED keys."""
    if key not in data or data[key] is None:
        return
    sub = data[key]
    if not isinstance(sub, dict):
        raise ConfigError(f"renovate.json: {key} must be an object")
    refuse_unknown_keys(sub, allowed, key)


def validate_top_level(data: dict[str, object]) -> None:
    """Validate top-level `renovate.json` keys and refuse non-empty redirect options."""
    refuse_unknown_keys(data, ALLOWED_TOP_KEYS, "top level")
    check_redirect_keys(data, TOP_REDIRECT_KEYS, "top level")


def parse_override_spec(raw: str | None, index: int) -> OverrideSpec:
    """Validate `overridePackageName` on `packageRules[INDEX]` as literal or safe `replace`."""
    if raw is None:
        return None
    if "{" not in raw and "}" not in raw:
        return raw
    match = TEMPLATE_TRIPLE.match(raw) or TEMPLATE_DOUBLE.match(raw)
    if match is None:
        raise RefusedError(
            f"packageRules[{index}]: unsupported template in overridePackageName: {raw!r}"
        )
    find_str, replacement = match.group(1), match.group(2)
    if "$" in replacement or "\\" in replacement:
        raise RefusedError(
            f"packageRules[{index}]: unsupported template replacement in overridePackageName: {raw!r}"
        )
    compiled = compile_re2_body(find_str, find_str, 0)
    return compiled, replacement


def apply_override(spec: OverrideSpec, dep_name: str) -> str | None:
    """Evaluate compiled `overridePackageName` SPEC against original DEP_NAME."""
    if spec is None or isinstance(spec, str):
        return spec
    pattern, replacement = spec
    return pattern.sub(replacement, dep_name)


def parse_rule(raw: object, index: int) -> CoverageRule | None:
    """Validate `packageRules[INDEX]`; return a `CoverageRule` when relevant to Docker age."""
    label = f"packageRules[{index}]"
    if not isinstance(raw, dict):
        raise ConfigError(f"renovate.json: {label} must be an object")
    refuse_unknown_keys(raw, ALLOWED_RULE_KEYS, label, allow_extra_matchers=True)
    datasources = parse_optional_patterns(raw, "matchDatasources", index)
    dep_patterns = parse_optional_patterns(raw, "matchDepNames", index)
    pkg_patterns = parse_optional_patterns(raw, "matchPackageNames", index)
    not_docker = datasources is not None and match_compiled("docker", datasources) is False
    check_redirect_keys(raw, RULE_REDIRECT_KEYS, label, enforce=not not_docker)
    has_age, age = parse_optional_str(raw, "minimumReleaseAge", label)
    has_beh, beh = parse_optional_str(raw, "minimumReleaseAgeBehaviour", label)
    has_ovr, ovr_raw = parse_optional_str(raw, "overridePackageName", label)
    # lib/util/package-rules/index.ts applies overridePackageName only when isString(...):
    # null leaves packageName unchanged, "" sets it empty (an invalid name, never dated).
    has_ovr = bool(has_ovr and ovr_raw is not None)
    bad_ovr = bool(UNSUPPORTED_OVERRIDES.intersection(raw))
    if not_docker or not any((has_age, has_beh, has_ovr, bad_ovr)):
        return None
    matchers = {key for key in raw if key.startswith(("match", "exclude"))}
    override = parse_override_spec(ovr_raw, index) if has_ovr else None
    return CoverageRule(
        index=index,
        datasources=datasources,
        dep_patterns=dep_patterns,
        pkg_patterns=pkg_patterns,
        has_extra_matcher=bool(matchers - SUPPORTED_MATCHERS),
        has_unsupported_override=bad_ovr,
        has_age=has_age,
        age=age,
        has_behaviour=has_beh,
        behaviour=beh,
        has_override=has_ovr,
        override=override,
    )


def parse_package_rules(data: dict[str, object]) -> tuple[CoverageRule, ...]:
    """Validate `packageRules` on DATA and return Docker-relevant `CoverageRule` entries."""
    raw_rules = data.get("packageRules", [])
    if not isinstance(raw_rules, list):
        raise ConfigError("renovate.json: packageRules must be a list")
    rules: list[CoverageRule] = []
    for index, raw_rule in enumerate(raw_rules):
        parsed = parse_rule(raw_rule, index)
        if parsed is not None:
            rules.append(parsed)
    return tuple(rules)


def parse_config(config: str | dict[str, object]) -> ParsedConfig:
    """Parse and validate `renovate.json` text or dict into a `ParsedConfig`."""
    if isinstance(config, str):
        try:
            data = json.loads(config, object_pairs_hook=no_duplicate_keys)
        except json.JSONDecodeError as exc:
            raise ConfigError(f"renovate.json: {exc}") from exc
    else:
        data = config
    if not isinstance(data, dict):
        raise ConfigError("renovate.json: top-level JSON value must be an object")
    check_json_bound(data)
    validate_top_level(data)
    validate_sub_object(data, "vulnerabilityAlerts", ALLOWED_VULN_KEYS)
    validate_sub_object(data, "lockFileMaintenance", ALLOWED_LOCK_KEYS)
    extends = parse_extends(data)
    enabled_managers = parse_enabled_managers(data)
    manager_patterns, docker_managers = parse_custom_managers(data)
    has_age, top_age = parse_optional_str(data, "minimumReleaseAge", "top-level")
    _, top_beh = parse_optional_str(data, "minimumReleaseAgeBehaviour", "top-level")
    rules = parse_package_rules(data)
    return ParsedConfig(
        age_null=has_age and is_age_disabled(top_age),
        behaviour_optional=(top_beh == "timestamp-optional"),
        rules=rules,
        manager_patterns=manager_patterns,
        docker_managers=docker_managers,
        enabled_managers=enabled_managers,
        extends=extends,
    )


def match_pkg_patterns(
    pkg_patterns: tuple[CompiledPattern, ...] | None, pkg_name: str | None
) -> bool | None:
    """Evaluate optional `matchPackageNames` PKG_PATTERNS against accumulated PKG_NAME."""
    if pkg_patterns is None:
        return True
    if pkg_name is None:
        return False
    return match_compiled(pkg_name, pkg_patterns)


def rule_applies(rule: CoverageRule, dep_name: str, pkg_name: str | None) -> bool | None:
    """Evaluate whether RULE applies (`True`, `False` or `None`) for DEP_NAME and PKG_NAME."""
    ds = True if rule.datasources is None else match_compiled("docker", rule.datasources)
    dep = True if rule.dep_patterns is None else match_compiled(dep_name, rule.dep_patterns)
    pkg = match_pkg_patterns(rule.pkg_patterns, pkg_name)
    extra = None if rule.has_extra_matcher else True
    return conjoin_three_valued((ds, dep, pkg, extra))


def apply_rule_to_state(state: RuleState, rule: CoverageRule, dep_name: str) -> RuleState:
    """Return a new `RuleState` after applying definite coverage changes of RULE."""
    age_null = is_age_disabled(rule.age) if rule.has_age else state.age_null
    beh_opt = (
        (rule.behaviour == "timestamp-optional") if rule.has_behaviour else state.behaviour_optional
    )
    pkg_name = apply_override(rule.override, dep_name) if rule.has_override else state.package_name
    return RuleState(age_null=age_null, behaviour_optional=beh_opt, package_name=pkg_name)


def step_states(
    states: set[RuleState], forked: list[int], rule: CoverageRule, dep_name: str
) -> set[RuleState]:
    """Advance STATES across RULE for DEP_NAME, recording forked rule indices and bounds."""
    next_states: set[RuleState] = set()
    did_fork = False
    for state in states:
        applies = rule_applies(rule, dep_name, state.package_name)
        if applies is False:
            next_states.add(state)
            continue
        if rule.has_unsupported_override:
            raise RefusedError(
                f"packageRules[{rule.index}]: overrideDepName or overrideDatasource is not supported"
            )
        if not rule.has_coverage_key:
            next_states.add(state)
            continue
        applied = apply_rule_to_state(state, rule, dep_name)
        if applies is True:
            next_states.add(applied)
        else:
            next_states.add(state)
            next_states.add(applied)
            did_fork = did_fork or (applied != state)
    if did_fork:
        forked.append(rule.index)
    if len(next_states) > MAX_STATES:
        raise RefusedError(
            f"packageRules[{rule.index}]: state set exceeded {MAX_STATES} entries for {dep_name}"
        )
    return next_states


def is_name_covered(
    dep_name: str,
    package_name: str,
    age_null: bool,
    beh_opt: bool,
    rules: Iterable[CoverageRule],
) -> bool:
    """Walk RULES over deduplicated `RuleState`s for `(DEP_NAME, PACKAGE_NAME)`; fail closed."""
    states: set[RuleState] = {
        RuleState(age_null=age_null, behaviour_optional=beh_opt, package_name=package_name)
    }
    forked: list[int] = []
    for rule in rules:
        states = step_states(states, forked, rule, dep_name)
    outcomes = {state.is_covered() for state in states}
    if outcomes == {True}:
        return True
    if outcomes == {False}:
        return False
    where = ", ".join(f"packageRules[{idx}]" for idx in forked)
    raise RefusedError(
        f"{where}: ambiguous rule applicability leaves coverage mixed for {dep_name}"
    )


def is_pin_covered(name: str, parsed: ParsedConfig) -> bool:
    """Report whether every Renovate extraction variant of NAME is covered under PARSED."""
    norm = docker_dep_name(name)
    variants = ((name, name),) if norm == name else ((name, name), (norm, name))
    results = [
        is_name_covered(dep, pkg, parsed.age_null, parsed.behaviour_optional, parsed.rules)
        for dep, pkg in variants
    ]
    return all(results)


def judge_captures(
    parsed: ParsedConfig, captures: Mapping[str, list[str]]
) -> dict[str, tuple[str, ...]]:
    """Return `{depName: (loc, ...)}` for captured Docker `depName`s left uncovered."""
    uncovered: dict[str, tuple[str, ...]] = {}
    for cap in sorted(captures):
        if not is_name_covered(cap, cap, parsed.age_null, parsed.behaviour_optional, parsed.rules):
            uncovered[cap] = tuple(captures[cap])
    return uncovered


def judge_full(
    config: str | dict[str, object],
    names: Iterable[str],
    records: Iterable[str] = (),
    *,
    record_names: Iterable[str] = (),
    searched: Iterable[str] = (),
    files: Mapping[str, bytes] | None = None,
) -> JudgeResult:
    """Evaluate release-age coverage over live pins, record pins and `custom.regex` captures."""
    live_list, record_list = sorted(set(names)), sorted(set(records))
    rec_name_list = sorted(set(record_names))
    check_input_domain(live_list, "image name")
    check_input_domain(record_list, "record path")
    check_input_domain(rec_name_list, "image name")
    parsed = parse_config(config)
    unmodeled = first_unmodeled_preset(parsed.extends)
    if unmodeled is None:
        if record_list:
            check_records_not_builtin(parsed.enabled_managers, record_list)
            check_records_not_managed(parsed.manager_patterns, record_list)
        judged_pins = tuple(live_list)
    else:
        judged_pins = tuple(sorted(set(live_list) | set(rec_name_list)))
    captures = extract_docker_captures(parsed.docker_managers, searched, files or {})
    uncovered_caps = judge_captures(parsed, captures)
    uncovered_pins = {name for name in judged_pins if not is_pin_covered(name, parsed)}
    return JudgeResult(
        uncovered=tuple(sorted(uncovered_pins | set(uncovered_caps))),
        judged_pins=judged_pins,
        unmodeled_preset=unmodeled,
        uncovered_captures=uncovered_caps,
    )


def judge(
    config: str | dict[str, object],
    names: Iterable[str],
    records: Iterable[str] = (),
    *,
    record_names: Iterable[str] = (),
    searched: Iterable[str] = (),
    files: Mapping[str, bytes] | None = None,
) -> list[str]:
    """Return sorted image and capture names that CONFIG leaves without release-age coverage."""
    res = judge_full(
        config, names, records, record_names=record_names, searched=searched, files=files
    )
    return list(res.uncovered)


def self_test_rule_state_walk() -> None:
    """Run assertions for top-level settings, accumulated overrides and `MAX_STATES`."""
    pin = "ghcr.io/o/app"
    if judge({"minimumReleaseAge": None}, [pin]) != []:
        raise AssertionError("top-level minimumReleaseAge null should cover")
    if judge({"packageRules": [{"minimumReleaseAge": None}]}, [pin]) != []:
        raise AssertionError("matcher-free rule should cover")
    chained = [
        {"matchDepNames": [pin], "overridePackageName": "ghcr.io/o/next"},
        {"matchPackageNames": ["ghcr.io/o/next"], "minimumReleaseAge": None},
    ]
    if judge({"packageRules": chained}, [pin]) != []:
        raise AssertionError("accumulated packageName should match later rule")
    too_many = [
        {"matchFileNames": [f"{i}.yml"], "overridePackageName": f"ghcr.io/o/p{i}"}
        for i in range(64)
    ]
    expect_failure(lambda: judge({"packageRules": too_many}, [pin]), RefusedError, ">64 states")


def self_test_zero_age() -> None:
    """Run table-driven assertions for `is_age_disabled`."""
    disabled = (None, "", "0", "0 days", "0d", " 0 Days ", "0.0 hours", "0 months", "0M")
    for val in disabled:
        if not is_age_disabled(val):
            raise AssertionError(f"is_age_disabled({val!r}) was False, want True")
    for val in ("1 day", "3 days", "0 days 1 hour", "zero", "-1 days"):
        if is_age_disabled(val):
            raise AssertionError(f"is_age_disabled({val!r}) was True, want False")


def self_test_override_and_aliases() -> None:
    """Assert null overridePackageName is a no-op, unknown keys and redirects fail closed."""
    pin = "ghcr.io/o/app"
    to_hub = {"matchDepNames": [pin], "overridePackageName": "docker.io/o/app"}
    null_ovr = {"matchDepNames": [pin], "overridePackageName": None}
    if judge({"packageRules": [to_hub, null_ovr]}, [pin]) != []:
        raise AssertionError("null overridePackageName must keep the earlier packageName")
    if judge({"packageRules": [{**to_hub, "overridePackageName": ""}]}, [pin]) != [pin]:
        raise AssertionError('empty overridePackageName "" must leave the pin uncovered')
    if judge({"registryAliases": {}, "registryUrls": [], "defaultRegistryUrls": None}, []) != []:
        raise AssertionError("empty or null top-level redirect keys must pass")
    npm_redir = {"matchDatasources": ["npm"], "registryUrls": ["https://registry.npmjs.org"]}
    if judge({"packageRules": [npm_redir]}, []) != []:
        raise AssertionError("non-Docker packageRule redirect must pass")
    refused_configs = (
        {"dockerfile": {"registryAliases": None}},
        {"registryAliases": {"docker.io": "mirror.gcr.io"}},
        {"registryUrls": ["https://ghcr.io"]},
        {"packageRules": [{"registryAliases": {"docker.io": "mirror.gcr.io"}}]},
        {"packageRules": [{"registryUrls": ["https://lscr.io"]}]},
    )
    for cfg in refused_configs:
        expect_failure(lambda c=cfg: judge(c, [pin]), RefusedError, repr(cfg))


def self_test_managers_and_records() -> None:
    """Assert `judge` refuses record files hit by `customManagers` (including `./` paths)."""
    mgr_exact = {
        "enabledManagers": ["custom.regex"],
        "customManagers": [{
            "customType": "regex",
            "managerFilePatterns": ["docs/adr.md"],
            "matchStrings": ["(?<depName>a):(?<currentValue>1)"],
            "datasourceTemplate": "docker",
        }],
    }
    if judge(mgr_exact, [], records=["other.md"], searched=["docs/adr.md"]) != []:
        raise AssertionError("unrelated record file should pass")
    dotted = {
        "enabledManagers": ["custom.regex"],
        "customManagers": [{**mgr_exact["customManagers"][0], "managerFilePatterns": ["./r.md"]}],
    }
    expect_failure(lambda: judge(dotted, [], records=["r.md"]), RefusedError, "dotted record")


def self_test_docker_dep_name() -> None:
    """Run table-driven assertions for `docker_dep_name` normalization."""
    cases = (
        ("library/x", "x"), ("amd64/x", "x"), ("arm64/library/x", "x"),
        ("library/amd64/x", "amd64/x"), ("quay.io:5000/a/b", "quay.io/a/b"),
        ("QUAY.IO:443/a", "quay.io/a"), ("Quay.io/a", "quay.io/a"),
        ("quay.io:0123/a", "quay.io:0123/a"), ("quay.io:123456/a", "quay.io6/a"),
        ("ghcr.io/library/x", "ghcr.io/library/x"), ("docker.io/library/x", "docker.io/library/x"),
        ("quayio/x", "quayio/x"), ("ghcr.io/a/b", "ghcr.io/a/b"),
    )
    for raw, want in cases:
        got = docker_dep_name(raw)
        if got != want:
            raise AssertionError(f"docker_dep_name({raw!r}) = {got!r}, want {want!r}")


def self_test_groups_and_inputs() -> None:
    """Assert hidden-group over-approximation and the printable-ASCII input domain."""
    base = {"customType": "regex", "managerFilePatterns": ["x.yml"], "datasourceTemplate": "docker"}
    for hidden in (r"\Q[\E(?<packageName>x)", r"[(?<packageName>x)]", r"\(?P<registryUrl>x)"):
        expect_failure(
            lambda h=hidden: judge({"customManagers": [{**base, "matchStrings": [h]}]}, []),
            RefusedError,
            hidden,
        )
    for all_on in ({}, {"enabledManagers": None}, {"enabledManagers": []}):
        expect_failure(
            lambda c=all_on: judge(c, ["ghcr.io/a/b"], records=["tools/e2e/x.yaml"]),
            RefusedError,
            f"record file under {all_on!r}",
        )
    only_regex = {"enabledManagers": ["custom.regex"]}
    if judge(only_regex, ["ghcr.io/a/b"], records=["tools/e2e/x.yaml"]) != ["ghcr.io/a/b"]:
        raise AssertionError("printable ASCII inputs must be judged")
    for name, rec in (("ghcr.io/a/b\n", "r.yml"), ("ghcr.io/a/b\v", "r.yml"), ("a", "réc.yml")):
        expect_failure(lambda n=name, r=rec: judge(only_regex, [n], [r]), RefusedError, repr(name))


def self_test_captures_and_presets() -> None:
    """Assert `judge_full` evaluates `custom.regex` captures and unmodeled-preset record pins."""
    ms = r"IMG=mirror\.gcr\.io/(?<depName>rhysd/actionlint):(?<currentValue>1)\n"
    cfg = {
        "enabledManagers": ["custom.regex"],
        "extends": ["customManagers:helmChartYamlAppVersions"],
        "customManagers": [{"customType": "regex", "datasourceTemplate": "docker",
                            "managerFilePatterns": ["tools/x.env"], "matchStrings": [ms]}],
        "packageRules": [{"matchPackageNames": ["rhysd/actionlint"],
                          "overridePackageName": "ghcr.io/rhysd/actionlint"}],
    }
    res = judge_full(
        cfg, [], ["charts/demo/Chart.yaml"],
        record_names=["ghcr.io/planted/undated"], searched=["tools/x.env"],
        files={"tools/x.env": b"IMG=mirror.gcr.io/rhysd/actionlint:1\n"},
    )
    if res.uncovered != ("ghcr.io/planted/undated", "rhysd/actionlint"):
        raise AssertionError(f"unexpected uncovered tuple: {res.uncovered!r}")
    if res.unmodeled_preset != "customManagers:helmChartYamlAppVersions":
        raise AssertionError(f"unexpected unmodeled_preset: {res.unmodeled_preset!r}")
    if res.uncovered_captures != {"rhysd/actionlint": ("customManagers[0] tools/x.env:1",)}:
        raise AssertionError(f"unexpected uncovered_captures: {res.uncovered_captures!r}")


def main(argv: list[str] | None = None) -> int:
    """Run `--self-test` when requested."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--self-test", action="store_true", help="run table-driven unit tests")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test_rule_state_walk()
        self_test_zero_age()
        self_test_override_and_aliases()
        self_test_managers_and_records()
        self_test_docker_dep_name()
        self_test_groups_and_inputs()
        self_test_captures_and_presets()
        print("ok   renovate_rules self-test")
    return 0


if __name__ == "__main__":
    sys.exit(main())
