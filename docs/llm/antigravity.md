<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Antigravity Connection Guide

This guide describes how to connect Antigravity CLI and IDE runtimes to SnatchArr's Model Context Protocol (MCP) server.

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

Antigravity supports configuring MCP servers via CLI commands (`agy mcp add`) or configuration files (`~/.gemini/config/mcp_config.json`).

### Option A: Subprocess stdio Transport

To compile and install the host `snatcharr` CLI binary, see [Install the proxy](index.md#install-the-proxy).

#### CLI Registration

Register the local CLI proxy using `agy mcp add`:

```bash
agy mcp add --env SNATCHARR_URL="http://localhost:8080" --env SNATCHARR_API_KEY="${SNATCHARR_API_KEY}" snatcharr snatcharr mcp stdio
```

For containerized deployments where the binary `/snatcharr-api` runs inside a container:

```bash
agy mcp add --env SNATCHARR_URL="http://localhost:8080" --env SNATCHARR_API_KEY="${SNATCHARR_API_KEY}" snatcharr docker exec -i -e SNATCHARR_URL -e SNATCHARR_API_KEY snatcharr-api /snatcharr-api mcp stdio
```

#### Configuration File (`mcp_config.json`)

Alternatively, configure the server in `~/.gemini/config/mcp_config.json`:

```json
{
  "mcpServers": {
    "snatcharr": {
      "command": "snatcharr",
      "args": ["mcp", "stdio"],
      "env": {
        "SNATCHARR_URL": "http://localhost:8080",
        "SNATCHARR_API_KEY": "your-snatcharr-api-key"
      }
    }
  }
}
```

### Option B: Streamable HTTP Transport

Register the SnatchArr Streamable HTTP endpoint using `agy mcp add`:

```bash
agy mcp add --header "Authorization: Bearer ${SNATCHARR_API_KEY}" snatcharr http://localhost:8080/api/v1/mcp
```

> [!IMPORTANT]
> In `agy mcp add`, options must appear before the server name (`agy mcp add [flags] <name> <commandOrUrl>`). When run in an interactive shell, `${SNATCHARR_API_KEY}` is expanded immediately by the shell, and the CLI stores the resulting header string literally into the configuration file as given.

---

## Agent Skills Installation

SnatchArr provides an official Agent Skills bundle complying with the `agentskills.io` standard at `skills/snatcharr/` in the repository root ([ADR-0021](../adr/0021-llm-access-module.md) section 8).

Copy the skill definition into your Antigravity skills directory:

- **Global user path**: `~/.gemini/config/skills/snatcharr/SKILL.md` (verified in `~/.gemini/antigravity-cli/builtin/skills/agy-customizations/docs/json_configs.md:8-9`)
- **Workspace project path**: `.agents/skills/snatcharr/SKILL.md` (verified in `~/.gemini/antigravity-cli/builtin/skills/agy-customizations/docs/skills.md:9-10`)

```bash
# Global installation
mkdir -p ~/.gemini/config/skills/snatcharr
cp -r skills/snatcharr/* ~/.gemini/config/skills/snatcharr/

# Or workspace installation
mkdir -p .agents/skills/snatcharr
cp -r skills/snatcharr/* .agents/skills/snatcharr/
```

---

## Verification

To verify the connection and list available tools:

1. Run the CLI tool verification command:

   ```bash
   agy mcp list
   ```

2. Alternatively, in the Antigravity IDE, open the **Additional Options (...)** menu, select **MCP Servers**, and confirm `snatcharr` is listed with a healthy status.

3. Confirm that standard read tools are loaded and discoverable:
   - `snatcharr_get_system_status`
   - `snatcharr_list_instances`
   - `snatcharr_list_runs`
   - `snatcharr_get_hourly_caps`

---

## Official Documentation References

- **Antigravity Customization Documentation**: Built-in manuals (`~/.gemini/antigravity-cli/builtin/skills/agy-customizations/docs/mcp_servers.md`, `json_configs.md:8-9`, `skills.md:9-10`), checked 2026-10-24.
- **SnatchArr Architectural Record**: [ADR-0021: LLM access module](../adr/0021-llm-access-module.md).
