# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Require the runnable Semgrep command in each implementation lane brief stage."""

import pathlib
import sys

import yaml

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

from semgrep_scan import read_bounded_text

COMMAND = "python3 -I tools/ci/semgrep_scan.py --semgrep semgrep"
MAX_BRIEF_BYTES = 64 * 1024


def main():
    """Check the two stage lists in one lane brief YAML file."""
    if len(sys.argv) != 2:
        print("usage: lane_brief_wiring.py FILE", file=sys.stderr)
        return 2
    try:
        data = yaml.safe_load(
            read_bounded_text(pathlib.Path(sys.argv[1]), MAX_BRIEF_BYTES)
        )
    except (OSError, UnicodeError, yaml.YAMLError) as err:
        print(f"lane brief unreadable: {err}", file=sys.stderr)
        return 1
    stages = data.get("required_by_stage") if isinstance(data, dict) else None
    for stage in ("impl", "fix"):
        lines = stages.get(stage) if isinstance(stages, dict) else None
        if not isinstance(lines, list) or COMMAND not in lines:
            print(f"lane brief missing {stage}: {COMMAND}", file=sys.stderr)
            return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
