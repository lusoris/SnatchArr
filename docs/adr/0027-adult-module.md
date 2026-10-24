<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0027: Adult module: stash-box sources, Whisparr requests and image sets

- Status: proposed
- Date: 2026-10-24
- Related: [ADR-0006](0006-download-client-backpressure.md), [ADR-0018](0018-product-front-door.md), [ADR-0021](0021-llm-access-module.md), [ADR-0023](0023-sso-viewer-role.md), [ADR-0025](0025-module-switches.md)

## Context

SnatchArr serves as the front door of the media stack ([ADR-0018](0018-product-front-door.md)). Under this unified scope, SnatchArr provides a single application to monitor missing media, govern download queues, manage adult media libraries and image sets, and expose structured Model Context Protocol (MCP) tooling (`snatcharr-product-job-20261020`). Specialized external applications remain the backend engines: where existing systems already perform a domain job, SnatchArr integrates with them to bridge gaps rather than duplicating functional software (`snatcharr-product-overlap-20261020`).

Follow-up item 3 of ADR-0018 (`docs/adr/0018-product-front-door.md:184`) and issue [#566](https://github.com/lusoris/SnatchArr/issues/566) require an architectural decision for the adult content module (`napp3-folded-into-napp2-20261020`). The module scope spans:

1. Federated discovery across multiple Stash-box community metadata instances.
2. Synchronizing and searching performer and studio favourites across instances.
3. Ingesting requests and dispatching them into Whisparr instances.
4. Managing and browsing local image-set photo galleries linked to Whisparr scenes.
5. Providing API endpoints for companion client integrations (specifically the Jellyfin companion plugin; [ADR-0018](0018-product-front-door.md) follow-up 4, issue [#567](https://github.com/lusoris/SnatchArr/issues/567)).

### Current implementation baseline

A survey of the repository shows the current baseline for adult media and integration handling:

1. **Whisparr instance support**:
   - `api/internal/domain/instance.go` defines `KindWhisparrV2` and `KindWhisparrV3`.
   - `api/internal/arrclient/probe.go:137-140,238-251` validates connectivity and distinguishes between Whisparr v2 (Sonarr-forked architecture) and Whisparr v3 (Eros, Radarr-forked architecture).
   - `api/internal/arrclient/queue.go:147-151,261-307` implements queue queries for Whisparr v2 (episodes) and v3 (movies).
   - In the Rust worker, `worker/crates/arr-client/src/lib.rs:40-43,376-415,807-810` parses Whisparr v2 scenes and Whisparr v3 movies (`WhisparrMovieLike`).
2. **Module switches**:
   - [ADR-0025](0025-module-switches.md) defines the unified switch framework: declarative module descriptors in `api/internal/modules`, persistence in the `modules` table (`docs/adr/0025-module-switches.md:95-100`), OpenAPI operation tagging via `x-module` (`docs/adr/0025-module-switches.md:121-135`), RFC 9457 HTTP 404 Problem JSON refusal when disabled, and quiescent periodic leader loops (`AGENTS.md:129`). ADR-0025 is proposed; this module depends on its slices.
3. **Secret encryption at rest**:
   - Stored credentials across instances and integrations are sealed via `crypto.Encryptor` (AES-GCM) in `api/internal/app/crypto.go:26-39`, `api/internal/instances/service.go:50`, and `api/internal/seerr/service.go:69`. Plaintext credentials never appear in API read responses or logs.
4. **Access control and roles**:
   - [ADR-0023](0023-sso-viewer-role.md) (`docs/adr/0023-sso-viewer-role.md:38-46`) maps SSO groups to `admin` and `viewer` roles. User accounts and identity integration with media servers (Jellyfin) are specified in the accounts planning track (`snatcharr-product-accounts-20261020`, `snatcharr-product-adult-visibility-20261020`).

### Problem statement

Integrating adult media into SnatchArr without clear architectural boundaries risks four distinct failure modes:

1. **Metadata fragmentation**: Stash-box instances (such as StashDB and private instances) maintain distinct UUID namespaces for identical performers and studios. Searching each instance in isolation produces disjointed, redundant results.
2. **Download client coupling**: Attempting to drive bittorrent or Usenet downloaders directly for adult releases violates [ADR-0006](0006-download-client-backpressure.md), bypassing Whisparr's state tracking, import naming rules, and indexer health governance.
3. **Storage exhaustion from image sets**: Storing raw high-resolution image sets directly inside Postgres inflates the database, its WAL and its backups, and slows general database operations.
4. **Unintended presence exposure**: Exposing adult metadata, scene titles, or performer names across general metrics labels, system-wide application logs, or public SSE channels violates household privacy and parental control requirements (`snatcharr-product-adult-visibility-20261020`).

## Decision

SnatchArr implements the adult content module under the module identifier `adult` (`napp2-name-direct-adult-20261020`), disabled by default (`napp-shape-20261020`), adhering to the architectural decisions detailed below.

### 1. Stash-box protocol and multi-instance federation

The adult module integrates with Stash-box instances using the upstream GraphQL API definition ([stashapp/stash-box](https://github.com/stashapp/stash-box) latest release `v0.10.3`, commit `5eb3551e19fd494e93be410d8149adc785cffd6a`; line numbers below are from that tag).

- **Upstream protocol characteristics**:
  - Transport: HTTP POST to the `/graphql` endpoint with JSON payload `{"query": "...", "variables": {...}}`.
  - Authentication: header `Apikey: <secret>` carrying the user's API key (`User.apiKey`, `graphql/schema/types/user.graphql:122`); queries and the favourite mutations require `@hasRole(role: READ)`.
  - Core entities: `Performer` (`types/performer.graphql`), `Studio` (`types/studio.graphql`), `Scene` (`types/scene.graphql`), and `Tag` (`types/tag.graphql`).
  - Identifiers: UUIDv4 strings generated per instance (`ID!`).
  - User favourites: The upstream schema provides `is_favorite: Boolean!` on `Performer` and `Studio`, (`types/performer.graphql:124`, `types/studio.graphql:11`), with query inputs `PerformerQueryInput.is_favorite` (`types/performer.graphql:382`) and `StudioQueryInput.is_favorite` (`types/studio.graphql:89`). The schema exposes mutations `favoritePerformer(id: ID!, favorite: Boolean!): Boolean!` and `favoriteStudio(id: ID!, favorite: Boolean!): Boolean!` (`graphql/schema/schema.graphql:215-217`). Favourites belong to the stash-box user that owns the API key, so each configured instance key represents one stash-box account.

- **Multi-instance storage and security**:
  - Stash-box instance configurations are stored in dedicated table `adult_stashbox_instances`:

    ```sql
    CREATE TABLE adult_stashbox_instances (
        id UUID PRIMARY KEY,
        name VARCHAR(128) NOT NULL,
        endpoint VARCHAR(512) NOT NULL,
        api_key_enc BYTEA NOT NULL,
        enabled BOOLEAN NOT NULL DEFAULT TRUE,
        sync_favorites BOOLEAN NOT NULL DEFAULT TRUE,
        priority INT NOT NULL DEFAULT 0,
        created_at TIMESTAMPTZ NOT NULL,
        updated_at TIMESTAMPTZ NOT NULL
    );
    ```

  - API keys are encrypted at rest using `crypto.Encryptor` (AES-GCM) (`api/internal/app/crypto.go:26-39`). In OpenAPI models, the API key is write-only, returning masked strings on read operations (`api/internal/instances/service.go:50`).

- **Upstream library reuse**:
  - Investigation of `github.com/golusoris/goenvoy` reveals an existing upstream client at release tag `metadata/adult/stashbox/v2.0.3` (commit `b08a9f272e927870f7f214bf9fd80494d1b17d08`, read in this run).
  - Package `github.com/golusoris/goenvoy/metadata/adult/stashbox/v2` (`stashbox.go`) provides `Client` with `Apikey` header auth (`stashbox.go:170`), a generic `Query(ctx, query, variables, dst)` (`stashbox.go:158`), `FindPerformer`, `QueryPerformers`, `SearchPerformers`, `FindScene`, `QueryScenes`, `SearchScenes`, `FindStudio`, `QueryStudios`, `SearchStudios`, `FindTag`, `QueryTags`, `SearchTags`, `ListSites`, `GetConfig`, and `FindScenesByFingerprints`.
  - In compliance with HISS-19 ("reuse before writing"), SnatchArr imports `github.com/golusoris/goenvoy/metadata/adult/stashbox/v2` directly for all query operations. Typed wrappers for the favourite mutations (`favoritePerformer`, `favoriteStudio`) and an `is_favorite` filter on `QueryInput` are contributed upstream to `goenvoy`; until they are released, the generic `Query` method can carry them without a second client in SnatchArr. Whether the goenvoy types follow the stash-box `v0.10.3` schema is checked in the federation slice (unverified here).

- **Options considered**:
  - *Option A: Reuse `goenvoy/metadata/adult/stashbox` v2.0.3 and upstream mutation additions* (Recommended).
    - *Trade-offs*: Follows project-wide reuse invariant HISS-19. Eliminates code duplication for GraphQL request execution, JSON decoding, and standard query fragments. Requires an upstream PR in goenvoy for write mutations.
  - *Option B: Write an internal custom GraphQL client inside `api/internal/adult`*.
    - *Trade-offs*: Fully self-contained inside the repository, but duplicates the GraphQL transport, boilerplate error handling, and test fixtures already verified in goenvoy.
  - *Option C: Generate code with `gqlgen` or `ogen` directly against stash-box schema files*.
    - *Trade-offs*: Heavy build-time code generation burden, introduces large generated Go files that conflict with HISS line limits, and creates schema synchronization overhead across independent stash-box versions.
- **Recommendation**: **Option A**. Reuse `github.com/golusoris/goenvoy/metadata/adult/stashbox/v2`.

### 2. Favourites synchronization and cross-instance search

Users maintain lists of preferred performers and studios across multiple community stash-box instances (`napp3-folded-into-napp2-20261020`).

- **Data synchronization**:
  - Synchronized entities: Performer IDs and Studio IDs flagged as `is_favorite` on each configured instance, along with their canonical names, aliases, and external link URLs (`urls { url site { id name url } }`).
  - Table `adult_favorite_performers` stores local favorite records:

    ```sql
    CREATE TABLE adult_favorite_performers (
        id UUID PRIMARY KEY,
        canonical_name VARCHAR(255) NOT NULL,
        disambiguation VARCHAR(255),
        created_at TIMESTAMPTZ NOT NULL,
        updated_at TIMESTAMPTZ NOT NULL
    );

    CREATE TABLE adult_performer_links (
        favorite_id UUID NOT NULL REFERENCES adult_favorite_performers(id) ON DELETE CASCADE,
        instance_id UUID NOT NULL REFERENCES adult_stashbox_instances(id) ON DELETE CASCADE,
        foreign_id VARCHAR(64) NOT NULL,
        is_favorite BOOLEAN NOT NULL DEFAULT TRUE,
        last_synced_at TIMESTAMPTZ NOT NULL,
        PRIMARY KEY (instance_id, foreign_id)
    );
    ```

  - A parallel structure (`adult_favorite_studios` and `adult_studio_links`) tracks studio favourites.

- **Cross-instance entity merging**:
  - Each stash-box instance issues independent UUIDs. Merging identifies identical performers and studios across instances by matching:
    1. Known cross-references in `urls` pointing to other stash-box endpoints (e.g. StashDB URL on an alternative instance).
    2. Normalized external site URLs (e.g., matching Twitter/X handle, Instagram profile, IAFD URL, or official domain).
    3. Exact normalized name match when disambiguation is empty.
  - Unmatched entities remain distinct records linked to their originating instance. The UI allows manual linking or unlinking of conflicting records.

- **Periodic background synchronization**:
  - In compliance with `AGENTS.md:129`, the sync loop registers in fx group `leader_loops` via `gate.Provide[*T]()`.
  - When the adult module is disabled in `modules` (`docs/adr/0025-module-switches.md:95-100`), the background loop performs a quiescent tick (returning immediately without HTTP I/O).
  - When enabled, the loop executes on a configurable interval (default: 6 hours).
  - In compliance with HISS-02 (scalar upper bounds on loops and I/O), queries paginate with explicit page limits (e.g., `per_page: 50`, maximum 500 items per sweep), with a deadline on every context (`ctx, cancel := context.WithTimeout(ctx, 30*time.Second)`).

- **Federated search**:
  - Search requests fan out concurrently across all active stash-box instances with a bounded worker pool (`HISS-06`, max concurrency 4).
  - Search results are deduplicated by matched external link or canonical ID before returning to the frontend.

- **Options considered**:
  - *Option A: Relational entity mapping with periodic leader-loop synchronization* (Recommended).
    - *Trade-offs*: Fast local queries, offline browsing of favourites, unified cross-instance view, resilient against stash-box endpoint outages. Requires local background reconciliation logic.
  - *Option B: Pure runtime pass-through without local storage*.
    - *Trade-offs*: Zero local database tables, but results in high latency on every UI page load, cannot search across instances when one instance is degraded, and loses favourites if an API key expires.
  - *Option C: Master-replica synchronization enforcing two-way write sync*.
    - *Trade-offs*: Complex distributed state reconciliation, risk of race conditions or accidental un-favoriting across instances when tokens change.
- **Recommendation**: **Option A**. Periodic leader-loop sync into local relational tables with cross-instance link mapping.

### 3. Whisparr mapping and request pipeline

Requests for missing adult scenes or performer filmographies are ingested into SnatchArr and dispatched directly to Whisparr (`snatcharr-product-adult-scope-20261020`).

- **Whisparr entity model comparison**:
  - **Whisparr v3 (Eros)**: Forked from Radarr (`Whisparr/Whisparr` tag `v3.1.0.2116`, commit `94480761ec412cae40ba84e55c70b723851340cf`, branch `eros`; the GitHub releases list shows only v2 builds).
    - Whisparr v3 models scenes and movies as one "movie" entity distinguished by `ItemType` (`WhisparrMovieLike` in `worker/crates/arr-client/src/lib.rs:412-415`; `src/Whisparr.Api.V3/Movies/MovieResource.cs:82`).
    - The resource carries a dedicated `StashId` (`MovieResource.cs:71`) next to `ForeignId` (`MovieResource.cs:70`); `ForeignId` follows the item's metadata source (`src/NzbDrone.Core/MetadataSource/SkyHook/SkyHookProxy.cs:495-504`), so SnatchArr matches on `StashId`, not on `ForeignId`.
    - Scene lookup accepts the `stash:` / `stashid:` term prefix (`SkyHookProxy.cs:950`) through the movie lookup endpoint (`src/Whisparr.Api.V3/Movies/MovieLookupController.cs`).
    - Adding a scene posts the looked-up resource to `POST /api/v3/movie` with quality profile, root folder, `monitored: true` and `addOptions.searchForMovie`.
  - **Whisparr v2**: Forked from Sonarr (`v2.2.0-release.231`, commit `face3be8956e4c3798a9364a0f2ea95948e34c1c`).
    - Whisparr v2 models sites as "series" and individual scenes as "episodes" (`SceneLike`, `episodeId` in `worker/crates/arr-client/src/lib.rs:376-392`).
    - Adding scenes requires creating or monitoring the parent performer/studio series, then monitoring specific episodes.

- **Request execution pipeline**:
  - User submits a scene request in the SnatchArr UI or companion Jellyfin plugin.
  - SnatchArr resolves the target instance via the instance priority table ([ADR-0022](0022-instance-priority.md)).
  - For Whisparr v3: SnatchArr checks whether a movie with the scene's `StashId` already exists. If absent, it looks the scene up with `stash:<id>` and submits `POST /api/v3/movie`.
  - For Whisparr v2: SnatchArr maps the scene's site to the series entity and enables monitoring for the target episode (mapping detail unverified; settled in the request slice).
  - **Zero download client interaction**: SnatchArr strictly delegates snatching and indexing to Whisparr. In accordance with [ADR-0006](0006-download-client-backpressure.md), SnatchArr never interfaces with qBittorrent, Deluge, Transmission, or SABnzbd for adult requests.
  - **Hourly cap**: Searches triggered in Whisparr count against the per-indexer hourly cap (`AGENTS.md:126`): one search per scene or movie, debited per dispatch, warning at 80 %.

- **Options considered**:
  - *Option A: Whisparr v3 primary target with `StashId` mapping, legacy support for v2* (Recommended).
    - *Trade-offs*: Whisparr v3 (Eros) carries a native `StashId` per scene. Matching on it avoids title matching and duplicate adds.
  - *Option B: Treat Whisparr as a generic Radarr/Sonarr client without StashID specialization*.
    - *Trade-offs*: Fails to leverage StashDB metadata matching in Whisparr; relies on fuzzy title matching that causes false-positive downloads on adult releases.
  - *Option C: Download files directly via SnatchArr worker and push to disk*.
    - *Trade-offs*: Blatantly violates ADR-0006 and AGENTS.md; duplicates torrent/usenet handling, queue state, file renaming, and error recovery.
- **Recommendation**: **Option A**. Map scenes via `StashId` into Whisparr v3, retaining v2 compatibility via the existing `api/internal/arrclient` abstraction.

### 4. Image sets and gallery management

The adult module provides library management for image sets (photo shoot galleries) linked to Whisparr scenes and performers (`napp3-folded-into-napp2-20261020`).

- **Definition and upstream verification**:
  - An image set is a bounded collection of still images (photographs) associated with an adult performer, studio, or scene.
  - Upstream verification of `stashapp/stash-box` (`v0.10.3`): the source tree has no gallery type or file (zero paths matching `galler`); images exist only as entity images (`graphql/schema/types/image.graphql`). Stash-box therefore is no image-set source.
  - Self-hosted Stash (`mediaserver/stash` in `goenvoy` v2.0.2 / `types.go:118-140`) supports a local `Gallery` and `Image` model. SnatchArr uses Stash's gallery concepts purely as architectural reference, with zero code copied.

- **Storage schema and file references**:
  - Image sets store **file paths and metadata only**, never binary blobs in Postgres:

    ```sql
    CREATE TABLE adult_image_sets (
        id UUID PRIMARY KEY,
        title VARCHAR(255) NOT NULL,
        directory_path VARCHAR(1024) NOT NULL UNIQUE,
        whisparr_scene_foreign_id VARCHAR(64),
        performer_id UUID REFERENCES adult_favorite_performers(id) ON DELETE SET NULL,
        item_count INT NOT NULL DEFAULT 0,
        total_size_bytes BIGINT NOT NULL DEFAULT 0,
        cover_image_path VARCHAR(1024),
        created_at TIMESTAMPTZ NOT NULL,
        updated_at TIMESTAMPTZ NOT NULL
    );

    CREATE TABLE adult_image_items (
        id UUID PRIMARY KEY,
        image_set_id UUID NOT NULL REFERENCES adult_image_sets(id) ON DELETE CASCADE,
        file_path VARCHAR(1024) NOT NULL,
        file_name VARCHAR(255) NOT NULL,
        file_size_bytes BIGINT NOT NULL,
        mime_type VARCHAR(64) NOT NULL,
        width INT,
        height INT,
        sha256_checksum CHAR(64) NOT NULL,
        sort_order INT NOT NULL DEFAULT 0,
        created_at TIMESTAMPTZ NOT NULL
    );

    CREATE INDEX idx_adult_image_items_set ON adult_image_items(image_set_id, sort_order);
    ```

- **Sources and ingest**:
  - Image sets are discovered from:
    1. Local folders located alongside the scene media file in Whisparr's library (e.g. `<scene-folder>/.imagesets/` or adjacent folder matching scene name).
    2. A designated image sets root directory monitored via file system notification or scheduled scan.
    3. Archive files (`.zip`, `.cbz`) unpacked into the storage cache.

- **Thumbnails and memory safeguards**:
  - Thumbnails are generated asynchronously and cached on local disk (`<cache_dir>/thumbnails/<sha256>.webp`).
  - Scalar limits apply per HISS-02 (proposed defaults, configurable):
    - Maximum single image decode size: 100 MB.
    - Maximum image set size: 2 GB / 2,000 items.
    - Thumbnail format: WebP, max bounding box 400x400 pixels, quality 80.
    - Streaming image delivery: Images are served via `http.ServeContent` directly from disk with RFC 9111 HTTP cache headers (`Cache-Control: private, max-age=86400`), bypassing Go application heap buffers.

- **Options considered**:
  - *Option A: Filesystem-backed image references with local thumbnail cache* (Recommended).
    - *Trade-offs*: Fast random access, zero database bloat, compatible with high-capacity storage mounts. Requires managing filesystem permissions.
  - *Option B: Store images as `BYTEA` blobs inside PostgreSQL*.
    - *Trade-offs*: Unified database backups, but inflates database size, WAL traffic, and query latency.
  - *Option C: Require an external running Stash server instance for all image sets*.
    - *Trade-offs*: Avoids writing gallery storage in SnatchArr, but requires running an entire separate application with its own database and web server just to browse scene photo sets (`napp3-folded-into-napp2-20261020`).
- **Recommendation**: **Option A**. Relational filesystem references with on-disk WebP thumbnail cache.

### 5. Visibility, parental control, and boundary fencing

Adult media requires strict privacy isolation to prevent leakage into non-adult household profiles and public telemetry (`snatcharr-product-adult-visibility-20261020`).

- **Per-user gating**:
  - Authentication claims (sourced via SSO or Jellyfin user profiles per ADR-0018/ADR-0023) carry an `adult_access: bool` capability.
  - Users without `adult_access` receive HTTP 404 Not Found on all adult routes, concealing the existence of adult endpoints and content.

- **OpenAPI and MCP fencing**:
  - Every adult HTTP route in `api/openapi/openapi.yaml` declares vendor extension `x-module: adult` (`docs/adr/0025-module-switches.md:121-135`).
  - When the module is disabled via `modules.enabled = false`, the `ModuleGateMiddleware` (`docs/adr/0025-module-switches.md:141-149`) intercepts all requests and responds with RFC 9457 HTTP 404 Problem JSON.
  - Model Context Protocol (MCP) tools exposed by this module (e.g., `adult_search_scenes`, `adult_request_scene`, `adult_list_favorites`) declare module ownership in their descriptor ([ADR-0021](0021-llm-access-module.md), [ADR-0025](0025-module-switches.md)). When the module is off or the connected token lacks adult access, the tools are completely omitted from the MCP tool list response.

- **Zero-leakage telemetry and logging**:
  - **Structured logs**: `slog` logging inside `api/internal/adult` sanitizes scene titles, performer names, and file paths. Log statements record only synthetic IDs (UUIDs) and error codes, ensuring raw adult metadata never enters central log aggregates.
  - **Metrics**: Prometheus metrics emitted by this module (e.g., `snatcharr_adult_requests_total`, `snatcharr_adult_sync_duration_seconds`) use static status labels (`status="success|failure"`). No dynamic performer names, studio names, or scene titles are permitted in metric labels, preventing high-cardinality leaks in Grafana.
  - **SSE events**: Real-time events (`adult_scene_requested`, `adult_sync_completed`) are broadcast only over SSE connections authenticated with `adult_access = true`. Public or unprivileged SSE streams never receive adult event frames.

- **Options considered**:
  - *Option A: Complete 404 concealment, masked telemetry, and capability-gated SSE* (Recommended).
    - *Trade-offs*: Strict privacy and parental compliance. Prevents presence disclosure. Requires consistent enforcement across HTTP, SSE, logs, and metrics.
  - *Option B: HTTP 403 Forbidden with unmasked titles in logs*.
    - *Trade-offs*: Simpler to debug, but leaks existence of endpoints to unauthorized users and exposes private metadata in central log collectors.
  - *Option C: Separate independent binary daemon*.
    - *Trade-offs*: Absolute isolation, but fractures the single binary operational model and multiplies container deployment overhead.
- **Recommendation**: **Option A**. Total route concealment (HTTP 404), capability-fenced SSE, and sanitized telemetry.

### 6. Jellyfin companion integration boundary

The adult module provides dedicated backend services for the companion Jellyfin plugin ([ADR-0018](0018-product-front-door.md) follow-up 4, `snatcharr-product-jellyfin-plugin-20261020`).

- **Contract boundary**:
  - The internal architecture and C# implementation of the Jellyfin plugin is explicitly **out of scope** for this ADR (governed separately by the companion Jellyfin plugin ADR, issue [#567](https://github.com/lusoris/SnatchArr/issues/567)).
  - This module exposes a stable, documented OpenAPI contract under `/api/v1/adult/jellyfin/*`:
    1. `GET /api/v1/adult/jellyfin/image-sets`: List image sets linked to a Jellyfin item or scene foreign ID.
    2. `GET /api/v1/adult/jellyfin/image-sets/{id}/items`: Retrieve image set items with thumbnail URLs and dimensions.
    3. `GET /api/v1/adult/jellyfin/image-sets/{id}/items/{itemId}/content`: Stream raw image content with HTTP caching.
    4. `POST /api/v1/adult/jellyfin/requests`: Submit a scene or performer request directly from the Jellyfin UI.
    5. `GET /api/v1/adult/jellyfin/lookup`: Check whether a scene identified by StashID is already available, pending, or snatching in Whisparr.

- **Authentication**:
  - How the plugin authenticates and on whose behalf it asks (server token, or the Jellyfin user via ADR-0026) is decided in the Jellyfin plugin ADR. Whatever it decides, these routes apply the per-user adult gate of the user the request acts for; a caller-supplied user id header is never trusted on its own.

- **Options considered**:
  - *Option A: Dedicated OpenAPI sub-path tailored for Jellyfin client needs* (Recommended).
    - *Trade-offs*: Decouples Jellyfin plugin consumption from internal web UI changes. Fully specified in OpenAPI, enabling client generation in C#.
  - *Option B: Force Jellyfin plugin to use generic internal frontend endpoints*.
    - *Trade-offs*: Reduces endpoint count, but couples the plugin to UI-specific data shapes and pagination semantics.
- **Recommendation**: **Option A**. Explicit `/api/v1/adult/jellyfin/*` OpenAPI contract.

### 7. Testing strategy and verification

In accordance with HISS-15 (3D testing: positive, negative, boundary) and HISS-20 (replayable evidence), the adult module includes comprehensive test suites:

- **Positive test cases**:
  - Multi-instance stash-box sync correctly paginates and extracts favorite performers and studios.
  - Cross-instance entity deduplication successfully merges records sharing identical external link URLs.
  - Submitting a valid scene request dispatches the looked-up scene with the right `StashId` to Whisparr v3 (`POST /api/v3/movie`) and debits the hourly cap.
  - Image set scanner discovers local images, computes SHA-256 checksums, and generates WebP thumbnails within size constraints.
  - Authorized user with `adult_access = true` successfully queries adult routes and receives adult SSE notifications.

- **Negative test cases**:
  - Stash-box API responding with HTTP 401 or GraphQL error returns wrapped `domain.ErrInvalid` without crashing the sync loop.
  - Corrupt or truncated image file in an image set directory fails gracefully without leaking memory or failing the entire gallery import.
  - Attempting to request a scene via Whisparr v3 when Whisparr returns HTTP 400 Bad Request maps to RFC 9457 Problem JSON.
  - Unauthenticated request to adult endpoints returns HTTP 401 Unauthorized.

- **Boundary test cases**:
  - Image file exactly at the 100 MB decode limit succeeds; file at 100 MB + 1 byte is rejected with a validation error.
  - Empty stash-box response (`{"count": 0, "performers": []}`) completes sync without error.
  - Indexer hourly cap at 80 % logs a warning; at 100 % the request dispatch waits and the item stays queued.

- **Planted negative cases (regression verification)**:
  - *Case 1 (Module disabled)*: When `modules.enabled = false` for `adult`, every adult endpoint must return HTTP 404 Problem JSON (`application/problem+json`). Verified by asserting that an enabled module responds with 200/401 while a disabled module produces strict 404.
  - *Case 2 (Parental gating)*: When a user session lacks `adult_access`, querying adult endpoints returns HTTP 404, and search results contain zero adult entities.

## Consequences

### Positive

- **Federated discovery**: Unifies fragmented community stash-box databases into a single searchable interface with combined favourites.
- **Decoupled architecture**: Clean separation of control plane routing, worker snatching, and filesystem image storage adhering to [ADR-0018](0018-product-front-door.md) and [ADR-0025](0025-module-switches.md).
- **Zero download client leakage**: Fully respects [ADR-0006](0006-download-client-backpressure.md) by delegating all torrent/usenet management to Whisparr.
- **Privacy by design**: Total concealment (HTTP 404) for unauthorized users and sanitized metrics/logs protect household privacy.
- **Lightweight storage**: Filesystem-backed image sets prevent PostgreSQL database bloat and memory exhaustion.

### Negative

- **Upstream dependency**: Relies on goenvoy upstream pull request for stash-box GraphQL mutations if write-back is enabled.
- **Filesystem mapping requirements**: Image set support requires configuring shared volume mounts across container deployments.

## Open questions

### 1. `adult-module-display-name`

In accordance with operator decision `napp2-name-direct-adult-20261020`, the module name must plainly and directly state that it governs adult content without euphemisms:

- **Options**:
  - *Option a: "Adult Content"* (Module ID: `adult`, Go package: `api/internal/adult`).
    - *Risk*: Very broad label, but immediately and unambiguously recognized in navigation and settings.
  - *Option b: "Adult Library"* (Module ID: `adultlibrary`, Go package: `api/internal/adultlibrary`).
    - *Risk*: Stresses browsing the own library and undersells discovery and requests.
  - *Option c: "Adult Media"* (Module ID: `adultmedia`, Go package: `api/internal/adultmedia`).
    - *Risk*: Slightly longer compound identifier without offering functional distinction over "Adult Content".
- **Recommendation**: **Option a** ("Adult Content", ID: `adult`).
- **Answered (2026-10-24, controller ledger `snatcharr-adult-module-display-name`)**: Option a. UI label "Adult Content", module id `adult`, Go package `adult` (`api/internal/adult`).

### 2. `adult-image-set-storage-path`

Where image set files and galleries should reside on disk:

- **Options**:
  - *Option a: Co-located inside Whisparr media library directories* (e.g. `<whisparr_root>/<scene_folder>/.imagesets/`).
    - *Risk*: Modifying files inside Whisparr library paths risks triggering Whisparr file rescan warnings, unexpected file moves, or accidental deletions.
  - *Option b: Dedicated external directory path configured in module settings* (e.g. `/media/imagesets/`).
    - *Risk*: Requires administrators to declare and mount an additional host path in Docker and Kubernetes configurations.
  - *Option c: Internal application state volume* (e.g. `/var/lib/snatcharr/imagesets/`).
    - *Risk*: Combines high-volume media assets with lightweight database backups, complicating storage tiering.
- **Recommendation**: **Option b** (Dedicated external directory path configured in module settings, with optional scene co-location linking).
- **Answered (2026-10-24, controller ledger `snatcharr-adult-image-set-storage-path`)**: Option b, narrowed. One configured directory, validated at boot: a missing directory refuses start-up. A scene link is optional. Directories managed by an *arr app stay untouched, and Jellyfin must be able to index the directory (ADR-0028). The cluster path and any new dataset are a later deployment order.

### 3. `adult-goenvoy-upstream-scope`

Extent of Stash-box client capabilities upstreamed to `goenvoy`:

- **Options**:
  - *Option a: Reuse `goenvoy/metadata/adult/stashbox` v2.0.3 for all queries; upstream mutations via pull request*.
    - *Risk*: Lead time for a goenvoy release; the generic `Query` method bridges it.
  - *Option b: Maintain bespoke GraphQL mutations in `api/internal/adult`*.
    - *Risk*: Breaches HISS-19 ("reuse before writing") by splitting stash-box protocol logic across two repositories.
- **Recommendation**: **Option a** (Reuse goenvoy v2.0.3, upstreaming mutation additions).
- **Answered (2026-10-24, controller ledger `snatcharr-adult-goenvoy-upstream-scope`)**: Option a. Typed `favoritePerformer` and `favoriteStudio` wrappers go to goenvoy `metadata/adult/stashbox` by pull request; the generic `Query` method bridges until a tagged goenvoy release, and SnatchArr then pins that tag (no pseudo-version).

## Follow-ups

- [ ] Upstream PR in `github.com/golusoris/goenvoy` for `metadata/adult/stashbox` write mutations (`favoritePerformer`, `favoriteStudio`).
- [ ] Database migration slice: tables `adult_stashbox_instances`, `adult_favorite_performers`, `adult_performer_links`, `adult_image_sets`, `adult_image_items`.
- [ ] Go control plane slice: module descriptor registration in `api/internal/modules` and `ModuleGateMiddleware` integration ([ADR-0025](0025-module-switches.md)).
- [ ] Stash-box federation slice: multi-instance client pooling, leader loop in `leader_loops` fx group with quiescent tick, bounded pagination sync.
- [ ] Whisparr request slice: scene request intake, `StashId` mapping for Whisparr v3 and series mapping for v2, hourly cap debit.
- [ ] Image set engine slice: directory scanner, WebP thumbnail generator with 100 MB decode bound, streaming image server, and a boot test that refuses a missing image-set directory.
- [ ] OpenAPI and Jellyfin integration slice: `/api/v1/adult/*` routes with `x-module: adult` and companion Jellyfin endpoints ([ADR-0018](0018-product-front-door.md) follow-up 4, issue [#567](https://github.com/lusoris/SnatchArr/issues/567)).
- [ ] Frontend SPA slice: adult settings toggle, stash-box instance management, favourites search, image set gallery viewer in Svelte 5.

## References

- `napp3-folded-into-napp2-20261020` (2026-10-20T14:09:54) (paraphrased): Image sets folded into adult module (working name stashbridge, not to be used as final); multi-instance stash-box sync, favourites search, Whisparr scene linking, Jellyfin integration.
- `napp2-name-direct-adult-20261020` (2026-10-20T14:09) (paraphrased): Name must directly and plainly state that it governs adult content without euphemisms. No name is decided; placeholder module id `adult` with 3 descriptive options.
- `napp-shape-20261020` (2026-10-20T14:13:07) (paraphrased): Module inside SnatchArr, off by default, dedicated package, routes, and database tables.
- `snatcharr-product-adult-scope-20261020` (2026-10-20T14:51:45) (paraphrased): Discover from stash-box sources, request into Whisparr, and browse local library including image sets.
- `snatcharr-product-overlap-20261020` (2026-10-20T14:48:58) (paraphrased): Where existing applications (Stash) perform a job, build only what bridges functional gaps.
- `snatcharr-product-adult-visibility-20261020` (2026-10-20T14:51:45) (paraphrased): Adult content visibility governed by Jellyfin user groups, parental ratings, and age restrictions.
- `snatcharr-product-accounts-20261020` (2026-10-20T14:55:08) (paraphrased): Users, roles, and adult access sourced from Jellyfin authentication and group configuration.
- `snatcharr-product-jellyfin-plugin-20261020` (2026-10-20T14:55:08) (paraphrased): Scope of companion Jellyfin plugin: requests, adult discovery, image-set galleries, and metadata badges.
- [ADR-0006: Download client backpressure](0006-download-client-backpressure.md): Download clients observed for backpressure, never driven directly.
- [ADR-0018: Product front door](0018-product-front-door.md): SnatchArr product redefinition and modular architecture.
- [ADR-0021: LLM access module](0021-llm-access-module.md): Built-in MCP server and tool fencing.
- [ADR-0023: Roles from SSO groups](0023-sso-viewer-role.md): Authentik group mapping to admin and viewer roles.
- [ADR-0025: Module switches](0025-module-switches.md): Architecture of optional module switches, `x-module`, and quiescent leader loops.
- [stashapp/stash-box](https://github.com/stashapp/stash-box): Upstream GraphQL schema, release tag `v0.10.3` (commit `5eb3551e19fd494e93be410d8149adc785cffd6a`, read 2026-10-24).
- [Whisparr/Whisparr](https://github.com/Whisparr/Whisparr): Upstream repository, tag `v3.1.0.2116` (commit `94480761ec412cae40ba84e55c70b723851340cf`, read 2026-10-24) and `v2.2.0-release.231` (commit `face3be8956e4c3798a9364a0f2ea95948e34c1c`).
- [golusoris/goenvoy](https://github.com/golusoris/goenvoy): Upstream client repository, release tags `metadata/adult/stashbox/v2.0.3` (commit `b08a9f272e927870f7f214bf9fd80494d1b17d08`), `mediaserver/stash/v2.0.2` and `arr/whisparr/v1.5.1`, read 2026-10-24.
- Issue [#566](https://github.com/lusoris/SnatchArr/issues/566): Adult content module tracking epic.
