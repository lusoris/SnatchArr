<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0019: Foreign-title search and release push without proxy indexers

- Status: proposed
- Date: 2026-10-20

## Context

When \*arr applications (Sonarr, Radarr, Lidarr, Readarr, Whisparr) perform automated searches for missing or cutoff-unmet items, they query indexers using canonical titles or title aliases stored in their database. Releases posted with localized titles—such as German umlaut transliterations (`ä` to `ae`, `ö` to `oe`, `ü` to `ue`, `ß` to `ss`), localized series names, accented Romance titles, Slavic extended Latin names, and romanized non-Latin scripts—frequently fail to be acquired or fail to import automatically:

1. **Indexer query misses**: The \*arr query builder either queries only the canonical English title or strips diacritics without expanding phonetic or scene transliterations. For example, Sonarr's title cleaning in `Parser.cs` ([src/NzbDrone.Core/Parser/Parser.cs](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Parser/Parser.cs)) calls `RemoveDiacritics()` from `StringExtensions.cs` ([src/NzbDrone.Common/Extensions/StringExtensions.cs](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Common/Extensions/StringExtensions.cs)). This decomposes Unicode characters and strips non-spacing marks (`ä` to `a`, `ö` to `o`, `ü` to `u`), missing indexer releases that follow the German scene convention of using `ae`, `oe`, and `ue`.
2. **Automatic import blocks ("matched by ID")**: Even when a localized release is grabbed (for instance via an ID search or interactive search), Sonarr's completed download handling blocks automatic import. In `CompletedDownloadService.cs` ([src/NzbDrone.Core/Download/CompletedDownloadService.cs](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Download/CompletedDownloadService.cs)), Sonarr parses the downloaded folder or release name with `_parsingService.GetSeries()` ([src/NzbDrone.Core/Parser/ParsingService.cs](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Parser/ParsingService.cs)). If the downloaded title does not match the series title or an established alias, Sonarr finds the grab in history but detects `seriesMatchType == SeriesMatchType.Id`. Sonarr marks the item as import-blocked with the error: `"Found matching series via grab history, but release was matched to series by ID. Automatic import is not possible."` This safeguard prevents misidentifying unrelated files, but permanently halts automated pipelines for foreign releases.
3. **Asymmetric application capability**: Radarr possesses native localized title capability that Sonarr lacks. In Radarr's `ReleaseSearchService.cs` ([src/NzbDrone.Core/IndexerSearch/ReleaseSearchService.cs](https://github.com/Radarr/Radarr/blob/develop/src/NzbDrone.Core/IndexerSearch/ReleaseSearchService.cs)), when a quality profile includes languages in `wantedLanguages`, Radarr populates `spec.SceneTitles` with localized translations from TheMovieDB (TMDB) and queries indexers with both English and translated titles. Furthermore, Radarr's `ParsingService.cs` ([src/NzbDrone.Core/Parser/ParsingService.cs](https://github.com/Radarr/Radarr/blob/develop/src/NzbDrone.Core/Parser/ParsingService.cs)) matches against `AlternativeTitles` and `Translations`. However, Sonarr, Lidarr, and Readarr do not provide equivalent translated scene searches, and even Radarr misses diacritic scene expansions (`ae`/`oe`/`ue`) when TMDB lists only accented forms.

### Prior art and idea credit

The behaviour of searching title variants and resolving import matching was studied from [PCJones/UmlautAdaptarr](https://github.com/PCJones/UmlautAdaptarr) v0.7.6. The upstream repository contains no licence file (all rights reserved): its documented behaviour and its issue tracker were studied, no code was copied into SnatchArr, and it is credited solely as the idea.

UmlautAdaptarr deploys as a proxy and fake indexer in Prowlarr, intercepting Torznab and Newznab XML feeds over an HTTP man-in-the-middle (MITM) proxy to rewrite release titles on the fly. SnatchArr rejects this proxy topology per operator direction: SnatchArr uses no fake indexers and does not touch or modify indexers. Analysis of UmlautAdaptarr's upstream issue tracker demonstrates that the proxy indexer architecture suffers from critical operational failure modes:

- **Security and TLS degradation**: Forcing indexers in Prowlarr to downgrade from HTTPS to plaintext HTTP to permit proxy inspection breaks when trackers enforce TLS, fails in Prowlarr for torrent indexers that disallow changing URLs, breaks FlareSolverr Cloudflare bypasses, and introduces IP leak hazards.
- **Uncontrolled query fan-out**: Multiplied alias queries per item without centralized hourly pacing rapidly exhaust daily and hourly API limits on private trackers and Usenet indexers.
- **Centralized failure points**: Relying on a third-party hosted metadata endpoint creates a single point of failure subject to rate limits (HTTP 429).
- **XML feed corruption**: In-flight modification of Torznab XML payloads corrupts feeds and triggers parser crashes on indexers such as DogNZB and NZBGeek.

SnatchArr requires a design that finds foreign releases and guarantees automated import across \*arr applications without modifying indexer configurations or injecting proxy layers.

## Decision

SnatchArr implements a language-generic search and release push architecture that operates alongside the \*arr applications and Prowlarr without modifying indexers or running proxy servers (`snatcharr-foreign-title-search-20261020`).

### 1. Title variant harvesting and declarative language rules

SnatchArr derives title variants from local cached metadata and declarative language definition files embedded in the binary:

- **Local \*arr cache first**: SnatchArr harvests title aliases directly from the \*arr instance: series `alternateTitles` (Sonarr `GET /api/v3/series/{id}`) and movie `alternativeTitles` and `translations` (Radarr `GET /api/v3/movie/{id}`). No new third-party credentials (TheTVDB, TheMovieDB, MusicBrainz) are required; SnatchArr queries what the \*arr already caches locally.
- **Declarative language rules**: Language expansion rules are specified declaratively in embedded configuration files (`rules/languages/<lang>.yaml`). Each language definition provides:
  - Diacritic expansion variants for query generation (e.g. `ä` expanding to `ae` and `a`; `ö` expanding to `oe` and `o`; `ü` expanding to `ue` and `u`; `ß` expanding to `ss` and `s`) and reverse contractions for title parsing.
  - Positive audio tags (`GERMAN`, `DEUTSCH`, `DUBBED`, `\bDL\b`, `\bML\b`, `\bGL\b`).
  - Subtitle-only tags (`\bOmU\b`, `\bOmeU\b`) to prevent importing subtitled releases when dubbed audio is required.
  - Bad-naming scene group exceptions (e.g. group `TvR` which omits language tags).
- **Phased language rollout**: German (`de`) is supported first, as it encompasses diacritic transliteration, TVDB English vs German title differences, scene group tag omissions, and dual-language (`DL`) requirements. Romance languages (`fr`, `es`, `it`, `pt`), Slavic and extended Latin languages (`pl`, `cs`, `sk`), and non-Latin romanization classes (`ja`, `ko`, `zh`, `ru`) follow the same declarative rule schema.

### 2. Search path: Prowlarr direct search

SnatchArr queries Prowlarr directly via its public REST API without modifying indexer settings or URLs:

- **Radarr native search first**: Because Radarr's `ReleaseSearchService.cs` already searches translated titles when configured in the movie's quality profile, SnatchArr relies on Radarr's native search for standard translations and applies SnatchArr language expansion rules only where native searches miss (such as `ae`/`oe`/`ue` diacritic scene transliterations).
- **Prowlarr search queries**: For Sonarr, Lidarr, Readarr, Whisparr, and Radarr edge cases, SnatchArr queries Prowlarr's search endpoint (`GET /api/v1/search` with `query={variant}&indexerIds={ids}&categories={cats}`) over HTTPS using SnatchArr's read-only Prowlarr client (`prowlarrclient`).
- **Target indexer language filtering**: Foreign-title queries are sent exclusively to indexers whose Prowlarr tags or language fields match the target language (for example, querying German title variants only against indexers tagged for German content). SnatchArr never sends localized query variants to generic English-only indexers.
- **Sequential waterfall**: The canonical title search runs first. Variant title searches are dispatched only when the canonical search yields zero matching candidates.

### 3. Grab path: release push with title rewriting

SnatchArr never dispatches releases directly to a download client ([ADR-0006](0006-download-client-backpressure.md)). Instead, SnatchArr pushes the selected release candidate to the \*arr instance:

- **Push endpoint**: SnatchArr calls `POST /api/v3/release/push` on Sonarr and Radarr, or `POST /api/v1/release/push` on Lidarr and Readarr (Sonarr `ReleasePushController.cs` in [src/Sonarr.Api.V3/Indexers/ReleasePushController.cs](https://github.com/Sonarr/Sonarr/blob/develop/src/Sonarr.Api.V3/Indexers/ReleasePushController.cs)).
- **Title rewriting**: When pushing a release acquired under a localized title, SnatchArr rewrites the release `<title>` attribute to use the series or movie canonical title while strictly preserving all release tokens:
  - Season and episode identifiers (e.g. `S01E01`, `e01`).
  - Resolution and source tokens (e.g. `1080p`, `720p`, `WEB-DL`, `BluRay`).
  - Codec, audio, and language tokens (e.g. `x264`, `GERMAN`, `DL`).
  - Release group suffix (e.g. `-GRP`, `-TvR`).
- **Clean automatic import**: Because `ReleasePushController` invokes `_downloadDecisionMaker.GetRssDecision()`, the \*arr evaluates the release against its quality profile, custom formats, and release restrictions. When approved and grabbed, Sonarr parses the canonical title with `_parsingService.GetSeries(title)` and records `SeriesMatchType.Title` in grab history. When download finishes, `CompletedDownloadService.cs` checks the grab history record ([src/NzbDrone.Core/Download/TrackedDownloads/TrackedDownloadService.cs](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Download/TrackedDownloads/TrackedDownloadService.cs)) and executes automated import without triggering the `SeriesMatchType.Id` block.

### 4. Fallback for existing blocked imports

For downloads grabbed prior to SnatchArr's intervention that remain in the \*arr queue in an `ImportBlocked` state, SnatchArr's queue management module ([ADR-0012](0012-queue-management-replaces-cleanuparr-link.md)) operates in observe-first mode. When it detects an error message matching `"Found matching series via grab history..."`, it triggers a manual import via the \*arr REST API (`POST /api/v3/manualimport` or `POST /api/v1/manualimport`) specifying the completed folder, series ID, and episode IDs.

### 5. Hourly search budget and stamina rules

In accordance with SnatchArr's core stamina invariant (issue #180, [AGENTS.md](https://github.com/lusoris/SnatchArr/blob/main/AGENTS.md)), search limits count queries sent per indexer, not raw HTTP requests:

- Every query sent through Prowlarr's search API debits the hourly search stamina bucket of each targeted indexer (1 search per query per indexer), with a warning emitted at 80% utilization.
- Search queries are limited to at most 2 title variants per missing item per cycle.
- Prowlarr search results are cached in SnatchArr for 1 hour to prevent redundant variant queries across subsequent search cycles.

### 6. Module configuration and defaults

Foreign-title search is part of the search and queue core module ([ADR-0018](0018-product-front-door.md)). Following the ADR-0018 modularity rules, foreign-title search is disabled by default per instance. Administrators explicitly enable the capability and select the active language list for each configured instance.

## Consequences

### Positive

- **Transparent localized snatching**: Missing and cutoff-unmet items with localized release names are located and snatched automatically across connected \*arr applications.
- **Bypasses import blockages**: Rewriting release titles on push ensures grab history stores `SeriesMatchType.Title`, allowing Sonarr's `CompletedDownloadService` to import completed downloads automatically without human intervention.
- **Zero proxy overhead or TLS compromise**: Indexer communication remains direct and encrypted; Prowlarr configuration is untouched; zero fake indexers are registered.
- **Zero external API credentials**: Eliminates dependencies on third-party metadata API keys by leveraging the \*arr's local database cache and embedded language rules.
- **Stamina-safe**: Adheres strictly to indexer hourly search limits (#180) through targeted indexer filtering, variant query caps, and response caching.

### Negative

- **API client expansion**: Requires adding release push endpoints (`POST /api/v3/release/push`, `POST /api/v1/release/push`) to `arrclient` and query search endpoints (`GET /api/v1/search`) to `prowlarrclient`.
- **Title rewriting correctness risk**: Incorrectly parsing and reconstructing release titles could drop or corrupt season, episode, quality, or release group tokens. This risk is guarded by rigorous test fixtures.
- **Prowlarr dependency**: Executing foreign-title search requires a linked Prowlarr instance; when Prowlarr is not configured, foreign-title search remains disabled.

### Open points recorded as unverified

The following operational behaviors from local research evidence are recorded as unverified and must be validated through the live integration check before the feature is enabled in production:

1. **Push-title import guarantee across Sonarr releases**: Whether pushing a synthesized canonical English title via `POST /api/v3/release/push` guarantees 100% bypass of Sonarr's `SeriesMatchType.Id` block across both Sonarr v3 and Sonarr v4 in scenarios where the download client creates a root folder using the raw indexer foreign title.
2. **Downloader folder parsing fallback**: Whether `TrackedDownloadService.cs` reliably falls back to `firstHistoryItem.SourceTitle` across all download clients and nested directory structures when `Parser.ParseTitle` fails on the localized directory path.

## Alternatives considered

- **Proxy or fake indexer (UmlautAdaptarr architecture)**: Rejected. Running a local HTTP proxy to intercept and mutate Torznab XML payloads requires downgrading Prowlarr indexer configurations to plaintext HTTP, breaks TLS integrity, breaks Cloudflare/FlareSolverr bypasses, causes XML schema corruption on DogNZB and NZBGeek, and risks container boot loops. Per operator direction, SnatchArr will not deploy fake indexers or modify indexer settings.
- **Direct Torznab searches to indexers**: Rejected. Querying indexers directly from SnatchArr bypasses Prowlarr's centralized rate limits and proxy settings, duplicates indexer credential management, and increases connection overhead.
- **Injecting alternative titles into Sonarr**: Rejected. Sonarr provides no API to register custom alternative titles or aliases; series aliases are populated exclusively via SkyHook and TheXEM through `SceneMappingService.cs` ([src/NzbDrone.Core/DataAugmentation/Scene/SceneMappingService.cs](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/DataAugmentation/Scene/SceneMappingService.cs)).
- **Direct TVDB and TMDB API integration**: Rejected. Querying upstream metadata providers directly requires user-supplied API keys, introduces network failure modes and third-party rate limits, and duplicates metadata that \*arr instances already cache locally.

## Verification

Implementation verification must satisfy three validation layers:

### 1. Per-language unit and boundary test suite

A comprehensive table-driven test suite exercises the four language classes:

| Language Class | Test Category | Fixture Input | Expected Behavior | Boundary / Guard |
| :--- | :--- | :--- | :--- | :--- |
| **Germanic / Umlauts** (`de`, `sv`, `da`, `no`) | Positive (Search) | Series `Käfer`, missing S01E01 | Generates queries `Käfer`, `Kaefer`; finds `Kaefer.S01E01.GERMAN.720p-GRP` | Capped by hourly indexer stamina |
| | Positive (Push) | Release `Alone.Ueberlebe.die.Wildnis.S01E01.720p-TvR` | Rewrites title to `Alone.Germany.S01E01.GERMAN.720p-TvR` with the `GERMAN` tag added because the language rules list group `TvR` as omitting it | Preserves season and episode tokens |
| | Negative | Release `Schon.S01E01.720p-GRP` | Does not match series `Schön` (distinct semantic word) | Strict word boundary matching |
| | Boundary | Uppercase umlaut `Die Ärzte` | Expands to `Die Aerzte` and `Die Arzte`; handles initial capital correctly | Case-insensitive invariant matching |
| **Romance / Accents** (`fr`, `es`, `pt`, `it`) | Positive (Search) | Movie `Le Fabuleux Destin d'Amélie Poulain` | Queries `Amelie Poulain` and original title on French-tagged indexers | Filters indexers by language tag |
| | Positive (Push) | Release `Le.Bureau.des.Legendes.S01E01.FRENCH.1080p-GRP` | Pushes to Sonarr with canonical title `The Bureau` and `FRENCH` tag | Auto-import succeeds without ID block |
| | Negative | Release `Movie.Title.VOSTFR.1080p-GRP` | Identified as subtitle-only; rejected if profile requires French audio dub | Subtitle vs dub distinction |
| | Boundary | French elision `L'Amour Ouf` | Normalizes to both `L'Amour` and `L Amour` without stripping leading letter `L` | Stop-word regex safety |
| **Slavic / Extended Latin** (`pl`, `cs`, `sk`) | Positive (Search) | Series `Wiedźmin`, missing S01E01 | Queries `Wiedzmin` and `The Witcher` on Polish-tagged indexers | Matches `LEKTOR` audio profile |
| | Positive (Push) | Release `Wiedzmin.S01E01.PL.LEKTOR.720p-GRP` | Rewrites to `The.Witcher.S01E01.POLISH.LEKTOR.720p-GRP` | Sonarr parser maps episode cleanly |
| | Negative | Release marked only `NAPISY` | Rejected if profile demands audio voiceover/dub | Avoids importing subbed when dubbed wanted |
| | Boundary | Cyrillic and Polish homoglyph characters | Normalizes Unicode confusables before regex evaluation | Prevents regex mismatch and spoofing |
| **Non-Latin Scripts** (`ja`, `ko`, `zh`, `ru`) | Positive (Search) | Series `進撃の巨人` (TVDB ID 267440) | Queries Romaji `Shingeki no Kyojin` on anime indexers | Pushes with canonical title `Attack on Titan` |
| | Positive (Push) | Release `[Group] Shingeki no Kyojin - 01 [1080p].mkv` | Rewrites to `Attack.on.Titan.S01E01.1080p-Group` | Absolute episode to standard conversion |
| | Negative | Ambiguous short Romaji title (e.g. `K`) | Rejects candidate if release year or TVDB ID does not match | Strict duration and year cross-check |
| | Boundary | Macron vowels `ō` / `ū` (`Jōjo` to `Joujou` vs `Jojo`) | Evaluates digraph (`ou`) and single letter (`o`) variants | Permutation limit capped at 2 variants |

### 2. Planted defect test

A negative verification test verifies that title rewriting failure is detected: a planted defect in which the title rewriting logic drops or corrupts the episode/season token (e.g. producing `Alone.Germany.GERMAN.720p-TvR` without `S01E01`) fails the test suite immediately (test red), proving that token preservation is strictly enforced.

### 3. Live verification check

The implementation must pass an end-to-end live verification check:

- One German umlaut title (e.g. `Käfer` or `Alone Germany`) and one non-German localized case are discovered via Prowlarr search, pushed to Sonarr, and automatically imported upon download completion.
- Prowlarr indexer count and configurations are verified before and after the run: the count must remain identical, confirming zero proxy or fake indexers were created or modified.
- The unverified open points (Sonarr v3/v4 root folder handling and downloader folder fallback) must pass this live check before foreign-title search is made generally available.

## References

- `snatcharr-foreign-title-search-20261020` (2026-10-20T15:58:11) (paraphrased): Direction to implement foreign-title search using local \*arr metadata, declarative language rules, Prowlarr search, and release push with title rewriting, rejecting proxy indexers and respecting indexer hourly stamina.
- Local research evidence, 2026-10-20 (not published): Technical analysis of UmlautAdaptarr architecture, \*arr parsing services, and test fixture definitions.
- [PCJones/UmlautAdaptarr](https://github.com/PCJones/UmlautAdaptarr) v0.7.6: Behavioural reference for localized title search and release title rewriting; all rights reserved (no licence file); credited as idea only.
- Upstream Sonarr source audit:
  - `src/NzbDrone.Core/Parser/Parser.cs`: [Sonarr parser](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Parser/Parser.cs)
  - `src/NzbDrone.Core/Parser/ParsingService.cs`: [Sonarr parsing service](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Parser/ParsingService.cs)
  - `src/NzbDrone.Core/Download/CompletedDownloadService.cs`: [Sonarr completed download service](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Download/CompletedDownloadService.cs)
  - `src/Sonarr.Api.V3/Indexers/ReleasePushController.cs`: [Sonarr release push controller](https://github.com/Sonarr/Sonarr/blob/develop/src/Sonarr.Api.V3/Indexers/ReleasePushController.cs)
  - `src/NzbDrone.Core/DataAugmentation/Scene/SceneMappingService.cs`: [Sonarr scene mapping service](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/DataAugmentation/Scene/SceneMappingService.cs)
- Upstream Radarr source audit:
  - `src/NzbDrone.Core/Parser/ParsingService.cs`: [Radarr parsing service](https://github.com/Radarr/Radarr/blob/develop/src/NzbDrone.Core/Parser/ParsingService.cs)
  - `src/NzbDrone.Core/IndexerSearch/ReleaseSearchService.cs`: [Radarr release search service](https://github.com/Radarr/Radarr/blob/develop/src/NzbDrone.Core/IndexerSearch/ReleaseSearchService.cs)
- [ADR-0006](0006-download-client-backpressure.md): Download clients observed for backpressure, never driven.
- [ADR-0011](0011-product-definition.md): Product definition: parity with Huntarr and newtarr.
- [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md): SnatchArr manages the download queue; the Cleanuparr link goes.
- [ADR-0018](0018-product-front-door.md): Product front door of the media stack.
- Hourly search stamina rule: [AGENTS.md](https://github.com/lusoris/SnatchArr/blob/main/AGENTS.md), issue #180.
