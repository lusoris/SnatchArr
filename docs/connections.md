<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Connections

SnatchArr connects to external services across your media automation stack to observe queues, indexers, request backlogs, and declared instances. It uses these signals to pace snatch activity without overloading your infrastructure.

The Connections hub at `/connections` gives a single operational view: are Prowlarr, download clients, Seerr, and Configarr linked and healthy?

## Overview of services

| Service | Purpose | Detailed documentation | Source of truth |
| --- | --- | --- | --- |
| **Download clients** | Observes transfer queues (qBittorrent, Transmission, Deluge, rTorrent, SABnzbd, NZBGet) to apply backpressure when downloads pile up. | [Download clients](download-clients.md) | `api/internal/dlclients/` |
| **Seerr** | Connects to Jellyseerr or Overseerr to detect pending user requests and prioritize snatching. | [Seerr](seerr.md) | `api/internal/seerr/` |
| **Prowlarr** | Connects indexers to coordinate search health, capabilities, and availability. | [Prowlarr](prowlarr.md) | `api/internal/prowlarr/` |
| **Configarr** | Syncs declared instances automatically from your Configarr configuration files. | [Configuration: Configarr](configuration.md#configarr) | `api/internal/configarr/` |

## Hub operation and health indicators

The hub displays one card per integrated service:

- **Health status**: Each card displays a `StatusIcon` reflecting operational health:
  - `ok`: All configured links are connected and reporting healthy responses.
  - `warn`: Service is operating but reporting non-critical issues (such as degraded indexers).
  - `danger`: One or more links failed health checks or returned errors during the last check.
  - `idle`: No links have been configured, or the service has never run a test check.
- **Counts and details**: Summarizes total and failing links (for example, "2 linked, 1 failing" or "linked mode, last import 5m ago").
- **Quick actions**: When no links exist, administrators see an inline "+ Add link" action directly pointing to the configuration page.

## Non-blocking resilience (UIF1)

Every service card on the hub owns its queries independently:

- If an external integration fails (e.g. returning HTTP 503 or timing out), only that service's card displays an inline `ProblemNote` with the error details.
- Other service cards continue to load and function normally.
- Shell navigation and top-level interactions remain responsive within 1 second even during an upstream outage.

## Roles and permissions

Permissions align with the service contracts defined in [ADR-0029](adr/0029-task-flow-ui.md):

- **Administrator**: Full access to all connection sub-routes, configuration forms, test actions, and delete actions.
- **Viewer**: Read-only visibility into allowed integrations (such as Prowlarr indexer status). Restricted administrative endpoints (such as `/api/v1/configarr/status` in `api/internal/httpapi/handlers_configarr.go`) are never called for viewers, preventing unauthorized HTTP 403 responses.
