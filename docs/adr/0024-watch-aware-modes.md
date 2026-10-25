<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0024: Watch-aware modes: remove when seen, and the season gate

- Status: proposed
- Date: 2026-10-23
- Related: [ADR-0018](0018-product-front-door.md) (modules, reuse map), [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md) (observe mode first), [ADR-0006](0006-download-client-backpressure.md) (no download-client writes)

## Context

SnatchArr decides when and how much to search, but it does not know whether anyone has watched what it fetched. Two jobs follow from that gap (`snatcharr-watch-aware-modes-20261023`):

1. **Remove when seen.** Media that has been watched is removed, in a configurable and cautious form.
2. **Season gate.** While nobody has watched a show, only its first season (or a configured first part) is fetched. A request for all seasons is narrowed to that part; later seasons follow once it has been watched.

Both jobs need the same input: per-user watch state from the media server. Both act on the *arr instances SnatchArr already holds. Removal is the first SnatchArr feature that deletes library files, so it carries the highest risk in the product.

### What the upstream applications offer

Checked against upstream source on 2026-10-23. The research notes are in the session's local state, outside the repository.

| Source | Watch state | Notes |
| :--- | :--- | :--- |
| Jellyfin (v12.3) | `GET /Users`; per-user `UserData`: `Played`, `PlayCount`, `LastPlayedDate`, `PlayedPercentage`, `PlaybackPositionTicks` | Legacy token headers off by default in v12; `Authorization: MediaBrowser Token=` |
| Emby (4.8+) | Same `UserData` shape under `/emby/Users/{id}/Items` | Webhook payload schema not verified |
| Plex | Owner token sees only the owner's state | Per-user state needs Tautulli (`get_history`) |
| Tautulli | `get_history` filtered by user and show/season rating keys | One API key, all users |

goenvoy already wraps Jellyfin and Emby (`GetUsers`, `UserData`) and Tautulli (`GetHistory`, `GetUsers`) in `github.com/golusoris/goenvoy/mediaserver`.

| *arr call | Effect |
| :--- | :--- |
| Sonarr `PUT /api/v3/series/{id}` with `seasons[].monitored` | Unmonitored seasons leave Wanted/Missing and are not grabbed by RSS sync |
| Sonarr `addOptions.monitor` (`firstSeason`, `pilot`, `future`, ...) | Monitoring at add time |
| Sonarr `DELETE /api/v3/episodefile/bulk`, `DELETE /api/v3/series/{id}?deleteFiles=&addImportListExclusion=` | File and series removal |
| Radarr `DELETE /api/v3/movie/{id}?deleteFiles=&addImportExclusion=` | Movie removal; the parameter name differs from Sonarr's |

Seerr (v3.5.1) marks availability from `statistics.episodeFileCount`, not from the monitored flag (`server/lib/availabilitySync.ts`). An unmonitored season with no files shows as not available, and an all-season request shows as partially available. Seerr never re-requests on its own. The gate therefore works without writing to Seerr, which keeps the read-only rule in [docs/seerr.md](../seerr.md) intact.

### Prior art

| Project | Licence | Overlap |
| :--- | :--- | :--- |
| Maintainerr v3.31.0 | MIT | Rule engine over Plex/Jellyfin; actions `DELETE`, `UNMONITOR`, `UNMONITOR_DELETE_EXISTING`, ...; "leaving soon" collection with a grace period; Seerr requester rule |
| Janitorr v2.2.2 | GPL-3.0 | Removes stale or unwatched media; explicitly does not remove after watching; dry run on by default |
| Episeerr | AGPL-3.0 | Episode-level progression from Jellyfin or Tautulli webhooks; dry run and an approval queue |
| Jellyfin Media Cleaner plugin | GPL-3.0 | Removes played items inside Jellyfin; protection rules |

No existing application gates seasons on watch state across Seerr requests at season granularity together with per-indexer search caps. Maintainerr covers most of remove-when-seen today.

## Decision

### Module

The two modes form one new module, **Watch-aware modes** (German label through Paraglide; `snatcharr-watch-module-name-20261023`). It follows ADR-0018: its own package `api/internal/watchaware`, its own OpenAPI routes, its own tables and migrations, and off by default. The two modes have separate switches inside the module, so an operator can run the season gate without any removal.

### Order of delivery

The season gate is built first (`snatcharr-watch-build-or-link-20261023`). Remove when seen follows in SnatchArr once the gate has shipped and its observe run has produced watch data; the design below fixes its shape now so the gate's tables and the watch predicate serve both. Until then, the documentation names Maintainerr as an alternative for removal. Revisit trigger: the gate's observe run is complete.

Watch state is read through a `WatchSource` interface with one adapter per media server, on the existing goenvoy clients. Jellyfin and Emby come first, and Plex follows through Tautulli. A periodic sync runs as a `leader_loops` member (`gate.Provide[*T]()`). It stores per user, per item: played, last played and resume position. Webhooks can later shorten the delay; the poll stays the source of truth.

"Watched" is one shared predicate for both modes. An item counts as watched by a user when `Played` is true, or when `PlayedPercentage` is at or above a threshold (default 90 %). A season counts as watched when the watched share of its aired episodes reaches a threshold (default: all of them for removal, and the last aired episode or 50 % for promotion).

Whose watch counts is a per-mode setting (`snatcharr-watch-whose-watch-20261023`); the default is `all` for removal and `any` for promotion:

- `requester`: the Seerr requester mapped to a Jellyfin user, falling back to `all` when the item has no request;
- `any`: any user with access;
- `all`: every active user with access.

### Season gate

The gate is hard: it unmonitors later seasons in Sonarr rather than only skipping SnatchArr's own searches, because Sonarr's RSS sync would otherwise still grab them (`snatcharr-watch-gate-hard-20261023`).

1. **On add.** For a series that enters Sonarr (from Seerr, an import list or by hand), the gate leaves seasons `1..N` monitored (default N = 1) and unmonitors the later ones through `PUT /api/v3/series/{id}`. Specials stay as Sonarr set them. Series that already have files beyond the gate are never narrowed: the gate only holds back, it never removes.
2. **Seerr requests are not edited.** The request stays as the user made it; Seerr shows the show as partially available.
   - The SnatchArr request view marks the held seasons as "waiting for season N to be watched".
   - SnatchArr reads that state from its own gate table, not from Seerr.
3. **Promotion.** When the gate season counts as watched (under the promotion "whose watch" rule, default `any`), the next season is monitored, and SnatchArr's core search picks it up under the normal hourly caps.
   - The look-ahead setting (default 1) says how many seasons stay monitored ahead of the furthest watched one.
4. **Exemptions.**
   - A Sonarr tag (default `snatcharr-no-gate`) exempts a series.
   - So does a per-request override, which lets an admin release all seasons.
   - Series added before the gate was switched on are never touched.
5. **Observe first.** As with queue management, the gate starts in observe mode. It records what it would unmonitor and promote, and writes nothing to Sonarr until the operator switches it to act.

### Remove when seen

This mode is the later delivery step (see Order of delivery). Removal is staged. Each stage can be inspected in the UI and audited in a table.

1. **Candidate.** A movie, or a season of a series, counts as watched under the removal "whose watch" rule (default `all`). Its last play must also be older than a minimum age (default 14 days).
2. **Protections.** Any one of these keeps the item:
   - an *arr tag (default `keep`);
   - a Jellyfin favourite by any user;
   - a resume position for any user (someone is mid-watch);
   - an open Seerr request by another user for the same media;
   - an item added fewer than N days ago (default 30);
   - the season the gate needs as its look-ahead anchor.
3. **Leaving soon.** Candidates enter a "leaving soon" list with a grace period (default 14 days), shown in SnatchArr and sent through the existing notification channels. A new play or a new protection during the grace period drops the candidate. A Jellyfin "leaving soon" collection is a later step; it needs a collection write that goenvoy does not wrap yet.
4. **Removal**, only through the *arr API, never on the filesystem and never through a download client (ADR-0006):
   - **Movie:** Radarr `DELETE /api/v3/movie/{id}?deleteFiles=true&addImportExclusion=true`.
   - **Season:** Sonarr bulk episode-file delete for that season, then the season is unmonitored, so the core search never re-fetches it.
   - **Series:** the whole series is removed (`deleteFiles=true&addImportListExclusion=true`) only when every season has been removed and Sonarr reports the series as ended.
5. **Limits.**
   - Each run removes at most a configured count (default 10) and size.
   - A run that would exceed the cap removes nothing and raises a notification.
   - Removal is off until the operator has run observe mode at least once and switched to act.
   - When the *arr instance has no recycle bin configured, the settings page warns that removal cannot be undone.

The two modes fit together: a season removed after watching stays unmonitored, and the gate's promotion only ever moves forward, so a removed season is not fetched again.

### Writes this module adds

| Target | Writes | Mode |
| :--- | :--- | :--- |
| Sonarr | season `monitored` (gate, promotion, after removal); episode-file bulk delete; series delete | act mode only |
| Radarr | movie delete with import exclusion | act mode only |
| Seerr | none | — |
| Jellyfin, Emby, Tautulli | none (read only); a collection write is a later step | — |

## Implementation plan

The season gate ships in three slices, each one pull request, in the order fixed under Order of delivery. Every slice keeps the module off by default and reuses existing SnatchArr patterns, so nothing below is a new mechanism (HISS-19).

### Patterns reused

- Leader loop: `Ticker` wrapping `gate.Periodic`, provided through `gate.Provide[*Ticker]()`, as in `api/internal/seerr/module.go:24` and `api/internal/instances/module.go`.
- Observe, then act: `queue.Mode` with `ModeObserve` and `ModeAct` (`api/internal/queue/rules.go:65-71`); observe mode records what it would do and returns before any write (`api/internal/queue/sweep.go:308`).
- Seerr requests: the cached request rows written by `UpsertSeerrRequest` (`api/internal/seerr/sync.go:138`).
- Instance credentials: `instances.Service.Credentials` (`api/internal/instances/service.go:265`).
- Notifications: `notify.Engine` (`api/internal/notify/engine.go:37`), see [docs/notifications.md](../notifications.md).
- Persistence: migrations in `api/internal/store/migrations/postgres/` (taking the next free migration number after `000028_modules`), queries in `api/internal/store/queries/`.

No Jellyfin or Emby account source exists in `api/internal`; ADR-0018 leaves accounts to a later decision, so the module keeps its own media-server connections.

### Slice 1: watch-state sync

The storage-free part of this slice shipped in [#602](https://github.com/lusoris/SnatchArr/pull/602) (package `api/internal/watchaware`: `WatchSource`, Jellyfin/Emby adapters, `Watched` predicate, `SyncAll`; `ErrTruncated` fail-closed). Storage and runtime gating follow under the ADR-0025 module architecture.

- Scope: a leader loop that reads per-user watch state from Jellyfin and Emby and stores it.
- Package: `api/internal/watchaware` (`module.go`, `source.go` with the `WatchSource` interface, `jellyfin.go`, `emby.go`, `sync.go`, `predicate.go`), wired in `api/internal/app`.
- Contracts:
  - table `watchaware_sources` (one row per media server: kind, URL, API key encrypted with `core/crypto` and write-only in the API, enabled);
  - table `watchaware_item_watches` (key: source, user, item; columns: series and season and episode numbers or movie id, played, played percentage, resume position, last played, synced at);
  - watchaware tables take the next free migration number after `000028_modules` (which is owned by ADR-0025 Slice 1);
  - OpenAPI: list, create, update and delete media-server sources, and a sync status read;
  - settings: module switch (default off) and sync interval live in `modules` row `watchaware` config JSONB (per ADR-0025 answer `snatcharr-adr25-module-config-storage`), not a settings column; default sync interval 15 minutes when config omits `sync_interval_minutes`, base tick 1 minute;
  - Paraglide keys under `watchaware_*` in `web/messages/en.json` and `de.json`.
- Upstream: goenvoy `mediaserver/jellyfin/v3` and `mediaserver/emby/v3` (`GetUsers`, `GetItems` with a user filter); new module requirements in `go.mod`, so `web/static/licences.json` is regenerated in the same pull request.
- Upstream dependency resolved: `UserItemDataDto` has `Played` and `PlayedPercentage` from `mediaserver/jellyfin/v3.1.0` and `mediaserver/emby/v3.1.0` (`mediaserver/jellyfin/types.go:91-92`, same in `mediaserver/emby/types.go`; [goenvoy#173](https://github.com/golusoris/goenvoy/pull/173)).
- Tests:
  - positive: an item with `Played` true, and one at 90 % played, count as watched;
  - negative: an unreachable server records the error and the loop goes on with the next source;
  - boundary: 89.9 % is not watched, 90.0 % is;
  - planted defect: a predicate that ignores the threshold makes the boundary test fail.
- Gate: `make api-verify`, `make web-verify`.
- Switch off: module switch off (default); the loop then never runs.

### Slice 2: season gate in observe mode

- Scope: evaluate the gate for series added to Sonarr and for watch progress, and record what it would unmonitor and promote, with no Sonarr write.
- Package: `api/internal/watchaware` (`gate.go`, `promote.go`).
- Contracts:
  - table `watchaware_gate_series` (key: instance, series; gate season, state, added-before-gate flag);
  - table `watchaware_gate_decisions` (instance, series, season, action, would-act flag, reason, time);
  - OpenAPI: per-instance gate settings, the decision list, and the held seasons per series for the request view;
  - settings, with the defaults from Decision: gate switch (off), mode (`observe`), seasons kept on add (1), look-ahead (1), promotion "whose watch" (`any`), season watched threshold (last aired episode or 50 %), exemption tag (`snatcharr-no-gate`);
  - Paraglide keys under `watchaware_gate_*`;
  - SSE and notification event for a recorded decision.
- Upstream: goenvoy `arr/sonarr` `GetAllSeries`, `GetSeries`, `GetEpisodes`, `GetTags`.
- Upstream dependency resolved for the `requester` rule: the Seerr `User` type has `JellyfinUserID` and `JellyfinUsername` from `arr/seerr/v1.6.0` (`arr/seerr/types.go:19-20`; [goenvoy#173](https://github.com/golusoris/goenvoy/pull/173)).
- Tests:
  - positive: a new unwatched series records "would unmonitor" for seasons after the first; a watched first season records "would promote" for the second;
  - negative: a series tagged `snatcharr-no-gate`, a series with files beyond the gate, and a series added before the gate was switched on all record nothing;
  - boundary: a season at 49.9 % holds, at 50.0 % promotes; look-ahead 0 and 1;
  - planted defect: a gate that ignores the exemption tag makes the negative test fail.
- Gate: `make api-verify`, `make web-verify`.
- Switch off: gate switch off (default); observe mode is the default and writes nothing.

Slice 2 lands in two parts:

- 2a, the Sonarr side.
  - The gate on add and the exemptions, with decisions recorded and nothing written to Sonarr.
  - The settings live under the key `gate` in the watch-aware module's `modules.config`, as ADR-0025 fixes for module config.
  - Migration `000031_watchaware_gate`. The table `watchaware_gate_baselines` marks the first pass for each instance. That pass records every existing series as `before_gate` in one transaction, so a series that is already in Sonarr when the gate is switched on is never gated.
  - The held seasons are stored on the series row, so pruning the decision log after 30 days keeps them.
  - Read endpoints `GET /watchaware/gate/series` and `GET /watchaware/gate/decisions`.
  - Packages: `gate.go` (pure) and `gatepass.go`.
- 2b, the rest of Slice 2.
  - 2b-1: season-gate event (`watchaware_gate_decided`), SPA view (`/automation?tab=season-gate`), and Paraglide keys.
  - 2b-2: promotion from watch data, observe mode only. A media-server series maps to its Sonarr series through the `Tvdb` entry of `ProviderIds`. A season counts as watched when its last library episode is watched or at least 50.0 % of its library episodes are; "aired" is approximated by "present in the media-server library". Held seasons up to the furthest watched season plus `look_ahead` get promote decisions, and `held_seasons` shrinks.

### Slice 3: season gate in act mode

- Scope: carry out the recorded decisions against Sonarr, and let an admin release all seasons of a series.
- Package: `api/internal/watchaware` (`act.go`).
- Contracts: mode `act` accepted by the settings validation; OpenAPI action to release a series (the per-request override from Decision); Paraglide keys for the release action.
- Upstream: goenvoy `arr/sonarr` `UpdateSeasonPass` (`arr/sonarr/series.go:48`) sets the season flags and keeps the series flag as Sonarr reports it. A season pass without monitoring options leaves the episode flags unchanged, and Sonarr's wanted/missing list filters on the episode flag, so the actor also calls `GetEpisodes` (`episode.go:11`) and `MonitorEpisodes` (`episode.go:29`) for the affected seasons. `UpdateSeries` stays unused.
- Pending decisions: only decisions recorded while the mode is `act` are carried out. Decisions recorded in observe mode are never replayed; after a switch to `act`, the next pass acts on the decisions it records from then on. When an unmonitor and a promote for the same season are pending together, the promote wins.
- Tests:
  - positive: act mode sends one season-pass update that unmonitors the later seasons, and one that monitors the promoted season;
  - negative: in observe mode, no Sonarr write reaches the fake server; a released series is never narrowed again;
  - boundary: a failed Sonarr write leaves the decision pending, and the next pass retries it;
  - planted defect: an actor that writes in observe mode makes the negative test fail.
- Gate: `make api-verify`, `make web-verify`, `make web-e2e` (gate settings and request view).
- Switch off: set the mode back to `observe` (writes stop at the next pass), or switch the gate off.

### Removal planning

Not planned here. It is planned once the season gate's observe run is complete (the revisit trigger under Order of delivery).

### Open points

- How a new series is noticed. Recommendation: the periodic pass compares Sonarr's series list with `watchaware_gate_series`; a Sonarr webhook can shorten the delay later, the poll stays the source of truth, as for watch state.

## Consequences

### Positive

- The fetch volume follows real interest: a show nobody watches costs one season of searches and disk, not all of them.
- Removal reuses SnatchArr's instances, Seerr link and Jellyfin account source, so no second application holds the same API keys.
- Observe mode, grace period, protections and per-run caps give several independent barriers before any file is deleted.

### Negative

- This is the first file deletion in SnatchArr. A rule mistake removes media, and recovery depends on the *arr recycle bin or a re-download.
- A held season looks partially available in Seerr, which users may read as a fault; the SnatchArr request view has to explain it.
- Plex users need Tautulli, which adds an application to their stack.
- Watch-state sync adds load on the media server; the poll interval and page size need bounds.

## Decisions taken

The four open questions of the first draft were answered on 2026-10-23:

| Question | Answer |
| :--- | :--- |
| Build remove-when-seen or link Maintainerr (`snatcharr-watch-build-or-link-20261023`) | Build the season gate now; build removal in SnatchArr after the gate's observe run; name Maintainerr as an alternative meanwhile |
| Hard or soft gate (`snatcharr-watch-gate-hard-20261023`) | Hard gate, observe mode first, tag `snatcharr-no-gate` exempts a series |
| Whose watch counts (`snatcharr-watch-whose-watch-20261023`) | Removal `all`, promotion `any`; the per-mode setting stays |
| Module name (`snatcharr-watch-module-name-20261023`) | Watch-aware modes, with a German equivalent; package `api/internal/watchaware` |

## Follow-ups

- [x] goenvoy: added `Played` and `PlayedPercentage` to `UserItemDataDto` in `mediaserver/jellyfin/v3.1.0` and `mediaserver/emby/v3.1.0` ([goenvoy#173](https://github.com/golusoris/goenvoy/pull/173)).
- [x] goenvoy: added `JellyfinUserID` and `JellyfinUsername` to `User` in `arr/seerr/v1.6.0` for the `requester` rule ([goenvoy#173](https://github.com/golusoris/goenvoy/pull/173)).
- [x] goenvoy: added collection writes (`CreateCollection`, `AddToCollection`, `RemoveFromCollection`) in `mediaserver/jellyfin/v3.1.0` for "leaving soon" ([goenvoy#173](https://github.com/golusoris/goenvoy/pull/173)).
- [x] Implementation plan: Slice 1 backend completed (persistence, HTTP API under `x-module` watchaware, leader loop ticking quiescent while module off; ADR-0025 Slice 5); season gate slices 2-3 and SPA follow in later slices.
- [x] goenvoy: add `ProviderIds` to `BaseItemDto` in `mediaserver/jellyfin` and `mediaserver/emby`, which season-gate promotion (Slice 2b) needs to map a media-server series to its Sonarr series (shipped in goenvoy mediaserver v3.2.0).
- [x] Docs: name Maintainerr as an alternative for removal until remove-when-seen ships ([docs/roadmap.md](../roadmap.md#watch-aware-modes)).

## References

- `snatcharr-watch-aware-modes-20261023` (2026-10-23T16:02:09) (paraphrased): SnatchArr lacks two watch-aware modes: removal of watched media in a configurable, cautious form, and a season gate that fetches only the first part of an unwatched show and narrows all-season requests until that part is watched. Concept first.
- `snatcharr-watch-build-or-link-20261023` (2026-10-23T17:10:36) (paraphrased): build the season gate now; removal follows later in SnatchArr after the gate's observe run; Maintainerr may be named meanwhile.
- `snatcharr-watch-gate-hard-20261023` (2026-10-23T17:10:36) (paraphrased): hard gate that unmonitors later seasons in Sonarr, observe mode first, an exemption tag per series.
- `snatcharr-watch-whose-watch-20261023` (2026-10-23T17:10:36) (paraphrased): default rules removal all and promotion any; the per-mode setting stays.
- `snatcharr-watch-module-name-20261023` (2026-10-23T17:10:36) (paraphrased): settings-page name Watch-aware modes with a German equivalent; package name unchanged.
