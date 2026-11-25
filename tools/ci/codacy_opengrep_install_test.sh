#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Offline planted cases for codacy_opengrep_install.sh: install outside tools/, propagate npm
# failure, replace stale manifests, and make the scanner resolve the CLI from the cache.
set -eu

LC_ALL=C
export LC_ALL

HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
SOURCE_ROOT=$(CDPATH='' cd -- "$HERE/../.." && pwd)
INSTALLER="$HERE/codacy_opengrep_install.sh"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT HUP INT TERM

# fail CASE DETAIL: report one failed planted case and stop.
fail() {
  printf 'FAIL %s: %s\n' "$1" "$2" >&2
  exit 1
}

# make_root ROOT: copy the tracked npm manifests and scanner into a fixture repository.
make_root() {
  mkdir -p "$1/tools/ci/codacy_opengrep" "$1/tools/ci" "$1/bin"
  cp "$SOURCE_ROOT/tools/ci/codacy_opengrep/package.json" \
    "$SOURCE_ROOT/tools/ci/codacy_opengrep/package-lock.json" \
    "$1/tools/ci/codacy_opengrep/"
  cp "$SOURCE_ROOT/tools/ci/codacy_opengrep_scan.py" "$1/tools/ci/"
}

# make_stub FILE: write an npm stub that records its cwd and arguments, then creates node_modules.
make_stub() {
  cat >"$1" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$PWD" >"$STUB_PWD"
printf '%s\n' "$@" >"$STUB_ARGS"
if [ "${STUB_EXIT:-0}" -ne 0 ]; then
  exit "$STUB_EXIT"
fi
mkdir -p node_modules
EOF
  chmod +x "$1"
}

# run_install ROOT EXIT: run the installer with the fixture npm first on PATH.
run_install() {
  STUB_PWD="$1/npm.pwd" STUB_ARGS="$1/npm.args" STUB_EXIT="$2" \
    PATH="$1/bin:$PATH" sh "$INSTALLER" "$1"
}

P1="$WORK/p1"
make_root "$P1"
make_stub "$P1/bin/npm"
run_install "$P1" 0
printf 'ci\n--ignore-scripts\n--no-audit\n' >"$P1/want.args"
cmp -s "$P1/want.args" "$P1/npm.args" || fail P1 "npm arguments differ"
[ "$(sed -n '1p' "$P1/npm.pwd")" = "$P1/.cache/codacy_opengrep" ] || fail P1 "npm cwd is not the cache directory"
[ -d "$P1/.cache/codacy_opengrep/node_modules" ] || fail P1 "cache node_modules missing"
TOOLS_MODULES=$(find "$P1/tools" -maxdepth 8 -type d -name node_modules -print)
[ -z "$TOOLS_MODULES" ] || fail P1 "node_modules created under tools: $TOOLS_MODULES"
printf 'ok   P1 installs with safe flags only under the root cache\n'

P2="$WORK/p2"
make_root "$P2"
make_stub "$P2/bin/npm"
P2_RC=0
if run_install "$P2" 7 >"$P2/out" 2>"$P2/err"; then
  fail P2 "npm failure was accepted"
else
  P2_RC=$?
fi
[ "$P2_RC" -eq 7 ] || fail P2 "exit $P2_RC, want 7"
grep -Fq 'codacy-opengrep install: npm ci failed' "$P2/err" || fail P2 "stderr does not name the npm step"
printf 'ok   P2 propagates npm failure and names the step\n'

P3="$WORK/p3"
make_root "$P3"
make_stub "$P3/bin/npm"
mkdir -p "$P3/.cache/codacy_opengrep"
printf '%s\n' stale >"$P3/.cache/codacy_opengrep/package.json"
printf '%s\n' stale >"$P3/.cache/codacy_opengrep/package-lock.json"
run_install "$P3" 0
cmp -s "$P3/tools/ci/codacy_opengrep/package.json" "$P3/.cache/codacy_opengrep/package.json" || fail P3 "package.json stayed stale"
cmp -s "$P3/tools/ci/codacy_opengrep/package-lock.json" "$P3/.cache/codacy_opengrep/package-lock.json" || fail P3 "package-lock.json stayed stale"
printf 'ok   P3 replaces both stale cache manifests\n'

mkdir -p "$P1/.cache/codacy_opengrep/node_modules/@codacy/analysis-cli" "$P1/.cache/codacy_opengrep/node_modules/.bin"
python3 -I -B - "$P1" <<'PY'
import importlib.util
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
tracked = json.loads((root / "tools/ci/codacy_opengrep/package.json").read_text(encoding="utf-8"))
installed = root / ".cache/codacy_opengrep/node_modules/@codacy/analysis-cli/package.json"
installed.write_text(json.dumps({"version": tracked["dependencies"]["@codacy/analysis-cli"]}), encoding="utf-8")
(root / ".cache/codacy_opengrep/node_modules/.bin/codacy-analysis").touch()
spec = importlib.util.spec_from_file_location("codacy_opengrep_scan", root / "tools/ci/codacy_opengrep_scan.py")
if spec is None or spec.loader is None:
    raise SystemExit("cannot load scanner fixture")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
want = root / ".cache/codacy_opengrep/node_modules/.bin/codacy-analysis"
if module.cli_path(root) != want:
    raise SystemExit(f"cli_path did not resolve cache CLI: {module.cli_path(root)}")
PY
printf 'ok   P4 scanner resolves the CLI under the root cache\n'

command -v node >/dev/null 2>&1 || fail P5 "node not on PATH"
python3 -I -B - "$P1" <<'PY'
import importlib.util
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
tracked = json.loads((root / "tools/ci/codacy_opengrep/package.json").read_text(encoding="utf-8"))
version = tracked["dependencies"]["@codacy/analysis-cli"]
adapter = root / ".cache/codacy_opengrep/node_modules/@codacy/analysis-adapters"
adapter.mkdir(parents=True)
(adapter / "package.json").write_text(
    json.dumps({"name": "@codacy/analysis-adapters", "main": "index.js"}), encoding="utf-8"
)
entries = [{"descriptor": {"id": "Semgrep", "preferredVersion": version}}]
(adapter / "index.js").write_text(
    f"exports.builtinAdapterEntries = {json.dumps(entries)};", encoding="utf-8"
)
spec = importlib.util.spec_from_file_location("codacy_opengrep_scan", root / "tools/ci/codacy_opengrep_scan.py")
if spec is None or spec.loader is None:
    raise SystemExit("FAIL P5: cannot load scanner fixture")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
try:
    module.check_cli_opengrep(root, version)
except module.ScanError as err:
    raise SystemExit(f"FAIL P5: {err}") from err
PY
printf 'ok   P5 scanner reads the adapter registry under the root cache\n'

printf 'codacy-opengrep install: all planted cases passed\n'
