#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Offline planted cases for codacy_opengrep_install.sh: install outside tools/, propagate npm
# failure, replace stale manifests, make the scanner resolve the CLI and adapter registry from
# the cache, enforce the npm ci deadline and its CODACY_OPENGREP_NPM_DEADLINE_S bounds, and
# verify Go's ./tools/... walk skips the install directory while listing the pre-fix path.
# Keeping node_modules out of Go's ./tools/... walk prevents go test ./tools/... from racing a concurrent npm ci rebuild (#1325).
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

# make_stub FILE: write an npm stub that records its cwd and arguments, optionally sleeps or fails, then creates node_modules.
make_stub() {
  cat >"$1" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$PWD" >"$STUB_PWD"
printf '%s\n' "$@" >"$STUB_ARGS"
if [ -n "${STUB_SLEEP:-}" ]; then
  if [ "${STUB_IGNORE_TERM:-0}" -eq 1 ]; then trap '' TERM; fi
  exec sleep "$STUB_SLEEP"
fi
if [ "${STUB_EXIT:-0}" -ne 0 ]; then
  exit "$STUB_EXIT"
fi
mkdir -p node_modules
EOF
  chmod +x "$1"
}

# run_install ROOT EXIT [ENV=VALUE...]: run the installer with the fixture npm first on PATH and optional env overrides.
run_install() {
  ri_root=$1
  ri_exit=$2
  shift 2
  env STUB_PWD="$ri_root/npm.pwd" STUB_ARGS="$ri_root/npm.args" STUB_EXIT="$ri_exit" \
    PATH="$ri_root/bin:$PATH" "$@" sh "$INSTALLER" "$ri_root"
}

# go_list_dirs ROOT ERR_FILE: run an isolated offline go list over ./tools/... in ROOT and print package directories.
go_list_dirs() {
  (
    CDPATH='' cd -- "$1" &&
      GOWORK=off GOFLAGS='' GOTOOLCHAIN=local GOPROXY=off \
        go list -f '{{.Dir}}' ./tools/... 2>"$2"
  )
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

P6="$WORK/p6"
make_root "$P6"
make_stub "$P6/bin/npm"
P6_START=$(date +%s)
P6_RC=0
if run_install "$P6" 0 STUB_SLEEP=30 CODACY_OPENGREP_NPM_DEADLINE_S=1 >"$P6/out" 2>"$P6/err"; then
  fail P6 "timed-out npm install was accepted"
else
  P6_RC=$?
fi
P6_ELAPSED=$(( $(date +%s) - P6_START ))
[ "$P6_RC" -eq 124 ] || fail P6 "exit $P6_RC, want 124"
[ "$P6_ELAPSED" -le 10 ] || fail P6 "elapsed ${P6_ELAPSED}s exceeds 10s"
grep -Fq '1 s' "$P6/err" || fail P6 "stderr does not name the deadline in seconds: $(cat "$P6/err")"
printf 'ok   P6 enforces the npm ci deadline and exits 124\n'

P6_START=$(date +%s)
P6_RC=0
run_install "$P6" 0 STUB_IGNORE_TERM=1 STUB_SLEEP=8 CODACY_OPENGREP_NPM_DEADLINE_S=1 >"$P6/out" 2>"$P6/err" || P6_RC=$?
[ "$P6_RC" -eq 124 ] || fail P6-KILL "exit $P6_RC, want 124"
[ "$(( $(date +%s) - P6_START ))" -le 5 ] || fail P6-KILL "TERM-resistant npm was not stopped promptly"
grep -Fq 'timed out after 1 s' "$P6/err" || fail P6-KILL "missing deadline diagnostic"
printf 'ok   P6-KILL escalates when npm ignores TERM\n'

for P6_EXIT in 124 137; do
  P6_RC=0
  run_install "$P6" "$P6_EXIT" >"$P6/out" 2>"$P6/err" || P6_RC=$?
  [ "$P6_RC" -eq "$P6_EXIT" ] || fail P6-EXIT "exit $P6_RC, want $P6_EXIT"
  grep -Fxq "codacy-opengrep install: npm ci failed with exit $P6_EXIT" "$P6/err" || fail P6-EXIT "missing ordinary exit diagnostic for $P6_EXIT"
  if grep -Fq 'timed out' "$P6/err"; then
    fail P6-EXIT "ordinary exit $P6_EXIT was reported as a timeout"
  fi
  printf 'ok   P6-EXIT preserves ordinary npm exit %s\n' "$P6_EXIT"
done

P7_IDX=0
for P7_BAD in 0 3601 abc ''; do
  P7_IDX=$((P7_IDX + 1))
  P7_DIR="$WORK/p7-bad-$P7_IDX"
  make_root "$P7_DIR"
  make_stub "$P7_DIR/bin/npm"
  P7_RC=0
  if run_install "$P7_DIR" 0 "CODACY_OPENGREP_NPM_DEADLINE_S=$P7_BAD" >"$P7_DIR/out" 2>"$P7_DIR/err"; then
    fail P7 "invalid deadline '$P7_BAD' was accepted"
  else
    P7_RC=$?
  fi
  [ "$P7_RC" -eq 2 ] || fail P7 "deadline '$P7_BAD': exit $P7_RC, want 2"
  grep -Fq 'CODACY_OPENGREP_NPM_DEADLINE_S' "$P7_DIR/err" || fail P7 "deadline '$P7_BAD': stderr does not name CODACY_OPENGREP_NPM_DEADLINE_S"
  [ ! -e "$P7_DIR/npm.args" ] || fail P7 "deadline '$P7_BAD': npm ran before validation"
done
for P7_GOOD in 1 3600; do
  P7_DIR="$WORK/p7-good-$P7_GOOD"
  make_root "$P7_DIR"
  make_stub "$P7_DIR/bin/npm"
  if ! run_install "$P7_DIR" 0 "CODACY_OPENGREP_NPM_DEADLINE_S=$P7_GOOD" >"$P7_DIR/out" 2>"$P7_DIR/err"; then
    fail P7 "valid deadline '$P7_GOOD' failed: $(cat "$P7_DIR/err")"
  fi
  [ -f "$P7_DIR/npm.args" ] || fail P7 "valid deadline '$P7_GOOD': npm did not run"
done
P7_NOTIMEOUT="$WORK/p7-notimeout"
make_root "$P7_NOTIMEOUT"
make_stub "$P7_NOTIMEOUT/bin/npm"
P7_SH=$(command -v sh)
P7_RC=0
if env STUB_PWD="$P7_NOTIMEOUT/npm.pwd" STUB_ARGS="$P7_NOTIMEOUT/npm.args" STUB_EXIT=0 \
  PATH="$P7_NOTIMEOUT/bin" "$P7_SH" "$INSTALLER" "$P7_NOTIMEOUT" >"$P7_NOTIMEOUT/out" 2>"$P7_NOTIMEOUT/err"; then
  fail P7 "missing timeout on PATH was accepted"
else
  P7_RC=$?
fi
[ "$P7_RC" -eq 2 ] || fail P7 "missing timeout: exit $P7_RC, want 2"
grep -Fq 'timeout' "$P7_NOTIMEOUT/err" || fail P7 "missing timeout: stderr does not name timeout"
[ ! -e "$P7_NOTIMEOUT/npm.args" ] || fail P7 "missing timeout: npm ran"
printf 'ok   P7 validates CODACY_OPENGREP_NPM_DEADLINE_S bounds and timeout presence\n'

command -v go >/dev/null 2>&1 || fail P8 "go not on PATH"
P8="$WORK/p8"
make_root "$P8"
make_stub "$P8/bin/npm"
run_install "$P8" 0
P8_NPM_CWD=$(sed -n '1p' "$P8/npm.pwd")
[ -n "$P8_NPM_CWD" ] || fail P8 "npm.pwd is empty"
printf 'module example.com/plant\n\ngo 1.21\n' >"$P8/go.mod"
mkdir -p "$P8/tools/ci/sentinel" "$P8_NPM_CWD/node_modules/plant"
printf 'package sentinel\n' >"$P8/tools/ci/sentinel/s.go"
printf 'package plant\n' >"$P8_NPM_CWD/node_modules/plant/p.go"
P8_RC=0
P8_DIRS=$(go_list_dirs "$P8" "$P8/go.err") || P8_RC=$?
[ "$P8_RC" -eq 0 ] || fail P8 "go list exited $P8_RC: $(cat "$P8/go.err")"
NL='
'
case "${NL}${P8_DIRS}${NL}" in
  *"${NL}${P8}/tools/ci/sentinel${NL}"*) ;;
  *) fail P8 "sentinel directory missing from go list: $P8_DIRS" ;;
esac
case "${NL}${P8_DIRS}${NL}" in
  *"${NL}${P8_NPM_CWD}${NL}"* | *"${NL}${P8_NPM_CWD}/"*)
    fail P8 "go list walked npm cwd $P8_NPM_CWD: $P8_DIRS"
    ;;
esac
printf 'ok   P8 Go ./tools/... walk lists the sentinel and skips the npm install directory\n'

command -v go >/dev/null 2>&1 || fail P9 "go not on PATH"
P9_PLANT="$P8/tools/ci/codacy_opengrep/node_modules/plant"
mkdir -p "$P9_PLANT"
printf 'package plant\n' >"$P9_PLANT/p.go"
P9_RC=0
P9_DIRS=$(go_list_dirs "$P8" "$P8/p9-go.err") || P9_RC=$?
[ "$P9_RC" -eq 0 ] || fail P9 "go list exited $P9_RC: $(cat "$P8/p9-go.err")"
case "${NL}${P9_DIRS}${NL}" in
  *"${NL}${P9_PLANT}${NL}"*) ;;
  *) fail P9 "pre-fix node_modules package $P9_PLANT missing from go list: $P9_DIRS" ;;
esac
printf 'ok   P9 Go ./tools/... walk lists a package planted at the pre-fix path\n'

# P10: pause real npm ci at local tarball extraction until the Go walk finishes.
command -v npm >/dev/null 2>&1 || fail P10 "npm not on PATH"
P10="$WORK/p10"
make_root "$P10"
mkdir -p "$P10/package" "$P10/tools/ci/sentinel" "$P10/home"
printf 'module example.com/overlap\n\ngo 1.21\n' >"$P10/go.mod"
printf 'package sentinel\n' >"$P10/tools/ci/sentinel/s.go"
printf '{"name":"plant","version":"1.0.0"}\n' >"$P10/package/package.json"
printf 'package plant\n' >"$P10/package/p.go"
tar -cf "$P10/plant.tar" -C "$P10" package
# file:../../plant.tar resolves under tools from the old tools/ci install directory.
cp "$P10/plant.tar" "$P10/tools/plant.tar"
printf '{"name":"overlap","dependencies":{"plant":"file:../../plant.tar"}}\n' \
  >"$P10/tools/ci/codacy_opengrep/package.json"
printf '{"name":"overlap","lockfileVersion":3,"packages":{"":{"dependencies":{"plant":"file:../../plant.tar"}},"node_modules/plant":{"version":"1.0.0","resolved":"file:../../plant.tar"}}}\n' \
  >"$P10/tools/ci/codacy_opengrep/package-lock.json"
cat >"$P10/barrier.cjs" <<'EOF'
const {execFileSync} = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const Module = require('node:module');
const load = Module._load;
const root = process.env.P10_ROOT;
Module._load = function (id, ...args) {
  const pkg = load.call(this, id, ...args);
  if (id === 'pacote' && !pkg.extract.p10) {
    const extract = pkg.extract;
    pkg.extract = async function (spec, dest, ...opts) {
      const tarball = 'file:../../plant.tar';
      // Check the same relative tarball from npm's actual install directory.
      fs.accessSync(path.resolve(process.cwd(), tarball.slice('file:'.length)), fs.constants.R_OK);
      fs.writeFileSync(path.join(root, 'barrier'), dest + '\n');
      fs.mkdirSync(dest, {recursive: true});
      const planted = path.join(dest, 'p.go');
      fs.copyFileSync(path.join(root, 'package/p.go'), planted, fs.constants.COPYFILE_EXCL);
      let dirs;
      try {
        dirs = execFileSync('go', ['list', '-f', '{{.Dir}}', './tools/...'],
          {cwd: root, encoding: 'utf8', timeout: 10000, killSignal: 'SIGKILL'});
      } finally {
        fs.unlinkSync(planted);
      }
      if (dirs !== path.join(root, 'tools/ci/sentinel') + '\n') {
        throw new Error('unexpected Go walk: ' + dirs);
      }
      fs.writeFileSync(path.join(root, 'walk.ok'), dirs);
      return extract.call(this, spec, dest, ...opts);
    };
    pkg.extract.p10 = true;
  }
  return pkg;
};
EOF
: >"$P10/user.npmrc"
: >"$P10/global.npmrc"
if ! env -i PATH="$PATH" LC_ALL=C HOME="$P10/home" P10_ROOT="$P10" \
  NODE_OPTIONS="--require=$P10/barrier.cjs" npm_config_offline=true \
  npm_config_cache="$P10/npm-cache" npm_config_userconfig="$P10/user.npmrc" npm_config_globalconfig="$P10/global.npmrc" \
  GOENV=off GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOCACHE="$P10/go-cache" \
  sh "$INSTALLER" "$P10" >"$P10/out" 2>"$P10/err"; then
  fail P10 "concurrent install failed: $(cat "$P10/err")"
fi
P10_DEST="$P10/.cache/codacy_opengrep/node_modules/plant"
[ -f "$P10/barrier" ] && [ "$(cat "$P10/barrier")" = "$P10_DEST" ] || fail P10 "extraction barrier missing or at the wrong destination"
[ -s "$P10/walk.ok" ] || fail P10 "Go walk did not reach the sentinel"
[ -f "$P10_DEST/p.go" ] || fail P10 "npm did not extract the planted package"
printf 'ok   P10 Go ./tools/... walk completes while real npm ci is in progress\n'

printf 'codacy-opengrep install: all planted cases passed\n'
