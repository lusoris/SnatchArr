# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse any live container image digest pin that renovate.json leaves without age coverage.

Renovate 44.234.0's Docker datasource dates tags only on Docker Hub (`index.docker.io`,
including `docker.io`, `registry-1.docker.io` and host-less names). Under the `horistes`
preset's 3-day `minimumReleaseAge`, an undated pin on any other registry host (`mirror.gcr.io`,
`ghcr.io`, `lscr.io`, `mcr.microsoft.com`, `gcr.io`, `localhost`, `host:port`, `[ipv6]:port`)
never ages and is never proposed unless `renovate.json` covers it via a disabled
`minimumReleaseAge` (`null`, `""`, `"0"`, `"0 days"`),
`minimumReleaseAgeBehaviour: "timestamp-optional"`, or an `overridePackageName` resolving to a
dated Docker Hub name. Every live pin (dated included) is judged so an override from Docker Hub
to an undated registry is caught; `image-pin-check: record` files are skipped only while no
`customManagers` file pattern may read them.

Exit 0 = ok, 1 = input error, 3 = refused.
"""

from __future__ import annotations

import argparse
import json
import subprocess  # nosec B404  # runs child --judge under REGEX_DEADLINE and catches git SubprocessError
import sys
from collections import defaultdict
from collections.abc import Iterable, Iterator
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import image_pin_check
import renovate_rules
from renovate_rules import ConfigError, RefusedError, is_dated

MAX_CONFIG_BYTES = 1 << 20
REGEX_DEADLINE = 20
RENOVATE_FILE = "renovate.json"
CONFIG_FILES = frozenset({RENOVATE_FILE})
ADVICE = (
    "add a renovate.json packageRule (minimumReleaseAge null for media apps, "
    "overridePackageName to a Docker Hub twin, or timestamp-optional with the measurement) "
    "for each image above"
)


def decode_config(raw: bytes | None, label: str) -> str:
    """Decode RAW, the start of tracked file LABEL, as UTF-8 of at most MAX_CONFIG_BYTES.

    RAW comes from the same reader as the pins (working tree or index), so both modes judge
    one snapshot; a missing, untracked, binary or oversized file raises ConfigError.
    """
    if raw is None:
        raise ConfigError(f"{label}: missing, untracked or not a regular file")
    if b"\0" in raw:
        raise ConfigError(f"{label}: binary content is not allowed")
    if len(raw) > MAX_CONFIG_BYTES:
        raise ConfigError(f"{label}: exceeds {MAX_CONFIG_BYTES} bytes")
    return raw.decode("utf-8")


def keep_configs(
    contents: Iterable[image_pin_check.Content], configs: dict[str, bytes | None]
) -> Iterator[image_pin_check.Content]:
    """Yield CONTENTS unchanged and keep the raw content of each CONFIG_FILES path in CONFIGS."""
    for rel, raw in contents:
        if rel in CONFIG_FILES:
            configs[rel] = raw
        yield rel, raw


def collect_live_pins(
    contents: Iterable[image_pin_check.Content],
) -> tuple[dict[str, list[str]], set[str]]:
    """Map each live image name in CONTENTS to `file:line` locations and collect record files."""
    found: dict[str, list[str]] = defaultdict(list)
    records: set[str] = set()
    for rel, lineno, name, _tag, _digest, record in image_pin_check.iter_pins(contents):
        if record:
            records.add(rel)
            continue
        found[name].append(f"{rel}:{lineno}")
    return found, records


def parse_judge_payload(raw: bytes) -> tuple[str | dict[str, object], list[str], list[str]]:
    """Validate `--judge` JSON request bytes into `(config, names, records)`."""
    payload = json.loads(raw.decode("utf-8"))
    if not isinstance(payload, dict):
        raise ConfigError("invalid judge payload: top-level value must be an object")
    config = payload["config"]
    names = payload["names"]
    records = payload.get("records", [])
    if not isinstance(config, (str, dict)):
        raise ConfigError("invalid judge payload: config must be a string or object")
    if not isinstance(names, list) or not all(isinstance(item, str) for item in names):
        raise ConfigError("invalid judge payload: names must be a list of strings")
    if not isinstance(records, list) or not all(isinstance(item, str) for item in records):
        raise ConfigError("invalid judge payload: records must be a list of strings")
    return config, names, records


def run_judge_mode() -> int:
    """Read `{"config", "names", "records"}` JSON on stdin, evaluate rules and print JSON verdict."""
    raw = sys.stdin.buffer.read((MAX_CONFIG_BYTES * 2) + 1)
    if len(raw) > MAX_CONFIG_BYTES * 2:
        json.dump({"status": "config_error", "message": "judge input exceeds bound"}, sys.stdout)
        return 0
    try:
        config, names, records = parse_judge_payload(raw)
        uncovered = renovate_rules.judge(config, names, records)
    except RefusedError as refused:
        json.dump({"status": "refused", "message": str(refused)}, sys.stdout)
    except ConfigError as error:
        json.dump({"status": "config_error", "message": str(error)}, sys.stdout)
    except (KeyError, TypeError, ValueError) as error:
        json.dump({"status": "config_error", "message": f"invalid judge payload: {error}"}, sys.stdout)
    else:
        json.dump({"status": "ok", "uncovered": uncovered}, sys.stdout)
    return 0


def judge_in_child(config_text: str, names: list[str], records: list[str]) -> list[str]:
    """Run pure `renovate_rules.judge` in an isolated child process bounded by REGEX_DEADLINE."""
    request = json.dumps({"config": config_text, "names": names, "records": records}).encode("utf-8")
    try:
        proc = subprocess.run(  # nosec B603  # fixed argv invoking own --judge mode, no shell
            [sys.executable, "-I", str(Path(__file__).resolve()), "--judge"],
            input=request,
            capture_output=True,
            timeout=REGEX_DEADLINE,
            check=False,
        )
    except subprocess.TimeoutExpired as exc:
        raise ConfigError(
            f"rule evaluation exceeded {REGEX_DEADLINE} s (a pattern backtracks)"
        ) from exc
    if proc.returncode != 0:
        detail = proc.stderr.decode("utf-8", errors="replace").strip()
        raise ConfigError(f"judge subprocess exited {proc.returncode}: {detail}")
    verdict = json.loads(proc.stdout.decode("utf-8"))
    if not isinstance(verdict, dict):
        raise ConfigError("judge subprocess returned malformed verdict")
    status = verdict.get("status")
    if status == "refused":
        raise RefusedError(str(verdict["message"]))
    if status == "config_error":
        raise ConfigError(str(verdict["message"]))
    if status != "ok" or not isinstance(verdict.get("uncovered"), list):
        raise ConfigError("judge subprocess returned malformed verdict")
    return [str(item) for item in verdict["uncovered"]]


def check(root: Path, *, index: bool = False) -> int:
    """Refuse any live container image digest pin without Renovate release-age coverage.

    One pass of image_pin_check's reader yields the pins and `renovate.json`, so the
    working-tree mode and the index mode each judge a single snapshot.
    """
    configs: dict[str, bytes | None] = {}
    live_pins, records = collect_live_pins(
        keep_configs(image_pin_check.read_contents(root, index=index), configs)
    )
    config_text = decode_config(configs.get(RENOVATE_FILE), RENOVATE_FILE)
    names = sorted(live_pins)
    uncovered = judge_in_child(config_text, names, sorted(records))
    if uncovered:
        for name in uncovered:
            where = ", ".join(dict.fromkeys(live_pins[name]))
            print(f"image_release_age_check: {name} ({where})")
        print(ADVICE)
        return 3
    undated_count = sum(1 for name in names if not is_dated(name))
    print(
        f"image_release_age_check: ok ({len(names)} images checked, {undated_count} undated covered)"
    )
    return 0


def parse_args(argv: list[str] | None) -> argparse.Namespace:
    """Parse CLI options (`--root`, `--index`, internal `--judge`) from ARGV."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--root", type=Path, default=Path("."), help="repository root (default .)")
    parser.add_argument(
        "--index",
        action="store_true",
        help="scan git index content instead of working-tree files",
    )
    parser.add_argument("--judge", action="store_true", help=argparse.SUPPRESS)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    """Parse CLI arguments from ARGV and run the container image release-age coverage check."""
    args = parse_args(argv)
    if args.judge:
        return run_judge_mode()
    try:
        return check(args.root, index=args.index)
    except RefusedError as refused:
        print(f"image_release_age_check: {refused}")
        return 3
    except (ConfigError, OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"image_release_age_check: error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
