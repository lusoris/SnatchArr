# ADR-0004: No Swaparr; Cleanuparr integration deferred

## Status

Accepted (2026-09-21)

## Context

newtarr bundled "Swaparr", a stalled-download reaper with a strike system. Cleanuparr does that job far better (per-client support, malware blocking, orphan cleanup) and additionally ships a *Seeker* that hunts missing and cutoff-unmet items — the same thing SnatchArr does. Cleanuparr's API is JWT-login only, so integrating with it means storing a Cleanuparr username and password.

## Decision

- SnatchArr ships **no** download-queue manipulation at all. Cleanuparr owns that domain.
- A read-only Cleanuparr integration (detect Seeker per instance and warn about double-hunting, skip recently struck items) is **deferred to v1.1**. v1 keeps the `internal/cleanuparr` package boundary with a `Disabled` implementation and hides the `respect_cleanuparr_strikes` policy flag.
- The README warns operators running both tools.

## Consequences

- No `cleanuparr_strikes` table or `/cleanuparr/*` routes in v1.
- Users of both tools must disable Seeker per instance manually until v1.1.
