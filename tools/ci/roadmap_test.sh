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
#      and drops the temporary branch.
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
echo "$*" >>"$STUB_LOG"
[ -z "${STUB_DOWN:-}" ] || exit 1
case "$*" in
  "api repos/o/r/pulls?"*) printf '%s\n' "${STUB_OPEN:-}" ;;
  "api repos/o/r/git/ref/heads/"*) [ -n "${STUB_BRANCH:-}" ] || exit 1; echo abc ;;
  "api -X PUT"*) [ -z "${STUB_FAIL_PUT:-}" ] || exit 1; echo newsha ;;
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

# refresh <name> <expect rc> [VAR=value ...]: runs the script, log in $WORK/<name>.log.
refresh() {
  name="$1"; want="$2"; shift 2
  : >"$WORK/$name.log"
  rc=0
  (cd "$repo" && env STUB_LOG="$WORK/$name.log" PATH="$WORK/bin:$PATH" REPO=o/r \
    BASE_SHA="$base" BASE_BRANCH=trunk RUN_ID=7 "$@" sh "$HERE/roadmap_refresh_pr.sh" \
    >"$WORK/$name.out" 2>&1) || rc=$?
  [ "$rc" -eq "$want" ]
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
  && called fresh "^pr create --repo o/r --base trunk"; then
  pass "planted stale count: commit on a temporary branch, bot branch created at it, pull request opened"
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

if refresh badrepo 2 REPO=bad && refresh nobase 2 BASE_BRANCH=; then
  pass "bad REPO and empty BASE_BRANCH refused"
else
  fail "bad input"
fi

exit "$failed"
