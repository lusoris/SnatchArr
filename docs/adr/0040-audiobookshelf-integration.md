<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: CC-BY-SA-4.0
-->
<!-- markdownlint-disable MD013 -->

# ADR-0040: Audiobookshelf Full Integration

- **Status**: Proposed (`OQ-0040-1` to `OQ-0040-8` answered 2026-10-26, ledger ids `snatcharr-oq-0040-1` to `snatcharr-oq-0040-8`; `OQ-0040-9` open)
- **Date**: 2026-10-26
- **Deciders**: lusoris
- **Relates to**: [ADR-0024](0024-watch-aware-modes.md), [ADR-0026](0026-accounts-and-roles.md), [ADR-0027](0027-adult-module.md), [ADR-0028](0028-jellyfin-plugin.md), [ADR-0035](0035-jellyfin-integration.md), [ADR-0036](0036-community-requests-and-voting.md)
- **Supersedes (in part)**: [ADR-0036](0036-community-requests-and-voting.md) `OQ-0036-9` (`Audiobookshelf = availability detection only` clause; ledger id `snatcharr-audiobookshelf-full-integration-20261026`)

## Context

**SnatchArr** (meaning: an automated media-snatching coordinator for `*arr` instances; origin: English *snatch* + the `*arr` suffix of the Servarr ecosystem; pronunciation: `/ˈsnætʃ.ɑːr/`) coordinates wanted-media searches, community requests, watch-aware scheduling, and notifications across self-hosted media stacks.

**Audiobookshelf** (abbreviated **ABS**; meaning: a self-hosted audiobook and podcast server; origin: English *audiobook* + *shelf*; pronunciation: `/ˈɔː.di.oʊ.bʊk.ʃɛlf/`) is the primary self-hosted media server for audiobooks and podcasts. In [ADR-0036](0036-community-requests-and-voting.md) (`OQ-0036-9`), Audiobookshelf was initially scoped as an availability-detection-only source while **Jellyfin** (meaning: a free-software media system; origin: English *jellyfish* + *fin*; pronunciation: `/ˈdʒɛl.i.fɪn/`) received full sign-in, account linking, request, vote, availability, watch-aware, and notification integration in [ADR-0035](0035-jellyfin-integration.md).

Per operator direction recorded under ledger id `snatcharr-audiobookshelf-full-integration-20261026`, Audiobookshelf receives a full integration of the same depth as Jellyfin in [ADR-0035](0035-jellyfin-integration.md):

1. **Account linking and sign-in**: household listeners can sign in to SnatchArr with their Audiobookshelf credentials or link an existing SnatchArr account to their Audiobookshelf account, with periodic revalidation of active status and library/content permissions.
2. **Requests and voting**: listeners browse audiobook and podcast discovery results, submit requests, and cast or withdraw votes within their quota.
3. **Library availability**: SnatchArr reconciles existing audiobooks and podcasts (including downloaded episodes) across configured Audiobookshelf libraries so already-available titles are badged in discovery and cannot be re-requested for the same target.
4. **Request fulfilment**: approved audiobook requests route through the book indexer and grab path (pending `OQ-0036-14` / ledger id `snatcharr-oq-0036-14-grab-path`), and approved podcast requests either subscribe and download episodes directly inside Audiobookshelf via its native RSS podcast manager or hand off to an indexer.
5. **Notifications**: SnatchArr delivers request lifecycle events (`request_submitted`, `request_approved`, `request_denied`, `request_available`, `request_failed`) and optionally ingests Audiobookshelf's own **Apprise** (meaning: a universal notification delivery library and API; origin: English *apprise*, to inform or notify; pronunciation: `/əˈpraɪz/`) webhook events (`onPodcastEpisodeDownloaded`, `onRSSFeedFailed`, `onRSSFeedDisabled`).
6. **Explicit-content and adult-module fencing**: Audiobookshelf's per-user `permissions.accessExplicitContent` flag, per-library `librariesAccessible` list, and tag allow/deny rules (`itemTagsSelected`, `selectedTagsNotAccessible`) are respected alongside SnatchArr's opt-in adult module ([ADR-0027](0027-adult-module.md)).
7. **Listening-progress / watch-state integration**: SnatchArr's `api/internal/watchaware` package ([ADR-0024](0024-watch-aware-modes.md)) reads Audiobookshelf `mediaProgress` and `playbackSessions` to power sequential audiobook series prefetching (next book in a series when the current book reaches the completion threshold) and listened-media retention.

### Any-adopter constraint

Every feature in this ADR must work on a standalone single-binary or `docker compose` deployment (`deploy/compose/docker-compose.yml`) containing only `postgres`, `snatcharr`, `snatcharr-worker`, and the adopter's `audiobookshelf` container:

- **No Kubernetes cluster assumed**: no ingress controller, service mesh, or external operator is required.
- **No external identity provider (IdP) or Single Sign-On (SSO) assumed**: while SnatchArr supports **Authentik** (meaning: an open-source identity provider; origin: German/English *authentic* spelled with a *k*; pronunciation: `/ɔːˈθɛn.tɪk/`) or ForwardAuth when configured (`api/internal/httpapi/security_sso.go`), and Audiobookshelf supports OpenID Connect (`server/Auth.js:362-480`), adopters running only local accounts in Audiobookshelf get full sign-in and account linking without deploying an OIDC provider.
- **No server-side plugin in Audiobookshelf assumed**: unlike Jellyfin ([ADR-0028](0028-jellyfin-plugin.md)), Audiobookshelf has no server plugin loader or client UI extension runtime (verified in the survey below). Every user-facing request and vote workflow works out of the box in the SnatchArr single-page application (SPA).

### Terminology and project names referenced in this ADR

To keep the specification self-contained, every upstream project or library referenced below is introduced with its meaning, origin, and pronunciation:

- **Emby** (meaning: a personal media server; origin: coined from *Media Browser* initials *M-B*; pronunciation: `/ˈɛm.bi/`): supported in `api/internal/watchaware` alongside Jellyfin.
- **Plex** (meaning: a client-server media player system; origin: Latin *plexus*, an interwoven network; pronunciation: `/plɛks/`): media server referenced in [ADR-0024](0024-watch-aware-modes.md) and [ADR-0036](0036-community-requests-and-voting.md).
- **Seerr** (meaning: a media request and discovery manager; origin: English *seer*, unified successor to Overseerr and Jellyseerr; pronunciation: `/ˈsɪər/`): external request bridge supported in SnatchArr (`api/internal/seerr`).
- **Readarr** (meaning: an ebook and audiobook collection manager; origin: English *read* + `*arr`; pronunciation: `/ˈriːd.ɑːr/`): retired upstream book manager documented in `docs/readarr.md`.
- **Chaptarr** (meaning: a fork of Readarr focused on books and audiobooks; origin: English *chapter* + `*arr`; pronunciation: `/ˈtʃæp.tɑːr/`): active book/audiobook manager candidate in [ADR-0036](0036-community-requests-and-voting.md) (`OQ-0036-14`).
- **Bookshelf** (meaning: a community maintained fork of Readarr; origin: English *bookshelf*; pronunciation: `/ˈbʊk.ʃɛlf/`): active book/audiobook manager candidate in [ADR-0036](0036-community-requests-and-voting.md) (`OQ-0036-14`).
- **LazyLibrarian** (meaning: an automated book, audiobook, and magazine manager; origin: English *lazy* + *librarian*; pronunciation: `/ˈleɪ.zi laɪˈbrɛər.i.ən/`): alternative book manager referenced in `docs/readarr.md`.
- **Lidarr** (meaning: a music collection manager; origin: *Lida* + `*arr`; pronunciation: `/ˈlaɪ.dɑːr/`), **Sonarr** (meaning: a television series PVR; origin: *sonar* + `*arr`; pronunciation: `/ˈsoʊ.nɑːr/`), **Radarr** (meaning: a movie collection manager; origin: *radar* + `*arr`; pronunciation: `/ˈreɪ.dɑːr/`), **Whisparr** (meaning: an adult video manager; origin: *whisper* + `*arr`; pronunciation: `/ˈwɪs.pɑːr/`), **Prowlarr** (meaning: an indexer manager and proxy; origin: *prowl* + `*arr`; pronunciation: `/ˈpraʊ.lɑːr/`), and **Configarr** (meaning: a declarative configuration sync tool for `*arr` instances; origin: *config* + `*arr`; pronunciation: `/kənˈfɪɡ.ɑːr/`): sibling Servarr components integrated in SnatchArr.
- **Stash** (meaning: a self-hosted organiser for adult media; origin: English *stash*, a private collection; pronunciation: `/stæʃ/`) and **stash-box** (meaning: an open-source metadata indexer and FingerPrint directory for Stash; origin: *Stash* + *box*; pronunciation: `/ˈstæʃ.bɒks/`): adult-module components governed by [ADR-0027](0027-adult-module.md).
- **golusoris** (meaning: shared Go foundation libraries for lusoris services; origin: *Go* + Latin *lusoris*, of the player; pronunciation: `/ˌɡoʊ.luːˈsɔː.rɪs/`), **goenvoy** (meaning: typed Go HTTP clients for media servers and `*arr` APIs; origin: *Go* + *envoy*, a diplomatic messenger; pronunciation: `/ˌɡoʊˈɛn.vɔɪ/`), and **sveltesentio** (meaning: shared Svelte 5 design system and component library; origin: *Svelte* + Latin *sentio*, to perceive or feel; pronunciation: `/ˌsvɛltˈsɛn.ti.oʊ/`): first-party libraries consumed by SnatchArr.

---

## Survey

### Pinned upstream sources

All upstream claims in this ADR were verified on `2026-10-26` against pinned Git commits:

| Upstream repository | Latest release tag | Commit / tag hash | Commit date | Licence | Verification scope |
| :--- | :--- | :--- | :--- | :--- | :--- |
| [`github.com/advplyr/audiobookshelf`](https://github.com/advplyr/audiobookshelf) | `v2.37.2` | tag `bb8355b3996a8a37dbfc25b1980720643098ff78` -> commit `edb1f7efc92ecacdef51aafb4d9d5054ce10604f` | `2026-10-23` | `GPL-3.0-only` (`LICENSE:1-674`) | Server routes, auth, users, permissions, books, podcasts, sessions, notifications, metadata providers, DB migrations |
| [`github.com/golusoris/goenvoy`](https://github.com/golusoris/goenvoy) | `mediaserver/audiobookshelf/v2.0.2` | commit `17183e338c983b01d497a42c0a76b82c7a3b1e53` (`main` `ac12960bb042cf3b582fbe2421150fe85b321593`) | `2026-10-26` | `MIT` (`LICENSE`) | Existing Go client in `mediaserver/audiobookshelf/` (`v2.0.2`) and gaps vs ABS `v2.37.2` |
| `lusoris/SnatchArr` (this worktree) | branch `docs/adr-0040-audiobookshelf` | `HEAD` (ADR-0035 on `main` since `868fc92d`) | `2026-10-26` | `AGPL-3.0-or-later` / `CC-BY-SA-4.0` (docs) | Existing `jellyfin`, `auth`, `httpapi`, `watchaware`, `notify`, `domain` packages |

### 1. Authentication, tokens, OIDC, users, and permissions (`advplyr/audiobookshelf` `v2.37.2`)

- **Mount layout (`server/Server.js:354-405`)**:
  - Public and authentication endpoints are mounted on the root Express router (`server/Server.js:354-404`), while `/api/*` is guarded by `this.auth.authMiddleware` (`server/Server.js:355`, `server/Auth.js:88-107`) using Passport Bearer authentication (`Authorization: Bearer <token>` or `?token=<token>` query string, `server/Auth.js:116-127`).
  - When `ROUTER_BASE_PATH` is set in ABS (default `/`, `server/Server.js:68`), all routes are mounted under that prefix (`server/Server.js:343`).
- **Unauthenticated server probe**:
  - `GET /status` (`server/Server.js:384-400`) returns `{ app: 'audiobookshelf', serverVersion, isInit, language, authMethods, authFormData }` without requiring a token.
  - `GET /ping` (`server/Server.js:401-404`) returns `{ success: true }`.
- **Credential login (`POST /login`, `server/Server.js:359`, `server/Auth.js:320-326`, `server/auth/LocalAuthStrategy.js`)**:
  - Accepts `{ username, password }` JSON body, rate-limited by `authRateLimiter` (`server/Auth.js:500-514`).
  - Returns `buildUserLoginResponsePayload(user, req, res)` (`server/Auth.js:272-312`): `{ user: user.toOldJSONForBrowser(), userDefaultLibraryId, serverSettings, Source }`.
  - **Token delivery header (`x-return-tokens: true`)**: when the client sends request header `x-return-tokens: true` (`server/Auth.js:274,300-310`), ABS includes `user.accessToken` (short-lived JWT, default `ACCESS_TOKEN_EXPIRY` = 1 hour = `3600` s, `server/auth/TokenManager.js:18,159-174`) and `user.refreshToken` (session-backed JWT, default `REFRESH_TOKEN_EXPIRY` = 30 days = `2592000` s, `server/auth/TokenManager.js:19,176-198`) in the JSON response body instead of setting an `HttpOnly` `refresh_token` cookie (`server/Auth.js:296-309`).
- **Session refresh and logout (`server/Server.js:360-361`, `server/Auth.js:331-359`, `server/auth/TokenManager.js:206-298`)**:
  - `POST /auth/refresh` (`server/Auth.js:331-359`) reads `x-refresh-token` header or `refresh_token` cookie, validates the row in the `sessions` table (`server/migrations/v2.26.0-create-auth-tables.js:31-66`), rotates the refresh token (with a 10-second `REFRESH_TOKEN_GRACE_PERIOD_MS` replay window using `lastRefreshToken`, added in `v2.35.0`, `server/migrations/v2.35.0-add-last-refresh-token.js:1-60`, `server/auth/TokenManager.js:20,231-265`), and returns a fresh `user.accessToken` and `user.refreshToken`.
  - `POST /logout` (`server/Server.js:360`, `server/Auth.js:230-263`) revokes the database session associated with `x-refresh-token` or cookie.
- **Scoped API keys (`v2.26.0+`, `server/routers/ApiRouter.js:338-341`, `server/controllers/ApiKeyController.js:22-189`, `server/models/ApiKey.js:1-147`)**:
  - Admin/root users manage database-backed API keys via `GET /api/api-keys`, `POST /api/api-keys`, `PATCH /api/api-keys/:id`, and `DELETE /api/api-keys/:id`.
  - Each `ApiKey` row (`server/models/ApiKey.js:56-102`) binds to a `userId` (the user whose permissions the key inherits, `server/auth/TokenManager.js:313-325`), an optional `expiresAt`, and `isActive`. The signed JWT payload has `{ type: 'api', keyId, userId, iat, exp? }` (`server/auth/TokenManager.js:140-157`).
  - **Version change (`v2.26.0`)**: prior to `v2.26.0` (`server/migrations/v2.26.0-create-auth-tables.js:10-130`), ABS used only `user.token`—a static, non-expiring JWT stored on the `users` row (`server/auth/TokenManager.js:111-113,330-335`). In `v2.37.2`, `TokenManager.generateAccessToken` is marked `@deprecated` (`server/auth/TokenManager.js:111`) and requests authenticated with legacy `user.token` set `user.isOldToken = true` (`server/auth/TokenManager.js:334`, `server/models/User.js:616-620`). Furthermore, `GET /api/users` and `GET /api/users/:id` hide the `root` user's legacy `token` (`token: ''`) whenever the caller is an `admin` rather than `root` (`server/controllers/UserController.js:31,37,100`, `server/models/User.js:603,617`).
- **OpenID Connect (`server/Server.js:362-382`, `server/Auth.js:362-480`, `server/models/User.js:214-341`)**:
  - ABS supports OIDC login via `GET /auth/openid`, `GET /auth/openid/callback`, `GET /auth/openid/mobile-redirect`, and `GET /auth/openid/config`.
  - OIDC-linked users store their provider subject claim in `user.extraData.authOpenIDSub` (`server/models/User.js:35-38,298,330,573-575`) and expose `hasOpenIDLink: true` in `toOldJSONForBrowser()` (`server/models/User.js:631`), but do not exposes raw `authOpenIDSub` in the browser JSON response unless read from the database or matched via email/username.
- **Users and permission model (`server/routers/ApiRouter.js:133-142,173-196`, `server/controllers/UserController.js:29-105`, `server/controllers/MeController.js:27-29`, `server/models/User.js:120-183,545-683`)**:
  - `GET /api/me` (`server/controllers/MeController.js:27-29`): returns the authenticated user's `toOldJSONForBrowser()` object (`id`, `username`, `email`, `type`, `isActive`, `isLocked`, `permissions`, `librariesAccessible`, `itemTagsSelected`, `mediaProgress`, `bookmarks`, `hasOpenIDLink`).
  - `GET /api/users` (`server/controllers/UserController.js:29-49`, requires `isAdminOrUp`): returns `{ users: [...] }` (minimal browser objects without `mediaProgress`).
  - `GET /api/users/:id` (`server/controllers/UserController.js:59-105`, requires `isAdminOrUp`): returns full user object with hydrated `mediaProgress`.
  - **Account types (`server/models/User.js:120,169-183,545-556`)**: `root` (exactly one initial superuser, cannot be deleted, `server/models/User.js:536-538`), `admin` (`isAdminOrUp === true`), `user`, and `guest`.
  - **Granular permissions (`server/models/User.js:169-183,603-683`)**:
    - `permissions.download` (`boolean`), `permissions.update` (`boolean`), `permissions.delete` (`boolean`), `permissions.upload` (`boolean`), `permissions.createEreader` (`boolean`).
    - `permissions.accessAllLibraries` (`boolean`) + `librariesAccessible` (`string[]` of library UUIDs; checked by `user.checkCanAccessLibrary(libraryId)`, `server/models/User.js:646-650`).
    - `permissions.accessExplicitContent` (`boolean`, defaults to `true` only for `root` and `admin`, `false` for `user` and `guest`, `server/models/User.js:178`; checked by `user.checkCanAccessLibraryItem(libraryItem)`, `server/models/User.js:675-683`).
    - `permissions.accessAllTags` (`boolean`) + `permissions.selectedTagsNotAccessible` (`boolean`, `true` = denylist, `false` = allowlist) + `itemTagsSelected` (`string[]`; checked by `user.checkCanAccessLibraryItemWithTags(tags)`, `server/models/User.js:658-667`).

### 2. Libraries, items (books and podcasts), and search (`advplyr/audiobookshelf` `v2.37.2`)

- **Libraries (`server/routers/ApiRouter.js:68-98`, `server/controllers/LibraryController.js:188-233`)**:
  - `GET /api/libraries` returns `{ libraries: [...] }`, automatically filtered to `req.user.permissions.librariesAccessible` when restricted (`server/controllers/LibraryController.js:191-194`).
  - Each library has `id` (UUID), `name`, `mediaType` (`'book'` or `'podcast'`, `server/controllers/LibraryController.js:80,201-205`), `provider` (default metadata provider slug, e.g., `'google'`, `'audible'`, `'itunes'`, or `'custom-<slug>'`), `folders` (`id`, `fullPath`), and `settings` (`markAsFinishedPercentComplete`, `markAsFinishedTimeRemaining`, `autoScanCronExpression`, `podcastSearchRegion`).
- **Library items (`server/routers/ApiRouter.js:75,108`, `server/controllers/LibraryController.js:604-639`, `server/models/Book.js:77-170`, `server/models/Podcast.js:14-204`, `server/models/PodcastEpisode.js:11-220`)**:
  - `GET /api/libraries/:id/items?limit=<n>&page=<p>&minified=0|1&include=rssfeed,numepisodesincomplete` returns `{ results, total, limit, page, mediaType }`.
  - `GET /api/items/:id?expanded=1` (`server/routers/ApiRouter.js:108`) returns a single `LibraryItem` with expanded `media`:
    - When `mediaType === 'book'` (`server/models/Book.js:81-134`): `media.id` (UUID), `metadata.title`, `metadata.subtitle`, `metadata.authors` (`[{ id, name }]`), `metadata.narrators` (`string[]`), `metadata.series` (`[{ id, name, sequence }]`), `metadata.isbn`, `metadata.asin`, `metadata.publishedYear`, `metadata.publisher`, `metadata.language`, `metadata.explicit` (`boolean`), `metadata.abridged` (`boolean`), `tags` (`string[]`), `genres` (`string[]`), `audioFiles` (`AudioFileObject[]`, total `duration` in seconds), `ebookFile` (`EBookFileObject`, optional supplementary ebook).
    - When `mediaType === 'podcast'` (`server/models/Podcast.js:18-71`, `server/models/PodcastEpisode.js:15-53`): `media.id` (UUID), `metadata.title`, `metadata.author`, `metadata.feedUrl` (`feedURL`), `metadata.itunesId`, `metadata.itunesPageUrl`, `metadata.itunesArtistId`, `metadata.language`, `metadata.explicit` (`boolean`), `metadata.type` (`episodic` / `serial`), `autoDownloadEpisodes` (`boolean`), `autoDownloadSchedule` (cron), `maxEpisodesToKeep`, `maxNewEpisodesToDownload`, and `episodes` (`PodcastEpisode[]` with `id`, `season`, `episode`, `title`, `pubDate`, `publishedAt`, `enclosure.url`, `guid` in `extraData.guid`, `audioFile.duration`).
  - `GET /api/libraries/:id/podcast-titles` (`server/routers/ApiRouter.js:96`, `server/controllers/LibraryController.js:1386-1413`, admin): returns `{ podcasts: [{ title, itunesId, libraryItemId, libraryId }] }` for fast podcast deduplication.
  - `POST /api/libraries/:id/scan?force=0|1` (`server/routers/ApiRouter.js:91`, `server/controllers/LibraryController.js:1262-1274`, admin): triggers an asynchronous filesystem scan on the library.
- **In-library and external metadata search (`server/routers/ApiRouter.js:84,286-292`, `server/controllers/LibraryController.js:959-969`, `server/controllers/SearchController.js:85-236`)**:
  - `GET /api/libraries/:id/search?q=<query>&limit=<n>` searches items inside library `:id` respecting the caller's explicit-content and tag permissions (`server/controllers/LibraryController.js:967`).
  - `GET /api/search/books?title=<t>&author=<a>&provider=<p>` (`server/controllers/SearchController.js:85-105`) queries external book providers (`google`, `openlibrary`, `itunes`, `audible`, `audible.<region>`, `audnexus`, `fantlab`, or registered `custom-*` providers, `server/controllers/SearchController.js:10-29,220-236`).
  - `GET /api/search/podcast?term=<term>&country=<cc>` (`server/routers/ApiRouter.js:288`, `server/controllers/SearchController.js:141-156`) searches iTunes and custom podcast metadata providers for podcast RSS feeds (`PodcastFinder.search`).

### 3. Listening progress and playback sessions (`advplyr/audiobookshelf` `v2.37.2`)

- **Per-user media progress (`server/models/MediaProgress.js:5-183`, `server/controllers/UserController.js:59-105`, `server/controllers/MeController.js:112-115,239-245`)**:
  - Stored in the `mediaProgresses` table (`server/models/MediaProgress.js:57-90`) with polymorphic association to `book` (`mediaItemType: 'book'`) or `podcastEpisode` (`mediaItemType: 'podcastEpisode'`, with `podcastId`).
  - `getOldMediaProgress()` (`server/models/MediaProgress.js:155-176`) returns `{ id, userId, libraryItemId, episodeId, mediaItemId, mediaItemType, duration, progress, currentTime, isFinished, hideFromContinueListening, ebookLocation, ebookProgress, lastUpdate, startedAt, finishedAt }`, where `progress` is a float in `[0, 1]`.
  - An admin/service key can read any user's full `mediaProgress` list via `GET /api/users/:id` (`server/controllers/UserController.js:59-105`), and a user token can read its own via `GET /api/me` (`server/controllers/MeController.js:27-29`) or `GET /api/me/progress` (`server/controllers/MeController.js:112-115`).
  - Series completion progress is also exposed per library via `GET /api/libraries/:id/series/:seriesId?include=progress` (`server/controllers/LibraryController.js:778-797`), returning `{ libraryItemIds, libraryItemIdsFinished, isFinished }`.
- **Playback sessions (`server/routers/ApiRouter.js:140-141,179-181,234-243`, `server/controllers/SessionController.js:29-142`)**:
  - `GET /api/sessions?user=<uuid>&itemsPerPage=<n>&page=<p>` (`server/controllers/SessionController.js:29-112`, admin) and `GET /api/users/:id/listening-sessions` (`server/routers/ApiRouter.js:140`) return historical playback sessions (`id`, `userId`, `libraryId`, `libraryItemId`, `episodeId`, `mediaType`, `mediaMetadata`, `duration`, `currentTime`, `timeListening`, `startedAt`, `updatedAt`).
  - `GET /api/sessions/open` (`server/controllers/SessionController.js:122-142`, admin) returns currently active playback streams across all users (`sessions` and `shareSessions`).

### 4. Podcast feed subscription and native episode downloading (`advplyr/audiobookshelf` `v2.37.2`)

Unlike video media servers (Jellyfin, Emby, Plex), Audiobookshelf is itself a full podcast client and downloader (`server/controllers/PodcastController.js:30-400`, `server/managers/PodcastManager.js:25-350`):

- **Inspect RSS feed (`POST /api/podcasts/feed`, `server/routers/ApiRouter.js:249`, `server/controllers/PodcastController.js:200-216`, requires `isAdminOrUp`)**:
  - Takes `{ rssFeed: "<url>" }`, fetches and parses the RSS XML via `getPodcastFeed(url)`, and returns `{ podcast }` with feed metadata (`title`, `author`, `description`, `feedUrl`, `imageUrl`, `itunesId`, `language`, `explicit`, `genres`, `episodes`).
- **Create / subscribe podcast (`POST /api/podcasts`, `server/routers/ApiRouter.js:248`, `server/controllers/PodcastController.js:40-189`, requires `isAdminOrUp`)**:
  - Takes `{ libraryId, folderId, path, media: { metadata: { title, author, feedUrl, imageUrl, itunesId, explicit, genres, ... }, autoDownloadEpisodes, autoDownloadSchedule, maxEpisodesToKeep, maxNewEpisodesToDownload, tags } }`.
  - Creates the podcast folder on disk inside the library folder (`server/controllers/PodcastController.js:71-99`), inserts the `podcast` and `libraryItem` rows in a transaction (`server/controllers/PodcastController.js:106-139`), downloads the cover image (`server/controllers/PodcastController.js:144-179`), emits Socket.IO `item_added` (`server/controllers/PodcastController.js:181`), and registers the podcast auto-download cron job if `autoDownloadEpisodes: true` (`server/controllers/PodcastController.js:186-188`).
  - Also supports bulk OPML creation via `POST /api/podcasts/opml/create` (`server/routers/ApiRouter.js:251`, `server/controllers/PodcastController.js:249-274`).
- **Check and download new episodes (`GET /api/podcasts/:id/checknew?limit=<n>`, `server/routers/ApiRouter.js:252`, `server/controllers/PodcastController.js:284-301`, requires `isAdminOrUp`)**:
  - Polls the podcast's `feedURL` and queues up to `limit` (default `3`) new episodes for download via `PodcastManager.checkAndDownloadNewEpisodes`.
- **Search and download specific episodes (`GET /api/podcasts/:id/search-episode?title=<q>`, `POST /api/podcasts/:id/download-episodes`, `server/routers/ApiRouter.js:255-256`, `server/controllers/PodcastController.js:342-380`)**:
  - `GET /api/podcasts/:id/search-episode?title=<q>` searches the RSS feed for matching episodes; `POST /api/podcasts/:id/download-episodes` (requires `isAdminOrUp`) accepts an array of `RssPodcastEpisode` objects and queues them in `PodcastManager.downloadPodcastEpisodes` (`server/managers/PodcastManager.js:60-66`), which downloads the audio enclosure, tags/probes it with `ffmpeg`/`ffprobe` (`server/managers/PodcastManager.js:124-160`), creates the `podcastEpisode` row (`server/managers/PodcastManager.js:207`), enforces `maxEpisodesToKeep` (`server/managers/PodcastManager.js:222-244`), and emits Socket.IO `episode_download_started`, `episode_download_finished`, and `episode_added` (`server/managers/PodcastManager.js:104,177,256`).

### 5. Notification system and Socket.IO events (`advplyr/audiobookshelf` `v2.37.2`)

- **Outbound Apprise notifications (`server/routers/ApiRouter.js:265-273`, `server/controllers/NotificationController.js:13-153`, `server/objects/settings/NotificationSettings.js:5-118`, `server/objects/Notification.js:3-133`, `server/utils/notifications.js:3-110`, `server/managers/NotificationManager.js:7-236`)**:
  - ABS stores notification configuration in `NotificationSettings` (`id: 'notification-settings'`, `appriseType: 'api'`, `appriseApiUrl`, `notifications: Notification[]`, `maxFailedAttempts: 5`, `maxNotificationQueue: 20`, `notificationDelay: 1000`).
  - When an event fires, `NotificationManager.sendNotification` (`server/managers/NotificationManager.js:222-234`) sends an HTTP `POST` to `appriseApiUrl` with timeout `6000` ms and JSON body `{ urls: string[], title: string, body: string }` rendered from `{{variable}}` templates (`server/objects/Notification.js:104-131`).
  - **Exhaustive ABS notification event list (`server/utils/notifications.js:4-109`)**:
    1. `onPodcastEpisodeDownloaded` (variables: `libraryItemId`, `libraryId`, `podcastTitle`, `podcastAuthor`, `podcastDescription`, `podcastGenres`, `episodeTitle`, `episodeSubtitle`, `episodeDescription`, `libraryName`, `episodeId`, `mediaTags`) — Note: fired only when `isAutoDownload === true` (`server/managers/PodcastManager.js:258-261`).
    2. `onBackupCompleted` (variables: `completionTime`, `backupPath`, `backupSize`, `backupCount`, `removedOldest`).
    3. `onBackupFailed` (variables: `errorMsg`).
    4. `onRSSFeedFailed` (variables: `feedUrl`, `numFailed`, `title`).
    5. `onRSSFeedDisabled` (variables: `feedUrl`, `numFailed`, `title`).
    6. `onTest` (variables: `version`).
  - Notably, ABS `v2.37.2` has **no** built-in Apprise notification event for `onBookAdded` (books added during library scans only emit Socket.IO `item_added` / `items_added`).
- **Socket.IO real-time events (`server/SocketAuthority.js:154-250`)**:
  - ABS runs a Socket.IO server at `/socket.io` (and `${RouterBasePath}/socket.io`, `server/SocketAuthority.js:164-174`). Clients authenticate after connection by emitting `socket.emit('auth', token)` (`server/SocketAuthority.js:188`).
  - Server-emitted events include `item_added`, `item_updated`, `item_removed`, `items_added`, `items_updated`, `episode_added`, `episode_download_queued`, `episode_download_started`, `episode_download_finished`, `user_added`, `user_updated`, `user_online`, `user_offline`, and `notifications_updated`.

### 6. Plugin / extension mechanism check (`advplyr/audiobookshelf` `v2.37.2`)

- **No server or UI plugin system exists**:
  - A full search of `advplyr/audiobookshelf` at `v2.37.2` (`server/`, `client/`, `docs/`, `index.js`) confirms there is no server-side plugin loader, no dynamic JavaScript/WebAssembly extension hook, and no client-side custom tab/widget injection mechanism comparable to Jellyfin's C# `IServerEntryPoint` / `IPlugin` architecture ([ADR-0028](0028-jellyfin-plugin.md)).
  - **Custom Metadata Provider REST specification (`custom-metadata-provider-specification.yaml:1-140`, `server/routers/ApiRouter.js:319-321`, `server/controllers/CustomMetadataProviderController.js`)**: the only external extension point in ABS is an outbound HTTP metadata provider contract (`GET /search?query=<q>&author=<a>` with `Authorization` header returning `{ matches: BookMetadata[] }`, registered at `POST /api/custom-metadata-providers`).
  - **Iframe embedding (`server/Server.js:263-267`)**: ABS only exposes an environment toggle `ALLOW_IFRAME=1` (`global.AllowIframe`), which omits `Content-Security-Policy: frame-ancestors 'none'` so ABS itself can be embedded inside an external dashboard iframe, not the reverse.
  - Consequently, all request submission and voting UI for Audiobookshelf users must live in the SnatchArr SPA (and optionally a lightweight browser WebExtension in a future milestone if desired, outside the server boundary).

### 7. Audit of `github.com/golusoris/goenvoy` Audiobookshelf client

- **Does `golusoris/goenvoy` already have an Audiobookshelf client?**
  - **Yes**: `mediaserver/audiobookshelf` in `github.com/golusoris/goenvoy` (Go module `github.com/golusoris/goenvoy/mediaserver/audiobookshelf/v2`, latest tag `mediaserver/audiobookshelf/v2.0.2` at commit `17183e338c983b01d497a42c0a76b82c7a3b1e53`, `2026-10-26`).
- **Implemented surface in `mediaserver/audiobookshelf/v2.0.2` (`audiobookshelf.go:1-342`, `types.go:1-225`)**:
  - `New(cfg Config, opts ...Option) (*Client, error)` (`audiobookshelf.go:93-128`): Bearer token auth via `bearerRoundTripper` (`transport.go`).
  - `GetLibraries(ctx)` (`GET /api/libraries`, `audiobookshelf.go:135-146`), `GetLibrary(ctx, id)` (`GET /api/libraries/{id}`, `audiobookshelf.go:150-159`), `GetLibraryItems(ctx, id)` (`GET /api/libraries/{id}/items`, `audiobookshelf.go:164-175`), `GetItem(ctx, id)` (`GET /api/items/{id}?expanded=1`, `audiobookshelf.go:179-188`).
  - `GetUsers(ctx)` (`GET /api/users`, `audiobookshelf.go:192-200`), `GetMe(ctx)` (`GET /api/me`, `audiobookshelf.go:204-212`).
  - `GetCollections(ctx, id)` (`audiobookshelf.go:216-227`), `GetSessions(ctx)` (`GET /api/sessions`, `audiobookshelf.go:232-243`), `Search(ctx, libraryID, query)` (`GET /api/libraries/{id}/search`, `audiobookshelf.go:254-273`), `GetMediaProgress(ctx, itemID)` (`GET /api/me/progress/{id}`, `audiobookshelf.go:277-286`), `GetAuthors(ctx, libraryID)` (`audiobookshelf.go:290-301`), `GetSeries(ctx, libraryID)` (`audiobookshelf.go:306-317`).
- **Gaps and mismatches in `mediaserver/audiobookshelf/v2.0.2` vs ABS `v2.37.2` (must be fixed upstream in `golusoris/goenvoy` before SnatchArr consumes them)**:
  1. **`GetServerInfo` calls a non-existent route**: `GetServerInfo` (`audiobookshelf.go:247-250`) calls `GET /api/server`, which does not exist in ABS `v2.37.2` (`server/routers/ApiRouter.js:64-362`). Upstream `goenvoy` needs `GetStatus(ctx)` calling `GET /status` (`server/Server.js:384-400`) and `Ping(ctx)` calling `GET /ping` (`server/Server.js:401-404`).
  2. **`GetUsers` response envelope mismatch**: `GetUsers` (`audiobookshelf.go:192-200`) decodes `[]User` directly, whereas ABS `UserController.findAll` (`server/controllers/UserController.js:46-48`) returns `{"users": [...]}`.
  3. **Missing `GetUser(ctx, id)` and complete `User` permissions/progress fields**: `types.go:115-126` defines `User` with only `ID`, `Username`, `Type`, `IsActive`, `IsLocked`, `LastSeen`, `CreatedAt`. It omits `Email`, `Permissions` (`accessAllLibraries`, `accessExplicitContent`, `accessAllTags`, `selectedTagsNotAccessible`, `download`, `update`, `delete`, `upload`), `LibrariesAccessible` (`[]string`), `ItemTagsSelected` (`[]string`), `MediaProgress` (`[]MediaProgress`), and `HasOpenIDLink` (`bool`), and lacks `GetUser(ctx, id)` (`GET /api/users/{id}`, `server/controllers/UserController.js:59-105`).
  4. **Missing `Login` / `Logout` / token verification**: no `Login(ctx, username, password)` method calling `POST /login` with `x-return-tokens: true` (`server/Auth.js:320-326`) and `POST /logout` (`server/Auth.js:230-263`) to immediately revoke the verification session.
  5. **Missing pagination parameters on `GetLibraryItems` and `GetSessions`**: `GetLibraryItems` (`audiobookshelf.go:164-175`) does not accept `limit`/`page`/`include`, and `ItemMetadata` (`types.go:53-70`) omits podcast-specific fields (`feedURL`, `itunesId`, `episodes`) and `ebookFile`.
  6. **Missing podcast management methods**: `ParsePodcastFeed(ctx, rssURL)` (`POST /api/podcasts/feed`), `CreatePodcast(ctx, req)` (`POST /api/podcasts`), `CheckNewEpisodes(ctx, itemID, limit)` (`GET /api/podcasts/{id}/checknew`), `DownloadEpisodes(ctx, itemID, episodes)` (`POST /api/podcasts/{id}/download-episodes`), and `SearchPodcasts(ctx, term, country)` (`GET /api/search/podcast`).

---

## Decision (answers recorded; sign-in surface pending `OQ-0040-9`)

> [!IMPORTANT]
> The operator answered `OQ-0040-1` through `OQ-0040-8`; the [Answered questions](#answered-questions) table binds where it differs from a row below. Row 1a (sign-in with Audiobookshelf) and the bootstrap part of row 2a wait for `OQ-0040-9`, because [ADR-0035](0035-jellyfin-integration.md) makes Jellyfin sign-in the only user path. No database migration, OpenAPI schema change, or Go/Rust/Svelte implementation lands for a row before its question is answered.

### Function-by-function mapping table

| # | Surface | Audiobookshelf endpoint / event (`v2.37.2`) | SnatchArr side (endpoints, packages, tables) | Who (role) | Status | Milestone |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **1a** | **Sign-in with Audiobookshelf** (standalone auth, no IdP required) | `POST /login` with header `x-return-tokens: true` (`server/Auth.js:274,320-326`), followed immediately by `POST /logout` (`server/Auth.js:230-263`) with `x-refresh-token` so SnatchArr never retains an ABS session or password | Planned `POST /api/v1/auth/audiobookshelf/login`; new `api/internal/audiobookshelf.Authenticator` mirroring `api/internal/jellyfin/authenticator.go:46-135`; calls `auth.Service.LoginExternal` (`api/internal/auth/identity.go`) with `provider = 'audiobookshelf'`; issues SnatchArr session cookie (`snatcharr_session`) | Any active ABS user (`isActive: true`, `isLocked: false`, `type != 'guest'`) | **Proposed** (`OQ-0040-2`, `OQ-0040-4`, `OQ-0040-7`) | after OQ-0040 answers |
| **1b** | **Account linking** (attach ABS identity to existing local or OIDC SnatchArr account) | `POST /login` (`x-return-tokens: true`) + `POST /logout` (credential proof) **or** `GET /api/me` (`server/controllers/MeController.js:27-29`) using a user-supplied ABS API key (`OQ-0040-2`) | Planned `POST /api/v1/auth/identities/audiobookshelf` and `DELETE /api/v1/auth/identities/audiobookshelf`; extends `api/internal/httpapi/handlers_identities.go:25-115` and `external_identities` table (`provider = 'audiobookshelf'`, `external_id = absUser.id`) | Authenticated SnatchArr `user`, `trusted_requester`, or `admin` | **Proposed** (`OQ-0040-2`) | after OQ-0040 answers |
| **2a** | **User & role mapping** | `user.type` (`root`, `admin`, `user`, `guest`) and `user.isActive`, `user.isLocked` from `POST /login` (`server/Auth.js:272-312`) and `GET /api/users/:id` (`server/controllers/UserController.js:59-105`) | Role mapping per `OQ-0040-7` (not chosen here); ABS `isLocked: true` or `isActive: false` -> refused (`403 Forbidden` RFC 9457 problem) | Provisioned on first login + refreshed by revalidator | **Proposed** (`OQ-0040-7`) | after OQ-0040 answers |
| **2b** | **Background identity & permission revalidation** | `GET /api/users/:id` (`server/controllers/UserController.js:59-105`) or `GET /api/users` (`server/controllers/UserController.js:29-49`) using SnatchArr's encrypted admin/service ABS API key (`apiKeys`, `v2.26.0+`) | New `api/internal/audiobookshelf.Revalidator` mirroring `api/internal/jellyfin/revalidator.go:53-163` and `api/internal/auth/jellyfin_ticker.go:19-105`, registered in `leader_loops` via `gate.Provide`; revokes SnatchArr sessions if ABS user is deleted, locked, or deactivated; syncs `external_identities.metadata` (`librariesAccessible`, `accessExplicitContent`, `itemTagsSelected`, `selectedTagsNotAccessible`) | Leader replica background loop (default every `15m`) | **Proposed** (`OQ-0040-4`, `OQ-0040-5`) | after OQ-0040 answers |
| **3a** | **Library availability sync (audiobooks & supplementary ebooks)** | `GET /api/libraries` (`mediaType: 'book'`, `server/controllers/LibraryController.js:188-212`) + paginated `GET /api/libraries/:id/items?limit=100&page=<p>` (`server/controllers/LibraryController.js:604-639`) | Planned `media_availability` cache in Postgres ([ADR-0036](0036-community-requests-and-voting.md)), keyed by `(server_kind='audiobookshelf', server_id, library_id, media_kind, external_id)` matching `asin`, `isbn`, and normalised `(title, author)` (`Book.js:83-100`); respects caller's `librariesAccessible` when badging discovery cards | All authenticated users (filtered to accessible libraries) | **Proposed** (`OQ-0040-3`, `OQ-0040-5`) | after OQ-0040 answers |
| **3b** | **Library availability sync (podcasts & episodes)** | `GET /api/libraries` (`mediaType: 'podcast'`) + `GET /api/libraries/:id/podcast-titles` (`server/controllers/LibraryController.js:1386-1413`) + `GET /api/items/:id?expanded=1` (`Podcast.js:29,37`, `PodcastEpisode.js:207-220`) | Planned `media_availability` rows keyed by `itunesId`, canonical `feedURL`, and episode `guid` / `enclosure.url`; badges subscribed podcasts and downloaded episodes in SnatchArr discovery | All authenticated users (filtered to accessible libraries) | **Proposed** (`OQ-0040-1`, `OQ-0040-3`) | after OQ-0040 answers |
| **4** | **Request submission & community voting UI** | External metadata search via SnatchArr's metadata proxy or ABS `GET /api/search/books` & `GET /api/search/podcast` (`server/controllers/SearchController.js:85-156`); ABS has **no** UI plugin system | SnatchArr SPA (`web/src/routes/requests/`, `web/src/routes/discover/`) and the SnatchArr Jellyfin plugin ([ADR-0028](0028-jellyfin-plugin.md), [ADR-0035](0035-jellyfin-integration.md)), both with deep links to the item in the adopter's ABS web UI; nothing is patched into ABS (answer `OQ-0040-3`, ledger `snatcharr-oq-0040-3`); `POST /api/v1/requests`, `POST /api/v1/requests/{id}/vote`, `DELETE /api/v1/requests/{id}/vote` ([ADR-0036](0036-community-requests-and-voting.md)) supporting `media_kind = 'audiobook'` and `media_kind = 'podcast'` | `user` (request + vote within quota), `trusted_requester` (auto-approved), `admin` (approve/deny/Prioritise) | **Proposed** (`OQ-0040-3`) | after OQ-0040 answers |
| **5a** | **Request fulfilment — Audiobooks** | After file import into the audiobook library folder, SnatchArr calls `POST /api/libraries/:id/scan` (`server/controllers/LibraryController.js:1262-1274`) and confirms presence via `GET /api/libraries/:id/items` | Routes approved `audiobook` requests through the book grab path chosen in [ADR-0036](0036-community-requests-and-voting.md) `OQ-0036-14` (`snatcharr-oq-0036-14-grab-path`: Chaptarr / Bookshelf `*arr` instance vs native SnatchArr Prowlarr search); debits 1 search per book/author dispatch against the instance's hourly cap (`AGENTS.md` Contracts); transitions request to `available` once ABS scan indexes the `book` item | Leader planner + Rust `snatch-worker` (for `*arr`/indexer searches) + Go ABS client (scan trigger) | **Proposed** (gated on `OQ-0036-14`) | after OQ-0040 answers |
| **5b** | **Request fulfilment — Podcasts** | `POST /api/podcasts/feed` (`PodcastController.js:200-216`) -> `POST /api/podcasts` (`PodcastController.js:40-189`) -> `GET /api/podcasts/:id/checknew` or `POST /api/podcasts/:id/download-episodes` (`PodcastController.js:284-301,367-380`) | Per `OQ-0040-1`:SnatchArr calls ABS with the admin/service API key to create the podcast in the target ABS `podcast` library (`libraryId`, `folderId`, sanitised `path` inside `folder.path`) and triggers episode download inside ABS (or hands off to an indexer per `OQ-0040-1`) | Go control plane (`api/internal/audiobookshelf`) on request approval | **Proposed** (`OQ-0040-1`) | after OQ-0040 answers |
| **6** | **Notifications (outbound lifecycle + optional ABS Apprise ingest)** | ABS Apprise webhook (`NotificationSettings.appriseApiUrl`, `server/managers/NotificationManager.js:222-234`) emitting `onPodcastEpisodeDownloaded`, `onRSSFeedFailed`, `onRSSFeedDisabled` (`server/utils/notifications.js:4-94`) | SnatchArr's `api/internal/notify.Engine` (`api/internal/notify/engine.go:30-165`) and SSE (`snatcharr.v1.EventType`) notify the requester and voters on `request_submitted`, `request_approved`, `request_denied`, `request_available`, `request_failed`; per `OQ-0040-6`, optional inbound webhook endpoint or periodic poll reconciles ABS episode downloads and RSS feed failures | Requester, voters (opted-in), and `admin` (on RSS failure) | **Proposed** (`OQ-0040-6`) | after OQ-0040 answers |
| **7** | **Explicit-content & adult-module fencing** | `user.permissions.accessExplicitContent` (`server/models/User.js:178,557-559,678-680`), `book.explicit` (`Book.js:104`), `podcast.explicit` (`Podcast.js:45`), `itemTagsSelected` / `selectedTagsNotAccessible` (`User.js:658-667`) | Coupled gate (answer `OQ-0040-5` **b**, ledger `snatcharr-oq-0040-5`, against the recommendation): an ABS item or search result with `explicit == true` follows SnatchArr's adult grant: it is shown in discovery, availability and requests only to a user who holds the per-user adult grant planned in ADR-0026 (not yet in the data model, so the gate fails closed until it exists). ABS denied tags still hide items for the linked ABS user. Adult-module content ([ADR-0027](0027-adult-module.md)) is never unlocked by ABS `accessExplicitContent == true` | Enforced in `api/internal/httpapi` and request/discovery services | **Answered** (`OQ-0040-5`) | after OQ-0040 answers |
| **8** | **Listening progress & watch-aware automation (`watchaware`)** | `GET /api/users/:id` (`mediaProgress` array, `UserController.js:59-105`), `GET /api/libraries/:id/series/:seriesId?include=progress` (`LibraryController.js:778-797`), `GET /api/sessions` (`SessionController.js:29-112`) | New `api/internal/watchaware/audiobookshelf.go` implementing `watchaware.Source` (`api/internal/watchaware/source.go:25-63`) alongside `jellyfin.go` (`api/internal/watchaware/jellyfin.go:18-45`); feeds `watchaware.Syncer` (`sync.go:30-95`) and `Predicate` (`predicate.go`) so completing book $N$ in a series (`isFinished == true` or `progress * 100 >= threshold`) triggers sequential snatching of book $N+1$ | Leader `watchaware.Syncer` loop (`leader_loops`) | **Proposed** (`OQ-0040-8`) | after OQ-0040 answers |

### Reuse architecture (HISS-19: one behaviour, one implementation)

Per `AGENTS.md` **HISS-19** (*one behavior = one implementation; extend or call existing code*), the Audiobookshelf integration reuses SnatchArr's existing authentication, identity linking, watch-state, and notification subsystems rather than introducing parallel pipelines:

1. **`github.com/golusoris/goenvoy/mediaserver/audiobookshelf/v2` (`go.mod`)**:
   - Extend the existing upstream `mediaserver/audiobookshelf` package in `golusoris/goenvoy` (currently `v2.0.2`) to fix `GetStatus` (`GET /status`), `GetUsers` (`{"users": [...]}` envelope), `GetUser` (`GET /api/users/{id}` with `Permissions`, `LibrariesAccessible`, `ItemTagsSelected`, `MediaProgress`), `Login` (`POST /login` with `x-return-tokens: true` + `POST /logout`), paginated `GetLibraryItems`, and podcast endpoints (`ParsePodcastFeed`, `CreatePodcast`, `CheckNewEpisodes`, `DownloadEpisodes`, `SearchPodcasts`), then bump `go.mod` in SnatchArr.
2. **`api/internal/auth` (`identity.go`, `service.go`, `jellyfin_ticker.go`) and `api/internal/httpapi` (`handlers_identities.go`, `handlers_jellyfin.go`)**:
   - `api/internal/auth/identity.go` (`LoginExternal`, `LinkIdentity`, `UnlinkIdentity`, `RevalidateExternalUser`) is already provider-agnostic—it keys on `(provider, external_id)` in `external_identities`.
   - Generalise or parameterise `api/internal/auth/jellyfin_ticker.go:19-105` into a provider-keyed external-identity revalidation ticker (or register an `audiobookshelf` `Revalidator` alongside `jellyfin.Revalidator` in `leader_loops` via `gate.Provide`).
   - Reuse the account-linking and login handler patterns in `api/internal/httpapi/handlers_identities.go:25-115` and `api/internal/httpapi/handlers_jellyfin.go:22-120`, returning RFC 9457 `application/problem+json` on every non-2xx response.
3. **`api/internal/watchaware` (`source.go`, `jellyfin.go`, `sync.go`, `predicate.go`, `module.go`)**:
   - Implement `watchaware.Source` (`Kind() string`, `FetchWatchState(ctx context.Context) ([]Record, error)`, `api/internal/watchaware/source.go:25-63`) in `api/internal/watchaware/audiobookshelf.go`, translating ABS `MediaProgress` (`progress * 100` -> `PlayedPercentage`, `isFinished` -> `Completed`, `lastUpdate` -> `LastPlayedAt`) into `watchaware.Record` rows stored in the existing watch-state table via `watchaware.Syncer` (`api/internal/watchaware/sync.go:30-95`).
4. **`api/internal/notify` (`engine.go`, `apprise.go`) and `api/internal/domain/notification.go`**:
   - Reuse `notify.Engine` (`api/internal/notify/engine.go:30-165`) and existing notification channel encryption (`core/crypto` AES-GCM) for request lifecycle notifications.
5. **Requests and voting (`docs/adr/0036-community-requests-and-voting.md`)**:
   - Share the `media_requests`, `media_request_votes`, `media_request_events`, and `media_availability` tables planned in [ADR-0036](0036-community-requests-and-voting.md), adding `audiobookshelf` as a `server_kind` and `audiobook` / `podcast` as `media_kind` values.

---

## Answered questions

The operator answered `OQ-0040-1` to `OQ-0040-8` on 2026-10-26. Each answer is recorded in the decision ledger under the id shown; the wording is a paraphrase. Where an answer differs from the recommendation in the question list that follows, the answer binds.

| ID | Answer | Ledger id |
| :--- | :--- | :--- |
| `OQ-0040-1` | **a**: podcasts only through ABS RSS subscription; a request for a podcast without a feed is refused with `application/problem+json`. | `snatcharr-oq-0040-1` |
| `OQ-0040-2` | **c**, against the recommendation: ephemeral ABS `POST /login` proof followed by `POST /logout`, storing only the ABS user id, plus an OIDC subject match when ABS and SnatchArr share an identity provider (optional, never assumed). | `snatcharr-oq-0040-2` |
| `OQ-0040-3` | Neither offered option: requests and votes for audiobooks and podcasts live in the SnatchArr Jellyfin plugin as well as the SnatchArr web app, with deep links to ABS; nothing is patched into ABS. | `snatcharr-oq-0040-3` |
| `OQ-0040-4` | **a**: require ABS 2.26.0 or newer and a scoped, revocable API key, AES-GCM encrypted at rest; older servers are refused after `GET /status`. | `snatcharr-oq-0040-4` |
| `OQ-0040-5` | **b**, against the recommendation: coupled; ABS explicit content follows the SnatchArr adult grant. | `snatcharr-oq-0040-5` |
| `OQ-0040-6` | **b**: an authenticated inbound webhook as ABS Apprise target, with polling as fallback. | `snatcharr-oq-0040-6` |
| `OQ-0040-7` | **a**: least privilege; first-admin bootstrap only on an uninitialised instance; ABS user maps to the configurable default role; guest refused; no automatic admin. | `snatcharr-oq-0040-7` |
| `OQ-0040-8` | **a**: a finished book or a progress threshold queues the next book in the series; podcast pruning stays in ABS, SnatchArr sends no `DELETE` to ABS. | `snatcharr-oq-0040-8` |

## Open questions

The list keeps the options and risks the answers above chose from. `OQ-0040-9` is new: the answers to `OQ-0040-2` and `OQ-0040-7` assume a sign-in through ABS, while [ADR-0035](0035-jellyfin-integration.md) makes Jellyfin sign-in the only user path.

- `OQ-0040-9 | Is Audiobookshelf a sign-in path next to Jellyfin, or only an account to link? | a) link only: users sign in with Jellyfin (ADR-0035) and link their ABS account from the account page with the OQ-0040-2 proof; OQ-0040-7 role mapping and bootstrap apply to no ABS sign-in (risk: an adopter who runs ABS without Jellyfin has no user sign-in) b) ABS sign-in as a second user path for adopters without Jellyfin, with OQ-0040-7 mapping and bootstrap; Jellyfin stays the only path where Jellyfin is configured (risk: two sign-in paths to test and secure; departs from the Jellyfin-only direction) c) ABS sign-in everywhere next to Jellyfin (risk: same account may exist twice; merges only through the narrowed OQ-0035-3 proof) | recommended: a, keeps the single user path of ADR-0035; adopters without Jellyfin are a separate question`

- `OQ-0040-1 | How should SnatchArr fulfil approved podcast requests for Audiobookshelf? | a) Native ABS RSS subscription only: SnatchArr calls POST /api/podcasts/feed and POST /api/podcasts (plus GET /api/podcasts/:id/checknew or POST /api/podcasts/:id/download-episodes) so ABS subscribes to the RSS feed and downloads episodes directly on its own schedule (risk: private or delisted podcast archives without an active HTTP RSS feed cannot be fetched via RSS) b) Indexer handoff only: SnatchArr searches Prowlarr indexers for podcast archives and imports audio files into the ABS library folder followed by POST /api/libraries/:id/scan (risk: bypasses ABS's built-in RSS episode tracker, auto-download cron, and episode retention rules for standard RSS podcasts) c) Hybrid RSS-first with indexer fallback: subscribe via POST /api/podcasts when the request carries a valid RSS feedUrl or iTunes podcast match, and allow admins to route archive-only podcast requests without a live RSS feed through Prowlarr indexer search (risk: two fulfilment paths to test and maintain) | recommended: a, standard podcasts in ABS are RSS-native and ABS's own PodcastManager handles episode downloading, tagging, and retention without consuming *arr indexer budget`

- `OQ-0040-2 | How should SnatchArr verify an Audiobookshelf user during sign-in and account linking? | a) Ephemeral credential proof via POST /login with header x-return-tokens: true followed immediately by POST /logout with x-refresh-token, persisting only the immutable ABS user UUID (user.id) and discarding both password and session tokens (risk: users whose ABS account is OIDC-only with pash == null cannot authenticate via POST /login and must sign in to SnatchArr via the shared OIDC provider or link via option b) b) User-supplied personal ABS API key verified via GET /api/me (risk: in ABS v2.37.2, POST /api/api-keys (ApiRouter.js:339) passes ApiKeyController.middleware (ApiKeyController.js:197-202), which answers 403 unless isAdminOrUp, so regular ABS users cannot mint their own scoped API keys in the ABS UI without an admin) c) Support POST /login ephemeral proof as the primary flow for all ABS users, plus admin-assisted or OIDC sub/email matching when both SnatchArr and ABS use OIDC, and never link or merge accounts by unverified username string (risk: slightly broader linking surface to test across local and OIDC accounts) | recommended: a, works for every standalone ABS user without admin intervention, stores zero ABS user credentials or session tokens, and immediately cleans up the verification session via POST /logout`

- `OQ-0040-3 | Where should the audiobook and podcast discovery, request submission, and voting UI live? | a) SnatchArr SPA only (web/src/routes/discover/ and web/src/routes/requests/), with deep links back to the item in the adopter's ABS web UI once available (risk: listeners switch to the SnatchArr web app to request or vote rather than clicking a button inside the ABS web client) b) SnatchArr SPA as primary UI plus an optional Custom Metadata Provider endpoint in SnatchArr implementing ABS's custom-metadata-provider-specification.yaml purely for admin match lookups (risk: ABS's Custom Metadata Provider API is only invoked by admins matching existing library items in ABS and cannot submit requests or votes for end users) | recommended: a, ABS v2.37.2 has no server plugin loader or client UI extension runtime, so the SnatchArr SPA is the only surface that works for every adopter without patching the upstream ABS container`

- `OQ-0040-4 | What minimum Audiobookshelf server version should SnatchArr support, and how should the service credential be authenticated? | a) Require Audiobookshelf >= v2.26.0 (verified via GET /status serverVersion) and require a scoped ABS API key (created in ABS Settings -> API Keys, stored in apiKeys table from migration v2.26.0-create-auth-tables.js) encrypted at rest with core/crypto AES-GCM (risk: adopters on ABS releases older than v2.26.0 must upgrade ABS before connecting it to SnatchArr) b) Support ABS >= v2.3.0 by accepting either a v2.26.0+ scoped API key or a legacy static user.token JWT (risk: legacy user.token has no expiry, is marked @deprecated in TokenManager.js:111, and is hidden for root when queried by non-root admins) | recommended: a, ABS v2.26.0 introduced revocable, expiring apiKeys and session-backed POST /login tokens; pinning >= v2.26.0 avoids deprecated non-expiring user.token tokens and matches the fleet rule to target current upstream versions`

- `OQ-0040-5 | How should Audiobookshelf's explicit-content flag (permissions.accessExplicitContent, book.explicit, podcast.explicit) and tag restrictions interact with SnatchArr's adult module (ADR-0027)? | a) Strict two-layer separation: ABS explicit == true represents standard publisher parental-advisory metadata (strong language or mature themes in mainstream audiobooks/podcasts) and is gated per user by their synced ABS permissions.accessExplicitContent and itemTagsSelected/selectedTagsNotAccessible flags; SnatchArr's adult module (ADR-0027) stays a separate opt-in gate (adult_enabled + per-user adult_access) that is never auto-unlocked by ABS accessExplicitContent (risk: an admin who puts adult audiobooks inside an ABS library must also restrict that ABS library via librariesAccessible or tags in ABS) b) Coupling ABS explicit == true to SnatchArr's ADR-0027 adult_access flag (risk: blocks mainstream novels and podcasts marked explicit by Audible/iTunes—such as standard thrillers or comedy podcasts—whenever the ADR-0027 adult module is disabled) | recommended: a, preserves ADR-0027's zero-leak isolation for adult instances while respecting ABS's native parental-advisory and tag controls for mainstream audiobooks and podcasts`

- `OQ-0040-6 | How should SnatchArr handle notifications between SnatchArr and Audiobookshelf? | a) SnatchArr notify.Engine only for request lifecycle events (request_submitted, request_approved, request_denied, request_available, request_failed), plus periodic library/podcast polling against ABS to detect newly available books and episodes (risk: availability transition latency equals the poll interval, e.g. 5-15 minutes, when an episode is auto-downloaded by ABS) b) Bi-directional: SnatchArr notify.Engine delivers all user/admin request lifecycle notifications, AND SnatchArr exposes an authenticated webhook receiver (POST /api/v1/webhooks/audiobookshelf/{instanceId}) that ABS can target as an Apprise API URL (or alongside Apprise) to ingest onPodcastEpisodeDownloaded, onRSSFeedFailed, and onRSSFeedDisabled immediately, with periodic polling as fallback (risk: ABS only allows a single appriseApiUrl in NotificationSettings.js:9, so adopters who already point ABS at an external Apprise server would either forward from Apprise or rely on SnatchArr's polling fallback) | recommended: b, gives instant episode-availability and RSS-failure alerts when pointed at SnatchArr while keeping periodic polling as a zero-config fallback when appriseApiUrl is already in use`

- `OQ-0040-7 | How should SnatchArr map Audiobookshelf account types (root, admin, user, guest) to SnatchArr roles on sign-in, including first-admin bootstrap? | a) Map ABS root and admin to SnatchArr user by default (never auto-grant SnatchArr admin after initial setup), allow first-user admin bootstrap on an uninitialised SnatchArr instance only when the ABS account has type == 'root' or 'admin', map ABS user to configurable default role (user or trusted_requester), and always reject ABS guest (risk: secondary ABS admins must be promoted to admin inside SnatchArr by the initial SnatchArr admin) b) Automatically map every ABS root and admin account to SnatchArr admin on every login (risk: any admin compromise or over-privileged account in ABS automatically gains full control-plane admin access over all *arr instances in SnatchArr) | recommended: a, enforces least privilege across application boundaries while still allowing a single-maintainer adopter to bootstrap SnatchArr with their ABS root/admin account`

- `OQ-0040-8 | How should watchaware interpret Audiobookshelf listening progress for sequential series prefetching and podcast retention? | a) Sync book mediaProgress (UserController.js:59-105) and library series progress (LibraryController.js:778-797) into watchaware so book N triggers book N+1 when isFinished == true or progress * 100 >= library.settings.markAsFinishedPercentComplete (default 95%), while leaving podcast episode deletion to ABS's native maxEpisodesToKeep setting (PodcastManager.js:222-244) (risk: podcast episode retention is configured in ABS per podcast rather than in SnatchArr's watchaware policy UI) b) Have SnatchArr also delete listened podcast episodes via DELETE /api/podcasts/:id/episode/:episodeId based on podcastEpisode mediaProgress (risk: conflicts with ABS's own maxEpisodesToKeep logic and multi-listener household progress) | recommended: a, leverages watchaware where SnatchArr owns the snatch pipeline (next audiobook in a series) and delegates podcast episode pruning to ABS's built-in PodcastManager`

---

## Consequences

### Positive

- **Feature parity across media servers**: adopters whose primary household media server is Audiobookshelf gain the same sign-in, account linking, request, voting, availability, notification, and watch-aware experience as Jellyfin adopters ([ADR-0035](0035-jellyfin-integration.md)).
- **Zero extra infrastructure for any adopter**: works in a plain `docker compose` setup without deploying an OIDC identity provider, reverse proxy auth layer, or separate request tool.
- **Native podcast handling without wasting `*arr` indexer caps**: when `OQ-0040-1` option `a` or `c` is selected, podcast subscriptions and episode downloads execute through Audiobookshelf's built-in RSS manager (`POST /api/podcasts`) rather than consuming hourly `*arr` indexer search budgets.
- **High code reuse (HISS-19)**: reuses `external_identities`, `auth.Service.LoginExternal`, `watchaware.Source`, `notify.Engine`, and the `golusoris/goenvoy/mediaserver/audiobookshelf/v2` client.

### Negative and trade-offs

- **Upstream `golusoris/goenvoy` release required first**: `mediaserver/audiobookshelf/v2.0.2` must be updated (`v2.1.0`) to fix `GetStatus`, `GetUsers` envelope decoding, `User` permissions/progress structs, `Login`/`Logout`, and podcast endpoints before SnatchArr's `api/internal/audiobookshelf` package can be wired.
- **No in-app ABS UI button**: because Audiobookshelf `v2.37.2` has no server or client plugin runtime, users submit requests and votes in the SnatchArr SPA rather than inside the Audiobookshelf web interface.
- **Audiobook fulfilment depends on `OQ-0036-14`**: end-to-end automated grabbing of requested audiobooks waits on the book grab path decision in [ADR-0036](0036-community-requests-and-voting.md) (`snatcharr-oq-0036-14-grab-path`).

---

## Alternatives considered

1. **Keep Audiobookshelf as availability-detection-only ([ADR-0036](0036-community-requests-and-voting.md) `OQ-0036-9` original wording)**:
   - *Rejected* by operator direction (`snatcharr-audiobookshelf-full-integration-20261026`). Audiobook and podcast listeners in a household need sign-in, requests, voting, notifications, and sequential series prefetching just as video viewers do in Jellyfin.
2. **Require an external OIDC provider (Authentik, Authelia, Keycloak) for Audiobookshelf sign-in**:
   - *Rejected*. Violates the any-adopter constraint: many home-server adopters run Audiobookshelf with built-in local accounts (`POST /login`) and no external IdP.
3. **Fork or patch the Audiobookshelf Vue/Nuxt frontend container to inject request buttons**:
   - *Rejected*. Maintaining a custom-patched ABS container image breaks standard adopter deployments and violates clean component boundaries.
4. **Build a bespoke HTTP client inside `api/internal/audiobookshelf` instead of extending `golusoris/goenvoy`**:
   - *Rejected*. Violates `AGENTS.md` **HISS-19** (*one behavior = one implementation*); `github.com/golusoris/goenvoy/mediaserver/audiobookshelf/v2` already exists and is the single home for typed media-server HTTP clients.

---

## Verification plan

Every surface in the decision table must ship with 3D table-driven tests (**HISS-15**: positive, negative, boundary) in `api/internal/audiobookshelf`, `api/internal/watchaware`, `api/internal/httpapi`, and `web/tests/e2e`:

| Surface | Positive test | Negative test (incl. planted failure case) | Boundary test |
| :--- | :--- | :--- | :--- |
| **0. Service key & version probe (`GET /status`, `GET /api/me`)** | Valid v2.26.0+ ABS API key and `serverVersion: "2.37.2"` succeed and persist AES-GCM encrypted key | **Planted negative case**: wrong/revoked ABS token returns `401 Unauthorized` from ABS and is refused by SnatchArr with `401`/`422` `application/problem+json` (never stored, never panics, never leaks token in logs or problem body) | Minimum supported `serverVersion` boundary (`2.26.0` accepted; `2.25.9` refused when `OQ-0040-4` option `a` is active) |
| **1. Sign-in & account linking (`POST /login` + `POST /logout`)** | Valid ABS `user` credentials with `x-return-tokens: true` create/match `external_identities` row (`provider='audiobookshelf'`), issue `snatcharr_session`, and call `POST /logout` with `x-refresh-token` | Wrong ABS password (`401`), locked ABS user (`isLocked: true` -> `403`), inactive ABS user (`isActive: false` -> `403`), or `type: "guest"` (`403`) refused with `application/problem+json`; username collision with an unlinked SnatchArr account never auto-merges | Empty username/password (`400`), rate-limited login endpoint (`429`), and re-linking an already linked ABS `user.id` to a different SnatchArr user (`409 Conflict`) |
| **2. Background revalidator (`GET /api/users/:id`)** | Active ABS user refreshes cached `librariesAccessible`, `accessExplicitContent`, and tag permissions | ABS user deleted (`404`) or locked (`isLocked: true`) on revalidation immediately revokes all active SnatchArr sessions for that user | Transient ABS network timeout (`context.DeadlineExceeded`) logs warning and preserves existing sessions until next tick |
| **3. Library availability (books & podcasts)** | Paginated `GET /api/libraries/:id/items` indexes books by `asin`/`isbn` and podcasts by `itunesId`/`feedURL`/episode `guid` | User without library ID in `librariesAccessible` cannot see availability badges or library items from that restricted ABS library | Empty library (`total: 0`), book with neither `asin` nor `isbn` (falls back to normalised title+author), and multi-edition audiobooks |
| **4. Explicit-content & adult fencing** | User with `accessExplicitContent: true` sees and can request `explicit: true` audiobooks and podcasts | User with `accessExplicitContent: false` or denied tag (`selectedTagsNotAccessible: true`) is refused (`403`) when requesting an explicit or tag-denied item; user with `accessExplicitContent: true` but `adult_access: false` is refused (`403` / hidden) on [ADR-0027](0027-adult-module.md) adult endpoints | Item with `explicit: false` and empty `tags: []` when user has allowlist tag mode (`accessAllTags: false`, `selectedTagsNotAccessible: false` -> denied per `User.js:665`) |
| **5. Podcast & audiobook fulfilment** | Approved podcast request validates RSS via `POST /api/podcasts/feed`, creates podcast via `POST /api/podcasts` inside library folder, and queues episodes | RSS feed returning invalid XML or podcast `path` outside `folder.path` (`../` traversal attempt) is rejected (`400`/`422`) before calling ABS | Podcast already present at target path (`409` / marks existing item available without duplicate creation) |
| **6. Notifications & webhook ingest** | `request_available` dispatches through `notify.Engine`; valid `onPodcastEpisodeDownloaded` webhook marks waiting podcast request available | Webhook request with invalid/missing shared secret or unknown `instanceId` is rejected with `401`/`404` `application/problem+json` | `onRSSFeedDisabled` after `numFailed: 5` emits admin alert once without duplicate storm |
| **7. Watch-aware progress (`watchaware`)** | Book `mediaProgress` with `isFinished: true` or `progress: 0.96` (`>= 95%`) emits `watchaware.Record` with `Completed: true` and triggers next book in series | Unauthenticated or non-admin ABS key returning `403` on `GET /api/users/:id` surfaces wrapped error (`%w`) without corrupting watch-state cache | Book with `duration: 0` or `progress: 0.949` (just below `95%` threshold) remains `Completed: false` |
| **8. SPA accessibility (WCAG 2.2 AA)** | Playwright + `axe-core` suite on ABS login, account linking, discovery, and request routes passes with zero violations (`en` and `de` Paraglide locales, `oklch` colours) | Form validation error states (`application/problem+json` toast/inline alert) keep `aria-invalid` and focus order intact | Keyboard-only navigation across audiobook/podcast request modal and vote toggle buttons |

---

## Follow-up slices

Milestones are assigned after the `OQ-0040` answers (step 3 of `snatcharr-audiobookshelf-full-integration-20261026`); the milestone issue then lists these slices. Each slice is one verifiable pull request.

1. **goenvoy client completion.** The six gaps listed in the survey are fixed in `golusoris/goenvoy` `mediaserver/audiobookshelf/v2`. SnatchArr does not open that work itself: it goes to the fleet controller as an ASK line, and SnatchArr bumps `go.mod` once a tagged release carries the fixes. Every slice below waits for it.
2. **Listening progress source.** `api/internal/watchaware/audiobookshelf.go` implements `watchaware.Source` over `mediaProgress` and series progress (`OQ-0040-8`), with positive, negative and boundary tests including the planted wrong-token case.
3. **Sign-in, account linking and revalidation.** `api/internal/audiobookshelf/` mirrors `api/internal/jellyfin/` (authenticator, revalidator in `leader_loops`), with the OpenAPI operations, the SPA login and linking screens in `en` and `de`, and Playwright plus axe-core tests (`OQ-0040-2`, `OQ-0040-4`, `OQ-0040-7`).
4. **Availability, requests and votes.** Book and podcast libraries feed the ADR-0036 availability table with per-user library, explicit-content and tag filters (`OQ-0040-3`, `OQ-0040-5`); the request and vote buttons ship in the SnatchArr web app and the SnatchArr Jellyfin plugin; audiobook requests use the grab path of `snatcharr-oq-0036-14-grab-path`.
5. **Podcast fulfilment and notifications.** The podcast path chosen in `OQ-0040-1` and the notification path chosen in `OQ-0040-6`.
6. **Explicit-content and adult fencing tests.** Integration tests that an ABS item with `explicit == true` reaches only users with the SnatchArr adult grant, and nobody while the grant is missing from the data model (`OQ-0040-5`, coupled).

---

## References

- [`github.com/advplyr/audiobookshelf`](https://github.com/advplyr/audiobookshelf) tag `v2.37.2` (commit `edb1f7efc92ecacdef51aafb4d9d5054ce10604f`, `2026-10-23`, `GPL-3.0-only`):
  - `server/Server.js:263-267,343-405`
  - `server/Auth.js:88-127,230-359,362-514`
  - `server/auth/TokenManager.js:17-21,111-198,206-335`
  - `server/routers/ApiRouter.js:64-362`
  - `server/models/User.js:120-183,214-341,545-831`
  - `server/models/ApiKey.js:1-147`
  - `server/models/Book.js:77-220`
  - `server/models/Podcast.js:14-220`
  - `server/models/PodcastEpisode.js:11-220`
  - `server/models/MediaProgress.js:5-220`
  - `server/controllers/ApiKeyController.js:22-189`
  - `server/controllers/UserController.js:29-105`
  - `server/controllers/MeController.js:27-115,239-245`
  - `server/controllers/LibraryController.js:188-233,604-639,778-797,959-969,1262-1274,1386-1413`
  - `server/controllers/PodcastController.js:40-380`
  - `server/controllers/SearchController.js:10-29,85-236`
  - `server/controllers/SessionController.js:29-142`
  - `server/controllers/NotificationController.js:13-153`
  - `server/managers/PodcastManager.js:25-350`
  - `server/managers/NotificationManager.js:7-236`
  - `server/objects/Notification.js:3-133`
  - `server/objects/settings/NotificationSettings.js:5-118`
  - `server/utils/notifications.js:3-110`
  - `server/SocketAuthority.js:154-250`
  - `server/migrations/v2.26.0-create-auth-tables.js:10-163`
  - `server/migrations/v2.35.0-add-last-refresh-token.js:1-60`
  - `custom-metadata-provider-specification.yaml:1-140`
- [`github.com/golusoris/goenvoy`](https://github.com/golusoris/goenvoy) tag `mediaserver/audiobookshelf/v2.0.2` (commit `17183e338c983b01d497a42c0a76b82c7a3b1e53`, `2026-10-26`, `MIT`):
  - `mediaserver/audiobookshelf/audiobookshelf.go:1-342`
  - `mediaserver/audiobookshelf/types.go:1-225`
  - `mediaserver/audiobookshelf/transport.go:1-46`
  - `mediaserver/audiobookshelf/CHANGELOG.md:1-35`
- SnatchArr repository references:
  - [ADR-0024: Watch-Aware Modes](0024-watch-aware-modes.md)
  - [ADR-0026: Accounts, Roles, and Authentication](0026-accounts-and-roles.md)
  - [ADR-0027: Optional Adult Module](0027-adult-module.md)
  - [ADR-0028: Jellyfin Plugin Architecture](0028-jellyfin-plugin.md)
  - [ADR-0035: Jellyfin Full Integration](0035-jellyfin-integration.md)
  - [ADR-0036: Community Requests and Voting](0036-community-requests-and-voting.md) (`OQ-0036-9`, `OQ-0036-14`)
  - `api/internal/jellyfin/{authenticator,revalidator,client}.go`
  - `api/internal/auth/{identity,service,jellyfin_ticker}.go`
  - `api/internal/httpapi/{handlers_jellyfin,handlers_identities,security,security_sso}.go`
  - `api/internal/watchaware/{source,jellyfin,sync,predicate,module}.go`
  - `api/internal/notify/{engine,apprise}.go`
  - `docs/roadmap.md`, `docs/readarr.md`, `docs/notifications.md`
