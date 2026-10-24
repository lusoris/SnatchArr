<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# LLM Access and Model Context Protocol

SnatchArr (/ˈsnætʃ.ɑːr/, combining "snatch" for automated media grabbing with the "*arr" family naming) includes a native Model Context Protocol (MCP) server directly in the Go API control plane (`api/internal/llm/llm.go`, [ADR-0021](../adr/0021-llm-access-module.md)).

The module allows AI assistants, autonomous coding agents, and operational tools to inspect library health, query snatch history, diagnose missing media, and inspect download queues without third-party bridge containers or direct database access.

---

## Supported Clients

SnatchArr supports all major agent and editor runtimes on equal footing. Follow the dedicated connection tutorial for your environment:

- [Antigravity](antigravity.md): CLI and IDE integration via `agy mcp add`.
- [Claude Code](claude-code.md): CLI integration via `claude mcp add` or `.mcp.json`.
- [Codex](codex.md): CLI and workspace integration via `~/.codex/config.toml` or `codex mcp add`.
- [Editors (Cursor, VS Code)](editors.md): IDE integrations via `.cursor/mcp.json` and `.vscode/mcp.json`.
- [Gemini CLI](gemini-cli.md): CLI integration via `~/.gemini/settings.json` and `/mcp`.
- [OpenCode](opencode.md): CLI and TUI integration via `~/.config/opencode/opencode.json` or `opencode mcp add`.

---

## Architectural Invariants

### 1. Transport Mechanisms

- **Streamable HTTP transport (available today)**:
  - Served directly by the Go API control plane at `/api/v1/mcp`.
  - Conforms to the MCP 2026-07-28 specification: clients send JSON-RPC 2.0 requests via HTTP `POST` requests and receive standard JSON (`application/json`) or Server-Sent Events (`text/event-stream`).
  - Stateless: no session state or session IDs are required across HTTP requests.
- **stdio transport**:
  - A lightweight local CLI wrapper (`snatcharr mcp stdio`, or `docker exec -i -e SNATCHARR_URL -e SNATCHARR_API_KEY <container> /snatcharr-api mcp stdio` for container deployments) that proxies standard input/output streams to `/api/v1/mcp` using `SNATCHARR_URL` and `SNATCHARR_API_KEY`. Bare `-e NAME` forwards the host environment variable value into the container process ([Docker exec documentation](https://docs.docker.com/reference/cli/docker/container/exec/)).
  - Inside the container, the URL addresses the container's own listener: `SNATCHARR_URL=http://localhost:8080` (since `APP_HTTP_ADDR` defaults to `:8080`).
  - Kubernetes: `kubectl exec` cannot set environment variables and the container image is distroless (no `env` binary or shell) -> stdio from a pod is not supported; use the host binary or Streamable HTTP. No `kubectl exec` snippet.
  - Conforms to the MCP stdio transport specification: newline-delimited JSON-RPC messages on stdin/stdout, logs only on stderr. Delegating directly to the HTTP endpoint ensures authentication, rate limiting, and audit logging remain unified ([ADR-0021](../adr/0021-llm-access-module.md) section 2).

### Install the Proxy

Release assets do not ship a standalone prebuilt `snatcharr` host binary (container images carry `/snatcharr-api`). To build and install the `snatcharr` CLI binary to `$GOPATH/bin`:

```bash
go install github.com/lusoris/SnatchArr/api/cmd/snatcharr@vX.Y.Z
```

Replace `vX.Y.Z` with a release tag whose [changelog](https://github.com/lusoris/SnatchArr/blob/main/CHANGELOG.md) lists `snatcharr mcp stdio`, ideally the same version your server runs. Pin an exact tag rather than `@latest` so the build stays reproducible.

### 2. Module Toggle Switch

The LLM module is governed by a global toggle switch on the settings page (`/settings`) labeled **Enable MCP server (/api/v1/mcp)**:

- The module is disabled (`off`) by default upon initial installation.
- When disabled, any HTTP request to `/api/v1/mcp` immediately returns HTTP `404 Not Found` with an RFC 9457 `application/problem+json` payload (`loadEnabledSettings` in `api/internal/llm/llm.go`).
- An administrator must toggle the module on before clients can connect.

### 3. Authentication and Permission Scopes

Access to `/api/v1/mcp` requires a valid bearer token in the `Authorization` header:

```http
Authorization: Bearer <SNATCHARR_API_KEY>
```

Tokens are minted on the account page (`/account`) under the **API keys** section with granular permission scopes ([ADR-0021](../adr/0021-llm-access-module.md) section 3):

| Scope | Protected Domain |
| :--- | :--- |
| `core:read` / `core:write` | Instances, snatches, schedules, queue policies, strikes |
| `requests:read` / `requests:write` | Seerr links, media requests, instance imports |
| `download_clients:read` / `download_clients:write` | Download client status, configuration, discovery |
| `prowlarr:read` / `prowlarr:write` | Indexer links, sync status, connectivity tests |
| `notifications:read` / `notifications:write` | Notification channels and event routing rules |
| `admin:read` / `admin:write` | System settings, audit logs, system status |
| `adult:read` / `adult:write` | (reserved; activates when the adult module lands) |

> [!WARNING]
> Unscoped legacy API keys and tokens lacking required scopes are refused with HTTP `403 Forbidden` and emit an RFC 6750 `WWW-Authenticate: Bearer error="insufficient_scope"` challenge. Wildcard scopes do not exist.

### 4. Safety and Trust Floor

SnatchArr implements four levels of defense against unintended model actions:

1. **Read-only by default**: Read-only tools (such as `snatcharr_list_instances` or `snatcharr_get_system_status`) are discoverable and executable as soon as the module is enabled.
2. **Administrative write fences**: Mutating tools (such as `snatcharr_create_instance` or `snatcharr_trigger_run`) are hidden from `tools/list` and return JSON-RPC error `-32021` (`tool not enabled by administrator`) until an administrator explicitly selects and enables that specific tool on the settings page (`/settings`) under **Allowed MCP write tools**.
3. **Destructive confirmation guard**: Destructive tools (such as `snatcharr_delete_instance` or `snatcharr_reset_state`) require the argument `confirm: true`. Calls omitting this parameter are rejected before any state changes occur.
4. **Excluded sensitive operations**: Authentication flows, password updates, session management, and API key management (10 OpenAPI endpoints) are permanently excluded from the tool catalog to prevent privilege escalation ([ADR-0021](../adr/0021-llm-access-module.md) line 92).

### 5. Prompt Injection Defense

Torrent release names, indexer descriptions, and Usenet headers are untrusted external text that could harbor prompt injection attacks. SnatchArr isolates external text into structured JSON fields (`structuredContent`), ensuring external strings are never interpolated directly into instruction prompts or prompt templates.

---

## Agent Skills Bundle

SnatchArr ships an Agent Skills bundle conforming to the `agentskills.io` standard at `skills/snatcharr/` in the repository root ([ADR-0021](../adr/0021-llm-access-module.md) section 8).

To install the skill bundle, copy the directory into your client's designated skill path:

| Client Environment | User / Global Path | Workspace / Project Path | Verified Documentation Reference |
| :--- | :--- | :--- | :--- |
| **Antigravity** | `~/.gemini/config/skills/snatcharr/SKILL.md` | `.agents/skills/snatcharr/SKILL.md` | Built-in: `json_configs.md:8-9`, `skills.md:9-10` |
| **Claude Code** | `~/.claude/skills/snatcharr/SKILL.md` | `.claude/skills/snatcharr/SKILL.md` | Anthropic: `https://code.claude.com/docs/en/skills` |
| **Codex** | `~/.agents/skills/snatcharr/SKILL.md` | `.agents/skills/snatcharr/SKILL.md` | OpenAI: `https://learn.chatgpt.com/docs/build-skills` |
| **Editors (Cursor)** | `~/.cursor/skills/snatcharr/SKILL.md` or `~/.agents/skills/snatcharr/SKILL.md` | `.cursor/skills/snatcharr/SKILL.md` or `.agents/skills/snatcharr/SKILL.md` | Cursor: `https://cursor.com/docs/context/skills` |
| **Editors (VS Code)** | `~/.copilot/skills/snatcharr/SKILL.md` or `~/.agents/skills/snatcharr/SKILL.md` | `.github/skills/snatcharr/SKILL.md` or `.agents/skills/snatcharr/SKILL.md` | Microsoft: `https://code.visualstudio.com/docs/agent-customization/agent-skills` |
| **Gemini CLI** | `~/.gemini/skills/snatcharr/SKILL.md` or `~/.agents/skills/snatcharr/SKILL.md` | `.gemini/skills/snatcharr/SKILL.md` or `.agents/skills/snatcharr/SKILL.md` | Gemini CLI: `docs/cli/skills.md` |
| **OpenCode** | `~/.config/opencode/skills/snatcharr/SKILL.md` or `~/.agents/skills/snatcharr/SKILL.md` | `.opencode/skills/snatcharr/SKILL.md` or `.agents/skills/snatcharr/SKILL.md` | OpenCode: `https://opencode.ai/docs/skills` |

---

## Machine-Readable Documentation (`llms.txt`)

For AI models and automated agents seeking context without scraping HTML, SnatchArr publishes standardized documentation files generated via `pawamoy/mkdocs-llmstxt`:

- `/llms.txt`: Curated markdown index of documentation sections and URLs.
- `/llms-full.txt`: Complete documentation corpus concatenated into a single text file.
