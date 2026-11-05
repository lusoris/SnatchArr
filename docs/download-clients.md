<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Download clients

SnatchArr watches the clients your *arr apps hand grabs to, so it stops snatching when the
grabs would only pile up. In observe mode, it never adds, pauses, removes or reorders a download, and it
resumes only downloads it paused itself, once act mode or attribution is withdrawn (`applyPriorityPlan` in
`api/internal/dlclients/priority_cleanup.go`).

Supported out of the box: **qBittorrent**, **Transmission**, **Deluge**, **rTorrent**,
**SABnzbd** and **NZBGet**.

Each adapter speaks the client's own API through the typed clients of
[goenvoy](https://github.com/golusoris/goenvoy) (`downloadclient/*/v2`):

| Client | API |
| --- | --- |
| qBittorrent | WebUI API v2 at `/api/v2` |
| Transmission | JSON-RPC at `/transmission/rpc`, or the path the stored URL names |
| Deluge | Web UI JSON-RPC at `/json` |
| rTorrent | XML-RPC behind an HTTP proxy |
| SABnzbd | `/api` with an API key |
| NZBGet | JSON-RPC at `/jsonrpc` |

No client version is named because none is tested: the adapters are checked against local
stand-ins that answer like the clients (`api/internal/dlclients/adapters_test.go`), not
against a running client, and none of the six publishes an OpenAPI spec for the contract
tests ([upstream contracts](upstream-contracts.md)).

## Getting them in

- **Discover**: `POST /api/v1/instances/{id}/download-clients/discover` reads the
  instance's own `/downloadclient` list and imports every supported client, credentials
  included. Clients the *arr no longer lists are removed again on the next discovery.
  Some *arr versions mask passwords in that list; those clients arrive without a secret
  and the result says so.
- **By hand**: `POST /api/v1/download-clients`. A hand-added client applies to every
  instance unless you pin it with `instance_id`.

`POST /api/v1/download-clients/test` observes credentials before you save them.

## What is observed

| Field | Meaning |
| --- | --- |
| `reachable` | The client answered and accepted the credentials |
| `paused` | Global pause (SABnzbd, NZBGet) |
| `active` | Downloads currently transferring |
| `queued` | Downloads waiting to start |
| `download_rate_bps` / `upload_rate_bps` | Current throughput |

For rTorrent `queued` is always 0. The adapter reads three flags per download (open, active,
complete), and none of them says that a download waits to start: a download counts as
`active` when it is open, active and not complete, and is not counted otherwise.

`GET /api/v1/download-clients/status` returns the latest observation of every enabled
client; observations are cached for 20 seconds.

## Foreplay: backpressure and pacing

| Setting | Effect |
| --- | --- |
| client unreachable or paused | The instance's snatches are deferred until it is back |
| `max_active` | Defer snatches while at least this many downloads are active (0 = no limit) |
| `max_queued` | Defer snatches while at least this many downloads are queued (0 = no limit) |
| `bandwidth_budget_bps` | Scale per-cycle counts by the share of the budget still free; a saturated budget defers (0 = off) |

Deferrals are recorded once as `snatch_deferred` and cleared with `snatch_resumed`; a paced
lease is recorded as a foreplay event naming the old and new per-cycle count. A client that
has never been observed does not block anything.

See [ADR-0006](adr/0006-download-client-backpressure.md) for the reasoning.

## Priority and queue ordering

SnatchArr coordinates queue priority across clients according to instance download ranks
([ADR-0022](adr/0022-instance-priority.md), [ADR-0046](adr/0046-download-client-priority-writes.md)).
Priority planning supports qBittorrent, NZBGet, and SABnzbd. Transmission, Deluge,
and rTorrent are excluded from priority groups and produce no priority plan, even
in observe mode (`priorityKind` in `api/internal/dlclients/priority_reconciler.go`).
Each supported client operates in either `observe` or `act` mode via `priority_mode`:

| Mode | Behaviour |
| --- | --- |
| `observe` (default) | Computes the priority plan, observes queue positions, and surfaces the intended mutations via the API and UI; executes no scheduling writes. Previously owned pauses are released when act mode or attribution is withdrawn. |
| `act` | Executes the plan for attributed transfers when every enabled row in the client group is configured for act mode. |

The reconciler processes at most 16 physical client groups per tick, rotating
through groups in configuration order. The final batch stops at the end before
the next tick wraps to the first group. With a stable set of G groups and
uninterrupted ticks, every group receives an observation attempt within
ceil(G / 16) ticks. Skipped groups retain their last successful plan in `Latest`
and their persisted pause ownership until their turn; cleanup of withdrawn
owned pauses also waits for that turn. A failed observation retains the previous
plan. `ObservedAt` identifies its age. Disabled or removed rows have their cached
plans pruned on the next tick. See `Tick`, `priorityBatch`, and `prunePlans` in
`api/internal/dlclients/priority_reconciler.go`.

### View and change the mode in the UI

Open **Connections → Download clients**, then choose **Priority plan** beneath a client's
name. The dialog refreshes every 30 seconds while open. It shows the effective mode,
observation time, priority levels before and after each move, queue order, and items to
pause or resume. Item identifiers come from the client. A client without an observation
shows **No observation yet**; an observed plan without moves shows an empty state.

As an administrator, choose **Edit**, select **Act** under **Priority mode**, then **Save**
to opt in for qBittorrent, NZBGet or SABnzbd. New clients default to **Observe**.
Transmission, Deluge and rTorrent remain observe only; their mode control is disabled.
Configarr-managed rows keep their existing read-only form behavior. The dialog names any
rows that keep a shared physical client in observe mode and warns when qBittorrent
queueing is disabled. Hand-moved qBittorrent items cannot be detected and are reordered
with other eligible queued items. See [ADR-0046](adr/0046-download-client-priority-writes.md).

### Act mode rules

When `act` mode is enabled:

- **Priority and ordering**: writes target queued items mapped to an instance category. Pause and resume may target active (downloading) lower-rank items; resume requires persisted SnatchArr pause ownership.
- **qBittorrent ordering**: With at least two distinct attributed ranks, SnatchArr items move ahead of unattributed queued items via `MoveToTop`. One distinct rank produces no priority or ordering writes (ADR-0046 Decision 3). Note that hand-moved qBittorrent items cannot be detected because qBittorrent does not provide a default queue position, so they are reordered alongside other eligible queued items. Queueing must be enabled in qBittorrent preferences.
- **NZBGet and SABnzbd priority**: hand-set priorities (values differing from default that were not set by SnatchArr) are preserved. SnatchArr-written priorities are updated as relative ranks shift.
- **Active transfer pausing**: lower-rank active items are paused while higher-rank items wait in queue on the same client.
- **Resumption**: SnatchArr resumes only items it previously paused, and only once higher-rank items on that client finish downloading. User-paused items are never resumed. Withdrawal of act mode or attribution releases only persisted SnatchArr-owned pauses.
- **Group mode rule**: when multiple enabled download client configurations resolve to the same physical client, writing occurs only when every enabled row in that group has `priority_mode: act`. If any enabled row is `observe`, the entire group remains in observe mode; only cleanup of previously owned pauses may write. Disabled rows are excluded from priority groups and never veto an enabled act row (`priorityGroups` in `api/internal/dlclients/priority_reconciler.go`).

Adding, removing, whole-queue pause/resume, and force starts remain strictly prohibited.
