<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0023: Viewer sign-in through single sign-on

- Status: proposed (draft; the sign-in model and how the operator stays admin wait for the operator's decision `snatcharr-served-identity-role`)
- Date: 2026-10-21

## Context

SnatchArr has two roles, `admin` and `viewer` (`api/internal/domain/user.go`, `User.role` in `api/openapi/openapi.yaml`), and every mutating handler plus three reads check for `admin` (`requireAdmin`, `api/internal/httpapi/handlers_instances.go`).

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

- An identity on both an admin list and a viewer list is refused at config validation, so a typo can never decide the role silently.
- `allowed_subjects`, `allowed_emails` and `allowed_users` keep mapping to the first admin, so the operator's own sign-in does not change.

### 2. Provisioning

- The first viewer sign-in of an identity creates a `users` row with role `viewer`, username `oidc:<sub>` or `proxy:<user>`, and no password hash, so it cannot use the password login.
- Later sign-ins reuse that row. Removing the identity from the viewer list refuses the next sign-in; the row stays for the audit trail.
- The session starts through the same `RenewSession` path as the admin (fresh session id, #268).

### 3. What a viewer sees

- All reads except the three admin-only ones (`ListLogs`, `GetConfigarrStatus`, `GetPolicyFilterOptions`), which return `403` `application/problem+json`; the SPA shows its problem note there.
- No mutation: every write handler already calls `requireAdmin`.
- The SPA hides write controls for `viewer`, based on the `role` of the signed-in user (part of this unit).

### 4. Tests

- Positive: a listed viewer subject, email and proxy user each get a viewer session; a second sign-in reuses the row.
- Negative: a viewer gets `403` on every mutating operation (table over the OpenAPI operations); an unlisted identity is refused; an identity on both lists fails config validation.
- Boundary: empty viewer lists keep today's behaviour; a viewer row has no password hash and fails the password login.
- Planted case: map the viewer list to `FirstAdmin` and the `403` table turns red.

## Consequences

- The served layout check signs in as a viewer identity the cluster creates in Authentik; the three admin-only routes are checked in their `403` state, not with data.
- One more allow-list to keep in sync with Authentik; config validation refuses overlaps.
- Proxy auth trusts the user header from the ingress exactly as it does today; the viewer list adds no new trust.

## Alternatives considered

- **Group claim mapping** (an Authentik group such as `snatcharr-viewers` in a `groups` claim): fewer config edits per identity, but it ties the role to a claim shape SnatchArr does not read today and to a scope that has to be requested. It remains an option for the operator.
- **Check identity as admin**: rejected, a check job never holds admin rights.
- **Local viewer accounts with passwords**: rejected, a second credential store next to Authentik.

## Open questions for the operator

- Allow-list per identity (this draft) or group claim mapping.
- Whether the operator's own sign-in moves to a per-identity admin row instead of the first admin.
