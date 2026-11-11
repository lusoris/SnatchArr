# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Run one command under a deadline, for hosts without GNU timeout (stock macOS).

Usage: python3 -I run_bounded.py SECONDS CMD [ARG...]

Exit status: that of CMD; 124 when SECONDS pass before CMD ends; 128+N when signal N ends CMD;
125 on a usage error (SECONDS must be a finite number above 0); 126 when CMD cannot be run;
127 when CMD is not found.

On POSIX, CMD runs in a session of its own. At the deadline that whole process group gets
SIGKILL, so no descendant keeps a captured stdout open past SECONDS, and INT, TERM or HUP sent
to this process reach the group as well. The handlers are in place before CMD starts: a signal
that arrives while CMD starts is held and sent once CMD runs, and when CMD cannot start the
held signal is dropped with it (exit 126 or 127). A descendant that starts a session of its own
leaves the group, and descendants still running after CMD exits are left alone. On Windows only
CMD itself is killed. tools/ci/test_lib.sh wraps this script as run_bounded.
"""

from __future__ import annotations

import math
import os
import signal
import subprocess  # nosec B404  # runs the caller's argument list, never a shell
import sys
from types import FrameType

GROUP = hasattr(os, "killpg")
FORWARDED = ("SIGINT", "SIGTERM", "SIGHUP")
REAP_SECONDS = 10.0


def parse(argv: list[str]) -> tuple[float, list[str]] | None:
    """Return (seconds, command) from argv, or None when argv is no valid call."""
    if len(argv) < 2:
        return None
    try:
        seconds = float(argv[0])
    except ValueError:
        return None
    if not math.isfinite(seconds) or seconds <= 0:
        return None
    return seconds, argv[1:]


def stop(proc: subprocess.Popen[bytes], signum: int) -> None:
    """Send signum to the process group of proc, or kill proc itself where groups are absent."""
    if not GROUP:
        proc.kill()
        return
    if not isinstance(proc.pid, int) or isinstance(proc.pid, bool) or proc.pid <= 1 or proc.pid == os.getpgrp():
        proc.kill()  # proc leads no group of its own: never signal init or this process's group
        return
    try:
        os.killpg(proc.pid, signum)
    except ProcessLookupError:
        pass  # every process of the group has been reaped already


def status(rc: int) -> int:
    """Map a Popen return code to a shell exit status: signal N becomes 128+N."""
    return 128 - rc if rc < 0 else rc


def start(cmd: list[str]) -> subprocess.Popen[bytes] | int:
    """Start cmd in a session of its own where POSIX allows it, or return 126 or 127."""
    try:
        return subprocess.Popen(cmd, start_new_session=GROUP)  # nosec B603  # caller's argument list, no shell
    except FileNotFoundError as err:
        print(f"run_bounded: {cmd[0]}: {err.strerror}", file=sys.stderr)
        return 127
    except OSError as err:
        print(f"run_bounded: {cmd[0]}: {err.strerror}", file=sys.stderr)
        return 126


class Forwarder:
    """Signal handler that sends INT, TERM or HUP on to the process group of the command.

    It is installed before the command starts. A signal that arrives before the command's handle
    exists is held, and attach() sends it on once the handle is recorded.
    """

    def __init__(self) -> None:
        """Start with no command and no held signal."""
        self.proc: subprocess.Popen[bytes] | None = None
        self.held: list[int] = []

    def __call__(self, signum: int, _frame: FrameType | None) -> None:
        """Send signum to the command's process group, or hold it until attach()."""
        if self.proc is None:
            self.held.append(signum)
        else:
            stop(self.proc, signum)

    def install(self) -> None:
        """Make this the handler of each forwarded signal the platform defines."""
        for name in FORWARDED:
            if hasattr(signal, name):
                signal.signal(getattr(signal, name), self)

    def attach(self, proc: subprocess.Popen[bytes]) -> None:
        """Record the started command, then send it each signal held while it started."""
        self.proc = proc
        while self.held:
            stop(proc, self.held.pop(0))


def main(argv: list[str]) -> int:
    """Run the command in argv under its deadline and return the exit status to report."""
    call = parse(argv)
    if call is None:
        print("usage: run_bounded.py SECONDS CMD [ARG...] (SECONDS above 0)", file=sys.stderr)
        return 125
    seconds, cmd = call
    forward = Forwarder()
    forward.install()
    proc = start(cmd)
    if isinstance(proc, int):
        return proc
    forward.attach(proc)
    try:
        return status(proc.wait(timeout=seconds))
    except subprocess.TimeoutExpired:
        stop(proc, signal.SIGKILL if GROUP else signal.SIGTERM)
    try:
        proc.wait(timeout=REAP_SECONDS)
    except subprocess.TimeoutExpired:
        print(f"run_bounded: {cmd[0]} still running {REAP_SECONDS}s after the kill", file=sys.stderr)
    return 124


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
