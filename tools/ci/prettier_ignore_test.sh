#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Cases for web/.prettierignore (gap audit C11-05). inlang writes its cache, .meta.json and
# README.md into web/project.inlang on a local build; `prettier --check .` then failed on files
# nobody tracks (#51). Ignored: those files and the other generated outputs. Checked: the tracked
# project.inlang/settings.json and the sources.
#
#   tools/ci/prettier_ignore_test.sh [IGNORE_FILE]
#
# Run from web/ after pnpm install. IGNORE_FILE (default: .prettierignore) lets a mutated copy
# be replayed; it must sit in web/, since prettier reads its patterns relative to the file's
# folder. Exit status: 0 every path behaved as recorded, 1 otherwise.
set -eu

ignore="${1:-.prettierignore}"
failed=0

# expect WANT PATH: prettier --file-info reports PATH as ignored (true) or checked (false).
expect() {
  info=$(./node_modules/.bin/prettier --ignore-path "$ignore" --file-info "$2")
  case "$info" in
  *"\"ignored\": $1"*) echo "ok   $2: ignored $1" ;;
  *)
    echo "FAIL $2: want ignored $1, got $info" >&2
    failed=1
    ;;
  esac
}

expect true project.inlang/README.md
expect true project.inlang/.meta.json
expect true project.inlang/cache/plugins/plugin.js
expect true src/lib/paraglide/messages.js
expect true src/lib/api/schema.d.ts
expect true pnpm-lock.yaml
expect false project.inlang/settings.json
expect false src/routes/+page.svelte
expect false src/lib/api/client.ts

exit "$failed"
