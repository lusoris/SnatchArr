# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Defensive unit test for the clones_check launch boundary.

The shell suite can only observe a fixture log, which cannot show whether the
subprocess API was reached. This test mocks subprocess.run (and Popen, which
run() uses internally) so no scanner, shell, or other program is ever started,
and asserts that an invalid configured executable is refused with a CheckError
of exit_code 2 before any launch is attempted.

Run isolated: python3 -I tools/ci/clones_check_launch_test.py
"""

import importlib.util
import os
import pathlib
import tempfile
import threading
import unittest
from unittest import mock

_MODULE_PATH = pathlib.Path(__file__).resolve().with_name("clones_check.py")


def _load_clones_check():
    """Load clones_check.py by path; -I leaves the script dir off sys.path."""
    spec = importlib.util.spec_from_file_location("clones_check", _MODULE_PATH)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load {_MODULE_PATH}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


clones_check = _load_clones_check()


class InvalidExecutableLaunchTest(unittest.TestCase):
    """An invalid PRAETORCTL must be refused before subprocess is used."""

    def test_invalid_executable_refused_before_subprocess_run(self) -> None:
        """Raise CheckError(exit_code=2) and never call subprocess.run/Popen."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            with (
                mock.patch.dict(os.environ, {"PRAETORCTL": "invalid-tool"}),
                mock.patch.object(clones_check.subprocess, "run") as run_mock,
                mock.patch.object(clones_check.subprocess, "Popen") as popen_mock,
                self.assertRaises(clones_check.CheckError) as ctx,
            ):
                clones_check.run_scan_report(root)

        # Checked after the exception so a call followed by a later validation
        # error still fails this test.
        run_mock.assert_not_called()
        popen_mock.assert_not_called()
        self.assertEqual(ctx.exception.exit_code, 2)
        self.assertIn("invalid PRAETORCTL=", ctx.exception.message)
        self.assertIn("invalid-tool", ctx.exception.message)


class ResolveLookupTest(unittest.TestCase):
    """The bounded filesystem lookup keeps results, errors and exit codes."""

    def test_allowed_names_resolve_from_path(self) -> None:
        """Both supported spellings are looked up by name on PATH."""
        for name in ("praetorctl", "standardsctl"):
            with (
                self.subTest(name=name),
                mock.patch.dict(os.environ, {"PRAETORCTL": name}),
                mock.patch.object(
                    clones_check.shutil, "which", return_value=f"/opt/bin/{name}"
                ) as which_mock,
            ):
                self.assertEqual(clones_check.resolve_praetorctl(), f"/opt/bin/{name}")
                which_mock.assert_called_once_with(name)

    def test_relative_result_is_anchored_to_caller_cwd(self) -> None:
        """A relative PATH hit is joined to the caller cwd without normalizing."""
        with (
            mock.patch.dict(os.environ, {"PRAETORCTL": "praetorctl"}),
            mock.patch.object(
                clones_check.shutil, "which", return_value="bin/praetorctl"
            ),
            mock.patch.object(clones_check.os, "getcwd", return_value="/caller"),
        ):
            result = clones_check.resolve_praetorctl()
        self.assertEqual(result, os.path.join("/caller", "bin/praetorctl"))

    def test_cwd_failure_keeps_message_and_exit_code(self) -> None:
        """An OSError while anchoring surfaces as the original CheckError."""
        with (
            mock.patch.dict(os.environ, {"PRAETORCTL": "praetorctl"}),
            mock.patch.object(
                clones_check.shutil, "which", return_value="bin/praetorctl"
            ),
            mock.patch.object(clones_check.os, "getcwd", side_effect=OSError("boom")),
            self.assertRaises(clones_check.CheckError) as ctx,
        ):
            clones_check.resolve_praetorctl()
        self.assertEqual(ctx.exception.exit_code, 2)
        self.assertIn(
            "cannot make 'bin/praetorctl' absolute: boom", ctx.exception.message
        )

    def test_missing_binary_keeps_message_and_exit_code(self) -> None:
        """A failed PATH lookup reports the existing not-found message."""
        with (
            mock.patch.dict(os.environ, {"PRAETORCTL": "standardsctl"}),
            mock.patch.object(clones_check.shutil, "which", return_value=None),
            self.assertRaises(clones_check.CheckError) as ctx,
        ):
            clones_check.resolve_praetorctl()
        self.assertEqual(ctx.exception.exit_code, 2)
        self.assertEqual(
            ctx.exception.message,
            "clones_check: praetorctl binary not found on PATH: standardsctl",
        )

    def test_absolute_executable_with_allowed_basename(self) -> None:
        """An executable absolute path with an allowed basename is returned as-is."""
        with tempfile.TemporaryDirectory() as tmp:
            exe = pathlib.Path(tmp) / "praetorctl"
            exe.write_text("inert", encoding="utf-8")
            exe.chmod(0o700)
            with mock.patch.dict(os.environ, {"PRAETORCTL": str(exe)}):
                self.assertEqual(clones_check.resolve_praetorctl(), str(exe))

    def test_os_and_value_errors_become_named_check_error(self) -> None:
        """Filesystem lookup failures surface as CheckError(2) with the cause."""
        for exc in (OSError("disk gone"), ValueError("embedded null byte")):
            with (
                self.subTest(error=type(exc).__name__),
                mock.patch.dict(os.environ, {"PRAETORCTL": "praetorctl"}),
                mock.patch.object(clones_check.shutil, "which", side_effect=exc),
                mock.patch.object(clones_check.subprocess, "run") as run_mock,
                mock.patch.object(clones_check.subprocess, "Popen") as popen_mock,
                self.assertRaises(clones_check.CheckError) as ctx,
            ):
                clones_check.resolve_praetorctl()
            run_mock.assert_not_called()
            popen_mock.assert_not_called()
            self.assertEqual(ctx.exception.exit_code, 2)
            self.assertIn("praetorctl lookup failed", ctx.exception.message)
            self.assertIn(type(exc).__name__, ctx.exception.message)
            self.assertIn(str(exc), ctx.exception.message)
            self.assertNotIn("ended without a result", ctx.exception.message)
            self.assertIs(ctx.exception.__cause__, exc)

    def test_stalled_lookup_times_out_and_starts_nothing(self) -> None:
        """A stalled lookup raises CheckError(2) and no process can start later."""
        entered = threading.Event()
        release = threading.Event()
        workers: list[threading.Thread] = []

        def stalled(_name: str) -> str:
            """Stand in for a blocked PATH lookup; bounded by a 5s wait."""
            workers.append(threading.current_thread())
            entered.set()
            release.wait(5)
            return "/never/praetorctl"

        def cleanup() -> None:
            """Release the stalled worker and join it with bounded waits."""
            release.set()
            for worker in workers:
                worker.join(5)
                self.assertFalse(worker.is_alive())

        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            with (
                mock.patch.dict(os.environ, {"PRAETORCTL": "praetorctl"}),
                mock.patch.object(clones_check, "LOOKUP_TIMEOUT_SECONDS", 0.05),
                mock.patch.object(clones_check.shutil, "which", side_effect=stalled),
                mock.patch.object(clones_check.subprocess, "run") as run_mock,
                mock.patch.object(clones_check.subprocess, "Popen") as popen_mock,
            ):
                # Both launch mocks stay installed until the worker is joined,
                # on success and on failure alike.
                try:
                    with self.assertRaises(clones_check.CheckError) as ctx:
                        clones_check.run_scan_report(root)
                    self.assertTrue(entered.wait(1))
                finally:
                    cleanup()

        run_mock.assert_not_called()
        popen_mock.assert_not_called()
        self.assertEqual(ctx.exception.exit_code, 2)
        self.assertIn("lookup timed out after 0.05s", ctx.exception.message)


if __name__ == "__main__":
    unittest.main()
