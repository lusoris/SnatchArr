<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0012: SnatchArr manages the download queue; the Cleanuparr link goes

- Status: accepted
- Date: 2026-10-17
- Supersedes: [ADR-0004](0004-no-swaparr-cleanuparr-deferred.md) in full,
  [ADR-0007](0007-read-only-cleanuparr-link.md) in full and
  [ADR-0009](0009-detect-cleanuparr-seeker.md) in full.
- Bounds: [ADR-0006](0006-download-client-backpressure.md) stands. SnatchArr still never
  talks to a download client directly.

## Context

ADR-0004 kept every download-queue action out of SnatchArr because Cleanuparr owned that
domain. ADR-0007 then linked Cleanuparr read-only, and ADR-0009 read its Seeker settings to
warn about double searching. As a result, an operator who wants stuck downloads cleared has
to run, configure and secure a second application next to SnatchArr, and SnatchArr carries a
client, a table, routes, a status page and a warning path whose only job is to cope with
that second application.

The queue items worth acting on are visible in the *arr queue API
(`GET /api/v3/queue`, `GET /api/v1/queue`), which SnatchArr already reads to skip queued items:

- `trackedDownloadState`: `downloading`, `importBlocked`, `importPending`, `failedPending`,
  `failed` and the others in the pinned specs.
- `trackedDownloadStatus`, `status` and `statusMessages`.
- `sizeleft` and `customFormatScore`.

The *arr queue API removes an item, removes it from the download client and blocklists the
release in one call: `DELETE .../queue/{id}` with `removeFromClient`, `blocklist` and
`skipRedownload`. Queue management therefore needs no download-client credentials and no
second application. It was requested in lusoris/SnatchArr#48, under epic
lusoris/SnatchArr#45.

## Decision

1. **SnatchArr manages the *arr download queue.** A periodic sweep classifies each queue
   item of each enabled instance into the rule classes stalled, slow, failed, import-blocked
   and unwanted. It removes an item only after the item breaks the same rule on a configured
   number of consecutive sweeps (strikes). The behaviour of each rule, its defaults and its
   planted test case are specified in lusoris/SnatchArr#362. That issue, not this record,
   is the reference for thresholds.
2. **Actions go through the *arr only.** A removal is `DELETE .../queue/{id}` with
   `removeFromClient=true` and `skipRedownload=true`. `blocklist=true` is set for every class
   except unwanted-because-unmonitored, so the *arr never grabs the same release again.
   SnatchArr calls no download-client API, and ADR-0006 stands.
3. **The replacement search is SnatchArr's own.** With `skipRedownload=true` the *arr does
   not search by itself. The item drops back into the wanted list, and SnatchArr's planner
   searches it like any other wanted item: under the instance's hourly cap and subject to
   processed memory and afterglow.
4. **The api decides and the sweep runs on the leader.** The sweep is a `leader_loops`
   member (`leaderx.Provide`). Strike state lives in Postgres with a per-row `expires_at`,
   and the api is its only writer, as it is for processed memory. Each sweep has a scalar
   bound on items read and on removals per instance (HISS-02, HISS-06).
5. **Observe first.** Queue management is configured per instance in the snatch policy.
   Every rule starts in observe mode. An observed strike is recorded and shown and emits an
   event, but the item is not removed. The operator switches a rule to act per instance.
6. **The Cleanuparr link is removed.** This covers the client package, the
   `cleanuparr_links` table (a down-migration drops it), the `/api/v1/cleanuparr` routes and
   the `seeker_overlap` field of the `Instance` schema. It also covers the status page, the
   Seeker badge and banner, the configuration keys, the docs and the tests. The goenvoy
   `arr/cleanuparr` package stays in goenvoy, and SnatchArr stops importing it.
7. **Unknown import blocks remove only for a bad match or an unrecognized line.** Added
   2026-10-25. The import-blocked rule covers items whose status lines classify as unknown.
   In act mode it removes an item only when its lines show a Lidarr or Readarr match below
   the fixable floor (`rematch_pending`) or a line no pattern recognizes (`unrecognized`).
   An item with an operational line (free space, locked or unpacking files, paths,
   permissions, sample detection) is never removed. Strikes, threshold, the per-sweep
   removal bound and removal through the *arr queue API apply unchanged, and observe stays
   the default.
8. **No outside code.** The rules are written from the pinned *arr OpenAPI specs and the
   behaviour issue. No code is copied from Cleanuparr or from any private script.

## Consequences

- **Positive:**
  - One application covers snatching and queue hygiene.
  - Operators drop a second service, its credentials and its configuration.
  - Each replacement search counts against the same hourly cap as every other search, so
    queue cleanup cannot cause a burst of searches.
  - The code that existed only to cope with Cleanuparr leaves (link, status page, Seeker
    detection).
- **Negative:**
  - SnatchArr now carries a destructive action. Observe mode is the default, and a rule
    acts only after the strike count and only within the per-sweep removal bound. Each rule
    ships a planted case that must fail before the rule is trusted.
  - The `/api/v1/cleanuparr` routes and the `seeker_overlap` field leave the public API.
    Their removal is a breaking change (HISS-14): the commit subject carries `!` and a
    `Migration:` footer.
  - Operators who keep Cleanuparr for its other features (malware blocking, orphan cleanup)
    must not let both tools act on the same queue. The docs say so. SnatchArr does not
    detect this case any longer.
- **Rollout:** the Cleanuparr deployment leaves the reference stack only after queue
  management runs green there. That step is tracked in #48 and is not part of this record.
