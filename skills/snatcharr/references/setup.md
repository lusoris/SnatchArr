<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->

# Setting Up SnatchArr MCP and Agent Skills

This guide walks operators through enabling the Model Context Protocol (MCP) Streamable HTTP endpoint (`<snatcharr-url>/api/v1/mcp`, default HTTP listen port `8080` via `APP_HTTP_ADDR`) in SnatchArr, configuring scoped credentials, opting in to mutating tools, and installing the Agent Skill across supported AI clients.

## 1. Enable the MCP Server

By default, SnatchArr disables the MCP endpoint to prevent unexpected exposure.

1. Sign in to the SnatchArr web UI as an administrator.
2. Navigate to `/settings`.
3. Enable the **Enable MCP server (/api/v1/mcp)** checkbox (`llm_enabled: true`).
4. Save settings.

Alternatively, enable the module via the REST API by issuing `PUT /api/v1/settings` with `"llm_enabled": true`.

> [!NOTE]
> If `llm_enabled` is false, all requests to `<snatcharr-url>/api/v1/mcp` are rejected with HTTP 404 Problem Details (`not found: llm module is disabled`).

## 2. Mint a Scoped API Key

The MCP endpoint requires bearer authentication and enforces capability-based access control.

1. Navigate to `/account` in the web UI (under the **API keys** section) or call `POST /api/v1/auth/api-keys`.
2. Enter a descriptive **Name** and create a key in the **API keys** section (or pass `name` and `scopes` to `POST /api/v1/auth/api-keys`).
3. Assign only the permission scopes required for the assistant's tasks:
   - `core:read`: instances, snatches, schedules, queue policies, strikes.
   - `core:write`: instances, snatches, schedules, queue policies, strikes.
   - `requests:read`: Seerr links, media requests, instance imports.
   - `requests:write`: Seerr links, media requests, instance imports.
   - `download_clients:read`: client status, configuration, discovery.
   - `download_clients:write`: client status, configuration, discovery.
   - `prowlarr:read`: indexer links, status, connectivity.
   - `prowlarr:write`: indexer links, status, connectivity.
   - `notifications:read`: channels, routing rules, test dispatches.
   - `notifications:write`: channels, routing rules, test dispatches.
   - `admin:read`: system settings, audit logs, system status.
   - `admin:write`: system settings, audit logs, system status.
   - `adult:read`: reserved; activates when the adult module lands.
   - `adult:write`: reserved; activates when the adult module lands.
4. Copy the generated API key immediately. Keys are stored as `argon2id` hashes (`api/internal/auth/service.go`) and shown once at creation.

> [!IMPORTANT]
> Unscoped API keys are refused by `<snatcharr-url>/api/v1/mcp` with HTTP 403 Forbidden (`forbidden: unscoped api keys are not permitted on mcp`). Every key used with MCP must hold at least one permission scope.

## 3. Opt-in Allowed MCP Write Tools

To protect against unintended state changes, all 43 mutating tools require explicit per-tool opt-in by an administrator:

1. Navigate to `/settings`.
2. Under **Allowed MCP write tools** (`llm_write_tools`), tools are grouped per scope:
   - **Core operations (instances, runs, policies, schedules)**
   - **Administrative operations (settings)**
   - **Download client operations**
   - **Seerr request operations**
   - **Prowlarr operations**
   - **Notification operations**
3. Select the specific write tools connected agents may invoke (for example, `snatcharr_trigger_run` and `snatcharr_preview_run`) and save settings.

Any write tool invoked without prior administrator opt-in is rejected with JSON-RPC error code `-32021` (`tool not enabled by administrator`).

## 4. Install the Agent Skill

The SnatchArr repository ships the canonical skill bundle at `skills/snatcharr/`. Copy the whole folder into the client's skills directory; per-client paths and connection guides live in the documentation under `docs/llm/`.
