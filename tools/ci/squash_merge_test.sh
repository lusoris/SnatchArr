#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for squash_merge.sh and main_commit_check.sh against a stub gh that records its
# calls. The landing helper must refuse a body without the sign-off of the git identity (the
# #1007 body), a sign-off of another identity, an empty head, a missing identity, a subject
# without the pull request number, a state other than CLEAN, a moved head and a main commit
# that lost the trailer; it merges a signed snapshot, including after an ambiguous client
# failure. Main checks every non-merge commit in a bounded range, with exact bot exceptions.
#
#   tools/ci/squash_merge_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' 0

# Three platform declarations, bounded by the three-element literal list.
platforms="Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only."
for script in "$HERE/squash_merge.sh" "$HERE/squash_merge_test.sh" "$HERE/main_commit_check.sh"; do
  grep -Fxq "# $platforms" "$script" || { echo "FAIL platform reason missing: $script" >&2; exit 1; }
done

# A copy of the helper and the policy in a repository with a known identity ("t").
new_repo "$WORK/repo"
mkdir -p "$WORK/repo/tools/ci" "$WORK/repo/.config/hooks"
cp "$HERE/squash_merge.sh" "$HERE/main_commit_check.sh" "$WORK/repo/tools/ci/"
cp "$ROOT/.config/hooks/commit-msg.sh" "$WORK/repo/.config/hooks/"
SHA=0123456789abcdef0123456789abcdef01234567
SUBJECT='feat(prowlarr): select indexers for a language (#7)'

# The stub records argv and the received body; API messages derive from that body.
cat >"$WORK/gh" <<'EOF'
#!/bin/sh
set -eu
echo "$1 $2" >>"$STUB_LOG"
printf '%s\n' "$@" >>"$STUB_ALL_ARGS"
case "$1 $2" in
  "pr view")
    case "$*" in
      *state,mergeCommit*) printf '%s\t%s\n' "$STUB_FINAL_STATE" fedcba9876543210fedcba9876543210fedcba98 ;;
      *mergeCommit*) echo fedcba9876543210fedcba9876543210fedcba98 ;;
      *)
        if [ "$STUB_MUTATE_BODY" != "" ]; then
          printf 'Changed after validation.\n' >"$STUB_MUTATE_BODY"
        fi
        printf '%s\t%s\n' "$STUB_STATE" "$STUB_HEAD"
        ;;
    esac ;;
  "pr merge")
    printf '%s\n' "$@" >"$STUB_ARGS"
    shift 3
    squash=0 match= subject= body=
    remaining=$#
    while [ "$remaining" -gt 0 ] && [ "$#" -gt 0 ]; do
      remaining=$((remaining - 1))
      case "$1" in
        --squash) squash=1; shift ;;
        --match-head-commit | --subject | --body-file | -R)
          [ "$#" -ge 2 ] || exit 90
          case "$1" in
            --match-head-commit) match="$2" ;;
            --subject) subject="$2" ;;
            --body-file) body="$2" ;;
            -R) [ "$2" = lusoris/SnatchArr ] || exit 90 ;;
          esac
          shift 2 ;;
        *) echo "stub: unexpected merge argument $1" >&2; exit 90 ;;
      esac
    done
    if [ "$squash" -ne 1 ] || [ "$match" != "$STUB_HEAD" ] ||
      [ -z "$subject" ] || [ -z "$body" ]; then
      echo "stub: missing required merge arguments" >&2
      exit 90
    fi
    printf '%s\n' "$body" >"$STUB_BODY_PATH"
    cp "$body" "$STUB_RECEIVED"
    printf '%s\n' "$subject" >"$STUB_SUBJECT"
    exit "$STUB_MERGE_RC"
    ;;
  "api "*)
    [ "$STUB_READ_RC" -eq 0 ] || exit "$STUB_READ_RC"
    cat "$STUB_SUBJECT"
    printf '\n'
    if [ "$STUB_LOST_TRAILER" -eq 0 ]; then cat "$STUB_RECEIVED"; fi
    ;;
  *) echo "stub: unexpected call" >&2; exit 90 ;;
esac
EOF
chmod +x "$WORK/gh"

printf 'Selects indexers.\n\nRefs #696\n' >"$WORK/unsigned"
printf 'Selects indexers.\n\nSigned-off-by: someone <s@example.invalid>\n' >"$WORK/other"
printf 'Selects indexers.\n\nSigned-off-by: t <t@example.invalid>\n' >"$WORK/signed"

# land NAME WANT_RC NEEDLE WANT_CALLS BODY [SHA] [SUBJECT] [--dry-run]: run the copy with the stub.
land() {
  : >"$WORK/log"
  : >"$WORK/all-args"
  rm -f "$WORK/received" "$WORK/body-path"
  cp "$5" "$WORK/validated"
  rc=0
  out="$(GH="$WORK/gh" STUB_LOG="$WORK/log" STUB_STATE="${STATE:-CLEAN}" STUB_HEAD="${HEAD_OID:-$SHA}" \
    STUB_ALL_ARGS="$WORK/all-args" STUB_ARGS="$WORK/args" STUB_BODY_PATH="$WORK/body-path" \
    STUB_RECEIVED="$WORK/received" STUB_SUBJECT="$WORK/subject" STUB_MERGE_RC="${MERGE_RC:-0}" \
    STUB_FINAL_STATE="${FINAL_STATE:-OPEN}" STUB_READ_RC="${READ_RC:-0}" \
    STUB_LOST_TRAILER="${LOST_TRAILER:-0}" STUB_MUTATE_BODY="${MUTATE_BODY:-}" \
    sh "$WORK/repo/tools/ci/squash_merge.sh" 7 "${6-$SHA}" "${7-$SUBJECT}" "$5" ${8:+"$8"} 2>&1)" || rc=$?
  check_case "$1" "$2" "$rc" "$3" "$out"
  calls="$(tr '\n' ';' <"$WORK/log")"
  [ "$calls" = "$4" ] || { printf 'FAIL %s: calls "%s", want "%s"\n' "$1" "$calls" "$4" >&2; exit 1; }
  if [ "$2" -eq 0 ] && [ "${8:-}" != --dry-run ]; then
    cmp "$WORK/validated" "$WORK/received" || { echo "FAIL $1: received body differs" >&2; exit 1; }
    [ "$(cat "$WORK/body-path")" != "$5" ] || { echo "FAIL $1: caller body reused" >&2; exit 1; }
  fi
}

land "planted #1007 body without the trailer" 3 "must end with 'Signed-off-by: t <t@example.invalid>'" "" "$WORK/unsigned"
land "trailer of another identity" 3 "must end with" "" "$WORK/other"
land "empty head from a failed read" 2 "40 hex" "" "$WORK/signed" ""
land "subject without the number" 4 "(#7)" "" "$WORK/signed" "$SHA" "feat(prowlarr): select indexers"
STATE=BLOCKED land "not CLEAN" 5 "mergeStateStatus 'BLOCKED'" "pr view;" "$WORK/signed"
HEAD_OID=1111111111111111111111111111111111111111 land "head moved" 5 "not $SHA" "pr view;" "$WORK/signed"
land "signed body, dry run" 0 "dry-run" "pr view;" "$WORK/signed" "$SHA" "$SUBJECT" --dry-run
land "signed body merges" 0 "merged #7" "pr view;pr merge;pr view;api repos/lusoris/SnatchArr/commits/fedcba9876543210fedcba9876543210fedcba98;" "$WORK/signed"
LOST_TRAILER=1 land "main lost the trailer" 6 "never rewrite main" \
  "pr view;pr merge;pr view;api repos/lusoris/SnatchArr/commits/fedcba9876543210fedcba9876543210fedcba98;" "$WORK/signed"

cp "$WORK/signed" "$WORK/mutable"
MUTATE_BODY="$WORK/mutable" land "body snapshot survives caller mutation" 0 "merged #7" \
  "pr view;pr merge;pr view;api repos/lusoris/SnatchArr/commits/fedcba9876543210fedcba9876543210fedcba98;" "$WORK/mutable"
MERGE_RC=1 FINAL_STATE=MERGED land "client error after completed merge" 0 "merge completed despite the client error" \
  "pr view;pr merge;pr view;api repos/lusoris/SnatchArr/commits/fedcba9876543210fedcba9876543210fedcba98;" "$WORK/signed"
MERGE_RC=1 FINAL_STATE=OPEN land "merge failed with OPEN state" 7 "merge failed" \
  "pr view;pr merge;pr view;" "$WORK/signed"
MERGE_RC=1 FINAL_STATE=MERGED LOST_TRAILER=1 land "client error still checks trailer" 6 "never rewrite main" \
  "pr view;pr merge;pr view;api repos/lusoris/SnatchArr/commits/fedcba9876543210fedcba9876543210fedcba98;" "$WORK/signed"
READ_RC=1 land "merged commit read-back fails" 8 \
  "merged as fedcba9876543210fedcba9876543210fedcba98, read-back failed: check main_commit_check" \
  "pr view;pr merge;pr view;api repos/lusoris/SnatchArr/commits/fedcba9876543210fedcba9876543210fedcba98;" "$WORK/signed"

# A planted helper drops --body-file: the signed-body merge assertion must detect it.
cp "$WORK/repo/tools/ci/squash_merge.sh" "$WORK/real-helper"
sed 's/ --body-file .*; then/; then/' \
  "$WORK/real-helper" >"$WORK/repo/tools/ci/squash_merge.sh"
rc=0
out="$(land "signed body merges" 0 "merged #7" \
  "pr view;pr merge;pr view;api repos/lusoris/SnatchArr/commits/fedcba9876543210fedcba9876543210fedcba98;" \
  "$WORK/signed" 2>&1)" || rc=$?
check_case "planted helper drops body-file" 1 "$rc" "FAIL signed body merges: exit 7, want 0" "$out"
printf '%s\n' "plant detected: $out"
cp "$WORK/real-helper" "$WORK/repo/tools/ci/squash_merge.sh"

# No identity: a repository without user.name, global and system config hidden.
new_repo "$WORK/anon"
git -C "$WORK/anon" config --unset user.name
git -C "$WORK/anon" config --unset user.email
mkdir -p "$WORK/anon/tools/ci"
cp "$HERE/squash_merge.sh" "$WORK/anon/tools/ci/"
rc=0
out="$(GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GH="$WORK/gh" STUB_LOG="$WORK/log" \
  sh "$WORK/anon/tools/ci/squash_merge.sh" 7 "$SHA" "$SUBJECT" "$WORK/signed" 2>&1)" || rc=$?
check_case "no git identity" 2 "$rc" "user.name and user.email are required" "$out"

# commit NAME EMAIL [COMMITTER_EMAIL]: create a real empty commit in the fixture only.
commit() {
  GIT_AUTHOR_NAME="$1" GIT_AUTHOR_EMAIL="$2" GIT_COMMITTER_NAME="$1" GIT_COMMITTER_EMAIL="${3:-$2}" \
    git -C "$WORK/repo" commit -q --allow-empty -F - >/dev/null
  git -C "$WORK/repo" rev-parse HEAD
}
# main_check NAME WANT_RC NEEDLE COMMIT_OR_RANGE: run the main policy against real history.
main_check() {
  rc=0
  out="$(sh "$WORK/repo/tools/ci/main_commit_check.sh" "$4" 2>&1)" || rc=$?
  check_case "$1" "$2" "$rc" "$3" "$out"
}
base="$(printf '%s\n\nBody.\n\nSigned-off-by: t <t@example.invalid>\n' "$SUBJECT" | commit Kilian k@example.invalid)"
main_check "signed human" 0 "passes the commit-msg policy" "$base"
unsigned="$(printf '%s\n\nBody.\n' "$SUBJECT" | commit Kilian k@example.invalid)"
main_check "unsigned maintainer squash on main" 1 "fails the commit-msg policy" "$unsigned"
tip="$(printf '%s\n\nBody.\n\nSigned-off-by: t <t@example.invalid>\n' "$SUBJECT" | commit Kilian k@example.invalid)"
main_check "range unsigned middle, signed tip" 1 "$unsigned fails the commit-msg policy" "$base..$tip"
main_check "zero before checks tip only" 0 "checking only $tip" "0000000000000000000000000000000000000000..$tip"
main_check "non-ancestor before checks tip only" 0 "checking only $base" "$tip..$base"
c="$(printf 'chore(main): release 0.8.1 (#7)\n' | commit 'evil[bot]' 'evil[bot]@users.noreply.github.com' noreply@github.com)"
main_check "fake bot author" 1 "fails the commit-msg policy" "$c"
c="$(printf 'chore(main): release 0.8.1 (#7)\n' | commit 'snatcharr-release[bot]' '340302165+snatcharr-release[bot]@users.noreply.github.com' evil@example.invalid)"
main_check "allowed bot, wrong committer" 1 "fails the commit-msg policy" "$c"
# Two allow-listed bot authors, bounded by this two-element literal list.
for bot_email in '340302165+snatcharr-release[bot]@users.noreply.github.com' '29139614+renovate[bot]@users.noreply.github.com'; do
  c="$(printf 'chore(main): release 0.8.1 (#7)\n' | commit bot "$bot_email" noreply@github.com)"
  main_check "allowed bot with GitHub committer" 0 "allowed bot author" "$c"
done
# Build a signed range with exactly 200 commits, then one beyond the scalar limit.
range_base="$c"
i=0
while [ "$i" -lt 200 ]; do
  c="$(printf 'ci: range boundary\n\nSigned-off-by: t <t@example.invalid>\n' | commit t t@example.invalid)"
  i=$((i + 1))
done
main_check "200 commits accepted" 0 "passes the commit-msg policy" "$range_base..$c"
c="$(printf 'ci: range overflow\n\nSigned-off-by: t <t@example.invalid>\n' | commit t t@example.invalid)"
main_check "more than 200 commits refused" 1 "range too long" "$range_base..$c"

echo "squash_merge_test: all cases passed"
