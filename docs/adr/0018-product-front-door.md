<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0018: Product front door of the media stack

- Status: proposed
- Date: 2026-10-20
- Supersedes: [ADR-0011](0011-product-definition.md)

## Context

ADR-0011 defined the SnatchArr product strictly as achieving functional parity with Huntarr and newtarr (issue #321). In that framing, SnatchArr was scoped narrowly to missing and cutoff-unmet searching across Sonarr, Radarr, Lidarr, Whisparr, and Readarr ([ADR-0008](0008-readarr-frozen-legacy-target.md)), with download queue management added by [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md).

The planning round of 2026-10-20 redefines the product scope from scratch (`snatcharr-product-core-20261020`, `snatcharr-product-adr-20261020`). Users manage separate applications, dashboards, logins, and rate limits, with no LLM access.

Under this product redefinition, ADR-0011 is superseded. Feature work beyond pull request #481 (live-events fix), release 0.5.2 (introducing grab limits), and preliminary architectural research awaits this architectural decision (`snatcharr-product-core-20261020`, `napp-shape-20261020`).

## Decision

SnatchArr (pronounced `/ˈsnætʃ.ɑːr/`: every search is a snatch, *-arr* is the \*arr family suffix, and the name is a deliberate pun ("the name means what you think it means" in [README.md](https://github.com/lusoris/SnatchArr/blob/main/README.md))) is defined as the front door of the media stack (`snatcharr-product-job-20261020`). It provides a single application to view and steer what is missing, requested, and downloading, manage the adult media library and image sets, and expose structured LLM access (`snatcharr-product-job-20261020`).

The underlying *arr applications (Sonarr, Radarr, Lidarr, Whisparr, Readarr) remain the specialized backend engines (`snatcharr-product-job-20261020`). Where an existing application already performs a specific domain task, SnatchArr connects to that deployed application and adds only the missing functional capabilities, never re-implementing existing working software (`snatcharr-product-overlap-20261020`, `snatcharr-product-seerr-20261020`, `snatcharr-product-more-media-20261020`).

### Target audience

The needs of the operator and the needs of public users weigh equally in every architectural and module decision (`snatcharr-audience-20261020`).

### Modular architecture and boundaries

SnatchArr adopts a modular architecture where every module, including existing SnatchArr features, can be switched on or off independently (`napp-shape-20261020`).

- **Toggleable modules**: The application provides a settings page exposing toggle switches for each module along with module-specific configuration options (`napp-shape-20261020`). New modules are disabled by default (`napp-shape-20261020`).
- **Hard module boundaries**: Each module maintains strict isolation boundaries, with its own dedicated Go package in `api/internal/`, separate HTTP routes in the OpenAPI specification (`api/openapi/openapi.yaml`), and dedicated database tables with independent database migrations (`napp-shape-20261020`).
- **Initial v1 modules**: The first stable version includes four primary modules (`snatcharr-product-v1-modules-20261020`):
  - **Search and queue core**: Automated scheduled sweeps for missing and cutoff-unmet items across *arr instances, hourly stamina and indexer rate limits, afterglow memory, and download queue governance via strike sweeps and blocklisting ([ADR-0006](0006-download-client-backpressure.md), [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md), `snatcharr-product-v1-modules-20261020`).
  - **Request portal (Seerr integration and gaps)**: Integration with Seerr, adding native support for media types unsupported by Seerr, subject to SnatchArr's unified search rules (`snatcharr-product-seerr-20261020`, `snatcharr-product-v1-modules-20261020`, `snatcharr-product-request-rules-20261020`).
  - **Adult module**: Multi-instance Stash-box discovery, favourites synchronization and search, request intake into Whisparr, local library browsing, and image-set gallery management linked to Whisparr scenes, with full Jellyfin integration; off by default (`napp3-folded-into-napp2-20261020`, `napp-shape-20261020`, `snatcharr-product-adult-scope-20261020`, `snatcharr-product-adult-visibility-20261020`). The plain direct descriptive name for this module will be chosen in the upcoming planning round (`napp3-folded-into-napp2-20261020`, `napp-shape-20261020`).
  - **LLM access module**: Built-in Model Context Protocol (MCP) server, repository-shipped agent skills, machine-readable `llms.txt` documentation, and per-client connection guides (`snatcharr-llm-integration-20261020`, `snatcharr-product-v1-modules-20261020`).

### Seerr integration and request scope

SnatchArr never re-implements Seerr (`snatcharr-product-seerr-20261020`). For standard TV show and movie requests, SnatchArr connects directly to an existing Seerr (Overseerr or Jellyseerr) instance: `import-instances` reads Seerr's Sonarr and Radarr servers, and SnatchArr reads requests and snatches them first through a read-only integration that never approves, declines, or retries a request (`snatcharr-product-seerr-20261020`, [docs/seerr.md](../seerr.md)).

SnatchArr provides native request handling only where Seerr has gaps (`snatcharr-product-seerr-20261020`):

- Adult media requests dispatched into Whisparr (`snatcharr-product-seerr-20261020`, `snatcharr-product-adult-scope-20261020`).
- Books dispatched into readarr-rresurrected (Readarr kind), audiobooks via audiobookshelf as library/player (`snatcharr-product-seerr-20261020`, `snatcharr-product-more-media-20261020`, `k8s-books-manager-20261020`).
- Comics dispatched into Comicarr (`snatcharr-product-seerr-20261020`, `snatcharr-product-more-media-20261020`, `k8s-comics-manager-20261020`).
- Video games dispatched into RomM (`snatcharr-product-seerr-20261020`, `snatcharr-product-more-media-20261020`).
- Enforcement of SnatchArr's search rule set across all request fulfillments (`snatcharr-product-seerr-20261020`, `snatcharr-product-request-rules-20261020`).

### Unified search and request rule set

All media requests follow the same rule set as background automated snatches (`snatcharr-product-request-rules-20261020`):

- Scheduling through SnatchArr's cycle scheduler with afterglow rest periods (`snatcharr-product-request-rules-20261020`).
- Indexer protocol priority: Usenet first with BitTorrent fallback (`snatcharr-product-request-rules-20261020`).
- Strict per-indexer hourly search limits and instance stamina caps (`snatcharr-product-request-rules-20261020`).

### Adult module scope and Jellyfin integration

The adult module incorporates both discovery and local library management, disabled by default (`napp3-folded-into-napp2-20261020`, `napp-shape-20261020`, `snatcharr-product-adult-scope-20261020`):

- **Stash-box discovery and favourites**: Combines multiple Stash-box community index instances into a unified discovery interface, synchronizes performer and scene favourites, and dispatches requests into Whisparr v2 and v3 (`napp3-folded-into-napp2-20261020`, `snatcharr-product-adult-scope-20261020`).
- **Own library browsing and image sets**: Provides browsing for locally owned adult media libraries. Image sets are managed directly within SnatchArr and displayed as galleries linked adjacent to their corresponding Whisparr scenes, providing Stash-like gallery viewing without duplicating video playback infrastructure (`napp3-folded-into-napp2-20261020`, `snatcharr-product-adult-scope-20261020`, `snatcharr-product-jellyfin-plugin-20261020`).
- **Jellyfin adult presentation**: Adult video files remain organized in Jellyfin media libraries, populated with performer, studio, and tag metadata via the SnatchArr Jellyfin plugin (`snatcharr-product-jellyfin-plugin-20261020`).
- **Access control**: Adult content visibility within SnatchArr and the Jellyfin plugin strictly mirrors Jellyfin user group memberships, parental ratings, and age restrictions (`snatcharr-product-adult-visibility-20261020`, `snatcharr-product-accounts-20261020`).

### Accounts, roles, and authentication

SnatchArr transitions from a single-admin model ([docs/sso.md](../sso.md)) to multi-user accounts with three distinct roles (`snatcharr-product-users-20261020`, `snatcharr-product-accounts-20261020`):

- `admin`: Full administrative control over instances, modules, system settings, and policies (`snatcharr-product-users-20261020`).
- `requester`: Authority to browse catalogues, submit media requests, view personal request status, and search allowed libraries (`snatcharr-product-users-20261020`).
- `viewer`: Read-only access to view library status, availability, and activity badges without requesting permissions (`snatcharr-product-users-20261020`).

User identities and roles are sourced directly from Jellyfin users and Jellyfin user groups via Jellyfin authentication, matching the authentication flow employed by Seerr (`snatcharr-product-accounts-20261020`). The account source must fit the existing Jellyfin plus SSO setup (`snatcharr-product-users-20261020`); how OIDC/proxy sign-in maps onto Jellyfin users is decided in the accounts ADR (follow-up 5).

### Jellyfin companion plugin

A dedicated SnatchArr plugin for Jellyfin provides in-interface integration across client devices (`snatcharr-product-jellyfin-plugin-20261020`):

- Direct media requests initiated within the Jellyfin UI (`snatcharr-product-jellyfin-plugin-20261020`).
- Adult content and performer discovery (`snatcharr-product-jellyfin-plugin-20261020`).
- Presentation of image sets as photo galleries directly alongside related scene video entries (`snatcharr-product-jellyfin-plugin-20261020`).
- Visual status badges indicating whether an item is requested, searching, downloading, or available (`snatcharr-product-jellyfin-plugin-20261020`).
- Performer and studio metadata enrichment for adult video libraries (`snatcharr-product-jellyfin-plugin-20261020`).

### LLM access module and agent surface

SnatchArr incorporates a comprehensive LLM integration module, switchable on the settings page (`snatcharr-llm-integration-20261020`):

- **Model Context Protocol (MCP) server**: Built directly into SnatchArr, providing a Streamable HTTP endpoint for remote agents and web clients, alongside a stdio transport for desktop agents (`snatcharr-llm-integration-20261020`).
- **Tool definitions and public API**: MCP tools are generated from or kept strictly aligned with the OpenAPI 3.1 specification (`api/openapi/openapi.yaml`). The server exposes resources (library, queue, history) and prompts (`snatcharr-llm-integration-20261020`). Public MCP tool names constitute a public API specified in a dedicated follow-up ADR (`snatcharr-llm-integration-20261020`).
- **Trust floor and access permissions**: A connected LLM is granted read-only access by default across all resources (`snatcharr-llm-rights-20261020`, `snatcharr-llm-integration-20261020`). Every mutating action (such as triggering searches, pausing queues, evicting items, or altering settings) remains disabled until explicitly toggled on per individual action in module configuration (`snatcharr-llm-rights-20261020`, `snatcharr-llm-integration-20261020`).
- **Scoped tokens**: Agent connections authenticate using scoped API tokens issued through the existing authentication system, restricted to authorized module endpoints (`snatcharr-llm-integration-20261020`).
- **Agent skills bundle**: The repository ships an Agent Skills bundle conforming to the `SKILL.md` specification, designed for equal footing across all AI coding and operational clients (Codex, Gemini CLI, Antigravity, opencode, Claude Code, and editor environments) without prioritizing any single client (`snatcharr-llm-integration-20261020`).
- **LLM context documentation**: The documentation site publishes machine-readable `llms.txt` and `llms-full.txt` files, accompanied by dedicated connection guides for each supported client (`snatcharr-llm-integration-20261020`).

### Supported media types (v1)

The first stable release supports seven primary media domains (`snatcharr-product-more-media-20261020`):

- **TV shows**: Driven through Sonarr instances (`snatcharr-product-job-20261020`, `snatcharr-product-seerr-20261020`).
- **Movies**: Driven through Radarr instances (`snatcharr-product-job-20261020`, `snatcharr-product-seerr-20261020`).
- **Music**: Driven through Lidarr instances (`snatcharr-product-more-media-20261020`).
- **Books and audiobooks**: Books driven through readarr-rresurrected, reusing the Readarr kind ([ADR-0008](0008-readarr-frozen-legacy-target.md) frozen Readarr support) once its v1 API is verified to match; audiobooks in audiobookshelf (`snatcharr-product-more-media-20261020`, `k8s-books-manager-20261020`).
- **Comics**: Driven through Comicarr; needs a new SnatchArr adapter for its own REST API (`snatcharr-product-more-media-20261020`, `k8s-comics-manager-20261020`).
- **Video games**: Managed and cataloged through RomM (`snatcharr-product-more-media-20261020`).
- **Adult content**: Scenes and performers driven through Whisparr v2 and v3, with Stash-box discovery and native image sets (`snatcharr-product-adult-scope-20261020`, `snatcharr-product-more-media-20261020`).

### Reuse and integration map

To avoid duplicating existing solutions, SnatchArr establishes clear boundaries across each module:

#### Module 1: Search and queue core

| Job | Existing app or library first | What SnatchArr adds | Link-only |
| :--- | :--- | :--- | :--- |
| Missing and cutoff-unmet search | Sonarr, Radarr, Lidarr, Whisparr, Readarr (readarr-rresurrected), Comicarr | Automated cycle scheduler, per-instance stamina caps, afterglow memory | No |
| Queue cleanup and stalled release eviction | *arr queue APIs (`DELETE .../queue/{id}`) | Multi-strike rule sweep (stalled, slow, failed, import-blocked, unwanted), automatic blocklist ([ADR-0012](0012-queue-management-replaces-cleanuparr-link.md)) | No |
| Download client backpressure observation | qBittorrent, Transmission, Deluge, rTorrent, SABnzbd, NZBGet | Read-only observation of queue depth and paused state to throttle searches ([ADR-0006](0006-download-client-backpressure.md)) | Yes |
| Centralized search rate limiting | Prowlarr | Global and per-indexer hourly search limits across all connected *arr instances | No |

#### Module 2: Request portal (Seerr integration and gaps)

| Job | Existing app or library first | What SnatchArr adds | Link-only |
| :--- | :--- | :--- | :--- |
| TV and movie request intake and approval | Seerr (Overseerr / Jellyseerr) | Connects directly to Seerr; imports its Sonarr and Radarr servers and reads requests read-only, never approving or declining ([docs/seerr.md](../seerr.md)) | No (connected, read-only) |
| Adult media requests | Whisparr (v2 and v3) | Request interface backed by Stash-box discovery and Whisparr ingestion | No |
| Book and audiobook requests | readarr-rresurrected (Readarr v1 API, to verify), audiobookshelf | Request interface for books and audiobooks with metadata resolution and target dispatch | No |
| Comic requests | Comicarr (own REST API) | New Comicarr instance adapter, request intake and dispatch | No |
| Game requests | RomM | Request intake and dispatch to RomM game library | No |
| Request fulfillment search rules | SnatchArr search scheduler | Applies SnatchArr scheduler rules: Usenet first with BitTorrent fallback, hourly indexer caps | No |

#### Module 3: Adult module

| Job | Existing app or library first | What SnatchArr adds | Link-only |
| :--- | :--- | :--- | :--- |
| Performer and scene discovery | Stash-box community instances | Multi-instance federation, unified search, and favourites synchronization | No |
| Adult scene search and snatching | Whisparr (v2 and v3) | Search triggering, stamina tracking, afterglow caching, and release management | No |
| Adult video streaming and browsing | Jellyfin | Libraries enriched with performer and studio metadata | Yes |
| Adult image set management and galleries | None (Stash-like without video) | Ingests, stores, and presents image sets as galleries linked adjacent to Whisparr scenes | No |

#### Module 4: LLM access module

| Job | Existing app or library first | What SnatchArr adds | Link-only |
| :--- | :--- | :--- | :--- |
| MCP protocol server | Model Context Protocol SDK | Built-in Streamable HTTP and stdio endpoints | No |
| MCP tool exposure | OpenAPI 3.1 (`api/openapi/openapi.yaml`) | Tools generated or kept in step with OpenAPI, resources (library, queue, history), prompts | No |
| Tool authorization and safety | SnatchArr auth subsystem | Read-only trust floor by default; per-action opt-in for mutating operations; scoped tokens | No |
| Agent integration skills | Agent Skills specification | Bundled `SKILL.md` skill definitions for Codex, Gemini CLI, Antigravity, opencode, Claude Code, and editors on equal footing | No |
| Machine-readable documentation | llms.txt standard | Generates `llms.txt` and `llms-full.txt` from documentation site; client-specific connect guides | No |

### Name

The product keeps the name SnatchArr; no rename is planned (`snatcharr-product-name-final-20261020`, superseding `snatcharr-product-rename-20261020`).

## Consequences

### Positive

- **Unified media operations**: Operators and users gain a single entry point ("front door") to monitor missing items, manage requests across all media formats, inspect download queues, and explore adult collections without operating fragmented standalone frontends (`snatcharr-product-job-20261020`).
- **Maximized upstream reuse**: By connecting directly to Seerr, Jellyfin, audiobookshelf, RomM, and the *arr suite, SnatchArr focuses development exclusively on functional gaps and workflow orchestration (`snatcharr-product-overlap-20261020`, `snatcharr-product-seerr-20261020`).
- **Architectural cleanliness via modularity**: Independent Go packages, routes, and database tables prevent cross-module coupling and a switched-off module serves no routes and runs no loops (`napp-shape-20261020`).
- **Centralized access control**: Sourcing authentication and permissions from Jellyfin eliminates redundant user directories and automatically applies established parental and age restrictions (`snatcharr-product-accounts-20261020`, `snatcharr-product-adult-visibility-20261020`).
- **Safe-by-default agent automation**: Built-in MCP endpoints and standard agent skill manifests allow safe AI interaction with full read visibility while requiring explicit administrative opt-in for any mutating operation (`snatcharr-llm-rights-20261020`, `snatcharr-llm-integration-20261020`).

### Negative

- **Expanded engineering scope**: Evolving from a targeted snatch scheduler into a complete media stack front door increases integration complexity, test surface, and dependency management requirements (`snatcharr-product-core-20261020`).
- **Multi-ecosystem maintenance burden**: Maintaining a companion Jellyfin plugin requires specialized C#/.NET development and testing across Jellyfin releases alongside the existing Go and Rust codebases (`snatcharr-product-jellyfin-plugin-20261020`).
- **Configuration overhead**: Supporting full module switchability requires dynamic route registration, conditional navigation menus, and modular database migration tooling (`napp-shape-20261020`).

### Follow-ups

Implementation of this product architecture requires five dedicated follow-up decisions; each is now drafted as its own ADR:

1. [x] **Module-switch architecture ADR**: Define the technical mechanism for dynamic module registration, route gating, and settings page schema (`napp-shape-20261020`). Drafted in [ADR-0025](0025-module-switches.md), proposed.
2. [x] **LLM integration ADR**: Define MCP server endpoints, public tool signatures, security boundary tests, and client configuration formats (`snatcharr-llm-integration-20261020`). Drafted in [ADR-0021](0021-llm-access-module.md), proposed.
3. [x] **Adult module ADR**: Establish the direct descriptive name, Stash-box synchronization protocol, Whisparr mapping rules, and image-set storage schema (`napp3-folded-into-napp2-20261020`, `napp-shape-20261020`, `snatcharr-product-adult-scope-20261020`). Drafted in [ADR-0027](0027-adult-module.md), proposed.
4. [x] **Jellyfin plugin ADR**: Specify the plugin architecture, API contract between Jellyfin and SnatchArr, gallery layout, and metadata delivery (`snatcharr-product-jellyfin-plugin-20261020`). Drafted in [ADR-0028](0028-jellyfin-plugin.md), proposed.
5. [x] **Accounts and roles ADR**: Detail Jellyfin authentication integration, session management, group-to-role mappings, and parental control enforcement (`snatcharr-product-users-20261020`, `snatcharr-product-accounts-20261020`). Drafted in [ADR-0026](0026-accounts-and-roles.md), proposed.

## References

- `napp3-folded-into-napp2-20261020` (2026-10-20T14:09:54) (paraphrased): Scope of adult image-set and Stash-box integration unified under a single module; plain descriptive name required.
- `napp-shape-20261020` (2026-10-20T14:13:07) (paraphrased): Architecture of switchable modules with hard package, route, and table boundaries; settings page design; phased implementation order.
- `snatcharr-llm-integration-20261020` (2026-10-20T14:44:41) (paraphrased): Built-in MCP server, OpenAPI synchronization, SKILL.md agent skills bundle, llms.txt context, and client setup documentation.
- `snatcharr-product-core-20261020` (2026-10-20T14:46:49) (paraphrased): Product definition rethought from scratch; ADR-0011 superseded.
- `snatcharr-audience-20261020` (2026-10-20T14:46:49) (paraphrased): Operator needs and public users weigh equally in all decisions.
- `snatcharr-llm-rights-20261020` (2026-10-20T14:46:49) (paraphrased): Connected LLMs read by default; mutating actions disabled until explicitly enabled per action.
- `snatcharr-product-job-20261020` (2026-10-20T14:48:58) (paraphrased): SnatchArr defined as the front door of the media stack while *arr apps remain engines.
- `snatcharr-product-overlap-20261020` (2026-10-20T14:48:58) (paraphrased): Add only what fixes real functional gaps; link and reuse existing applications.
- `snatcharr-product-v1-modules-20261020` (2026-10-20T14:48:58) (paraphrased): Definition of the four initial modules for the first stable release.
- `snatcharr-product-rename-20261020` (2026-10-20T14:48:58) (paraphrased): Product rename pending collision research and final selection before subsequent releases. Superseded by snatcharr-product-name-final-20261020.
- `snatcharr-product-users-20261020` (2026-10-20T14:51:45) (paraphrased): User accounts with admin, requester, and viewer roles.
- `snatcharr-product-adult-scope-20261020` (2026-10-20T14:51:45) (paraphrased): Adult module encompasses Stash-box discovery, Whisparr requesting, and local library and image-set browsing.
- `snatcharr-product-adult-visibility-20261020` (2026-10-20T14:51:45) (paraphrased): Adult content visibility governed by Jellyfin user groups, parental ratings, and age restrictions.
- `snatcharr-product-request-rules-20261020` (2026-10-20T14:51:45) (paraphrased): Requests governed by the unified search rule set: scheduler, Usenet first, and indexer caps.
- `snatcharr-product-accounts-20261020` (2026-10-20T14:55:08) (paraphrased): Users, roles, and adult access sourced from Jellyfin authentication and group configuration.
- `snatcharr-product-jellyfin-plugin-20261020` (2026-10-20T14:55:08) (paraphrased): Scope of companion Jellyfin plugin: requests, adult discovery, image-set galleries, and metadata badges.
- `snatcharr-product-seerr-20261020` (2026-10-20T14:55:08) (paraphrased): Direct connection to Seerr; zero re-implementation; adds only missing media types and snatch rule enforcement.
- `snatcharr-product-more-media-20261020` (2026-10-20T14:55:08) (paraphrased): First stable version covers TV, movies, music, books, audiobooks, comics, games, and adult media.
- `snatcharr-product-adr-20261020` (2026-10-20T14:58:12) (paraphrased): Direct order to draft ADR superseding ADR-0011 using the recorded decision rows.
- `k8s-books-manager-20261020` (2026-10-20T15:16:09) (paraphrased): Books manager set to readarr-rresurrected; audiobookshelf retained as library and player; SnatchArr reuses Readarr support if v1 API matches.
- `k8s-comics-manager-20261020` (2026-10-20T15:16:09) (paraphrased): Comics manager set to Comicarr in media namespace, Usenet first via Newznab/Torznab; Comicarr REST API requires new SnatchArr adapter.
- `snatcharr-product-name-final-20261020` (2026-10-20T15:17:14) (paraphrased): Name stays SnatchArr; no rename.
- [ADR-0006](0006-download-client-backpressure.md): Download clients observed for backpressure, never driven.
- [ADR-0008](0008-readarr-frozen-legacy-target.md): Readarr stays a supported target, frozen.
- [ADR-0011](0011-product-definition.md): Product definition: parity with Huntarr and newtarr (superseded).
- [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md): SnatchArr manages the download queue; the Cleanuparr link goes.
- [ADR-0017](0017-anatomical-brand-mark.md): An anatomical vulva mark replaces the M3 vesica.
