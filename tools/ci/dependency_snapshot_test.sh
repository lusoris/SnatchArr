#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Tests tools/ci/dependency_snapshot.py:
# - check_snapshot fixtures (--check-fixture):
#   1. accepting fixture
#   2. refusing fixture (missing direct dependency)
#   3. boundary fixture (empty manifests)
# - --docker lookup and scan path with a stub docker binary:
#   4-7. negative and boundary --docker validation (relative, basename, non-executable, empty)
#   8. positive explicit executable --docker path runs planted + real scan and writes out.json
#   9. positive bare docker name resolved on PATH
#   10. negative directory named docker refused
#   11. negative bare docker not on PATH refused
#   12. bounded --docker lookup: injected blocked fake lookup, success, validation errors, cleanup
#
#   tools/ci/dependency_snapshot_test.sh
#
# Exit status: 0 all cases pass, 1 failure, 2 python3 not on PATH.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh disable=SC1091
. "$HERE/test_lib.sh"
PYTHON="$(command -v python3)" || PYTHON=""
if [ -z "$PYTHON" ]; then
  printf 'dependency_snapshot_test: python3 not on PATH\n' >&2
  exit 2
fi
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
SCRIPT="$HERE/dependency_snapshot.py"
FIXTURES="$HERE/testdata/dependency_snapshot"

cd "$REPO_ROOT"

# run_case NAME WANT_RC NEEDLE SNAPSHOT PKG: run the snapshot check on one fixture and assert its exit and output.
run_case() {
  name="$1"
  want_rc="$2"
  needle="$3"
  snap="$4"
  pkg="$5"

  got_rc=0
  out="$("$PYTHON" -I "$SCRIPT" --check-fixture "$snap" "$pkg" 2>&1)" || got_rc=$?

  check_case "$name" "$want_rc" "$got_rc" "$needle" "$out"

  printf 'ok   %s\n' "$name"
}

# 1. Accepting fixture
run_case "accepting fixture" 0 "Fixture check ok" \
  "$FIXTURES/accept_snapshot.json" "$FIXTURES/accept_package.json"

# 2. Refusing fixture (missing direct dep)
run_case "refusing fixture (missing direct dep)" 1 "direct dependencies missing from resolved snapshot: missing-dep" \
  "$FIXTURES/refuse_snapshot.json" "$FIXTURES/refuse_package.json"

# 3. Boundary fixture (empty manifests)
run_case "boundary fixture (empty manifests)" 1 "snapshot missing manifest for pnpm-lock.yaml" \
  "$FIXTURES/empty_snapshot.json" "$FIXTURES/empty_package.json"

fixture_raw="$(mktemp -d)"
fixture="$(CDPATH='' cd -- "$fixture_raw" && pwd)"
trap 'rm -rf "$fixture"' EXIT

mkdir -p "$fixture/web"
cp "$FIXTURES/accept_package.json" "$fixture/web/package.json"
cp "$FIXTURES/accept_snapshot.json" "$fixture/accept_snapshot.json"
printf 'lockfileVersion: "9.0"\n' > "$fixture/web/pnpm-lock.yaml"

SNAP_STUB_ARGS="$fixture/stub.args"
export SNAP_STUB_ARGS

# make_stub PATH: write a fake docker script that logs argv and emits accept_snapshot.json.
make_stub() {
  stub_path="$1"
  mkdir -p "$(dirname "$stub_path")"
  cat > "$stub_path" <<'EOF'
#!/bin/sh
set -eu
cat > /dev/null
printf '%s\n' "$*" >> "$SNAP_STUB_ARGS"
cat "${SNAP_STUB_ARGS%/*}/accept_snapshot.json"
EOF
  chmod 755 "$stub_path"
}

make_stub "$fixture/bin/docker"
make_stub "$fixture/bin/evil"
make_stub "$fixture/noexec/docker"
chmod 644 "$fixture/noexec/docker"

DIGEST="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
IMAGE="example.invalid/snapshot-fixture:1@sha256:$DIGEST"
SHA="1234567890abcdef1234567890abcdef12345678"
post_scan_check=""

# expect_scan NAME WANT NEEDLE RUNS ARGS...: run dependency_snapshot.py in fixture and assert exit, needle and stub run count.
expect_scan() {
  scan_name="$1"
  want_rc="$2"
  needle="$3"
  want_runs="$4"
  shift 4

  rm -f "$SNAP_STUB_ARGS" "$fixture/out.json"

  got_rc=0
  out="$(
    cd "$fixture" && "$PYTHON" -I "$SCRIPT" \
      --trivy "$IMAGE" \
      --out "$fixture/out.json" \
      --root "$fixture" \
      --docker "$fixture/bin/docker" \
      --sha "$SHA" \
      --ref refs/heads/main \
      "$@" 2>&1
  )" || got_rc=$?

  check_case "$scan_name" "$want_rc" "$got_rc" "$needle" "$out"

  got_runs=0
  if [ -f "$SNAP_STUB_ARGS" ]; then
    got_runs="$(wc -l < "$SNAP_STUB_ARGS" | tr -d '[:space:]')"
  fi
  if [ "$got_runs" -ne "$want_runs" ]; then
    printf 'FAIL %s: stub ran %s times, want %s\n%s\n' "$scan_name" "$got_runs" "$want_runs" "$out" >&2
    exit 1
  fi

  if [ -n "$post_scan_check" ]; then
    check_fn="$post_scan_check"
    post_scan_check=""
    "$check_fn" "$scan_name"
  fi

  printf 'ok   %s\n' "$scan_name"
}

# check_out_manifest NAME: assert out.json exists and carries the rewritten web/pnpm-lock.yaml manifest.
check_out_manifest() {
  if [ ! -f "$fixture/out.json" ] || ! "$PYTHON" -I -c 'import json, pathlib, sys
data = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
sys.exit(0 if "web/pnpm-lock.yaml" in data.get("manifests", {}) else 1)' "$fixture/out.json"; then
    printf 'FAIL %s: out.json missing or lacks manifest web/pnpm-lock.yaml\n' "$1" >&2
    exit 1
  fi
}

# 4. Negative: relative --docker
expect_scan "relative --docker refused" 1 "--docker" 0 --docker bin/docker

# 5. Negative: non-docker basename
expect_scan "non-docker basename refused" 1 "--docker" 0 --docker "$fixture/bin/evil"

# 6. Negative: non-executable --docker
expect_scan "non-executable --docker refused" 1 "--docker" 0 --docker "$fixture/noexec/docker"

# 7. Boundary: empty --docker
expect_scan "empty --docker refused" 1 "--docker" 0 --docker ""

# 8. Positive: explicit executable --docker path runs planted + real scan and writes rewritten out.json
post_scan_check="check_out_manifest"
expect_scan "explicit executable --docker accepted" 0 "Dependency snapshot: 2 resolved packages, 2 direct dependencies checked" 2 \
  --docker "$fixture/bin/docker"

# 9. Positive: bare docker name resolved on PATH
(
  PATH="$fixture/bin:$PATH"
  expect_scan "bare docker resolved on PATH" 0 "Dependency snapshot: 2 resolved packages, 2 direct dependencies checked" 2 \
    --docker docker
)

# 10. Negative: directory named docker
mkdir -p "$fixture/dir/docker"
chmod 755 "$fixture/dir/docker"
expect_scan "directory named docker refused" 1 "--docker" 0 --docker "$fixture/dir/docker"

# 11. Negative: bare docker not on PATH
mkdir -p "$fixture/empty"
(
  real_rm="$(command -v rm)"
  # rm ARGS...: forward rm to the resolved binary while PATH holds only the empty directory.
  rm() {
    "$real_rm" "$@"
  }
  # shellcheck disable=SC2123 # intentional search path holding only the empty fixture directory
  PATH="$fixture/empty"
  expect_scan "bare docker not on PATH refused" 1 "not found on PATH" 0 \
    --docker docker
)

# 12. Bounded lookup deadline with an in-memory blocked fake lookup (isolated import, in-test watchdog)
LOOKUP_TEST="$(dirname -- "$SCRIPT")/dependency_snapshot_lookup_test.py"
if "$PYTHON" -I "$LOOKUP_TEST"
then
  printf 'ok   %s\n' "bounded --docker lookup deadline"
else
  printf 'FAIL bounded --docker lookup deadline\n' >&2
  exit 1
fi
