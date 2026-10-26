<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Single Sign-On (SSO)

SnatchArr supports three external sign-in mechanisms:

1. **OpenID Connect (OIDC)**: Authorization code flow with PKCE S256 against identity providers like Authentik, Keycloak, Okta, or Google.
2. **Trusted reverse-proxy authentication**: Pre-authenticated user identity asserted via a reverse-proxy HTTP header (Authelia, Authentik proxy outpost, Cloudflare Access, Tailscale).
3. **Jellyfin authentication**: Direct credential sign-in and Quick Connect against a Jellyfin server ([ADR-0026](adr/0026-accounts-and-roles.md)).

Both modes resolve a role from the identity's groups ([ADR-0023](adr/0023-sso-viewer-role.md), [ADR-0026](adr/0026-accounts-and-roles.md), `sso.ResolveRole` in `api/internal/sso/role.go`):

- A member of any admin group signs in as the local administrator account (`GetFirstAdmin` in `api/internal/store/queries/auth.sql`).
- A member of a requester group, and of no admin group, signs in as its own requester account. The account is created on first sign-in and keyed by the identity in `users.external_id` (`UpsertExternalUser`). Requesters can manage their own API keys and submit requests.
- A member of a viewer group, and of no admin or requester group, signs in as its own viewer account (`UpsertExternalUser`). Viewers read everything and get `403` on every change.
- An identity in no mapped group is refused.

Role precedence is `admin > requester > viewer`.

Group names match exactly and case-sensitively; a name may contain spaces (`SnatchArr Viewers`), and only the space around each comma-separated entry is trimmed (`cleanTrusted` in `api/internal/config/config.go`). Both modes are disabled by default. If the initial setup has not been completed, SSO login attempts are refused until the admin account is created.

### Session revocation

A single sign-on session stores its origin: the mode, the subject, the groups and the role it was granted (`SSOOrigin` in `api/internal/auth/ssoorigin.go`). Every cookie-authenticated request re-resolves the role against the current configuration (`checkSSOOrigin` in `api/internal/httpapi/security_sso.go`). When the mode is disabled, the identity's groups no longer map to a role, the role differs from the stored one, or the proxy asserts a different user, the session row is deleted and the request gets `401`; the log line `httpapi: sso session revoked` carries the reason. Sessions from the local login form are not affected.

- **Proxy mode** judges the groups the proxy sends with each request, so removing an identity from its group in the identity provider ends the session on its next request.
- **OIDC** has no per-request group source: the stored token groups are judged against the configured `admin_groups`, `requester_groups`, and `viewer_groups`. Removing a group from the configuration ends its sessions on their next request; removing an identity from a group in the identity provider takes effect at its next sign-in.
- **Jellyfin mode** revokes active sessions immediately if Jellyfin authentication is disabled (`APP_SNATCHARR_JELLYFIN_ENABLED=false`).

---

## OpenID Connect (OIDC)

### Getting started

To configure OIDC:

1. Register an application in your identity provider with:
   - Redirect URI: `https://snatcharr.example.com/api/v1/auth/oidc/callback` (or your configured `APP_SNATCHARR_PUBLIC_URL` + `/api/v1/auth/oidc/callback`).
   - Grant type: Authorization Code.
   - PKCE: S256 code challenge.
2. Provide your IdP's issuer URL, client ID, client secret, and the admin and viewer group names.
   Configure the provider to put the user's groups into the ID token (Authentik: the `profile` scope already carries `groups`).
3. The web UI login page detects that OIDC is enabled via `GET /api/v1/auth/sso` and displays a "Sign in with SSO" button.

### Configuration

| Environment variable | Helm value | Default | Description |
| --- | --- | --- | --- |
| `APP_SNATCHARR_OIDC_ENABLED` | `auth.oidc.enabled` | `false` | Enable OIDC single sign-on. |
| `APP_SNATCHARR_OIDC_ISSUER_URL` | `auth.oidc.issuerUrl` | | OpenID Connect discovery URL. Must use `https://` in production; `http://` is accepted only under `APP_SNATCHARR_PROFILE=dev`. |
| `APP_SNATCHARR_OIDC_CLIENT_ID` | `auth.oidc.clientId` | | OAuth2 client identifier. |
| `APP_SNATCHARR_OIDC_CLIENT_SECRET` | via `auth.oidc.existingSecret` or `auth.oidc.secretKeyRef` | | OAuth2 client secret. Never committed or inlined in values files. |
| `APP_SNATCHARR_OIDC_REDIRECT_URL` | `auth.oidc.redirectUrl` | `public.url` + `/api/v1/auth/oidc/callback` | OAuth2 redirect URL. |
| `APP_SNATCHARR_OIDC_SCOPES` | `auth.oidc.scopes` | `openid,email,profile` | Comma-separated list of scopes. `openid` is always required and added automatically if omitted. |
| `APP_SNATCHARR_OIDC_ADMIN_GROUPS` | `auth.oidc.adminGroups` | | Comma-separated groups whose members sign in as admin. Required. |
| `APP_SNATCHARR_OIDC_REQUESTER_GROUPS` | `auth.oidc.requesterGroups` | | Comma-separated groups whose members sign in as requesters. |
| `APP_SNATCHARR_OIDC_VIEWER_GROUPS` | `auth.oidc.viewerGroups` | | Comma-separated groups whose members sign in as viewers. |
| `APP_SNATCHARR_OIDC_GROUPS_CLAIM` | `auth.oidc.groupsClaim` | `groups` | ID token claim holding the user's groups as a list of strings. |
| `APP_SNATCHARR_OIDC_DISCOVERY_TIMEOUT` | `auth.oidc.discoveryTimeout` | `10s` | HTTP timeout for provider discovery and token exchange. |

### Security guarantees

- **Mandatory admin group**: Startup fails if OIDC is enabled with an empty `admin_groups`, or with a group appearing in any two group lists (`config.Validate` in `api/internal/config/config.go`).
- **Legacy allow-lists refused**: `allowed_subjects` and `allowed_emails` no longer grant access; startup fails while either is set, so a stale value cannot quietly keep or lose access.
- **Missing or malformed groups**: A token without the groups claim, or with a claim that is not a list of strings, is refused.
- **PKCE and state integrity**: Every login generates 32 random bytes for state, nonce, and PKCE verifier (`sso.LoginHandler` in `api/internal/sso/oidc.go`). State and nonce verification uses constant-time comparison.
- **Single-use session tokens**: The state, nonce, and verifier are deleted from the session storage on the very first callback request before exchanging tokens, preventing replay attacks.
- **Flow timeout**: An authorization flow expires 10 minutes after generation.
- **Resilient startup**: Provider discovery is performed lazily. An IdP outage will prevent OIDC logins but will not prevent the API server from starting or serving credential logins.

---

## Trusted reverse-proxy authentication

### Getting started

When deploying behind an authenticating reverse proxy (such as Authelia, Authentik outpost, or Tailscale serve/funnel with authentication):

1. The reverse proxy authenticates the user and forwards their verified identity in an HTTP request header (e.g. `Remote-User`).
2. The proxy forwards the request to SnatchArr.
3. When the user loads the SnatchArr web UI, the frontend checks `GET /api/v1/auth/sso` and calls `POST /api/v1/auth/proxy-session`.
4. SnatchArr validates that the direct TCP peer is inside the configured trusted CIDR list and resolves a role from the groups header, then issues a session cookie for the admin or for the identity's viewer account.

### Configuration

| Environment variable | Helm value | Default | Description |
| --- | --- | --- | --- |
| `APP_SNATCHARR_PROXY_AUTH_ENABLED` | `auth.proxyAuth.enabled` | `false` | Enable trusted proxy-header authentication. |
| `APP_SNATCHARR_PROXY_AUTH_HEADER` | `auth.proxyAuth.header` | `Remote-User` | Name of the incoming header containing the pre-authenticated username. |
| `APP_SNATCHARR_PROXY_AUTH_TRUSTED_CIDRS` | `auth.proxyAuth.trustedCidrs` | | Comma-separated list of IP CIDRs allowed to assert proxy authentication (e.g. `10.0.0.0/8,172.16.0.0/12`). |
| `APP_SNATCHARR_PROXY_AUTH_GROUPS_HEADER` | `auth.proxyAuth.groupsHeader` | `X-authentik-groups` | Name of the incoming header containing the user's groups, separated by `\|`. |
| `APP_SNATCHARR_PROXY_AUTH_SECRET_HEADER` | `auth.proxyAuth.secretHeader` | `X-SnatchArr-Proxy-Secret` | Name of the incoming header containing the shared proxy authentication secret. |
| `APP_SNATCHARR_PROXY_AUTH_SECRET` | via `auth.proxyAuth.existingSecret` | | Shared secret the reverse proxy must send (min 32 bytes after leading and trailing whitespace is trimmed, so a secret file's trailing newline is harmless). Never committed or inlined in values files. |
| `APP_SNATCHARR_PROXY_AUTH_ADMIN_GROUPS` | `auth.proxyAuth.adminGroups` | | Comma-separated groups whose members sign in as admin. Required. |
| `APP_SNATCHARR_PROXY_AUTH_REQUESTER_GROUPS` | `auth.proxyAuth.requesterGroups` | | Comma-separated groups whose members sign in as requesters. |
| `APP_SNATCHARR_PROXY_AUTH_VIEWER_GROUPS` | `auth.proxyAuth.viewerGroups` | | Comma-separated groups whose members sign in as viewers. |

### Reverse proxy configuration

The reverse proxy must inject the shared secret header and strip any untrusted incoming copy sent by external clients:

- **Header injection**: The reverse proxy authenticates the user, forwards identity headers (`Remote-User` and `X-authentik-groups`), and injects the shared secret header (`X-SnatchArr-Proxy-Secret`):
  - **Traefik**: Use a Middleware with `customRequestHeaders`:
    ```yaml
    apiVersion: traefik.io/v1alpha1
    kind: Middleware
    metadata:
      name: snatcharr-proxy-auth
    spec:
      headers:
        customRequestHeaders:
          X-SnatchArr-Proxy-Secret: "YOUR_SHARED_SECRET_32_BYTES_MIN"
    ```
  - **nginx**: Use `proxy_set_header`:
    ```nginx
    proxy_set_header Remote-User $remote_user;
    proxy_set_header X-authentik-groups $authentik_groups;
    proxy_set_header X-SnatchArr-Proxy-Secret "YOUR_SHARED_SECRET_32_BYTES_MIN";
    ```
  - **Authentik outpost**: Place the outpost behind a proxy configured as above to append the shared secret header.
- **Client header stripping**: The reverse proxy must strip any client-supplied copy of the secret header so external clients cannot forge the secret.
- **Enforcement**: Any request originating from a trusted CIDR presenting proxy identity headers with a missing or incorrect secret receives an immediate `401 Unauthorized` (`application/problem+json`) response.

### Upgrading from a release without the shared secret

Proxy authentication now refuses to start without `APP_SNATCHARR_PROXY_AUTH_SECRET`. Before upgrading, create a Kubernetes Secret with a random value of at least 32 bytes (for example `openssl rand -hex 32`), set `auth.proxyAuth.existingSecret` (and `auth.proxyAuth.secretKey` if the key is not `proxy-auth-secret`), and configure the reverse proxy to send that value in the secret header.

### Security risks and operational rules

> [!WARNING]
> Any client capable of reaching the SnatchArr HTTP port directly from an IP within `trusted_cidrs` can set the configured headers and gain admin access.

To prevent unauthorized access when using proxy authentication:

1. **Direct peer verification**: The proxy authentication middleware (`sso.ProxyHeaderMiddleware` in `api/internal/sso/sso.go`) checks the direct TCP peer IP before `middleware.TrustProxy` evaluates any `X-Forwarded-For` header. Spoofed `X-Forwarded-For` headers cannot trick the proxy check.
2. **Shared proxy secret**: In addition to CIDR trust, the proxy must present a pre-shared secret of at least 32 bytes (`APP_SNATCHARR_PROXY_AUTH_SECRET`). Any trusted-CIDR request presenting proxy headers without or with an invalid secret is refused with `401 Unauthorized`. Secret comparisons are performed in constant time via SHA-256 digests.
3. **Wildcard CIDRs rejected**: Startup fails if `trusted_cidrs` contains `0.0.0.0/0` or `::/0` (`config.Validate` in `api/internal/config/config.go`).
4. **Mandatory admin group**: Startup fails if `admin_groups` is empty, if a group is on both lists, or while the legacy `allowed_users` is set.
5. **Header stripping**: All proxy authentication headers (username, groups, and secret) are deleted from the request immediately after evaluation. Downstream application handlers never see the raw incoming headers or secret.
6. **No direct request authentication**: The proxy header does not authenticate API requests directly. It is only accepted at `POST /api/v1/auth/proxy-session` to issue a standard cookie session.
7. **Network isolation**: Ensure the SnatchArr HTTP service is not exposed directly to untrusted networks or pods. Only the reverse proxy should be able to route traffic to the container port.

---

## Jellyfin sign-in

SnatchArr supports Jellyfin authentication via user credentials or Quick Connect ([ADR-0026](adr/0026-accounts-and-roles.md)):

1. **Credential sign-in**: `POST /api/v1/auth/jellyfin` authenticates against Jellyfin's `/Users/AuthenticateByName`.
2. **Quick Connect**: `POST /api/v1/auth/jellyfin/quickconnect/initiate` initiates an authorization code flow displayed in the Jellyfin client, and `POST /api/v1/auth/jellyfin/quickconnect/authenticate` completes authentication once approved on the Jellyfin server.

### Role mapping and user provisioning

- Jellyfin administrators (`UserPolicy.IsAdministrator: true`) sign in directly as the local administrator account (`GetFirstAdmin`).
- Non-administrator Jellyfin users are provisioned as independent accounts on first sign-in, keyed by `jellyfin:<id>` in `users.external_id` (`UpsertExternalUser`).
- Non-administrator accounts receive the role configured in `APP_SNATCHARR_JELLYFIN_DEFAULT_ROLE` (`requester` or `viewer`, default `requester`).
- Jellyfin access tokens are never logged, stored in the database, or returned to clients.

### Configuration

| Environment variable | Helm value | Default | Description |
| --- | --- | --- | --- |
| `APP_SNATCHARR_JELLYFIN_ENABLED` | `jellyfin.enabled` | `false` | Enable Jellyfin authentication. |
| `APP_SNATCHARR_JELLYFIN_URL` | `jellyfin.url` | | Base URL of the Jellyfin server (e.g. `http://jellyfin:8096`). Must be absolute http/https with host. |
| `APP_SNATCHARR_JELLYFIN_API_KEY` | via `jellyfin.existingSecret` or `jellyfin.secretKeyRef` | | Jellyfin server API key for querying Quick Connect state and user policies. |
| `APP_SNATCHARR_JELLYFIN_TIMEOUT` | `jellyfin.timeout` | `10s` | Request timeout for communication with Jellyfin. |
| `APP_SNATCHARR_JELLYFIN_DEFAULT_ROLE` | `jellyfin.defaultRole` | `requester` | Role for non-admin Jellyfin users on first sign-in (`requester` or `viewer`). |

---

## Linked sign-ins

Users can link external identities (Jellyfin accounts and reverse-proxy sign-ins) to their existing SnatchArr account from the account page (`/account`) under the **Linked sign-ins** section ([ADR-0026](adr/0026-accounts-and-roles.md)):

- **Jellyfin**: An active user can link their Jellyfin account by submitting their Jellyfin username and password. SnatchArr verifies credentials directly against the configured Jellyfin server and links the identity to the account. Jellyfin administrator accounts cannot be linked; an identity already linked to another account, or one that has its own SnatchArr account, is refused (409).
- **Reverse proxy**: When accessing SnatchArr through an authenticating reverse proxy, the user can link the proxy identity of the active browser session. Proxy identities in an administrator group cannot be linked.
- **Unlinking**: Any linked identity can be unlinked at any time via an explicit confirmation dialog.
- **Precedence**: Account linking requires actively authenticating with the second identity. SnatchArr never automatically links accounts based solely on matching usernames.

---

## Helm deployment example

```yaml
auth:
  oidc:
    enabled: true
    issuerUrl: "https://auth.example.com"
    clientId: "snatcharr"
    existingSecret: "snatcharr-oidc-secret"
    adminGroups:
      - "Admins"
    viewerGroups:
      - "SnatchArr Viewers"

  proxyAuth:
    enabled: false
```
