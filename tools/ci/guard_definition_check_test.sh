#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for the merge-time workflow-refs guard check. First runs
# tools/ci/guard_definition_check_test.py (the comparison cases for
# guard_definition_check.py), then holds tools/ci/guard_definition_check.sh, the commit-msg
# entry, to these cases against a bare file:// origin whose refs/pull/<N>/merge refs are real
# merge commits of a pull-request branch into main. Every case runs from a single-branch clone
# of that origin, the way the merge script runs commit-msg from a default-branch clone.
# Offline; every git and script call is bounded.
#
#   tools/ci/guard_definition_check_test.sh
#
# Positive: pull request 7 edits README only (unchanged), a '#' comment line before the subject
# is skipped (comment-subject), a subject without (#N) passes silently (no-pr-number), so do
# (#0) (pr-number-zero), (#N) inside the subject (pr-number-mid-subject) and an eight-digit
# number (pr-number-eight-digits), pull request 8 drops a guard step and carries a valid
# GUARD_DEFINITION_ACK (ack-valid), GUARD_DEFINITION_FETCH_SECONDS at its bounds 1 and 9999
# (deadline-env-1, deadline-env-9999).
# Negative: no MSGFILE or two arguments (usage, usage-two-args), a MSGFILE that does not exist
# (unreadable-msgfile), a clone without origin (no-origin), pull request 8 drops a guard step
# (step-removed), refs/pull/9/merge is a plain commit (not-a-merge), refs/pull/10/merge does
# not exist (merge-ref-missing), PYTHON_BIN with a space (python-bin-word),
# GUARD_DEFINITION_FETCH_SECONDS 0, 10000, 05 and abc (deadline-env-0, deadline-env-10000,
# deadline-env-05, deadline-env-abc), a git whose fetch never ends past a 1 s deadline
# (fetch-deadline), and GIT_DIR, GIT_INDEX_FILE and GIT_OBJECT_DIRECTORY of the clone exported
# as a git hook sees them, after which the clone holds no object of the fetched merge (hook-env).
# Boundary: the largest number the subject rule reads, (#9999999) (pr-number-max).
# Platform (HISS-21): Linux and macOS; requires bash, git and python3. An explicit PYTHON_BIN
# must import yaml; without one, ensure_pyyaml_python (test_lib.sh) takes host python3 when it
# imports yaml, else builds .cache/pyyaml-venv with tools/ci/pyyaml_venv.sh (needs the package
# index). Exit status: 0 every case passed; 1 a case failed.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
CHECK="$HERE/guard_definition_check.sh"
ensure_pyyaml_python
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
CASES=0
WORKFLOW='name: Workflow refs
on:
  pull_request:
jobs:
  workflow-refs:
    runs-on: ubuntu-24.04
    steps:
      - name: Planted cases
        run: bash tools/ci/workflow_refs_test.sh
      - name: Check
        run: bash tools/ci/workflow_refs.sh'
NO_STEP='name: Workflow refs
on:
  pull_request:
jobs:
  workflow-refs:
    runs-on: ubuntu-24.04
    steps:
      - name: Check
        run: bash tools/ci/workflow_refs.sh'

# g ARGS...: run git with ARGS under a 30 s deadline.
g() {
  run_bounded 30 git "$@"
}

# bounded_new_repo DIR: run test_lib.sh new_repo DIR in a child bash under a 30 s deadline.
bounded_new_repo() {
  # shellcheck disable=SC2016 # the child bash expands $1 and $2, its own positional arguments
  run_bounded 30 bash -c '. "$1/test_lib.sh" && new_repo "$2"' bounded_new_repo "$HERE" "$1"
}

# pr_branch REPO N WORKFLOW_TEXT: on a branch pr-N off main, write WORKFLOW_TEXT and a README
# line naming N, commit, and print nothing.
pr_branch() {
  g -C "$1" checkout -q -b "pr-$2" main
  printf '%s\n' "$3" > "$1/.github/workflows/workflow-refs.yml"
  printf 'readme %s\n' "$2" >> "$1/README.md"
  g -C "$1" commit -q -a -m "pr $2"
}

# push_merge REPO N: merge branch pr-N into a moved copy of main with --no-ff and push the merge
# commit as refs/pull/N/merge.
push_merge() {
  g -C "$1" checkout -q --detach main
  g -C "$1" commit -q --allow-empty -m "main moves past pr $2"
  g -C "$1" merge -q --no-ff --no-edit "pr-$2"
  g -C "$1" push -q origin "HEAD:refs/pull/$2/merge"
}

# build_origin: create $WORK/origin.git with main holding the guard set, refs/pull/7/merge and
# refs/pull/9999999/merge leaving the guard unchanged, refs/pull/8/merge dropping a guard step
# and refs/pull/9/merge pointing at a plain commit; then clone main alone to $WORK/clone and
# record the object id of refs/pull/7/merge in MERGE7.
build_origin() {
  local src="$WORK/src"
  g init -q --bare "$WORK/origin.git"
  bounded_new_repo "$src"
  mkdir -p "$src/.github/workflows" "$src/tools/ci"
  printf '%s\n' "$WORKFLOW" > "$src/.github/workflows/workflow-refs.yml"
  printf 'bash tools/ci/workflow_refs.sh --planted\n' > "$src/tools/ci/workflow_refs_test.sh"
  printf 'echo check\n' > "$src/tools/ci/workflow_refs.sh"
  printf 'readme\n' > "$src/README.md"
  g -C "$src" add -A
  g -C "$src" commit -q -m base
  g -C "$src" remote add origin "file://$WORK/origin.git"
  g -C "$src" push -q origin main
  pr_branch "$src" 7 "$WORKFLOW"
  push_merge "$src" 7
  MERGE7="$(g -C "$src" rev-parse HEAD)"
  g -C "$src" push -q origin "HEAD:refs/pull/9999999/merge"
  pr_branch "$src" 8 "$NO_STEP"
  push_merge "$src" 8
  pr_branch "$src" 9 "$WORKFLOW"
  g -C "$src" push -q origin "pr-9:refs/pull/9/merge"
  g clone -q --single-branch --branch main "file://$WORK/origin.git" "$WORK/clone"
}

# run_check NAME SUBJECT [VAR=VALUE...]: write SUBJECT to a message file, run the entry script
# from $WORK/clone under a 40 s deadline with the extra environment, and set RC and OUT.
run_check() {
  local msg="$WORK/$1.msg"
  printf '%s\n\nbody line\n' "$2" > "$msg"
  shift 2
  RC=0
  OUT="$(cd "$WORK/clone" && env "$@" python3 -I "$HERE/run_bounded.py" 40 bash "$CHECK" "$msg" 2>&1)" || RC=$?
}

# expect NAME WANT_RC NEEDLE SUBJECT [VAR=VALUE...]: run the entry script on SUBJECT and hold
# its exit status to WANT_RC and its output to NEEDLE (check_case).
expect() {
  local name="$1" want="$2" needle="$3"
  shift 3
  run_check "$name" "$@"
  check_case "$name" "$want" "$RC" "$needle" "$OUT"
  CASES=$((CASES + 1))
  echo "ok   $name"
}

# expect_silent NAME SUBJECT [VAR=VALUE...]: run the entry script on SUBJECT and require exit 0
# with no output (the subject carries no pull request number).
expect_silent() {
  local name="$1"
  shift
  run_check "$name" "$@"
  check_case "$name" 0 "$RC" "" "$OUT"
  if [ -n "$OUT" ]; then
    printf 'FAIL %s: want no output\n%s\n' "$name" "$OUT" >&2
    exit 1
  fi
  CASES=$((CASES + 1))
  echo "ok   $name"
}

# expect_args NAME WANT_RC NEEDLE ARGS...: run the entry script with ARGS as given and hold its
# exit status and output.
expect_args() {
  local name="$1" want="$2" needle="$3" rc=0 out
  shift 3
  out="$(run_bounded 40 bash "$CHECK" "$@" 2>&1)" || rc=$?
  check_case "$name" "$want" "$rc" "$needle" "$out"
  CASES=$((CASES + 1))
  echo "ok   $name"
}

# no_origin_case: run the entry script from a repository without origin and require exit 1.
no_origin_case() {
  local repo="$WORK/no-origin" rc=0 out
  bounded_new_repo "$repo"
  printf 'fix: x (#7)\n' > "$WORK/no-origin.msg"
  out="$(cd "$repo" && run_bounded 40 bash "$CHECK" "$WORK/no-origin.msg" 2>&1)" || rc=$?
  check_case no-origin 1 "$rc" "guard-definition: no origin remote" "$out"
  CASES=$((CASES + 1))
  echo "ok   no-origin"
}

# fetch_deadline_case: put a git first on PATH whose fetch sleeps 30 s, run with a 1 s fetch
# deadline and require the deadline message within the 40 s case bound.
fetch_deadline_case() {
  local bin="$WORK/slow-git"
  mkdir -p "$bin"
  {
    printf '#!/usr/bin/env bash\nreal=%q\n' "$(command -v git)"
    cat << 'WRAP'
for arg in "$@"; do
  if [ "$arg" = fetch ]; then exec sleep 30; fi
done
exec "$real" "$@"
WRAP
  } > "$bin/git"
  chmod +x "$bin/git"
  expect fetch-deadline 1 "fetch of refs/pull/7/merge passed the 1 s deadline" 'fix: x (#7)' \
    "PATH=$bin:$PATH" GUARD_DEFINITION_FETCH_SECONDS=1
}

# hook_env_case: export the clone's GIT_DIR, GIT_INDEX_FILE and GIT_OBJECT_DIRECTORY as a git
# hook sees them, require the unchanged pass, then require that the clone lacks MERGE7.
hook_env_case() {
  local gitdir="$WORK/clone/.git"
  expect hook-env 0 "leaves the workflow-refs guard unchanged" 'fix: x (#7)' \
    "GIT_DIR=$gitdir" "GIT_INDEX_FILE=$gitdir/index" "GIT_OBJECT_DIRECTORY=$gitdir/objects"
  if g -C "$WORK/clone" cat-file -e "$MERGE7^{commit}" 2> /dev/null; then
    printf 'FAIL hook-env: the clone holds the fetched merge %s\n' "$MERGE7" >&2
    exit 1
  fi
}

# main: run the comparison cases, build the origin, then every entry case in the header order.
main() {
  run_bounded 300 "$PYTHON_BIN" -I "$HERE/guard_definition_check_test.py" "$HERE/guard_definition_check.py"
  build_origin
  local same="leaves the workflow-refs guard unchanged"
  expect unchanged 0 "$same" 'fix: x (#7)'
  expect comment-subject 0 "$same" "$(printf '# Please enter the message\n#\nfix: x (#7)')"
  expect_silent no-pr-number 'fix: x'
  expect_silent pr-number-zero 'fix: x (#0)'
  expect_silent pr-number-mid-subject 'fix: (#7) x'
  expect_silent pr-number-eight-digits 'fix: x (#12345678)'
  expect ack-valid 0 "change acknowledged for pull request 8" 'fix: x (#8)' \
    'GUARD_DEFINITION_ACK=8: reviewed guard edit in this test'
  expect deadline-env-1 1 "refs/pull/10/merge" 'fix: x (#10)' GUARD_DEFINITION_FETCH_SECONDS=1
  expect deadline-env-9999 0 "$same" 'fix: x (#7)' GUARD_DEFINITION_FETCH_SECONDS=9999
  expect_args usage 2 "usage: guard_definition_check.sh MSGFILE"
  expect_args usage-two-args 2 "usage: guard_definition_check.sh MSGFILE" a b
  expect_args unreadable-msgfile 2 "guard-definition: cannot read" "$WORK/absent.msg"
  no_origin_case
  expect step-removed 1 ".github/workflows/workflow-refs.yml: changed by pull request 8" 'fix: x (#8)'
  expect not-a-merge 1 "refs/pull/9/merge is not a merge commit" 'fix: x (#9)'
  expect merge-ref-missing 1 "cannot fetch refs/pull/10/merge" 'fix: x (#10)'
  expect python-bin-word 2 "PYTHON_BIN must be a command name" 'fix: x (#7)' 'PYTHON_BIN=python3 -c 1'
  local bad
  for bad in 0 10000 05 abc; do
    expect "deadline-env-$bad" 2 "seconds from 1 to 9999, got '$bad'" 'fix: x (#7)' \
      "GUARD_DEFINITION_FETCH_SECONDS=$bad"
  done
  fetch_deadline_case
  hook_env_case
  expect pr-number-max 0 "pull request 9999999 leaves the workflow-refs guard unchanged" 'fix: x (#9999999)'
  echo "guard_definition_check_test: $CASES cases passed"
}

main
