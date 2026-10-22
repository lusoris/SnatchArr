#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for lint_exceptions.sh: synthetic golangci-lint configuration
# and reports testing valid exceptions, expired entries, stale entries, new findings,
# and immutability of the target configuration.
#
#   tools/ci/lint_exceptions_test.sh
#
# Exit status: 0 all cases behaved as recorded, 1 otherwise.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
SCRIPT="$HERE/lint_exceptions.sh"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT HUP INT TERM

cd "$REPO_ROOT"
export PRAETOR_ENGINE="${PRAETOR_ENGINE:-sh .config/lefthook/engine.sh}"

FIXTURE="$WORK/golangci.yml"
cat > "$FIXTURE" <<'EOF'
version: "2"
linters:
  enable:
    - exhaustive
    - revive
  exclusions:
    rules:
      # praetor:generated-exceptions begin (praetorctl linters exceptions; never edit by hand)
      # category A, entered 2026-10-22, expires 2027-01-20, #1226
      - path: ^tools/x\.go$
        linters:
          - exhaustive
      # category A, entered 2026-10-22, expires 2027-01-20, #1226
      - path: ^tools/x\.go$
        linters:
          - revive
      # praetor:generated-exceptions end
EOF

REPORT_BOTH="$WORK/report_both.json"
cat > "$REPORT_BOTH" <<'EOF'
{"Issues":[{"FromLinter":"exhaustive","Pos":{"Filename":"tools/x.go"}},{"FromLinter":"revive","Pos":{"Filename":"tools/x.go"}}]}
EOF

REPORT_EXHAUSTIVE="$WORK/report_exhaustive.json"
cat > "$REPORT_EXHAUSTIVE" <<'EOF'
{"Issues":[{"FromLinter":"exhaustive","Pos":{"Filename":"tools/x.go"}}]}
EOF

REPORT_NEW="$WORK/report_new.json"
cat > "$REPORT_NEW" <<'EOF'
{"Issues":[{"FromLinter":"exhaustive","Pos":{"Filename":"tools/x.go"}},{"FromLinter":"revive","Pos":{"Filename":"tools/x.go"}},{"FromLinter":"revive","Pos":{"Filename":"tools/y.go"}}]}
EOF

INIT_SHA="$(cksum < "$FIXTURE")"

run_case() {
  name="$1"
  want_rc="$2"
  needle="$3"
  report="$4"
  today="$5"

  got_rc=0
  out="$(LINT_EXCEPTIONS_TODAY="$today" sh "$SCRIPT" "$FIXTURE" "$report" 1226 2>&1)" || got_rc=$?

  if [ "$got_rc" -ne "$want_rc" ]; then
    printf 'FAIL %s: exit %s, want %s\n%s\n' "$name" "$got_rc" "$want_rc" "$out" >&2
    exit 1
  fi

  case "$out" in
    *"$needle"*) ;;
    *)
      printf 'FAIL %s: output did not contain "%s"\n%s\n' "$name" "$needle" "$out" >&2
      exit 1
      ;;
  esac

  current_sha="$(cksum < "$FIXTURE")"
  if [ "$current_sha" != "$INIT_SHA" ]; then
    printf 'FAIL %s: config was modified by gate run\n' "$name" >&2
    exit 1
  fi

  printf 'ok   %s\n' "$name"
}

# a) both findings in report, today 2026-11-01 -> exit 0 "2 exception(s) hold"
run_case "case a: both findings in report" 0 "2 exception(s) hold" "$REPORT_BOTH" "2026-11-01"

# b) planted expired: same, today 2027-02-01 -> exit 1, "generate refused (rc 3)"
run_case "case b: planted expired" 1 "generate refused (rc 3)" "$REPORT_BOTH" "2027-02-01"

# c) planted stale: report only exhaustive, today 2026-11-01 -> exit 1, "stale"
run_case "case c: planted stale" 1 "stale" "$REPORT_EXHAUSTIVE" "2026-11-01"

# d) planted new finding: report adds tools/y.go revive -> exit 1, "generate refused (rc 3)"
run_case "case d: planted new finding" 1 "generate refused (rc 3)" "$REPORT_NEW" "2026-11-01"

# e) config untouched after every case (sha of fixture before == after).
FINAL_SHA="$(cksum < "$FIXTURE")"
if [ "$FINAL_SHA" != "$INIT_SHA" ]; then
  printf 'FAIL case e: config untouched after every case: sha changed (%s != %s)\n' "$FINAL_SHA" "$INIT_SHA" >&2
  exit 1
fi
printf 'ok   case e: config untouched after every case\n'
