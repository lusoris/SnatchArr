<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0009: Detect Cleanuparr Seeker and warn about double searching

- Status: accepted
- Date: 2026-10-16
- Supersedes: [ADR-0007](0007-read-only-cleanuparr-link.md), its "There is no Seeker detection" bullet only. The read-only link and strike handling stand.

## Context

Cleanuparr ships Seeker, a background job that searches *arr instances for missing and cutoff-unmet items. Upstream Cleanuparr (`v2.10.11`, commit `9f324ead`) implements it for Sonarr and Radarr only:

- `code/backend/Cleanuparr.Infrastructure/Features/Jobs/Seeker.cs` (the job; skips search commands under global dry run)
- `code/backend/Cleanuparr.Api/Features/Seeker/Controllers/SeekerConfigController.cs` (`GET /api/configuration/seeker`)
- `code/backend/Cleanuparr.Api/Features/Arr/Controllers/ArrConfigController.cs` (`GET /api/configuration/{sonarr,radarr}`)
- `code/backend/Cleanuparr.Persistence/Models/Configuration/General/GeneralConfig.cs` (`DryRun`)
- `code/backend/Cleanuparr.Api/DependencyInjection/AuthDI.cs` (requests with `X-Api-Key` use the API key scheme)

When Seeker's proactive search is active on the same Sonarr or Radarr library that SnatchArr is searching, both tools issue search commands for missing and upgrade items. This duplicates queries to indexers, consumes download client capacity, and risks rate-limit strikes or bans from indexers.

ADR-0007 originally noted that there was no Seeker detection, relying on afterglow to absorb searches following item removal. While afterglow handles replacement searches when Cleanuparr strikes and re-searches a single removed item, it does not prevent continuous duplicate searches when Seeker's proactive routine runs across the whole library.

## Decision

- **Read-only inspection**: SnatchArr checks Seeker configuration using the existing credentials of each linked Cleanuparr instance (`GET /api/configuration/seeker`, `GET /api/configuration/general`, `GET /api/configuration/sonarr`, `GET /api/configuration/radarr`). SnatchArr never writes to Cleanuparr or changes any setting there.
- **Strict allow-list for sensitive fields**: Cleanuparr's `/api/configuration/{type}` response records carry an `apiKey` property (masked by `SensitiveDataResolver`). SnatchArr uses a strict allow-list parser that decodes only `id`, `name`, `url`, and `enabled`. Cleanuparr's instance `apiKey` is never deserialized, retained in memory, logged, or returned by the API.
- **Activity criteria**: Seeker is considered active for a library only when all five conditions hold:
  1. `searchEnabled` is true in Seeker configuration,
  2. `proactiveSearchEnabled` is true in Seeker configuration,
  3. `instance.enabled` is true for that Seeker instance,
  4. `instance.arrInstanceEnabled` is true, and
  5. `general.dryRun` is false (global dry run prevents Seeker from dispatching searches).
  Replacement-only search (`proactiveSearchEnabled == false`) does not warn, as afterglow already absorbs strike replacements.
- **Normalised URL matching**: Seeker instances are mapped to base URLs via `arrInstanceId` lookups against the Sonarr/Radarr configuration endpoints. Matching between a Cleanuparr Seeker instance and a SnatchArr instance is performed strictly by normalised base URL (scheme, lowercase host, default ports `:80`/`:443` stripped, trailing slashes removed). There is no fallback to name-based matching.
- **Supported kinds**: Cleanuparr Seeker only manages Sonarr and Radarr. Lidarr and Readarr instances in SnatchArr are never flagged as overlapping.
- **Fail-open behaviour**: If Cleanuparr is unreachable or returns HTTP 404 on `/api/configuration/seeker` (such as on older Cleanuparr releases before Seeker existed), Seeker state is reported as `unknown` with the reason, and no warnings are generated. When the Sonarr or Radarr configuration cannot be read, the state stays as computed, `reason` names the failed lookup, and the affected instances are reported unmatched.
- **Additive API schema**:
  - `GET /api/v1/cleanuparr/status` includes a `seeker` object with overall state (`active`, `inactive`, `dry_run`, `unknown`), optional `reason`, and the list of observed Seeker instances with their match status.
  - The `Instance` schema gains an optional `seeker_overlap: bool` property indicating whether an active Seeker instance targets the same library URL.
- **Web UI notifications**:
  - The instances table displays a badge on overlapping instances.
  - The instance detail view displays a warning banner explaining that both tools search the same library and that running both risks rate limits.
  - When overlapping and enabled, the banner includes a "Pause SnatchArr for this instance" button using the existing `PUT /instances/{id}` endpoint (`enabled: false`) with an in-page confirmation step.
  - The Cleanuparr status page lists the observed Seeker configuration and instance matches per link.

## Consequences

- Operators are warned before indexer limits or stamina budgets are doubled by overlapping proactive searches.
- The user can pause SnatchArr for the overlapping library with a single click, or disable Seeker in Cleanuparr.
- Older Cleanuparr versions without Seeker continue working without spurious errors or warnings.
- The separation of responsibilities remains intact: Cleanuparr manages download health, SnatchArr manages snatches, and SnatchArr remains entirely read-only toward Cleanuparr.
