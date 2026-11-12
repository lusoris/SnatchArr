# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Pure RE2 regex, `matchRegexOrGlobList` matcher and `custom.regex` compiler for Renovate 44.234.0.

Split from `renovate_rules.py`: owns `ConfigError` / `RefusedError`, two-barrier RE2 compilation
(`re2_subset.py` tokenizer + `re._parser` opcode/code/flag allow-list), `matchRegexOrGlobList`
three-valued evaluation, and `custom.regex` `matchStrings` compilation (refusing `$`, empty-width
matches/repeats and `depName` under multi-repeats where Python `re` and RE2 can diverge).
"""

from __future__ import annotations

import argparse
import re
import string
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

ASCII_LOWER = str.maketrans(string.ascii_uppercase, string.ascii_lowercase)
DOCKER_ALLOWED_GROUPS = frozenset({"depName", "currentValue", "currentDigest"})
GLOB_CHARS = frozenset("*?[]{}()\\")
NAMED_GROUP_RE = re.compile(r"\(\?P?<(?![=!])([^>]*)(>?)")
# Names, captures and record paths stay printable ASCII: there `$`, `\s` and `/i` behave the
# same in Python `re` (re.ASCII) and RE2 (trailing `\n`, `\v`, Unicode folding).
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
MAX_PARSE_NODES = 10000


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


def ascii_lower(value: str) -> str:
    """Fold ASCII uppercase letters (`A-Z`) in VALUE to lowercase while preserving non-ASCII."""
    return value.translate(ASCII_LOWER)


def check_input_domain(values: Iterable[str], kind: str) -> None:
    """Refuse any VALUES entry outside printable ASCII, where `re` and RE2 can differ."""
    for value in values:
        if any(not INPUT_MIN <= ord(ch) <= INPUT_MAX for ch in value):
            raise RefusedError(
                f"{kind} {value!r} is outside printable ASCII: regex matching on it is not modeled"
            )


def extract_named_groups(match_strings: Iterable[str], where: str) -> set[str]:
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
    except (re.error, RecursionError) as exc:
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


def advance_match_repeat(op_name: str, av: object, where: str, in_multi: bool) -> bool:
    """Validate repeat min-width > 0 in `matchStrings` and return updated `in_multi` flag."""
    if op_name not in REPEAT_OPS:
        return in_multi
    _lo, hi, item = av  # type: ignore[misc]
    if item.getwidth()[0] == 0:
        raise RefusedError(f"{where}: repeat body has minimum width 0")
    return in_multi or hi == re_constants.MAXREPEAT or int(hi) > 1


def step_match_node(
    op_name: str,
    av: object,
    where: str,
    dep_group_id: int,
    in_multi: bool,
    stack: list[tuple[Iterable[tuple[object, object]], bool]],
) -> None:
    """Validate one `matchStrings` parse-tree node and push child subpatterns with repeat state."""
    children: list[Iterable[tuple[object, object]]] = []
    try:
        check_parse_node(op_name, av, where, children)
    except ConfigError as exc:
        raise RefusedError(f"{where}: {exc}") from exc
    if (op_name, str(av).upper()) == ("AT", "AT_END"):
        raise RefusedError(
            f"{where}: '$' anchor is refused (Python re matches before trailing '\\n', RE2 does not)"
        )
    next_multi = advance_match_repeat(op_name, av, where, in_multi)
    if in_multi and op_name == "SUBPATTERN" and av[0] == dep_group_id:  # type: ignore[index]
        raise RefusedError(f"{where}: 'depName' group under a repeat with max > 1 is refused")
    for child in children:
        stack.append((child, next_multi))


def walk_match_tree(parsed: object, where: str) -> None:
    """Validate `re_parser.parse` output PARSED for Docker `custom.regex` `matchStrings`."""
    state = parsed.state  # type: ignore[attr-defined]
    if int(state.flags) != int(re.ASCII):
        raise RefusedError(f"{where}: inline flags ({state.flags!r}) are not modeled")
    groupdict: dict[str, int] = dict(state.groupdict)
    extra = sorted(set(groupdict) - DOCKER_ALLOWED_GROUPS)
    if extra:
        raise RefusedError(f"{where}: group {extra[0]!r} is not modeled on a Docker customManager")
    if "depName" not in groupdict:
        raise RefusedError(f"{where}: matchString must capture a 'depName' group")
    if parsed.getwidth()[0] == 0:  # type: ignore[attr-defined]
        raise RefusedError(f"{where}: pattern minimum width is 0; empty matches are refused")
    dep_group_id = int(groupdict["depName"])
    stack: list[tuple[Iterable[tuple[object, object]], bool]] = [
        (parsed.data, False)  # type: ignore[attr-defined]
    ]
    steps = 0
    while stack:
        items, in_multi = stack.pop()
        for op, av in items:
            steps += 1
            if steps > MAX_PARSE_NODES:
                raise RefusedError(f"{where}: regex exceeds parse bound")
            step_match_node(str(op).upper(), av, where, dep_group_id, in_multi, stack)


def compile_match_string(ms: str, where: str) -> re.Pattern[str]:
    """Compile one Docker `customManagers` `matchStrings` entry under RE2-parity rules."""
    textual_groups = extract_named_groups((ms,), where)
    extra = sorted(textual_groups - DOCKER_ALLOWED_GROUPS)
    if extra:
        raise RefusedError(f"{where}: group {extra[0]!r} is not modeled on a Docker customManager")
    try:
        py_body = check_re2_subset(ms, where, allowed_groups=DOCKER_ALLOWED_GROUPS)
    except SubsetError as exc:
        raise RefusedError(f"{where}: {exc}") from exc
    try:
        parsed = re_parser.parse(py_body, re.ASCII)
    except (re.error, RecursionError) as exc:
        raise RefusedError(f"{where}: invalid regex: {exc}") from exc
    walk_match_tree(parsed, where)
    try:
        compiled = re.compile(py_body, re.ASCII)
    except re.error as exc:
        raise RefusedError(f"{where}: invalid regex: {exc}") from exc
    if textual_groups != set(compiled.groupindex):
        raise RefusedError(
            f"{where}: named-group tokens {sorted(textual_groups)!r} differ from "
            f"compiled groups {sorted(compiled.groupindex)!r}"
        )
    return compiled


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
        return CompiledPattern(negated=True, kind="exact", value=ascii_lower(rest))
    if pattern.startswith("/"):
        return CompiledPattern(
            negated=False, kind="regex", value=compile_regex_literal(pattern, pattern)
        )
    if pattern.startswith("#") or GLOB_CHARS.intersection(pattern):
        return CompiledPattern(negated=False, kind="unknown")
    return CompiledPattern(negated=False, kind="exact", value=ascii_lower(pattern))


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
        matched = ascii_lower(value) == entry.value
        return (not matched) if entry.negated else matched
    rx = entry.value
    if not isinstance(rx, re.Pattern):
        raise ConfigError("renovate.json: internal regex state error")
    matched = bool(rx.search(value))
    return (not matched) if entry.negated else matched


def disjoin_three_valued(vals: list[bool | None]) -> bool | None:
    """Return three-valued Kleene OR over VALS (`True` when VALS is empty)."""
    if not vals:
        return True
    has_unknown = False
    for val in vals:
        if val is True:
            return True
        if val is None:
            has_unknown = True
    return None if has_unknown else False


def conjoin_three_valued(vals: Iterable[bool | None]) -> bool | None:
    """Return three-valued Kleene AND over VALS (`True` when VALS is empty)."""
    has_unknown = False
    for val in vals:
        if val is False:
            return False
        if val is None:
            has_unknown = True
    return None if has_unknown else True


def combine_parts(pos_vals: list[bool | None], neg_vals: list[bool | None]) -> bool | None:
    """Combine positive and negative three-valued outcomes per `matchRegexOrGlobList`."""
    return conjoin_three_valued((disjoin_three_valued(pos_vals), *neg_vals))


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


def expect_failure(call: object, error: type[Exception], what: str) -> None:
    """Assert that zero-argument CALL raises ERROR; WHAT names the case."""
    try:
        call()  # type: ignore[operator]
    except error:
        return
    raise AssertionError(f"{what} was not refused with {error.__name__}")


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
        expect_failure(lambda b=bad: match_list("docker", b), ConfigError, repr(bad))
    for bad_re in ("/(?=a)/", "/(?!a)/", "/(?<=a)/", "/(?<!a)/", "/(a)\\1/", "/(?>a+)/", "/a*+/"):
        expect_failure(lambda r=bad_re: match_list("a", [r]), ConfigError, bad_re)
    if match_list("\u0660", [r"/^\d$/"]) is not False:
        raise AssertionError("re.ASCII did not restrict \\d to ASCII digits")


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
        expect_failure(
            lambda t=tree, lb=label, ef=exp_flags: walk_re2_tree(t, lb, ef),
            ConfigError,
            label,
        )


def self_test_walker_alone() -> None:
    """Assert both RE2 compilers still refuse bad bodies with `check_re2_subset` switched off."""
    saved = globals()["check_re2_subset"]
    globals()["check_re2_subset"] = lambda body, _label, allowed_groups=frozenset(): body
    try:
        for body in ("a\\Z", "(?=a)", "(a)\\1", "(?P<n>a)", "(?i:a)"):
            expect_failure(
                lambda b=body: compile_re2_body(b, "self-test"), ConfigError, f"walker on {body!r}"
            )
        for ms in (
            r"(?P<depName>a)\Z",
            r"(?i)(?P<depName>a)",
            r"(?P<depName>a)$",
            r"(?P<depName>[a-z]*)",
            r"(?:[a-z]*)*(?P<depName>x)",
            r"(?:(?P<depName>[a-z]+),)+",
        ):
            expect_failure(
                lambda m=ms: compile_match_string(m, "customManagers[0].matchStrings[0]"),
                RefusedError,
                f"match walker on {ms!r}",
            )
    finally:
        globals()["check_re2_subset"] = saved


def self_test_compile_match_string() -> None:
    """Run positive and negative assertions for `compile_match_string`."""
    where = "customManagers[0].matchStrings[0]"
    rx_re2 = compile_match_string(r"IMG=(?<depName>[a-z/]+):(?<currentValue>[0-9]+)\n", where)
    rx_py = compile_match_string(r"(?:pre-)?(?P<depName>[a-z]+)", where)
    found = rx_re2.search("IMG=rhysd/actionlint:1\n")
    if found is None or found.group("depName") != "rhysd/actionlint":
        raise AssertionError("compile_match_string failed to extract (?<depName>...)")
    if rx_py.search("pre-postgres") is None:
        raise AssertionError("compile_match_string failed on (?P<depName>...) or optional prefix")
    refused = (
        r"IMG=postgres:1\n",
        r"IMG=(?<depName>postgres)\n$",
        r"(?<depName>[a-z]*)",
        r"(?:[a-z]*)*(?<depName>x)",
        r"(?:(?<depName>[a-z]+),)+",
        r"[(?<depName>x)]",
        r"[(?<currentValue>x)](?<depName>y)",
        r"(?<registryUrl>x)/(?<depName>y)",
    )
    for bad in refused:
        try:
            compile_match_string(bad, where)
        except RefusedError as exc:
            if where not in str(exc):
                raise AssertionError(f"RefusedError for {bad!r} lacked {where}: {exc}") from exc
        else:
            raise AssertionError(f"compile_match_string accepted {bad!r}")
    if extract_named_groups(["(?<=a)(?<!b)(?<depName>c)"], "test") != {"depName"}:
        raise AssertionError("lookbehind must not count as a named group")
    expect_failure(
        lambda: extract_named_groups(["(?<depName"], "test"), ConfigError, "unclosed group"
    )


def main(argv: list[str] | None = None) -> int:
    """Run `--self-test` when requested."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--self-test", action="store_true", help="run table-driven unit tests")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test_match_list()
        self_test_tree_walker()
        self_test_walker_alone()
        self_test_compile_match_string()
        print("ok   renovate_regex self-test")
    return 0


if __name__ == "__main__":
    sys.exit(main())
