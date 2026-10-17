<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0011: Product definition: parity with Huntarr and newtarr

- Status: accepted
- Date: 2026-10-17

## Context

No document said what a complete SnatchArr is from the point of view of a Huntarr or newtarr user. Issue #321 compared the three as a parity matrix:

- Huntarr v9.4.2 (2026-02-23), the last release before its repository was removed, read from a public archive mirror.
- newtarr v1.0.0 (2026-02-24), the elfhosted fork of Huntarr v6.6.3.
- SnatchArr as of main `e5e624fd`.

The matrix found 17 expectations already covered and 8 open gaps. Each open gap was decided as in or out of scope.

## Decision

SnatchArr is complete for a Huntarr or newtarr user when it covers the expectations below.

### Covered

Covered already, as listed in #321:

- **Apps**: Sonarr, Radarr, Lidarr, Readarr (frozen, [ADR-0008](0008-readarr-frozen-legacy-target.md)), and Whisparr v2 and v3, with several instances per app.
- **Searching**:
  - missing and cutoff-unmet searches, with items per cycle and a cycle interval
  - an hourly cap per instance
  - queue-aware pausing through download client backpressure ([ADR-0006](0006-download-client-backpressure.md))
  - schedules and pause windows
  - Sonarr season packs
- **Memory and history**: processed-item memory with reset, history, and live activity over SSE.
- **Links**: a read-only Prowlarr link.
- **Access**: login, reverse-proxy and OIDC sign-in ([Single Sign-On](../sso.md)), and a REST API with API keys described in OpenAPI 3.1.
- **Notifications**: Discord, Slack, Telegram, ntfy, Gotify, Teams and signed webhooks.
- **Installs**: compose, a Helm chart and ArgoCD.
- **Backups**: delegated to CloudNativePG or the compose volume ([Deployment](../deployment.md)).

### In scope, open

| Expectation | Issue |
| :--- | :--- |
| Pushover, Pushbullet, email (SMTP), Notifiarr and others through one Apprise channel | #348 |
| In-app log viewer for api and worker logs | #349 |
| Radarr minimum availability filter: skip movies Radarr reports as not yet available | #350 |

### Out of scope

| Expectation | Reason |
| :--- | :--- |
| TOTP two-factor login | OIDC sign-in hands two-factor login to the identity provider ([Single Sign-On](../sso.md)). A second-factor store in SnatchArr would duplicate it and add secrets to keep. |
| Recovery key or password reset | OIDC and proxy sign-in map to the single admin account, so a locked-out admin signs in through them. A recovery key would be one more long-lived credential to store and leak. |
| Local-network login bypass | It trades authentication for a network address that a reverse proxy, VPN or container network can make ambiguous. Trusted proxy-header sign-in covers the convenience case with an explicit trust boundary. |
| Plex sign-in | Plex sign-in goes through Plex's own account API, not OIDC, so it would need a Plex-specific auth path beside OIDC and proxy sign-in. Users who want Plex identities put an identity provider in front. |
| Unraid template, native Windows or macOS installs | The signed and attested container images are the release unit, and an Unraid host runs containers. Native builds would add platforms without that provenance and outside the tested matrix. |

Out of scope by design, as already recorded:

- **Swaparr (stalled downloads):** [ADR-0004](0004-no-swaparr-cleanuparr-deferred.md). Cleanuparr owns it ([ADR-0007](0007-read-only-cleanuparr-link.md)). *(Note: ADR-0012 supersedes the Cleanuparr link; see #362.)*
- **Movie Hunt, TV Hunt, Index Master, NZB Hunt, Requestarr:** these replace Radarr, Sonarr, Prowlarr, SABnzbd and Seerr. SnatchArr drives the *arr apps and links Seerr and Prowlarr; it does not replace them.

## Consequences

- **Positive**: the scope is written down. A feature request is checked against this record, and a parity gap either has an issue above or a stated reason.
- **Negative**: users migrating from Huntarr lose TOTP, Plex sign-in, LAN bypass and native installs. They have to use an identity provider or a container host instead.
- **Revisit when**: a decided-out row gets a request with a use case that its reason does not answer, or a new Huntarr-family release adds an expectation.
