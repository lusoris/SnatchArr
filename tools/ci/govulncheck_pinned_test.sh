#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Replay scan headers and exit statuses in a throwaway module; a removed guard must
# accept the planted distro header that the real guard refuses.
#
#   tools/ci/govulncheck_pinned_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise. Needs go and sed.
# Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' 0
# Two platform declarations, bounded by the two-element literal list.
platforms="Platforms: POSIX sh, runs in CI on the ubuntu workflows job and on Linux maintainer hosts; macOS and Windows skipped: the repository's CI and landing run on Linux only."
for script in "$HERE/govulncheck_pinned.sh" "$HERE/govulncheck_pinned_test.sh"; do
  grep -Fxq "# $platforms" "$script" || { echo "FAIL platform reason missing: $script" >&2; exit 1; }
done
mkdir -p "$WORK/mod"
printf 'module example.invalid/m\n\ngo 1.27.0\n' >"$WORK/mod/go.mod"
cat >"$WORK/scanner" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$GOTOOLCHAIN" "$@" >>"$RECORD"
cat "$HEADER"
printf 'scan body\n'
printf 'scanner stderr\n' >&2
exit "$SCAN_RC"
EOF
chmod +x "$WORK/scanner"
RECORD="$WORK/record"
HEADER="$WORK/header"
SCAN_RC=0
export RECORD HEADER SCAN_RC
GUARD="$HERE/govulncheck_pinned.sh"

# run [PATTERN...]: scan the throwaway module; sets rc, stdout and combined out.
run() {
  : >"$RECORD"
  rc=0
  stdout="$(cd "$WORK/mod" && sh "$GUARD" "$WORK/scanner" "$@" 2>"$WORK/stderr")" || rc=$?
  out="$(printf '%s\n' "$stdout"; cat "$WORK/stderr")"
}
printf 'Go: go1.27.0\n' >"$HEADER"
run
check_case 'exact release' 0 "$rc" 'scan body' "$out"
expected="$(printf 'Go: go1.27.0\nscan body\nscanner stderr\n')"
if [ "$stdout" != "$expected" ]; then
  printf 'FAIL output changed:\n%s\n' "$stdout" >&2
  exit 1
fi
# One recorded call: the guard reads the header of the scan itself, never a second probe.
record="$(cat "$RECORD")"
expected="$(printf 'go1.27.0\n-show\nversion\n./...\n')"
if [ "$record" != "$expected" ]; then
  printf 'FAIL toolchain, arguments or scanner invocation count:\n%s\n' "$record" >&2
  exit 1
fi
run ./x
check_case 'explicit pattern' 0 "$rc" "$(printf 'go1.27.0\n-show\nversion\n./x\n')" "$(cat "$RECORD")"
SCAN_RC=3
run
check_case 'findings status retained' 3 "$rc" 'scan body' "$out"
SCAN_RC=1
run
check_case 'scanner error retained' 1 "$rc" 'scanner stderr' "$out"
SCAN_RC=0
# Six mismatches, bounded by the six-element literal list.
for seen in go1.27.0-X:nodwarf5 go1.27.1 go1.27.00 go1.27.0rc1 go1.27 ''; do
  if [ -z "$seen" ]; then
    : >"$HEADER"
  else
    printf 'Go: %s\n' "$seen" >"$HEADER"
  fi
  run
  check_case "reject '$seen'" 1 "$rc" "govulncheck_pinned: govulncheck read Go '$seen', want 'go1.27.0': a distro or GOEXPERIMENT build hides standard library vulnerabilities" "$out"
done
printf 'Go: go1.27.1\nGo: go1.27.0\n' >"$HEADER"
run
check_case 'two Go headers' 1 "$rc" "want 'go1.27.0'" "$out"
printf 'Go: go1.27.0\nGo: \n' >"$HEADER"
run
check_case 'second empty Go header' 1 "$rc" "want 'go1.27.0'" "$out"
rc=0
out="$(cd "$WORK/mod" && sh "$GUARD" 2>&1)" || rc=$?
check_case 'no scanner' 2 "$rc" 'govulncheck_pinned: usage:' "$out"

# Remove the comparison block in a copy: the same distro plant must then pass.
sed '/^if \[ .*go_headers.* != /,/^fi$/d' "$HERE/govulncheck_pinned.sh" >"$WORK/unguarded.sh"
GUARD="$WORK/unguarded.sh"
printf 'Go: go1.27.0-X:nodwarf5\n' >"$HEADER"
run
check_case 'removed guard accepts planted distro header' 0 "$rc" 'Go: go1.27.0-X:nodwarf5' "$out"
echo 'govulncheck_pinned_test: all cases passed (removed guard accepted distro plant)'
