<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: CC-BY-SA-4.0
-->
<!-- markdownlint-disable MD013 -->

# ADR-0043: Adult Module Served HTTP Surface and UI Screens

- **Status**: Accepted
- **Date**: 2026-10-27
- **Deciders**: lusoris
- **Relates to**: [ADR-0024](0024-watch-aware-modes.md), [ADR-0025](0025-module-switches.md), [ADR-0027](0027-adult-module.md), [ADR-0029](0029-task-flow-ui.md), [ADR-0033](0033-adult-identity-and-model.md), [ADR-0036](0036-community-requests-and-voting.md), [ADR-0037](0037-studio-scoped-numbered-performer-names.md)

## Context

**SnatchArr** (meaning: an automated media-snatching coordinator for `*arr` instances; origin: English *snatch* + the `*arr` suffix of the Servarr ecosystem; pronunciation: `/ˈsnætʃ.ɑːr/`) ships background synchronization and image-set scanning in its optional adult module (`api/internal/adult`, governed by [ADR-0027](0027-adult-module.md), [ADR-0033](0033-adult-identity-and-model.md), and [ADR-0037](0037-studio-scoped-numbered-performer-names.md)), alongside community request settings in `api/internal/requests/settings.go` ([ADR-0036](0036-community-requests-and-voting.md)).

Inspection of the current control plane (`api/openapi/openapi.yaml`, `api/internal/adult`, `api/internal/httpapi`, and `api/internal/requests`) shows that several shipped domain capabilities have no served HTTP route or Single-Page Application (SPA) screen:

1. **Catalogue sources (`adult_stashbox_instances`)**:
   - Migration `000036_adult_module.up.sql` (`api/internal/store/migrations/postgres/000036_adult_module.up.sql:7-17`) defines `adult_stashbox_instances`, and `api/internal/store/queries/adult.sql:4-25` defines `UpsertAdultStashboxInstance`, `GetAdultStashboxInstance`, `ListSyncableAdultStashboxInstances`, and `ListEnabledAdultStashboxInstances`.
   - Each catalogue source connects to a stash-box compatible GraphQL endpoint (where **stash-box**, meaning an open-source GraphQL metadata directory and fingerprint service, from English *stash* + *box*, pronounced `/ˈstæʃ.bɒks/`, defines the upstream GraphQL schema family) via `api/internal/adult/pool.go:24-48`.
   - Because no OpenAPI operation calls `UpsertAdultStashboxInstance`, operators cannot register catalogue sources over HTTP or the UI, leaving `Service.SyncAll` (`api/internal/adult/sync.go:34-54`) and the periodic leader loop (`api/internal/adult/ticker.go:18-41`) with zero rows to synchronize.
2. **Synchronized performers and studios (`adult_performers`, `adult_studios`, `adult_external_refs`)**:
   - Migration `000046_adult_normalized_model.up.sql` (`api/internal/store/migrations/postgres/000046_adult_normalized_model.up.sql:8-98`) and `api/internal/store/queries/adult.sql:27-194` store normalized performers, studios, and external references, populated by `Service.SyncInstance` (`api/internal/adult/sync.go:57-88`), with no read routes or UI views.
3. **Image sets and bounded image byte streaming (`adult_image_sets`, `adult_image_items`)**:
   - `Service.ScanImageSets` (`api/internal/adult/imageset.go:36-112`) scans image-set directories into `adult_image_sets` and `adult_image_items`, and `Service.ServeImage` (`api/internal/adult/serve.go:25-93`) validates root containment and streams image bytes with `http.ServeContent`, but no HTTP route invokes `ScanImageSets` or `ServeImage`.
4. **Community request settings (`api/internal/requests/settings.go`)**:
   - `requests.Settings` (`api/internal/requests/settings.go:33-85`) defines `voting_mode`, `allow_viewer_votes`, `visibility`, and `voting_ttl_days` with validation and role checks, but has no `/requests/settings` HTTP endpoint or UI settings view.

### Terminology and project names referenced in this ADR

Every project or library named in this record is introduced below with its meaning, origin, and pronunciation:

- **Whisparr** (meaning: an adult video collection manager in the Servarr family; origin: English *whisper* + `*arr`; pronunciation: `/ˈwɪs.pɑːr/`): `*arr` target supported by SnatchArr (`domain.KindWhisparrV2`, `domain.KindWhisparrV3`).
- **golusoris** (meaning: shared Go foundation libraries for lusoris services; origin: *Go* + Latin *lusoris*, of the player; pronunciation: `/ˌɡoʊ.luːˈsɔː.rɪs/`), **goenvoy** (meaning: typed Go HTTP and GraphQL clients for media servers and `*arr` APIs; origin: *Go* + *envoy*, a diplomatic messenger; pronunciation: `/ˌɡoʊˈɛn.vɔɪ/`), and **sveltesentio** (meaning: shared Svelte 5 design system, query, and accessibility library; origin: *Svelte* + Latin *sentio*, to perceive or feel; pronunciation: `/ˌsvɛltˈsɛn.ti.oʊ/`): first-party libraries used across SnatchArr.
- **ogen** (meaning: an OpenAPI 3 code generator for Go without reflection; origin: *OpenAPI* + *generator*; pronunciation: `/ˈoʊ.dʒɛn/`): generates `api/internal/build/oas` from `api/openapi/openapi.yaml` (`api/internal/httpapi/generate.go:9`).
- **sqlc** (meaning: a SQL compiler generating type-safe Go from SQL queries; origin: *SQL* + *compiler*; pronunciation: `/ˌɛs.kjuː.ɛlˈsiː/`): generates `api/internal/store/sqlcgen` from `api/internal/store/queries/*.sql`.
- **Paraglide** (meaning: a compile-time internationalization library from Inlang; origin: English *paraglide*; pronunciation: `/ˈpær.ə.ɡlaɪd/`): compiles `web/messages/en.json` and `web/messages/de.json` into typed message functions.

---

## Decision

Every adult module feature and the community request settings model receive a fixed OpenAPI HTTP route surface under `/api/v1` (`api/openapi/openapi.yaml`) and corresponding SPA routes under `web/src/routes/`, mirroring the `watchaware` precedent (`/watchaware/sources`, `/watchaware/sources/{sourceId}`, `/watchaware/sync/status` in `api/openapi/openapi.yaml:2041-2142` and `api/internal/httpapi/handlers_watchaware.go:19-139`).

### 1. Fixed HTTP route surface (`/api/v1`)

| Path | Method | `operationId` | `x-module` | Auth / API key scope | Purpose and backing implementation |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `/adult/sources` | `GET` | `listAdultSources` | `adult` | Viewer+ / `adult:read` | List configured catalogue sources (`adult_stashbox_instances`), ordered by `priority ASC, name ASC`; never returns `api_key` or `api_key_enc`. |
| `/adult/sources` | `POST` | `createAdultSource` | `adult` | Admin / `adult:write` | Create a catalogue source (`AdultSourceInput`); validates `http`/`https` endpoint URL, seals `api_key` with AES-GCM (`core/crypto.Encryptor.Seal`, mirroring `api/internal/instances/service.go:90` and `api/internal/watchaware/store.go:88`), and persists via `UpsertAdultStashboxInstance`. |
| `/adult/sources/{sourceId}` | `GET` | `getAdultSource` | `adult` | Viewer+ / `adult:read` | Fetch a single catalogue source by UUID (`GetAdultStashboxInstance`); `404` RFC 9457 problem when absent. |
| `/adult/sources/{sourceId}` | `PUT` | `updateAdultSource` | `adult` | Admin / `adult:write` | Update a catalogue source; preserves existing `api_key_enc` when `api_key` is omitted or empty, or re-seals when a new `api_key` is supplied (`UpsertAdultStashboxInstance`). |
| `/adult/sources/{sourceId}` | `DELETE` | `deleteAdultSource` | `adult` | Admin / `adult:write` | Delete a catalogue source (`DeleteAdultStashboxInstance`; `204 No Content`; `404` when absent). |
| `/adult/sync` | `POST` | `triggerAdultSync` | `adult` | Admin / `adult:write` | Run favourite performer and studio synchronization immediately across enabled `sync_favorites` catalogue sources (`adult.Service.SyncNow`), returning `AdultSyncSummary` (`sources_synced`, `performers_synced`, `studios_synced`). |
| `/adult/sync/status` | `GET` | `getAdultSyncStatus` | `adult` | Viewer+ / `adult:read` | Return `AdultSyncStatus`: `enabled_sources`, `total_sources`, `last_sync_at`, `last_error`, `performers_synced`, and `studios_synced`. |
| `/adult/performers` | `GET` | `listAdultPerformers` | `adult` | Viewer+ / `adult:read` | Paginated list of synchronized performers (`adult_performers`, `api/internal/store/queries/adult.sql:70-89`), supporting `favorite` filter and `limit`/`cursor` pagination. |
| `/adult/performers/{performerId}` | `GET` | `getAdultPerformer` | `adult` | Viewer+ / `adult:read` | Fetch a single performer (`GetAdultPerformer`) with external catalogue references (`ListAdultExternalRefsByEntity`, `entity_type = 'performer'`). |
| `/adult/studios` | `GET` | `listAdultStudios` | `adult` | Viewer+ / `adult:read` | Paginated list of synchronized studios (`adult_studios`, `api/internal/store/queries/adult.sql:148-167`), supporting `favorite` filter and `limit`/`cursor` pagination. |
| `/adult/studios/{studioId}` | `GET` | `getAdultStudio` | `adult` | Viewer+ / `adult:read` | Fetch a single studio (`GetAdultStudio`) with external catalogue references (`ListAdultExternalRefsByEntity`, `entity_type = 'studio'`). |
| `/adult/image-sets` | `GET` | `listAdultImageSets` | `adult` | Viewer+ / `adult:read` | List discovered image sets (`ListAdultImageSets`, `api/internal/store/queries/adult.sql:214-217`) with pagination. |
| `/adult/image-sets/{setId}` | `GET` | `getAdultImageSet` | `adult` | Viewer+ / `adult:read` | Fetch an image set (`GetAdultImageSet`), its `ignore_file_present` status, and its ordered items (`ListAdultImageItemsBySet`, `api/internal/store/queries/adult.sql:233-236`). |
| `/adult/image-items/{itemId}/file` | `GET` | `getAdultImageItemFile` | `adult` | Viewer+ / `adult:read` | Stream image bytes for `itemId` via `adult.Service.ServeImage` (`api/internal/adult/serve.go:25-93`), enforcing root-directory containment (`os.OpenInRoot`) and `X-Content-Type-Options: nosniff`. |
| `/adult/scan` | `POST` | `triggerAdultScan` | `adult` | Admin / `adult:write` | Trigger an on-demand image-set scan (`adult.Service.ScanImageSets`, `api/internal/adult/imageset.go:36`); uses the dedicated path `/adult/scan` so it never collides with `/adult/image-sets/{setId}`. |
| `/requests/settings` | `GET` | `getRequestsSettings` | none | Viewer+ / `requests:read` | Read community request settings (`api/internal/requests/settings.go:33-50`). Core request configuration (no `x-module`). |
| `/requests/settings` | `PUT` | `updateRequestsSettings` | none | Admin / `admin` | Validate (`Settings.Validate`, `api/internal/requests/settings.go:53-71`) and persist community request settings. |

### 2. Cross-cutting contracts and security invariants

1. **Module gate (`x-module: adult`)**:
   - Every `/adult/*` operation in `api/openapi/openapi.yaml` carries `tags: [adult]` and `x-module: adult`.
   - `tools/moduleroutes` (`api/internal/httpapi/generate.go:10`) compiles the operation map into `api/internal/build/moduleops/moduleops_gen.go`.
   - `httpapi.ModuleGateMiddleware` (`api/internal/httpapi/module_gate.go:25-49`) intercepts requests when the `adult` module is disabled (`default: false` in `api/internal/adult/module.go:24-30`) and returns `404 Not Found` as `application/problem+json` without executing handler logic.
2. **Security scopes (`adult:read`, `adult:write`)**:
   - `ApiKeyScope` (`api/openapi/openapi.yaml:2567-2568`, `api/internal/domain/apikey.go:37-38`) defines `adult:read` for `GET` operations and `adult:write` for mutating `POST`, `PUT`, and `DELETE` operations.
   - Session-authenticated requests require `admin` role (`requireAdmin` in `api/internal/httpapi/security.go`) for all mutating operations (`POST`, `PUT`, `DELETE`).
3. **Write-only AES-GCM credentials**:
   - `AdultSourceInput.api_key` is write-only: required when creating a catalogue source, optional when updating (an empty or omitted key retains the stored ciphertext), sealed at rest with `crypto.Encryptor.Seal` (`AES-256-GCM`), and never included in `AdultSource` responses.
4. **RFC 9457 Problem Details**:
   - All validation failures (`400`/`422`), authentication/authorization errors (`401`/`403`), missing entities or disabled module gates (`404`), and duplicate source names (`409`) return `application/problem+json` via `httpapi.ErrorHandler` (`api/internal/httpapi/problem.go`).
5. **Privacy-preserving logging ([ADR-0027](0027-adult-module.md))**:
   - Structured log entries in `api/internal/adult` and `api/internal/httpapi` record only UUIDs (`source_id`, `performer_id`, `studio_id`, `set_id`, `item_id`) and numeric counts—never performer names, studio names, file paths, or API keys.

### 3. UI screens and navigation gating

| SPA Route | Governing module | Purpose | Delivery slice |
| :--- | :--- | :--- | :--- |
| `/adult/sources` | `adult` | Manage catalogue sources (create, edit, delete), inspect synchronization status (`GET /adult/sync/status`), and trigger manual synchronization (`POST /adult/sync`). | Slice 1 (this PR) |
| `/adult/performers` | `adult` | Browse synchronized performers, filter by favourite status, and inspect external catalogue references. | Slice 2 |
| `/adult/studios` | `adult` | Browse synchronized studios, filter by favourite status, and inspect external catalogue references. | Slice 2 |
| `/adult/image-sets` | `adult` | Browse discovered image sets, trigger `/adult/scan`, and view items streamed from `/adult/image-items/{itemId}/file`. | Slice 3 |
| `/settings/requests` | none | Configure community request voting mode, viewer voting permission, visibility, and voting TTL (`GET`/`PUT /requests/settings`). | Slice 4 |

- **Navigation and route guard (`web/src/lib/components/shell/nav.ts`)**:
  - `PRIMARY_NAV_ITEMS` includes `{ href: '/adult/sources', label: m.nav_adult_sources(), icon: 'heart', module: 'adult' }`, filtered by `filterByModule(PRIMARY_NAV_ITEMS, activeModules)`.
  - `MODULE_ROUTES` maps `'/adult': 'adult'` so `isRouteDisabled` in `web/src/routes/+layout.svelte:91` renders the 404 guard view whenever the `adult` module is disabled.
  - All user-facing strings are localized in both `web/messages/en.json` and `web/messages/de.json` via Paraglide.

---

## Consequences

### Positive

- **Complete vertical slice for catalogue sources**: operators can register, update, and remove catalogue sources, run on-demand synchronization, and inspect synchronization telemetry through a contract-first OpenAPI surface and accessible Svelte 5 UI.
- **Stable route lattice across follow-up PRs**: fixing all `/adult/*` and `/requests/settings` paths in one ADR prevents path collisions (such as `/adult/scan` vs `/adult/image-sets/{setId}`) and keeps OpenAPI operation naming uniform.
- **Zero-leak module boundary**: when the `adult` module is off (the default), every `/adult/*` API endpoint returns `404 application/problem+json` and the `/adult/*` navigation entry and routes remain hidden.

### Negative and trade-offs

- **Multi-slice rollout**: while the full route table is fixed in this ADR, the performer/studio browser (`/adult/performers`, `/adult/studios`), image-set browser (`/adult/image-sets`, `/adult/scan`), and request settings (`/requests/settings`) ship in dedicated follow-up pull requests to keep each change reviewable.
- **In-process single-flight only**: `Service.SyncNow` serializes concurrent sweeps within a single API replica (`409 Conflict` on overlap, leader tick skipped at debug), so a manual `POST /adult/sync` routed to a non-leader replica can still overlap with a leader sweep across replicas, which remains safe because performer, studio, external-ref, and sync-status writes are idempotent upserts.

---

## Verification

1. **Slice 1 (`/adult/sources`, `/adult/sync`, `/adult/sync/status`)**:
   - OpenAPI generation (`make api-gen`, `make web-gen`) and module-route verification (`tools/moduleroutes`).
   - Go integration tests in `api/internal/httpapi/handlers_adult_sources_integration_test.go` and `router_integration_test.go` covering positive CRUD + sync, negative (`400` invalid endpoint URL / missing API key, `404` unknown UUID, `404` when `adult` module is disabled, `403` viewer mutation), and boundary (name length limits, disabled source sync summary `0`).
   - Vitest unit tests in `web/src/routes/adult/sources/page.svelte.test.ts` and `web/src/lib/components/shell/nav.test.ts`.
   - End-to-end Playwright spec in `web/e2e/features/674-adult-source-sync.test.ts` against a local GraphQL stub server.
