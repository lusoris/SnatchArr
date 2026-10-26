<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0031: Creator content catalogued in FansDB inside the adult module

- Status: proposed
- Date: 2026-10-26
- Related: [ADR-0006](0006-download-client-backpressure.md), [ADR-0018](0018-product-front-door.md), [ADR-0022](0022-instance-priority.md), [ADR-0025](0025-module-switches.md), [ADR-0027](0027-adult-module.md)

## Context

SnatchArr operates as the unified front door of the media stack ([ADR-0018](0018-product-front-door.md)). Under this modular architecture, [ADR-0027](0027-adult-module.md) established the foundation for the optional adult content module (`adult`), covering federated metadata discovery across community stash-box instances, performer and studio favourite synchronization, request dispatch into Whisparr instances, and local image-set gallery management.

A substantial and expanding portion of contemporary independent media consists of creator content: self-published, episodic releases, short video clips, and high-resolution photo sets produced directly by independent performers and studios. Traditional metadata providers and media organizers historically prioritized studio-produced feature films and series, leaving creator content poorly structured, fragmented, or uncatalogued.

### FansDB ecosystem

FansDB (`https://fansdb.cc/`, read 2026-10-26) addresses this metadata gap as a dedicated, crowdsourced community metadata database focused specifically on creator content. It runs on the upstream `stash-box` software architecture (`stashapp/stash-box`, release `v0.10.3`, MIT, read 2026-10-26), exposing a standard GraphQL API (`/graphql`). FansDB catalogues performers (creators), studios (creator channels, labels, or distribution outlets), scenes (individual video clips or releases), and tags.

Unlike open public registries, FansDB operates on an invite-only membership model (`https://fansdb.cc/register`, read 2026-10-26). Authenticated users receive a personal API key to query the GraphQL endpoint.

### Scope and architectural constraints

Integrating FansDB into SnatchArr must adhere strictly to established architectural boundaries:

1. **Search and hand-off only (hard constraint)**:
   - In accordance with [ADR-0006](0006-download-client-backpressure.md) and `AGENTS.md`, SnatchArr **never downloads content directly**.
   - SnatchArr performs metadata matching, checks local processed memory, evaluates per-indexer hourly search stamina, and hands the search off to existing indexer or *arr pathways.
   - External download clients outside SnatchArr fetch the actual release files. SnatchArr neither operates protocol transfer clients nor manages storage staging pipelines.
2. **Credential security**:
   - Access keys for FansDB represent privileged account credentials.
   - Keys must be stored in SnatchArr's instance secret store (`adult_stashbox_instances`), encrypted at rest with AES-GCM via `crypto.Encryptor` (`api/internal/app/crypto.go:26-39`).
   - All API models treat credentials as write-only, returning masked strings on read operations and excluding them from application logs and telemetry.
3. **Strict terminology and privacy fence**:
   - Public documentation, user interfaces, logs, metrics, and API models must never reference commercial host platforms, specific third-party portal domains, real-world performers, external retrieval utilities, or digital access restrictions.
   - All public facing text refers exclusively to "creator content" or "FansDB-catalogued content".
4. **Existing pipeline reuse (HISS-19)**:
   - SnatchArr already implements stash-box pooling and synchronization in `api/internal/adult` using `github.com/golusoris/goenvoy/metadata/adult/stashbox/v2` (v2.0.4, MIT, read 2026-10-26).
   - Any FansDB integration must extend the existing multi-instance federation architecture rather than creating bespoke communication paths.

### Problem statement

Integrating creator content catalogued in FansDB presents four technical challenges:

1. **Metadata federation**: FansDB uses independent UUID namespaces for performers, studios, and scenes. Merging FansDB records into SnatchArr's federated favourite cache without corrupting studio-oriented stash-box instances requires deterministic mapping.
2. **Pre-search release matching**: File fingerprints (`phash`, `oshash`, `md5`) are widely used by media taggers to identify files *after* they are downloaded. However, prior to snatching, SnatchArr has no local media file bytes—only indexer release titles and metadata feeds. Pre-search matching must rely on parsed textual metadata (studio, date, title, and performer).
3. **Search path routing and hourly cap accounting**: SnatchArr must route creator content searches through supported indexers (configured in Whisparr or Prowlarr) while strictly enforcing the contract of 1 search debit per item against the per-indexer hourly cap (`AGENTS.md:126`).
4. **Image-set handling**: Creator content frequently pairs video scenes with multi-image photo sets. Existing *arr and media server tools handle image posts poorly, often collapsing order, corrupting duplicate galleries, or ignoring photo sets altogether. SnatchArr must govern image sets cleanly under the [ADR-0027](0027-adult-module.md) specification.

## Decision

SnatchArr incorporates FansDB as an additional supported stash-box source within the `adult` content module, adhering to the technical specifications below.

### 1. FansDB entity model and protocol integration

FansDB operates as a standard instance of the upstream `stashapp/stash-box` GraphQL service (`v0.10.3`):

- **Transport and authentication**:
  - Transport: HTTP POST to the `/graphql` endpoint with JSON payloads.
  - Authentication: Header `Apikey: <secret>`.
  - Roles: Read operations require the `READ` role, satisfied by standard user API keys.
- **Entity representation in FansDB**:
  - `Performer`: Represents individual content creators. Fields include canonical `name`, `disambiguation`, `aliases`, `urls` (pointing to verified creator profiles and external indices), and `is_favorite`.
  - `Studio`: Represents the creator's brand, publishing channel, or studio network. Fields include `name`, `parent`, `child_studios`, and `urls`.
  - `Scene`: Represents an individual video release or clip. Fields include `title`, `details`, `release_date` (ISO-8601 `YYYY-MM-DD`), `duration`, `studio`, `performers`, `tags`, and `fingerprints`.
  - `Image`: Hosted avatar, banner, or scene preview images. As verified in `stashapp/stash-box` (`v0.10.3`, Issue #1179), upstream stash-box contains **no native `Gallery` entity**. Images in FansDB exist strictly as single-image entity attachments (cover art and profile avatars, downscaled to <= 1280px JPEG), not as multi-image photo sets.
- **Fingerprints vs pre-search reality**:
  - FansDB records scene fingerprints: `PHASH` (perceptual video hash across frames), `OSHASH` (OpenSubtitles 64 KB head/tail checksum), and legacy `MD5`.
  - Fingerprints are strictly post-download verification artifacts. SnatchArr cannot compute or query fingerprints prior to search because no media file exists locally. Pre-search release matching must operate on textual release metadata.
- **Client library reuse**:
  - In compliance with HISS-19 ("reuse before writing"), SnatchArr communicates with FansDB exclusively through `github.com/golusoris/goenvoy/metadata/adult/stashbox/v2` (`v2.0.4`, MIT, tag commit `392968f04f02`, read 2026-10-26).
  - Package `api/internal/adult` reuses `stashbox.New(endpoint, apiKey)` (`api/internal/adult/pool.go:57`), `QueryPerformers`, `SearchPerformers`, `QueryStudios`, `SearchStudios`, `FindScene`, and `QueryScenes`.

### 2. Options considered for search and dispatch

| Option | Upstream reused (version, licence) | Effort | Risk |
| :--- | :--- | :--- | :--- |
| **(a) FansDB as one more stash-box source in adult sync + search via Whisparr** (Recommended) | `Whisparr/Whisparr` v3 (`v3.1.0.2116`, GPL-3.0); `goenvoy` stashbox (`v2.0.4`, MIT) | Low | Low. Reuses existing `adult` stash-box pool and Whisparr v3 `StashId` search pipeline. Respects [ADR-0006](0006-download-client-backpressure.md) and [ADR-0027](0027-adult-module.md). |
| **(b) FansDB source + direct indexer search through Prowlarr** | `Prowlarr` API (`v2.6.5.5623`, GPL-3.0); `itsrenoria/ptt-go` (untagged, MIT) or `razsteinmetz/go-ptn` (MIT) | High | Critical. Violates [ADR-0006](0006-download-client-backpressure.md). Requires SnatchArr to implement full torrent/usenet grab evaluation, quality profile parsing, custom formats, and download client queue management. Bypasses Whisparr library state and file unblockers. |
| **(c) Link only, no SnatchArr feature** | None | Zero | High. Fails product objectives ([ADR-0018](0018-product-front-door.md)). Forces operators to manually discover, monitor, search, and map creator content without fleet automation or stamina governance. |

#### Recommendation

**Option (a) is selected**. Adding FansDB as a configurable stash-box endpoint within `api/internal/adult` preserves architectural consistency. FansDB is treated as a peer metadata provider alongside StashDB.

### 3. Pre-search matching and search dispatch

Because pre-search indexing occurs before file bytes are downloaded, matching relies on structured string normalization rather than perceptual hashes:

- **Indexer naming patterns for creator content**:
  Releases on Usenet and BitTorrent indexers for creator content follow common scene and P2P formatting:
  - Pattern 1 (Studio + Date + Title): `Studio.Name.YYYY.MM.DD.Scene.Title.1080p.WEB-DL.x264-GROUP`
  - Pattern 2 (Performer + Date + Title): `Performer.Name.YYYY.MM.DD.Scene.Title.720p.MP4-GROUP`
  - Pattern 3 (Studio + Title): `Studio.Name.Scene.Title.2160p.WEB.H265-GROUP`
- **Whisparr v3 (Eros) integration**:
  - Whisparr v3 (Eros, Radarr-forked architecture, tag `v3.1.0.2116`, commit `94480761ec41`) natively tracks scenes as movie-like entities carrying a `StashId` (`MovieResource.cs:71`).
  - When Whisparr v3 is configured with FansDB as a metadata provider, SnatchArr submits scene additions and searches using the FansDB scene UUID via `stash:<id>`.
  - When Whisparr v3 lacks direct FansDB metadata integration, SnatchArr matches the looked-up scene by querying Whisparr's movie lookup endpoint with structured search strings (`Studio YYYY-MM-DD Title`).
  - SnatchArr triggers search execution via Whisparr's command API: `POST /api/v3/command` with `{ "name": "MoviesSearch", "movieIds": [id] }`.
- **Survey of upstream release parsers (HISS-19)**:
  - If pre-search filtering or release title parsing is evaluated in Go, existing MIT-licensed parsers were surveyed:
    - `github.com/itsrenoria/ptt-go` (no release tags, head commit `4a423b13e344`, MIT per its LICENSE file, read 2026-10-26): Comprehensive Go port of `parse-torrent-title`, extracting title, date, resolution, codec, and release group.
    - `github.com/razsteinmetz/go-ptn` (commit `72f87a8`, MIT, read 2026-10-26): Lightweight regex-based torrent title parser.
  - Under Option (a), release candidate parsing is handled internally by Whisparr (`src/NzbDrone.Core/Parser/Parser.cs`), eliminating the need to compile or maintain custom torrent title parsers in the Go control plane.
- **Hourly cap accounting**:
  - Searches dispatched to Whisparr count directly against the per-indexer hourly cap (`AGENTS.md:126`).
  - SnatchArr debits exactly **1 search per item** (per scene or movie) upon dispatch.
  - The rate bucket tracks indexer stamina, issuing warnings at 80 % consumption and deferring dispatches when stamina is exhausted (`snatch.GateIndexerLimit`).

### 4. Image-set handling and gallery management

Creator content frequently features photo sets and still image galleries alongside video scenes. Current media automation tools exhibit significant defects when handling image posts:

#### Why existing tools fail on image posts

1. **Lack of post grouping**: Creator posts often package 10 to 100 images per update. Files often land in flat folders or root directories, losing the contextual grouping of the post.
2. **Loss of sequence and ordering**: Image files often carry nondescript names or alphanumeric hashes. Media servers sort them alphabetically, destroying narrative or sequential shooting order (Stash Issue #4950, #5845).
3. **Destructive duplicate merging**: Creators frequently repost identical promotional photos or previews across multiple updates. Standard image scanners that deduplicate automatically by MD5 or image hash consolidate separate galleries unintentionally, corrupt chronological viewing order, or strip image references from existing galleries when duplicate files are removed (Stash Issue #7192, #5670, #4163, #4639).
4. **Loss of post captions**: Text captions accompanying image posts are either discarded or isolated into unread sidecar text files that standard image viewers ignore.
5. **Mixed image and video posts**: Posts containing both video clips and photo sets are split: the video is imported by Whisparr while the photos are ignored and orphaned on disk.
6. **Whisparr architectural limitation**: Whisparr v3 is designed for video media. Its API resources carry no image-set entity, and the stash-box schema offers no gallery fingerprint search yet (stash-box #1179).

#### SnatchArr image-set mapping

SnatchArr addresses these failures through the image-set architecture defined in [ADR-0027](0027-adult-module.md):

- **Decoupled filesystem storage**:
  - All image sets reside in a dedicated filesystem path configured in module settings (`image_set_dir`), validated at boot.
  - Whisparr library directories remain strictly read-only and unpolluted by image files.
- **Relational schema (`adult_image_sets` and `adult_image_items`)**:
  - `adult_image_sets` tracks the gallery container: title, directory path, total size, item count, and optional foreign linkage.
  - `adult_image_items` tracks individual image entries: file path, dimensions, MIME type, `sha256_checksum`, and explicit `sort_order INT`.
- **Deduplication without gallery destruction**:
  - Image files are hashed via SHA-256 (`sha256_checksum CHAR(64)`).
  - SHA-256 deduplication is used strictly for **storage optimization and thumbnail caching** (`<cache_dir>/thumbnails/<sha256>.webp`).
  - Galleries maintain independent records in `adult_image_items`. Two distinct image sets may reference the same file or identical image content while retaining their independent gallery identity, post titles, and custom sort order, resolving Stash Issue #7192 and #4163.
- **Mapping to FansDB entities**:
  - Because FansDB lacks a native `Gallery` GraphQL type, image sets in SnatchArr link to FansDB entities via:
    1. `adult_image_sets.performer_id`: Links the photo set to a synchronized FansDB performer UUID (`adult_favorite_performers.id`).
    2. `adult_image_sets.whisparr_scene_foreign_id`: Links the photo set to a Whisparr/FansDB scene foreign UUID, binding mixed-media posts together.
- **Memory and processing bounds (HISS-02)**:
  - Maximum image set size: 2 GB or 2,000 items per set.
  - Maximum single image decode buffer: 100 MB.
  - Thumbnails: Generated asynchronously as WebP, bounding box 400x400 pixels, quality 80.
  - Streaming delivery: Images are served via `http.ServeContent` with HTTP cache headers (`Cache-Control: private, max-age=86400`), bypassing application heap memory.

### 5. Boundary fencing, security, and privacy

In alignment with [ADR-0025](0025-module-switches.md) and [ADR-0027](0027-adult-module.md):

- **Route concealment**:
  - All FansDB and creator content configuration endpoints reside under `/api/v1/adult/*` with vendor tag `x-module: adult`.
  - When the adult module is disabled, all requests return RFC 9457 HTTP 404 Problem JSON (`application/problem+json`).
  - Users without the `adult_access` capability receive HTTP 404 on all endpoints.
- **Sanitized telemetry**:
  - Structured logs (`slog`) sanitize scene titles, performer names, and directory paths, recording only synthetic UUIDs and numeric status codes.
  - Prometheus metrics use static labels (`status="success|failure"`), preventing performer names or channel labels from leaking into central metric collectors.
- **Strict naming policy**:
  - Source code, configuration keys, database columns, and user interfaces must use neutral terms: "FansDB", "stash-box", "creator content", and "image sets".
  - Specific commercial host platforms, real performer names and third-party tools stay out of all committed assets.

## Consequences

### Positive

- **Unified metadata federation**: Extends SnatchArr's multi-instance stash-box sync to include creator content without duplicating client libraries or schemas.
- **Architectural purity**: Respects [ADR-0006](0006-download-client-backpressure.md) by delegating all media downloads and indexer grabs to Whisparr, keeping SnatchArr strictly within its search-and-planning role.
- **Robust gallery management**: Resolves widespread ordering, deduplication, and mixed-post bugs found in existing tools (Stash #7192) through relational ordering and SHA-256 cached thumbnails.
- **Fleet stamina protection**: Enforces the contract of 1 search per item against the indexer hourly cap, preventing search spam on creator content.
- **Strict privacy by design**: Route concealment (HTTP 404), encrypted secret storage, and sanitized telemetry protect household privacy.

### Negative

- **Invite dependency**: FansDB requires an invite-only account and API key; users without FansDB access cannot resolve creator metadata through this pathway.
- **Whisparr metadata coupling**: If Whisparr v3 is not configured to resolve FansDB stash IDs, scene matching must fall back to textual studio/date/title lookup.
- **Filesystem coordination**: Local image sets require accessible volume mounts shared between SnatchArr and media storage.

## Open questions

### 1. `fansdb-instance-onboarding`

How FansDB instances are registered in SnatchArr:

- **Options**:
  - *Option a: Pre-configured preset endpoint in SnatchArr settings with the user providing only their API key*.
    - *Risk*: Hardcodes the third-party endpoint URL in application defaults; requires a code change if the domain or GraphQL path moves.
  - *Option b: Generic stash-box instance entry where the operator provides both endpoint URL (`https://fansdb.cc/graphql`) and API key* (Recommended).
    - *Risk*: Requires the operator to manually copy the GraphQL endpoint URL into the instance configuration form.
  - *Option c: Automatic credential import from Whisparr v3 configuration*.
    - *Risk*: Copies a credential out of another application's settings; unverified whether the Whisparr v3 API returns stash-box keys at all.
- **Recommendation**: **Option b** (Generic stash-box instance entry). Treats FansDB uniformly as a stash-box instance in `adult_stashbox_instances` without hardcoding third-party domains.

### 2. `fansdb-scene-request-matching`

How SnatchArr submits creator scene requests to Whisparr v3:

- **Options**:
  - *Option a: Direct `StashId` lookup (`stash:<id>`) assuming Whisparr v3 has FansDB configured as a metadata provider* (Recommended).
    - *Risk*: Fails to add the movie in Whisparr if the user has not configured FansDB in Whisparr's metadata settings.
  - *Option b: Fallback to structured title and date query (`Studio YYYY-MM-DD Title`) if `stash:<id>` lookup yields no match*.
    - *Risk*: Fuzzy title matching in Whisparr can occasionally match similarly named scenes from the same studio.
  - *Option c: Require manual movie mapping in the SnatchArr UI prior to dispatch*.
    - *Risk*: Breaks automated background request processing and introduces significant operator friction.
- **Recommendation**: **Option a with fallback to Option b**. Attempt primary lookup by FansDB `StashId`; fall back to structured `Studio YYYY-MM-DD Title` search if unmatched.

### 3. `fansdb-image-set-ingest`

How creator image sets in `image_set_dir` are discovered and paired with FansDB metadata:

- **Options**:
  - *Option a: Directory scanner using folder naming convention (`[Studio] [YYYY-MM-DD] Title` or `[Performer] [YYYY-MM-DD] Title`)* (Recommended).
    - *Risk*: Relies on consistent folder naming on the library side.
  - *Option b: Sidecar metadata reader (`.json` or `.nfo` files placed alongside images)*.
    - *Risk*: Library folders may carry no standardized metadata sidecars.
  - *Option c: Manual UI upload and folder creation only*.
    - *Risk*: Infeasible for large multi-gigabyte collections containing hundreds of photo galleries.
- **Recommendation**: **Option a with optional sidecar fallback**. Primary directory scanner parses folder names matching `[Studio] [YYYY-MM-DD] Title` to pair with FansDB performers and scenes, reading optional JSON sidecars when present.

## References

- [FansDB](https://fansdb.cc/): Crowdsourced creator metadata database, GraphQL endpoint `/graphql`, read 2026-10-26.
- [stashapp/stash-box](https://github.com/stashapp/stash-box): Upstream GraphQL schema, release `v0.10.3` (commit `5eb3551e19fd`, MIT, read 2026-10-26). Issue #1179: Stash-box gallery fingerprint searching.
- [golusoris/goenvoy](https://github.com/golusoris/goenvoy): Stash-box client package `metadata/adult/stashbox/v2`, release `v2.0.4` (tag commit `392968f04f02`, MIT, read 2026-10-26).
- [Whisparr/Whisparr](https://github.com/Whisparr/Whisparr): Upstream repository, release `v3.1.0.2116` (tag commit `94480761ec41`, GPL-3.0, read 2026-10-26); `StashId` in `src/Whisparr.Api.V3/Movies/MovieResource.cs:71` at that tag.
- [stashapp/stash](https://github.com/stashapp/stash): Self-hosted media organizer, release `v0.31.1` (AGPL-3.0, read 2026-10-26). Issues #4163, #4639, #4950, #5670, #5845, #7192 documenting image set, gallery sorting, duplicate merging, and ordering bugs.
- [itsrenoria/ptt-go](https://github.com/itsrenoria/ptt-go): Release title parser in Go, no release tags (head commit `4a423b13e344`, MIT per its LICENSE file, read 2026-10-26).
- [razsteinmetz/go-ptn](https://github.com/razsteinmetz/go-ptn): Torrent filename parser in Go (commit `72f87a8`, MIT, read 2026-10-26).
- [ADR-0006: Download client backpressure](0006-download-client-backpressure.md): Download clients observed for backpressure, never driven directly.
- [ADR-0018: Product front door](0018-product-front-door.md): SnatchArr modular front door architecture.
- [ADR-0022: Per-instance priority](0022-instance-priority.md): Target instance selection for requests.
- [ADR-0025: Module switches](0025-module-switches.md): Optional module switches, `x-module` tagging, and quiescent leader loops.
- [ADR-0027: Adult module](0027-adult-module.md): Adult module specification, stash-box federation, Whisparr requests, and image sets.
