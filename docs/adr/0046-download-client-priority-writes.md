<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: CC-BY-SA-4.0
-->
<!-- markdownlint-disable MD013 -->

# ADR-0046: Download Client Priority and Queue-Order Writes

- **Status**: Proposed (operator decision pending: ledger ids `snatcharr-dlclient-prio-adr0006`, `snatcharr-dlclient-prio-clients`, `snatcharr-dlclient-prio-mode`, `snatcharr-dlclient-prio-mapping`, `snatcharr-dlclient-prio-scope`)
- **Date**: 2026-11-01
- **Deciders**: lusoris
- **Related**: [ADR-0006](0006-download-client-backpressure.md), [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md), [ADR-0022](0022-instance-priority.md), issue #560

## Context

SnatchArr coordinates media snatches across `*arr` instances and respects per-instance hourly caps. While download backpressure and queue sanitation have been established, queue ordering inside external download clients remains unmanaged:

1. **Client queue ordering requested.** Issue #560 and operator direction (`SNX-DLCLIENT-PRIO`, node noted in [ADR-0022](0022-instance-priority.md)) request download ranking inside download clients, partitioned per category or tag. When an instance with higher priority snatches items, those downloads currently compete FIFO or in arbitrary order against lower-priority downloads in the download client.
2. **ADR-0006 strictly forbids driving clients.** [ADR-0006](0006-download-client-backpressure.md) (Decision, paragraph 3) specifies that "SnatchArr never adds, pauses, resumes, removes or reorders downloads (no Swaparr)." Adapters in [api/internal/dlclients/adapters.go](../../api/internal/dlclients/adapters.go) are strictly read-only (`Snapshotter`, `Goenvoy.Snapshot`), reducing client queries to backpressure and bandwidth metrics.
3. **Queue actions use observe-first semantics.** [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md) established automated queue hygiene, enforcing that queue operations start in observe mode before act mode is enabled per instance, and route removals strictly through the `*arr` API rather than download client APIs.
4. **Download rank is already defined.** [ADR-0022](0022-instance-priority.md) introduced per-instance priorities with an integer rank from 1 to 100, where lower rank runs first ([api/internal/domain/priority.go](../../api/internal/domain/priority.go), `ResolveDownload`, preset `media_first`). ADR-0022 explicitly stated that ordering inside download clients would follow in node `SNX-DLCLIENT-PRIO` reading this same rank.

### Capability matrix

Inspecting the pinned `goenvoy` download client packages in [go.mod](../../go.mod) reveals asymmetric support for priority and queue manipulation across supported clients:

| Client | goenvoy package | Queue-order / priority write capability |
| :--- | :--- | :--- |
| qBittorrent | `downloadclient/qbit/v2` `v2.0.3` | `TopPriority`, `BottomPriority`, `IncreasePriority`, `DecreasePriority` (`qbit.go:680-701`); acts only with qBittorrent torrent queueing enabled |
| NZBGet | `downloadclient/nzbget/v2` `v2.0.3` | `SetPriority(id, priority)` (`nzbget.go:284`), `EditQueue` (`nzbget.go:175`) |
| SABnzbd | `downloadclient/sabnzbd/v2` `v2.0.3` | None (`GetQueue`, `SetCategory` only) |
| Transmission | `downloadclient/transmission/v2` `v2.0.3` | None (`MoveTorrents` moves storage, not queue) |
| Deluge | `downloadclient/deluge/v2` `v2.0.3` (latest `v2.0.4` same) | None |
| rTorrent | `downloadclient/rtorrent/v2` `v2.1.2` | None |

## Decision (proposed)

This ADR proposes **Option a: partial supersession of ADR-0006 for priority and queue-order writes only**.

1. **Permitted mutations.** SnatchArr may adjust the queue position or priority level of queued items that it can attribute to an instance (by the instance's configured download-client category), exclusively using the client write methods identified above.
2. **Forbidden mutations.** Adding, pausing, resuming, and removing downloads remain strictly prohibited inside download clients. These actions continue to be governed by [ADR-0006](0006-download-client-backpressure.md) and [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md) (item removals route exclusively through the `*arr` queue API).
3. **Observe mode first.** In alignment with ADR-0012, download client priority management operates in observe mode first. Planned priority shifts and reordering actions are computed, logged, and surfaced in the UI. Mutations are executed against download clients only when act mode is explicitly enabled.
4. **Leader loop execution.** Priority reconciliation runs periodically on the elected leader replica as a member of the fx group `leader_loops` (`gate.Provide`), avoiding split-brain queue reorders across multiple API replicas.
5. **Release boundary.** No write paths or mutating client calls will be shipped until this ADR is accepted and the pending operator decisions below are resolved.

## Open questions

The following open questions are pending operator decision (fleet ledger ids `snatcharr-dlclient-prio-adr0006`, `snatcharr-dlclient-prio-clients`, `snatcharr-dlclient-prio-mode`, `snatcharr-dlclient-prio-mapping`, `snatcharr-dlclient-prio-scope`):

| ID | Ledger ID | Question | Options |
| :--- | :--- | :--- | :--- |
| `OQ-0046-0` | `snatcharr-dlclient-prio-adr0006` | How far does the supersession of ADR-0006 go? | a) priority and queue-order writes only; add, pause, resume, remove stay forbidden b) also pause and resume of low-rank items while high-rank items wait |
| `OQ-0046-1` | `snatcharr-dlclient-prio-clients` | Which clients get priority writes in the first release? | a) qBittorrent and NZBGet now, SABnzbd after a goenvoy addition, the others later b) qBittorrent, NZBGet and SABnzbd, with a goenvoy SABnzbd priority method first c) all six, with goenvoy additions for Transmission, Deluge and rTorrent |
| `OQ-0046-2` | `snatcharr-dlclient-prio-mode` | What is the default mode? | a) observe by default per download client, act is opt-in per client b) act by default c) off by default, the user picks observe or act |
| `OQ-0046-3` | `snatcharr-dlclient-prio-mapping` | How does the rank (1-100) become a client priority, and where does the category come from? | a) category read from each instance's *arr download-client entry; distinct ranks split into the client's levels (never force), qBittorrent queue sorted by rank then added time b) the user maps category or tag to rank by hand per client c) a, plus a manual override per category |
| `OQ-0046-4` | `snatcharr-dlclient-prio-scope` | Which items does the reconciler touch? | a) queued items only, a manual priority change is overwritten b) queued items only, items whose priority differs from the client default are left alone c) queued and active items |

## Consequences

### Positive

- Higher-priority media items (e.g. newly broadcast TV episodes or requested films) gain front-of-queue placement in download clients, reducing time-to-availability.
- Automates client queue management across heterogeneous downloaders without requiring operators to manually adjust items or run secondary scripts.
- Reuses the existing instance download priority model (`ResolveDownload`, preset `media_first`) defined in [ADR-0022](0022-instance-priority.md).

### Negative

- Partially relaxes the boundary in [ADR-0006](0006-download-client-backpressure.md) that download clients are observed only and never driven, slightly increasing operational complexity.
- Uneven downloader feature parity: qBittorrent and NZBGet can be reordered or reprioritised, whereas SABnzbd, Transmission, Deluge, and rTorrent are restricted to observation until upstream libraries add support.
- qBittorrent queue ordering depends on the client's internal "Torrent Queueing" setting being enabled; if disabled, queue priority API calls succeed without affecting transfer order.

### Status and governance

- [ADR-0006](0006-download-client-backpressure.md) is not modified while this ADR is in **Proposed** status. Upon formal acceptance of this ADR, ADR-0006 will be annotated with a status note: "Partially superseded by ADR-0046 for priority and queue-order writes only."
- **Class defence planned**: A static analysis check (semgrep rule or AST linter in `tools/ci/`) will enforce that `api/internal/dlclients` cannot invoke any mutating methods on `goenvoy` clients outside the explicitly allowed priority methods (`TopPriority`, `BottomPriority`, `IncreasePriority`, `DecreasePriority` for qBittorrent; `SetPriority`, `EditQueue` for NZBGet). Direct calls to add, pause, resume, or delete downloads will fail gating.

## References

- Operator direction of 2026-10-20 (paraphrased): rank downloads inside download clients according to category or tag, reading the instance download priority.
- Ledger entry: `snatcharr-dlclient-priority-20261020`
- Issue #560: download rank inside clients, per category or tag.
- [ADR-0006: Download clients are observed for backpressure, never driven](0006-download-client-backpressure.md)
- [ADR-0012: SnatchArr manages the download queue; the Cleanuparr link goes](0012-queue-management-replaces-cleanuparr-link.md)
- [ADR-0022: Per-instance priority for searches and downloads](0022-instance-priority.md)
- Priority domain logic: [api/internal/domain/priority.go](../../api/internal/domain/priority.go) (`ResolveDownload`, `PresetMediaFirst`)
- Pinned goenvoy client packages in [go.mod](../../go.mod)
