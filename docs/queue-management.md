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

An instance kind whose queue lacks the fields the rules read (Whisparr today) is skipped, and the skip is counted in the queue metrics (`api/internal/queue/service.go`). Tests: `TestEveryRuleNegative` (its "never touched" cases), `TestGraceProtectsNewItem`, `TestWhisparrSkipped`.

## States

Each sweep checks an item against the rules in this order and gives it at most one strike, for the first rule it breaks. A sweep in which the item breaks no rule resets its count to zero.

| State | Strike when (one sweep) | Default strikes | Default blocklist | Test |
| :--- | :--- | :--- | :--- | :--- |
| stalled | `trackedDownloadState=downloading`, `sizeleft > 0`, and `sizeleft` did not drop since the previous sweep | 6 | yes | `TestStateStalled` |
| slow | `downloading`, `sizeleft` dropped, but the rate since the previous sweep is below `min_bytes_per_second` | 6 | yes | `TestStateSlow` |
| failed | `trackedDownloadState` is `failed`, `failedPending`, `downloadFailed`, `downloadFailedPending` or `importFailed`, or `status=failed` | 2 | yes | `TestStateFailed` |
| import-blocked | `trackedDownloadState=importBlocked`, or `importPending` with `trackedDownloadStatus=warning` | 6 | yes | `TestStateImportBlocked` |
| unwanted (score) | `customFormatScore` below `min_custom_format_score` | 1 | yes | `TestStateUnwantedScore` |
| unwanted (unmonitored) | the item is not monitored | 1 | no | `TestStateUnwantedUnmonitored` |

Each rule carries its own `blocklist` switch next to `mode` and `threshold` (the Blocklist column in the instance view). Left out of a write, the rule keeps the default from the table; reads always return the effective value. Switching it off removes the item without a blocklist entry, so the release may be grabbed again; switching it on for unwanted (unmonitored) blocklists the release as well. Tests: `TestRuleBlocklistSetting` and `TestRuleBlocklistDefaultsMatchTable` in `api/internal/queue/rules_test.go`.

The state tests live in `api/internal/queue/states_test.go`. Each has `positive/`, `negative/` and `boundary/` subtests: the threshold minus one strike removes nothing, the threshold strike removes, and a clean sweep resets the count.

### Stalled

Zero progress is stalled, never slow, even when a speed floor is set. A `sizeleft` that grew also counts. The first sighting of an item gives no strike, because there is no earlier `sizeleft` to compare, and neither does `sizeleft = 0`.

### Slow

The rate is `(previous sizeleft - sizeleft) / seconds since the previous sweep`. A rate equal to `min_bytes_per_second` is not slow; it has to be below. `min_bytes_per_second = 0`, the default, turns the rule off.

### Failed

Either field alone is enough, compared without regard to case. A failed item that is also `queued` or `paused` stays untouched.

Sonarr and Radarr call the states `failed` and `failedPending`. Lidarr and the Readarr family call them `downloadFailed` and `downloadFailedPending`, and add `importFailed`, which marks an import the app gave up on and never retries; all five count as failed. An item in `importBlocked` is still import-blocked on every kind.

`trackedDownloadStatus=error` on its own is not a failure. Sonarr and Radarr set it only when they fail a download, together with the state `failedPending`, which the rule already matches; Lidarr and Readarr never set it (`failedErrorStatusCases`).

### Import-blocked

`importPending` with status `ok` is an import in progress and gets no strike.

An item is also left alone when its only problem is the note "Automatic import is not possible": status `warning`, state `importBlocked` or `importPending`, and every status message line is either that note or the item's own release title. The *arr matched such a release by grab history but cannot parse its title, and a manual import fixes it; an import unblocker job can do that on a schedule, and the sweep must not remove the release first. Any other line, no lines at all, or a status other than `warning` strikes as usual (`TestStateImportBlocked`, `unblocker_` cases). Lidarr and Readarr items with the same note are left alone too and need a manual import.

### Unwanted

A score equal to `min_custom_format_score` is wanted; it has to be below. Leaving `min_custom_format_score` unset, the default, turns the score rule off. An unmonitored item is removed without a blocklist entry, so the release stays available if the item is monitored again. When an item breaks an earlier rule as well, that rule wins: a failed, unmonitored download is struck as failed.

## Strikes, bounds and events

- Strike rows live in the `queue_strikes` table, one per instance and `downloadId`, with the rule, count, last `sizeleft`, last seen time and `expires_at` (last seen plus 24 hours). The API is their only writer; rows of items that left the queue expire.
- The sweep runs every 10 minutes on the elected leader (`leader_loops`). Per instance it reads at most 10 pages of 200 items and removes at most `max_removals_per_sweep` items (default 10); the rest wait for the next sweep without an extra strike (`TestRemovalBoundExhaustion`, `TestMaxRemovalsBound`).
- A removal that fails keeps the row and is retried next sweep without counting twice (`TestFailedDeleteKeepsRowAndRetries`).
- Events on the SSE stream: `queue_item_struck` (`rule`, `count`, `threshold`, `mode`, `would_remove`) and `queue_item_removed` (`rule`, `blocklist`).

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
| `max_removals_per_sweep` | 10 | 1 to 100 |

## Cleanuparr

Queue management is the precondition for retiring a separate [Cleanuparr](https://github.com/Cleanuparr/Cleanuparr) deployment. SnatchArr's read-only Cleanuparr link is already gone (ADR-0012); nothing is removed from your deployment. Do not let both tools remove items from the same *arr instance: turn Cleanuparr's queue cleaner off for an instance before you set its SnatchArr rules to act.
