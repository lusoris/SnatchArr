# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Scan Svelte files for raw markup text and unlocalized attributes.

Prohibits hardcoded user-facing text in markup and in attributes
(aria-label, title, placeholder, alt) that bypass Paraglide i18n, both as
plain text and as string literals an expression renders ({ok ? 'Saved' : x}).
Only literals in value positions count (the expression itself, conditional
branches, logical operands, + concatenation, template text); comparisons and
call arguments such as {mode === 'random'} are not rendered and stay allowed.
Allow-list permits punctuation/separators, technical units, and code inside <code>.
"""

import argparse
import json
import pathlib
import subprocess
import sys

TIMEOUT_SECONDS = 60
NODE_CHECKER_SCRIPT = r"""
const path = require('path');
const fs = require('fs');

const root = process.argv[1];
const sveltePath = path.resolve(root, 'web/node_modules/svelte/compiler');
const { parse } = require(sveltePath);

const isPunctuationOrSymbol = /^[\p{P}\p{S}\s\d]+$/u;
const allowedUnits = /^(?:[KMGT]?i?B(?:\/s)?|[kMG]bps|px|rem|em|ms|s|h|%|4K)$/i;

function isAllowedText(text) {
  if (!text) return true;
  const tokens = text.trim().split(/\s+/);
  return tokens.every((t) => isPunctuationOrSymbol.test(t) || allowedUnits.test(t));
}

// String literals an expression renders: the value itself, both branches of a
// conditional, logical operands, + concatenation and template text. Comparisons,
// calls and member access are not followed, since their literals are not shown.
function renderedLiterals(expr, out) {
  if (!expr) return;
  switch (expr.type) {
    case 'Literal':
      if (typeof expr.value === 'string') out.push({ text: expr.value, start: expr.start });
      break;
    case 'TemplateLiteral':
      for (const q of expr.quasis) out.push({ text: q.value.cooked ?? '', start: q.start });
      for (const e of expr.expressions) renderedLiterals(e, out);
      break;
    case 'ConditionalExpression':
      renderedLiterals(expr.consequent, out);
      renderedLiterals(expr.alternate, out);
      break;
    case 'LogicalExpression':
      renderedLiterals(expr.left, out);
      renderedLiterals(expr.right, out);
      break;
    case 'BinaryExpression':
      if (expr.operator === '+') {
        renderedLiterals(expr.left, out);
        renderedLiterals(expr.right, out);
      }
      break;
    case 'SequenceExpression':
      renderedLiterals(expr.expressions[expr.expressions.length - 1], out);
      break;
  }
}

const files = process.argv.slice(2);
const findings = [];

function checkSnippet(source, filename) {
  const ast = parse(source);
  const lines = source.split('\n');
  function getLine(pos) {
    let count = 0;
    for (let i = 0; i < lines.length; i++) {
      count += lines[i].length + 1;
      if (pos < count) return i + 1;
    }
    return lines.length;
  }

  function checkExpression(tag, where) {
    const found = [];
    renderedLiterals(tag.expression, found);
    for (const lit of found) {
      const text = lit.text.trim();
      if (!isAllowedText(text)) {
        findings.push({
          file: filename,
          line: getLine(lit.start),
          message: `${where} expression literal "${text}" bypasses i18n`
        });
      }
    }
  }

  function walk(node, inCode) {
    if (!node) return;
    const isCode = inCode || (node.type === 'Element' && node.name === 'code');

    if (node.attributes) {
      for (const attr of node.attributes) {
        if (attr.type === 'Attribute' && ['aria-label', 'title', 'placeholder', 'alt'].includes(attr.name)) {
          if (Array.isArray(attr.value)) {
            for (const v of attr.value) {
              if (v.type === 'MustacheTag') {
                checkExpression(v, `[${attr.name}]`);
              } else if (v.type === 'Text') {
                const text = v.data.trim();
                if (attr.name === 'alt' && !text) continue;
                if (!isAllowedText(text)) {
                  findings.push({
                    file: filename,
                    line: getLine(v.start),
                    message: `[${attr.name}] literal "${text}" bypasses i18n`
                  });
                }
              }
            }
          }
        }
      }
    }

    if (node.type === 'Element' && node.name === 'code') return;

    if (node.children) {
      for (const child of node.children) {
        if (child.type === 'Text') {
          const text = child.data.trim();
          if (!isAllowedText(text)) {
            findings.push({
              file: filename,
              line: getLine(child.start),
              message: `<${node.name || 'element'}> markup text "${text}" bypasses i18n`
            });
          }
        } else if (child.type === 'MustacheTag') {
          checkExpression(child, `<${node.name || 'element'}>`);
        } else {
          walk(child, isCode);
        }
      }
    }
    if (node.else) walk(node.else, inCode);
    if (node.snippet) walk(node.snippet, inCode);
    if (node.body) walk(node.body, inCode);
    for (const branch of ['pending', 'then', 'catch']) {
      if (node[branch]) walk(node[branch], inCode);
    }
  }
  walk(ast.html, false);
}

for (const file of files) {
  try {
    const content = fs.readFileSync(file, 'utf8');
    checkSnippet(content, file);
  } catch (err) {
    findings.push({ file, line: 1, message: `parse error: ${err.message}` });
  }
}
console.log(JSON.stringify(findings));
"""


class ScanError(Exception):
    """A scan that failed or that found unlocalized strings."""


def run_checker(root: pathlib.Path, file_paths: list[str]) -> list[dict]:
    cmd = ["node", "-e", NODE_CHECKER_SCRIPT, "--", str(root), *file_paths]
    try:
        proc = subprocess.run(
            cmd,
            cwd=str(root),
            capture_output=True,
            text=True,
            timeout=TIMEOUT_SECONDS,
            check=True,
        )
    except subprocess.CalledProcessError as err:
        sys.stderr.write(err.stderr)
        raise ScanError(f"node checker exited {err.returncode}: {err.stderr}") from err
    except subprocess.TimeoutExpired as err:
        raise ScanError("node checker timed out") from err

    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError as err:
        raise ScanError(f"invalid JSON from node checker: {proc.stdout}") from err


def self_test(root: pathlib.Path) -> None:
    fixtures_dir = root / "tools" / "ci" / "testdata" / "web_strings"
    clean_fixture = fixtures_dir / "clean.svelte"
    planted_fixture = fixtures_dir / "planted.svelte"

    if not clean_fixture.is_file() or not planted_fixture.is_file():
        raise ScanError(f"missing test fixtures in {fixtures_dir}")

    # 1. Clean fixture must produce 0 findings
    clean_findings = run_checker(root, [str(clean_fixture)])
    if clean_findings:
        raise ScanError(
            f"self-test clean fixture failed with {len(clean_findings)} findings: {clean_findings}"
        )

    # 2. Planted fixture must produce findings
    planted_findings = run_checker(root, [str(planted_fixture)])
    if not planted_findings:
        raise ScanError(
            "self-test planted fixture produced 0 findings; scanner failed to detect defects"
        )

    actions_found = any("Actions" in f["message"] for f in planted_findings)
    delete_found = any("Delete" in f["message"] for f in planted_findings)
    if not actions_found:
        raise ScanError("self-test planted fixture did not detect markup 'Actions'")
    if not delete_found:
        raise ScanError("self-test planted fixture did not detect attribute 'Delete'")
    for word in ("Saved", "Failed", "Working", "items", "Hello", "Nested", "Awaited"):
        if not any(
            f'"{word}' in f["message"] or f" {word}" in f["message"]
            for f in planted_findings
        ):
            raise ScanError(
                f"self-test planted fixture did not detect expression literal '{word}'"
            )

    print(
        f"Web strings self-test: clean and planted fixtures verified ({len(planted_findings)} planted defects caught)"
    )


def scan(root: pathlib.Path) -> None:
    web_src = root / "web" / "src"
    if not web_src.is_dir():
        raise ScanError(f"web source directory {web_src} not found")

    target_files = sorted(str(p.relative_to(root)) for p in web_src.rglob("*.svelte"))
    if not target_files:
        raise ScanError(f"no .svelte files found in {web_src}")

    findings = run_checker(root, target_files)
    if findings:
        for f in findings:
            print(f"{f['file']}:{f['line']}: {f['message']}", file=sys.stderr)
        raise ScanError(
            f"{len(findings)} unlocalized string(s) detected across {len(target_files)} files"
        )

    print(f"Web strings: {len(target_files)} Svelte files scanned, 0 findings")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--root",
        default=str(pathlib.Path(__file__).resolve().parent.parent.parent),
        help="Repository root directory",
    )
    args = parser.parse_args()
    root = pathlib.Path(args.root).resolve()

    try:
        self_test(root)
        scan(root)
    except ScanError as err:
        print(f"Web strings scan failed: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
