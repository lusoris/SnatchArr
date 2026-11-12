# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Pure RE2 syntax allow-list validator for Renovate regex patterns.

Tokenizes regex bodies before `re._parser.parse` and accepts only constructs with identical
semantics in Python `re` (with `re.ASCII`) and Renovate 44.234.0's RE2 wrapper (`util/regex.js`),
enforcing RE2's 1000 counted-repetition and nested-repetition product limits (`RepetitionWalker`).
"""

from __future__ import annotations

import argparse
import re
import sys

MAX_REPEAT_BOUND = 1000
PRINTABLE_MIN = 0x20
PRINTABLE_MAX = 0x7E
ASCII_PUNCT = frozenset('!"#$%&\'()*+,-./:;<=>?@[\\]^_`{|}~')
HEX_DIGITS = frozenset("0123456789abcdefABCDEF")
CTRL_ESCAPES: dict[str, int] = {"t": 9, "n": 10, "r": 13, "f": 12, "v": 11}
CLASS_ESCAPES = frozenset("dDsSwW")
ANCHOR_ESCAPES = frozenset("bBA")
OUTER_META = frozenset(r"\.^$|?*+()[]{}")
OUTER_LITERALS = (
    frozenset(chr(code) for code in range(PRINTABLE_MIN, PRINTABLE_MAX + 1)) - OUTER_META
)
CLASS_LITERALS = (
    frozenset(chr(code) for code in range(PRINTABLE_MIN, PRINTABLE_MAX + 1)) - frozenset(r"\[]-")
)
CLASS_PAIR_CHARS = frozenset("&-~|")
BRACE_QUANT_RE = re.compile(r"\{([0-9]+)(,([0-9]*))?\}", re.ASCII)


class SubsetError(ValueError):
    """Raised when a regex body uses syntax outside the modeled RE2 subset."""


def parse_hex_escape(body: str, pos: int) -> tuple[int, int]:
    """Validate `\\xHH` starting at POS and return `(codepoint, next_pos)`."""
    if pos + 4 <= len(body) and body[pos + 2] in HEX_DIGITS and body[pos + 3] in HEX_DIGITS:
        return int(body[pos + 2 : pos + 4], 16), pos + 4
    token = body[pos : pos + 4]
    raise SubsetError(f"invalid hex escape {token!r} at offset {pos}")


def parse_outer_escape(body: str, pos: int) -> tuple[str, int]:
    """Validate an escape sequence outside `[...]` at POS; return `('atom'|'anchor', next_pos)`."""
    if pos + 1 >= len(body):
        bs = "\\"
        raise SubsetError(f"trailing {bs!r} at offset {pos}")
    esc = body[pos + 1]
    if esc == "x":
        _, end = parse_hex_escape(body, pos)
        return "atom", end
    if esc in ASCII_PUNCT or esc in CTRL_ESCAPES or esc in CLASS_ESCAPES:
        return "atom", pos + 2
    if esc in ANCHOR_ESCAPES:
        return "anchor", pos + 2
    token = body[pos : pos + 2]
    raise SubsetError(f"unsupported escape {token!r} at offset {pos}")


def parse_class_escape(body: str, pos: int) -> tuple[str, int | None, int]:
    """Validate an escape sequence inside `[...]` at POS; return `(kind, codepoint, next_pos)`."""
    if pos + 1 >= len(body):
        bs = "\\"
        raise SubsetError(f"trailing {bs!r} in character class at offset {pos}")
    esc = body[pos + 1]
    if esc == "x":
        cp, end = parse_hex_escape(body, pos)
        return "char", cp, end
    if esc in ASCII_PUNCT:
        return "char", ord(esc), pos + 2
    if esc in CTRL_ESCAPES:
        return "char", CTRL_ESCAPES[esc], pos + 2
    if esc in CLASS_ESCAPES:
        return "class", None, pos + 2
    token = body[pos : pos + 2]
    raise SubsetError(f"unsupported class escape {token!r} at offset {pos}")


def parse_class_atom(body: str, pos: int) -> tuple[str, int | None, int]:
    """Validate one non-`-`, non-`]` character-class item or range endpoint at POS."""
    if pos >= len(body):
        raise SubsetError(f"unclosed '[' before offset {pos}")
    ch = body[pos]
    if ch == "\\":
        return parse_class_escape(body, pos)
    if ch not in CLASS_LITERALS:
        pair = body[pos : pos + 2]
        tok = pair if pair in ("[[", "[:", "--") else ch
        raise SubsetError(f"unsupported {tok!r} in character class at offset {pos}")
    if ch in CLASS_PAIR_CHARS and body[pos : pos + 2] == ch * 2:
        pair = ch * 2
        raise SubsetError(f"unsupported pair {pair!r} in character class at offset {pos}")
    return "char", ord(ch), pos + 1


def parse_class_range(
    body: str, atom_pos: int, kind1: str, cp1: int | None, dash_pos: int
) -> int:
    """Validate `x-y` inside `[...]` starting at ATOM_POS with `-` at DASH_POS."""
    if body[dash_pos : dash_pos + 2] == "--":
        raise SubsetError(f"unsupported pair '--' in character class at offset {dash_pos}")
    if kind1 != "char" or cp1 is None:
        raise SubsetError(f"invalid character class range at offset {atom_pos}")
    kind2, cp2, end2 = parse_class_atom(body, dash_pos + 1)
    if kind2 != "char" or cp2 is None or cp1 > cp2:
        span = body[atom_pos:end2]
        raise SubsetError(f"invalid character class range {span!r} at offset {atom_pos}")
    return end2


def consume_literal_dash(body: str, pos: int, first_pos: int) -> int:
    """Validate a standalone `-` at POS in `[...]` (allowed only first or last, never `--`)."""
    if body[pos : pos + 2] == "--":
        raise SubsetError(f"unsupported pair '--' in character class at offset {pos}")
    if pos != first_pos and body[pos + 1 : pos + 2] != "]":
        raise SubsetError(f"unescaped '-' in character class at offset {pos}")
    return pos + 1


def parse_class_entry(body: str, pos: int, first_pos: int) -> int:
    """Parse one item or `x-y` range inside `[...]` starting at POS."""
    if body[pos] == "-":
        return consume_literal_dash(body, pos, first_pos)
    kind1, cp1, next_pos = parse_class_atom(body, pos)
    if body[next_pos : next_pos + 1] == "-" and body[next_pos + 1 : next_pos + 2] != "]":
        return parse_class_range(body, pos, kind1, cp1, next_pos)
    return next_pos


def parse_char_class(body: str, start: int) -> int:
    """Validate `[...]` starting at START and return the offset after closing `]`."""
    pos = start + 2 if body.startswith("[^", start) else start + 1
    first_pos = pos
    items = 0
    while pos < len(body) and body[pos] != "]":
        pos = parse_class_entry(body, pos, first_pos)
        items += 1
    if pos >= len(body):
        raise SubsetError(f"unclosed '[' at offset {start}")
    if items == 0:
        raise SubsetError(f"empty character class '[]' at offset {start}")
    return pos + 1


def parse_brace_quantifier(body: str, pos: int) -> tuple[int, int]:
    """Validate `{n}`, `{n,}` or `{n,m}` at POS; return `(counted_max_factor, next_pos)`."""
    match = BRACE_QUANT_RE.match(body, pos)
    if match is None:
        end = body.find("}", pos)
        stop = pos + 6 if end == -1 else min(end + 1, pos + 12)
        tok = body[pos:stop]
        raise SubsetError(f"invalid brace quantifier {tok!r} at offset {pos}")
    lo = int(match.group(1))
    hi_str = match.group(3)
    hi = lo if (match.group(2) is None or not hi_str) else int(hi_str)
    if lo > MAX_REPEAT_BOUND or hi > MAX_REPEAT_BOUND or lo > hi:
        tok = match.group(0)
        raise SubsetError(
            f"repeat bound out of 0..{MAX_REPEAT_BOUND} in {tok!r} at offset {pos}"
        )
    return max(1, hi), match.end()


def parse_quantifier(body: str, pos: int, atom_mult: int | None) -> tuple[int, int]:
    """Validate a quantifier at POS (plus optional lazy `?`); return `(mult, next_pos)`."""
    ch = body[pos]
    factor, end = parse_brace_quantifier(body, pos) if ch == "{" else (1, pos + 1)
    if atom_mult is None:
        tok = body[pos:end]
        raise SubsetError(f"quantifier {tok!r} without quantifiable atom at offset {pos}")
    mult = atom_mult * factor
    if mult > MAX_REPEAT_BOUND:
        tok = body[pos:end]
        raise SubsetError(
            f"nested repeat product {mult} for {tok!r} exceeds {MAX_REPEAT_BOUND} at offset {pos}"
        )
    if end < len(body) and body[end] == "?":
        end += 1
    return mult, end


def parse_open_group(body: str, pos: int) -> int:
    """Validate `(` or `(?:` at POS and return the offset after the opener."""
    if not body.startswith("?", pos + 1):
        return pos + 1
    if body.startswith("?:", pos + 1):
        return pos + 3
    tok = body[pos : pos + 4]
    raise SubsetError(f"unsupported group {tok!r} at offset {pos}")


def step_group(
    body: str, pos: int, stack: list[tuple[int, int]], cur_max: int
) -> tuple[int, int | None, int]:
    """Advance across `(` or `)` at POS, updating STACK and returning `(next_pos, atom_mult, cur_max)`."""
    if body[pos] == "(":
        next_pos = parse_open_group(body, pos)
        stack.append((pos, cur_max))
        return next_pos, None, 1
    if not stack:
        raise SubsetError(f"unbalanced ')' at offset {pos}")
    _, parent_max = stack.pop()
    return pos + 1, cur_max, max(parent_max, cur_max)


def step_token(
    body: str,
    pos: int,
    atom_mult: int | None,
    cur_max: int,
    stack: list[tuple[int, int]],
) -> tuple[int, int | None, int]:
    """Consume one regex token at POS and return `(next_pos, next_atom_mult, next_cur_max)`."""
    ch = body[pos]
    if ch in OUTER_LITERALS or ch == ".":
        return pos + 1, 1, cur_max
    if ch in "^$|":
        return pos + 1, None, cur_max
    if ch == "\\":
        kind, end = parse_outer_escape(body, pos)
        return end, (1 if kind == "atom" else None), cur_max
    if ch == "[":
        return parse_char_class(body, pos), 1, cur_max
    if ch in "()":
        return step_group(body, pos, stack, cur_max)
    if ch in "*+?{":
        mult, end = parse_quantifier(body, pos, atom_mult)
        return end, None, max(cur_max, mult)
    raise SubsetError(f"unsupported token {ch!r} at offset {pos}")


def check_re2_subset(body: str, label: str) -> None:
    """Validate that regex BODY for LABEL stays inside the modeled RE2 syntax subset."""
    if not isinstance(body, str) or not isinstance(label, str):
        raise SubsetError("regex body and label must be strings at offset 0")
    stack: list[tuple[int, int]] = []
    pos = 0
    atom_mult: int | None = None
    cur_max = 1
    while pos < len(body):
        pos, atom_mult, cur_max = step_token(body, pos, atom_mult, cur_max, stack)
    if stack:
        open_pos, _ = stack[-1]
        raise SubsetError(f"unbalanced '(' at offset {open_pos}")


SELF_TEST_ACCEPTED = (
    "",
    "abc/def_123",
    r"\.\^\$\|\?\*\+\(\)\[\]\{\}\/\\\-",
    r"\d\D\s\S\w\W\b\B\A\t\n\r\f\v\x00\x7f\xFF",
    r"^ghcr\.io/(?:hotio|linuxserver)/[a-z0-9._-]+$",
    r"[-a-z]",
    r"[^-a-z]",
    r"[a-z-]",
    r"[-]",
    r"[\x20-\x7e]",
    r"[\t-\r]",
    r"[\--0]",
    r"[\[\]\\]",
    r"a*b+c?d*?e+?f??g{0}h{3}i{3,}j{3,5}k{3,5}?m{1000}",
    r"(a{10}){100}",
    r"(a{500}|b{500}){2}",
    r"a{500}b{500}",
    r"(a{100,}){10}",
    r"(a{1000})*",
)
SELF_TEST_REFUSED = (
    r"\Z",
    r"\z",
    r"\Q.\E",
    r"\p{L}",
    r"\P{L}",
    r"\C",
    r"\G",
    r"\K",
    r"\N{SPACE}",
    r"\u0041",
    r"\U00000041",
    r"\a",
    r"\e",
    r"\0",
    r"\1",
    r"\9",
    r"\x",
    r"\x1",
    r"\xGG",
    r"\x{41}",
    "\\",
    r"(?#comment)",
    r"(?P<x>a)",
    r"(?<x>a)",
    r"(?=a)",
    r"(?!a)",
    r"(?>a)",
    r"(?i)a",
    r"(?i:a)",
    r"(?-i:a)",
    r"(?m)a",
    r"(?)",
    r"{,3}",
    r"a{,3}",
    r"a{}",
    r"a{5,3}",
    r"a{1001}",
    r"a{1001,}",
    r"a{1,1001}",
    r"(a{100}){100}",
    r"(a{10}){101}",
    r"(a{100,}){11}",
    r"((a{50})b){21}",
    r"{",
    r"}",
    r"a{1",
    r"a}",
    r"*a",
    r"+a",
    r"?a",
    r"{2}a",
    r"^*",
    r"$+",
    r"\b+",
    r"\B*",
    r"\A?",
    r"a|*",
    r"(+a)",
    r"a++",
    r"a*+",
    r"a?+",
    r"a{2}+",
    r"a{2}{3}",
    r"a*??",
    r"a???",
    r"[[:alpha:]]",
    r"[[a]]",
    r"[]",
    r"[^]",
    r"[a",
    r"[z-a]",
    r"[\d-z]",
    r"[a-\d]",
    r"[a-b-c]",
    r"[\b]",
    r"[\A]",
    r"[\Z]",
    r"[a&&b]",
    r"[a--b]",
    r"[--a]",
    r"[a--]",
    r"[a~~b]",
    r"[a||b]",
    r"[!-&&]",
    r"(abc",
    r"abc)",
    "\u00e9",
    "\x00",
)


def self_test_accepted() -> None:
    """Assert valid RE2 subset patterns are accepted."""
    for pat in SELF_TEST_ACCEPTED:
        check_re2_subset(pat, f"/{pat}/")


def self_test_refused() -> None:
    """Assert non-RE2, ambiguous or out-of-bound patterns raise `SubsetError` with offset."""
    for pat in SELF_TEST_REFUSED:
        try:
            check_re2_subset(pat, f"/{pat}/")
        except SubsetError as exc:
            if "offset" not in str(exc):
                raise AssertionError(f"SubsetError for {pat!r} lacked offset: {exc}") from exc
        else:
            raise AssertionError(f"pattern {pat!r} was not refused")


def main(argv: list[str] | None = None) -> int:
    """Run `--self-test` when requested."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--self-test", action="store_true", help="run table-driven unit tests")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test_accepted()
        self_test_refused()
        print("ok   re2_subset self-test")
    return 0


if __name__ == "__main__":
    sys.exit(main())
