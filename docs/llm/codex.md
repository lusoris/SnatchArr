<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# OpenAI Codex Connection Guide

This guide describes how to connect OpenAI Codex to SnatchArr's Model Context Protocol (MCP) server.

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

Codex manages MCP server connections using TOML configuration files or the `codex mcp` CLI.

### Option A: Configuration File (`config.toml`)

Add the `snatcharr` server block to `~/.codex/config.toml` (global) or `.codex/config.toml` (workspace):

```toml
[mcp_servers.snatcharr]
url = "http://localhost:8080/api/v1/mcp"
bearer_token_env_var = "SNATCHARR_API_KEY"
```

> [!IMPORTANT]
> In Codex TOML configurations, `http_headers` only accepts static string literals and does not support environment variable interpolation. Use `bearer_token_env_var` to supply your API key dynamically from the named environment variable without committing secrets to disk. For custom non-bearer headers, Codex provides `env_http_headers` to map header names to environment variables.

### Option B: CLI Helper

Register the server using the Codex CLI:

```bash
codex mcp add snatcharr --url http://localhost:8080/api/v1/mcp --bearer-token-env-var SNATCHARR_API_KEY
```

---

## Agent Skills Installation

SnatchArr provides an official Agent Skills bundle complying with the `agentskills.io` standard at `skills/snatcharr/` in the repository root ([ADR-0021](../adr/0021-llm-access-module.md) section 8).

Copy the skill definition into your Codex skills directory:

- **Global user path**: `~/.agents/skills/snatcharr/SKILL.md` ([Codex skills](https://learn.chatgpt.com/docs/build-skills))
- **Workspace project path**: `.agents/skills/snatcharr/SKILL.md`; Codex scans `.agents/skills` in every directory from the working directory up to the repository root ([Codex skills](https://learn.chatgpt.com/docs/build-skills))

```bash
# Global installation
mkdir -p ~/.agents/skills/snatcharr
cp -r skills/snatcharr/* ~/.agents/skills/snatcharr/

# Or workspace installation
mkdir -p .agents/skills/snatcharr
cp -r skills/snatcharr/* .agents/skills/snatcharr/
```

---

## Verification

To verify the connection and list available tools:

1. Run the Codex MCP status command:

   ```bash
   codex mcp list
   ```

2. Confirm that `snatcharr` is displayed as an active server and that standard read tools are discoverable:
   - `snatcharr_get_system_status`
   - `snatcharr_list_instances`
   - `snatcharr_list_runs`
   - `snatcharr_get_hourly_caps`

---

## Official Documentation References

- **OpenAI Codex MCP Documentation**: OpenAI official reference ([OpenAI MCP CLI Guide](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)) ([Codex skills](https://learn.chatgpt.com/docs/build-skills)) and `codex mcp add --help`, checked 2026-10-24.
- **SnatchArr Architectural Record**: [ADR-0021: LLM access module](../adr/0021-llm-access-module.md).
