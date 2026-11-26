# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Compare release-please's marked versions and first changelog entry to its manifest.

Exit 0 = versions agree, 1 = release refused, 2 = unreadable or malformed input.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections.abc import Iterator
from pathlib import Path
from tempfile import TemporaryDirectory

MANIFEST = ".release-please-manifest.json"
CONFIG = "release-please-config.json"
MAX_BYTES = 4 * 1024 * 1024
MAX_LINES = 50_000
MAX_FILES = 64
MAX_CASES = 32
CORE = r"[0-9]+\.[0-9]+\.[0-9]+"
VERSION = re.compile(rf"(?<!\w)v?({CORE}(?:[-+][0-9A-Za-z.+-]+)?)(?!\w|\.[0-9])")
# Match the version field of each supported line without checking the image digest.
VALUE_LINE = re.compile(
    rf"(?:version|appVersion):\s+{CORE}(?=\s)"
    rf"|image:\s+[^\s@]+:{CORE}(?=@|\s|$)"
    rf"|\[!\[Release\]\(\S*?[?&]message=v{CORE}(?=[&)])"
)
BLOCK_METADATA = re.compile(r"artifacthub\.io/images: \||- name: \S+")
SINGLE = "x-release-please-version"
START = "x-release-please-start-version"
END = "x-release-please-end"


def read(root: Path, name: str) -> str:
    """Read one bounded UTF-8 input within the release root."""
    path = root / name
    if not path.resolve().is_relative_to(root.resolve()) or not path.is_file():
        raise ValueError(f"{name}:1: expected a regular file inside the release root")
    with path.open("rb") as stream:
        raw = stream.read(MAX_BYTES + 1)
    if len(raw) > MAX_BYTES:
        raise ValueError(f"{name}:1: exceeds {MAX_BYTES} bytes")
    try:
        return raw.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise ValueError(f"{name}:1: {exc}") from exc


def unique_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    """Reject repeated JSON keys at every nesting level."""
    result = dict(pairs)
    if len(result) != len(pairs):
        raise ValueError("duplicate key")
    return result


def load_object(root: Path, name: str) -> dict:
    """Load a JSON object and identify input errors by file."""
    raw = read(root, name)
    try:
        obj = json.loads(raw, object_pairs_hook=unique_keys)
    except (ValueError, RecursionError) as exc:
        raise ValueError(f"{name}:{getattr(exc, 'lineno', 1)}: {exc}") from exc
    if not isinstance(obj, dict):
        raise TypeError(f"{name}:1: expected a JSON object")
    return obj


def release_version(root: Path) -> str:
    """Require one numeric release version for the root package."""
    obj = load_object(root, MANIFEST)
    version = obj.get(".")
    if (
        obj.keys() != {"."}
        or not isinstance(version, str)
        or not re.fullmatch(CORE, version)
    ):
        raise ValueError(
            f"{MANIFEST}:1: expected exactly one '.' key with a major.minor.patch version"
        )
    return version


def relative_name(value: object) -> str:
    """Accept repository-relative input names from release-please configuration."""
    if (
        not isinstance(value, str)
        or not value
        or Path(value).is_absolute()
        or ".." in Path(value).parts
    ):
        raise ValueError(f"{CONFIG}:1: expected a repository-relative file path")
    return value


def release_files(root: Path) -> tuple[list[str], str]:
    """Read the root package's generic updater paths and changelog path."""
    packages = load_object(root, CONFIG).get("packages")
    if not isinstance(packages, dict) or not isinstance(packages.get("."), dict):
        raise TypeError(f"{CONFIG}:1: expected configuration for package '.'")
    package = packages["."]
    extra = package.get("extra-files")
    if not isinstance(extra, list) or not 1 <= len(extra) <= MAX_FILES:
        raise ValueError(f"{CONFIG}:1: expected 1..{MAX_FILES} extra-files")
    names = []
    for entry in extra:
        if (
            not isinstance(entry, dict)
            or entry.keys() != {"type", "path"}
            or entry["type"] != "generic"
        ):
            raise ValueError(f"{CONFIG}:1: expected a generic updater with a path")
        names.append(relative_name(entry["path"]))
    return names, relative_name(package.get("changelog-path", "CHANGELOG.md"))


def lines(text: str, name: str) -> list[str]:
    """Reject oversized files instead of silently ignoring trailing markers."""
    result = text.splitlines()
    if len(result) > MAX_LINES:
        raise ValueError(f"{name}:1: exceeds {MAX_LINES} lines")
    return result


def marker(line: str) -> str:
    """Recognize the two inline comment forms and the two block delimiters."""
    if "x-release-please-" not in line:
        return ""
    text = line.strip()
    if text == f"# {START}":
        return START
    if text == f"# {END}":
        return END
    if text.count("x-release-please-") == 1 and text.endswith(
        (f"# {SINGLE}", f"<!-- {SINGLE} -->")
    ):
        return SINGLE
    return "invalid"


def check_block(
    numbered: Iterator[tuple[int, str]],
    name: str,
    start: int,
    version: str,
    findings: list[str],
) -> int:
    """Consume the current block from the bounded file iterator, through its end."""
    count = 0
    for number, line in numbered:
        kind = marker(line)
        where = f"{name}:{number}"
        if kind == END:
            if not count:
                findings.append(f"{name}:{start}: block contains no version")
            return count
        if kind in (START, "invalid"):
            problem = (
                "nested version block" if kind == START else "invalid release marker"
            )
            findings.append(f"{where}: {problem}")
        else:
            count += compare_line(line, where, version, findings)
    findings.append(f"{name}:{start}: unterminated version block")
    return count


def check_marked(text: str, name: str, version: str, findings: list[str]) -> int:
    """Compare every version on inline-marked lines and within paired blocks."""
    count = 0
    numbered = enumerate(lines(text, name), 1)
    for number, line in numbered:
        kind = marker(line)
        where = f"{name}:{number}"
        if kind == START:
            count += check_block(numbered, name, number, version, findings)
        elif kind == SINGLE:
            count += compare_line(line, where, version, findings)
        elif kind:
            findings.append(f"{where}: invalid or unmatched release marker")
    if not count:
        findings.append(f"{name}:1: no marked line with a version")
    return count


def compare_line(line: str, where: str, version: str, findings: list[str]) -> int:
    """Compare all version tokens, allowing the chart block's unversioned metadata."""
    found = VERSION.findall(line)
    text = line.strip()
    if not (VALUE_LINE.match(text) or BLOCK_METADATA.fullmatch(text)):
        findings.append(
            f"{where}: expected a chart version, image tag or release badge"
        )
    findings.extend(
        f"{where}: {item} != {version}" for item in found if item != version
    )
    return int(bool(found))


def check_changelog(text: str, name: str, version: str) -> list[str]:
    """Require the first level-two heading to link the manifest version."""
    for number, line in enumerate(lines(text, name), 1):
        if line.startswith("## "):
            if line.startswith(f"## [{version}]("):
                return []
            match = re.match(r"## \[([^\]]+)\]\(", line)
            if match:
                return [f"{name}:{number}: {match[1]} != {version}"]
            return [f"{name}:{number}: expected heading starting with ## [{version}]("]
    return [f"{name}:1: no level-two heading"]


def evaluate(root: Path) -> tuple[int, str]:
    """Return the CLI status and diagnostics for a release tree."""
    try:
        version = release_version(root)
        names, changelog = release_files(root)
        findings: list[str] = []
        counts = []
        for name in names:
            count = check_marked(read(root, name), name, version, findings)
            counts.append(f"{name}={count}")
        findings.extend(check_changelog(read(root, changelog), changelog, version))
    except (OSError, TypeError, ValueError) as exc:
        return 2, str(exc)
    if findings:
        return 1, "\n".join(findings)
    counts.append(f"{changelog}=1")
    return 0, f"release {version}, checked lines: {', '.join(counts)}"


type Case = tuple[str, dict[str, str], int, str]


def stale_line(name: str, text: str, prefix: str, version: str, old: str) -> Case:
    """Plant a stale value and require the diagnostic at its original line."""
    needle = prefix + version
    before, sep, after = text.partition(needle)
    if not sep:
        raise ValueError(f"{name}: fixture lacks {needle!r}")
    line = (before + prefix).count("\n") + 1
    expected = f"{name}:{line}: {old} != {version}"
    return (
        f"{name} {prefix.strip()}",
        {name: before + prefix + old + after},
        1,
        expected,
    )


def planted_cases(
    files: dict[str, str], names: list[str], changelog: str, version: str
) -> list[Case]:
    """Build independent mutations of the real release inputs."""
    chart = next(name for name in names if "appVersion: " in files[name])
    badge = next(name for name in names if "&message=v" in files[name])
    history = files[changelog]
    previous = re.findall(rf"^## \[({CORE})\]\(", history, re.MULTILINE)
    old = next((item for item in previous if item != version), version + "0")
    cases = [
        ("real copy", {}, 0, f"release {version},"),
        stale_line(chart, files[chart], "\nversion: ", version, old),
        stale_line(chart, files[chart], "\nappVersion: ", version, old),
        stale_line(chart, files[chart], "snatcharr-api:", version, old),
        stale_line(badge, files[badge], "&message=v", version, old),
        stale_line(changelog, history, "## [", version, old),
    ]
    return (
        cases
        + marker_cases(files, chart, badge, version, old)
        + input_cases(files, changelog, version)
    )


def marker_cases(
    files: dict[str, str], chart: str, badge: str, version: str, old: str
) -> list[Case]:
    """Exercise missing and malformed markers and multiple versions on one line."""
    two_versions = files[badge].replace(
        f"message=v{version}&", f"message=v{version}&old=v{old}&", 1
    )
    malformed = files[badge].replace(
        f"message=v{version}&", f"message=unknown&other=v{version}&", 1
    )
    return [
        (
            "malformed badge",
            {badge: malformed},
            1,
            "expected a chart version, image tag or release badge",
        ),
        ("two versions", {badge: two_versions}, 1, f"{old} != {version}"),
        (
            "README marker removed",
            {badge: files[badge].replace(SINGLE, "")},
            1,
            "no marked line",
        ),
        (
            "unclosed block",
            {chart: files[chart].replace(END, "")},
            1,
            "unterminated version block",
        ),
        (
            "nested block",
            {chart: files[chart].replace(START, f"{START}\n  # {START}")},
            1,
            "nested version block",
        ),
    ]


def input_cases(files: dict[str, str], changelog: str, version: str) -> list[Case]:
    """Exercise configuration, manifest, changelog and multi-digit boundaries."""
    boundary = "10.20.30"
    config = json.loads(files[CONFIG])
    config["packages"]["."]["extra-files"].append(
        {"type": "generic", "path": "missing-extra-file"}
    )
    no_headings = re.sub(r"^## ", "### ", files[changelog], flags=re.MULTILINE)
    return [
        (
            "CHANGELOG without heading",
            {changelog: no_headings},
            1,
            "no level-two heading",
        ),
        ("duplicate key", {MANIFEST: '{".":"1.2.3",".":"1.2.3"}'}, 2, "duplicate key"),
        ("invalid version", {MANIFEST: '{".":"0.9"}'}, 2, "major.minor.patch"),
        ("missing extra file", {CONFIG: json.dumps(config)}, 2, "missing-extra-file"),
        (
            "multi-digit version",
            {k: v.replace(version, boundary) for k, v in files.items()},
            0,
            f"release {boundary},",
        ),
    ]


def self_test(root: Path) -> tuple[int, str]:
    """Replay each planted case in an isolated copy and report every mismatch."""
    version = release_version(root)
    names, changelog = release_files(root)
    files = {name: read(root, name) for name in (MANIFEST, CONFIG, *names, changelog)}
    try:
        cases = planted_cases(files, names, changelog, version)
    except (StopIteration, ValueError) as exc:
        return 1, f"self-test: real release fixture lacks an expected value: {exc}"
    if len(cases) > MAX_CASES:
        return 1, f"self-test exceeds {MAX_CASES} cases"
    failures = []
    with TemporaryDirectory() as tmp:
        fixture = Path(tmp)
        for label, changes, expected, substring in cases:
            for name, content in (files | changes).items():
                path = fixture / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content, encoding="utf-8")
            status, message = evaluate(fixture)
            if status != expected or substring not in message:
                failures.append(
                    f"{label.strip()} (expected {expected}, {substring!r}): got {status}, {message}"
                )
    if failures:
        return 1, "\n".join(failures)
    return 0, f"self-test ok {len(cases)} cases"


def main() -> int:
    """Check the requested release tree or run the planted cases."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--root", type=Path, default=Path("."), help="repository root")
    parser.add_argument(
        "--self-test", action="store_true", help="run the planted cases"
    )
    args = parser.parse_args()
    try:
        status, message = (
            self_test(args.root) if args.self_test else evaluate(args.root)
        )
    except (OSError, TypeError, ValueError) as exc:
        status, message = 2, str(exc)
    print(message, file=sys.stderr if status else sys.stdout)
    return status


if __name__ == "__main__":
    sys.exit(main())
