#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for tools/ci/generated_files.py:
# - positive: changed listed file writes manifest.json (horistes.generated-files.v1) + files/<path>
# - negative: unchanged listed file writes nothing; '..' path, absolute path, backslash path,
#   duplicate path, missing working-tree file, and untracked file missing from HEAD are refused
# - boundary: empty allowlist (comments and blank lines only) writes nothing and exits 0
#
#   bash tools/ci/generated_files_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
TOOL="$HERE/generated_files.py"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

accepted=0
refused=0

# fresh_repo DIR: recreate DIR as a repository holding a generated-files list and its files.
fresh_repo() {
  local dir="$1"
  rm -rf "$dir"
  new_repo "$dir"
  mkdir -p "$dir/.github" "$dir/web/static"
  cat > "$dir/.github/generated-files" <<'EOF'
# comment line
web/static/licences.json
EOF
  printf '{"packages":[]}\n' > "$dir/web/static/licences.json"
  git -C "$dir" add .
  git -C "$dir" commit --quiet -m "initial"
}

expect_refused() {
  local name="$1" needle="$2" setup="$3"
  local repo="$WORK/repo" out_dir="$WORK/out"
  rm -rf "$out_dir"
  fresh_repo "$repo"
  (cd "$repo" && bash -ec "$setup")
  local got=0 out=""
  out="$(python3 -B "$TOOL" --root "$repo" --out "$out_dir" 2>&1)" || got=$?
  if [ "$got" -eq 0 ]; then
    printf 'FAIL %s: accepted, expected refusal\n%s\n' "$name" "$out" >&2
    exit 1
  fi
  case "$out" in
    *"$needle"*) ;;
    *)
      printf 'FAIL %s: refused without "%s"\n%s\n' "$name" "$needle" "$out" >&2
      exit 1
      ;;
  esac
  if [ -e "$out_dir" ]; then
    printf 'FAIL %s: refused case created output directory %s\n' "$name" "$out_dir" >&2
    exit 1
  fi
  refused=$((refused + 1))
  echo "ok   $name"
}

# 1. Positive: changed listed file writes manifest.json + files/<path> with exact bytes and sha256.
repo="$WORK/repo"
out_dir="$WORK/out-pos"
fresh_repo "$repo"
head_sha="$(git -C "$repo" rev-parse HEAD)"
new_payload='{"packages":[{"name":"demo","version":"1.0.0"}]}'
printf '%s\n' "$new_payload" > "$repo/web/static/licences.json"
want_bytes="$(wc -c < "$repo/web/static/licences.json" | tr -d ' ')"
want_sha="$(sha256sum "$repo/web/static/licences.json" | awk '{print $1}')"

out="$(python3 -B "$TOOL" --root "$repo" --out "$out_dir" 2>&1)"
if [ ! -f "$out_dir/manifest.json" ] || [ ! -f "$out_dir/files/web/static/licences.json" ]; then
  printf 'FAIL positive changed file: missing manifest.json or files member\n%s\n' "$out" >&2
  exit 1
fi
if ! cmp -s "$repo/web/static/licences.json" "$out_dir/files/web/static/licences.json"; then
  echo "FAIL positive changed file: copied member bytes differ from working tree" >&2
  exit 1
fi
jq -e \
  --arg head "$head_sha" \
  --arg sha "$want_sha" \
  --argjson bytes "$want_bytes" \
  '.schema == "horistes.generated-files.v1"
   and .head == $head
   and (.files | length == 1)
   and .files[0].path == "web/static/licences.json"
   and .files[0].bytes == $bytes
   and .files[0].sha256 == $sha' "$out_dir/manifest.json" > /dev/null
member_count="$(find "$out_dir" -type f | wc -l | tr -d ' ')"
if [ "$member_count" -ne 2 ]; then
  echo "FAIL positive changed file: expected 2 files in output dir, got $member_count" >&2
  exit 1
fi
accepted=$((accepted + 1))
echo "ok   positive: changed file writes manifest and member"

# 2. Negative (no-op): unchanged file writes nothing.
rm -rf "$WORK/out-unchanged"
fresh_repo "$repo"
out="$(python3 -B "$TOOL" --root "$repo" --out "$WORK/out-unchanged" 2>&1)"
if [ -e "$WORK/out-unchanged" ]; then
  echo "FAIL unchanged file: output directory was created" >&2
  exit 1
fi
case "$out" in
  *"nothing to regenerate"*) ;;
  *)
    printf 'FAIL unchanged file: unexpected output: %s\n' "$out" >&2
    exit 1
    ;;
esac
accepted=$((accepted + 1))
echo "ok   negative: unchanged file writes nothing"

# 3. Negative (refusals): parent traversal, absolute, backslash, duplicate, missing file, untracked file.
expect_refused "parent traversal '..' path refused" "unsafe listed path" \
  'printf "../outside.json\n" > .github/generated-files'
expect_refused "nested '..' path refused" "unsafe listed path" \
  'printf "web/../../outside.json\n" > .github/generated-files'
expect_refused "absolute path refused" "unsafe listed path" \
  'printf "/etc/passwd\n" > .github/generated-files'
expect_refused "backslash path refused" "unsafe listed path" \
  'printf "web\\static\\licences.json\n" > .github/generated-files'
expect_refused "duplicate listed path refused" "duplicate listed path" \
  'printf "web/static/licences.json\nweb/static/licences.json\n" > .github/generated-files'
expect_refused "missing working-tree path refused" "listed path is missing: web/static/missing.json" \
  'printf "web/static/missing.json\n" > .github/generated-files'
expect_refused "deleted listed path refused" "listed path is missing: web/static/licences.json" \
  'rm -f web/static/licences.json'
expect_refused "untracked path missing from HEAD refused" "git show HEAD:web/static/new.json exited" \
  'printf "{}\n" > web/static/new.json && printf "web/static/new.json\n" > .github/generated-files'
expect_refused "missing allowlist file refused" "cannot read allowlist" \
  'rm -f .github/generated-files'

# 4. Boundary: empty allowlist (comments and blank lines only) writes nothing and exits 0.
rm -rf "$WORK/out-empty"
fresh_repo "$repo"
printf '# only comments\n\n  # another comment\n' > "$repo/.github/generated-files"
out="$(python3 -B "$TOOL" --root "$repo" --out "$WORK/out-empty" 2>&1)"
if [ -e "$WORK/out-empty" ]; then
  echo "FAIL boundary empty list: output directory was created" >&2
  exit 1
fi
case "$out" in
  *"nothing to regenerate"*) ;;
  *)
    printf 'FAIL boundary empty list: unexpected output: %s\n' "$out" >&2
    exit 1
    ;;
esac
accepted=$((accepted + 1))
echo "ok   boundary: empty allowlist writes nothing"

# 5. Repository check: .github/generated-files in this repository is valid and clean at HEAD.
rm -rf "$WORK/out-repo"
python3 -B "$TOOL" --root "$REPO_ROOT" --out "$WORK/out-repo" > /dev/null
accepted=$((accepted + 1))
echo "ok   repository .github/generated-files is valid"

echo "generated_files: $accepted accepted, $refused refused"
