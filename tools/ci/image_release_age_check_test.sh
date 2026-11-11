#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for image_release_age_check.py: each case stages a throwaway git repository with
# .needs.yaml, renovate.json and one container image digest pin, then checks the exit status and
# diagnostic output. Digests are constructed at run time so this file holds no literal pin.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
check=$HERE/image_release_age_check.py
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"

D=$(printf '%064d' 0 | tr 0 a)
ADVICE="add a renovate.json packageRule (minimumReleaseAge null for media apps, overridePackageName to a Docker Hub twin, or timestamp-optional with the measurement) for each image above"

# setup_null_exact: exact matchDepNames rule setting minimumReleaseAge to null covers an undated pin.
setup_null_exact() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["ghcr.io/planted/undated"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_null_regex_i: case-insensitive /re/i pattern covers an undated pin.
setup_null_regex_i() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["/^GHCR\\.IO/PLANTED/UNDATED$/i"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_timestamp_optional: minimumReleaseAgeBehaviour timestamp-optional covers an undated pin.
setup_timestamp_optional() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: mcr.microsoft.com/playwright:v1.0.0@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["mcr.microsoft.com/playwright"],
        "minimumReleaseAgeBehaviour": "timestamp-optional"
      }]
    }'
}

# setup_override_hub: literal overridePackageName to a Docker Hub image covers an undated pin.
setup_override_hub() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: lscr.io/linuxserver/ffmpeg:9.0@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["lscr.io/linuxserver/ffmpeg"],
        "overridePackageName": "linuxserver/ffmpeg"
      }]
    }'
}

# setup_override_template: replace template in overridePackageName strips mirror.gcr.io/ to hub name.
setup_override_template() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: mirror.gcr.io/aquasec/trivy:0.75.0@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["/^mirror\\.gcr\\.io\\//"],
        "overridePackageName": "{{{replace '\''^mirror\\.gcr\\.io/'\'' '\'''\'' depName}}}"
      }]
    }'
}

# setup_override_double_brace: {{replace ...}} template form is also accepted.
setup_override_double_brace() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: mirror.gcr.io/library/postgres:18-alpine@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchPackageNames": ["mirror.gcr.io/library/postgres"],
        "overridePackageName": "{{replace '\''^mirror\\.gcr\\.io/'\'' '\'''\'' depName}}"
      }]
    }'
}

# setup_own_image: ghcr.io/<owner>/ is exempt when .needs.yaml names github.com/<owner>/<repo>.
setup_own_image() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/o/x:1.0@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_hub_host: docker.io host is dated on Docker Hub and needs no rule.
setup_hub_host() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: docker.io/linuxserver/deluge:2.2.0@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_hostless: host-less Docker Hub reference is dated and needs no rule.
setup_hostless() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: postgres:18-alpine@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_no_rule: undated pin with no matching rule is refused with file:line and advice.
setup_no_rule() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_override_undated: overridePackageName pointing to an undated host does not cover the pin.
setup_override_undated() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["ghcr.io/planted/undated"],
        "overridePackageName": "ghcr.io/x/y"
      }]
    }'
}

# setup_extra_matcher_null: rule with matchFileNames holding null does not count as coverage.
setup_extra_matcher_null() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["ghcr.io/planted/undated"],
        "matchFileNames": ["a.yml"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_later_wait: a later rule setting minimumReleaseAge back to "3 days" undoes an earlier null.
setup_later_wait() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [
        {
          "matchDatasources": ["docker"],
          "matchDepNames": ["ghcr.io/planted/undated"],
          "minimumReleaseAge": null
        },
        {
          "matchDatasources": ["docker"],
          "matchDepNames": ["ghcr.io/planted/undated"],
          "minimumReleaseAge": "3 days"
        }
      ]
    }'
}

# setup_extra_matcher_undo: unmodeled matcher setting non-null minimumReleaseAge fails closed naming rule index.
setup_extra_matcher_undo() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [
        {
          "matchDatasources": ["docker"],
          "matchDepNames": ["ghcr.io/planted/undated"],
          "minimumReleaseAge": null
        },
        {
          "matchDatasources": ["docker"],
          "matchFileNames": ["a.yml"],
          "minimumReleaseAge": "3 days"
        }
      ]
    }'
}

# setup_wrong_datasource: rule matching only npm datasource does not cover a docker image.
setup_wrong_datasource() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["npm"],
        "matchDepNames": ["ghcr.io/planted/undated"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_glob_null: glob pattern in matchDepNames with null does not count as coverage.
setup_glob_null() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["ghcr.io/planted/*"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_negation_undo: ! negation pattern with non-null minimumReleaseAge fails closed naming rule index.
setup_negation_undo() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [
        {
          "matchDatasources": ["docker"],
          "matchDepNames": ["ghcr.io/planted/undated"],
          "minimumReleaseAge": null
        },
        {
          "matchDatasources": ["docker"],
          "matchDepNames": ["!ghcr.io/other/app"],
          "minimumReleaseAge": "3 days"
        }
      ]
    }'
}

# setup_unsupported_template: any template other than replace '<re>' '<s>' depName is refused.
setup_unsupported_template() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["ghcr.io/planted/undated"],
        "overridePackageName": "{{{depName}}}"
      }]
    }'
}

# setup_bad_json: malformed renovate.json fails closed with exit status 1.
setup_bad_json() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: postgres:18-alpine@sha256:$D"
    track renovate.json '{not valid json'
}

# setup_missing_needs: missing .needs.yaml fails closed with exit status 1.
setup_missing_needs() {
    track a.yml "image: postgres:18-alpine@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_untracked_needs: .needs.yaml on disk but never staged counts as missing (exit status 1).
setup_untracked_needs() {
    printf '%s\n' "repository: github.com/o/r" > .needs.yaml
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_bad_regex: invalid regex in matchDepNames fails closed with exit status 1.
setup_bad_regex() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["/(unclosed/"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_oversized_renovate: renovate.json over 1 MiB fails closed with exit status 1.
setup_oversized_renovate() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: postgres:18-alpine@sha256:$D"
    python3 -I -c 'from pathlib import Path; Path("renovate.json").write_text("{\"a\":\"" + "x" * (1 << 20) + "\"}\n", encoding="utf-8")'
    git add renovate.json
}

run_repo_case null-exact 0 setup_null_exact "image_release_age_check: ok (1 undated images covered)"
run_repo_case null-regex-i 0 setup_null_regex_i "image_release_age_check: ok (1 undated images covered)"
run_repo_case timestamp-optional 0 setup_timestamp_optional "image_release_age_check: ok (1 undated images covered)"
run_repo_case override-hub 0 setup_override_hub "image_release_age_check: ok (1 undated images covered)"
run_repo_case override-template 0 setup_override_template "image_release_age_check: ok (1 undated images covered)"
run_repo_case override-double-brace 0 setup_override_double_brace "image_release_age_check: ok (1 undated images covered)"
run_repo_case own-image 0 setup_own_image "image_release_age_check: ok (1 undated images covered)"
run_repo_case hub-host 0 setup_hub_host "image_release_age_check: ok (0 undated images covered)"
run_repo_case hostless 0 setup_hostless "image_release_age_check: ok (0 undated images covered)"

run_repo_case no-rule 3 setup_no_rule \
    "image_release_age_check: ghcr.io/planted/undated (a.yml:1)" \
    "$ADVICE"
run_repo_case override-undated 3 setup_override_undated \
    "image_release_age_check: ghcr.io/planted/undated (a.yml:1)" \
    "$ADVICE"
run_repo_case extra-matcher-null 3 setup_extra_matcher_null \
    "image_release_age_check: ghcr.io/planted/undated (a.yml:1)" \
    "$ADVICE"
run_repo_case later-wait 3 setup_later_wait \
    "image_release_age_check: ghcr.io/planted/undated (a.yml:1)" \
    "$ADVICE"
run_repo_case extra-matcher-undo 3 setup_extra_matcher_undo \
    "packageRules[1]"
run_repo_case wrong-datasource 3 setup_wrong_datasource \
    "image_release_age_check: ghcr.io/planted/undated (a.yml:1)" \
    "$ADVICE"
run_repo_case glob-null 3 setup_glob_null \
    "image_release_age_check: ghcr.io/planted/undated (a.yml:1)" \
    "$ADVICE"
run_repo_case negation-undo 3 setup_negation_undo \
    "packageRules[1]"
run_repo_case unsupported-template 3 setup_unsupported_template \
    "unsupported template"

run_repo_case bad-json 1 setup_bad_json "image_release_age_check: error:"
run_repo_case missing-needs 1 setup_missing_needs "image_release_age_check: error: .needs.yaml:"
run_repo_case untracked-needs 1 setup_untracked_needs "image_release_age_check: error: .needs.yaml: missing, untracked"
run_repo_case bad-regex 1 setup_bad_regex "image_release_age_check: error: renovate.json: invalid regex"
run_repo_case oversized-renovate 1 setup_oversized_renovate "image_release_age_check: error: renovate.json: exceeds 1048576 bytes"

# Staged renovate.json drops the rule while the working tree keeps it: --index refuses (rc 3),
# working-tree scan passes (rc 0).
# setup_index_drop: stage empty packageRules, then restore the covering rule in the working tree only.
setup_index_drop() {
    setup_null_exact
    git commit -q -m base
    track renovate.json '{"packageRules": []}'
    cat > renovate.json <<'EOF'
{
  "packageRules": [{
    "matchDatasources": ["docker"],
    "matchDepNames": ["ghcr.io/planted/undated"],
    "minimumReleaseAge": null
  }]
}
EOF
}

new_repo "$work/index-drop"
(cd "$work/index-drop" && setup_index_drop)
status=0
out=$(python3 -I "$check" --root "$work/index-drop" --index 2>&1) || status=$?
check_case index-drop/index 3 "$status" "ghcr.io/planted/undated (a.yml:1)" "$out"
status=0
out=$(python3 -I "$check" --root "$work/index-drop" 2>&1) || status=$?
check_case index-drop/worktree 0 "$status" "image_release_age_check: ok (1 undated images covered)" "$out"
echo "ok   index-drop"

# Staged renovate.json and .needs.yaml are deleted from the working tree: --index reads both
# blobs and passes (rc 0), the working-tree scan finds them missing (rc 1).
new_repo "$work/index-only-configs"
(cd "$work/index-only-configs" && setup_null_exact && rm renovate.json .needs.yaml)
status=0
out=$(python3 -I "$check" --root "$work/index-only-configs" --index 2>&1) || status=$?
check_case index-only-configs/index 0 "$status" "image_release_age_check: ok (1 undated images covered)" "$out"
status=0
out=$(python3 -I "$check" --root "$work/index-only-configs" 2>&1) || status=$?
check_case index-only-configs/worktree 1 "$status" "image_release_age_check: error: .needs.yaml: missing" "$out"
echo "ok   index-only-configs"

# CI (.github/workflows/ci.yml) and `make workflows-verify` (Makefile) run both readers: the
# working tree as checked out, and the index (cases index-drop, index-only-configs).
assert_wired image_release_age_check.py .github/workflows/ci.yml Makefile
echo "ok   wiring"
