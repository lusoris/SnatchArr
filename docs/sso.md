<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Single Sign-On (SSO)

SnatchArr supports two single sign-on mechanisms:

1. **OpenID Connect (OIDC)**: Authorization code flow with PKCE S256 against identity providers like Authentik, Keycloak, Okta, or Google.
2. **Trusted reverse-proxy authentication**: Pre-authenticated user identity asserted via a reverse-proxy HTTP header (Authelia, Authentik proxy outpost, Cloudflare Access, Tailscale).

Both modes map verified identities to the local administrator account (`GetFirstAdmin` in `api/internal/store/queries/auth.sql`), preserving SnatchArr's single-admin model. Both modes are disabled by default. If the initial setup has not been completed, SSO login attempts are refused until the admin account is created.

---

## OpenID Connect (OIDC)

### Getting started

To configure OIDC:

1. Register an application in your identity provider with:
   - Redirect URI: `https://snatcharr.example.com/api/v1/auth/oidc/callback` (or your configured `APP_SNATCHARR_PUBLIC_URL` + `/api/v1/auth/oidc/callback`).
   - Grant type: Authorization Code.
   - PKCE: S256 code challenge.
2. Provide your IdP's issuer URL, client ID, client secret, and at least one allowed subject or email.
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
| `APP_SNATCHARR_OIDC_ALLOWED_SUBJECTS` | `auth.oidc.allowedSubjects` | | Comma-separated list of IdP subject (`sub`) claims allowed to log in as admin. |
| `APP_SNATCHARR_OIDC_ALLOWED_EMAILS` | `auth.oidc.allowedEmails` | | Comma-separated list of email addresses allowed to log in as admin (case-insensitive exact match; requires `email_verified == true`). |
| `APP_SNATCHARR_OIDC_DISCOVERY_TIMEOUT` | `auth.oidc.discoveryTimeout` | `10s` | HTTP timeout for provider discovery and token exchange. |

### Security guarantees

- **Mandatory allowlist**: Startup fails if OIDC is enabled without at least one entry in `allowed_subjects` or `allowed_emails` (`config.Validate` in `api/internal/config/config.go`).
- **Email verification**: When matching by email address, the ID token's `email_verified` claim must be explicitly `true`.
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
4. SnatchArr validates that the direct TCP peer is inside the configured trusted CIDR list and that the username is in the allowed list, then issues a session cookie.

### Configuration

| Environment variable | Helm value | Default | Description |
| --- | --- | --- | --- |
| `APP_SNATCHARR_PROXY_AUTH_ENABLED` | `auth.proxyAuth.enabled` | `false` | Enable trusted proxy-header authentication. |
| `APP_SNATCHARR_PROXY_AUTH_HEADER` | `auth.proxyAuth.header` | `Remote-User` | Name of the incoming header containing the pre-authenticated username. |
| `APP_SNATCHARR_PROXY_AUTH_TRUSTED_CIDRS` | `auth.proxyAuth.trustedCidrs` | | Comma-separated list of IP CIDRs allowed to assert proxy authentication (e.g. `10.0.0.0/8,172.16.0.0/12`). |
| `APP_SNATCHARR_PROXY_AUTH_ALLOWED_USERS` | `auth.proxyAuth.allowedUsers` | | Comma-separated list of usernames allowed to log in as admin (case-insensitive exact match). |

### Security risks and operational rules

> [!WARNING]
> Any client capable of reaching the SnatchArr HTTP port directly from an IP within `trusted_cidrs` can set the configured header and gain admin access.

To prevent unauthorized access when using proxy authentication:

1. **Direct peer verification**: The proxy authentication middleware (`sso.ProxyHeaderMiddleware` in `api/internal/sso/sso.go`) checks the direct TCP peer IP before `middleware.TrustProxy` evaluates any `X-Forwarded-For` header. Spoofed `X-Forwarded-For` headers cannot trick the proxy check.
2. **Wildcard CIDRs rejected**: Startup fails if `trusted_cidrs` contains `0.0.0.0/0` or `::/0` (`config.Validate` in `api/internal/config/config.go`).
3. **Mandatory user allowlist**: Startup fails if `allowed_users` is empty.
4. **Header stripping**: The header is deleted from the request immediately after evaluation. Downstream application handlers never see the raw incoming header.
5. **No direct request authentication**: The proxy header does not authenticate API requests directly. It is only accepted at `POST /api/v1/auth/proxy-session` to issue a standard cookie session.
6. **Network isolation**: Ensure the SnatchArr HTTP service is not exposed directly to untrusted networks or pods. Only the reverse proxy should be able to route traffic to the container port.

---

## Helm deployment example

```yaml
auth:
  oidc:
    enabled: true
    issuerUrl: "https://auth.example.com"
    clientId: "snatcharr"
    existingSecret: "snatcharr-oidc-secret"
    allowedEmails:
      - "admin@example.com"

  proxyAuth:
    enabled: false
```
