# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse release workflows or configs that publish the release page before all gates pass.

Checks .github/workflows/*.yml and release-please-config.json:
1. release-please-config.json must set draft: true and force-tag-creation: true.
2. release.yml must not trigger on release events of type published or created.
3. Only release.yml may publish a GitHub release, at least one job in release.yml must
   publish, and every publishing job must transitively need dogfood-gate, image-smoke,
   build, release-files, and verify.
Exit 0 = ok, 1 = input or parse error, 3 = refused.
"""

from __future__ import annotations

import argparse
import contextlib
import io
import json
import re
import sys
import tempfile
from pathlib import Path

import yaml

DEFAULT_WORKFLOWS_DIR = Path(".github/workflows")
DEFAULT_CONFIG = Path("release-please-config.json")
RELEASE_WORKFLOW = "release.yml"
REQUIRED_GATES = ("dogfood-gate", "image-smoke", "build", "release-files", "verify")
FORBIDDEN_RELEASE_TYPES = frozenset(("published", "created"))
MAX_FILES = 64
MAX_JOBS = 64
MAX_STEPS = 64
MAX_PACKAGES = 64
MAX_LINES = 500
MAX_CASES = 32
RELEASE_ACTIONS = (
    "softprops/action-gh-release",
    "actions/create-release",
    "ncipollo/release-action",
    "marvinpinto/action-automatic-releases",
    "elgohr/Github-Release-Action",
)
GH_CREATE_RE = re.compile(r"\bgh\s+release\s+create\b")
GH_DRAFT_FLAG_RE = re.compile(r"(?:^|\s)(?:--draft(?:=true)?|-d)(?:\s|$)")
GH_DRAFT_FALSE_RE = re.compile(r"(?:^|\s)--draft=false(?:\s|$)")
GH_EDIT_RE = re.compile(r"\bgh\s+release\s+edit\b")
GH_API_RELEASES_RE = re.compile(r"\bgh\s+api\b.*\breleases\b")
GH_API_DRAFT_FALSE_RE = re.compile(r"\bdraft\s*[=:]\s*false\b")
GATE_BYPASS_RE = re.compile(r"\b(?:always|failure|cancelled)\s*\(")


class CheckError(Exception):
    """Input or parse error that prevents the check from running."""


class SelfTestError(Exception):
    """A planted self-test case did not behave as required."""


class UniqueKeyLoader(yaml.SafeLoader):
    """SafeLoader that refuses a duplicate mapping key instead of keeping the last one."""

    def construct_mapping(self, node: yaml.MappingNode, deep: bool = False) -> dict:
        """Build the mapping, raising on a key that appears twice."""
        seen: set = set()
        for key_node, _ in node.value:
            key = self.construct_object(key_node, deep=deep)
            if key in seen:
                raise yaml.constructor.ConstructorError(
                    None, None, f"duplicate key {key!r}", key_node.start_mark
                )
            seen.add(key)
        return super().construct_mapping(node, deep=deep)


def load_unique(text: str) -> object:
    """Parse one YAML document with UniqueKeyLoader."""
    loader = UniqueKeyLoader(text)
    try:
        return loader.get_single_data()
    finally:
        loader.dispose()


def unique_json_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    """Build a dict from JSON pairs, refusing a duplicate key."""
    seen: dict[str, object] = {}
    for key, value in pairs:
        if key in seen:
            raise ValueError(f"duplicate key {key!r}")
        seen[key] = value
    return seen


def check_release_please_config(config_path: Path) -> list[str]:
    """Require draft: true and force-tag-creation: true in release-please-config.json."""
    try:
        raw = config_path.read_text(encoding="utf-8")
        config = json.loads(raw, object_pairs_hook=unique_json_keys)
    except (OSError, UnicodeDecodeError, ValueError) as error:
        raise CheckError(f"{config_path}: {error}") from error
    if not isinstance(config, dict):
        raise CheckError(f"{config_path}: root must be a JSON object")
    refusals: list[str] = []
    if config.get("draft") is not True:
        refusals.append(f"{config_path}: lacks draft: true")
    if config.get("force-tag-creation") is not True:
        refusals.append(f"{config_path}: lacks force-tag-creation: true")
    packages = config.get("packages")
    if isinstance(packages, dict):
        for pkg_name, pkg_cfg in list(packages.items())[:MAX_PACKAGES]:
            if isinstance(pkg_cfg, dict):
                if pkg_cfg.get("draft", True) is not True:
                    refusals.append(
                        f"{config_path}: package {pkg_name!r} sets draft != true"
                    )
                if pkg_cfg.get("force-tag-creation", True) is not True:
                    refusals.append(
                        f"{config_path}: package {pkg_name!r} sets force-tag-creation != true"
                    )
    return refusals


def check_release_triggers(workflow: dict[str, object], path: Path) -> list[str]:
    """Refuse release.yml triggers on release published or created events."""
    on_cfg = workflow.get("on") if "on" in workflow else workflow.get(True)
    if on_cfg is None:
        raise CheckError(f"{path}: missing 'on' trigger configuration")
    if isinstance(on_cfg, str):
        if on_cfg == "release":
            return [
                f"{path}: triggers on release event (default types include published)"
            ]
        return []
    if isinstance(on_cfg, list):
        if "release" in on_cfg:
            return [
                f"{path}: triggers on release event (default types include published)"
            ]
        return []
    if not isinstance(on_cfg, dict) or "release" not in on_cfg:
        return []
    rel_cfg = on_cfg.get("release")
    if not isinstance(rel_cfg, dict) or "types" not in rel_cfg:
        return [f"{path}: triggers on release event (default types include published)"]
    raw_types = rel_cfg.get("types")
    types = [raw_types] if isinstance(raw_types, str) else list(raw_types or [])
    bad = [
        str(item) for item in types[:MAX_STEPS] if str(item) in FORBIDDEN_RELEASE_TYPES
    ]
    if bad:
        return [
            (
                f"{path}: triggers on release: {', '.join(bad)} "
                "(gates would start after the release page is public)"
            )
        ]
    return []


def shell_commands(script: str) -> list[str]:
    """Strip comment lines and join backslash-continued lines in a shell snippet."""
    commands: list[str] = []
    current = ""
    for raw_line in script.splitlines()[:MAX_LINES]:
        stripped = raw_line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if stripped.endswith("\\"):
            current += stripped[:-1] + " "
        else:
            commands.append(current + stripped)
            current = ""
    if current:
        commands.append(current)
    return commands


def step_publish_reason(step: dict[str, object]) -> str | None:
    """Return a description when step publishes a GitHub release, else None."""
    uses = step.get("uses")
    if isinstance(uses, str):
        action = uses.split("@", 1)[0].strip()
        if action in RELEASE_ACTIONS:
            with_cfg = step.get("with")
            draft_val = with_cfg.get("draft") if isinstance(with_cfg, dict) else None
            if draft_val is not True and str(draft_val).lower() != "true":
                return f"uses {action} without draft: true"
    run_script = step.get("run")
    if isinstance(run_script, str):
        for cmd in shell_commands(run_script):
            if GH_CREATE_RE.search(cmd) and (
                GH_DRAFT_FALSE_RE.search(cmd) or not GH_DRAFT_FLAG_RE.search(cmd)
            ):
                return "runs 'gh release create' without --draft"
            if GH_EDIT_RE.search(cmd) and GH_DRAFT_FALSE_RE.search(cmd):
                return "runs 'gh release edit --draft=false'"
            if GH_API_RELEASES_RE.search(cmd) and GH_API_DRAFT_FALSE_RE.search(cmd):
                return "runs 'gh api' on releases with draft=false"
    return None


def job_publish_reasons(job_id: str, job: dict[str, object], path: Path) -> list[str]:
    """Return all publish action descriptions found in the steps of job."""
    steps = job.get("steps")
    if steps is None:
        return []
    if not isinstance(steps, list) or len(steps) > MAX_STEPS:
        raise CheckError(f"{path}: job {job_id}: invalid or oversized steps list")
    reasons: list[str] = []
    for step in steps[:MAX_STEPS]:
        if not isinstance(step, dict):
            raise CheckError(f"{path}: job {job_id}: step is not a mapping")
        reason = step_publish_reason(step)
        if reason is not None:
            reasons.append(reason)
    return reasons


def direct_needs(job_id: str, job: dict[str, object], path: Path) -> list[str]:
    """Return the list of direct job dependencies declared by job.needs."""
    raw = job.get("needs")
    if raw is None:
        return []
    if isinstance(raw, str):
        return [raw]
    if isinstance(raw, list) and all(isinstance(item, str) for item in raw):
        return list(raw[:MAX_JOBS])
    raise CheckError(f"{path}: job {job_id}: invalid needs declaration")


def transitive_needs(
    jobs: dict[str, dict[str, object]], start_job: str, path: Path
) -> set[str]:
    """Return all existing jobs transitively needed by start_job, bounded by MAX_JOBS."""
    reached: set[str] = set()
    frontier = [
        dep for dep in direct_needs(start_job, jobs[start_job], path) if dep in jobs
    ]
    for _ in range(MAX_JOBS):
        if not frontier:
            break
        current = frontier.pop()
        if current in reached or current not in jobs:
            continue
        reached.add(current)
        for dep in direct_needs(current, jobs[current], path):
            if dep in jobs and dep not in reached:
                frontier.append(dep)
    return reached


def load_workflow_jobs(
    path: Path,
) -> tuple[dict[str, object], dict[str, dict[str, object]]]:
    """Parse one workflow YAML file and validate its jobs mapping."""
    try:
        raw = path.read_text(encoding="utf-8")
        doc = load_unique(raw)
    except (OSError, UnicodeDecodeError, yaml.YAMLError, TypeError) as error:
        raise CheckError(f"{path}: {error}") from error
    if not isinstance(doc, dict):
        raise CheckError(f"{path}: workflow root must be a YAML mapping")
    raw_jobs = doc.get("jobs")
    if not isinstance(raw_jobs, dict) or not raw_jobs or len(raw_jobs) > MAX_JOBS:
        raise CheckError(
            f"{path}: jobs must be a non-empty mapping of at most {MAX_JOBS} jobs"
        )
    jobs: dict[str, dict[str, object]] = {}
    for job_id, job_cfg in list(raw_jobs.items())[:MAX_JOBS]:
        if not isinstance(job_id, str) or not isinstance(job_cfg, dict):
            raise CheckError(f"{path}: job {job_id!r} must be a mapping")
        jobs[job_id] = job_cfg
    return doc, jobs


def check_workflow_file(path: Path) -> list[str]:
    """Check one workflow file for release trigger and publish ordering violations."""
    doc, jobs = load_workflow_jobs(path)
    is_release = path.name == RELEASE_WORKFLOW
    refusals = check_release_triggers(doc, path) if is_release else []
    publish_count = 0
    for job_id, job_cfg in list(jobs.items())[:MAX_JOBS]:
        reasons = job_publish_reasons(job_id, job_cfg, path)
        if not reasons:
            continue
        publish_count += 1
        if not is_release:
            for reason in reasons[:MAX_STEPS]:
                refusals.append(
                    f"{path}: job {job_id}: {reason} in workflow other than {RELEASE_WORKFLOW}"
                )
            continue
        condition = job_cfg.get("if")
        if isinstance(condition, str) and GATE_BYPASS_RE.search(condition):
            refusals.append(
                f"{path}: job {job_id}: condition {condition!r} runs it after a failed gate"
            )
        reached = transitive_needs(jobs, job_id, path)
        missing = [gate for gate in REQUIRED_GATES if gate not in reached]
        if missing:
            for reason in reasons[:MAX_STEPS]:
                refusals.append(
                    f"{path}: job {job_id}: {reason} without needing gate(s): "
                    f"{', '.join(missing)}"
                )
    if is_release and publish_count == 0:
        refusals.append(f"{path}: no job publishes the draft release")
    return refusals


def evaluate(workflows_dir: Path, config_path: Path) -> tuple[int, list[str]]:
    """Evaluate config_path and all *.yml workflows in workflows_dir."""
    refusals = check_release_please_config(config_path)
    if not workflows_dir.is_dir():
        raise CheckError(f"{workflows_dir}: workflow directory does not exist")
    files = sorted(workflows_dir.glob("*.yml"))
    if not files or len(files) > MAX_FILES:
        raise CheckError(
            f"{workflows_dir}: expected 1..{MAX_FILES} *.yml files, found {len(files)}"
        )
    if not (workflows_dir / RELEASE_WORKFLOW).is_file():
        raise CheckError(
            f"{workflows_dir / RELEASE_WORKFLOW}: missing {RELEASE_WORKFLOW}"
        )
    for path in files[:MAX_FILES]:
        refusals.extend(check_workflow_file(path))
    if refusals:
        return 3, refusals
    return 0, [
        (
            f"release_publish_order_check: ok ({len(files)} workflow(s), "
            "draft release published only after all gates)"
        )
    ]


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
    config: dict[str, object] | str = GOOD_CONFIG,
    release_yml: str = GOOD_RELEASE_YML,
    extra_workflows: dict[str, str] | None = None,
) -> tuple[Path, Path]:
    """Create a temporary workflows directory and release-please config for one test case."""
    case_dir = root / name
    wf_dir = case_dir / "workflows"
    wf_dir.mkdir(parents=True, exist_ok=True)
    cfg_path = case_dir / "release-please-config.json"
    cfg_text = config if isinstance(config, str) else json.dumps(config)
    cfg_path.write_text(cfg_text, encoding="utf-8")
    (wf_dir / RELEASE_WORKFLOW).write_text(release_yml, encoding="utf-8")
    for filename, content in list((extra_workflows or {}).items())[:MAX_FILES]:
        (wf_dir / filename).write_text(content, encoding="utf-8")
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
        "runs 'gh api' on releases with draft=false",
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


def self_test() -> None:
    """Run planted positive, refusal, and error cases."""
    with tempfile.TemporaryDirectory() as scratch:
        root = Path(scratch)
        for label, name, cfg, rel_yml, extra, want_exit, needle in SELF_TEST_CASES[
            :MAX_CASES
        ]:
            wf_dir, cfg_path = write_fixture(root, name, cfg, rel_yml, extra)
            run_cli_case(label, wf_dir, cfg_path, want_exit, needle)
        run_cli_case(
            "unreadable file",
            root / "ok_current" / "workflows",
            root / "nonexistent-config.json",
            1,
            "nonexistent-config.json",
        )
    print(
        f"release_publish_order_check self-test: {len(SELF_TEST_CASES) + 1} cases behaved "
        "as required"
    )


def main(argv: list[str] | None = None) -> int:
    """CLI entrypoint for the release publish order check."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--workflows-dir",
        type=Path,
        default=DEFAULT_WORKFLOWS_DIR,
        help="directory containing GitHub workflow *.yml files",
    )
    parser.add_argument(
        "--config",
        type=Path,
        default=DEFAULT_CONFIG,
        help="path to release-please-config.json",
    )
    parser.add_argument(
        "--self-test", action="store_true", help="run planted self-tests"
    )
    args = parser.parse_args(argv)
    try:
        if args.self_test:
            self_test()
            return 0
        exit_code, lines = evaluate(args.workflows_dir, args.config)
    except SelfTestError as error:
        print(f"release_publish_order_check self-test failed: {error}", file=sys.stderr)
        return 1
    except CheckError as error:
        print(f"release_publish_order_check error: {error}", file=sys.stderr)
        return 1
    stream = sys.stderr if exit_code != 0 else sys.stdout
    for line in lines:
        print(line, file=stream)
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
