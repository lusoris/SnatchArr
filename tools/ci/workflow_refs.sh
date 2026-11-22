#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Refuses a workflow or composite action `uses:` reference whose repository has moved to another
# owner or name. Git, the REST API and zizmor follow repository redirects; GitHub Actions does
# not. A reusable workflow called by its old name stops the whole run before any job starts
# ("workflow was not found"), and an action named the old way fails its step. Only public
# repositories are supported: private and missing repositories cannot be read by the workflow
# token and fail closed.
# Platform (HISS-21): runs on Linux in CI (the verdict reads repository files and the GitHub API,
# independent of runner OS) and on macOS/Linux hosts with bash 4.4 or newer (mapfile -d, associative
# arrays, ${var,,}); exits 2 naming bash 4.4 or newer on older bash such as macOS stock bash 3.2.
#
#   tools/ci/workflow_refs.sh [ROOT]      (default: this repository)
#
# Exit status: 0 clean, 1 a finding or a failed lookup, 2 usage or unsupported bash.
# Needs bash 4.4 or newer (mapfile -d), python3 with PyYAML, jq, curl, network, token (GH_TOKEN or GITHUB_TOKEN).
# WORKFLOW_REFS_DEADLINE_SECONDS (default 60, a whole number from 1 to 9999, else exit 2) bounds all
# repository lookups together; a passed deadline exits 1 naming the batch it stopped at.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tools/ci/bash_version.sh
. "$HERE/bash_version.sh"
if ! bash_version_supported "${BASH_VERSINFO[0]}" "${BASH_VERSINFO[1]}"; then
  echo "error: ${0##*/} requires bash 4.4 or newer (mapfile -d), found $BASH_VERSION" >&2
  exit 2
fi

if [ $# -gt 1 ]; then
  echo "usage: ${0##*/} [ROOT]" >&2
  exit 2
fi

DEADLINE_SECONDS="${WORKFLOW_REFS_DEADLINE_SECONDS:-60}"
if ! [[ "$DEADLINE_SECONDS" =~ ^[1-9][0-9]{0,3}$ ]]; then
  echo "error: WORKFLOW_REFS_DEADLINE_SECONDS must be a whole number of seconds from 1 to 9999, got '$DEADLINE_SECONDS'" >&2
  exit 2
fi

REPO_ROOT="$(cd "$HERE/../.." && pwd)"
ROOT="$(cd "${1:-$REPO_ROOT}" && pwd)"

TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
if [ -z "$TOKEN" ]; then
  echo "error: GH_TOKEN or GITHUB_TOKEN is required" >&2
  exit 1
fi

MAX_FILES=4096
shopt -s nullglob dotglob
FILES=()
file_count=0
for f in "$ROOT"/.github/workflows/*.yml "$ROOT"/.github/workflows/*.yaml; do
  file_count=$((file_count + 1))
  if [ "$file_count" -gt "$MAX_FILES" ]; then
    echo "error: more than $MAX_FILES workflow files under $ROOT" >&2
    exit 1
  fi
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
ACTION_FILES=()
mapfile -d '' -n "$((MAX_FILES + 1))" -t ACTION_FILES < "$WORK/actions.nul"
file_count=$((file_count + ${#ACTION_FILES[@]}))
if [ "$file_count" -gt "$MAX_FILES" ]; then
  echo "error: more than $MAX_FILES workflow and composite action files under $ROOT" >&2
  exit 1
fi

if ! uses_lines="$(cd "$ROOT" && "$PYTHON" -I "$HERE/workflow_uses.py" -- "${FILES[@]}" "${ACTION_FILES[@]}")"; then
  exit 1
fi

MAX_REFS=16384
declare -A FIRST_USE=()
ref_count=0
if [ -n "$uses_lines" ]; then
  while IFS=$'\t' read -r repo loc; do
    ref_count=$((ref_count + 1))
    if [ "$ref_count" -gt "$MAX_REFS" ]; then
      echo "error: $loc: more than $MAX_REFS uses references" >&2
      exit 1
    fi
    [ -n "$repo" ] || continue
    [ -n "${FIRST_USE[$repo]:-}" ] || FIRST_USE[$repo]="$loc"
  done <<< "$uses_lines"
fi

REPOS=()
if [ ${#FIRST_USE[@]} -gt 0 ]; then
  mapfile -t REPOS < <(printf '%s\n' "${!FIRST_USE[@]}" | LC_ALL=C sort)
fi

CURL="${CURL_BIN:-curl}"
GRAPHQL_URL="${GITHUB_GRAPHQL_URL:-https://api.github.com/graphql}"
GRAPHQL_URL="${GRAPHQL_URL%/}"
BATCH_SIZE=100
MAX_BATCHES=50
if [ ${#REPOS[@]} -gt $((BATCH_SIZE * MAX_BATCHES)) ]; then
  echo "error: ${FIRST_USE[${REPOS[0]}]}: more than $((BATCH_SIZE * MAX_BATCHES)) distinct repositories" >&2
  exit 1
fi

# build_graphql_payload OFFSET: print JSON GraphQL body for up to BATCH_SIZE entries from REPOS[OFFSET].
build_graphql_payload() {
  local offset="$1" query_fields="query {" repo owner rname i
  local -a chunk=("${REPOS[@]:offset:BATCH_SIZE}")
  for ((i = 0; i < BATCH_SIZE && i < ${#chunk[@]}; i++)); do
    repo="${chunk[$i]}"
    if ! [[ "$repo" =~ ^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)$ ]]; then
      echo "error: ${FIRST_USE[$repo]}: invalid repository reference $repo" >&2
      exit 1
    fi
    owner="${BASH_REMATCH[1]}"
    rname="${BASH_REMATCH[2]}"
    query_fields+=" r$i: repository(owner: \"$owner\", name: \"$rname\") { nameWithOwner }"
  done
  query_fields+=" }"
  jq -cn --arg query "$query_fields" '{query: $query}'
}

# report_graphql_error BODY FIRST_REPO CHUNK...: print the first GraphQL error naming path:line and exit 1.
report_graphql_error() {
  local body="$1" err_repo="$2" err_idx err_msg
  shift 2
  local -a chunk=("$@")
  if ! err_idx="$(jq -sr '.[0] | if (.errors | type) == "array" and (.errors | length) > 0 and (.errors[0] | type) == "object" and (.errors[0].path | type) == "array" and (.errors[0].path[0] | type) == "string" then (.errors[0].path[0] | capture("^r(?<i>[0-9]+)$").i // "") else "" end' <<< "$body" 2> /dev/null)"; then
    err_idx=""
  fi
  if [[ "$err_idx" =~ ^[0-9]+$ ]] && [ "$err_idx" -lt "${#chunk[@]}" ]; then
    err_repo="${chunk[$err_idx]}"
  fi
  err_msg="$(jq -sr '.[0] | if (.errors | type) == "array" and (.errors | length) > 0 and (.errors[0] | type) == "object" and (.errors[0].message | type) == "string" then .errors[0].message else "GraphQL error" end' <<< "$body" 2> /dev/null || echo "GraphQL error")"
  echo "error: ${FIRST_USE[$err_repo]}: GraphQL returned errors for $err_repo ($GRAPHQL_URL): $err_msg" >&2
  exit 1
}

# query_batch OFFSET: look up up to BATCH_SIZE repositories starting at REPOS[OFFSET] in one
# GraphQL POST, setting moved=1 on any renamed repository and exiting 1 on transport or schema error.
query_batch() {
  local offset="$1" payload body first_repo repo actual i
  local -a chunk=("${REPOS[@]:offset:BATCH_SIZE}") resolved=()
  first_repo="${chunk[0]}"
  payload="$(build_graphql_payload "$offset")"
  if ! body="$(printf 'header = "Authorization: Bearer %s"\n' "$TOKEN" |
    "$CURL" -K - -fsS --max-time 20 --retry 2 \
      -H 'Content-Type: application/json' \
      --data-raw "$payload" \
      "$GRAPHQL_URL")"; then
    echo "error: ${FIRST_USE[$first_repo]}: cannot read repository $first_repo from $GRAPHQL_URL" >&2
    exit 1
  fi
  if ! jq -se 'length == 1 and (.[0] | type) == "object"' <<< "$body" > /dev/null 2>&1; then
    echo "error: ${FIRST_USE[$first_repo]}: invalid JSON response from $GRAPHQL_URL" >&2
    exit 1
  fi
  if jq -se '.[0] | has("errors")' <<< "$body" > /dev/null 2>&1; then
    report_graphql_error "$body" "$first_repo" "${chunk[@]}"
  fi
  mapfile -t resolved < <(jq -sr --argjson n "${#chunk[@]}" '
    .[0] as $root |
    [range(0; $n) | "r\(.)" as $k |
      if ($root.data | type) == "object" and ($root.data | has($k)) and ($root.data[$k] | type) == "object" and ($root.data[$k].nameWithOwner | type) == "string" and ($root.data[$k].nameWithOwner | test("\\A[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+\\z"))
      then $root.data[$k].nameWithOwner
      else ""
      end
    ] | .[]
  ' <<< "$body")
  for ((i = 0; i < BATCH_SIZE && i < ${#chunk[@]}; i++)); do
    repo="${chunk[$i]}"
    actual="${resolved[$i]:-}"
    if [ -z "$actual" ]; then
      echo "error: ${FIRST_USE[$repo]}: names no repository the token can read for $repo: private and missing repositories are not supported; reference a public repository ($GRAPHQL_URL)" >&2
      exit 1
    fi
    checked=$((checked + 1))
    if [ "${actual,,}" != "${repo,,}" ]; then
      echo "refused ${FIRST_USE[$repo]}: $repo moved to $actual; GitHub Actions does not follow the redirect" >&2
      moved=1
    fi
  done
}

moved=0
checked=0
total_batches=$(( (${#REPOS[@]} + BATCH_SIZE - 1) / BATCH_SIZE ))
deadline_at=$((SECONDS + DEADLINE_SECONDS))
for ((batch = 0; batch < MAX_BATCHES && batch * BATCH_SIZE < ${#REPOS[@]}; batch++)); do
  remaining=$((deadline_at - SECONDS))
  first_repo="${REPOS[$((batch * BATCH_SIZE))]}"
  if [ "$remaining" -le 0 ]; then
    echo "error: ${FIRST_USE[$first_repo]}: repository lookup deadline of $DEADLINE_SECONDS s passed at batch $((batch + 1)) of $total_batches (first repository $first_repo)" >&2
    exit 1
  fi
  query_batch $((batch * BATCH_SIZE))
done
if [ "$moved" -ne 0 ]; then
  exit 1
fi
echo "repositories: all $checked used by name are under their current name"
