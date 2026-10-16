<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Cleanuparr

[Cleanuparr](https://github.com/Cleanuparr/Cleanuparr) strikes and removes downloads
that stall, crawl, fail to import or turn out dead. SnatchArr decides what to search for
next. The two do not overlap ([ADR-0007](adr/0007-read-only-cleanuparr-link.md)),
and SnatchArr shows what Cleanuparr is doing right next to its own snatches.

## Linking

`POST /api/v1/cleanuparr` with the Cleanuparr URL and an API key (Cleanuparr → Settings
→ General). Nothing is ever written to Cleanuparr.

## What you see

`GET /api/v1/cleanuparr/status` (cached 30 s) returns, per link:

| Field | Meaning |
| --- | --- |
| `reachable`, `version`, `started_at` | Server health |
| `media_managers` | Instance count per *arr type Cleanuparr manages |
| `struck_downloads` | Downloads that currently carry strikes |
| `marked_for_removal`, `removed_downloads` | What Cleanuparr is about to, or already did, remove |
| `recent_strikes` | The latest strikes with type, title and download id |

## How it fits snatching

A download that Cleanuparr is striking is still in the *arr queue, so SnatchArr already
skips its item (see [Concepts](concepts.md#per-cycle)). When Cleanuparr removes a download,
the item becomes missing again; Cleanuparr can trigger a search itself and SnatchArr's
afterglow keeps its own next attempt from piling on. Strikes are keyed by download, not
by *arr item, so SnatchArr does not try to map them back to episodes or movies.

## Seeker and double-search warnings

Cleanuparr includes a subsystem called **Seeker** that can periodically search Sonarr and Radarr libraries for missing and cutoff-unmet items.

When Cleanuparr Seeker's proactive search is enabled for an instance that SnatchArr is also searching, both services query indexers for the same library simultaneously. This duplicates indexer load, consumes stamina rapidly, and risks rate-limit bans from indexers ([ADR-0009](adr/0009-detect-cleanuparr-seeker.md)).

### Detection

SnatchArr inspects Seeker's configuration via the read-only Cleanuparr link. A library is flagged for **Seeker overlap** when:

1. Seeker search and proactive search are enabled in Cleanuparr.
2. The specific *arr instance is enabled in both Cleanuparr and Seeker.
3. Cleanuparr is not running in global dry-run mode.
4. The instance base URL matches an active SnatchArr instance (normalised URL matching).

When overlap is detected:

- A warning badge appears in the **Instances** list.
- An alert banner appears on the **Instance Detail** page explaining that both tools are actively searching the library.
- The **Cleanuparr** status page displays the observed Seeker state and individual instance matches.

*(Note: Replacement-only searches triggered by item removals do not trigger a warning, as SnatchArr's afterglow already accommodates them.)*

### How to resolve

To eliminate double searching, you have two options:

1. **Pause SnatchArr for this instance**: Click the **Pause SnatchArr for this instance** button on the instance warning banner. SnatchArr disables snatch cycles for this library while keeping its configuration, history, and monitoring intact until you re-enable it.
2. **Disable Seeker in Cleanuparr**: In Cleanuparr, navigate to **Settings → Seeker** and either turn off **Proactive Search** or uncheck the specific Sonarr/Radarr instance. Cleanuparr will continue managing stalled downloads and strike replacements while SnatchArr handles library searches.

