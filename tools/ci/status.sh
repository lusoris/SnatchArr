#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Decides "CI success", the required check that stands for every job of the CI workflow.
# A job the change filter leaves out ends as skipped, and a skipped job satisfies a required
# check on its own. This script reads every job result instead: a job passes when it succeeded,
# or when it was skipped and nothing selected it.
#
#   NEEDS='<toJSON(needs)>' COMMITS_EXPECTED=true|false tools/ci/status.sh
#   tools/ci/status.sh mirror WORKFLOW     the workflow must match the rule table below
#
# Exit status: 0 passed, 1 refused, 2 usage. Needs jq (first form) and awk (second form).
set -eu

# One job per line with what selects it: "always", "commits" (COMMITS_EXPECTED), or the outputs
# of the changes job, any of which selects the job. The mirror form holds ci.yml to this table.
RULES='workflows always
secret-scan always
semgrep always
trivy always
changes always
commits commits
api-lint api
api-test api
api-security api
api-openapi api
worker worker
e2e api,worker,proto
image-smoke api,worker,proto,web,deploy
dev-compose deploy
dlclients-live api
arr-forceimport-live api
integrations-live api,web
web web
web-e2e-api api,web
web-visual web
proto proto
deploy deploy
contracts contracts
governance governance
docs docs
licences licences'

DRAFT_STEP='Stop on a draft pull request'

decide() {
  case "${COMMITS_EXPECTED:-}" in
    true | false) ;;
    *)
      echo "usage: NEEDS=<json> COMMITS_EXPECTED=true|false ${0##*/}" >&2
      return 2
      ;;
  esac
  if ! refused="$(jq -n -r --arg needs "${NEEDS:-}" --arg rules "$RULES" --arg commits "$COMMITS_EXPECTED" '
    ($needs | fromjson) as $needs
    | ($needs.changes.outputs // {}) as $selected
    | ($rules | split("\n") | map(split(" ") | {job: .[0], on: .[1]})) as $table
    | (
        $table[]
        | . as $rule
        | ($needs[$rule.job].result // "not in needs") as $got
        | (if $rule.on == "always" then true
           elif $rule.on == "commits" then $commits == "true"
           else any($rule.on | split(",")[]; $selected[.] == "true") end) as $must
        | select(($got == "success" or (($must | not) and $got == "skipped")) | not)
        | "job \($rule.job): \($got)" + (if $must then ", must succeed" else ", may only be skipped" end)
      ),
      (($needs | keys) - ($table | map(.job)) | .[] | "job \(.): no rule in tools/ci/status.sh")
  ')"; then
    echo "error: the job results could not be read" >&2
    return 1
  fi
  if [ -n "$refused" ]; then
    printf '%s\n' "$refused" >&2
    return 1
  fi
  echo "CI success: every job ended as its rule demands"
}

# workflow_jobs FILE: "<job>|<job-level if expression>" per job of the workflow.
workflow_jobs() {
  awk '
    /^jobs:$/ { in_jobs = 1; next }
    in_jobs && /^  [A-Za-z0-9_-]+:$/ {
      if (job != "") print job "|" cond
      job = substr($0, 3, length($0) - 3); cond = ""; seen = 0
      next
    }
    in_jobs && job != "" && !seen && /^    if: / { cond = substr($0, 9); seen = 1 }
    END { if (job != "") print job "|" cond }
  ' "$1"
}

# status_needs FILE: the jobs the status job needs, one per line.
status_needs() {
  awk '
    /^  [A-Za-z0-9_-]+:$/ { in_status = ($0 == "  status:"); in_needs = 0; next }
    in_status && /^    needs:$/ { in_needs = 1; next }
    in_needs && /^      - / { print substr($0, 9); next }
    { in_needs = 0 }
  ' "$1"
}

# first_steps FILE: "<job>|<name of its first step>" per job of the workflow.
first_steps() {
  awk '
    /^jobs:$/ { in_jobs = 1; next }
    in_jobs && /^  [A-Za-z0-9_-]+:$/ { job = substr($0, 3, length($0) - 3); in_steps = 0; seen = 0; next }
    in_jobs && job != "" && /^    steps:$/ { in_steps = 1; next }
    # A step anchored as &stop-draft names itself on the next line; *stop-draft resolves to that name.
    anchored != "" {
      step = $0
      if (sub(/^        name: /, "", step)) draft = step; else step = "&stop-draft"
      print anchored "|" step; anchored = ""; next
    }
    in_steps && !seen && /^      - / {
      step = $0; sub(/^      - (name: )?/, "", step); seen = 1
      if (step == "&stop-draft") { anchored = job; next }
      if (step == "*stop-draft" && draft != "") step = draft
      print job "|" step
    }
  ' "$1"
}

# expected_if ON COMMITS_EXPRESSION: the job-level condition a rule stands for.
expected_if() {
  case "$1" in
    always) echo "" ;;
    commits) echo "$2" ;;
    *)
      echo "$1" | awk -F, '{
        for (i = 1; i <= NF; i++) printf "%s%s", (i > 1 ? " || " : ""), "needs.changes.outputs." $i " == \047true\047"
        print ""
      }'
      ;;
  esac
}

mirror() {
  file="$1"
  jobs="$(workflow_jobs "$file")"
  needs="$(status_needs "$file")"
  commits_expr="$(sed -n 's/^ *COMMITS_EXPECTED: \${{ \(.*\) }}$/\1/p' "$file")"
  status=0
  if [ -z "$commits_expr" ]; then
    echo "status: no COMMITS_EXPECTED expression" >&2
    status=1
  fi
  if ! printf '%s\n' "$jobs" | grep -qx 'status|always()'; then
    echo "status: must run with \"if: always()\"" >&2
    status=1
  fi
  steps="$(first_steps "$file")"
  while read -r job on; do
    # A draft runs no gate: a job the change filter decides needs the changes job, which stops
    # the draft in its first step, so GitHub skips it; every other job stops the draft itself.
    case "$on" in
      always | commits)
        if ! printf '%s\n' "$steps" | grep -qxF "$job|$DRAFT_STEP"; then
          echo "job $job: the first step is not \"$DRAFT_STEP\"" >&2
          status=1
        fi
        ;;
    esac
    want="$(expected_if "$on" "$commits_expr")"
    if ! printf '%s\n' "$jobs" | grep -qxF "$job|$want"; then
      echo "job $job: the workflow does not carry the condition of its rule ($on)" >&2
      status=1
    fi
    if ! printf '%s\n' "$needs" | grep -qxF "$job"; then
      echo "job $job: not needed by the status job" >&2
      status=1
    fi
  done <<EOF
$RULES
EOF
  unknown="$({
    printf '%s\n' "$jobs" | cut -d'|' -f1
    printf '%s\n' "$needs"
  } | sort -u | while read -r job; do
    if [ -n "$job" ] && [ "$job" != status ] && ! printf '%s\n' "$RULES" | grep -q "^$job "; then
      echo "job $job: no rule in tools/ci/status.sh"
    fi
  done)"
  if [ -n "$unknown" ]; then
    printf '%s\n' "$unknown" >&2
    status=1
  fi
  [ "$status" -eq 0 ] && echo "mirror: $file matches the rule table"
  return "$status"
}

case "$#:${1:-}" in
  0:) decide ;;
  2:mirror) mirror "$2" ;;
  *)
    echo "usage: ${0##*/} | ${0##*/} mirror WORKFLOW" >&2
    exit 2
    ;;
esac
