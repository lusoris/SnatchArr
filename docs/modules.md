<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Modules

Optional modules add features outside the core search dispatch and queue management.

## Overview

Each module has a switch ([ADR-0025](adr/0025-module-switches.md)). Modules are off by default, and administrators turn them on or off at runtime without restarting the service.

Each module has its own Go package under `api/internal/` and its own HTTP routes.

## Available modules

The following optional modules are registered:

- **LLM Access (`llm`)**: exposes SnatchArr operations as Model Context Protocol (MCP) tools at `/api/v1/mcp` ([ADR-0021](adr/0021-llm-access-module.md)).
- **Watch-Aware Modes (`watchaware`)**: reads watch state from Jellyfin and Emby. The season gate and removal modes it feeds are planned in [ADR-0024](adr/0024-watch-aware-modes.md).

## Enabling and disabling modules

Administrators can toggle optional modules in the web interface under **System > Modules**, or via the REST API:

- `GET /api/v1/system/modules` lists all registered optional modules and their current state.
- `GET /api/v1/system/modules/{id}` returns the configuration and state of one module.
- `PUT /api/v1/system/modules/{id}` updates the configuration and state of one module.

When a module is disabled, its HTTP endpoints return an RFC 9457 HTTP 404 Problem JSON response, its MCP tools are omitted from tool discovery, and its background loops remain inactive without performing work.
