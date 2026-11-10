#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for the roadmap table, against a stub gh that records its calls:
#   1. `roadmap_render.py --check-shape` passes on the committed doc with the forge unreachable
#      (the stub gh fails every call) and calls neither gh nor git.
#   2. roadmap_refresh_pr.sh: an unchanged doc proposes nothing and closes the bot's stale pull
#      request; a planted stale count writes the file on a temporary branch, points the bot branch
#      at the new commit (never at the base commit) and opens the pull request against the base
#      branch; with the bot's pull request open it opens none; the open lookup matches only the
#      bot's own head repository and base; a dry run sends no write; a failed write fails the run
#      and drops the temporary branch; a rerun resets a temporary branch an earlier attempt left.
#   3. Sign-off (#1036): the commit message the bot sends ends with the Signed-off-by trailer of
#      the App's bot account and passes the commit-msg policy; planted unsigned bot commits fail:
#      a script without the trailer is refused before any write, and one that also skips the
#      policy check sends a message this test refuses. An author other than the signed-off
#      account, an unknown account id and a bad APP_SLUG fail the run.
#
#   tools/ci/roadmap_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
failed=0

mkdir -p "$WORK/bin"
cat >"$WORK/bin/gh" <<'EOF'
#!/bin/sh
# One log line per call, so a multi-line commit message stays on the line of its call.
printf '%s\n' "$*" | tr '\n' ' ' >>"$STUB_LOG"
echo >>"$STUB_LOG"
[ -z "${STUB_DOWN:-}" ] || exit 1
for arg in "$@"; do
  case "$arg" in message=*) printf '%s\n' "${arg#message=}" >"$STUB_MSG" ;; esac
done
case "$*" in
  "api repos/o/r/pulls?"*) printf '%s\n' "${STUB_OPEN:-}" ;;
  "api users/snatcharr-test[bot] --jq .id") [ -n "${STUB_NO_ID:-}" ] || echo 7 ;;
  "api repos/o/r/git/ref/heads/"*) [ -n "${STUB_BRANCH:-}" ] || exit 1; echo abc ;;
  "api -X POST repos/o/r/git/refs -f ref=refs/heads/bot/roadmap-refresh-tmp-"*)
    [ -z "${STUB_TMP_LEFT:-}" ] || exit 1 ;;
  "api -X PUT"*)
    [ -z "${STUB_FAIL_PUT:-}" ] || exit 1
    echo "newsha ${STUB_AUTHOR:-7+snatcharr-test[bot]@users.noreply.github.com}" ;;
esac
exit 0
EOF
chmod +x "$WORK/bin/gh"

# pass records a case that behaved as recorded.
pass() { echo "ok   $1"; }
# fail records a case that did not and marks the run failed.
fail() { echo "FAIL $1"; failed=1; }

# Case 1: forge unreachable, shape check offline.
: >"$WORK/gh.log"
rc=0
(cd "$ROOT" && STUB_LOG="$WORK/gh.log" STUB_DOWN=1 GH_TOKEN=invalid PATH="$WORK/bin:$PATH" \
  python3 -I tools/ci/roadmap_render.py --check-shape >"$WORK/shape.out" 2>&1) || rc=$?
if [ "$rc" -eq 0 ] && [ ! -s "$WORK/gh.log" ]; then
  pass "check-shape green with the forge unreachable, no gh call"
else
  fail "check-shape with the forge unreachable: rc $rc, gh calls: $(wc -l <"$WORK/gh.log")"
  cat "$WORK/shape.out"
fi

# A scratch repository holding a committed roadmap doc.
repo="$WORK/repo"
git init -q "$repo"
mkdir -p "$repo/docs"
printf '| [0.9](https://github.com/o/r/milestone/14) | Scope | 87 |\n' >"$repo/docs/roadmap.md"
git -C "$repo" add docs/roadmap.md
git -C "$repo" -c user.name=t -c user.email=t@example.invalid -c commit.gpgsign=false \
  commit -q -m "docs: seed"
base="$(git -C "$repo" rev-parse HEAD)"

# refresh_with <script> <name> <expect rc> [VAR=value ...]: runs a refresh script, log in
# $WORK/<name>.log, the commit message it sent in $WORK/<name>.msg.
refresh_with() {
  script="$1"; name="$2"; want="$3"; shift 3
  : >"$WORK/$name.log"
  rm -f "$WORK/$name.msg"
  rc=0
  (cd "$repo" && env STUB_LOG="$WORK/$name.log" STUB_MSG="$WORK/$name.msg" PATH="$WORK/bin:$PATH" \
    REPO=o/r BASE_SHA="$base" BASE_BRANCH=trunk RUN_ID=7 APP_SLUG=snatcharr-test "$@" \
    sh "$script" >"$WORK/$name.out" 2>&1) || rc=$?
  [ "$rc" -eq "$want" ]
}
# refresh <name> <expect rc> [VAR=value ...]: runs the repository's refresh script.
refresh() { refresh_with "$HERE/roadmap_refresh_pr.sh" "$@"; }
signoff='Signed-off-by: snatcharr-test[bot] <7+snatcharr-test[bot]@users.noreply.github.com>'
# signed_off <case>: the message sent in that case passes the commit-msg policy and its last line
# is the sign-off of the stub App's bot account.
signed_off() {
  [ -f "$WORK/$1.msg" ] && sh "$ROOT/.config/hooks/commit-msg.sh" "$WORK/$1.msg" >/dev/null 2>&1 \
    && [ "$(sed -e '/^[[:space:]]*$/d' "$WORK/$1.msg" | tail -n 1)" = "$signoff" ]
}
# called <case> <pattern>: the stub gh logged a matching call in that case.
called() { grep -q -- "$2" "$WORK/$1.log"; }
# writes <case>: prints how many write calls the stub gh logged in that case.
writes() { awk '/-X |^pr c/ { n++ } END { print n + 0 }' "$WORK/$1.log"; }
tmp_ref="repos/o/r/git/refs/heads/bot/roadmap-refresh-tmp-7"
bot_ref="repos/o/r/git/refs/heads/bot/roadmap-refresh "

if refresh unchanged 0 && [ "$(writes unchanged)" -eq 0 ] \
  && called unchanged "pulls?head=o:bot/roadmap-refresh&base=trunk&state=open"; then
  pass "unchanged doc, no pull request: no write; lookup limited to own head and base"
else
  fail "unchanged doc"; cat "$WORK/unchanged.out"
fi

if refresh unchanged_open 0 STUB_OPEN=42 && called unchanged_open "^pr close 42" \
  && [ "$(writes unchanged_open)" -eq 1 ]; then
  pass "unchanged doc, bot pull request open: stale pull request closed"
else
  fail "unchanged doc with an open pull request"; cat "$WORK/unchanged_open.out"
fi

# Planted stale count: the render moved 87 to 88.
sed 's/| 87 |/| 88 |/' "$repo/docs/roadmap.md" >"$WORK/stale"
cp "$WORK/stale" "$repo/docs/roadmap.md"

if refresh fresh 0 \
  && called fresh "-X POST repos/o/r/git/refs -f ref=refs/heads/bot/roadmap-refresh-tmp-7 -f sha=$base" \
  && called fresh "-X PUT repos/o/r/contents/docs/roadmap.md .* -f branch=bot/roadmap-refresh-tmp-7" \
  && called fresh "-X POST repos/o/r/git/refs -f ref=refs/heads/bot/roadmap-refresh -f sha=newsha" \
  && called fresh "-X DELETE $tmp_ref" \
  && called fresh "^pr create --repo o/r --base trunk" && signed_off fresh; then
  pass "planted stale count: signed-off commit on a temporary branch, bot branch created at it, pull request opened"
else
  fail "planted stale count, no bot branch"; cat "$WORK/fresh.out"
fi

if refresh update 0 STUB_BRANCH=1 STUB_OPEN=42 \
  && called update "-X PATCH ${bot_ref}-f sha=newsha" \
  && ! called update "${bot_ref}-f sha=$base" && ! called update "^pr create"; then
  pass "planted stale count, bot pull request open: bot branch moved to the new commit only, no second pull request"
else
  fail "planted stale count, pull request open"; cat "$WORK/update.out"
fi

if refresh rerun 0 STUB_TMP_LEFT=1 \
  && called rerun "-X PATCH $tmp_ref -f sha=$base -F force=true" \
  && called rerun "^pr create --repo o/r --base trunk"; then
  pass "rerun with a leftover temporary branch: branch reset to the base, pull request proposed"
else
  fail "rerun with a leftover temporary branch"; cat "$WORK/rerun.out"
fi

if refresh dry 0 DRY_RUN=1 && [ "$(writes dry)" -eq 0 ] \
  && grep -q "dry-run: gh pr create" "$WORK/dry.out"; then
  pass "dry run: writes printed, none sent"
else
  fail "dry run"; cat "$WORK/dry.out"
fi

if refresh putfail 1 STUB_FAIL_PUT=1 && ! called putfail "^pr create" \
  && called putfail "-X DELETE $tmp_ref" && ! called putfail "$bot_ref"; then
  pass "failed file write fails the run, drops the temporary branch, leaves the bot branch"
else
  fail "failed file write"; cat "$WORK/putfail.out"
fi

if refresh badrepo 2 REPO=bad && refresh nobase 2 BASE_BRANCH= && refresh noslug 2 APP_SLUG= \
  && refresh badslug 2 'APP_SLUG=Bad Slug'; then
  pass "bad REPO, empty BASE_BRANCH, empty or malformed APP_SLUG refused"
else
  fail "bad input"
fi

# Planted unsigned bot commits: copies of the script in a scratch tree that carries the policy.
mutants="$WORK/mutants"
mkdir -p "$mutants/tools/ci" "$mutants/.config/hooks"
cp "$ROOT/.config/hooks/commit-msg.sh" "$mutants/.config/hooks/"
# mutant <name> <sed program>: writes the mutated script; fails when the program changed nothing.
mutant() {
  sed -e "$2" "$HERE/roadmap_refresh_pr.sh" >"$mutants/tools/ci/$1.sh"
  ! cmp -s "$HERE/roadmap_refresh_pr.sh" "$mutants/tools/ci/$1.sh"
}
# shellcheck disable=SC2016 # sed programs: the $title and $root text must stay literal
drop_trailer='s|^message=.*Signed-off-by.*$|message="$title"|'
# shellcheck disable=SC2016 # same: literal sed program text
skip_policy='s|^sh "$root/.config/hooks/commit-msg.sh" "$msgfile"|true|'

if mutant unsigned "$drop_trailer" \
  && refresh_with "$mutants/tools/ci/unsigned.sh" unsigned 1 && [ "$(writes unsigned)" -eq 0 ] \
  && grep -q "fails the commit-msg policy" "$WORK/unsigned.out"; then
  pass "planted bot commit without Signed-off-by: refused by the commit-msg policy before any write"
else
  fail "planted bot commit without Signed-off-by"; cat "$WORK/unsigned.out"
fi

if mutant unchecked "$drop_trailer;$skip_policy" \
  && refresh_with "$mutants/tools/ci/unchecked.sh" unchecked 0 && [ -f "$WORK/unchecked.msg" ] \
  && ! signed_off unchecked; then
  pass "planted bot commit without Signed-off-by or policy check: the message it sends is refused here"
else
  fail "planted bot commit without Signed-off-by or policy check went unnoticed"; cat "$WORK/unchecked.out"
fi

if refresh foreign 1 STUB_AUTHOR=someone@example.invalid && called foreign "-X DELETE $tmp_ref" \
  && ! called foreign "$bot_ref" && ! called foreign "^pr create" \
  && grep -q "is not the sign-off" "$WORK/foreign.out"; then
  pass "commit author other than the signed-off account: run fails, temporary branch dropped, bot branch left"
else
  fail "commit author other than the signed-off account"; cat "$WORK/foreign.out"
fi

if refresh noid 1 STUB_NO_ID=1 && [ "$(writes noid)" -eq 0 ]; then
  pass "no account id for the App's bot: run fails before any write"
else
  fail "no account id for the App's bot"; cat "$WORK/noid.out"
fi

exit "$failed"
