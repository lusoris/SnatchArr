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

# read_hook_run FILE: print the run command under lane-judge-guard: using plain-text matching.
read_hook_run() {
    if [ ! -f "$1" ]; then
        return 1
    fi
    awk '
        { sub(/\r$/, "") }
        /^[[:space:]]*#/ || /^[[:space:]]*$/ { next }
        in_job {
            match($0, /^[[:space:]]*/)
            if (RLENGTH <= job_indent) {
                in_job = 0
            } else if ($0 ~ /^[[:space:]]*run:[[:space:]]*/) {
                sub(/^[[:space:]]*run:[[:space:]]*/, "")
                sub(/[[:space:]]*$/, "")
                print
                exit
            }
        }
        !in_job && /^[[:space:]]*lane-judge-guard:[[:space:]]*(#.*)?$/ {
            match($0, /^[[:space:]]*/)
            job_indent = RLENGTH
            in_job = 1
        }
    ' "$1"
}

# load_hook_args FILE: read lane-judge-guard arguments from FILE and resolve the guard path.
load_hook_args() {
    local run_line rest arg saw_guard=0
    local -a raw_args=()
    if ! run_line=$(read_hook_run "$1") || [ -z "$run_line" ]; then
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

load_hook_args "$lefthook"
expect_wired case-11-wiring 1 "path '$path' mtime" index_all_tracked "${hook_args[@]}"
expect_wired case-11b-path-limited 1 "path '$path' mtime" index_path_limited "${hook_args[@]}"

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
