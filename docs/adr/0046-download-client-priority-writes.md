<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: CC-BY-SA-4.0
-->
<!-- markdownlint-disable MD013 -->

# ADR-0046: Download Client Priority and Queue-Order Writes

- **Status**: Accepted (open questions OQ-0046-0 to OQ-0046-4 answered by the operator on 2026-11-01; implementation follows in the slices below)
- **Date**: 2026-11-01
- **Deciders**: lusoris
- **Related**: [ADR-0006](0006-download-client-backpressure.md), [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md), [ADR-0022](0022-instance-priority.md), issue #560, PR #969

## Context

SnatchArr coordinates media snatches across `*arr` instances and respects per-instance hourly caps. While download backpressure and queue sanitation have been established, queue ordering inside external download clients remains unmanaged:

1. **Client queue ordering requested.** Issue #560 and operator direction (`SNX-DLCLIENT-PRIO`, node noted in [ADR-0022](0022-instance-priority.md)) request download ranking inside download clients, partitioned per category or tag. When an instance with higher priority snatches items, those downloads currently compete FIFO or in arbitrary order against lower-priority downloads in the download client.
2. **ADR-0006 established a read-only boundary.** [ADR-0006](0006-download-client-backpressure.md) originally prohibited adding, pausing, resuming, removing and reordering downloads. Adapters in `api/internal/dlclients/adapters.go` remain read-only (`Snapshotter`, `Goenvoy.Snapshot`), reducing client queries to backpressure and bandwidth metrics. This record narrows that boundary; it does not implement writes.
3. **Queue actions use observe-first semantics.** [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md) established automated queue hygiene, enforcing that queue operations start in observe mode before act mode is enabled per instance, and route removals strictly through the `*arr` API rather than download client APIs.
4. **Download rank is already defined.** [ADR-0022](0022-instance-priority.md) introduced per-instance priorities with an integer rank from 1 to 100, where lower rank runs first (`api/internal/domain/priority.go`, `ResolveDownload`, preset `media_first`). ADR-0022 explicitly stated that ordering inside download clients would follow in node `SNX-DLCLIENT-PRIO` reading this same rank.
5. **Read-side discovery has landed.** PR #969 (squash `82a4a065`) added `api/internal/dlclients/priority_live_test.go` and extended `tools/e2e/dlclients_live.sh`. Its live run on 2026-11-01 (`tools/e2e/dlclients_live.sh` against the pinned clients) observed all six clients and refused the planted capability mismatch. This establishes read-side evidence, not write-path verification.

### Capability matrix

The following capabilities were measured on 2026-11-01 against the versions in `go.mod`. Package names are relative to `github.com/golusoris/goenvoy/`; daemon source references appear below.

| Client | Pinned goenvoy package | Queue-order / priority | Per-item pause / resume | Forbidden or unavailable path |
| :--- | :--- | :--- | :--- | :--- |
| qBittorrent | `downloadclient/qbit/v2` `v2.0.3` | `TopPriority`, `BottomPriority`, `IncreasePriority`, `DecreasePriority` (`qbit.go:680-701`) | `StopTorrents`, `StartTorrents` **to be added** for 5.x | Existing `PauseTorrents`, `ResumeTorrents` (`qbit.go:375,383`) POST to legacy `/api/v2/torrents/pause` and `/api/v2/torrents/resume`, absent in 5.3.0, the pinned E2E image |
| NZBGet | `downloadclient/nzbget/v2` `v2.0.3` | `SetPriority` wraps `EditQueue` action `GroupSetPriority` (`nzbget.go:284`) | `EditQueue` with `GroupPause` / `GroupResume`, restricted to explicit item IDs | `PauseDownload`, `ResumeDownload` (`nzbget.go:184,193`) affect the whole queue: forbidden |
| SABnzbd | `downloadclient/sabnzbd/v2` `v2.0.3` | `SetPriority` **to be added**, using `mode=queue`, `name=priority` | `PauseItem`, `ResumeItem` per `nzo_id` (`sabnzbd.go:170,179`) | `Pause`, `Resume` (`sabnzbd.go:160,165`) affect the whole queue: forbidden |
| Transmission | `downloadclient/transmission/v2` `v2.0.3` | Read probe exists; no writes admitted in this release | No writes admitted | Observe only; `MoveTorrents` changes storage, not queue order |
| Deluge | `downloadclient/deluge/v2` `v2.0.3` | Read probe exists; no writes admitted in this release | No writes admitted | Observe only |
| rTorrent | `downloadclient/rtorrent/v2` `v2.1.2` | Read probe exists; no writes admitted in this release | No writes admitted | Observe only |

At [`torrentscontroller.h` on `release-5.3.0`](https://github.com/qbittorrent/qBittorrent/blob/release-5.3.0/src/webui/api/torrentscontroller.h#L72-L73), the version the E2E compose (`tools/e2e/dlclients/compose.yaml`) pins, qBittorrent exposes `startAction` / `stopAction`, not pause / resume; the priority actions are at lines 101–104. goenvoy therefore adds `StartTorrents` / `StopTorrents` (POST `/api/v2/torrents/start` / `/api/v2/torrents/stop`) before pause and resume ship. S1 and S5 check the calls against the pinned release.

## Decision

### 1. Boundary and client allow-list

Partially supersede [ADR-0006](0006-download-client-backpressure.md) for priority, queue-order and per-item pause/resume writes. The rationale recorded in `OQ-0046-0` is that scheduling requires control over the client's execution order. The first #560 release includes qBittorrent, NZBGet and SABnzbd; Transmission, Deluge and rTorrent remain observe only (`OQ-0046-1`).

Only these write calls are permitted, subject to the item and mode rules below:

| Client | Allowed goenvoy write calls | Restrictions |
| :--- | :--- | :--- |
| qBittorrent | `TopPriority`, `BottomPriority`, `IncreasePriority`, `DecreasePriority`; `StopTorrents`, `StartTorrents` (**to be added**) | Explicit item hashes only; stop/start implement per-item pause/resume; no force start |
| NZBGet | `SetPriority`; `EditQueue` with literal `GroupPause` or `GroupResume` | Explicit group IDs only; `SetPriority` internally uses `GroupSetPriority`; no other direct `EditQueue` action |
| SABnzbd | `SetPriority` (**to be added**); `PauseItem`, `ResumeItem` | Explicit `nzo_id` only; normal priority levels only |

Adding, deleting/removing, whole-queue pause/resume, force start, category changes and storage changes remain forbidden. In particular, NZBGet `PauseDownload` / `ResumeDownload` and SABnzbd `Pause` / `Resume` are not substitutes for per-item methods. Removals continue exclusively through the `*arr` queue API under [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md). The goenvoy additions in S1 are release prerequisites, not permissions to fall back to other endpoints.

### 2. Observe by default, act per client

Every download client defaults to observe mode; act requires explicit opt-in for that client. Existing clients remain observe on upgrade (`OQ-0046-2`), following the observe-first model of [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md). In observe mode the same planner computes priority, queue-order, pause and resume moves but executes no writes. Every planned and executed move is logged and surfaced through the API and UI, with client, item, reason, old and intended state, mode and execution outcome (S3–S5).

### 3. Attribution, rank mapping and manual priorities

Read the category from each instance's `*arr` download-client entry and use its effective download rank from `ResolveDownload` in `api/internal/domain/priority.go`, as answered in `OQ-0046-3`. Only items attributable to an instance through that client/category participate; unattributed items remain untouched.

Split sorted distinct ranks across the client's ordinary priority levels: best rank maps to high, worst to low, and force levels are never used. Equal ranks map to the same level; when ranks outnumber levels, adjacent ranks share a level without reversing their order. Empty input yields no moves. With one distinct rank, map all items to the client's default/normal level and plan no priority or queue-order write; hand-set priorities remain untouched. For qBittorrent, order eligible queued items by rank ascending, then added time ascending. S2 supplies unit and property tests for these rules.

Priority and queue-order writes affect queued items only. Preserve hand-set priorities: a priority is hand-set when it differs from the client's default and is not the value last recorded as set by SnatchArr. Record SnatchArr's own priority writes per client/item so they are not mistaken for manual choices, and exclude hand-set items from priority writes (`OQ-0046-4`; S4 regression test). qBittorrent has no default queue position to compare against, so a hand-moved qBittorrent item cannot be detected and is reordered with the other eligible queued items; the UI states this limit.

### 4. Pause ownership and resumption

The queued-only limit for priority changes does not prevent pausing an active lower-rank transfer. Pause it only while a higher-rank item on the same download client waits, meaning queued and not downloading. Equal ranks do not trigger a pause. Resume once the higher-rank items have finished, that is when no higher-rank item on that client is waiting or downloading (`OQ-0046-0`, widening `OQ-0046-4`). Resuming as soon as nothing waits would flap: the waiting item starts downloading, the paused item resumes and competes with it again.

Persist which client/items SnatchArr paused. Resume only those items, never an item paused by the user; an absent ownership record authorizes no resume. The pause plan and ownership transitions belong to S2–S4, with lifecycle and user-paused-item tests in S4–S5.

### 5. Leader execution

The reconciler runs periodically on the elected API leader as an fx `leader_loops` member registered through `gate.Provide[*T]()`, following `api/internal/snatch/module.go`. S3 computes and exposes plans; S4 adds only the allow-listed writes behind the per-client act setting.

## Answered questions

The operator answered all five questions on 2026-11-01. Answers are paraphrased; `OQ-0046-0` widens the pause/resume scope of `OQ-0046-4`.

| ID | Ledger ID | Answer letter | Answer |
| :--- | :--- | :--- | :--- |
| `OQ-0046-0` | `snatcharr-dlclient-prio-adr0006` | b | Supersede ADR-0006 for priority, queue order and pausing/resuming lower-rank items while higher-rank work waits; add/remove stay forbidden. |
| `OQ-0046-1` | `snatcharr-dlclient-prio-clients` | b | Ship qBittorrent, NZBGet and SABnzbd in the first #560 release, after the goenvoy SABnzbd priority addition; the other three remain observe only. |
| `OQ-0046-2` | `snatcharr-dlclient-prio-mode` | a | Observe by default per client, including upgrades; act is opt-in per client. |
| `OQ-0046-3` | `snatcharr-dlclient-prio-mapping` | a | Read the category from the instance's `*arr` client entry; split distinct ranks across non-force levels; qBittorrent sorts by rank, then added time. |
| `OQ-0046-4` | `snatcharr-dlclient-prio-scope` | b, widened by OQ-0046-0 | Priority writes touch queued items and preserve hand-set priorities; pause/resume may control active lower-rank transfers. |

## Implementation slices

Acceptance records the decision; these slices remain implementation work and execute in order.

| Slice | Deliverable | Verification required before proceeding |
| :--- | :--- | :--- |
| S1 | In `golusoris/goenvoy`, add SABnzbd `SetPriority` first and qBittorrent `StopTorrents` / `StartTorrents`; release, then bump the pins in `go.mod`. | Per-method request/response tests, including rejected requests; verify qBittorrent uses `/stop` / `/start` and SABnzbd uses `mode=queue`, `name=priority`. |
| S2 | Pure domain planner for rank-to-level splitting, qBittorrent queue order and pause/resume plans. | Unit and property tests: ties, one rank, empty input, rank endpoints, excess ranks sharing levels, added-time ordering, same-client waiting condition and pause ownership. |
| S3 | Leader reconciler in observe mode; log plans and expose them through the API and UI. | Fake client log contains no write calls in observe; upgrade test keeps existing clients in observe; API/UI show each planned move. |
| S4 | Per-client act opt-in and the allow-listed writes, recording priority and pause ownership. | Each client's fake log contains only permitted methods, actions and explicit item IDs; hand-set priority stays unchanged; pause/resume lifecycle and user-paused-item regressions pass. |
| S5 | Per-client fakes in dev compose (`deploy/compose/docker-compose.yml`) and E2E coverage for each of qBittorrent, NZBGet and SABnzbd. | Through the served API/UI, demonstrate reorder/priority and pause/resume for each client; retain the live read probe (`tools/e2e/dlclients_live.sh`) and verify writes against pinned real releases. |

## Class defence

S4 must add a Semgrep rule to `.semgrep.yml`, with its fixture under `tools/ci/semgrep/testdata`. Across `api/`, refuse any goenvoy download-client method call outside an explicit per-client allow-list comprising existing read-only setup/observation/probe calls and exactly the writes in Decision 1. Match client identity, including import aliases, rather than allowing any receiver with a matching method name. For NZBGet, reject direct `EditQueue` calls unless the action is literal `GroupPause` or `GroupResume` with explicit IDs; the generic method must not permit other actions. Whole-queue targets are forbidden for every client.

Fixtures must include allowed read and write calls and planted forbidden calls, including alias imports, whole-queue pause/resume and a forbidden `EditQueue` action. Replay the planted violations with the rule absent to show the missing defence, then with the rule present to show refusal, using the existing fixture convention documented in `.semgrep.yml`. This documentation change adds no executable check.

S3–S5 must also supply independent behavioural tests: no writes in the fake client log during observe; an existing client remains observe after upgrade; a hand-set item retains its priority; an active lower-rank item pauses only while higher-rank work on that client waits and resumes after the higher-rank items finish; a user-paused item never resumes. Each new check ships a planted negative case and is run once without its fix.

## Consequences

### Positive

- S2–S5 extend the existing `ResolveDownload` model from [ADR-0022](0022-instance-priority.md) into client execution order, including pausing competing active transfers under Decision 4.
- Per-client observe mode exposes intended moves before opt-in, while pause ownership and manual-priority detection preserve user choices (Decision 2–4).

### Negative

- [ADR-0006](0006-download-client-backpressure.md) now permits the scoped priority, queue-order and per-item pause/resume writes in Decision 1; add/remove and whole-queue pause/resume remain prohibited. Reconciliation requires durable priority and pause ownership records (Decision 3–4).
- SABnzbd priority and qBittorrent 5.x pause/resume cannot ship before the S1 goenvoy additions and pin bump. Transmission, Deluge and rTorrent remain observe only (`OQ-0046-1`).
- qBittorrent queueing must be enabled for queue order to take effect; the existing priority probe (`api/internal/dlclients/priority_live_test.go`) checks `QueueingEnabled`. Report the missing capability rather than enabling queueing through an unlisted settings write.
- A continuing stream of higher-rank waiting items can keep lower-rank transfers paused; Decision 4 authorizes resumption only after the higher-rank items finish.

### Status and governance

- This accepted record and the status note in [ADR-0006](0006-download-client-backpressure.md) describe the same partial supersession. Acceptance is not evidence that S1–S5 or the class defence have shipped.

## References

- Operator direction of 2026-10-20 (paraphrased): rank downloads inside download clients according to category or tag, reading the instance download priority.
- Ledger entry: `snatcharr-dlclient-priority-20261020`
- Issue #560: download rank inside clients, per category or tag.
- [ADR-0006: Download clients are observed for backpressure, never driven](0006-download-client-backpressure.md)
- [ADR-0012: SnatchArr manages the download queue; the Cleanuparr link goes](0012-queue-management-replaces-cleanuparr-link.md)
- [ADR-0022: Per-instance priority for searches and downloads](0022-instance-priority.md)
- Priority domain logic: `api/internal/domain/priority.go` (`ResolveDownload`, `PresetMediaFirst`)
- Pinned goenvoy client packages in `go.mod`
- Operator answers, 2026-11-01 (paraphrased): `OQ-0046-0` / `snatcharr-dlclient-prio-adr0006`, `OQ-0046-1` / `snatcharr-dlclient-prio-clients`, `OQ-0046-2` / `snatcharr-dlclient-prio-mode`, `OQ-0046-3` / `snatcharr-dlclient-prio-mapping`, `OQ-0046-4` / `snatcharr-dlclient-prio-scope`; see Answered questions.
- PR #969, squash `82a4a065`: read-side priority capability test (`api/internal/dlclients/priority_live_test.go`), live runner (`tools/e2e/dlclients_live.sh`), and pinned clients (`tools/e2e/dlclients/compose.yaml`); live run 2026-11-01.
- qBittorrent [`release-5.3.0`, `src/webui/api/torrentscontroller.h:72-73`](https://github.com/qbittorrent/qBittorrent/blob/release-5.3.0/src/webui/api/torrentscontroller.h#L72-L73), with priority actions at [lines 101–104](https://github.com/qbittorrent/qBittorrent/blob/release-5.3.0/src/webui/api/torrentscontroller.h#L101-L104), commit `05fcaa8f505f`, read 2026-11-01.
- NZBGet [`daemon/remote/XmlRpc.cpp:2422-2423`](https://github.com/nzbgetcom/nzbget/blob/eb68e5e1f801f0ee0587e220a9108dd49ed8538c/daemon/remote/XmlRpc.cpp#L2422-L2423): `GroupPause` / `GroupResume`; commit `eb68e5e1f801`, read 2026-11-01.
- SABnzbd [`sabnzbd/api.py:1281`, `_api_queue_priority`](https://github.com/sabnzbd/sabnzbd/blob/432d49d38976d9f560cd429ca953df1c162a7472/sabnzbd/api.py#L1281): `mode=queue`, `name=priority`; commit `432d49d38976`, read 2026-11-01.
