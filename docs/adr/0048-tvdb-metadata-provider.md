<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: CC-BY-SA-4.0
-->
<!-- markdownlint-disable MD013 -->

# ADR-0048: TheTVDB as opt-in metadata provider

- **Status**: Accepted
- **Date**: 2026-11-13
- **Deciders**: lusoris
- **Supersedes**: the TheTVDB half of the rejected alternative "Direct TVDB and TMDB API integration" in [ADR-0019](0019-foreign-title-search.md), as an opt-in provider; TMDB stays rejected there
- **Related**: [ADR-0019](0019-foreign-title-search.md) (amended), [ADR-0039](0039-metadata-single-source.md), [ADR-0044](0044-runtime-settings-parity.md)

## Context

**SnatchArr** (meaning: automated media search-and-snatch coordinator across `*arr` instances; origin: English *snatch* + the `*arr` suffix of the Servarr ecosystem; pronunciation: `/ˈsnætʃ.ɑːr/`) builds foreign-language search titles from the aliases Sonarr and Radarr cache ([ADR-0019](0019-foreign-title-search.md)). Those aliases carry no language field and are often missing a localized title that TheTVDB lists. Operator direction on 2026-11-13 asks for TheTVDB API v4 as an opt-in metadata provider (paraphrased; ledger id `snatcharr-metadata-providers-tvdb-first-20261113`, 2026-11-13, topic: metadata providers, TheTVDB first).

**TheTVDB** (meaning: community-driven television and movie metadata database; origin: *TV* + *DB* with the *the* of its domain name; pronunciation: "the T-V-D-B") serves translations and aliases per language through its v4 REST API.

**goenvoy** (meaning: typed Go client SDK for `*arr` and media servers; origin: *Go* + *envoy*; pronunciation: `/ˈɡoʊ.ɛn.vɔɪ/`) already ships a TVDB v4 client (`metadata/video/tvdb`, v1.3.7). SnatchArr uses it and writes no TVDB client of its own.

## Decision

1. **Opt-in and secret.** `snatcharr.metadata.tvdb.api_key` and `snatcharr.metadata.tvdb.pin` are secret-class settings (encrypted at rest, write-only in the API, restart to apply). An empty key disables the provider: it is nil-safe and makes zero network requests.
2. **Enrich only ([ADR-0039](0039-metadata-single-source.md)).** The provider adds search titles; the *arr aliases stay the base. ADR-0039 keeps the *arr instance and its upstream catalogue canonical for catalogue metadata and Jellyfin or Emby canonical for watch state; TVDB titles are derived, read-only search input held in memory. SnatchArr never writes them back to an *arr, Jellyfin or Emby field, never stores them in Postgres, and never lets them replace a field those systems own. A TVDB error never fails a search or an API response, it only sets a status.
3. **What it reads.** Sonarr: series translation and aliases by TVDB id. Radarr: remote-id search (IMDB id, then TMDB id, movie hits only), then the movie's translation and aliases. Other app kinds: no call. Rule language codes map to TVDB's three-letter codes through `golang.org/x/text` (`de` to `deu`, `fr` to `fra`), checked for every embedded rule language by test.
4. **Bounds.** Every call has a deadline; at most 200 aliases per item; response bodies capped at 4 MiB; requests rate limited with `golang.org/x/time/rate` (default 2 per second, burst 4) at the transport, so logins and retries count; a 429 is retried a bounded number of times, then returns `ErrRateLimited`. One TVDB request runs at a time: the goenvoy client rewrites its request auth in shared state on every call (goenvoy issue 321), so the provider holds a one-slot semaphore around it, and a waiting lookup gives up at its own deadline. Outbound requests carry the SnatchArr User-Agent the *arr clients send.
5. **Cache.** One shared in-process L1 pool (`golusoris/cache/memory`, settings `cache.memory.max_size` and `cache.memory.ttl`) serves every outbound metadata provider. Each provider builds its own `golusoris/cache/twotier` typed view over that pool, with its own key prefix (TVDB: `metadata.tvdb`) and its own expiry, so providers share capacity without sharing keys. TVDB keys are `kind:id:lang` under that prefix and never contain the key, PIN or bearer token; login answers are never cached. 24 hours for a hit, 1 hour for a 404 or an unknown remote id (a timestamp inside the entry, as the view has one expiry for all its entries). Concurrent identical lookups share one request. No L2 yet.
6. **Login latch.** A refused login (401 or 403 on `/login`) returns `ErrUnauthorized` and latches: no further request until restart with a new key. Other 401 or 403 answers do not latch. goenvoy exports no sentinel for a refused login yet (goenvoy issue 323), so the provider matches its fixed message text until one exists.
7. **Secrets stay out.** The provider returns its own sentinel errors; goenvoy error text, URLs with credentials and response bodies are never surfaced. The key and PIN travel only in the login body, which goenvoy sends; the provider has no write calls of its own.
8. **Surface.** `GET /api/v1/instances/{instanceId}/items/{itemId}/foreign-titles?language=` (`getForeignTitles`, admin only) returns the titles, each tagged `arr` or `tvdb`, the provider status and the attribution.
9. **Attribution.** TheTVDB requires attribution: "Data courtesy of TheTVDB.com". It is carried in the provider package doc, the operator docs and the API description and response.

## Consequences

### Positive

- Missing localized titles (for example a German series title) become searchable without a manual alias.
- Zero effect for operators who set no key.

### Negative

- A new external dependency with its own rate limit, outage and key lifecycle; mitigated by cache, limit, latch and the enrich-only rule.
- TVDB data quality varies by title; wrong aliases cost a search. The title list is capped (50) and deduplicated, and the hourly search cap still bounds spend.

### Open points

- TVDB publishes no rate limit; the 2 per second default is a conservative guess, not a measured limit. Revisit when TVDB documents one or a 429 shows up in the field.
- Subscriber PIN semantics are unverified against a live key; no live TVDB call was made in tests.
- The cache moves to the golusoris outbound HTTP cache and rate-limit transport once it is released (fleet order `golusoris-outbound-http-cache-dragonfly-20261113`), wired through the goenvoy `WithHTTPClient` option, with Dragonfly as L2 through rueidis. Until then the in-process view above stands; the switch changes no setting name of the provider.
- goenvoy gaps found while wiring the client: shared request-auth state (issue 321), no response body cap (issue 322; SnatchArr caps at its transport), no login-refused sentinel (issue 323). Each workaround names its issue in code and goes when the issue is fixed.
- The hardcoded `SnatchArr/1.0` User-Agent of the *arr clients carries no build version; tracked as its own issue.

## Alternatives considered

- **Own TVDB client**: rejected; goenvoy ships one and the repository rule is to reuse maintained code.
- **Always on with a project key**: rejected; a shared key breaks TVDB's per-project terms and the zero-request default.
- **TMDB translations too**: not now; Radarr already queries TMDB for its own aliases ([ADR-0019](0019-foreign-title-search.md)).

## Verification

- Provider tests (`api/internal/metadata/tvdb/provider_test.go`) run against a stub server: translation and alias paths, Radarr remote-id path, no key means zero requests, wrong credentials latch, 429 bounded, 404 negative cache and expiry, alias cap, cache hit, ten concurrent calls share one lookup, a waiting lookup honours its deadline, the User-Agent header, a missing cache pool, language mapping for every rule file, and planted key and PIN never appear in errors or logs.
- Helm: `deploy/helm/snatcharr/tests/metadata_test.yaml` (no Secret means no TVDB variable, Secret and custom key names render `secretKeyRef`, an inline key is refused by the schema).
- Service and handler tests: `api/internal/foreigntitles/service_test.go`, `api/internal/httpapi/handlers_foreign_titles_test.go`.

## References

- Operator direction 2026-11-13 (paraphrased): add TheTVDB as an opt-in metadata provider; unit brief `metadata-tvdb-impl`.
- [TheTVDB API information](https://thetvdb.com/api-information) and [v4 API docs](https://thetvdb.github.io/v4-api/): keys, login, translation and remote-id search routes, attribution.
- goenvoy `metadata/video/tvdb` v1.3.7: [github.com/golusoris/goenvoy](https://github.com/golusoris/goenvoy); client gaps: <https://github.com/golusoris/goenvoy/issues/321>, <https://github.com/golusoris/goenvoy/issues/322>, <https://github.com/golusoris/goenvoy/issues/323>.
- golusoris `cache/memory` and `cache/twotier` v0.20.0: [github.com/golusoris/golusoris](https://github.com/golusoris/golusoris).
