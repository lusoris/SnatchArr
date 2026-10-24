<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Gemini CLI Connection Guide

This guide describes how to connect Google Gemini CLI to SnatchArr's Model Context Protocol (MCP) server.

---

## Protocol and Transport

SnatchArr exposes a native Model Context Protocol server over two transports:

- **Streamable HTTP**: `http://localhost:8080/api/v1/mcp` (sessionless JSON-RPC 2.0).
- **Subprocess stdio**: `snatcharr mcp stdio` (or `docker exec -i -e SNATCHARR_URL -e SNATCHARR_API_KEY <container> /snatcharr-api mcp stdio` for container deployments), a thin proxy that delegates to `/api/v1/mcp` via `SNATCHARR_URL` and `SNATCHARR_API_KEY`.

---

## Authentication and Prerequisites

Connecting to SnatchArr requires:

1. Enabling the MCP server on the settings page (`/settings`) via the **Enable MCP server (/api/v1/mcp)** toggle.
2. Generating a scoped bearer API key on the account page (`/account`) under the **API keys** section.

See [Module Toggle Switch](index.md#2-module-toggle-switch) and [Authentication and Permission Scopes](index.md#3-authentication-and-permission-scopes) for full scope descriptions, challenge behavior, and write-tool activation instructions.

Export the connection environment variables in your shell:

```bash
export SNATCHARR_URL="http://localhost:8080"
export SNATCHARR_API_KEY="your-snatcharr-api-key"
```

---

## Client Configuration

Gemini CLI reads MCP server declarations from `~/.gemini/settings.json` (global) or `.gemini/settings.json` (project-specific).

### Option A: Subprocess stdio Transport (`settings.json`)

To compile and install the host `snatcharr` CLI binary, see [Install the proxy](index.md#install-the-proxy).

Configure the local stdio proxy:

```json
{
  "mcpServers": {
    "snatcharr": {
      "command": "snatcharr",
      "args": ["mcp", "stdio"],
      "env": {
        "SNATCHARR_URL": "http://localhost:8080",
        "SNATCHARR_API_KEY": "$SNATCHARR_API_KEY"
      }
    }
  }
}
```

For containerized deployments:

```json
{
  "mcpServers": {
    "snatcharr": {
      "command": "docker",
      "args": ["exec", "-i", "-e", "SNATCHARR_URL", "-e", "SNATCHARR_API_KEY", "snatcharr-api", "/snatcharr-api", "mcp", "stdio"],
      "env": {
        "SNATCHARR_URL": "http://localhost:8080",
        "SNATCHARR_API_KEY": "$SNATCHARR_API_KEY"
      }
    }
  }
}
```

### Option B: Streamable HTTP Transport (`settings.json`)

Alternatively, configure the HTTP transport directly:

```json
{
  "mcpServers": {
    "snatcharr": {
      "httpUrl": "http://localhost:8080/api/v1/mcp",
      "headers": {
        "Authorization": "Bearer $SNATCHARR_API_KEY"
      }
    }
  }
}
```

> [!IMPORTANT]
> The `$SNATCHARR_API_KEY` token reference expands dynamically from your environment at runtime when settings load, per Gemini CLI configuration specification (`docs/reference/configuration.md#environment-variables`). Never paste raw bearer tokens into `settings.json`.

---

## Agent Skills Installation

SnatchArr provides an official Agent Skills bundle complying with the `agentskills.io` standard at `skills/snatcharr/` in the repository root ([ADR-0021](../adr/0021-llm-access-module.md) section 8).

Copy the skill definition into your Gemini CLI skills directory:

- **Global user path**: `~/.gemini/skills/snatcharr/SKILL.md` or `~/.agents/skills/snatcharr/SKILL.md` (verified in `https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/skills.md`)
- **Workspace project path**: `.gemini/skills/snatcharr/SKILL.md` or `.agents/skills/snatcharr/SKILL.md` (verified in `https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/skills.md`)

```bash
# Global installation
mkdir -p ~/.gemini/skills/snatcharr
cp -r skills/snatcharr/* ~/.gemini/skills/snatcharr/

# Or workspace installation
mkdir -p .gemini/skills/snatcharr
cp -r skills/snatcharr/* .gemini/skills/snatcharr/
```

---

## Verification

To verify the connection and list available tools:

1. Launch the interactive Gemini CLI:

   ```bash
   gemini
   ```

2. Inside the CLI session, run the `/mcp` command:

   ```text
   /mcp
   ```

3. Confirm that `snatcharr` is listed as connected and that read tools are available:
   - `snatcharr_get_system_status`
   - `snatcharr_list_instances`
   - `snatcharr_list_runs`
   - `snatcharr_get_hourly_caps`

---

## Official Documentation References

- **Gemini CLI MCP Server Guide**: Google Gemini CLI official documentation ([gemini-cli MCP Server Guide](https://github.com/google-gemini/gemini-cli/blob/main/docs/tools/mcp-server.md)), checked 2026-10-24.
- **Gemini CLI Configuration & Environment Expansion**: Google Gemini CLI documentation ([gemini-cli Configuration Reference](https://github.com/google-gemini/gemini-cli/blob/main/docs/reference/configuration.md#environment-variables)), checked 2026-10-24.
- **Gemini CLI Skills Documentation**: Google Gemini CLI documentation ([gemini-cli Skills Guide](https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/skills.md)), checked 2026-10-24.
- **SnatchArr Architectural Record**: [ADR-0021: LLM access module](../adr/0021-llm-access-module.md).
