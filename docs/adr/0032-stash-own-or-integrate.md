<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0032: Stash: own or integrate — adult library engine evaluation

- Status: accepted (2026-10-26)
- Date: 2026-10-26
- Related: [ADR-0018](0018-product-front-door.md), [ADR-0025](0025-module-switches.md), [ADR-0027](0027-adult-module.md), [ADR-0028](0028-jellyfin-plugin.md)

## Context

SnatchArr operates as the front door of the media stack ([ADR-0018](0018-product-front-door.md)). Under this unified scope, SnatchArr governs search cycles, manages download client queues, monitors release availability, manages adult media and image sets, and provides structured Model Context Protocol (MCP) tooling ([ADR-0021](0021-llm-access-module.md)).

A foundational architectural precedent established on 2026-10-20 (`snatcharr-product-overlap-20261020`, paraphrased) dictates that:
> Where an existing application already performs a domain job (such as Seerr for mainstream requests, Stash for adult media organization, or Cleanuparr for queue clearing), SnatchArr connects to that deployed application to link functionality and builds its own internal module only when it resolves an authentic functional gap.

[ADR-0027](0027-adult-module.md) defined the baseline scope for the adult content module (`adult`), which incorporates federated multi-instance Stash-box discovery, favourites synchronization, Whisparr request dispatching, and local image-set gallery management. However, questions remain regarding the boundary between SnatchArr and [Stash](https://github.com/stashapp/stash): should SnatchArr preserve Stash as the primary adult video library engine, build native replacements for its functions inside SnatchArr, or operate in a hybrid configuration?

This ADR evaluates Stash's architectural capabilities, upstream client packages, licensing constraints, and known operational gaps to determine the exact boundary for the SnatchArr adult module.

### Current implementation baseline

The SnatchArr repository currently contains:

1. **Adult module control plane (`api/internal/adult/`)**:
   - `module.go`: Uber fx registration under `snatcharr.adult`, binding the leader sync loop into fx group `leader_loops` (`AGENTS.md`, contract on periodic loops).
   - `service.go`: Core service managing multi-instance client pooling, encrypted credentials, and federated operations.
   - `pool.go`: Multi-instance client builder backed by `crypto.Encryptor` (AES-GCM).
   - `sync.go`: Leader loop synchronizing performer and studio favourites across federated Stash-box instances with bounded pagination (`HISS-02`).
   - `merge.go`: Deduplication engine merging identical entities across independent Stash-box UUID namespaces via canonical links, external URLs, and normalized names.
   - `search.go`: Federated search fanning out across configured Stash-box endpoints (`HISS-06`, max concurrency 4).
2. **Database schema**:
   - Migration `api/internal/store/migrations/postgres/000036_adult_module.up.sql` creates tables `adult_stashbox_instances`, `adult_favorite_performers`, `adult_performer_links`, `adult_favorite_studios`, `adult_studio_links`, `adult_image_sets`, and `adult_image_items`.
   - Migration `000038_adult_favorite_urls.up.sql` indexes external site URLs for cross-instance deduplication.
3. **Module switches**:
   - Registered in `api/internal/modules` as module `adult`, disabled by default, strictly gated by `ModuleGateMiddleware` returning RFC 9457 HTTP 404 Problem JSON when deactivated ([ADR-0025](0025-module-switches.md)).
4. **Whisparr integration**:
   - `api/internal/arrclient/probe.go:238-251` and `worker/crates/arr-client/src/lib.rs:412-415` support Whisparr v2 and Whisparr v3 (Eros), mapping adult items by `StashId`.

### Upstream ecosystem baseline

Investigation of the live upstream ecosystem verifies the following state as of 2026-10-26:

1. **Stash upstream repository ([stashapp/stash](https://github.com/stashapp/stash))**:
   - Latest release: `v0.31.1` (released 2026-04-13; previous minor `v0.31.0` released 2026-03-30).
   - Licence: GNU Affero General Public License v3.0 (`AGPL-3.0-only`).
   - Official container: `docker.io/stashapp/stash:v0.31.1`.
   - Transport: GraphQL HTTP endpoint at `/graphql` authenticated via header `Apikey: <secret>` or session cookies.
   - Ecosystem: Scrapers hosted at `github.com/stashapp/CommunityScrapers` (YAML configurations executed via embedded Python/CDP runner); plugin system executing local scripts triggered by lifecycle hooks (e.g. `Scene.Create.Post`, `Scan.Post`).
2. **Go upstream client repository ([github.com/golusoris/goenvoy](https://github.com/golusoris/goenvoy))**:
   - Main branch: commit `ac12960bb042cf3b582fbe2421150fe85b321593` (read 2026-10-26).
   - Licence: MIT License.
   - Package `github.com/golusoris/goenvoy/mediaserver/stash/v2`: Pinned release tag `mediaserver/stash/v2.0.3` (commit `17183e338c983b01d497a42c0a76b82c7a3b1e53`). Exposes `Client` with `New(endpoint, apiKey, opts...)`, pre-built GraphQL fragments, and typed query methods: `FindScene`, `FindScenes`, `FindPerformer`, `FindPerformers`, `FindStudio`, `FindStudios`, `FindTag`, `FindTags`, `FindGallery`, `FindGalleries`, `FindImage`, `FindImages`, `FindGroup`, `FindGroups`, `FindSceneMarkers`, `Stats`, `Version`, and `SystemStatus`, alongside generic `Query(ctx, query, variables, dst)`.
   - Package `github.com/golusoris/goenvoy/metadata/adult/stashbox/v2`: Pinned release tag `metadata/adult/stashbox/v2.0.4` (commit `392968f04f0287825d2010d5f38f3a1d4bbe137d`). Exposes typed Stash-box client query methods and generic `Query`.

### Licensing implications: AGPL-3.0 vs EUPL-1.2

A critical consideration in evaluating whether to copy or absorb Stash components into SnatchArr is open-source licence compatibility:

- **SnatchArr licensing**: SnatchArr is licensed under the European Union Public Licence v1.2 (`EUPL-1.2`).
- **Stash licensing**: Stash is licensed under the GNU Affero General Public License v3.0 (`AGPL-3.0-only`).
- **Code reuse (in-tree inclusion)**: copying or adapting Stash code into SnatchArr would bring AGPL-3.0 terms into the EUPL-1.2 tree. This ADR proposes that SnatchArr copies no Stash code; Stash concepts serve only as a high-level reference.
- **API integration (separate process)**: Stash exposes its functions over a GraphQL HTTP API. SnatchArr talks to a separately deployed Stash through the MIT-licensed client `github.com/golusoris/goenvoy/mediaserver/stash/v2`; no Stash code enters the SnatchArr tree, and Stash runs in its own container.

### Image handling gaps in Stash

While Stash excels at video scene management, its image and gallery handling exhibits structural limitations that directly affect media stacks:

1. **No automatic scene association**: Stash maintains galleries and scenes as separate first-class entities. Upon initial library scanning, Stash does not automatically associate zipped or folder galleries with corresponding video scenes ([stashapp/stash#3344](https://github.com/stashapp/stash/issues/3344)). Users must manually link galleries to scenes.
2. **ZIP gallery failure modes**: When image sets are archived in ZIP or CBZ format, attempting to delete an individual image inside a ZIP gallery fails silently without feedback ([stashapp/stash#7106](https://github.com/stashapp/stash/issues/7106)). Furthermore, path calculation errors in `filepath.Rel` trigger failures during routine clean tasks on zipped images ([stashapp/stash#4542](https://github.com/stashapp/stash/issues/4542)), and converting galleries between ZIP-based and folder-based storage leads to duplicate entity records ([stashapp/stash#3921](https://github.com/stashapp/stash/issues/3921)).
3. **Memory allocation spikes**: Generating image perceptual hashes (`phash`) during background scans causes extreme heap allocations and RAM spikes ([stashapp/stash#7129](https://github.com/stashapp/stash/issues/7129)), degrading container stability on resource-constrained hosts. Large image libraries also suffer severe browsing bottlenecks and memory leakage ([stashapp/stash#7222](https://github.com/stashapp/stash/issues/7222), [stashapp/stash#2195](https://github.com/stashapp/stash/issues/2195)).
4. **No external metadata or companion streaming**: Stash lacks an export mechanism to store gallery metadata outside its internal database (such as portable ComicInfo XML; [stashapp/stash#4735](https://github.com/stashapp/stash/issues/4735)) and provides no streaming API optimized for external media server companion clients such as Jellyfin.
5. **No stash-box gallery matching**: galleries carry no hashes that stash-box could match on ([stashapp/stash#3496](https://github.com/stashapp/stash/issues/3496), open feature request); stash-box matching covers performers, studios, scenes and tags.

## Stash functional comparison

In accordance with the baseline precedent (`snatcharr-product-overlap-20261020`), every row evaluated for a `build` verdict must prove an authentic functional gap that existing deployed software fails to address.

| Function | Stash does it (version, how) | SnatchArr needs it? (why, which ADR) | Effort to build (S/M/L, Go packages) | Upstream reuse (goenvoy / Go libs, licence) | Risk (licence AGPL contact, maintenance, duplication) | Verdict |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Scan / library import** | v0.31.1; filesystem walker, embedded FFprobe metadata extraction, SQLite/PostgreSQL storage, `.stashignore` rules (added v0.31.0). | **For video**: No. Whisparr owns adult video storage and imports; Jellyfin indexes files for playback ([ADR-0018](0018-product-front-door.md):69). **For image sets**: Yes. SnatchArr manages photo galleries linked to Whisparr scenes ([ADR-0027](0027-adult-module.md):184-248). | **Video**: L (`os`, `database/sql`, Cgo FFmpeg). **Image sets**: S (`os.WalkDir`, `path/filepath` in `api/internal/adult`). | Whisparr API via `goenvoy/arr/whisparr/v1` (MIT); standard Go stdlib for image sets. | **High** if duplicating video scanner (wastes I/O, conflicts with Whisparr). **Low** for lightweight image-set scanner. | **Link** (video files managed by Whisparr/Jellyfin/Stash). **Build** (image sets: gap is scene-adjacent photo gallery tracking missing from *arr). |
| **Fingerprints (phash, oshash, md5)** | v0.31.1; generates md5, OpenSubtitles hash (`oshash`), video frame perceptual hash (`phash`), and image phash (added v0.31.0) for StashDB matching. | **No**. SnatchArr request pipeline matches scenes into Whisparr v3 using canonical `StashId` ([ADR-0027](0027-adult-module.md):160). Image sets use cryptographic SHA-256 for dedup and thumbnail caching. | M (pure Go for `oshash`/`md5`; Cgo or FFmpeg exec for video frame extraction and DCT phash). | `github.com/corona10/go-image-hash` (MIT); pure Go oshash libraries (MIT). | **High**. Video phash requires Cgo or external FFmpeg binary, complicating multi-arch distroless OCI builds. Copying Stash algorithms risks AGPL-3.0 contact. | **Link** (defer all video fingerprinting to Stash or Whisparr). |
| **Stash-box matching (Tagger)** | v0.31.1; interactive web UI "Tagger" querying Stash-box via fingerprints or search terms, showing field-level diffs, and scraping metadata. | **For file tagging UI**: No. SnatchArr is a discovery and request front door, not an interactive media tagging workbench. **For cross-instance discovery**: Yes ([ADR-0027](0027-adult-module.md):54-151). | **Tagger UI**: L (complex frontend diffing workbench). **Federated discovery**: M (already built in `api/internal/adult`). | `goenvoy/metadata/adult/stashbox/v2` (MIT; commit `392968f04f0287825d2010d5f38f3a1d4bbe137d`). | **High** if re-implementing Tagger UI. **Low** for federated discovery. | **Integrate** (query Stash library via GraphQL if needed). **Build** (federated stash-box search: gap is multi-instance sync missing in Stash). |
| **Scrapers (community repo)** | v0.31.1; executes YAML-defined web scrapers from `stashapp/CommunityScrapers` via Python or Chrome DevTools Protocol (CDP). | **No**. SnatchArr requests query clean GraphQL APIs (Stash-box). Direct website HTML scraping is brittle, anti-bot prone, and unnecessary for requests. | L (Python sandbox runner, headless Chromium management, HTML parser). | `stashapp/CommunityScrapers` (AGPL-3.0; not embedded in the EUPL tree). | **High**. Site scrapers need constant maintenance against bot protection and markup changes; embedding them would bring AGPL-3.0 code into the tree. | **Link** (leave website scraping to Stash / CommunityScrapers; SnatchArr communicates via APIs). |
| **Galleries / images** | v0.31.1; models `Gallery` and `Image` in SQLite/Postgres. Supports `.zip`/`.cbz` and folders. Suffers from memory leaks (#7129, #2195), zip deletion bugs (#7106), and lacks Whisparr scene linking (#3344). | **Yes**. Scene-linked photo galleries are a declared product requirement ([ADR-0018](0018-product-front-door.md):68, [ADR-0027](0027-adult-module.md):184-248). Streams to companion Jellyfin plugin ([ADR-0028](0028-jellyfin-plugin.md)). | M (`api/internal/adult`, `archive/zip`, `golang.org/x/image/webp`, `http.ServeContent`). | Standard library; `golang.org/x/image` (BSD-3-Clause). Stash gallery concepts used only as high-level architectural reference. | **Low**. Filesystem-backed storage avoids database bloat; bounded decode memory (100 MB ceiling) prevents Stash's OOM issues. | **Build** (gap: Stash fails to link galleries to Whisparr scenes, suffers ZIP bugs, and cannot stream to Jellyfin). |
| **Tagging / performers / studios** | v0.31.1; full relational CRUD for Performers, Studios, Tags, Groups (series/movies), and Scene Markers with custom fields and aliases. | **Partially**. SnatchArr needs favourites tracking across federated Stash-box instances to power Whisparr requests ([ADR-0027](0027-adult-module.md):98-151). It does not need marker timestamping or tag taxonomy editing. | M (favourites tables and deduplication; built in `api/internal/adult`). L (if building full taxonomy editor). | `goenvoy/metadata/adult/stashbox/v2` (MIT) and `goenvoy/mediaserver/stash/v2` (MIT). | **Medium**. High schema maintenance if duplicating full entity relationship models. Zero risk using API clients. | **Build** (favourites sync: gap is cross-box federation missing in Stash). **Integrate** (read full library tags and performers via Stash GraphQL). |
| **Player / streaming / transcode** | v0.31.1; embedded video player, sprite preview generator, on-the-fly FFmpeg transcoding, HLS streaming, and interactive device synchronization. | **No**. SnatchArr is a control plane and front door, not a media streaming player ([ADR-0018](0018-product-front-door.md):24). Video playback is handled by Jellyfin (via companion plugin) or Stash directly. | L (FFmpeg hardware transcode pipeline (VA-API/NVENC), HLS segmenter, WebSocket telemetry). | Jellyfin (GPL-2.0) and Stash (AGPL-3.0) already provide complete playback infrastructure. | **High**. Re-implementing video streaming invites CPU/GPU resource contention with SnatchArr's snatch cycle scheduler, and explodes container size. | **Link** (leave all video playback, transcoding, and device sync to Jellyfin and Stash). |
| **Plugins** | v0.31.1; YAML-defined hooks (`Scene.Create.Post`, `Scan.Post`) executing Python/Node.js scripts or binaries with JSON GraphQL mutation payloads. | **No**. SnatchArr utilizes native Go module descriptors ([ADR-0025](0025-module-switches.md)), Model Context Protocol (MCP) tool exposure ([ADR-0021](0021-llm-access-module.md)), and Uber fx. | L (process supervisor, external script sandbox, lifecycle hook dispatcher). | Native SnatchArr architecture (`api/internal/modules`). | **High**. Running arbitrary user scripts inside the container expands attack surface and violates distroless container immutability. | **Link** (users run Stash plugins within Stash; SnatchArr extends via MCP and native Go modules). |
| **Multi-user / auth** | v0.31.1; basic single-user or multi-user mode with simple roles (read-only/full access) and static API keys. No native OIDC/OAuth2/SSO integration. | **No for Stash auth**. SnatchArr already provides central auth architecture ([ADR-0023](0023-sso-viewer-role.md), [ADR-0026](0026-accounts-and-roles.md)): OIDC/Authentik SSO, Jellyfin linked accounts, roles (`admin`, `requester`, `viewer`), and parental controls. | Already implemented / designed in `api/internal/auth`. | `api/internal/auth`, `golusoris/core/crypto` (EUPL-1.2 / MIT). | **Low**. Stash's native auth is insufficient for multi-tenant media stacks. SnatchArr connects to Stash using a single administrator API key. | **Build** (central front-door auth: gap is multi-role SSO and parental gating across the stack). |
| **DLNA** | v0.31.1; built-in mini DLNA media server broadcasting scene files to UPnP/DLNA local clients. | **No**. Mainstream and adult TV playback across the home network is governed by Jellyfin. | M (UPnP discovery, SSDP multicasting, XML device descriptors, chunked HTTP range streaming). | Jellyfin; `github.com/koron/go-ssdp` (MIT). | **Medium**. High protocol bloat for an obsolete legacy standard. Irrelevant to modern browser, mobile, and casting workflows. | **Link** (leave DLNA to Jellyfin or Stash). |

## Options considered

### Option (a): Keep Stash, integrate via GraphQL (status quo ADR-0027)

SnatchArr acts strictly as a lightweight front door, querying an external Stash instance via `github.com/golusoris/goenvoy/mediaserver/stash/v2` for all adult library data. SnatchArr implements no local storage for image sets, relying on Stash to manage both videos and galleries.

- **Trade-offs**:
  - *Positive*: Zero image-set or gallery code in SnatchArr; minimal database schema.
  - *Negative*: Directly exposes users to Stash's severe gallery limitations (silent zip deletion failures [#7106](https://github.com/stashapp/stash/issues/7106), zip cleaning crashes [#4542](https://github.com/stashapp/stash/issues/4542), missing scene-to-gallery associations [#3344](https://github.com/stashapp/stash/issues/3344), and memory spikes [#7129](https://github.com/stashapp/stash/issues/7129)). Requires running Stash even for users who only want scene requests and photo gallery browsing. Fails to provide companion streaming to Jellyfin.
  - *Risk*: High user friction; inability to fix upstream Stash gallery architecture.

### Option (b): Hybrid: Stash for video library + SnatchArr for image sets and snatching (Chosen)

Stash remains the recommended engine for adult video library management (organizing local video files, scene marker timestamping, interactive scrubbing, and community website scraping). SnatchArr builds native modules **only where real functional gaps exist**:

1. Multi-instance stash-box favourites synchronization and federated search (gap: Stash tags its own library against configured endpoints but keeps no cross-instance favourites or link deduplication).
2. Whisparr request intake and snatch scheduling with hourly cap protection (gap: Stash does not integrate with Whisparr search or download queues).
3. Native filesystem-backed image sets and photo galleries directly linked to Whisparr scenes, with high-performance WebP thumbnail caching and streaming to the Jellyfin companion plugin (gap: Stash treats galleries as secondary, fails to link them to Whisparr scenes, and suffers memory/zip bugs).
4. Centralized front-door authentication, SSO mapping, and parental capability concealment (gap: Stash lacks granular role-based access control and parental gating).

Where Stash is deployed, SnatchArr connects to it via GraphQL (`github.com/golusoris/goenvoy/mediaserver/stash/v2`) as an optional companion instance to query scene availability and prevent duplicate requests. Where Stash is absent, SnatchArr operates seamlessly with Whisparr and Jellyfin alone.

- **Trade-offs**:
  - *Positive*: Fully adheres to the baseline precedent (`snatcharr-product-overlap-20261020`) and HISS-19 ("reuse before writing"). Reuses Stash's mature video organization and player ecosystem without duplicating video transcoding or website scrapers. Addresses the cited Stash gallery issues with a bounded-memory Go implementation. Copies no Stash code, so the tree stays EUPL-1.2. Allows deployments with or without Stash.
  - *Negative*: Introduces two potential gallery locations if an adopter configures both Stash galleries and SnatchArr image sets on the same disk storage.
  - *Risk*: Low. Clear division of responsibility: video library = Stash/Jellyfin; search, requests, federation, and image sets = SnatchArr.

### Option (c): Replace Stash entirely with native SnatchArr components

SnatchArr builds a complete adult media server: native filesystem video scanner, embedded FFprobe/FFmpeg video transcoding, video perceptual hashing (`phash`), interactive tagging and marker editor, and web scraper runner.

- **Trade-offs**:
  - *Positive*: Single monolithic binary for all adult media management.
  - *Negative*: Contradicts the baseline precedent (`snatcharr-product-overlap-20261020`), ADR-0018 and HISS-19. Needs a full media-server code base, FFmpeg dependencies that complicate distroless container builds, overlap with Whisparr's library tracking, and ongoing scraper maintenance. Recreating Stash's tagger and player adds no capability Stash lacks.
  - *Risk*: Large scope and permanent maintenance load for duplicated functions.

## Decision

**Option (b): Hybrid model**, accepted by the maintainer on 2026-10-26 (ledger `snatcharr-stash-own-or-integrate`).

SnatchArr preserves Stash as an external, optional video library engine, integrating via its GraphQL API (`github.com/golusoris/goenvoy/mediaserver/stash/v2`). SnatchArr builds native components exclusively to bridge proven functional gaps:

1. Federated Stash-box discovery and multi-instance favourites synchronization (`api/internal/adult`).
2. Automated scene request pipeline into Whisparr v2 and v3 respecting hourly rate limits ([ADR-0006](0006-download-client-backpressure.md), [ADR-0027](0027-adult-module.md)).
3. Dedicated, lightweight filesystem-backed image sets and photo galleries linked adjacent to Whisparr scenes, with bounded-memory thumbnail generation (100 MB decode limit) and direct streaming to the Jellyfin companion plugin ([ADR-0028](0028-jellyfin-plugin.md)), resolving Stash's chronic gallery bugs.
4. Unified multi-tenant access control with strict parental concealment (HTTP 404) and role-gated SSE channels ([ADR-0023](0023-sso-viewer-role.md), [ADR-0025](0025-module-switches.md), [ADR-0026](0026-accounts-and-roles.md)).

## Consequences

### Positive

- **Architectural focus**: Keeps SnatchArr focused on its core identity as the front door of the media stack (search, requests, queue governance, federation), avoiding video player and transcode bloat.
- **Licence clarity**: no Stash code enters the tree; SnatchArr stays EUPL-1.2 and talks to Stash through the MIT-licensed `goenvoy` client.
- **Image sets**: SnatchArr's image-set engine avoids the cited Stash gallery issues by design (bounded decode memory, filesystem-backed sets) and links sets to Whisparr scenes and the Jellyfin plugin.
- **Deployment flexibility**: Stash is not a mandatory dependency. Users who only need Whisparr request dispatching and image sets can run SnatchArr without Stash. Users with existing Stash libraries can link them seamlessly via GraphQL.

### Negative

- **Dual-engine awareness**: Users running both Stash and SnatchArr must configure volume mounts to ensure Stash does not perform redundant, memory-heavy image phash scans on SnatchArr's image set directories.

## Resolved questions

The maintainer answered all three on 2026-10-26; each answer is recorded in the decision ledger under the question id.

### 1. `stash-integration-mode`

How should Stash be represented within SnatchArr's configuration and instance registry?

- **Options**:
  - *Option a: Optional companion media server instance* (Registered as an instance under `instances` with `kind: stash`, queried via `goenvoy/mediaserver/stash/v2` for scene inventory matching).
    - *Risk*: Requires adding `kind: stash` to instance validation, but cleanly fits the existing instance management model.
  - *Option b: Adult module internal setting* (Configured exclusively within the adult module settings page as an optional endpoint and API key).
    - *Risk*: Isolates Stash from general instance health probes and metrics, creating a special-case connection pattern.
  - *Option c: Mandatory backend dependency* (SnatchArr refuses adult module activation unless a healthy Stash instance is connected).
    - *Risk*: Forces all adult module users to run Stash, preventing standalone Whisparr + Jellyfin deployments.
- **Decision**: **Option a**. `stash` is appended to the instance kind enum (append-only) and uses the same health probes as other instances.

### 2. `gallery-storage-coexistence`

How should SnatchArr prevent Stash from executing duplicate, memory-intensive scanning on SnatchArr's image-set directories when both applications access the same storage volume?

- **Options**:
  - *Option a: Automatic `.stashignore` placement in image-set root directories* (SnatchArr automatically drops a `.stashignore` file into its configured `image_set_dir` upon initialization).
    - *Risk*: Depends on Stash v0.31.0+ respecting `.stashignore`. Effectively shields Stash from high-RAM image phash processing without requiring manual user exclusion rules.
  - *Option b: Disjoint filesystem paths mandated by documentation* (Rely entirely on operator documentation instructing administrators never to mount the same path in both containers).
    - *Risk*: Prone to operator misconfiguration, leading to duplicate indexing and memory exhaustion in Stash.
  - *Option c: Proxy image sets into Stash via GraphQL* (Push SnatchArr image sets into Stash's database via GraphQL mutations).
    - *Risk*: Reintroduces Stash's gallery bugs, zip deletion failures, and memory bloat into SnatchArr's workflow.
- **Decision**: **Option a**. SnatchArr writes `.stashignore` at the `image_set_dir` root. The docs and the Stash instance probe state the Stash v0.31.0 minimum. Nothing is written when the directory is read-only.

### 3. `stash-auth-credential-storage`

How should Stash GraphQL API credentials be secured at rest within SnatchArr?

- **Options**:
  - *Option a: Encrypted at rest via `crypto.Encryptor` (AES-GCM)* (Stored in the database with write-only OpenAPI models, matching *arr, Seerr, and Stash-box credentials).
    - *Risk*: Requires the existing encryption key configuration; matches the repository rule on encrypted credentials (`AGENTS.md`, Don'ts).
  - *Option b: Plaintext database column with read masking*.
    - *Risk*: Violates the repository rule on encrypted credentials (`AGENTS.md`, Don'ts).
  - *Option c: Static environment variable (`SNATCHARR_STASH_API_KEY`)*.
    - *Risk*: Inflexible in multi-instance deployments; bypasses UI configuration.
- **Decision**: **Option a**. The key is AES-GCM encrypted at rest and `writeOnly` in the API.

## Follow-ups

| Item | Issue | Milestone |
| :--- | :--- | :--- |
| Add `stash` to the instance kind enum in `api/openapi/openapi.yaml` (today `sonarr, radarr, lidarr, readarr, whisparr_v2, whisparr_v3`) as an optional companion target. | [#701](https://github.com/lusoris/SnatchArr/issues/701) | **0.10** |
| Implement Stash inventory probe and scene presence check using `github.com/golusoris/goenvoy/mediaserver/stash/v2`. | [#701](https://github.com/lusoris/SnatchArr/issues/701) | **0.10** |
| Implement `.stashignore` file writer during image-set directory initialization in `api/internal/adult`; skip on a read-only directory. | [#701](https://github.com/lusoris/SnatchArr/issues/701) | **0.10** |
| Document the Stash v0.31.0 minimum and report it from the Stash instance probe. | [#701](https://github.com/lusoris/SnatchArr/issues/701) | **0.10** |
| Complete the image-set engine slice with WebP thumbnail caching and streaming endpoints for the Jellyfin plugin ([ADR-0027](0027-adult-module.md):390, [ADR-0028](0028-jellyfin-plugin.md)). | [#702](https://github.com/lusoris/SnatchArr/issues/702) | **0.10** |
| Add OpenAPI specification entries for Stash connection settings under `x-module: adult`. | [#701](https://github.com/lusoris/SnatchArr/issues/701) | **0.10** |
## References

- `snatcharr-product-overlap-20261020` (2026-10-20T14:48:58) (paraphrased): Where existing applications (Seerr, Stash, Cleanuparr) perform a job, build only what bridges functional gaps.
- `snatcharr-product-job-20261020` (2026-10-20T14:48:58) (paraphrased): Front door of the media stack; specialized external applications remain backend engines.
- `napp3-folded-into-napp2-20261020` (2026-10-20T14:09:54) (paraphrased): Image sets folded into adult module; multi-instance stash-box sync, favourites search, Whisparr scene linking, Jellyfin integration.
- [stashapp/stash](https://github.com/stashapp/stash): Upstream repository, release `v0.31.1` (released 2026-04-13) and `v0.31.0` (released 2026-03-30), read 2026-10-26. Licence: GNU AGPL-3.0 (`AGPL-3.0-only`).
- [stashapp/CommunityScrapers](https://github.com/stashapp/CommunityScrapers): Community scrapers repository, licence AGPL-3.0, read 2026-10-26.
- [golusoris/goenvoy](https://github.com/golusoris/goenvoy): Upstream Go client repository, commit `ac12960bb042cf3b582fbe2421150fe85b321593` (read 2026-10-26), release tags `mediaserver/stash/v2.0.3` (commit `17183e338c983b01d497a42c0a76b82c7a3b1e53`) and `metadata/adult/stashbox/v2.0.4` (commit `392968f04f0287825d2010d5f38f3a1d4bbe137d`). Licence: MIT.
- Stash issue [#7106](https://github.com/stashapp/stash/issues/7106): Deleting an image in a zip gallery silently fails (read 2026-10-26).
- Stash issue [#4542](https://github.com/stashapp/stash/issues/4542): Clean task fails on certain zipped images (read 2026-10-26).
- Stash issue [#4735](https://github.com/stashapp/stash/issues/4735): ZIP gallery metadata file / external companion export (read 2026-10-26).
- Stash issue [#3921](https://github.com/stashapp/stash/issues/3921): Gallery update when galleries change from ZIP based to folder based (read 2026-10-26).
- Stash issue [#3748](https://github.com/stashapp/stash/issues/3748): Image scraping limitations (read 2026-10-26).
- Stash issue [#3344](https://github.com/stashapp/stash/issues/3344): Stash does not automatically associate zip galleries with corresponding scene(s) (read 2026-10-26).
- Stash issue [#7129](https://github.com/stashapp/stash/issues/7129): Image phash generation memory spikes (read 2026-10-26).
- Stash issue [#7222](https://github.com/stashapp/stash/issues/7222) / [#2195](https://github.com/stashapp/stash/issues/2195): Large image libraries performance bottlenecks and memory leaks (read 2026-10-26).
- Stash issue [#3496](https://github.com/stashapp/stash/issues/3496): adding hashes to galleries for potential stash-box integration (read 2026-10-26).
- Stash release [v0.31.0](https://github.com/stashapp/stash/releases/tag/v0.31.0): image phash generation and `.stashignore` support (read 2026-10-26).
- [ADR-0006: Download client backpressure](0006-download-client-backpressure.md): Download clients observed for backpressure, never driven directly.
- [ADR-0018: Product front door of the media stack](0018-product-front-door.md): Front door scope and modular architecture.
- [ADR-0021: LLM access module](0021-llm-access-module.md): Built-in MCP server and tool gating.
- [ADR-0023: Roles from single sign-on groups](0023-sso-viewer-role.md): Authentik SSO group mapping.
- [ADR-0025: Architecture and mechanism of optional module switches](0025-module-switches.md): Optional module switches and `ModuleGateMiddleware`.
- [ADR-0026: Accounts, roles and Jellyfin sign-in](0026-accounts-and-roles.md): Multi-role user accounts and media server sign-in.
- [ADR-0027: Adult module: stash-box sources, Whisparr requests and image sets](0027-adult-module.md): Adult module baseline architecture.
- [ADR-0028: Jellyfin companion plugin and its contract](0028-jellyfin-plugin.md): Jellyfin companion integration contract.
