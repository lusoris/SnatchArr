#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Offline planted cases for renovate_presets.py and renovate_presets_dump.mjs.
# Builds stub dump, horistes and repository fixtures in a scratch directory (no Docker, no
# network, no .cache reads) and checks both exit code and diagnostic substring on each case.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check="$HERE/renovate_presets.py"
dump_mjs="$HERE/renovate_presets_dump.mjs"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"

DIGEST="$(printf '%064d' 0 | tr 0 a)"
BASE_REPO="$work/base-repo"

# write_base_repo DIR [EXTRA_JSON]: create a stub git repository with standard enabledManagers.
write_base_repo() {
  local dir="$1" extra="${2:-}"
  new_repo "$dir"
  python3 -I -c '
import json, pathlib, sys
cfg = {
    "extends": ["github>plurano/horistes", ":semanticCommits"],
    "enabledManagers": ["npm", "dockerfile", "github-actions", "custom.regex"],
    "customManagers": [
        {
            "customType": "regex",
            "managerFilePatterns": ["tools/ci/tool-versions.env"],
            "matchStrings": ["IMG=(?<depName>renovate/renovate):(?<currentValue>[^@\\s]+)@(?<currentDigest>sha256:[a-f0-9]{64})\\n"],
            "datasourceTemplate": "docker",
        }
    ],
}
if sys.argv[2]:
    cfg.update(json.loads(sys.argv[2]))
pathlib.Path(sys.argv[1], "renovate.json").write_text(json.dumps(cfg), encoding="utf-8")
' "$dir" "$extra"
  (cd "$dir" && git add renovate.json)
}

# write_stub_fixtures DUMP_PATH HORISTES_PATH [GROUPS_PATCH_JSON] [HORISTES_JSON]
# [EMITTERS_JSON] [RENAMES_JSON]:
# emit a valid stub dump JSON and horistes JSON with optional overrides.
write_stub_fixtures() {
  local dump_path="$1" horistes_path="$2"
  local groups_patch="${3:-"{}"}" horistes_json="${4:-}" emitters_json="${5:-}"
  local renames_json="${6:-}"
  python3 -I -c '
import json, pathlib, sys
dump_p, hor_p, g_patch, h_raw, e_raw, r_raw = sys.argv[1:7]
groups = {
    "default": {
        "semanticCommits": {"semanticCommits": "enabled"},
    },
    "config": {
        "recommended": {"description": "base"},
    },
    "monorepo": {
        "clean": {"description": "mono", "matchPackageNames": ["foo"]},
    },
}
for grp, entries in json.loads(g_patch).items():
    groups.setdefault(grp, {}).update(entries)
emitters = json.loads(e_raw) if e_raw else {
    "github-runner": [
        "modules/manager/github-actions/dep-types.js",
        "modules/manager/github-actions/extract.js",
    ],
    "php2shell-runtime": [
        "config/presets/internal/security.preset.js",
    ],
}
renames = json.loads(r_raw) if r_raw else {
    "matchManagers": {
        "regex": "custom.regex",
        "renovate-config-presets": "renovate-config",
    },
    "enabledManagers": {
        "yarn": "npm",
        "regex": "custom.regex",
        "renovate-config-presets": "renovate-config",
    },
}
dump = {
    "renovateVersion": "44.234.0",
    "node": "v24.22.2",
    "groups": groups,
    "dockerManagers": ["dockerfile", "github-actions", "maven", "nuget"],
    "depTypeEmitters": emitters,
    "managerRenames": renames,
}
horistes = json.loads(h_raw) if h_raw else {"extends": ["config:recommended"]}
pathlib.Path(dump_p).write_text(json.dumps(dump), encoding="utf-8")
pathlib.Path(hor_p).write_text(json.dumps(horistes), encoding="utf-8")
' "$dump_path" "$horistes_path" "$groups_patch" "$horistes_json" "$emitters_json" "$renames_json"
}

# expect_preset_case NAME WANT_RC NEEDLE HORISTES_JSON [GROUPS_PATCH_JSON] [REPO_DIR]
# [EMITTERS_JSON] [RENAMES_JSON]:
# run renovate_presets.py against stub dump + horistes under a 10s bound and assert verdict.
expect_preset_case() {
  local name="$1" want="$2" needle="$3" hor_json="$4"
  local g_patch="${5:-"{}"}" repo_dir="${6:-$BASE_REPO}" e_json="${7:-}" r_json="${8:-}"
  local dir="$work/case-$name" rc=0 out
  mkdir -p "$dir"
  write_stub_fixtures \
    "$dir/dump.json" "$dir/horistes.json" "$g_patch" "$hor_json" "$e_json" "$r_json"
  out="$(run_bounded 10 python3 -I "$check" \
    --dump "$dir/dump.json" \
    --horistes "$dir/horistes.json" \
    --repo-root "$repo_dir" 2>&1)" || rc=$?
  check_case "$name" "$want" "$rc" "$needle" "$out"
  echo "ok   $name"
}

# write_fake_dist ROOT: synthetic Renovate dist tree with a Docker-datasource manager,
# an import-only Docker manager (github-actions), dep-type emitters and both manager migrations.
write_fake_dist() {
  local root="$1"
  mkdir -p "$root/dist/config/presets/internal" \
    "$root/dist/config/migrations/custom" \
    "$root/dist/modules/manager/dockerfile" \
    "$root/dist/modules/manager/github-actions" \
    "$root/dist/modules/manager/npm"
  printf '%s\n' '{"version":"44.234.0","type":"module"}' > "$root/package.json"
  cat > "$root/dist/config/presets/internal/index.js" <<'EOF'
export const groups = { default: { semanticCommits: { semanticCommits: "enabled" } } };
EOF
  cat > "$root/dist/config/presets/internal/security.preset.js" <<'EOF'
export const sec = "php2shell-runtime";
EOF
  cat > "$root/dist/modules/manager/api.js" <<'EOF'
export default new Map([
  ["dockerfile", { supportedDatasources: ["docker"] }],
  ["github-actions", { supportedDatasources: ["github-tags"] }],
  ["npm", { supportedDatasources: ["npm"] }],
]);
EOF
  cat > "$root/dist/modules/manager/github-actions/dep-types.js" <<'EOF'
export const RUNNER = "github-runner";
EOF
  cat > "$root/dist/modules/manager/github-actions/extract.js" <<'EOF'
import { x } from '../../datasource/docker/common.js';
export const kind = "github-runner";
EOF
  cat > "$root/dist/modules/manager/npm/extract.js" <<'EOF'
export const n = 1;
EOF
  cat > "$root/dist/config/migrations/custom/match-managers-migration.js" <<'EOF'
export function migrate(value) {
  switch (value) {
    case "regex": return "custom.regex";
    case "renovate-config-presets": return "renovate-config";
  }
}
EOF
  cat > "$root/dist/config/migrations/custom/enabled-managers-migration.js" <<'EOF'
export function migrate(value) {
  switch (value) {
    case "yarn": return "npm";
    case "regex": return "custom.regex";
    case "renovate-config-presets": return "renovate-config";
  }
}
EOF
}

# check_dump_positive ROOT [PRELOAD]: intact fake dist emits byte-identical payload, even on short reads.
check_dump_positive() {
  local root="$1" out
  if [ -n "${2:-}" ]; then
    out="$(RENOVATE_ROOT="$root" node --import "$2" "$dump_mjs")"
  else
    out="$(RENOVATE_ROOT="$root" node "$dump_mjs")"
  fi
  python3 -I -c '
import json, sys
d = json.loads(sys.argv[1])
if d["renovateVersion"] != "44.234.0" or d["dockerManagers"] != ["dockerfile", "github-actions"]:
    raise SystemExit(f"unexpected dump: {d}")
if d["depTypeEmitters"]["github-runner"] != [
    "modules/manager/github-actions/dep-types.js",
    "modules/manager/github-actions/extract.js",
]:
    raise SystemExit(f"unexpected emitters: {d}")
if d["managerRenames"]["matchManagers"]["regex"] != "custom.regex":
    raise SystemExit(f"missing matchManagers rename: {d}")
if d["managerRenames"]["enabledManagers"]["yarn"] != "npm":
    raise SystemExit(f"missing enabledManagers rename: {d}")
expected = {
    "renovateVersion": "44.234.0", "node": d["node"],
    "groups": {"default": {"semanticCommits": {"semanticCommits": "enabled"}}},
    "dockerManagers": ["dockerfile", "github-actions"],
    "depTypeEmitters": {
        "github-runner": ["modules/manager/github-actions/dep-types.js", "modules/manager/github-actions/extract.js"],
        "php2shell-runtime": ["config/presets/internal/security.preset.js"],
    },
    "managerRenames": {
        "matchManagers": {"regex": "custom.regex", "renovate-config-presets": "renovate-config"},
        "enabledManagers": {"yarn": "npm", "regex": "custom.regex", "renovate-config-presets": "renovate-config"},
    },
}
if sys.argv[1] != json.dumps(expected, separators=(",", ":")):
    raise SystemExit("dump payload bytes or key order changed")
' "$out"
  echo "ok   dump-mjs-positive"
}

# expect_dump_failure NAME ROOT NEEDLE: dump.mjs over ROOT must exit 1 and print NEEDLE.
expect_dump_failure() {
  local name="$1" root="$2" needle="$3" out rc=0
  out="$(RENOVATE_ROOT="$root" node "$dump_mjs" 2>&1)" || rc=$?
  check_case "$name" 1 "$rc" "$needle" "$out"
  echo "ok   $name"
}

# test_dump_proto ROOT: preserve prototype-like migration names until Python validation.
test_dump_proto() {
  local root="$work/proto-renovate" out
  cp -R "$1" "$root"
  printf '%s\n' 'case "__proto__": return "dockerfile";' \
    >> "$root/dist/config/migrations/custom/match-managers-migration.js"
  out="$(RENOVATE_ROOT="$root" node "$dump_mjs")"
  python3 -I -c '
import json, sys
if json.loads(sys.argv[1])["managerRenames"]["matchManagers"].get("__proto__") != "dockerfile":
    raise SystemExit("prototype-like manager rename was silently dropped")
' "$out"
  echo "ok   dump-mjs-proto-preserved"
}

# test_dump_reads ROOT: refuse symlinks, non-files, oversized files and growth after fstat.
test_dump_reads() {
  local root="$1" out rc=0
  cp -R "$root" "$work/symlink-renovate"
  mv "$work/symlink-renovate/package.json" "$work/symlink-renovate/real.json"
  ln -s real.json "$work/symlink-renovate/package.json"
  expect_dump_failure "dump-mjs-symlink" "$work/symlink-renovate" "renovate_presets_dump: error:"
  cp -R "$root" "$work/nonfile-renovate"
  rm "$work/nonfile-renovate/package.json"
  mkdir "$work/nonfile-renovate/package.json"
  expect_dump_failure "dump-mjs-nonfile" "$work/nonfile-renovate" "expected regular file"
  cp -R "$root" "$work/oversize-renovate"
  python3 -I -c 'import pathlib, sys; pathlib.Path(sys.argv[1]).write_bytes(b" " * (8 * 1024 * 1024 + 1))' \
    "$work/oversize-renovate/package.json"
  expect_dump_failure "dump-mjs-oversize" "$work/oversize-renovate" "exceeds cap"
  cp -R "$root" "$work/growing-renovate"
  cat > "$work/grow.mjs" <<'JS'
import fs from 'node:fs';
import path from 'node:path';
const original = fs.fstatSync;
let first = true;
fs.fstatSync = (...args) => {
  const stat = original(...args);
  if (first) {
    first = false;
    fs.appendFileSync(path.join(process.env.RENOVATE_ROOT, 'package.json'), ' ');
  }
  return stat;
};
JS
  out="$(RENOVATE_ROOT="$work/growing-renovate" node --import "$work/grow.mjs" "$dump_mjs" 2>&1)" || rc=$?
  check_case "dump-mjs-growth" 1 "$rc" "grew while reading" "$out"
  echo "ok   dump-mjs-growth"
}

# test_dump_read_boundaries ROOT: accept the exact size cap and accumulate partial descriptor reads.
test_dump_read_boundaries() {
  local root="$1"
  cp -R "$root" "$work/at-cap-renovate"
  python3 -I - "$work/at-cap-renovate/package.json" <<'PY'
import pathlib
import sys

package = pathlib.Path(sys.argv[1])
package.write_bytes(package.read_bytes().ljust(8 * 1024 * 1024, b" "))
PY
  check_dump_positive "$work/at-cap-renovate"
  echo "ok   dump-mjs-at-cap"
  cat > "$work/short-reads.mjs" <<'JS'
import fs from 'node:fs';
const original = fs.readSync;
fs.readSync = (fd, buffer, offset, length, position) =>
  original(fd, buffer, offset, Math.min(length, 7), position);
JS
  check_dump_positive "$root" "$work/short-reads.mjs"
  echo "ok   dump-mjs-short-reads"
}

# test_dump_mjs: verify renovate_presets_dump.mjs against positive and broken synthetic dist trees.
test_dump_mjs() {
  local root="$work/fake-renovate" mig="dist/config/migrations/custom"
  write_fake_dist "$root"
  check_dump_positive "$root"
  test_dump_proto "$root"
  test_dump_reads "$root"
  test_dump_read_boundaries "$root"

  cp -R "$root" "$work/no-import-renovate"
  printf '%s\n' 'export const kind = "github-runner";' \
    > "$work/no-import-renovate/dist/modules/manager/github-actions/extract.js"
  expect_dump_failure "dump-mjs-stale-import-regex" "$work/no-import-renovate" \
    "DOCKER_IMPORT_RE may be stale"

  cp -R "$root" "$work/no-migration-renovate"
  rm "$work/no-migration-renovate/$mig/match-managers-migration.js"
  expect_dump_failure "dump-mjs-missing-migration" "$work/no-migration-renovate" \
    "match-managers-migration.js"

  cp -R "$root" "$work/zero-migration-renovate"
  printf '%s\n' 'export const unchanged = true;' \
    > "$work/zero-migration-renovate/$mig/enabled-managers-migration.js"
  expect_dump_failure "dump-mjs-zero-case-migration" "$work/zero-migration-renovate" \
    "yielded no manager renames"

  cp -R "$root" "$work/many-migrations"
  python3 -I - "$work/many-migrations/$mig/match-managers-migration.js" <<'PY'
import pathlib
import sys

pathlib.Path(sys.argv[1]).write_text(
    "\n".join(f'case "m{i}": return "n{i}";' for i in range(65)),
    encoding="utf-8",
)
PY
  expect_dump_failure "dump-mjs-over-max-renames" "$work/many-migrations" \
    "exceeds 64 manager renames"

  mkdir -p "$work/bad-renovate"
  expect_dump_failure "dump-mjs-missing-pkg" "$work/bad-renovate" "renovate_presets_dump: error:"
}

# 0. Horistes fetch self-test (no network) + dump.mjs unit test.
self_out="$(python3 -I "$check" --self-test 2>&1)"
check_case "horistes-self-test" 0 0 "renovate_presets: self-test ok" "$self_out"
echo "ok   horistes-self-test"
test_dump_mjs

write_base_repo "$BASE_REPO"

# 1. Baseline clean closure.
expect_preset_case "baseline-pass" 0 "renovate_presets: ok (renovate=44.234.0, horistes=offline" \
  '{"extends":["config:recommended"]}'
expect_preset_case "malformed-manager-renames" 1 "managerRenames must be an object" \
  '{"description":"ok"}' "{}" "$BASE_REPO" "" '[]'
expect_preset_case "missing-manager-rename-map" 1 "managerRenames must contain" \
  '{"description":"ok"}' "{}" "$BASE_REPO" "" '{"matchManagers":{}}'
expect_preset_case "malformed-manager-rename-map" 1 \
  "managerRenames['matchManagers'] must be an object" \
  '{"description":"ok"}' "{}" "$BASE_REPO" "" \
  '{"matchManagers":[],"enabledManagers":{}}'
expect_preset_case "invalid-manager-rename-name" 1 "has an invalid manager name" \
  '{"description":"ok"}' "{}" "$BASE_REPO" "" \
  '{"matchManagers":{"bad name":"npm"},"enabledManagers":{}}'
expect_preset_case "proto-manager-rename-source" 1 "has an invalid manager name" \
  '{"description":"ok"}' "{}" "$BASE_REPO" "" \
  '{"matchManagers":{"__proto__":"dockerfile"},"enabledManagers":{}}'
expect_preset_case "proto-manager-rename-target" 1 "has an invalid manager name" \
  '{"description":"ok"}' "{}" "$BASE_REPO" "" \
  '{"matchManagers":{},"enabledManagers":{"npm":"__proto__"}}'
RENAMES_64="$(python3 -I -c '
import json
print(json.dumps({
    "matchManagers": {f"m{i}": f"n{i}" for i in range(64)},
    "enabledManagers": {"regex": "custom.regex"},
}))
')"
expect_preset_case "manager-renames-at-cap" 0 "renovate_presets: ok" \
  '{"description":"ok"}' "{}" "$BASE_REPO" "" "$RENAMES_64"
RENAMES_65="$(python3 -I -c '
import json
print(json.dumps({
    "matchManagers": {f"m{i}": f"n{i}" for i in range(65)},
    "enabledManagers": {"regex": "custom.regex"},
}))
')"
expect_preset_case "manager-renames-over-cap" 1 "exceeds 64 entries" \
  '{"description":"ok"}' "{}" "$BASE_REPO" "" "$RENAMES_65"

# 2. Override & redirect keys (all exit 3).
expect_preset_case "override-datasource" 3 "packageRules[0]: key 'overrideDatasource' is not allowed on a Docker-reachable rule" \
  '{"packageRules":[{"matchDatasources":["docker"],"overrideDatasource":"npm"}]}'
expect_preset_case "override-package-name-unscoped" 3 "packageRules[0]: key 'overridePackageName' is not allowed on a Docker-reachable rule" \
  '{"packageRules":[{"overridePackageName":"x"}]}'
expect_preset_case "rule-registry-urls" 3 "packageRules[0]: key 'registryUrls' is not allowed on a Docker-reachable rule" \
  '{"packageRules":[{"registryUrls":["https://ghcr.io"]}]}'
expect_preset_case "top-registry-aliases" 3 "top-level key 'registryAliases' is not allowed" \
  '{"registryAliases":{"a":"b"}}'

# 3. enabled: false refusals (all exit 3).
expect_preset_case "disable-docker-datasource" 3 "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"matchDatasources":["docker"],"enabled":false}]}'
expect_preset_case "disable-nonliteral-datasource" 3 "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"matchDatasources":["!npm"],"enabled":false}]}'
expect_preset_case "disable-dockerfile-manager" 3 "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"matchManagers":["dockerfile"],"enabled":false}]}'
expect_preset_case "disable-stage-deptype" 3 "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"matchDepTypes":["stage"],"enabled":false}]}'
expect_preset_case "disable-mixed-datasource" 3 "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"matchDatasources":["npm","docker"],"enabled":false}]}'
expect_preset_case "disable-mixed-manager" 3 "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"matchManagers":["npm","dockerfile"],"enabled":false}]}'
expect_preset_case "disable-mixed-deptype" 3 "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"matchDepTypes":["github-runner","stage"],"enabled":false}]}'
expect_preset_case "top-enabled-false" 3 "top-level key 'enabled' is false" \
  '{"enabled":false}'

REPO_DEPTYPE="$work/repo-deptype"
write_base_repo "$REPO_DEPTYPE" '{
  "customManagers": [{
    "customType": "regex",
    "managerFilePatterns": ["tools/ci/tool-versions.env"],
    "matchStrings": ["IMG=(?<depName>renovate/renovate):(?<currentValue>[^@\\s]+)\\n"],
    "datasourceTemplate": "docker",
    "depTypeTemplate": "github-runner"
  }]
}'
expect_preset_case "disable-deptype-used-by-repo-docker-mgr" 3 \
  "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"matchDepTypes":["github-runner"],"enabled":false}]}' \
  "{}" "$REPO_DEPTYPE"

REPO_DYN_DEPTYPE="$work/repo-dyn-deptype"
write_base_repo "$REPO_DYN_DEPTYPE" '{
  "customManagers": [{
    "customType": "regex",
    "managerFilePatterns": ["tools/ci/tool-versions.env"],
    "matchStrings": ["IMG=(?<depName>renovate/renovate):(?<currentValue>[^@\\s]+)\\n"],
    "datasourceTemplate": "docker",
    "depTypeTemplate": "{{depType}}"
  }]
}'
expect_preset_case "disable-deptype-with-templated-repo-mgr" 3 \
  "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"matchDepTypes":["github-runner"],"enabled":false}]}' \
  "{}" "$REPO_DYN_DEPTYPE"

# 4. Exit 0 exclusion boundaries.
expect_preset_case "exclude-disabled-maven-manager" 0 "excluded=1 (manager=1)" \
  '{"packageRules":[{"matchManagers":["maven"],"enabled":false}]}'
expect_preset_case "exclude-github-runner-deptype" 0 "excluded=1 (depType=1)" \
  '{"packageRules":[{"matchDepTypes":["github-runner"],"matchManagers":["github-actions"],"enabled":false}]}'
expect_preset_case "exclude-maven-datasource" 0 "excluded=1 (datasource=1)" \
  '{"packageRules":[{"matchDatasources":["maven"],"enabled":false}]}'
expect_preset_case "renamed-regex-manager-refused" 3 \
  "key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"matchManagers":["regex"],"enabled":false}]}'
expect_preset_case "unrenamed-npm-manager-excluded" 0 "excluded=1 (manager=1)" \
  '{"packageRules":[{"matchManagers":["npm"],"enabled":false}]}'

REPO_RENAMED_ENABLED="$work/repo-renamed-enabled"
write_base_repo "$REPO_RENAMED_ENABLED" '{"enabledManagers":["regex"]}'
expect_preset_case "renamed-enabled-manager-refused" 3 \
  "key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"matchManagers":["custom.regex"],"enabled":false}]}' \
  "{}" "$REPO_RENAMED_ENABLED"

# 5. Preset customManagers.
expect_preset_case "preset-cm-docker" 3 "customManagers[0]: key 'datasourceTemplate' ('docker')" \
  '{"customManagers":[{"customType":"regex","managerFilePatterns":["a"],"matchStrings":["(?<depName>x)"],"datasourceTemplate":"docker"}]}'
expect_preset_case "preset-cm-absent-ds" 3 "customManagers[0]: key 'datasourceTemplate' (None)" \
  '{"customManagers":[{"customType":"regex","managerFilePatterns":["a"],"matchStrings":["(?<depName>x)"]}]}'
expect_preset_case "preset-cm-templated-ds" 3 "customManagers[0]: key 'datasourceTemplate' ('{{datasource}}')" \
  '{"customManagers":[{"customType":"regex","managerFilePatterns":["a"],"matchStrings":["(?<depName>x)"],"datasourceTemplate":"{{datasource}}"}]}'
expect_preset_case "preset-cm-github-tags-pass" 0 "renovate_presets: ok" \
  '{"customManagers":[{"customType":"regex","managerFilePatterns":["a"],"matchStrings":["(?<depName>x)"],"datasourceTemplate":"github-tags"}]}'

# 6. Disallowed preset forms & ignorePresets.
expect_preset_case "preset-form-other-github" 3 "non-built-in preset 'github>someone/else' is refused" \
  '{"extends":["github>someone/else"]}'
expect_preset_case "preset-form-local" 3 "non-built-in preset 'local>x/y' is refused" \
  '{"extends":["local>x/y"]}'
expect_preset_case "preset-form-parameterized" 3 "templated or parameterized preset 'config:recommended(a)' is refused" \
  '{"extends":["config:recommended(a)"]}'
expect_preset_case "preset-form-template" 3 "templated or parameterized preset '{{x}}' is refused" \
  '{"extends":["{{x}}"]}'
expect_preset_case "preset-form-missing" 3 "unresolved preset 'group:missing'" \
  '{"extends":["group:missing"]}'

REPO_IGNORE_PRESETS="$work/repo-ignore-presets"
write_base_repo "$REPO_IGNORE_PRESETS" '{"ignorePresets":[":prHourlyLimit2"]}'
expect_preset_case "repo-ignore-presets" 3 "renovate.json: key 'ignorePresets' at ignorePresets is refused" \
  '{"description":"ok"}' "{}" "$REPO_IGNORE_PRESETS"

expect_preset_case "preset-ignore-presets" 3 "key 'ignorePresets' at ignorePresets is refused" \
  '{"ignorePresets":[":prHourlyLimit2"]}'

# 7. Unknown keys & top-level manager blocks.
expect_preset_case "unknown-top-key" 3 "unknown top-level key 'mysteryTopOption'" \
  '{"mysteryTopOption":true}'
expect_preset_case "unknown-rule-key" 3 "packageRules[0]: unknown rule key 'mysteryRuleOption'" \
  '{"packageRules":[{"mysteryRuleOption":1}]}'
expect_preset_case "top-docker-manager-block" 3 "top-level Docker-capable manager block 'dockerfile' is not allowed" \
  '{"dockerfile":{"enabled":false}}'
expect_preset_case "top-enabled-managers" 3 "top-level key 'enabledManagers' is not allowed" \
  '{"enabledManagers":["npm"]}'

# 7a. A preset cannot disable the top-level release wait unless the repository overrides it.
expect_preset_case "top-age-null" 3 "top-level minimumReleaseAge None disables the release wait" \
  '{"minimumReleaseAge":null}'
expect_preset_case "top-age-zero" 3 "top-level minimumReleaseAge '0 days' disables the release wait" \
  '{"minimumReleaseAge":"0 days"}'
expect_preset_case "top-age-positive" 0 "renovate_presets: ok" \
  '{"minimumReleaseAge":"3 days"}'
REPO_TOP_AGE="$work/repo-top-age"
write_base_repo "$REPO_TOP_AGE" '{"minimumReleaseAge":"3 days"}'
expect_preset_case "top-age-repo-override" 0 "renovate_presets: ok" \
  '{"minimumReleaseAge":null}' "{}" "$REPO_TOP_AGE"
expect_preset_case "top-age-invalid-type" 1 "minimumReleaseAge must be string or null" \
  '{"minimumReleaseAge":3}'

# 8. Nested extends inside packageRules and disallowed nested extends.
expect_preset_case "nested-rule-extends-refused" 3 "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"extends":["monorepo:x"]}]}' \
  '{"monorepo":{"x":{"matchDatasources":["docker"],"enabled":false}}}'
expect_preset_case "chained-rule-preset-extends-refused" 3 \
  "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"extends":["grp:a"]}]}' \
  '{"grp":{"a":{"extends":["grp:b"]},"b":{"matchDatasources":["docker"],"enabled":false}}}'
expect_preset_case "chained-rule-preset-extends-pass" 0 \
  "excluded=1 (datasource=1)" \
  '{"packageRules":[{"extends":["grp:a"]}]}' \
  '{"grp":{"a":{"extends":["grp:b"]},"b":{"matchDatasources":["maven"],"enabled":false}}}'
expect_preset_case "rule-union-keeps-docker" 3 \
  "packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"packageRules":[{"extends":["grp:d"],"matchDatasources":["npm"],"enabled":false}]}' \
  '{"grp":{"d":{"matchDatasources":["docker"]}}}'
expect_preset_case "rule-union-non-docker-pass" 0 \
  "excluded=1 (datasource=1)" \
  '{"packageRules":[{"extends":["grp:n"],"matchDatasources":["npm"],"enabled":false}]}' \
  '{"grp":{"n":{"matchDatasources":["maven"]}}}'
expect_preset_case "nested-extends-outside-rules" 3 "key 'extends' at vulnerabilityAlerts.extends is outside allowed contexts" \
  '{"vulnerabilityAlerts":{"extends":["config:recommended"]}}'

REPO_RULE_EXTENDS="$work/repo-rule-extends"
write_base_repo "$REPO_RULE_EXTENDS" '{"packageRules":[{"matchPackageNames":["x"],"extends":["monorepo:bad"]}]}'
expect_preset_case "repo-rule-extends-refused" 3 \
  "renovate.json packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"extends":["config:recommended"]}' \
  '{"monorepo":{"bad":{"matchDatasources":["docker"],"enabled":false}}}' \
  "$REPO_RULE_EXTENDS"

REPO_RULE_EXTENDS_CLEAN="$work/repo-rule-extends-clean"
write_base_repo "$REPO_RULE_EXTENDS_CLEAN" '{"packageRules":[{"matchPackageNames":["x"],"extends":["monorepo:clean"]}]}'
expect_preset_case "repo-rule-extends-pass" 0 \
  "renovate_presets: ok" \
  '{"extends":["config:recommended"]}' \
  "{}" \
  "$REPO_RULE_EXTENDS_CLEAN"

REPO_RULE_EXTENDS_EXCLUDED="$work/repo-rule-extends-excluded"
write_base_repo "$REPO_RULE_EXTENDS_EXCLUDED" '{"packageRules":[{"matchPackageNames":["x"],"extends":["grp:non-docker"]}]}'
expect_preset_case "repo-rule-extends-excluded" 0 \
  "excluded=1 (datasource=1)" \
  '{"extends":["config:recommended"]}' \
  '{"grp":{"non-docker":{"matchDatasources":["maven"],"enabled":false}}}' \
  "$REPO_RULE_EXTENDS_EXCLUDED"

# 9. Cycle safety & MAX_PRESETS cap.
expect_preset_case "cycle-clean" 0 "renovate_presets: ok" \
  '{"extends":["cyc:a"]}' \
  '{"cyc":{"a":{"extends":["cyc:b"]},"b":{"extends":["cyc:a"]}}}'
expect_preset_case "cycle-refused" 3 "cyc:b packageRules[0]: key 'enabled' is false on a Docker-reachable rule" \
  '{"extends":["cyc:a"]}' \
  '{"cyc":{"a":{"extends":["cyc:b"]},"b":{"extends":["cyc:a"],"packageRules":[{"matchDatasources":["docker"],"enabled":false}]}}}'

OVER_DIR="$work/case-over-max-presets"
mkdir -p "$OVER_DIR"
python3 -I -c '
import json, pathlib, sys
d = pathlib.Path(sys.argv[1])
chain = {f"p{i}": ({"extends": [f"big:p{i+1}"]} if i < 4096 else {"description": "end"}) for i in range(4097)}
dump = {
    "renovateVersion": "44.234.0",
    "node": "v24.22.2",
    "groups": {"default": {"semanticCommits": {"semanticCommits": "enabled"}}, "big": chain},
    "dockerManagers": ["dockerfile"],
    "depTypeEmitters": {
        "github-runner": [
            "modules/manager/github-actions/dep-types.js",
            "modules/manager/github-actions/extract.js",
        ],
        "php2shell-runtime": ["config/presets/internal/security.preset.js"],
    },
    "managerRenames": {
        "matchManagers": {"regex": "custom.regex"},
        "enabledManagers": {"regex": "custom.regex", "yarn": "npm"},
    },
}
(d / "dump.json").write_text(json.dumps(dump), encoding="utf-8")
(d / "horistes.json").write_text(json.dumps({"extends": ["big:p0"]}), encoding="utf-8")
' "$OVER_DIR"
over_rc=0
over_out="$(run_bounded 15 python3 -I "$check" \
  --dump "$OVER_DIR/dump.json" \
  --horistes "$OVER_DIR/horistes.json" \
  --repo-root "$BASE_REPO" 2>&1)" || over_rc=$?
check_case "over-max-presets" 1 "$over_rc" "exceeds MAX_PRESETS (4096)" "$over_out"
echo "ok   over-max-presets"

# 10. ignorePaths shape & segment collision with tracked pins / Docker customManagers.
expect_preset_case "ignorepaths-bad-shape" 3 "ignorePaths[0]: key 'ignorePaths' entry 'src/**'" \
  '{"ignorePaths":["src/**"]}'

REPO_TEST_PIN="$work/repo-test-pin"
write_base_repo "$REPO_TEST_PIN"
(
  cd "$REPO_TEST_PIN"
  track "tests/image.yaml" "image: alpine:3.20@sha256:${DIGEST}"
)
expect_preset_case "ignorepaths-hides-pin" 3 "matches image pin path 'tests/image.yaml'" \
  '{"ignorePaths":["**/tests/**"]}' "{}" "$REPO_TEST_PIN"

REPO_TEST_MGR="$work/repo-test-mgr"
write_base_repo "$REPO_TEST_MGR" '{
  "customManagers": [{
    "customType": "regex",
    "managerFilePatterns": ["tests/versions.env"],
    "matchStrings": ["IMG=(?<depName>renovate/renovate):(?<currentValue>[^@\\s]+)\\n"],
    "datasourceTemplate": "docker"
  }]
}'
expect_preset_case "ignorepaths-hides-manager-file" 3 "matches manager path 'tests/versions.env'" \
  '{"ignorePaths":["**/tests/**"]}' "{}" "$REPO_TEST_MGR"

# 11. depTypeEmitters mismatch & unpinned RENOVATE_IMAGE.
expect_preset_case "deptype-emitters-mismatch" 3 "depTypeEmitters['github-runner'] mismatch" \
  '{"description":"ok"}' "{}" "$BASE_REPO" \
  '{"github-runner":["modules/manager/github-actions/extract.js","modules/manager/other/new.js"],"php2shell-runtime":["config/presets/internal/security.preset.js"]}'

BAD_TV="$work/bad-tool-versions.env"
printf '%s\n' 'RENOVATE_IMAGE=mirror.gcr.io/renovate/renovate:44.234.0' > "$BAD_TV"
tv_rc=0
tv_out="$(python3 -I "$check" --tool-versions "$BAD_TV" --repo-root "$BASE_REPO" 2>&1)" || tv_rc=$?
check_case "unpinned-renovate-image" 3 "$tv_rc" "is not pinned by version and sha256 digest" "$tv_out"
echo "ok   unpinned-renovate-image"

# 12. Local reads stay bounded, and a large stderr stream cannot block the Docker dump child.
FAKE_BIN="$work/fake-bin"
mkdir -p "$FAKE_BIN"
cat > "$FAKE_BIN/docker" <<'PY'
#!/usr/bin/env python3
import sys

sys.stderr.write("x" * (1 << 20))
sys.stdout.write('{"dump":"ok"}')
PY
chmod +x "$FAKE_BIN/docker"
run_bounded 10 python3 -I - "$check" "$FAKE_BIN" "$work" <<'PY'
import importlib.util
import os
import pathlib
import sys

spec = importlib.util.spec_from_file_location("renovate_presets", sys.argv[1])
if spec is None or spec.loader is None:
    raise SystemExit("cannot load renovate_presets module")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)
module.DOCKER_TIMEOUT_SECONDS = 5
os.environ["PATH"] = f"{sys.argv[2]}:{os.environ['PATH']}"
if module.run_docker_dump("example.invalid/x:1@sha256:" + "a" * 64, pathlib.Path(sys.argv[1])) != b'{"dump":"ok"}':
    raise SystemExit("large-stderr dump output mismatch")
module.MAX_STDERR_READS = 1
try:
    module.run_docker_dump("example.invalid/x:1@sha256:" + "a" * 64, pathlib.Path(sys.argv[1]))
except module.ConfigError as exc:
    if "stderr exceeded bounded drain" not in str(exc):
        raise
else:
    raise SystemExit("stderr drain read bound was not enforced")
bounded = pathlib.Path(sys.argv[3], "bounded.txt")
bounded.write_bytes(b"x" * 5)
if module.read_bounded_file(bounded, 5, "bounded") != b"x" * 5:
    raise SystemExit("cap boundary was not accepted")
bounded.write_bytes(b"x" * 6)
try:
    module.read_bounded_file(bounded, 5, "bounded")
except module.ConfigError as exc:
    if "exceeds cap of 5" not in str(exc):
        raise
else:
    raise SystemExit("oversized local file was accepted")
PY
echo "ok   bounded-file-and-docker-stderr"

# 13. Both CI entry points invoke this check; planted copies prove every assertion fails closed.
assert_wiring() {
  python3 -I - "$1" "$2" "$3" <<'PY'
import pathlib
import sys

text = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
if sys.argv[2] == "workflow":
    marker = "      - name: Renovate presets reachable from renovate.json keep Docker release-age coverage, after their planted cases\n"
    if marker not in text:
        raise SystemExit("presets workflow step is missing")
    section = text.split(marker, 1)[1].split("\n      - name:", 1)[0]
else:
    marker = "workflows-lint:"
    if marker not in text:
        raise SystemExit("workflows-lint target is missing")
    section = text.split(marker, 1)[1].split("\n\n", 1)[0]
if sys.argv[3] not in section:
    raise SystemExit(f"missing wiring: {sys.argv[3]}")
PY
}

assert_wiring "$HERE/../../.github/workflows/ci.yml" workflow \
  "python3 -I tools/ci/renovate_presets.py"
assert_wiring "$HERE/../../.github/workflows/ci.yml" workflow "cosign verify"
assert_wiring "$HERE/../../Makefile" makefile "python3 -I tools/ci/renovate_presets.py"

# assert_workflow_timeout CI_YML [minimum]: require enough budget or print its ceiling in minutes.
assert_workflow_timeout() {
  python3 -I - "$1" "${2:-assert}" <<'PY'
import pathlib
import re
import sys

text = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
job_match = re.search(r"^  workflows:\n(?P<body>.*?)(?=^  [a-z][a-z0-9_-]*:\n|\Z)", text, re.M | re.S)
if job_match is None:
    raise SystemExit("workflows job is missing")
job = job_match.group("body")
minutes = re.search(r"^    timeout-minutes: ([0-9]+)$", job, re.M)
if minutes is None:
    raise SystemExit("workflows job timeout-minutes is missing")
step = re.search(
    r"^      - name: Renovate presets reachable from renovate\.json keep Docker release-age coverage, after their planted cases\n(?P<body>.*?)(?=^      - name:|\Z)",
    job,
    re.M | re.S,
)
if step is None:
    raise SystemExit("presets workflow step is missing")
required = sum(int(value) for value in re.findall(r"^\s*timeout ([0-9]+)\b", step.group("body"), re.M)) + 300
available = int(minutes.group(1)) * 60
if sys.argv[2] == "minimum":
    print((required + 59) // 60)
elif available < required:
    raise SystemExit(f"workflows timeout {available}s is below required {required}s")
PY
}

# write_workflow_timeout SOURCE TARGET MINUTES: rewrite exactly one timeout in the workflows job.
write_workflow_timeout() {
  python3 -I - "$1" "$2" "$3" <<'PY'
import pathlib
import re
import sys

text = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
job_match = re.search(r"^  workflows:\n(?P<body>.*?)(?=^  [a-z][a-z0-9_-]*:\n|\Z)", text, re.M | re.S)
if job_match is None:
    raise SystemExit("workflows job is missing")
body, changed = re.subn(r"^    timeout-minutes: [0-9]+$", f"    timeout-minutes: {int(sys.argv[3])}", job_match.group("body"), flags=re.M)
if changed != 1 or body == job_match.group("body"):
    raise SystemExit(f"timeout mutation must change exactly one line (matched {changed})")
pathlib.Path(sys.argv[2]).write_text(
    text[:job_match.start("body")] + body + text[job_match.end("body"):], encoding="utf-8"
)
PY
}

# test_workflow_timeout: prove value-independent mutation, exact-one-line refusal and both boundaries.
test_workflow_timeout() {
  local ci="$HERE/../../.github/workflows/ci.yml" minimum
  minimum="$(assert_workflow_timeout "$ci" minimum)"
  python3 -I - "$ci" "$work" "$minimum" <<'PY'
import pathlib
import re
import sys

text = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
pattern = r"(^  workflows:\n.*?)^    timeout-minutes: [0-9]+$"
for name, replacement in (
    ("26", "    timeout-minutes: 26"),
    ("boundary-source", f"    timeout-minutes: {int(sys.argv[3]) + 1}"),
    ("missing", ""),
    ("duplicate", "    timeout-minutes: 25\n    timeout-minutes: 26"),
):
    fixture, changed = re.subn(pattern, lambda m: m.group(1) + replacement, text, count=1, flags=re.M | re.S)
    if changed != 1:
        raise SystemExit("cannot seed workflows timeout fixture")
    pathlib.Path(sys.argv[2], f"ci-{name}.yml").write_text(fixture, encoding="utf-8")
PY
  write_workflow_timeout "$work/ci-26.yml" "$work/ci-short-timeout.yml" 10
  if assert_workflow_timeout "$work/ci-short-timeout.yml"; then
    echo "FAIL wiring: workflow copy with insufficient timeout passed (source timeout 26)" >&2
    exit 1
  fi
  write_workflow_timeout "$work/ci-boundary-source.yml" "$work/ci-boundary.yml" "$minimum"
  assert_workflow_timeout "$work/ci-boundary.yml"
  write_workflow_timeout "$work/ci-boundary-source.yml" "$work/ci-below-boundary.yml" "$((minimum - 1))"
  if assert_workflow_timeout "$work/ci-below-boundary.yml"; then
    echo "FAIL wiring: workflow timeout one minute below the boundary passed" >&2
    exit 1
  fi
  for name in missing duplicate; do
    if write_workflow_timeout "$work/ci-$name.yml" "$work/ci-bad-mutation.yml" 10; then
      echo "FAIL wiring: $name timeout mutation did not refuse its match count" >&2
      exit 1
    fi
  done
  echo "ok   workflow-timeout-26-and-boundaries (minimum=${minimum}m)"
}

assert_workflow_timeout "$HERE/../../.github/workflows/ci.yml"

sed '/python3 -I tools\/ci\/renovate_presets.py/d' "$HERE/../../.github/workflows/ci.yml" \
  > "$work/ci-no-check.yml"
if assert_wiring "$work/ci-no-check.yml" workflow "python3 -I tools/ci/renovate_presets.py"; then
  echo "FAIL wiring: workflow copy without checker passed" >&2
  exit 1
fi
sed '/cosign verify/d' "$HERE/../../.github/workflows/ci.yml" > "$work/ci-no-cosign.yml"
if assert_wiring "$work/ci-no-cosign.yml" workflow "cosign verify"; then
  echo "FAIL wiring: workflow copy without cosign verification passed" >&2
  exit 1
fi
sed '/python3 -I tools\/ci\/renovate_presets.py/d' "$HERE/../../Makefile" \
  > "$work/make-no-check"
if assert_wiring "$work/make-no-check" makefile "python3 -I tools/ci/renovate_presets.py"; then
  echo "FAIL wiring: Makefile copy without checker passed" >&2
  exit 1
fi
test_workflow_timeout
echo "ok   checker-wiring"
