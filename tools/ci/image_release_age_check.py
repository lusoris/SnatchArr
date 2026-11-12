# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Refuse any live container image pin or Docker capture left without Renovate age coverage.

Renovate 44.234.0's Docker datasource dates tags only on Docker Hub (`index.docker.io`,
`docker.io`, `registry-1.docker.io` and host-less names). Under a 3-day `minimumReleaseAge`, an
undated image on another registry host never ages unless `renovate.json` disables
`minimumReleaseAge`, sets `minimumReleaseAgeBehaviour: "timestamp-optional"`, or overrides
`packageName` to a dated Docker Hub name (`enabled: false` skips lookup rather than covering).
Pure evaluation runs in `renovate_rules.py`, `renovate_managers.py` and `renovate_regex.py` under
`REGEX_DEADLINE` over live pins, Docker `custom.regex` `depName` captures, and `record` pins
(`image-pin-check: record` files are excluded only when every `extends` preset is modeled and no
enabled built-in or custom manager pattern may read them).

Exit 0 = ok, 1 = input error, 3 = refused.
"""

from __future__ import annotations

import argparse
import base64
import binascii
import json
import subprocess  # nosec B404  # runs child --judge under REGEX_DEADLINE and catches git SubprocessError
import sys
from collections import defaultdict
from collections.abc import Iterable, Iterator
from dataclasses import dataclass
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import image_pin_check
import renovate_managers
import renovate_rules
from renovate_regex import ascii_lower
from renovate_rules import ConfigError, JudgeResult, RefusedError, is_dated

MAX_CONFIG_BYTES = 1 << 20
MAX_MANAGED_FILES = 256
MAX_MANAGED_BYTES = 4 << 20
# Stdin bound for `--judge`: JSON-escaped config (<= 6 * MAX_CONFIG_BYTES) + base64 of kept
# files (4 * ceil(n / 3) <= (MAX_MANAGED_BYTES * 4) // 3 + 4 * MAX_MANAGED_FILES) + 2 MiB for
# pin/record/search lists and JSON framing.
MAX_JUDGE_INPUT_BYTES = (
    (MAX_CONFIG_BYTES * 6)
    + ((MAX_MANAGED_BYTES * 4) // 3)
    + (MAX_MANAGED_FILES * 4)
    + (2 << 20)
)
REGEX_DEADLINE = 20
RENOVATE_FILE = "renovate.json"
CONFIG_FILES = frozenset({RENOVATE_FILE})
ADVICE = (
    "add a renovate.json packageRule (minimumReleaseAge null for media apps, "
    "overridePackageName to a Docker Hub twin, or timestamp-optional with the measurement) "
    "for each image above"
)


@dataclass(frozen=True)
class JudgeRequest:
    """Validated `--judge` child request payload."""

    config: str | dict[str, object]
    names: list[str]
    records: list[str]
    record_names: list[str]
    searched: list[str]
    files: dict[str, bytes]


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
    contents: Iterable[image_pin_check.Content],
    configs: dict[str, bytes | None],
    snapshot: set[renovate_managers.SnapshotEntry],
) -> Iterator[image_pin_check.Content]:
    """Yield CONTENTS unchanged while recording CONFIG_FILES bytes and `(path, sha256)` entries."""
    for rel, raw in contents:
        snapshot.add(renovate_managers.snapshot_entry(rel, raw))
        if rel in CONFIG_FILES:
            configs[rel] = raw
        yield rel, raw


def collect_live_pins(
    contents: Iterable[image_pin_check.Content],
) -> tuple[dict[str, list[str]], dict[str, list[str]], set[str]]:
    """Map live and record image names in CONTENTS to `file:line` locations and record paths."""
    found: dict[str, list[str]] = defaultdict(list)
    record_pins: dict[str, list[str]] = defaultdict(list)
    records: set[str] = set()
    for rel, lineno, name, _tag, _digest, record in image_pin_check.iter_pins(contents):
        if record:
            records.add(rel)
            record_pins[name].append(f"{rel}:{lineno}")
            continue
        found[name].append(f"{rel}:{lineno}")
    return found, record_pins, records


def read_wanted_files(
    root: Path,
    wanted: set[str],
    pass1_snapshot: set[renovate_managers.SnapshotEntry],
    *,
    index: bool = False,
) -> dict[str, bytes]:
    """Read exact `customManagers` paths in pass 2 and verify the tree snapshot stayed identical."""
    pass2_snapshot: set[renovate_managers.SnapshotEntry] = set()
    kept: dict[str, bytes] = {}
    unreadable: list[str] = []
    total_bytes = 0
    for rel, raw in image_pin_check.read_contents(root, index=index):
        pass2_snapshot.add(renovate_managers.snapshot_entry(rel, raw))
        if ascii_lower(rel) not in wanted:
            continue
        if raw is None or len(raw) > image_pin_check.MAX_BYTES:
            unreadable.append(rel)
            continue
        if len(kept) >= MAX_MANAGED_FILES:
            raise ConfigError(f"customManagers match more than {MAX_MANAGED_FILES} files")
        total_bytes += len(raw)
        if total_bytes > MAX_MANAGED_BYTES:
            raise ConfigError(f"customManagers matched files exceed {MAX_MANAGED_BYTES} bytes")
        kept[rel] = raw
    renovate_managers.check_same_snapshot(pass1_snapshot, pass2_snapshot)
    if unreadable:
        raise RefusedError(
            f"customManagers file {unreadable[0]} cannot be read like Renovate reads it"
        )
    return kept


def parse_payload_list(
    payload: dict[str, object], key: str, *, required: bool = False
) -> list[str]:
    """Validate string-list field KEY on `--judge` PAYLOAD."""
    raw = payload[key] if required else payload.get(key, [])
    if not isinstance(raw, list) or not all(isinstance(item, str) for item in raw):
        raise ConfigError(f"invalid judge payload: {key} must be a list of strings")
    return raw


def parse_payload_files(payload: dict[str, object]) -> dict[str, bytes]:
    """Decode `{path: base64(raw)}` from `--judge` PAYLOAD into raw byte buffers."""
    raw_files = payload.get("files", {})
    if not isinstance(raw_files, dict):
        raise ConfigError("invalid judge payload: files must be an object")
    decoded: dict[str, bytes] = {}
    for path, b64 in raw_files.items():
        if not isinstance(path, str) or not isinstance(b64, str):
            raise ConfigError("invalid judge payload: files entries must map string to base64")
        try:
            decoded[path] = base64.b64decode(b64.encode("ascii"), validate=True)
        except (UnicodeEncodeError, binascii.Error) as exc:
            raise ConfigError(f"invalid judge payload: bad base64 for {path!r}") from exc
    return decoded


def parse_judge_payload(raw: bytes) -> JudgeRequest:
    """Validate `--judge` JSON request bytes into a `JudgeRequest`."""
    payload = json.loads(raw.decode("utf-8"))
    if not isinstance(payload, dict):
        raise ConfigError("invalid judge payload: top-level value must be an object")
    config = payload["config"]
    if not isinstance(config, (str, dict)):
        raise ConfigError("invalid judge payload: config must be a string or object")
    return JudgeRequest(
        config=config,
        names=parse_payload_list(payload, "names", required=True),
        records=parse_payload_list(payload, "records"),
        record_names=parse_payload_list(payload, "record_names"),
        searched=parse_payload_list(payload, "searched"),
        files=parse_payload_files(payload),
    )


def run_judge_mode() -> int:
    """Read `--judge` JSON on stdin, evaluate rules and print a JSON verdict."""
    raw = sys.stdin.buffer.read(MAX_JUDGE_INPUT_BYTES + 1)
    if len(raw) > MAX_JUDGE_INPUT_BYTES:
        json.dump({"status": "config_error", "message": "judge input exceeds bound"}, sys.stdout)
        return 0
    try:
        req = parse_judge_payload(raw)
        result = renovate_rules.judge_full(
            req.config,
            req.names,
            req.records,
            record_names=req.record_names,
            searched=req.searched,
            files=req.files,
        )
    except RefusedError as refused:
        json.dump({"status": "refused", "message": str(refused)}, sys.stdout)
    except ConfigError as error:
        json.dump({"status": "config_error", "message": str(error)}, sys.stdout)
    except (KeyError, TypeError, ValueError) as error:
        json.dump({"status": "config_error", "message": f"invalid judge payload: {error}"}, sys.stdout)
    else:
        json.dump(
            {
                "status": "ok",
                "uncovered": list(result.uncovered),
                "judged_pins": list(result.judged_pins),
                "unmodeled_preset": result.unmodeled_preset,
                "captures": {k: list(v) for k, v in result.uncovered_captures.items()},
            },
            sys.stdout,
        )
    return 0


def check_ok_verdict_shape(verdict: dict[str, object]) -> None:
    """Refuse malformed `"ok"` verdict payloads from the `--judge` child process."""
    list_keys = ("uncovered", "judged_pins")
    lists_ok = all(isinstance(verdict.get(key), list) for key in list_keys)
    unmodeled = verdict.get("unmodeled_preset")
    preset_ok = unmodeled is None or isinstance(unmodeled, str)
    if verdict.get("status") != "ok" or not lists_ok or not preset_ok:
        raise ConfigError("judge subprocess returned malformed verdict")
    if not isinstance(verdict.get("captures"), dict):
        raise ConfigError("judge subprocess returned malformed verdict")


def decode_verdict(verdict: object) -> JudgeResult:
    """Validate the JSON verdict returned by the `--judge` child process."""
    if not isinstance(verdict, dict):
        raise ConfigError("judge subprocess returned malformed verdict")
    status = verdict.get("status")
    if status == "refused":
        raise RefusedError(str(verdict["message"]))
    if status == "config_error":
        raise ConfigError(str(verdict["message"]))
    check_ok_verdict_shape(verdict)
    uncovered: list[object] = verdict["uncovered"]  # type: ignore[assignment]
    judged_pins: list[object] = verdict["judged_pins"]  # type: ignore[assignment]
    unmodeled: str | None = verdict.get("unmodeled_preset")  # type: ignore[assignment]
    raw_caps: dict[object, object] = verdict["captures"]  # type: ignore[assignment]
    captures = {
        str(k): tuple(str(loc) for loc in locs)
        for k, locs in raw_caps.items()
        if isinstance(locs, list)
    }
    return JudgeResult(
        uncovered=tuple(str(item) for item in uncovered),
        judged_pins=tuple(str(item) for item in judged_pins),
        unmodeled_preset=unmodeled,
        uncovered_captures=captures,
    )


def judge_in_child(request: JudgeRequest) -> JudgeResult:
    """Run pure `renovate_rules.judge_full` in an isolated child bounded by REGEX_DEADLINE."""
    encoded_files = {
        path: base64.b64encode(raw).decode("ascii") for path, raw in sorted(request.files.items())
    }
    payload = json.dumps({
        "config": request.config,
        "names": request.names,
        "records": request.records,
        "record_names": request.record_names,
        "searched": request.searched,
        "files": encoded_files,
    }).encode("utf-8")
    try:
        proc = subprocess.run(  # nosec B603  # fixed argv invoking own --judge mode, no shell
            [sys.executable, "-I", str(Path(__file__).resolve()), "--judge"],
            input=payload,
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
    return decode_verdict(json.loads(proc.stdout.decode("utf-8")))


def format_locations(
    name: str,
    live_pins: dict[str, list[str]],
    record_pins: dict[str, list[str]],
    result: JudgeResult,
) -> str:
    """Format deduplicated `live, record-judged-live, capture` locations for uncovered NAME."""
    locs: list[str] = list(live_pins.get(name, ()))
    if result.unmodeled_preset is not None:
        note = f" (record pin judged live: extends entry {result.unmodeled_preset} is not modeled)"
        locs.extend(f"{loc}{note}" for loc in record_pins.get(name, ()))
    locs.extend(result.uncovered_captures.get(name, ()))
    return ", ".join(dict.fromkeys(locs))


def check(root: Path, *, index: bool = False) -> int:
    """Refuse any live pin, judged record pin or Docker capture without release-age coverage."""
    configs: dict[str, bytes | None] = {}
    pass1_snapshot: set[renovate_managers.SnapshotEntry] = set()
    live_pins, record_pins, records = collect_live_pins(
        keep_configs(image_pin_check.read_contents(root, index=index), configs, pass1_snapshot)
    )
    config_text = decode_config(configs.get(RENOVATE_FILE), RENOVATE_FILE)
    wanted = renovate_managers.wanted_manager_paths(config_text)
    files = read_wanted_files(root, wanted, pass1_snapshot, index=index) if wanted else {}
    result = judge_in_child(
        JudgeRequest(
            config=config_text,
            names=sorted(live_pins),
            records=sorted(records),
            record_names=sorted(record_pins),
            searched=sorted(wanted),
            files=files,
        )
    )
    if result.uncovered:
        for name in result.uncovered:
            where = format_locations(name, live_pins, record_pins, result)
            print(f"image_release_age_check: {name} ({where})")
        print(ADVICE)
        return 3
    judged = result.judged_pins
    undated_count = sum(1 for name in judged if not is_dated(name))
    print(
        f"image_release_age_check: ok ({len(judged)} images checked, {undated_count} undated covered)"
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
