# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""List tracked or local Python files started by lefthook via python.sh and their sibling imports.

Scans `lefthook.yml` and `.config/lefthook/*.yml` for lines invoking `.config/lefthook/python.sh`,
collects every `.py` token on those lines, and walks `Import` / `ImportFrom` AST nodes transitively
for sibling `<dir>/<module>.py` files so `ruff_check.sh` holds every hook module to the floor.
"""

from __future__ import annotations

import argparse
import ast
import sys
from pathlib import Path

MAX_CONFIG_LINES = 10000
MAX_HOOK_FILES = 1024
MAX_AST_NODES = 100000


def lefthook_configs(root: Path) -> list[Path]:
    """Return existing lefthook YAML configuration files under ROOT."""
    candidates = [root / "lefthook.yml"]
    extra_dir = root / ".config" / "lefthook"
    if extra_dir.is_dir():
        candidates.extend(sorted(extra_dir.glob("*.yml")))
    return [path for path in candidates if path.is_file()]


def entry_scripts(root: Path) -> set[str]:
    """Extract `.py` script paths from lefthook lines that invoke `python.sh`."""
    found: set[str] = set()
    for cfg in lefthook_configs(root):
        lines = cfg.read_text(encoding="utf-8", errors="replace").splitlines()[:MAX_CONFIG_LINES]
        for line in lines:
            stripped = line.lstrip()
            if stripped.startswith("#") or "python.sh" not in stripped:
                continue
            for token in stripped.split():
                cleaned = token.strip("'\" ;")
                if cleaned.endswith(".py"):
                    rel = Path(cleaned).as_posix()
                    if (root / rel).is_file():
                        found.add(rel)
    return found


def imported_modules(tree: ast.AST) -> set[str]:
    """Collect candidate sibling module names from `Import` and `ImportFrom` nodes in TREE."""
    modules: set[str] = set()
    for count, node in enumerate(ast.walk(tree), 1):
        if count > MAX_AST_NODES:
            break
        if isinstance(node, ast.Import):
            for alias in node.names:
                modules.add(alias.name.partition(".")[0])
        elif isinstance(node, ast.ImportFrom):
            if node.level in (0, 1) and node.module:
                modules.add(node.module.partition(".")[0])
            elif node.level == 1 and node.module is None:
                for alias in node.names:
                    modules.add(alias.name)
    return modules


def sibling_imports(root: Path, rel_path: str) -> list[str]:
    """Return existing sibling `.py` paths imported by REL_PATH under ROOT."""
    full = root / rel_path
    if not full.is_file():
        return []
    try:
        tree = ast.parse(full.read_text(encoding="utf-8"), filename=rel_path)
    except (OSError, SyntaxError, UnicodeDecodeError):
        return []
    rel_dir = Path(rel_path).parent
    siblings: list[str] = []
    for mod in sorted(imported_modules(tree)):
        candidate = (rel_dir / f"{mod}.py").as_posix()
        if (root / candidate).is_file():
            siblings.append(candidate)
    return siblings


def collect_hook_files(root: Path) -> list[str]:
    """Return sorted repo-relative paths of all hook Python scripts and transitive siblings."""
    found = entry_scripts(root)
    queue = sorted(found)
    idx = 0
    while idx < len(queue) and idx < MAX_HOOK_FILES:
        for sib in sibling_imports(root, queue[idx]):
            if sib not in found and len(found) < MAX_HOOK_FILES:
                found.add(sib)
                queue.append(sib)
        idx += 1
    return sorted(found)


def main(argv: list[str] | None = None) -> int:
    """Parse CLI arguments and print sorted hook Python paths under ROOT."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("root", nargs="?", default=".", help="repository root (default .)")
    args = parser.parse_args(argv)
    for path in collect_hook_files(Path(args.root)):
        print(path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
