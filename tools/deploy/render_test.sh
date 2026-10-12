#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Planted cases for render.sh, with a stub helm. Refused: a render that fails (the case the old
# pipe into kubeconform passed), a render that prints nothing, a render without a resource, and
# a call without a chart. Accepted: a render with a resource.
#
#   tools/deploy/render_test.sh
#
# Exit status: 0 every case behaved as recorded, 1 otherwise.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
failed=0

# stub NAME BODY: a helm that runs BODY.
stub() {
  printf '#!/bin/sh\n%s\n' "$2" >"$WORK/$1"
  chmod +x "$WORK/$1"
}
# Prints a resource and then fails, so that only the exit status tells this render from a
# good one.
stub fails 'printf "%s\n" "kind: ConfigMap"; echo "Error: execution error: planted" >&2; exit 1'
stub empty 'exit 0'
stub nokind 'echo "# only a comment"'
stub fine 'printf "%s\n" "---" "apiVersion: v1" "kind: ConfigMap" "metadata:" "  name: x"'

# expect WANT STUB [args...]: render.sh with that stub exits WANT.
expect() {
  want="$1"
  name="$2"
  shift 2
  got=0
  HELM="$WORK/$name" sh "$HERE/render.sh" "$@" 2>"$WORK/err" || got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: exit $got, want $want" >&2
    sed 's/^/  /' "$WORK/err" >&2
    failed=1
    return 0
  fi
  echo "ok   $name: exit $got"
}

expect 1 fails "$WORK/out" rel chart
if [ -e "$WORK/out" ]; then
  echo "FAIL fails: a failed render left $WORK/out behind" >&2
  failed=1
fi
expect 1 empty "$WORK/out" rel chart
expect 1 nokind "$WORK/out" rel chart
expect 2 fine "$WORK/out" rel
expect 0 fine "$WORK/out" rel chart
if ! grep -q '^kind: ConfigMap$' "$WORK/out"; then
  echo "FAIL fine: the render is not in $WORK/out" >&2
  failed=1
fi
exit "$failed"
