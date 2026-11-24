# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Offline planted self-test cases for the step-text and version-inputs scans of workflow_actions.py."""

from __future__ import annotations

import sys
import tempfile
from collections.abc import Callable
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import workflow_yaml
from workflow_actions import (
    collect_files_step_text,
    collect_files_version_inputs,
    collect_text_step_lines,
    scalar_value_lines,
)
from workflow_yaml import (
    MAX_STEP_TEXT_LINES,
    UsesError,
    compose_top_mapping,
)


def check_step_text_case(
    root: Path,
    name: str,
    rel_path: str,
    body: str,
    want: list[tuple[str, int, str]],
    *,
    repeat: bool = False,
) -> list[str]:
    """Write body at root/rel_path and compare collect_files_step_text against want."""
    full = root / rel_path
    full.parent.mkdir(parents=True, exist_ok=True)
    full.write_text(body, encoding="utf-8")
    args = [rel_path, rel_path] if repeat else [rel_path]
    try:
        got = collect_files_step_text(str(root), args)
    except UsesError as err:
        return [f"{name}: unexpected error: {err}"]
    if got != want:
        return [f"{name}: got {got!r}, want {want!r}"]
    print(f"ok   {name}")
    return []


def step_text_manifest_cases() -> list[
    tuple[str, str, str, list[tuple[str, int, str]], bool]
]:
    """Return composite block/plain and flow-manifest --step-text test tuples."""
    act_rel = ".github/actions/sample/action.yml"
    act_doc = (
        "name: sample\ndescription: go install golang.org/x/vuln/cmd/govulncheck@latest\n"
        "inputs:\n  x:\n    description: go install golang.org/x/vuln/cmd/govulncheck@latest\n"
        "runs:\n  using: composite\n  steps:\n    - name: block\n      shell: bash\n"
        "      run: |\n        echo first\n        go install example.com/cmd@v1.2.3\n"
        "    - name: single\n      shell: bash\n      run: go install x@v1.0.0\n"
        "    - uses: actions/checkout@v4\n      with:\n        ref: main\n"
    )
    flow_rel = ".github/actions/flow/action.yml"
    flow_doc = (
        "{name: sample, description: 'Example: go install example.com/tool',"
        " runs: {using: composite, steps: [{shell: bash, run: echo ok}]}}\n"
    )
    return [
        (
            "step-text-records",
            act_rel,
            act_doc,
            [
                (act_rel, 12, "echo first"),
                (act_rel, 13, "go install example.com/cmd@v1.2.3"),
                (act_rel, 16, "go install x@v1.0.0"),
                (act_rel, 19, "main"),
            ],
            True,
        ),
        (
            "step-text-flow-manifest",
            flow_rel,
            flow_doc,
            [(flow_rel, 1, "echo ok")],
            False,
        ),
    ]


def step_text_quoted_and_folded_cases() -> list[
    tuple[str, str, str, list[tuple[str, int, str]], bool]
]:
    """Return quoted/sanitized and multi-paragraph folded --step-text test tuples."""
    quoted_rel = ".github/workflows/quoted.yml"
    quoted_doc = (
        "jobs:\n  j:\n    steps:\n"
        "      - run: 'go install golang.org/x/vuln/cmd/govulncheck@latest'\n"
        '      - run: >-\n          folded one\n      - run: "nul\\0cr\\rend"\n'
    )
    folded_rel = ".github/actions/folded/action.yml"
    folded_doc = (
        "steps: &s\n  - run: >\n      echo a\n      echo b\n\n      echo c\n\n\n"
        "      echo d\n    shell: bash\nruns: {using: composite, steps: *s}\n"
    )
    return [
        (
            "step-text-quoted-folded-sanitized",
            quoted_rel,
            quoted_doc,
            [
                (quoted_rel, 4, "go install golang.org/x/vuln/cmd/govulncheck@latest"),
                (quoted_rel, 6, "folded one"),
                (quoted_rel, 7, "nul cr end"),
            ],
            False,
        ),
        (
            "step-text-folded-paragraphs",
            folded_rel,
            folded_doc,
            [
                (folded_rel, 3, "echo a echo b"),
                (folded_rel, 6, "echo c"),
                (folded_rel, 7, ""),
                (folded_rel, 9, "echo d"),
            ],
            False,
        ),
    ]


def step_text_folded_cursor_cases() -> list[
    tuple[str, str, str, list[tuple[str, int, str]], bool]
]:
    """Return more-indented, prefix-collision and repeated-line folded --step-text tuples."""
    indented_rel = ".github/actions/folded_indent/action.yml"
    indented_doc = (
        "steps: &s\n  - run: >\n      a\n        b\n      c\n"
        "runs: {using: composite, steps: *s}\n"
    )
    prefix_rel = ".github/actions/folded_prefix/action.yml"
    prefix_doc = (
        "steps: &s\n  - run: >\n      x\n      y\n\n      y z\n    shell: bash\n"
        "runs: {using: composite, steps: *s}\n"
    )
    repeat_rel = ".github/actions/folded_repeat/action.yml"
    repeat_doc = (
        "steps: &s\n  - run: >\n      echo\n      echo\n\n      echo\n    shell: bash\n"
        "runs: {using: composite, steps: *s}\n"
    )
    return [
        (
            "step-text-folded-more-indented",
            indented_rel,
            indented_doc,
            [
                (indented_rel, 3, "a"),
                (indented_rel, 4, "  b"),
                (indented_rel, 5, "c"),
            ],
            False,
        ),
        (
            "step-text-folded-prefix-collision",
            prefix_rel,
            prefix_doc,
            [(prefix_rel, 3, "x y"), (prefix_rel, 6, "y z")],
            False,
        ),
        (
            "step-text-folded-repeat-line",
            repeat_rel,
            repeat_doc,
            [(repeat_rel, 3, "echo echo"), (repeat_rel, 6, "echo")],
            False,
        ),
    ]


def step_text_positive_table() -> list[
    tuple[str, str, str, list[tuple[str, int, str]], bool]
]:
    """Return planted --step-text documents and expected (file, line, text) records."""
    return [
        *step_text_manifest_cases(),
        *step_text_quoted_and_folded_cases(),
        *step_text_folded_cursor_cases(),
    ]


def check_step_text_alias_dedup(root: Path) -> list[str]:
    """Verify 64 steps aliasing one 4096-line block scalar expand once and yield 4096 records."""
    rel = ".github/workflows/aliased_big.yml"
    block = "".join(f"  echo line-{idx}\n" for idx in range(4096))
    steps = "".join("      - run: *big\n" for _ in range(64))
    doc = f"x-big: &big |\n{block}jobs:\n  j:\n    steps:\n{steps}"
    want_pairs = [(2 + idx, f"echo line-{idx}") for idx in range(4096)]
    try:
        pairs = collect_text_step_lines(doc, rel)
    except UsesError as err:
        return [f"step-text-alias-dedup: unexpected error: {err}"]
    if pairs != want_pairs:
        return [
            f"step-text-alias-dedup: collect_text_step_lines returned {len(pairs)} pairs, want 4096"
        ]
    want_records = [(rel, line_no, text) for line_no, text in want_pairs]
    return check_step_text_case(root, "step-text-alias-dedup", rel, doc, want_records)


def check_step_text_max_lines() -> list[str]:
    """Verify collect_text_step_lines stops at MAX_STEP_TEXT_LINES across distinct scalars."""
    chunk = "\\n".join(f"x{idx}" for idx in range(60000))
    steps = "".join(f'      - run: "{chunk}-{step_i}"\n' for step_i in range(5))
    doc = f"jobs:\n  j:\n    steps:\n{steps}"
    needle = f"over.yml:8: more than {MAX_STEP_TEXT_LINES} step text lines"
    try:
        got = collect_text_step_lines(doc, "over.yml")
        return [f"step-text-over-max-lines: accepted {len(got)} lines"]
    except UsesError as err:
        if needle not in str(err):
            return [f"step-text-over-max-lines: error {str(err)!r} lacks {needle!r}"]
        print("ok   step-text-over-max-lines")
        return []


def check_bad_path_cases(
    root: Path,
    prefix: str,
    runner: Callable[[str, list[str]], object],
) -> list[str]:
    """Verify runner refuses tab, lf (with real fixtures) and nul paths as invalid."""
    failures: list[str] = []
    for label, bad_arg, make_fixture in (
        ("tab", "a\tb.yml", True),
        ("lf", "a\nb.yml", True),
        ("nul", "a\x00b.yml", False),
    ):
        case_name = f"{prefix}-bad-{label}"
        skipped = False
        if make_fixture:
            try:
                (root / bad_arg).write_text("jobs: {}\n", encoding="utf-8")
            except OSError as err:
                print(f"skip {case_name}: {err}")
                skipped = True
        try:
            runner(str(root), [bad_arg])
            failures.append(f"{case_name}: accepted {bad_arg!r}")
        except UsesError as err:
            if "invalid file path" not in str(err):
                failures.append(
                    f"{case_name}: error {str(err)!r} lacks 'invalid file path'"
                )
            elif not skipped:
                print(f"ok   {case_name}")
        except ValueError as err:
            failures.append(f"{case_name}: unexpected ValueError: {err}")
    return failures


def check_folded_missing_buffer() -> list[str]:
    """Verify scalar_value_lines refuses a folded ScalarNode whose mark buffer is None."""
    top = compose_top_mapping("run: >\n  echo hi\n", "nobuf.yml")
    val_node = top["run"][1]
    if not isinstance(val_node, workflow_yaml.yaml.ScalarNode):
        return [
            f"step-text-folded-missing-buffer: run value is {type(val_node).__name__}, want ScalarNode"
        ]
    val_node.start_mark.buffer = None
    try:
        scalar_value_lines(val_node, "nobuf.yml")
        return ["step-text-folded-missing-buffer: accepted None buffer"]
    except UsesError as err:
        if "nobuf.yml:1" not in str(err):
            return [
                f"step-text-folded-missing-buffer: error {str(err)!r} lacks 'nobuf.yml:1'"
            ]
        print("ok   step-text-folded-missing-buffer")
        return []


def run_step_text_cases() -> list[str]:
    """Verify --step-text emits scalar lines with exact line numbers and refuses bad paths."""
    failures: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        for name, rel, doc, want, repeat in step_text_positive_table():
            failures.extend(
                check_step_text_case(root, name, rel, doc, want, repeat=repeat)
            )
        failures.extend(check_step_text_alias_dedup(root))
        failures.extend(check_step_text_max_lines())
        failures.extend(check_folded_missing_buffer())
        failures.extend(
            check_bad_path_cases(root, "step-text", collect_files_step_text)
        )
    return failures


def check_version_inputs_case(
    root: Path,
    name: str,
    rel_path: str,
    body: str,
    want: list[tuple[str, int, str, str]],
) -> list[str]:
    """Write body at root/rel_path and compare collect_files_version_inputs against want."""
    full = root / rel_path
    full.parent.mkdir(parents=True, exist_ok=True)
    full.write_text(body, encoding="utf-8")
    try:
        got = collect_files_version_inputs(str(root), [rel_path, rel_path])
    except UsesError as err:
        return [f"{name}: unexpected error: {err}"]
    if got != want:
        return [f"{name}: got {got!r}, want {want!r}"]
    print(f"ok   {name}")
    return []


def version_inputs_key_case_row(
    helm_sha: str,
) -> tuple[str, str, str, list[tuple[str, int, str, str]]]:
    """Return the planted --version-inputs document whose version keys differ in letter case."""
    case_rel = ".github/workflows/case.yml"
    case_doc = (
        f"jobs:\n  j:\n    steps:\n      - uses: {helm_sha}\n"
        "        with:\n          Version: v4.3.1\n"
        f"      - uses: {helm_sha}\n        with:\n          VERSION: latest\n"
    )
    return (
        "version-inputs-key-case",
        case_rel,
        case_doc,
        [
            (case_rel, 4, "azure/setup-helm", "v4.3.1"),
            (case_rel, 7, "azure/setup-helm", "latest"),
        ],
    )


def version_inputs_positive_table() -> list[
    tuple[str, str, str, list[tuple[str, int, str, str]]]
]:
    """Return planted --version-inputs documents and expected 4-tuple records."""
    helm_sha = "azure/setup-helm@9bc31f4ebc9c6b171d7bfbaa5d006ae7abdb4310"
    flow_rel = ".github/workflows/flow.yml"
    flow_doc = (
        f"jobs:\n  j:\n    steps:\n      - {{uses: {helm_sha}, with: {{}}}}\n"
        f"      - {{uses: {helm_sha}, with: {{version: v4.3.1}}}}\n"
        f"      - uses: {helm_sha}\n        with:\n          version: 3.19\n"
        f"      - uses: {helm_sha}\n        with:\n          version: [v4.3.1]\n"
        f'      - uses: {helm_sha}\n        with:\n          version: "v4.3.1\\tbad"\n'
    )
    alias_rel = ".github/workflows/alias.yml"
    alias_doc = (
        f"x-base: &base\n  uses: {helm_sha}\n  with:\n    version: latest\n"
        f"jobs:\n  first:\n    steps: &shared\n      - uses: {helm_sha}\n"
        "  second:\n    steps: *shared\n"
        "  merged:\n    steps:\n      - <<: *base\n        with:\n          version: v4.3.2\n"
    )
    comp_rel = ".github/actions/helm/action.yml"
    comp_doc = (
        f"runs:\n  using: composite\n  steps:\n    - uses: {helm_sha}\n"
        "      with:\n        version: v4.3.1\n"
    )
    return [
        version_inputs_key_case_row(helm_sha),
        (
            "version-inputs-flow-and-scalars",
            flow_rel,
            flow_doc,
            [
                (flow_rel, 4, "azure/setup-helm", ""),
                (flow_rel, 5, "azure/setup-helm", "v4.3.1"),
                (flow_rel, 6, "azure/setup-helm", "3.19"),
                (flow_rel, 9, "azure/setup-helm", ""),
                (flow_rel, 12, "azure/setup-helm", ""),
            ],
        ),
        (
            "version-inputs-alias-and-merge",
            alias_rel,
            alias_doc,
            [
                (alias_rel, 8, "azure/setup-helm", ""),
                (alias_rel, 2, "azure/setup-helm", "v4.3.2"),
            ],
        ),
        (
            "version-inputs-composite",
            comp_rel,
            comp_doc,
            [(comp_rel, 4, "azure/setup-helm", "v4.3.1")],
        ),
    ]


def check_version_input_twice(root: Path) -> list[str]:
    """Verify a with mapping naming version in two letter cases raises UsesError."""
    name = "version-inputs-key-twice"
    rel = ".github/workflows/twice.yml"
    full = root / rel
    full.parent.mkdir(parents=True, exist_ok=True)
    full.write_text(
        "jobs:\n  j:\n    steps:\n"
        "      - uses: azure/setup-helm@9bc31f4ebc9c6b171d7bfbaa5d006ae7abdb4310\n"
        "        with:\n          version: v4.3.1\n          Version: latest\n",
        encoding="utf-8",
    )
    want = f"{rel}:7: with names the version input twice (lines 6, 7)"
    try:
        got = collect_files_version_inputs(str(root), [rel])
    except UsesError as err:
        if str(err) != want:
            return [f"{name}: error {str(err)!r}, want {want!r}"]
        print(f"ok   {name}")
        return []
    return [f"{name}: accepted {got!r}"]


def run_version_inputs_cases() -> list[str]:
    """Verify --version-inputs covers flow, numeric tags, key case, aliases, merges and bad paths."""
    failures: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        for name, rel, doc, want in version_inputs_positive_table():
            failures.extend(check_version_inputs_case(root, name, rel, doc, want))
        failures.extend(check_version_input_twice(root))
        try:
            collect_files_version_inputs(
                str(root), [".github/workflows/flow.yml"], max_refs=3
            )
            failures.append("version-inputs-max-refs: accepted >3 refs")
        except UsesError:
            print("ok   version-inputs-max-refs")
        failures.extend(
            check_bad_path_cases(root, "version-inputs", collect_files_version_inputs)
        )
    return failures
