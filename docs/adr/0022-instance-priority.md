<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0022: Per-instance priority for searches and downloads

- Status: proposed
- Date: 2026-10-21

## Context

SnatchArr (pronounced "snatch-arr": every search is a snatch, *-arr* is the \*arr family suffix) treats every \*arr instance alike today:

- The worker leases queued runs oldest first (`LeaseRun` in `api/internal/store/queries/runs.sql`, `ORDER BY queued_at`). When workers are busy or the shared stamina (`global_hourly_cap`, `api/internal/domain/settings.go`) runs short, whichever instance queued first takes the searches.
- The queue sweep walks instances in database order (`SweepAll` in `api/internal/queue/service.go`), so the force imports and removals of a Whisparr queue can run before those of Sonarr.

The operator asked (2026-10-20, node `SNX-INSTANCE-PRIO`, paraphrased) for a priority per instance: a sensible default puts movies and TV first, music next and adult content last, and each user sets the order for searches and the order for downloads separately. The default is a shipped preset the user edits, not a fixed constant.

## Decision

### 1. Two orders, one rank each

Every instance carries two ranks: a search rank and a download rank. A rank is an integer from 1 to 100; a lower rank goes first. Equal ranks share their place.

- **Search order** decides which queued run a worker leases next, and so which instance spends the indexer budget and the shared stamina first.
- **Download order** decides the order in which the queue sweep visits instances, so imports, removals and re-searches of a higher instance run first in each sweep. Ordering inside download clients follows in node `SNX-DLCLIENT-PRIO`, which reads the same rank; this record does not change any download client.

### 2. Shipped preset, editable

The ranks come from a preset keyed by instance kind. SnatchArr ships one preset, `media_first`:

| Kind | Search rank | Download rank |
| :--- | ---: | ---: |
| `sonarr`, `radarr` (TV and movies) | 10 | 10 |
| `lidarr` (music) | 20 | 20 |
| `readarr` (books) | 30 | 30 |
| `whisparr_v2`, `whisparr_v3` (adult) | 40 | 40 |

Books are not named in the request; they sit after music and before adult content, and the user can move them. The steps of ten leave room to slot a kind in between without renumbering.

- The active preset is stored in the global settings row and edited through `PUT /api/v1/settings` like every other setting (`search_priority_preset`, `download_priority_preset`). A stored preset names every instance kind; a preset that leaves one out is refused, so the lease query never needs a fallback.
- `GET /api/v1/priority-presets` returns the shipped presets, so the UI restores `media_first` from the server instead of a copy in the web code.
- An instance may override either rank through `GET`/`PUT /api/v1/instances/{instanceId}/priority` (`search_priority`, `download_priority`, null = follow the preset). The response carries the effective rank and its source (`preset` or `instance`) for each order. Clearing an override returns the instance to the preset.
- The priority endpoint is separate from the instance definition, like the snatch policy, so Configarr-managed instances accept it while their definition stays read-only.

### 3. No starvation

Rank ordering alone could hold a low-ranked run back for as long as higher runs keep arriving. A queued run older than `priority_max_wait` (one hour, a constant in `api/internal/snatch`) sorts before every younger run regardless of rank. Inside each group the order is rank, then `queued_at`. The bound keeps the wait of the lowest instance finite (HISS-02) while the preset still decides every ordinary contention.

### 4. Tests

Each consumer ships positive, negative and boundary tests:

- Positive: with `media_first`, a Sonarr run queued after a Whisparr run is leased first; the sweep visits Sonarr before Whisparr.
- Negative: ranks outside 1–100, unknown kinds in a preset and malformed overrides are refused with RFC 9457 problem details; a cleared override falls back to the preset.
- Boundary: equal ranks keep `queued_at` order; a run past `priority_max_wait` overtakes a higher rank; ranks 1 and 100 are accepted.
- Planted case: a lease query that ignores the rank (back to `ORDER BY queued_at`) turns the positive test red.

## Consequences

- **Positive:** the operator's ordering applies out of the box; scarce stamina goes to the media the user cares about first; search and download orders can differ (for example, music searched before books but imported after them).
- **Neutral:** existing installs get `media_first` on upgrade; no instance carries an override until the user sets one. With idle workers and spare stamina the order changes nothing, because every due run is leased at once.
- **Negative:** a lowest-ranked instance can wait up to `priority_max_wait` longer than today under contention.

## Alternatives rejected

1. **Weights (share of the budget per instance) instead of ranks:** rejected for the first version. Ranks answer "who goes first" directly and are easy to read in the UI; weights need a fair-share scheduler over the stamina buckets and can follow if measured contention calls for them.
2. **A fixed order in code:** rejected; the request asks for a preset the user edits.
3. **One shared order for searches and downloads:** rejected; the request asks for separate orders.

## References

- Node `SNX-INSTANCE-PRIO` (2026-10-20) (paraphrased): instances get priorities; the default puts movies and TV first, music next and adult content last; users set search and download orders separately; the default is an editable shipped preset.
- Node `SNX-DLCLIENT-PRIO` (2026-10-20) (paraphrased): applies the instance priorities inside download clients per category or tag.
- [ADR-0006](0006-download-client-backpressure.md), [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md), [ADR-0018](0018-product-front-door.md), [ADR-0020](0020-snatch-policy-defaults-and-presets.md).
