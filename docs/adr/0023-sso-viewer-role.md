<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0023: Viewer sign-in through single sign-on

- Status: proposed (draft; the sign-in model and how the operator stays admin wait for the operator's decision `snatcharr-served-identity-role`)
- Date: 2026-10-21

## Context

SnatchArr has two roles, `admin` and `viewer` (`api/internal/domain/user.go`, `User.role` in `api/openapi/openapi.yaml`), but only part of the API checks for `admin` today:

- Gated: the instance, download-client, Prowlarr and Seerr writes and their `Test*Input` probes (`requireAdmin`, `api/internal/httpapi/handlers_instances.go`), and four reads: `ListLogs`, `GetConfigarrStatus`, `GetPolicyFilterOptions` and the log stream (`adminOnly`, `api/internal/httpapi/module.go`).
- Not gated: the notification channel and rule writes plus `TestNotificationChannel` (`handlers_notifications.go`), `CreateAPIKey` and `RevokeAPIKey` (`handlers_apikeys.go`), and the probes of stored targets `TestInstance`, `TestDownloadClient`, `TestProwlarrLink`, `TestSeerrLink`.

With one role in practice the gaps are harmless; with a viewer they would let it send webhooks, change alerts and mint API keys. Closing them is part of this decision.

No path creates a viewer:

- First-run setup creates the single admin (`api/internal/auth/service.go`, `Setup`).
- An OIDC sign-in whose `sub` or `email` is on `allowed_subjects` / `allowed_emails` starts a session as the first admin (`api/internal/sso/oidc.go`, `completeCallback`).
- A proxy sign-in whose user is on `proxy_auth.allowed_users` does the same (`api/internal/httpapi/handlers.go`, `CreateProxySession`).

So every identity SnatchArr lets in is the admin. That blocks least-privilege access for anyone or anything that only needs to look: a household member reading History, a dashboard, and the synthetic check that loads the served site after each deploy to verify the rendered layout (order `snatcharr-history-layout-ui-check-20261021`, step 3). The cluster can create such a check identity in Authentik, but SnatchArr would make it admin, which the fleet rules forbid for a check job.

## Decision

Single sign-on gains a second, separate allow-list that maps to a per-identity `viewer` user. The existing allow-lists keep their meaning.

### 1. Configuration

| Key | Environment | Meaning |
| :--- | :--- | :--- |
| `snatcharr.oidc.viewer_subjects` | `APP_SNATCHARR_OIDC_VIEWER_SUBJECTS` | `sub` claims that sign in as viewer |
| `snatcharr.oidc.viewer_emails` | `APP_SNATCHARR_OIDC_VIEWER_EMAILS` | `email` claims that sign in as viewer |
| `snatcharr.proxy_auth.viewer_users` | `APP_SNATCHARR_PROXY_AUTH_VIEWER_USERS` | proxy user header values that sign in as viewer |

- An identity on both an admin list and a viewer list is refused at config validation (compared case-insensitively), so a typo can never decide the role silently.
- The three keys join `CompoundKeys()` in `api/internal/config/config.go`, otherwise the `APP_SNATCHARR_*` variables never reach them.
- `allowed_subjects`, `allowed_emails` and `allowed_users` keep mapping to the first admin, so the operator's own sign-in does not change.

### 2. Provisioning

- The first viewer sign-in of an identity creates a `users` row with role `viewer`. `ValidateUsername` allows letters, digits, `.`, `-` and `_` up to 64 characters (`api/internal/domain/user.go`), so the username is `oidc-` or `proxy-` followed by a short hash of the subject or user, and the full identity goes into a new `external_id` column (unique). Creation is an upsert on `external_id`, so two first sign-ins at once yield one row.
- `users.password_hash` is `NOT NULL` (`000001_init.up.sql`). A viewer row stores a fixed sentinel that is never a valid hash, and `auth.Login` refuses it with `401` before it calls `crypto.VerifyPassword`, which would otherwise return an error and answer `500`.
- No viewer row is created while setup is incomplete: `SetupComplete` counts users (`api/internal/auth/service.go`), so a viewer created first would lock the admin out of `/auth/setup`. Before setup, a viewer sign-in is refused.
- Later sign-ins reuse that row. Removing the identity from the viewer list refuses the next sign-in; the row stays for the audit trail.
- Cookie and bearer authentication load the user by id only (`api/internal/httpapi/security.go`). A viewer row whose identity is no longer on a viewer list is refused there as well, so a removed identity loses its open session and any API key at once instead of after the session TTL.
- The session starts through the same `RenewSession` path as the admin (fresh session id, #268).

### 3. What a viewer sees

- All reads except the four admin-only ones (`ListLogs`, the log stream, `GetConfigarrStatus`, `GetPolicyFilterOptions`), which return `403` `application/problem+json`; the SPA shows its problem note there.
- No mutation and no outbound probe: `requireAdmin` is added to every handler listed as not gated above.
- The SPA hides write controls for `viewer`, based on `isAdmin` from the session (`web/src/lib/session.svelte.ts`); today it gates only the Logs route, so the other pages gain the same check (part of this unit).

### 4. Tests

- Positive: a listed viewer subject, email and proxy user each get a viewer session; a second sign-in reuses the row.
- Negative: a viewer gets `403` on every mutating and probing operation, generated as a table from the OpenAPI operations so a new one cannot be missed, notifications and API keys included; `403` on the log stream; an unlisted identity is refused; an identity on both lists fails config validation; a viewer sign-in before setup is refused and `/auth/setup` still works.
- Boundary: empty viewer lists keep today's behaviour; the password login against a viewer row answers `401`, not `500`; a subject longer than 64 characters or with `:` and `@` yields a valid username; two concurrent first sign-ins yield one row; an identity removed from the list loses its session and API key on the next request.
- Planted case: map the viewer list to `FirstAdmin` and the `403` table turns red.

## Consequences

- The served layout check signs in as a viewer identity the cluster creates in Authentik; the admin-only routes are checked in their `403` state, not with data.
- One more allow-list to keep in sync with Authentik; config validation refuses overlaps.
- A migration adds `users.external_id`; there is still no role change or user deletion query, so moving an identity from the viewer list to an admin list signs it in as the first admin and leaves the viewer row unused.
- Proxy auth trusts the user header from the ingress exactly as it does today; the viewer list adds no new trust.

## Alternatives considered

- **Group claim mapping** (an Authentik group such as `snatcharr-viewers` in a `groups` claim): fewer config edits per identity, but it ties the role to a claim shape SnatchArr does not read today and to a scope that has to be requested. It remains an option for the operator.
- **Check identity as admin**: rejected, a check job never holds admin rights.
- **Local viewer accounts with passwords**: rejected, a second credential store next to Authentik.

## Open questions for the operator

- Allow-list per identity (this draft) or group claim mapping.
- Whether the operator's own sign-in moves to a per-identity admin row instead of the first admin.
