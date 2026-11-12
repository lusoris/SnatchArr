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

# write_stub_fixtures DUMP_PATH HORISTES_PATH [GROUPS_PATCH_JSON] [HORISTES_JSON] [EMITTERS_JSON]:
# emit a valid stub dump JSON and horistes JSON with optional overrides.
write_stub_fixtures() {
  local dump_path="$1" horistes_path="$2"
  local groups_patch="${3:-"{}"}" horistes_json="${4:-}" emitters_json="${5:-}"
  python3 -I -c '
import json, pathlib, sys
dump_p, hor_p, g_patch, h_raw, e_raw = sys.argv[1:6]
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
dump = {
    "renovateVersion": "44.234.0",
    "node": "v24.22.2",
    "groups": groups,
    "dockerManagers": ["dockerfile", "github-actions", "maven", "nuget"],
    "depTypeEmitters": emitters,
}
horistes = json.loads(h_raw) if h_raw else {"extends": ["config:recommended"]}
pathlib.Path(dump_p).write_text(json.dumps(dump), encoding="utf-8")
pathlib.Path(hor_p).write_text(json.dumps(horistes), encoding="utf-8")
' "$dump_path" "$horistes_path" "$groups_patch" "$horistes_json" "$emitters_json"
}

# expect_preset_case NAME WANT_RC NEEDLE HORISTES_JSON [GROUPS_PATCH_JSON] [REPO_DIR] [EMITTERS_JSON]:
# run renovate_presets.py against stub dump + horistes under a 10s bound and assert verdict.
expect_preset_case() {
  local name="$1" want="$2" needle="$3" hor_json="$4"
  local g_patch="${5:-"{}"}" repo_dir="${6:-$BASE_REPO}" e_json="${7:-}"
  local dir="$work/case-$name" rc=0 out
  mkdir -p "$dir"
  write_stub_fixtures "$dir/dump.json" "$dir/horistes.json" "$g_patch" "$hor_json" "$e_json"
  out="$(run_bounded 10 python3 -I "$check" \
    --dump "$dir/dump.json" \
    --horistes "$dir/horistes.json" \
    --repo-root "$repo_dir" 2>&1)" || rc=$?
  check_case "$name" "$want" "$rc" "$needle" "$out"
  echo "ok   $name"
}

# test_dump_mjs: verify renovate_presets_dump.mjs against positive and broken synthetic dist trees.
test_dump_mjs() {
  local root="$work/fake-renovate" bad="$work/bad-renovate" out rc=0
  mkdir -p "$root/dist/config/presets/internal" \
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
import { x } from "../../datasource/docker/common.js";
export const kind = "github-runner";
EOF
  cat > "$root/dist/modules/manager/npm/extract.js" <<'EOF'
export const n = 1;
EOF
  out="$(RENOVATE_ROOT="$root" node "$dump_mjs")"
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
' "$out"
  echo "ok   dump-mjs-positive"

  mkdir -p "$bad"
  out="$(RENOVATE_ROOT="$bad" node "$dump_mjs" 2>&1)" || rc=$?
  check_case "dump-mjs-missing-pkg" 1 "$rc" "renovate_presets_dump: error:" "$out"
  echo "ok   dump-mjs-missing-pkg"
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
