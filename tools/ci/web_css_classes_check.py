# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Verify that utility classes in Svelte templates generate CSS in the build output.

Scans Svelte templates for static and expression-based utility class tokens
matching candidate prefixes (bg, text, border, etc.) and ensures that
corresponding CSS rules exist in the built CSS output.
"""

import argparse
import dataclasses
import pathlib
import re
import sys

CANDIDATE_BASE_RE = re.compile(
    r"^(bg|text|border|ring|outline|fill|stroke|divide|from|via|to|accent|caret|decoration|placeholder)-[a-z][a-z0-9-]*$"
)

CLASS_TARGET_RE = re.compile(r"\bclass(?::([a-zA-Z0-9_\-\/]+)|\s*=\s*([\"'{]))")


class CheckError(Exception):
    """Raised when scan or self-test encounters an error."""


@dataclasses.dataclass(frozen=True)
class Finding:
    file: str
    line: int
    token: str


@dataclasses.dataclass
class ExprFrame:
    is_hole: bool
    hole_start: int


@dataclasses.dataclass
class TemplateFrame:
    start: int
    holes: list[tuple[int, int]]


def strip_script_and_style(content: str) -> str:
    """Blank script and style blocks with spaces preserving offsets and lines."""

    def replacer(m: re.Match[str]) -> str:
        s = m.group(0)
        return "".join("\n" if c == "\n" else " " for c in s)

    return re.sub(
        r"<(script|style)\b[^>]*>.*?</\1>",
        replacer,
        content,
        flags=re.DOTALL | re.IGNORECASE,
    )


def scan_quoted_string(content: str, start: int, quote: str) -> tuple[int, str, int]:
    """Scan single or double quoted string starting at quote character."""
    i = start + 1
    n = len(content)
    while i < n:
        c = content[i]
        if c == "\\":
            i += 2
        elif c == quote:
            return i + 1, content[start + 1 : i], start + 1
        else:
            i += 1
    return n, content[start + 1 : n], start + 1


def scan_comment(content: str, start: int) -> int:
    """Skip over single-line or multi-line comment, returning next index."""
    if start + 1 >= len(content):
        return len(content)
    if content[start + 1] == "/":
        nl = content.find("\n", start + 2)
        return nl if nl != -1 else len(content)
    if content[start + 1] == "*":
        end = content.find("*/", start + 2)
        return end + 2 if end != -1 else len(content)
    return start + 1


def blank_holes(raw: str, holes: list[tuple[int, int]], base_offset: int) -> str:
    """Blank hole regions within template literal with spaces."""
    if not holes:
        return raw
    chars = list(raw)
    for hs, he in holes:
        start_rel = max(0, hs - base_offset)
        end_rel = min(len(chars), he - base_offset)
        for idx in range(start_rel, end_rel):
            chars[idx] = " "
    return "".join(chars)


def step_expr(
    content: str,
    i: int,
    stack: list[ExprFrame | TemplateFrame],
    literals: list[tuple[str, int]],
) -> int:
    """Advance parser state inside expression frame."""
    c = content[i]
    if c in ("'", '"'):
        next_i, s_text, s_offset = scan_quoted_string(content, i, c)
        literals.append((s_text, s_offset))
        return next_i
    if c == "`":
        stack.append(TemplateFrame(start=i + 1, holes=[]))
        return i + 1
    if c == "{":
        stack.append(ExprFrame(is_hole=False, hole_start=-1))
        return i + 1
    if c == "}":
        popped = stack.pop()
        end_hole = i + 1
        if isinstance(popped, ExprFrame) and popped.is_hole and stack:
            top = stack[-1]
            if isinstance(top, TemplateFrame):
                top.holes.append((popped.hole_start, end_hole))
        return i + 1
    if c == "/":
        return scan_comment(content, i)
    return i + 1


def step_template(
    content: str,
    i: int,
    stack: list[ExprFrame | TemplateFrame],
    literals: list[tuple[str, int]],
) -> int:
    """Advance parser state inside template literal frame."""
    c = content[i]
    if c == "\\":
        return i + 2
    if c == "$" and i + 1 < len(content) and content[i + 1] == "{":
        stack.append(ExprFrame(is_hole=True, hole_start=i))
        return i + 2
    if c == "`":
        popped = stack.pop()
        if isinstance(popped, TemplateFrame):
            tmpl_raw = content[popped.start : i]
            blanked = blank_holes(tmpl_raw, popped.holes, popped.start)
            literals.append((blanked, popped.start))
        return i + 1
    return i + 1


def parse_expression(content: str, start_pos: int) -> tuple[int, list[tuple[str, int]]]:
    """Parse balanced JS expression block starting at opening brace."""
    stack: list[ExprFrame | TemplateFrame] = [ExprFrame(is_hole=False, hole_start=-1)]
    literals: list[tuple[str, int]] = []
    i = start_pos + 1
    n = len(content)
    while stack and i < n:
        top = stack[-1]
        if isinstance(top, ExprFrame):
            i = step_expr(content, i, stack, literals)
        else:
            i = step_template(content, i, stack, literals)
    return i, literals


def parse_quoted_class(
    content: str, quote_start: int, quote: str
) -> tuple[int, list[tuple[str, int]]]:
    """Parse class attribute with quotes, handling embedded {...} blocks."""
    val_start = quote_start + 1
    i = val_start
    n = len(content)
    holes: list[tuple[int, int]] = []
    literals: list[tuple[str, int]] = []

    while i < n:
        c = content[i]
        if c == "{":
            expr_end, expr_lits = parse_expression(content, i)
            holes.append((i, expr_end))
            literals.extend(expr_lits)
            i = expr_end
        elif c == "\\":
            i += 2
        elif c == quote:
            break
        else:
            i += 1

    quote_end = i
    static_raw = content[val_start:quote_end]
    static_blanked = blank_holes(static_raw, holes, val_start)
    literals.append((static_blanked, val_start))
    return min(n, quote_end + 1), literals


def filter_tokens(
    content: str, literals: list[tuple[str, int]]
) -> list[tuple[str, int]]:
    """Extract candidate tokens and calculate line numbers from string literals."""
    candidates: list[tuple[str, int]] = []
    for s_text, s_offset in literals:
        for tok_match in re.finditer(r"\S+", s_text):
            tok = tok_match.group(0)
            if any(c in tok for c in ("{", "[", "$")):
                continue
            base = tok.split(":")[-1]
            if not CANDIDATE_BASE_RE.match(base):
                continue
            tok_offset = s_offset + tok_match.start()
            line = content[:tok_offset].count("\n") + 1
            candidates.append((tok, line))
    return candidates


def handle_directive(
    content: str, scannable: str, m: re.Match[str]
) -> tuple[int, list[tuple[str, int]]]:
    """Process class:NAME directive match and return (next_pos, candidates)."""
    directive_name = m.group(1)
    name_end = m.end(1)
    n = len(scannable)
    if name_end < n and scannable[name_end] == ":":
        return name_end + 1, []
    candidates: list[tuple[str, int]] = []
    base = directive_name.split(":")[-1]
    if CANDIDATE_BASE_RE.match(base) and not any(
        c in directive_name for c in ("{", "[", "$")
    ):
        line = content[: m.start(1)].count("\n") + 1
        candidates.append((directive_name, line))
    pos = m.end()
    after = scannable[pos:].lstrip()
    if after.startswith("={"):
        brace_idx = scannable.find("{", pos)
        end_pos, _ = parse_expression(scannable, brace_idx)
        pos = end_pos
    return pos, candidates


def extract_candidate_tokens(content: str) -> list[tuple[str, int]]:
    """Extract candidate utility tokens and line numbers from class contexts."""
    scannable = strip_script_and_style(content)
    pos = 0
    all_candidates: list[tuple[str, int]] = []
    n = len(scannable)

    while pos < n:
        m = CLASS_TARGET_RE.search(scannable, pos)
        if not m:
            break
        if m.group(1) is not None:
            pos, dir_cands = handle_directive(content, scannable, m)
            all_candidates.extend(dir_cands)
        elif m.group(2) == "{":
            expr_start = m.end() - 1
            end_pos, lits = parse_expression(scannable, expr_start)
            all_candidates.extend(filter_tokens(content, lits))
            pos = end_pos
        else:
            quote = m.group(2)
            quote_start = m.end() - 1
            end_pos, lits = parse_quoted_class(scannable, quote_start, quote)
            all_candidates.extend(filter_tokens(content, lits))
            pos = end_pos

    return all_candidates


def load_css(css_dir: pathlib.Path) -> str:
    """Load and concatenate all CSS files under css_dir."""
    css_files = sorted(css_dir.rglob("*.css"))
    if not css_files:
        raise CheckError(
            f"no built CSS files found in {css_dir} (run pnpm build first)"
        )
    return "\n".join(p.read_text(encoding="utf-8") for p in css_files)


def is_token_in_css(token: str, css_text: str, cache: dict[str, bool]) -> bool:
    """Check if token's CSS selector exists in built CSS, using cache."""
    if token in cache:
        return cache[token]
    css_escaped = token.replace(":", r"\:")
    pattern = re.compile(r"\." + re.escape(css_escaped) + r"(?![a-zA-Z0-9_-])")
    matched = bool(pattern.search(css_text))
    cache[token] = matched
    return matched


def scan_classes(
    src_dir: pathlib.Path, css_dir: pathlib.Path, base_dir: pathlib.Path | None = None
) -> list[Finding]:
    """Scan Svelte files under src_dir against CSS under css_dir."""
    css_text = load_css(css_dir)
    svelte_files = sorted(src_dir.rglob("*.svelte"))
    if not svelte_files:
        raise CheckError(f"no .svelte files found in {src_dir}")

    cache: dict[str, bool] = {}
    findings: list[Finding] = []
    root_ref = base_dir or src_dir

    for svelte_file in svelte_files:
        content = svelte_file.read_text(encoding="utf-8")
        candidates = extract_candidate_tokens(content)
        rel_path = str(svelte_file.relative_to(root_ref))
        for token, line in candidates:
            if not is_token_in_css(token, css_text, cache):
                findings.append(Finding(file=rel_path, line=line, token=token))

    return findings


def self_test(root: pathlib.Path) -> None:
    """Run self-test against clean, planted, and planted_expr fixtures."""
    fixtures_dir = root / "tools" / "ci" / "testdata" / "web_css_classes"
    clean_dir = fixtures_dir / "clean"
    planted_dir = fixtures_dir / "planted"
    planted_expr_dir = fixtures_dir / "planted_expr"

    if (
        not clean_dir.is_dir()
        or not planted_dir.is_dir()
        or not planted_expr_dir.is_dir()
    ):
        raise CheckError(f"missing test fixtures in {fixtures_dir}")

    clean_findings = scan_classes(clean_dir, clean_dir)
    if clean_findings:
        raise CheckError(
            f"self-test clean fixture failed with {len(clean_findings)} findings: {clean_findings}"
        )

    planted_findings = scan_classes(planted_dir, planted_dir)
    if len(planted_findings) != 1 or planted_findings[0].token != "bg-muted":
        raise CheckError(
            f"self-test planted fixture did not yield exactly bg-muted finding; got: {planted_findings}"
        )

    planted_expr_findings = scan_classes(planted_expr_dir, planted_expr_dir)
    actual_expr = {(f.token, f.line) for f in planted_expr_findings}
    expected_expr = {
        ("bg-dead-ternary", 5),
        ("bg-dead-template", 6),
        ("bg-dead-object", 7),
        ("bg-dead-directive", 8),
    }
    if actual_expr != expected_expr:
        raise CheckError(
            f"self-test planted_expr fixture did not yield expected findings; "
            f"got: {sorted(actual_expr)}, expected: {sorted(expected_expr)}"
        )

    print(
        f"Web CSS classes self-test: clean, planted, and planted_expr fixtures verified "
        f"({len(planted_findings) + len(planted_expr_findings)} planted defects caught)"
    )


def scan_real(root: pathlib.Path) -> list[Finding]:
    """Run verification against web/src and web/build."""
    web_src = root / "web" / "src"
    web_build = root / "web" / "build"
    return scan_classes(web_src, web_build, base_dir=root)


def main(argv: list[str] | None = None) -> int:
    """CLI entrypoint."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--root",
        default=str(pathlib.Path(__file__).resolve().parent.parent.parent),
        help="Repository root directory",
    )
    args = parser.parse_args(argv)
    root = pathlib.Path(args.root).resolve()

    try:
        self_test(root)
        findings = scan_real(root)
    except CheckError as err:
        print(f"Web CSS classes check failed: {err}", file=sys.stderr)
        return 1

    if findings:
        for f in findings:
            print(
                f"{f.file}:{f.line}: missing utility class '{f.token}' in built CSS",
                file=sys.stderr,
            )
        print(
            f"Web CSS classes check failed: {len(findings)} missing utility class(es) detected in built CSS",
            file=sys.stderr,
        )
        return 1

    print(
        "Web CSS classes: all Svelte utility classes verified in built CSS, 0 findings"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
