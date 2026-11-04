# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Planted cases for release_publish_order_check.py --self-test.

The check loads this module from its own directory and registers itself as
release_publish_order_check first, so the import below resolves to the running check.
"""

from __future__ import annotations

import contextlib
import copy
import io
import json
import tempfile
from pathlib import Path

import yaml
from release_publish_order_check import (
    MAX_CASES,
    MAX_FILES,
    MAX_LINES,
    MAX_PYTHON_NODES,
    MAX_TOKENS,
    RELEASE_WORKFLOW,
    SelfTestError,
    main,
)

GOOD_CONFIG = {
    "release-type": "simple",
    "draft": True,
    "force-tag-creation": True,
    "packages": {".": {"initial-version": "0.1.0"}},
}

GOOD_RELEASE_YML = """\
name: release
on:
  push:
    tags: ['v*']
  workflow_dispatch:
    inputs:
      tag:
        required: true
jobs:
  meta:
    runs-on: ubuntu-26.04
    steps:
      - run: echo ok
  dogfood-gate:
    needs: meta
    uses: ./.github/workflows/release-dogfood-gate.yml
  image-smoke:
    needs: [meta, dogfood-gate]
    uses: ./.github/workflows/image-smoke.yml
  build:
    needs: [meta, image-smoke]
    uses: ./.github/workflows/release-build.yml
  release-files:
    needs: [meta, build]
    runs-on: ubuntu-26.04
    steps:
      - run: gh release upload "$TAG" release/* --clobber
  verify:
    needs: [meta, build, release-files]
    runs-on: ubuntu-26.04
    steps:
      - run: gh release download "$TAG" --dir page
  publish:
    needs: [meta, dogfood-gate, image-smoke, build, release-files, verify]
    runs-on: ubuntu-26.04
    steps:
      - run: gh release edit "$TAG" --draft=false --verify-tag
"""


def write_fixture(
    root: Path,
    name: str,
    config: dict[str, object] | str | None = None,
    release_yml: str = GOOD_RELEASE_YML,
    extra_workflows: dict[str, str] | None = None,
    repo_files: dict[str, str] | None = None,
) -> tuple[Path, Path]:
    """Create a temporary workflows directory and release-please config for one test case."""
    if config is None:
        config = copy.deepcopy(GOOD_CONFIG)
    case_dir = root / name
    wf_dir = case_dir / ".github" / "workflows"
    wf_dir.mkdir(parents=True, exist_ok=True)
    cfg_path = case_dir / "release-please-config.json"
    cfg_text = config if isinstance(config, str) else json.dumps(config)
    cfg_path.write_text(cfg_text, encoding="utf-8")
    (wf_dir / RELEASE_WORKFLOW).write_text(release_yml, encoding="utf-8")
    for filename, content in list((extra_workflows or {}).items())[:MAX_FILES]:
        (wf_dir / filename).write_text(content, encoding="utf-8")
    for filename, content in list((repo_files or {}).items())[:MAX_FILES]:
        target = case_dir / filename
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")
    return wf_dir, cfg_path


def run_cli_case(
    label: str, wf_dir: Path, cfg_path: Path, want_exit: int, needle: str
) -> None:
    """Invoke main() with wf_dir and cfg_path and assert exit code and output substring."""
    out_buf = io.StringIO()
    err_buf = io.StringIO()
    with contextlib.redirect_stdout(out_buf), contextlib.redirect_stderr(err_buf):
        got_exit = main(["--workflows-dir", str(wf_dir), "--config", str(cfg_path)])
    output = out_buf.getvalue() + err_buf.getvalue()
    if got_exit != want_exit:
        raise SelfTestError(
            f"{label}: expected exit {want_exit}, got {got_exit} ({output.strip()})"
        )
    if needle not in output:
        raise SelfTestError(f"{label}: expected {needle!r} in {output!r}")
    print(f"ok   {label}: exit {got_exit}")


SELF_TEST_CASES: tuple[
    tuple[str, str, dict[str, object] | str, str, dict[str, str] | None, int, str], ...
] = (
    (
        "current-shape ok",
        "ok_current",
        GOOD_CONFIG,
        GOOD_RELEASE_YML,
        None,
        0,
        "release_publish_order_check: ok",
    ),
    (
        "publish job needs only meta",
        "needs_only_meta",
        GOOD_CONFIG,
        GOOD_RELEASE_YML.replace(
            "needs: [meta, dogfood-gate, image-smoke, build, release-files, verify]",
            "needs: [meta]",
        ),
        None,
        3,
        (
            "job publish: runs 'gh release edit --draft=false' without needing gate(s): "
            "dogfood-gate, image-smoke, build, release-files, verify"
        ),
    ),
    (
        "publish job missing verify in needs chain (transitive via release-files counted ok)",
        "missing_verify_transitive",
        GOOD_CONFIG,
        GOOD_RELEASE_YML.replace(
            "needs: [meta, dogfood-gate, image-smoke, build, release-files, verify]",
            "needs: [release-files]",
        ),
        None,
        3,
        "job publish: runs 'gh release edit --draft=false' without needing gate(s): verify",
    ),
    (
        "config without draft",
        "cfg_no_draft",
        {"release-type": "simple", "force-tag-creation": True},
        GOOD_RELEASE_YML,
        None,
        3,
        "lacks draft: true",
    ),
    (
        "config without force-tag-creation",
        "cfg_no_force_tag",
        {"release-type": "simple", "draft": True},
        GOOD_RELEASE_YML,
        None,
        3,
        "lacks force-tag-creation: true",
    ),
    (
        "trigger release published",
        "trigger_published",
        GOOD_CONFIG,
        GOOD_RELEASE_YML.replace(
            "  push:\n    tags: ['v*']",
            "  release:\n    types: [published]",
        ),
        None,
        3,
        "triggers on release: published",
    ),
    (
        "gh release create without --draft in another job",
        "create_in_other_job",
        GOOD_CONFIG,
        GOOD_RELEASE_YML.replace(
            'gh release upload "$TAG" release/* --clobber',
            'gh release create "$TAG" release/*',
        ),
        None,
        3,
        (
            "job release-files: runs 'gh release create' without --draft without needing gate(s): "
            "release-files, verify"
        ),
    ),
    (
        "publish in another workflow",
        "publish_other_workflow",
        GOOD_CONFIG,
        GOOD_RELEASE_YML,
        {
            "other.yml": (
                "name: other\non: push\njobs:\n  pub:\n"
                "    runs-on: ubuntu-26.04\n    steps:\n"
                "      - uses: softprops/action-gh-release@deadbeef\n"
            )
        },
        3,
        (
            "job pub: uses softprops/action-gh-release without draft: true in workflow other than "
            "release.yml"
        ),
    ),
    (
        "no publish job",
        "no_publish_job",
        GOOD_CONFIG,
        GOOD_RELEASE_YML.split("  publish:\n")[0],
        None,
        3,
        "no job publishes the draft release",
    ),
    (
        "publish job runs after a failed gate",
        "publish_always",
        GOOD_CONFIG,
        GOOD_RELEASE_YML.replace(
            "  publish:\n", "  publish:\n    if: ${{ always() }}\n"
        ),
        None,
        3,
        "runs it after a failed gate",
    ),
    (
        "gh api publish in another workflow",
        "gh_api_other",
        GOOD_CONFIG,
        GOOD_RELEASE_YML,
        {
            "other.yml": (
                "on: push\njobs:\n  pub:\n    runs-on: ubuntu-26.04\n    steps:\n"
                "      - run: gh api -X PATCH repos/o/r/releases/1 -F draft=false\n"
            )
        },
        3,
        "runs 'gh api' on releases with a mutating request",
    ),
    (
        "duplicate YAML key",
        "dup_yaml_key",
        GOOD_CONFIG,
        GOOD_RELEASE_YML + "  meta:\n    runs-on: ubuntu-26.04\n",
        None,
        1,
        "duplicate key 'meta'",
    ),
)


def command_case(
    name: str,
    command: str,
    want_exit: int,
    needle: str = "release_publish_order_check: ok",
) -> tuple:
    """Exercise a command in a workflow that cannot publish."""
    workflow = {
        "on": "push",
        "jobs": {"test": {"runs-on": "ubuntu-26.04", "steps": [{"run": command}]}},
    }
    return (
        name,
        name,
        GOOD_CONFIG,
        GOOD_RELEASE_YML,
        {
            "other.yaml"
            if name == "yaml publishes early"
            else "other.yml": yaml.safe_dump(workflow)
        },
        want_exit,
        needle,
    )


COMMAND_CASES = (
    (
        "yaml publishes early",
        "gh release edit v1 --draft=false",
        3,
        "in workflow other than",
    ),
    ("script too long", "# comment\n" * (MAX_LINES + 1), 1, "exceeds MAX_LINES"),
    ("script at line limit", "# comment\n" * (MAX_LINES - 1) + "echo ok", 0, "ok"),
    (
        "quoted draft notes",
        'gh release create v1 --notes "use --draft later"',
        3,
        "without --draft",
    ),
    ("draft long flag", "gh release create v1 --draft", 0, "ok"),
    ("draft true flag", "gh release create v1 --draft=true", 0, "ok"),
    ("draft one flag", "gh release create v1 --draft=1", 0, "ok"),
    ("draft zero flag", "gh release create v1 --draft --draft=0", 3, "without --draft"),
    ("draft False flag", "gh release create v1 --draft=False", 3, "without --draft"),
    ("edit draft zero", "gh release edit v1 --draft=0", 3, "edit --draft=false"),
    ("edit draft F", "gh release edit v1 --draft=F", 3, "edit --draft=false"),
    ("edit draft one", "gh release edit v1 --draft=1", 0, "ok"),
    ("draft unparsable", "gh release create v1 --draft=yes", 1, "not a boolean"),
    (
        "draft after separator",
        "gh release create v1 -- --draft=1",
        3,
        "without --draft",
    ),
    (
        "draft as target value",
        "gh release create v1 --target --draft",
        3,
        "without --draft",
    ),
    ("draft as repo value", "gh release create v1 -R --draft", 3, "without --draft"),
    ("draft after repo value", "gh release create v1 --repo o/r --draft", 0, "ok"),
    ("draft before separator", "gh release create v1 --draft -- a.tgz", 0, "ok"),
    ("draft short flag", "gh release create v1 -d", 0, "ok"),
    (
        "draft false wins",
        "gh release create v1 --draft --draft=false",
        3,
        "without --draft",
    ),
    ("invalid quoting", 'gh release create v1 "', 1, "shell parse error"),
    ("unknown subcommand", "gh release surprise v1", 3, "not on the allow-list"),
    ("release view", "gh release view v1", 0, "ok"),
    ("release list", "gh release list", 0, "ok"),
    ("release download", "gh release download v1", 0, "ok"),
    ("release upload", "gh release upload v1 file", 0, "ok"),
    ("release edit", "gh release edit v1 --title title", 0, "ok"),
    ("release delete asset", "gh release delete-asset v1 asset", 0, "ok"),
    ("release verify asset", "gh release verify-asset v1 asset", 0, "ok"),
    (
        "api patch",
        "gh api -X PATCH repos/o/r/releases/1 -f draft=false",
        3,
        "runs 'gh api'",
    ),
    ("api post", "gh api --method=POST repos/o/r/releases", 3, "runs 'gh api'"),
    ("api field", "gh api repos/o/r/releases/1 --field name=x", 3, "runs 'gh api'"),
    (
        "api raw field",
        "gh api repos/o/r/releases/1 --raw-field=name=x",
        3,
        "runs 'gh api'",
    ),
    (
        "api input",
        "gh api repos/o/r/releases/1 --input payload.json",
        3,
        "runs 'gh api'",
    ),
    ("api get", "gh api repos/o/r/releases/1", 0, "ok"),
    ("api explicit get", "gh api --method GET repos/o/r/releases/1", 0, "ok"),
    ("api unrelated", "gh api -X POST repos/o/r/issues -f title=x", 0, "ok"),
    (
        "curl patch",
        "curl -X PATCH https://api.github.com/repos/o/r/releases/1",
        3,
        "runs 'curl'",
    ),
    (
        "curl request",
        "curl --request=PUT https://api.github.com/repos/o/r/releases/1",
        3,
        "runs 'curl'",
    ),
    (
        "curl data",
        "curl --data-raw '{}' https://api.github.com/repos/o/r/releases",
        3,
        "runs 'curl'",
    ),
    (
        "curl upload",
        "curl -T payload.json https://api.github.com/repos/o/r/releases/1",
        3,
        "runs 'curl'",
    ),
    ("curl get", "curl https://api.github.com/repos/o/r/releases/1", 0, "ok"),
    (
        "wget request",
        "wget --method=PATCH https://api.github.com/repos/o/r/releases/1",
        3,
        "runs 'wget'",
    ),
    (
        "wget post",
        "wget --post-data='{}' https://api.github.com/repos/o/r/releases",
        3,
        "runs 'wget'",
    ),
    ("wget get", "wget https://api.github.com/repos/o/r/releases/1", 0, "ok"),
    (
        "indirect publisher",
        "bash tools/x.sh",
        3,
        "runs tools/x.sh which runs 'gh release create'",
    ),
    ("indirect reader", "bash tools/read.sh", 0, "ok"),
    ("indirect too long", "sh tools/long.sh", 1, "exceeds MAX_LINES"),
    ("indirect at limit", "sh tools/limit.sh", 0, "ok"),
    (
        "indirect python",
        "python3 tools/x.py",
        3,
        "runs tools/x.py which runs 'gh release create'",
    ),
    ("indirect python reader", "python3 tools/read.py", 0, "ok"),
    (
        "notes equal draft flag",
        'gh release create v1 --notes "--draft"',
        3,
        "without --draft",
    ),
    ("edit notes false flag", 'gh release edit v1 --notes "--draft=false"', 0, "ok"),
    ("token overflow", "echo " + "word " * MAX_TOKENS, 1, "exceeds MAX_TOKENS"),
    ("token boundary", "echo " + "word " * (MAX_TOKENS - 1), 0, "ok"),
    ("python node overflow", "python3 tools/nodes.py", 1, "exceeds MAX_PYTHON_NODES"),
    ("python invalid", "python3 tools/invalid.py", 1, "Python parse error"),
    (
        "python argv publisher",
        "python3 tools/argv.py",
        3,
        "runs tools/argv.py which runs 'gh release create'",
    ),
    ("python argv reader", "python3 tools/argv-read.py", 0, "ok"),
    ("quoted multiline", 'echo "first\nsecond"\ngh release view v1', 0, "ok"),
    (
        "chained publisher",
        "gh release edit v1 --draft=false; echo ok",
        3,
        "runs 'gh release edit --draft=false'",
    ),
    ("chained reader", "gh release view v1; echo ok", 0, "ok"),
    ("comment draft flag", "gh release create v1 # --draft", 3, "without --draft"),
    ("comment reader", "gh release view v1 # a comment", 0, "ok"),
    (
        "python imported call",
        "python3 tools/imported.py",
        3,
        "runs tools/imported.py which runs 'gh release create'",
    ),
    ("one level only", "sh tools/outer.sh", 0, "ok"),
)

SELF_TEST_FILES = {
    "python imported call": {
        "tools/imported.py": 'from os import system\nsystem("gh release create v1")\n'
    },
    "one level only": {
        "tools/outer.sh": "sh tools/nested.sh\n",
        "tools/nested.sh": "gh release create v1\n",
    },
    "gated indirect unknown": {
        "tools/x.sh": "gh release create v1\ngh release surprise v1\n"
    },
    "python node overflow": {"tools/nodes.py": "x=0;" * (MAX_PYTHON_NODES // 4 + 1)},
    "python invalid": {"tools/invalid.py": "def invalid(:\n"},
    "python argv publisher": {
        "tools/argv.py": 'import subprocess\nsubprocess.run(["gh", "release", "create", "v1"])\n'
    },
    "python argv reader": {
        "tools/argv-read.py": 'import subprocess\nsubprocess.run(["gh", "release", "view", "v1"])\n'
    },
    "indirect publisher": {"tools/x.sh": "gh release create v1\n"},
    "indirect reader": {"tools/read.sh": "gh release view v1\n"},
    "indirect too long": {"tools/long.sh": "# comment\n" * (MAX_LINES + 1)},
    "indirect at limit": {
        "tools/limit.sh": "# comment\n" * (MAX_LINES - 1) + "echo ok\n"
    },
    "indirect python": {"tools/x.py": 'import os\nos.system("gh release create v1")\n'},
    "indirect python reader": {
        "tools/read.py": 'import os\nos.system("gh release view v1")\n'
    },
}


def review_cases() -> tuple:
    """Plant action and malformed-trigger cases alongside command cases."""
    cases = [command_case(*case) for case in COMMAND_CASES[:MAX_CASES]]
    for action, draft, want in (
        ("unknown/release-action", None, 3),
        ("unknown/release-action", True, 3),
        ("UNKNOWN/Release-Action", None, 3),
        ("googleapis/release-please-action", None, 0),
        ("softprops/action-gh-release", True, 0),
        ("SOFTPROPS/Action-Gh-Release", True, 0),
        ("actions/checkout", None, 0),
    )[:MAX_CASES]:
        name = f"action {action} draft {draft}"
        case = list(
            command_case(
                name, "echo ok", want, "not on the allow-list" if want else "ok"
            )
        )
        step = {"uses": action + "@deadbeef", "with": {"draft": draft}}
        case[4] = {
            "other.yml": yaml.safe_dump(
                {"on": "push", "jobs": {"test": {"steps": [step]}}}
            )
        }
        cases.append(tuple(case))
    for types, want in (({}, 1), (42, 1), (None, 1), ("edited", 0), (["edited"], 0))[
        :MAX_CASES
    ]:
        name = f"trigger types {types}"
        release = GOOD_RELEASE_YML.replace(
            "  push:\n    tags: ['v*']", "  release:\n    types: " + json.dumps(types)
        )
        cases.append(
            (
                name,
                name,
                GOOD_CONFIG,
                release,
                None,
                want,
                "types must be a string or list" if want else "ok",
            )
        )
    return tuple(cases)


def gated_allowlist_cases() -> tuple:
    """An unknown publisher remains refused even after all gates."""
    cases = []
    for name, replacement, want in (
        ("gated unknown command", "run: gh release surprise v1", 3),
        ("gated unknown action", "uses: unknown/release-action@deadbeef", 3),
        ("gated allowed command", "run: gh release edit v1 --draft=false", 0),
        (
            "gated later unknown",
            "run: |\n          gh release edit v1 --draft=false\n          gh release surprise v1",
            3,
        ),
        (
            "gated chained unknown",
            "run: gh release edit v1 --draft=false; gh release surprise v1",
            3,
        ),
        ("gated indirect unknown", "run: bash tools/x.sh", 3),
    )[:MAX_CASES]:
        release = GOOD_RELEASE_YML.replace(
            'run: gh release edit "$TAG" --draft=false --verify-tag', replacement
        )
        cases.append(
            (
                name,
                name,
                GOOD_CONFIG,
                release,
                None,
                want,
                "not on the allow-list" if want else "ok",
            )
        )
    return tuple(cases)


def self_test() -> None:
    """Run planted positive, refusal, and error cases."""
    with tempfile.TemporaryDirectory() as scratch:
        root = Path(scratch)
        cases = SELF_TEST_CASES + review_cases() + gated_allowlist_cases()
        if len(cases) > MAX_CASES:
            raise SelfTestError("self-test cases exceed MAX_CASES")
        failures: list[str] = []
        for label, name, cfg, rel_yml, extra, want_exit, needle in cases[:MAX_CASES]:
            wf_dir, cfg_path = write_fixture(
                root, name, cfg, rel_yml, extra, SELF_TEST_FILES.get(name)
            )
            try:
                run_cli_case(label, wf_dir, cfg_path, want_exit, needle)
            except (SelfTestError, TypeError) as error:
                failures.append(str(error))
        run_cli_case(
            "unreadable file",
            root / "ok_current" / ".github" / "workflows",
            root / "nonexistent-config.json",
            1,
            "nonexistent-config.json",
        )
    if failures:
        raise SelfTestError("\n".join(failures))
    print(
        f"release_publish_order_check self-test: {len(cases) + 1} cases behaved "
        "as required"
    )
