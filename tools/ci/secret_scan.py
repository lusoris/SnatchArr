"""Gate Gitleaks findings on fingerprint-scoped and path+rule exceptions that expire.

Copied from tekmora/discord-bot tools/ci/secret_scan.py (EUPL-1.2, same author)
at commit 70642249adc9d99375b707cebe0eb756970905cc, which copied it from
jellysin/plugin-lastfm tools/secret_scan.py at commit
e5b0c49be03ed4bfe397dc5c78ede1233271204a and made excepted() report a scanner
that misses the planted key as a ScanError. Changed here: support exact path:rule exceptions alongside commit:path:rule:line fingerprints.
"""

import argparse
import base64
import datetime
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

EXCEPTIONS = ".gitleaksignore"
MAX_EXCEPTIONS = 16
MAX_EXCEPTION_BYTES = 16 * 1024
MAX_REPORT_BYTES = 16 * 1024 * 1024
LEAK_EXIT = 3
SCAN_TIMEOUT_SECONDS = 300
WILDCARDS = set("*?[]{}")
EXPIRY = re.compile(r"# expires (\d{4}-\d{2}-\d{2}): \S.*")
FINGERPRINT = re.compile(r"[0-9a-f]{40}:[^:*?\s]+:[a-z0-9-]+:[1-9][0-9]*")
PATH_RULE = re.compile(r"([^:\s]+):([a-z0-9-]+)")
FINDING = re.compile(r"^([0-9a-f]{40}):([^:*?\s]+):([a-z0-9-]+):([1-9][0-9]*)$")


class ScanError(ValueError):
    """The exception list is malformed or the scanner did not complete."""


@dataclass(frozen=True)
class Exemption:
    target: str
    expires: datetime.date
    path: str | None = None
    rule: str | None = None

    def active(self, today):
        return today < self.expires

    def matches(self, finding_fingerprint, finding_path, finding_rule):
        if self.path is not None:
            return finding_path == self.path and finding_rule == self.rule
        return finding_fingerprint == self.target


def parse_exceptions(text):
    """Read `# expires YYYY-MM-DD: reason` lines, each followed by one exception."""
    entries, expires = [], None
    for number, raw in enumerate(text.splitlines(), 1):
        line = raw.strip()
        if not line:
            continue
        if line.startswith("#"):
            match = EXPIRY.fullmatch(line)
            if match:
                if expires:
                    raise ScanError(f"line {number}: expiry without an exception before it")
                expires = parse_date(match.group(1), number)
            continue
        if not expires:
            raise ScanError(f"line {number}: exception without an expiry and reason")
        if FINGERPRINT.fullmatch(line):
            entries.append(Exemption(line, expires))
            expires = None
            continue
        if any(char in WILDCARDS for char in line):
            raise ScanError(f"line {number}: a path:rule exception names one exact path, no wildcards")
        match = PATH_RULE.fullmatch(line)
        if not match:
            raise ScanError(f"line {number}: not a commit:path:rule:line or path:rule exception")
        path, rule = match.groups()
        if path.startswith("/") or Path(path).is_absolute() or any(part == ".." for part in path.replace("\\", "/").split("/")):
            raise ScanError(f"line {number}: path must be relative and not contain .. segments")
        entries.append(Exemption(line, expires, path=path, rule=rule))
        expires = None
    if expires:
        raise ScanError("an expiry at the end names no exception")
    if len(entries) > MAX_EXCEPTIONS or len({entry.target for entry in entries}) != len(entries):
        raise ScanError("exceptions are duplicated or exceed the supported count")
    return entries


def parse_date(value, number):
    try:
        return datetime.date.fromisoformat(value)
    except ValueError as error:
        raise ScanError(f"line {number}: invalid expiry date") from error


def load_exceptions(repository):
    path = Path(repository) / EXCEPTIONS
    if not path.is_file():
        return []
    if path.stat().st_size > MAX_EXCEPTION_BYTES:
        raise ScanError("the exception list is too large")
    return parse_exceptions(path.read_text(encoding="utf-8"))


def parse_finding(fingerprint):
    match = FINDING.match(fingerprint)
    if not match:
        raise ScanError(f"unparseable Gitleaks finding fingerprint: {fingerprint}")
    commit, path, rule, line = match.groups()
    return commit, path, rule, line


def findings(gitleaks, repository):
    """Return every fingerprint Gitleaks reports with no exception applied."""
    located = subprocess.run(["git", "rev-parse", "--absolute-git-dir"], cwd=repository, capture_output=True, text=True, timeout=60, check=True)
    with tempfile.TemporaryDirectory() as scratch:
        # Gitleaks applies a .gitleaksignore in its source directory without regard to expiry,
        # so it reads the history through GIT_DIR from a directory that holds no list.
        source = Path(scratch) / "source"
        source.mkdir()
        report = Path(scratch) / "report.json"
        command = [
            str(Path(gitleaks).resolve()), "git", "--redact", "--no-banner", "--exit-code", str(LEAK_EXIT),
            "--gitleaks-ignore-path", str(source), "--report-format", "json", "--report-path", str(report), str(source),
        ]
        environment = {**os.environ, "GIT_DIR": located.stdout.strip()}
        result = subprocess.run(command, cwd=source, env=environment, capture_output=True, text=True, timeout=SCAN_TIMEOUT_SECONDS, check=False)
        if result.returncode not in (0, LEAK_EXIT):
            raise ScanError(f"Gitleaks did not complete (exit {result.returncode}): {result.stderr.strip()[-400:]}")
        if not report.is_file() or report.stat().st_size > MAX_REPORT_BYTES:
            raise ScanError("Gitleaks wrote no usable report")
        found = [item["Fingerprint"] for item in json.loads(report.read_text(encoding="utf-8"))]
    if bool(found) != (result.returncode == LEAK_EXIT):
        raise ScanError("the Gitleaks exit status contradicts its report")
    return found


def scan(gitleaks, repository, today):
    """Return (allowed, blocking, unused) fingerprints for one repository."""
    entries = load_exceptions(repository)
    found = findings(gitleaks, repository)
    parsed = []
    for item in found:
        _, path, rule, _ = parse_finding(item)
        parsed.append((item, path, rule))

    active_entries = [entry for entry in entries if entry.active(today)]
    allowed = sorted(item for item, path, rule in parsed if any(entry.matches(item, path, rule) for entry in active_entries))
    blocking = sorted(item for item, path, rule in parsed if not any(entry.matches(item, path, rule) for entry in active_entries))
    unused = sorted(entry.target for entry in entries if not any(entry.matches(item, path, rule) for item, path, rule in parsed))
    return allowed, blocking, unused


def git(repository, *arguments):
    identity = ["-c", "user.name=fixture", "-c", "user.email=fixture@invalid", "-c", "commit.gpgsign=false"]
    subprocess.run(["git", *identity, *arguments], cwd=repository, capture_output=True, text=True, timeout=60, check=True)


def planted_line():
    # Derived at run time so that this file holds no key-shaped literal of its own.
    value = base64.urlsafe_b64encode(hashlib.sha256(b"JellySin planted scan fixture").digest()).decode()[:40]
    return f'api_key = "{value}"\n'


def fixture(root, name, side_branch=False):
    repository = Path(root) / name
    repository.mkdir()
    git(repository, "init", "--quiet", "--initial-branch", "main")
    (repository / "README.md").write_text("fixture\n", encoding="utf-8")
    git(repository, "add", "README.md")
    git(repository, "commit", "--quiet", "--message", "fixture")
    if name == "clean":
        return repository
    if side_branch:
        git(repository, "switch", "--quiet", "--create", "side")
    (repository / "settings.py").write_text(planted_line(), encoding="utf-8")
    git(repository, "add", "settings.py")
    git(repository, "commit", "--quiet", "--message", "planted")
    if side_branch:
        git(repository, "switch", "--quiet", "main")
    return repository


def excepted(root, gitleaks, name, expires):
    repository = fixture(root, name)
    found = findings(gitleaks, repository)
    if len(found) != 1:
        raise ScanError(f"self-test '{name}': the planted key gave {len(found)} finding(s), expected 1")
    (fingerprint,) = found
    (repository / EXCEPTIONS).write_text(f"# expires {expires.isoformat()}: fixture\n{fingerprint}\n", encoding="utf-8")
    return repository


def path_rule_excepted(root, gitleaks, name, expires, rule=None):
    repository = fixture(root, name)
    found = findings(gitleaks, repository)
    if len(found) != 1:
        raise ScanError(f"self-test '{name}': the planted key gave {len(found)} finding(s), expected 1")
    _, f_path, f_rule, _ = parse_finding(found[0])
    target_rule = rule or f_rule
    (repository / EXCEPTIONS).write_text(
        f"# expires {expires.isoformat()}: fixture\n{f_path}:{target_rule}\n",
        encoding="utf-8",
    )
    return repository


def check_other_file(root, gitleaks, today, day):
    repository = fixture(root, "pr_other")
    (repository / "other.py").write_text(planted_line(), encoding="utf-8")
    git(repository, "add", "other.py")
    git(repository, "commit", "--quiet", "--message", "planted other")
    found = findings(gitleaks, repository)
    if len(found) != 2:
        raise ScanError(f"self-test 'path+rule other file': expected 2 findings, got {len(found)}")
    rule = None
    for item in found:
        _, f_path, f_rule, _ = parse_finding(item)
        if f_path == "settings.py":
            rule = f_rule
    if not rule:
        raise ScanError("self-test 'path+rule other file': missing settings.py finding")
    (repository / EXCEPTIONS).write_text(
        f"# expires {(today + day).isoformat()}: fixture\nsettings.py:{rule}\n",
        encoding="utf-8",
    )
    _, blocking, _ = scan(gitleaks, repository, today)
    if len(blocking) != 1:
        raise ScanError(f"expected 1 blocking finding(s), got {len(blocking)}")
    _, b_path, _, _ = parse_finding(blocking[0])
    if b_path != "other.py":
        raise ScanError(f"expected blocking finding in other.py, got {b_path}")


def check_path_rule_expired(root, gitleaks, today, day):
    repository = fixture(root, "pr_expired")
    found = findings(gitleaks, repository)
    if len(found) != 1:
        raise ScanError(f"self-test 'path+rule expired': expected 1 finding, got {len(found)}")
    _, f_path, f_rule, _ = parse_finding(found[0])
    for exp in (today - day, today):
        (repository / EXCEPTIONS).write_text(
            f"# expires {exp.isoformat()}: fixture\n{f_path}:{f_rule}\n",
            encoding="utf-8",
        )
        blocking = len(scan(gitleaks, repository, today)[1])
        if blocking != 1:
            raise ScanError(f"expected 1 blocking finding(s), got {blocking}")


def verify_glob_refused(future_date):
    future = future_date.isoformat()
    bad_entries = (
        "sett*.py:generic-api-key",
        "../settings.py:generic-api-key",
        "/settings.py:generic-api-key",
    )
    for bad in bad_entries:
        try:
            parse_exceptions(f"# expires {future}: fixture\n{bad}\n")
        except ScanError:
            pass
        else:
            raise ScanError(f"expected ScanError for '{bad}'")


def self_test(gitleaks, today):
    """Prove the gate against planted keys before its result on the real history is trusted."""
    day = datetime.timedelta(days=1)

    def check_repo(repository, expected):
        blocking = len(scan(gitleaks, repository, today)[1])
        if blocking != expected:
            raise ScanError(f"expected {expected} blocking finding(s), got {blocking}")

    with tempfile.TemporaryDirectory() as root:
        cases = {
            "clean": lambda: check_repo(fixture(root, "clean"), 0),
            "planted on HEAD": lambda: check_repo(fixture(root, "head"), 1),
            "planted on a side branch": lambda: check_repo(fixture(root, "side", side_branch=True), 1),
            "unexpired exception": lambda: check_repo(excepted(root, gitleaks, "valid", today + day), 0),
            "exception expiring today": lambda: check_repo(excepted(root, gitleaks, "today", today), 1),
            "expired exception": lambda: check_repo(excepted(root, gitleaks, "expired", today - day), 1),
            "path+rule exception": lambda: check_repo(path_rule_excepted(root, gitleaks, "pr_valid", today + day), 0),
            "path+rule other file": lambda: check_other_file(root, gitleaks, today, day),
            "path+rule other rule": lambda: check_repo(path_rule_excepted(root, gitleaks, "pr_rule", today + day, rule="kubernetes-secret-yaml"), 1),
            "path+rule expired": lambda: check_path_rule_expired(root, gitleaks, today, day),
            "glob refused": lambda: verify_glob_refused(today + day),
        }
        for label, runner in cases.items():
            try:
                runner()
            except ScanError as error:
                raise ScanError(f"self-test '{label}': {error}") from error
    print(f"Secret scan self-test: {len(cases)} cases behaved as required")


def main(arguments=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gitleaks", type=Path, required=True)
    parser.add_argument("--repository", type=Path, default=Path("."))
    parser.add_argument("--self-test", action="store_true")
    options = parser.parse_args(arguments)
    today = datetime.datetime.now(datetime.UTC).date()
    try:
        if options.self_test:
            self_test(options.gitleaks, today)
            return 0
        allowed, blocking, unused = scan(options.gitleaks, options.repository, today)
    except (ScanError, OSError, subprocess.SubprocessError, json.JSONDecodeError, KeyError) as error:
        print(f"Secret scan failed: {error}", file=sys.stderr)
        return 2
    print(f"Secret scan: {len(allowed)} findings allowed by unexpired exceptions, {len(blocking)} blocking")
    for fingerprint in unused:
        print(f"Unused exception, remove it: {fingerprint}")
    for fingerprint in blocking:
        print(f"Blocking finding: {fingerprint}", file=sys.stderr)
    return 1 if blocking else 0


if __name__ == "__main__":
    sys.exit(main())
