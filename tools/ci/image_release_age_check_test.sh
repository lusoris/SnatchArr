#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for image_release_age_check.py: each case stages a throwaway git repository with
# renovate.json and one container image digest pin, then checks the exit status and diagnostic
# output. Digests are constructed at run time so this file holds no literal pin.
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
    track a.yml "image: lscr.io/linuxserver/ffmpeg:9.0@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["lscr.io/linuxserver/ffmpeg"],
        "overridePackageName": "linuxserver/ffmpeg"
      }]
    }'
}

# setup_null_override_noop: a later overridePackageName null keeps the earlier hub packageName.
setup_null_override_noop() {
    track a.yml "image: lscr.io/linuxserver/ffmpeg:9.0@sha256:$D"
    track renovate.json '{
      "packageRules": [
        {
          "matchDatasources": ["docker"],
          "matchDepNames": ["lscr.io/linuxserver/ffmpeg"],
          "overridePackageName": "linuxserver/ffmpeg"
        },
        {
          "matchDatasources": ["docker"],
          "matchDepNames": ["lscr.io/linuxserver/ffmpeg"],
          "overridePackageName": null
        }
      ]
    }'
}

# setup_registry_aliases: a manager-level registryAliases rewrite is refused (rc 3).
setup_registry_aliases() {
    track a.yml "image: postgres:18-alpine@sha256:$D"
    track renovate.json '{
      "dockerfile": {"registryAliases": {"docker.io": "mirror.gcr.io"}},
      "packageRules": []
    }'
}

# setup_override_template: replace template in overridePackageName strips mirror.gcr.io/ to hub name.
setup_override_template() {
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
    track a.yml "image: mirror.gcr.io/library/postgres:18-alpine@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchPackageNames": ["mirror.gcr.io/library/postgres"],
        "overridePackageName": "{{replace '\''^mirror\\.gcr\\.io/'\'' '\'''\'' depName}}"
      }]
    }'
}

# setup_own_image: .needs.yaml owner does not exempt ghcr.io/<owner>/ without a renovate.json rule.
setup_own_image() {
    track .needs.yaml "repository: github.com/o/r"
    track a.yml "image: ghcr.io/o/x:1.0@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_hub_host: docker.io host is dated on Docker Hub and needs no rule.
setup_hub_host() {
    track a.yml "image: docker.io/linuxserver/deluge:2.2.0@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_hostless: host-less Docker Hub reference is dated and needs no rule.
setup_hostless() {
    track a.yml "image: postgres:18-alpine@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_exact_nocase: exact matchDepNames is case-insensitive per Renovate minimatch nocase.
setup_exact_nocase() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["GHCR.IO/PLANTED/UNDATED"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_datasource_regex_neg: matchDatasources supports regex and ! negation via shared match_list.
setup_datasource_regex_neg() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["/^dock/", "!npm"],
        "matchDepNames": ["ghcr.io/planted/undated"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_matcher_free_null: a rule with no matcher keys applies definitely to every dependency.
setup_matcher_free_null() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "minimumReleaseAge": null
      }]
    }'
}

# setup_accumulated_pkg_name: later matchPackageNames matches packageName overridden by an earlier rule.
setup_accumulated_pkg_name() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [
        {
          "matchDatasources": ["docker"],
          "matchDepNames": ["ghcr.io/planted/undated"],
          "overridePackageName": "ghcr.io/x/step"
        },
        {
          "matchDatasources": ["docker"],
          "matchPackageNames": ["ghcr.io/x/step"],
          "minimumReleaseAge": null
        }
      ]
    }'
}

# setup_unmodeled_then_definite: a later definite covering rule resolves an earlier unmodeled wait rule.
setup_unmodeled_then_definite() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [
        {
          "matchDatasources": ["docker"],
          "matchFileNames": ["a.yml"],
          "minimumReleaseAge": "3 days"
        },
        {
          "matchDatasources": ["docker"],
          "matchDepNames": ["ghcr.io/planted/undated"],
          "minimumReleaseAge": null
        }
      ]
    }'
}

# setup_top_level_age_null: top-level minimumReleaseAge null seeds the initial state as covered.
setup_top_level_age_null() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "minimumReleaseAge": null,
      "packageRules": []
    }'
}

# setup_no_rule: undated pin with no matching rule is refused with file:line and advice.
setup_no_rule() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_override_undated: overridePackageName pointing to an undated host does not cover the pin.
setup_override_undated() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["ghcr.io/planted/undated"],
        "overridePackageName": "ghcr.io/x/y"
      }]
    }'
}

# setup_extra_matcher_null: rule with matchFileNames holding null forks coverage and fails closed.
setup_extra_matcher_null() {
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
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["npm"],
        "matchDepNames": ["ghcr.io/planted/undated"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_glob_null: glob pattern in matchDepNames with null forks coverage and fails closed naming rule index.
setup_glob_null() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["ghcr.io/planted/*"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_negation_undo: exact ! negation matching the dependency definitely undoes coverage.
setup_negation_undo() {
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

# setup_negation_glob_undo: negated glob with non-null minimumReleaseAge forks and names rule index.
setup_negation_glob_undo() {
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
          "matchDepNames": ["!ghcr.io/other/*"],
          "minimumReleaseAge": "3 days"
        }
      ]
    }'
}

# setup_override_dep_name: rule holding overrideDepName that may apply fails closed naming rule index.
setup_override_dep_name() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [
        {
          "matchDatasources": ["docker"],
          "matchDepNames": ["ghcr.io/planted/undated"],
          "overrideDepName": "postgres"
        },
        {
          "matchDatasources": ["docker"],
          "matchDepNames": ["ghcr.io/planted/undated"],
          "minimumReleaseAge": null
        }
      ]
    }'
}

# setup_unsupported_template: any template other than replace '<re>' '<s>' depName is refused.
setup_unsupported_template() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["ghcr.io/planted/undated"],
        "overridePackageName": "{{{depName}}}"
      }]
    }'
}

# setup_replacement_dollar: JS $ replacement token in replace template is refused.
setup_replacement_dollar() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json "{
      \"packageRules\": [{
        \"matchDatasources\": [\"docker\"],
        \"matchDepNames\": [\"ghcr.io/planted/undated\"],
        \"overridePackageName\": \"{{{replace '^(ghcr\\\\.io)/' '\$1/' depName}}}\"
      }]
    }"
}

# setup_replacement_backslash: Python \ replacement token in replace template is refused.
setup_replacement_backslash() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["ghcr.io/planted/undated"],
        "overridePackageName": "{{{replace '\''^ghcr\\.io/(.*)'\'' '\''\\1'\'' depName}}}"
      }]
    }'
}

# setup_ipv6_host_no_rule: bracketed IPv6 literal host with port is extracted and refused when uncovered.
setup_ipv6_host_no_rule() {
    track a.yml "image: [2001:db8::1]:5000/team/app:1@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_uppercase_host_no_rule: mixed-case domain host is extracted and refused when uncovered.
setup_uppercase_host_no_rule() {
    track a.yml "image: Registry.Example.com/team/app:1@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_uppercase_hub_no_rule: uppercase Docker.io is case-sensitively undated and refused when uncovered.
setup_uppercase_hub_no_rule() {
    track a.yml "image: Docker.io/linuxserver/deluge:2.2.0@sha256:$D"
    track renovate.json '{"packageRules": []}'
}

# setup_bad_json: malformed renovate.json fails closed with exit status 1.
setup_bad_json() {
    track a.yml "image: postgres:18-alpine@sha256:$D"
    track renovate.json '{not valid json'
}

# setup_missing_renovate: missing renovate.json fails closed with exit status 1.
setup_missing_renovate() {
    track a.yml "image: postgres:18-alpine@sha256:$D"
}

# setup_untracked_renovate: renovate.json on disk but never staged counts as missing (exit status 1).
setup_untracked_renovate() {
    printf '%s\n' '{"packageRules": []}' > renovate.json
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
}

# setup_bad_regex: invalid regex in matchDepNames fails closed with exit status 1.
setup_bad_regex() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["/(unclosed/"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_re2_lookahead: lookahead regex rejected by Renovate RE2 fails closed with exit status 1.
setup_re2_lookahead() {
    track a.yml "image: ghcr.io/planted/undated:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["/^ghcr\\.io/(?=planted)/"],
        "minimumReleaseAge": null
      }]
    }'
}

# setup_oversized_renovate: renovate.json over 1 MiB fails closed with exit status 1.
setup_oversized_renovate() {
    track a.yml "image: postgres:18-alpine@sha256:$D"
    python3 -I -c 'from pathlib import Path; Path("renovate.json").write_text("{\"a\":\"" + "x" * (1 << 20) + "\"}\n", encoding="utf-8")'
    git add renovate.json
}

# setup_redos_deadline: catastrophic backtracking regex is stopped by REGEX_DEADLINE in child --judge.
setup_redos_deadline() {
    local many_a
    many_a=$(printf '%030d' 0 | tr 0 a)
    track a.yml "image: ghcr.io/x/${many_a}b:1@sha256:$D"
    track renovate.json '{
      "packageRules": [{
        "matchDatasources": ["docker"],
        "matchDepNames": ["/^ghcr\\.io\\/x\\/(a+)+$/"],
        "minimumReleaseAge": null
      }]
    }'
}

run_repo_case null-exact 0 setup_null_exact "image_release_age_check: ok (1 undated images covered)"
run_repo_case null-regex-i 0 setup_null_regex_i "image_release_age_check: ok (1 undated images covered)"
run_repo_case timestamp-optional 0 setup_timestamp_optional "image_release_age_check: ok (1 undated images covered)"
run_repo_case override-hub 0 setup_override_hub "image_release_age_check: ok (1 undated images covered)"
run_repo_case null-override-noop 0 setup_null_override_noop "image_release_age_check: ok (1 undated images covered)"
run_repo_case override-template 0 setup_override_template "image_release_age_check: ok (1 undated images covered)"
run_repo_case override-double-brace 0 setup_override_double_brace "image_release_age_check: ok (1 undated images covered)"
run_repo_case hub-host 0 setup_hub_host "image_release_age_check: ok (0 undated images covered)"
run_repo_case hostless 0 setup_hostless "image_release_age_check: ok (0 undated images covered)"
run_repo_case exact-nocase 0 setup_exact_nocase "image_release_age_check: ok (1 undated images covered)"
run_repo_case datasource-regex-neg 0 setup_datasource_regex_neg "image_release_age_check: ok (1 undated images covered)"
run_repo_case matcher-free-null 0 setup_matcher_free_null "image_release_age_check: ok (1 undated images covered)"
run_repo_case accumulated-pkg-name 0 setup_accumulated_pkg_name "image_release_age_check: ok (1 undated images covered)"
run_repo_case unmodeled-then-definite 0 setup_unmodeled_then_definite "image_release_age_check: ok (1 undated images covered)"
run_repo_case top-level-age-null 0 setup_top_level_age_null "image_release_age_check: ok (1 undated images covered)"

run_repo_case own-image 3 setup_own_image \
    "image_release_age_check: ghcr.io/o/x (a.yml:1)" \
    "$ADVICE"
run_repo_case no-rule 3 setup_no_rule \
    "image_release_age_check: ghcr.io/planted/undated (a.yml:1)" \
    "$ADVICE"
run_repo_case override-undated 3 setup_override_undated \
    "image_release_age_check: ghcr.io/planted/undated (a.yml:1)" \
    "$ADVICE"
run_repo_case extra-matcher-null 3 setup_extra_matcher_null \
    "packageRules[0]"
run_repo_case later-wait 3 setup_later_wait \
    "image_release_age_check: ghcr.io/planted/undated (a.yml:1)" \
    "$ADVICE"
run_repo_case extra-matcher-undo 3 setup_extra_matcher_undo \
    "packageRules[1]"
run_repo_case wrong-datasource 3 setup_wrong_datasource \
    "image_release_age_check: ghcr.io/planted/undated (a.yml:1)" \
    "$ADVICE"
run_repo_case glob-null 3 setup_glob_null \
    "packageRules[0]"
run_repo_case negation-undo 3 setup_negation_undo \
    "image_release_age_check: ghcr.io/planted/undated (a.yml:1)" \
    "$ADVICE"
run_repo_case negation-glob-undo 3 setup_negation_glob_undo \
    "packageRules[1]: ambiguous rule applicability"
run_repo_case override-dep-name 3 setup_override_dep_name \
    "packageRules[0]"
run_repo_case registry-aliases 3 setup_registry_aliases \
    "registryAliases is not modeled"
run_repo_case unsupported-template 3 setup_unsupported_template \
    "unsupported template"
run_repo_case replacement-dollar 3 setup_replacement_dollar \
    "unsupported template"
run_repo_case replacement-backslash 3 setup_replacement_backslash \
    "unsupported template"
run_repo_case ipv6-host-no-rule 3 setup_ipv6_host_no_rule \
    "image_release_age_check: [2001:db8::1]:5000/team/app (a.yml:1)" \
    "$ADVICE"
run_repo_case uppercase-host-no-rule 3 setup_uppercase_host_no_rule \
    "image_release_age_check: Registry.Example.com/team/app (a.yml:1)" \
    "$ADVICE"
run_repo_case uppercase-hub-no-rule 3 setup_uppercase_hub_no_rule \
    "image_release_age_check: Docker.io/linuxserver/deluge (a.yml:1)" \
    "$ADVICE"

run_repo_case bad-json 1 setup_bad_json "image_release_age_check: error:"
run_repo_case missing-renovate 1 setup_missing_renovate "image_release_age_check: error: renovate.json:"
run_repo_case untracked-renovate 1 setup_untracked_renovate "image_release_age_check: error: renovate.json: missing, untracked"
run_repo_case bad-regex 1 setup_bad_regex "image_release_age_check: error: renovate.json: invalid regex"
run_repo_case re2-lookahead 1 setup_re2_lookahead '/^ghcr\\.io/(?=planted)/'
run_repo_case oversized-renovate 1 setup_oversized_renovate "image_release_age_check: error: renovate.json: exceeds 1048576 bytes"

new_repo "$work/redos-deadline"
(cd "$work/redos-deadline" && setup_redos_deadline)
status=0
out=$(run_bounded 10 python3 -I -c 'import sys; sys.path.insert(0, sys.argv[1]); import image_release_age_check as m; m.REGEX_DEADLINE = 2; sys.exit(m.main(sys.argv[2:]))' "$HERE" --root "$work/redos-deadline" 2>&1) || status=$?
check_case redos-deadline 1 "$status" "error: rule evaluation exceeded 2 s (a pattern backtracks)" "$out"
echo "ok   redos-deadline"

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

# Staged renovate.json is deleted from the working tree: --index reads the staged blob and passes
# (rc 0), the working-tree scan finds it missing (rc 1).
new_repo "$work/index-only-configs"
(cd "$work/index-only-configs" && setup_null_exact && rm renovate.json)
status=0
out=$(python3 -I "$check" --root "$work/index-only-configs" --index 2>&1) || status=$?
check_case index-only-configs/index 0 "$status" "image_release_age_check: ok (1 undated images covered)" "$out"
status=0
out=$(python3 -I "$check" --root "$work/index-only-configs" 2>&1) || status=$?
check_case index-only-configs/worktree 1 "$status" "image_release_age_check: error: renovate.json: missing" "$out"
echo "ok   index-only-configs"

python3 -I "$HERE/renovate_rules.py" --self-test

# CI (.github/workflows/ci.yml) and `make workflows-verify` (Makefile) run both readers: the
# working tree as checked out, and the index (cases index-drop, index-only-configs).
assert_wired image_release_age_check.py .github/workflows/ci.yml Makefile
mkdir -p "$work/wired-commented/tools/ci"
printf '%s\n' 'workflows-verify:' '	# python3 -I tools/ci/image_release_age_check.py' '	# python3 -I tools/ci/image_release_age_check.py --index' > "$work/wired-commented/Makefile"
status=0
out=$(sh -c '. "$1"; HERE="$2"; assert_wired "$3" Makefile' _ "$HERE/test_lib.sh" "$work/wired-commented/tools/ci" image_release_age_check.py 2>&1) || status=$?
check_case wiring-commented 1 "$status" "FAIL wiring: Makefile runs no 'python3 -I tools/ci/image_release_age_check.py'" "$out"
echo "ok   wiring"
