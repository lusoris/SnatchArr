# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Offline planted self-test suite for workflow_yaml.py and workflow_actions.py."""

from __future__ import annotations

import contextlib
import io
import os
import sys
import tempfile
from collections.abc import Callable, Iterable
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import workflow_yaml
from workflow_actions import (
    list_action_files,
    resolve_local_action,
    walk_github_actions,
)
from workflow_uses import main as uses_main
from workflow_uses_selftest_steps import run_step_text_cases, run_version_inputs_cases
from workflow_yaml import (
    MAX_FILE_BYTES,
    MAX_FILE_LINES,
    MAX_REFS,
    MAX_STEP_VISITS,
    MAX_STEPS,
    MAX_YAML_DEPTH,
    UsesError,
    collect_text_uses,
    compose_top_mapping,
    filter_local_uses,
    parse_uses_value,
    read_workflow_text,
)


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


def check_max_refs_skips_dropped(three_doc: str) -> list[str]:
    """Assert local and docker references, which extract mode drops, take no MAX_REFS slot."""
    name = "max-refs-skips-dropped"
    doc = f"{three_doc}      - uses: ./local\n      - uses: docker://alpine:3\n"
    try:
        got = collect_text_uses(doc, "dropped.yml", max_refs=3)
    except UsesError as err:
        return [f"{name}: unexpected error: {err}"]
    if [line for _, line in got] != [4, 5, 6]:
        return [f"{name}: got {got!r}, want the 3 named references"]
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
    failures.extend(check_max_refs_skips_dropped(three_doc))
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


def check_windows_local_resolution(base: Path, win_uses: str) -> list[str]:
    """Verify resolve_local_action resolves .\\ in-root and refuses root escapes."""
    failures: list[str] = []
    root = base / "repo"
    act = root / ".github" / "actions" / "x" / "action.yml"
    act.parent.mkdir(parents=True)
    act.write_text("runs:\n  using: composite\n  steps: []\n", encoding="utf-8")
    outside = base / "outside"
    outside.mkdir()
    (outside / "action.yml").write_text(
        "runs:\n  using: composite\n  steps: []\n", encoding="utf-8"
    )
    try:
        got = resolve_local_action(root, win_uses, "ci.yml", 6)
    except UsesError as err:
        failures.append(f"windows-local-resolve-action: raised {err}")
    else:
        if got != ".github/actions/x/action.yml":
            failures.append(f"windows-local-resolve-action: got {got!r}")
        else:
            print("ok   windows-local-resolve-action")
    try:
        resolve_local_action(root, r".\..\outside", "ci.yml", 7)
        failures.append("windows-local-escape: accepted .\\..\\outside")
    except UsesError as err:
        if "escapes repository root" not in str(err):
            failures.append(f"windows-local-escape: unexpected error {str(err)!r}")
        else:
            print("ok   windows-local-escape")
    return failures


def run_windows_local_path_cases() -> list[str]:
    """Verify .\\ local paths parse, filter, resolve in-root and refuse root escapes."""
    failures: list[str] = []
    win_uses = r".\.github\actions\x"
    try:
        parsed = parse_uses_value(win_uses, "win.yml", 1)
    except UsesError as err:
        failures.append(f"windows-local-parse-uses: raised {err}")
    else:
        if parsed is not None:
            failures.append(
                f"windows-local-parse-uses: did not return None for {win_uses!r}"
            )
        else:
            print("ok   windows-local-parse-uses")
    try:
        kept = filter_local_uses([(win_uses, 4), ("actions/checkout@v4", 5)], "win.yml")
    except UsesError as err:
        failures.append(f"windows-local-filter-uses: raised {err}")
    else:
        if kept != [(win_uses, 4)]:
            failures.append(f"windows-local-filter-uses: got {kept!r}")
        else:
            print("ok   windows-local-filter-uses")
    with tempfile.TemporaryDirectory() as tmp:
        failures.extend(check_windows_local_resolution(Path(tmp).resolve(), win_uses))
    return failures


def run_step_visit_bound_cases() -> list[str]:
    """Verify MAX_STEP_VISITS boundary at 8 and fast refusal on 1024x4096x1024 alias bomb."""
    failures: list[str] = []
    four_steps = "x-steps: &s\n" + "".join(f"  - run: echo {i}\n" for i in range(4))
    two_jobs = f"{four_steps}jobs:\n  j0:\n    steps: *s\n  j1:\n    steps: *s\n"
    three_jobs = f"{two_jobs}  j2:\n    steps: *s\n"
    try:
        got = collect_text_uses(two_jobs, "v8.yml", max_step_visits=8)
        if got != []:
            failures.append(f"step-visits-exact-8: got {got!r}, want []")
        else:
            print("ok   step-visits-exact-8")
    except UsesError as err:
        failures.append(f"step-visits-exact-8: unexpected error: {err}")
    try:
        collect_text_uses(three_jobs, "v9.yml", max_step_visits=8)
        failures.append("step-visits-over-8: accepted 12 step visits at limit 8")
    except UsesError as err:
        if "more than 8 step visits" not in str(err):
            failures.append(f"step-visits-over-8: error {str(err)!r} lacks needle")
        else:
            print("ok   step-visits-over-8")
    scale_doc = (
        "x-step: &m\n"
        + "".join(f"  k{i}: 0\n" for i in range(1024))
        + "x-steps: &seq\n"
        + "  - *m\n" * 4096
        + "jobs:\n"
        + "".join(f"  j{i}:\n    steps: *seq\n" for i in range(1024))
    )
    try:
        collect_text_uses(scale_doc, "scale.yml")
        failures.append("step-visits-full-scale: accepted 1024x4096 step visits")
    except UsesError as err:
        if f"more than {MAX_STEP_VISITS} step visits" not in str(err):
            failures.append(f"step-visits-full-scale: error {str(err)!r} lacks needle")
        else:
            print("ok   step-visits-full-scale")
    return failures


def run_docwalk_cache_case() -> list[str]:
    """Verify DocWalk.resolve reads an aliased step mapping node once across jobs."""
    doc = (
        "x-step: &m\n  name: build\n  run: echo ok\n"
        "x-steps: &s\n  - *m\n  - *m\n  - *m\n  - *m\n"
        "jobs:\n  j0:\n    steps: *s\n  j1:\n    steps: *s\n  j2:\n    steps: *s\n"
    )
    orig = workflow_yaml.explicit_entries
    counts: dict[int, int] = {}
    step_ids: set[int] = set()

    def counting_explicit(
        node: workflow_yaml.yaml.MappingNode,
        path: str,
        *,
        any_scalar_key: bool = False,
    ) -> tuple[
        dict[str, tuple[workflow_yaml.yaml.ScalarNode, workflow_yaml.yaml.Node]],
        list[workflow_yaml.yaml.MappingNode],
    ]:
        """Count explicit_entries calls per mapping node id and delegate."""
        node_id = id(node)
        counts[node_id] = counts.get(node_id, 0) + 1
        if any(
            isinstance(k, workflow_yaml.yaml.ScalarNode) and k.value == "run"
            for k, _ in node.value
        ):
            step_ids.add(node_id)
        return orig(node, path, any_scalar_key=any_scalar_key)

    workflow_yaml.explicit_entries = counting_explicit
    try:
        collect_text_uses(doc, "cache.yml")
    except UsesError as err:
        return [f"docwalk-mapping-cache: unexpected error: {err}"]
    finally:
        workflow_yaml.explicit_entries = orig
    step_reads = sum(counts[nid] for nid in step_ids)
    if len(step_ids) != 1 or step_reads != 1:
        return [f"docwalk-mapping-cache: step mapping read {step_reads} times, want 1"]
    print("ok   docwalk-mapping-cache")
    return []


def run_all_uses_cases() -> list[str]:
    """Verify --all-uses emits remote, local, escaped-key and job uses while ignoring run text."""
    failures: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        wf = Path(tmp) / "all.yml"
        wf.write_text(
            "on: push\njobs:\n  call:\n    uses: ./.github/workflows/reusable.yml\n"
            "  build:\n    runs-on: ubuntu-26.04\n    steps:\n"
            "      - uses: actions/checkout@v4\n"
            "      - uses: ./.github/actions/local\n"
            '      - {"\\x75ses": ./.github/actions/planted}\n'
            "      - name: script\n        run: |\n          uses: x/y@v1\n",
            encoding="utf-8",
        )
        out_buf = io.StringIO()
        with contextlib.redirect_stdout(out_buf):
            rc = uses_main(["--all-uses", str(wf)])
        want = [
            f"{wf}:4\t./.github/workflows/reusable.yml",
            f"{wf}:8\tactions/checkout@v4",
            f"{wf}:9\t./.github/actions/local",
            f"{wf}:10\t./.github/actions/planted",
        ]
        got = out_buf.getvalue().splitlines()
        if rc != 0 or got != want:
            failures.append(
                f"all-uses-remote-local-escaped-and-run: rc={rc}, got {got!r}, want {want!r}"
            )
        else:
            print("ok   all-uses-remote-local-escaped-and-run")
    err_buf = io.StringIO()
    with contextlib.redirect_stderr(err_buf):
        missing_rc = uses_main(["planted.yml"], yaml_mod=None)
    err_text = err_buf.getvalue()
    if (
        missing_rc != 2
        or "PyYAML is required" not in err_text
        or "tools/ci/pyyaml_venv.sh" not in err_text
        or "PYTHON_BIN=" not in err_text
    ):
        failures.append(f"pyyaml-missing-hint: rc={missing_rc}, err={err_text!r}")
    else:
        print("ok   pyyaml-missing-hint")
    return failures


def run_file_bounds_cases() -> list[str]:
    """Verify MAX_FILE_BYTES pre-read refusal and MAX_FILE_LINES boundary in compose_top_mapping."""
    failures: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        oversized = Path(tmp) / "oversized.yml"
        oversized.write_bytes(b"\xff")
        os.truncate(oversized, MAX_FILE_BYTES + 1)
        needle = f"oversized.yml: exceeds {MAX_FILE_BYTES} bytes"
        try:
            read_workflow_text(oversized, "oversized.yml")
            failures.append("file-size-over-max-bytes: accepted oversized file")
        except UsesError as err:
            if needle not in str(err):
                failures.append(
                    f"file-size-over-max-bytes: error {str(err)!r} lacks {needle!r}"
                )
            else:
                print("ok   file-size-over-max-bytes")
    exact_doc = "k: 1\n" + "#\n" * (MAX_FILE_LINES - 1)
    try:
        top = compose_top_mapping(exact_doc, "exact.yml")
        if "k" not in top:
            failures.append("compose-exact-max-lines: missing root key 'k'")
        else:
            print("ok   compose-exact-max-lines")
    except UsesError as err:
        failures.append(f"compose-exact-max-lines: unexpected error: {err}")
    over_doc = f"{exact_doc}#\n"
    over_needle = f"over.yml: exceeds {MAX_FILE_LINES} lines"
    try:
        compose_top_mapping(over_doc, "over.yml")
        failures.append("compose-over-max-lines: accepted MAX_FILE_LINES + 1 lines")
    except UsesError as err:
        if over_needle not in str(err):
            failures.append(
                f"compose-over-max-lines: error {str(err)!r} lacks {over_needle!r}"
            )
        else:
            print("ok   compose-over-max-lines")
    return failures


def check_compose_refusal(name: str, doc: str, path: str, needle: str) -> list[str]:
    """Assert compose_top_mapping raises UsesError containing needle (never RecursionError)."""
    try:
        compose_top_mapping(doc, path)
        return [f"{name}: accepted document, expected UsesError with {needle!r}"]
    except RecursionError as err:
        return [f"{name}: raised RecursionError instead of UsesError: {err}"]
    except UsesError as err:
        if needle not in str(err):
            return [f"{name}: error {str(err)!r} lacks {needle!r}"]
        print(f"ok   {name}")
        return []


def run_yaml_depth_cases() -> list[str]:
    """Verify MAX_YAML_DEPTH boundary, refusal message, and 5000-deep flow sequence guard."""
    failures: list[str] = []
    d64 = "k: " + "[" * (MAX_YAML_DEPTH - 1) + "1" + "]" * (MAX_YAML_DEPTH - 1) + "\n"
    try:
        top = compose_top_mapping(d64, "d64.yml")
        if "k" not in top:
            failures.append("yaml-depth-exact-64: missing root key 'k'")
        else:
            print("ok   yaml-depth-exact-64")
    except (UsesError, RecursionError) as err:
        failures.append(f"yaml-depth-exact-64: unexpected error: {err}")
    d65 = "a: 1\nk: " + "[" * MAX_YAML_DEPTH + "1" + "]" * MAX_YAML_DEPTH + "\n"
    failures.extend(
        check_compose_refusal(
            "yaml-depth-over-64",
            d65,
            "d65.yml",
            f"d65.yml:2: YAML nesting depth exceeds {MAX_YAML_DEPTH}",
        )
    )
    d5000 = "k: " + "[" * 5000 + "]" * 5000 + "\n"
    failures.extend(
        check_compose_refusal(
            "yaml-depth-5000-flow-seq",
            d5000,
            "d5000.yml",
            f"d5000.yml:1: YAML nesting depth exceeds {MAX_YAML_DEPTH}",
        )
    )
    return failures


def self_test() -> int:
    """Run all planted positive and negative cases; exit 1 naming any failed case."""
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf):
        failures = [
            *run_positive_cases(),
            *run_negative_cases(),
            *run_max_refs_cases(),
            *run_all_uses_cases(),
            *run_file_bounds_cases(),
            *run_yaml_depth_cases(),
            *run_list_positive_case(),
            *run_list_negative_cases(),
            *run_step_text_cases(),
            *run_version_inputs_cases(),
            *run_windows_local_path_cases(),
            *run_step_visit_bound_cases(),
            *run_docwalk_cache_case(),
        ]
    out = buf.getvalue()
    sys.stdout.write(out)
    for msg in failures:
        print(f"FAIL {msg}", file=sys.stderr)
    if failures:
        return 1
    passed = sum(1 for line in out.splitlines() if line.startswith("ok   "))
    print(f"workflow_uses self-test: {passed} passed")
    return 0
