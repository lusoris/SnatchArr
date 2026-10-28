<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Claude Code Connection Guide

This guide describes how to connect Anthropic Claude Code to SnatchArr's Model Context Protocol (MCP) server.

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

Claude Code supports configuring MCP servers via CLI commands or configuration files.

### Option A: Subprocess stdio Transport

To compile and install the host `snatcharr` CLI binary, see [Install the proxy](index.md#install-the-proxy).

#### CLI Registration

Register the local CLI proxy using `claude mcp add`:

```bash
claude mcp add snatcharr -e SNATCHARR_URL="http://localhost:8080" -e SNATCHARR_API_KEY="${SNATCHARR_API_KEY}" -- snatcharr mcp stdio
```

For containerized deployments where the binary `/snatcharr-api` runs inside a container:

```bash
claude mcp add snatcharr -e SNATCHARR_URL="http://localhost:8080" -e SNATCHARR_API_KEY="${SNATCHARR_API_KEY}" -- docker exec -i -e SNATCHARR_URL -e SNATCHARR_API_KEY snatcharr-api /snatcharr-api mcp stdio
```

#### Project Configuration (`.mcp.json`)

Alternatively, configure the stdio server at the project root in `.mcp.json`, where Claude Code natively expands `${VAR}` in `env`:

```json
{
  "mcpServers": {
    "snatcharr": {
      "command": "snatcharr",
      "args": ["mcp", "stdio"],
      "env": {
        "SNATCHARR_URL": "http://localhost:8080",
        "SNATCHARR_API_KEY": "${SNATCHARR_API_KEY}"
      }
    }
  }
}
```

### Option B: Streamable HTTP Transport

#### CLI Registration

Register the HTTP transport server using `claude mcp add`:

```bash
claude mcp add --transport http snatcharr http://localhost:8080/api/v1/mcp --header "Authorization: Bearer ${SNATCHARR_API_KEY}"
```

> [!IMPORTANT]
> The shell expands `${SNATCHARR_API_KEY}` before executing the command, and `claude mcp add` stores the expanded literal value into the client configuration. For dynamic environment variable expansion that avoids persisting plaintext secrets into configuration files, prefer Option A or Option B (`.mcp.json`).

#### Project Configuration (`.mcp.json`)

Alternatively, define the HTTP server in `.mcp.json`:

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

This repository's own `.mcp.json` carries this entry with the URL read from `${SNATCHARR_URL:-http://localhost:8080}`, so the server started by `make dev` works without extra setup. Export `SNATCHARR_URL` and `SNATCHARR_API_KEY` in the shell that starts Claude Code; the key never goes into the file. With `SNATCHARR_API_KEY` unset, `claude mcp list` warns about the missing variable and the server answers `401`.

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
