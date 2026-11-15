#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Mirror Codacy's two enabled Lizard 1.24.0 patterns over tracked source files.

The baseline is a dated, shrink-only record. A missing or larger measured finding,
expired exception, or stale record fails. --base-ref compares the baseline with a
commit's baseline; --self-test replays inclusive threshold boundaries offline.
"""

from __future__ import annotations

import argparse
import fnmatch
import json
import re
import subprocess  # nosec B404  # fixed git argv, no shell or network
import sys
from datetime import UTC, date, datetime, timedelta
from importlib.metadata import version
from pathlib import Path

import lizard
from lizard_languages import get_reader_for

ROOT = Path(__file__).resolve().parents[2]
BASELINE = "tools/ci/codacy_lizard/baseline.json"
FILE_PATTERN = "Lizard_file-nloc-medium"
PARAM_PATTERN = "Lizard_parameter-count-medium"
LIMITS = {FILE_PATTERN: 1000, PARAM_PATTERN: 12}
DAYS = {"B": 3, "C": 10, "D": 120}
FIELDS = {"patternId", "filePath", "functionName", "value", "entered", "expires", "category", "issue", "reason"}
REF_RE = re.compile(r"[A-Za-z0-9][A-Za-z0-9._/~^-]{0,199}")


class CheckError(Exception):
    """Refuse a scan or baseline whose result cannot be trusted."""


def git(*args: str) -> bytes:
    """Run bounded, local-only git commands at the repository root."""
    try:
        result = subprocess.run(["git", *args], cwd=ROOT, capture_output=True, timeout=30, check=False)  # nosec B603  # fixed git subcommands; no shell
    except (OSError, subprocess.TimeoutExpired) as err:
        raise CheckError(f"git {args[0]} failed: {err}") from err
    if result.returncode:
        raise CheckError(f"git {args[0]} failed: {result.stderr.decode(errors='replace').strip()}")
    return result.stdout


def exclusions() -> list[str]:
    """Read the two exact exclude_paths lists from the checked-in Codacy YAML subset."""
    text = (ROOT / ".codacy.yml").read_text(encoding="utf-8")
    patterns = []
    for heading, indent in ((r"^exclude_paths:$", 2), (r"^  lizard:\n    exclude_paths:$", 6)):
        block = re.search(heading + rf"\n((?: {{{indent}}}- \"[^\"\n]+\"\n)+)", text, re.MULTILINE)
        if block is None:
            raise CheckError(".codacy.yml: expected global and Lizard exclude_paths as quoted lists")
        for line in block.group(1).splitlines():
            value = json.loads(line.strip()[2:])
            if not isinstance(value, str) or not value or value.startswith("/") or ".." in value.split("/"):
                raise CheckError(f".codacy.yml: invalid exclude path {value!r}")
            patterns.append(value)
    return patterns


def selected_files() -> list[str]:
    """Select every tracked regular source file supported by this Lizard version."""
    patterns = exclusions()
    names = [name.decode("utf-8") for name in git("ls-files", "-z").split(b"\0") if name]
    selected = []
    for name in names:
        if name.startswith("/") or ".." in name.split("/"):
            raise CheckError(f"unsafe tracked path: {name!r}")
        if get_reader_for(name) is None or any(fnmatch.fnmatchcase(name, pattern) for pattern in patterns):
            continue
        if not (ROOT / name).is_file():
            raise CheckError(f"tracked source missing: {name}")
        selected.append(name)
    if not selected:
        raise CheckError("no tracked Lizard source files selected")
    return sorted(selected)


def source_findings(name: str, source: str) -> list[dict[str, object]]:
    """Read file NLOC and function parameter counts from Lizard, inclusively."""
    info = lizard.analyze_file.analyze_source_code(name, source)
    findings = []
    if info.nloc >= LIMITS[FILE_PATTERN]:
        findings.append({"patternId": FILE_PATTERN, "filePath": name, "functionName": "", "value": info.nloc})
    for function in info.function_list:
        if function.parameter_count >= LIMITS[PARAM_PATTERN]:
            findings.append({"patternId": PARAM_PATTERN, "filePath": name, "functionName": function.name, "value": function.parameter_count})
    return findings


def key(item: dict[str, object]) -> tuple[str, str, str]:
    """Key a finding without its measured value or source line."""
    return str(item["patternId"]), str(item["filePath"]), str(item["functionName"])


def measure() -> tuple[dict[tuple[str, str, str], dict[str, object]], int]:
    """Analyze selected files, refusing duplicate function identities."""
    files = selected_files()
    found = {}
    for name in files:
        try:
            source = (ROOT / name).read_text(encoding="utf-8")
            items = source_findings(name, source)
        except (OSError, UnicodeError, IndexError, RecursionError) as err:
            raise CheckError(f"cannot analyze {name}: {err}") from err
        for item in items:
            identity = key(item)
            if identity in found:
                raise CheckError(f"duplicate Lizard function identity: {identity}")
            found[identity] = item
    return found, len(files)


def parse_baseline(raw: object, today: date | None) -> dict[tuple[str, str, str], dict[str, object]]:
    """Validate dated MEL entries, their keys, and their expiry."""
    if not isinstance(raw, dict) or set(raw) != {"version", "findings"} or raw["version"] != 1 or not isinstance(raw["findings"], list):
        raise CheckError('baseline must be {"version": 1, "findings": [...]}')
    entries = {}
    for item in raw["findings"]:
        if not isinstance(item, dict) or set(item) != FIELDS:
            raise CheckError(f"baseline entry has invalid fields: {item!r}")
        if not all(isinstance(item[field], str) for field in ("patternId", "filePath", "functionName")):
            raise CheckError(f"baseline entry has invalid identity: {item!r}")
        pattern, path, function = key(item)
        value = item["value"]
        if pattern not in LIMITS or not isinstance(value, int) or isinstance(value, bool) or value < LIMITS[pattern]:
            raise CheckError(f"baseline entry has invalid pattern or value: {pattern} {value!r}")
        if not isinstance(path, str) or not path or path.startswith("/") or ".." in path.split("/") or not isinstance(function, str):
            raise CheckError(f"baseline entry has invalid path or function: {path!r} {function!r}")
        if (pattern == FILE_PATTERN and function) or (pattern == PARAM_PATTERN and not function):
            raise CheckError(f"baseline entry has invalid function for {pattern}: {function!r}")
        if item["category"] not in ("A", "B", "C", "D") or not all(isinstance(item[field], str) and item[field] for field in ("issue", "reason")):
            raise CheckError(f"baseline entry lacks MEL category, issue or reason: {path}")
        try:
            entered, expires = date.fromisoformat(item["entered"]), date.fromisoformat(item["expires"])
        except (TypeError, ValueError) as err:
            raise CheckError(f"baseline entry has invalid dates: {path}") from err
        days = DAYS.get(item["category"])
        if expires <= entered or (days is not None and expires != entered + timedelta(days=days)):
            raise CheckError(f"baseline entry has invalid {item['category']} expiry: {path}")
        if today is not None and expires < today:
            raise CheckError(f"baseline entry expired {expires}: {path} ({item['issue']})")
        if key(item) in entries:
            raise CheckError(f"duplicate baseline entry: {key(item)}")
        entries[key(item)] = item
    return entries


def load_baseline(path: Path, today: date | None) -> dict[tuple[str, str, str], dict[str, object]]:
    """Load a JSON baseline with a clear path diagnostic."""
    try:
        return parse_baseline(json.loads(path.read_text(encoding="utf-8")), today)
    except (OSError, UnicodeError, json.JSONDecodeError) as err:
        raise CheckError(f"cannot read {path}: {err}") from err


def compare_scan(measured: dict, baseline: dict) -> list[str]:
    """Return new, grown, and stale findings."""
    problems = []
    for identity, item in sorted(measured.items()):
        recorded = baseline.get(identity)
        if recorded is None:
            problems.append(f"new {identity[0]}: {identity[1]}{':' + identity[2] if identity[2] else ''} = {item['value']}")
        elif item["value"] > recorded["value"]:
            problems.append(f"grew {identity[0]}: {identity[1]}{':' + identity[2] if identity[2] else ''} {recorded['value']} -> {item['value']}")
    for identity in sorted(baseline.keys() - measured.keys()):
        problems.append(f"stale {identity[0]}: {identity[1]}{':' + identity[2] if identity[2] else ''}")
    return problems


def compare_base(current: dict, previous: dict) -> list[str]:
    """Refuse baseline additions, higher values, or renewed kept entries."""
    problems = []
    for identity, item in sorted(current.items()):
        old = previous.get(identity)
        if old is None:
            problems.append(f"baseline grew: new {identity}")
            continue
        if item["value"] > old["value"]:
            problems.append(f"baseline grew: {identity} {old['value']} -> {item['value']}")
        if item["expires"] > old["expires"]:
            problems.append(f"baseline expiry moved: {identity}")
        for field in ("entered", "category", "issue", "reason"):
            if item[field] != old[field]:
                problems.append(f"baseline {field} changed: {identity}")
    return problems


def base_ref_problems(ref: str, current: dict) -> list[str]:
    """Compare with the baseline blob at a verified local commit."""
    if REF_RE.fullmatch(ref) is None or ".." in ref:
        raise CheckError(f"invalid --base-ref {ref!r}")
    git("rev-parse", "--verify", f"{ref}^{{commit}}")
    if not git("ls-tree", "-z", ref, "--", BASELINE):
        print(f"codacy-lizard: first baseline at {ref}; no prior entries")
        return []
    try:
        raw = git("show", f"{ref}:{BASELINE}")
    except CheckError as err:
        raise CheckError(f"cannot read {BASELINE} at {ref}: {err}") from err
    try:
        previous = parse_baseline(json.loads(raw), None)
    except (json.JSONDecodeError, CheckError) as err:
        raise CheckError(f"{BASELINE} at {ref}: {err}") from err
    return compare_base(current, previous)


def self_test() -> None:
    """Replay inclusive boundaries and a planted unbaselined failure."""
    cases = (
        ("1001 lines", "package planted\n" + "var x = 1\n" * 1000, FILE_PATTERN, 1),
        ("999 lines", "package planted\n" + "var x = 1\n" * 998, FILE_PATTERN, 0),
        ("12 parameters", "package planted\nfunc f(" + ", ".join(f"p{i} int" for i in range(12)) + ") {}\n", PARAM_PATTERN, 1),
        ("11 parameters", "package planted\nfunc f(" + ", ".join(f"p{i} int" for i in range(11)) + ") {}\n", PARAM_PATTERN, 0),
    )
    for label, source, pattern, want in cases:
        measured = {key(item): item for item in source_findings("planted.go", source)}
        actual = sum(identity[0] == pattern for identity in measured)
        new = sum(problem.startswith("new ") for problem in compare_scan(measured, {}))
        if actual != want or new != want or len(measured) != want:
            raise CheckError(f"self-test {label}: want {want} new {pattern}, got {measured}")
        print(f"ok   {label}: {want} new finding")
    planted = {"patternId": FILE_PATTERN, "filePath": "planted.go", "functionName": "", "value": 1001}
    identity = key(planted)
    recorded = {**planted, "value": 1000, "entered": "2026-11-15", "expires": "2027-03-15", "category": "D", "issue": "planted", "reason": "self-test"}
    baseline = parse_baseline({"version": 1, "findings": [recorded]}, date(2026, 11, 15))
    if not compare_scan({identity: planted}, baseline)[0].startswith("grew "):
        raise CheckError("self-test: measured growth was accepted")
    if not compare_scan({}, baseline)[0].startswith("stale "):
        raise CheckError("self-test: stale baseline entry was accepted")
    if not compare_base(baseline, {}):
        raise CheckError("self-test: baseline addition was accepted")
    try:
        parse_baseline({"version": 1, "findings": [recorded]}, date(2027, 3, 16))
    except CheckError:
        pass
    else:
        raise CheckError("self-test: expired baseline entry was accepted")
    print("ok   grown, stale, added and expired baseline entries refused")


def main() -> int:
    """Check the baseline and measured worktree, or replay the planted cases."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--base-ref")
    parser.add_argument("--baseline", type=Path, default=ROOT / BASELINE)
    args = parser.parse_args()
    try:
        if version("lizard") != "1.24.0":
            raise CheckError(f"Lizard {version('lizard')} installed; want 1.24.0")
        if args.self_test:
            self_test()
            return 0
        baseline = load_baseline(args.baseline, datetime.now(UTC).date())
        if args.base_ref:
            problems = base_ref_problems(args.base_ref, baseline)
        else:
            measured, count = measure()
            problems = compare_scan(measured, baseline)
            if not problems:
                print(f"codacy-lizard: {count} tracked source files, {len(measured)} recorded findings, 0 new/grown/stale")
        for problem in problems:
            print(f"codacy-lizard: {problem}", file=sys.stderr)
        return 1 if problems else 0
    except CheckError as err:
        print(f"codacy-lizard: {err}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
