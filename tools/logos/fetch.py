#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Download and verify vendor-checked integrated-application logos."""

import argparse
import fnmatch
import hashlib
import json
import os
import sys
import tomllib
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

import yaml

EXPECTED_EXCEPTIONS = {
    "teams": "chat",
    "slack": "chat",
    "telegram": "chat",
    "discord": "chat",
    "rtorrent": "torrent",
    "emby": "media-server",
}
EXCEPTION_REASON = "vendor terms bar integration-icon use"
EXTRA_INTEGRATION_KEYS = {"prowlarr", "seerr", "configarr"}


class LogoError(Exception):
    """An error that names the key and triggers exit code 1."""

    def __init__(self, key: str, message: str):
        super().__init__(f"Error [{key}]: {message}")
        self.key = key


def load_expected_keys(openapi_path: Path) -> set[str]:
    """Read integrated app enum keys from api/openapi/openapi.yaml."""
    if not openapi_path.exists():
        raise LogoError("openapi", f"OpenAPI spec not found at {openapi_path}")

    with openapi_path.open("r", encoding="utf-8") as f:
        spec = yaml.safe_load(f)

    schemas = spec.get("components", {}).get("schemas", {})
    keys: set[str] = set()

    app_kinds = schemas.get("AppKind", {}).get("enum", [])
    keys.update(app_kinds)

    dl_kinds = schemas.get("DownloadClientKind", {}).get("enum", [])
    keys.update(dl_kinds)

    notif_kinds = schemas.get("NotificationChannelKind", {}).get("enum", [])
    keys.update(k for k in notif_kinds if k != "webhook")

    watch_kinds = schemas.get("WatchawareSourceKind", {}).get("enum", [])
    keys.update(watch_kinds)

    keys.update(EXTRA_INTEGRATION_KEYS)
    return keys


def parse_raw_url(repo_url: str, commit: str, rel_path: str) -> str:
    """Format raw.githubusercontent.com URL for GitHub repository assets."""
    parsed = urllib.parse.urlparse(repo_url)
    parts = parsed.path.strip("/").split("/")
    if len(parts) < 2:
        raise ValueError(f"Invalid GitHub repo URL: {repo_url}")
    owner, repo = parts[0], parts[1]
    return f"https://raw.githubusercontent.com/{owner}/{repo}/{commit}/{rel_path}"


def download_file(url: str, expected_sha256: str, key: str) -> bytes:
    """Download asset over HTTP with 30s timeout and bounded retries."""
    headers = {"User-Agent": "SnatchArr-LogoFetcher/1.0"}
    req = urllib.request.Request(url, headers=headers)
    max_retries = 3

    for attempt in range(1, max_retries + 1):
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                data = resp.read()
                actual_sha256 = hashlib.sha256(data).hexdigest()
                if actual_sha256 != expected_sha256:
                    raise LogoError(
                        key,
                        f"sha256 mismatch: expected {expected_sha256}, got {actual_sha256}",
                    )
                return data
        except (urllib.error.URLError, TimeoutError) as exc:
            if attempt == max_retries:
                raise LogoError(key, f"failed to download {url}: {exc}") from exc
    raise LogoError(key, f"exhausted retries downloading {url}")


def match_reuse_spdx(rel_file_path: str, reuse_data: dict) -> str | None:
    """Find matching SPDX licence identifier in REUSE annotations for file path."""
    posix_path = rel_file_path.replace(os.sep, "/")
    matched_override: str | None = None
    matched_aggregate: str | None = None

    for annot in reuse_data.get("annotations", []):
        patterns = annot.get("path", [])
        if isinstance(patterns, str):
            patterns = [patterns]
        precedence = annot.get("precedence", "aggregate")
        spdx = annot.get("SPDX-License-Identifier")

        for pattern in patterns:
            norm_pattern = pattern.replace(os.sep, "/")
            if norm_pattern == posix_path:
                return spdx
            if fnmatch.fnmatch(posix_path, norm_pattern):
                if precedence == "override":
                    matched_override = spdx
                elif matched_aggregate is None:
                    matched_aggregate = spdx

    return matched_override if matched_override is not None else matched_aggregate


def verify_exceptions(manifest: dict) -> None:
    """Ensure manifest exception set matches the required 6 exactly."""
    manifest_exceptions: dict[str, str] = {}
    for key, entry in manifest.items():
        if "exception" in entry:
            manifest_exceptions[key] = entry["exception"]
            if key not in EXPECTED_EXCEPTIONS:
                raise LogoError(key, "unexpected exception (not in allowed 6)")
            expected_kind = EXPECTED_EXCEPTIONS[key]
            if entry["exception"] != expected_kind:
                raise LogoError(
                    key,
                    f"wrong exception type: expected {expected_kind}, got {entry['exception']}",
                )
            if entry.get("reason") != EXCEPTION_REASON:
                raise LogoError(key, f"invalid exception reason: {entry.get('reason')}")

    for expected_key in EXPECTED_EXCEPTIONS:
        if expected_key not in manifest_exceptions:
            raise LogoError(expected_key, "missing expected exception")


def verify_manifest_keys(manifest: dict, expected_keys: set[str]) -> None:
    """Verify all expected enum and integration keys are present in manifest."""
    for key in sorted(expected_keys):
        if key not in manifest:
            raise LogoError(key, "manifest missing required key")


def verify_logo_files(
    manifest: dict,
    logos_dir: Path,
    repo_root: Path,
    reuse_data: dict,
) -> None:
    """Verify file existence, hash integrity, stray files, and REUSE coverage."""
    expected_files: set[str] = set()

    for key, entry in manifest.items():
        if "file" not in entry:
            continue
        filename = entry["file"]
        expected_files.add(filename)
        dest = logos_dir / filename

        if not dest.exists():
            raise LogoError(key, f"logo file missing: {dest}")

        content = dest.read_bytes()
        actual_sha256 = hashlib.sha256(content).hexdigest()
        if actual_sha256 != entry.get("sha256"):
            raise LogoError(
                key,
                f"sha256 mismatch on disk: expected {entry.get('sha256')}, got {actual_sha256}",
            )

        annot_path = f"web/static/logos/{filename}"
        annot_spdx = match_reuse_spdx(annot_path, reuse_data)
        if not annot_spdx:
            raise LogoError(key, f"REUSE.toml missing annotation for {annot_path}")
        if annot_spdx != entry.get("spdx"):
            raise LogoError(
                key,
                f"REUSE.toml spdx mismatch: expected '{entry.get('spdx')}', got '{annot_spdx}'",
            )

    if logos_dir.exists():
        for item in logos_dir.iterdir():
            if item.is_file() and item.name not in expected_files:
                raise LogoError(item.name, f"stray file found in {logos_dir}")


def check_offline(
    manifest_path: Path,
    logos_dir: Path,
    openapi_path: Path,
    reuse_path: Path,
    repo_root: Path,
) -> None:
    """Run offline verification of manifest, logos, exceptions, and REUSE."""
    if not manifest_path.exists():
        raise LogoError("manifest", f"manifest file not found at {manifest_path}")

    with manifest_path.open("r", encoding="utf-8") as f:
        manifest = json.load(f)

    if not reuse_path.exists():
        raise LogoError("reuse", f"REUSE.toml not found at {reuse_path}")

    with reuse_path.open("rb") as f:
        reuse_data = tomllib.load(f)

    expected_keys = load_expected_keys(openapi_path)
    verify_manifest_keys(manifest, expected_keys)
    verify_exceptions(manifest)
    verify_logo_files(manifest, logos_dir, repo_root, reuse_data)
    print("Logo check passed: all keys, hashes, exceptions, and REUSE annotations match.")


def fetch_all(manifest_path: Path, logos_dir: Path) -> None:
    """Fetch all logo files declared in manifest from upstream repositories."""
    if not manifest_path.exists():
        raise LogoError("manifest", f"manifest file not found at {manifest_path}")

    with manifest_path.open("r", encoding="utf-8") as f:
        manifest = json.load(f)

    logos_dir.mkdir(parents=True, exist_ok=True)

    for key, entry in sorted(manifest.items()):
        if "file" not in entry:
            continue
        filename = entry["file"]
        url = parse_raw_url(entry["repo"], entry["commit"], entry["path"])
        expected_sha256 = entry["sha256"]
        print(f"Fetching [{key}] -> {filename} from {url} ...")
        data = download_file(url, expected_sha256, key)
        dest = logos_dir / filename
        dest.write_bytes(data)
        print(f"Saved {dest} ({len(data)} bytes, sha256 verified)")


def parse_arguments() -> argparse.Namespace:
    """Parse CLI options for logo fetch and check operations."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--check",
        action="store_true",
        help="Run offline verification checks (no network calls)",
    )
    parser.add_argument(
        "--manifest",
        type=str,
        default="",
        help="Path to logos.json manifest file",
    )
    parser.add_argument(
        "--logos-dir",
        type=str,
        default="",
        help="Path to static logos directory",
    )
    parser.add_argument(
        "--openapi",
        type=str,
        default="",
        help="Path to openapi.yaml",
    )
    parser.add_argument(
        "--reuse",
        type=str,
        default="",
        help="Path to REUSE.toml",
    )
    return parser.parse_args()


def main() -> None:
    """Entry point for fetch.py."""
    args = parse_arguments()
    repo_root = Path(__file__).resolve().parent.parent.parent

    manifest_path = (
        Path(args.manifest)
        if args.manifest
        else repo_root / "web" / "src" / "lib" / "logos" / "logos.json"
    )
    logos_dir = (
        Path(args.logos_dir)
        if args.logos_dir
        else repo_root / "web" / "static" / "logos"
    )
    openapi_path = (
        Path(args.openapi)
        if args.openapi
        else repo_root / "api" / "openapi" / "openapi.yaml"
    )
    reuse_path = Path(args.reuse) if args.reuse else repo_root / "REUSE.toml"

    try:
        if args.check:
            check_offline(
                manifest_path,
                logos_dir,
                openapi_path,
                reuse_path,
                repo_root,
            )
        else:
            fetch_all(manifest_path, logos_dir)
    except LogoError as err:
        print(str(err), file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
