# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Regression tests for the release checker's --self-test entry point."""

import json
import subprocess
import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory


def _history_cases() -> tuple[tuple[str, str, int], ...]:
    """Return changelog fixtures with their labels and expected case counts."""
    base = "https://example.com/owner/repo"
    current = f"## [1.2.3]({base}/compare/v1.2.2...v1.2.3) (2026-02-01)\n"
    linked = f"\n## [1.2.2]({base}/compare/v1.2.1...v1.2.2) (2026-01-01)\n"
    older = "\n## 1.2.1 (2025-12-01)\n"
    return (
        ("initial", "## 1.2.3 (2026-02-01)\n", 51),
        ("bare predecessor", current + "\n   ##\t1.2.2 (2026-01-01)\n", 56),
        (
            "bare predecessor with older history",
            current + "\n## 1.2.2 (2026-01-01)\n" + older,
            56,
        ),
        ("linked predecessor", current + linked + older, 56),
        ("linked predecessor without older heading", current + linked, 56),
    )


def _write_fixture(root: Path, quote: str, app_quote: str, history: str) -> None:
    """Write the release-check inputs for one quote and history combination."""
    config = {
        "packages": {
            ".": {
                "extra-files": [
                    {"type": "generic", "path": "Chart.yaml"},
                    "README.md",
                ],
            },
        },
    }
    files = {
        ".release-please-manifest.json": json.dumps({".": "1.2.3"}),
        "release-please-config.json": json.dumps(config),
        "Chart.yaml": (
            f"version: {quote}1.2.3{quote} # x-release-please-version\n"
            f"appVersion: {app_quote}1.2.3{app_quote}"
            " # x-release-please-version\n"
            "annotations:\n"
            "  # x-release-please-start-version\n"
            "  artifacthub.io/images: |\n"
            "    - name: api\n"
            f"      image: {quote}registry.example/snatcharr-api:"
            f"1.2.3@sha256:abcd{quote}\n"
            "    - name: web\n"
            f"      image: {app_quote}registry.example/web:"
            f"1.2.3@sha256:abcd{app_quote}\n"
            "  # x-release-please-end\n"
        ),
        "README.md": (
            "[![Release](https://example.com/badge?label=release"
            "&message=v1.2.3&color=blue)](https://example.com)"
            " <!-- x-release-please-version -->\n"
        ),
        "CHANGELOG.md": history,
    }
    for name, text in files.items():
        (root / name).write_text(text, encoding="utf-8")


class ReleaseSelfTest(unittest.TestCase):
    def test_chart_quotes_and_changelog_history(self) -> None:
        """Run the bounded self-test against plain and mixed-quote fixtures."""
        script = Path(__file__).resolve().with_name("release_version_check.py")
        histories = _history_cases()
        for quote, app_quote in (("", ""), ("'", '"'), ('"', "'")):
            for label, history, expected_cases in histories:
                with (
                    self.subTest(
                        version_quote=quote, app_quote=app_quote, history=label
                    ),
                    TemporaryDirectory() as tmp,
                ):
                    root = Path(tmp)
                    _write_fixture(root, quote, app_quote, history)
                    result = subprocess.run(
                        [
                            sys.executable,
                            str(script),
                            "--root",
                            str(root),
                            "--self-test",
                        ],
                        capture_output=True,
                        text=True,
                        check=False,
                        timeout=10,
                    )
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(
                        result.stdout.strip(), f"self-test ok {expected_cases} cases"
                    )


if __name__ == "__main__":
    unittest.main()
