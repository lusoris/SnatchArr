<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: CC-BY-SA-4.0
-->
<!-- markdownlint-disable MD013 -->

# ADR-0045: Quality Availability, Profile Mismatch and Split-Quality Series

- **Status**: Accepted (concept only; open questions `OQ-0045-1` to `OQ-0045-4` answered by the operator on 2026-10-29, implementation follows in the slices below)
- **Date**: 2026-10-29
- **Deciders**: lusoris
- **Related**: [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md), [ADR-0019](0019-foreign-title-search.md), [ADR-0024](0024-watch-aware-modes.md), [ADR-0035](0035-jellyfin-integration.md), [ADR-0036](0036-community-requests-and-voting.md), [ADR-0038](0038-vmafx-library-quality-scoring.md), issue #36

## Context

**SnatchArr** (meaning: an automated media-snatching coordinator for `*arr` instances; origin: English *snatch* + the `*arr` suffix of the Servarr ecosystem; pronunciation: `/ˈsnætʃ.ɑːr/`) searches missing and cutoff-unmet items and leaves the choice of quality to the quality profile of each `*arr` instance. Three gaps follow, reported by the operator (paraphrased): the request portal shows no available qualities, so a request can target a quality that does not exist; long-running series end up split across qualities; nothing detects or corrects either.

1. **Seerr has no quality view.** **Seerr** (meaning: media request and discovery portal that unifies Overseerr and Jellyseerr; origin: *seer* + `*arr`; pronunciation: `/ˈsɪər/`, "seer") stores only an availability status per title and per season, twice: `status` and `status4k` (`server/entity/Media.ts:137,141` and `server/entity/Season.ts:22,25` at `seerr-team/seerr` `v3.5.0`, commit `e2f24cb46079`), with values `UNKNOWN`, `PENDING`, `PROCESSING`, `PARTIALLY_AVAILABLE`, `AVAILABLE`, `DELETED`. A request carries `is4k`, `serverId`, `profileId` and `rootFolder` (`POST /api/v1/request`, `PUT /api/v1/request/{requestId}` in `seerr-api.yml`). Nothing tells the requester which resolutions exist on disk or on the indexers.
2. **Sonarr profiles are per series, not per season or episode.** `SeriesResource` holds one `qualityProfileId`; `SeasonResource` has none (Sonarr `v4.0.20.3014`, `src/Sonarr.Api.V3/openapi.json`). When a profile changes during a run, when `upgradeAllowed` is false, or when early episodes only exist at a lower resolution, one series holds several qualities and nothing reports it. Each file still reports its own `quality` (`id`, `name`, `source`, `resolution`), `qualityCutoffNotMet`, `customFormatScore` and `mediaInfo` (`EpisodeFileResource`, `MovieFileResource`).
3. **SnatchArr sees only what `/wanted/cutoff` lists.** The worker pages `/wanted/missing` and `/wanted/cutoff` (`worker/crates/snatch-worker/src/executor.rs`, `fetch_candidates`); the API reads per-file scores only for queue decisions (`api/internal/arrclient/queue.go`, `sonarrFileScore`, `radarrFileScore`). A cutoff that no indexer can meet is searched again every cycle and burns the hourly cap (#180 counting rules) without a result, and a series whose files all sit inside the profile but at different resolutions never shows up at all.

### Data sources, pinned

Every pin below was read on 2026-10-29. The Sonarr, Radarr and Seerr spec files are the ones `tools/upstreamspec/specs.yaml` already pins through `golusoris/goenvoy` commit `8c974b72b957`; their sha256 values were recomputed against the downloaded files and match.

| Source | Version and commit | What this ADR reads | Write paths considered |
| :--- | :--- | :--- | :--- |
| Sonarr (`Sonarr/Sonarr`) | `v4.0.20.3014`, `cab419ade8ac`, spec sha256 `03108f7f6b20…` | `GET /api/v3/qualityprofile` (`cutoff`, `items[].allowed`, `upgradeAllowed`, `minFormatScore`, `cutoffFormatScore`); `GET /api/v3/episodefile?seriesId=` (`quality`, `qualityCutoffNotMet`, `customFormatScore`, `mediaInfo.resolution`, `mediaInfo.videoDynamicRangeType`); `GET /api/v3/history/series` (`eventType`, `quality`, `qualityCutoffNotMet`) | `POST /api/v3/command` `EpisodeSearch` / `SeasonSearch` (existing, budget-debited); `PUT /api/v3/series/{id}` (`qualityProfileId`) |
| Radarr (`Radarr/Radarr`) | `v6.4.4.10685`, `a96bf7c3ecb2`, spec sha256 `95ea90624851…` | `GET /api/v3/qualityprofile`; `GET /api/v3/moviefile?movieId=`; `GET /api/v3/history/movie` | `MoviesSearch` command (existing); `PUT /api/v3/movie/{id}` (`qualityProfileId`) |
| Seerr (`seerr-team/seerr`) | spec `v3.5.0`, `e2f24cb46079`, sha256 `9533fc4ce04a…`; latest release `v3.5.1` (`564bfb08b54c`) | `GET /api/v1/request` (`is4k`, `serverId`, `profileId`); `GET /api/v1/movie/{id}`, `GET /api/v1/tv/{id}` (`mediaInfo.status`, season status); `GET /api/v1/service/sonarr/{id}` and `/service/radarr/{id}` (profile names per server) | `PUT /api/v1/request/{requestId}` (`is4k`, `serverId`, `profileId`) while a request is pending |
| Indexers via the `*arr` instance | as above | `GET /api/v3/release?episodeId=` / `?movieId=` returns `ReleaseResource` with `quality`, `rejected`, `rejections`, `customFormatScore` | none: `POST /api/v3/release` (grab) is never used by this feature |
| Prowlarr (`Prowlarr/Prowlarr`) | latest release `v2.7.0.5723`, `0dc20d8495ba`; client `goenvoy/arr/prowlarr/v2` `v2.1.6` | not read by this feature (see Alternatives) | none |
| Jellyfin (`jellyfin/jellyfin`) | latest release `v12.4`, `83d0d0e7f76e`; client `goenvoy/mediaserver/jellyfin/v3` `v3.3.0` | `MediaStream.Width`, `Height`, `BitDepth`, `GetVideoColorRange()` (`MediaBrowser.Model/Entities/MediaStream.cs:465,519,525,806`) through the item's `MediaSources` | none |

`goenvoy/mediaserver/jellyfin/v3` `v3.3.0` does not model `MediaSources` or `MediaStreams` (`types.go`); reading them needs a goenvoy addition first (upstream issue, see Follow-up slices). The indexer row is a live search on every call: `GET /api/v3/release` makes the `*arr` instance query every enabled indexer, so it counts against the hourly cap exactly like an `EpisodeSearch` and is covered by `OQ-0045-1`.

## Decision

### 1. One quality tier per file

SnatchArr normalises every `*arr` quality to a **tier** = (`resolution`, source class). Source classes collapse the per-application enums: Sonarr `QualitySource` (`television`, `televisionRaw`, `web`, `webRip`, `dvd`, `bluray`, `blurayRaw`) and Radarr `QualitySource` (`cam` … `bluray`) map to `disc`, `web`, `broadcast`, `low` and `unknown`. The ordering inside a tier comes from the profile's own `items` order, never from a SnatchArr ranking, so a profile that prefers `WEBDL-1080p` over `Bluray-1080p` keeps that preference. The mapping is a pure function in `snatch-core` (Rust) with a Go twin generated from one table, property-tested both ways (`HISS-19`: one table, two consumers).

### 2. Availability per title, season and episode

For each tracked unit (movie; series → season → episode) SnatchArr keeps three facts:

| Fact | Source | Freshness |
| :--- | :--- | :--- |
| **present** tier | `episodefile` / `moviefile` `quality`; cross-checked against `mediaInfo.resolution`, and against the media server's stream width and height when Jellyfin or Emby is linked | refreshed on the existing sync cadence and on `history` `downloadFolderImported` events |
| **requested** tier set | the instance quality profile (`allowed` items, `cutoff`, `upgradeAllowed`); for Seerr-routed titles also `is4k`, `serverId` and `profileId` of the request | on profile change (Configarr re-import, ADR-0003) and on Seerr sync |
| **obtainable** tier set | passive evidence: `history` `grabbed` and `downloadFailed` events, SnatchArr's own search outcome log (search dispatched, nothing grabbed); optional active evidence per `OQ-0045-1` | each search cycle; evidence expires with the processed-memory `expires_at` of the entity |

A series view aggregates episodes into a per-season histogram of present tiers; a title view shows present, requested and obtainable side by side. These are what the request flow ([ADR-0036](0036-community-requests-and-voting.md)) and the Jellyfin surface ([ADR-0035](0035-jellyfin-integration.md)) show before a request is sent, which closes the "Seerr shows no qualities" gap for every request that goes through SnatchArr.

### 3. Mismatch classes

| Class | Condition | Example |
| :--- | :--- | :--- |
| `cutoff_unobtainable` | the cutoff tier was searched at least `N` cycles (default 6, per entity) and every grab or result in the evidence window lies below it | profile cutoff `Bluray-2160p` for a 1990s TV series that was never released in 2160p |
| `request_route_mismatch` | Seerr request `is4k` disagrees with the `is4k` flag of the server it was sent to, or `profileId` is not a profile of that server | 4K request that ended on the standard Sonarr |
| `present_outside_profile` | the present file's quality is not `allowed` in the current profile | profile changed from 720p to 1080p after import |
| `upgrade_blocked` | `qualityCutoffNotMet` is true and `upgradeAllowed` is false | file stays below cutoff forever |
| `split_series` | see section 4 | seasons 1-3 in 720p, seasons 4-9 in 1080p |

Classes are derived, never stored as truth: a nightly recompute from the facts in section 2 replaces them, so a fixed profile clears its own finding.

### 4. Split-quality series detection

A season or series is **split** when its present-tier histogram holds more than one tier. Two shapes are kept apart because their fixes differ:

- **season boundary split**: every season is uniform but seasons differ (typical for long-running series whose early seasons only exist at a lower resolution);
- **scattered split**: tiers differ inside one season (typical for single-episode grabs and later partial upgrades).

A split becomes a finding only above the tolerance of `OQ-0045-2`. Specials (season 0) are excluded by default. The detector is a pure function over the histogram (no I/O) and is property-tested with generated histograms, including the boundary of exactly one minority episode.

### 5. Correction flow: proposals first, observe mode first

Each finding produces a **proposal** with one or more actions. Nothing changes until the proposal is applied; the default mode is `observe` per instance, as with queue management ([ADR-0012](0012-queue-management-replaces-cleanuparr-link.md)).

| Action | Applies to | Write | Who confirms |
| :--- | :--- | :--- | :--- |
| `upgrade_search` for the lagging episodes | `split_series` (scattered), `upgrade_blocked` after a profile fix | `EpisodeSearch` through the existing lease, debited per #180 | per `OQ-0045-3` |
| `season_search` | `split_series` with a minority share above the tolerance in one season | `SeasonSearch`, debited as one search | per `OQ-0045-3` |
| `stop_searching` the unobtainable cutoff | `cutoff_unobtainable` | SnatchArr-side only: the entity leaves `SnatchUpgrade` selection until new evidence (a grab or result at the cutoff tier) appears | automatic, shown as a notice |
| `change_profile` of the series or movie | `cutoff_unobtainable`, `present_outside_profile`, `upgrade_blocked` | `PUT /api/v3/series/{id}` or `/movie/{id}` `qualityProfileId` | admin, always |
| `fix_request` | `request_route_mismatch` while the Seerr request is pending | `PUT /api/v1/request/{requestId}` (`is4k`, `serverId`, `profileId`) | admin, always |
| `accept` | any | none; the finding is muted until the facts change | admin |

Rules:

- Writes go only through `*arr` and Seerr APIs; SnatchArr never touches files or download clients ([ADR-0006](0006-download-client-backpressure.md)).
- Instances with `source=configarr` own their profiles: `change_profile` on such an instance is refused with a pointer to the Configarr file, matching the read-only rule for Configarr rows (ADR-0003).
- A requester (role `requester`, [ADR-0026](0026-accounts-and-roles.md)) sees findings for their own requests and may propose `fix_request`; applying it stays with an admin.
- Proposals that replace a higher tier with a lower one to make a series uniform are not generated (`OQ-0045-4`).
- Every applied action emits an event (`quality_proposal_applied`, lower snake case per the SSE naming rule) with the acting user, and every automatic one names the rule that allowed it.
- Once [ADR-0038](0038-vmafx-library-quality-scoring.md) scoring exists, two files of the same tier are compared by their pooled VMAFx score before an `upgrade_search` is proposed, so a tier-equal but visibly worse file is the first candidate.

### 6. Placement

- **API (Go)** owns the facts, the findings and the proposals (new tables `quality_facts`, `quality_findings`, `quality_proposals`, each keyed by instance × entity with `expires_at`), the nightly recompute as a `leader_loops` member, and the HTTP surface under `/api/v1/quality/…` in `api/openapi/openapi.yaml`.
- **Worker (Rust)** keeps doing the searches it already does; `upgrade_search` and `season_search` reach it as ordinary leased runs. `snatch-core` gains the tier mapping and the split detector as pure functions.
- **Web** gains a quality column and histogram on the instance and request views, and a proposals list under the activity view ([ADR-0029](0029-task-flow-ui.md)).
- The feature is one module switch, `quality_insight`, off by default ([ADR-0025](0025-module-switches.md)).

### 7. Limits that bind the build

- The concept unit that adds this ADR runs no live search and no grab, and moves no favourites into downloads.
- Every later slice tests `*arr` and Seerr writes only against the dev compose stack and the fixture stack; no slice runs `GET /api/v3/release` against a live instance before `OQ-0045-1` is answered.
- `POST /api/v3/release` (grab a chosen release) is out of scope for every slice.

## Open questions

All four were answered on 2026-10-29 with the recommended option (fleet answer ledger ids `snatcharr-oq-0045-1` to `snatcharr-oq-0045-4`):

- `OQ-0045-1` = b: passive evidence by default; the per-instance release lookup is opt-in, off by default, debited against the hourly cap, at most one lookup per entity per evidence window. The limits of section 7 stand.
- `OQ-0045-2` = c: an admin threshold per instance, defaulting to a minority share of at least 20 % in a season or any season boundary split.
- `OQ-0045-3` = b: after observe mode, `upgrade_search` and `season_search` run automatically within the hourly cap; `change_profile` and `fix_request` always need an admin.
- `OQ-0045-4` = a: no downgrade proposals; only upgrade the lagging part or accept the split.

| ID | Question | Options | Recommended |
| :--- | :--- | :--- | :--- |
| `OQ-0045-1` | Where does "obtainable" evidence come from? | a) passive only: `history` and SnatchArr's own search outcomes (risk: a quality that exists on the indexers but was never grabbed stays unknown) b) passive, plus an opt-in per instance interactive lookup (`GET /api/v3/release`), debited against the hourly cap, at most one lookup per entity per evidence window (risk: spends cap on lookups that grab nothing) c) lookup on every request and every finding (risk: cap exhaustion on large libraries) | b |
| `OQ-0045-2` | When is a split a finding? | a) any second tier (risk: noise from one odd episode) b) fixed rule: minority share at least 20 % in a season, or any season boundary split (risk: wrong for some libraries) c) admin threshold per instance, default as in b (risk: one more setting) | c |
| `OQ-0045-3` | What may run without a confirmation? | a) nothing: an admin confirms every proposal (risk: proposals pile up) b) in-profile searches (`upgrade_search`, `season_search`) run automatically within the hourly cap once the instance leaves observe mode; profile and request changes always need an admin (risk: cap spent on upgrades the admin would have skipped) c) everything automatic after observe mode (risk: profile changes without a human) | b |
| `OQ-0045-4` | May a proposal downgrade higher-tier files to make a series uniform? | a) never; only upgrade the lagging part or accept the split (risk: a series stays mixed when the higher tier cannot be completed) b) offer it as an admin-only action (risk: deletes better files on a mis-click) | a |

## Consequences

### Positive

- Requesters and admins see per title, season and episode which tiers exist, are wanted and were seen on the indexers, which Seerr does not show.
- Searches for a cutoff no indexer offers stop instead of draining the hourly cap every cycle.
- Split series become visible with a concrete fix per shape.

### Negative

- Three new tables and a nightly recompute; `episodefile` reads per series grow with library size (bounded per cycle like `maxFileLookups` in `api/internal/arrclient/queue.go`).
- Profile changes through `PUT /api/v3/series/{id}` overlap with Configarr; the refusal rule above keeps one owner but adds a message path.
- The media-server cross-check depends on a goenvoy release that models `MediaStreams`.

### Neutral

- Lidarr, Readarr forks and Whisparr follow later; their file resources carry `quality` too, but split detection is defined here for series and films only.

## Alternatives considered

- **Show qualities inside Seerr.** Seerr has no extension point for per-quality availability and no field to store it; a fork is out of scope. SnatchArr shows it in its own request view and through the Jellyfin surface instead.
- **Query indexers directly through Prowlarr (`/api/v1/search`).** Gives raw results without the instance's profile, rejections and custom-format scores, and still spends the indexer cap. The `*arr` release lookup returns the same results already judged against the profile.
- **Trust `/wanted/cutoff` alone.** It cannot see splits inside the allowed set and cannot tell "not yet found" from "does not exist".

## Verification plan (`HISS-15`, `HISS-20`)

- `snatch-core`: property tests for the tier mapping (every enum value of both pinned specs maps; unknown maps to `unknown`) and for the split detector (uniform → none; one minority episode below tolerance → none; at tolerance → finding; season boundary → boundary shape).
- Go: table tests for each mismatch class with positive, negative and boundary rows; a planted defect per class (for example the `upgradeAllowed` check inverted) must turn the suite red.
- Contract: the new read operations (`/qualityprofile`, `/episodefile`, `/moviefile`, `/history/series`, `/history/movie`, Seerr `/service/*`) join `tools/upstreamspec/specs.yaml`, so `make contracts-verify` checks them against the pinned specs.
- End-to-end on the fixture stack: a fake Sonarr serving a 720p/1080p split series and a fake Seerr with a 4K request routed to a standard server produce exactly the expected findings; applying `fix_request` writes only to the fake Seerr.

## Follow-up slices

1. Facts and tier mapping (read only): reads, `quality_facts`, the per-season histogram in the UI.
2. Findings and the split detector, observe mode only.
3. Proposals and the in-profile actions (`upgrade_search`, `season_search`, `stop_searching`).
4. Admin-only actions (`change_profile`, `fix_request`) and the requester view.
5. Media-server cross-check after the goenvoy `MediaStreams` addition; obtainable lookup per `OQ-0045-1`.
6. VMAFx tie-break once [ADR-0038](0038-vmafx-library-quality-scoring.md) scoring ships.

## References

- Operator direction of 2026-10-29 (paraphrased): Seerr shows no available qualities, so requests can target the wrong quality; long-running series can be split across qualities; detect and correct this automatically.
- Issue #36: policy filters on tags, quality profiles and post-release delay (the profile filter is where findings re-enter selection).
- Pinned specs: `tools/upstreamspec/specs.yaml`, `golusoris/goenvoy` `upstream/specs.yaml` at `8c974b72b957`.
