<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0006: Download clients are observed for backpressure, never driven

- Status: accepted
- Partially superseded by [ADR-0046](0046-download-client-priority-writes.md) (2026-11-01): priority and queue-order writes, and pause and resume of lower-rank items, for qBittorrent, NZBGet and SABnzbd. Adding, removing or deleting items and whole-queue operations stay forbidden; everything else below still binds.
- Date: 2026-09-21

## Context

Huntarr and newtarr only look at the *arr queue size before snatching. A snatch that succeeds
while the download client is unreachable, paused or saturated just piles up grabs the
client cannot take, and a bandwidth-starved client turns every extra search into a stalled
download. Users asked for the common torrent and Usenet clients to be first-class.

## Decision

SnatchArr observes qBittorrent, Transmission, Deluge, rTorrent, SABnzbd and NZBGet through
`goenvoy/downloadclient/*` and reduces each observation to one `Snapshot` (reachable,
paused, active, queued, download and upload rate). Client definitions are imported from
each *arr's `/downloadclient` list (secrets included, unless the *arr masks them) or added
by hand; a hand-added client applies to every instance unless it is pinned to one.

Two rules turn snapshots into snatching decisions:

1. **Backpressure gate** (planner, `snatch.Gate`): an unreachable or paused client, or one
   at its `max_active` limit, defers the instance's snatches. Transitions are recorded as
   `snatch_deferred` / `snatch_resumed` events, never one event per tick.
2. **Bandwidth pacing** (lease): with a `bandwidth_budget_bps`, the share of the budget
   still free scales `per_cycle` (`ceil(per_cycle * free)`, floor 1); a saturated budget
   blocks. The tightest client wins.

SnatchArr never adds, pauses, resumes, removes or reorders downloads (no Swaparr).
Observations are cached for 20 s and taken at most once per client per tick.

## Consequences

- The *arr queue ceiling (`max_queue_size`) stays as a cheap fallback for setups without
  a linked client.
- A client that was never observed does not block: ignorance is not backpressure.
- Secrets are encrypted at rest like *arr API keys and never returned by the API.

## Amendment 2026-10-26: Prowlarr grab path for types without an *arr

Ledger answer `snatcharr-oq-0036-14-grab-path`, recorded for
[ADR-0036](0036-community-requests-and-voting.md):

- Media types with no `*arr` target (games and ROMs, and books, audiobooks
  and comics when no Readarr-compatible, Kapowarr or Mylar3 target is
  connected) grab a chosen release through Prowlarr `GrabRelease`
  (`POST /api/v1/search`). Prowlarr hands the release to its own configured
  download client.
- SnatchArr still downloads nothing and never adds, pauses or removes a
  download in a client; the decision above stands for every client SnatchArr
  observes.
- No import step follows the grab. The library application picks the file
  up from the download folder; SnatchArr only detects availability.
- Each grab search debits the indexer hourly cap like an `*arr` search.
