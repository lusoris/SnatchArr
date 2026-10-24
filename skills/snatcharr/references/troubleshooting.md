<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->

# Troubleshooting SnatchArr MCP Integration

This reference explains common status codes, error responses, and resolution steps encountered when communicating with the SnatchArr Model Context Protocol (MCP) Streamable HTTP endpoint at `<snatcharr-url>/api/v1/mcp`.

All non-2xx HTTP responses use RFC 9457 Problem Details (`Content-Type: application/problem+json`) rendered by `api/internal/httpapi` (`type: "about:blank"`, `title`, `status`, `detail`, `instance`).

## HTTP 404 Not Found: Module Disabled

### Symptoms
Any request to `<snatcharr-url>/api/v1/mcp` returns HTTP 404 with an RFC 9457 `application/problem+json` response whose `detail` wraps `"llm module is disabled"` (from `loadEnabledSettings` in `api/internal/llm/llm.go`):

```json
{
  "type": "about:blank",
  "title": "Not Found",
  "status": 404,
  "detail": "not found: llm module is disabled",
  "instance": "/api/v1/mcp"
}
```

### Cause
The MCP server toggle `llm_enabled` is set to `false` in SnatchArr settings.

### Resolution
1. An administrator must sign in to the SnatchArr web UI.
2. Navigate to `/settings`.
3. Enable the **Enable MCP server (/api/v1/mcp)** checkbox (`llm_enabled: true`) and save changes.

---

## HTTP 401 Unauthorized: Missing or Invalid API Key

### Symptoms
The server returns HTTP 401 Unauthorized with the header:

```http
WWW-Authenticate: Bearer
```

Problem Details response (`api/internal/httpapi/problem.go` maps `domain.ErrUnauthorized` to `"authentication required"`):

```json
{
  "type": "about:blank",
  "title": "Unauthorized",
  "status": 401,
  "detail": "authentication required",
  "instance": "/api/v1/mcp"
}
```

### Cause
The client failed to provide an `Authorization: Bearer <API_KEY>` header, or provided a key that does not exist or has been revoked.

### Resolution
- Verify client configuration includes the exact header `Authorization: Bearer <API_KEY>`.
- Check on `/account` under **API keys** that the key is active and not revoked.
- If lost, create a key in the **API keys** section on `/account`.

---

## HTTP 403 Forbidden: Unscoped Key or Insufficient Scope

### Symptoms
- If the API key has no scopes, the server returns HTTP 403 Forbidden with detail wrapping `"unscoped api keys are not permitted on mcp"`:

```json
{
  "type": "about:blank",
  "title": "Forbidden",
  "status": 403,
  "detail": "forbidden: unscoped api keys are not permitted on mcp",
  "instance": "/api/v1/mcp"
}
```

- If the API key lacks the scope required for a specific tool, the server returns HTTP 403 Forbidden with the `WWW-Authenticate` header naming the missing scope:

```http
WWW-Authenticate: Bearer error="insufficient_scope", scope="core:read"
```

```json
{
  "type": "about:blank",
  "title": "Forbidden",
  "status": 403,
  "detail": "forbidden: key lacks required scope core:read",
  "instance": "/api/v1/mcp"
}
```

### Cause
The API key presented by the client either has no scopes (`"unscoped api keys are not permitted on mcp"`) or lacks the scope required to call the requested tool.

### Resolution
- Inspect the `scope="..."` attribute in the `WWW-Authenticate` response header to identify the missing scope.
- Issue a scoped API key with the required scope (e.g., `core:read`, `core:write`, `requests:read`, `download_clients:read`).
- **Agent Rule**: Connected models must **never** retry by attempting to escalate permissions or asking end users for broader keys, unless the user is an administrator who explicitly asked about permission requirements.

---

## HTTP 403 Forbidden: Cross-Origin Request Refused

### Symptoms
The server returns HTTP 403 Forbidden with detail wrapping `"cross-origin request refused"`:

```json
{
  "type": "about:blank",
  "title": "Forbidden",
  "status": 403,
  "detail": "forbidden: cross-origin request refused",
  "instance": "/api/v1/mcp"
}
```

### Cause
The incoming request included an `Origin` header whose host or port does not match the server's `Host` header. This check protects against Cross-Site Request Forgery (CSRF) and DNS rebinding attacks against local services.

### Resolution
- For direct MCP clients (Codex, Gemini, Antigravity, opencode, Claude Code, editors), do not set an `Origin` header when sending requests to `<snatcharr-url>/api/v1/mcp`.
- If routing through a reverse proxy, ensure the proxy forwards or preserves the matching `Host` header.

---

## JSON-RPC Error -32021: Tool Not Enabled by Administrator

### Symptoms
The HTTP status is 200 OK, but the JSON-RPC response contains error code `-32021` with message `"tool not enabled by administrator"`:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32021,
    "message": "tool not enabled by administrator"
  }
}
```

### Cause
The tool called is a mutating tool (`Write`), and it has not been added to the administrator's allowlist (`llm_write_tools`) in system settings.

### Resolution
- Mutating tools are disabled by default. If the operator wants the agent to perform this mutation, an administrator must enable the tool (e.g., `snatcharr_trigger_run`) on `/settings` under **Allowed MCP write tools**.
- If the tool is not opted in, the agent should report that the action is not permitted by system policy and continue with read-only analysis. Do not poll or loop retrying the tool.

---

## Tool Error: Confirmation Required (`confirm=true`)

### Symptoms
The tool execution returns `isError: true` in the MCP result content with text `"confirmation required: pass confirm=true"`:

```json
{
  "content": [
    {
      "type": "text",
      "text": "confirmation required: pass confirm=true"
    }
  ],
  "isError": true
}
```

### Cause
The tool called is classified as destructive (e.g., `snatcharr_delete_instance`, `snatcharr_reset_state`, `snatcharr_cancel_run`, `snatcharr_delete_schedule`), which irreversibly modifies or deletes state, and the client passed `confirm: false` or omitted the `confirm` parameter.

### Resolution
- Explain the planned action, the affected entity ID, and the permanent consequences to the user.
- Only when the user explicitly approves the operation, re-invoke the tool passing `"confirm": true` alongside the required path parameter (for example, camelCase `instanceId`, `runId`, or `scheduleId`):

```json
{
  "instanceId": "7b0a7019-216f-4a0f-90e9-74d17ad26d8f",
  "confirm": true
}
```
