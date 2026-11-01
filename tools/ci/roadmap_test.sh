#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for the roadmap table, against a stub gh that records its calls:
#   1. `roadmap_render.py --check-shape` passes on the committed doc with the forge unreachable
#      (the stub gh fails every call) and calls neither gh nor git.
#   2. roadmap_refresh_pr.sh: an unchanged doc proposes nothing; a planted stale count creates
#      the branch, writes the file and opens the pull request; with the branch and a pull request
#      already there it resets the branch and opens none; a dry run sends no write; a failed write
#      fails the run.
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
  "api repos/o/r/git/ref/heads/"*) [ -n "${STUB_BRANCH:-}" ] || exit 1; echo abc ;;
  "pr list"*) printf '%s\n' "${STUB_OPEN:-}" ;;
  "api -X PUT"*) [ -z "${STUB_FAIL_PUT:-}" ] || exit 1 ;;
esac
exit 0
EOF
chmod +x "$WORK/bin/gh"

pass() { echo "ok   $1"; }
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
    BASE_SHA="$base" "$@" sh "$HERE/roadmap_refresh_pr.sh" >"$WORK/$name.out" 2>&1) || rc=$?
  [ "$rc" -eq "$want" ]
}
called() { grep -q -- "$2" "$WORK/$1.log"; }

if refresh unchanged 0 && [ ! -s "$WORK/unchanged.log" ]; then
  pass "unchanged doc: no gh call"
else
  fail "unchanged doc"
fi

# Planted stale count: the render moved 87 to 88.
sed 's/| 87 |/| 88 |/' "$repo/docs/roadmap.md" >"$WORK/stale" && cp "$WORK/stale" "$repo/docs/roadmap.md"

if refresh fresh 0 && called fresh "api -X POST repos/o/r/git/refs" \
  && called fresh "api -X PUT repos/o/r/contents/docs/roadmap.md" && called fresh "pr create"; then
  pass "planted stale count: branch created, file written, pull request opened"
else
  fail "planted stale count, no branch"; cat "$WORK/fresh.out"
fi

if refresh update 0 STUB_BRANCH=1 STUB_OPEN=42 && called update "api -X PATCH repos/o/r/git/refs/heads/bot/roadmap-refresh" \
  && called update "api -X PUT" && ! called update "pr create"; then
  pass "planted stale count, branch and pull request open: branch reset, no second pull request"
else
  fail "planted stale count, pull request open"; cat "$WORK/update.out"
fi

if refresh dry 0 DRY_RUN=1 && ! called dry "-X P" && ! called dry "pr create" \
  && grep -q "dry-run: gh pr create" "$WORK/dry.out"; then
  pass "dry run: writes printed, none sent"
else
  fail "dry run"; cat "$WORK/dry.out"
fi

if refresh putfail 1 STUB_FAIL_PUT=1 && ! called putfail "pr create"; then
  pass "failed file write fails the run, no pull request"
else
  fail "failed file write"
fi

if refresh badrepo 2 REPO=bad; then
  pass "bad REPO refused"
else
  fail "bad REPO"
fi

exit "$failed"
