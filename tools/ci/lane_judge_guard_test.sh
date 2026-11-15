#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
# Planted lane edits in isolated throwaway repositories; real lane markers untouched.
set -euo pipefail

# Strip every inherited Git setting before any fixture command, then keep both
# config isolation barriers for the shell and all its descendants.
if [ "${1-}" != --isolated ]; then
    python3 -I - "$0" <<'PY'
import os
import subprocess
import sys

fixture_env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
fixture_env["GIT_CONFIG_GLOBAL"] = os.devnull
fixture_env["GIT_CONFIG_NOSYSTEM"] = "1"
result = subprocess.run(
    ["bash", sys.argv[1], "--isolated", sys.executable], env=fixture_env, check=False, timeout=30
)
sys.exit(result.returncode)
PY
    exit $?
fi
python=$2

HERE=$(cd "$(dirname "$0")" && pwd)
check=$HERE/lane_judge_guard.py
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"

label=snatcharr-guard-fixture
launched=2026-11-13T21:00:00+01:00
ended=2026-11-13T22:00:00+01:00
path='lane output.txt'

# marker: write only the throwaway repository's launcher fixture.
marker() {
    printf '{"label":"%s","runs":"%s/runs.tsv","launched_at":"%s"}\n' \
        "$label" "$PWD" "$launched" > .git/lane-judge.json
    printf '%s\t%s\texit\trc=0\tseconds=3600\n' "$ended" "$label" > runs.tsv
}

# stamp TIME: use GNU touch where present, stdlib timestamps on other platforms.
stamp() {
    if touch -d "$1" "$path" 2>/dev/null; then return; fi
    "$python" -I - "$1" "$path" <<'PY'
import os
import sys
from datetime import datetime

stamp = datetime.fromisoformat(sys.argv[1]).timestamp()
os.utime(sys.argv[2], (stamp, stamp))
PY
}

# staged TIME: plant a staged file with a controlled mtime.
staged() {
    printf '%s\n' 'lane output' > "$path"
    git --literal-pathspecs add -- "$path"
    stamp "$1"
}

# expect NAME RC NEEDLE SETUP: assert status and diagnostic in a fresh fixture.
expect() {
    local name=$1 want=$2 needle=$3 setup=$4 out status=0
    new_repo "$work/$name"
    (cd "$work/$name" && "$setup")
    out=$(cd "$work/$name" && "$python" -I "$check" 2>&1) || status=$?
    check_case "$name" "$want" "$status" "$needle" "$out"
    if [ "$want" -eq 1 ]; then
        case "$out" in
            *'send the change back to a lane fix round; the judge does not edit lane output') ;;
            *) printf 'FAIL %s: missing refusal instruction\n%s\n' "$name" "$out" >&2; exit 1 ;;
        esac
    fi
    if [ "$want" -eq 0 ] && [ -z "$needle" ] && [ -n "$out" ]; then
        printf 'FAIL %s: expected silence, got %s\n' "$name" "$out" >&2
        exit 1
    fi
    echo "ok   $name (rc=$status)"
}

# setup_none: ordinary worktrees need no lane check.
setup_none() { staged 2026-11-13T22:10:00+01:00; }

# setup_running: a model row is no exit row.
setup_running() {
    marker
    printf '%s\t%s\tcordana/auto\n' "$ended" "$label" > runs.tsv
}

# setup_old_exit: an earlier run of the same label cannot end this run.
setup_old_exit() {
    marker
    printf '2026-11-13T20:59:59+01:00\t%s\texit\trc=0\n' "$label" > runs.tsv
}

# setup_early: lane output predates the exit.
setup_early() { marker; staged 2026-11-13T21:50:00+01:00; }

# setup_late: the planted judge edit occurs ten minutes after exit.
setup_late() { marker; staged 2026-11-13T22:10:00+01:00; }

# setup_boundary: the full 120-second grace is permitted.
setup_boundary() { marker; staged 2026-11-13T22:02:00+01:00; }

# setup_over_boundary: the next second is refused.
setup_over_boundary() { marker; staged 2026-11-13T22:02:01+01:00; }

# setup_unstaged: early mtime cannot hide further unstaged content.
setup_unstaged() {
    setup_early
    printf '%s\n' 'judge changed this' >> "$path"
    stamp 2026-11-13T21:50:00+01:00
}

# setup_merge: only the presence of MERGE_HEAD exempts conflict resolution.
setup_merge() { setup_late; printf '%040d\n' 0 > .git/MERGE_HEAD; }

# setup_extra: extra marker keys fail closed.
setup_extra() {
    marker
    printf '{"label":"x","runs":"%s/runs.tsv","launched_at":"%s","extra":true}\n' \
        "$PWD" "$launched" > .git/lane-judge.json
}

# setup_not_json: malformed launcher state fails closed.
setup_not_json() { marker; printf '%s\n' 'not JSON' > .git/lane-judge.json; }

# setup_delete: a staged deletion has no worktree mtime to check.
setup_delete() {
    setup_early
    git commit -qm fixture
    git rm -q -- "$path"
}

# field KEY JSON: change one marker field in the throwaway fixture.
field() {
    "$python" -I - "$1" "$2" <<'PY'
import json
import sys
from pathlib import Path

path = Path('.git/lane-judge.json')
marker = json.loads(path.read_text())
marker[sys.argv[1]] = json.loads(sys.argv[2])
path.write_text(json.dumps(marker))
PY
}

# setup_wrong_type: strings are required, even for an otherwise valid marker.
setup_wrong_type() { marker; field label 17; }

# setup_bad_time: unparsable launch times fail closed.
setup_bad_time() { marker; field launched_at '"bad time"'; }

# setup_naive_time: local timestamps cannot be compared with offset timestamps.
setup_naive_time() { marker; field launched_at '"2026-11-13T21:00:00"'; }

# setup_naive_exit_time: exit timestamps without an offset fail closed.
setup_naive_exit_time() {
    marker
    printf '2026-11-13T21:00:00\t%s\texit\trc=0\tseconds=3600\n' "$label" > runs.tsv
}

# setup_relative_runs: the launcher must name an absolute runs file.
setup_relative_runs() { marker; field runs '"runs.tsv"'; }

# setup_missing_runs: disappearing launcher history is an error, not a pass.
setup_missing_runs() { marker; rm runs.tsv; }

# setup_unreadable_marker: a marker directory cannot be read as JSON.
setup_unreadable_marker() { marker; rm .git/lane-judge.json; mkdir .git/lane-judge.json; }

# setup_latest_exit: maximum time wins, irrespective of row order or other labels.
setup_latest_exit() {
    marker
    printf '%s\n' \
        $'2026-11-13T21:00:00+00:00\tsnatcharr-guard-fixture\texit\trc=124' \
        $'2026-11-13T21:10:00+01:00\tsnatcharr-guard-fixture\texit\trc=0' \
        $'2026-11-13T23:00:00+01:00\tother-label\texit\trc=0' > runs.tsv
    staged 2026-11-13T22:02:00+01:00
}

# setup_magic_path: pathspec syntax in a filename must not hide unstaged edits.
setup_magic_path() {
    local path=':(exclude)*'
    setup_unstaged
}

# setup_row_bound: a full row bound is valid, but one additional row fails closed.
setup_row_bound() {
    marker
    "$python" -I - <<'PY'
from pathlib import Path

path = Path('runs.tsv')
path.write_text(path.read_text() + '\n' * 199999)
PY
}

# setup_over_row_bound: no matching exit can bypass the input row limit.
setup_over_row_bound() { setup_row_bound; printf '\n' >> runs.tsv; }

# setup_over_path_bound: 5001 staged additions exceed the inspection bound.
setup_over_path_bound() {
    marker
    "$python" -I - <<'PY'
from pathlib import Path

for number in range(5001):
    Path(f'file-{number}').touch()
PY
    git add -- file-*
}

# setup_zulu: trailing Z UTC timestamps parse consistently across Python 3.10 and newer.
setup_zulu() {
    marker
    field launched_at '"2026-11-13T20:00:00Z"'
    printf '2026-11-13T21:00:00Z\t%s\texit\trc=0\tseconds=3600\n' "$label" > runs.tsv
    staged 2026-11-13T22:02:00+01:00
}

# setup_zulu_late: reject a judge edit after the same UTC exit instant in Z form.
setup_zulu_late() {
    marker
    field launched_at '"2026-11-13T20:00:00Z"'
    printf '2026-11-13T21:00:00Z\t%s\texit\trc=0\tseconds=3600\n' "$label" > runs.tsv
    staged 2026-11-13T22:10:00+01:00
}

expect case-1-no-marker 0 '' setup_none
expect case-2-no-exit 1 "lane $label has no exit row after $launched; wait for it to end" setup_running
expect case-3-old-exit 1 'has no exit row after' setup_old_exit
expect case-4-early 0 '' setup_early
expect case-5-judge-edit 1 "path '$path' mtime" setup_late
expect case-6-boundary-120 0 '' setup_boundary
expect case-6-boundary-121 1 'exceeds exit time' setup_over_boundary
expect case-7-unstaged 1 'has an unstaged change' setup_unstaged
expect case-8-merge 0 "merge commit, conflict resolution is the judge's job" setup_merge
expect case-9-extra-key 2 'must have exactly keys' setup_extra
expect case-9-not-json 2 'lane judge guard:' setup_not_json
expect case-10-deletion 0 '' setup_delete
expect wrong-type 2 'must be strings' setup_wrong_type
expect bad-time 2 'bad time' setup_bad_time
expect naive-time 2 'UTC offset required' setup_naive_time
expect naive-exit-time 2 'UTC offset required' setup_naive_exit_time
expect zulu-time 0 '' setup_zulu
expect zulu-judge-edit 1 "path '$path' mtime" setup_zulu_late
expect relative-runs 2 'must be an absolute path' setup_relative_runs
expect missing-runs 2 'lane runs file missing' setup_missing_runs
expect unreadable-marker 2 'lane judge guard:' setup_unreadable_marker
expect latest-exit 0 '' setup_latest_exit
expect literal-path 1 'has an unstaged change' setup_magic_path
expect row-bound 0 '' setup_row_bound
expect over-row-bound 2 'exceeds 200000 rows' setup_over_row_bound
expect over-path-bound 2 'staged paths exceed 5000' setup_over_path_bound

root=$(cd "$HERE/../.." && pwd)
work_abs=$(cd "$work" && pwd)
lefthook=${LANE_JUDGE_LEFTHOOK:-$root/lefthook.yml}

cat > "$work_abs/hook_yaml.py" <<'PY'
"""Indentation-scoped stdlib reader for lefthook pre-commit wiring checks."""

from __future__ import annotations

import sys
from pathlib import Path

CAP = 10000


def fail(msg: str) -> None:
    """Print a case-11-wiring failure diagnostic and exit 1."""
    print(f"FAIL case-11-wiring: {msg}", file=sys.stderr)
    sys.exit(1)


def load_entries(path: Path) -> list[tuple[int, str]]:
    """Read bounded non-comment YAML lines with their leading-space indentation."""
    if not path.is_file():
        fail(f"missing lefthook file {path}")
    entries: list[tuple[int, str]] = []
    try:
        with path.open(encoding="utf-8") as handle:
            for _line_no in range(CAP):
                raw = handle.readline()
                if not raw:
                    return entries
                line = raw.rstrip("\r\n")
                st = line.lstrip(" ")
                if not st or st.startswith("#") or st == "---":
                    continue
                if "\t" in line[: len(line) - len(st)]:
                    fail(f"tab indentation in {path}")
                entries.append((len(line) - len(st), st))
            if handle.read(1):
                fail(f"{path} exceeds {CAP} lines")
    except (OSError, UnicodeDecodeError) as error:
        fail(f"{path}: {error}")
    return entries


def pmap(blk: list[tuple[int, str]]) -> dict[str, tuple[str, list[tuple[int, str]]]]:
    """Parse one indentation level of YAML mapping keys into (scalar, child_lines)."""
    out: dict[str, tuple[str, list[tuple[int, str]]]] = {}
    idx = 0
    for _step in range(CAP):
        if idx >= len(blk):
            return out
        ind, txt = blk[idx]
        if ind != blk[0][0] or ":" not in txt or txt.startswith("-"):
            fail(f"expected mapping key at {txt!r}")
        key, rest = txt.split(":", 1)
        name = key.strip()
        if name in out:
            fail(f"duplicate key {name!r}")
        idx, sub = idx + 1, []
        for _sub in range(CAP):
            if idx >= len(blk) or blk[idx][0] < ind or (blk[idx][0] == ind and not blk[idx][1].startswith("-")):
                break
            sub.append(blk[idx])
            idx += 1
        val = rest.split(" #", 1)[0].split("\t#", 1)[0].strip()
        val = "" if val.startswith("#") else val
        out[name] = (val[1:-1] if len(val) >= 2 and val[0] == val[-1] and val[0] in ("'", '"') else val, sub)
    return out


def pseq(blk: list[tuple[int, str]]) -> list[dict[str, tuple[str, list[tuple[int, str]]]]]:
    """Split a YAML block sequence into parsed mapping dictionaries."""
    items: list[list[tuple[int, str]]] = []
    for ind, txt in blk[:CAP]:
        if blk and ind == blk[0][0] and (txt == "-" or txt.startswith("- ")):
            rem, body = txt[1:], txt[1:].lstrip(" ")
            items.append([(ind + 1 + len(rem) - len(body), body)] if body and not body.startswith("#") else [])
        elif items and ind > blk[0][0]:
            items[-1].append((ind, txt))
        else:
            fail(f"expected sequence item at {txt!r}")
    return [pmap(item) for item in items]
PY

# read_hook_run FILE: verify piped format-then-guard ordering and print lane-judge-guard run.
read_hook_run() {
    "$python" -I - "$work_abs" "$1" <<'PY'
import sys
from pathlib import Path

sys.path.insert(0, sys.argv[1])
from hook_yaml import CAP, fail, load_entries, pmap, pseq  # noqa: E402


def guard_run(path: Path) -> str:
    """Validate pre-commit format-then-guard structure and return lane-judge-guard run."""
    top = pmap(load_entries(path))
    if "pre-commit" not in top:
        fail(f"missing pre-commit section in {path}")
    pc = pmap(top["pre-commit"][1])
    if "commands" in pc:
        cmds = pmap(pc["commands"][1])
        for moved in ("gofmt", "lane-judge-guard"):
            if moved in cmds:
                fail(f"{moved} must not appear under pre-commit.commands in {path}")
    if "jobs" not in pc or not pc["jobs"][1]:
        fail(f"missing pre-commit.jobs in {path}")
    top_jobs = pseq(pc["jobs"][1])
    for job in top_jobs:
        if job.get("name", ("", []))[0] in ("gofmt", "lane-judge-guard"):
            fail(f"{job['name'][0]} must be inside format-then-guard in {path}")
    groups = [job for job in top_jobs if job.get("name", ("", []))[0] == "format-then-guard"]
    if len(groups) != 1:
        fail(f"expected one format-then-guard job in pre-commit.jobs in {path}")
    outer = groups[0]
    if "glob" in outer:
        fail(f"format-then-guard must not set glob in {path}")
    if "group" not in outer or not outer["group"][1]:
        fail(f"format-then-guard missing group in {path}")
    grp = pmap(outer["group"][1])
    if grp.get("piped", ("", []))[0] != "true":
        fail(f"format-then-guard group must set piped: true in {path}")
    if "jobs" not in grp or not grp["jobs"][1]:
        fail(f"format-then-guard group missing jobs in {path}")
    nested = pseq(grp["jobs"][1])
    names = [job.get("name", ("", []))[0] for job in nested]
    if names.count("lane-judge-guard") != 1:
        fail(f"missing lane-judge-guard job in format-then-guard in {path}")
    if names.count("gofmt") != 1:
        fail(f"missing gofmt job in format-then-guard in {path}")
    gofmt_idx, guard_idx = names.index("gofmt"), names.index("lane-judge-guard")
    if guard_idx <= gofmt_idx:
        fail(f"lane-judge-guard must come after gofmt in format-then-guard in {path}")
    if nested[gofmt_idx].get("stage_fixed", ("", []))[0] != "true":
        fail(f"gofmt job must set stage_fixed: true in {path}")
    rval, rsub = nested[guard_idx].get("run", ("", []))
    run_line = " ".join(t for _i, t in rsub[:CAP]) if rval in (">-", ">") else rval
    if not run_line:
        fail(f"missing lane-judge-guard run line in {path}")
    return run_line


print(guard_run(Path(sys.argv[2])))
PY
}

# load_hook_args FILE: read lane-judge-guard arguments from FILE and resolve the guard path.
load_hook_args() {
    local run_line rest arg saw_guard=0
    local -a raw_args=()
    if ! run_line=$(read_hook_run "$1"); then
        exit 1
    fi
    if [ -z "$run_line" ]; then
        printf 'FAIL case-11-wiring: missing lane-judge-guard run line in %s\n' "$1" >&2
        exit 1
    fi
    case "$run_line" in
        'sh .config/lefthook/python.sh '*)
            rest=${run_line#'sh .config/lefthook/python.sh '}
            ;;
        *)
            printf 'FAIL case-11-wiring: unexpected lane-judge-guard run line: %s\n' "$run_line" >&2
            exit 1
            ;;
    esac
    IFS=$' \t' read -r -a raw_args <<< "$rest"
    hook_args=()
    for arg in "${raw_args[@]}"; do
        if [ "$arg" = tools/ci/lane_judge_guard.py ]; then
            hook_args+=("$root/$arg")
            saw_guard=1
        else
            hook_args+=("$arg")
        fi
    done
    if [ "$saw_guard" -ne 1 ]; then
        printf 'FAIL case-11-wiring: run line does not name tools/ci/lane_judge_guard.py: %s\n' \
            "$run_line" >&2
        exit 1
    fi
}

# setup_tracked_late: commit an initial revision, write exit state, then edit after exit.
setup_tracked_late() {
    staged 2026-11-13T21:50:00+01:00
    git commit -qm fixture
    marker
    printf '%s\n' 'judge edited this' > "$path"
    stamp 2026-11-13T22:10:00+01:00
}

# index_no_marker INDEX: stage an ordinary path in a temporary index with no lane marker.
index_no_marker() {
    printf '%s\n' 'ordinary worktree edit' > "$path"
    GIT_INDEX_FILE="$1" git --literal-pathspecs add -- "$path"
}

# index_all_tracked INDEX: stage tracked edits in a temporary index like commit -a.
index_all_tracked() {
    setup_tracked_late
    cp .git/index "$1"
    GIT_INDEX_FILE="$1" git add -u
    git diff --cached --quiet
}

# index_path_limited INDEX: stage one path in a temporary index built from HEAD.
index_path_limited() {
    setup_tracked_late
    GIT_INDEX_FILE="$1" git read-tree HEAD
    GIT_INDEX_FILE="$1" git --literal-pathspecs add -- "$path"
    git diff --cached --quiet
}

# expect_wired NAME RC NEEDLE SETUP ARGS...: run python.sh against a temporary index.
expect_wired() {
    local name=$1 want=$2 needle=$3 setup=$4 idx="$work_abs/$1.index" py_dir out status=0
    py_dir=$(dirname "$python")
    shift 4
    new_repo "$work/$name"
    (cd "$work/$name" && "$setup" "$idx")
    out=$(
        cd "$work/$name" &&
            PATH="$py_dir:$PATH" GIT_INDEX_FILE="$idx" sh "$root/.config/lefthook/python.sh" "$@" 2>&1
    ) || status=$?
    check_case "$name" "$want" "$status" "$needle" "$out"
    if [ "$want" -eq 1 ]; then
        case "$out" in
            *'send the change back to a lane fix round; the judge does not edit lane output') ;;
            *) printf 'FAIL %s: missing refusal instruction\n%s\n' "$name" "$out" >&2; exit 1 ;;
        esac
    fi
    if [ "$want" -eq 0 ] && [ -z "$needle" ] && [ -n "$out" ]; then
        printf 'FAIL %s: expected silence, got %s\n' "$name" "$out" >&2
        exit 1
    fi
    echo "ok   $name (rc=$status)"
}

# run_floor_python: exercise python.sh and the guard under the available floor Python.
run_floor_python() {
    local floor_raw=${LANE_JUDGE_FLOOR_PYTHON-} floor_dir="$work_abs/floor-bin"
    local floor_exec want_info got_info="" got_rc=0 py_dir
    if [ "${LANE_JUDGE_FLOOR_PYTHON+x}" = x ]; then
        if [ -z "$floor_raw" ]; then
            echo 'skip floor python: LANE_JUDGE_FLOOR_PYTHON empty'
            return
        fi
    else
        if ! floor_raw=$(command -v python3.10); then
            echo 'skip floor python: no python3.10 on PATH and LANE_JUDGE_FLOOR_PYTHON unset'
            return
        fi
        printf 'floor python: %s\n' "$floor_raw"
    fi
    if ! floor_exec=$("$floor_raw" -I -c 'import sys; print(sys.executable)' 2>/dev/null) || [ -z "$floor_exec" ]; then
        printf 'FAIL floor-python: cannot execute %s\n' "$floor_raw" >&2
        exit 1
    fi
    want_info=$("$floor_exec" -I -c 'import os, sys; print(sys.version_info[:2], os.path.realpath(sys.executable))')
    mkdir -p "$floor_dir"
    ln -sf "$floor_exec" "$floor_dir/python3"
    local python="$floor_dir/python3"
    py_dir=$(dirname "$python")
    got_info=$(
        PATH="$py_dir:$PATH" sh "$root/.config/lefthook/python.sh" -I -c \
            'import os, sys; print(sys.version_info[:2], os.path.realpath(sys.executable))' 2>&1
    ) || got_rc=$?
    if [ "$got_rc" -ne 0 ]; then
        printf 'FAIL floor-python: python.sh exit %s: %s\n' "$got_rc" "$got_info" >&2
        exit 1
    fi
    if [ "$got_info" != "$want_info" ]; then
        printf 'FAIL floor-python: python.sh ran %s, want %s\n' "$got_info" "$want_info" >&2
        exit 1
    fi
    expect_wired floor-no-marker 0 '' index_no_marker "${hook_args[@]}"
    expect_wired floor-judge-edit 1 "path '$path' mtime" index_all_tracked "${hook_args[@]}"
    expect floor-zulu-time 0 '' setup_zulu
    expect floor-zulu-judge-edit 1 "path '$path' mtime" setup_zulu_late
}

# expect_wiring_mutant NAME FILE NEEDLE: assert malformed hook wiring fails case-11-wiring.
expect_wiring_mutant() {
    local name=$1 file=$2 needle=$3 out status=0
    out=$(load_hook_args "$file" 2>&1) || status=$?
    check_case "$name" 1 "$status" 'FAIL case-11-wiring:' "$out"
    check_case "$name" 1 "$status" "$needle" "$out"
    echo "ok   $name (rc=$status)"
}

load_hook_args "$lefthook"
expect_wired case-11-wiring 1 "path '$path' mtime" index_all_tracked "${hook_args[@]}"
expect_wired case-11b-path-limited 1 "path '$path' mtime" index_path_limited "${hook_args[@]}"
run_floor_python

# Disable only the mtime comparison; case 5 must now be wrongly permitted.
sed 's/if mtime > ended.timestamp() + GRACE_SECONDS:/if False:/' "$check" > "$work/mutant.py"
check=$work/mutant.py
expect mutant-case-5 0 '' setup_late
echo 'mutant: case 5 passes under mutant -> test bites'

# Remove --keep-index from the wired arguments; case 11 must now be wrongly permitted.
mutant_args=()
for arg in "${hook_args[@]}"; do
    if [ "$arg" != --keep-index ]; then
        mutant_args+=("$arg")
    fi
done
expect_wired mutant-case-11 0 '' index_all_tracked "${mutant_args[@]}"
echo 'mutant: case 11 passes without --keep-index -> wiring test bites'

# Planted lefthook.yml wiring mutations: missing guard, guard before gofmt, unpiped group.
grep -v 'piped: true' "$root/lefthook.yml" > "$work/unpiped.yml"
expect_wiring_mutant mutant-wiring-unpiped "$work/unpiped.yml" 'piped: true'
"$python" -I - "$root/lefthook.yml" "$work/no-guard.yml" "$work/swapped.yml" <<'PY'
import sys
from pathlib import Path

text = Path(sys.argv[1]).read_text(encoding="utf-8")
gofmt_block = (
    "          - name: gofmt\n"
    '            glob: "*.go"\n'
    "            run: gofmt -w {staged_files}\n"
    "            stage_fixed: true\n"
)
guard_block = (
    "          - name: lane-judge-guard\n"
    "            # The guard must read the index being recorded (option -a, path-limited), which git passes only in GIT_INDEX_FILE.\n"
    "            run: sh .config/lefthook/python.sh --keep-index -I tools/ci/lane_judge_guard.py\n"
)
if gofmt_block + guard_block not in text:
    sys.exit(2)
Path(sys.argv[2]).write_text(text.replace(guard_block, "", 1), encoding="utf-8")
Path(sys.argv[3]).write_text(text.replace(gofmt_block + guard_block, guard_block + gofmt_block, 1), encoding="utf-8")
PY
expect_wiring_mutant mutant-wiring-no-guard "$work/no-guard.yml" 'missing lane-judge-guard job'
expect_wiring_mutant mutant-wiring-order "$work/swapped.yml" 'after gofmt'

# no_make_guard FILE: refuse a Makefile recipe line that runs the live guard. The guard reads the
# worktree's own lane marker and refuses while a lane runs even with nothing staged, so only the
# lefthook pre-commit job may run it; the recipe keeps the fixture test (case 12). A line counts
# when it is indented or continues a line ending in a backslash; make passes a continued recipe
# to the shell as one command, and a `#` comment ends at the newline, so the next line still runs.
no_make_guard() {
    if awk '
        { live = cont || /^[[:space:]]/; cont = /\\$/; code = $0; sub(/#.*/, "", code) }
        live && code ~ /tools\/ci\/lane_judge_guard\.py/ { print FNR ":" $0; hit = 1 }
        END { exit !hit }' "$1"; then
        echo "FAIL case-12-make-wiring: $1 runs the live guard; only the pre-commit hook may" >&2
        return 1
    fi
}

# expect_make_wiring NAME RC NEEDLE FILE: assert no_make_guard status and diagnostic for FILE.
expect_make_wiring() {
    local name=$1 want=$2 needle=$3 file=$4 out status=0
    out=$(no_make_guard "$file" 2>&1) || status=$?
    check_case "$name" "$want" "$status" "$needle" "$out"
    echo "ok   $name (rc=$status)"
}

expect_make_wiring case-12-make-wiring 0 '' "${LANE_JUDGE_MAKEFILE:-$root/Makefile}"
printf '%s\n' 'workflows-verify:' '	bash tools/ci/lane_judge_guard_test.sh' \
    '	python3 -I tools/ci/lane_judge_guard.py' > "$work/live-guard.mk"
expect_make_wiring mutant-make-live-guard 1 'FAIL case-12-make-wiring:' "$work/live-guard.mk"
printf '%s\n' 'workflows-verify:' '	bash tools/ci/lane_judge_guard_test.sh' \
    '	# python3 -I tools/ci/lane_judge_guard.py' > "$work/commented-guard.mk"
expect_make_wiring make-commented-guard 0 '' "$work/commented-guard.mk"
printf 'workflows-verify:\n\tpython3 -I \\\n\t    tools/ci/lane_judge_guard.py\n' > "$work/continued-guard.mk"
expect_make_wiring mutant-make-continued-guard 1 'FAIL case-12-make-wiring:' "$work/continued-guard.mk"
printf 'workflows-verify:\n\tpython3 -I \\\ntools/ci/lane_judge_guard.py\n' > "$work/unindented-guard.mk"
expect_make_wiring mutant-make-unindented-guard 1 'FAIL case-12-make-wiring:' "$work/unindented-guard.mk"
printf 'workflows-verify:\n\t# python3 -I \\\n\ttools/ci/lane_judge_guard.py\n' > "$work/comment-continued-guard.mk"
expect_make_wiring mutant-make-comment-continued-guard 1 'FAIL case-12-make-wiring:' "$work/comment-continued-guard.mk"
echo 'mutant: live guard in a Makefile recipe fails case 12 -> wiring test bites'
