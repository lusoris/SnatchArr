<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0028: Jellyfin companion plugin and its contract

- Status: proposed
- Date: 2026-10-24
- Related: [ADR-0018](0018-product-front-door.md), [ADR-0023](0023-sso-viewer-role.md), [ADR-0025](0025-module-switches.md), [ADR-0026](0026-accounts-and-roles.md), [ADR-0027](0027-adult-module.md)

## Context

SnatchArr operates as the unified front door of the media stack ([ADR-0018](0018-product-front-door.md)). Under this architecture, the media server remains the primary interface where household users browse libraries, stream content, and check availability. To bridge the media viewing interface with SnatchArr's scheduling, request intake, adult media metadata, and queue tracking, ADR-0018 specifies a dedicated companion Jellyfin plugin as Follow-up 4 (`snatcharr-product-jellyfin-plugin-20261020`, [ADR-0018](0018-product-front-door.md)).

Issue [#567](https://github.com/lusoris/SnatchArr/issues/567) (under Epic [#572](https://github.com/lusoris/SnatchArr/issues/572), plan node `INT-206`) requires defining the companion plugin architecture, the OpenAPI integration contract, the metadata and gallery delivery mechanisms, and the codebase distribution lifecycle before implementing plugin code.

### Current implementation baseline

A survey of the SnatchArr codebase and upstream dependencies establishes the foundation for this integration:

1. **API scopes and permissions**:
   - `api/openapi/openapi.yaml:1863-1879` defines `ApiKeyScope` with read and write pairs for `core`, `requests`, `download_clients`, `prowlarr`, `notifications`, `admin` and `adult`.
   - API tokens are created with scoped permissions via `createAPIKey` (`api/openapi/openapi.yaml:241`) and authenticated via HTTP Bearer tokens (`api/openapi/openapi.yaml:1674-1676`). Every path is relative to the server URL `/api/v1` (`api/openapi/openapi.yaml:13-14`).
2. **Standardized problem payloads**:
   - All API endpoints emit RFC 9457 Problem Details payloads (`application/problem+json`) via the error handler in `api/internal/httpapi/problem.go:45`.
   - `domain.ErrUnauthorized` resolves to HTTP 401, `domain.ErrForbidden` to HTTP 403 and `domain.ErrNotFound` to HTTP 404 (`api/internal/httpapi/problem.go:30,32,34`).
3. **Module switch gating**:
   - ADR-0025 ([0025-module-switches.md](0025-module-switches.md), proposed) makes optional subsystems such as the adult module and the request portal switchable, off by default.
   - When a module is switched off, its routes return HTTP 404 Problem Details (ADR-0025 open question `snatcharr-adr25-disabled-status`, answered a).
4. **User roles and access control**:
   - ADR-0023 ([0023-sso-viewer-role.md](0023-sso-viewer-role.md)) maps Authentik groups to the `admin` and `viewer` roles.
   - ADR-0026 ([0026-accounts-and-roles.md](0026-accounts-and-roles.md), proposed) adds the `requester` role and Jellyfin sign-in (`snatcharr-product-users-20261020`).
   - Adult visibility follows each Jellyfin user's policy (`snatcharr-product-adult-visibility-20261020`); per the answered question `snatcharr-adult-rating-scale` the gate reads the user's `MaxParentalRating`, `BlockedTags` and `AllowedTags` and never grants more than Jellyfin allows.
5. **Contract compatibility gate**:
   - `tools/apicompat/gate/main.go` checks Go module API compatibility against a base revision. No check compares `api/openapi/openapi.yaml` against a base today; HISS-14 (append-only public API) binds the OpenAPI contract as policy only.
6. **Multi-ecosystem engineering burden**:
   - ADR-0018 lists the multi-ecosystem maintenance burden of a C#/.NET Jellyfin plugin as a negative consequence (ADR-0018, Consequences, Negative).

### Upstream technical environment

1. **Jellyfin Server release and target framework**:
   - The current stable Jellyfin Server release is `v12.3` (published 2026-10-23, read 2026-10-24 from [jellyfin/jellyfin release v12.3](https://github.com/jellyfin/jellyfin/releases/tag/v12.3)).
   - Jellyfin Server `v12.3` (tag commit `06de749d435e2d0be0c5119a41f2300fdd7143e5`) targets `net10.0` (`Jellyfin.Server/Jellyfin.Server.csproj:11`) with .NET SDK `10.0.0`, roll-forward `latestMinor` (`global.json`); assembly version `12.3.0` (`SharedVersion.cs`).
   - NuGet carries `Jellyfin.Controller` `12.3.0` (read 2026-10-24 from the nuget.org flat container index).
   - The official plugin template `jellyfin/jellyfin-plugin-template` (commit `c93225a0a5a76d3843db05b4b5b77fcfc482fba3`, 2026-09-18) still targets `net9.0`, ABI `10.11.0.0` and `Jellyfin.Controller` / `Jellyfin.Model` `10.11.5` (`Jellyfin.Plugin.Template/Jellyfin.Plugin.Template.csproj:4,13,16`, `build.yaml:5-6`). The plugin follows the server instead: `net10.0`, `Jellyfin.Controller` `12.3.0`, target ABI `12.3.0.0`; the exact ABI string for 12.x is checked against a released 12.x plugin in the first plugin slice (unverified here).
2. **Upstream licenses**:
   - `jellyfin/jellyfin` is licensed under GNU General Public License v2.0 (read 2026-10-24 from [jellyfin LICENSE](https://raw.githubusercontent.com/jellyfin/jellyfin/v12.3/LICENSE)).
   - `jellyfin/jellyfin-plugin-template` is licensed under GNU General Public License v3.0 (read 2026-10-24 from [jellyfin-plugin-template LICENSE](https://raw.githubusercontent.com/jellyfin/jellyfin-plugin-template/master/LICENSE)).
   - C# plugins linking against `Jellyfin.Controller` and `MediaBrowser.*` assemblies form a combined work with GPL code under the Free Software Foundation's reading of the GPL. Per answered question `snatcharr-jellyfin-plugin-license`, the plugin carries `GPL-3.0-or-later`, matching `jellyfin/jellyfin-plugin-template`; template code may now be used with its notice, while SnatchArr core stays EUPL-1.2.
3. **Existing community plugins and link-not-build**:
   - Stash metadata plugins exist in the community: `DirtyRacer1337/Jellyfin.Plugin.Stash` (read 2026-10-24 from [DirtyRacer1337/Jellyfin.Plugin.Stash](https://github.com/DirtyRacer1337/Jellyfin.Plugin.Stash), MIT License, head commit `b1ab78b` of 2026-09-10) and the fork `alysson-souza/Jellyfin.Plugin.Stash` (MIT, last push 2026-09-09, read 2026-10-24 from [alysson-souza/Jellyfin.Plugin.Stash](https://github.com/alysson-souza/Jellyfin.Plugin.Stash)).
   - Stash-side NFO export tools exist: `carrotwaxr/stash-plugins` (`mcMetadata`, MIT, read 2026-10-24 from [carrotwaxr/stash-plugins](https://github.com/carrotwaxr/stash-plugins)).
   - Community web UI injection tools exist: `IAmParadox27/jellyfin-plugin-file-transformation` (GPL-3.0, last push 2026-10-12, read 2026-10-24 from [IAmParadox27/jellyfin-plugin-file-transformation](https://github.com/IAmParadox27/jellyfin-plugin-file-transformation)) and `n00bcodr/Jellyfin-JavaScript-Injector` (GPL-3.0, last push 2026-10-20, read 2026-10-24 from [n00bcodr/Jellyfin-JavaScript-Injector](https://github.com/n00bcodr/Jellyfin-JavaScript-Injector)).

### Problem statement

Integrating SnatchArr capabilities directly into Jellyfin presents distinct architectural challenges:

1. **Jellyfin plugin extensibility boundaries**: Jellyfin server plugins have well-defined server-side extension points (metadata providers, scheduled tasks, REST API controllers, admin configuration pages), but Jellyfin has no official plugin API for modifying the client Web UI (e.g. injecting buttons, badges, or interactive search dialogs).
2. **Metadata overlap and ecosystem reuse**: Whisparr and Stash-box store adult scene metadata, while existing community plugins bridge standalone Stash instances to Jellyfin. SnatchArr must deliver metadata without forcing users to host a redundant Stash server while honoring link-not-build principles (`snatcharr-product-overlap-20261020`).
3. **Multi-client delivery of image-set galleries**: Adult image sets must appear alongside video scenes (`snatcharr-product-jellyfin-plugin-20261020`) across diverse Jellyfin clients (smart TVs, mobile apps, web browsers) without leaking upstream Stash-box API credentials.
4. **Boundary between Seerr and SnatchArr**: Movie and TV show requests belong to Seerr (`snatcharr-product-seerr-20261020`), whereas SnatchArr handles gaps (adult, books, comics, games). The plugin must not duplicate Seerr request workflows.
5. **Codebase isolation and licensing**: Compiling against GPL-licensed Jellyfin libraries introduces licensing requirements that must not compromise SnatchArr's EUPL-1.2/MIT core repository.

## Decision

SnatchArr adopts the companion plugin architecture and integration contract specified below.

### 1. Jellyfin plugin model and Web UI injection strategy

The companion plugin is implemented as a server-side Jellyfin plugin in C# targeting the current Jellyfin plugin ABI.

#### Server-side capabilities

The plugin leverages supported server-side extension points:

1. **Base plugin**: Extends `MediaBrowser.Common.Plugins.BasePlugin<PluginConfiguration>`, managing plugin lifecycle, local settings, and connection configuration.
2. **Metadata providers**: Implements `MediaBrowser.Controller.Providers.IRemoteMetadataProvider<Movie, MovieInfo>` and `IRemoteImageProvider` to fetch adult scene details, performer credits, studio tags, and poster/backdrop artwork from SnatchArr.
3. **API controllers**: Implements ASP.NET Core API controllers inheriting from `Jellyfin.Api.BaseJellyfinApiController` (`Jellyfin.Api/BaseJellyfinApiController.cs:18`), exposing REST endpoints on the Jellyfin server port (under `/SnatchArr/`) to serve cached status badges, proxy gallery thumbnails, and broker request handshakes.
4. **Scheduled tasks**: Implements `MediaBrowser.Model.Tasks.IScheduledTask` to run background sweeps:
   - Synchronizing library availability states.
   - Refreshing cached adult performer and studio records.
   - Checking SnatchArr service connectivity and version handshake.
5. **Configuration page**: Implements `MediaBrowser.Model.Plugins.IHasWebPages` returning `PluginPageInfo` entries (`MediaBrowser.Model/Plugins/IHasWebPages.cs`), serving an embedded HTML/JavaScript configuration panel in the Jellyfin Dashboard to configure the SnatchArr base URL, API authentication tokens, Seerr base URL, and library mappings.

#### Web UI injection options and evaluation

Jellyfin provides no official API for server plugins to modify client web interfaces. Three options were evaluated:

| Option | Architecture | Upgrade durability | Client compatibility | Maintenance risk |
| :--- | :--- | :--- | :--- | :--- |
| **Option 1A: Server-first metadata + optional web companion script (Recommended)** | Pure server plugin for metadata, tags, and photo collections. Interactive badges and request dialogs delivered via an optional companion JS file loaded through the community JavaScript Injector plugin. | High. Server features survive all Jellyfin upgrades. Web script isolates DOM changes. | Server features work on all clients (TV, Mobile, Web). Badges/modals activate on Web. | Low. Server plugin never touches `index.html` on disk. |
| **Option 1B: Mandatory file-transformation middleware** | Server plugin depends on `jellyfin-plugin-file-transformation` to intercept and rewrite `index.html` HTTP responses dynamically. | Low. Brittle across major Jellyfin Web asset bundling changes and CSP updates. | Web client only. Zero effect on Android TV, Apple TV, or Roku. | High. Dependency on a third-party binary plugin that rewrites served files; container permission issues. |
| **Option 1C: Custom Jellyfin Web build or proxy filter** | Custom Docker image of `jellyfin-web` or reverse proxy rewrite (Nginx `sub_filter`) injecting SnatchArr assets. | Very low. Breaks on every upstream web container bump. | Web client only. | Severe. Heavy operational overhead for adopters. |

**Chosen approach**: Option 1A. Core capabilities (metadata enrichment, studio/performer tagging, photo library gallery synchronization) operate strictly on the server and are universally accessible across all client applications (Android TV, Apple TV, Roku, desktop, and web). Rich interactive UI features on the web (in-page status badges, one-click request modals) are packaged as a companion JavaScript script loaded through the community `Jellyfin-JavaScript-Injector` plugin. Jellyfin itself has no custom JavaScript setting: its branding options carry `CustomCss` only (`MediaBrowser.Model/Branding/BrandingOptions.cs:18`).

### 2. Metadata delivery for adult videos

Adult video files sit directly within standard Jellyfin Movie or Music Video libraries (`snatcharr-product-jellyfin-plugin-20261020`). Metadata resolution follows the link-not-build principle (`snatcharr-product-overlap-20261020`):

| Approach | Metadata source | Stash dependency | Whisparr integration | Verdict |
| :--- | :--- | :--- | :--- | :--- |
| **Option 2A: Native SnatchArr metadata provider (Recommended)** | SnatchArr REST API (`/api/v1/adult/metadata/scene`, proposed) querying SnatchArr's internal index of Whisparr scenes and Stash-box IDs. | None. Whisparr and SnatchArr suffice. | Direct mapping to Whisparr file paths and scene hashes. | **Adopted**. Single front door; zero duplicate databases. |
| **Option 2B: Community Stash plugin (`DirtyRacer1337/Jellyfin.Plugin.Stash`)** | Standalone Stash instance via GraphQL. | Required. User must run a full Stash instance. | Indirect. Stash must separately index the media folder. | **Supported via link-not-build**. Adopters with an existing Stash setup keep using it; SnatchArr does not rebuild this bridge. |
| **Option 2C: Jellyfin built-in providers (TMDb, TVDB, OMDb)** | Mainstream metadata scrapers. | None. | None. Zero coverage for adult scenes. | **Rejected**. Ineffective for adult video libraries. |

**Chosen approach**: Option 2A. The companion plugin registers a native Jellyfin metadata provider (`SnatchArrAdultProvider`) prioritizing SnatchArr's API. When Jellyfin scans an adult library:

1. The provider parses the file name, directory structure, or embedded Whisparr scene ID / stash-box hash.
2. The provider queries SnatchArr's `GET /api/v1/adult/metadata/scene?path={path}&hash={hash}`.
3. SnatchArr responds with the canonical scene title, studio/network, release date, synopsis, tag list, and performer credits (including performer gender, aliases, and profile image URLs).
4. Jellyfin populates `PersonInfo` entries for performers, allowing users to browse all scenes featuring a specific performer across the entire Jellyfin server.
5. If an adopter already operates a standalone Stash server and prefers community tooling, SnatchArr documents `DirtyRacer1337/Jellyfin.Plugin.Stash` (MIT) as a supported configuration under link-not-build principles (`snatcharr-product-overlap-20261020`).

### 3. API contract between Jellyfin plugin and SnatchArr

The plugin communicates with SnatchArr exclusively over HTTP REST, conforming to SnatchArr's OpenAPI 3.1 specification (`api/openapi/openapi.yaml`).

#### Authentication model

Authentication uses a two-tier model:

1. **Server-level scoped token**: Configured in the plugin dashboard settings. The administrator generates a dedicated API key in SnatchArr with scopes:
   - `core:read`: Read system status and indexer health.
   - `requests:read`, `requests:write`: Submit gap requests and query request statuses.
   - `adult:read`: Query adult scene metadata, performer profiles, and gallery image streams.
   This token authenticates all server-to-server operations (metadata scraping, scheduled sync tasks, background status updates).
2. **User-delegated context**: For interactive actions initiated by a Jellyfin user (e.g. submitting a request or browsing adult discovery), SnatchArr must know which user acts and must not take the plugin's word for it. A caller-supplied user id header alone is never trusted. The protocol is open question 4; the recommended shape forwards the user's own Jellyfin access token, which SnatchArr verifies with Jellyfin `GET /Users/Me` (`Jellyfin.Api/Controllers/UserController.cs:581`) and maps to an account per ADR-0026, enforcing role (`requester` vs `viewer`) and the adult gate of that user.

#### Contract endpoints

Paths are relative to the server URL `/api/v1`, as in `api/openapi/openapi.yaml`. Only `/system/status` exists today; every other path is proposed.

```yaml
# Contract operations consumed by the Jellyfin plugin
paths:
  /system/status:
    get:
      summary: Service handshake and version compatibility check
      security: [{ bearerAuth: [] }]
      responses:
        "200":
          description: Service status, version, and active modules

  /media/status:
    post:
      summary: Batch query item availability status for library badges
      security: [{ bearerAuth: [] }]
      requestBody:
        content:
          application/json:
            schema:
              type: object
              required: [items]
              properties:
                items:
                  type: array
                  items:
                    type: object
                    required: [provider_ids]
                    properties:
                      provider_ids:
                        type: object
                        additionalProperties: { type: string }
      responses:
        "200":
          description: Map of item statuses (available, downloading, searching, requested, unmonitored)

  /requests:
    get:
      summary: List user requests and fulfillment progress
      security: [{ bearerAuth: [] }]
    post:
      summary: Submit a gap media request (adult scene, book, comic, game)
      security: [{ bearerAuth: [] }]
      requestBody:
        content:
          application/json:
            schema:
              type: object
              required: [media_type, target_id]
              properties:
                media_type: { type: string, enum: [adult_scene, book, comic, game] }
                target_id: { type: string }

  /adult/metadata/scene:
    get:
      summary: Fetch adult scene metadata by hash, file path, or external ID
      security: [{ bearerAuth: [] }]
      parameters:
        - name: hash
          in: query
          schema: { type: string }
        - name: path
          in: query
          schema: { type: string }
      responses:
        "200":
          description: Adult scene metadata, performers, studio, tags
        "404":
          $ref: "#/components/responses/Problem"

  /adult/scenes/{id}/gallery:
    get:
      summary: Fetch image set gallery metadata for an adult scene
      security: [{ bearerAuth: [] }]
      responses:
        "200":
          description: Gallery image list with proxy URLs
        "404":
          $ref: "#/components/responses/Problem"

  /adult/galleries/{id}/images/{imageId}:
    get:
      summary: Stream proxied gallery image bytes
      security: [{ bearerAuth: [] }]
      responses:
        "200":
          description: Image binary stream (image/jpeg, image/webp)
        "404":
          $ref: "#/components/responses/Problem"
```

#### Module-off and unauthorized behavior

In strict accordance with ADR-0025 ([0025-module-switches.md](0025-module-switches.md)):

1. If the adult module is disabled in SnatchArr settings, all `/api/v1/adult/*` endpoints return HTTP 404 Problem Details (`application/problem+json`).
2. If the request portal module is disabled, `/api/v1/requests` endpoints return HTTP 404 Problem Details.
3. If a Jellyfin user lacks adult permissions (due to Jellyfin user group exclusions, age ratings, or parental locks per `snatcharr-product-adult-visibility-20261020`), SnatchArr returns HTTP 404 Problem Details. This prevents information leakage regarding the existence of restricted media or modules.
4. The plugin handles HTTP 404 cleanly by suppressing metadata enrichment, hiding request actions, and skipping gallery sync without logging noisy error traces.

#### Versioning and compatibility

1. Contract changes in `api/openapi/openapi.yaml` follow HISS-14 (append-only). An OpenAPI diff check against the base revision is a follow-up; the Go gate `tools/apicompat/gate/main.go` does not read the OpenAPI file.
2. During initialization, the plugin calls `GET /api/v1/system/status` and reads `version` (`SystemStatus`, `api/openapi/openapi.yaml:1759`); a list of active modules is a proposed addition to that schema. If the major version differs or required capabilities are absent, the plugin logs an administrative warning and enters a degraded fallback mode (disabling advanced features while maintaining existing cached metadata).

### 4. Presentation and delivery of image-set galleries next to scenes

Image sets must be presented as galleries linked adjacent to their corresponding Whisparr video scenes (`snatcharr-product-jellyfin-plugin-20261020`):

#### Jellyfin library presentation

1. **Jellyfin photo library integration**: Image-set folders live under the storage path chosen in ADR-0027 (stash-box has no gallery type, so sets come from local storage). A Jellyfin photo library pointed at that path shows them with Jellyfin's own photo support (`Emby.Photos/PhotoProvider.cs`), one folder per set named after its scene.
2. **Link between scene and set**: Jellyfin extras cannot carry photos: `ExtraType` holds only video kinds (`MediaBrowser.Model/Entities/ExtraType.cs`, `Clip` to `Short`). On native clients the set is therefore reached through the photo library; the scene overview names the set. A native cross-link (for example a collection holding both) is unverified and stays a follow-up.
3. **Web client enhancement**: When the companion web script is active, a "View Photo Gallery" button is injected into the video detail header, opening a full-screen image lightbox for convenient gallery browsing without leaving the scene page.

#### Image delivery and security fencing

1. **Zero credential leakage**: Stash-box API keys, upstream indexer endpoints, and external storage credentials MUST NEVER be exposed to Jellyfin, client browsers, or mobile apps (`snatcharr-product-adult-scope-20261020`).
2. **Authenticated image proxy**: All gallery images are served through SnatchArr's internal authenticated proxy: `GET /api/v1/adult/galleries/{id}/images/{imageId}`.
3. **Caching and optimization**: SnatchArr caches image thumbnails on local disk storage, streaming pre-scaled WebP/JPEG assets to Jellyfin. Jellyfin's internal image cache further accelerates repeat client requests.

### 5. Request routing and Seerr integration

Request handling adheres strictly to the rule: "SnatchArr never re-implements Seerr; it connects to Seerr and adds only what Seerr lacks" (`snatcharr-product-seerr-20261020`):

```text
User initiates request in Jellyfin
               │
               ▼
      Is media Type Movie or TV?
         │                │
        Yes               No (Adult, Book, Comic, Game)
         │                │
         ▼                ▼
Deep link / API to Seerr   Forward to SnatchArr
(Overseerr / Jellyseerr)   POST /api/v1/requests
                          │
                          ▼
                 SnatchArr Search Scheduler
                 (Usenet first, hourly cap,
                  dispatch to Whisparr/Readarr)
```

1. **Movies and TV series**:
   - Handled exclusively by Seerr (Overseerr or Jellyseerr).
   - If a user clicks a request prompt or status badge for a standard movie or TV show, the plugin provides a direct deep link to the corresponding title in Seerr (e.g. `https://seerr.example.com/movie/{tmdbId}`).
   - The plugin does not accept, approve, or forward movie/TV requests to Sonarr or Radarr.
2. **Gap media formats**:
   - Media types unsupported by Seerr (adult scenes for Whisparr, books for readarr-rresurrected, comics for Comicarr, games for RomM) are processed natively by SnatchArr (`snatcharr-product-more-media-20261020`).
   - The plugin submits the request to SnatchArr via `POST /api/v1/requests`.
   - SnatchArr verifies user quota and permissions, schedules the snatch according to unified search rules (Usenet first, BitTorrent fallback, per-indexer hourly cap per `snatcharr-product-request-rules-20261020`), and records processed memory.

### 6. Code location, build pipeline, release artifacts, and licensing

#### Code location options and evaluation

| Option | Description | Trade-offs | Verdict |
| :--- | :--- | :--- | :--- |
| **Option 6A: Subdirectory in `lusoris/SnatchArr` monorepo** | Place plugin in `plugins/jellyfin/`. | Synchronized versioning, but mixes .NET toolchain into Go/Rust CI; creates licensing friction with GPL linking. | Rejected. |
| **Option 6B: Dedicated repository `lusoris/jellyfin-plugin-snatcharr` (Recommended)** | Dedicated repository under the `lusoris` GitHub organization. | Clean separation of C# build tooling, independent release cadence, isolated GPL licensing boundary, standard Jellyfin plugin manifest hosting. Requires multi-repo issue tracking. | **Adopted**. Standard Jellyfin community practice. |
| **Option 6C: Multi-client companion plugins repository** | Combined repository for all future media server plugins (Jellyfin, Plex, Emby). | Premature abstraction before other platform plugins exist. | Rejected. |

The repository is `lusoris/jellyfin-plugin-snatcharr` in the lusoris organisation beside SnatchArr, with the same rulesets, secrets, and release setup as SnatchArr, created after the licence answer and with fleet adoption (per answered question `snatcharr-jellyfin-plugin-repo-name`).

#### Build and release pipeline

1. **Toolchain pin**: .NET SDK `10.0.x` via `global.json` at the plugin repository root, matching Jellyfin v12.3.
2. **Jellyfin plugin manifest**: The plugin repository publishes a standard Jellyfin plugin catalog manifest (`manifest.json`; the GUID below was generated once at repository creation on 2026-10-25 and never changes, the version and URL values are an example):

   ```json
   [
     {
       "guid": "040a8e66-1be2-4e9b-befb-89a889fdb229",
       "name": "SnatchArr Companion",
       "description": "Media requests, adult library metadata, and image-set galleries for SnatchArr.",
       "overview": "Bridges Jellyfin with your SnatchArr front door.",
       "owner": "lusoris",
       "category": "General",
       "versions": [
         {
           "version": "1.0.0.0",
           "changelog": "Initial release of SnatchArr companion plugin.",
           "targetAbi": "12.3.0.0",
           "framework": "net10.0",
           "sourceUrl": "https://github.com/lusoris/jellyfin-plugin-snatcharr/releases/download/v1.0.0.0/snatcharr-jellyfin-1.0.0.0.zip",
           "checksum": "<sha256-hex-digest>",
           "timestamp": "2026-10-24T05:15:00Z"
         }
       ]
     }
   ]
   ```

3. **Artifact signing and verification**: Release zip archives are signed using Ed25519 / minisign keys. Checksums are recorded in the manifest.
4. **CI workflow**: GitHub Actions in the plugin repository executes `dotnet test`, packages the release zip, generates SHA256 checksums, and updates the hosted `manifest.json` via GitHub Pages or raw release assets.

#### Licensing consequence of linking Jellyfin assemblies

- **Legal fact**: Jellyfin Server is licensed under GPLv2 ([LICENSE](https://raw.githubusercontent.com/jellyfin/jellyfin/v12.3/LICENSE)) and Jellyfin Plugin Template is licensed under GPLv3 ([LICENSE](https://raw.githubusercontent.com/jellyfin/jellyfin-plugin-template/master/LICENSE)). Compiling a C# plugin requires linking against `Jellyfin.Controller`, `Jellyfin.Model`, `MediaBrowser.Controller`, and `MediaBrowser.Model`.
- **Consequence**: Under the Free Software Foundation's reading of the GPL, dynamic linking against GPL libraries forms a combined work, so the plugin licence must be one under which that combination can be distributed. Per answered question `snatcharr-jellyfin-plugin-license`, the plugin carries `GPL-3.0-or-later`, matching `jellyfin/jellyfin-plugin-template`; recorded as a named exception to the fleet EUPL-1.2 own-code rule, reason: combined work with GPL Jellyfin assemblies; REUSE headers and the template's licence notice are carried. Template code may now be used with its notice. SnatchArr core stays EUPL-1.2.
- **Separation benefit**: Housing the plugin in a dedicated repository `lusoris/jellyfin-plugin-snatcharr` (`Option 6B`) ensures that Jellyfin's GPL copyleft boundary remains strictly confined to the C# plugin repository and does not cross into SnatchArr's EUPL-1.2/MIT core repository.

### 7. Verification and testing architecture

A three-tier testing strategy guarantees contract correctness and security boundary enforcement:

1. **Plugin unit tests (.NET / xUnit)**:
   - **Metadata transformation**: Verifies mapping of SnatchArr adult scene JSON payloads to Jellyfin `MovieInfo`, `PersonInfo` (actors, director, studio), and genre tags.
   - **Request routing logic**: Verifies that movie/TV queries correctly resolve to Seerr deep link structures, while adult/book media queries correctly build SnatchArr `POST /api/v1/requests` payloads.
   - **Status badge state mapping**: Verifies that SnatchArr queue states (`downloading`, `searching`, `available`, `unmonitored`) translate to correct visual badge tokens.
2. **Contract tests against SnatchArr OpenAPI**:
   - Automated tests run against a mock HTTP server or schema validator generated from `api/openapi/openapi.yaml`.
   - Asserts that all endpoints, request bodies, query parameters, and RFC 9457 Problem Details responses match SnatchArr's published API contract.
   - Changes in `api/openapi/openapi.yaml` are checked by the OpenAPI diff follow-up (HISS-14); the Go gate does not cover them.
3. **Planted negative security case**:
   - **Scenario**: A Jellyfin user without adult media privileges (restricted by Jellyfin user group membership or parental age rating per `snatcharr-product-adult-visibility-20261020`) attempts to view an adult scene gallery or request an adult title via the plugin.
   - **Expected behavior**: The plugin and SnatchArr API refuse the request, returning an RFC 9457 HTTP 404 Problem Details response (`application/problem+json`). Zero metadata, images, or scene existence cues are returned.
   - **Planted verification**: The gate lives in SnatchArr, so the planted case removes the adult gate from the SnatchArr handler under test; the API test MUST fail (red). Restoring the gate turns it green. A plugin-side mock alone proves nothing about the server.

## Consequences

### Positive

- **Native media center experience**: Users can browse adult scenes, view performer credits, inspect image-set galleries, and view snatch status badges directly within their existing Jellyfin applications (`snatcharr-product-jellyfin-plugin-20261020`).
- **Zero Seerr duplication**: Movie and TV requests continue through Seerr without code duplication or competing approval workflows (`snatcharr-product-seerr-20261020`).
- **Secure credential fencing**: Stash-box API credentials, upstream indexer keys, and private network URLs remain protected inside SnatchArr, completely isolated from client applications (`snatcharr-product-adult-scope-20261020`).
- **Clean licensing boundary**: A separate repository keeps the C# plugin that links GPL Jellyfin assemblies apart from SnatchArr's EUPL-1.2/MIT core codebase.
- **Resilient module gating**: Adherence to ADR-0025 ensures that disabled modules and unauthorized content return clean 404 responses without leaking information or throwing server exceptions.

### Negative

- **Multi-ecosystem development**: Developing and maintaining a C#/.NET repository introduces a second primary development stack alongside Go, Rust, and TypeScript/Svelte (`docs/adr/0018-product-front-door.md:174-175`).
- **Jellyfin Web UI maintenance**: The companion JavaScript script for Jellyfin Web requires ongoing regression testing against upstream Jellyfin Web client DOM changes across releases.
- **Dual-tier authentication complexity**: A server-level scoped API key for background work plus verified user context for interactive requests means two credentials paths to test.

## Follow-ups

- [x] Create `lusoris/jellyfin-plugin-snatcharr` with fleet adoption, rulesets/secrets/release setup as SnatchArr, GPL-3.0-or-later LICENSE + REUSE headers + template notice (created 2026-10-25; adoption in lusoris/jellyfin-plugin-snatcharr#1; the release app private key is still to be stored by the operator).
- [ ] Record the GPL-3.0-or-later exception in the fleet exception list with category and expiry.
- [ ] Add an OpenAPI diff check against the base revision (HISS-14) before the plugin contract ships.
- [ ] Add plugin API client endpoints to `api/openapi/openapi.yaml` and regenerate server bindings via `make api-gen`.
- [ ] Implement `SnatchArrAdultProvider` metadata provider and image provider in the plugin repository.
- [ ] Implement image-set presentation through a Jellyfin photo library and verify whether a native scene-to-set link exists.
- [ ] Implement request routing in the plugin (delegating movies/TV to Seerr, gap media to SnatchArr).
- [ ] Implement companion JavaScript script for Jellyfin Web status badges and request modals.
- [ ] Configure GitHub Actions CI in the plugin repository with .NET SDK pinning, automated unit tests, and manifest generation.

## Open questions

### Question 1: Plugin repository location and name (`jellyfin-plugin-repo-name`)

- **Context**: The companion plugin requires a dedicated repository in the `lusoris` organization. Permanent repository names must not be chosen autonomously.
- **Options**:
  - a) `lusoris/jellyfin-plugin-snatcharr` (Standard Jellyfin community naming convention, matching `jellyfin-plugin-*`).
    - *Risk*: Slightly diverges from other SnatchArr companion tools if additional plugins (e.g. Plex, Emby) are created later.
  - b) `lusoris/snatcharr-jellyfin` (SnatchArr ecosystem prefix).
    - *Risk*: Less recognizable within Jellyfin's plugin ecosystem catalogs.
  - c) `lusoris/snatcharr-companion-jellyfin` (Explicit descriptor).
    - *Risk*: Overly verbose repository identifier.
- **Recommended**: Option a (`lusoris/jellyfin-plugin-snatcharr`). Matches the standard convention followed by virtually all community Jellyfin plugins, simplifying catalog discovery.
- **Answered (2026-10-24, controller ledger `snatcharr-jellyfin-plugin-repo-name` and `snatcharr-jellyfin-plugin-org`)**: Option a. Repository `lusoris/jellyfin-plugin-snatcharr` in the lusoris organisation beside SnatchArr, same rulesets, secrets and release setup as SnatchArr; created after the licence answer and with fleet adoption.

### Question 2: Plugin open-source license (`jellyfin-plugin-license`)

- **Context**: The plugin links directly to GPL-licensed Jellyfin assemblies (`Jellyfin.Controller`, `Jellyfin.Model`). The fleet's own-code licence is EUPL-1.2, and copying GPL-3.0 template code would bind the plugin to the GPL.
- **Options**:
  - a) `GPL-3.0-or-later` (Permits forward compatibility with modern GPL-3.0+ plugins and template).
    - *Risk*: Departs from the fleet's own-code licence EUPL-1.2.
  - b) `GPL-2.0-only` (Direct match with Jellyfin Server repository license).
    - *Risk*: Incompatible with GPL-3.0-only dependencies or newer Jellyfin plugin template components.
  - c) `EUPL-1.2`, the fleet's own-code licence, with no template code copied.
    - *Risk*: Whether an EUPL-1.2 plugin linking GPL assemblies can be distributed needs a licence review.
- **Recommended**: Option a (`GPL-3.0-or-later`), matching `jellyfin/jellyfin-plugin-template`.
- **Answered (2026-10-24, controller ledger `snatcharr-jellyfin-plugin-license`)**: Option a. `GPL-3.0-or-later`, matching `jellyfin/jellyfin-plugin-template`; recorded as a named exception to the fleet EUPL-1.2 own-code rule, reason: combined work with GPL Jellyfin assemblies; REUSE headers and the template's licence notice are carried. Template code may now be used with its notice. SnatchArr core stays EUPL-1.2.

### Question 3: Web UI injection distribution method (`jellyfin-plugin-web-injection-method`)

- **Context**: Server metadata and photo libraries work on all clients, but interactive status badges and request buttons in Jellyfin Web need a client-side script. Jellyfin has no native custom JavaScript setting (`MediaBrowser.Model/Branding/BrandingOptions.cs` carries `CustomCss` only).
- **Options**:
  - a) Ship the companion script as a snippet for the community `Jellyfin-JavaScript-Injector` plugin (GPL-3.0, last push 2026-10-20).
    - *Risk*: Administrators install a second community plugin; its upkeep is outside SnatchArr.
  - b) Integrate optional support for `jellyfin-plugin-file-transformation` (GPL-3.0) into the C# plugin.
    - *Risk*: Rewriting served web files breaks across Jellyfin Web bundling changes.
  - c) No web script in the first release; server features only.
    - *Risk*: No in-page badges or request buttons on the web until a later release.
- **Recommended**: Option a, optional. Server features never depend on it, so a broken injector loses only the web extras.
- **Answered (2026-10-24, controller ledger `snatcharr-jellyfin-plugin-web-injection-method`)**: Option a. Every server feature works without the injector; a missing injector is logged, and the docs name it optional. A test runs the plugin with the injector absent.

### Question 4: User-delegated authorization protocol (`jellyfin-plugin-user-auth-method`)

- **Context**: Interactive requests and adult visibility must act for the Jellyfin user who clicked, with that user's role and adult gate (ADR-0026). A user id named by the caller proves nothing.
- **Options**:
  - a) Header `X-Jellyfin-User-Id` from the plugin, trusted from configured network ranges.
    - *Risk*: Anything that can reach SnatchArr from those ranges can act as any user, adult gate included.
  - b) The plugin forwards the user's own Jellyfin access token; SnatchArr verifies it with Jellyfin `GET /Users/Me` (`Jellyfin.Api/Controllers/UserController.cs:581`), caches the result briefly and maps the user per ADR-0026.
    - *Risk*: SnatchArr handles a Jellyfin user token and needs a Jellyfin round trip per new session; whether a plugin controller can read the caller's token is checked in the first plugin slice.
  - c) Per-user SnatchArr API tokens stored in Jellyfin user preferences.
    - *Risk*: High friction; every household member creates and pastes a key.
- **Recommended**: Option b. Jellyfin itself proves who the user is, and no network position grants identity.
- **Answered (2026-10-24, controller ledger `snatcharr-jellyfin-plugin-user-auth-method`)**: Option b. SnatchArr verifies the forwarded token with `GET /Users/Me`, caches the result briefly and never stores the token. When no user token is reachable, the request fails closed. Tests: a forged or expired token gets 401, and a caller-supplied user id header is ignored.

## References

- `snatcharr-product-jellyfin-plugin-20261020` (2026-10-20T14:55:08) (paraphrased): a SnatchArr plugin inside Jellyfin does requests from Jellyfin, adult discovery, image sets as galleries next to their scene, status badges; adult videos sit in Jellyfin libraries with performer and studio metadata.
- `snatcharr-product-adult-visibility-20261020` (2026-10-20T14:51:45) (paraphrased): adult visibility follows Jellyfin user groups and age or parental flags.
- `snatcharr-product-seerr-20261020` (2026-10-20T14:54) (paraphrased): SnatchArr never re-implements Seerr; it connects to Seerr and adds only what Seerr lacks.
- `snatcharr-product-overlap-20261020` (2026-10-20T14:48:58) (paraphrased): Add only what fixes real functional gaps; link and reuse existing applications.
- `snatcharr-product-adult-scope-20261020` (2026-10-20T14:51:45) (paraphrased): Adult module encompasses Stash-box discovery, Whisparr requesting, and local library and image-set browsing.
- `snatcharr-product-users-20261020` (2026-10-20T14:51:45) (paraphrased): User accounts with admin, requester, and viewer roles.
- `snatcharr-product-accounts-20261020` (2026-10-20T14:55:08) (paraphrased): Users, roles, and adult access sourced from Jellyfin authentication and group configuration.
- `snatcharr-product-request-rules-20261020` (2026-10-20T14:51:45) (paraphrased): Requests governed by the unified search rule set: scheduler, Usenet first, and indexer caps.
- `snatcharr-product-more-media-20261020` (2026-10-20T14:55:08) (paraphrased): First stable version covers TV, movies, music, books, audiobooks, comics, games, and adult media.
- [Jellyfin Server release v12.3](https://github.com/jellyfin/jellyfin/releases/tag/v12.3) (published 2026-10-23, read 2026-10-24; .NET SDK 10.0.0, target framework `net10.0`, GPLv2).
- [Jellyfin Plugin Template](https://github.com/jellyfin/jellyfin-plugin-template) (commit `c93225a0a5a76d3843db05b4b5b77fcfc482fba3`, read 2026-10-24; target framework `net9.0`, target ABI `10.11.0.0`, GPLv3).
- [DirtyRacer1337/Jellyfin.Plugin.Stash](https://github.com/DirtyRacer1337/Jellyfin.Plugin.Stash) (head commit `b1ab78b`, pushed 2026-09-10, read 2026-10-24; MIT License, community Stash metadata provider for Jellyfin).
- [alysson-souza/Jellyfin.Plugin.Stash](https://github.com/alysson-souza/Jellyfin.Plugin.Stash) (active fork of Jellyfin Stash metadata plugin, read 2026-10-24).
- [carrotwaxr/stash-plugins](https://github.com/carrotwaxr/stash-plugins) (`mcMetadata`, read 2026-10-24; Stash-side NFO and image exporter).
- [IAmParadox27/jellyfin-plugin-file-transformation](https://github.com/IAmParadox27/jellyfin-plugin-file-transformation) (read 2026-10-24; community file-transformation middleware for Jellyfin Web).
- [n00bcodr/Jellyfin-JavaScript-Injector](https://github.com/n00bcodr/Jellyfin-JavaScript-Injector) (read 2026-10-24; community script injector for Jellyfin Web).
- [ADR-0018: Product front door of the media stack](0018-product-front-door.md).
- [ADR-0023: Roles from single sign-on groups](0023-sso-viewer-role.md).
- [ADR-0025: Architecture and mechanism of optional module switches](0025-module-switches.md).
- `api/openapi/openapi.yaml:1863-1880`: `ApiKeyScope` enum definitions.
- `api/internal/httpapi/problem.go:34,45-48`: Centralized RFC 9457 Problem Details error handler.
- `tools/apicompat/gate/main.go:1-30`: Go and OpenAPI public contract compatibility gate.
