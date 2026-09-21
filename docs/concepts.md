<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Concepts

SnatchArr's vocabulary is deliberately double-edged. Every term below maps to one exact
thing in the API, so you can enjoy the innuendo and still know what the numbers mean.

## Snatch

One run against one instance for one kind of item: a **missing snatch** goes after
monitored items that have no file, an **upgrade snatch** goes after items below their
quality cutoff. A snatch is planned by the API, leased by a worker, and ends with a list of
what was searched. Runs live under `/api/v1/runs`.

The planner queues a snatch when the instance's **refractory period** (`cycle_interval_s`,
default 900 s) has passed since the last one of that kind, nothing is paused by a schedule,
and no download client is holding things back.

## Per cycle

`missing_per_cycle` (default 1) and `upgrade_per_cycle` (default 0) say how many items a
single snatch goes after. Candidates are filtered **first** (monitored, released, not in
afterglow) and selected **after**, so a snatch is never short because it drew an unlucky
page. Selection is `random` (a random page, then random items) or `sequential` (a cursor
that walks the whole backlog).

## Afterglow

After an item has been snatched it rests. `processed_ttl_h` (default 168 h, one week) is how
long the afterglow lasts before the item may be searched again. Missing and upgrade
afterglows are tracked separately, so an upgrade search never blocks a missing one.
`POST /api/v1/state/reset` ends every afterglow at once.

## Stamina

`hourly_cap` (default 20, range 1 to 500) is how many items an instance may search per
clock hour. It is debited atomically at dispatch time, so two workers can never overspend
it together. At 80 % the dashboard shows the instance getting tired; at 100 % remaining
targets are deferred to the next hour with a `budget_acquired` event that says so.

!!! warning "Indexers ban impatient users"
    The cap exists because indexers count your API hits. Raising it above the default is
    your call; the UI warns you when you do.

## Foreplay

Download clients are observed, never driven ([ADR-0006](adr/0006-download-client-backpressure.md)).
When a client is unreachable, paused or already has `max_active` downloads, the instance's
snatches are deferred and history gets one `snatch_deferred` event (and one
`snatch_resumed` when it clears). With a `bandwidth_budget_bps`, the share of the budget
still free scales the per-cycle count down, so a busy line gets fewer new grabs.

## Quickie

A manual run-now (`POST /api/v1/instances/{id}/runs`). It skips the refractory period and
the schedule but still respects stamina and afterglow, and it shows up in history as a
quickie so nobody wonders why a snatch happened at 3 pm on a Tuesday.

## Schedules

Weekly windows that either **pause** snatching or **override the cap** for one instance or
for everyone. Windows may cross midnight and carry their own time zone.

## History and live events

Everything above is recorded as events (`/api/v1/events`) and streamed over Server-Sent
Events (`/api/v1/events/stream`). Event names are lower snake case: `run_queued`,
`run_started`, `page_fetched`, `candidates_filtered`, `budget_acquired`,
`search_dispatched`, `search_completed`, `run_finished`, `snatch_deferred`,
`snatch_resumed`.
