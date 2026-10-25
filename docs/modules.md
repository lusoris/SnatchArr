<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Modules

Optional modules provide modular capabilities that extend SnatchArr beyond core search dispatching and queue management.

## Overview

In accordance with [ADR-0025](adr/0025-module-switches.md), SnatchArr features a unified optional module switch mechanism. Modules remain disabled by default and can be toggled by administrators at runtime without service restarts.

Each module maintains strict isolation across Go packages, database tables, HTTP endpoints, and frontend user interfaces.

## Available modules

The following optional modules are supported or planned:

- **LLM Access (`llm`)**: Model Context Protocol (MCP) tool exposure and AI agent orchestration for autonomous media operations ([ADR-0021](adr/0021-llm-access-module.md)).
- **Watch-Aware Modes (`watchaware`)**: Synchronizes watch status from Jellyfin and Emby to prioritize snatching media that users are currently watching ([ADR-0024](adr/0024-watch-aware-modes.md)).

## Enabling and disabling modules

Administrators can toggle optional modules in the web interface under **System > Modules**, or programmatically via the REST API:

- `GET /api/v1/system/modules` lists all registered optional modules and their current state.
- `GET /api/v1/system/modules/{id}` retrieves module details and configuration.
- `PUT /api/v1/system/modules/{id}` toggles the enabled state or updates configuration.

Disabling a module immediately terminates its background loops and returns an RFC 9457 HTTP 404 response on any routes owned by that module.
