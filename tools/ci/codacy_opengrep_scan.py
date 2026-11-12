#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Mirror Codacy's enabled Opengrep (Semgrep) Python rules locally, against a shrink-only baseline.

Modes (one at a time):
  (none)           run the live canary, then scan every tracked Python file; network, Linux x64
  --self-test      replay the recorded reports and the planted cases; offline
  --drift LISTING  compare a Codacy pattern listing with the checked-in config; offline
  --base-ref REF   refuse a baseline that grew (a new key or a higher count), or a kept entry
                   whose expiry moved later or whose entered date, category, issue or reason
                   changed, against the baseline at REF; offline

Every mode except --drift validates the baseline first and fails on an expired entry
(--today YYYY-MM-DD replaces the current UTC date). A baseline entry is keyed by rule, path and
the normalised source text of the finding, so a line shift keeps its key: whitespace runs outside
string literals collapse to one space, whitespace inside a literal stays as written. Its count is
how many findings share that key; one more is a new finding.

The canary renames the parameter of judge_in_child in tools/ci/image_release_age_check.py from
`job` to `request`, the defect commit 58af3cb4 planted, and requires exactly one new finding of
the Django subprocess-injection rule. It runs before the tree scan and fails when the rules stop
reporting that defect.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess  # nosec B404  # fixed argument lists built in this file, never a shell
import sys
import tempfile
import threading
import urllib.request
from collections import Counter
from collections.abc import Callable, Iterator
from contextlib import contextmanager
from datetime import UTC, date, datetime, timedelta
from pathlib import Path
from typing import Self

BASELINE = "tools/ci/codacy_opengrep/baseline.json"
CONFIG = "tools/ci/codacy_opengrep/opengrep-python.config.json"
FIXTURES = "tools/ci/codacy_opengrep/testdata"
PACKAGE = "tools/ci/codacy_opengrep"
TOOL_VERSIONS = "tools/ci/tool-versions.env"
CLI_PACKAGE = "@codacy/analysis-cli"
CANARY_FILE = "tools/ci/image_release_age_check.py"
CANARY_FUNCTION = "judge_in_child"
CANARY_RULE = "Semgrep_python.django.security.injection.command.subprocess-injection.subprocess-injection"
DOWNLOAD_PREFIX = "https://github.com/opengrep/opengrep/releases/download/v"
BASELINE_VERSION = 2
BASELINE_KEYS = frozenset({"patternId", "filePath", "contentSha256", "count", "category", "entered", "expires", "issue", "reason"})
KEPT_FIELDS = ("entered", "category", "issue", "reason")  # a kept entry may not change these
CATEGORY_DAYS = {"B": 3, "C": 10, "D": 120}  # MEL categories; A states its own interval
DATE_RE = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}")
SHA_RE = re.compile(r"[0-9a-f]{64}")
COMMIT_RE = re.compile(r"[0-9a-f]{40}|[0-9a-f]{64}")
COUNT_RE = re.compile(r"[1-9][0-9]?")
ISSUE_RE = re.compile(r"[1-9][0-9]{0,5}")
PATH_RE = re.compile(r"[A-Za-z0-9_.][A-Za-z0-9_./-]{0,250}\.py")
REF_RE = re.compile(r"[A-Za-z0-9][A-Za-z0-9._/~^-]{0,199}")
VERSION_RE = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+")
# Python string literals: triple-quoted (may run to the end of the reported text), then one-line.
LITERAL_RE = re.compile(r'"""(?:\\.|(?!""").)*(?:"""|\Z)' r"|'''(?:\\.|(?!''').)*(?:'''|\Z)" r'|"(?:\\.|[^"\\\n])*"' r"|'(?:\\.|[^'\\\n])*'", re.DOTALL)
REASON_CHARS = (20, 300)
MAX_BASELINE_ENTRIES = 512
MAX_CONTENT_CHARS = 64 * 1024
MAX_FILES = 4096
GIT_TIMEOUT_SECONDS = 30
CLI_TIMEOUT_SECONDS = 240
TOOL_TIMEOUT_MS = 210_000
DOWNLOAD_TIMEOUT_SECONDS = 60
DOWNLOAD_DEADLINE_SECONDS = 180
MAX_DOWNLOAD_BYTES = 128 * 1024 * 1024
CHUNK_BYTES = 1024 * 1024
MAX_READS = 1 << 20
ROOT = Path(__file__).resolve().parents[2]


class ScanError(Exception):
    """Signal a report, baseline, or scanner result that cannot be trusted."""


def load_json(path: Path) -> object:
    """Load one UTF-8 JSON document or raise a scanner error."""
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as err:
        raise ScanError(f"cannot read {path}: {err}") from err


def run_capture(argv: list[str], cwd: Path, timeout: float, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    """Run one argument list (never a shell) under a deadline and capture its text output."""
    try:
        return subprocess.run(argv, cwd=cwd, env=env, capture_output=True, text=True, timeout=timeout, check=False)  # nosec B603  # argument lists built in this file, no shell
    except (OSError, subprocess.TimeoutExpired) as err:
        raise ScanError(f"{Path(argv[0]).name} did not complete: {err}") from err


def git_env(environ: dict[str, str]) -> dict[str, str]:
    """Return ENVIRON without GIT_* variables: GIT_DIR, GIT_INDEX_FILE and the like would point git elsewhere."""
    return {key: value for key, value in environ.items() if not key.startswith("GIT_")}


def git(root: Path, *args: str) -> subprocess.CompletedProcess[str]:
    """Run git, resolved on PATH, on the repository at ROOT."""
    exe = shutil.which("git")
    if exe is None:
        raise ScanError("git not found on PATH")
    return run_capture([exe, *args], root, GIT_TIMEOUT_SECONDS, git_env(dict(os.environ)))


def config_tool(root: Path) -> dict[str, object]:
    """Return the one tool entry of the checked-in config; refuse a fallback to local rule files."""
    data = load_json(root / CONFIG)
    tools = data.get("tools") if isinstance(data, dict) else None
    if not isinstance(tools, list) or len(tools) != 1 or not isinstance(tools[0], dict):
        raise ScanError(f"{CONFIG}: want exactly one tool entry")
    if tools[0].get("useLocalConfigurationFile") is not False:
        raise ScanError(f"{CONFIG}: useLocalConfigurationFile must be false, or the CLI reads .semgrep.yml")
    return tools[0]


def config_ids(root: Path) -> list[str]:
    """Return the unique, sorted Semgrep pattern IDs from the checked-in config."""
    patterns = config_tool(root).get("patterns")
    ids = [item.get("patternId") for item in patterns if isinstance(item, dict)] if isinstance(patterns, list) else []
    if not ids or not all(isinstance(item, str) for item in ids):
        raise ScanError(f"{CONFIG}: invalid pattern list")
    if ids != sorted(ids) or len(ids) != len(set(ids)):
        raise ScanError(f"{CONFIG}: pattern IDs must be sorted and unique")
    return ids


def parse_date(text: str) -> date | None:
    """Return the date TEXT names in YYYY-MM-DD form, else None."""
    if not DATE_RE.fullmatch(text):
        return None
    try:
        return date.fromisoformat(text)
    except ValueError:
        return None


def date_problems(item: dict[str, str]) -> list[str]:
    """Return why the entered and expires dates of one entry break its MEL category."""
    entered, expires = parse_date(item["entered"]), parse_date(item["expires"])
    if entered is None or expires is None:
        return ["entered and expires must be YYYY-MM-DD dates"]
    days = CATEGORY_DAYS.get(item["category"])
    if days is not None and expires != entered + timedelta(days=days):
        return [f"category {item['category']} expires {days} days after entered, on {entered + timedelta(days=days)}"]
    if expires <= entered:
        return ["expires must follow entered"]
    return []


def entry_problems(item: object, configured: set[str] | None) -> list[str]:
    """Return why one baseline entry is malformed: allow-listed keys and value shapes only.

    CONFIGURED None skips the enabled-pattern check: an older baseline may name a rule the
    current mirror no longer enables, and removing that rule's entries is a valid shrink.
    """
    if not isinstance(item, dict) or set(item) != BASELINE_KEYS or not all(isinstance(value, str) for value in item.values()):
        return [f"keys must be exactly {sorted(BASELINE_KEYS)}, every value a string"]
    path = item["filePath"]
    checks = (
        (configured is None or item["patternId"] in configured, "patternId is not an enabled pattern"),
        (PATH_RE.fullmatch(path) is not None and ".." not in path.split("/"), "filePath is not a relative .py path"),
        (SHA_RE.fullmatch(item["contentSha256"]) is not None, "contentSha256 is not 64 lowercase hex digits"),
        (COUNT_RE.fullmatch(item["count"]) is not None, "count is not a number from 1 to 99"),
        (ISSUE_RE.fullmatch(item["issue"]) is not None, "issue is not an issue number"),
        (REASON_CHARS[0] <= len(item["reason"]) <= REASON_CHARS[1] and "\n" not in item["reason"], "reason needs 20 to 300 characters on one line"),
        (item["category"] in ("A", "B", "C", "D"), "category is not a MEL category A to D"),
    )
    problems = [why for ok, why in checks if not ok]
    return problems if problems else date_problems(item)


def entry_key(item: dict[str, str]) -> str:
    """Return the rule, path and source-text key of one baseline entry."""
    return f"{item['patternId']}\0{item['filePath']}\0{item['contentSha256']}"


def entry_expired(item: dict[str, str], today: date) -> bool:
    """Return True once TODAY is past the entry's expires date (the expires day itself still holds)."""
    return date.fromisoformat(item["expires"]) < today


def baseline_findings(data: object) -> list[object]:
    """Return the findings list of a version-2 baseline document of at most MAX_BASELINE_ENTRIES, else raise."""
    if not (isinstance(data, dict) and set(data) == {"version", "findings"} and data["version"] == BASELINE_VERSION and isinstance(data["findings"], list)):
        raise ScanError(f"baseline must be {{\"version\": {BASELINE_VERSION}, \"findings\": [...]}}")
    if len(data["findings"]) > MAX_BASELINE_ENTRIES:
        raise ScanError(f"baseline holds {len(data['findings'])} entries, at most {MAX_BASELINE_ENTRIES}")
    return data["findings"]


def entry_failures(item: object, configured: set[str] | None, entries: dict[str, dict[str, str]], today: date | None) -> list[str]:
    """Return why ITEM cannot join ENTRIES: malformed, a duplicate key, or (with TODAY) expired."""
    found = entry_problems(item, configured)
    if found:
        return found
    if entry_key(item) in entries:
        return ["duplicate key; raise the count of the first entry instead"]
    if today is not None and entry_expired(item, today):
        return [f"expired {item['expires']} (issue #{item['issue']}); fix the finding, the shrink-only check refuses a renewed entry"]
    return []


def parse_baseline(data: object, configured: set[str] | None, today: date | None) -> dict[str, dict[str, str]]:
    """Return baseline entries by key; refuse a malformed, duplicate or (with TODAY) expired entry."""
    entries: dict[str, dict[str, str]] = {}
    problems = []
    for index, item in enumerate(baseline_findings(data)):
        found = entry_failures(item, configured, entries, today)
        if found:
            where = item.get("filePath") if isinstance(item, dict) else "?"
            problems.extend(f"entry {index} ({where}): {why}" for why in found)
        else:
            entries[entry_key(item)] = item
    if problems:
        raise ScanError("; ".join(problems))
    return entries


def load_baseline(root: Path, today: date) -> dict[str, dict[str, str]]:
    """Return the checked-in baseline entries by key, refusing expired entries."""
    try:
        return parse_baseline(load_json(root / BASELINE), set(config_ids(root)), today)
    except ScanError as err:
        raise ScanError(f"{BASELINE}: {err}") from err


def baseline_counts(entries: dict[str, dict[str, str]]) -> Counter[str]:
    """Return how many findings each baseline key admits."""
    return Counter({key: int(item["count"]) for key, item in entries.items()})


def entry_changes(key: str, old: dict[str, str], new: dict[str, str]) -> list[str]:
    """Return why NEW, the kept entry KEY, is no shrink of OLD."""
    problems = [f"expiry moved from {old['expires']} to {new['expires']}: {pretty(key)}"] if new["expires"] > old["expires"] else []
    if int(new["count"]) > int(old["count"]):
        problems.append(f"baseline grew: count {old['count']} -> {new['count']}: {pretty(key)}")
    problems.extend(f"{field} changed from {old[field]!r} to {new[field]!r}: {pretty(key)}" for field in KEPT_FIELDS if new[field] != old[field])
    return problems


def compare_baselines(base: dict[str, dict[str, str]], current: dict[str, dict[str, str]]) -> list[str]:
    """Return why CURRENT is not a shrink of BASE: a new key, a higher count, a later expiry or a changed record."""
    problems = [f"baseline grew: {pretty(key)}" for key in sorted(set(current) - set(base))]
    for key in sorted(set(current) & set(base)):
        problems.extend(entry_changes(key, base[key], current[key]))
    return problems


def ref_problem(ref: str) -> str | None:
    """Return why REF is no plain branch, tag, commit or HEAD~N name, else None; refuses option-like text."""
    if REF_RE.fullmatch(ref) is None or ".." in ref:
        return f"--base-ref {ref!r}: not a branch, tag or commit name"
    return None


def resolve_commit(root: Path, ref: str) -> str:
    """Return the commit id REF names; only that id reaches later git commands."""
    problem = ref_problem(ref)
    if problem is not None:
        raise ScanError(problem)
    found = git(root, "rev-parse", "--verify", "--quiet", "--end-of-options", f"{ref}^{{commit}}")
    commit = found.stdout.strip()
    if found.returncode != 0 or COMMIT_RE.fullmatch(commit) is None:
        raise ScanError(f"--base-ref {ref}: not a commit")
    return commit


def shrink_problems(root: Path, ref: str, today: date) -> list[str]:
    """Compare the baseline with its version at REF; a REF without one is the first baseline."""
    commit = resolve_commit(root, ref)
    current = load_baseline(root, today)
    listed = git(root, "ls-tree", "--name-only", commit, "--", BASELINE)
    if listed.returncode != 0:
        raise ScanError(f"git ls-tree {ref} failed: {listed.stderr.strip()}")
    if not listed.stdout.strip():
        print(f"codacy-opengrep: {BASELINE} absent at {ref}; first baseline, {len(current)} entries")
        return []
    shown = git(root, "show", f"{commit}:{BASELINE}")
    if shown.returncode != 0:
        raise ScanError(f"git show {ref}:{BASELINE} failed: {shown.stderr.strip()}")
    try:
        base = parse_baseline(json.loads(shown.stdout), None, None)
    except (json.JSONDecodeError, ScanError) as err:
        raise ScanError(f"{BASELINE} at {ref}: {err}") from err
    problems = compare_baselines(base, current)
    if not problems:
        print(f"codacy-opengrep: baseline is a shrink of the one at {ref} ({len(base)} -> {len(current)} entries)")
    return problems


def pretty(key: str) -> str:
    """Render a NUL-joined key for a diagnostic."""
    return key.replace("\0", " | ")


def normalised_source(text: str) -> str:
    """Collapse whitespace runs outside Python string literals to one space; keep literals verbatim."""
    parts, last = [], 0
    for match in LITERAL_RE.finditer(text):
        parts += [re.sub(r"\s+", " ", text[last:match.start()]), match.group(0)]
        last = match.end()
    parts.append(re.sub(r"\s+", " ", text[last:]))
    return "".join(parts).strip()


def fingerprint(issue: object) -> str:
    """Return the line-number-independent key of one Codacy issue."""
    if not isinstance(issue, dict):
        raise ScanError("report issue is not an object")
    pattern = issue.get("patternId")
    path = issue.get("filePath")
    content = issue.get("multilineContent")
    if not isinstance(content, list):
        content = [issue.get("lineContent")]
    if not isinstance(pattern, str) or not isinstance(path, str) or not all(isinstance(line, str) for line in content):
        raise ScanError("report issue lacks patternId, filePath, or source content")
    text = "\n".join(content)
    if len(text) > MAX_CONTENT_CHARS:
        raise ScanError(f"report issue in {path} carries {len(text)} source characters, at most {MAX_CONTENT_CHARS}")
    digest = hashlib.sha256(normalised_source(text).encode("utf-8")).hexdigest()
    return f"{pattern}\0{path}\0{digest}"


def capability_problems(report: dict[str, object], version: str) -> list[str]:
    """Return why the report did not come from the pinned Opengrep with the Codacy pattern list."""
    capability = report.get("capability")
    ready = capability.get("ready") if isinstance(capability, dict) else None
    semgrep = [item for item in ready if isinstance(item, dict) and item.get("toolId") == "Semgrep"] if isinstance(ready, list) else []
    if len(semgrep) != 1:
        return ["report lacks one ready Semgrep capability"]
    problems = []
    if semgrep[0].get("configSource") != "codacy":
        problems.append(f"rules came from configSource {semgrep[0].get('configSource')!r}, not the checked-in Codacy pattern list")
    if semgrep[0].get("version") != version:
        problems.append(f"Opengrep {semgrep[0].get('version')} ran, the pin is {version}")
    return problems


def report_lists(report: object) -> tuple[list[object], list[object], dict[str, object]]:
    """Return the errors, the issues and the one successful Semgrep result of REPORT, else raise."""
    if not isinstance(report, dict):
        raise ScanError("report is not a JSON object")
    errors, results, issues = report.get("errors"), report.get("toolResults"), report.get("issues")
    if not isinstance(errors, list) or not isinstance(results, list) or not isinstance(issues, list):
        raise ScanError("report lacks errors, toolResults, or issues lists")
    semgrep = [item for item in results if isinstance(item, dict) and item.get("toolId") == "Semgrep"]
    if len(semgrep) != 1 or semgrep[0].get("status") != "success":
        raise ScanError("report does not contain one successful Semgrep result")
    return errors, issues, semgrep[0]


def report_problems(report: object, files: list[str], baseline: Counter[str], version: str, enforce_stale: bool = True) -> list[str]:
    """Return report, provenance, coverage, new-finding, and stale-entry failures.

    Findings count per key: each finding past the baselined count is new, each one short is stale.
    """
    try:
        errors, issues, semgrep = report_lists(report)
        actual = Counter(fingerprint(issue) for issue in issues)
    except ScanError as err:
        return [str(err)]
    problems = capability_problems(report, version) + [f"tool error: {item}" for item in errors]
    if semgrep.get("filesAnalyzed") != len(files):
        problems.append(f"report covered {semgrep.get('filesAnalyzed')} of {len(files)} passed files")
    allowed = set(files)
    problems.extend(f"report names an unpassed file: {issue['filePath']}" for issue in issues if issue["filePath"] not in allowed)
    problems.extend(f"new finding: {pretty(item)}" for item in sorted((actual - baseline).elements()))
    if enforce_stale:
        problems.extend(f"stale baseline entry: {pretty(item)}" for item in sorted((baseline - actual).elements()))
    return problems


def canary_problems(report: object, baseline: Counter[str], version: str) -> list[str]:
    """Return [] when REPORT shows the planted defect as exactly one new CANARY_RULE finding."""
    problems = report_problems(report, [CANARY_FILE], baseline, version, enforce_stale=False)
    if len(problems) == 1 and problems[0].startswith(f"new finding: {CANARY_RULE} | {CANARY_FILE} | "):
        return []
    return [f"canary: want exactly one new {CANARY_RULE} finding in {CANARY_FILE}, got {problems}"]


def canary_source(root: Path) -> str:
    """Return CANARY_FILE with judge_in_child's parameter renamed from job to request."""
    try:
        text = (root / CANARY_FILE).read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as err:
        raise ScanError(f"canary: cannot read {CANARY_FILE}: {err}") from err
    match = re.search(rf"^def {CANARY_FUNCTION}\(.*?(?=^\S|\Z)", text, re.DOTALL | re.MULTILINE)
    if match is None:
        raise ScanError(f"canary: {CANARY_FUNCTION} not found in {CANARY_FILE}; re-derive the canary")
    body, count = re.subn(r"\bjob\b", "request", match.group(0))
    if count < 2 or re.search(r"\brequest\b", match.group(0)):
        raise ScanError(f"canary: {CANARY_FUNCTION} no longer takes `job` ({count} uses); re-derive the canary")
    return text[: match.start()] + body + text[match.end():]


def fixture_files(report: object) -> list[str]:
    """Create a synthetic passed-file set matching a recorded report's count."""
    _errors, issues, semgrep = report_lists(report)
    names = sorted({item.get("filePath") for item in issues if isinstance(item, dict) and isinstance(item.get("filePath"), str)})
    count = semgrep.get("filesAnalyzed")
    if not isinstance(count, int) or count < len(names):
        raise ScanError("fixture lacks a valid Semgrep filesAnalyzed count")
    return names + [f"clean-fixture-{index}.py" for index in range(count - len(names))]


def tracked_files(root: Path) -> list[str]:
    """List bounded tracked Python targets, excluding the recorded fixtures."""
    result = git(root, "ls-files", "-z", "--", "*.py")
    if result.returncode != 0:
        raise ScanError(f"git ls-files failed: {result.stderr.strip()}")
    names = [name for name in result.stdout.split("\0") if name and not name.startswith(FIXTURES + "/")]
    if not names or len(names) > MAX_FILES:
        raise ScanError(f"{len(names)} Python files to scan, expected 1 to {MAX_FILES}")
    return sorted(names)


def tool_versions(root: Path) -> dict[str, str]:
    """Read the exact Opengrep release pin without sourcing shell text."""
    values = {}
    for line in (root / TOOL_VERSIONS).read_text(encoding="utf-8").splitlines():
        if "=" in line and not line.lstrip().startswith("#"):
            key, value = line.split("=", 1)
            values[key] = value
    required = ("OPENGREP_VERSION", "OPENGREP_SHA256", "OPENGREP_URL")
    if any(not values.get(key) for key in required):
        raise ScanError(f"{TOOL_VERSIONS}: missing Opengrep pin")
    if not VERSION_RE.fullmatch(values["OPENGREP_VERSION"]) or not SHA_RE.fullmatch(values["OPENGREP_SHA256"]):
        raise ScanError(f"{TOOL_VERSIONS}: OPENGREP_VERSION or OPENGREP_SHA256 malformed")
    if not values["OPENGREP_URL"].startswith(f"{DOWNLOAD_PREFIX}{values['OPENGREP_VERSION']}/"):
        raise ScanError(f"{TOOL_VERSIONS}: OPENGREP_URL must start with {DOWNLOAD_PREFIX}{values['OPENGREP_VERSION']}/")
    return values


def copy_capped(response: object, output: object, digest: object) -> int:
    """Copy RESPONSE into OUTPUT and DIGEST in bounded reads under MAX_DOWNLOAD_BYTES; return the size."""
    size = 0
    for _ in range(MAX_READS):
        chunk = response.read(CHUNK_BYTES)
        if not chunk:
            return size
        size += len(chunk)
        if size > MAX_DOWNLOAD_BYTES:
            raise ScanError(f"Opengrep download passed {MAX_DOWNLOAD_BYTES} bytes")
        digest.update(chunk)
        output.write(chunk)
    raise ScanError(f"Opengrep download did not end within {MAX_READS} reads")


def fetch_into(url: str, destination: Path, opener: Callable[..., object], outcome: dict[str, object]) -> None:
    """Download URL to DESTINATION; record the sha256 and size, or the error, in OUTCOME."""
    digest = hashlib.sha256()
    try:
        with opener(url, timeout=DOWNLOAD_TIMEOUT_SECONDS) as response, destination.open("wb") as output:
            outcome["size"] = copy_capped(response, output, digest)
        outcome["sha256"] = digest.hexdigest()
    except (OSError, ScanError) as err:
        outcome["error"] = f"Opengrep download failed: {err}"


def fetch_within(url: str, destination: Path, deadline: float, opener: Callable[..., object]) -> tuple[str, int]:
    """Download URL in a worker thread and give up once DEADLINE seconds pass, however the socket behaves.

    A server that trickles bytes just inside the socket timeout cannot hold the scan past DEADLINE;
    the abandoned daemon thread ends with the process.
    """
    outcome: dict[str, object] = {}
    worker = threading.Thread(target=fetch_into, args=(url, destination, opener, outcome), name="opengrep-download", daemon=True)
    worker.start()
    worker.join(deadline)
    if worker.is_alive():
        raise ScanError(f"Opengrep download did not finish within {deadline} s")
    if "error" in outcome:
        raise ScanError(str(outcome["error"]))
    return str(outcome["sha256"]), int(outcome["size"])


def download_binary(destination: Path, versions: dict[str, str]) -> None:
    """Download the pinned Linux x64 binary under a byte cap and one deadline; refuse a digest mismatch."""
    if platform.system() != "Linux" or platform.machine() not in {"x86_64", "AMD64"}:
        raise ScanError("the checked-in Opengrep pin supports Linux x64 only")
    opener = urllib.request.urlopen  # nosec B310  # https URL checked against the release prefix in tool_versions
    sha256, size = fetch_within(versions["OPENGREP_URL"], destination, DOWNLOAD_DEADLINE_SECONDS, opener)
    if sha256 != versions["OPENGREP_SHA256"]:
        raise ScanError(f"Opengrep sha256 mismatch: got {sha256} ({size} bytes)")
    destination.chmod(0o755)


@contextmanager
def codacy_home(versions: dict[str, str]) -> Iterator[Path]:
    """Yield a temporary CODACY_GLOBAL_DIR holding the verified Opengrep binary."""
    with tempfile.TemporaryDirectory(prefix="snatcharr-opengrep-") as temporary:
        home = Path(temporary) / "global"
        (home / "tools/Semgrep").mkdir(parents=True)
        download_binary(home / "tools/Semgrep/opengrep", versions)
        yield home


def cli_path(root: Path) -> Path:
    """Return the installed CLI after checking it is the exact version package.json pins."""
    package = root / PACKAGE
    wanted = load_json(package / "package.json")
    pin = wanted.get("dependencies", {}).get(CLI_PACKAGE) if isinstance(wanted, dict) else None
    if not isinstance(pin, str) or not VERSION_RE.fullmatch(pin):
        raise ScanError(f"{PACKAGE}/package.json: {CLI_PACKAGE} needs an exact x.y.z pin")
    installed_file = package / "node_modules" / CLI_PACKAGE / "package.json"
    cli = package / "node_modules/.bin/codacy-analysis"
    if not installed_file.is_file() or not cli.is_file():
        raise ScanError(f"{CLI_PACKAGE} not installed: run npm ci --ignore-scripts in {PACKAGE}")
    installed = load_json(installed_file)
    if not isinstance(installed, dict) or installed.get("version") != pin:
        raise ScanError(f"{CLI_PACKAGE} {installed.get('version') if isinstance(installed, dict) else '?'} installed, the pin is {pin}")
    return cli


def remove_generated_config(cwd: Path) -> None:
    """Remove the ignored rule file the CLI writes for every scan, and its empty parents."""
    generated = cwd / ".codacy/generated/Semgrep/opengrep-rules.yaml"
    generated.unlink(missing_ok=True)
    for directory in (generated.parent, generated.parent.parent, generated.parent.parent.parent):
        try:
            directory.rmdir()
        except OSError:
            return


def live_report(root: Path, cwd: Path, home: Path, files: list[str]) -> dict[str, object]:
    """Run the pinned CLI in CWD over FILES with the verified Opengrep under HOME; return its report."""
    cli = cli_path(root)
    with tempfile.TemporaryDirectory(prefix="snatcharr-opengrep-report-") as temporary:
        report = Path(temporary) / "report.json"
        command = [str(cli), "analyze", ".", "--tool", "Semgrep", "--config-file", str(root / CONFIG), "--output-format", "json", "--output", str(report), "--files", *files, "--fail-if-missing", "--parallel-tools", "1", "--tool-timeout", str(TOOL_TIMEOUT_MS), "--no-log", "--no-update-notifier"]
        try:
            result = run_capture(command, cwd, CLI_TIMEOUT_SECONDS, dict(os.environ, CODACY_GLOBAL_DIR=str(home)))
        finally:
            remove_generated_config(cwd)
        if result.returncode not in (0, 1) or not report.is_file():
            raise ScanError(f"Codacy CLI failed (exit {result.returncode}): {result.stderr[-2000:]}")
        data = load_json(report)
    if not isinstance(data, dict):
        raise ScanError("Codacy CLI report is not a JSON object")
    metadata = data.get("metadata")
    if isinstance(metadata, dict):
        metadata.pop("repositoryRoot", None)  # a local path; recorded fixtures stay host-neutral
    return data


def canary_report(root: Path, home: Path) -> dict[str, object]:
    """Scan the planted canary source at its real path in a scratch repository (the CLI needs one)."""
    with tempfile.TemporaryDirectory(prefix="snatcharr-opengrep-canary-") as temporary:
        scratch = Path(temporary)
        target = scratch / CANARY_FILE
        target.parent.mkdir(parents=True)
        target.write_text(canary_source(root), encoding="utf-8")
        for args in (("init", "-q"), ("add", "--", CANARY_FILE)):
            result = git(scratch, *args)
            if result.returncode != 0:
                raise ScanError(f"canary: git {args[0]} failed: {result.stderr.strip()}")
        return live_report(root, scratch, home, [CANARY_FILE])


def save_report(directory: Path | None, name: str, report: dict[str, object]) -> None:
    """Write REPORT as DIRECTORY/NAME when --report-out names a directory."""
    if directory is not None:
        directory.mkdir(parents=True, exist_ok=True)
        (directory / name).write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def canary_check(root: Path, home: Path, baseline: Counter[str], version: str, report_out: Path | None) -> list[str]:
    """Replay the planted 58af3cb4 defect live and return why it was not exactly one new finding."""
    try:
        canary = canary_report(root, home)
    except ScanError as err:  # the tree scan still runs: a planted tree also breaks the canary derivation
        return [str(err)]
    save_report(report_out, "canary-report.json", canary)
    return canary_problems(canary, baseline, version)


def scan(root: Path, today: date, report_out: Path | None) -> list[str]:
    """Run the canary first, then scan every tracked Python file; return the failures."""
    versions = tool_versions(root)
    version = versions["OPENGREP_VERSION"]
    baseline = baseline_counts(load_baseline(root, today))
    targets = tracked_files(root)
    with codacy_home(versions) as home:
        problems = canary_check(root, home, baseline, version, report_out)
        report = live_report(root, root, home, targets)
        save_report(report_out, "tree-report.json", report)
    problems += report_problems(report, targets, baseline, version)
    if not problems:
        print(f"codacy-opengrep: canary: the 58af3cb4 defect is one new {CANARY_RULE.rsplit('.', 1)[-1]} finding; {len(targets)} files, 0 new, 0 stale")
    return problems


def with_capability(report: object, **fields: str) -> object:
    """Return a copy of REPORT whose Semgrep capability carries FIELDS."""
    copy = json.loads(json.dumps(report))
    copy["capability"]["ready"][0].update(fields)
    return copy


def with_tool_error(report: object) -> object:
    """Return a copy of REPORT with a tool error and its file count unchanged."""
    copy = json.loads(json.dumps(report))
    copy["errors"] = [{"kind": "tool failure"}]
    return copy


def with_repeat(report: object) -> object:
    """Return a copy of REPORT whose first issue occurs a second time, as at another line."""
    copy = json.loads(json.dumps(report))
    copy["issues"].append(dict(copy["issues"][0]))
    return copy


def without_rule(report: object, rule: str) -> object:
    """Return a copy of REPORT without the issues of RULE."""
    copy = json.loads(json.dumps(report))
    copy["issues"] = [issue for issue in copy["issues"] if issue.get("patternId") != rule]
    return copy


def refused_with(problems: list[str], prefix: str) -> bool:
    """Return True when PROBLEMS is exactly one failure starting with PREFIX."""
    return len(problems) == 1 and problems[0].startswith(prefix)


def raises(call: Callable[[], object]) -> bool:
    """Return True when CALL raises a scanner error."""
    try:
        call()
    except ScanError:
        return True
    return False


def report_cases(root: Path, baseline: Counter[str], version: str) -> list[tuple[str, bool]]:
    """Return (name, held) for the recorded-report cases, the planted finding first."""
    main = load_json(root / FIXTURES / "main-report.json")
    planted = load_json(root / FIXTURES / "planted-report.json")
    files = fixture_files(main)
    stale = baseline + Counter({"demo\0missing.py\0" + "0" * 64: 1})
    return [
        ("planted finding: exactly one new canary finding", not canary_problems(planted, baseline, version)),
        ("planted finding gone: canary refused", bool(canary_problems(without_rule(planted, CANARY_RULE), baseline, version))),
        ("planted finding with an empty baseline: canary refused", bool(canary_problems(planted, Counter(), version))),
        ("main baseline: 0 new, 0 stale", not report_problems(main, files, baseline, version)),
        ("second occurrence of a baselined finding is new", refused_with(report_problems(with_repeat(main), files, baseline, version), "new finding: ")),
        ("stale entry refused", refused_with(report_problems(main, files, stale, version), "stale baseline entry: demo")),
        ("tool error refused", refused_with(report_problems(with_tool_error(main), files, baseline, version), "tool error: ")),
        ("local rule file refused", refused_with(report_problems(with_capability(main, configSource="local"), files, baseline, version), "rules came from configSource 'local'")),
        ("other Opengrep version refused", refused_with(report_problems(with_capability(main, version="0.0.0"), files, baseline, version), "Opengrep 0.0.0 ran")),
    ]


def same_key(left: str, right: str) -> bool:
    """Return True when two source texts of one rule and path share a fingerprint."""
    return fingerprint({"patternId": "p", "filePath": "a.py", "lineContent": left}) == fingerprint({"patternId": "p", "filePath": "a.py", "lineContent": right})


def fingerprint_cases() -> list[tuple[str, bool]]:
    """Return (name, held) for the source-text normalisation cases."""
    return [
        ("indentation and spacing outside literals keep the key", same_key("    run(cmd,   shell=True)", "run(cmd, shell=True)")),
        ("whitespace inside a string literal changes the key", not same_key('run("a  b")', 'run("a b")')),
        ("whitespace inside a triple-quoted literal changes the key", not same_key('x = """a\n  b"""', 'x = """a\n b"""')),
        ("an escaped quote keeps the literal open", not same_key('run("a\\"  b")', 'run("a\\" b")')),
        ("oversized source text refused", raises(lambda: same_key("x" * (MAX_CONTENT_CHARS + 1), "x"))),
    ]


def guard_cases(scratch: Path) -> list[tuple[str, bool]]:
    """Return (name, held) for the git environment, base ref and canary read guards."""
    return [
        ("GIT_* variables dropped for git", git_env({"GIT_DIR": "/elsewhere", "GIT_INDEX_FILE": "/elsewhere/index", "PATH": "/bin"}) == {"PATH": "/bin"}),
        ("option-like base ref refused", ref_problem("--output=/tmp/x") is not None),
        ("range base ref refused", ref_problem("main..HEAD") is not None),
        ("branch, remote branch, commit and HEAD~1 accepted", all(ref_problem(ref) is None for ref in ("main", "origin/main", "3506378db2f78c26f54bd8dd5d8f73a6a59bd411", "HEAD~1"))),
        ("unreadable canary file is a scan error", raises(lambda: canary_source(scratch))),
        ("failed canary derivation is returned, so the tree scan still runs", refused_with(canary_check(scratch, scratch, Counter(), "0.0.0", None), "canary: cannot read ")),
        ("baseline at the entry cap read", len(baseline_findings({"version": BASELINE_VERSION, "findings": [{}] * MAX_BASELINE_ENTRIES})) == MAX_BASELINE_ENTRIES),
        ("baseline over the entry cap refused", raises(lambda: baseline_findings({"version": BASELINE_VERSION, "findings": [{}] * (MAX_BASELINE_ENTRIES + 1)}))),
    ]


class FakeResponse:
    """A context-managed HTTP response stand-in: CHUNKS one-byte reads, each after DELAY seconds."""

    def __init__(self, chunks: int, delay: float) -> None:
        """Serve CHUNKS reads of b"x", DELAY seconds apart, until stopped."""
        self.left, self.delay, self.stop = chunks, delay, threading.Event()

    def __enter__(self) -> Self:
        """Return the response itself."""
        return self

    def __exit__(self, *_exc: object) -> None:
        """Stop serving."""
        self.stop.set()

    def read(self, _size: int) -> bytes:
        """Return b"x" after the delay, or b"" once exhausted or stopped."""
        if self.left <= 0 or self.stop.wait(self.delay):
            return b""
        self.left -= 1
        return b"x"


def download_cases(scratch: Path) -> list[tuple[str, bool]]:
    """Return (name, held) for the total download deadline, against a quick and a trickling server."""
    quick, slow = FakeResponse(3, 0.0), FakeResponse(300, 0.01)
    fast = fetch_within("https://example.invalid/quick", scratch / "quick", 5.0, lambda *_args, **_kwargs: quick)
    try:
        fetch_within("https://example.invalid/slow", scratch / "slow", 0.2, lambda *_args, **_kwargs: slow)
        stopped = False
    except ScanError as err:
        stopped = "did not finish within 0.2 s" in str(err)
    finally:
        slow.stop.set()
    return [
        ("download within its deadline accepted", fast == (hashlib.sha256(b"xxx").hexdigest(), 3)),
        ("download trickling past its deadline refused", stopped),
    ]


def entry_cases(valid: dict[str, str], configured: set[str]) -> list[tuple[str, bool]]:
    """Return (name, held) for the baseline entry shape and expiry cases, from the valid category D entry VALID."""
    variants = {
        "extra key refused": dict(valid, line="57"),
        "missing key refused": {key: value for key, value in valid.items() if key != "reason"},
        "category D a day long refused": dict(valid, expires="2027-03-13"),
        "category A without interval refused": dict(valid, category="A", expires="2026-11-12"),
        "impossible date refused": dict(valid, expires="2027-02-30"),
        "unknown pattern refused": dict(valid, patternId="Semgrep_unknown.rule"),
        "parent path refused": dict(valid, filePath="../outside.py"),
        "category E refused": dict(valid, category="E"),
        "short reason refused": dict(valid, reason="fixed argv"),
        "count 0 refused": dict(valid, count="0"),
        "count 100 refused": dict(valid, count="100"),
    }
    cases = [("valid category D entry", not entry_problems(valid, configured)), ("count 99 accepted", not entry_problems(dict(valid, count="99"), configured))]
    cases += [(name, bool(entry_problems(item, configured))) for name, item in variants.items()]
    cases += [
        ("older baseline may name a pattern the mirror dropped", not entry_problems(dict(valid, patternId="Semgrep_unknown.rule"), None)),
        ("entry holds on its expires day", not entry_expired(valid, date(2027, 3, 12))),
        ("entry expires the day after", entry_expired(valid, date(2027, 3, 13))),
    ]
    return cases


def sample_entry(configured: list[str]) -> dict[str, str]:
    """Return a valid category D entry for the first configured pattern."""
    return {
        "patternId": configured[0], "filePath": "tools/ci/example.py", "contentSha256": "0" * 64, "count": "1",
        "category": "D", "entered": "2026-11-12", "expires": "2027-03-12", "issue": "1098",
        "reason": "synthetic entry; the self-test changes one field per case",
    }


def shrink_cases(entries: dict[str, dict[str, str]], sample: dict[str, str]) -> list[tuple[str, bool]]:
    """Return (name, held) for the shrink-only comparison cases; an empty ENTRIES uses SAMPLE."""
    entries = entries or {entry_key(sample): sample}
    first = next(iter(entries))
    old = entries[first]
    smaller = {key: value for key, value in entries.items() if key != first}

    def changed(**fields: str) -> dict[str, dict[str, str]]:
        """Return ENTRIES with FIELDS replaced in the first entry."""
        return {**entries, first: dict(old, **fields)}

    return [
        ("unchanged baseline accepted", not compare_baselines(entries, entries)),
        ("removed entry accepted", not compare_baselines(entries, smaller)),
        ("added entry refused", bool(compare_baselines(smaller, entries))),
        ("later expiry refused", bool(compare_baselines(entries, changed(expires="2099-01-01")))),
        ("changed category refused", bool(compare_baselines(entries, changed(category="C" if old["category"] != "C" else "B")))),
        ("changed issue refused", bool(compare_baselines(entries, changed(issue=str(int(old["issue"]) + 1))))),
        ("changed reason refused", bool(compare_baselines(entries, changed(reason=old["reason"] + " (edited)")))),
        ("higher count refused", bool(compare_baselines(entries, changed(count=str(int(old["count"]) + 1))))),
        ("lower count accepted", not compare_baselines(changed(count=str(int(old["count"]) + 1)), entries)),
    ]


def run_self_test(root: Path, today: date) -> int:
    """Replay recorded reports and planted baseline cases; return 1 when any case does not hold."""
    version = tool_versions(root)["OPENGREP_VERSION"]
    entries = load_baseline(root, today)
    sample = sample_entry(config_ids(root))
    with tempfile.TemporaryDirectory(prefix="snatcharr-opengrep-selftest-") as temporary:
        scratch = Path(temporary)
        cases = report_cases(root, baseline_counts(entries), version) + fingerprint_cases() + guard_cases(scratch) + download_cases(scratch)
    cases += entry_cases(sample, set(config_ids(root))) + shrink_cases(entries, sample)
    cases.append(("empty baseline: the shrink cases still run", all(held for _name, held in shrink_cases({}, sample))))
    failed = [name for name, held in cases if not held]
    for name, held in cases:
        print(f"{'ok  ' if held else 'FAIL'} {name}", file=sys.stdout if held else sys.stderr)
    return 1 if failed else 0


def check_drift(root: Path, listing: Path) -> None:
    """Reject any listing hash, count, or ID-set drift from the mirror config."""
    raw = listing.read_bytes()
    listed = raw.decode("utf-8").splitlines()
    configured = config_ids(root)
    data = load_json(root / CONFIG)
    metadata = data.get("metadata", {}) if isinstance(data, dict) else {}
    if metadata.get("listingSha256") != hashlib.sha256(raw).hexdigest() or metadata.get("listingCount") != len(listed):
        raise ScanError("listing metadata hash or count differs")
    added, removed = sorted(set(listed) - set(configured)), sorted(set(configured) - set(listed))
    if listed != sorted(listed) or len(listed) != len(set(listed)) or added or removed:
        raise ScanError(f"listing drift: added={added}, removed={removed}")


def parse_today(text: str) -> date:
    """Parse --today as YYYY-MM-DD."""
    parsed = parse_date(text)
    if parsed is None:
        raise argparse.ArgumentTypeError(f"not a YYYY-MM-DD date: {text}")
    return parsed


def parse_args(argv: list[str]) -> argparse.Namespace:
    """Parse one mode plus the shared --today and --report-out options."""
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--self-test", action="store_true", help="replay recorded reports and planted cases")
    mode.add_argument("--drift", type=Path, metavar="LISTING", help="compare a pattern listing with the config")
    mode.add_argument("--base-ref", metavar="REF", help="refuse a baseline that is no shrink of the one at REF")
    parser.add_argument("--today", type=parse_today, default=datetime.now(UTC).date(), help="date for the expiry check (default: today in UTC)")
    parser.add_argument("--report-out", type=Path, metavar="DIR", help="write the live reports here")
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    """Run the selected mode and print every failure."""
    args = parse_args(argv)
    try:
        if args.self_test:
            return run_self_test(ROOT, args.today)
        if args.drift:
            check_drift(ROOT, args.drift)
            print("Opengrep listing: exact match")
            return 0
        if args.base_ref:
            problems = shrink_problems(ROOT, args.base_ref, args.today)
        else:
            problems = scan(ROOT, args.today, args.report_out)
    except ScanError as err:
        problems = [str(err)]
    for problem in problems:
        print(f"codacy-opengrep: {problem}", file=sys.stderr)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
