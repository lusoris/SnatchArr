<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0026: Accounts, roles and Jellyfin sign-in

- Status: proposed
- Date: 2026-10-24
- Related: [ADR-0018](0018-product-front-door.md), [ADR-0023](0023-sso-viewer-role.md), [ADR-0024](0024-watch-aware-modes.md), [ADR-0025](0025-module-switches.md)

## Context

SnatchArr operates as the unified front door of the media stack ([ADR-0018](0018-product-front-door.md)). ADR-0018 transitions SnatchArr from a single-operator scheduler into a multi-user application serving household members, media requesters, and automated systems (`snatcharr-product-users-20261020`). ADR-0018 establishes three user roles—`admin`, `requester`, and `viewer`—and mandates that user identities, roles, and adult media access derive from Jellyfin users and existing single sign-on (SSO) infrastructure (`snatcharr-product-accounts-20261020`, `snatcharr-product-adult-visibility-20261020`).

### Current implementation baseline

The existing security architecture accommodates local password login and single sign-on via OIDC and reverse proxy authentication ([ADR-0023](0023-sso-viewer-role.md)), but only recognizes two roles (`admin` and `viewer`) and does not integrate with media server authentication:

1. **Role representation and OpenAPI contract**:
   - Roles are represented by `domain.Role` (`RoleAdmin = "admin"`, `RoleViewer = "viewer"`) in `api/internal/domain/user.go:17-23`.
   - OpenAPI schemas define `role` as `enum: [admin, viewer]` on `User` (`api/openapi/openapi.yaml:1839-1840`).
   - Domain user struct `domain.User` (`api/internal/domain/user.go:26-32`) holds `ID`, `Username`, `Role`, `CreatedAt`, and `UpdatedAt`.
2. **Local authentication and user store**:
   - Initial application setup creates a single administrator via `auth.Service.Setup` (`api/internal/auth/service.go:102-104`).
   - User credentials and sessions are persisted in PostgreSQL: `users` table (`api/internal/store/migrations/postgres/000001_init.up.sql:4-12`) and `sessions` table (`api/internal/store/migrations/postgres/000001_init.up.sql:14-19`).
   - Migration `000026_user_external_id.up.sql:6` added `ALTER TABLE users ADD COLUMN external_id TEXT NULL UNIQUE` to support external identity mapping.
   - `auth.Service.Login` (`api/internal/auth/service.go:133-162`) authenticates against argon2id password hashes and enforces lockout counters (`api/internal/store/queries/auth.sql:86-136`).
3. **SSO authentication and session origins ([ADR-0023](0023-sso-viewer-role.md))**:
   - OIDC callback processing in `sso.Service.completeCallback` (`api/internal/sso/oidc.go:235-273`) extracts user groups from the ID token claim and resolves the role via `sso.ResolveRole` (`api/internal/sso/role.go:17-31`).
   - Role assignment maps admin identities to the existing first admin record via `auth.Service.FirstAdmin` (`api/internal/sso/oidc.go:276-282`). Viewer identities are provisioned via `auth.Service.ProvisionExternalViewer` (`api/internal/auth/service.go:236-258`), creating a synthetic username `oidc-<hash>` and recording the subject in `users.external_id`.
   - Reverse proxy sign-in in `httpapi.Handlers.CreateProxySession` (`api/internal/httpapi/handlers.go:318-335`) reads trusted headers (`X-authentik-username`, `X-authentik-groups`), resolving admin to `FirstAdmin` and viewers to `proxy-<hash>`.
   - SSO session metadata is captured in `auth.SSOOrigin` (`api/internal/auth/ssoorigin.go:23-36`) with keys `sso_source`, `sso_subject`, `sso_groups`, and `sso_role`.
   - On every request, `httpapi.Security.HandleCookieAuth` (`api/internal/httpapi/security.go:55-86`) evaluates `checkSSOOrigin` (`api/internal/httpapi/security_sso.go:48-63`). If an identity's groups changed or the provider was disabled, the session is revoked (`api/internal/auth/ssoorigin.go:44-50`) and rejected with HTTP 401.
4. **API and SPA role checks**:
   - Administrative enforcement relies on `requireAdmin(ctx)` (`api/internal/httpapi/handlers_instances.go:15-24`), which verifies `u.Role == domain.RoleAdmin` and returns `domain.ErrForbidden` (RFC 9457 HTTP 403 Problem JSON).
   - `requireAdmin` gates mutating endpoints across instances, download clients, Prowlarr links, Seerr links, schedules, notifications, queue management, snatches, and API keys. The live log stream is gated by `adminOnly` middleware (`api/internal/httpapi/module.go:77-90`).
   - The frontend SPA checks `session.isAdmin` (`web/src/lib/session.svelte.ts:22-24`), hiding navigation links (`web/src/lib/components/shell/nav.ts:37-75`) and form controls (`web/src/lib/components/AdminFormSlot.svelte`, `web/src/lib/components/AdminSaveButton.svelte`).

### Problem statement

The current architecture cannot support a multi-user media portal:

1. **Missing `requester` role**: Household members cannot be granted permission to browse catalogues and submit requests without also gaining administrative access to download clients, indexers, and system settings.
2. **No media server sign-in**: Users cannot sign in using their existing Jellyfin credentials, forcing operators to either manage duplicate credentials in Authentik or grant all users direct access to administrative proxies.
3. **No parental and adult filtering**: Access to adult media (`ADR-0018`, `ADR-0025`) cannot be restricted per user; either the adult module is completely hidden, or visible to all authenticated viewers regardless of age restrictions.

## Decision

SnatchArr establishes unified multi-user accounts, adds the `requester` role, and integrates Jellyfin authentication alongside existing SSO and local credentials.

### 1. Jellyfin sign-in architecture and prior art

#### Prior art: Seerr Jellyfin authentication

Seerr (read from [seerr-team/seerr@v3.5.1](https://github.com/seerr-team/seerr/tree/v3.5.1)) provides the battle-tested model for media server authentication:

1. **Credential-based login**:
   - Seerr exposes `POST /api/v1/auth/jellyfin` (`server/routes/auth.ts:246-370`).
   - It formats an authorization header (`MediaBrowser Client="Seerr", Device="Seerr", DeviceId="...", Version="..."`) and calls Jellyfin's `POST /Users/AuthenticateByName` with `{ "Username": username, "Pw": password }` (`server/api/jellyfin.ts:183-242`).
   - Jellyfin responds with an `AuthenticationResult` payload containing `AccessToken`, `User` (`Id`, `Name`, `Policy`), and `SessionInfo`.
   - Seerr inspects `User.Policy.IsAdministrator`. If true, the account receives administrative rights; otherwise, it is assigned `settings.main.defaultPermissions` (`server/routes/auth.ts:339-408`).
   - Seerr records `jellyfinUserId`, `jellyfinUsername`, and `jellyfinDeviceId` on its local `User` entity and initializes an HTTP session cookie.
2. **Quick Connect login**:
   - Seerr initiates passwordless login via `POST /api/v1/auth/jellyfin/quickconnect/initiate` (`server/routes/auth.ts:640`; check at line 676, authenticate at line 716), calling Jellyfin's `POST /QuickConnect/Initiate` to obtain a short code and secret (`server/api/jellyfin.ts:244-263`).
   - The client displays the code. An authenticated device authorizes the code on Jellyfin.
   - Seerr polls `GET /QuickConnect/Connect?secret=...` and completes sign-in via `POST /Users/AuthenticateWithQuickConnect` (`server/api/jellyfin.ts:289-308`), issuing an internal session.

#### Upstream Jellyfin capabilities and group model

Verification against the Jellyfin server source at release [v12.3](https://github.com/jellyfin/jellyfin/blob/v12.3/MediaBrowser.Model/Users/UserPolicy.cs) (`MediaBrowser.Model/Users/UserPolicy.cs`), the API reference ([api.jellyfin.org](https://api.jellyfin.org/)) and the user guide ([jellyfin.org/docs](https://jellyfin.org/docs/general/server/users/)):

1. **Authentication endpoints**:
   - `POST /Users/AuthenticateByName`: Standard username/password authentication returning `AuthenticationResult`.
   - `POST /QuickConnect/Initiate`: Generates code and secret for device authorization.
   - `GET /QuickConnect/Connect`: Checks authorization status.
   - `POST /Users/AuthenticateWithQuickConnect`: Exchanges authorized secret for `AuthenticationResult`.
2. **User groups in Jellyfin**:
   - Jellyfin **does not possess native user groups** in its core domain model or administration interface. All access control, parental limits, and library visibility rules are stored per user account.
   - Access rules reside in `UserPolicy` (`MediaBrowser.Model/Users/UserPolicy.cs`); `UserConfiguration` holds display preferences only and carries no parental field:
     - `UserPolicy.IsAdministrator` (`bool`): Grants full server administrative privileges.
     - `UserPolicy.MaxParentalRating` (`int?`, line 112): highest parental rating score the user may see. `null` (the default, line 53) means unrestricted; `0` is a real restriction, not "unrestricted".
     - `UserPolicy.BlockUnratedItems` (`UnratedItem[]`, line 124): item kinds hidden when they carry no rating.
     - `UserPolicy.BlockedTags` (`[]string`): Blacklisted metadata tags. Items matching any blocked tag are omitted.
     - `UserPolicy.AllowedTags` (`[]string`): Whitelisted metadata tags.
     - `UserPolicy.EnableAllFolders` (`bool`, line 170) and `UserPolicy.EnabledFolders` (`Guid[]`, line 168): allow-list of accessible top-level libraries.
3. **Expressing group-like behavior in SnatchArr**:
   - *Option A: SSO groups only (ADR-0023)*. Jellyfin sign-in only uses `IsAdministrator`. (Rejected: ignores parental controls and folder limits configured in Jellyfin).
   - *Option B: Pure Jellyfin policy mapping*. Role and adult access derived exclusively from Jellyfin policy fields. (Rejected: prevents SSO-managed deployments from controlling roles without Jellyfin).
   - *Option C: Hybrid policy and group mapping* (Recommended). For SSO users, Authentik groups map to roles and adult access per ADR-0023. For Jellyfin users, `IsAdministrator == true` maps to `admin`. Non-admin Jellyfin users receive the configurable default role (`snatcharr.jellyfin.default_role`, default `requester`). Adult access strictly reflects Jellyfin's `MaxParentalRating`, `BlockedTags` and library access (`EnableAllFolders` / `EnabledFolders`).

### 2. Coexistence of SSO and Jellyfin authentication

SnatchArr supports simultaneous sign-in methods against a unified `users` table:

```text
+-----------------------------------------------------------------------------------+
|                                    users Table                                    |
+----+-------------------+-------------+-----------------------+--------------------+
| id | username          | role        | external_id           | password_hash      |
+----+-------------------+-------------+-----------------------+--------------------+
| u1 | admin             | admin       | NULL                  | $argon2id$...      |
| u2 | oidc-a1b2c3d4e5f6 | requester   | oidc:alice@domain.org | sentinel_forbidden |
| u3 | jf-bob            | requester   | jellyfin:550e8400-... | sentinel_forbidden |
+----+-------------------+-------------+-----------------------+--------------------+
```

1. **Identity representation and external keys**:
   - `users.external_id` (migration `000026_user_external_id.up.sql:6`) serves as the unique mapping key for all federated identities. Today it stores the bare SSO identity (`api/internal/auth/service.go:257-261`); this ADR introduces source prefixes, and the migration that adds them rewrites existing rows (`oidc-` handles to `oidc:`, `proxy-` handles to `proxy:`) in the same transaction:
     - OIDC: `oidc:<subject_or_email>`
     - Reverse proxy: `proxy:<username>`
     - Jellyfin: `jellyfin:<jellyfin_user_id>` (UUID format)
2. **User provisioning and upsert**:
   - The first sign-in of a Jellyfin user provisions a `users` row via an expanded `UpsertExternalUser` query (`api/internal/store/queries/auth.sql`).
   - The local handle is `jf-<username>` when that satisfies `domain.ValidateUsername` (`api/internal/domain/user.go:44-55`) and is free; otherwise a numeric suffix is appended (`jf-<username>-2`). The Jellyfin user id in `external_id`, never the handle, identifies the account.
   - Non-password external accounts store a fixed invalid password hash sentinel, which `auth.Service.Login` already refuses with HTTP 401 before argon2 verification (`api/internal/auth/service.go:144`, sentinel `ExternalViewerSentinelHash` at `api/internal/auth/service.go:44`); the sentinel is renamed for all external accounts.
3. **Account linking and precedence**:
   - *Local admin*: Direct password sign-in against the local admin account remains functional for emergency break-glass operations.
   - *Identity linking*: SSO and Jellyfin identities of one person stay separate accounts until linked (per answered question `snatcharr-account-auto-link`). There is no automatic linking; a signed-in user links the second identity by signing in with it from the account page, proving control of both identities. Username-based auto-linking is refused because it allows account takeover. Jellyfin's `UserDto` carries no e-mail address, so e-mail matching is not available.
   - *Role precedence*: If an identity is recognized as an administrator by either Authentik (`admin_groups`) OR Jellyfin (`IsAdministrator == true`), the user receives the `admin` role. In multi-provider setups, administrative privilege takes precedence over unprivileged group assignments.
   - *Session origin isolation*: Each session records its active authentication source in `auth.SSOOrigin` (`api/internal/auth/ssoorigin.go:31-36`). Revalidation follows the provider that authenticated the session.

### 3. Role model: admin, requester, and viewer

The domain model expands from two to three roles:

```go
// api/internal/domain/user.go
type Role string

const (
    RoleAdmin     Role = "admin"
    RoleRequester Role = "requester"
    RoleViewer    Role = "viewer"
)
```

The OpenAPI specification (`api/openapi/openapi.yaml`) updates the `UserRole` schema to `enum: [admin, requester, viewer]`.

#### Capability matrix across API scopes

| Capability / API Domain | `admin` | `requester` | `viewer` | Enforcement Mechanism |
| :--- | :---: | :---: | :---: | :--- |
| **System Instances & Downloader Config** | Full Read/Write | None (403) | None (403) | `requireAdmin(ctx)` |
| **Prowlarr & Indexer Caps** | Full Read/Write | None (403) | None (403) | `requireAdmin(ctx)` |
| **Application Settings & Module Switches** | Full Read/Write | None (403) | None (403) | `requireAdmin(ctx)` |
| **System Logs & Diagnostics** | Full Read/Stream | None (403) | None (403) | `adminOnly` Chi middleware |
| **API Keys Management** | Create / Revoke All | Own Keys Only | None (403) | `requireAdminOrOwner(ctx)` |
| **Media Status & Snatch History** | Read All | Read All | Read All | Authenticated User |
| **Queue Inspection & Live Badges** | Read All | Read All | Read All | Authenticated User |
| **Queue Force-Import & Actions** | Mutate | None (403) | None (403) | `requireAdmin(ctx)` |
| **Media Catalog & Discovery** | Browse All | Browse All | Browse All | Filtered by Parental Policy |
| **Submit Media Requests (Seerr / *arr)** | Create / Approve | Create Own | None (403) | `requireRequester(ctx)` |
| **Personal Request Status & History** | Manage All | View / Cancel Own | None (403) | Filtered by Request Owner |
| **Watch-Aware Progression ([ADR-0024](0024-watch-aware-modes.md))** | Configure Rules | View Own Seasons | View Own Seasons | Filtered by User ID |
| **Adult Media Catalog ([ADR-0018](0018-product-front-door.md))** | Gated by Policy | Gated by Policy | Gated by Policy | Adult Parental Gate Filter |
| **MCP Tools ([ADR-0021](0021-llm-access-module.md))** | Read + Write | Read Only | None (403) | Tool-level role fencing |

#### Role assignment rules

1. **SSO (OIDC and Reverse Proxy)**:
   - Evaluated by an extended `sso.ResolveRole(groups, adminGroups, requesterGroups, viewerGroups)`; today it takes admin and viewer groups only (`api/internal/sso/role.go:17`). Matching stays exact and case-sensitive as today:
     - Admin group match: `admin`.
     - Else requester group match: `requester`.
     - Else viewer group match: `viewer`.
     - Else: Refused with HTTP 403 Problem JSON.
   - Environment variables:
     - `APP_SNATCHARR_OIDC_REQUESTER_GROUPS` (`snatcharr.oidc.requester_groups`)
     - `APP_SNATCHARR_PROXY_AUTH_REQUESTER_GROUPS` (`snatcharr.proxy_auth.requester_groups`)
2. **Jellyfin sign-in**:
   - `UserPolicy.IsAdministrator == true`: Assigned `admin`.
   - Otherwise: Setting `snatcharr.jellyfin.default_role`, default `requester`, decides the role of a non-admin Jellyfin member on first sign-in (per answered question `snatcharr-jellyfin-default-role`).

### 4. Parental control and adult media visibility

Adult content visibility (`ADR-0018`, `ADR-0025`) is governed by a strict two-barrier model:

```text
[ Inbound Request ]
         |
         v
+------------------------------------+
| Barrier 1: Global Module Switch    | ---> [ Disabled ] ---> HTTP 404 Not Found
| (ADR-0025: modules.adult.enabled)  |
+------------------------------------+
         | [ Enabled ]
         v
+------------------------------------+
| Barrier 2: Per-User Parental Policy| ---> [ Unauthorized ] ---> HTTP 404 / Omitted
| (Jellyfin Policy / SSO Groups)     |
+------------------------------------+
         | [ Authorized ]
         v
[ Serve Adult Media Payload ]
```

1. **Source of truth**:
   - *Jellyfin users*: Governed by `UserPolicy.MaxParentalRating`, `UserPolicy.BlockedTags` and library access. A user is authorized for adult media if and only if:
     1. `MaxParentalRating == null` (unrestricted) OR `MaxParentalRating >= AdultRatingThreshold` (configurable threshold setting `snatcharr.adult.min_parental_rating`, default 18, per answered question `snatcharr-adult-rating-scale`).
     2. `BlockedTags` does not contain any configured adult restriction tag (e.g. `adult`, `nsfw`, `xxx`).
     3. The adult library id is included in `EnabledFolders` when `EnableAllFolders == false`.
   - *SSO users*: Governed by group membership in `snatcharr.oidc.adult_groups` or `snatcharr.proxy_auth.adult_groups`.
2. **Re-reading and caching**:
   - Policy is fetched upon sign-in; the session stores only the derived `adult_allowed` flag next to the `auth.SSOOrigin` keys, never the raw policy.
   - A leader loop (`gate.Periodic`, `api/internal/jellyfin`) synchronizes user policies periodically (default: every 30 minutes) from Jellyfin's `/Users` endpoint (`api/internal/jellyfin` is a new package); a changed result revokes the user's sessions, as `checkSSOOrigin` does for SSO group changes.
   - Cache TTL on the active session revalidates policy every 15 minutes.
3. **Hard enforcement point (API and query layer)**:
   - In accordance with the *two-barrier rule*, parental filtering is executed at the database and HTTP handler layer, never delegated to frontend UI hiding.
   - Generic list operations (snatch history, queue and catalogue listings) omit items marked with adult ratings or tags if the requesting user lacks adult authorization.
   - Dedicated adult endpoints (`/api/v1/adult/...`) return RFC 9457 HTTP 404 Problem JSON (`domain.ErrNotFound`) if the authenticated user is unauthorized, preventing enumeration of sensitive titles.

### 5. Session management and credential security

1. **Session storage reuse**:
   - All authenticated sessions reuse the PostgreSQL session manager (`github.com/golusoris/golusoris/auth/session`).
   - Sessions are protected with `SameSite=Lax`, `HttpOnly`, `Secure` cookies, and anti-CSRF tokens (`api/internal/auth/service.go:164-210`).
   - Session renewal (`RenewSession`) runs upon every login to eliminate session fixation risks (#268).
2. **Minimal token footprint (zero user token persistence)**:
   - *Option A: Encrypted token storage*. Store user Jellyfin access tokens encrypted in PostgreSQL using `core/crypto` (AES-GCM). (Rejected: introduces credential hoarding and complex revocation synchronization).
   - *Option B: Ephemeral in-memory token cache*. Store tokens in RAM with session TTL. (Rejected: loses state across pod restarts).
   - *Option C: Zero user token persistence* (Recommended). SnatchArr communicates with Jellyfin using its own configured service account API key (`snatcharr.jellyfin.api_key`, encrypted via `core/crypto`). The Jellyfin access token issued during `AuthenticateByName` or Quick Connect is used ephemerally to verify credentials and fetch `UserDto`, then immediately dropped. No per-user Jellyfin tokens are stored in the database.

### 6. Upstream goenvoy audit (`mediaserver/jellyfin/v3.1.0`)

An audit of `github.com/golusoris/goenvoy` at tag `mediaserver/jellyfin/v3.1.0` (commit `132b6d20a0c0019ebe7fb7772568ff2de6d553e8`) verifies the current integration surface:

1. **Available methods and models**:
   - `Client.AuthenticateByName(ctx, username, password) error` (`/Users/AuthenticateByName`, `jellyfin.go:237`): authenticates, but returns only an error and keeps the access token inside the client; the `User` and its `Policy` from `AuthenticationResult` are discarded.
   - `Client.GetUsers(ctx)` (`/Users`, `jellyfin.go:287`) and `Client.GetCurrentUser(ctx)` (`/Users/Me`, `jellyfin.go:300`): functional.
   - Models (`mediaserver/jellyfin/types.go`): `AuthenticationResult`, `UserDto`, `UserConfig`, and `UserPolicy` (`IsAdministrator`, `BlockedTags`, ...). goenvoy places `MaxParentalRating` on `UserConfig` (`types.go:28`), while Jellyfin v12.3 serves it on `UserPolicy`; the field therefore never decodes from a real server and is a goenvoy defect.
2. **Missing methods and upstream follow-up requirements**:
   - Quick Connect endpoints: `InitiateQuickConnect` (`POST /QuickConnect/Initiate`), `CheckQuickConnect` (`GET /QuickConnect/Connect`), and `AuthenticateWithQuickConnect` (`POST /Users/AuthenticateWithQuickConnect`) are absent from `goenvoy`.
   - Single user query: `GetUser(ctx, id string)` (`GET /Users/{id}`) is absent (only `/Users` list and `/Users/Me` exist).
   - Missing `UserPolicy` fields: `MaxParentalRating *int`, `EnableAllFolders bool`, `EnabledFolders []string` (GUIDs), `AllowedTags []string` and `BlockUnratedItems` are absent from `UserPolicy` in `types.go`.
   - `AuthenticateByName` must return the `AuthenticationResult` (and leave the shared client stateless), so one service client can verify many users concurrently.
   - Upstream PRs must be submitted to `github.com/golusoris/goenvoy` under `mediaserver/jellyfin` before implementing Quick Connect and folder whitelist gating.

### 7. Verification and test architecture

1. **Positive test cases**:
   - An administrator signing in via Jellyfin (`UserPolicy.IsAdministrator == true`) receives a session with `role: "admin"`.
   - A non-admin user signing in via Jellyfin receives `role: "requester"` and is able to submit media requests.
   - A user with `MaxParentalRating == null` or at or above the threshold and no adult tags in `BlockedTags` can view adult content through the API.
   - An SSO user matching `APP_SNATCHARR_OIDC_REQUESTER_GROUPS` receives `role: "requester"`.
   - Quick Connect authorization successfully authenticates a new device.
2. **Negative test cases**:
   - A user with `role: "requester"` attempting to mutate system instances (`POST /api/v1/instances`) receives HTTP 403 Problem JSON.
   - A user with `role: "viewer"` attempting to submit a media request receives HTTP 403 Problem JSON.
   - An unauthenticated request with invalid Jellyfin credentials receives HTTP 401 Unauthorized.
   - A user with `MaxParentalRating == 12` receives HTTP 404 Problem JSON when requesting adult media items by ID, and adult items are excluded from search results.
   - A user with `BlockedTags: ["adult"]` receives HTTP 404 Problem JSON when accessing adult media, regardless of numerical parental rating.
3. **Boundary test cases**:
   - A user with `MaxParentalRating == 17` is denied adult access, while `MaxParentalRating == 18` is permitted (with threshold 18).
   - Group names match exactly and case-sensitively (as `sso.ResolveRole` does today); tag names compare case-insensitively, as Jellyfin does.
   - `MaxParentalRating == 0` is treated as a restriction, not as unrestricted.
   - Jellyfin username colliding with an existing local user handle safely receives a distinct namespaced handle (`jf-<user>-2`).
   - Concurrent first-time sign-ins for the same external identity resolve to a single `users` row via database upsert.
4. **Planted defect cases**:
   - Planted case 1: Disable the backend parental filter in adult handlers (the user with `MaxParentalRating` below the threshold then sees adult items). The negative test verifying that a minor cannot retrieve adult media fails red.
   - Planted case 2: Map `requester` role to `requireAdmin`. The test verifying that requesters cannot edit instances fails red.

## Consequences

### Positive

- **Unified access control**: Family members and users sign in using existing Jellyfin credentials without managing secondary passwords.
- **Principle of least privilege**: The new `requester` role permits media requests and personal queue inspection while locking down system configuration and indexer settings.
- **Automated parental protection**: Adult media visibility automatically respects Jellyfin parental ratings and blocked tags, enforcing safety across API and UI surfaces.
- **Minimal secret exposure**: Avoiding persistent storage of user Jellyfin access tokens eliminates credential theft risks and simplifies session management.

### Negative

- **Upstream dependency**: Full Quick Connect support and folder-level authorization require additions to `github.com/golusoris/goenvoy`.
- **Database schema update**: Expanding `domain.Role` and supporting Jellyfin external identity mapping requires database migrations on `users`.
- **Periodic sync overhead**: Background polling of Jellyfin user policies introduces a periodic leader loop and network traffic.

## Alternatives considered

- **Plex OAuth instead of Jellyfin**: Rejected; the media stack standardizes on Jellyfin as the primary media server (`ADR-0018`, `snatcharr-product-accounts-20261020`).
- **Store Jellyfin passwords locally**: Rejected; storing third-party plaintext passwords or re-hashing them violates credential hygiene.
- **Frontend-only parental hiding**: Rejected; violates the *two-barrier rule* and allows unauthorized users to inspect adult titles via direct API queries.
- **Single flat role model**: Rejected; cannot distinguish between household members requesting media and guests who only need availability visibility.

## Open questions

1. **Adult rating threshold scale (`snatcharr-adult-rating-scale`)**:
   - *Option a*: Fixed numerical constant of 18. (Risk: Jellyfin supports custom rating systems and US TV-MA internal integers that do not equate to 18).
   - *Option b*: Configurable threshold setting (`snatcharr.adult.min_parental_rating`, default 18) combined with tag blacklist (`snatcharr.adult.blocked_tags`). (Risk: Minor configuration surface addition).
   - *Option c*: Strict metadata tag whitelist/blacklist only, ignoring age integers. (Risk: Bypasses standard Jellyfin parental age controls).
   - *Recommendation*: Option b.
   - *Answered (2026-10-24, controller ledger `snatcharr-adult-rating-scale`)*: Option b, narrowed. The gate reads the user's Jellyfin policy fields `MaxParentalRating` (`null` = unrestricted), `BlockedTags` and `AllowedTags`. The configurable threshold (default 18) only classifies which items count as adult; it never widens access. SnatchArr never grants a user more than Jellyfin allows that user.
2. **Default role for new Jellyfin sign-ins (`snatcharr-jellyfin-default-role`)**:
   - *Option a*: Always assign `requester`. (Risk: New users can immediately submit requests before admin review).
   - *Option b*: Always assign `viewer`. (Risk: Requires admin intervention to elevate household members).
   - *Option c*: Configurable setting (`snatcharr.jellyfin.default_role`, default `requester`). (Risk: Configuration knob addition).
   - *Recommendation*: Option c.
   - *Answered (2026-10-24, controller ledger `snatcharr-jellyfin-default-role`)*: Option c. Setting `snatcharr.jellyfin.default_role`, default `requester`, decides the role of a non-admin Jellyfin member on first sign-in; Jellyfin administrators map to `admin`.
3. **Linking an SSO identity and a Jellyfin identity of one person (`snatcharr-account-auto-link`)**:
   - *Option a*: Link automatically when the SSO username equals the Jellyfin username. (Risk: account takeover when one person can choose a name that collides with another person's account on the other provider.)
   - *Option b*: Keep identities separate; the signed-in user links the other identity by signing in with it from the account page. (Risk: a linking flow in the SPA; until used, one person has two accounts.)
   - *Option c*: Only the admin links accounts. (Risk: manual work per household member.)
   - *Recommendation*: Option b; it proves control of both identities. Jellyfin's `UserDto` has no e-mail address, so e-mail matching is not an option.
   - *Answered (2026-10-24, controller ledger `snatcharr-account-auto-link`)*: Option b. No automatic linking; a signed-in user links the second identity by signing in with it from the account page, so the user proves both. Username-based auto-linking is refused because it allows account takeover.

## Follow-ups

- [x] Upstream PR to `github.com/golusoris/goenvoy` (`mediaserver/jellyfin`): Add `InitiateQuickConnect`, `CheckQuickConnect`, `AuthenticateWithQuickConnect`, and `GetUser(id)`. (released in goenvoy `mediaserver/jellyfin/v3.3.0`, commit `17183e33`; SnatchArr pins it)
- [x] Upstream PR to `github.com/golusoris/goenvoy` (`mediaserver/jellyfin`): Move `MaxParentalRating` to `UserPolicy` as `*int`; add `EnableAllFolders`, `EnabledFolders`, `AllowedTags` and `BlockUnratedItems`; return `AuthenticationResult` from `AuthenticateByName`. (released in goenvoy `mediaserver/jellyfin/v3.3.0`, commit `17183e33`; SnatchArr pins it)
- [x] Implement `snatcharr.jellyfin.default_role` setting with tests: non-admin gets configured role, admin maps to `admin`, invalid setting value refused at boot. (`JellyfinOptions` in `api/internal/config/config.go` accepts `requester` or `viewer` and refuses `admin`, an empty value and any other value at boot; `domain.RoleForJellyfinMember` maps Jellyfin administrators to `admin`; the Jellyfin sign-in handler calls it.)
- [ ] Implement account-page linking flow with tests: link requires sign-in with second identity; equal usernames on both providers do NOT link; planted case: auto-link by username makes test fail.
- [x] Database migration: Add `'requester'` to role enum/checks and create `jellyfin_user_id` column or expanded `external_id` indexes on `users`. (slice A: CHECK widened in 000033; the existing UNIQUE `external_id` index carries `jellyfin:<id>`, no new column; the prefix rewrite moves into the Jellyfin sign-in item)
- [x] Implement `requireRequester` API middleware and update OpenAPI security schemes.
- [x] Implement Jellyfin credential authentication handler (`POST /api/v1/auth/jellyfin`) in `api/internal/auth`. (`api/internal/httpapi/handlers_jellyfin.go`, route `POST /auth/jellyfin`)
- [x] Implement Jellyfin Quick Connect handlers in `api/internal/auth`. (`api/internal/httpapi/handlers_jellyfin.go`, routes `POST /auth/jellyfin/quickconnect/initiate` and `POST /auth/jellyfin/quickconnect/authenticate`)
- [x] Revalidate Jellyfin session policy: re-read the member with `GetUser` after 15 minutes of session age and in a 30-minute leader loop; revoke on missing, disabled, or changed admin flag or derived adult access. (`api/internal/jellyfin/revalidator.go` re-reads a member at most every 15 minutes per subject and session role on the request path; `api/internal/auth/jellyfin_ticker.go` runs the 30-minute leader loop over at most 1000 live sessions and 200 members per tick. A refused service key revokes too; a transient Jellyfin error keeps the session. Derived adult access has no policy setting yet and lands with the parental filtering item below.)
- [ ] Implement parental rating and tag filtering in media catalog and adult module endpoints, with boundary tests at threshold minus one, at the threshold and with `MaxParentalRating` `null`, plus a `BlockedTags` case.
- [x] Update SvelteKit SPA navigation and request dialogs to gate on `session.isRequester`. (`session.isRequester` mirrors `Role.CanRequest`; the API key create and revoke controls on the account page now use it, matching `requireRequester` in `api/internal/httpapi/handlers_apikeys.go`. The SPA has no request dialog or requester-only navigation yet; the request slice gates its own controls on the same getter.)

## References

- `snatcharr-product-users-20261020` (2026-10-20T14:51:45) (paraphrased): User accounts with admin, requester, and viewer roles; account source fits the existing Jellyfin plus SSO setup.
- `snatcharr-product-accounts-20261020` (2026-10-20T14:55:08) (paraphrased): Users, roles, and adult access sourced from Jellyfin authentication and group configuration.
- `snatcharr-product-adult-visibility-20261020` (2026-10-20T14:51:45) (paraphrased): Adult content visibility governed by Jellyfin user groups, parental ratings, and age restrictions.
- Seerr upstream authentication: [seerr-team/seerr@v3.5.1](https://github.com/seerr-team/seerr/tree/v3.5.1), `server/routes/auth.ts` and `server/api/jellyfin.ts`.
- Jellyfin server source: [jellyfin/jellyfin@v12.3](https://github.com/jellyfin/jellyfin/tree/v12.3), `MediaBrowser.Model/Users/UserPolicy.cs`.
- Jellyfin REST API documentation: [api.jellyfin.org](https://api.jellyfin.org/).
- Jellyfin User Management: [jellyfin.org/docs/general/server/users](https://jellyfin.org/docs/general/server/users/).
- GoEnvoy Jellyfin client: [github.com/golusoris/goenvoy@mediaserver/jellyfin/v3.1.0](https://github.com/golusoris/goenvoy/tree/mediaserver/jellyfin/v3.1.0).
