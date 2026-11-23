# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Planted cases for guard_definition_check.py: each builds a base and a merge commit and checks
the exit status and the exact stdout lines.

Usage: python3 -I guard_definition_check_test.py CHECK_PY [CASE ...]

CHECK_PY is the checker under test; CASE names run only those cases. Each case loads a fresh copy
of CHECK_PY and calls its main() in this process; every git call the checker makes carries its own
deadline. Exit status: 0 = every case passed, 1 = a case failed, 2 = usage error.
"""

from __future__ import annotations

import ast
import contextlib
import importlib.util
import io
import os
import subprocess  # nosec B404  # runs only git with fixed argument lists
import sys
import tempfile
from pathlib import Path
from types import ModuleType

WF = ".github/workflows/workflow-refs.yml"
PR = "7"
GIT_ID = [
    "-c",
    "user.name=fixture",
    "-c",
    "user.email=fixture@invalid",
    "-c",
    "commit.gpgsign=false",
]
REFUSE = (
    f"refusing: pull request {PR} changes the workflow-refs guard; review the change, then merge "
    f"with GUARD_DEFINITION_ACK='{PR}: <reason, 20 to 200 printable characters>'"
)


def unchanged(count: int) -> str:
    """Return the pass line naming count compared files."""
    return f"guard-definition: pull request {PR} leaves the workflow-refs guard unchanged ({count} files compared)"


UNCHANGED = unchanged(26)
WORKFLOW = """name: Workflow refs
on:
  pull_request:
  schedule:
    - cron: '17 4 * * *'
jobs:
  workflow-refs:
    runs-on: ubuntu-24.04
    timeout-minutes: 10
    steps:
      - name: PyYAML
        run: bash tools/ci/pyyaml_venv.sh "$RUNNER_TEMP/pyyaml"
      - name: Planted cases
        run: bash tools/ci/workflow_refs_test.sh
      - name: Check
        run: bash tools/ci/workflow_refs.sh
"""
LAYOUT = """# Workflow refs guard, layout only
name: Workflow refs  # trailing comment
on:
    pull_request:
    schedule: [{cron: "17 4 * * *"}]
jobs:
    workflow-refs:
        runs-on: ubuntu-24.04
        timeout-minutes: 10
        steps:
        -   {name: PyYAML, run: bash tools/ci/pyyaml_venv.sh "$RUNNER_TEMP/pyyaml"}
        -   name: Planted cases
            run:    bash tools/ci/workflow_refs_test.sh
        -   name: Check
            run: bash tools/ci/workflow_refs.sh
"""
LEFTHOOK = """commit-msg:
  commands:
    commit-policy:
      run: sh .config/hooks/commit-msg.sh {1}
    guard-definition:
      run: bash tools/ci/guard_definition_check.sh {1}
"""
BASE = {
    WF: WORKFLOW,
    "lefthook.yml": LEFTHOOK,
    "tools/ci/pyyaml_venv.sh": 'python3 -I "$HERE/run_bounded.py" 60 pip install\n',
    "tools/ci/workflow_refs_test.sh": "bash tools/ci/workflow_refs.sh --planted\n",
    "tools/ci/workflow_refs.sh": 'HERE=$(dirname "$0")\npython3 -I "${HERE}/workflow_refs.py"\n',
    "tools/ci/workflow_refs.py": "import sys\nfrom workflow_refs_lib import read\n",
    "tools/ci/workflow_refs_lib.py": "def read():\n    return 1\n",
    "tools/ci/run_bounded.py": "import os\n",
    "tools/ci/guard_definition_check.sh": "echo sh\n",
    "tools/ci/guard_definition_check.py": "print('py')\n",
    "tools/ci/unrelated.sh": "echo unrelated\n",
    "README.md": "readme\n",
}


def merge_bomb(levels: int) -> str:
    """Return a pre-commit hook whose merge keys name the level below ten times per level.

    yaml.safe_load copies every merged pair into each mapping, 10**levels pairs at the top; the
    checker reads each merged mapping once.
    """
    lines = ["pre-commit:", "  l0: &l0 {a: 1}"]
    for n in range(1, levels + 1):
        lines.append(f"  l{n}: &l{n} {{<<: [{', '.join([f'*l{n - 1}'] * 10)}]}}")
    return "\n".join(lines) + "\n"


def alias_bomb(levels: int) -> str:
    """Return a pre-commit hook whose lists name the list below ten times per level."""
    lines = ["pre-commit:", f"  a0: &a0 [{', '.join(['x'] * 10)}]"]
    for n in range(1, levels + 1):
        lines.append(f"  a{n}: &a{n} [{', '.join([f'*a{n - 1}'] * 10)}]")
    return "\n".join(lines) + "\n"


def wide_loop(width: int) -> str:
    """Return a pre-commit alias loop that guards against width x MAX_NODES stack growth."""
    return "pre-commit: &loop [" + "x, " * (width - 1) + "*loop]\n"


def wide_map_loop(width: int) -> str:
    """Return a mapping alias loop, the mapping counterpart of width x MAX_NODES stack growth."""
    entries = ", ".join(f"k{n}: *loop" for n in range(width))
    return f"pre-commit: &loop {{{entries}}}\n"


OTHER_HOOK = "pre-commit:\n  commands:\n    lint:\n      run: true\n"
CUSTOM_HOOK = "my-hook:\n  commands:\n    lint:\n      run: true\n"


def label(path: str) -> str:
    """Return the change line the checker prints for path."""
    return f"guard-definition: {path}: changed by pull request {PR}"


def refused(*paths: str) -> list[str]:
    """Return the stdout lines of a refusal naming paths."""
    return [label(path) for path in sorted(paths)] + [REFUSE]


def git(repo: Path, *args: str) -> None:
    """Run git in repo with the fixture identity; raise on failure."""
    subprocess.run(  # nosec B603  # fixed git argv list, no shell
        ["git", "-C", str(repo), *GIT_ID, *args],
        check=True,
        capture_output=True,
        timeout=30,
    )


def write_tree(repo: Path, tree: dict[str, str | None]) -> None:
    """Write each path's text into repo (None deletes, a leading '+x ' sets the execute bit)."""
    for path, text in tree.items():
        target = repo / path
        if text is None:
            target.unlink(missing_ok=True)
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        executable = text.startswith("+x ")
        target.write_text(text.removeprefix("+x "))
        target.chmod(0o755 if executable else 0o644)


def build(
    root: Path, base: dict[str, str | None], merge: dict[str, str | None]
) -> Path:
    """Create a repository with a base commit and a merge commit on top; return its git dir."""
    repo = root / "repo"
    repo.mkdir()
    git(repo, "init", "-q", "-b", "main")
    write_tree(repo, base)
    git(repo, "add", "-A")
    git(repo, "commit", "-q", "-m", "base")
    write_tree(repo, merge)
    git(repo, "add", "-A")
    git(repo, "commit", "-q", "--allow-empty", "-m", "merge")
    return repo / ".git"


def load_checker(check: str) -> ModuleType:
    """Load a fresh copy of the checker module from its path, so no case sees another's state."""
    spec = importlib.util.spec_from_file_location("guard_definition_check_case", check)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {check}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def call_main(check: str, argv: list[str], env: dict[str, str]) -> tuple[int, str, str]:
    """Call the checker's main(argv) with os.environ minus the ack plus env; restore os.environ.

    Return (exit status, stdout, stderr); an argparse exit counts with its own status.
    """
    module = load_checker(check)
    saved = dict(os.environ)
    os.environ.pop("GUARD_DEFINITION_ACK", None)
    os.environ.update(env)
    out, err = io.StringIO(), io.StringIO()
    try:
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            try:
                status = module.main(argv)
            except SystemExit as exc:
                status = exc.code if isinstance(exc.code, int) else 1
    finally:
        os.environ.clear()
        os.environ.update(saved)
    return status, out.getvalue(), err.getvalue()


def run_check(
    check: str, git_dir: Path, extra: list[str], env: dict[str, str]
) -> tuple[int, list[str], str]:
    """Run the checker on HEAD~1 and HEAD; return (exit status, stdout lines, stderr)."""
    argv = ["--git-dir", str(git_dir), "--base", "HEAD~1", "--merge", "HEAD"]
    argv += ["--pr", PR] if not extra else extra
    status, out, err = call_main(check, argv, env)
    return status, out.splitlines(), err


def many_refs(count: int, present: bool) -> dict[str, str | None]:
    """Return a workflow_refs.sh naming count tools/ci files, written too when present."""
    names = [f"tools/ci/f{n}.sh" for n in range(count)]
    tree: dict[str, str | None] = {
        "tools/ci/workflow_refs.sh": "".join(f"bash {n}\n" for n in names)
    }
    if present:
        tree |= {name: "x\n" for name in names}
    return tree


def ack_line(reason: str) -> str:
    """Return the acknowledged line for reason."""
    return f"guard-definition: change acknowledged for pull request {PR}: {reason}"


REASON = "reviewed the bounded fetch change"
NOT_IN_FORM = (
    "guard-definition: GUARD_DEFINITION_ACK ignored: not in the form "
    "'<N>: <reason, 20 to 200 printable characters>'"
)
NO_BASE = {WF: None}
CASES: dict[str, tuple[dict, dict, dict, list[str], int, list[str]]] = {
    # name: (base edits, merge edits, env, extra argv, rc, stdout lines)
    "first-introduction": (
        NO_BASE,
        {WF: WORKFLOW},
        {},
        [],
        0,
        [f"guard-definition: default branch has no {WF}; first introduction passes"],
    ),
    "unchanged": (
        {},
        {"README.md": "new\n", "tools/ci/unrelated.sh": "echo 2\n"},
        {},
        [],
        0,
        [UNCHANGED],
    ),
    "validator-changed": (
        {},
        {"tools/ci/workflow_refs.sh": "exit 0\n"},
        {},
        [],
        1,
        refused("tools/ci/workflow_refs.sh"),
    ),
    "layout-only": ({}, {WF: LAYOUT}, {}, [], 0, [UNCHANGED]),
    "quoted-scalar": (
        {},
        {WF: WORKFLOW.replace("timeout-minutes: 10", "timeout-minutes: '10'")},
        {},
        [],
        1,
        refused(WF),
    ),
    "duplicate-key": (
        {},
        {
            WF: WORKFLOW.replace(
                "    timeout-minutes: 10\n",
                "    timeout-minutes: 10\n    timeout-minutes: 10\n",
            )
        },
        {},
        [],
        1,
        [f"guard-definition: {WF}:10: duplicate key 'timeout-minutes'"],
    ),
    "step-removed": (
        {},
        {
            WF: WORKFLOW.replace(
                "      - name: Planted cases\n        run: bash tools/ci/workflow_refs_test.sh\n",
                "",
            )
        },
        {},
        [],
        1,
        refused(WF),
    ),
    "workflow-deleted": ({}, {WF: None}, {}, [], 1, refused(WF)),
    "unparsable-merge": ({}, {WF: WORKFLOW + "  bad: [\n"}, {}, [], 1, refused(WF)),
    "unparsable-both-differ": (
        {WF: WORKFLOW + "  bad: [\n"},
        {WF: WORKFLOW + "  bad: [x\n"},
        {},
        [],
        1,
        refused(WF),
    ),
    "unparsable-both-same": (
        {WF: WORKFLOW + "  bad: [\n"},
        {"README.md": "new\n"},
        {},
        [],
        0,
        [unchanged(20)],
    ),
    "ack-valid": (
        {},
        {"tools/ci/workflow_refs.sh": "exit 0\n"},
        {"GUARD_DEFINITION_ACK": f"{PR}: {REASON}"},
        [],
        0,
        [label("tools/ci/workflow_refs.sh"), ack_line(REASON)],
    ),
    "ack-other-pr": (
        {},
        {"tools/ci/workflow_refs.sh": "exit 0\n"},
        {"GUARD_DEFINITION_ACK": f"8: {REASON}"},
        [],
        1,
        [
            label("tools/ci/workflow_refs.sh"),
            f"guard-definition: GUARD_DEFINITION_ACK ignored: names pull request 8, not {PR}",
            REFUSE,
        ],
    ),
    "ack-short-reason": (
        {},
        {"tools/ci/workflow_refs.sh": "exit 0\n"},
        {"GUARD_DEFINITION_ACK": f"{PR}: ok"},
        [],
        1,
        [label("tools/ci/workflow_refs.sh"), NOT_IN_FORM, REFUSE],
    ),
    "self-changed": (
        {},
        {"tools/ci/guard_definition_check.py": "print(0)\n"},
        {},
        [],
        1,
        refused("tools/ci/guard_definition_check.py"),
    ),
    "self-sh-changed": (
        {},
        {"tools/ci/guard_definition_check.sh": "exit 0\n"},
        {},
        [],
        1,
        refused("tools/ci/guard_definition_check.sh"),
    ),
    "hook-removed": (
        {},
        {"lefthook.yml": LEFTHOOK.split("    guard-definition:")[0]},
        {},
        [],
        1,
        [
            f"guard-definition: lefthook.yml: commit-msg guard-definition: changed by pull request {PR}",
            REFUSE,
        ],
    ),
    "hook-other-command": (
        {},
        {"lefthook.yml": LEFTHOOK.replace("commit-msg.sh {1}", "commit-msg.sh {1} x")},
        {},
        [],
        0,
        [UNCHANGED],
    ),
    "hook-skip": (
        {},
        {"lefthook.yml": LEFTHOOK + "  skip: true\n"},
        {},
        [],
        1,
        refused("lefthook.yml: commit-msg hook controls"),
    ),
    "hook-jobs": (
        {},
        {"lefthook.yml": LEFTHOOK + "  jobs:\n    - run: true\n"},
        {},
        [],
        1,
        refused("lefthook.yml: commit-msg hook controls"),
    ),
    "top-level-setting": (
        {},
        {"lefthook.yml": "no_tty: true\n" + LEFTHOOK},
        {},
        [],
        1,
        refused("lefthook.yml: top-level settings and custom hooks"),
    ),
    "custom-hook-added": (
        {},
        {"lefthook.yml": LEFTHOOK + CUSTOM_HOOK},
        {},
        [],
        1,
        refused("lefthook.yml: top-level settings and custom hooks"),
    ),
    "other-git-hook-added": (
        {},
        {"lefthook.yml": LEFTHOOK + OTHER_HOOK},
        {},
        [],
        0,
        [UNCHANGED],
    ),
    "rc-key": (
        {},
        {"lefthook.yml": "rc: .lefthookrc\n" + LEFTHOOK},
        {},
        [],
        1,
        refused(
            "lefthook.yml: rc names files this check does not read",
            "lefthook.yml: top-level settings and custom hooks",
        ),
    ),
    "extends-kept": (
        {"lefthook.yml": "extends: [base.yml]\n" + LEFTHOOK},
        {"README.md": "new\n"},
        {},
        [],
        1,
        refused("lefthook.yml: extends names files this check does not read"),
    ),
    "local-config-added": (
        {},
        {"lefthook-local.yml": "commit-msg:\n  skip: true\n"},
        {},
        [],
        1,
        refused("lefthook-local.yml"),
    ),
    "local-config-nested": (
        {},
        {".config/lefthook-local.toml": "[commit-msg]\nskip = true\n"},
        {},
        [],
        1,
        refused(".config/lefthook-local.toml"),
    ),
    "merge-bomb-other-hook": (
        {},
        {"lefthook.yml": LEFTHOOK + merge_bomb(13)},
        {},
        [],
        0,
        [UNCHANGED],
    ),
    "alias-bomb-other-hook": (
        {},
        {"lefthook.yml": LEFTHOOK + alias_bomb(6)},
        {},
        [],
        1,
        [
            "guard-definition: lefthook.yml: more than 100000 YAML nodes after aliases and merges"
        ],
    ),
    "wide-alias-loop-other-hook": (
        {},
        {"lefthook.yml": LEFTHOOK + wide_loop(1024)},
        {},
        [],
        1,
        [
            "guard-definition: lefthook.yml: more than 100000 YAML nodes after aliases and merges"
        ],
    ),
    "wide-map-loop-other-hook": (
        {},
        {"lefthook.yml": LEFTHOOK + wide_map_loop(1024)},
        {},
        [],
        1,
        [
            "guard-definition: lefthook.yml: more than 100000 YAML nodes after aliases and merges"
        ],
    ),
    "hook-duplicate-key": (
        {},
        {"lefthook.yml": LEFTHOOK + OTHER_HOOK + OTHER_HOOK},
        {},
        [],
        1,
        ["guard-definition: lefthook.yml:11: duplicate key 'pre-commit'"],
    ),
    "here-dependency": (
        {},
        {"tools/ci/run_bounded.py": "import time\n"},
        {},
        [],
        1,
        refused("tools/ci/run_bounded.py"),
    ),
    "import-dependency": (
        {},
        {"tools/ci/workflow_refs_lib.py": "def read():\n    return 0\n"},
        {},
        [],
        1,
        refused("tools/ci/workflow_refs_lib.py"),
    ),
    "mode-changed": (
        {},
        {"tools/ci/workflow_refs.sh": "+x " + BASE["tools/ci/workflow_refs.sh"]},
        {},
        [],
        1,
        refused("tools/ci/workflow_refs.sh"),
    ),
    "validator-deleted": (
        {},
        {"tools/ci/workflow_refs.py": None},
        {},
        [],
        1,
        refused("tools/ci/workflow_refs.py"),
    ),
    "new-step-file": (
        {},
        {
            WF: WORKFLOW + "      - run: bash tools/ci/new_check.sh\n",
            "tools/ci/new_check.sh": "x\n",
        },
        {},
        [],
        1,
        refused(WF, "tools/ci/new_check.sh"),
    ),
    "dropped-step-file-changed": (
        {},
        {
            WF: WORKFLOW.replace(
                '        run: bash tools/ci/pyyaml_venv.sh "$RUNNER_TEMP/pyyaml"\n',
                "        run: true\n",
            ),
            "tools/ci/pyyaml_venv.sh": "exit 0\n",
        },
        {},
        [],
        1,
        refused(WF, "tools/ci/pyyaml_venv.sh"),
    ),
    "max-files": (
        many_refs(61, True),
        {},
        {},
        [],
        1,
        ["guard-definition: validator file set passed MAX_FILES (64)"],
    ),
    "max-files-boundary": (many_refs(60, True), {}, {}, [], 0, [unchanged(84)]),
    "max-candidates": (
        many_refs(508, False),
        {},
        {},
        [],
        1,
        ["guard-definition: validator candidates passed MAX_CANDIDATES (512)"],
    ),
    "max-candidates-boundary": (many_refs(507, False), {}, {}, [], 0, [unchanged(24)]),
    "self-in-workflow": (
        {WF: WORKFLOW + "      - run: bash tools/ci/guard_definition_check.sh\n"},
        {"tools/ci/guard_definition_check.sh": "exit 0\n"},
        {},
        [],
        1,
        refused("tools/ci/guard_definition_check.sh"),
    ),
    "max-bytes": (
        {"tools/ci/workflow_refs.sh": "#" * (1024 * 1024) + "\n"},
        {},
        {},
        [],
        1,
        [
            "guard-definition: tools/ci/workflow_refs.sh at HEADSHORT is larger than MAX_BYTES (1048576)"
        ],
    ),
    "bad-revision": (
        {},
        {},
        {},
        ["--pr", PR, "--base", "nosuchrev"],
        1,
        ["guard-definition: revision nosuchrev names no commit"],
    ),
    "pr-zero": ({}, {}, {}, ["--pr", "0"], 2, []),
    "pr-word": ({}, {}, {}, ["--pr", "abc"], 2, []),
    "revision-dash": ({}, {}, {}, ["--pr", PR, "--base=-x"], 2, []),
}


def expand(lines: list[str], git_dir: Path) -> list[str]:
    """Replace HEADSHORT with the first 12 hex digits of the base commit id."""
    if not any("HEADSHORT" in line for line in lines):
        return lines
    out = subprocess.run(  # nosec B603  # fixed git argv list, no shell
        ["git", "--git-dir", str(git_dir), "rev-parse", "HEAD~1"],
        capture_output=True,
        text=True,
        timeout=30,
        check=True,
    )
    return [line.replace("HEADSHORT", out.stdout.strip()[:12]) for line in lines]


def run_case(check: str, name: str) -> bool:
    """Run one case; print ok or FAIL with the difference."""
    base_edit, merge_edit, env, extra, want_rc, want = CASES[name]
    with tempfile.TemporaryDirectory(prefix="guard-def-") as tmp:
        git_dir = build(Path(tmp), BASE | base_edit, merge_edit)
        if extra:
            extra = [
                "--git-dir",
                str(git_dir),
                "--merge",
                "HEAD",
                "--base",
                "HEAD~1",
                *extra,
            ]
        rc, lines, err = run_check(check, git_dir, extra, env)
        want = expand(want, git_dir)
    if rc == want_rc and lines == want:
        print(f"ok {name}")
        return True
    print(
        f"FAIL {name}: rc {rc} want {want_rc}\n  got  {lines}\n  want {want}\n  stderr {err.strip()[-300:]}"
    )
    return False


def run_no_yaml(check: str) -> bool:
    """Run the checker with PyYAML hidden from import: it exits 2 with its stderr line only."""
    with tempfile.TemporaryDirectory(prefix="guard-def-") as tmp:
        git_dir = build(Path(tmp), BASE, {})
        argv = [
            "--git-dir",
            str(git_dir),
            "--base",
            "HEAD~1",
            "--merge",
            "HEAD",
            "--pr",
            PR,
        ]
        saved = sys.modules.get("yaml")
        sys.modules["yaml"] = None  # type: ignore[assignment]  # import yaml now raises ImportError
        try:
            status, out, err = call_main(check, argv, {})
        finally:
            if saved is None:
                sys.modules.pop("yaml", None)
            else:
                sys.modules["yaml"] = saved
    if status == 2 and err == "guard-definition: PyYAML is required\n" and not out:
        print("ok no-pyyaml")
        return True
    print(f"FAIL no-pyyaml: rc {status} stdout {out!r} stderr {err!r}")
    return False


def run_self_imports(check: str) -> bool:
    """Check that SELF names every module beside CHECK_PY that CHECK_PY imports."""
    here = Path(check).resolve().parent
    tree = ast.parse(Path(check).read_text(encoding="utf-8"))
    names = {
        alias.name
        for node in ast.walk(tree)
        if isinstance(node, ast.Import)
        for alias in node.names
    }
    names |= {
        node.module
        for node in ast.walk(tree)
        if isinstance(node, ast.ImportFrom) and node.module
    }
    local = sorted(
        f"tools/ci/{name}.py" for name in names if (here / f"{name}.py").is_file()
    )
    missing = [path for path in local if path not in load_checker(check).SELF]
    if local and not missing:
        print("ok self-imports")
        return True
    print(f"FAIL self-imports: local imports {local}, missing from SELF {missing}")
    return False


def run_node_budget(check: str) -> bool:
    """Check load() at MAX_NODES (MAX_NODES - 2 scalars) and one scalar past MAX_NODES."""
    module = load_checker(check)
    yaml = module.load_yaml()
    if yaml is None:
        print("FAIL node-budget: PyYAML missing")
        return False
    limit = module.MAX_NODES
    at_limit = "pre-commit: [" + ", ".join(["x"] * (limit - 2)) + "]\n"
    try:
        doc = module.load(yaml, at_limit, "lefthook.yml")
    except module.GuardError as exc:
        print(f"FAIL node-budget: at MAX_NODES raised GuardError {exc}")
        return False
    items = doc.get("pre-commit") if isinstance(doc, dict) else None
    if not isinstance(items, list) or len(items) != limit - 2:
        got = len(items) if isinstance(items, list) else type(doc).__name__
        print(f"FAIL node-budget: at MAX_NODES got {got}, want {limit - 2} items")
        return False
    past_limit = "pre-commit: [" + ", ".join(["x"] * (limit - 1)) + "]\n"
    want = "lefthook.yml: more than 100000 YAML nodes after aliases and merges"
    try:
        module.load(yaml, past_limit, "lefthook.yml")
    except module.GuardError as exc:
        if str(exc) == want:
            print("ok node-budget")
            return True
        print(f"FAIL node-budget: GuardError {str(exc)!r} != {want!r}")
        return False
    print("FAIL node-budget: MAX_NODES + 1 did not raise GuardError")
    return False


EXTRA = {
    "no-pyyaml": run_no_yaml,
    "self-imports": run_self_imports,
    "node-budget": run_node_budget,
}


def main() -> int:
    """Run the selected cases; return 0 when all pass."""
    if len(sys.argv) < 2 or not Path(sys.argv[1]).is_file():
        print(
            "usage: guard_definition_check_test.py CHECK_PY [CASE ...]", file=sys.stderr
        )
        return 2
    check, names = sys.argv[1], sys.argv[2:] or [*CASES, *EXTRA]
    unknown = [n for n in names if n not in CASES and n not in EXTRA]
    if unknown:
        print(f"unknown cases: {unknown}", file=sys.stderr)
        return 2
    results = [EXTRA[n](check) if n in EXTRA else run_case(check, n) for n in names]
    print(
        f"guard-definition cases: {results.count(True)} passed, {results.count(False)} failed"
    )
    return 0 if all(results) else 1


if __name__ == "__main__":
    sys.exit(main())
