#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Count Jellyfin movie naming mismatches from NUL-delimited path records."""

from __future__ import annotations

import json
import re
import secrets
import subprocess
import sys
from collections import defaultdict
from pathlib import Path
from typing import TypedDict

PRIVACY_TIMEOUT_SECONDS = 30


class CensusResult(TypedDict):
    """Structured census totals and per-class video counts."""

    classes: dict[str, dict[str, int]]
    totals: dict[str, int | dict[str, int]]


class VideoGroups(TypedDict):
    """Video sets and folder indexes used by naming classification."""

    scanned: set[str]
    scanned_by_folder: dict[str, list[str]]
    groupable_by_folder: dict[str, list[str]]
    stacked: set[str]
    multiversion: set[str]
    collisions: set[str]


# Jellyfin's pinned video extensions: NamingOptions.cs:24-78.
# https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Naming/Common/NamingOptions.cs#L24-L78
VIDEO_EXTENSIONS = frozenset(
    {
        ".001",
        ".3g2",
        ".3gp",
        ".amv",
        ".asf",
        ".asx",
        ".avi",
        ".bin",
        ".bivx",
        ".divx",
        ".dv",
        ".dvr-ms",
        ".f4v",
        ".fli",
        ".flv",
        ".ifo",
        ".img",
        ".iso",
        ".m2t",
        ".m2ts",
        ".m2v",
        ".m4v",
        ".mkv",
        ".mk3d",
        ".mov",
        ".mp4",
        ".mpe",
        ".mpeg",
        ".mpg",
        ".mts",
        ".mxf",
        ".nrg",
        ".nsv",
        ".nuv",
        ".ogm",
        ".ogv",
        ".pva",
        ".qt",
        ".rec",
        ".rm",
        ".rmvb",
        ".strm",
        ".svq3",
        ".tp",
        ".ts",
        ".ty",
        ".viv",
        ".vob",
        ".vp3",
        ".webm",
        ".wmv",
        ".wtv",
        ".xvid",
    }
)
EXTENSION_GROUPS = {
    "image": frozenset(
        {".avif", ".bmp", ".gif", ".jpeg", ".jpg", ".png", ".tif", ".tiff", ".webp"}
    ),
    "metadata": frozenset({".nfo", ".xml", ".json"}),
    "subtitle": frozenset({".ass", ".srt", ".ssa", ".sub", ".sup", ".vtt"}),
    "audio": frozenset({".aac", ".flac", ".m4a", ".mp3", ".ogg", ".wav"}),
    "archive": frozenset({".7z", ".cbz", ".cbr", ".rar", ".zip"}),
}
EXTRA_FOLDERS = frozenset(
    {
        "behind the scenes",
        "backdrops",
        "clips",
        "deleted scenes",
        "extras",
        "extra",
        "featurettes",
        "interviews",
        "other",
        "sample",
        "samples",
        "scenes",
        "shorts",
        "trailers",
    }
)
EXTRA_SUFFIXES = (
    "-trailer",
    ".trailer",
    "_trailer",
    "- trailer",
    "-sample",
    ".sample",
    "_sample",
    "- sample",
    "-scene",
    "-clip",
    "-interview",
    "-behindthescenes",
    "-deleted",
    "-deletedscene",
    "-featurette",
    "-short",
    "-extra",
    "-other",
)
SAMPLE_SUFFIXES = ("-sample", ".sample", "_sample", "- sample")
STACK_RULES = (
    # NamingOptions.cs:142; FileStackRule.cs:20 compiles with IgnoreCase.
    (
        re.compile(
            r"^(?P<filename>.*?)(?:(?<=[\]\)\}])|[ _.-]+)[\(\[]?(?P<parttype>cd|dvd|part|pt|dis[ck])[ _.-]*(?P<number>[0-9]+)[\)\]]?(?:\.[^.]+)?$",
            re.IGNORECASE,
        ),
        True,
    ),
    # NamingOptions.cs:143; StackResolver.cs:86-120 tries this after the numeric rule.
    (
        re.compile(
            r"^(?P<filename>.*?)(?:(?<=[\]\)\}])|[ _.-]+)[\(\[]?(?P<parttype>cd|dvd|part|pt|dis[ck])[ _.-]*(?P<number>[a-d])[\)\]]?(?:\.[^.]+)?$",
            re.IGNORECASE,
        ),
        False,
    ),
)
YEAR_RE = re.compile(r"(?:19|20)[0-9]{2}")
DATE_ONLY_RE = re.compile(r"^(?:19|20)[0-9]{2}[._ -][0-9]{2}[._ -][0-9]{2}(?:$|[ _.-])")
PROVIDER_RE = re.compile(r"\[(?:imdbid|tmdbid|tvdbid)-[^\]]+\]", re.IGNORECASE)
RELEASE_WORDS_RE = re.compile(
    r"(?:^|[ ._-])(?:2160p|1080p|720p|480p|4k|uhd|bluray|blu-ray|web-dl|h\.?26[45]|x26[45])(?:$|[ ._-])",
    re.IGNORECASE,
)


def parse_records(data: bytes) -> list[tuple[str, str]]:
    """Parse find-style records without ever including names in errors."""
    chunks = data.split(b"\0")
    if chunks and chunks[-1] == b"":
        chunks.pop()
    records: list[tuple[str, str]] = []
    for index, chunk in enumerate(chunks, 1):
        if b"\t" not in chunk:
            raise ValueError(f"record {index}: missing type separator")
        raw_type, raw_path = chunk.split(b"\t", 1)
        if len(raw_type) != 1 or not raw_path:
            raise ValueError(f"record {index}: invalid type or empty path")
        path = raw_path.decode("utf-8", errors="surrogateescape")
        if path.startswith("/") or ".." in path.split("/"):
            raise ValueError(f"record {index}: path is not relative")
        kind = raw_type.decode("ascii", errors="replace")
        records.append((kind if kind in {"f", "d", "l"} else "other", path))
    return records


def parent(path: str) -> str:
    """Return the relative directory containing a path."""
    return path.rpartition("/")[0]


def extension(path: str) -> str:
    """Return the lowercase final extension of a path."""
    return Path(path).suffix.lower()


def clean_key(path: str) -> tuple[str, str]:
    """Approximate Jellyfin title/year normalization for collision candidates."""
    # Jellyfin cleaners: CleanStringParser.cs:19-49 and CleanDateTimeParser.cs:38-50.
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Naming/Video/CleanStringParser.cs#L19-L49
    stem = Path(path).stem
    stem = PROVIDER_RE.sub("", stem)
    year_match = YEAR_RE.search(stem)
    year = year_match.group(0) if year_match else ""
    stem = YEAR_RE.sub("", stem)
    stem = RELEASE_WORDS_RE.sub(" ", stem)
    for suffix in EXTRA_SUFFIXES:
        if stem.lower().endswith(suffix):
            stem = stem[: -len(suffix)]
            break
    title = "".join(char.casefold() for char in stem if char.isalnum())
    return title, year


def _stack_members(videos: set[str]) -> set[str]:
    """Find videos grouped by the pinned numeric and letter stack rules."""
    # Jellyfin stacking patterns: NamingOptions.cs:140-144; StackResolver.cs:71-131.
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Naming/Video/StackResolver.cs#L71-L131
    groups: dict[tuple[str, str], tuple[str, bool, dict[str, str]]] = {}
    for path in sorted(videos):
        for rule, is_numerical in STACK_RULES:
            match = rule.match(Path(path).name)
            if match is None:
                continue
            key = (parent(path), match.group("filename"))
            part_type = match.group("parttype").casefold()
            part_number = match.group("number").casefold()
            group = groups.get(key)
            if group is None:
                groups[key] = (part_type, is_numerical, {part_number: path})
            elif group[0] == part_type and group[1] == is_numerical:
                group[2].setdefault(part_number, path)
            break
    return {
        path
        for _, _, parts in groups.values()
        if len(parts) > 1
        for path in parts.values()
    }


def _multiversion_members(videos: set[str]) -> set[str]:
    """Find groups whose filenames use the containing folder as a prefix."""
    # Jellyfin version grouping: VideoListResolver.cs:180-205, pinned source.
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Naming/Video/VideoListResolver.cs#L180-L205
    groups: dict[str, list[str]] = defaultdict(list)
    for path in videos:
        folder = parent(path)
        folder_name = folder.rpartition("/")[2]
        stem = Path(path).stem
        if folder_name and stem.casefold().startswith(folder_name.casefold()):
            tail = stem[len(folder_name) :]
            if tail and tail[0] in "-_. [":
                groups[folder].append(path)
    return {path for group in groups.values() if len(group) > 1 for path in group}


def _collision_members(videos: set[str]) -> set[str]:
    """Find videos sharing an approximate clean title and year."""
    # Candidate collisions: Jellyfin clean-name and year parsers, pinned source.
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Naming/Video/CleanStringParser.cs#L19-L49
    groups: dict[tuple[str, str], list[str]] = defaultdict(list)
    for path in videos:
        key = clean_key(path)
        if key[0]:
            groups[key].append(path)
    return {path for group in groups.values() if len(group) > 1 for path in group}


def _mark(
    label: str,
    path: str,
    affected: dict[str, set[str]],
    folders: dict[str, set[str]],
    folder: str | None = None,
) -> None:
    """Record an affected video and its containing folder for a class."""
    affected[label].add(path)
    folders[label].add(parent(path) if folder is None else folder)


def _mark_extras(
    path: str,
    stem: str,
    containing: str,
    ignored_dirs: set[str],
    affected: dict[str, set[str]],
    folders: dict[str, set[str]],
) -> tuple[bool, bool]:
    """Mark video extras, sample markers, and ignored directory members."""
    is_extra = False
    # Exact parent-folder extra tokens: NamingOptions.cs:495-585.
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Naming/Common/NamingOptions.cs#L495-L585
    immediate = containing.rpartition("/")[2].casefold()
    if immediate in EXTRA_FOLDERS and containing:
        _mark("extra_folder", path, affected, folders, containing)
        is_extra = True
    # Jellyfin trims filename digits before file rule matching: ExtraRuleResolver.cs:33-52.
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Naming/Video/ExtraRuleResolver.cs#L33-L52
    rule_stem = stem.rstrip("0123456789")
    # Extra suffix tokens: NamingOptions.cs:606-711.
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Naming/Common/NamingOptions.cs#L606-L711
    if rule_stem in {"sample", "trailer"} or any(
        rule_stem.endswith(suffix) for suffix in EXTRA_SUFFIXES
    ):
        _mark("extra_suffix", path, affected, folders)
        is_extra = True
    # Sample suffix rules: NamingOptions.cs:587-597, 629-650.
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Naming/Common/NamingOptions.cs#L587-L650
    if rule_stem == "sample" or any(
        rule_stem.endswith(suffix) for suffix in SAMPLE_SUFFIXES
    ):
        _mark("sample_marker", path, affected, folders)
        is_extra = True
    # `.ignore` excludes a directory and descendants, docs, Excluding whole directories.
    # https://github.com/jellyfin/jellyfin.org/blob/38b0cda4ad0f2a43662aea94f026decf8a5b4990/docs/general/server/media/excluding-directory.md#excluding-whole-directories
    is_ignored = any(
        path == directory or path.startswith(directory + "/")
        for directory in ignored_dirs
    )
    if is_ignored:
        _mark("dot_ignore", path, affected, folders)
    return is_extra, is_ignored


def _mark_video(
    path: str,
    video_folders: dict[str, list[str]],
    stacked: set[str],
    multiversion: set[str],
    affected: dict[str, set[str]],
    folders: dict[str, set[str]],
) -> None:
    """Classify one video using naming and folder heuristics."""
    containing = parent(path)
    if path in stacked:
        _mark("stacking", path, affected, folders)
    if path in multiversion:
        _mark("multiversion", path, affected, folders)
    # MovieResolver.cs:252-280 processes a folder's direct video children.
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Server.Implementations/Library/Resolvers/Movies/MovieResolver.cs#L252-L280
    peers = video_folders[containing]
    if (
        path in peers
        and len(peers) > 1
        and path not in stacked
        and path not in multiversion
    ):
        _mark("multiple_videos_per_folder", path, affected, folders)
    # Root-level videos use their filenames: MovieResolver.cs:287-308.
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Server.Implementations/Library/Resolvers/Movies/MovieResolver.cs#L287-L308
    if not containing:
        _mark("library_root_video", path, affected, folders, "")
    # Movies docs, Organization: one title folder below library root.
    # https://github.com/jellyfin/jellyfin.org/blob/38b0cda4ad0f2a43662aea94f026decf8a5b4990/docs/general/server/media/movies.md#organization
    if len(path.split("/")) - 1 > 1:
        _mark("nested_video", path, affected, folders)
    # Year parser: CleanDateTimeParser.cs:38-50; defaults in NamingOptions.cs:146-150.
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Naming/Video/CleanDateTimeParser.cs#L38-L50
    name = Path(path).stem
    if not YEAR_RE.search(name) or DATE_ONLY_RE.match(name):
        _mark("missing_year_or_date_title", path, affected, folders)
    # Parsed provider tags: MovieResolver.cs:372-402 (TMDb, TVDb, IMDb).
    # https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Emby.Server.Implementations/Library/Resolvers/Movies/MovieResolver.cs#L372-L402
    if PROVIDER_RE.search(path):
        _mark("provider_id_tags", path, affected, folders)


def _mark_sidecars(
    videos_by_folder: dict[str, list[str]],
    nonvideo_files: set[str],
    affected: dict[str, set[str]],
    folders: dict[str, set[str]],
) -> None:
    """Mark videos sharing a folder with non-video sidecars."""
    # Movie folder docs allow NFO, artwork, subtitles and other local files.
    # https://github.com/jellyfin/jellyfin.org/blob/38b0cda4ad0f2a43662aea94f026decf8a5b4990/docs/general/server/media/movies.md#organization
    sidecar_folders = {parent(path) for path in nonvideo_files} & set(videos_by_folder)
    for folder in sidecar_folders:
        for path in videos_by_folder[folder]:
            _mark("non_video_sidecars", path, affected, folders, folder)


def _extension_counts(paths: set[str]) -> dict[str, int]:
    """Count non-video files in fixed extension groups."""
    counts = {name: 0 for name in (*EXTENSION_GROUPS, "other")}
    for path in paths:
        ext = extension(path)
        group = next(
            (name for name, values in EXTENSION_GROUPS.items() if ext in values),
            "other",
        )
        counts[group] += 1
    return counts


def _prepare_video_groups(
    videos: set[str],
    ignored_dirs: set[str],
    affected: dict[str, set[str]],
    affected_folders: dict[str, set[str]],
) -> VideoGroups:
    """Mark exclusions and prepare the scanned video grouping inputs."""
    extra_videos: set[str] = set()
    ignored_videos: set[str] = set()
    for path in videos:
        is_extra, is_ignored = _mark_extras(
            path,
            Path(path).stem.casefold(),
            parent(path),
            ignored_dirs,
            affected,
            affected_folders,
        )
        if is_extra:
            extra_videos.add(path)
        if is_ignored:
            ignored_videos.add(path)
    scanned_videos = videos - ignored_videos
    groupable_videos = scanned_videos - extra_videos
    scanned_by_folder: dict[str, list[str]] = defaultdict(list)
    groupable_by_folder: dict[str, list[str]] = defaultdict(list)
    for path in scanned_videos:
        scanned_by_folder[parent(path)].append(path)
    for path in groupable_videos:
        groupable_by_folder[parent(path)].append(path)
    return {
        "scanned": scanned_videos,
        "scanned_by_folder": scanned_by_folder,
        "groupable_by_folder": groupable_by_folder,
        "stacked": _stack_members(groupable_videos),
        "multiversion": _multiversion_members(groupable_videos),
        "collisions": _collision_members(groupable_videos),
    }


def census(records: list[tuple[str, str]]) -> CensusResult:
    """Summarize naming mismatch classes without opening the listed paths."""
    files = {path for kind, path in records if kind == "f"}
    folders_on_disk = {path for kind, path in records if kind == "d"}
    videos = {path for path in files if extension(path) in VIDEO_EXTENSIONS}
    ignored_dirs = {parent(path) for path in files if Path(path).name == ".ignore"}
    ignored_dirs.update(path for path in folders_on_disk if f"{path}/.ignore" in files)
    affected: dict[str, set[str]] = defaultdict(set)
    affected_folders: dict[str, set[str]] = defaultdict(set)
    groups = _prepare_video_groups(videos, ignored_dirs, affected, affected_folders)
    for path in groups["scanned"]:
        _mark_video(
            path,
            groups["groupable_by_folder"],
            groups["stacked"],
            groups["multiversion"],
            affected,
            affected_folders,
        )
    _mark_sidecars(
        groups["scanned_by_folder"], files - videos, affected, affected_folders
    )
    for path in groups["collisions"]:
        _mark("clean_title_year_collision", path, affected, affected_folders)
    labels = (
        "extra_folder",
        "extra_suffix",
        "sample_marker",
        "dot_ignore",
        "stacking",
        "multiversion",
        "multiple_videos_per_folder",
        "library_root_video",
        "nested_video",
        "missing_year_or_date_title",
        "clean_title_year_collision",
        "non_video_sidecars",
        "provider_id_tags",
    )
    classes = {
        label: {"videos": len(affected[label]), "folders": len(affected_folders[label])}
        for label in labels
    }
    max_depth = max((len(path.split("/")) for _, path in records), default=0)
    return {
        "classes": classes,
        "totals": {
            "videos": len(videos),
            "folders": len(folders_on_disk),
            "non_video_files_by_extension_group": _extension_counts(files - videos),
            "max_depth": max_depth,
        },
    }


def _case(name: str, records: list[tuple[str, str]], label: str, expected: int) -> None:
    """Assert the expected video count for one synthetic case."""
    got = census(records)["classes"][label]["videos"]
    if got != expected:
        raise AssertionError(name)


SELF_TEST_CASES = [
    (
        "extra-folder-positive",
        [("d", "film"), ("d", "film/scenes"), ("f", "film/scenes/one.mp4")],
        "extra_folder",
        1,
    ),
    (
        "extra-folder-negative",
        [("d", "film"), ("f", "film/one.mp4")],
        "extra_folder",
        0,
    ),
    (
        "extra-folder-boundary",
        [("d", "film"), ("d", "film/scenes2"), ("f", "film/scenes2/one.mp4")],
        "extra_folder",
        0,
    ),
    ("suffix-positive", [("f", "film/a-scene.mp4")], "extra_suffix", 1),
    ("suffix-negative", [("f", "film/a-feature.mp4")], "extra_suffix", 0),
    ("suffix-boundary", [("f", "film/a-sceneinside.mp4")], "extra_suffix", 0),
    ("suffix-numbered-positive", [("f", "film/a-scene2.mp4")], "extra_suffix", 1),
    ("extra-suffix-space-negative", [("f", "film/X trailer.mkv")], "extra_suffix", 0),
    (
        "extra-suffix-dash-space-positive",
        [("f", "film/X - trailer.mkv")],
        "extra_suffix",
        1,
    ),
    (
        "extra-suffix-numbered-case-positive",
        [("f", "film/X-TRAILER2.mkv")],
        "extra_suffix",
        1,
    ),
    ("extra-suffix-other-positive", [("f", "film/X-other.mkv")], "extra_suffix", 1),
    (
        "sample-positive",
        [("d", "film"), ("f", "film/sample.mp4")],
        "sample_marker",
        1,
    ),
    (
        "sample-negative",
        [("d", "film"), ("f", "film/sample-video.mp4")],
        "sample_marker",
        0,
    ),
    (
        "sample-boundary",
        [("d", "film"), ("f", "film/sampleish1.mp4")],
        "sample_marker",
        0,
    ),
    (
        "sample-numbered-positive",
        [("d", "film"), ("f", "film/sample1.mp4")],
        "sample_marker",
        1,
    ),
    ("sample-space-negative", [("f", "film/X sample.mkv")], "sample_marker", 0),
    ("sample-underscore-positive", [("f", "film/X_sample.mkv")], "sample_marker", 1),
    (
        "ignore-positive",
        [("d", "film"), ("f", "film/.ignore"), ("f", "film/video.mp4")],
        "dot_ignore",
        1,
    ),
    ("ignore-negative", [("d", "film"), ("f", "film/video.mp4")], "dot_ignore", 0),
    (
        "ignore-boundary",
        [
            ("d", "film"),
            ("d", "film/ignored"),
            ("d", "film/other"),
            ("f", "film/ignored/.ignore"),
            ("f", "film/other/video.mp4"),
        ],
        "dot_ignore",
        0,
    ),
    (
        "stack-positive",
        [("f", "Film cd1.mkv"), ("f", "Film cd2.mkv")],
        "stacking",
        2,
    ),
    ("stack-negative", [("f", "Film.mkv"), ("f", "Other.mkv")], "stacking", 0),
    (
        "stack-boundary-cd1-cd10",
        [("f", "Film cd1.mkv"), ("f", "Film cd10.mkv")],
        "stacking",
        2,
    ),
    (
        "stack-no-separator-negative",
        [("f", "film/abcd1.mkv"), ("f", "film/abcd2.mkv")],
        "stacking",
        0,
    ),
    (
        "stack-letter-positive",
        [("f", "film/ZQ-cda.mkv"), ("f", "film/ZQ-cdb.mkv")],
        "stacking",
        2,
    ),
    (
        "stack-closing-bracket-positive",
        [("f", "film/ZQ (2020)cd1.mkv"), ("f", "film/ZQ (2020)cd2.mkv")],
        "stacking",
        2,
    ),
    (
        "version-positive",
        [
            ("d", "Film"),
            ("f", "Film/Film - 720p.mkv"),
            ("f", "Film/Film - 1080p.mkv"),
        ],
        "multiversion",
        2,
    ),
    (
        "version-negative",
        [("d", "Film"), ("f", "Film/Film.mkv"), ("f", "Film/Other.mkv")],
        "multiversion",
        0,
    ),
    (
        "version-boundary",
        [("d", "Film"), ("f", "Film/Film - 720p.mkv")],
        "multiversion",
        0,
    ),
    (
        "main-plus-trailer-not-multiversion",
        [
            ("d", "A"),
            ("f", "A/A (2020).mkv"),
            ("f", "A/A (2020)-trailer.mkv"),
        ],
        "multiversion",
        0,
    ),
    (
        "main-plus-trailer-no-collision",
        [
            ("d", "A"),
            ("f", "A/A (2020).mkv"),
            ("f", "A/A (2020)-trailer.mkv"),
        ],
        "clean_title_year_collision",
        0,
    ),
    (
        "main-plus-trailer-not-multiple-videos",
        [
            ("d", "B"),
            ("f", "B/A (2020).mkv"),
            ("f", "B/A (2020)-trailer.mkv"),
        ],
        "multiple_videos_per_folder",
        0,
    ),
    (
        "stacked-with-trailer",
        [
            ("d", "No"),
            ("f", "No/No (2012) part1.mp4"),
            ("f", "No/No (2012) part2.mp4"),
            ("f", "No/No (2012) part1-trailer.mp4"),
            ("f", "No/No (2012)-trailer.mp4"),
        ],
        "stacking",
        2,
    ),
    (
        "two-mains-still-multiversion",
        [
            ("d", "A"),
            ("f", "A/A - 1080p.mkv"),
            ("f", "A/A - 720p.mkv"),
        ],
        "multiversion",
        2,
    ),
    (
        "ignored-not-collision",
        [
            ("d", "library"),
            ("d", "library/ignored"),
            ("f", "library/ignored/.ignore"),
            ("f", "library/ignored/Film (2020).mkv"),
            ("f", "library/Film (2020).mkv"),
        ],
        "clean_title_year_collision",
        0,
    ),
    (
        "mixed-positive",
        [("d", "f"), ("f", "f/a.mkv"), ("f", "f/b.mkv")],
        "multiple_videos_per_folder",
        2,
    ),
    (
        "mixed-negative",
        [("d", "f"), ("f", "f/a.mkv")],
        "multiple_videos_per_folder",
        0,
    ),
    (
        "mixed-boundary",
        [("d", "f"), ("f", "f/a.mkv"), ("f", "f/a-cd1.mkv"), ("f", "f/a-cd2.mkv")],
        "multiple_videos_per_folder",
        1,
    ),
    ("root-positive", [("f", "a.mkv")], "library_root_video", 1),
    ("root-negative", [("d", "a"), ("f", "a/a.mkv")], "library_root_video", 0),
    (
        "root-boundary",
        [("d", "a"), ("d", "a/b"), ("f", "a/b/a.mkv")],
        "library_root_video",
        0,
    ),
    (
        "depth-positive",
        [("d", "one"), ("d", "one/two"), ("f", "one/two/a.mkv")],
        "nested_video",
        1,
    ),
    ("depth-negative", [("d", "one"), ("f", "one/a.mkv")], "nested_video", 0),
    (
        "depth-boundary-limit-one",
        [("d", "one"), ("f", "one/a.mkv")],
        "nested_video",
        0,
    ),
    ("year-positive-missing", [("f", "Film.mp4")], "missing_year_or_date_title", 1),
    (
        "year-negative-present",
        [("f", "Film (2024).mp4")],
        "missing_year_or_date_title",
        0,
    ),
    (
        "year-boundary-date-only",
        [("f", "2024-03-02.mp4")],
        "missing_year_or_date_title",
        1,
    ),
    (
        "collision-positive",
        [("f", "Film (2024) 1080p.mkv"), ("f", "Film (2024) 720p.mkv")],
        "clean_title_year_collision",
        2,
    ),
    (
        "collision-negative",
        [("f", "Film A (2024).mkv"), ("f", "Film B (2024).mkv")],
        "clean_title_year_collision",
        0,
    ),
    (
        "collision-boundary",
        [("f", "Film (2024).mkv"), ("f", "Film (2025).mkv")],
        "clean_title_year_collision",
        0,
    ),
    (
        "sidecar-positive",
        [("d", "Film"), ("f", "Film/a.mkv"), ("f", "Film/a.nfo")],
        "non_video_sidecars",
        1,
    ),
    (
        "sidecar-negative",
        [("d", "Film"), ("f", "Film/a.mkv")],
        "non_video_sidecars",
        0,
    ),
    (
        "sidecar-boundary",
        [
            ("d", "Film"),
            ("d", "Film/side"),
            ("f", "Film/a.mkv"),
            ("f", "Film/side/a.nfo"),
        ],
        "non_video_sidecars",
        0,
    ),
    (
        "provider-positive",
        [("f", "Film [imdbid-tt123].mkv")],
        "provider_id_tags",
        1,
    ),
    ("provider-negative", [("f", "Film (2024).mkv")], "provider_id_tags", 0),
    ("provider-boundary", [("f", "Film imdbid-tt123.mkv")], "provider_id_tags", 0),
]


def self_test() -> None:
    """Run synthetic class cases and process-boundary privacy checks."""
    try:
        for name, records, label, expected in SELF_TEST_CASES:
            _case(name, records, label, expected)
        _privacy_test()
    except (AssertionError, ValueError, OSError, subprocess.SubprocessError) as error:
        name = (
            str(error)
            if isinstance(error, AssertionError)
            else "self-test-internal-error"
        )
        print(f"self-test failed: {name}", file=sys.stderr)
        raise SystemExit(1) from None
    print(f"ok   naming_census self-test ({len(SELF_TEST_CASES)} cases plus privacy)")


def _privacy_test() -> None:
    """Verify that input path tokens never appear in process output."""
    tokens = [secrets.token_hex(16) for _ in range(4)]
    inputs = (
        (f"f\t{tokens[0]}/{tokens[1]}.mkv\0d\t{tokens[2]}\0".encode(), 0, b""),
        (
            f"f\t{tokens[3]}.mkv\0{tokens[1]}-malformed\0".encode(),
            2,
            b"record 2: missing type separator\n",
        ),
    )
    for input_data, expected_exit, expected_error in inputs:
        try:
            result = subprocess.run(
                [sys.executable, "-I", sys.argv[0]],
                input=input_data,
                capture_output=True,
                check=False,
                timeout=PRIVACY_TIMEOUT_SECONDS,
            )
        except subprocess.TimeoutExpired:
            raise AssertionError("privacy-check") from None
        rendered = result.stdout + result.stderr
        if (
            result.returncode != expected_exit
            or result.stderr != expected_error
            or any(token.encode() in rendered for token in tokens)
        ):
            raise AssertionError("privacy-check")


def main() -> int:
    """Run the self-test or emit a privacy-preserving census as JSON."""
    if sys.argv[1:] == ["--self-test"]:
        self_test()
        return 0
    if sys.argv[1:]:
        print("usage: naming_census.py [--self-test]", file=sys.stderr)
        return 2
    try:
        result = census(parse_records(sys.stdin.buffer.read()))
    except ValueError as error:
        print(str(error), file=sys.stderr)
        return 2
    except OSError:
        print("input read error", file=sys.stderr)
        return 2
    sys.stdout.write(json.dumps(result, ensure_ascii=True, sort_keys=True) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
