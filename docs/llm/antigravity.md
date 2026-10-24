<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Antigravity Connection Guide

This guide describes how to connect Antigravity CLI and IDE runtimes to SnatchArr's Model Context Protocol (MCP) server.

---

## Protocol and Transport

SnatchArr exposes a native Model Context Protocol server over Streamable HTTP:

- **Endpoint**: `http://localhost:8080/api/v1/mcp` (adjust the host and port for your deployment).
- **Transport**: Streamable HTTP (sessionless JSON-RPC 2.0).
- **Subprocess stdio**: A local CLI wrapper (`snatcharr mcp stdio`) is planned for a future release; current configurations connect directly over HTTP.

---

## Authentication and Prerequisites

Connecting to SnatchArr requires:

1. Enabling the MCP server on the settings page (`/settings`) via the **Enable MCP server (/api/v1/mcp)** toggle.
2. Generating a scoped bearer API key on the account page (`/account`) under the **API keys** section.

See [Module Toggle Switch](index.md#2-module-toggle-switch) and [Authentication and Permission Scopes](index.md#3-authentication-and-permission-scopes) for full scope descriptions, challenge behavior, and write-tool activation instructions.

Export the API key in your shell environment:

```bash
export SNATCHARR_API_KEY="your-snatcharr-api-key"
```

---

## Client Configuration

Antigravity registers MCP servers using the `agy mcp add` CLI command.

### CLI Registration

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
