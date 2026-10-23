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

No existing application gates seasons on watch state across Seerr requests at season granularity together with per-indexer search caps. Maintainerr covers most of remove-when-seen. Whether SnatchArr builds or links that half is open question 1.

## Decision

### Module

The two modes form one new module, **watch-aware modes**. The final display name is part of open question 4. It follows ADR-0018: its own package `api/internal/watchaware`, its own OpenAPI routes, its own tables and migrations, and off by default. The two modes have separate switches inside the module, so an operator can run the season gate without any removal.

Watch state is read through a `WatchSource` interface with one adapter per media server, on the existing goenvoy clients. Jellyfin and Emby come first, and Plex follows through Tautulli. A periodic sync runs as a `leader_loops` member (`gate.Provide[*T]()`). It stores per user, per item: played, last played and resume position. Webhooks can later shorten the delay; the poll stays the source of truth.

"Watched" is one shared predicate for both modes. An item counts as watched by a user when `Played` is true, or when `PlayedPercentage` is at or above a threshold (default 90 %). A season counts as watched when the watched share of its aired episodes reaches a threshold (default: all of them for removal, and the last aired episode or 50 % for promotion).

Whose watch counts is a per-mode setting:

- `requester`: the Seerr requester mapped to a Jellyfin user, falling back to `all` when the item has no request;
- `any`: any user with access;
- `all`: every active user with access.

### Season gate

1. **On add.** For a series that enters Sonarr (from Seerr, an import list or by hand), the gate leaves seasons `1..N` monitored (default N = 1) and unmonitors the later ones through `PUT /api/v3/series/{id}`. Specials stay as Sonarr set them. Series that already have files beyond the gate are never narrowed: the gate only holds back, it never removes.
2. **Seerr requests are not edited.** The request stays as the user made it; Seerr shows the show as partially available.
   - The SnatchArr request view marks the held seasons as "waiting for season N to be watched".
   - SnatchArr reads that state from its own gate table, not from Seerr.
3. **Promotion.** When the gate season counts as watched (under the configured "whose watch" rule), the next season is monitored, and SnatchArr's core search picks it up under the normal hourly caps.
   - The look-ahead setting (default 1) says how many seasons stay monitored ahead of the furthest watched one.
4. **Exemptions.**
   - A Sonarr tag (default `snatcharr-no-gate`) exempts a series.
   - So does a per-request override, which lets an admin release all seasons.
   - Series added before the gate was switched on are never touched.
5. **Observe first.** As with queue management, the gate starts in observe mode. It records what it would unmonitor and promote, and writes nothing to Sonarr until the operator switches it to act.

### Remove when seen

Removal is staged. Each stage can be inspected in the UI and audited in a table.

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

## Open questions

These are sent to the controller as DECISION lines; this ADR stays proposed until they are answered.

1. `snatcharr-watch-build-or-link-20261023`: build remove-when-seen in SnatchArr, or link Maintainerr for it and build only the season gate.
2. `snatcharr-watch-gate-hard-20261023`: hard gate (unmonitor in Sonarr) or soft gate (SnatchArr only skips the searches; Sonarr RSS can still grab later seasons).
3. `snatcharr-watch-whose-watch-20261023`: default "whose watch counts" rule for removal and for promotion.
4. `snatcharr-watch-module-name-20261023`: final module name, as shown on the settings page.

## Follow-ups

- goenvoy: confirm `UserData.PlayedPercentage` and `PlaybackPositionTicks` on the Jellyfin and Emby clients, and add a collection write for "leaving soon".
- Implementation plan, after the open questions are answered: watch-state sync first, then the season gate in observe mode, then removal in observe mode.

## References

- `snatcharr-watch-aware-modes-20261023` (2026-10-23T16:02:09) (paraphrased): SnatchArr lacks two watch-aware modes: removal of watched media in a configurable, cautious form, and a season gate that fetches only the first part of an unwatched show and narrows all-season requests until that part is watched. Concept first.
