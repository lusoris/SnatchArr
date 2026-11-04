# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse release workflows or configs that publish the release page before all gates pass.

Checks .github/workflows/*.yml, *.yaml and release-please-config.json:
1. release-please-config.json must set draft: true and force-tag-creation: true.
2. release.yml must not trigger on release events of type published or created.
3. Only release.yml may publish a GitHub release, at least one job in release.yml must
   publish, and every publishing job must transitively need dogfood-gate, image-smoke,
   build, release-files, and verify.
Release actions and gh release subcommands use allow-lists. Mutating release REST
calls count as publishing. Relative .sh/.py files referenced by run steps are scanned
one level deep; scripts called by those files are not followed (non-recursive DAG).
Python scanning covers literal subprocess/os shell commands, not arbitrary evaluation.
Exit 0 = ok, 1 = input or parse error, 3 = refused.
"""

from __future__ import annotations

import argparse
import ast
import importlib.util
import itertools
import json
import re
import shlex
import sys
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
MAX_LINES = 4096
MAX_TOKENS = 16384
MAX_PYTHON_NODES = 65536
MAX_CASES = 128
RELEASE_ACTIONS = (
    "softprops/action-gh-release",
    "actions/create-release",
    "ncipollo/release-action",
    "marvinpinto/action-automatic-releases",
    "elgohr/github-release-action",
)
ALLOWED_RELEASE_ACTIONS = frozenset(("googleapis/release-please-action",))
ALLOWED_RELEASE_COMMANDS = frozenset(
    (
        "view",
        "list",
        "download",
        "upload",
        "create",
        "edit",
        "delete-asset",
        "verify-asset",
    )
)
# gh release create/edit flags that take a value (gh 2.102.1 --help); pflag consumes the next token.
GH_RELEASE_VALUE_FLAGS = frozenset(
    (
        "--discussion-category",
        "--notes",
        "-n",
        "--notes-file",
        "-F",
        "--notes-start-tag",
        "--repo",
        "-R",
        "--tag",
        "--target",
        "--title",
        "-t",
    )
)
# Values Go's strconv.ParseBool accepts (pkg.go.dev/strconv#ParseBool); gh parses --draft with it.
PARSEBOOL_TRUE = frozenset(("1", "t", "T", "TRUE", "true", "True"))
PARSEBOOL_FALSE = frozenset(("0", "f", "F", "FALSE", "false", "False"))
PYTHON_COMMAND_CALLS = frozenset(
    ("run", "call", "check_call", "check_output", "Popen", "system", "popen")
)
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
    return check_release_event_types(on_cfg.get("release"), path)


def check_release_event_types(rel_cfg: object, path: Path) -> list[str]:
    """Validate release event types and refuse created/published triggers."""
    if not isinstance(rel_cfg, dict) or "types" not in rel_cfg:
        return [f"{path}: triggers on release event (default types include published)"]
    raw_types = rel_cfg.get("types")
    if not isinstance(raw_types, (str, list)):
        raise CheckError(f"{path}: release types must be a string or list")
    types = [raw_types] if isinstance(raw_types, str) else raw_types
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


def bounded_lines(script: str) -> list[str]:
    """Reject input exceeding the scan bound rather than ignoring its tail."""
    lines = script.splitlines()
    if len(lines) > MAX_LINES:
        raise CheckError(f"script exceeds MAX_LINES ({MAX_LINES})")
    return lines


def shell_commands(script: str) -> list[str]:
    """Join continuations and multiline quoted strings; refuse incomplete quoting."""
    commands: list[str] = []
    current = ""
    for raw_line in bounded_lines(script)[:MAX_LINES]:
        stripped = raw_line.strip()
        if not current and (not stripped or stripped.startswith("#")):
            continue
        if stripped.endswith("\\"):
            current += stripped[:-1] + " "
            continue
        current += raw_line
        try:
            shlex.split(current, comments=True, posix=True)
        except ValueError:
            current += "\n"
            continue
        commands.append(current)
        current = ""
    if current:
        try:
            shlex.split(current, comments=True, posix=True)
        except ValueError as error:
            raise CheckError(f"shell parse error: {error}") from error
        commands.append(current)
    return commands


def command_tokens(command: str) -> list[str]:
    """Use POSIX quoting and separate shell operators without executing anything."""
    try:
        shlex.split(command, comments=True, posix=True)
        lexer = shlex.shlex(command, posix=True, punctuation_chars=";&|()<>")
        lexer.whitespace_split = True
        tokens = list(itertools.islice(lexer, MAX_TOKENS + 1))
    except ValueError as error:
        raise CheckError(f"shell parse error: {error}") from error
    if len(tokens) > MAX_TOKENS:
        raise CheckError(f"command exceeds MAX_TOKENS ({MAX_TOKENS})")
    return tokens


def option_tokens(tokens: list[str]) -> list[str]:
    """Keep flags as gh's pflag parser sees them.

    The token after a value-taking flag is its value, whatever it looks like, and
    everything after "--" is a positional argument (an asset file name).
    """
    flags: list[str] = []
    skip = False
    for arg in tokens[:MAX_TOKENS]:
        if skip:
            skip = False
            continue
        if arg == "--":
            break
        if arg in GH_RELEASE_VALUE_FLAGS:
            skip = True
        else:
            flags.append(arg)
    return flags


def draft_state(flags: list[str]) -> bool | None:
    """Return the last --draft value as gh parses it (strconv.ParseBool), None if absent.

    An unparsable value raises CheckError: gh would refuse it, and the check fails closed.
    """
    state = None
    for flag in flags[:MAX_TOKENS]:
        if flag in ("--draft", "-d"):
            state = True
            continue
        name, sep, value = flag.partition("=")
        if not sep or name not in ("--draft", "-d"):
            continue
        if value in PARSEBOOL_TRUE:
            state = True
        elif value in PARSEBOOL_FALSE:
            state = False
        else:
            raise CheckError(f"--draft value {value!r} is not a boolean gh accepts")
    return state


def gh_release_reason(args: list[str]) -> str | None:
    """Allow known gh release operations, counting create/edit publication."""
    sub = args[0] if args else ""
    if sub not in ALLOWED_RELEASE_COMMANDS:
        return f"runs 'gh release {sub}' (not on the allow-list)"
    draft = draft_state(option_tokens(args[1:]))
    if sub == "create" and draft is not True:
        return "runs 'gh release create' without --draft"
    if sub == "edit" and draft is False:
        return "runs 'gh release edit --draft=false'"
    return None


def request_method(tokens: list[str], index: int) -> str | None:
    """Read separate, equals and attached forms of HTTP method flags."""
    token = tokens[index]
    option, _, value = token.partition("=")
    if token.startswith("-X"):
        value = token[2:]
    elif option not in ("--method", "--request"):
        return None
    return value or (tokens[index + 1] if index + 1 < len(tokens) else "")


def body_option(token: str, api: bool) -> bool:
    """Identify flags that make a release request mutating even with explicit GET."""
    option = token.partition("=")[0]
    if api:
        return option in ("--field", "--raw-field", "--input") or token.startswith(
            ("-f", "-F")
        )
    return (
        option.startswith("--data")
        or option
        in ("--upload-file", "--post-data", "--post-file", "--body-data", "--body-file")
        or token.startswith(("-d", "-T"))
    )


def mutating_options(tokens: list[str], api: bool = False) -> bool:
    """Recognise explicit non-read methods and flags that imply a request body."""
    for index, token in enumerate(tokens[:MAX_TOKENS]):
        if body_option(token, api):
            return True
        method = request_method(tokens, index)
        if method is not None and method.upper() != "GET":
            return True
    return False


def gh_command_reason(args: list[str]) -> str | None:
    """Check a gh release or release REST operation."""
    if not args:
        return None
    if args[0] == "release":
        return gh_release_reason(args[1:])
    if (
        args[0] == "api"
        and any("releases" in arg for arg in args[:MAX_TOKENS])
        and mutating_options(args[1:], api=True)
    ):
        return "runs 'gh api' on releases with a mutating request"
    return None


def command_arguments(tokens: list[str], index: int) -> list[str]:
    """Stop at a shell operator, keeping another command's flags out of this one."""
    tail = tokens[index + 1 :]
    end = next(
        (
            i
            for i, item in enumerate(tail[:MAX_TOKENS])
            if item in (";", "&&", "||", "|", ")", ">", ">>")
        ),
        len(tail),
    )
    return tail[:end]


def preferred_reason(first: str | None, found: str | None) -> str | None:
    """Keep the first publisher, prioritising an unconditional allow-list refusal."""
    if found and "(not on the allow-list)" in found:
        return found
    return first or found


def command_publish_reason(tokens: list[str]) -> str | None:
    """Apply the same gh and REST rules to direct and indirect commands."""
    first = None
    for index, token in enumerate(tokens[:MAX_TOKENS]):
        if token not in ("gh", "curl", "wget"):
            continue
        args = command_arguments(tokens, index)
        if token == "gh":  # nosec B105 -- command name, not a password
            reason = gh_command_reason(args)
        else:
            reason = None
            if any(
                "/releases" in arg for arg in args[:MAX_TOKENS]
            ) and mutating_options(args):
                reason = f"runs '{token}' on /releases with a mutating request"
        first = preferred_reason(first, reason)
    return first


def python_command_argument(arg: ast.expr) -> str | None:
    """Read a literal shell command or literal argv without evaluating Python."""
    if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
        return arg.value
    if isinstance(arg, (ast.List, ast.Tuple)):
        parts = [
            item.value
            for item in arg.elts[:MAX_PYTHON_NODES]
            if isinstance(item, ast.Constant) and isinstance(item.value, str)
        ]
        if len(parts) == len(arg.elts):
            return shlex.join(parts)
    return None


def python_call_name(func: ast.expr) -> str:
    """Recognise qualified and imported process/shell call names."""
    if isinstance(func, ast.Attribute):
        return func.attr
    return func.id if isinstance(func, ast.Name) else ""


def python_commands(script: str) -> list[str]:
    """Extract literal command arguments from Python process/shell calls, without execution."""
    bounded_lines(script)
    try:
        tree = ast.parse(script)
    except SyntaxError as error:
        raise CheckError(f"Python parse error: {error}") from error
    commands: list[str] = []
    nodes = list(itertools.islice(ast.walk(tree), MAX_PYTHON_NODES + 1))
    if len(nodes) > MAX_PYTHON_NODES:
        raise CheckError("script exceeds MAX_PYTHON_NODES")
    for node in nodes[:MAX_PYTHON_NODES]:
        if not isinstance(node, ast.Call) or not node.args:
            continue
        name = python_call_name(node.func)
        if name not in PYTHON_COMMAND_CALLS:
            continue
        command = python_command_argument(node.args[0])
        if command is not None:
            commands.append(command)
    return commands


def repository_script(token: str, root: Path) -> tuple[Path, str] | None:
    """Read an existing relative repository script without following external paths."""
    path = Path(token)
    if path.is_absolute() or path.suffix not in (".sh", ".py"):
        return None
    target = (root / path).resolve()
    if not target.is_relative_to(root) or not target.is_file():
        return None
    try:
        script = target.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as error:
        raise CheckError(f"{path}: {error}") from error
    return path, script


def script_publish_reason(script: str) -> str | None:
    """Scan one snippet completely, keeping allow-list refusals ahead of publishers."""
    first = None
    for command in shell_commands(script)[:MAX_LINES]:
        first = preferred_reason(first, command_publish_reason(command_tokens(command)))
    return first


def indirect_publish_reason(tokens: list[str], repo_root: Path) -> str | None:
    """Scan repository .sh/.py arguments once; never follow their script calls."""
    root = repo_root.resolve()
    first = None
    for token in tokens[:MAX_TOKENS]:
        item = repository_script(token, root)
        if item is None:
            continue
        path, script = item
        chunks = python_commands(script) if path.suffix == ".py" else [script]
        for chunk in chunks[:MAX_PYTHON_NODES]:
            reason = script_publish_reason(chunk)
            if reason:
                first = preferred_reason(first, f"runs {path} which {reason}")
    return first


def action_publish_reason(uses: str, with_cfg: object) -> str | None:
    """Allow release-please and recognised draft actions; refuse unknown release actions."""
    action = uses.split("@", 1)[0].strip()
    owner_repo = "/".join(action.split("/")[:2]).lower()
    if owner_repo in RELEASE_ACTIONS:
        draft = with_cfg.get("draft") if isinstance(with_cfg, dict) else None
        if draft is not True and str(draft).lower() != "true":
            return f"uses {action} without draft: true"
    elif "release" in owner_repo and owner_repo not in ALLOWED_RELEASE_ACTIONS:
        return f"uses {action} (not on the allow-list)"
    return None


def step_publish_reason(step: dict[str, object], repo_root: Path) -> str | None:
    """Find publishing actions/commands, giving unknown operations priority."""
    uses = step.get("uses")
    first = (
        action_publish_reason(uses, step.get("with")) if isinstance(uses, str) else None
    )
    run_script = step.get("run")
    if isinstance(run_script, str):
        for command in shell_commands(run_script)[:MAX_LINES]:
            tokens = command_tokens(command)
            first = preferred_reason(first, command_publish_reason(tokens))
            first = preferred_reason(first, indirect_publish_reason(tokens, repo_root))
    return first


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
        reason = step_publish_reason(step, path.parent.parent.parent)
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


def publish_job_refusals(
    job_id: str,
    job: dict[str, object],
    jobs: dict[str, dict[str, object]],
    path: Path,
    reasons: list[str],
) -> list[str]:
    """Require gates for publishers and refuse operations outside the allow-lists."""
    refusals = [
        f"{path}: job {job_id}: {reason}"
        for reason in reasons[:MAX_STEPS]
        if "(not on the allow-list)" in reason
    ]
    condition = job.get("if")
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
    return refusals


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
        refusals.extend(publish_job_refusals(job_id, job_cfg, jobs, path, reasons))
    if is_release and publish_count == 0:
        refusals.append(f"{path}: no job publishes the draft release")
    return refusals


def evaluate(workflows_dir: Path, config_path: Path) -> tuple[int, list[str]]:
    """Evaluate config_path and all *.yml and *.yaml workflows in workflows_dir."""
    refusals = check_release_please_config(config_path)
    if not workflows_dir.is_dir():
        raise CheckError(f"{workflows_dir}: workflow directory does not exist")
    files = sorted((*workflows_dir.glob("*.yml"), *workflows_dir.glob("*.yaml")))
    if not files or len(files) > MAX_FILES:
        raise CheckError(
            f"{workflows_dir}: expected 1..{MAX_FILES} *.yml or *.yaml files, found {len(files)}"
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


SELF_TEST_MODULE = "release_publish_order_selftest"


def run_self_test() -> None:
    """Load the planted cases from the sibling module and run them against this module."""
    sys.modules.setdefault("release_publish_order_check", sys.modules[__name__])
    path = Path(__file__).with_name(f"{SELF_TEST_MODULE}.py")
    spec = importlib.util.spec_from_file_location(SELF_TEST_MODULE, path)
    if not path.is_file() or spec is None or spec.loader is None:
        raise SelfTestError(f"cannot load self-test cases from {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.self_test()


def main(argv: list[str] | None = None) -> int:
    """CLI entrypoint for the release publish order check."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--workflows-dir",
        type=Path,
        default=DEFAULT_WORKFLOWS_DIR,
        help="directory containing GitHub workflow *.yml and *.yaml files",
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
            run_self_test()
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
