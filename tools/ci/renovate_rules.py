# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Pure Renovate 44.234.0 Docker release-age coverage evaluator.

Models `matchRegexOrGlobList` (`lib/util/string-match.ts`), RE2 pattern compilation, rule
applicability (`matchDatasources`, `matchDepNames`, `matchPackageNames`), `overridePackageName`
templates, and ordered `packageRules` state evaluation without file I/O or subprocess calls.
A non-empty `registryAliases` is refused: it rewrites `packageName` before any rule runs.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections.abc import Iterable
from dataclasses import dataclass

try:
    from re import _parser as re_parser  # type: ignore[attr-defined]
except ImportError:  # Python 3.10
    import sre_parse as re_parser  # type: ignore[no-redef]

DATED_HOSTS = frozenset({"docker.io", "index.docker.io", "registry-1.docker.io"})
SUPPORTED_MATCHERS = frozenset({"matchDatasources", "matchDepNames", "matchPackageNames"})
UNSUPPORTED_OVERRIDES = frozenset({"overrideDepName", "overrideDatasource"})
GLOB_CHARS = frozenset("*?[]{}()\\")
RE2_FORBIDDEN_OPS = frozenset({
    "ASSERT",
    "ASSERT_NOT",
    "GROUPREF",
    "GROUPREF_EXISTS",
    "ATOMIC_GROUP",
    "POSSESSIVE_REPEAT",
    "POSSESSIVE_REPEAT_ONE",
})
MAX_STATES = 64
MAX_PARSE_NODES = 10000
MAX_CONFIG_NODES = 1 << 20
TEMPLATE_TRIPLE = re.compile(r"^\{\{\{\s*replace\s+'([^']*)'\s+'([^']*)'\s+depName\s*\}\}\}$")
TEMPLATE_DOUBLE = re.compile(r"^\{\{\s*replace\s+'([^']*)'\s+'([^']*)'\s+depName\s*\}\}$")

# Implicit alias, no `type` statement: the hook floor (.config/lefthook/python.sh) is Python 3.10.
OverrideSpec = tuple[re.Pattern[str], str] | str | None


class ConfigError(Exception):
    """Raised when `renovate.json` or a matcher/regex pattern is malformed or non-RE2."""


class RefusedError(Exception):
    """Raised when a `packageRule` fails closed, forks ambiguously or uses an unmodeled feature."""


@dataclass(frozen=True)
class CompiledPattern:
    """One compiled entry from a Renovate `match*` pattern list."""

    negated: bool
    kind: str
    value: str | re.Pattern[str] | None = None


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


def no_duplicate_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    """Construct a JSON object from PAIRS while refusing duplicate keys."""
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ConfigError(f"renovate.json: duplicate JSON key {key!r}")
        result[key] = value
    return result


def push_subpatterns(av: object, stack: list[object]) -> None:
    """Append child `SubPattern` nodes held inside parse-tree operand AV onto STACK."""
    if isinstance(av, re_parser.SubPattern):
        stack.append(av)
    elif isinstance(av, (tuple, list)):
        for item in av:
            if isinstance(item, re_parser.SubPattern):
                stack.append(item)
            elif isinstance(item, list):
                stack.extend(child for child in item if isinstance(child, re_parser.SubPattern))


def compile_re2_body(body: str, label: str, flags: int = 0) -> re.Pattern[str]:
    """Compile regex BODY with `re.ASCII` after refusing constructs outside RE2.

    Refuses lookahead/lookbehind (`ASSERT`, `ASSERT_NOT`), backreferences (`GROUPREF`,
    `GROUPREF_EXISTS`), atomic groups and possessive repeats because Renovate's RE2 rejects
    them and silently falls back to a glob of the literal text.
    """
    full_flags = flags | re.ASCII
    try:
        parsed = re_parser.parse(body, full_flags)
    except re.error as exc:
        raise ConfigError(f"renovate.json: invalid regex {label!r}: {exc}") from exc
    stack: list[object] = [parsed]
    steps = 0
    while stack:
        steps += 1
        if steps > MAX_PARSE_NODES:
            raise ConfigError(f"renovate.json: regex {label!r} exceeds parse bound")
        sub = stack.pop()
        for op, av in sub.data:  # type: ignore[attr-defined]
            op_name = str(op).upper()
            if op_name in RE2_FORBIDDEN_OPS:
                raise ConfigError(
                    f"renovate.json: non-RE2 construct {op_name} in pattern {label!r}"
                )
            push_subpatterns(av, stack)
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
    if not isinstance(raw, dict):
        raise ConfigError(f"renovate.json: packageRules[{index}] must be an object")
    datasources = parse_optional_patterns(raw, "matchDatasources", index)
    dep_patterns = parse_optional_patterns(raw, "matchDepNames", index)
    pkg_patterns = parse_optional_patterns(raw, "matchPackageNames", index)
    label = f"packageRules[{index}]"
    has_age, age = parse_optional_str(raw, "minimumReleaseAge", label)
    has_beh, beh = parse_optional_str(raw, "minimumReleaseAgeBehaviour", label)
    has_ovr, ovr_raw = parse_optional_str(raw, "overridePackageName", label)
    # lib/util/package-rules/index.ts applies overridePackageName only when isString(...):
    # null leaves packageName unchanged, "" sets it empty (an invalid name, never dated).
    has_ovr = has_ovr and ovr_raw is not None
    bad_ovr = bool(UNSUPPORTED_OVERRIDES.intersection(raw))
    if not (has_age or has_beh or has_ovr or bad_ovr):
        return None
    if datasources is not None and match_compiled("docker", datasources) is False:
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


def refuse_registry_aliases(data: dict[str, object]) -> None:
    """Refuse a non-empty `registryAliases` object anywhere in DATA.

    Renovate 44.234.0 `getDepFromImageRef` (`lib/modules/manager/dockerfile/extract.ts`) rewrites
    `packageName` from `registryAliases` at extract time, before any packageRule. This module does
    not model that rewrite, so an alias could turn a dated Docker Hub name into an undated one.
    """
    stack: list[object] = [data]
    for _ in range(MAX_CONFIG_NODES):
        if not stack:
            return
        node = stack.pop()
        if isinstance(node, dict):
            aliases = node.get("registryAliases")
            if aliases is not None and aliases != {}:
                raise RefusedError(
                    "registryAliases is not modeled: it rewrites packageName before packageRules"
                )
            stack.extend(node.values())
        elif isinstance(node, list):
            stack.extend(node)
    raise ConfigError(f"renovate.json: more than {MAX_CONFIG_NODES} JSON values")


def parse_config(config: str | dict[str, object]) -> tuple[bool, bool, list[CoverageRule]]:
    """Parse `renovate.json` text or dict into `(age_null, behaviour_optional, rules)`."""
    if isinstance(config, str):
        try:
            data = json.loads(config, object_pairs_hook=no_duplicate_keys)
        except json.JSONDecodeError as exc:
            raise ConfigError(f"renovate.json: {exc}") from exc
    else:
        data = config
    if not isinstance(data, dict):
        raise ConfigError("renovate.json: top-level JSON value must be an object")
    refuse_registry_aliases(data)
    has_age, top_age = parse_optional_str(data, "minimumReleaseAge", "top-level")
    _, top_beh = parse_optional_str(data, "minimumReleaseAgeBehaviour", "top-level")
    raw_rules = data.get("packageRules", [])
    if not isinstance(raw_rules, list):
        raise ConfigError("renovate.json: packageRules must be a list")
    rules: list[CoverageRule] = []
    for index, raw_rule in enumerate(raw_rules):
        parsed = parse_rule(raw_rule, index)
        if parsed is not None:
            rules.append(parsed)
    return has_age and top_age is None, top_beh == "timestamp-optional", rules


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
    age_null = (rule.age is None) if rule.has_age else state.age_null
    beh_opt = (rule.behaviour == "timestamp-optional") if rule.has_behaviour else state.behaviour_optional
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
    dep_name: str, age_null: bool, beh_opt: bool, rules: list[CoverageRule]
) -> bool:
    """Walk RULES over deduplicated `RuleState`s for DEP_NAME; fail closed on mixed states."""
    states: set[RuleState] = {
        RuleState(age_null=age_null, behaviour_optional=beh_opt, package_name=dep_name)
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
    raise RefusedError(f"{where}: ambiguous rule applicability leaves coverage mixed for {dep_name}")


def judge(config: str | dict[str, object], names: Iterable[str]) -> list[str]:
    """Return sorted undated image NAMES that CONFIG leaves without release-age coverage."""
    age_null, beh_opt, rules = parse_config(config)
    uncovered: list[str] = []
    for name in sorted(set(names)):
        if not is_name_covered(name, age_null, beh_opt, rules):
            uncovered.append(name)
    return uncovered


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


def self_test_override_and_aliases() -> None:
    """Assert null overridePackageName is a no-op and non-empty registryAliases is refused."""
    pin = "ghcr.io/o/app"
    to_hub = {"matchDepNames": [pin], "overridePackageName": "docker.io/o/app"}
    null_ovr = {"matchDepNames": [pin], "overridePackageName": None}
    if judge({"packageRules": [to_hub, null_ovr]}, [pin]) != []:
        raise AssertionError("null overridePackageName must keep the earlier packageName")
    if judge({"packageRules": [{**to_hub, "overridePackageName": ""}]}, [pin]) != [pin]:
        raise AssertionError('empty overridePackageName "" must leave the pin uncovered')
    if judge({"registryAliases": {}, "dockerfile": {"registryAliases": None}}, []) != []:
        raise AssertionError("empty or null registryAliases must pass")
    for aliased in (
        {"registryAliases": {"docker.io": "mirror.gcr.io"}},
        {"dockerfile": {"registryAliases": {"docker.io": "mirror.gcr.io"}}},
        {"packageRules": [{"registryAliases": {"docker.io": "mirror.gcr.io"}}]},
    ):
        try:
            judge(aliased, [pin])
        except RefusedError:
            continue
        raise AssertionError(f"registryAliases in {aliased!r} was not refused")


def main(argv: list[str] | None = None) -> int:
    """Run `--self-test` when requested."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--self-test", action="store_true", help="run table-driven unit tests")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test_match_list()
        self_test_re2_and_walk()
        self_test_override_and_aliases()
        print("ok   renovate_rules self-test")
    return 0


if __name__ == "__main__":
    sys.exit(main())
