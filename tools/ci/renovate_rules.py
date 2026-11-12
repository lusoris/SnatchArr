# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Pure Renovate 44.234.0 Docker release-age coverage evaluator.

Models `matchRegexOrGlobList` (`lib/util/string-match.ts`), two-barrier RE2 pattern compilation
(syntax allow-list in `re2_subset.py` plus parse-tree opcode/code/flag allow-list), config-key
allow-lists across top level, `packageRules`, `customManagers`, `vulnerabilityAlerts` and
`lockFileMaintenance`, zero-duration `minimumReleaseAge` parsing (`util/pretty-time.js` + `ms`),
`getDepFromImageRef` `depName` normalization (`lib/modules/manager/dockerfile/extract.ts`),
`overridePackageName` templates, `record`-file protection against `enabledManagers` built-in
`managerFilePatterns` and `customManagers`, and ordered `packageRules` state evaluation over all
live pins without file I/O or subprocess calls.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path

try:
    from re import _constants as re_constants  # type: ignore[attr-defined]
    from re import _parser as re_parser  # type: ignore[attr-defined]
except ImportError:  # Python 3.10
    import sre_constants as re_constants  # type: ignore[no-redef]
    import sre_parse as re_parser  # type: ignore[no-redef]

sys.path.insert(0, str(Path(__file__).resolve().parent))

from re2_subset import MAX_REPEAT_BOUND, SubsetError, check_re2_subset

DATED_HOSTS = frozenset({"docker.io", "index.docker.io", "registry-1.docker.io"})
SUPPORTED_MATCHERS = frozenset({"matchDatasources", "matchDepNames", "matchPackageNames"})
UNSUPPORTED_OVERRIDES = frozenset({"overrideDepName", "overrideDatasource"})
MODELED_RULE_KEYS = SUPPORTED_MATCHERS | frozenset({
    "minimumReleaseAge",
    "minimumReleaseAgeBehaviour",
    "overridePackageName",
})
# `enabled` stays inert: the check answers whether the age wait holds a pin back once Renovate
# looks it up. `enabled: false` skips the lookup on purpose (an opt-out, not a silent wait), so it
# never counts as coverage; a pin only a disable rule matches is still refused. A `matchFileNames`
# disable (the praetor-managed files rule in renovate.json) may match any pin here, so refusing a
# matching disable would refuse the live config.
INERT_RULE_KEYS = frozenset({
    "allowedVersions",
    "automerge",
    "description",
    "enabled",
    "groupName",
    "labels",
})
RULE_REDIRECT_KEYS = (
    "registryUrls",
    "defaultRegistryUrls",
    "registryAliases",
    "registryUrlTemplate",
)
ALLOWED_RULE_KEYS = (
    MODELED_RULE_KEYS | UNSUPPORTED_OVERRIDES | INERT_RULE_KEYS | frozenset(RULE_REDIRECT_KEYS)
)
TOP_REDIRECT_KEYS = ("registryAliases", "registryUrls", "defaultRegistryUrls")
ALLOWED_TOP_KEYS = frozenset(TOP_REDIRECT_KEYS) | frozenset({
    "$schema",
    "customManagers",
    "enabledManagers",
    "extends",
    "lockFileMaintenance",
    "minimumReleaseAge",
    "minimumReleaseAgeBehaviour",
    "packageRules",
    "platformCommit",
    "postUpdateOptions",
    "schedule",
    "timezone",
    "vulnerabilityAlerts",
})
ALLOWED_VULN_KEYS = frozenset({
    "enabled",
    "groupName",
    "minimumReleaseAge",
    "minimumReleaseAgeBehaviour",
    "schedule",
})
ALLOWED_LOCK_KEYS = frozenset({"automerge", "enabled", "schedule"})
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
DOCKER_ALLOWED_GROUPS = frozenset({"depName", "currentValue", "currentDigest"})
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
DOCKER_DEP_PREFIXES = ("amd64", "arm64", "library")
QUAY_DEP_RE = re.compile(r"^quay\.io(?::[1-9][0-9]{0,4})?", re.IGNORECASE | re.ASCII)
UNKNOWN_KEY_SUFFIX = (
    "classify it in tools/ci/renovate_rules.py after checking it cannot redirect a Docker "
    "lookup or change release age"
)
REDIRECT_SUFFIX = "is not modeled: it can redirect a Docker lookup to an undated registry"

GLOB_CHARS = frozenset("*?[]{}()\\")
NAMED_GROUP_RE = re.compile(r"\(\?P?<(?![=!])([^>]*)(>?)")
# Names and record paths the judge matches stay printable ASCII: there `$`, `\s` and `/i`
# behave the same in Python `re` (re.ASCII) and RE2 (trailing `\n`, `\v`, Unicode folding).
INPUT_MIN = 0x20
INPUT_MAX = 0x7E
ALLOWED_LEAF_OPS = frozenset({"LITERAL", "NOT_LITERAL", "ANY", "NEGATE"})
ALLOWED_AT_CODES = frozenset({
    "AT_BEGINNING",
    "AT_END",
    "AT_BOUNDARY",
    "AT_NON_BOUNDARY",
    "AT_BEGINNING_STRING",
})
ALLOWED_CATEGORIES = frozenset({
    "CATEGORY_DIGIT",
    "CATEGORY_NOT_DIGIT",
    "CATEGORY_SPACE",
    "CATEGORY_NOT_SPACE",
    "CATEGORY_WORD",
    "CATEGORY_NOT_WORD",
})
CODE_ALLOWLISTS: dict[str, frozenset[str]] = {
    "AT": ALLOWED_AT_CODES,
    "CATEGORY": ALLOWED_CATEGORIES,
}
REPEAT_OPS = frozenset({"MAX_REPEAT", "MIN_REPEAT"})

MAX_STATES = 64
MAX_PARSE_NODES = 10000
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


class ConfigError(Exception):
    """Raised when `renovate.json` or a matcher/regex pattern is malformed or non-RE2."""


class RefusedError(Exception):
    """Raised when a config key, customManager or `packageRule` fails closed."""


@dataclass(frozen=True)
class CompiledPattern:
    """One compiled entry from a Renovate `match*` pattern list."""

    negated: bool
    kind: str
    value: str | re.Pattern[str] | None = None


@dataclass(frozen=True)
class ManagerFilePattern:
    """One compiled `customManagers[i].managerFilePatterns` entry."""

    manager_index: int
    raw: str
    kind: str
    regex: re.Pattern[str] | None = None


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
    """Validated `renovate.json` state used to judge live image pins and record files."""

    age_null: bool
    behaviour_optional: bool
    rules: tuple[CoverageRule, ...]
    manager_patterns: tuple[ManagerFilePattern, ...]
    enabled_managers: tuple[str, ...] | None = None


@dataclass(frozen=True)
class RuleState:
    """Accumulated `(age_null, behaviour_optional, package_name)` state for one dependency."""

    age_null: bool = False
    behaviour_optional: bool = False
    package_name: str | None = None

    def is_covered(self) -> bool:
        """Report whether Renovate can propose updates in this accumulated state."""
        return self.age_null or self.behaviour_optional or is_dated(self.package_name)


def registry_host(name: str) -> str | None:
    """Return the registry host of Docker reference NAME, or None when NAME has no host."""
    first, sep, rest = name.partition("/")
    if not sep or not rest:
        return None
    if "." in first or ":" in first or first == "localhost":
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


def no_duplicate_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    """Construct a JSON object from PAIRS while refusing duplicate keys."""
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ConfigError(f"renovate.json: duplicate JSON key {key!r}")
        result[key] = value
    return result


def check_leaf_or_code(op_name: str, av: object, label: str) -> bool:
    """Validate leaf, `RANGE`, `AT` or `CATEGORY` node; return True when handled."""
    if op_name in ALLOWED_LEAF_OPS:
        return True
    if op_name == "RANGE":
        lo, hi = av  # type: ignore[misc]
        if lo > hi:
            raise ConfigError(
                f"renovate.json: invalid regex {label!r}: RANGE ({lo}, {hi}) out of order"
            )
        return True
    if op_name in CODE_ALLOWLISTS:
        code = str(av).upper()
        if code not in CODE_ALLOWLISTS[op_name]:
            raise ConfigError(
                f"renovate.json: invalid regex {label!r}: non-RE2 {op_name} code {code}"
            )
        return True
    return False


def check_repeat_node(
    op_name: str, av: tuple[int, object, object], label: str
) -> Iterable[tuple[object, object]]:
    """Validate `MAX_REPEAT`/`MIN_REPEAT` bounds <= 1000 (unless unbounded) and return child ops."""
    lo, hi, item = av
    hi_int = int(hi)  # type: ignore[call-overload]
    unbounded = hi == re_constants.MAXREPEAT
    if lo > MAX_REPEAT_BOUND or (not unbounded and (hi_int > MAX_REPEAT_BOUND or lo > hi_int)):
        raise ConfigError(
            f"renovate.json: invalid regex {label!r}: {op_name} bound ({lo}, {hi}) "
            f"exceeds {MAX_REPEAT_BOUND}"
        )
    return item.data  # type: ignore[attr-defined, no-any-return]


def check_subpattern_node(
    av: tuple[object, int, int, object], label: str
) -> Iterable[tuple[object, object]]:
    """Validate `SUBPATTERN` add/del flags are zero and return child ops."""
    _group, add_flags, del_flags, sub = av
    if add_flags != 0 or del_flags != 0:
        raise ConfigError(
            f"renovate.json: invalid regex {label!r}: SUBPATTERN flags "
            f"({add_flags}, {del_flags}) not allowed"
        )
    return sub.data  # type: ignore[attr-defined, no-any-return]


def check_parse_node(
    op_name: str,
    av: object,
    label: str,
    stack: list[Iterable[tuple[object, object]]],
) -> None:
    """Validate one `(op_name, av)` parse-tree node against the RE2 opcode allow-list."""
    if check_leaf_or_code(op_name, av, label):
        return
    if op_name == "IN":
        stack.append(av)  # type: ignore[arg-type]
        return
    if op_name == "BRANCH":
        for branch in av[1]:  # type: ignore[index]
            stack.append(branch.data)
        return
    if op_name == "SUBPATTERN":
        stack.append(check_subpattern_node(av, label))  # type: ignore[arg-type]
        return
    if op_name in REPEAT_OPS:
        stack.append(check_repeat_node(op_name, av, label))  # type: ignore[arg-type]
        return
    raise ConfigError(f"renovate.json: invalid regex {label!r}: non-RE2 parse op {op_name}")


def walk_re2_tree(parsed: object, label: str, expected_flags: int = re.ASCII) -> None:
    """Walk `re_parser.parse` output PARSED and allow only RE2-compatible ops, codes and flags."""
    state = parsed.state  # type: ignore[attr-defined]
    if (
        not (int(expected_flags) & int(re.ASCII))
        or int(state.flags) != int(expected_flags)
        or state.groupdict
    ):
        raise ConfigError(
            f"renovate.json: invalid regex {label!r}: inline flags or named groups "
            f"({state.flags!r}) not allowed"
        )
    stack: list[Iterable[tuple[object, object]]] = [parsed.data]  # type: ignore[attr-defined]
    steps = 0
    while stack:
        items = stack.pop()
        for op, av in items:
            steps += 1
            if steps > MAX_PARSE_NODES:
                raise ConfigError(f"renovate.json: regex {label!r} exceeds parse bound")
            check_parse_node(str(op).upper(), av, label, stack)


def compile_re2_body(body: str, label: str, flags: int = 0) -> re.Pattern[str]:
    """Compile regex BODY with `re.ASCII` after both RE2 barriers (syntax + parse tree)."""
    try:
        check_re2_subset(body, label)
    except SubsetError as exc:
        raise ConfigError(
            f"renovate.json: invalid regex {label!r}: {exc} "
            "(outside the RE2 subset modeled in tools/ci/re2_subset.py)"
        ) from exc
    full_flags = flags | re.ASCII
    try:
        parsed = re_parser.parse(body, full_flags)
    except re.error as exc:
        raise ConfigError(f"renovate.json: invalid regex {label!r}: {exc}") from exc
    walk_re2_tree(parsed, label, full_flags)
    try:
        return re.compile(body, full_flags)
    except re.error as exc:
        raise ConfigError(f"renovate.json: invalid regex {label!r}: {exc}") from exc


def compile_regex_literal(raw: str, literal: str) -> re.Pattern[str]:
    """Compile a `/re/` or `/re/i` regex literal (already stripped of optional leading `!`)."""
    if literal.endswith("/i") and len(literal) >= 3:
        return compile_re2_body(literal[1:-2], raw, re.IGNORECASE)
    if literal.endswith("/") and len(literal) >= 2:
        return compile_re2_body(literal[1:-1], raw, 0)
    raise ConfigError(f"renovate.json: malformed regex pattern {raw!r}")


def compile_single_pattern(pattern: str) -> CompiledPattern:
    """Compile one Renovate string matcher entry into a `CompiledPattern`."""
    if pattern == "*":
        return CompiledPattern(negated=False, kind="any")
    if pattern.startswith("!"):
        rest = pattern[1:]
        if rest.startswith("/"):
            return CompiledPattern(
                negated=True, kind="regex", value=compile_regex_literal(pattern, rest)
            )
        if not rest or rest.startswith(("!", "#")) or GLOB_CHARS.intersection(rest):
            return CompiledPattern(negated=True, kind="unknown")
        return CompiledPattern(negated=True, kind="exact", value=rest.lower())
    if pattern.startswith("/"):
        return CompiledPattern(
            negated=False, kind="regex", value=compile_regex_literal(pattern, pattern)
        )
    if pattern.startswith("#") or GLOB_CHARS.intersection(pattern):
        return CompiledPattern(negated=False, kind="unknown")
    return CompiledPattern(negated=False, kind="exact", value=pattern.lower())


def compile_pattern_list(raw: object, label: str) -> tuple[CompiledPattern, ...]:
    """Validate and compile a Renovate matcher list into `CompiledPattern` entries."""
    if not isinstance(raw, list) or not all(isinstance(item, str) for item in raw):
        raise ConfigError(f"renovate.json: {label} must be a list of strings")
    return tuple(compile_single_pattern(item) for item in raw)


def eval_pattern(value: str, entry: CompiledPattern) -> bool | None:
    """Evaluate one `CompiledPattern` against VALUE under Renovate `matchRegexOrGlob` rules."""
    if entry.kind == "any":
        return True
    if entry.kind == "unknown":
        return None
    if entry.kind == "exact":
        matched = value.lower() == entry.value
        return (not matched) if entry.negated else matched
    rx = entry.value
    if not isinstance(rx, re.Pattern):
        raise ConfigError("renovate.json: internal regex state error")
    matched = bool(rx.search(value))
    return (not matched) if entry.negated else matched


def combine_parts(pos_vals: list[bool | None], neg_vals: list[bool | None]) -> bool | None:
    """Combine positive and negative three-valued outcomes per `matchRegexOrGlobList`."""
    if not pos_vals or any(v is True for v in pos_vals):
        pos: bool | None = True
    elif any(v is None for v in pos_vals):
        pos = None
    else:
        pos = False

    if any(v is False for v in neg_vals):
        neg: bool | None = False
    elif any(v is None for v in neg_vals):
        neg = None
    else:
        neg = True

    if pos is False or neg is False:
        return False
    if pos is None or neg is None:
        return None
    return True


def match_compiled(value: str, patterns: tuple[CompiledPattern, ...]) -> bool | None:
    """Match VALUE against pre-compiled PATTERNS (`None` when globs prevent deciding)."""
    if not patterns:
        return False
    pos_vals = [eval_pattern(value, p) for p in patterns if not p.negated]
    neg_vals = [eval_pattern(value, p) for p in patterns if p.negated]
    return combine_parts(pos_vals, neg_vals)


def match_list(value: str, patterns: object) -> bool | None:
    """Match VALUE against Renovate pattern list PATTERNS, returning `True`, `False` or `None`."""
    return match_compiled(value, compile_pattern_list(patterns, "patterns"))


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


def parse_str_list(obj: dict[str, object], key: str, where: str) -> list[str]:
    """Validate optional list-of-strings option KEY on OBJ (default `[]`)."""
    raw = obj.get(key, [])
    if not isinstance(raw, list) or not all(isinstance(item, str) for item in raw):
        raise ConfigError(f"renovate.json: {where}.{key} must be a list of strings")
    return raw


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


def extract_named_groups(match_strings: list[str], where: str) -> set[str]:
    """Over-approximate the capture group names in MATCH_STRINGS.

    Every `(?<name>` or `(?P<name>` substring counts, even escaped, inside a class or inside
    RE2 `\\Q...\\E`, so no tokenizer has to mirror RE2: a false group only refuses more.
    """
    groups: set[str] = set()
    for pat in match_strings:
        for found in NAMED_GROUP_RE.finditer(pat):
            if not found.group(2):
                raise ConfigError(f"renovate.json: {where}: unclosed named group in matchStrings")
            groups.add(found.group(1))
    return groups


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
    if not raw or raw.startswith(("!", "#")) or GLOB_CHARS.intersection(raw):
        return ManagerFilePattern(manager_index=index, raw=raw, kind="glob")
    if any(seg in ("", ".", "..") for seg in raw.split("/")):
        # Only a plain relative path compares exactly; `./x`, `a//b` or `..` may match after
        # minimatch normalizes it, so it counts as a match like any glob.
        return ManagerFilePattern(manager_index=index, raw=raw, kind="glob")
    return ManagerFilePattern(manager_index=index, raw=raw, kind="exact")


def validate_custom_manager(raw: object, index: int) -> list[ManagerFilePattern]:
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
    if not is_non_docker_manager(raw, named_groups):
        check_docker_manager(raw, named_groups, where)
    return [compile_manager_file_pattern(index, pat) for pat in raw_pats]


def file_pattern_may_match(entry: ManagerFilePattern, record_path: str) -> bool:
    """Report whether `managerFilePatterns` ENTRY may match RECORD_PATH."""
    if entry.kind == "glob":
        return True
    if entry.kind == "exact":
        return entry.raw.lower() == record_path.lower()
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
                "BUILTIN_MANAGER_FILE_PATTERNS in tools/ci/renovate_rules.py"
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
    has_ovr = has_ovr and ovr_raw is not None
    bad_ovr = bool(UNSUPPORTED_OVERRIDES.intersection(raw))
    if not_docker or not (has_age or has_beh or has_ovr or bad_ovr):
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


def parse_custom_managers(data: dict[str, object]) -> tuple[ManagerFilePattern, ...]:
    """Validate `customManagers` on DATA and return compiled `ManagerFilePattern` entries."""
    raw_managers = data.get("customManagers", [])
    if not isinstance(raw_managers, list):
        raise ConfigError("renovate.json: customManagers must be a list")
    patterns: list[ManagerFilePattern] = []
    for index, raw_mgr in enumerate(raw_managers):
        patterns.extend(validate_custom_manager(raw_mgr, index))
    return tuple(patterns)


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
    enabled_managers = parse_enabled_managers(data)
    manager_patterns = parse_custom_managers(data)
    has_age, top_age = parse_optional_str(data, "minimumReleaseAge", "top-level")
    _, top_beh = parse_optional_str(data, "minimumReleaseAgeBehaviour", "top-level")
    rules = parse_package_rules(data)
    return ParsedConfig(
        age_null=has_age and is_age_disabled(top_age),
        behaviour_optional=(top_beh == "timestamp-optional"),
        rules=rules,
        manager_patterns=manager_patterns,
        enabled_managers=enabled_managers,
    )


def rule_applies(rule: CoverageRule, dep_name: str, pkg_name: str | None) -> bool | None:
    """Evaluate whether RULE applies (`True`, `False` or `None`) for DEP_NAME and PKG_NAME."""
    ds = True if rule.datasources is None else match_compiled("docker", rule.datasources)
    dep = True if rule.dep_patterns is None else match_compiled(dep_name, rule.dep_patterns)
    if rule.pkg_patterns is None:
        pkg: bool | None = True
    elif pkg_name is None:
        pkg = False
    else:
        pkg = match_compiled(pkg_name, rule.pkg_patterns)
    if ds is False or dep is False or pkg is False:
        return False
    if rule.has_extra_matcher or ds is None or dep is None or pkg is None:
        return None
    return True


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


def check_input_domain(values: Iterable[str], kind: str) -> None:
    """Refuse any VALUES entry outside printable ASCII, where `re` and RE2 can differ."""
    for value in values:
        if any(not INPUT_MIN <= ord(ch) <= INPUT_MAX for ch in value):
            raise RefusedError(
                f"{kind} {value!r} is outside printable ASCII: regex matching on it is not modeled"
            )


def judge(
    config: str | dict[str, object],
    names: Iterable[str],
    records: Iterable[str] = (),
) -> list[str]:
    """Return sorted image NAMES that CONFIG leaves without release-age coverage."""
    name_list, record_list = sorted(set(names)), sorted(set(records))
    check_input_domain(name_list, "image name")
    check_input_domain(record_list, "record path")
    parsed = parse_config(config)
    if record_list:
        check_records_not_builtin(parsed.enabled_managers, record_list)
        check_records_not_managed(parsed.manager_patterns, record_list)
    return [name for name in name_list if not is_pin_covered(name, parsed)]


def self_test_match_list() -> None:
    """Run table-driven assertions for `match_list` and RE2 / type validation."""
    cases: list[tuple[str, list[str], bool | None]] = [
        ("docker", [], False),
        ("docker", ["*"], True),
        ("docker", ["DOCKER"], True),
        ("docker", ["/^dock/"], True),
        ("docker", ["/^DOCKER$/i"], True),
        ("docker", ["/^DOCKER$/"], False),
        ("docker", ["!npm"], True),
        ("docker", ["!DOCKER"], False),
        ("docker", ["!/^npm$/"], True),
        ("docker", ["!/^DOCK/i"], False),
        ("ghcr.io/a/b", ["ghcr.io/a/*"], None),
        ("ghcr.io/a/b", ["ghcr.io/a/?"], None),
        ("ghcr.io/a/b", ["[2001:db8::1]/a"], None),
        ("ghcr.io/a/b", ["ghcr.io/{a,b}/b"], None),
        ("ghcr.io/a/b", ["ghcr.io/+(a|b)/b"], None),
        ("ghcr.io/a/b", ["ghcr.io\\a"], None),
        ("ghcr.io/a/b", ["#comment"], None),
        ("ghcr.io/a/b", ["!ghcr.io/a/*"], None),
        ("ghcr.io/a/b", ["ghcr.io/a/*", "ghcr.io/A/B"], True),
        ("ghcr.io/a/b", ["ghcr.io/a/*", "!ghcr.io/a/b"], False),
    ]
    for val, pats, want in cases:
        got = match_list(val, pats)
        if got is not want:
            raise AssertionError(f"match_list({val!r}, {pats!r}) = {got!r}, want {want!r}")
    for bad in ("docker", [1]):
        try:
            match_list("docker", bad)
        except ConfigError:
            continue
        raise AssertionError(f"match_list accepted invalid patterns {bad!r}")


def self_test_tree_walker() -> None:
    """Test `walk_re2_tree` directly on `re_parser.parse` output (bypassing barrier 1)."""
    in_bad = re_parser.parse("[ab]", re.ASCII)
    in_bad.data[0] = (re_constants.IN, [(re_constants.CATEGORY, re_constants.CATEGORY_LINEBREAK)])
    bad_trees = (
        (re_parser.parse(r"\Z", re.ASCII), r"/\Z/", int(re.ASCII)),
        (re_parser.parse(r"(?i)a", re.ASCII), "/(?i)a/", int(re.ASCII)),
        (re_parser.parse(r"(?i:a)", re.ASCII), "/(?i:a)/", int(re.ASCII)),
        (re_parser.parse(r"a{1001}", re.ASCII), "/a{1001}/", int(re.ASCII)),
        (re_parser.parse(r"(?=a)", re.ASCII), "/(?=a)/", int(re.ASCII)),
        (re_parser.parse(r"[\d]", 0), r"/[\d]/", int(re.ASCII)),
        (in_bad, "/[linebreak]/", int(re.ASCII)),
    )
    for tree, label, exp_flags in bad_trees:
        try:
            walk_re2_tree(tree, label, exp_flags)
        except ConfigError:
            continue
        raise AssertionError(f"walk_re2_tree accepted non-RE2 parse tree for {label}")


def self_test_re2_and_walk() -> None:
    """Run table-driven assertions for RE2 parity, templates and the rule state walk."""
    for bad_re in ("/(?=a)/", "/(?!a)/", "/(?<=a)/", "/(?<!a)/", "/(a)\\1/", "/(?>a+)/", "/a*+/"):
        try:
            match_list("a", [bad_re])
        except ConfigError:
            pass
        else:
            raise AssertionError(f"non-RE2 pattern {bad_re!r} was not refused")
    if match_list("\u0660", [r"/^\d$/"]) is not False:
        raise AssertionError("re.ASCII did not restrict \\d to ASCII digits")
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
    try:
        judge({"packageRules": too_many}, [pin])
    except RefusedError:
        return
    raise AssertionError(">64 states did not raise RefusedError")


def self_test_zero_age() -> None:
    """Run table-driven assertions for `is_age_disabled`."""
    disabled_cases: tuple[str | None, ...] = (
        None,
        "",
        "0",
        "0 days",
        "0d",
        " 0 Days ",
        "0.0 hours",
        "0 months",
        "0M",
    )
    for val in disabled_cases:
        if not is_age_disabled(val):
            raise AssertionError(f"is_age_disabled({val!r}) was False, want True")
    enabled_cases = ("1 day", "3 days", "0 days 1 hour", "zero", "-1 days")
    for val in enabled_cases:
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
        try:
            judge(cfg, [pin])
        except RefusedError:
            continue
        raise AssertionError(f"config {cfg!r} was not refused")


def self_test_managers_and_records() -> None:
    """Assert customManagers Docker rules and record-file protection fail closed."""
    mgr_exact = {
        "enabledManagers": ["custom.regex"],
        "customManagers": [
            {
                "customType": "regex",
                "managerFilePatterns": ["docs/adr.md"],
                "matchStrings": ["(?<depName>a):(?<currentValue>1)"],
                "datasourceTemplate": "docker",
            }
        ],
    }
    if judge(mgr_exact, [], records=["other.md"]) != []:
        raise AssertionError("unrelated record file should pass")
    for rec in ("docs/adr.md", "DOCS/ADR.MD"):
        try:
            judge(mgr_exact, [], records=[rec])
        except RefusedError as exc:
            if "customManagers[0]" not in str(exc):
                raise AssertionError(f"unexpected RefusedError on {rec!r}: {exc}") from exc
            continue
        raise AssertionError(f"record file {rec!r} matched by exact manager was not refused")
    dotted_mgr = {**mgr_exact["customManagers"][0], "managerFilePatterns": ["./docs/adr.md"]}
    dotted = {"enabledManagers": ["custom.regex"], "customManagers": [dotted_mgr]}
    try:
        judge(dotted, [], records=["docs/adr.md"])
    except RefusedError as exc:
        if "customManagers[0]" not in str(exc):
            raise AssertionError(f"unexpected RefusedError on dotted manager: {exc}") from exc
        return
    raise AssertionError("'./docs/adr.md' must count as a possible match for record docs/adr.md")


def expect_failure(call: object, error: type[Exception], what: str) -> None:
    """Assert that zero-argument CALL raises ERROR; WHAT names the case."""
    try:
        call()  # type: ignore[operator]
    except error:
        return
    raise AssertionError(f"{what} was not refused with {error.__name__}")


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
            lambda m=mgr, r=rec: judge({"enabledManagers": [m]}, [], records=[r]),
            RefusedError,
            f"{mgr} hitting {rec}",
        )
    all_mgrs = {"enabledManagers": sorted(BUILTIN_MANAGER_FILE_PATTERNS)}
    safe_records = [
        "docs/adr/0019-foreign-title-search.md",
        "tools/e2e/pushtitle/images.yaml",
        "tools/e2e/pushtitle_live.sh",
    ]
    if judge(all_mgrs, [], records=safe_records) != []:
        raise AssertionError("repo record files must miss all modeled built-in managers")
    expect_failure(
        lambda: judge({"enabledManagers": ["kubernetes"]}, [], records=["rec.yml"]),
        RefusedError,
        "unmodeled enabledManagers entry",
    )
    expect_failure(
        lambda: judge({"enabledManagers": "dockerfile"}, []), ConfigError, "bad enabledManagers"
    )


def self_test_docker_dep_name() -> None:
    """Run table-driven assertions for `docker_dep_name` normalization."""
    cases = (
        ("library/x", "x"),
        ("amd64/x", "x"),
        ("arm64/library/x", "x"),
        ("library/amd64/x", "amd64/x"),
        ("quay.io:5000/a/b", "quay.io/a/b"),
        ("QUAY.IO:443/a", "quay.io/a"),
        ("Quay.io/a", "quay.io/a"),
        ("quay.io:0123/a", "quay.io:0123/a"),
        ("quay.io:123456/a", "quay.io6/a"),
        ("ghcr.io/library/x", "ghcr.io/library/x"),
        ("docker.io/library/x", "docker.io/library/x"),
        ("quayio/x", "quayio/x"),
        ("ghcr.io/a/b", "ghcr.io/a/b"),
    )
    for raw, want in cases:
        got = docker_dep_name(raw)
        if got != want:
            raise AssertionError(f"docker_dep_name({raw!r}) = {got!r}, want {want!r}")


def self_test_walker_alone() -> None:
    """Assert `compile_re2_body` still refuses non-RE2 bodies with the tokenizer switched off.

    Barrier 1 (`check_re2_subset`) refuses every planted body first, so only this case shows
    that barrier 2 (`walk_re2_tree`) stays wired in.
    """
    saved = globals()["check_re2_subset"]
    globals()["check_re2_subset"] = lambda _body, _label: None
    try:
        for body in ("a\\Z", "(?=a)", "(a)\\1", "(?P<n>a)", "(?i:a)"):
            expect_failure(
                lambda b=body: compile_re2_body(b, "self-test"), ConfigError, f"walker on {body!r}"
            )
    finally:
        globals()["check_re2_subset"] = saved


def self_test_groups_and_inputs() -> None:
    """Assert named-group over-approximation and the printable-ASCII input domain."""
    base = {"customType": "regex", "managerFilePatterns": ["x.yml"], "datasourceTemplate": "docker"}

    def with_match(match: str) -> dict[str, object]:
        """Return a config whose one regex manager carries MATCH as its only matchString."""
        return {"customManagers": [{**base, "matchStrings": [match]}]}

    if judge(with_match("(?<=a)(?<!b)(?<depName>c)"), []) != []:
        raise AssertionError("lookbehind must not count as a named group")
    for hidden in (r"\Q[\E(?<packageName>x)", r"[(?<packageName>x)]", r"\(?P<registryUrl>x)"):
        expect_failure(lambda m=hidden: judge(with_match(m), []), RefusedError, hidden)
    expect_failure(lambda: judge(with_match("(?<depName"), []), ConfigError, "unclosed group")
    for all_on in ({}, {"enabledManagers": None}, {"enabledManagers": []}):
        expect_failure(
            lambda c=all_on: judge(c, ["ghcr.io/a/b"], records=["tools/e2e/x.yaml"]),
            RefusedError,
            f"record file under {all_on!r}",
        )
    only_regex = {"enabledManagers": ["custom.regex"]}
    if judge(only_regex, ["ghcr.io/a/b"], records=["tools/e2e/x.yaml"]) != ["ghcr.io/a/b"]:
        raise AssertionError("printable ASCII inputs must be judged")
    outside = (("ghcr.io/a/b\n", "r.yml"), ("ghcr.io/a/b\v", "r.yml"), ("a", "réc.yml"))
    for name, rec in outside:
        expect_failure(
            lambda n=name, r=rec: judge(only_regex, [n], records=[r]),
            RefusedError,
            repr((name, rec)),
        )


def main(argv: list[str] | None = None) -> int:
    """Run `--self-test` when requested."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--self-test", action="store_true", help="run table-driven unit tests")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test_match_list()
        self_test_tree_walker()
        self_test_re2_and_walk()
        self_test_zero_age()
        self_test_override_and_aliases()
        self_test_managers_and_records()
        self_test_builtin_managers()
        self_test_docker_dep_name()
        self_test_groups_and_inputs()
        self_test_walker_alone()
        print("ok   renovate_rules self-test")
    return 0


if __name__ == "__main__":
    sys.exit(main())
