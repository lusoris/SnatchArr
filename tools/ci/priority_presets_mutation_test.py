#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2

"""Replay priority regressions against planted defects, restoring each source afterward.

Default: DB-free Go and UI tests. --integration additionally exercises the HTTP
apply path, database uniqueness and the 20-preset cap (requires test Postgres).
Backups and full command output remain under .workingdir/evidence/priority-mutations.
"""

import argparse
from pathlib import Path
import subprocess
import time


ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / ".workingdir/evidence/priority-mutations"
DOMAIN = "api/internal/domain/priority_named.go"
SERVICE = "api/internal/settings/priority_presets.go"
ORDER = "web/src/lib/priority-order.ts"
FIELDS = "web/src/lib/components/PriorityOrderFields.svelte"


def go_test(package, name, short=True):
    return ["go", "test", *(["-short"] if short else []), package, "-run", name, "-count=1"]


def web_test(file):
    return ["pnpm", "--dir", "web", "exec", "vitest", "run", file]


def cases(integration):
    unit = [
        ("name-bounds", DOMAIN, "n < 1 || n > 40", "n < 0",
         go_test("./api/internal/domain", "TestPriorityPresetInputName"), "--- FAIL: TestPriorityPresetInputName"),
        ("complete-maps", DOMAIN, "in.Search.Validate(); err != nil", "in.Search.Validate(); false",
         go_test("./api/internal/domain", "TestPriorityPresetInputMaps"), "--- FAIL: TestPriorityPresetInputMaps"),
        ("readonly-builtin", SERVICE, "strings.EqualFold(ref, domain.PresetMediaFirst)", "false",
         go_test("./api/internal/settings", "TestPriorityPresetReference"), "--- FAIL: TestPriorityPresetReference"),
        ("apply-copy", SERVICE, "maps.Clone(preset.Search)", "maps.Clone(in.SearchPriorityPreset)",
         go_test("./api/internal/settings", "TestSettingsWithPriorityPresetCopies"), "--- FAIL: TestSettingsWithPriorityPresetCopies"),
        ("rank-steps", ORDER, "10 * (index + 1)", "index + 1",
         web_test("src/lib/priority-order.test.ts"), "AssertionError"),
        ("explicit-ties", ORDER, "index > 0 && row.tied", "false",
         web_test("src/lib/priority-order.test.ts"), "AssertionError"),
        ("reorder-save", FIELDS, "preset = ranksFromRows(next);", "void ranksFromRows(next);",
         web_test("src/lib/components/CapsPresetsForm.svelte.test.ts"), "AssertionError"),
        ("reorder-reactive-identity", FIELDS,
         "preset = ranksFromRows(next);\n\t\tlastPreset = preset;",
         "lastPreset = ranksFromRows(next);\n\t\tpreset = lastPreset;",
         web_test("src/lib/components/CapsPresetsForm.svelte.test.ts"), "AssertionError"),
    ]
    if integration:
        unit.extend([
            ("http-apply-skips-copy", SERVICE, "s.Update(ctx, settingsWithPriorityPreset(in, preset))",
             "s.Update(ctx, settingsWithPriorityPreset(in, domain.NamedPriorityPreset{Name: preset.Name, Search: in.SearchPriorityPreset, Download: in.DownloadPriorityPreset}))",
             go_test("./api/internal/httpapi", "TestNamedPriorityPresetApplyCopiesHTTP", False),
             "--- FAIL: TestNamedPriorityPresetApplyCopiesHTTP"),
            ("twenty-first-save", SERVICE, "count >= domain.MaxPriorityPresets", "count > domain.MaxPriorityPresets",
             go_test("./api/internal/httpapi", "TestNamedPriorityPresetConflictAndCapHTTP", False),
             "--- FAIL: TestNamedPriorityPresetConflictAndCapHTTP"),
            ("case-insensitive-name", "api/internal/store/migrations/postgres/000053_priority_presets.up.sql",
             "ON priority_presets (lower(name))", "ON priority_presets (name)",
             go_test("./api/internal/httpapi", "TestNamedPriorityPresetConflictAndCapHTTP", False),
             "--- FAIL: TestNamedPriorityPresetConflictAndCapHTTP"),
        ])
    return unit


def wait_for_memory():
    meminfo = Path("/proc/meminfo")
    if not meminfo.exists():
        print("memory guard: /proc/meminfo unavailable on this platform", flush=True)
        return
    for _ in range(60):
        fields = dict(line.split(":", 1) for line in meminfo.read_text().splitlines())
        available = int(fields["MemAvailable"].split()[0])
        swap = int(fields["SwapFree"].split()[0])
        if available >= 12 * 1024 * 1024 and swap >= 15 * 1024 * 1024:
            return
        print("memory guard: waiting for shared RAM/swap headroom", flush=True)
        time.sleep(5)
    raise SystemExit("memory guard: insufficient shared headroom; no test started")


def replay(case):
    label, relative, before, after, command, failure = case
    path = ROOT / relative
    original = path.read_text()
    if original.count(before) != 1:
        raise SystemExit(f"{label}: expected exactly one mutation site")
    wait_for_memory()
    (EVIDENCE / f"{label}.source").write_text(original)
    try:
        path.write_text(original.replace(before, after, 1))
        result = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, timeout=180)
        output = result.stdout + result.stderr
        (EVIDENCE / f"{label}.log").write_text(f"command: {command!r}\nexit: {result.returncode}\n{output}")
        if result.returncode == 0 or failure not in output or "[build failed]" in output:
            raise SystemExit(f"{label}: regression did not reject the planted behavior; see {EVIDENCE}")
        print(f"{label}: rejected (exit {result.returncode})", flush=True)
    finally:
        path.write_text(original)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--integration", action="store_true", help="also run database-backed HTTP mutants")
    options = parser.parse_args()
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    for case in cases(options.integration):
        replay(case)


if __name__ == "__main__":
    main()
