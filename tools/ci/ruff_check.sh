#!/bin/sh
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Runs pinned ruff over every tracked Python file in the repository (or ROOT).
# Uses a virtual environment under CACHE_DIR pinned by tools/ci/ruff-requirements.txt.
# Refuses requirements without pin + sha256 hash. Refuses ruff version mismatch.
#
#   tools/ci/ruff_check.sh CACHE_DIR [ROOT]
#
# Exit status: 0 if no findings; non-zero on a finding (xargs reports 123) or a refused setup.
set -eu

cache="${1:?usage: ruff_check.sh CACHE_DIR [ROOT]}"
mkdir -p "$cache"
cache="$(cd "$cache" && pwd)"
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
root="${2:-$REPO_ROOT}"

RUFF_REQ="${RUFF_REQ:-$HERE/ruff-requirements.txt}"
if [ ! -f "$RUFF_REQ" ] || \
   ! grep -qE '^ruff==[0-9]+(\.[0-9]+)+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?([[:space:]]*\\)?$' "$RUFF_REQ" || \
   ! grep -qE -e '--hash=sha256:[a-f0-9]{64}' "$RUFF_REQ"; then
  echo "error: $RUFF_REQ: ruff is not pinned by version and hash" >&2
  exit 1
fi

pinned_version="$(sed -n 's/^ruff==\([0-9][0-9a-zA-Z.+_-]*\).*/\1/p' "$RUFF_REQ" | head -n 1)"

req_hash="$(sha256sum "$RUFF_REQ" | cut -c1-12)"
venv="$cache/ruff-$req_hash"
if [ -z "${RUFF_BIN:-}" ] && [ ! -x "$venv/bin/ruff" ]; then
  mkdir -p "$cache"
  python3 -m venv "$venv"
  "$venv/bin/pip" install --quiet --disable-pip-version-check --require-hashes -r "$RUFF_REQ"
fi

ruff="${RUFF_BIN:-$venv/bin/ruff}"
if [ ! -x "$ruff" ]; then
  echo "error: ruff executable not found at $ruff" >&2
  exit 1
fi

version="$("$ruff" --version 2>&1 | awk '{print $2}')"
if [ "$version" != "$pinned_version" ]; then
  echo "error: ruff reports $version, want $pinned_version" >&2
  exit 1
fi

# check_exceptions RUFF TODAY: every entry under [lint.per-file-ignores] in
# ./ruff.toml needs the comment line "# category X, entered DATE, expires DATE: reason"
# right above it, an expiry not before TODAY, and every listed rule still firing
# in its file with the exceptions switched off. Exits 1 on the first violation.
check_exceptions() {
  [ -f ruff.toml ] || return 0
  entries="$(awk '
    /^\[/ { in_pfi = ($0 == "[lint.per-file-ignores]"); prev = ""; next }
    in_pfi && /^"/ {
      split($0, kv, "\"")
      rules = $0; sub(/^[^[]*\[/, "", rules); sub(/\].*$/, "", rules); gsub(/[" ,]+/, " ", rules)
      ex = "none"
      if (prev ~ /^# category [ABCD], entered [0-9]{4}-[0-9]{2}-[0-9]{2}, expires [0-9]{4}-[0-9]{2}-[0-9]{2}: ./) {
        ex = prev; sub(/^.*expires /, "", ex); sub(/:.*$/, "", ex)
      }
      print kv[2] "|" ex "|" rules
    }
    { prev = $0 }
  ' ruff.toml)" || { echo "error: ruff.toml: reading [lint.per-file-ignores] failed" >&2; exit 1; }
  [ -n "$entries" ] || return 0
  printf '%s\n' "$entries" | while IFS='|' read -r file expires rules; do
    if [ "$expires" = none ]; then
      echo "error: ruff.toml: exception for $file lacks its \"# category X, entered DATE, expires DATE: reason\" line" >&2
      exit 1
    fi
    if [ "$(printf %s "$expires" | tr -d -)" -lt "$(printf %s "$2" | tr -d -)" ]; then
      echo "error: ruff.toml: exception for $file expired on $expires" >&2
      exit 1
    fi
    if [ ! -f "$file" ]; then
      echo "error: ruff.toml: exception for $file is stale (no such file)" >&2
      exit 1
    fi
    rc=0
    found="$("$1" check --no-cache --output-format concise --config 'lint.per-file-ignores = {}' "$file" 2>&1)" || rc=$?
    if [ "$rc" -gt 1 ]; then
      printf 'error: ruff failed on %s (exit %s)\n%s\n' "$file" "$rc" "$found" >&2
      exit 1
    fi
    for rule in $rules; do
      case "$found" in
        *": $rule "*) ;;
        *)
          echo "error: ruff.toml: exception $rule for $file is stale (rule no longer fires)" >&2
          exit 1
          ;;
      esac
    done
  done
}

cd "$root"
check_exceptions "$ruff" "${RUFF_CHECK_TODAY:-$(date +%F)}"
if [ -z "$(git ls-files '*.py')" ]; then
  exit 0
fi

git ls-files -z '*.py' | xargs -0 "$ruff" check --no-cache --output-format concise
