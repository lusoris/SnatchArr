# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Offline planted self-test suite for workflow_yaml.py and workflow_actions.py."""

from __future__ import annotations

import sys
import tempfile
from collections.abc import Callable, Iterable
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from workflow_actions import (
    collect_files_step_text,
    collect_files_version_inputs,
    list_action_files,
    walk_github_actions,
)
from workflow_yaml import MAX_REFS, MAX_STEPS, UsesError, collect_text_uses


def positive_cases_table() -> list[tuple[str, str, list[tuple[str, int]]]]:
    """Return planted positive YAML documents and expected (owner/repo, line) lists."""
    workflow_doc = (
        "on: push\njobs:\n  call:\n    uses: org.1/reusable/.github/workflows/w.yml@v1\n"
        "  build:\n    runs-on: ubuntu-26.04\n    env:\n      uses: ignored/env@v1\n"
        "    steps:\n      - uses: ./local/action\n      - uses: $/.github/actions/x\n"
        "      - uses: docker://alpine:3.20\n"
        "      - name: script\n        run: |\n          uses: moved/in-run@v1\n"
        "      - uses: act_1/step-repo/sub@deadbeef # v1\n"
    )
    anchor_doc = (
        "x-ref: &ref a-owner/a-repo@v1\nx-step: &step\n  uses: b.owner/b_repo@v2\n"
        "jobs:\n  j:\n    steps:\n      - uses: *ref\n      - *step\n"
        "      - <<: *step\n        name: merged\n      - {name: f, uses: c/d@v3}\n"
    )
    return [
        (
            "workflow-jobs-steps-and-skips",
            workflow_doc,
            [("org.1/reusable", 4), ("act_1/step-repo", 16)],
        ),
        (
            "anchors-aliases-merges-and-flow",
            anchor_doc,
            [
                ("a-owner/a-repo", 1),
                ("b.owner/b_repo", 3),
                ("b.owner/b_repo", 3),
                ("c/d", 11),
            ],
        ),
        (
            "composite-action-runs",
            "name: c\nruns:\n  using: composite\n  steps:\n    - uses: comp/act@v1\n",
            [("comp/act", 5)],
        ),
        (
            "bool-job-id",
            "jobs:\n  yes:\n    steps:\n      - uses: a/b@c\n",
            [("a/b", 4)],
        ),
        (
            "merged-bool-job-id",
            "x-jobs: &j\n  on:\n    steps:\n      - uses: d/e@f\njobs:\n  <<: *j\n",
            [("d/e", 4)],
        ),
        (
            "self-repo-uses-no-lookup",
            "jobs:\n  j:\n    steps:\n      - uses: $/.github/actions/x\n",
            [],
        ),
    ]


def run_positive_cases() -> list[str]:
    """Run planted positive self-test cases and return failure descriptions."""
    failures: list[str] = []
    for name, text, want in positive_cases_table():
        try:
            got = collect_text_uses(text, "case.yml")
        except UsesError as err:
            failures.append(f"{name}: unexpected error: {err}")
            continue
        if got != want:
            failures.append(f"{name}: got {got!r}, want {want!r}")
        else:
            print(f"ok   {name}")
    return failures


def negative_cases_table() -> list[tuple[str, str, str]]:
    """Return planted negative YAML documents and expected error substrings."""
    return [
        ("invalid-yaml", "jobs: [\n", "bad.yml"),
        ("non-mapping-root", "- item\n", "list.yml"),
        (
            "duplicate-key",
            "jobs:\n  j:\n    steps:\n      - uses: a/b@v1\n        uses: c/d@v1\n",
            "dup.yml:5",
        ),
        (
            "duplicate-bool-str-job-id",
            'jobs:\n  yes:\n    steps: []\n  "yes":\n    steps: []\n',
            "dupjob.yml:4: duplicate key 'yes'",
        ),
        ("missing-ref", "jobs:\n  j:\n    uses: owner/repo\n", "noref.yml:3"),
        ("empty-ref", "jobs:\n  j:\n    uses: owner/repo@\n", "emptyref.yml:3"),
        (
            "bad-owner-charset",
            "jobs:\n  j:\n    uses: bad$owner/repo@v1\n",
            "owner.yml:3",
        ),
        (
            "bad-repo-charset",
            'jobs:\n  j:\n    uses: "owner/bad repo@v1"\n',
            "repo.yml:3",
        ),
        ("empty-subpath", "jobs:\n  j:\n    uses: owner/repo/@v1\n", "sub.yml:3"),
        ("dotdot-owner", "jobs:\n  j:\n    uses: ../repo@v1\n", "dot.yml:3"),
        ("dotdot-repo", "jobs:\n  j:\n    uses: owner/..@v1\n", "dotrepo.yml:3"),
        ("non-scalar-uses", "jobs:\n  j:\n    uses: [a/b@v1]\n", "listuses.yml:3"),
        ("null-uses", "jobs:\n  j:\n    steps:\n      - uses:\n", "null.yml:4"),
        (
            "self-repo-with-ref",
            "jobs:\n  j:\n    steps:\n      - uses: $/.github/actions/x@v1\n",
            "selfref.yml:4: self-repository reference '$/.github/actions/x@v1' must not include @ref",
        ),
        (
            "bare-self-repo",
            "jobs:\n  j:\n    steps:\n      - uses: $/\n",
            "bareself.yml:4: invalid uses reference '$/'",
        ),
    ]


def run_negative_cases() -> list[str]:
    """Run planted negative self-test cases and return failure descriptions."""
    failures: list[str] = []
    for name, text, needle in negative_cases_table():
        path = needle.split(":", 1)[0]
        try:
            got = collect_text_uses(text, path)
            failures.append(f"{name}: accepted {got!r}, expected error with {needle!r}")
        except UsesError as err:
            if needle not in str(err):
                failures.append(f"{name}: error {str(err)!r} lacks {needle!r}")
            else:
                print(f"ok   {name}")
    return failures


def check_max_refs_refusal(
    name: str, text: str, path: str, max_refs: int, needle: str
) -> list[str]:
    """Assert collect_text_uses at max_refs raises UsesError containing needle."""
    try:
        got = collect_text_uses(text, path, max_refs=max_refs)
        return [f"{name}: accepted {len(got)} refs, expected error with {needle!r}"]
    except UsesError as err:
        if needle not in str(err):
            return [f"{name}: error {str(err)!r} lacks {needle!r}"]
        print(f"ok   {name}")
        return []


def run_max_refs_cases() -> list[str]:
    """Verify MAX_REFS bounds extract and --list-actions modes cleanly."""
    failures: list[str] = []
    three_doc = (
        "jobs:\n  j:\n    steps:\n      - uses: a/b@v1\n"
        "      - uses: c/d@v1\n      - uses: e/f@v1\n"
    )
    four_doc = f"{three_doc}      - uses: g/h@v1\n"
    try:
        got = collect_text_uses(three_doc, "three.yml", max_refs=3)
        if len(got) != 3:
            failures.append(f"max-refs-exact-3: got {got!r}, want 3 entries")
        else:
            print("ok   max-refs-exact-3")
    except UsesError as err:
        failures.append(f"max-refs-exact-3: unexpected error: {err}")
    failures.extend(
        check_max_refs_refusal(
            "max-refs-over-3",
            four_doc,
            "four.yml",
            3,
            "four.yml:7: more than 3 uses references",
        )
    )
    big_doc = (
        "x-steps: &shared\n"
        + "".join("  - uses: a/b@c\n" for _ in range(MAX_STEPS))
        + "jobs:\n"
        + "".join(f"  j{idx}:\n    steps: *shared\n" for idx in range(5))
    )
    failures.extend(
        check_max_refs_refusal(
            "max-refs-anchored-jobs",
            big_doc,
            "big.yml",
            MAX_REFS,
            "big.yml:2: more than 16384 uses references",
        )
    )
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        wf_dir = root / ".github" / "workflows"
        wf_dir.mkdir(parents=True)
        (wf_dir / "four.yml").write_text(four_doc, encoding="utf-8")
        failures.extend(
            check_list_refusal(
                "list-actions-max-refs",
                root,
                ".github/workflows/four.yml",
                ".github/workflows/four.yml:7: more than 3 uses references",
                max_refs=3,
            )
        )
    return failures


def write_list_positive_fixture(root: Path) -> list[str]:
    """Populate root with workflows, composite actions and a Docker-only action."""
    wf_dir = root / ".github" / "workflows"
    wf_dir.mkdir(parents=True)
    (wf_dir / "ci.yml").write_text(
        "on: push\njobs:\n  call:\n"
        "    uses: ./.github/workflows/reusable.yml\n"
        "  build:\n    runs-on: ubuntu-26.04\n    steps:\n"
        "      - uses: ./tools/actions/x\n"
        "      - uses: $/.github/actions/x\n"
        "      - uses: $/tools/actions/z\n"
        "      - uses: ./tools/actions/docker-only\n",
        encoding="utf-8",
    )
    (wf_dir / "reusable.yml").write_text(
        "on: workflow_call\njobs:\n  r:\n    runs-on: ubuntu-26.04\n"
        "    steps:\n      - uses: actions/checkout@v4\n",
        encoding="utf-8",
    )
    for rel, body in (
        (
            "tools/actions/x/action.yml",
            (
                "runs:\n  using: composite\n  steps:\n    - uses: ./tools/actions/y\n"
                "    - uses: ./tools/actions/x\n    - uses: moved/outside@v1\n"
            ),
        ),
        (
            "tools/actions/y/action.yaml",
            "runs:\n  using: composite\n  steps:\n    - uses: moved/chained@v1\n",
        ),
        (
            "tools/actions/z/action.yml",
            "runs:\n  using: composite\n  steps:\n    - uses: moved/self-out@v1\n",
        ),
        (
            ".github/actions/x/action.yml",
            "runs:\n  using: composite\n  steps:\n    - uses: moved/self@v1\n",
        ),
        (
            ".github/actions/d1/d2/d3/d4/d5/d6/d7/d8/d9/action.yml",
            "runs:\n  using: composite\n  steps:\n    - uses: moved/deep@v1\n",
        ),
        ("tools/actions/docker-only/Dockerfile", "FROM scratch\n"),
    ):
        full = root / rel
        full.parent.mkdir(parents=True, exist_ok=True)
        full.write_text(body, encoding="utf-8")
    return [
        ".github/actions/d1/d2/d3/d4/d5/d6/d7/d8/d9/action.yml",
        ".github/actions/x/action.yml",
        "tools/actions/x/action.yml",
        "tools/actions/y/action.yaml",
        "tools/actions/z/action.yml",
    ]


def run_list_positive_case() -> list[str]:
    """Verify --list-actions finds ./, $/ and deep actions while skipping Docker-only."""
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        want = write_list_positive_fixture(root)
        try:
            got = list_action_files(
                str(root),
                [".github/workflows/ci.yml", ".github/workflows/reusable.yml"],
            )
        except UsesError as err:
            return [f"list-actions-discovery: unexpected error: {err}"]
        if got != want:
            return [f"list-actions-discovery: got {got!r}, want {want!r}"]
        print("ok   list-actions-discovery")
    return []


def check_list_refusal(
    name: str, root: Path, wf_rel: str, needle: str, max_refs: int = MAX_REFS
) -> list[str]:
    """Assert list_action_files raises UsesError containing needle."""
    try:
        got = list_action_files(str(root), [wf_rel], max_refs=max_refs)
        return [f"{name}: accepted {got!r}, expected error with {needle!r}"]
    except UsesError as err:
        if needle not in str(err):
            return [f"{name}: error {str(err)!r} lacks {needle!r}"]
        print(f"ok   {name}")
        return []


def check_walk_error_case(base: Path) -> list[str]:
    """Verify walk_github_actions propagates an injected onerror PermissionError."""
    walk_root = (base / "walk-error-case").resolve()
    locked = walk_root / ".github" / "actions" / "locked"
    locked.mkdir(parents=True)

    def fake_walk(
        _top: Path, *, onerror: Callable[[OSError], None]
    ) -> Iterable[tuple[str, list[str], list[str]]]:
        """Invoke onerror with a simulated PermissionError and yield nothing."""
        onerror(PermissionError(13, "Permission denied", str(locked)))
        return ()

    try:
        got = walk_github_actions(walk_root, walk=fake_walk)
        return [f"list-actions-unreadable-dir: accepted {got!r}, expected UsesError"]
    except UsesError as err:
        if not str(err).startswith(".github/actions:"):
            msg = (
                f"list-actions-unreadable-dir: error {str(err)!r}"
                " does not start with '.github/actions:'"
            )
            return [msg]
        print("ok   list-actions-unreadable-dir")
        return []


def run_list_negative_cases() -> list[str]:
    """Verify --list-actions refuses missing actions, escapes, @ref and walk errors."""
    failures: list[str] = []
    wf_rel = ".github/workflows/planted.yml"
    wf_head = "on: push\njobs:\n  j:\n    runs-on: ubuntu-26.04\n    steps:\n"
    with tempfile.TemporaryDirectory() as tmp:
        base = Path(tmp)
        outside = base / "outside"
        outside.mkdir()
        (outside / "action.yml").write_text(
            "runs:\n  using: composite\n  steps: []\n", encoding="utf-8"
        )
        for case_name, uses_val, needle in (
            (
                "list-actions-missing",
                "./missing",
                f"{wf_rel}:6: local action './missing' has no action.yml, action.yaml or Dockerfile",
            ),
            (
                "list-actions-escape",
                "./../outside",
                f"{wf_rel}:6: local action './../outside' escapes repository root",
            ),
            (
                "list-actions-self-repo-escape",
                "$/../outside",
                f"{wf_rel}:6: local action '$/../outside' escapes repository root",
            ),
            (
                "list-actions-self-repo-with-ref",
                "$/.github/actions/x@v1",
                f"{wf_rel}:6: self-repository reference '$/.github/actions/x@v1' must not include @ref",
            ),
        ):
            case_root = base / case_name
            (case_root / ".github" / "workflows").mkdir(parents=True)
            (case_root / wf_rel).write_text(
                f"{wf_head}      - uses: {uses_val}\n", encoding="utf-8"
            )
            failures.extend(check_list_refusal(case_name, case_root, wf_rel, needle))
        failures.extend(check_walk_error_case(base))
    return failures


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


def step_text_positive_table() -> list[
    tuple[str, str, str, list[tuple[str, int, str]], bool]
]:
    """Return planted --step-text documents and expected (file, line, text) records."""
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
    quoted_rel = ".github/workflows/quoted.yml"
    quoted_doc = (
        "jobs:\n  j:\n    steps:\n"
        "      - run: 'go install golang.org/x/vuln/cmd/govulncheck@latest'\n"
        '      - run: >-\n          folded one\n      - run: "nul\\0cr\\rend"\n'
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
    ]


def run_step_text_cases() -> list[str]:
    """Verify --step-text emits scalar lines with exact line numbers and refuses bad paths."""
    failures: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        for name, rel, doc, want, repeat in step_text_positive_table():
            failures.extend(
                check_step_text_case(root, name, rel, doc, want, repeat=repeat)
            )
        for label, bad_arg in (
            ("tab", "a\tb.yml"),
            ("lf", "a\nb.yml"),
            ("nul", "a\x00b.yml"),
        ):
            try:
                collect_files_step_text(str(root), [bad_arg])
                failures.append(f"step-text-bad-{label}: accepted {bad_arg!r}")
            except UsesError:
                print(f"ok   step-text-bad-{label}")
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


def run_version_inputs_cases() -> list[str]:
    """Verify --version-inputs covers flow, numeric tags, aliases, merges and bad paths."""
    failures: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        for name, rel, doc, want in version_inputs_positive_table():
            failures.extend(check_version_inputs_case(root, name, rel, doc, want))
        try:
            collect_files_version_inputs(
                str(root), [".github/workflows/flow.yml"], max_refs=3
            )
            failures.append("version-inputs-max-refs: accepted >3 refs")
        except UsesError:
            print("ok   version-inputs-max-refs")
        for label, bad_arg in (("tab", "a\tb.yml"), ("nul", "a\x00b.yml")):
            try:
                collect_files_version_inputs(str(root), [bad_arg])
                failures.append(f"version-inputs-bad-{label}: accepted {bad_arg!r}")
            except UsesError:
                print(f"ok   version-inputs-bad-{label}")
    return failures


def self_test() -> int:
    """Run all planted positive and negative cases; exit 1 naming any failed case."""
    failures = [
        *run_positive_cases(),
        *run_negative_cases(),
        *run_max_refs_cases(),
        *run_list_positive_case(),
        *run_list_negative_cases(),
        *run_step_text_cases(),
        *run_version_inputs_cases(),
    ]
    for msg in failures:
        print(f"FAIL {msg}", file=sys.stderr)
    return 1 if failures else 0
