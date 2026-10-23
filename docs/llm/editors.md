<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Editor and IDE Connection Guide (Cursor & VS Code)

This guide describes how to connect modern editor environments—specifically Cursor and Visual Studio Code—to SnatchArr's Model Context Protocol (MCP) server.

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

Export the API key in your shell environment before launching your editor:

```bash
export SNATCHARR_API_KEY="your-snatcharr-api-key"
```

---

## Cursor Configuration

Cursor reads MCP configurations from `~/.cursor/mcp.json` (global) or `.cursor/mcp.json` (workspace root).

### Configuration Snippet (`.cursor/mcp.json`)

Create or update `.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "snatcharr": {
      "url": "http://localhost:8080/api/v1/mcp",
      "headers": {
        "Authorization": "Bearer ${env:SNATCHARR_API_KEY}"
      }
    }
  }
}
```

> [!IMPORTANT]
> The `${env:SNATCHARR_API_KEY}` syntax dynamically pulls the token from the host environment. Never commit plaintext tokens to version control.

### Cursor Skills Installation

Copy the official Agent Skills bundle from `skills/snatcharr/` in the repository root ([ADR-0021](../adr/0021-llm-access-module.md) section 8):

- **User global path**: `~/.cursor/skills/snatcharr/SKILL.md` or `~/.agents/skills/snatcharr/SKILL.md` (verified in `https://cursor.com/docs/context/skills`)
- **Workspace path**: `.cursor/skills/snatcharr/SKILL.md` or `.agents/skills/snatcharr/SKILL.md` (verified in `https://cursor.com/docs/context/skills`)

```bash
mkdir -p .cursor/skills/snatcharr
cp -r skills/snatcharr/* .cursor/skills/snatcharr/
```

### Cursor Verification

1. In Cursor, open Settings, select Features, then navigate to MCP.
2. Locate the `snatcharr` server in the list.
3. Verify that the indicator turns green and that the tool list displays standard read tools:
   - `snatcharr_get_system_status`
   - `snatcharr_list_instances`
   - `snatcharr_list_runs`
   - `snatcharr_get_hourly_caps`

---

## Visual Studio Code Configuration

Visual Studio Code supports MCP servers via workspace configuration files located at `.vscode/mcp.json` or through profile-level settings.

### Configuration Snippet (`.vscode/mcp.json`)

Create or update `.vscode/mcp.json`:

```json
{
  "mcpServers": {
    "snatcharr": {
      "type": "http",
      "url": "http://localhost:8080/api/v1/mcp",
      "headers": {
        "Authorization": "Bearer ${env:SNATCHARR_API_KEY}"
      }
    }
  }
}
```

> [!IMPORTANT]
> Use the `${env:SNATCHARR_API_KEY}` reference to keep credentials out of repository files. Ensure VS Code is launched from a shell where `SNATCHARR_API_KEY` is exported.

### Visual Studio Code Skills Installation

Copy the official Agent Skills bundle from `skills/snatcharr/`:

- **User global path**: `~/.copilot/skills/snatcharr/SKILL.md` or `~/.agents/skills/snatcharr/SKILL.md` (verified in `https://code.visualstudio.com/docs/agent-customization/agent-skills`)
- **Workspace path**: `.github/skills/snatcharr/SKILL.md` or `.agents/skills/snatcharr/SKILL.md` (verified in `https://code.visualstudio.com/docs/agent-customization/agent-skills`)

```bash
mkdir -p .github/skills/snatcharr
cp -r skills/snatcharr/* .github/skills/snatcharr/
```

### Visual Studio Code Verification

1. Open the VS Code Command Palette (`Ctrl+Shift+P` on Linux/Windows, `Cmd+Shift+P` on macOS).
2. Run **MCP: List Servers** or inspect the **MCP Output Channel** in the Output panel.
3. Confirm `snatcharr` is running and reports discovered tools including `snatcharr_get_system_status` and `snatcharr_list_instances`.

---

## Official Documentation References

- **Cursor MCP Documentation**: Cursor official reference ([Cursor Model Context Protocol](https://cursor.com/docs/context/model-context-protocol)) and ([Cursor Agent Skills](https://cursor.com/docs/context/skills)), checked 2026-10-24.
- **Visual Studio Code MCP Documentation**: Microsoft official reference ([VS Code MCP Documentation](https://code.visualstudio.com/docs/copilot/mcp)) and ([VS Code Agent Skills](https://code.visualstudio.com/docs/agent-customization/agent-skills)), checked 2026-10-24.
- **SnatchArr Architectural Record**: [ADR-0021: LLM access module](../adr/0021-llm-access-module.md).
