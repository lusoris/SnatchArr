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
# Needs docker (actionlint), python3 with venv, jq, network, token.
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
# zizmor now runs on the host and would read a second token from the environment; it gets $TOKEN only.
unset GITHUB_TOKEN

ACTIONLINT_IMAGE=""
# shellcheck source=/dev/null
. "$REPO_ROOT/tools/ci/tool-versions.env"
IMAGES=("$ACTIONLINT_IMAGE")
for image in "${IMAGES[@]}"; do
  if ! [[ "$image" =~ ^[a-z0-9./-]+:[A-Za-z0-9._-]+@sha256:[a-f0-9]{64}$ ]]; then
    echo "error: tools/ci/tool-versions.env: \"$image\" is not an image pinned by version and digest" >&2
    exit 1
  fi
done

ZIZMOR_REQ="$REPO_ROOT/tools/ci/zizmor-requirements.txt"
if [ ! -f "$ZIZMOR_REQ" ] || \
   ! grep -qE '^zizmor==[0-9]+(\.[0-9]+)+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?([[:space:]]*\\)?$' "$ZIZMOR_REQ" || \
   ! grep -qE -e '--hash=sha256:[a-f0-9]{64}' "$ZIZMOR_REQ"; then
  echo "error: tools/ci/zizmor-requirements.txt: zizmor is not pinned by version and hash" >&2
  exit 1
fi

shopt -s nullglob
FILES=()
for f in "$ROOT"/.github/workflows/*.yml "$ROOT"/.github/workflows/*.yaml; do
  FILES+=("${f#"$ROOT"/}")
done
if [ ${#FILES[@]} -eq 0 ]; then
  echo "error: no workflow file under $ROOT" >&2
  exit 1
fi

ACTION_FILES=()
for f in "$ROOT"/.github/actions/*/*.yml "$ROOT"/.github/actions/*/*.yaml; do
  ACTION_FILES+=("${f#"$ROOT"/}")
done

# go_install_pinned ARGS: whether every package after `go install` names a release (v + semver)
# or a workflow variable (${...}). @latest, a branch, or no version at all floats.
go_install_pinned() {
  local word ver seen=0 words
  read -r -a words <<< "$1"
  for word in "${words[@]}"; do
    case "$word" in
      '&&' | '||' | ';' | '|' | '#'*) break ;;
      -*) continue ;;
    esac
    word="${word//[\"\']/}"
    seen=1
    [[ "$word" == *@* ]] || return 1
    ver="${word##*@}"
    [[ "$ver" =~ ^\$\{[^}]+\}$ ]] && continue
    [[ "$ver" =~ ^v[0-9]+(\.[0-9]+)+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]] || return 1
  done
  [ "$seen" -eq 1 ]
}

# A Go tool installed at a floating version changes under an unchanged workflow; Renovate moves
# the pinned ones (renovate.json, manager for go install in workflows).
floating=0
for file in "${FILES[@]}" "${ACTION_FILES[@]}"; do
  lineno=0
  while IFS= read -r line || [ -n "$line" ]; do
    lineno=$((lineno + 1))
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    [[ "$line" =~ (^|[;\&|[:space:]])go[[:space:]]+install([[:space:]]+(.*)|$) ]] || continue
    if ! go_install_pinned "${BASH_REMATCH[3]}"; then
      echo "refused $file:$lineno: go install not pinned to a version" >&2
      floating=1
    fi
  done < "$ROOT/$file"
done
if [ "$floating" -ne 0 ]; then
  exit 1
fi

"$DOCKER" run --rm -v "$ROOT:/repo:ro" -w /repo \
  -v "$REPO_ROOT/.github/actionlint.yaml:/actionlint.yaml:ro" \
  "$ACTIONLINT_IMAGE" -no-color -config-file /actionlint.yaml "${FILES[@]}"
echo "actionlint: ${#FILES[@]} workflow files passed"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

ZIZMOR="${ZIZMOR_BIN:-}"
if [ -z "$ZIZMOR" ]; then
  if ! python3 -m venv "$WORK/zizmor" || \
     ! "$WORK/zizmor/bin/pip" install --quiet --disable-pip-version-check \
       --require-hashes --no-deps --only-binary :all: -r "$ZIZMOR_REQ"; then
    echo "error: failed to install zizmor from tools/ci/zizmor-requirements.txt" >&2
    exit 1
  fi
  ZIZMOR="$WORK/zizmor/bin/zizmor"
fi

rc=0
(cd "$ROOT" && GH_TOKEN="$TOKEN" "$ZIZMOR" --no-progress --color never --format json --persona pedantic \
  --no-ignores --no-config --strict-collection "${FILES[@]}" "${ACTION_FILES[@]}") \
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
if [ ${#ACTION_FILES[@]} -gt 0 ]; then
  echo "zizmor: every action in ${#FILES[@]} workflow files and ${#ACTION_FILES[@]} composite action files is pinned to a commit; no version comment names another tag"
else
  echo "zizmor: every action in ${#FILES[@]} workflow files is pinned to a commit; no version comment names another tag"
fi
