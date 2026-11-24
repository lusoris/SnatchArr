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
    consume_folded_rest,
    scalar_value_lines,
)
from workflow_uses import build_parser, format_tsv_mode
from workflow_yaml import (
    MAX_FILE_LINES,
    MAX_STEP_TEXT_LINES,
    OutputBudget,
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


def consume_folded_rest_table() -> list[tuple[str, list[str], int, str, int]]:
    """Return positive, negative and boundary rows for consume_folded_rest."""
    return [
        (
            "consume-folded-three-lines",
            ["  head", "  one", "  two", "  three", "  stop"],
            1,
            " one two three",
            4,
        ),
        (
            "consume-folded-stops-second-line",
            ["  head", "  one", "  other", "  three"],
            1,
            " one two three",
            2,
        ),
        (
            "consume-folded-empty-rest",
            ["  head", "  one", "  two"],
            1,
            "",
            1,
        ),
        (
            "consume-folded-max-file-lines",
            ["  x"] * (MAX_FILE_LINES + 2),
            0,
            " x" * (MAX_FILE_LINES + 2),
            MAX_FILE_LINES,
        ),
    ]


def run_consume_folded_rest_cases() -> list[str]:
    """Verify consume_folded_rest across positive, negative and MAX_FILE_LINES boundary rows."""
    failures: list[str] = []
    for name, body, start_idx, rest, want in consume_folded_rest_table():
        got = consume_folded_rest(body, start_idx, rest)
        if got != want:
            failures.append(f"{name}: got {got}, want {want}")
        else:
            print(f"ok   {name}")
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


def check_version_inputs_max_refs(root: Path) -> list[str]:
    """Verify collect_files_version_inputs enforces max_refs=3 on 5- and 3-ref files.

    The limit 3 refuses the five-reference flow.yml at its fourth reference (line 9)
    and accepts a three-reference file.
    """
    failures: list[str] = []
    want_err = ".github/workflows/flow.yml:9: more than 3 uses references"
    try:
        collect_files_version_inputs(
            str(root), [".github/workflows/flow.yml"], max_refs=3
        )
        failures.append("version-inputs-max-refs: accepted >3 refs")
    except UsesError as err:
        if str(err) != want_err:
            failures.append(
                f"version-inputs-max-refs: error {str(err)!r}, want {want_err!r}"
            )
        else:
            print("ok   version-inputs-max-refs")
    three_rel = ".github/workflows/three.yml"
    (root / three_rel).write_text(
        "jobs:\n  j:\n    steps:\n"
        "      - uses: actions/checkout@v4\n        with:\n          version: v1\n"
        "      - uses: actions/checkout@v4\n        with:\n          version: v2\n"
        "      - uses: actions/checkout@v4\n",
        encoding="utf-8",
    )
    want = [
        (three_rel, 4, "actions/checkout", "v1"),
        (three_rel, 7, "actions/checkout", "v2"),
        (three_rel, 10, "actions/checkout", ""),
    ]
    try:
        got = collect_files_version_inputs(str(root), [three_rel], max_refs=3)
    except UsesError as err:
        failures.append(f"version-inputs-max-refs-exact-3: unexpected error: {err}")
    else:
        if got != want:
            failures.append(
                f"version-inputs-max-refs-exact-3: got {got!r}, want {want!r}"
            )
        else:
            print("ok   version-inputs-max-refs-exact-3")
    return failures


def check_output_budget_exact_10() -> list[str]:
    """Verify OutputBudget(10) accepts an ASCII line of 9 chars plus newline."""
    try:
        got = OutputBudget(10).line(("abcdefghi",))
    except UsesError as err:
        return [f"output-budget-exact-10: unexpected error: {err}"]
    if got != "abcdefghi":
        return [f"output-budget-exact-10: got {got!r}"]
    print("ok   output-budget-exact-10")
    return []


def check_output_budget_over_10() -> list[str]:
    """Verify OutputBudget(10) refuses 11-byte ASCII and 2-byte UTF-8 lines."""
    failures: list[str] = []
    needle = "output exceeds 10 bytes"
    for name, fields in (
        ("output-budget-over-10", ("abcdefghij",)),
        ("output-budget-utf8-over-10", ("éabcdefgh",)),
    ):
        try:
            OutputBudget(10).line(fields)
            failures.append(f"{name}: accepted {fields!r} at limit 10")
        except UsesError as err:
            if str(err) != needle:
                failures.append(f"{name}: error {str(err)!r}, want {needle!r}")
            else:
                print(f"ok   {name}")
    return failures


def check_output_budget_aggregate() -> list[str]:
    """Verify OutputBudget(10) sums byte counts across consecutive lines."""
    needle = "output exceeds 10 bytes"
    agg = OutputBudget(10)
    try:
        first = agg.line(("abcd",))
        second = agg.line(("efgh",))
    except UsesError as err:
        return [f"output-budget-aggregate: unexpected early error: {err}"]
    if (first, second) != ("abcd", "efgh"):
        return [f"output-budget-aggregate: got {(first, second)!r}"]
    try:
        agg.line(("ijkl",))
        return ["output-budget-aggregate: accepted third 4-char line"]
    except UsesError as err:
        if str(err) != needle:
            return [f"output-budget-aggregate: error {str(err)!r}"]
    print("ok   output-budget-aggregate")
    return []


def check_output_budget_utf8_exact_10() -> list[str]:
    """Verify OutputBudget(10) accepts a 2-byte UTF-8 character plus 7 ASCII chars."""
    try:
        utf8_ok = OutputBudget(10).line(("éabcdefg",))
    except UsesError as err:
        return [f"output-budget-utf8-exact-10: unexpected error: {err}"]
    if utf8_ok != "éabcdefg":
        return [f"output-budget-utf8-exact-10: got {utf8_ok!r}"]
    print("ok   output-budget-utf8-exact-10")
    return []


def check_output_budget_unit_cases() -> list[str]:
    """Verify OutputBudget exact limit, over limit, aggregate and 2-byte UTF-8 boundary."""
    return [
        *check_output_budget_exact_10(),
        *check_output_budget_over_10(),
        *check_output_budget_aggregate(),
        *check_output_budget_utf8_exact_10(),
    ]


def check_output_budget_version_inputs() -> list[str]:
    """Verify 1 KiB aliased version across 5 uses steps refuses at 3000 and passes default."""
    failures: list[str] = []
    ver = "v" + "1" * 1023
    steps = "".join(
        "      - uses: actions/checkout@v4\n        with:\n          version: *ver\n"
        for _ in range(5)
    )
    doc = f"x-ver: &ver {ver}\njobs:\n  j:\n    steps:\n{steps}"
    rel = ".github/workflows/aliased_ver.yml"
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        full = root / rel
        full.parent.mkdir(parents=True, exist_ok=True)
        full.write_text(doc, encoding="utf-8")
        args = build_parser().parse_args(["--version-inputs", str(root), rel])
        try:
            format_tsv_mode(args, budget=OutputBudget(3000))
            failures.append(
                "version-inputs-output-budget-refusal: accepted >3000 bytes"
            )
        except UsesError as err:
            if str(err) != "output exceeds 3000 bytes":
                failures.append(
                    f"version-inputs-output-budget-refusal: error {str(err)!r}"
                )
            else:
                print("ok   version-inputs-output-budget-refusal")
        try:
            lines = format_tsv_mode(args)
        except UsesError as err:
            failures.append(
                f"version-inputs-output-budget-default: unexpected error: {err}"
            )
        else:
            if len(lines) != 5 or not all(
                line.endswith(f"\tactions/checkout\t{ver}") for line in lines
            ):
                failures.append(
                    f"version-inputs-output-budget-default: got {len(lines)} lines"
                )
            else:
                print("ok   version-inputs-output-budget-default")
    return failures


def check_output_budget_other_modes() -> list[str]:
    """Verify default, --all-uses and --step-text modes format via OutputBudget."""
    failures: list[str] = []
    rel = ".github/workflows/modes.yml"
    doc = (
        "jobs:\n  j:\n    steps:\n"
        "      - uses: actions/checkout@v4\n      - run: echo ok\n"
    )
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        wf = root / rel
        wf.parent.mkdir(parents=True, exist_ok=True)
        wf.write_text(doc, encoding="utf-8")
        for label, argv, want in (
            ("default", [str(wf)], [f"actions/checkout\t{wf}:4"]),
            ("all-uses", ["--all-uses", str(wf)], [f"{wf}:4\tactions/checkout@v4"]),
            ("step-text", ["--step-text", str(root), rel], [f"{rel}:5\techo ok"]),
        ):
            args = build_parser().parse_args(argv)
            try:
                format_tsv_mode(args, budget=OutputBudget(5))
                failures.append(
                    f"output-budget-other-tsv-modes ({label}): accepted past 5"
                )
                continue
            except UsesError as err:
                if str(err) != "output exceeds 5 bytes":
                    failures.append(
                        f"output-budget-other-tsv-modes ({label}): {str(err)!r}"
                    )
                    continue
            got = format_tsv_mode(args)
            if got != want:
                failures.append(
                    f"output-budget-other-tsv-modes ({label}): {got!r} != {want!r}"
                )
    if not failures:
        print("ok   output-budget-other-tsv-modes")
    return failures


def check_output_budget_streams() -> list[str]:
    """Verify all four TSV modes stop at OutputBudget(5) before reading a second broken file."""
    failures: list[str] = []
    first_rel = ".github/workflows/first.yml"
    broken_rel = ".github/workflows/broken.yml"
    first_doc = (
        "jobs:\n  j:\n    steps:\n"
        "      - uses: actions/checkout@v4\n        with:\n          version: v1\n"
        "      - run: echo ok\n"
    )
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        first = root / first_rel
        broken = root / broken_rel
        first.parent.mkdir(parents=True, exist_ok=True)
        first.write_text(first_doc, encoding="utf-8")
        broken.write_text("jobs: [\n", encoding="utf-8")
        for mode, argv in (
            ("default", [str(first), str(broken)]),
            ("all-uses", ["--all-uses", str(first), str(broken)]),
            ("step-text", ["--step-text", str(root), first_rel, broken_rel]),
            ("version-inputs", ["--version-inputs", str(root), first_rel, broken_rel]),
        ):
            args = build_parser().parse_args(argv)
            try:
                format_tsv_mode(args, budget=OutputBudget(5))
                failures.append(
                    f"output-budget-streams ({mode}): accepted past 5 bytes"
                )
            except UsesError as err:
                if str(err) != "output exceeds 5 bytes":
                    failures.append(f"output-budget-streams ({mode}): {str(err)!r}")
                else:
                    print(f"ok   output-budget-streams ({mode})")
    return failures


def run_output_budget_cases() -> list[str]:
    """Run unit, streaming and end-to-end OutputBudget cases across all four TSV modes."""
    return [
        *check_output_budget_unit_cases(),
        *check_output_budget_version_inputs(),
        *check_output_budget_other_modes(),
        *check_output_budget_streams(),
    ]


def run_version_inputs_cases() -> list[str]:
    """Verify --version-inputs covers flow, numeric tags, key case, aliases, merges and bad paths."""
    failures: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        for name, rel, doc, want in version_inputs_positive_table():
            failures.extend(check_version_inputs_case(root, name, rel, doc, want))
        failures.extend(check_version_input_twice(root))
        failures.extend(check_version_inputs_max_refs(root))
        failures.extend(
            check_bad_path_cases(root, "version-inputs", collect_files_version_inputs)
        )
    return failures
