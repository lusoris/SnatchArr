#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for security_issue.sh against a stub gh that records its calls: no open issue,
# the issue open, an issue whose title only contains the title, a failed listing, a failed
# create, a failed comment, no run address, and a named workflow (the soak) beside the scan's
# issue.
#
#   tools/ci/security_issue_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs jq.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
failed=0

# The stub prints STUB_LIST for `issue list`, exits STUB_FAIL_<verb> when set, and records the
# verb and the issue number or title of every call.
cat >"$WORK/gh" <<'EOF'
#!/bin/sh
verb="$2"
echo "$verb $3 $4" >>"$STUB_LOG"
case "$verb" in
  list) [ -z "${STUB_FAIL_list:-}" ] || exit "$STUB_FAIL_list"; printf '%s\n' "$STUB_LIST" ;;
  create) [ -z "${STUB_FAIL_create:-}" ] || exit "$STUB_FAIL_create" ;;
  comment) [ -z "${STUB_FAIL_comment:-}" ] || exit "$STUB_FAIL_comment" ;;
esac
EOF
chmod +x "$WORK/gh"

# expect NAME WANT_EXIT WANT_CALLS LIST [VAR=VALUE...] [-- ARGS...]: the run exits WANT_EXIT and
# the stub saw WANT_CALLS (calls joined by "; ").
expect() {
  name="$1"
  want="$2"
  calls="$3"
  list="$4"
  shift 4
  : >"$WORK/log"
  got=0
  env GH="$WORK/gh" STUB_LOG="$WORK/log" STUB_LIST="$list" "$@" >"$WORK/out" 2>&1 || got=$?
  seen=$(awk 'NR > 1 { printf "; " } { printf "%s", $0 }' "$WORK/log")
  if [ "$got" -ne "$want" ] || [ "$seen" != "$calls" ]; then
    echo "FAIL $name: exit $got, want $want; calls '$seen', want '$calls'" >&2
    sed 's/^/  /' "$WORK/out" >&2
    failed=1
    return 0
  fi
  echo "ok   $name: exit $got"
}

SCRIPT="$HERE/security_issue.sh"
URL=https://example.invalid/run/1
LIST_CREATE="list --state open; create --title Security Scan fails on main"

expect none 0 "$LIST_CREATE" '[]' sh "$SCRIPT" "$URL"
expect open 0 "list --state open; comment 7 --body" \
  '[{"number":3,"title":"other"},{"number":7,"title":"Security Scan fails on main"}]' sh "$SCRIPT" "$URL"
expect contains_only 0 "$LIST_CREATE" \
  '[{"number":9,"title":"Security Scan fails on main again"}]' sh "$SCRIPT" "$URL"
expect list_fails 1 "list --state open" '[]' STUB_FAIL_list=1 sh "$SCRIPT" "$URL"
expect create_fails 1 "$LIST_CREATE" '[]' STUB_FAIL_create=1 sh "$SCRIPT" "$URL"
expect comment_fails 1 "list --state open; comment 7 --body" \
  '[{"number":7,"title":"Security Scan fails on main"}]' STUB_FAIL_comment=1 sh "$SCRIPT" "$URL"
expect named 0 "list --state open; create --title Soak fails on main" \
  '[{"number":7,"title":"Security Scan fails on main"}]' sh "$SCRIPT" "$URL" Soak
expect named_open 0 "list --state open; comment 8 --body" \
  '[{"number":7,"title":"Security Scan fails on main"},{"number":8,"title":"Soak fails on main"}]' sh "$SCRIPT" "$URL" Soak
expect too_many_args 2 "" '[]' sh "$SCRIPT" "$URL" Soak extra
expect no_url 2 "" '[]' sh "$SCRIPT" ""
expect no_args 2 "" '[]' sh "$SCRIPT"
exit "$failed"
