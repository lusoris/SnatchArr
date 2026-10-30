# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Positive, negative and boundary cases for the go-licenses label corrections."""

import io
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

import generate  # noqa: E402

EUPL_12 = "EUROPEAN UNION PUBLIC LICENCE v. 1.2\nEUPL (c) the European Union 2007, 2016\n"
BSD_1 = (
    "Copyright (c) 2025 Someone All rights reserved.\n\n"
    "1. Redistributions of source code must retain the above copyright\n"
    "   notice, this list of conditions and the following disclaimer.\n\n"
    "THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS \"AS IS\"\n"
)
BSD_2 = BSD_1.replace(
    "disclaimer.\n",
    "disclaimer.\n2. Redistributions in binary form must reproduce the above notice.\n",
)


class CorrectGoLicence(unittest.TestCase):
    def test_eupl_version_from_header(self):
        self.assertEqual(generate.correct_go_licence("EUPL-1.0", EUPL_12), "EUPL-1.2")

    def test_eupl_without_header_kept(self):
        self.assertEqual(generate.correct_go_licence("EUPL-1.1", "no header"), "EUPL-1.1")

    def test_uthash_label_on_bsd_1_clause(self):
        self.assertEqual(generate.correct_go_licence("LicenseRef-uthash", BSD_1), "BSD-1-Clause")

    def test_uthash_label_on_two_clauses_refused(self):
        self.assertIsNone(generate.correct_go_licence("LicenseRef-uthash", BSD_2))

    def test_unknown_licenseref_refused(self):
        self.assertIsNone(generate.correct_go_licence("LicenseRef-other", BSD_1))

    def test_spdx_label_kept(self):
        self.assertEqual(generate.correct_go_licence("MIT", ""), "MIT")


class NormalizeExpression(unittest.TestCase):
    def test_collapses_whitespace(self):
        self.assertEqual(generate.normalize_expression("Apache-2.0  OR \tMIT"), "Apache-2.0 OR MIT")

    def test_empty_stays_empty(self):
        self.assertEqual(generate.normalize_expression("   "), "")


class NormalizeGoUrl(unittest.TestCase):
    def test_positive_gobreaker_collapses(self):
        url = "https://github.com/sony/gobreaker/blob/v2.4.0/v2/LICENSE"
        expected = "https://github.com/sony/gobreaker/blob/v2.4.0/LICENSE"
        self.assertEqual(generate.normalize_go_url("github.com/sony/gobreaker/v2", url), expected)
        res = generate.normalize_go_entry(
            "github.com/sony/gobreaker/v2", "v2.4.0", "MIT", url, "/dummy/path", "/cache"
        )
        self.assertEqual(res[3], expected)

    def test_negative_nonsuffixed_module_untouched(self):
        url = "https://github.com/x/y/blob/v1.0.0/v2/LICENSE"
        self.assertEqual(generate.normalize_go_url("github.com/x/y", url), url)
        res = generate.normalize_go_entry("github.com/x/y", "v1.0.0", "MIT", url, "/dummy/path", "/cache")
        self.assertEqual(res[3], url)

    def test_boundary_v1_suffix_untouched(self):
        url = "https://github.com/x/y/blob/v1.0.0/v1/LICENSE"
        self.assertEqual(generate.normalize_go_url("github.com/x/y/v1", url), url)
        res = generate.normalize_go_entry("github.com/x/y/v1", "v1.0.0", "MIT", url, "/dummy/path", "/cache")
        self.assertEqual(res[3], url)

    def test_boundary_goenvoy_untouched(self):
        url = "https://github.com/golusoris/goenvoy/blob/arr/v2.6.1/arr/LICENSE"
        self.assertEqual(generate.normalize_go_url("github.com/golusoris/goenvoy/arr/v2", url), url)
        res = generate.normalize_go_entry(
            "github.com/golusoris/goenvoy/arr/v2", "v2.6.1", "MIT", url, "/dummy/path", "/cache"
        )
        self.assertEqual(res[3], url)


class ReconcileGoUrl(unittest.TestCase):
    def test_positive_unknown_with_committed_url_kept(self):
        pkg = {"name": "filippo.io/csrf", "version": "v0.2.1", "url": "Unknown"}
        committed = {
            "name": "filippo.io/csrf",
            "version": "v0.2.1",
            "url": "https://github.com/FiloSottile/csrf/blob/v0.2.1/LICENSE",
        }
        self.assertEqual(
            generate.reconcile_go_url(pkg, committed),
            "https://github.com/FiloSottile/csrf/blob/v0.2.1/LICENSE",
        )
        with patch("sys.stderr", io.StringIO()):
            res = generate.reconcile_go_package(pkg, committed, check=True)
        self.assertEqual(res["url"], "https://github.com/FiloSottile/csrf/blob/v0.2.1/LICENSE")

    def test_negative_unknown_with_no_committed_empty(self):
        pkg = {"name": "filippo.io/csrf", "version": "v0.2.1", "url": "Unknown"}
        self.assertEqual(generate.reconcile_go_url(pkg, None), "")
        self.assertEqual(generate.reconcile_go_url(pkg, {}), "")
        res = generate.reconcile_go_package(pkg, None, check=True)
        self.assertEqual(res["url"], "")

    def test_negative_unknown_with_other_version_empty(self):
        pkg = {"name": "filippo.io/csrf", "version": "v0.2.1", "url": "Unknown"}
        committed = {
            "name": "filippo.io/csrf",
            "version": "v0.3.0",
            "url": "https://github.com/FiloSottile/csrf/blob/v0.3.0/LICENSE",
        }
        self.assertEqual(generate.reconcile_go_url(pkg, committed), "")
        res = generate.reconcile_go_package(pkg, committed, check=True)
        self.assertEqual(res["url"], "")

    def test_boundary_resolved_differing_from_committed_resolved_wins(self):
        pkg = {
            "name": "filippo.io/csrf",
            "version": "v0.2.1",
            "url": "https://github.com/FiloSottile/csrf/blob/v0.2.1/LICENSE",
        }
        committed = {
            "name": "filippo.io/csrf",
            "version": "v0.2.1",
            "url": "https://example.com/other/LICENSE",
        }
        self.assertEqual(
            generate.reconcile_go_url(pkg, committed),
            "https://github.com/FiloSottile/csrf/blob/v0.2.1/LICENSE",
        )
        res = generate.reconcile_go_package(pkg, committed, check=True)
        self.assertEqual(res["url"], "https://github.com/FiloSottile/csrf/blob/v0.2.1/LICENSE")

    def test_positive_generate_mode_failed_lookup_raises(self):
        pkg = {"name": "filippo.io/csrf", "version": "v0.2.1", "url": "Unknown"}
        committed = {
            "name": "filippo.io/csrf",
            "version": "v0.2.1",
            "url": "https://github.com/FiloSottile/csrf/blob/v0.2.1/LICENSE",
        }
        with self.assertRaises(generate.LicenceError) as ctx:
            generate.reconcile_go_package(pkg, committed, check=False)
        self.assertEqual(ctx.exception.status, 2)
        self.assertIn("filippo.io/csrf@v0.2.1", str(ctx.exception))

    def test_negative_generate_mode_missing_committed_passes(self):
        pkg = {"name": "filippo.io/csrf", "version": "v0.2.1", "url": "Unknown"}
        res = generate.reconcile_go_package(pkg, None, check=False)
        self.assertEqual(res["url"], "")

    def test_positive_check_mode_stderr(self):
        pkg = {"name": "filippo.io/csrf", "version": "v0.2.1", "url": "Unknown"}
        committed = {
            "name": "filippo.io/csrf",
            "version": "v0.2.1",
            "url": "https://github.com/FiloSottile/csrf/blob/v0.2.1/LICENSE",
        }
        stderr_buf = io.StringIO()
        with patch("sys.stderr", stderr_buf):
            res = generate.reconcile_go_package(pkg, committed, check=True)
        self.assertEqual(res["url"], "https://github.com/FiloSottile/csrf/blob/v0.2.1/LICENSE")
        err = stderr_buf.getvalue()
        self.assertIn("filippo.io/csrf", err)
        self.assertIn("v0.2.1", err)
        self.assertIn("Kept committed licence URL", err)


if __name__ == "__main__":
    unittest.main()
