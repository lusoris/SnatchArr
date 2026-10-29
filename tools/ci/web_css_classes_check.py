# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Verify that utility classes in Svelte templates generate CSS in the build output.

Scans Svelte templates for static utility class tokens matching candidate
prefixes (bg, text, border, etc.) and ensures that corresponding CSS rules
exist in the built CSS output.
"""

import argparse
import dataclasses
import pathlib
import re
import sys

CANDIDATE_BASE_RE = re.compile(
    r"^(bg|text|border|ring|outline|fill|stroke|divide|from|via|to|accent|caret|decoration|placeholder)-[a-z][a-z0-9-]*$"
)

CLASS_ATTR_RE = re.compile(
    r"""\bclass\s*=\s*(?:"([^"]*)"|'([^']*)')""",
    re.DOTALL,
)


class CheckError(Exception):
    """Raised when scan or self-test encounters an error."""


@dataclasses.dataclass(frozen=True)
class Finding:
    file: str
    line: int
    token: str


def strip_expressions(raw: str) -> str:
    """Replace {...} expression blocks with spaces to preserve character offsets."""
    return re.sub(r"\{[^{}]*\}", lambda m: " " * len(m.group(0)), raw, flags=re.DOTALL)


def extract_candidate_tokens(content: str) -> list[tuple[str, int]]:
    """Extract candidate utility tokens and line numbers from class attributes."""
    candidates: list[tuple[str, int]] = []
    for m in CLASS_ATTR_RE.finditer(content):
        val = m.group(1) if m.group(1) is not None else m.group(2)
        val_start = m.start(1) if m.group(1) is not None else m.start(2)
        stripped = strip_expressions(val)
        for tok_match in re.finditer(r"\S+", stripped):
            tok = tok_match.group(0)
            if any(c in tok for c in ("{", "[", "$")):
                continue
            base = tok.split(":")[-1]
            if not CANDIDATE_BASE_RE.match(base):
                continue
            tok_offset = val_start + tok_match.start()
            line = content[:tok_offset].count("\n") + 1
            candidates.append((tok, line))
    return candidates


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
    """Run self-test against clean and planted fixtures."""
    fixtures_dir = root / "tools" / "ci" / "testdata" / "web_css_classes"
    clean_dir = fixtures_dir / "clean"
    planted_dir = fixtures_dir / "planted"

    if not clean_dir.is_dir() or not planted_dir.is_dir():
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

    print(
        f"Web CSS classes self-test: clean and planted fixtures verified "
        f"({len(planted_findings)} planted defect caught)"
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

    print("Web CSS classes: all Svelte utility classes verified in built CSS, 0 findings")
    return 0


if __name__ == "__main__":
    sys.exit(main())
