#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for rust_msrv_check.py. Each case gets an independent workspace fixture and
# verifies both the exact exit status and a diagnostic fragment.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
check=$HERE/rust_msrv_check.py
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# check_case <name> <want rc> <got rc> <needle> <output>: exits 1 with a FAIL line on a
# wrong exit status or a missing diagnostic fragment.
check_case() {
    local name=$1 want=$2 got=$3 needle=$4 out=$5
    if [ "$got" -ne "$want" ]; then
        printf 'FAIL %s: exit %s, want %s\n%s\n' "$name" "$got" "$want" "$out" >&2
        exit 1
    fi
    case "$out" in
        *"$needle"*) ;;
        *)
            printf 'FAIL %s: output did not contain "%s"\n%s\n' "$name" "$needle" "$out" >&2
            exit 1
            ;;
    esac
}

# fixture <name> <rust-version> <channel>: creates $work/<name> with a Cargo.toml and a
# rust-toolchain.toml for the given versions.
fixture() {
    local name=$1 rust_version=$2 channel=$3 dir="$work/$1"
    mkdir -p "$dir"
    if [ "$rust_version" = missing ]; then
        printf '%s\n' '[workspace]' 'members = []' '[workspace.package]' > "$dir/Cargo.toml"
    else
        printf '%s\n' '[workspace]' 'members = []' '[workspace.package]' \
            "rust-version = \"$rust_version\"" > "$dir/Cargo.toml"
    fi
    printf '%s\n' '[toolchain]' "channel = \"$channel\"" > "$dir/rust-toolchain.toml"
}

# expect_fixture <name> <want rc> <needle>: runs the check on $work/<name> and verifies
# the exit status and a diagnostic fragment via check_case.
expect_fixture() {
    local name=$1 want=$2 needle=$3 out status=0
    out=$(python3 -I "$check" --root "$work/$name" 2>&1) || status=$?
    check_case "$name" "$want" "$status" "$needle" "$out"
    echo "ok   $name"
}

# copy_real_workspace <destination>: copies the real workspace manifest files (root and
# member crates) to <destination> for mutation.
copy_real_workspace() {
    local destination=$1
    python3 -I -c '
import shutil
import sys
import tomllib
from pathlib import Path

source = Path(sys.argv[1])
destination = Path(sys.argv[2])
destination.mkdir(parents=True)
shutil.copy2(source / "Cargo.toml", destination / "Cargo.toml")
shutil.copy2(source / "rust-toolchain.toml", destination / "rust-toolchain.toml")
with (source / "Cargo.toml").open("rb") as manifest_file:
    members = tomllib.load(manifest_file)["workspace"]["members"]
for pattern in members:
    for candidate in source.glob(pattern):
        manifest = candidate if candidate.name == "Cargo.toml" else candidate / "Cargo.toml"
        if manifest.is_file():
            relative = manifest.relative_to(source)
            target = destination / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(manifest, target)
' "$ROOT" "$destination"
}

# rewrite_release <path> <key> <value>: rewrites <key> in <path> to <value>, failing if
# the manifest does not contain exactly one match.
rewrite_release() {
    local path=$1 key=$2 value=$3
    python3 -I -c '
import re
import sys
from pathlib import Path

path = Path(sys.argv[1])
key = re.escape(sys.argv[2])
replacement = f"{sys.argv[2]} = \"{sys.argv[3]}\""
text = path.read_text(encoding="utf-8")
updated, count = re.subn(rf"(?m)^{key}\s*=\s*\"[^\"]+\"\s*$", replacement, text)
if count != 1:
    raise SystemExit(f"expected one {sys.argv[2]} in {path}, found {count}")
path.write_text(updated, encoding="utf-8")
' "$path" "$key" "$value"
}

# expect_repository_ok <root>: runs the check on a full workspace and verifies the exact
# expected output line.
expect_repository_ok() {
    local root=$1 name msrv channel expected out status=0
    name=$(basename "$root")
    if [ "$root" = "$ROOT" ]; then
        name=real-repository
    fi
    msrv=$(python3 -I -c '
import sys
import tomllib

with open(sys.argv[1], "rb") as manifest_file:
    print(tomllib.load(manifest_file)["workspace"]["package"]["rust-version"])
' "$root/Cargo.toml")
    channel=$(python3 -I -c '
import sys
import tomllib

with open(sys.argv[1], "rb") as toolchain_file:
    print(tomllib.load(toolchain_file)["toolchain"]["channel"])
' "$root/rust-toolchain.toml")
    expected="ok: Cargo.toml rust-version $msrv matches rust-toolchain.toml channel $channel"
    out=$(python3 -I "$check" --root "$root") || status=$?
    check_case "$name" 0 "$status" "$expected" "$out"
    if [ "$out" != "$expected" ]; then
        printf 'FAIL %s: output was not the exact expected line\nwant: %s\ngot:  %s\n' \
            "$name" "$expected" "$out" >&2
        exit 1
    fi
    echo "ok   $name"
}

fixture short-msrv 1.100 1.100.0
expect_fixture short-msrv 0 'ok: Cargo.toml rust-version 1.100 matches rust-toolchain.toml channel 1.100.0'

fixture full-match 1.100.1 1.100.1
expect_fixture full-match 0 'ok: Cargo.toml rust-version 1.100.1 matches rust-toolchain.toml channel 1.100.1'

fixture patch-only 1.100 1.100.1
expect_fixture patch-only 3 'rust-version 1.100 does not match rust-toolchain.toml:2 channel 1.100.1'

fixture two-part-channel 1.100 1.100
expect_fixture two-part-channel 3 'rust-toolchain.toml:2 channel 1.100 is no fixed release and proves no minimum'

fixture stale-msrv 1.99 1.100.0
expect_fixture stale-msrv 3 'Cargo.toml:4 rust-version 1.99 does not match rust-toolchain.toml:2 channel 1.100.0'

fixture stable-channel 1.100 stable
expect_fixture stable-channel 3 'rust-toolchain.toml:2 channel stable is no fixed release and proves no minimum'

fixture nightly-channel 1.100 nightly-2026-11-01
expect_fixture nightly-channel 3 'rust-toolchain.toml:2 channel nightly-2026-11-01 is no fixed release and proves no minimum'

fixture missing-msrv missing 1.100.0
expect_fixture missing-msrv 3 'Cargo.toml is missing [workspace.package].rust-version'

fixture literal-member 1.100 1.100.0
mkdir -p "$work/literal-member/worker/crates/snatch-core"
printf '%s\n' '[workspace]' 'members = ["worker/crates/*"]' '[workspace.package]' \
    'rust-version = "1.100"' > "$work/literal-member/Cargo.toml"
printf '%s\n' '[package]' 'name = "snatch-core"' 'version = "0.1.0"' \
    'rust-version = "1.99"' > "$work/literal-member/worker/crates/snatch-core/Cargo.toml"
expect_fixture literal-member 3 'worker/crates/snatch-core/Cargo.toml:4 has literal rust-version 1.99; use rust-version.workspace = true'

fixture missing-member 1.100 1.100.0
mkdir -p "$work/missing-member/crates/missing"
printf '%s\n' '[workspace]' 'members = ["crates/*"]' '[workspace.package]' \
    'rust-version = "1.100"' > "$work/missing-member/Cargo.toml"
printf '%s\n' '[package]' 'name = "missing"' 'version = "0.1.0"' \
    > "$work/missing-member/crates/missing/Cargo.toml"
expect_fixture missing-member 3 \
    'crates/missing/Cargo.toml:1 has no rust-version; use rust-version.workspace = true'

fixture false-member 1.100 1.100.0
mkdir -p "$work/false-member/crates/false"
printf '%s\n' '[workspace]' 'members = ["crates/*"]' '[workspace.package]' \
    'rust-version = "1.100"' > "$work/false-member/Cargo.toml"
printf '%s\n' '[package]' 'name = "false"' 'version = "0.1.0"' \
    'rust-version.workspace = false' > "$work/false-member/crates/false/Cargo.toml"
expect_fixture false-member 3 \
    "crates/false/Cargo.toml:4 has literal rust-version {'workspace': False}; use rust-version.workspace = true"

fixture excluded-glob-member 1.100 1.100.0
mkdir -p "$work/excluded-glob-member/crates/ignored"
printf '%s\n' '[workspace]' 'members = ["crates/*"]' 'exclude = ["crates/ignored"]' \
    '[workspace.package]' 'rust-version = "1.100"' > "$work/excluded-glob-member/Cargo.toml"
printf '%s\n' '[package]' 'name = "ignored"' 'version = "0.1.0"' \
    'rust-version = "1.99"' > "$work/excluded-glob-member/crates/ignored/Cargo.toml"
expect_fixture excluded-glob-member 0 'ok: Cargo.toml rust-version 1.100 matches rust-toolchain.toml channel 1.100.0'

fixture exclude-boundary 1.100 1.100.0
mkdir -p "$work/exclude-boundary/crates/legacy"
printf '%s\n' '[workspace]' 'members = ["crates/*"]' 'exclude = ["crates/leg"]' \
    '[workspace.package]' 'rust-version = "1.100"' > "$work/exclude-boundary/Cargo.toml"
printf '%s\n' '[package]' 'name = "legacy"' 'version = "0.1.0"' \
    'rust-version = "1.99"' > "$work/exclude-boundary/crates/legacy/Cargo.toml"
expect_fixture exclude-boundary 3 \
    'crates/legacy/Cargo.toml:4 has literal rust-version 1.99; use rust-version.workspace = true'

fixture explicit-excluded-member 1.100 1.100.0
mkdir -p "$work/explicit-excluded-member/crates/leg"
printf '%s\n' '[workspace]' 'members = ["crates/*", "crates/leg"]' \
    'exclude = ["crates/leg"]' '[workspace.package]' 'rust-version = "1.100"' \
    > "$work/explicit-excluded-member/Cargo.toml"
printf '%s\n' '[package]' 'name = "leg"' 'version = "0.1.0"' \
    'rust-version = "1.99"' > "$work/explicit-excluded-member/crates/leg/Cargo.toml"
expect_fixture explicit-excluded-member 3 \
    'crates/leg/Cargo.toml:4 has literal rust-version 1.99; use rust-version.workspace = true'

fixture exclude-not-list 1.100 1.100.0
printf '%s\n' '[workspace]' 'members = []' 'exclude = "crates/ignored"' \
    '[workspace.package]' 'rust-version = "1.100"' > "$work/exclude-not-list/Cargo.toml"
expect_fixture exclude-not-list 1 'error: Cargo.toml [workspace].exclude must be a list of relative paths'

# Cargo makes every path dependency inside the workspace a member, listed or not.
fixture path-dep-member 1.100 1.100.0
mkdir -p "$work/path-dep-member/crates/a" "$work/path-dep-member/extra/b"
printf '%s\n' '[workspace]' 'members = ["crates/a"]' '[workspace.package]' \
    'rust-version = "1.100"' > "$work/path-dep-member/Cargo.toml"
printf '%s\n' '[package]' 'name = "a"' 'version = "0.1.0"' 'rust-version.workspace = true' \
    '[dependencies]' 'b = { path = "../../extra/b" }' > "$work/path-dep-member/crates/a/Cargo.toml"
printf '%s\n' '[package]' 'name = "b"' 'version = "0.1.0"' \
    'rust-version = "1.99"' > "$work/path-dep-member/extra/b/Cargo.toml"
expect_fixture path-dep-member 3 \
    'extra/b/Cargo.toml:4 has literal rust-version 1.99; use rust-version.workspace = true'

# workspace = true under a target dev-dependency table, then a build path dependency of it.
fixture path-dep-chain 1.100 1.100.0
mkdir -p "$work/path-dep-chain/crates/a" "$work/path-dep-chain/extra/b" "$work/path-dep-chain/extra/c"
printf '%s\n' '[workspace]' 'members = ["crates/a"]' '[workspace.package]' \
    'rust-version = "1.100"' '[workspace.dependencies]' 'b = { path = "extra/b" }' \
    > "$work/path-dep-chain/Cargo.toml"
printf '%s\n' '[package]' 'name = "a"' 'version = "0.1.0"' 'rust-version.workspace = true' \
    "[target.'cfg(unix)'.dev-dependencies]" 'b = { workspace = true }' \
    > "$work/path-dep-chain/crates/a/Cargo.toml"
printf '%s\n' '[package]' 'name = "b"' 'version = "0.1.0"' 'rust-version.workspace = true' \
    '[build-dependencies]' 'c = { path = "../c" }' > "$work/path-dep-chain/extra/b/Cargo.toml"
printf '%s\n' '[package]' 'name = "c"' 'version = "0.1.0"' \
    'rust-version = "1.99"' > "$work/path-dep-chain/extra/c/Cargo.toml"
expect_fixture path-dep-chain 3 \
    'extra/c/Cargo.toml:4 has literal rust-version 1.99; use rust-version.workspace = true'

# A path dependency outside the workspace, or below an exclude entry, is no member.
fixture path-dep-skipped 1.100 1.100.0
mkdir -p "$work/path-dep-skipped/crates/a" "$work/path-dep-skipped/extra/x" "$work/path-dep-outside"
printf '%s\n' '[workspace]' 'members = ["crates/a"]' 'exclude = ["extra/x"]' \
    '[workspace.package]' 'rust-version = "1.100"' > "$work/path-dep-skipped/Cargo.toml"
printf '%s\n' '[package]' 'name = "a"' 'version = "0.1.0"' 'rust-version.workspace = true' \
    '[dependencies]' 'x = { path = "../../extra/x" }' \
    'out = { path = "../../../path-dep-outside" }' > "$work/path-dep-skipped/crates/a/Cargo.toml"
for crate in path-dep-skipped/extra/x path-dep-outside; do
    printf '%s\n' '[package]' "name = \"${crate##*/}\"" 'version = "0.1.0"' \
        'rust-version = "1.99"' > "$work/$crate/Cargo.toml"
done
expect_fixture path-dep-skipped 0 'ok: Cargo.toml rust-version 1.100 matches rust-toolchain.toml channel 1.100.0'

fixture path-dep-missing 1.100 1.100.0
mkdir -p "$work/path-dep-missing/crates/a"
printf '%s\n' '[workspace]' 'members = ["crates/a"]' '[workspace.package]' \
    'rust-version = "1.100"' > "$work/path-dep-missing/Cargo.toml"
printf '%s\n' '[package]' 'name = "a"' 'version = "0.1.0"' 'rust-version.workspace = true' \
    '[dependencies]' 'none = { path = "../none" }' > "$work/path-dep-missing/crates/a/Cargo.toml"
expect_fixture path-dep-missing 1 'error: cannot read crates/none/Cargo.toml'

fixture path-dep-absolute 1.100 1.100.0
mkdir -p "$work/path-dep-absolute/crates/a" "$work/path-dep-absolute/extra/b"
printf '%s\n' '[workspace]' 'members = ["crates/a"]' '[workspace.package]' \
    'rust-version = "1.100"' > "$work/path-dep-absolute/Cargo.toml"
printf '%s\n' '[package]' 'name = "a"' 'version = "0.1.0"' 'rust-version.workspace = true' \
    '[dependencies]' "b = { path = '$work/path-dep-absolute/extra/b' }" \
    > "$work/path-dep-absolute/crates/a/Cargo.toml"
printf '%s\n' '[package]' 'name = "b"' 'version = "0.1.0"' \
    'rust-version = "1.99"' > "$work/path-dep-absolute/extra/b/Cargo.toml"
expect_fixture path-dep-absolute 1 'error: crates/a/Cargo.toml: absolute path dependency b'

# The walk shares the 64-manifest bound: crates/a plus 63 path dependencies pass, plus 64 fail.
for count in 63 64; do
    name=path-dep-bound-$count
    fixture "$name" 1.100 1.100.0
    mkdir -p "$work/$name/crates/a"
    printf '%s\n' '[workspace]' 'members = ["crates/a"]' '[workspace.package]' \
        'rust-version = "1.100"' > "$work/$name/Cargo.toml"
    deps=()
    for index in $(seq 1 "$count"); do
        mkdir -p "$work/$name/extra/d$index"
        printf '%s\n' '[package]' "name = \"d$index\"" 'version = "0.1.0"' \
            'rust-version.workspace = true' > "$work/$name/extra/d$index/Cargo.toml"
        deps+=("d$index = { path = \"../../extra/d$index\" }")
    done
    printf '%s\n' '[package]' 'name = "a"' 'version = "0.1.0"' 'rust-version.workspace = true' \
        '[dependencies]' "${deps[@]}" > "$work/$name/crates/a/Cargo.toml"
done
expect_fixture path-dep-bound-63 0 'ok: Cargo.toml rust-version 1.100 matches rust-toolchain.toml channel 1.100.0'
expect_fixture path-dep-bound-64 1 'error: workspace manifests exceed 64'

fixture invalid-cargo 1.100 1.100.0
printf '%s\n' '[workspace' > "$work/invalid-cargo/Cargo.toml"
expect_fixture invalid-cargo 1 'error: cannot parse Cargo.toml'

fixture newer-patch 1.100.1 1.100.0
expect_fixture newer-patch 3 'rust-version 1.100.1 does not match rust-toolchain.toml:2 channel 1.100.0'

fixture string-prefix 1.10 1.100.0
expect_fixture string-prefix 3 'rust-version 1.10 does not match rust-toolchain.toml:2 channel 1.100.0'

fixture major-mismatch 2.0 1.100.0
expect_fixture major-mismatch 3 'rust-version 2.0 does not match rust-toolchain.toml:2 channel 1.100.0'

fixture host-suffix 1.100 1.100.0-x86_64-unknown-linux-gnu
expect_fixture host-suffix 3 'channel 1.100.0-x86_64-unknown-linux-gnu is no fixed release and proves no minimum'

expect_repository_ok "$ROOT"

next_values=$(python3 -I -c '
import sys
import tomllib

with open(sys.argv[1], "rb") as cargo_file:
    msrv = tomllib.load(cargo_file)["workspace"]["package"]["rust-version"]
with open(sys.argv[2], "rb") as toolchain_file:
    channel = tomllib.load(toolchain_file)["toolchain"]["channel"]
msrv_parts = msrv.split(".")
channel_parts = channel.split(".")
if msrv_parts[:2] != channel_parts[:2]:
    raise SystemExit("real Rust versions do not share major and minor components")
next_minor = f"{msrv_parts[0]}.{int(msrv_parts[1]) + 1}.0"
next_patch = f"{channel_parts[0]}.{channel_parts[1]}.{int(channel_parts[2]) + 1}"
print(next_minor, next_minor, next_patch)
' "$ROOT/Cargo.toml" "$ROOT/rust-toolchain.toml")
read -r next_msrv next_channel next_patch <<< "$next_values"

moved_together=$work/moved-together
copy_real_workspace "$moved_together"
rewrite_release "$moved_together/Cargo.toml" rust-version "$next_msrv"
rewrite_release "$moved_together/rust-toolchain.toml" channel "$next_channel"
expect_repository_ok "$moved_together"

moved_alone=$work/moved-alone
copy_real_workspace "$moved_alone"
rewrite_release "$moved_alone/rust-toolchain.toml" channel "$next_channel"
status=0
out=$(python3 -I "$check" --root "$moved_alone" 2>&1) || status=$?
check_case moved-alone 3 "$status" 'does not match rust-toolchain.toml' "$out"
echo 'ok   moved-alone'

moved_patch_together=$work/moved-patch-together
copy_real_workspace "$moved_patch_together"
rewrite_release "$moved_patch_together/Cargo.toml" rust-version "$next_patch"
rewrite_release "$moved_patch_together/rust-toolchain.toml" channel "$next_patch"
expect_repository_ok "$moved_patch_together"

moved_patch_alone=$work/moved-patch-alone
copy_real_workspace "$moved_patch_alone"
rewrite_release "$moved_patch_alone/rust-toolchain.toml" channel "$next_patch"
status=0
out=$(python3 -I "$check" --root "$moved_patch_alone" 2>&1) || status=$?
check_case moved-patch-alone 3 "$status" 'does not match rust-toolchain.toml' "$out"
echo 'ok   moved-patch-alone'

mkdir -p "$work/renovate-capture"
printf '%s\n' '[workspace.package]' 'rust-version = "1.100.1"' \
    > "$work/renovate-capture/Cargo.toml"
status=0
out=$(python3 -I - "$ROOT/renovate.json" "$ROOT/Cargo.toml" \
    "$work/renovate-capture/Cargo.toml" <<'PY'
import json
import re
import sys
import tomllib
from pathlib import Path

renovate_path = Path(sys.argv[1])
real_cargo_path = Path(sys.argv[2])
planted_cargo_path = Path(sys.argv[3])
config = json.loads(renovate_path.read_text(encoding="utf-8"))
manager = next(
    item for item in config["customManagers"] if item.get("depNameTemplate") == "rust"
)
pattern = manager["matchStrings"][0].replace("(?<", "(?P<")
real_text = real_cargo_path.read_text(encoding="utf-8")
planted_text = planted_cargo_path.read_text(encoding="utf-8")
real_match = re.search(pattern, real_text)
planted_match = re.search(pattern, planted_text)
if real_match is None or planted_match is None:
    raise SystemExit("Renovate pattern did not capture a rust-version")
with real_cargo_path.open("rb") as cargo_file:
    expected = tomllib.load(cargo_file)["workspace"]["package"]["rust-version"]
if real_match.group("currentValue") != expected:
    raise SystemExit(
        f"real currentValue {real_match.group('currentValue')} != tomllib value {expected}"
    )
if planted_match.group("currentValue") != "1.100.1":
    raise SystemExit(
        f"planted currentValue {planted_match.group('currentValue')} != 1.100.1"
    )
old_pattern = pattern.replace(r"(?:\.[0-9]+)?", "")
if re.search(old_pattern, planted_text) is not None:
    raise SystemExit("old two-part Renovate pattern captured a three-part version")
print("ok renovate capture")
PY
) || status=$?
check_case renovate-capture 0 "$status" 'ok renovate capture' "$out"
echo 'ok   renovate-capture'
