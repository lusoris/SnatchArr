#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Tests tools/ci/dependency_snapshot.py:
# - check_snapshot fixtures (--check-fixture):
#   1. accepting fixture
#   2. refusing fixture (missing direct dependency)
#   3. boundary fixture (empty manifests)
# - --docker lookup and full scan path with a stub docker binary:
#   4-6, 11. negative and boundary --docker validation (relative, basename, non-executable, empty)
#   7-10, 12. negative and boundary metadata flag validation (--sha, --ref, --run-id, --workflow)
#   13a. positive explicit executable --docker path runs planted + real scan and writes out.json
#   13b. boundary empty optional metadata flags omitted from the docker client environment (ambient GITHUB_WORKFLOW, GITHUB_JOB, GITHUB_RUN_ID)
#   14. positive full metadata passed via the docker client environment (planted + real scan, clean argv, out.json)
#   15. positive bare docker name resolved on PATH
#   16. negative invalid --job refused
#   17. negative directory named docker refused
#   18. negative bare docker not on PATH refused
#   19. bounded --docker lookup: injected blocked fake lookup, success, validation errors, cleanup
#   20. --workflow class at every position and --ref via git check-ref-format: literal env transport,
#       long nested/plus/Unicode refs, malformed refs, missing git, failing git
# Native Windows Python skips only the POSIX execute-bit check and the POSIX shebang stub launches.
#
#   tools/ci/dependency_snapshot_test.sh
#
# Exit status: 0 all cases pass, 1 failure, 2 python3 or git not on PATH.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh disable=SC1091
. "$HERE/test_lib.sh"
PYTHON="$(command -v python3)" || PYTHON=""
if [ -z "$PYTHON" ]; then
  printf 'dependency_snapshot_test: python3 not on PATH\n' >&2
  exit 2
fi
if ! command -v git > /dev/null 2>&1; then
  printf 'dependency_snapshot_test: git not on PATH\n' >&2
  exit 2
fi
# Check the Python host, not the shell: Git Bash can run native Windows Python.
python_os="$("$PYTHON" -I -c 'import os, sys; sys.stdout.write(os.name)')"
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
SNAP_STUB_ENV="$fixture/stub.env"
export SNAP_STUB_ARGS SNAP_STUB_ENV

# make_stub PATH: write a fake docker script that logs argv and GitHub env vars, then emits accept_snapshot.json.
make_stub() {
  stub_path="$1"
  mkdir -p "$(dirname "$stub_path")"
  cat > "$stub_path" <<'EOF'
#!/bin/sh
set -eu
cat > /dev/null
printf '%s\n' "$*" >> "$SNAP_STUB_ARGS"
printf 'sha=%s ref=%s wf=%s job=%s run=%s\n' \
  "${GITHUB_SHA-unset}" "${GITHUB_REF-unset}" "${GITHUB_WORKFLOW-unset}" \
  "${GITHUB_JOB-unset}" "${GITHUB_RUN_ID-unset}" >> "$SNAP_STUB_ENV"
cat "${SNAP_STUB_ARGS%/*}/accept_snapshot.json"
EOF
  chmod 755 "$stub_path"
}

if [ "$python_os" = nt ]; then
  # Keep real files for the relative-path, wrong-basename and metadata-refusal checks; neither is launched.
  mkdir -p "$fixture/bin"
  : > "$fixture/bin/docker"
  : > "$fixture/bin/evil"
  chmod 755 "$fixture/bin/docker"
else
  make_stub "$fixture/bin/docker"
  make_stub "$fixture/bin/evil"
  make_stub "$fixture/noexec/docker"
  chmod 644 "$fixture/noexec/docker"
fi

DIGEST="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
IMAGE="example.invalid/snapshot-fixture:1@sha256:$DIGEST"
SHA="1234567890abcdef1234567890abcdef12345678"
ZERO_SHA="0000000000000000000000000000000000000000"
ARGV_PREFIX="run --rm -i -e GITHUB_SHA -e GITHUB_REF -e GITHUB_WORKFLOW -e GITHUB_JOB -e GITHUB_RUN_ID --entrypoint sh"
post_scan_check=""

# expect_scan NAME WANT NEEDLE RUNS ARGS...: run dependency_snapshot.py in fixture and assert exit, needle and stub run count.
expect_scan() {
  scan_name="$1"
  want_rc="$2"
  needle="$3"
  want_runs="$4"
  shift 4

  rm -f "$SNAP_STUB_ARGS" "$SNAP_STUB_ENV" "$fixture/out.json"

  got_rc=0
  out="$(
    cd "$fixture" && "$PYTHON" -I "$SCRIPT" \
      --trivy "$IMAGE" \
      --out "$fixture/out.json" \
      --root "$fixture" \
      --docker "$fixture/bin/docker" \
      --sha "$SHA" \
      --ref refs/heads/main \
      --workflow '' \
      --job '' \
      --run-id '' \
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

# check_empty_job_vars NAME: assert the real scan leaves empty --workflow and --job unset in the docker client env.
check_empty_job_vars() {
  env_line2="$(sed -n '2p' "$SNAP_STUB_ENV")"
  want_env2="sha=$SHA ref=refs/heads/main wf=unset job=unset run=1"
  if [ "$env_line2" != "$want_env2" ]; then
    printf 'FAIL %s: second env line = "%s", want "%s"\n' "$1" "$env_line2" "$want_env2" >&2
    exit 1
  fi
}

# check_argv_clean NAME IDX VALUE...: assert argv line IDX has the bare -e prefix and contains none of the VALUEs outside the pinned image reference.
check_argv_clean() {
  cac_name="$1"
  cac_idx="$2"
  shift 2
  cac_line="$(sed -n "${cac_idx}p" "$SNAP_STUB_ARGS")"
  case "$cac_line" in
    "$ARGV_PREFIX "*) ;;
    *)
      printf 'FAIL %s: argv line %s missing bare -e prefix: %s\n' "$cac_name" "$cac_idx" "$cac_line" >&2
      exit 1
      ;;
  esac
  case "$cac_line" in
    *"$IMAGE"*) ;;
    *)
      printf 'FAIL %s: argv line %s lacks pinned image: %s\n' "$cac_name" "$cac_idx" "$cac_line" >&2
      exit 1
      ;;
  esac
  cac_stripped="${cac_line%%"$IMAGE"*}${cac_line#*"$IMAGE"}"
  for cac_value in "$@"; do
    case "$cac_stripped" in
      *"$cac_value"*)
        printf 'FAIL %s: argv line %s leaked metadata value "%s": %s\n' "$cac_name" "$cac_idx" "$cac_value" "$cac_line" >&2
        exit 1
        ;;
    esac
  done
}

# check_unicode_metadata NAME: assert a Unicode/colon workflow name reaches the client env only, never argv.
check_unicode_metadata() {
  uni_env="$(sed -n '2p' "$SNAP_STUB_ENV")"
  want_uni="sha=$SHA ref=refs/heads/main wf=Sécurité: Scan job=ci-build run=987654321"
  if [ "$uni_env" != "$want_uni" ]; then
    printf 'FAIL %s: env line = "%s", want "%s"\n' "$1" "$uni_env" "$want_uni" >&2
    exit 1
  fi
  for idx in 1 2; do
    check_argv_clean "$1" "$idx" "$SHA" "$ZERO_SHA" "refs/heads/main" "Sécurité" "Scan" "ci-build" "987654321"
  done
}

# check_full_metadata NAME: assert planted/real env isolation, clean argv without env values, and rewritten out.json.
check_full_metadata() {
  env_line1="$(sed -n '1p' "$SNAP_STUB_ENV")"
  want_env1="sha=$ZERO_SHA ref=refs/heads/main wf=unset job=unset run=unset"
  if [ "$env_line1" != "$want_env1" ]; then
    printf 'FAIL %s: planted env line = "%s", want "%s"\n' "$1" "$env_line1" "$want_env1" >&2
    exit 1
  fi
  env_line2="$(sed -n '2p' "$SNAP_STUB_ENV")"
  want_env2="sha=$SHA ref=refs/heads/main wf=Security Scan job=dependency-submission run=12345678"
  if [ "$env_line2" != "$want_env2" ]; then
    printf 'FAIL %s: real env line = "%s", want "%s"\n' "$1" "$env_line2" "$want_env2" >&2
    exit 1
  fi
  for idx in 1 2; do
    check_argv_clean "$1" "$idx" "$SHA" "$ZERO_SHA" "refs/heads/main" \
      "Security Scan" "Security" "dependency-submission" "12345678"
  done
  check_out_manifest "$1"
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
if [ "$python_os" = nt ]; then
  printf 'skip %s: native Windows does not use POSIX execute bits\n' "non-executable --docker refused"
else
  expect_scan "non-executable --docker refused" 1 "--docker" 0 --docker "$fixture/noexec/docker"
fi

# 7. Negative: short --sha (39 hex chars)
expect_scan "short --sha refused" 1 "--sha" 0 --sha "1234567890abcdef1234567890abcdef1234567"

# 8. Negative: injected --ref
expect_scan "injected --ref refused" 1 "--ref" 0 --ref "refs/heads/main -e X=1"

# 9. Negative: non-numeric --run-id
expect_scan "non-numeric --run-id refused" 1 "--run-id" 0 --run-id 12a

# 10. Negative: injected --workflow
expect_scan "injected --workflow refused" 1 "--workflow" 0 --workflow "Security Scan;id"

# 11. Boundary: empty --docker
expect_scan "empty --docker refused" 1 "--docker" 0 --docker ""

# 12. Boundary: uppercase --sha (40 uppercase hex chars)
expect_scan "uppercase --sha refused" 1 "--sha" 0 --sha "ABCDEF0123456789ABCDEF0123456789ABCDEF01"

# check_env2 NAME: assert the real scan's client env line is exactly $WANT_ENV2.
check_env2() {
  got_env2="$(sed -n '2p' "$SNAP_STUB_ENV")"
  if [ "$got_env2" != "$WANT_ENV2" ]; then
    printf 'FAIL %s: env line = "%s", want "%s"\n' "$1" "$got_env2" "$WANT_ENV2" >&2
    exit 1
  fi
}

# expect_env2 NAME WANT_LINE ARGS...: assert a successful scan whose real client env line is WANT_LINE.
expect_env2() {
  ee_name="$1"
  WANT_ENV2="$2"
  shift 2
  post_scan_check="check_env2"
  expect_scan "$ee_name" 0 "Dependency snapshot: 2 resolved packages" 2 "$@"
}

if [ "$python_os" = nt ]; then
  printf 'skip %s: native Windows Python cannot launch the POSIX shebang Docker fixture\n' \
    "explicit executable --docker accepted" \
    "empty optional flags omitted from env" \
    "full metadata via docker client env" \
    "unicode workflow with colon accepted" \
    "128-char --workflow accepted" \
    "single leading parenthesis --workflow accepted" \
    "leading colon --workflow accepted" \
    "git-valid --ref accepted" \
    "bare docker resolved on PATH"
else
  # 13a. Positive: explicit executable --docker path runs planted + real scan and writes rewritten out.json
  post_scan_check="check_out_manifest"
  expect_scan "explicit executable --docker accepted" 0 "Dependency snapshot: 2 resolved packages, 2 direct dependencies checked" 2 \
    --docker "$fixture/bin/docker"

  # 13b. Boundary: empty --job and --workflow; ambient job variables of the host or CI job stay out of the client environment
  (
    GITHUB_WORKFLOW=ambient-wf
    GITHUB_JOB=ambient-job
    GITHUB_RUN_ID=999
    export GITHUB_WORKFLOW GITHUB_JOB GITHUB_RUN_ID
    post_scan_check="check_empty_job_vars"
    expect_scan "empty optional flags omitted from env" 0 "Dependency snapshot: 2 resolved packages, 2 direct dependencies checked" 2 \
      --job '' --workflow '' --run-id 1
  )

  # 14. Positive twin: full metadata passed via client env, clean argv, rewritten out.json
  post_scan_check="check_full_metadata"
  expect_scan "full metadata via docker client env" 0 "Dependency snapshot: 2 resolved packages, 2 direct dependencies checked" 2 \
    --workflow 'Security Scan' --job dependency-submission --run-id 12345678

  # 14b. Positive: Unicode letters and colon in --workflow are accepted and stay out of argv
  post_scan_check="check_unicode_metadata"
  expect_scan "unicode workflow with colon accepted" 0 "Dependency snapshot: 2 resolved packages, 2 direct dependencies checked" 2 \
    --workflow 'Sécurité: Scan' --job ci-build --run-id 987654321

  # 14c. Boundary: workflow name of exactly 128 characters accepted
  wf_128="$(printf '%0128d' 0 | tr 0 W)"
  expect_scan "128-char --workflow accepted" 0 "Dependency snapshot: 2 resolved packages" 2 --workflow "$wf_128"

  # 14d2. Positive: first-position punctuation and length 1 in --workflow arrive literally in the client env
  expect_env2 "single leading parenthesis --workflow accepted" \
    "sha=$SHA ref=refs/heads/main wf=( job=unset run=unset" --workflow '('
  expect_env2 "leading colon --workflow accepted" \
    "sha=$SHA ref=refs/heads/main wf=: Scan job=unset run=unset" --workflow ': Scan'

  # 14d3. Positive: --ref is decided by git: plus, Unicode and a 316-character nested ref pass
  seg="$(printf '%050d' 0 | tr 0 s)"
  long_ref="refs/heads/$seg/$seg/$seg/$seg/$seg/$seg"
  for tref in 'refs/heads/feature+x' 'refs/heads/fé/日本語' "$long_ref"; do
    expect_env2 "git-valid --ref of length ${#tref} accepted" \
      "sha=$SHA ref=$tref wf=unset job=unset run=unset" --ref "$tref"
  done

  # 15. Positive: bare docker name resolved on PATH
  (
    PATH="$fixture/bin:$PATH"
    expect_scan "bare docker resolved on PATH" 0 "Dependency snapshot: 2 resolved packages, 2 direct dependencies checked" 2 \
      --docker docker
  )
fi

# 14c2. Boundary: workflow name of 129 characters refused
wf_129="$(printf '%0129d' 0 | tr 0 W)"
expect_scan "129-char --workflow refused" 1 "--workflow" 0 --workflow "$wf_129"

# 14d. Negative: controls and shell metacharacters in --workflow remain refused
expect_scan "tab in --workflow refused" 1 "--workflow" 0 --workflow "$(printf 'Security\tScan')"
expect_scan "newline in --workflow refused" 1 "--workflow" 0 --workflow "$(printf 'Security\nScan')"
expect_scan "NUL-adjacent control in --workflow refused" 1 "--workflow" 0 --workflow "$(printf 'Security\001Scan')"
expect_scan "command substitution in --workflow refused" 1 "--workflow" 0 --workflow "Scan \$()"

# 14d4. Negative: malformed --ref names refused by git check-ref-format or prefix check
for tref in 'heads/main' 'refs/heads/a..b' 'refs/heads/x.lock' 'refs/heads/a@{b}' 'refs/heads/a//b'; do
  expect_scan "malformed --ref $tref refused" 1 "invalid --ref value" 0 --ref "$tref"
done

# 14e. Negative: the relaxed workflow rule does not loosen --job or --run-id
expect_scan "colon in --job refused" 1 "--job" 0 --job 'ci:build'
expect_scan "unicode --job refused" 1 "--job" 0 --job 'sécurité'
expect_scan "unicode digits in --run-id refused" 1 "--run-id" 0 --run-id '١٢٣'

# 16. Negative: invalid --job
expect_scan "invalid --job refused" 1 "--job" 0 --job 'dependency-submission;id'

# 17. Negative: directory named docker
mkdir -p "$fixture/dir/docker"
chmod 755 "$fixture/dir/docker"
expect_scan "directory named docker refused" 1 "--docker" 0 --docker "$fixture/dir/docker"

# 18. Negative: bare docker not on PATH, missing git and failing git
mkdir -p "$fixture/empty"
if [ "$python_os" != nt ]; then
  mkdir -p "$fixture/gitfail"
  printf '#!/bin/sh\nexit 128\n' > "$fixture/gitfail/git"
  chmod 755 "$fixture/gitfail/git"
fi
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
  expect_scan "missing git refused" 1 "git not found" 0
  if [ "$python_os" = nt ]; then
    printf 'skip %s: native Windows Python cannot launch the POSIX shebang git fixture\n' \
      "git operational failure refused"
  else
    # shellcheck disable=SC2123 # intentional search path holding only a git stub that exits 128
    PATH="$fixture/gitfail"
    expect_scan "git operational failure refused" 1 "git check-ref-format exited 128" 0
  fi
)

# 19. Bounded lookup deadline with an in-memory blocked fake lookup (isolated import, in-test watchdog)
LOOKUP_TEST="$(dirname -- "$SCRIPT")/dependency_snapshot_lookup_test.py"
if "$PYTHON" -I "$LOOKUP_TEST"
then
  printf 'ok   %s\n' "bounded --docker lookup deadline"
else
  printf 'FAIL bounded --docker lookup deadline\n' >&2
  exit 1
fi
