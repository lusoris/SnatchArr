#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Visual baseline of the public pages (gap audit C15-03). Runs the Playwright project
# "visual" inside the Playwright image pinned in web/e2e/visual-image.env, so the pixels never
# depend on the host's fonts, browsers or GPU. The baseline lives in web/e2e/visual.
#
#   tools/e2e/web_visual.sh            compare the pages with the baseline
#   tools/e2e/web_visual.sh --update   rewrite the baseline; review the image diff before committing
#
# SNATCHARR_VISUAL_PLANT=<case> plants a visual change (web/e2e/visual.test.ts); the run must
# fail. make web-visual runs every planted case through tools/e2e/planted.sh, then the clean run.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
# shellcheck source=web/e2e/visual-image.env
. "$root/web/e2e/visual-image.env"

case "$PLAYWRIGHT_IMAGE" in
mcr.microsoft.com/playwright:v*-noble@sha256:*) ;;
*)
    echo "web-visual: PLAYWRIGHT_IMAGE \"$PLAYWRIGHT_IMAGE\" is not a noble image pinned by version and digest" >&2
    exit 1
    ;;
esac
image_version=${PLAYWRIGHT_IMAGE#*:v}
image_version=${image_version%%-*}
lock_version=$(sed -n "s|^  '@playwright/test@\([0-9.]*\)':.*|\1|p" "$root/web/pnpm-lock.yaml" | head -1)
if [ "$image_version" != "$lock_version" ]; then
    echo "web-visual: image Playwright $image_version differs from @playwright/test $lock_version in web/pnpm-lock.yaml" >&2
    exit 1
fi

update=
mode=ro
case "${1:-}" in
"") ;;
--update)
    update=--update-snapshots
    mode=rw
    ;;
*)
    echo "usage: tools/e2e/web_visual.sh [--update]" >&2
    exit 2
    ;;
esac

results=$root/web/test-results
mkdir -p "$root/web/e2e/visual" "$results"
# The web tree is copied inside the container without node_modules and build output, so the
# host's install is never touched; only the baseline (read-only unless --update) and
# web/test-results (Playwright replaces its visual directory) are mounted writable.
docker run --rm --ipc=host --user "$(id -u):$(id -g)" \
    -e HOME=/tmp -e CI=1 \
    -e SNATCHARR_VISUAL=1 -e SNATCHARR_VISUAL_DIR=/baseline -e SNATCHARR_VISUAL_OUT=/results/visual \
    -e SNATCHARR_VISUAL_PLANT="${SNATCHARR_VISUAL_PLANT:-}" -e UPDATE="$update" \
    -v "$root/web:/src:ro" -v "$root/web/e2e/visual:/baseline:$mode" -v "$results:/results" \
    "$PLAYWRIGHT_IMAGE" bash -euo pipefail -c '
        mkdir /tmp/web
        tar -C /src --exclude=./node_modules --exclude=./build --exclude=./.svelte-kit \
            --exclude=./test-results --exclude=./playwright-report -cf - . | tar -C /tmp/web -xf -
        cd /tmp/web
        # Node 25 and later ship no corepack, so pnpm is installed through npm at the version
        # web/package.json names in packageManager (a "+sha..." suffix is dropped for npm).
        pm=$(node -p "require(\"./package.json\").packageManager")
        npm install --global --prefix /tmp/pm --no-audit --no-fund --loglevel=error "${pm%%+*}"
        export PATH=/tmp/pm/bin:$PATH
        pnpm install --frozen-lockfile --reporter=silent
        pnpm exec playwright test --project visual $UPDATE'
