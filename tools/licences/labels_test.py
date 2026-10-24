# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Positive, negative and boundary cases for the go-licenses label corrections."""

import sys
import unittest
from pathlib import Path

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


if __name__ == "__main__":
    unittest.main()
