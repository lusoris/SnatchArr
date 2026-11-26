# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Compare release-please's marked versions and first changelog entry to its manifest.

Exit 0 = versions agree, 1 = release refused, 2 = unreadable or malformed input.
"""

from __future__ import annotations

import argparse
import contextlib
import io
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
    rf"(?:version|appVersion):\s+{CORE}(?=\s|$)"
    rf"|image:\s+[^\s@]+:{CORE}(?=@|\s|$)"
    rf"|\[!\[Release\]\(\S*?[?&]message=v{CORE}(?=[&)])"
)
HEADING = re.compile(r"^ {0,3}##(?:[ \t]+|$)")
HEADING_LINK = re.compile(r"^\[([^\]]+)\]\(([^)]*)\)")
COMPARE_LINK = re.compile(rf"^(.+)/compare/v({CORE})\.\.\.v({CORE})$")
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
        if isinstance(entry, str):
            names.append(string_extra_file(entry))
        elif (
            not isinstance(entry, dict)
            or entry.keys() != {"type", "path"}
            or entry["type"] != "generic"
        ):
            raise ValueError(f"{CONFIG}:1: expected a generic updater with a path")
        else:
            names.append(relative_name(entry["path"]))
    return names, relative_name(package.get("changelog-path", "CHANGELOG.md"))


def string_extra_file(entry: str) -> str:
    """Accept Generic-only string paths and reject composite updater suffixes."""
    path = relative_name(entry)
    formats = {
        ".json": "JSON",
        ".yaml": "YAML",
        ".yml": "YAML",
        ".toml": "TOML",
        ".xml": "XML",
    }
    suffix = next((item for item in formats if path.endswith(item)), "")
    if suffix:
        raise ValueError(
            f"{CONFIG}:1: {path}: a string path ending {suffix} also runs "
            f"release-please's {formats[suffix]} updater, which rewrites the file "
            'and drops the markers; use {"type": "generic", "path": ...}'
        )
    return path


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
        elif unmarked_value(line):
            findings.append(
                f"{where}: release version field without a release-please marker"
            )
    if not count:
        findings.append(f"{name}:1: no marked line with a version")
    return count


def unmarked_value(line: str) -> bool:
    """Identify an unmarked release field, excluding indented YAML child keys."""
    text = line.strip()
    if not VALUE_LINE.match(text):
        return False
    return not (line[:1].isspace() and text.startswith(("version:", "appVersion:")))


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
    """Require a CommonMark level-two release heading linked to its predecessor."""
    headings = changelog_headings(text, name)
    if not headings:
        return [f"{name}:1: no level-two heading"]
    number, first = headings[0]
    link = HEADING_LINK.match(first)
    if not link:
        return [f"{name}:{number}: expected heading starting with ## [{version}]("]
    if link[1] != version:
        return [f"{name}:{number}: {link[1]} != {version}"]
    if len(headings) == 1:
        return []
    previous_number, previous_heading = headings[1]
    previous = heading_version(previous_heading)
    if not previous:
        return [
            (
                f"{name}:{previous_number}: expected ## [P](...) or ## P (YYYY-MM-DD) "
                "for the previous release"
            )
        ]
    return compare_changelog_link(
        number, first, previous_heading, version, previous, name
    )


def changelog_headings(text: str, name: str) -> list[tuple[int, str]]:
    """Return CommonMark level-two ATX headings without their prefix."""
    return [
        (number, line[match.end() :].rstrip())
        for number, line in enumerate(lines(text, name), 1)
        if (match := HEADING.match(line))
    ]


def heading_version(text: str) -> str:
    """Extract a release number from a linked or bare second changelog heading."""
    match = HEADING_LINK.match(text)
    if match:
        return match[1]
    match = re.match(rf"({CORE}) \(", text)
    return match[1] if match else ""


def compare_changelog_link(
    number: int, first: str, second: str, version: str, previous: str, name: str
) -> list[str]:
    """Compare the first changelog link with the preceding release's link base."""
    link = HEADING_LINK.match(first)
    second_link = HEADING_LINK.match(second)
    first_parts = COMPARE_LINK.match(link[2]) if link else None
    if not first_parts or first_parts[3] != version:
        return [f"{name}:{number}: {expected_message('<base>', previous, version)}"]
    if first_parts[2] != previous:
        return [f"{name}:{number}: compare link from v{first_parts[2]} != v{previous}"]
    if not matches_expected_heading(first, first_parts[1], previous, version):
        return [
            f"{name}:{number}: {expected_message(first_parts[1], previous, version)}"
        ]
    if second_link and "/compare/" in second_link[2]:
        second_base = second_link[2].split("/compare/", 1)[0]
        if first_parts[1] != second_base:
            return [
                f"{name}:{number}: compare link base {first_parts[1]} != {second_base}"
            ]
    return []


def expected_heading(base: str, previous: str, version: str) -> str:
    """Build the exact release-please changelog heading grammar."""
    return f"[{version}]({base}/compare/v{previous}...v{version}) (YYYY-MM-DD)"


def matches_expected_heading(
    first: str, base: str, previous: str, version: str
) -> bool:
    """Match the exact release heading grammar while allowing a calendar date."""
    link = f"{base}/compare/v{previous}...v{version}"
    pattern = (
        rf"\[{re.escape(version)}\]\({re.escape(link)}\) \(\d{{4}}-\d{{2}}-\d{{2}}\)"
    )
    return re.fullmatch(pattern, first) is not None


def expected_message(base: str, previous: str, version: str) -> str:
    """Describe the exact heading expected for the manifest release version."""
    return f"expected ## {expected_heading(base, previous, version)}"


def evaluate(root: Path, tag_version: str | None = None) -> tuple[int, str]:
    """Return the CLI status and diagnostics for a release tree."""
    try:
        version = release_version(root)
        names, changelog = release_files(root)
        findings: list[str] = []
        if tag_version is not None and version != tag_version:
            findings.append(f"{MANIFEST}:1: manifest {version} != tag {tag_version}")
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


Case = tuple[str, dict[str, str], int, str, tuple[str, ...]]


def case(
    label: str,
    changes: dict[str, str],
    expected: int,
    substring: str,
    arguments: tuple[str, ...] = (),
) -> Case:
    """Create one self-test case with optional extra CLI arguments."""
    return label, changes, expected, substring, arguments


def stale_line(name: str, text: str, prefix: str, version: str, old: str) -> Case:
    """Plant a stale value and require the diagnostic at its original line."""
    needle = prefix + version
    before, sep, after = text.partition(needle)
    if not sep:
        raise ValueError(f"{name}: fixture lacks {needle!r}")
    line = (before + prefix).count("\n") + 1
    expected = f"{name}:{line}: {old} != {version}"
    return case(
        f"{name} {prefix.strip()}", {name: before + prefix + old + after}, 1, expected
    )


def planted_cases(
    files: dict[str, str], names: list[str], changelog: str, version: str
) -> list[Case]:
    """Build independent mutations of the real release inputs."""
    chart = next(name for name in names if "appVersion: " in files[name])
    badge = next(name for name in names if "&message=v" in files[name])
    history = files[changelog]
    headings = changelog_headings(history, changelog)
    first_link = HEADING_LINK.match(headings[0][1])
    previous = heading_version(headings[1][1])
    old = previous if previous != version else version + "0"
    base = COMPARE_LINK.match(first_link[2])[1] if first_link else ""
    cases = [
        case("real copy", {}, 0, f"release {version},"),
        stale_line(chart, files[chart], "\nversion: ", version, old),
        stale_line(chart, files[chart], "\nappVersion: ", version, old),
        stale_line(chart, files[chart], "snatcharr-api:", version, old),
        stale_line(badge, files[badge], "&message=v", version, old),
        stale_line(changelog, history, "## [", version, old),
    ]
    return (
        cases
        + marker_cases(files, chart, badge, version, old)
        + input_cases(files, names, changelog, version, previous, base)
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
        case(
            "malformed badge",
            {badge: malformed},
            1,
            "expected a chart version, image tag or release badge",
        ),
        case("two versions", {badge: two_versions}, 1, f"{old} != {version}"),
        case(
            "README marker removed",
            {badge: files[badge].replace(SINGLE, "")},
            1,
            "no marked line",
        ),
        case(
            "unclosed block",
            {chart: files[chart].replace(END, "")},
            1,
            "unterminated version block",
        ),
        case(
            "nested block",
            {chart: files[chart].replace(START, f"{START}\n  # {START}")},
            1,
            "nested version block",
        ),
    ]


def input_cases(
    files: dict[str, str],
    names: list[str],
    changelog: str,
    version: str,
    previous: str,
    base: str,
) -> list[Case]:
    """Exercise configuration, manifest, changelog and multi-digit boundaries."""
    config = json.loads(files[CONFIG])
    config["packages"]["."]["extra-files"].append(
        {"type": "generic", "path": "missing-extra-file"}
    )
    no_headings = re.sub(r"^## ", "### ", files[changelog], flags=re.MULTILINE)
    chart = next(name for name in names if "appVersion: " in files[name])
    badge = next(name for name in names if "&message=v" in files[name])
    return [
        *base_input_cases(files, changelog, version, no_headings, config),
        *tag_cases(version),
        *unmarked_value_cases(files, chart, version),
        *changelog_cases(files[changelog], changelog, version, previous, base),
        *extra_file_cases(files, chart, badge, version),
    ]


def base_input_cases(
    files: dict[str, str], changelog: str, version: str, no_headings: str, config: dict
) -> list[Case]:
    """Cover malformed release inputs and a multi-digit version boundary."""
    boundary = "10.20.30"
    return [
        case(
            "CHANGELOG without heading",
            {changelog: no_headings},
            1,
            "no level-two heading",
        ),
        case(
            "duplicate key", {MANIFEST: '{".":"1.2.3",".":"1.2.3"}'}, 2, "duplicate key"
        ),
        case("invalid version", {MANIFEST: '{".":"0.9"}'}, 2, "major.minor.patch"),
        case(
            "missing extra file", {CONFIG: json.dumps(config)}, 2, "missing-extra-file"
        ),
        case(
            "multi-digit version",
            {key: value.replace(version, boundary) for key, value in files.items()},
            0,
            f"release {boundary},",
        ),
    ]


def tag_cases(version: str) -> list[Case]:
    """Cover a release tag matching and differing from the manifest version."""
    return [
        case("tag matches", {}, 0, f"release {version},", ("--tag-version", version)),
        case(
            "tag differs",
            {},
            1,
            f"manifest {version} != tag 9.9.9",
            ("--tag-version", "9.9.9"),
        ),
    ]


def unmarked_value_cases(files: dict[str, str], chart: str, version: str) -> list[Case]:
    """Cover top-level marker removal and an exempt nested dependency version."""
    removed = files[chart].replace(
        f"version: {version} # {SINGLE}", f"version: {version}", 1
    )
    line = files[chart].partition(f"version: {version} # {SINGLE}")[0].count("\n") + 1
    nested = files[chart] + "\ndependencies:\n  - name: x\n    version: 1.2.3\n"
    return [
        case(
            "Chart.yaml version marker removed",
            {chart: removed},
            1,
            f"{chart}:{line}: release version field without a release-please marker",
        ),
        case(
            "unmarked nested dependency version",
            {chart: nested},
            0,
            f"release {version},",
        ),
    ]


def changelog_cases(
    history: str, changelog: str, version: str, previous: str, base: str
) -> list[Case]:
    """Plant release link and CommonMark heading mutations from real changelog text."""
    first = f"{base}/compare/v{previous}...v{version}"
    second = changelog_headings(history, changelog)[1][1]
    without_version = history.replace(f"## {second}", "## Unreleased", 1)
    return [
        *changelog_link_cases(history, changelog, version, previous, base, first),
        case(
            "second changelog heading without a version",
            {changelog: without_version},
            1,
            "expected ## [P](...) or ## P (YYYY-MM-DD)",
        ),
        *changelog_indent_cases(history, changelog, version, previous, base),
    ]


def changelog_link_cases(
    history: str, changelog: str, version: str, previous: str, base: str, first: str
) -> list[Case]:
    """Cover malformed and mismatched release-please compare links."""
    second = f"## [{previous}]({base}/compare/"
    return [
        case(
            "changelog link to the wrong release",
            {
                changelog: history.replace(
                    first, f"{base}/compare/v{previous}...v9.9.9", 1
                )
            },
            1,
            "expected ## [",
        ),
        case(
            "changelog link from the wrong release",
            {
                changelog: history.replace(
                    first, f"{base}/compare/v9.9.9...v{version}", 1
                )
            },
            1,
            "compare link from",
        ),
        case(
            "changelog link base differs",
            {
                changelog: history.replace(
                    second, f"## [{previous}]({base}//compare/", 1
                )
            },
            1,
            "compare link base",
        ),
        case(
            "changelog heading truncated",
            {changelog: history.replace(first, f"{base}/compare/v{previous}", 1)},
            1,
            "expected ## [",
        ),
    ]


def changelog_indent_cases(
    history: str, changelog: str, version: str, previous: str, base: str
) -> list[Case]:
    """Cover CommonMark's three-space heading indentation boundary."""
    return [
        case(
            "indented first changelog heading",
            {
                changelog: f"   ## [9.9.9]({base}/compare/v{version}...v9.9.9) (2026-01-01)\n{history}"
            },
            1,
            f"9.9.9 != {version}",
        ),
        case(
            "real first heading indented three spaces",
            {changelog: history.replace("## [", "   ## [", 1)},
            0,
            f"release {version},",
        ),
        case(
            "real first heading indented four spaces",
            {changelog: history.replace("## [", "    ## [", 1)},
            1,
            f"{previous} != {version}",
        ),
    ]


def extra_file_cases(
    files: dict[str, str], chart: str, badge: str, version: str
) -> list[Case]:
    """Cover release-please string extra-file updater selection."""
    return [
        case(
            "README as a string extra file",
            {CONFIG: string_config(files[CONFIG], badge)},
            0,
            f"release {version},",
        ),
        case(
            "Chart.yaml as a string extra file",
            {CONFIG: string_config(files[CONFIG], chart)},
            2,
            "YAML updater",
        ),
    ]


def string_config(text: str, path: str) -> str:
    """Replace one generic updater object with its release-please string shorthand."""
    config = json.loads(text)
    extra = config["packages"]["."]["extra-files"]
    config["packages"]["."]["extra-files"] = [
        path if item["path"] == path else item for item in extra
    ]
    return json.dumps(config)


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
        for index, (label, changes, expected, substring, arguments) in enumerate(cases):
            fixture = Path(tmp) / str(index)
            for name, content in (files | changes).items():
                path = fixture / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content, encoding="utf-8")
            stdout = io.StringIO()
            stderr = io.StringIO()
            with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                status = run_check(
                    parse_arguments(["--root", str(fixture), *arguments])
                )
            output = stdout.getvalue() if expected == 0 else stderr.getvalue()
            if status != expected or substring not in output:
                failures.append(
                    f"{label.strip()} (expected {expected}, {substring!r}): got {status}, {output}"
                )
    if failures:
        return 1, "\n".join(failures)
    return 0, f"self-test ok {len(cases)} cases"


def parse_arguments(argv: list[str] | None) -> argparse.Namespace:
    """Parse release-version checker command-line options."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--root", type=Path, default=Path("."), help="repository root")
    parser.add_argument(
        "--self-test", action="store_true", help="run the planted cases"
    )
    parser.add_argument(
        "--tag-version", help="require this tag version to match the manifest"
    )
    return parser.parse_args(argv)


def run_check(args: argparse.Namespace) -> int:
    """Evaluate release inputs and stream diagnostics to their status channel."""
    try:
        status, message = evaluate(args.root, args.tag_version)
    except (OSError, TypeError, ValueError) as exc:
        status, message = 2, str(exc)
    print(message, file=sys.stderr if status else sys.stdout)
    return status


def main(argv: list[str] | None = None) -> int:
    """Run the release checker or its isolated planted cases."""
    args = parse_arguments(argv)
    if not args.self_test:
        return run_check(args)
    try:
        status, message = self_test(args.root)
    except (OSError, TypeError, ValueError) as exc:
        status, message = 2, str(exc)
    print(message, file=sys.stderr if status else sys.stdout)
    return status


if __name__ == "__main__":
    sys.exit(main())
