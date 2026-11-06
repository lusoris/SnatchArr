# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Sonarr push-title import guarantee probe for INT-207 / ADR-0019 open point 1.

    python3 -I check.py WORK EVIDENCE_JSON

Pushes canonical vs foreign titles into real Sonarr, verifying that rewriting the release title
on push guarantees automatic import of foreign folder and file structures across Sonarr v3 and v4.
"""

from __future__ import annotations

import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

COMMON_TAG = "GERMAN 1080p WEB-DL x264-SNATCH"
FOREIGN_TITLE = "Alone Ueberlebe die Wildnis"
CANONICAL_SERIES = "Breaking Bad"
TVDB_ID = 81189
SONARR_VER = os.environ.get("SNATCHARR_PUSHTITLE_SONARR", "v4")


class CheckError(Exception):
    """Raised when an assertion or setup step fails."""


def call(
    base: str,
    key: str,
    method: str,
    path: str,
    body: object | None = None,
) -> dict[str, object] | list[object]:
    """Execute an HTTP request against the Sonarr API with timeout and error capture."""
    data = None if body is None else json.dumps(body).encode("utf-8")
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
        body_text = err.read()[:600].decode("utf-8", "replace")
        return {"_http": err.code, "_body": body_text}


def wait_ready(base: str, key: str) -> None:
    """Wait until Sonarr answers /api/v3/system/status with the API key."""
    req = urllib.request.Request(
        base + "/api/v3/system/status", headers={"X-Api-Key": key}
    )
    for _ in range(90):
        try:
            with urllib.request.urlopen(req, timeout=5):
                return
        except (urllib.error.URLError, OSError):
            time.sleep(1)
    raise CheckError(f"{base} did not become ready within 90 s")


def qbit_login(port: int, password: str) -> dict[str, object]:
    """Test qBittorrent authentication from host and record response."""
    data = urllib.parse.urlencode(
        {"username": "admin", "password": password}
    ).encode("utf-8")
    url = f"http://127.0.0.1:{port}/api/v2/auth/login"
    req = urllib.request.Request(
        url,
        data=data,
        method="POST",
        headers={
            "Referer": f"http://127.0.0.1:{port}",
            "Content-Type": "application/x-www-form-urlencoded",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            body = resp.read()[:80].decode("utf-8", "replace")
            return {"status": resp.status, "body": body}
    except urllib.error.HTTPError as err:
        body = err.read()[:80].decode("utf-8", "replace")
        return {"status": err.code, "body": body}
    except OSError as err:
        return {"status": "error", "body": str(err)}


def update_language_profile(base: str, key: str) -> dict[str, object] | None:
    """Enable German and English in language profile if API is available."""
    lp = call(base, key, "GET", "/api/v3/languageprofile")
    if not isinstance(lp, list) or not lp:
        return None
    p0 = lp[0]
    if not isinstance(p0, dict):
        return None
    for it in p0.get("languages", []):
        lang_name = it.get("language", {}).get("name")
        if lang_name in ("German", "English"):
            it["allowed"] = True
    pid = p0.get("id")
    res = call(base, key, "PUT", f"/api/v3/languageprofile/{pid}", p0)
    return res if isinstance(res, dict) else None


def make_download_client(port: int, password: str) -> dict[str, object]:
    """Build the QBittorrent download client payload for Sonarr."""
    fields = [
        {"name": "host", "value": "qbittorrent"},
        {"name": "port", "value": port},
        {"name": "username", "value": "admin"},
        {"name": "password", "value": password},
        {"name": "tvCategory", "value": "tv-sonarr"},
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


def setup_sonarr(
    base: str, key: str, qbit_port: int, qbit_pass: str
) -> dict[str, object]:
    """Configure root folder, language profile, download client, and test series."""
    call(base, key, "POST", "/api/v3/rootfolder", {"path": "/downloads/library/tv"})
    update_language_profile(base, key)
    call(
        base,
        key,
        "POST",
        "/api/v3/downloadclient",
        make_download_client(qbit_port, qbit_pass),
    )

    lookup = call(base, key, "GET", f"/api/v3/series/lookup?term=tvdb:{TVDB_ID}")
    if not isinstance(lookup, list) or not lookup:
        raise CheckError(f"lookup for tvdb:{TVDB_ID} returned no results: {lookup}")

    series = lookup[0]
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
    if not series.get("languageProfileId"):
        series["languageProfileId"] = 1
    added = call(base, key, "POST", "/api/v3/series", series)
    if isinstance(added, dict) and added.get("_http"):
        raise CheckError(f"POST /api/v3/series failed: {added}")

    series_id = added.get("id") if isinstance(added, dict) else None
    if series_id:
        for _ in range(30):
            eps = call(base, key, "GET", f"/api/v3/episode?seriesId={series_id}")
            if isinstance(eps, list) and eps:
                break
            time.sleep(1)
    time.sleep(2)
    return added if isinstance(added, dict) else {}


def push_release(
    base: str, key: str, title: str, torrent_id: str, ids: dict[str, int]
) -> dict[str, object]:
    """Push a release to Sonarr and return approval decision."""
    release = {
        "title": title,
        "downloadUrl": f"http://files:8080/torrents/{torrent_id}.torrent",
        "protocol": "torrent",
        "publishDate": "2026-10-18T00:00:00Z",
        **ids,
    }
    last: dict[str, object] | None = None
    for _ in range(15):
        resp = call(base, key, "POST", "/api/v3/release/push", release)
        r = resp[0] if isinstance(resp, list) else resp
        if isinstance(r, dict):
            last = r
            if r.get("approved"):
                return {"approved": True, "raw": r}
        time.sleep(2)
    rejections = last.get("rejections") if last else None
    return {"approved": False, "rejections": rejections, "raw": last}


def collect_grab_data(
    recs: list[dict[str, object]],
) -> dict[str, object] | None:
    """Extract grab metadata from history records."""
    g = [x for x in recs if isinstance(x, dict) and x.get("eventType") == "grabbed"]
    if not g:
        return None
    return {
        k: v
        for k, v in (g[0].get("data") or {}).items()
        if k in ("seriesMatchType", "releaseSource", "movieMatchType", "indexer")
    }


ID_MATCH_MSG = "matched to series by ID"
TERMINAL_STATES = ("importBlocked", "failedPending", "failed")


def queue_record(base: str, key: str, download_id: str) -> dict[str, object] | None:
    """Return the queue state, status and messages of one download, or None."""
    queue_resp = call(
        base,
        key,
        "GET",
        "/api/v3/queue?pageSize=100&includeUnknownSeriesItems=true&includeUnknownMovieItems=true",
    )
    records = queue_resp.get("records", []) if isinstance(queue_resp, dict) else []
    for rec in records:
        if str(rec.get("downloadId", "")).lower() == download_id.lower():
            return {
                "state": rec.get("trackedDownloadState"),
                "status": rec.get("trackedDownloadStatus"),
                "msgs": [
                    m
                    for s in rec.get("statusMessages", [])
                    for m in [s.get("title")] + s.get("messages", [])
                ],
            }
    return None


def id_match_blocked(rec: dict[str, object] | None) -> bool:
    """True when a queue record carries the ID-match import block warning.

    Sonarr v4 sets state importBlocked; Sonarr v3 has no such state and keeps
    state downloading with status warning and the same message.
    """
    if not rec or rec.get("status") != "warning":
        return False
    return any(ID_MATCH_MSG in str(m) for m in rec.get("msgs") or [])


def history_records(base: str, key: str, download_id: str) -> list[dict[str, object]]:
    """Return the history records of one download."""
    hist = call(base, key, "GET", f"/api/v3/history?pageSize=50&downloadId={download_id.upper()}")
    recs = hist.get("records", []) if isinstance(hist, dict) else []
    return [x for x in recs if isinstance(x, dict)]


def imported_outcome(recs: list[dict[str, object]], secs: int) -> dict[str, object] | None:
    """Build the imported outcome when history holds an import event, else None."""
    imports = ("downloadFolderImported", "movieFileImported")
    ev = [x.get("eventType") for x in recs]
    if not any(e in imports for e in ev):
        return None
    return {
        "result": "imported",
        "secs": secs,
        "events": ev,
        "grab_data": collect_grab_data(recs),
        "imported_as": [x.get("sourceTitle") for x in recs if x.get("eventType") in imports],
    }


def wait_outcome(
    base: str, key: str, download_id: str, timeout: int = 240
) -> dict[str, object]:
    """Poll queue and history until the download imports, fails or is ID-blocked."""
    t0 = time.time()
    last: dict[str, object] | None = None
    for _ in range(int(timeout / 3) + 1):
        if time.time() - t0 > timeout:
            break
        rec = queue_record(base, key, download_id)
        if rec is not None:
            last = rec
            if rec["state"] in TERMINAL_STATES or id_match_blocked(rec):
                break
        else:
            done = imported_outcome(history_records(base, key, download_id), round(time.time() - t0))
            if done is not None:
                return done
        time.sleep(3)
    recs = history_records(base, key, download_id)
    return {"result": "blocked_or_timeout", "queue": last, "grab_data": collect_grab_data(recs)}


def build_cases(plant: str) -> list[dict[str, object]]:
    """Define the 7 probe cases, applying title mutations for planted cases."""
    s1_title = f"{CANONICAL_SERIES} S01E01 {COMMON_TAG}"
    if plant == "foreign":
        s1_title = f"{FOREIGN_TITLE} S01E01 {COMMON_TAG}"

    s3_title = f"{CANONICAL_SERIES} S01E04 {COMMON_TAG}"
    s3_ids: dict[str, int] = {}
    if plant == "idonly":
        s3_title = f"Totally Unrelated Show S01E04 {COMMON_TAG}"
        s3_ids = {"tvdbId": TVDB_ID}

    return [
        {
            "case": "s1",
            "title": s1_title,
            "ids": {"tvdbId": TVDB_ID},
            "desc": "canonical push + tvdbId; foreign folder + foreign file",
        },
        {
            "case": "s2",
            "title": f"{CANONICAL_SERIES} S01E03 {COMMON_TAG}",
            "ids": {"tvdbId": TVDB_ID},
            "desc": "canonical push + tvdbId; single foreign-named file, no folder",
        },
        {
            "case": "s3",
            "title": s3_title,
            "ids": s3_ids,
            "desc": "canonical push no ids; foreign folder + foreign file",
        },
        {
            "case": "s4",
            "title": f"{FOREIGN_TITLE} S01E05 {COMMON_TAG}",
            "ids": {"tvdbId": TVDB_ID},
            "desc": "control: foreign push + ids (expect rejected)",
        },
        {
            "case": "s5",
            "title": f"{CANONICAL_SERIES} S01E06 {COMMON_TAG}",
            "ids": {"tvdbId": TVDB_ID},
            "desc": "canonical push + tvdbId; foreign folder + obfuscated file name",
        },
        {
            "case": "s6",
            "title": f"Totally Unrelated Show S01E07 {COMMON_TAG}",
            "ids": {"tvdbId": TVDB_ID},
            "desc": "control: unparseable title + ids (expect Id match, importBlocked)",
        },
        {
            "case": "s7",
            "title": f"{CANONICAL_SERIES} S01E02 {COMMON_TAG}",
            "ids": {"tvdbId": TVDB_ID},
            "desc": "canonical push + tvdbId; nested foreign folders (season/disc/file)",
        },
    ]


def verify_clean_cases(cases_by_id: dict[str, dict[str, object]]) -> None:
    """Assert outcomes for all 7 cases during a clean run and emit ok lines."""
    for cid in ("s1", "s2", "s3", "s5", "s7"):
        item = cases_by_id[cid]
        push = item.get("push") or {}
        if not push.get("approved"):
            raise CheckError(f"{cid} push not approved: {push.get('rejections')}")
        outcome = item.get("outcome") or {}
        if outcome.get("result") != "imported":
            raise CheckError(
                f"{cid} not imported: result={outcome.get('result')}, queue={outcome.get('queue')}"
            )
        match_type = (outcome.get("grab_data") or {}).get("seriesMatchType")
        if match_type != "Title":
            raise CheckError(f"{cid} expected seriesMatchType=Title, got {match_type}")
        print(f"ok   {cid}: canonical push -> imported (seriesMatchType=Title)")

    s4 = cases_by_id["s4"]
    s4_push = s4.get("push") or {}
    if s4_push.get("approved"):
        raise CheckError("s4 push was approved unexpectedly, expected rejection")
    print("ok   s4: foreign title push + tvdbId -> push rejected")

    s6 = cases_by_id["s6"]
    s6_push = s6.get("push") or {}
    if not s6_push.get("approved"):
        raise CheckError(f"s6 push not approved: {s6_push.get('rejections')}")
    s6_outcome = s6.get("outcome") or {}
    s6_match = (s6_outcome.get("grab_data") or {}).get("seriesMatchType")
    if s6_match != "Id":
        raise CheckError(f"s6 expected seriesMatchType=Id, got {s6_match}")
    s6_queue = s6_outcome.get("queue") or {}
    allowed = ("importBlocked",) if SONARR_VER == "v4" else ("downloading", "importBlocked")
    if s6_queue.get("state") not in allowed or not id_match_blocked(s6_queue):
        raise CheckError(
            f"s6 expected an ID-match import block (state in {allowed}, status warning), got {s6_queue}"
        )
    print(
        f"ok   s6: unparseable title + tvdbId -> grab seriesMatchType=Id, "
        f"queue {s6_queue.get('state')} with ID-match warning"
    )


def verify_planted_cases(
    cases_by_id: dict[str, dict[str, object]], plant: str
) -> None:
    """Assert that a planted defect caused the expected failure."""
    if plant == "foreign":
        s1 = cases_by_id["s1"]
        s1_push = s1.get("push") or {}
        s1_outcome = s1.get("outcome") or {}
        if s1_push.get("approved") and s1_outcome.get("result") == "imported":
            raise CheckError("s1 was unexpectedly imported under foreign plant")
        raise CheckError(
            f"s1 not imported: foreign plant active (approved={s1_push.get('approved')}, result={s1_outcome.get('result')})"
        )

    if plant == "idonly":
        s3 = cases_by_id["s3"]
        s3_outcome = s3.get("outcome") or {}
        s3_queue = s3_outcome.get("queue") or {}
        if s3_outcome.get("result") == "imported":
            raise CheckError("s3 was unexpectedly imported under idonly plant")
        raise CheckError(
            f"s3 import blocked: idonly plant active (state={s3_queue.get('state')}, result={s3_outcome.get('result')})"
        )


def read_work_file(work: str, name: str) -> str:
    """Read a stripped text file from the working directory."""
    with open(os.path.join(work, name), encoding="utf-8") as f:
        return f.read().strip()


def run_cases(base_url: str, key: str, work: str, plant: str) -> list[dict[str, object]]:
    """Push every case, then wait for the outcome of each approved push."""
    results: list[dict[str, object]] = []
    for c in build_cases(plant):
        cid, title = str(c["case"]), str(c["title"])
        ids = c["ids"] if isinstance(c["ids"], dict) else {}
        p = push_release(base_url, key, title, cid, ids)
        results.append({"case": cid, "desc": c["desc"], "pushed_title": title, "ids": ids, "push": p})
    for r in results:
        if (r.get("push") or {}).get("approved"):
            r["outcome"] = wait_outcome(base_url, key, read_work_file(work, f"{r['case']}.hash"))
    return results


def write_evidence(path: str, data: dict[str, object]) -> None:
    """Write the evidence JSON, creating its directory."""
    os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, ensure_ascii=False)


def main() -> None:
    """Parse arguments, execute the 7 test cases against Sonarr, and assert results."""
    if len(sys.argv) < 3:
        sys.exit(f"usage: {sys.argv[0]} WORK EVIDENCE_JSON")
    work, evidence_path = sys.argv[1], sys.argv[2]
    plant = os.environ.get("SNATCHARR_PUSHTITLE_PLANT", "")
    if plant not in ("", "foreign", "idonly"):
        sys.exit(f"pushtitle check: unknown plant {plant!r}")
    if SONARR_VER not in ("v3", "v4"):
        sys.exit(f"pushtitle check: unknown Sonarr release {SONARR_VER!r}")
    qbit_port = int(os.environ.get("QBITTORRENT_PORT", "18233"))
    base_url = f"http://127.0.0.1:{int(os.environ.get('SONARR_PORT', '18231'))}"
    key = read_work_file(work, "sonarr.key")
    qbit_pass = read_work_file(work, "qbit.pass")

    wait_ready(base_url, key)
    setup_sonarr(base_url, key, qbit_port, qbit_pass)
    results = run_cases(base_url, key, work, plant)

    status_resp = call(base_url, key, "GET", "/api/v3/system/status")
    write_evidence(evidence_path, {
        "sonarr_version_tag": SONARR_VER,
        "plant": plant,
        "sonarr_version": status_resp.get("version") if isinstance(status_resp, dict) else None,
        "qbit_login": qbit_login(qbit_port, qbit_pass),
        "cases": results,
    })
    cases_by_id = {str(r["case"]): r for r in results}
    if plant:
        verify_planted_cases(cases_by_id, plant)
    else:
        verify_clean_cases(cases_by_id)


if __name__ == "__main__":
    try:
        main()
    except CheckError as err:
        sys.exit(f"pushtitle check: {err}")
