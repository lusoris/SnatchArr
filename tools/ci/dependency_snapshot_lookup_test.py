# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Unit tests for the bounded ``resolve_docker`` lookup in dependency_snapshot.

Run in isolated mode (``python -I``); the target is located relative to this file.
A module-level watchdog aborts the process if a broken implementation hangs.
"""
import faulthandler
import importlib.util
import pathlib
import threading
import time
import unittest

TARGET = pathlib.Path(__file__).resolve().with_name("dependency_snapshot.py")
WATCHDOG_SECONDS = 20


def setUpModule():
    """Arm a hard watchdog that dumps tracebacks and exits on a hang."""
    faulthandler.dump_traceback_later(WATCHDOG_SECONDS, exit=True)


def tearDownModule():
    """Disarm the watchdog once all tests have finished."""
    faulthandler.cancel_dump_traceback_later()


def load_target():
    """Import the dependency snapshot script from its explicit path."""
    spec = importlib.util.spec_from_file_location("dependency_snapshot", TARGET)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class ResolveDockerTest(unittest.TestCase):
    """Behaviour of resolve_docker with injected in-memory lookups."""

    @classmethod
    def setUpClass(cls):
        """Load the module under test once."""
        cls.mod = load_target()

    def test_found_on_path(self):
        """A bare name is resolved through the lookup."""
        result = self.mod.resolve_docker(
            "docker", lookup=lambda value: "/fake/docker", timeout=1)
        self.assertEqual(result, "/fake/docker")

    def test_explicit_path(self):
        """An explicit path is passed through the lookup unchanged."""
        result = self.mod.resolve_docker(
            "/a/docker", lookup=lambda value: value, timeout=1)
        self.assertEqual(result, "/a/docker")

    def test_missing_raises_scan_error(self):
        """A lookup returning None raises ScanError with a useful message."""
        cases = (("docker", "not found on PATH"), ("/a/docker", "must be"))
        for value, text in cases:
            with self.subTest(value=value):
                with self.assertRaises(self.mod.ScanError) as ctx:
                    self.mod.resolve_docker(
                        value, lookup=lambda name: None, timeout=1)
                self.assertIn(text, str(ctx.exception))

    def test_lookup_exception_propagates(self):
        """An exception raised by the lookup reaches the caller unchanged."""

        def boom(value):
            """Fail like a broken filesystem lookup."""
            raise OSError("boom")

        with self.assertRaises(OSError) as ctx:
            self.mod.resolve_docker("docker", lookup=boom, timeout=1)
        self.assertEqual(str(ctx.exception), "boom")

    def test_blocked_lookup_times_out_and_cleans_up(self):
        """A blocked lookup hits the deadline and its worker thread ends."""
        release = threading.Event()
        workers = []
        self.addCleanup(release.set)

        def blocked(value):
            """Block until released, recording the thread that runs it."""
            workers.append(threading.current_thread())
            release.wait(15)
            return "/fake/docker"

        start = time.monotonic()
        with self.assertRaises(self.mod.ScanError) as ctx:
            self.mod.resolve_docker("docker", lookup=blocked, timeout=0.1)
        elapsed = time.monotonic() - start
        self.assertIn("exceeded", str(ctx.exception))
        self.assertLess(elapsed, 5)

        release.set()
        self.assertTrue(workers, "lookup never ran")
        for worker in workers:
            if worker is not threading.current_thread():
                worker.join(5)
                self.assertFalse(worker.is_alive())


if __name__ == "__main__":
    unittest.main()
