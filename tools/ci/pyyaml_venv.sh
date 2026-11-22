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
# Exit status: 0 installed and verified, 1 failed to install or import PyYAML, 2 usage.
# Needs python3 with venv and network.
set -euo pipefail

if [ $# -ne 1 ] || [ -z "$1" ]; then
  echo "usage: ${0##*/} DIR" >&2
  exit 2
fi

DIR="$1"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
PYYAML_REQ="$REPO_ROOT/tools/ci/pyyaml-requirements.txt"

if ! python3 -m venv "$DIR" || \
   ! "$DIR/bin/pip" install --quiet --disable-pip-version-check \
     --require-hashes --no-deps --only-binary :all: -r "$PYYAML_REQ" || \
   ! "$DIR/bin/python" -I -c 'import yaml'; then
  echo "error: failed to install PyYAML from tools/ci/pyyaml-requirements.txt" >&2
  exit 1
fi
