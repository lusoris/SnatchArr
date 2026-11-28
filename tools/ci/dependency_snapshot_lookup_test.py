# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Unit tests for the bounded ``resolve_docker`` lookup in dependency_snapshot.

Run in isolated mode (``python -I``); the target is located relative to this file.
A module-level watchdog aborts the process if a broken implementation hangs.
"""
import contextlib
import faulthandler
import importlib.util
import io
import pathlib
import sys
import threading
import time
import unittest
from unittest import mock

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
        started = threading.Event()
        self.addCleanup(release.set)

        def blocked(value):
            """Block until released, recording the thread that runs it."""
            workers.append(threading.current_thread())
            started.set()
            release.wait(15)
            return "/fake/docker"

        start = time.monotonic()
        with self.assertRaises(self.mod.ScanError) as ctx:
            self.mod.resolve_docker("docker", lookup=blocked, timeout=0.1)
        elapsed = time.monotonic() - start
        self.assertIn("exceeded", str(ctx.exception))
        self.assertLess(elapsed, 1.0)

        release.set()
        self.assertTrue(started.wait(5), "lookup never started")
        self.assertTrue(workers, "lookup never ran")
        for worker in workers:
            if worker is not threading.current_thread():
                worker.join(5)
                self.assertFalse(worker.is_alive())

    def _check_late_completion(self, start_cost, join_cost, want_join):
        """Run resolve_docker on a fake clock where lookup finishes at/after deadline."""
        clock = [100.0]
        call = {}
        thread = mock.Mock()
        thread.is_alive.return_value = False

        def on_start():
            """Advance the fake monotonic clock for thread startup."""
            clock[0] += start_cost

        def on_join(timeout=None):
            """Complete the lookup and advance the fake clock to arrival time."""
            self.assertEqual(timeout, want_join)
            call["target"](*call["args"])
            clock[0] += join_cost

        def make_thread(*, target, args, daemon):
            """Record worker arguments and return the fake thread."""
            self.assertTrue(daemon)
            call["target"], call["args"] = target, args
            return thread

        thread.start.side_effect = on_start
        thread.join.side_effect = on_join
        fake_time = mock.Mock(monotonic=lambda: clock[0])
        with (
            mock.patch.object(self.mod, "time", fake_time),
            mock.patch.object(self.mod.threading, "Thread", side_effect=make_thread),
            self.assertRaises(self.mod.ScanError) as ctx,
        ):
            self.mod.resolve_docker("docker", lookup=lambda _v: "/x", timeout=0.5)
        self.assertIn("exceeded", str(ctx.exception))
        thread.start.assert_called_once_with()
        thread.join.assert_called_once_with(timeout=want_join)
        return clock[0]

    def test_deadline_budget_and_at_deadline_completion(self):
        """Join gets the remaining budget; arrival at the deadline is refused."""
        self.assertEqual(self._check_late_completion(0.25, 0.25, 0.25), 100.5)

    def test_strictly_after_deadline_completion(self):
        """Arrival strictly after the deadline (and start overrun) is refused."""
        self.assertGreater(self._check_late_completion(0.25, 0.5, 0.25), 100.5)
        self.assertGreater(self._check_late_completion(0.75, 0.0, 0.0), 100.5)

    def test_main_wiring_and_resolver_errors(self):
        """main wires the resolved docker path and fails cleanly on resolver errors."""
        argv = ["x", "--trivy", "img:t@sha256:" + "0" * 64, "--out", "o.json",
                "--sha", "s", "--ref", "r"]
        with mock.patch.object(sys, "argv", argv), mock.patch.object(
                self.mod, "scan_and_write", return_value=0) as scan:
            with mock.patch.object(
                    self.mod, "resolve_docker", return_value="/r/docker") as resolve:
                self.assertEqual(self.mod.main(), 0)
            resolve.assert_called_once_with("docker")
            self.assertEqual(scan.call_args.args[1], "/r/docker")
            for error in (OSError("gone"), ValueError("invalid")):
                with self.subTest(error=error):
                    stderr = io.StringIO()
                    with contextlib.redirect_stderr(stderr), mock.patch.object(
                            self.mod, "resolve_docker", side_effect=error):
                        self.assertEqual(self.mod.main(), 1)
                    self.assertIn("could not be resolved", stderr.getvalue())
                    self.assertIn(str(error), stderr.getvalue())
            scan.assert_called_once()


if __name__ == "__main__":
    unittest.main()
