<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0023: Roles from single sign-on groups

- Status: accepted (operator decision `snatcharr-served-identity-role`, answer a, 2026-10-21)
- Date: 2026-10-21

## Context

SnatchArr has two roles, `admin` and `viewer` (`api/internal/domain/user.go`, `User.role` in `api/openapi/openapi.yaml`), but only part of the API checks for `admin` today:

- Gated: the instance, download-client, Prowlarr and Seerr writes and their `Test*Input` probes (`requireAdmin`, `api/internal/httpapi/handlers_instances.go`), and four reads: `ListLogs`, `GetConfigarrStatus`, `GetPolicyFilterOptions` and the log stream (`adminOnly`, `api/internal/httpapi/module.go`).
- Not gated: the notification channel and rule writes plus `TestNotificationChannel` (`handlers_notifications.go`), `CreateAPIKey` and `RevokeAPIKey` (`handlers_apikeys.go`), and the probes of stored targets `TestInstance`, `TestDownloadClient`, `TestProwlarrLink`, `TestSeerrLink`.

No path creates a viewer:

- First-run setup creates the single admin (`api/internal/auth/service.go`, `Setup`).
- An OIDC sign-in whose `sub` or verified `email` is on `allowed_subjects` / `allowed_emails` starts a session as the first admin (`api/internal/sso/oidc.go`, `completeCallback`, `verifyClaims`).
- A proxy sign-in whose user is on `proxy_auth.allowed_users` does the same (`api/internal/httpapi/handlers.go`, `CreateProxySession`).

So every identity SnatchArr lets in is the admin. That blocks least-privilege access for anything that only needs to look: a household member reading History, a dashboard, and the synthetic check that loads the served site after each deploy (order `snatcharr-history-layout-ui-check-20261021`). A check job never holds admin rights.

Authentik already carries group membership on both sign-in paths:

- OIDC: the default scope mapping for `profile` returns `"groups": [group.name for group in request.user.groups.all()]` (`blueprints/system/providers-oauth2.yaml` in goauthentik/authentik, read 2026-10-21 on `main`). SnatchArr requests `openid,email,profile` by default (`api/internal/config/config.go`).
- Proxy outpost: `X-authentik-groups` carries the user's groups separated by pipes, for example `foo|bar|baz` (docs.goauthentik.io, proxy provider, "Headers sent to upstream applications", read 2026-10-21).

## Decision

The role of a single sign-on identity comes from its Authentik groups. One group maps to admin, one to viewer; an identity in neither is refused. The per-identity allow-lists stop granting admin.

### 1. Configuration

| Key | Environment | Meaning |
| :--- | :--- | :--- |
| `snatcharr.oidc.admin_groups` | `APP_SNATCHARR_OIDC_ADMIN_GROUPS` | group names in the `groups` claim that sign in as the admin |
| `snatcharr.oidc.viewer_groups` | `APP_SNATCHARR_OIDC_VIEWER_GROUPS` | group names that sign in as a viewer |
| `snatcharr.oidc.groups_claim` | `APP_SNATCHARR_OIDC_GROUPS_CLAIM` | claim name, default `groups` |
| `snatcharr.proxy_auth.groups_header` | `APP_SNATCHARR_PROXY_AUTH_GROUPS_HEADER` | header with pipe-separated groups, default `X-authentik-groups` |
| `snatcharr.proxy_auth.admin_groups` | `APP_SNATCHARR_PROXY_AUTH_ADMIN_GROUPS` | groups that sign in as the admin |
| `snatcharr.proxy_auth.viewer_groups` | `APP_SNATCHARR_PROXY_AUTH_VIEWER_GROUPS` | groups that sign in as a viewer |

- Group names compare exactly (case-sensitive), as Authentik emits them.
- A group on both the admin and the viewer list is refused at config validation.
- With OIDC or proxy auth enabled, an empty `admin_groups` is refused at config validation. The new version then fails to start instead of starting with nobody able to reach admin; on Kubernetes the rolling update stops and the old pod keeps serving.
- `allowed_subjects`, `allowed_emails` and `allowed_users` no longer grant admin. Setting any of them fails config validation with a message that names the group keys, so a stale value cannot quietly keep or lose access.
- The new list keys join `CompoundKeys()` in `api/internal/config/config.go`, otherwise the `APP_SNATCHARR_*` variables never reach them.
- The proxy groups header is read and stripped by `ProxyHeaderMiddleware` under the same trusted-CIDR check as the user header (`api/internal/sso/sso.go`); from an untrusted peer it is dropped.

### 2. Role resolution

One pure function decides, shared by both paths: groups in, `admin`, `viewer` or refused out.

- Any admin group present: admin. Admin wins over viewer, so an operator who is also in the viewer group stays admin.
- Else any viewer group present: viewer.
- Else, or with a missing or malformed claim or header: refused, `403` `application/problem+json`, logged with the subject and the group count, never the group names of an unknown identity.
- Authentik's default mapping lists direct memberships (`request.user.groups.all()`); a group reached only through a parent group does not count. The cluster adds identities to the groups directly.

### 3. Provisioning

- Admin: the session starts as the first admin, as today (`auth.FirstAdmin`). There is still one admin row; admin-group membership is what reaches it.
- Viewer: the first sign-in of an identity creates a `users` row with role `viewer`. `ValidateUsername` allows letters, digits, `.`, `-` and `_` up to 64 characters (`api/internal/domain/user.go`), so the username is `oidc-` or `proxy-` followed by a short hash of the subject or user, and the full identity goes into a new `external_id` column (unique). Creation is an upsert on `external_id`, so two first sign-ins at once yield one row.
- `users.password_hash` is `NOT NULL` (`000001_init.up.sql`). A viewer row stores a fixed sentinel that is never a valid hash, and `auth.Login` refuses it with `401` before it calls `crypto.VerifyPassword`, which would otherwise return an error and answer `500`.
- No viewer row is created while setup is incomplete: `SetupComplete` counts users (`api/internal/auth/service.go`), so a viewer created first would lock the admin out of `/auth/setup`. Before setup, a viewer sign-in is refused.
- The role is decided at every sign-in. A viewer removed from the viewer group is refused on the next sign-in; the row stays for the audit trail.
- Cookie and bearer authentication load the user by id only (`api/internal/httpapi/security.go`) and cannot see groups between sign-ins. A viewer session therefore lives at most the session TTL after a group removal; proxy auth re-reads the header on every session creation. Revocation inside the TTL is the `snatcharr-admin-handlers-hardening-20261021` unit.
- The session starts through the same `RenewSession` path as the admin (fresh session id, #268).

### 4. What a viewer sees

- All reads except the four admin-only ones (`ListLogs`, the log stream, `GetConfigarrStatus`, `GetPolicyFilterOptions`), which return `403` `application/problem+json`; the SPA shows its problem note there.
- No mutation and no outbound probe: `requireAdmin` is added to every handler listed as not gated above.
- The SPA hides write controls for `viewer`, based on `isAdmin` from the session (`web/src/lib/session.svelte.ts`); today it gates only the Logs route, so the other pages gain the same check.

### 5. Tests

- Positive: an admin-group identity gets the first admin's session on both paths; a viewer-group identity gets a viewer session and a second sign-in reuses the row; an identity in both groups is admin.
- Negative: an identity in no mapped group, with no `groups` claim, with a claim of the wrong type, or with the groups header from an untrusted peer is refused; a viewer gets `403` on every mutating and probing operation, generated as a table from the OpenAPI operations so a new one cannot be missed, notifications and API keys included, and on the log stream; config with an empty `admin_groups`, a group on both lists, or a legacy allow-list set fails validation; a viewer sign-in before setup is refused and `/auth/setup` still works.
- Boundary: an empty groups list, a group name differing only in case, a header with empty segments (`a||b`), a subject longer than 64 characters or containing `:` and `@` (valid username), two concurrent first viewer sign-ins (one row), the password login against a viewer row (`401`, not `500`).
- Planted cases, each run red once before the green result is trusted: remove the admin-group mapping and the operator-is-admin test fails; map the viewer group to `FirstAdmin` and the `403` table fails.

## Rollout

The change can lock the operator out if the operator's identity is not in the admin group, so it ships behind an operator decision, not with the merge:

1. The cluster creates the admin and viewer groups in Authentik, adds the operator to the admin group and the synthetic check identity (`k8s-synthetic-check-identity-20261021`) to the viewer group, and sets the group keys alongside removing the allow-lists.
2. Proof before rollout: a read-only check that the operator's identity is a direct member of the admin group, and that the token or header the deployed provider emits for the check identity carries `groups` with the viewer group.
3. After deploy: the operator's sign-in lands as admin (session `role` read back), the served layout check runs as the viewer and passes with its screenshots.
4. Fallback: the previous image with the previous config; the admin password login (`/auth/login`) is not affected by this change.

## Consequences

- Access is managed in Authentik groups, not in SnatchArr config; adding a viewer needs no SnatchArr change.
- The deployed config must change in the same rollout (legacy allow-lists out, group keys in); startup refuses the old shape.
- A migration adds `users.external_id`. There is still no role change or user deletion query: an identity moved from the viewer to the admin group signs in as the first admin and leaves its viewer row unused.
- Proxy auth trusts the groups header exactly as far as the user header: only from the trusted CIDRs.

## Alternatives considered

- **Per-identity viewer allow-lists** (the first draft of this ADR): one config edit per identity, and the allow-lists stay a second place to keep in sync with Authentik. Not chosen by the operator.
- **Keep allow-lists as admin next to the groups**: rejected, an identity outside every group would still start as admin.
- **Check identity as admin**: rejected, a check job never holds admin rights.
- **Local viewer accounts with passwords**: rejected, a second credential store next to Authentik.
