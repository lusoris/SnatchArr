#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Lints every workflow file and refuses an action that is not pinned to a commit, whose
# version comment names a tag that is not that commit, or whose repository has moved to
# another name. Adapted from tools/workflow-check.sh of github.com/golusoris/goenvoy (now
# github.com/plurano/goenvoy).
#
#   tools/ci/workflow-check.sh [ROOT]      (default: this repository)
#
# Exit status: 0 clean, 1 a finding or a failed run, 2 usage.
# Needs docker (actionlint), python3 with venv, jq, curl, network, token.
set -euo pipefail

if [ $# -gt 1 ]; then
  echo "usage: ${0##*/} [ROOT]" >&2
  exit 2
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

PYTHON="${PYTHON_BIN:-python3}"
if ! "$PYTHON" -I "$HERE/workflow_uses.py" --list-actions "$ROOT" -- "${FILES[@]}" > "$WORK/actions.nul"; then
  exit 1
fi
MAX_ACTION_FILES=4096
ACTION_FILES=()
mapfile -d '' -n "$((MAX_ACTION_FILES + 1))" -t ACTION_FILES < "$WORK/actions.nul"
if [ "${#ACTION_FILES[@]}" -gt "$MAX_ACTION_FILES" ]; then
  echo "error: more than $MAX_ACTION_FILES composite action files under $ROOT" >&2
  exit 1
fi

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
if ! "$PYTHON" -I "$HERE/workflow_uses.py" --step-text "$ROOT" -- "${FILES[@]}" "${ACTION_FILES[@]}" > "$WORK/step-text.tsv"; then
  exit 1
fi
MAX_STEP_LINES=262144
step_line_count=0
floating=0
while IFS= read -r record || [ -n "$record" ]; do
  step_line_count=$((step_line_count + 1))
  if [ "$step_line_count" -gt "$MAX_STEP_LINES" ]; then
    echo "error: more than $MAX_STEP_LINES step text lines under $ROOT" >&2
    exit 1
  fi
  loc="${record%%$'\t'*}"
  line="${record#*$'\t'}"
  file="${loc%:*}"
  lineno="${loc##*:}"
  [[ "$line" =~ ^[[:space:]]*# ]] && continue
  [[ "$line" =~ (^|[;\&|[:space:]])go[[:space:]]+install([[:space:]]+(.*)|$) ]] || continue
  if ! go_install_pinned "${BASH_REMATCH[3]}"; then
    echo "refused $file:$lineno: go install not pinned to a version" >&2
    floating=1
  fi
done < "$WORK/step-text.tsv"

# Actions that install a tool and fall back to a floating default when `with: version` is
# missing. azure/setup-helm asks the GitHub API for the newest release and, when that call
# fails, installs its built-in default: 3.18.4 broke `helm plugin install --verify` on main
# 6a83713996. Allow-list: an action listed here must carry a semver `version` in its step.
VERSION_INPUT_ACTIONS=(azure/setup-helm)

# step_version_pinned INDEX COL: whether the step whose `uses:` key sits at column COL on
# STEP_LINES[INDEX] sets `version:` to a release. The step ends at the first non-empty line
# indented less than COL.
step_version_pinned() {
  local i line indent
  for ((i = $1 + 1; i < ${#STEP_LINES[@]}; i++)); do
    line="${STEP_LINES[i]}"
    [[ "$line" =~ ^[[:space:]]*$ ]] && continue
    indent="${line%%[![:space:]]*}"
    [ "${#indent}" -lt "$2" ] && return 1
    [[ "$line" =~ ^[[:space:]]+version:[[:space:]]*[\"\']?v?[0-9]+(\.[0-9]+)+[\"\']?[[:space:]]*(#.*)?$ ]] && return 0
  done
  return 1
}

for file in "${FILES[@]}" "${ACTION_FILES[@]}"; do
  mapfile -t STEP_LINES < "$ROOT/$file"
  for ((idx = 0; idx < ${#STEP_LINES[@]}; idx++)); do
    [[ "${STEP_LINES[idx]}" =~ ^([[:space:]]*(-[[:space:]]+)?)uses:[[:space:]]*([^@[:space:]]+)@ ]] || continue
    for action in "${VERSION_INPUT_ACTIONS[@]}"; do
      [ "${BASH_REMATCH[3]}" = "$action" ] || continue
      if ! step_version_pinned "$idx" "${#BASH_REMATCH[1]}"; then
        echo "refused $file:$((idx + 1)): $action without a pinned version input" >&2
        floating=1
      fi
    done
  done
done
if [ "$floating" -ne 0 ]; then
  exit 1
fi

# Check that every remote `uses:` repository in workflows and composite actions still carries its
# current owner/name (shared with .github/workflows/workflow-refs.yml).
GH_TOKEN="$TOKEN" bash "$HERE/workflow_refs.sh" "$ROOT"

"$DOCKER" run --rm -v "$ROOT:/repo:ro" -w /repo \
  -v "$REPO_ROOT/.github/actionlint.yaml:/actionlint.yaml:ro" \
  "$ACTIONLINT_IMAGE" -no-color -config-file /actionlint.yaml "${FILES[@]}"
echo "actionlint: ${#FILES[@]} workflow files passed"

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

# One line per finding: "<verdict> <file>:<line>: <audit>: <annotation>". Read: the two pin
# audits and template-injection (a ${{ }} expression expanded into a run script, at any
# severity: pass it through env instead). zizmor's other audits are not adopted here yet. A
# comment that names a branch cannot decide: the branch moves upstream without a change here,
# and the next update pull request is what moves the pin. Such a pin is printed and passes.
jq -r '
  .[]
  | select(.ident == "unpinned-uses" or .ident == "ref-version-mismatch" or .ident == "template-injection")
  | (.locations | map(select(.symbolic.kind == "Primary")) | .[0]) as $at
  | (if .ident == "ref-version-mismatch" and ($at.symbolic.annotation | startswith("branch points to commit "))
     then "note" else "refused" end) as $verdict
  | "\($verdict) \($at.symbolic.key.Local.verbatim_path):\($at.concrete.location.start_point.row + 1): \(.ident): \($at.symbolic.annotation)"
' "$WORK/report.json" > "$WORK/findings.txt"

sed -n '/^note /p' "$WORK/findings.txt"
if grep '^refused ' "$WORK/findings.txt" >&2; then
  echo "error: pin every action to a commit and keep its version comment on that commit's tag; pass expressions to run scripts through env" >&2
  exit 1
fi
if [ ${#ACTION_FILES[@]} -gt 0 ]; then
  echo "zizmor: every action in ${#FILES[@]} workflow files and ${#ACTION_FILES[@]} composite action files is pinned to a commit; no version comment names another tag"
else
  echo "zizmor: every action in ${#FILES[@]} workflow files is pinned to a commit; no version comment names another tag"
fi
