# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Set up the real Sonarr and Radarr of tools/e2e/forceimport_live.sh and grab one release each.

    python3 -I arr_setup.py WORK PLANT

WORK holds the API keys (sonarr.key, radarr.key) and the qBittorrent password (qbit.pass),
and the .torrent files under WORK/dl/torrents. Each app gets a root folder, qBittorrent as
download client and one library entry (Breaking Bad, tvdb 81189; The Matrix, tmdb 603). The
release push names the item by ID under a title the parser cannot match, so grab history is
the only link: the queue item blocks with the ID-match note. Writes WORK/live.json, the input
of TestLiveForceImport, with the planted case of PLANT (second-line, skip) applied.
"""

import json
import os
import sys
import time
import urllib.error
import urllib.request

SONARR = f"http://127.0.0.1:{os.environ.get('SONARR_PORT', '18201')}"
RADARR = f"http://127.0.0.1:{os.environ.get('RADARR_PORT', '18202')}"
QBIT_PORT = int(os.environ.get("QBITTORRENT_PORT", "18203"))
SONARR_TITLE = "Totally Unrelated Show S01E02 1080p WEB-DL x264-SNATCH"
RADARR_TITLE = "Ganz Anderer Film 1999 1080p WEB-DL x264-SNATCH"
EXTRA_LINE = "Not an upgrade for existing episode file(s)"


class SetupError(Exception):
    """A step failed; the __main__ entry point prints it and exits non-zero."""


def call(base, key, method, path, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(
        base + path,
        data=data,
        method=method,
        headers={"X-Api-Key": key, "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.loads(resp.read() or b"null")
    except urllib.error.HTTPError as err:
        raise SetupError(
            f"forceimport-live: {method} {path}: HTTP {err.code}: {err.read()[:400]!r}"
        )


def wait_ready(base, key):
    req = urllib.request.Request(
        base + "/api/v3/system/status", headers={"X-Api-Key": key}
    )
    for _ in range(90):
        try:
            with urllib.request.urlopen(req, timeout=5):
                return
        except (urllib.error.URLError, OSError):
            time.sleep(1)
    raise SetupError(f"forceimport-live: {base} did not serve its API within 90 s")


def download_client(qbit_pass, category_field, category):
    fields = [
        {"name": "host", "value": "qbittorrent"},
        {"name": "port", "value": QBIT_PORT},
        {"name": "username", "value": "admin"},
        {"name": "password", "value": qbit_pass},
        {"name": category_field, "value": category},
    ]
    return {
        "name": "qbittorrent",
        "implementation": "QBittorrent",
        "configContract": "QBittorrentSettings",
        "protocol": "torrent",
        "enable": True,
        "priority": 1,
        "removeCompletedDownloads": False,
        "removeFailedDownloads": False,
        "fields": fields,
    }


def setup_sonarr(key, qbit_pass):
    call(SONARR, key, "POST", "/api/v3/rootfolder", {"path": "/downloads/library/tv"})
    call(
        SONARR,
        key,
        "POST",
        "/api/v3/downloadclient",
        download_client(qbit_pass, "tvCategory", "tv-sonarr"),
    )
    series = call(SONARR, key, "GET", "/api/v3/series/lookup?term=tvdb:81189")[0]
    series.update(
        qualityProfileId=1,
        rootFolderPath="/downloads/library/tv",
        monitored=True,
        seasonFolder=True,
        addOptions={
            "monitor": "all",
            "searchForMissingEpisodes": False,
            "searchForCutoffUnmetEpisodes": False,
        },
    )
    call(SONARR, key, "POST", "/api/v3/series", series)


def setup_radarr(key, qbit_pass):
    call(
        RADARR, key, "POST", "/api/v3/rootfolder", {"path": "/downloads/library/movies"}
    )
    call(
        RADARR,
        key,
        "POST",
        "/api/v3/downloadclient",
        download_client(qbit_pass, "movieCategory", "radarr"),
    )
    movie = call(RADARR, key, "GET", "/api/v3/movie/lookup/tmdb?tmdbId=603")
    movie.update(
        qualityProfileId=1,
        rootFolderPath="/downloads/library/movies",
        monitored=True,
        minimumAvailability="released",
        addOptions={"searchForMovie": False},
    )
    call(RADARR, key, "POST", "/api/v3/movie", movie)


def push(base, key, title, torrent, ids):
    # No size: the push would be refused as smaller than the quality's minimum.
    release = {
        "title": title,
        "downloadUrl": f"http://files:8080/torrents/{torrent}",
        "protocol": "torrent",
        "publishDate": "2026-10-18T00:00:00Z",
        **ids,
    }
    for _ in range(30):
        out = call(base, key, "POST", "/api/v3/release/push", release)
        out = out[0] if isinstance(out, list) else out
        if out.get("approved"):
            return
        # The series or movie metadata may still be refreshing right after the add.
        time.sleep(2)
    raise SetupError(
        f"forceimport-live: {base} refused the push of {title}: {out.get('rejections')}"
    )


def wait_blocked(base, key, download_id):
    lines = []
    for _ in range(60):
        for rec in call(base, key, "GET", "/api/v3/queue?pageSize=50")["records"]:
            if rec.get("downloadId", "").lower() != download_id:
                continue
            lines = [
                m for s in rec.get("statusMessages", []) for m in s.get("messages", [])
            ]
            if rec.get("trackedDownloadState") == "importBlocked":
                return
        time.sleep(2)
    raise SetupError(
        f"forceimport-live: {base} queue shows no importBlocked item for {download_id}: {lines}"
    )


def main():
    work, plant = sys.argv[1], sys.argv[2]

    def read(name):
        with open(os.path.join(work, name), encoding="utf-8") as f:
            return f.read().strip()

    keys = {"sonarr": read("sonarr.key"), "radarr": read("radarr.key")}
    qbit_pass = read("qbit.pass")
    hashes = {"sonarr": read("sonarr.hash"), "radarr": read("radarr.hash")}
    wait_ready(SONARR, keys["sonarr"])
    wait_ready(RADARR, keys["radarr"])
    setup_sonarr(keys["sonarr"], qbit_pass)
    setup_radarr(keys["radarr"], qbit_pass)
    push(SONARR, keys["sonarr"], SONARR_TITLE, "sonarr.torrent", {"tvdbId": 81189})
    push(RADARR, keys["radarr"], RADARR_TITLE, "radarr.torrent", {"tmdbId": 603})
    wait_blocked(SONARR, keys["sonarr"], hashes["sonarr"])
    wait_blocked(RADARR, keys["radarr"], hashes["radarr"])
    cases = []
    for kind, base in (("sonarr", SONARR), ("radarr", RADARR)):
        case = {
            "kind": kind,
            "url": base,
            "api_key": keys[kind],
            "download_id": hashes[kind],
            "files": 1,
        }
        if plant == "second-line":
            case["extra_line"] = EXTRA_LINE
        if plant == "skip":
            case["skip_import"] = True
        cases.append(case)
    fd = os.open(
        os.path.join(work, "live.json"), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600
    )
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump(cases, f)


if __name__ == "__main__":
    try:
        main()
    except SetupError as err:
        sys.exit(str(err))
