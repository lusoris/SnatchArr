<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0020: Snatch policy defaults and presets

- Status: proposed
- Date: 2026-10-20

## Context

SnatchArr (pronounced "snatch-arr", from *to snatch* media combined with the *-arr* suite naming convention) coordinates missing and upgrade media searches across Sonarr, Radarr, Lidarr, Whisparr, and Readarr.

The operator reported that the out-of-the-box defaults (`missing_per_cycle = 1`, `upgrade_per_cycle = 0`, `cycle_interval_s = 900`, `hourly_cap = 20`) leave instances virtually idle. Because cutoff-unmet upgrades were disabled entirely by default, an instance only queried missing media, dispatching a single item every 15 minutes (at most 4 queries per hour). For medium or large libraries, progress was imperceptible.

Furthermore, operators found settings difficult to navigate without plain-language explanations. The core pacing terms—stamina, refractory period, afterglow, and foreplay—felt abstract, while numerical fields lacked units and explicit statements of operational effect.

An initial research sweep proposed throttling the hourly cap down to 1 query per hour on a 3600-second cycle to prevent exceeding free-tier limits. However, free usenet tiers largely offer no API access at all, rendering a 1-query/hour system default counter-productive because it locks instances in a permanent near-idle state. A gentle preset handles metered and low-limit users without crippling standard deployments.

## Decision

SnatchArr adopts balanced defaults for new instances, introduces four named presets for the web UI preset module, and standardizes field documentation on a plain label, unit, and single-sentence effect model.

### 1. New default policy values

The default policy updates three numeric fields. All other fields (selection order, search modes, queue limits, TTLs, and grace delays) keep their existing values because ecosystem lineage (Huntarr, Houndarr) confirms their efficacy and Lidarr artist searches count as a single query per issue #180.

| Field Name | Old Default | New Default | Rationale |
| :--- | :--- | :--- | :--- |
| `missing_per_cycle` | 1 | **2** | Increases per-cycle missing throughput to ensure perceptible library backlog clearance without burst-rate indexer penalties. |
| `upgrade_per_cycle` | 0 | **1** | Enables cutoff-unmet quality upgrades out of the box so upgrades are active rather than dormant. |
| `hourly_cap` | 20 | **15** | Calibrates stamina headroom to ensure multiple instances sharing a single mid-tier indexer ($D=1000$) remain safely below daily limits. |
| `cycle_interval_s` | 900 (15 min) | **900** (15 min) | Unchanged. Yields a nominal cadence of $(2 + 1) \times 4 = 12$ searches/hour, safely under the 15 searches/hour cap. |
| `selection` | `random` | `random` | Unchanged. Uniformly samples library wanted lists. |
| `monitored_only` | `true` | `true` | Unchanged. Universal convention; ignores unmonitored media. |
| `skip_future_releases` | `true` | `true` | Unchanged. Avoids futile indexer queries before official release. |
| `radarr_release_type` | `physical` | `physical` | Unchanged. Conservative availability baseline. |
| `sonarr_missing_mode` | `episodes` | `episodes` | Unchanged. Granular episode queries align with Huntarr lineage and Houndarr defaults. |
| `sonarr_upgrade_mode` | `episodes` | `episodes` | Unchanged. Targets specific cutoff-unmet episodes cleanly. |
| `lidarr_missing_mode` | `artist` | `artist` | Unchanged. An artist query dispatches a single search command per issue #180. |
| `processed_ttl_h` | 168 (7 d) | 168 (7 d) | Unchanged. Standard 7-day cooldown prevents duplicate searching. |
| `afterglow_max_h` | 720 (30 d) | 720 (30 d) | Unchanged. Caps exponential backoff at 30 days. |
| `max_queue_size` | -1 (disabled) | -1 (disabled) | Unchanged. Queue management handled via dedicated queue policies (ADR-0012). |
| `recent_grab_window_h` | 24 | 24 | Unchanged. Excludes items undergoing active import. |
| `release_delay_h` | 0 | 0 | Unchanged. Dispatches searches immediately upon availability. |
| `await_command` | `false` | `false` | Unchanged. Asynchronous non-blocking dispatch prevents worker lease timeouts. |
| `page_size` | 100 | 100 | Unchanged. Paging window balances memory and latency. |
| `skip_unavailable_movies` | `true` | `true` | Unchanged. Skips unavailable Radarr movies. |
| `dry_run` | `false` | `false` | Unchanged. Dispatches actual commands by default. |
| `include_tags` | `[]` | `[]` | Unchanged. Evaluates all items regardless of tags. |
| `exclude_tags` | `[]` | `[]` | Unchanged. No tag exclusions by default. |
| `include_quality_profiles` | `[]` | `[]` | Unchanged. Evaluates all configured quality profiles. |
| `exclude_quality_profiles` | `[]` | `[]` | Unchanged. No quality profile exclusions by default. |

### 2. Named policy presets

The UI settings preset module carries four standardized presets:

1. **`gentle` (Metered / Free Tier / ~100/day limits):** Paces searches for users on strict indexer quotas, capping daily queries at 96 searches/day.
2. **`balanced` (Standard Daily Driver — New Defaults):** Paces searches for users on 1–3 mid-tier indexers ($D \approx 1000$), averaging 12 searches/hour with a 15/hour cap (288 nominal, 360 max daily).
3. **`catch_up` (Backlog Ingestion / High Throughput):** Rapidly scans large unfulfilled backlogs for users on generous indexers ($D \ge 5000$), capping at 60 searches/hour (1,440 max daily).
4. **`upgrades` (Quality Hunter / Cutoff Unmet):** Prioritizes upgrading existing files to cutoff profiles and TRaSH custom format targets, nominal 36 searches/hour clamped to 25/hour (600 max daily).

#### Preset Configuration Matrix

| Field | `gentle` | `balanced` (Default) | `catch_up` | `upgrades` |
| :--- | :--- | :--- | :--- | :--- |
| `missing_per_cycle` | 1 | **2** | 10 | 1 |
| `upgrade_per_cycle` | 0 | **1** | 2 | 5 |
| `cycle_interval_s` | 900 (15 min) | **900** (15 min) | 300 (5 min) | 600 (10 min) |
| `hourly_cap` | 4 | **15** | 60 | 25 |
| `selection` | `random` | `random` | `sequential` | `random` |
| `monitored_only` | `true` | `true` | `true` | `true` |
| `skip_future_releases` | `true` | `true` | `true` | `true` |
| `radarr_release_type` | `physical` | `physical` | `physical` | `physical` |
| `sonarr_missing_mode` | `episodes` | `episodes` | `episodes` | `episodes` |
| `sonarr_upgrade_mode` | `episodes` | `episodes` | `episodes` | `episodes` |
| `lidarr_missing_mode` | `artist` | `artist` | `artist` | `artist` |
| `processed_ttl_h` | 168 | 168 | 168 | 168 |
| `afterglow_max_h` | 720 | 720 | 720 | 720 |
| `max_queue_size` | -1 | -1 | -1 | -1 |
| `recent_grab_window_h` | 24 | 24 | 24 | 24 |
| `release_delay_h` | 0 | 0 | 0 | 0 |
| `await_command` | `false` | `false` | `false` | `false` |
| `page_size` | 100 | 100 | 100 | 100 |
| `skip_unavailable_movies` | `true` | `true` | `true` | `true` |
| `dry_run` | `false` | `false` | `false` | `false` |
| `include_tags` | `[]` | `[]` | `[]` | `[]` |
| `exclude_tags` | `[]` | `[]` | `[]` | `[]` |
| `include_quality_profiles` | `[]` | `[]` | `[]` | `[]` |
| `exclude_quality_profiles` | `[]` | `[]` | `[]` | `[]` |
| **Max daily searches at cap** | **96** | **360** | **1,440** | **600** |

### 3. Plain-language labels, units, and help text

User-facing configuration forms adhere to the principle: **plain label + unit + effect sentence**. Domain jargon terms are retained only with immediate inline explanations:

- **Stamina:** The maximum number of search queries an instance may dispatch to indexers across a rolling 60-minute window (`hourly_cap`).
- **Refractory Period:** The mandatory cooling-off time between two consecutive search cycles of the same kind (`cycle_interval_s`).
- **Afterglow:** The temporary rest period during which an already queried item is excluded from repeat searches (`processed_ttl_h`), doubling on repeated fruitless queries up to `afterglow_max_h`.
- **Foreplay:** The automatic download-client backpressure mechanism (ADR-0006) that pauses or paces snatching when download clients are busy, paused, unreachable, or saturated.

#### Field Descriptions and Units

| Field Name | Plain-Language Label | Unit | Effect Sentence (en) |
| :--- | :--- | :--- | :--- |
| `missing_per_cycle` | Missing items per cycle | items | Maximum number of missing items queried during a single missing search run. |
| `upgrade_per_cycle` | Upgrades per cycle | items | Maximum number of cutoff-unmet items queried during a single upgrade search run. |
| `cycle_interval_s` | Cycle interval | seconds | Minimum waiting duration between two consecutive search runs of the same kind. |
| `hourly_cap` | Hourly search cap (stamina) | searches/hour | Hard limit on the number of searches this instance may dispatch to indexers within any one-hour rolling window. |
| `selection` | Candidate selection strategy | enum | Order in which candidates are picked from the wanted list (`random`, `sequential`, or `recent`). |
| `monitored_only` | Monitored items only | boolean | Restricts all searches to media marked as monitored in the \*arr application. |
| `skip_future_releases` | Skip future releases | boolean | Prevents searching for media whose announced release date is still in the future. |
| `radarr_release_type` | Radarr release date type | enum | Selects which Radarr release date determines availability (`digital`, `physical`, or `cinema`). |
| `sonarr_missing_mode` | Sonarr missing search mode | enum | Determines whether Sonarr searches for missing items as individual `episodes`, complete `season_packs`, or entire `shows`. |
| `sonarr_upgrade_mode` | Sonarr upgrade search mode | enum | Determines whether Sonarr searches for cutoff-unmet upgrades as individual `episodes` or complete `season_packs`. |
| `lidarr_missing_mode` | Lidarr missing search mode | enum | Determines whether Lidarr queries indexers for missing music by individual `album` or by whole `artist`. |
| `processed_ttl_h` | Search cooldown (afterglow) | hours | Number of hours a searched item rests before SnatchArr is allowed to search for it again. |
| `afterglow_max_h` | Maximum cooldown | hours | Upper ceiling for the exponential cooldown that doubles each time a search yields no results. |
| `max_queue_size` | Download queue ceiling | items | Pauses scheduled searches whenever the \*arr download queue contains this many or more items (-1 disables check). |
| `recent_grab_window_h` | Recent grab exclusion window | hours | Excludes items grabbed by the \*arr within this many hours from being searched again. |
| `release_delay_h` | Post-release grace delay | hours | Delays searching newly released media until this many hours have elapsed since official release. |
| `await_command` | Wait for search completion | boolean | Blocks the worker until the \*arr search command finishes executing before completing the lease. |
| `page_size` | Wanted list page size | records | Number of candidate records fetched from the \*arr API per page when scanning wanted media. |
| `skip_unavailable_movies` | Skip unavailable movies | boolean | Radarr only: skips movies that have not yet reached their minimum configured availability status. |
| `dry_run` | Preview mode (dry run) | boolean | Simulates candidate selection and logs candidate actions without dispatching search commands to the \*arr. |
| `include_tags` | Include tag filter | tag IDs | Restricts searches exclusively to media tagged with at least one of these \*arr tag IDs. |
| `exclude_tags` | Exclude tag filter | tag IDs | Skips media tagged with any of these \*arr tag IDs during search candidate selection. |
| `include_quality_profiles` | Include quality profiles | profile IDs | Restricts searches exclusively to media assigned to one of these \*arr quality profile IDs. |
| `exclude_quality_profiles` | Exclude quality profiles | profile IDs | Skips media assigned to any of these \*arr quality profile IDs during search runs. |

## Comparison of Comparable Tools

Defaults and limits from comparable tools in the ecosystem:

| Tool | Ref / Commit / Tag | Missing / Cycle | Upgrades / Cycle | Cycle Interval | Hourly Cap | Selection | Queue Stop | State Reset / TTL | Source Reference |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| [Huntarr](https://github.com/MGHazz/huntarr.io-archive) | `MGHazz/huntarr.io-archive`<br>commit `543223b` (v6.2.15+) | 1 | 0 | 900 s | 20 | `random` | -1 (disabled) | 72 h | [`default_settings.py`](https://github.com/MGHazz/huntarr.io-archive/blob/543223b030e4843af31c87e54643e52bd37ca712/src/primary/default_settings.py) |
| [newtarr](https://github.com/elfhosted/newtarr) | `elfhosted/newtarr`<br>tag `v1.0.0` (`1c65ed0`) | 1 | 0 | 900 s | 20 | `random` | -1 (disabled) | 168 h (7 d) | [`sonarr.json`](https://github.com/elfhosted/newtarr/blob/v1.0.0/src/primary/default_configs/sonarr.json) |
| [Houndarr](https://github.com/av1155/houndarr) | `av1155/houndarr`<br>tag `v1.13.3` (`9b39fdb`) | 2 (batch) | 0 (off by default) | 1800 s | 4 (missing; 1 upgrade) | `Random` | 0 (disabled) | 14 d (missing), 21 d (cutoff) | [`instance-settings.md`](https://github.com/av1155/houndarr/blob/v1.13.3/website/docs/reference/instance-settings.md) |
| [scoutarr](https://github.com/SuFxGIT/scoutarr) | `SuFxGIT/scoutarr`<br>tag `v1.1.0` (`bb3d754`) | 5 | 5 | 21600 s (`0 */6 * * *`) | None | `random` | Via status | Tag-based | [`search.ts`](https://github.com/SuFxGIT/scoutarr/blob/v1.1.0/backend/src/routes/search.ts) |
| [Cleanuparr](https://github.com/Cleanuparr/Cleanuparr) | `Cleanuparr/Cleanuparr`<br>tag `v2.10.12` (`51b8776`) | 1 item/run | 0 (off by default) | 600 s | None | `BalancedWeighted` | 3 downloads | Min 7 d | [`SeekerConfig.cs`](https://github.com/Cleanuparr/Cleanuparr/blob/v2.10.12/code/backend/Cleanuparr.Persistence/Models/Configuration/Seeker/SeekerConfig.cs) |
| [Upgradinatorr](https://github.com/angrycuban13/Just-A-Bunch-Of-Starr-Scripts) | `angrycuban13/Just-A-Bunch-Of-Starr-Scripts`<br>commit `9a2d48c` | 0 | 10 (Radarr), 5 (Sonarr/Lidarr) | External cron | None | `random` | Movie released | Tag-based | [`upgradinatorr.ps1`](https://github.com/angrycuban13/Just-A-Bunch-Of-Starr-Scripts/blob/main/Upgradinatorr/upgradinatorr.ps1) |
| [missarr](https://github.com/l3uddz/missarr) | `l3uddz/missarr`<br>commit `299128d` | 10 | 0 (10 with `--cutoff`) | External cron | None | Sequential | Release date | 28 d cooldown | [`radarr.go`](https://github.com/l3uddz/missarr/blob/master/cmd/missarr/radarr.go) |
| [Sonarr Built-in](https://github.com/Sonarr/Sonarr) | `Sonarr/Sonarr`<br>develop (`290d235`) | All missing | All cutoff unmet | Manual trigger | None | Sequential | Skips queued | Immediate bulk | [`EpisodeSearchService.cs`](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/IndexerSearch/EpisodeSearchService.cs) |
| [Radarr Built-in](https://github.com/Radarr/Radarr) | `Radarr/Radarr`<br>develop (`3473ee2`) | All missing | All cutoff unmet | Manual trigger | None | Sequential | Skips queued | Immediate bulk | [`MoviesSearchService.cs`](https://github.com/Radarr/Radarr/blob/develop/src/NzbDrone.Core/IndexerSearch/MoviesSearchService.cs) |
| **Sonarr / Radarr RSS** | Upstream default | N/A | N/A | Sonarr: 15 min<br>Radarr: 30 min | None | Chronological | N/A | N/A | [`ConfigService.cs`](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Configuration/ConfigService.cs) |

> [!NOTE]
> Checked 2026-10-20 against the pinned sources linked above: newtarr v1.0.0 `sonarr.json` = missing 1, upgrade 0, 900 s, cap 20, modes episodes; Houndarr v1.13.3 = batch 2, interval 30 min, cap 4, cutoff batch 1 / cap 1, Sonarr episode mode, Lidarr album mode; Prowlarr `IndexerBaseSettings` `QueryLimit`/`GrabLimit` nullable (no limit by default).

## Budget Math and Mathematical Model

In standard \*arr deployments, automated media searches broadcast queries to **all enabled indexers in parallel**. Consequently, a multi-indexer setup is bounded by its **most restrictive indexer** (bottleneck principle). Let $D$ denote the daily API hit limit of the bottleneck indexer.

### 1. Baseline Load and Headroom

Background activity consumes indexer capacity regardless of backlog snatching:

- **Sonarr RSS Sync:** Every 15 minutes = 4 syncs/hour = 96 hits/day per indexer.
- **Radarr RSS Sync:** Every 30 minutes = 2 syncs/hour = 48 hits/day per indexer.
- **Total Combined RSS Syncs:** $96 + 48 = 144$ API hits/day per indexer.
- **Interactive / Manual Search Buffer:** $\approx 56$ hits/day.
- **20% Headroom Reserve:** For a mid-tier indexer with $D = 1000$, a $20\%$ reserve ($0.20 \times D = 200$ hits/day) covers baseline RSS ($144$) and manual use ($56$).

### 2. General Capacity Equation

The maximum allowable automated search rate across all combined \*arr instances sharing indexers is:

$$\text{Available Searches per Hour} = \frac{0.80 \times D}{24}$$

### 3. Dual-Instance Sizing Under Mid-Tier Bottleneck ($D = 1000$)

Consider a standard dual-instance setup (Sonarr + Radarr) sharing a common mid-tier indexer with daily limit $D = 1000$ (e.g. DrunkenSlug Ale, unverified, from `https://drunkenslug.com`, checked 2026-10-20; or NinjaCentral Ninja Dan, unverified, from `https://ninjacentral.co.za`, checked 2026-10-20):

- **Per-instance hourly cap:** `hourly_cap = 15`.
- **Worst-case daily searches dispatched at cap:**
  $$2 \text{ instances} \times 15 \text{ searches/hour} \times 24 \text{ hours} = 720 \text{ searches/day}$$
- **Total daily indexer hits (searches + RSS):**
  $$720 \text{ searches} + 144 \text{ RSS hits} = 864 \text{ hits/day}$$
  $$864 < 1000 \quad (136 \text{ hits remaining for manual buffer})$$
- **Nominal daily load under steady-state cadence:**
  With `missing_per_cycle = 2`, `upgrade_per_cycle = 1`, and `cycle_interval_s = 900` (4 cycles/hour):
  $$\text{Nominal rate} = (2 + 1) \times 4 = 12 \text{ searches/hour per instance}$$
  $$2 \text{ instances} \times 12 \text{ searches/hour} \times 24 \text{ hours} = 576 \text{ searches/day}$$
  $$576 \text{ searches} + 144 \text{ RSS hits} = 720 \text{ hits/day} < 1000 \quad (28\% \text{ headroom reserve})$$

### 4. Indexer Tier References

Published daily API limits $D$ from indexer landing pages and community reviews:

| Indexer | Tier | Daily Limit ($D$) | Status & Citation |
| :--- | :--- | :--- | :--- |
| **NZBgeek** | Free / Registered | 0 | Unverified, from `https://nzbgeek.info`, checked 2026-10-20 (no API without paid VIG) |
| **NZBgeek** | VIG (Paid) | Unlimited | Unverified, from `https://nzbgeek.info`, checked 2026-10-20 ("fair use" anti-scraping) |
| **DrunkenSlug** | Water (Free) | 25 | Unverified, from `https://drunkenslug.com`, checked 2026-10-20 (manual/trial) |
| **DrunkenSlug** | Ale (Mid tier) | 1,000 | Unverified, from `https://drunkenslug.com`, checked 2026-10-20 |
| **DrunkenSlug** | Wine (High tier) | 2,500 | Unverified, from `https://drunkenslug.com`, checked 2026-10-20 |
| **NZBFinder** | Free | 10 | Unverified, from `https://nzbfinder.ws`, checked 2026-10-20 (no API integration) |
| **NZBFinder** | Basic / VIP | 5,000 | Unverified, from `https://nzbfinder.ws`, checked 2026-10-20 |
| **NinjaCentral** | Ninja Dan | 1,000–2,000 | Unverified, from `https://ninjacentral.co.za`, checked 2026-10-20 |
| **Tabula Rasa** | Free | 0 | Unverified, from `https://www.tabularasa.info`, checked 2026-10-20 (API disabled on free) |
| **Tabula Rasa** | Supporter | 2,000 | Unverified, from `https://www.tabularasa.info`, checked 2026-10-20 |
| **althub** | Free | 100 | Unverified, from `https://althub.co.za`, checked 2026-10-20 (metered) |
| **nzb.su / nzb.life** | VIP | 5,000 | Unverified, from `https://api.nzb.life`, checked 2026-10-20 |
| **NZBPlanet** | VIP | 5,000 | Unverified, from `https://nzbplanet.net`, checked 2026-10-20 |

Because free tiers either lack API access entirely or offer only token trial hits (10–25/day), configuring system-wide defaults for free tiers paralyzes the application for the vast majority of adopters. Users on metered tiers select the explicit `gentle` preset.

## Consequences

- **Positive:**
  - Newly added instances start processing missing and cutoff-unmet items immediately at a steady, sustainable cadence.
  - Multi-instance deployments sharing mid-tier indexers ($D=1000$) do not exceed rate limits, even if both instances run at full stamina capacity all day.
  - User interface presents clear, understandable labels, explicit units, and effect descriptions.
- **Neutral:**
  - Existing instances retain their stored policy values in PostgreSQL. The database migration updates column `DEFAULT` definitions only; it does not execute `UPDATE` statements on existing rows.
  - Running instances can be moved to the new defaults or to any preset through the settings API (`PUT /api/v1/instances/{id}/policy`) or the UI preset picker.
- **Negative:**
  - Adopters on strictly metered or free-tier indexers ($D < 300$) who leave the balanced defaults active could encounter indexer rate limits unless they switch to the `gentle` preset.

## Alternatives Rejected

1. **Hourly cap of 1 search/h with 3600 s cycle interval:** Rejected. Recreates the exact reported problem of SnatchArr appearing idle. Free tiers largely offer no API access, making them an inappropriate baseline for product defaults. The `gentle` preset handles low-quota environments.
2. **Defaulting Sonarr to `season_packs`:** Rejected. Full season pack queries pull large release sets and reduce granular file-level discretion. Keeping `episodes` preserves fine-grained control and aligns with Huntarr lineage and Houndarr defaults.
3. **Defaulting Lidarr to `album`:** Rejected. An artist search in Lidarr dispatches a single command across monitored releases (1 query per issue #180), making whole-artist searches stamina-efficient.
4. **Migrating existing database rows to new defaults:** Rejected. Mutating user-configured settings during database upgrades destroys intentional tuning. New defaults apply only to newly created instances.

## References

- `snatcharr-settings-redo-20261020` (2026-10-20) (paraphrased): the settings surface needs plain labels with units and effect text, defaults with stated reasons compared against Huntarr-style tools, indexer limits and the hourly cap, and named presets that fill the form.
- `snatcharr-settings-live-write-20261020` (2026-10-20) (paraphrased): live instances move to the new values only through the policy API, with before values stored for rollback.
- Issue #180: the hourly cap counts searches per indexer, not HTTP requests.
- [ADR-0006](0006-download-client-backpressure.md), [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md).
