#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Refuses a workflow or composite action `uses:` reference whose repository has moved to another
# owner or name. Git, the REST API and zizmor follow repository redirects; GitHub Actions does
# not. A reusable workflow called by its old name stops the whole run before any job starts
# ("workflow was not found"), and an action named the old way fails its step.
#
#   tools/ci/workflow_refs.sh [ROOT]      (default: this repository)
#
# Exit status: 0 clean, 1 a finding or a failed lookup, 2 usage.
# Needs python3 with PyYAML, jq, curl, network, token (GH_TOKEN or GITHUB_TOKEN).
set -euo pipefail

if [ $# -gt 1 ]; then
  echo "usage: ${0##*/} [ROOT]" >&2
  exit 2
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
ROOT="$(cd "${1:-$REPO_ROOT}" && pwd)"

TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
if [ -z "$TOKEN" ]; then
  echo "error: GH_TOKEN or GITHUB_TOKEN is required" >&2
  exit 1
fi

MAX_FILES=4096
shopt -s nullglob
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

ACTION_FILES=()
if [ -d "$ROOT/.github/actions" ]; then
  while IFS= read -r -d '' f; do
    file_count=$((file_count + 1))
    if [ "$file_count" -gt "$MAX_FILES" ]; then
      echo "error: more than $MAX_FILES workflow and composite action files under $ROOT" >&2
      exit 1
    fi
    ACTION_FILES+=("${f#"$ROOT"/}")
  done < <(find "$ROOT/.github/actions" -maxdepth 8 -type f \( -name 'action.yml' -o -name 'action.yaml' \) -print0 | LC_ALL=C sort -z)
fi

PYTHON="${PYTHON_BIN:-python3}"
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
API_URL="${GITHUB_API_URL:-https://api.github.com}"
API_URL="${API_URL%/}"
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
  if ! err_idx="$(jq -r 'if (.errors | type) == "array" and (.errors | length) > 0 and (.errors[0] | type) == "object" and (.errors[0].path | type) == "array" and (.errors[0].path[0] | type) == "string" then (.errors[0].path[0] | capture("^r(?<i>[0-9]+)$").i // "") else "" end' <<< "$body" 2> /dev/null)"; then
    err_idx=""
  fi
  if [[ "$err_idx" =~ ^[0-9]+$ ]] && [ "$err_idx" -lt "${#chunk[@]}" ]; then
    err_repo="${chunk[$err_idx]}"
  fi
  err_msg="$(jq -r 'if (.errors | type) == "array" and (.errors | length) > 0 and (.errors[0] | type) == "object" and (.errors[0].message | type) == "string" then .errors[0].message else "GraphQL error" end' <<< "$body" 2> /dev/null || echo "GraphQL error")"
  echo "error: ${FIRST_USE[$err_repo]}: GraphQL returned errors for $err_repo ($API_URL/graphql): $err_msg" >&2
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
      "$API_URL/graphql")"; then
    echo "error: ${FIRST_USE[$first_repo]}: cannot read repository $first_repo from $API_URL/graphql" >&2
    exit 1
  fi
  if ! jq -e 'type == "object"' <<< "$body" > /dev/null 2>&1; then
    echo "error: ${FIRST_USE[$first_repo]}: invalid JSON response from $API_URL/graphql" >&2
    exit 1
  fi
  if jq -e 'has("errors")' <<< "$body" > /dev/null 2>&1; then
    report_graphql_error "$body" "$first_repo" "${chunk[@]}"
  fi
  mapfile -t resolved < <(jq -r --argjson n "${#chunk[@]}" '
    . as $root |
    [range(0; $n) | "r\(.)" as $k |
      if ($root.data | type) == "object" and ($root.data | has($k)) and ($root.data[$k] | type) == "object" and ($root.data[$k].nameWithOwner | type) == "string"
      then $root.data[$k].nameWithOwner
      else ""
      end
    ] | .[]
  ' <<< "$body")
  for ((i = 0; i < BATCH_SIZE && i < ${#chunk[@]}; i++)); do
    repo="${chunk[$i]}"
    actual="${resolved[$i]:-}"
    if [ -z "$actual" ]; then
      echo "error: ${FIRST_USE[$repo]}: names no repository for $repo ($API_URL/graphql)" >&2
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
for ((batch = 0; batch < MAX_BATCHES && batch * BATCH_SIZE < ${#REPOS[@]}; batch++)); do
  query_batch $((batch * BATCH_SIZE))
done
if [ "$moved" -ne 0 ]; then
  exit 1
fi
echo "repositories: all $checked used by name are under their current name"
