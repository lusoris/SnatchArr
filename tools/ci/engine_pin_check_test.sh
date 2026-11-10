#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for engine_pin_check.py: each case builds a throwaway repository with a CI
# workflow pin and other files that name a Praetor ref, then checks exit status and output.
# shellcheck disable=SC2016 # Fixture variables must remain literal shell source.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
check=$HERE/engine_pin_check.py
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# shellcheck source=tools/ci/test_lib.sh
. "$HERE/test_lib.sh"

PIN=4e6c6d9cbf9cc8e634f2c9e4751706496278cd52
OLD=21bf5366356b29878cd1bbdd43168bc9fa421707
INSTALL=github.com/cordanaLLM/praetor/cmd/standardsctl

# workflow VALUE...: track .github/workflows/ci.yml declaring one PRAETOR_REF line per VALUE.
workflow() {
    mkdir -p .github/workflows
    printf '%s\n' 'name: CI' 'jobs:' '  standards:' '    steps:' '      - env:' > .github/workflows/ci.yml
    for value in "$@"; do
        printf '          PRAETOR_REF: %s\n' "$value" >> .github/workflows/ci.yml
    done
    printf '%s\n' "        run: go install \"$INSTALL@\${PRAETOR_REF}\"" >> .github/workflows/ci.yml
    git add .github/workflows/ci.yml
}

# workflow_commit VALUE: track .github/workflows/ci.yml declaring only PRAETOR_COMMIT.
workflow_commit() {
    mkdir -p .github/workflows
    printf '%s\n' 'name: CI' 'jobs:' '  standards:' '    steps:' '      - env:' > .github/workflows/ci.yml
    printf '          PRAETOR_COMMIT: %s\n' "$1" >> .github/workflows/ci.yml
    printf '%s\n' "        run: go install \"$INSTALL@\${PRAETOR_COMMIT}\"" >> .github/workflows/ci.yml
    git add .github/workflows/ci.yml
}

# devcontainer LINE: track .devcontainer/post-create.sh with LINE on physical line 2.
devcontainer() {
    mkdir -p .devcontainer
    printf '%s\n' '#!/usr/bin/env bash' "$1" > .devcontainer/post-create.sh
    git add .devcontainer/post-create.sh
}

# expect NAME WANT_STATUS NEEDLE SETUP: plant a fixture with SETUP and check the output.
expect() {
    local name=$1 want=$2 needle=$3 setup=$4 repo out status=0
    repo="$work/$name"
    new_repo "$repo"
    (cd "$repo" && "$setup")
    out=$(python3 -I "$check" --root "$repo" 2>&1) || status=$?
    check_case "$name" "$want" "$status" "$needle" "$out"
    echo "ok   $name"
}

# setup_match: the devcontainer installs the CI pin.
setup_match() { workflow "$PIN"; devcontainer "go install $INSTALL@$PIN"; }

# setup_drift: the devcontainer still installs the engine before the last re-adopt.
setup_drift() { workflow "$PIN"; devcontainer "go install $INSTALL@$OLD"; }

# setup_short: a short commit id of the pin is a different ref, not the pin.
setup_short() { workflow "$PIN"; devcontainer "go install $INSTALL@${PIN:0:12}"; }

# setup_branch: a branch name moves, so it never equals the pin.
setup_branch() { workflow "$PIN"; devcontainer "go install $INSTALL@main"; }

# setup_variable: a ref read from a variable is no literal ref.
setup_variable() { workflow "$PIN"; devcontainer 'go install "$INSTALL@${PRAETOR_REF}"'; }

# setup_escaped: a variable escaped inside a quoted fixture is no literal ref either.
setup_escaped() { workflow "$PIN"; devcontainer 'plant "go install \"$INSTALL@\${PRAETOR_REF}\""'; }

# setup_source: the check's own pattern, tracked in the repository it reads, is no ref.
setup_source() { workflow "$PIN"; cp "$check" engine_pin_check.py; git add engine_pin_check.py; }

# setup_tag: a release tag is refused even when every literal ref matches.
setup_tag() { workflow v1.2.3; devcontainer "go install $INSTALL@v1.2.3"; }

# setup_short_pin: a short pin is refused even when every literal ref matches.
setup_short_pin() { workflow "${PIN:0:12}"; devcontainer "go install $INSTALL@${PIN:0:12}"; }

# setup_branch_pin: a branch pin is refused even when every literal ref matches.
setup_branch_pin() { workflow main; devcontainer "go install $INSTALL@main"; }

# setup_upper_pin: an uppercase commit id is refused as a pin.
setup_upper_pin() { workflow "${PIN^^}"; }

# setup_commit_short: a short PRAETOR_COMMIT is refused rather than silently dropped.
setup_commit_short() { workflow_commit "${PIN:0:12}"; }

# setup_commit_full: a full PRAETOR_COMMIT and matching literal ref are accepted.
setup_commit_full() { workflow_commit "$PIN"; devcontainer "go install $INSTALL@$PIN"; }

# setup_placeholder: a comment placeholder such as <ref> is no literal ref.
setup_placeholder() { workflow "$PIN"; devcontainer '# uses: cordanaLLM/praetor/.github/workflows/<workflow>@<ref>'; }

# setup_quoted: a quoted pin with a trailing comment declares the same pin.
setup_quoted() { workflow "\"$PIN\" # engine pin"; devcontainer "go install $INSTALL@$PIN"; }

# setup_case: the owner is matched without regard to case.
setup_case() { workflow "$PIN"; devcontainer "go install github.com/cordanallm/praetor/cmd/standardsctl@$OLD"; }

# setup_uses: a reusable-workflow ref in another workflow must equal the pin too.
setup_uses() {
    workflow "$PIN"
    printf '%s\n' 'jobs:' '  adopt:' "    uses: cordanaLLM/praetor/.github/workflows/adopt.yml@$OLD" > .github/workflows/adopt.yml
    git add .github/workflows/adopt.yml
}

# setup_untracked: a file git does not track is not read.
setup_untracked() { workflow "$PIN"; mkdir -p scratch; printf '%s\n' "go install $INSTALL@$OLD" > scratch/notes.sh; }

# setup_no_pin: no workflow declares PRAETOR_REF.
setup_no_pin() { devcontainer "go install $INSTALL@$PIN"; }

# setup_two_pins: two different PRAETOR_REF values pin no engine.
setup_two_pins() { workflow "$PIN" "$OLD"; devcontainer "go install $INSTALL@$PIN"; }

# setup_same_twice: the same value declared twice is still one pin.
setup_same_twice() { workflow "$PIN" "$PIN"; devcontainer "go install $INSTALL@$PIN"; }

expect match 0 "ok: every literal Praetor ref equals the engine pin $PIN (.github/workflows/ci.yml:6)" setup_match
expect drift 3 "refused .devcontainer/post-create.sh:2: Praetor ref $OLD differs from the engine pin $PIN" setup_drift
expect short 3 "refused .devcontainer/post-create.sh:2: Praetor ref ${PIN:0:12} differs" setup_short
expect branch 3 "refused .devcontainer/post-create.sh:2: Praetor ref main differs" setup_branch
expect variable 0 "ok: every literal Praetor ref" setup_variable
expect escaped 0 "ok: every literal Praetor ref" setup_escaped
expect source 0 "ok: every literal Praetor ref" setup_source
expect tag 3 "refused: engine pin v1.2.3 (.github/workflows/ci.yml:6) is not a full 40-character lowercase commit id" setup_tag
expect short-pin 3 "is not a full 40-character lowercase commit id" setup_short_pin
expect branch-pin 3 "is not a full 40-character lowercase commit id" setup_branch_pin
expect upper-pin 3 "is not a full 40-character lowercase commit id" setup_upper_pin
expect commit-short 3 "is not a full 40-character lowercase commit id" setup_commit_short
expect commit-full 0 "ok: every literal Praetor ref equals the engine pin $PIN" setup_commit_full
expect placeholder 0 "ok: every literal Praetor ref" setup_placeholder
expect quoted 0 "ok: every literal Praetor ref equals the engine pin $PIN" setup_quoted
expect case 3 "refused .devcontainer/post-create.sh:2: Praetor ref $OLD differs" setup_case
expect uses 3 "refused .github/workflows/adopt.yml:3: Praetor ref $OLD differs" setup_uses
expect untracked 0 "ok: every literal Praetor ref" setup_untracked
expect no-pin 3 "refused: want exactly one PRAETOR_REF value under .github/workflows/, found: none" setup_no_pin
expect two-pins 3 "refused: want exactly one PRAETOR_REF value" setup_two_pins
expect same-twice 0 "ok: every literal Praetor ref equals the engine pin $PIN" setup_same_twice

# Not a repository: git ls-files fails, and the check fails closed with status 1.
mkdir -p "$work/plain"
status=0
out=$(GIT_CEILING_DIRECTORIES=$work python3 -I "$check" --root "$work/plain" 2>&1) || status=$?
check_case not-a-repository 1 "$status" "error: git ls-files failed" "$out"
echo "ok   not-a-repository"
