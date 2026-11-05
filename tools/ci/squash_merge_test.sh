#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for squash_merge.sh and main_commit_check.sh against a stub gh that records its
# calls. The landing helper must refuse a body without the sign-off of the git identity (the
# #1007 body), a sign-off of another identity, an empty head, a missing identity, a subject
# without the pull request number, a state other than CLEAN, a moved head and a main commit
# that lost the trailer; it merges a signed body. The main check must refuse an unsigned
# maintainer commit and pass a signed one and a bot commit.
#
#   tools/ci/squash_merge_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# A copy of the helper and the policy in a repository with a known identity ("t").
new_repo "$WORK/repo"
mkdir -p "$WORK/repo/tools/ci" "$WORK/repo/.config/hooks"
cp "$HERE/squash_merge.sh" "$HERE/main_commit_check.sh" "$WORK/repo/tools/ci/"
cp "$ROOT/.config/hooks/commit-msg.sh" "$WORK/repo/.config/hooks/"
SHA=0123456789abcdef0123456789abcdef01234567
SUBJECT='feat(prowlarr): select indexers for a language (#7)'

# STUB_STATE / STUB_HEAD answer pr view; STUB_MESSAGE is the merged commit's message.
cat >"$WORK/gh" <<'EOF'
#!/bin/sh
echo "$1 $2" >>"$STUB_LOG"
case "$1 $2" in
  "pr view")
    case "$*" in
      *mergeCommit*) echo fedcba9876543210fedcba9876543210fedcba98 ;;
      *) printf '%s\t%s\n' "$STUB_STATE" "$STUB_HEAD" ;;
    esac ;;
  "pr merge") ;;
  "api "*) printf '%s\n' "$STUB_MESSAGE" ;;
esac
EOF
chmod +x "$WORK/gh"

printf 'Selects indexers.\n\nRefs #696\n' >"$WORK/unsigned"
printf 'Selects indexers.\n\nSigned-off-by: someone <s@example.invalid>\n' >"$WORK/other"
printf 'Selects indexers.\n\nSigned-off-by: t <t@example.invalid>\n' >"$WORK/signed"

# land NAME WANT_RC NEEDLE WANT_CALLS BODY [SHA] [SUBJECT] [--dry-run]: run the copy with the stub.
land() {
  : >"$WORK/log"
  rc=0
  out="$(GH="$WORK/gh" STUB_LOG="$WORK/log" STUB_STATE="${STATE:-CLEAN}" STUB_HEAD="${HEAD_OID:-$SHA}" \
    STUB_MESSAGE="${MESSAGE:-$(printf '%s\n\n' "$SUBJECT"; cat "$WORK/signed")}" \
    sh "$WORK/repo/tools/ci/squash_merge.sh" 7 "${6-$SHA}" "${7-$SUBJECT}" "$5" ${8:+"$8"} 2>&1)" || rc=$?
  check_case "$1" "$2" "$rc" "$3" "$out"
  calls="$(tr '\n' ';' <"$WORK/log")"
  [ "$calls" = "$4" ] || { printf 'FAIL %s: calls "%s", want "%s"\n' "$1" "$calls" "$4" >&2; exit 1; }
}

land "planted #1007 body without the trailer" 3 "must end with 'Signed-off-by: t <t@example.invalid>'" "" "$WORK/unsigned"
land "trailer of another identity" 3 "must end with" "" "$WORK/other"
land "empty head from a failed read" 2 "40 hex" "" "$WORK/signed" ""
land "subject without the number" 4 "(#7)" "" "$WORK/signed" "$SHA" "feat(prowlarr): select indexers"
STATE=BLOCKED land "not CLEAN" 5 "mergeStateStatus 'BLOCKED'" "pr view;" "$WORK/signed"
HEAD_OID=1111111111111111111111111111111111111111 land "head moved" 5 "not $SHA" "pr view;" "$WORK/signed"
land "signed body, dry run" 0 "dry-run" "pr view;" "$WORK/signed" "$SHA" "$SUBJECT" --dry-run
land "signed body merges" 0 "merged #7" "pr view;pr merge;pr view;api repos/lusoris/SnatchArr/commits/fedcba9876543210fedcba9876543210fedcba98;" "$WORK/signed"
MESSAGE="$SUBJECT" land "main lost the trailer" 6 "never rewrite main" \
  "pr view;pr merge;pr view;api repos/lusoris/SnatchArr/commits/fedcba9876543210fedcba9876543210fedcba98;" "$WORK/signed"

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

# main_commit_check.sh over real commits in the copy.
commit() {
  git -C "$WORK/repo" -c user.name="$1" -c user.email="$2" commit -q --allow-empty -F - >/dev/null
  git -C "$WORK/repo" rev-parse HEAD
}
# main_check NAME WANT_RC NEEDLE COMMIT: run main_commit_check.sh on COMMIT.
main_check() {
  rc=0
  out="$(sh "$WORK/repo/tools/ci/main_commit_check.sh" "$4" 2>&1)" || rc=$?
  check_case "$1" "$2" "$rc" "$3" "$out"
}
c="$(printf '%s\n\nBody.\n' "$SUBJECT" | commit Kilian k@example.invalid)"
main_check "unsigned maintainer squash on main" 1 "fails the commit-msg policy" "$c"
c="$(printf '%s\n\nBody.\n\nSigned-off-by: t <t@example.invalid>\n' "$SUBJECT" | commit Kilian k@example.invalid)"
main_check "signed maintainer squash on main" 0 "passes the commit-msg policy" "$c"
c="$(printf 'chore(main): release 0.8.1 (#7)\n' | commit 'snatcharr-release[bot]' '1+snatcharr-release[bot]@users.noreply.github.com')"
main_check "bot commit on main" 0 "bot author" "$c"

echo "squash_merge_test: all cases passed"
