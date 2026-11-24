#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Installs PyYAML pinned by version and hashes into a virtual environment at DIR, then checks
# that the environment's python imports yaml under -I.
# Platform (HISS-21): runs on Linux in CI and on maintainer hosts; requires bash and python3 venv.
#
#   tools/ci/pyyaml_venv.sh DIR
#
# PYYAML_VENV_DEADLINE_SECONDS (default 60, whole seconds from 1 to 9999) bounds the three setup
# steps together; pip install also gets a 15 s socket timeout and 2 retries.
# Exit status: 0 installed and verified, 1 failed to install or import PyYAML or the deadline
# passed, 2 usage or a deadline outside 1-9999.
# Needs python3 with venv and network.
set -euo pipefail

if [ $# -ne 1 ] || [ -z "$1" ]; then
  echo "usage: ${0##*/} DIR" >&2
  exit 2
fi

DEADLINE_SECONDS="${PYYAML_VENV_DEADLINE_SECONDS:-60}"
if ! [[ "$DEADLINE_SECONDS" =~ ^[1-9][0-9]{0,3}$ ]]; then
  echo "error: PYYAML_VENV_DEADLINE_SECONDS must be a whole number of seconds from 1 to 9999, got '$DEADLINE_SECONDS'" >&2
  exit 2
fi

DIR="$1"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
PYYAML_REQ="$REPO_ROOT/tools/ci/pyyaml-requirements.txt"

deadline_at=$((SECONDS + DEADLINE_SECONDS))

# run_step NAME CMD...: run CMD bounded by the seconds left before deadline_at, exiting 1 with a
# message naming NAME when the deadline passes or CMD fails.
run_step() {
  local name="$1" remaining rc=0
  shift
  remaining=$((deadline_at - SECONDS))
  if [ "$remaining" -gt 0 ]; then
    python3 -I "$HERE/run_bounded.py" "$remaining" "$@" || rc=$?
  else
    rc=124
  fi
  if [ "$rc" -eq 124 ]; then
    echo "error: PyYAML setup deadline of $DEADLINE_SECONDS s passed at $name" >&2
    exit 1
  fi
  if [ "$rc" -ne 0 ]; then
    echo "error: failed to install PyYAML from tools/ci/pyyaml-requirements.txt" >&2
    exit 1
  fi
}

run_step venv python3 -m venv "$DIR"
run_step "pip install" "$DIR/bin/pip" install --quiet --disable-pip-version-check \
  --timeout 15 --retries 2 --require-hashes --no-deps --only-binary :all: -r "$PYYAML_REQ"
run_step "import check" "$DIR/bin/python" -I -c 'import yaml'
