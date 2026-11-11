# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse an undated container image digest pin that no renovate.json rule lets Renovate propose.

Renovate 44.234.0's Docker datasource dates tags only on Docker Hub (`index.docker.io`,
including `docker.io`, `registry-1.docker.io` and host-less names). Under the `horistes`
preset's 3-day `minimumReleaseAge`, an undated pin on any other registry host (`mirror.gcr.io`,
`ghcr.io`, `lscr.io`, `mcr.microsoft.com`, `gcr.io`, `localhost`, `host:port`) never ages and is
never proposed unless `packageRules` covers it via `minimumReleaseAge: null`,
`minimumReleaseAgeBehaviour: "timestamp-optional"`, or an `overridePackageName` resolving to a
dated Docker Hub name.

Own images under `ghcr.io/<owner>/` are exempt: the preset rule covers own owners, and reading
`<owner>` from `.needs.yaml` (`repository: github.com/<owner>/<repo>`) keeps forks right.

Exit 0 = ok, 1 = input error, 3 = refused.
"""

from __future__ import annotations

import json
import re
import subprocess  # nosec B404  # catches SubprocessError from image_pin_check's fixed git calls
import sys
from collections import defaultdict
from collections.abc import Iterable, Iterator
from dataclasses import dataclass
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import image_pin_check

MAX_CONFIG_BYTES = 1 << 20
NEEDS_FILE = ".needs.yaml"
RENOVATE_FILE = "renovate.json"
CONFIG_FILES = frozenset({NEEDS_FILE, RENOVATE_FILE})
DATED_HOSTS = frozenset({"docker.io", "index.docker.io", "registry-1.docker.io"})
SUPPORTED_MATCHERS = frozenset({"matchDatasources", "matchDepNames", "matchPackageNames"})
GLOB_CHARS = frozenset("*?[{")
NEEDS_REPO_RE = re.compile(r"^github\.com/([A-Za-z0-9][A-Za-z0-9._-]*)/([A-Za-z0-9._-]+)$")
TEMPLATE_TRIPLE = re.compile(r"^\{\{\{\s*replace\s+'([^']*)'\s+'([^']*)'\s+depName\s*\}\}\}$")
TEMPLATE_DOUBLE = re.compile(r"^\{\{\s*replace\s+'([^']*)'\s+'([^']*)'\s+depName\s*\}\}$")
ADVICE = (
    "add a renovate.json packageRule (minimumReleaseAge null for media apps, "
    "overridePackageName to a Docker Hub twin, or timestamp-optional with the measurement) "
    "for each image above"
)

type PatternEntry = re.Pattern[str] | str | None
type OverrideSpec = tuple[re.Pattern[str], str] | str | None


class ConfigError(Exception):
    """Raised when `.needs.yaml` or `renovate.json` is missing, malformed or out of bounds."""


class RefusedError(Exception):
    """Raised when a packageRule fails closed or uses an unsupported template."""


@dataclass(frozen=True)
class CoverageRule:
    """One `packageRules` entry that touches release-age coverage and can apply to Docker."""

    index: int
    dep_patterns: tuple[PatternEntry, ...] | None
    pkg_patterns: tuple[PatternEntry, ...] | None
    has_extra_matcher: bool
    has_age: bool
    age: str | None
    has_behaviour: bool
    behaviour: str | None
    has_override: bool
    override: OverrideSpec


def decode_config(raw: bytes | None, label: str) -> str:
    """Decode RAW, the start of tracked file LABEL, as UTF-8 of at most MAX_CONFIG_BYTES.

    RAW comes from the same reader as the pins (working tree or index), so both modes judge
    one snapshot; a missing, untracked, binary or oversized file raises ConfigError.
    """
    if raw is None:
        raise ConfigError(f"{label}: missing, untracked or not a regular file")
    if b"\0" in raw:
        raise ConfigError(f"{label}: binary content is not allowed")
    if len(raw) > MAX_CONFIG_BYTES:
        raise ConfigError(f"{label}: exceeds {MAX_CONFIG_BYTES} bytes")
    return raw.decode("utf-8")


def parse_yaml_scalar(raw: str) -> str:
    """Extract a plain or quoted scalar value from a single-line YAML `key: value` mapping."""
    text = raw.strip()
    if text.startswith(("'", '"')):
        end = text.find(text[0], 1)
        tail = text[end + 1 :].strip() if end >= 0 else ""
        if end < 0 or (tail and not tail.startswith("#")):
            raise ConfigError(f".needs.yaml: malformed quoted scalar {raw!r}")
        return text[1:end]
    return re.sub(r"(?:^|\s)#.*$", "", text).strip()


def read_owner(raw: bytes | None) -> str:
    """Read the lowercase repository owner from `.needs.yaml` RAW (`github.com/<owner>/<repo>`).

    The preset rule covers own owners; reading `.needs.yaml` keeps forks right.
    """
    content = decode_config(raw, NEEDS_FILE)
    repos = [
        parse_yaml_scalar(line.partition(":")[2])
        for line in content.splitlines()
        if line.startswith("repository:")
    ]
    if len(repos) != 1:
        raise ConfigError(f".needs.yaml: want one top-level repository key, found {len(repos)}")
    match = NEEDS_REPO_RE.match(repos[0])
    if match is None:
        raise ConfigError(f".needs.yaml: repository {repos[0]!r} is not github.com/<owner>/<repo>")
    return match.group(1).lower()


def registry_host(name: str) -> str | None:
    """Return the registry host of Docker reference NAME, or None when NAME has no host."""
    first, sep, rest = name.partition("/")
    if not sep or not rest:
        return None
    if "." in first or ":" in first or first == "localhost":
        return first
    return None


def is_dated(name: str) -> bool:
    """Report whether Renovate's Docker datasource dates NAME via Docker Hub."""
    if not name or name.startswith("/") or name.endswith("/") or any(c.isspace() for c in name):
        return False
    host = registry_host(name)
    return host is None or host.lower() in DATED_HOSTS


def is_own_image(name: str, owner: str) -> bool:
    """Report whether NAME is under `ghcr.io/<owner>/` (covered by the preset rule)."""
    prefix = f"ghcr.io/{owner}/"
    lowered = name.lower()
    return lowered.startswith(prefix) and len(lowered) > len(prefix)


def no_duplicate_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    """Construct a JSON object from PAIRS while refusing duplicate keys."""
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ConfigError(f"renovate.json: duplicate JSON key {key!r}")
        result[key] = value
    return result


def compile_regex_pattern(pattern: str) -> re.Pattern[str]:
    """Compile a Renovate `/re/` or `/re/i` regex literal into a Python Pattern."""
    if pattern.endswith("/i") and len(pattern) >= 3:
        body, flags = pattern[1:-2], re.IGNORECASE
    elif pattern.endswith("/") and len(pattern) >= 2:
        body, flags = pattern[1:-1], 0
    else:
        raise ConfigError(f"renovate.json: malformed regex pattern {pattern!r}")
    try:
        return re.compile(body, flags)
    except re.error as exc:
        raise ConfigError(f"renovate.json: invalid regex {pattern!r}: {exc}") from exc


def compile_name_pattern(pattern: str) -> PatternEntry:
    """Compile `/re/` or `/re/i`, return None for globs or `!`, or return exact PATTERN."""
    if not pattern:
        raise ConfigError("renovate.json: empty name pattern")
    if pattern.startswith("/"):
        return compile_regex_pattern(pattern)
    if pattern.startswith("!") or bool(GLOB_CHARS.intersection(pattern)):
        return None
    return pattern


def parse_string_list(rule: dict[str, object], key: str, index: int) -> list[str] | None:
    """Return optional string list KEY from packageRules[INDEX], raising ConfigError on bad types."""
    if key not in rule:
        return None
    value = rule[key]
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        raise ConfigError(f"renovate.json: packageRules[{index}].{key} must be a list of strings")
    return value


def compile_pattern_list(
    rule: dict[str, object], key: str, index: int
) -> tuple[PatternEntry, ...] | None:
    """Parse and compile optional name-matcher list KEY on packageRules[INDEX]."""
    raw = parse_string_list(rule, key, index)
    if raw is None:
        return None
    return tuple(compile_name_pattern(item) for item in raw)


def parse_optional_str(
    rule: dict[str, object], key: str, index: int
) -> tuple[bool, str | None]:
    """Return `(present, value)` for string-or-null option KEY on packageRules[INDEX]."""
    if key not in rule:
        return False, None
    value = rule[key]
    if value is not None and not isinstance(value, str):
        raise ConfigError(f"renovate.json: packageRules[{index}].{key} must be string or null")
    return True, value


def parse_override_spec(raw: str | None, index: int) -> OverrideSpec:
    """Validate `overridePackageName` on packageRules[INDEX] as a literal or `replace` template."""
    if raw is None:
        return None
    if "{" not in raw and "}" not in raw:
        return raw
    match = TEMPLATE_TRIPLE.match(raw) or TEMPLATE_DOUBLE.match(raw)
    if match is None:
        raise RefusedError(
            f"packageRules[{index}]: unsupported template in overridePackageName: {raw!r}"
        )
    try:
        return re.compile(match.group(1)), match.group(2)
    except re.error as exc:
        raise ConfigError(
            f"renovate.json: packageRules[{index}]: invalid regex in overridePackageName: {exc}"
        ) from exc


def apply_override(spec: OverrideSpec, name: str) -> str | None:
    """Evaluate compiled `overridePackageName` SPEC against image NAME."""
    if spec is None or isinstance(spec, str):
        return spec
    pattern, replacement = spec
    try:
        return pattern.sub(replacement, name)
    except re.error as exc:
        raise ConfigError(
            f"renovate.json: invalid replacement in overridePackageName: {exc}"
        ) from exc


def has_extra_matchers(rule: dict[str, object]) -> bool:
    """Report whether RULE lacks supported matchers or carries any unsupported matcher key."""
    matchers = {key for key in rule if key.startswith(("match", "exclude"))}
    return not matchers or bool(matchers - SUPPORTED_MATCHERS)


def parse_rule(raw: object, index: int) -> CoverageRule | None:
    """Validate packageRules[INDEX]; return a CoverageRule when it affects Docker age."""
    if not isinstance(raw, dict):
        raise ConfigError(f"renovate.json: packageRules[{index}] must be an object")
    datasources = parse_string_list(raw, "matchDatasources", index)
    dep_patterns = compile_pattern_list(raw, "matchDepNames", index)
    pkg_patterns = compile_pattern_list(raw, "matchPackageNames", index)
    has_age, age = parse_optional_str(raw, "minimumReleaseAge", index)
    has_beh, beh = parse_optional_str(raw, "minimumReleaseAgeBehaviour", index)
    has_ovr, ovr_raw = parse_optional_str(raw, "overridePackageName", index)
    if not has_age and not has_beh and not has_ovr:
        return None
    if datasources is not None and "docker" not in datasources:
        return None
    override = parse_override_spec(ovr_raw, index) if has_ovr else None
    return CoverageRule(
        index=index,
        dep_patterns=dep_patterns,
        pkg_patterns=pkg_patterns,
        has_extra_matcher=has_extra_matchers(raw),
        has_age=has_age,
        age=age,
        has_behaviour=has_beh,
        behaviour=beh,
        has_override=has_ovr,
        override=override,
    )


def read_rules(raw: bytes | None) -> list[CoverageRule]:
    """Parse `renovate.json` RAW and return its Docker release-age CoverageRules."""
    text = decode_config(raw, RENOVATE_FILE)
    data = json.loads(text, object_pairs_hook=no_duplicate_keys)
    if not isinstance(data, dict):
        raise ConfigError("renovate.json: top-level JSON value must be an object")
    raw_rules = data.get("packageRules", [])
    if not isinstance(raw_rules, list):
        raise ConfigError("renovate.json: packageRules must be a list")
    parsed: list[CoverageRule] = []
    for index, raw_rule in enumerate(raw_rules):
        rule = parse_rule(raw_rule, index)
        if rule is not None:
            parsed.append(rule)
    return parsed


def matches_definite(patterns: tuple[PatternEntry, ...] | None, name: str) -> bool:
    """Report whether glob-free PATTERNS match NAME, or True when PATTERNS is absent."""
    if patterns is None:
        return True
    for entry in patterns:
        if isinstance(entry, str) and entry == name:
            return True
        if isinstance(entry, re.Pattern) and entry.search(name):
            return True
    return False


def patterns_may_match(patterns: tuple[PatternEntry, ...] | None, name: str) -> bool:
    """Report whether PATTERNS definitely match NAME or hold a glob/`!` entry."""
    if patterns is None or any(entry is None for entry in patterns):
        return True
    return matches_definite(patterns, name)


def is_unmodeled_rule(rule: CoverageRule) -> bool:
    """Report whether RULE carries an extra matcher or a glob/`!` name pattern."""
    if rule.has_extra_matcher:
        return True
    if rule.dep_patterns is not None and any(entry is None for entry in rule.dep_patterns):
        return True
    return rule.pkg_patterns is not None and any(entry is None for entry in rule.pkg_patterns)


def rule_undoes_coverage(rule: CoverageRule, name: str) -> bool:
    """Report whether an unmodeled RULE could undo release-age coverage for NAME."""
    if rule.has_age and rule.age is not None:
        return True
    if rule.has_behaviour and rule.behaviour != "timestamp-optional":
        return True
    if not rule.has_override:
        return False
    resolved = apply_override(rule.override, name)
    return resolved is None or not is_dated(resolved)


@dataclass
class RuleState:
    """Accumulated release-age settings across applying `packageRules` for one image."""

    age_null: bool = False
    behaviour: str | None = None
    override: str | None = None

    def apply(self, rule: CoverageRule, name: str) -> None:
        """Apply definite CoverageRule RULE to the state of image NAME."""
        if rule.has_age:
            self.age_null = rule.age is None
        if rule.has_behaviour:
            self.behaviour = rule.behaviour
        if rule.has_override:
            self.override = apply_override(rule.override, name)

    def is_covered(self) -> bool:
        """Report whether the final accumulated settings let Renovate propose updates."""
        if self.age_null or self.behaviour == "timestamp-optional":
            return True
        return self.override is not None and is_dated(self.override)


def rule_may_apply(rule: CoverageRule, name: str) -> bool:
    """Report whether RULE's name matchers match or may match NAME."""
    return patterns_may_match(rule.dep_patterns, name) and patterns_may_match(
        rule.pkg_patterns, name
    )


def is_covered_by_rules(name: str, rules: list[CoverageRule]) -> bool:
    """Walk RULES in order for undated image NAME; fail closed on ambiguous undo rules."""
    state = RuleState()
    for rule in rules:
        if not rule_may_apply(rule, name):
            continue
        if not is_unmodeled_rule(rule):
            state.apply(rule, name)
        elif rule_undoes_coverage(rule, name):
            raise RefusedError(
                f"packageRules[{rule.index}]: unmodeled matcher or pattern may undo coverage for {name}"
            )
    return state.is_covered()


def keep_configs(contents: Iterable[image_pin_check.Content], configs: dict[str, bytes | None]) -> Iterator[image_pin_check.Content]:
    """Yield CONTENTS unchanged and keep the raw content of each CONFIG_FILES path in CONFIGS."""
    for rel, raw in contents:
        if rel in CONFIG_FILES:
            configs[rel] = raw
        yield rel, raw


def collect_undated_pins(contents: Iterable[image_pin_check.Content]) -> dict[str, list[str]]:
    """Map each undated image name in CONTENTS to its `file:line` locations."""
    found: dict[str, list[str]] = defaultdict(list)
    for rel, lineno, name, _tag, _digest, _record in image_pin_check.iter_pins(contents):
        if not is_dated(name):
            found[name].append(f"{rel}:{lineno}")
    return found


def check(root: Path, *, index: bool = False) -> int:
    """Refuse any undated container image digest pin without Renovate release-age coverage.

    One pass of image_pin_check's reader yields the pins and the two config files, so the
    working-tree mode and the index mode each judge a single snapshot.
    """
    configs: dict[str, bytes | None] = {}
    undated = collect_undated_pins(keep_configs(image_pin_check.read_contents(root, index=index), configs))
    owner = read_owner(configs.get(NEEDS_FILE))
    rules = read_rules(configs.get(RENOVATE_FILE))
    uncovered: list[tuple[str, list[str]]] = []
    for name in sorted(undated):
        if is_own_image(name, owner):
            continue
        if not is_covered_by_rules(name, rules):
            uncovered.append((name, undated[name]))
    if uncovered:
        for name, locs in uncovered:
            where = ", ".join(dict.fromkeys(locs))
            print(f"image_release_age_check: {name} ({where})")
        print(ADVICE)
        return 3
    print(f"image_release_age_check: ok ({len(undated)} undated images covered)")
    return 0


def main() -> int:
    """Parse CLI arguments and run the container image release-age coverage check."""
    args = image_pin_check.parse_cli(__doc__.splitlines()[0])
    try:
        return check(args.root, index=args.index)
    except RefusedError as refused:
        print(f"image_release_age_check: {refused}")
        return 3
    except (ConfigError, OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"image_release_age_check: error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
