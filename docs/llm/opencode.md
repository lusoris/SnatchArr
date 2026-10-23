<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# OpenCode Connection Guide

This guide describes how to connect OpenCode to SnatchArr's Model Context Protocol (MCP) server.

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

OpenCode supports configuring MCP servers via `opencode.json` (or `opencode.jsonc`) or via the `opencode mcp` CLI.

### Option A: Configuration File (`opencode.json`)

Add the `snatcharr` server under the `mcp` key in `~/.config/opencode/opencode.json` (global) or `opencode.json` (workspace):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "snatcharr": {
      "type": "remote",
      "url": "http://localhost:8080/api/v1/mcp",
      "enabled": true,
      "headers": {
        "Authorization": "Bearer {env:SNATCHARR_API_KEY}"
      }
    }
  }
}
```

> [!IMPORTANT]
> OpenCode's `{env:VARIABLE_NAME}` syntax interpolates the secret from your environment dynamically at runtime. Never place raw bearer credentials into the JSON configuration file.

### Option B: CLI Helper

Register the remote MCP server using the CLI:

```bash
opencode mcp add snatcharr --url http://localhost:8080/api/v1/mcp --header 'Authorization=Bearer {env:SNATCHARR_API_KEY}'
```

> [!IMPORTANT]
> Single quotes around `'Authorization=Bearer {env:SNATCHARR_API_KEY}'` are mandatory. Single quotes protect the `{env:...}` block from premature shell expansion or brace interpretation, ensuring the literal template string is written to `opencode.json` for OpenCode to resolve at runtime.

---

## Agent Skills Installation

SnatchArr provides an official Agent Skills bundle complying with the `agentskills.io` standard at `skills/snatcharr/` in the repository root ([ADR-0021](../adr/0021-llm-access-module.md) section 8).

Copy the skill definition into your OpenCode skills directory:

- **Global user path**: `~/.config/opencode/skills/snatcharr/SKILL.md` or `~/.agents/skills/snatcharr/SKILL.md` (verified in `https://opencode.ai/docs/skills`)
- **Workspace project path**: `.opencode/skills/snatcharr/SKILL.md` or `.agents/skills/snatcharr/SKILL.md` (verified in `https://opencode.ai/docs/skills`)

```bash
# Global installation
mkdir -p ~/.config/opencode/skills/snatcharr
cp -r skills/snatcharr/* ~/.config/opencode/skills/snatcharr/

# Or workspace installation
mkdir -p .opencode/skills/snatcharr
cp -r skills/snatcharr/* .opencode/skills/snatcharr/
```

---

## Verification

To verify the connection and list available tools:

1. Run the CLI tool verification command:

   ```bash
   opencode mcp list
   ```

2. Confirm that `snatcharr` reports an active status and that standard read tools are listed:
   - `snatcharr_get_system_status`
   - `snatcharr_list_instances`
   - `snatcharr_list_runs`
   - `snatcharr_get_hourly_caps`

---

## Official Documentation References

- **OpenCode MCP Documentation**: OpenCode official reference ([OpenCode MCP Servers](https://opencode.ai/docs/mcp-servers)), checked 2026-10-24.
- **OpenCode Skills Documentation**: OpenCode official reference ([OpenCode Skills](https://opencode.ai/docs/skills)), checked 2026-10-24.
- **SnatchArr Architectural Record**: [ADR-0021: LLM access module](../adr/0021-llm-access-module.md).
