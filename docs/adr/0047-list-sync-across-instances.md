<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: CC-BY-SA-4.0
-->
<!-- markdownlint-disable MD013 -->

# ADR-0047: List sync across instances

- **Status**: Accepted (open questions OQ-0047-1 to OQ-0047-5 answered by the operator on 2026-11-10; implementation follows in L0–L7)
- **Date**: 2026-11-10
- **Deciders**: lusoris
- **Related**: [ADR-0003](0003-configarr-as-instance-source.md), [ADR-0006](0006-download-client-backpressure.md), [ADR-0008](0008-readarr-frozen-legacy-target.md), [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md), [ADR-0022](0022-instance-priority.md), [ADR-0024](0024-watch-aware-modes.md), [ADR-0036](0036-community-requests-and-voting.md), [ADR-0046](0046-download-client-priority-writes.md)

## Context

**SnatchArr** (meaning: automated media search-and-snatch coordinator across `*arr` instances; origin: English *snatch* + the `*arr` suffix of the Servarr ecosystem; pronunciation: `/ˈsnætʃ.ɑːr/`) already coordinates searches under per-instance hourly caps (`AGENTS.md`, Contracts). Operator direction on 2026-11-10 identifies cross-instance list syncing as a missing capability (paraphrased; ledger `snatcharr-list-sync-gap-20261110`). At baseline commit `021c2d147199`, no ADR, roadmap entry or issue covered it; the issue search on 2026-11-10 at 19:43 CET covered import lists, exclusions, watchlists and source-specific list-sync terms.

### Project terminology

Names already defined in [ADR-0036](0036-community-requests-and-voting.md) retain that record's wording. Pronunciations are reading aids; undocumented upstream origins are stated explicitly.

- **Sonarr** (meaning: automated television series manager; origin: *sonar* + `*arr`; pronunciation: `/ˈsoʊ.nɑːr/`).
- **Radarr** (meaning: automated movie collection manager; origin: *radar* + `*arr`; pronunciation: `/ˈreɪ.dɑːr/`).
- **Lidarr** (meaning: automated music collection manager; origin: *LIDAR* + `*arr`; pronunciation: `/ˈlaɪ.dɑːr/`).
- **Readarr** (meaning: automated book and audiobook manager; origin: *reader* + `*arr`; pronunciation: `/ˈriː.dɑːr/`).
- **Whisparr** (meaning: automated adult video manager in the Servarr stack; origin: *whisper* + `*arr`; pronunciation: `/ˈwɪs.pɑːr/`).
- **Seerr** (meaning: media request and discovery manager; origin: shortening of *Overseerr* and *Jellyseerr*, from English *overseer* with the `*rr` ecosystem suffix; pronunciation: `/ˈsɪər/`).
- **Overseerr** (meaning: Plex media request management UI; origin: *overseer* + `*rr`; pronunciation: `/ˌoʊ.vərˈsɪər/`).
- **Jellyseerr** (meaning: Jellyfin and Emby fork of Overseerr; origin: *Jellyfin* + *Overseerr*; pronunciation: `/ˈdʒɛl.i.sɪər/`).
- **Jellyfin** (meaning: open-source media system; origin: *jellyfish* + *fin*, referring to streaming fins; pronunciation: `/ˈdʒɛl.i.fɪn/`).
- **Pulsarr** (meaning: Plex watchlist-to-`*arr` bridge; origin: *pulsar* + `*arr`; pronunciation: `/ˈpʌl.sɑːr/`).
- **goenvoy** (meaning: typed Go client SDK for `*arr` and media servers; origin: *Go* + *envoy*; pronunciation: `/ˈɡoʊ.ɛn.vɔɪ/`).
- **Configarr** (meaning: configuration reconciler whose import-list definitions are considered here; origin not documented upstream; pronunciation: “config-arr”; source: tagged `importListBase.ts` below).
- **MDBList** (meaning: hosted list service supplying Custom List URLs; origin not documented upstream; pronunciation: “M-D-B-list”; role: D8 and OQ-0047-1).
- **Listrr** (meaning: hosted list service supplying Custom List URLs; origin not documented upstream; pronunciation: “list-er”; role: D8 and OQ-0047-1).
- **Trakt** (meaning: tracking service with collection and watchlist targets; origin not documented upstream; pronunciation: “tracked”; sources: native notification contracts and SDK survey below).
- **Plex** (meaning: media-server and watchlist source; origin not documented upstream; pronunciation: “plex”; source: SDK survey in References).
- **Emby** (meaning: media-server source; origin not documented upstream; pronunciation: “em-bee”; source: SDK survey in References).
- **Letterboxd** (meaning: tracking source considered for list ingestion; origin not documented upstream; pronunciation: “letter-boxed”; source: SDK survey in References).
- **Simkl** (meaning: tracking source available in the SDK survey; origin not documented upstream; pronunciation: “sim-kul”; source: SDK survey in References).
- **Hardcover** (meaning: book source considered for list ingestion; origin not documented upstream; pronunciation: “hard-cover”; scope: OQ-0047-1).
- **ListenBrainz** (meaning: tracking source considered for list ingestion; origin not documented upstream; pronunciation: “listen-brains”; source: SDK survey in References).
- **TMDb** (meaning: video metadata source in the SDK survey; origin not documented upstream; pronunciation: “T-M-D-B”; source: SDK survey in References).
- **TVDb** (meaning: video metadata source in the SDK survey; origin not documented upstream; pronunciation: “T-V-D-B”; source: SDK survey in References).

### Existing boundaries

The existing request sync stays in `api/internal/seerr/sync.go` under ADR-0036. Queue rules and their per-rule `Blocklist` setting live in `api/internal/queue/rules.go`; ADR-0012 owns removal through the `*arr` queue API. ADR-0024 creates exclusions during removal with `addImportListExclusion` or `addImportExclusion`, so exclusions can originate outside a list controller. ADR-0022 continues to decide instance priority.

ADR-0003 imports `source=configarr` instances, re-imports linked files on change, and makes those instances read-only in the UI except for snatch policy. The periodic reconciler must follow `api/internal/snatch/module.go`: registration in the fx group `leader_loops` through `gate.Provide[*T]()`, rather than starting its own loop.

### Pinned application surfaces

The following contracts were checked on 2026-11-10. Readarr's `v0.4.18.2805` is its last prerelease and stays a frozen target under ADR-0008. Route support is taken from these tagged contracts, not inferred from SDK method names.

| App | OpenAPI contract | Exclusion base route | Exclusion identity | Search-on-add field |
| :--- | :--- | :--- | :--- | :--- |
| Sonarr | [`v4.0.20.3014`](https://github.com/Sonarr/Sonarr/blob/v4.0.20.3014/src/Sonarr.Api.V3/openapi.json) | `/api/v3/importlistexclusion` | `tvdbId` | `searchForMissingEpisodes` |
| Radarr | [`v6.4.4.10685`](https://github.com/Radarr/Radarr/blob/v6.4.4.10685/src/Radarr.Api.V3/openapi.json) | `/api/v3/exclusions` | `tmdbId` | `searchOnAdd` |
| Lidarr | [`v3.1.6.5079`](https://github.com/Lidarr/Lidarr/blob/v3.1.6.5079/src/Lidarr.Api.V1/openapi.json) | `/api/v1/importlistexclusion` | `foreignId` | `shouldSearch` |
| Readarr | [`v0.4.18.2805`](https://github.com/Readarr/Readarr/blob/v0.4.18.2805/src/Readarr.Api.V1/openapi.json) | `/api/v1/importlistexclusion` | `foreignId` | `shouldSearch` |

All four exclusion APIs support base GET/POST and item GET/PUT/DELETE. Sonarr additionally has paged GET and bulk DELETE; Radarr has those plus bulk POST. Lidarr and Readarr have neither paged nor bulk exclusion routes. All four blocklist APIs expose list GET, item DELETE and bulk DELETE, with no POST/PUT/PATCH. `BlocklistResource` includes `sourceTitle`, `indexer`, `protocol`, `quality`, `date` and `customFormats`, plus `languages` on Sonarr/Radarr; none exposes a release GUID or info hash (tagged contracts above).

The SDK pins in `go.mod` are `arr/sonarr v1.5.5`, `arr/radarr v1.5.5`, `arr/lidarr v1.6.2` and `arr/readarr v1.5.2`. They carry `CreateImportList`, `UpdateImportListsBulk`, `CreateImportListExclusion`, `GetBlocklist` and `DeleteBlocklistBulk`; Radarr also carries `CreateImportListExclusionsBulk`. However, Lidarr `v1.6.2` `importlist.go:91,97` calls unsupported `/api/v1/importlistexclusion/paged` and `/bulk` routes. Both the pinned application contract and the checked `develop` OpenAPI at `28ab333eafa1` lack those routes; L0 must fix or drop those SDK methods before adoption.

Configarr `v1.39.1` reconciles import-list definitions for Sonarr and Radarr via `src/importLists/importListBase.ts`, `importListSonarr.ts` and `importListRadarr.ts`. Its `delete_unmanaged` is opt-in, and it does not sync exclusions ([tagged source](https://github.com/raydak-labs/configarr/blob/v1.39.1/src/importLists/importListBase.ts)); this establishes the D4 ownership boundary.

### Maintained projects compared

Before any new code, 30 projects and services were compared on the four scopes: **S1** external lists into `*arr` instances, **S2** import-list and exclusion sync across instances of one app, **S3** blocklist sync between instances, **S4** state written back to source lists. Licence, latest release, default branch head and last push were read from each forge on 2026-11-10 (GitHub API, re-checked 19:47 CET); `part` means a partial cover. Names ending in *-arr* follow the Servarr naming pattern (an English word plus *arr*, read as “-ar”); the *What it is* column gives each name's meaning.

| Project | What it is | Licence | Latest release | Default branch head | Last push | S1 / S2 / S3 / S4 | Fit |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| [Pulsarr](https://github.com/jamcalli/Pulsarr) | Plex watchlist bridge into Sonarr and Radarr, routing per instance | AGPL-3.0 | `v1.1.1` 2026-11-08 | `3317440ac1e0` 2026-11-08 | 2026-11-09 | yes / - / - / - | link out (D8) |
| [Seerr](https://github.com/seerr-team/seerr) | request manager (successor of Overseerr and Jellyseerr) | MIT | `v3.5.1` 2026-10-21 | `3f7d5c3bd2af` 2026-11-09 | 2026-11-10 | yes / - / - / part | call its API |
| [Configarr](https://github.com/raydak-labs/configarr) | declarative `*arr` configuration sync, import lists for Sonarr and Radarr | AGPL-3.0 | `v1.39.1` 2026-11-06 | `deaeba18dcd6` 2026-11-06 | 2026-11-06 | part / part / - / - | borrow design |
| [Recyclarr](https://github.com/recyclarr/recyclarr) | quality profile and custom format sync | MIT | `v8.8.1` 2026-10-28 | `3eaa713021c3` 2026-11-04 | 2026-11-08 | - / - / - / - | none |
| [Traktarr](https://github.com/l3uddz/traktarr) | adds Trakt lists to Sonarr and Radarr | GPL-3.0 | `1.2.5` 2019-11-09 | `52d71964a2cd` 2023-03-14 | 2023-03-14 | yes / - / - / - | borrow design |
| [Watchlistarr](https://github.com/nylonee/watchlistarr) | Plex watchlist bridge into Sonarr and Radarr | GPL-3.0 | `v0.2.6` 2024-10-17 | `bce86ec684f9` 2025-11-08 | 2025-11-08 | yes / - / - / - | borrow design |
| [Syncarr](https://github.com/syncarr/syncarr) | content sync between two `*arr` instances | MIT | `1.13.1` 2021-06-07 | `cfe9a7198bfa` 2023-11-15 | 2024-03-15 | - / - / - / - | borrow design |
| [Cleanuparr](https://github.com/Cleanuparr/Cleanuparr) | download queue cleanup | GPL-3.0 | `v2.11.0` 2026-11-07 | `aa2a7923945a` 2026-11-08 | 2026-11-08 | - / - / - / - | none |
| [Ombi](https://github.com/Ombi-app/Ombi) | request portal | GPL-2.0 | `v4.53.10` 2026-05-30 | `3855cacf0bc0` 2026-11-06 | 2026-11-06 | part / - / - / - | link out |
| [Petio](https://github.com/petio-team/petio) | request portal | MIT | `v0.5.5` 2024-03-30 | `abda13502a89` 2025-01-17 | 2025-01-17 | part / - / - / - | none |
| [Buildarr core](https://github.com/buildarr/buildarr) | declarative `*arr` configuration, one plugin per app | GPL-3.0 | `v0.7.1` 2023-11-13 | `093bd02e8269` 2024-05-04 | 2024-05-04 | part / part / - / - | borrow design |
| [Buildarr Sonarr plugin](https://github.com/buildarr/buildarr-sonarr) | Buildarr plugin for Sonarr | GPL-3.0 | `v0.6.4` 2024-03-02 | `0a847c823d0b` 2024-05-04 | 2024-05-04 | part / part / - / - | borrow design |
| [Buildarr Radarr plugin](https://github.com/buildarr/buildarr-radarr) | Buildarr plugin for Radarr, lists and exclusions | GPL-3.0 | `v0.2.6` 2024-04-26 | `9e1909cdc178` 2024-04-26 | 2024-04-26 | part / yes / - / - | borrow design |
| [Excludarr](https://github.com/haijeploeg/excludarr) | rule-driven import exclusions | MIT | `v2.0.1` 2026-11-05 | `527c295d039b` 2026-11-05 | 2026-11-05 | - / - / - / - | borrow design |
| [Listrr legacy public source](https://github.com/TheUltimateC0der/listrr) | old public source of the Listrr list builder | GPL-3.0 | none published | `31fc23bf53ad` 2022-10-14 | 2022-12-08 | part / - / - / - | borrow design |
| [Listrr Sonarr bridge](https://github.com/TheUltimateC0der/listrr.pro.Sonarr) | old Listrr to Sonarr bridge | AGPL-3.0 | none published | `f2ce60bf2d53` 2022-11-21 | 2022-11-21 | yes / - / - / - | none |
| [ListSync](https://github.com/Woahai321/list-sync) | external lists into Seerr requests | MIT | `v0.6.6` 2025-12-11 | `b635a833e1bb` 2025-12-30 | 2025-12-30 | yes / - / - / - | link out |
| [Kometa](https://github.com/Kometa-Team/Kometa) | Plex collection and metadata manager (formerly Plex Meta Manager) | MIT | `v2.5.4` 2026-10-31 | `cc35af068696` 2026-10-31 | 2026-11-10 | yes / - / - / part | link out |
| [Blockbusterr](https://github.com/Mahcks/blockbusterr) | Trakt-driven discovery adder for Sonarr and Radarr | MIT | `v2.0.1` 2026-09-13 | `1e5f468844b7` 2026-09-13 | 2026-09-13 | yes / - / - / - | link out |
| [BookOtter](https://github.com/Boren/bookotter) | Hardcover want-to-read matcher | MIT | none published | `b12a2c79aa9b` 2026-09-29 | 2026-09-29 | - / - / - / - | borrow design |
| [MDBList Jellyfin plugin](https://github.com/MDBList/jellyfin-plugin-mdblist) | Jellyfin watched and collected sync to MDBList | GPL-3.0 | `v1.0.0.21` 2026-11-06 | `6618101a796c` 2026-11-06 | 2026-11-06 | - / - / - / part | link out |
| [MDBList Emby plugin](https://github.com/MDBList/emby-plugin-mdblist) | Emby watched and collected sync to MDBList | GPL-3.0 | `v1.1.16` 2026-10-30 | `0a1f2cdc35c6` 2026-10-30 | 2026-10-30 | - / - / - / part | link out |
| [Fetcharr](https://github.com/Fetcharr/Fetcharr) | Plex watchlist bridge into Sonarr and Radarr | MIT | `0.1.18` 2026-06-30 | `2be2032081ef` 2026-06-30 | 2026-06-30 | yes / - / - / - | link out |
| [Sonobarr](https://github.com/Dodelidoo-Labs/sonobarr) | music discovery for Lidarr | MIT | `0.13.0` 2026-09-09 | `830ed8d170b2` 2026-09-09 | 2026-09-09 | yes / - / - / - | call its API |
| [FlexGet](https://github.com/Flexget/Flexget) | automation tool with list add and remove plugins | MIT | `v3.22.12` 2026-11-09 | `b752e146ec3e` 2026-11-09 | 2026-11-09 | yes / - / - / part | borrow design |
| [MDBListarr](https://github.com/linaspurinis/mdblistarr) | MDBList lists into `*arr` instances | none declared | none published | `cd98d34e43e0` 2026-11-02 | 2026-11-02 | yes / - / - / part | link out |
| [Notifiarr client](https://github.com/Notifiarr/notifiarr) | notification client with MDBList library sync | MIT | `v0.10.0` 2026-11-07 | `7b3bf89382d5` 2026-11-09 | 2026-11-09 | part / - / - / part | link out |
| [Listrr service](https://listrr.pro) | hosted list builder serving Custom List URLs | - | - | - | - | hosted service, no public source | link out |
| [MDBList service](https://mdblist.com) | hosted list service serving Custom List URLs | - | - | - | - | hosted service, no public source | link out (D8) |
| [Huntarr](https://github.com/plexguide/Huntarr.io) | missing-media hunter | - | - | - | - | origin returns 404, unassessed | none |

No project covers S2 across all four apps or S3 at all. Buildarr's Radarr plugin is the closest S2 precedent (one app, dormant since 2024), and FlexGet and Kometa cover parts of S4 inside their own workflows. The surveyed fit (*call its API*, *borrow design*, *link out*, *none*) feeds D8; no code is reused from them.

## Decision

### D1. Cross-instance list controller

SnatchArr controls list definitions and exclusions across every instance of one application, called an **app group**. There is one group for each of Sonarr, Radarr, Lidarr and Readarr; Whisparr is outside this record. The applications' native import lists fetch list content. SnatchArr fans out configuration and admits list-added items into its wanted planner; it does not become a list fetcher. Sources without a native kind follow the OQ-0047-1 answer: documented MDBList and Listrr Custom List link-outs now, and a SnatchArr-served Custom List feed for one source only after a measured adopter gap.

### D2. Search-on-add off; searches use hourly caps

Every definition SnatchArr writes disables search-on-add: `ImportListResource.searchForMissingEpisodes` for Sonarr, `searchOnAdd` for Radarr, and `shouldSearch` for Lidarr and Readarr (the tagged OpenAPI contracts in References). Added items enter SnatchArr's wanted planner and are searched under the existing hourly caps and ADR-0022 priority. The cap counts searches sent per indexer, rather than HTTP requests or added titles (`AGENTS.md`, Contracts).

An unmanaged list with search-on-add enabled produces the observe finding **“list searches outside SnatchArr's caps”**. SnatchArr never edits it outside act mode. L4 must verify both the written fields and the unmanaged-list finding.

### D3. Fan out by name; resolve each target

One definition, keyed by name, applies to every group member. Resolve quality profile, metadata profile, root folder and tags by name on each target; never copy instance-local IDs between instances. A target missing a named profile or root folder produces a plan error and receives no write. L3 verifies target resolution and the no-write failure path.

### D4. One writer alongside Configarr

On a `source=configarr` instance whose Configarr file declares import lists, SnatchArr never writes list definitions. Configarr remains their sole writer, preserving ADR-0003's ownership boundary. Configarr does not manage exclusions, so SnatchArr may sync exclusions on those instances. OQ-0047-2 chose this ownership policy; L3 supplies the guard test.

### D5. Exclusion union with observed-removal tombstones

An exclusion's identity is its app-specific foreign ID: Sonarr `tvdbId`, Radarr `tmdbId`, Lidarr `foreignId`, Readarr `foreignId` (tagged OpenAPI contracts). An exclusion observed on one member is added to the others. A removal propagates only after SnatchArr has observed that exclusion on that member and subsequently observed it gone; an initially absent exclusion is not a removal. Persist that observation history and tombstones so a simple union does not resurrect a removal. OQ-0047-3 chose this policy.

Use Radarr's bulk exclusion POST; the other apps receive single POSTs with a scalar per-tick bound. L2 must test union and tombstones for ties, empty groups, one member, and seen versus unseen removals, and prove the fake log contains only exclusion POST/DELETE calls.

### D6. Blocklist propagation through queue removal

None of the four pinned applications accepts blocklist insertion. Never replay blocklist records or write an application's database. Read each member's blocklist into deny records keyed exactly by `(app, indexer, protocol, sourceTitle)`; use no fuzzy matching. The tagged `BlocklistResource` contracts expose no release GUID or info hash with which to make a stronger identity.

Add `blocklisted_elsewhere` to the ADR-0012 queue rules in `api/internal/queue/rules.go`. When a queue item on another member matches the exact key, removal goes through that member's `*arr` queue API with `blocklist=true`; the application creates its own blocklist entry through that allowed path. ADR-0012's observe-first, strikes and bounded-removal rules still apply, including `removeFromClient=true` and `skipRedownload=true`. There is no download-client call under ADR-0006 or ADR-0046. The cost is one started grab per member before propagation can block that release there. OQ-0047-4 chose this approach without an upstream request for a blocklist insertion route; L5 checks exact matching and the queue call.

### D7. Two-way behaviour, with explicit limits

Collection state uses the native Trakt Connect notification on Sonarr and Radarr: add on import and remove on delete. Fan out that notification configuration like list definitions, resolving each target rather than copying local IDs. The pinned `src/NzbDrone.Core/Notifications/Trakt/Trakt.cs` sources are linked in References; L6 verifies the fan-out.

For sources with a write API in goenvoy, support removing an imported title from its Trakt or TMDb source watchlist: Trakt `RemoveFromWatchlist` (`metadata/tracking/trakt/trakt.go:729`) and TMDb `AddToWatchlist` with `watchlist=false` (`metadata/video/tmdb/tmdb.go:1058`) at goenvoy `c8341574c82f`. A Plex watchlist needs a plex.tv watchlist client in goenvoy first; `mediaserver/plex` covers the local server only. This is per-list opt-in, off by default and observe first, as OQ-0047-5 chose. L6 must prove that other lists and observe mode produce no removal.

Watched/read state write-back remains outside this record. ADR-0024 reads watch state and writes none back; scrobbling stays with media-server plugins, such as the MDBList Jellyfin and Emby plugins in the survey table. Lidarr and Readarr have no write-back target in this record; that is an explicit coverage gap, not an implied fallback.

### D8. Reuse existing fetchers and ownership

Seerr requests keep their existing sync and ADR-0036 ownership. Pulsarr remains a link-out for multi-instance Plex watchlist routing. MDBList and Listrr content enters through each application's native Custom List kind where available: fan out the list URL, without calling an MDBList API. Readarr has no Custom List kind (tagged OpenAPI contract); the OQ-0047-1 answer keeps that limit.

Borrow Configarr's schema-driven reconciliation and opt-in `delete_unmanaged` design. No code is copied from GPL/AGPL projects; SnatchArr's code is EUPL-1.2 (`LICENSES/EUPL-1.2.txt`). The project survey in References informs design and link-outs, not code reuse.

### D9. Leader-only reconciliation

Register the reconciler in the fx group `leader_loops` through `gate.Provide[*T]()`, following `api/internal/snatch/module.go`. The elected API leader owns reconciliation, consistent with the existing wanted planner and ADR-0012 queue sweep.

### D10. Observe first for every write class

Definitions, exclusions, the blocklist queue rule and source write-back all default to observe per app group. Native notification fan-out follows the same boundary. Show planned writes through the API and UI; act requires explicit opt-in per group, and upgrades keep observe. Per-list source write-back additionally requires D7's opt-in. L1–L6 must verify zero writes in observe, including the upgrade path.

## Answered questions

The operator answered all five questions on 2026-11-10. Answers are paraphrased; each one matches the recommendation in the question list that follows.

| ID | Ledger ID | Answer letter | Answer |
| :--- | :--- | :--- | :--- |
| `OQ-0047-1` | `snatcharr-oq-0047-1-unlisted-sources` | c | Document the MDBList and Listrr Custom List link-out now, with no code; SnatchArr serves its own Custom List feed for a source only after a measured adopter gap. |
| `OQ-0047-2` | `snatcharr-oq-0047-2-configarr-lists` | a | Configarr owns import-list definitions on instances whose Configarr file declares them; SnatchArr syncs only exclusions there, one writer per field. |
| `OQ-0047-3` | `snatcharr-oq-0047-3-exclusion-merge` | a | Union with tombstones: an exclusion added on any member spreads to all; a removal spreads only after SnatchArr saw the exclusion exist and then vanish. |
| `OQ-0047-4` | `snatcharr-oq-0047-4-blocklist-sync` | a | Queue rule `blocklisted_elsewhere` with an exact `(app, indexer, protocol, sourceTitle)` match, removal through the queue API, observe first; no `*arr` database writes, no download-client calls and no upstream request. |
| `OQ-0047-5` | `snatcharr-oq-0047-5-source-writeback` | a | Native Trakt Connect plus per-list opt-in removal from Trakt and TMDb watchlists after import, off by default and observe first; a Plex watchlist waits for a goenvoy client. |

### Questions as asked

The table keeps each question with its options, risks and recommendation as asked.

| ID | Question | Options with risk | Recommended |
| :--- | :--- | :--- | :--- |
| OQ-0047-1 | How should sources without a native list kind enter: Letterboxd, Jellyfin/Emby favourites or playlists, Hardcover and ListenBrainz? | **a)** Link out to MDBList / Listrr Custom Lists only (risk: hosted service and account; Jellyfin/Emby favourites uncovered). **b)** Serve Custom List JSON feeds built from goenvoy clients (risk: new fetchers to maintain; Readarr has no Custom List kind). **c)** Use a now, then consider b per source after a measured gap (risk: uncovered sources wait for that evidence). | **c** |
| OQ-0047-2 | Who owns Configarr-declared lists? | **a)** Configarr owns declared lists; SnatchArr syncs exclusions only there (risk: two tools to configure). **b)** SnatchArr owns lists everywhere and warns users to drop Configarr `importLists` (risk: two writers until they do). | **a** |
| OQ-0047-3 | Which exclusion semantics should apply? | **a)** Union with tombstones (risk: an unwanted exclusion spreads group-wide). **b)** Mirror one primary member to the others (risk: exclusions added on other members are dropped). | **a** |
| OQ-0047-4 | How should blocklist propagation work? | **a)** Exact-key `blocklisted_elsewhere` queue rule, observe first (risk: one started grab per member). **b)** No blocklist sync (risk: the same bad release is grabbed on every member). **c)** a plus an upstream request for a blocklist insertion route (risk: external post requiring ask-first approval). | **a** |
| OQ-0047-5 | Should import remove titles from their source watchlists? | **a)** Remove from Trakt and TMDb watchlists after import, per-list opt-in and observe first; Plex after a goenvoy plex.tv client (risk: removes a title the user still wants listed). **b)** Collection only through native Trakt Connect (risk: watchlists keep downloaded titles). | **a** |

## Implementation slices

This record adds no implementation. Execute these slices in order; every open question is answered. Each slice that ships code ships with an end-to-end test through the served API and UI; a client-only test is insufficient evidence for a served feature.

| Slice | Deliverable | Verification before next |
| :--- | :--- | :--- |
| L0 | In goenvoy, fix or drop Lidarr `GetImportListExclusionsPaged` / `DeleteImportListExclusionsBulk`: `/paged` and `/bulk` are absent at `v3.1.6.5079`. Release, then bump the pin in `go.mod`. | Request test against the pinned Lidarr route table; a planted `/paged` call is refused. |
| L1 | Observe read model: definitions, exclusions and blocklists from every member of each app group; drift exposed per group through API and UI. | Fake `*arr` fixtures for all four apps show planted drift; fake logs contain zero write calls; upgrade keeps observe. |
| L2 | D5 exclusion sync, act opt-in, after OQ-0047-3. | Union/tombstone unit and property tests: ties, empty input, one member, seen/unseen removals. Fake log contains only exclusion POST/DELETE calls; observe writes nothing. |
| L3 | D3 definition fan-out, per-target resolution and D4 Configarr guard, after OQ-0047-2. | A missing profile/root produces a plan error and no write; a Configarr instance with declared lists receives zero definition writes; resolved IDs belong to each target. |
| L4 | D2 search-on-add guard. | Property test over all four apps: every written definition disables search-on-add. An unmanaged list with it on produces the observe finding. |
| L5 | D6 `blocklisted_elsewhere` queue rule, after OQ-0047-4. | Exact-key matches only; a planted near-miss title is not matched. Fake log shows queue API removal with `blocklist=true`; observe writes nothing. |
| L6 | D7 Trakt Connect fan-out and opted-in source watchlist removal, after OQ-0047-5. | Notification configuration resolves per target. Watchlist removal occurs only for opted-in lists after import; observe writes nothing. |
| L7 | Sources without a native kind, per OQ-0047-1: document the MDBList and Listrr Custom List link-out per app; no code. A SnatchArr-served feed for one source needs a measured adopter gap and its own record. | `make docs-verify` passes with the page; the page names the Custom List kind per app from the pinned contracts and states Readarr's missing Custom List kind rather than substituting an unsupported kind. |

## Class defence

Enforce one writer with a Semgrep rule in `.semgrep.yml`, with its fixture under `tools/ci/semgrep/testdata`. Refuse goenvoy import-list, exclusion and notification write calls outside the list-sync package. Match client identity, including import aliases, rather than matching only method names. Queue removals remain in ADR-0012's queue path; D6 does not introduce a blocklist insertion call.

Follow `.semgrep.yml`'s existing fixture convention: `ruleid:` marks calls that must be reported and `ok:` marks allowed calls. Include planted forbidden calls, alias imports and allowed list-sync calls. Replay the planted fixture with the rule absent (passes), then present (refused). L4's property test of D2 supplies an independent behavioural defence for cap bypass through search-on-add. Each new check ships a planted negative case and runs once without its fix.

This ADR adds no executable check. The rule, fixtures, property tests and their replay evidence remain implementation deliverables, not enforcement claims made by this documentation change.

## Consequences

### Positive

- One definition and exclusion set spans each app group, with per-target name resolution and one writer (D1, D3–D5).
- List-added items enter the existing capped search planner rather than triggering uncapped search-on-add (D2; L4).
- Blocklist knowledge spreads through the existing queue API boundary without database writes (D6; L5).
- Native lists, goenvoy clients and Configarr's reconciliation design are reused; Seerr requests retain their existing owner (D4, D8).

### Negative

- Configarr-declared lists remain outside SnatchArr's definition control, so those users configure two tools (D4; OQ-0047-2).
- Blocklist propagation costs one started grab per member and cannot replay an entry pre-emptively (D6).
- Readarr has no Custom List kind, and Lidarr/Readarr have no write-back target in this record (D7–D8).
- MDBList and Listrr are hosted services; link-outs require their availability and an account (OQ-0047-1).
- Reconciliation needs durable per-member observation and tombstone state, and L0 must repair the SDK's unsupported Lidarr routes before relying on them (D5; L0).

## References

- Operator direction, 2026-11-10 (paraphrased): add cross-instance list syncing unless already covered by a plan; ledger `snatcharr-list-sync-gap-20261110`.
- Repository baseline: `021c2d147199`; issue search checked 2026-11-10 at 19:43 CET.
- [ADR-0003: Configarr as an instance source](0003-configarr-as-instance-source.md).
- [ADR-0006: Download-client backpressure](0006-download-client-backpressure.md).
- [ADR-0008: Readarr stays a supported target, frozen](0008-readarr-frozen-legacy-target.md).
- [ADR-0012: Queue management](0012-queue-management-replaces-cleanuparr-link.md).
- [ADR-0022: Instance priority](0022-instance-priority.md).
- [ADR-0024: Watch-aware modes](0024-watch-aware-modes.md).
- [ADR-0036: Community requests and voting](0036-community-requests-and-voting.md).
- [ADR-0046: Download-client priority writes](0046-download-client-priority-writes.md).
- Local contracts and existing owners: `AGENTS.md`, `go.mod`, `api/internal/seerr/sync.go`, `api/internal/queue/rules.go`, `api/internal/snatch/module.go`, `.semgrep.yml`.
- Application OpenAPI contracts: the four tagged URLs in Context, checked 2026-11-10.
- Configarr [`v1.39.1`, `src/importLists/importListBase.ts`](https://github.com/raydak-labs/configarr/blob/v1.39.1/src/importLists/importListBase.ts), checked 2026-11-10.
- Native Trakt Connect: Sonarr [`v4.0.20.3014`, `Trakt.cs`](https://github.com/Sonarr/Sonarr/blob/v4.0.20.3014/src/NzbDrone.Core/Notifications/Trakt/Trakt.cs) and Radarr [`v6.4.4.10685`, `Trakt.cs`](https://github.com/Radarr/Radarr/blob/v6.4.4.10685/src/NzbDrone.Core/Notifications/Trakt/Trakt.cs), checked 2026-11-10.
- goenvoy SDK survey: [`c8341574c82f`](https://github.com/golusoris/goenvoy/tree/c8341574c82f), checked 2026-11-10: tracking clients for Letterboxd, ListenBrainz, Simkl and Trakt; video metadata clients for TMDb and TVDb; media-server clients for Plex (local server only, no plex.tv watchlist), Jellyfin and Emby; no MDBList client. Installed app pins are recorded in `go.mod`.
