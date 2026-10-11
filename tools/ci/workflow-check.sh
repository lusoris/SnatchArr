#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Lints every workflow file and refuses an action that is not pinned to a commit, or whose
# version comment names a tag that is not that commit. Adapted from tools/workflow-check.sh
# of github.com/golusoris/goenvoy.
#
#   tools/ci/workflow-check.sh [ROOT]      (default: this repository)
#
# Exit status: 0 clean, 1 a finding or a failed run, 2 usage.
# Needs docker, jq, network and GH_TOKEN or GITHUB_TOKEN (read access is enough).
set -euo pipefail

if [ $# -gt 1 ]; then
  echo "usage: ${0##*/} [ROOT]" >&2
  exit 2
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ROOT="$(cd "${1:-$REPO_ROOT}" && pwd)"
DOCKER="${DOCKER_BIN:-docker}"

# zizmor compares a version comment with the upstream tags, which it reads only with a token;
# without one it drops that audit and still exits 0.
TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
if [ -z "$TOKEN" ]; then
  echo "error: GH_TOKEN or GITHUB_TOKEN is required: without a token the version comments are not checked" >&2
  exit 1
fi

ACTIONLINT_IMAGE=""
ZIZMOR_IMAGE=""
# shellcheck source=/dev/null
. "$REPO_ROOT/tools/ci/tool-versions.env"
for image in "$ACTIONLINT_IMAGE" "$ZIZMOR_IMAGE"; do
  if ! [[ "$image" =~ ^[a-z0-9./-]+:[A-Za-z0-9._-]+@sha256:[a-f0-9]{64}$ ]]; then
    echo "error: tools/ci/tool-versions.env: \"$image\" is not an image pinned by version and digest" >&2
    exit 1
  fi
done

shopt -s nullglob
FILES=()
for f in "$ROOT"/.github/workflows/*.yml "$ROOT"/.github/workflows/*.yaml; do
  FILES+=("${f#"$ROOT"/}")
done
if [ ${#FILES[@]} -eq 0 ]; then
  echo "error: no workflow file under $ROOT" >&2
  exit 1
fi

"$DOCKER" run --rm -v "$ROOT:/repo:ro" -w /repo \
  -v "$REPO_ROOT/.github/actionlint.yaml:/actionlint.yaml:ro" \
  "$ACTIONLINT_IMAGE" -no-color -config-file /actionlint.yaml "${FILES[@]}"
echo "actionlint: ${#FILES[@]} workflow files passed"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
rc=0
GH_TOKEN="$TOKEN" "$DOCKER" run --rm -e GH_TOKEN -v "$ROOT:/repo:ro" -w /repo \
  "$ZIZMOR_IMAGE" --no-progress --color never --format json --persona pedantic \
  --no-ignores --no-config --strict-collection "${FILES[@]}" \
  > "$WORK/report.json" 2> "$WORK/zizmor.log" || rc=$?
# 0 = no finding, 11 to 14 = findings by highest severity; anything else is a failed run.
case "$rc" in
  0 | 11 | 12 | 13 | 14) ;;
  *)
    cat "$WORK/zizmor.log" >&2
    echo "error: zizmor exit $rc" >&2
    exit 1
    ;;
esac
# A run that wrote no report must not read as "no finding".
if ! jq -e -s 'length == 1 and (.[0] | type == "array")' "$WORK/report.json" > /dev/null; then
  cat "$WORK/zizmor.log" >&2
  echo "error: zizmor wrote no report" >&2
  exit 1
fi

# One line per pin finding: "<verdict> <file>:<line>: <audit>: <annotation>". Only the two pin
# audits are read; zizmor's other audits are not adopted here yet. A comment that names a branch
# cannot decide: the branch moves upstream without a change here, and the next update pull
# request is what moves the pin. Such a pin is printed and passes.
jq -r '
  .[]
  | select(.ident == "unpinned-uses" or .ident == "ref-version-mismatch")
  | (.locations | map(select(.symbolic.kind == "Primary")) | .[0]) as $at
  | (if .ident == "ref-version-mismatch" and ($at.symbolic.annotation | startswith("branch points to commit "))
     then "note" else "refused" end) as $verdict
  | "\($verdict) \($at.symbolic.key.Local.verbatim_path):\($at.concrete.location.start_point.row + 1): \(.ident): \($at.symbolic.annotation)"
' "$WORK/report.json" > "$WORK/findings.txt"

sed -n '/^note /p' "$WORK/findings.txt"
if grep '^refused ' "$WORK/findings.txt" >&2; then
  echo "error: pin every action to a commit and keep its version comment on that commit's tag" >&2
  exit 1
fi
echo "zizmor: every action in ${#FILES[@]} workflow files is pinned to a commit; no version comment names another tag"
