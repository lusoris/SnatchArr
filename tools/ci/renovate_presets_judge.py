# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Resolve and judge Renovate presets against Docker release-age coverage rules."""

from __future__ import annotations

import re
from collections import Counter
from dataclasses import dataclass, field

from renovate_managers import (
    is_exact_file_pattern,
    is_non_docker_manager,
    parse_extends,
    parse_str_list,
)
from renovate_presets_net import HORISTES_REF
from renovate_regex import ConfigError, RefusedError, ascii_lower, extract_named_groups
from renovate_rules import (
    RULE_REDIRECT_KEYS,
    TOP_REDIRECT_KEYS,
    UNSUPPORTED_OVERRIDES,
    is_age_disabled,
)

MAX_PRESETS = 4096

MAX_CONFIG_NODES = 1 << 20

MAX_RULE_STEPS = MAX_PRESETS * 8

LITERAL_MATCHER_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")

IGNORE_SEGMENT_RE = re.compile(r"^\*\*/([A-Za-z0-9._-]+)/\*\*$")

NON_DOCKER_DEP_TYPES = frozenset({"github-runner", "php2shell-runtime"})

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


def is_str_list(value: object) -> bool:
    """Report whether VALUE is a `list` whose elements are all `str`."""
    if not isinstance(value, list):
        return False
    for item in value:
        if not isinstance(item, str):
            return False
    return True


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


def manager_matcher_values(
    rule: dict[str, object],
    union_matchers: dict[str, tuple[str, ...]],
    state: ClosureState,
) -> list[str] | None:
    """Return literal manager matchers after applying the measured migration map."""
    managers = literal_matcher_values(rule, union_matchers, "matchManagers")
    if managers is None:
        return None
    renames = state.dump.manager_renames["matchManagers"]
    return [renames.get(manager, manager) for manager in managers]


def rule_exclusion_reason(
    rule: dict[str, object],
    union_matchers: dict[str, tuple[str, ...]],
    state: ClosureState,
) -> str | None:
    """Return the Docker-exclusion reason (`datasource`, `manager`, `depType`) or `None` (3d)."""
    datasources = literal_matcher_values(rule, union_matchers, "matchDatasources")
    if datasources is not None and all(ascii_lower(ds) != "docker" for ds in datasources):
        return "datasource"

    managers = manager_matcher_values(rule, union_matchers, state)
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


def judge_top_release_age(preset_ref: str, body: dict[str, object], repo: RepoContext) -> None:
    """Validate a preset release wait unless the repository overrides its value."""
    if "minimumReleaseAge" not in body:
        return
    age = body["minimumReleaseAge"]
    if age is not None and not isinstance(age, str):
        raise ConfigError(f"{preset_ref}: top-level minimumReleaseAge must be string or null")
    if not repo.has_top_minimum_release_age and is_age_disabled(age):
        raise RefusedError(
            f"{preset_ref}: top-level minimumReleaseAge {age!r} disables the release wait"
        )


def judge_top_preset(preset_ref: str, body: dict[str, object], state: ClosureState) -> tuple[str, ...]:
    """Judge one top-context preset body against rules 3a–3f and return its `extends` list."""
    check_nested_directives(body, preset_ref, allow_rule_extends=True)
    check_top_prohibited_keys(preset_ref, body, state)
    judge_top_release_age(preset_ref, body, state.repo)
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


def run_self_test() -> None:
    """Verify rule merges retain ordered matcher unions and reject nonliteral exclusions."""
    empty = merge_rule_parts([])
    if empty.merged or empty.union_matchers:
        raise AssertionError("empty rule merge mismatch")
    merged = merge_rule_parts([
        ({"matchDatasources": ["docker"]}, {}),
        ({"matchDatasources": ["npm"], "enabled": False}, {}),
    ])
    values = literal_matcher_values(merged.merged, merged.union_matchers, "matchDatasources")
    if values != ["npm", "docker"] or merged.merged.get("enabled") is not False:
        raise AssertionError("rule matcher union lost an inherited Docker datasource")
    for value in ([], ["/docker/"], ["__proto__"], [3]):
        if literal_matcher_values({"matchManagers": value}, {}, "matchManagers") is not None:
            raise AssertionError("nonliteral matcher allowed an exclusion")
