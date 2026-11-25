#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Replay concurrent gitleaks setup while an extracted candidate remains open for writing.
set -eu
export LC_ALL=C

cache="${1:?usage: gitleaks_scan_test.sh CACHE_DIR}"
HERE="$(cd "$(dirname "$0")" && pwd)"
platform="$(uname -s)/$(uname -m)"
if [ "$platform" != "Linux/x86_64" ]; then
  echo "gitleaks scan test: skipped on $platform: the gitleaks pin is the linux x64 archive"
  exit 0
fi

pin_count="$(awk '/^GITLEAKS_VERSION=/ { count += 1 } END { print count + 0 }' "$HERE/gitleaks_scan.sh")"
version="$(sed -n 's/^GITLEAKS_VERSION=//p' "$HERE/gitleaks_scan.sh")"
if [ "$pin_count" -ne 1 ]; then
  echo 'BAD pin' >&2
  exit 1
fi
case "$version" in
[0-9]*)
  case "$version" in
  *[!0-9.]*)
    echo 'BAD pin' >&2
    exit 1
    ;;
  esac
  ;;
*)
  echo 'BAD pin' >&2
  exit 1
  ;;
esac
archive="$cache/gitleaks_${version}_linux_x64.tar.gz"
if [ ! -f "$archive" ]; then
  echo "gitleaks scan test: skipped: no gitleaks linux x64 archive in $cache"
  exit 0
fi
work="$(mktemp -d)"
repo="$work/repo"
fixture_cache="$repo/cache"
ready="$work/ready"
hold="$work/hold"
stub="$work/stub"
worker_pid=""
failed=0

# cleanup ARGS: remove the fixture workspace and wait for the background scanner.
cleanup() {
  rc=$?
  rm -f "$hold"
  if [ -n "$worker_pid" ]; then
    if wait "$worker_pid"; then
      :
    fi
  fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup 0

# ok ARGS: report a passing concurrency assertion.
ok() {
  echo "ok $1"
}

# bad ARGS: record and report a failed concurrency assertion.
bad() {
  echo "BAD $1" >&2
  failed=$((failed + 1))
}

# fixture_env ARGS: run a fixture command with isolated Git configuration.
fixture_env() {
  env -i PATH="$PATH" HOME="$work/home" GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 "$@"
}

# fixture_git ARGS: run Git with isolated configuration in the fixture repository.
fixture_git() {
  fixture_env git -C "$repo" "$@"
}

# run_scan ARGS: run the scanner against the disposable fixture repository.
run_scan() {
  (
    cd "$repo"
    fixture_env sh tools/ci/gitleaks_scan.sh "$fixture_cache"
  )
}

# wait_for_ready ARGS: wait a bounded number of polls for the extraction stub.
wait_for_ready() {
  checks=0
  while [ "$checks" -lt 600 ]; do
    if [ -f "$ready" ]; then
      return 0
    fi
    sleep 0.1
    checks=$((checks + 1))
  done
  return 1
}

mkdir -p "$repo/tools/ci" "$fixture_cache" "$work/home" "$stub"
cp "$HERE/gitleaks_scan.sh" "$HERE/secret_scan.py" "$repo/tools/ci/"
cp "$archive" "$fixture_cache/"
fixture_git init --quiet --initial-branch main
printf 'fixture\n' > "$repo/README.md"
fixture_git add README.md tools/ci
fixture_git -c user.name=fixture -c user.email=fixture@invalid -c commit.gpgsign=false commit --quiet --message fixture

if run_scan; then
  ok 'initial scanner setup'
else
  bad 'initial scanner setup'
fi

real_tar="$(command -v tar)"
cat > "$stub/tar" << 'EOF'
#!/bin/sh
set -eu
"$REAL_TAR" "$@"
directory=""
previous=""
for argument in "$@"; do
	if [ "$previous" = "-C" ]; then
		directory="$argument"
	fi
	previous="$argument"
done
if [ -z "$directory" ]; then
	echo 'stub tar: missing -C directory' >&2
	exit 1
fi
exec 3>>"$directory/gitleaks"
: > "$READY"
checks=0
while [ -e "$HOLD" ] && [ "$checks" -lt 600 ]; do
	sleep 0.1
	checks=$((checks + 1))
done
exec 3>&-
EOF
chmod 700 "$stub/tar"
: > "$hold"
(
  cd "$repo"
  env -i PATH="$stub:$PATH" HOME="$work/home" GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
    REAL_TAR="$real_tar" READY="$ready" HOLD="$hold" sh tools/ci/gitleaks_scan.sh "$fixture_cache"
) > "$work/second.out" 2>&1 &
worker_pid=$!

if wait_for_ready; then
  ok 'second extraction reached writer hold'
else
  bad 'second extraction reached writer hold'
fi
final="$fixture_cache/gitleaks-$version/gitleaks"
rc=0
reported_version="$("$final" version 2>&1)" || rc=$?
if [ "$rc" -eq 0 ] && [ "$reported_version" = "$version" ]; then
  ok 'previous scanner executes during staged extraction'
else
  echo "$reported_version" >&2
  bad 'previous scanner executes during staged extraction'
fi
rm -f "$hold"
if wait "$worker_pid"; then
  ok 'second scanner setup'
else
  cat "$work/second.out" >&2
  bad 'second scanner setup'
fi
worker_pid=""

rc=0
reported_version="$("$final" version 2>&1)" || rc=$?
if [ "$rc" -eq 0 ] && [ "$reported_version" = "$version" ]; then
  ok 'final scanner executes'
else
  echo "$reported_version" >&2
  bad 'final scanner executes'
fi
set -- "$fixture_cache"/extract.*
if [ -e "$1" ]; then
  bad 'staging directories removed'
else
  ok 'staging directories removed'
fi
stub_fail="$work/stub-fail"
mkdir -p "$stub_fail"
cat > "$stub_fail/tar" << 'EOF'
#!/bin/sh
set -eu
"$REAL_TAR" "$@"
exit 1
EOF
chmod 700 "$stub_fail/tar"
cp "$final" "$work/before"
rc=0
(
  cd "$repo"
  env -i PATH="$stub_fail:$PATH" HOME="$work/home" GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
    REAL_TAR="$real_tar" sh tools/ci/gitleaks_scan.sh "$fixture_cache"
) > "$work/fail.out" 2> "$work/fail.err" || rc=$?
if [ "$rc" -eq 2 ]; then
  ok 'failed extraction exits 2'
else
  bad 'failed extraction exits 2'
fi
if grep -F 'secret-scan: scanner setup failed' "$work/fail.err" > /dev/null; then
  ok 'failed extraction reports setup failure'
else
  bad 'failed extraction reports setup failure'
fi
set -- "$fixture_cache"/extract.*
if [ -e "$1" ]; then
  bad 'failed extraction removes staging directory'
else
  ok 'failed extraction removes staging directory'
fi
rc=0
reported_version="$("$final" version 2>&1)" || rc=$?
if cmp -s "$final" "$work/before" && [ "$rc" -eq 0 ] && [ "$reported_version" = "$version" ]; then
  ok 'installed scanner unchanged after failed extraction'
else
  bad 'installed scanner unchanged after failed extraction'
fi

if [ "$failed" -ne 0 ]; then
  exit 1
fi
