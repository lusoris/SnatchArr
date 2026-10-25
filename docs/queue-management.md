<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Queue management

SnatchArr can clean up the download queue of each Sonarr, Radarr, Lidarr and Readarr-fork instance: downloads that stall, crawl, fail, cannot import or are no longer wanted are removed through the *arr queue API, and the item falls back into the wanted list for SnatchArr to search again under the hourly cap. It never talks to a download client itself ([ADR-0006](adr/0006-download-client-backpressure.md)); the design is [ADR-0012](adr/0012-queue-management-replaces-cleanuparr-link.md).

## Start here

1. Queue management is **off** for every instance until you turn it on in the instance's snatch policy.
2. When on, every rule starts in **observe** mode. An item that breaks a rule gets a strike; at the threshold SnatchArr emits `queue_item_struck` with `would_remove=true` and deletes nothing.
3. Watch the struck items in the instance view for a while. When a rule only flags downloads you want gone, set that rule to **act**. From then on, reaching the threshold removes the item with `DELETE .../queue/{id}?removeFromClient=true&skipRedownload=true&blocklist=<per rule>`.

`skipRedownload=true` keeps the *arr from searching on its own; SnatchArr's planner searches the item again within the instance's hourly cap.

## Never touched

A queue item gets no strike while any of these hold (`api/internal/queue/sweep.go`, `isNeverTouched`):

- `status` is `queued`, `paused`, `delay` or `downloadClientUnavailable`;
- `trackedDownloadState` is `importing`, `imported` or `ignored`;
- the item was `added` less than `grace_seconds` ago (default 15 minutes, at most 24 hours);
- queue management is off for the instance.

An instance kind whose adapter returns `ErrQueueUnsupported` for the queue is skipped, and the skip is counted in the queue metrics (`api/internal/queue/service.go`). No kind is skipped today: Whisparr v2 and v3 queues are read through goenvoy whisparr 1.5.0 (`api/internal/arrclient/queue.go`). Tests: `TestEveryRuleNegative` (its "never touched" cases), `TestGraceProtectsNewItem`, `TestUnsupportedKindSkipped`.

## States

Each sweep checks an item against the rules in this order and gives it at most one strike, for the first rule it breaks. A sweep in which the item breaks no rule resets its count to zero.

| Rule | Class | Action | Blocklist | Re-search | Defaults |
| :--- | :--- | :--- | :--- | :--- | :--- |
| stalled | - | remove | yes | no | observe, threshold 6 |
| metadata_stuck | - | remove | yes | no | observe, threshold 180 minutes of age |
| slow | - | remove | yes | no | observe, threshold 6 |
| failed | - | remove | yes | yes | observe, threshold 2 |
| id_matched | - | force-import | never removes | no | observe, threshold 1 |
| import_fixable | fixable | force-import | never removes | no | observe, threshold 1 |
| import_broken | broken | remove | yes | yes | observe, threshold 2 |
| import_not_wanted | not_wanted | remove | yes | no | observe, threshold 1 |
| import_blocked | unknown | observe only (never removes) | yes (no effect) | no | observe, threshold 6 |
| unwanted_score | - | remove | yes | no | observe, threshold 1 |
| not_an_upgrade | - | remove | no | no | observe, threshold 1 |
| duplicate | - | remove | no | no | observe, threshold 1 |
| unwanted_unmonitored | - | remove | no | no | observe, threshold 1 |

Each rule carries its own `blocklist` switch next to `mode` and `threshold` (the Blocklist column in the instance view). Left out of a write, the rule keeps the default from the table; reads always return the effective value. Switching it off removes the item without a blocklist entry, so the release may be grabbed again; switching it on for not an upgrade, duplicate or unwanted (unmonitored) blocklists the release as well. Tests: `TestRuleBlocklistSetting` and `TestRuleBlocklistDefaultsMatchTable` in `api/internal/queue/rules_test.go`.

The state tests live in `api/internal/queue/states_test.go` and `api/internal/queue/reason_rules_test.go`. Each has `positive/`, `negative/` and `boundary/` subtests: the threshold minus one strike removes nothing, the threshold strike removes, and a clean sweep resets the count.

### Reason classifier and precedence

For import-blocked and import-failed items, SnatchArr evaluates the item's status messages through a pure classifier (`api/internal/queue/classify.go`) against an allow-list of anchored, case-insensitive patterns derived from upstream *arr sources.

When an item carries several status lines, precedence resolves as:
1. `unknown` / operational lines win over everything (never delete when e.g. "Not enough free space", path or permission lines are present).
2. `broken`
3. `not_wanted`
4. `fixable`

A line that matches no pattern and is not the item's own release title resolves to `unknown`. An item with zero lines resolves to `unknown`.

Taxonomy overrides:
- "No files found are eligible for import" is `broken` only after `grace_seconds` and when no "still being unpacked" or "Locked file" line is present; otherwise `unknown`.
- "Unable to determine if file is a sample" is `unknown`.
- "Episode/Movie file already imported at" is `not_wanted` (no re-search).
- "File is suspected multi-part" is `broken`.

### Match percentage floors

Lidarr and Readarr distance messages ("Album match is not close enough: P% vs 80%", "Book match ...", "Worst track match: P% vs 60%", "Track match is not close enough: P% vs 60%") have their percentage parsed (accepting both period and comma decimal points).

Per instance, four floors govern classification:
- `match_fixable_min_percent` (album/book match, default 75%): match $\ge$ floor $\rightarrow$ `fixable`.
- `match_broken_below_percent` (default 40%): reserved for rematch fallback in subsequent release. In this release, distance messages never yield `broken`; values below the fixable floor yield `unknown` with sub-reason `rematch_pending`.
- `track_fixable_min_percent` (track match, default 50%): worst track match $\ge$ floor $\rightarrow$ `fixable`.
- `track_broken_below_percent` (default 40%): reserved for rematch fallback in subsequent release.

Floor constraints: 0..100, with fixable floor strictly greater than broken floor. Tune these floors per instance based on the observe report.

### Stalled

Zero progress is stalled, never slow, even when a speed floor is set. A `sizeleft` that grew also counts. The first sighting of an item gives no strike, because there is no earlier `sizeleft` to compare, and neither does `sizeleft = 0`. The stalled rule applies to torrents only; usenet clients queue jobs serially and report waiting jobs as downloading, and their own failure handling covers dead jobs.

### Metadata stuck

A torrent whose client never fetched the metadata of its magnet link. It matches a torrent item (protocol `torrent`, or no protocol reported) that is either `queued` with a status message containing `downloading metadata`, or in warning with a message containing `cannot resolve magnet link`; these are the messages Sonarr's qBittorrent client reports with DHT on and off. Since the messages may be localized, a torrent with a total size of 0 that is `queued` or in warning matches too. That fallback also matches a magnet that the client holds back behind its active-torrent limit before any metadata arrives; the age threshold and the observe default bound that case.

Unlike the other rules, the threshold counts minutes, not strikes: the item is removed (with the client data, and blocklisted by default) once `now - added` reaches it. The range is 30 to 1440 minutes, the default 180. An item without an `added` time never reaches the threshold: it is struck and reported, never removed. Queued items are normally never touched; a metadata-stuck match is the exception. Tests: `api/internal/queue/metadata_stuck_test.go`.

### Slow

The rate is `(previous sizeleft - sizeleft) / seconds since the previous sweep`. A rate equal to `min_bytes_per_second` is not slow; it has to be below. `min_bytes_per_second = 0`, the default, turns the rule off.

### Failed

Either field alone is enough, compared without regard to case. A failed item that is also `queued` or `paused` stays untouched.

Sonarr and Radarr call the states `failed` and `failedPending`. Lidarr and the Readarr family call them `downloadFailed` and `downloadFailedPending`. All four count as `failed`. `importFailed` leaves `checkFailed` and is evaluated by the reason classifier alongside `importBlocked` and `importPending` with warning.

`trackedDownloadStatus=error` on its own is not a failure. Sonarr and Radarr set it only when they fail a download, together with the state `failedPending`, which the rule already matches; Lidarr and Readarr never set it (`failedErrorStatusCases`).

Removals triggered by `failed` (and `import_broken`) set `skipRedownload=false` so the *arr re-searches the media.

### Import-fixable, import-broken, import-not-wanted, import-blocked

- `import_fixable`: class `fixable`. Attempts manual import via `ForceImport` (subject to `max_imports_per_sweep` bound). On Sonarr, Radarr, Lidarr, and Readarr, candidates qualify if mapped and all rejections classify as `fixable`. On Whisparr, `ForceImport` returns `ErrQueueUnsupported` and the item is reported as `WouldImport` with `import_refused`, never removed.
- `import_broken`: class `broken`. Removes the item with `blocklist=true` and `skipRedownload=false` (re-search).
- `import_not_wanted`: class `not_wanted`. Removes the item with `blocklist=true` and `skipRedownload=true` (no re-search).
- `import_blocked`: class `unknown`. Strikes the item for reporting (records `Decision.Unknown = true` and `Pattern`), but **never removes** or blocklists, even when configured in `act` mode.

### ID-matched

The *arr matched such a release to its series or movie by grab history but cannot parse its title, for example a release named after the episode title. A manual import clears it, and the id-matched rule sends that import itself ([ADR-0013](adr/0013-force-import-id-matched-releases.md)). It covers Sonarr and Radarr, whose manual-import candidates carry the mapping, and needs a `downloadId`.

- The rule reads `GET /api/v3/manualimport?downloadId=…&filterExistingFiles=true` and keeps only candidates without a rejection that are mapped: a series and at least one episode on Sonarr, a movie on Radarr. It sends those with `POST /api/v3/command` (`name=ManualImport`, `importMode=auto`), each call under the import timeout of 120 seconds rather than the 15-second client timeout, because the *arr scans the download's files to list the candidates; Lidarr took 39 to 48 seconds for an album download (`api/internal/arrclient/forceimport.go`, `TestForceImport`, `TestForceImportTimeout`).
- A download without such a candidate is refused: nothing is sent, the item stays for you, and SnatchArr reports `queue_item_import_failed` with `no_clean_candidate`. The rule never removes or blocklists.
- In observe mode, the default, the strike event carries `would_import=true` and nothing is sent. In act mode each sweep imports at most `max_imports_per_sweep` downloads (default 25); further items wait for the next sweep. A season pack lists one item per episode under one `downloadId` and costs one import (`TestIDMatchedImportBound`, `TestIDMatchedSeasonPackImportsOnce`).
- The command runs asynchronously, so the next sweep checks the result. A download still ID-matched one sweep after its import is reported once as `queue_item_import_failed` with `still_id_matched`. No download is imported again until 24 hours after its last import, when its strike row would expire (`TestIDMatchedReadBack`, `TestServiceForceImportSent`).
- In observe mode the sweep in which a download's strike reaches the threshold logs `queue: would force import` (`instance`, `download_id`, `title`) and counts `snatcharr_queue_imports_total{result="would_import"}`, once per download. The first sweep of a strike under any rule logs `queue: item struck` (`instance`, `rule`, `download_id`, `title`). Tests: `api/internal/queue/shadow_test.go`.
- The parity week of [ADR-0014](adr/0014-shadow-parity-week-for-id-matched.md) compares those lines with the cluster unblocker's: `go run ./tools/idmatchparity -day 2026-10-19 -snatcharr api.log -unblocker unblocker.log`, where `api.log` holds the api's JSON lines and `unblocker.log` the unblocker runs' `kubectl logs --timestamps`. It prints one row (`matched`, `skipped`, `unseen`, `mismatches`, `verdict`) and one line per mismatch, and exits 1 on a red day.
- A failed command call (`error`) leaves no import stamp, so the next sweep tries again (`TestServiceForceImportSendErrorRetries`).
- `make arr-forceimport-live` proves the path on real Sonarr and Radarr started for the test, never on a live stack: each grabs a release by ID under a title it cannot parse, and the production client imports it and reads the queue back (`TestLiveForceImport` in `api/internal/arrclient/forceimport_live_test.go`, harness `tools/e2e/forceimport_live.sh`). A file the *arr rejects, such as one without an audio track or, in a download of several files, one it maps to an episode the series lacks, is never sent. CI runs it as the job `arr-forceimport-live`.

### Unwanted

A score equal to `min_custom_format_score` is wanted; it has to be below. Leaving `min_custom_format_score` unset, the default, turns the score rule off. An unmonitored item is removed without a blocklist entry, so the release stays available if the item is monitored again. When an item breaks an earlier rule as well, that rule wins: a failed, unmonitored download is struck as failed.

### Not an upgrade

The rule compares the download's `customFormatScore` with the score of the file already on disk for the same episode or movie. A score equal to the file's score is no upgrade; it has to be above. The comparison covers custom-format scores only, the way the cron script it replaces did: a download the *arr grabbed for a better quality but a lower custom-format score is struck too, so watch the rule in observe mode before you set it to act. It removes without a blocklist entry by default, because the release itself is not defective.

The file score comes from extra reads during the queue read (`api/internal/arrclient/queue.go`, `withFileScores`): Sonarr's `/api/v3/episodefile/{id}` for the queue item's `episode.episodeFileId`, and Radarr's `/api/v3/moviefile/{id}` for `movie.movieFileId`, because the movie the queue embeds carries no custom-format score. One queue read makes at most 50 file reads, each with the client timeout, and reads each file once. A file past that bound, a failed read and a file without a score leave the score unknown, and an unknown score gives no strike. Lidarr and Readarr have no single file per queue item; the rule never strikes there. Tests: `TestQueueFileScores`, `TestQueueFileScoreLookupBound`.

### Duplicate

The sweep groups the instance's queue items by media item: the episode for Sonarr, the movie for Radarr, the album for Lidarr and the book for Readarr (`Item.MediaID`, `api/internal/arrclient/queue.go`). In a group with downloads from two or more `downloadId`s, downloads are ranked by:
1. `customFormatScore` (higher wins)
2. `QualityRank` (higher wins; 0 when unknown / unranked)
3. `Added` (older grab / earlier added time wins)
4. Queue `ID` (lower id wins)

Note: goenvoy does not expose quality profile ordering for any *arr kind, so `QualityRank` defaults to 0 and ties fall back to `Added` and `ID`.

A season pack lists one queue item per episode under one `downloadId`: its items never strike each other, and the pack is struck as a whole once it loses any one of its episodes to a better download, as the cron script did. Items without a media id are never grouped. The rule removes without a blocklist entry by default, because the release itself is not defective. Tests: `TestStateDuplicate`, `TestDuplicateTieHierarchy`, `TestDuplicateObserveDefault` in `api/internal/queue/duplicate_test.go`, `TestQueueMediaID` in `api/internal/arrclient/queue_filescore_test.go`.

## Strikes, bounds and events

- Strike rows live in the `queue_strikes` table, one per instance and `downloadId`, with the rule, count, last `sizeleft`, last seen time, the last force import (`imported_at`, `import_refused`) and `expires_at` (last seen plus 24 hours). The API is their only writer; rows of items that left the queue expire.
- The sweep runs every 10 minutes on the elected leader (`leader_loops`). Per instance it reads at most 10 pages of 200 items plus at most 50 file scores, and removes at most `max_removals_per_sweep` items (default 10); the rest wait for the next sweep without an extra strike (`TestRemovalBoundExhaustion`, `TestMaxRemovalsBound`).
- A removal that fails keeps the row and is retried next sweep without counting twice (`TestFailedDeleteKeepsRowAndRetries`).
- Events on the SSE stream: `queue_item_struck` (`rule`, `count`, `threshold`, `mode`, `would_remove`, `would_import`), `queue_item_removed` (`rule`, `blocklist`), `queue_item_imported` and `queue_item_import_failed` (detail `no_clean_candidate`, `still_id_matched` or `error`). The metric `snatcharr_queue_imports_total{arr_instance,result}` counts the same import results, with `ok` for a sent import and `would_import` for an observed one.

## Settings

Per instance, in the snatch policy (`QueuePolicy` in `api/openapi/openapi.yaml`, bounds in `Settings.Validate`, `api/internal/queue/rules.go`):

| Setting | Default | Range |
| :--- | :--- | :--- |
| `enabled` | `false` | |
| `rules.<rule>.mode` | `observe` | `observe` or `act` |
| `rules.<rule>.threshold` | per state, see the table above | 1 to 100 |
| `grace_seconds` | 900 | 0 to 86400 |
| `min_bytes_per_second` | 0 (slow rule off) | 0 or more |
| `min_custom_format_score` | unset (score rule off) | any integer |
| `match_fixable_min_percent` | 75 | 0 to 100 |
| `match_broken_below_percent` | 40 | 0 to 100 |
| `track_fixable_min_percent` | 50 | 0 to 100 |
| `track_broken_below_percent` | 40 | 0 to 100 |
| `max_removals_per_sweep` | 10 | 1 to 100 |
| `max_imports_per_sweep` | 25 | 1 to 100 |

## Cleanuparr

Queue management is the precondition for retiring a separate [Cleanuparr](https://github.com/Cleanuparr/Cleanuparr) deployment. SnatchArr's read-only Cleanuparr link is already gone (ADR-0012); nothing is removed from your deployment. Do not let both tools remove items from the same *arr instance: turn Cleanuparr's queue cleaner off for an instance before you set its SnatchArr rules to act.
