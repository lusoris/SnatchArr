#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Generate web/static/licences.json for shipped Go, Rust, and npm dependencies."""

import argparse
import difflib
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

OWN_CRATES = {"snatch-worker", "snatch-core", "snatch-proto", "arr-client"}


class LicenceError(Exception):
    """A failure that main() reports and turns into an exit status."""

    def __init__(self, message, status):
        super().__init__(message)
        self.status = status


def run_cmd(cmd, cwd=None, env=None):
    """Run a command, checking exit status; exit 2 on failure with stderr tail."""
    res = subprocess.run(
        cmd,
        cwd=cwd,
        env=env,
        capture_output=True,
        text=True,
    )
    if res.returncode != 0:
        tail = "\n".join(res.stderr.strip().splitlines()[-20:])
        raise LicenceError(f"Error running {' '.join(cmd)} (exit {res.returncode}):\n{tail}", 2)
    return res.stdout


def build_rust_license_map(licenses):
    """Build a mapping of (crate_name, crate_ver) -> list of unique license texts."""
    crate_texts = {}
    for lic in licenses:
        txt = lic.get("text")
        if not txt:
            continue
        for u in lic.get("used_by", []):
            crate = u.get("crate", {})
            key = (crate.get("name"), crate.get("version"))
            texts = crate_texts.setdefault(key, [])
            if txt not in texts:
                texts.append(txt)
    return crate_texts


def collect_rust(repo_root):
    """Collect third-party Rust dependencies using cargo-about 0.9.2."""
    version_out = run_cmd(["cargo", "about", "--version"], cwd=repo_root).strip()
    if version_out != "cargo-about 0.9.2":
        raise LicenceError(f"Refusing cargo-about version: expected 'cargo-about 0.9.2', got '{version_out}'", 2)

    raw_json = run_cmd(["cargo", "about", "generate", "--format", "json"], cwd=repo_root)
    data = json.loads(raw_json)
    crate_texts = build_rust_license_map(data.get("licenses", []))

    rust_pkgs = []
    for c in data.get("crates", []):
        pkg = c["package"]
        name = pkg["name"]
        if name in OWN_CRATES:
            continue
        ver = pkg["version"]
        texts = crate_texts.get((name, ver), [])
        rust_pkgs.append({
            "ecosystem": "rust",
            "name": name,
            "version": ver,
            "licence": c.get("license") or "",
            "url": pkg.get("repository") or pkg.get("homepage") or "",
            "text": "\n\n---\n\n".join(texts),
        })
    return rust_pkgs


def run_go_licenses(repo_root, env):
    """Run go-licenses report with custom tab-separated template."""
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", delete=False) as f:
        f.write("{{range .}}{{.Name}}\t{{.Version}}\t{{.LicenseName}}\t{{.LicenseURL}}\t{{.LicensePath}}\n{{end}}")
        tpl_path = f.name
    try:
        cmd = [
            "go", "tool", "-modfile=tools/go/go.mod", "go-licenses", "report",
            "./api/cmd/snatcharr", "--ignore=github.com/lusoris/SnatchArr",
            f"--template={tpl_path}",
        ]
        return run_cmd(cmd, cwd=repo_root, env=env)
    finally:
        if os.path.exists(tpl_path):
            os.remove(tpl_path)


def normalize_go_url(name, url):
    """Drop /vN/ from GitHub URLs when the module ends in /vN (N>=2).

    go-licenses v2.0.1 (internal/third_party/pkgsite/source/source.go
    adjustVersionedModuleDirectory) sends a HEAD request for <repo>/blob/<tag>/vN/go.mod.
    On success it keeps the /vN/ dir, but on failure (e.g. CI rate limits) it
    drops it. For packages like github.com/sony/gobreaker/v2 whose LICENSE is at
    repo root, dropping the /vN/ segment makes both probe outcomes consistent.
    """
    if not url:
        return url
    m = re.search(r"/(v\d+)$", name)
    if m and int(m.group(1)[1:]) >= 2:
        return re.sub(r"(/blob/[^/]+)/" + re.escape(m.group(1)) + r"/", r"\1/", url, count=1)
    return url


def normalize_go_entry(name, ver, lic, url, path, gomodcache):
    """Normalize Go entry fields, handling known special packages."""
    if name.startswith("github.com/segmentio/asm"):
        return (
            "github.com/segmentio/asm",
            "v1.2.1",
            "MIT-0",
            "https://github.com/segmentio/asm/blob/v1.2.1/LICENSE",
            os.path.join(gomodcache, "github.com/segmentio/asm@v1.2.1/LICENSE"),
        )
    url = normalize_go_url(name, url)
    return name, ver, lic, url, path


EUPL_HEADER = re.compile(r"^\s*EUROPEAN UNION PUBLIC LICENCE v\. (\d\.\d)\b")

# BSD-1-Clause (https://spdx.org/licenses/BSD-1-Clause.html): one numbered
# condition plus the BSD disclaimer. go-licenses v2.0.1 labels this text
# LicenseRef-uthash (filippo.io/csrf v0.2.1), which is no SPDX identifier.
BSD_1_CLAUSE_MARKERS = (
    "1. Redistributions of source code must retain the above copyright notice",
    "THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS",
)


def normalize_expression(lic):
    """Collapse runs of whitespace in an SPDX licence expression."""
    return " ".join(lic.split())


def correct_go_licence(lic, text):
    """Correct go-licenses classifier labels that disagree with the licence text.

    go-licenses v2.0.1 reports EUPL-1.2 texts as EUPL-1.0; the version in the
    text header wins. A LicenseRef-* label is only accepted when the text is
    recognised; anything else returns None so the caller fails.
    """
    flat = " ".join(text.split())
    header = EUPL_HEADER.match(text)
    if lic.startswith("EUPL-") and header:
        return f"EUPL-{header.group(1)}"
    if lic == "LicenseRef-uthash":
        if all(m in flat for m in BSD_1_CLAUSE_MARKERS) and "2. Redistributions" not in flat:
            return "BSD-1-Clause"
        return None
    if lic.startswith("LicenseRef-"):
        return None
    return lic


def update_go_entry(entry, lic, url, path):
    """Update aggregated licence, url, and path metadata for a Go package."""
    if lic and lic != "Unknown":
        entry["licences"].add(lic)
    if url and url != "Unknown" and not entry["url"]:
        entry["url"] = url
    if path and path != "Unknown" and not entry["path"]:
        entry["path"] = path


def parse_go_report(out, gomodcache):
    """Parse go-licenses report lines into grouped package records."""
    by_pkg = {}
    for line in out.splitlines():
        if "\t" not in line:
            continue
        parts = line.split("\t")
        name, ver, lic, url, path = normalize_go_entry(
            parts[0], parts[1], parts[2], parts[3], parts[4], gomodcache
        )
        entry = by_pkg.setdefault((name, ver), {"licences": set(), "url": "", "path": ""})
        update_go_entry(entry, lic, url, path)
    return by_pkg


def read_file_or_empty(path):
    """Read file content as utf-8 string, or return empty string."""
    if path and path != "Unknown" and os.path.exists(path):
        with open(path, "r", encoding="utf-8", errors="replace") as f:
            return f.read()
    return ""


def load_committed_go_packages(committed_path):
    """Load map of Go packages from committed file, or empty dict if missing."""
    if not committed_path or not os.path.exists(committed_path):
        return {}
    try:
        with open(committed_path, "r", encoding="utf-8") as f:
            data = json.load(f)
        return {
            p["name"]: p
            for p in data.get("packages", [])
            if p.get("ecosystem") == "go" and "name" in p
        }
    except (OSError, json.JSONDecodeError, KeyError):
        return {}


def reconcile_go_url(pkg, committed):
    """Reconcile resolved Go package URL with committed metadata on lookup failure."""
    resolved_url = pkg.get("url") or ""
    if resolved_url and resolved_url != "Unknown":
        return resolved_url

    if not committed:
        return ""

    committed_pkg = (
        committed
        if ("version" in committed or "url" in committed)
        else committed.get(pkg.get("name"))
    )
    if not committed_pkg:
        return ""

    if committed_pkg.get("version") == pkg.get("version"):
        return committed_pkg.get("url") or ""

    return ""


def reconcile_go_package(pkg, committed, check=False):
    """Reconcile Go package licence URL, reporting or erroring on failed lookup."""
    raw_url = pkg.get("url") or ""
    committed_pkg = (
        committed
        if (not committed or "version" in committed or "url" in committed)
        else committed.get(pkg.get("name"))
    )
    url = reconcile_go_url(pkg, committed_pkg)
    if (not raw_url or raw_url == "Unknown") and url:
        name = pkg.get("name", "")
        ver = pkg.get("version", "")
        if check:
            print(f"Kept committed licence URL for {name}@{ver}", file=sys.stderr)
        else:
            raise LicenceError(f"Failed licence URL lookup for {name}@{ver}; rerun to retry", 2)
    reconciled = dict(pkg)
    reconciled["url"] = url
    return reconciled


def collect_go(repo_root, committed_path=None, check=False):
    """Collect third-party Go dependencies using go-licenses v2.0.1."""
    env = os.environ.copy()
    env["GOOS"] = "linux"
    env["GOARCH"] = "amd64"
    gomodcache = run_cmd(["go", "env", "GOMODCACHE"], repo_root, env).strip()

    out = run_go_licenses(repo_root, env)
    by_pkg = parse_go_report(out, gomodcache)
    committed_pkgs = load_committed_go_packages(committed_path)

    go_pkgs = []
    for (name, ver), info in by_pkg.items():
        text = read_file_or_empty(info.get("path"))
        corrected = sorted({correct_go_licence(lic, text) or "Unknown" for lic in info["licences"]})
        lic_str = " AND ".join(corrected) if corrected else "Unknown"
        pkg = {
            "ecosystem": "go",
            "name": name,
            "version": ver,
            "licence": lic_str,
            "url": info.get("url") or "",
            "text": text,
        }
        reconciled = reconcile_go_package(pkg, committed_pkgs.get(name), check=check)
        go_pkgs.append(reconciled)
    return go_pkgs


def find_npm_text(pth, lic, name, ver):
    """Find or synthesize license text for an npm package path."""
    if os.path.exists(pth):
        files = os.listdir(pth)
        lic_files = sorted(f for f in files if any(f.lower().startswith(x) for x in ["licen", "copy", "notice"]))
        if lic_files:
            with open(os.path.join(pth, lic_files[0]), "r", encoding="utf-8", errors="replace") as f:
                return f.read()
        readme_files = [f for f in files if f.lower() == "readme.md"]
        if readme_files:
            with open(os.path.join(pth, readme_files[0]), "r", encoding="utf-8", errors="replace") as f:
                rtxt = f.read()
            idx = rtxt.lower().find("license")
            if idx != -1:
                return rtxt[idx:].strip()

    return f"{lic} License\n\nPackage: {name}@{ver}\n"


def collect_npm(repo_root):
    """Collect production npm dependencies using pnpm."""
    web_dir = os.path.join(repo_root, "web")
    pnpm_ver = run_cmd(["pnpm", "--version"], cwd=web_dir).strip()
    raw_json = run_cmd(["pnpm", "licenses", "list", "--json", "--prod"], cwd=web_dir)
    data = json.loads(raw_json)

    npm_pkgs = []
    for lic, pkgs in data.items():
        for p in pkgs:
            name = p["name"]
            url = p.get("homepage") or ""
            for ver, pth in zip(p["versions"], p["paths"]):
                text = find_npm_text(pth, lic, name, ver)
                npm_pkgs.append({
                    "ecosystem": "npm",
                    "name": name,
                    "version": ver,
                    "licence": lic,
                    "url": url,
                    "text": text,
                })
    return pnpm_ver, npm_pkgs


def build_payload(repo_root, committed_path=None, check=False):
    """Build the complete sorted payload dictionary and validate licenses."""
    if committed_path is None:
        committed_path = repo_root / "web" / "static" / "licences.json"
    rust_pkgs = collect_rust(repo_root)
    go_pkgs = collect_go(repo_root, committed_path=committed_path, check=check)
    pnpm_ver, npm_pkgs = collect_npm(repo_root)

    all_pkgs = go_pkgs + rust_pkgs + npm_pkgs
    all_pkgs.sort(key=lambda x: (x["ecosystem"], x["name"], x["version"]))

    # Check for missing or unknown licences
    for p in all_pkgs:
        p["licence"] = normalize_expression(p.get("licence", ""))
        lic = p["licence"]
        if not lic or "unknown" in lic.lower().split():
            raise LicenceError(f"Error: licence missing or unknown for {p['ecosystem']} package '{p['name']}@{p['version']}'", 1)

    payload = {
        "generatedBy": {
            "cargo-about": "0.9.2",
            "go-licenses": "2.0.1",
            "pnpm": pnpm_ver,
        },
        "packages": all_pkgs,
    }
    return json.dumps(payload, indent=2, sort_keys=True) + "\n"


def main():
    """Main CLI entrypoint for generating or checking licences.json."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="Check if committed licences.json is up to date")
    parser.add_argument("--committed", type=str, default="", help="Path to committed licences.json")
    args = parser.parse_args()

    repo_root = Path(__file__).resolve().parent.parent.parent
    committed_path = Path(args.committed) if args.committed else repo_root / "web" / "static" / "licences.json"

    if args.check and not committed_path.exists():
        print(f"Check failed: committed file '{committed_path}' does not exist", file=sys.stderr)
        sys.exit(1)

    try:
        new_content = build_payload(repo_root, committed_path=committed_path, check=args.check)
    except LicenceError as err:
        print(err, file=sys.stderr)
        sys.exit(err.status)

    if args.check:
        committed_content = committed_path.read_text(encoding="utf-8")
        if new_content == committed_content:
            print("Licences check passed: committed licences.json is up to date.")
            sys.exit(0)

        diff = list(difflib.unified_diff(
            committed_content.splitlines(keepends=True),
            new_content.splitlines(keepends=True),
            fromfile=str(committed_path),
            tofile="generated",
        ))
        head = "".join(diff[:40])
        print(f"Check failed: licences.json is stale:\n{head}", file=sys.stderr)
        sys.exit(1)
    else:
        committed_path.parent.mkdir(parents=True, exist_ok=True)
        committed_path.write_text(new_content, encoding="utf-8")
        size_bytes = len(new_content.encode("utf-8"))
        size_mb = size_bytes / (1024 * 1024)
        print(f"Wrote {committed_path} ({size_bytes} bytes, {size_mb:.2f} MB)")


if __name__ == "__main__":
    main()
