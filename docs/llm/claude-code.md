<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Claude Code Connection Guide

This guide describes how to connect Anthropic Claude Code to SnatchArr's Model Context Protocol (MCP) server.

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

Claude Code supports configuring MCP servers via CLI commands or configuration files.

### Option A: CLI Helper

Register the HTTP transport server using the `claude mcp add` command:

```bash
claude mcp add --transport http snatcharr http://localhost:8080/api/v1/mcp --header "Authorization: Bearer ${SNATCHARR_API_KEY}"
```

> [!IMPORTANT]
> The shell expands `${SNATCHARR_API_KEY}` before executing the command, and `claude mcp add` stores the expanded literal value into the client configuration. For dynamic environment variable expansion that avoids persisting plaintext secrets into configuration files, prefer Option B (`.mcp.json`).

### Option B: Project Configuration (`.mcp.json`)

Alternatively, define the server at the project root in `.mcp.json`, where Claude Code natively expands `${VAR}` in `url` and `headers` at runtime:

```json
{
  "mcpServers": {
    "snatcharr": {
      "type": "http",
      "url": "http://localhost:8080/api/v1/mcp",
      "headers": {
        "Authorization": "Bearer ${SNATCHARR_API_KEY}"
      }
    }
  }
}
```

---

## Agent Skills Installation

SnatchArr provides an official Agent Skills bundle complying with the `agentskills.io` standard at `skills/snatcharr/` in the repository root ([ADR-0021](../adr/0021-llm-access-module.md) section 8).

Copy the skill definition into your Claude Code skills directory:

- **Global user path**: `~/.claude/skills/snatcharr/SKILL.md` (verified in `https://code.claude.com/docs/en/skills`)
- **Workspace project path**: `.claude/skills/snatcharr/SKILL.md` (verified in `https://code.claude.com/docs/en/skills`)

```bash
# Global installation
mkdir -p ~/.claude/skills/snatcharr
cp -r skills/snatcharr/* ~/.claude/skills/snatcharr/

# Or workspace installation
mkdir -p .claude/skills/snatcharr
cp -r skills/snatcharr/* .claude/skills/snatcharr/
```

---

## Verification

To verify the connection and list available tools:

1. Run the CLI tool verification command:

   ```bash
   claude mcp list
   ```

2. Or start an interactive session and inspect active tools:

   ```bash
   claude
   /mcp
   ```

3. Confirm that standard read tools are loaded and discoverable:
   - `snatcharr_get_system_status`
   - `snatcharr_list_instances`
   - `snatcharr_list_runs`
   - `snatcharr_get_hourly_caps`

---

## Official Documentation References

- **Claude Code MCP Configuration**: Anthropic official documentation ([Claude Code MCP](https://code.claude.com/docs/en/mcp)) and `claude mcp add --help`, checked 2026-10-24.
- **Claude Code Skills Documentation**: Anthropic official documentation ([Claude Code Skills](https://code.claude.com/docs/en/skills)), checked 2026-10-24.
- **SnatchArr Architectural Record**: [ADR-0021: LLM access module](../adr/0021-llm-access-module.md).
