# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Run an MCP dogfood pass against a deployed SnatchArr instance and record its status.

Checks `/api/v1/system/status` to confirm the running instance serves the target commit,
exercises the MCP Streamable HTTP endpoint (`/api/v1/mcp`) by listing all tools and
invoking every read-only tool with no required arguments, and posts the GitHub commit
status `snatcharr/dogfood` when `--post` is set. Never logs the API key or tool outputs.
"""

import argparse
import contextlib
import http.server
import io
import json
import os
import re
import subprocess
import sys
import threading
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any

HTTP_TIMEOUT_SECONDS = 30
GH_TIMEOUT_SECONDS = 60
MAX_TOOL_PAGES = 20
MAX_TOOL_CALLS = 500
MIN_COMMIT_PREFIX = 7
DOGFOOD_CONTEXT = "snatcharr/dogfood"
MCP_PROTOCOL_VERSION = "2025-06-18"
MCP_CLIENT_NAME = "snatcharr-dogfood"
REPO_RE = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
COMMIT_RE = re.compile(r"^[0-9a-f]{7,40}$")
INSTANCE_RE = re.compile(r"^[a-z0-9][a-z0-9-]{0,31}$")


class CheckError(Exception):
    """HTTP transport error, invalid JSON, or invalid arguments."""


class SelfTestError(Exception):
    """A planted self-test case did not behave as required."""


@dataclass
class MCPSession:
    """State for an MCP Streamable HTTP session."""

    base_url: str
    api_key: str
    session_id: str | None = None
    next_id: int = 1


def commit_matches(served: str, expected: str) -> bool:
    """Return True when served equals expected or is a valid short prefix of expected."""
    if not served or not expected:
        return False
    if served == expected:
        return True
    if len(served) >= MIN_COMMIT_PREFIX and expected.startswith(served):
        return True
    return False


def parse_mcp_body(content_type: str, raw_body: str) -> dict[str, Any]:
    """Parse JSON or the last SSE `data:` line from an MCP HTTP response body."""
    if not raw_body.strip():
        return {}
    payload_text = raw_body
    if "text/event-stream" in content_type.lower():
        data_lines = [
            line[5:].strip()
            for line in raw_body.splitlines()
            if line.startswith("data:") and line[5:].strip()
        ]
        if not data_lines:
            raise CheckError("MCP event-stream response contained no data: line")
        payload_text = data_lines[-1]
    try:
        parsed = json.loads(payload_text)
    except ValueError as error:
        raise CheckError("invalid JSON in MCP response") from error
    if not isinstance(parsed, dict):
        raise CheckError("MCP response is not a JSON object")
    return parsed


def http_get_json(url: str, api_key: str) -> dict[str, Any]:
    """Perform a Bearer-authenticated GET request and return the parsed JSON object."""
    req = urllib.request.Request(
        url,
        headers={
            "Authorization": f"Bearer {api_key}",
            "Accept": "application/json",
        },
        method="GET",
    )
    try:
        with urllib.request.urlopen(req, timeout=HTTP_TIMEOUT_SECONDS) as resp:
            raw = resp.read().decode("utf-8")
    except urllib.error.HTTPError as error:
        raise CheckError(f"GET {url} failed with HTTP {error.code}") from error
    except (OSError, urllib.error.URLError) as error:
        raise CheckError(f"GET {url} failed: {error.reason if hasattr(error, 'reason') else error}") from error
    try:
        data = json.loads(raw)
    except ValueError as error:
        raise CheckError(f"GET {url} returned invalid JSON") from error
    if not isinstance(data, dict):
        raise CheckError(f"GET {url} did not return a JSON object")
    return data


def mcp_post(session: MCPSession, payload: dict[str, Any]) -> dict[str, Any]:
    """Send one JSON-RPC message over MCP Streamable HTTP and return the parsed response."""
    url = f"{session.base_url}/api/v1/mcp"
    headers = {
        "Authorization": f"Bearer {session.api_key}",
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
    }
    if session.session_id:
        headers["Mcp-Session-Id"] = session.session_id
    body = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=body, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=HTTP_TIMEOUT_SECONDS) as resp:
            sid = resp.headers.get("Mcp-Session-Id")
            if sid:
                session.session_id = sid
            ctype = resp.headers.get("Content-Type") or ""
            raw = resp.read().decode("utf-8")
    except urllib.error.HTTPError as error:
        raise CheckError(f"POST {url} failed with HTTP {error.code}") from error
    except (OSError, urllib.error.URLError) as error:
        raise CheckError(f"POST {url} failed: {error.reason if hasattr(error, 'reason') else error}") from error
    return parse_mcp_body(ctype, raw)


def mcp_request(
    session: MCPSession, method: str, params: dict[str, Any] | None = None
) -> dict[str, Any]:
    """Send a JSON-RPC request with an incrementing id and return the response."""
    req_id = session.next_id
    session.next_id += 1
    payload: dict[str, Any] = {"jsonrpc": "2.0", "id": req_id, "method": method}
    if params is not None:
        payload["params"] = params
    return mcp_post(session, payload)


def mcp_initialize(session: MCPSession) -> None:
    """Run MCP initialize handshake and send notifications/initialized."""
    resp = mcp_request(
        session,
        "initialize",
        {
            "protocolVersion": MCP_PROTOCOL_VERSION,
            "capabilities": {},
            "clientInfo": {"name": MCP_CLIENT_NAME, "version": "1.0.0"},
        },
    )
    if "error" in resp:
        raise CheckError("MCP initialize returned a JSON-RPC error")
    mcp_post(session, {"jsonrpc": "2.0", "method": "notifications/initialized"})


def mcp_list_tools(session: MCPSession) -> list[dict[str, Any]]:
    """List all MCP tools across paginated responses, bounded by MAX_TOOL_PAGES."""
    tools: list[dict[str, Any]] = []
    cursor: str | None = None
    for page in range(MAX_TOOL_PAGES):
        params: dict[str, Any] = {"cursor": cursor} if cursor else {}
        resp = mcp_request(session, "tools/list", params)
        if "error" in resp or not isinstance(resp.get("result"), dict):
            raise CheckError("MCP tools/list failed")
        result = resp["result"]
        page_tools = result.get("tools")
        if not isinstance(page_tools, list):
            raise CheckError("MCP tools/list result missing tools list")
        tools.extend(entry for entry in page_tools if isinstance(entry, dict))
        next_cursor = result.get("nextCursor")
        if not next_cursor:
            return tools
        if not isinstance(next_cursor, str) or page == MAX_TOOL_PAGES - 1:
            raise CheckError("MCP tools/list exceeded page bound")
        cursor = next_cursor
    return tools


def is_callable_readonly(tool: dict[str, Any]) -> bool:
    """Return True when tool has annotations.readOnlyHint=True and no required arguments."""
    annotations = tool.get("annotations")
    if not isinstance(annotations, dict) or annotations.get("readOnlyHint") is not True:
        return False
    schema = tool.get("inputSchema")
    if isinstance(schema, dict):
        required = schema.get("required")
        if required is not None and (not isinstance(required, list) or len(required) > 0):
            return False
    name = tool.get("name")
    return isinstance(name, str) and bool(name)


def call_readonly_tools(
    session: MCPSession, tools: list[dict[str, Any]]
) -> tuple[int, int]:
    """Call every eligible read-only tool with `{}` and return (called_count, failed_count)."""
    readonly = [tool for tool in tools if is_callable_readonly(tool)]
    if len(readonly) > MAX_TOOL_CALLS:
        raise CheckError(f"read-only tool count {len(readonly)} exceeds {MAX_TOOL_CALLS}")
    failed = 0
    for tool in readonly:
        resp = mcp_request(
            session, "tools/call", {"name": tool["name"], "arguments": {}}
        )
        result = resp.get("result")
        if (
            "error" in resp
            or not isinstance(result, dict)
            or result.get("isError") is True
        ):
            failed += 1
    return len(readonly), failed


def record_status(
    repository: str, commit: str, state: str, description: str, post: bool
) -> None:
    """Post or dry-run print the snatcharr/dogfood commit status on GitHub."""
    command = [
        "gh",
        "api",
        f"repos/{repository}/statuses/{commit}",
        "-f",
        f"state={state}",
        "-f",
        f"context={DOGFOOD_CONTEXT}",
        "-f",
        f"description={description}",
    ]
    if not post:
        print(f"dry-run: {' '.join(command)}")
        return
    try:
        result = subprocess.run(
            command,
            capture_output=True,
            text=True,
            timeout=GH_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise CheckError(f"gh api statuses did not complete: {error}") from error
    if result.returncode != 0:
        detail = result.stderr.strip() or f"exit {result.returncode}"
        raise CheckError(f"gh api statuses failed: {detail}")


def validate_target(
    repository: str, commit: str, instance: str, base_url: str, api_key: str
) -> None:
    """Validate repository, commit SHA, instance label, and credentials."""
    if not REPO_RE.fullmatch(repository):
        raise CheckError(f"invalid repository: {repository!r}")
    if not COMMIT_RE.fullmatch(commit):
        raise CheckError(f"invalid commit SHA: {commit!r}")
    if not INSTANCE_RE.fullmatch(instance):
        raise CheckError(f"invalid instance label: {instance!r}")
    if not base_url or not api_key:
        raise CheckError("SNATCHARR_URL and SNATCHARR_API_KEY must be set")


def run_dogfood(
    repository: str,
    commit: str,
    instance: str,
    base_url: str,
    api_key: str,
    post: bool,
) -> int:
    """Verify served commit, run MCP read-only dogfood calls, and record status."""
    validate_target(repository, commit, instance, base_url, api_key)
    clean_url = base_url.rstrip("/")
    status_data = http_get_json(f"{clean_url}/api/v1/system/status", api_key)
    served_commit = status_data.get("commit")
    if not isinstance(served_commit, str) or not commit_matches(served_commit, commit):
        print(
            f"dogfood: instance={instance} served commit {served_commit!r} "
            f"does not match {commit[:12]}"
        )
        record_status(
            repository,
            commit,
            "failure",
            f"MCP dogfood on {instance}: commit mismatch",
            post,
        )
        return 3
    session = MCPSession(base_url=clean_url, api_key=api_key)
    mcp_initialize(session)
    tools = mcp_list_tools(session)
    called, failed = call_readonly_tools(session, tools)
    print(
        f"dogfood: instance={instance} served_commit={served_commit} "
        f"{len(tools)} tools listed, {called} read-only calls, {failed} failed"
    )
    if called >= 1 and failed == 0:
        record_status(
            repository,
            commit,
            "success",
            f"MCP dogfood on {instance}: {called} read-only calls passed",
            post,
        )
        return 0
    record_status(
        repository,
        commit,
        "failure",
        f"MCP dogfood on {instance}: {called} read-only calls, {failed} failed",
        post,
    )
    return 3


STUB_COMMIT = "a1b2c3d4e5f60000000000000000000000000000"
STUB_SERVED_SHORT = "a1b2c3d4e5f6"
STUB_WRONG_COMMIT = "ffffffffffff"
STUB_TOKEN = "stub-token-not-a-secret"
STUB_SESSION_ID = "stub-mcp-session-1"


def stub_tools_for_mode(mode: str, cursor: str | None) -> dict[str, Any]:
    """Return tools/list result payload for a self-test stub mode and cursor."""
    if mode == "zero_readonly":
        return {
            "tools": [
                {
                    "name": "snatcharr_trigger_run",
                    "annotations": {"readOnlyHint": False},
                    "inputSchema": {"type": "object"},
                },
                {
                    "name": "snatcharr_get_instance",
                    "annotations": {"readOnlyHint": True},
                    "inputSchema": {"type": "object", "required": ["id"]},
                },
            ]
        }
    if not cursor:
        return {
            "tools": [
                {
                    "name": "snatcharr_get_system_status",
                    "annotations": {"readOnlyHint": True},
                    "inputSchema": {"type": "object"},
                },
                {
                    "name": "snatcharr_get_instance",
                    "annotations": {"readOnlyHint": True},
                    "inputSchema": {"type": "object", "required": ["id"]},
                },
            ],
            "nextCursor": "page-2",
        }
    return {
        "tools": [
            {
                "name": "snatcharr_list_instances",
                "annotations": {"readOnlyHint": True},
                "inputSchema": {"type": "object", "required": []},
            },
            {
                "name": "snatcharr_trigger_run",
                "annotations": {"readOnlyHint": False},
                "inputSchema": {"type": "object"},
            },
        ]
    }


def send_stub_json(
    handler: http.server.BaseHTTPRequestHandler,
    payload: dict[str, Any],
    session_id: str | None = None,
) -> None:
    """Write a 200 JSON response to the stub HTTP client."""
    raw = json.dumps(payload).encode("utf-8")
    handler.send_response(200)
    handler.send_header("Content-Type", "application/json")
    if session_id:
        handler.send_header("Mcp-Session-Id", session_id)
    handler.end_headers()
    handler.wfile.write(raw)


def send_stub_tool_call(
    handler: http.server.BaseHTTPRequestHandler, mode: str, body: dict[str, Any]
) -> None:
    """Write an SSE text/event-stream response for a stub tools/call request."""
    name = (body.get("params") or {}).get("name")
    is_err = mode == "tool_error" and name == "snatcharr_list_instances"
    res = {"content": [{"type": "text", "text": "stub"}], "isError": is_err}
    line = json.dumps({"jsonrpc": "2.0", "id": body.get("id"), "result": res})
    sse = f"event: message\ndata: {line}\n\n".encode("utf-8")
    handler.send_response(200)
    handler.send_header("Content-Type", "text/event-stream")
    handler.end_headers()
    handler.wfile.write(sse)


def handle_stub_mcp(
    handler: http.server.BaseHTTPRequestHandler, mode: str, body: dict[str, Any]
) -> None:
    """Dispatch one MCP JSON-RPC request in the local self-test stub server."""
    method = body.get("method")
    req_id = body.get("id")
    if method == "initialize":
        params = body.get("params") or {}
        if (
            params.get("protocolVersion") != MCP_PROTOCOL_VERSION
            or (params.get("clientInfo") or {}).get("name") != MCP_CLIENT_NAME
        ):
            handler.send_error(400)
            return
        resp = {"jsonrpc": "2.0", "id": req_id, "result": {"protocolVersion": MCP_PROTOCOL_VERSION}}
        send_stub_json(handler, resp, session_id=STUB_SESSION_ID)
        return
    if handler.headers.get("Mcp-Session-Id") != STUB_SESSION_ID:
        handler.send_error(400, "missing Mcp-Session-Id")
        return
    if method == "notifications/initialized":
        handler.send_response(202)
        handler.end_headers()
        return
    if method == "tools/list":
        cursor = (body.get("params") or {}).get("cursor")
        result = stub_tools_for_mode(mode, cursor)
        send_stub_json(handler, {"jsonrpc": "2.0", "id": req_id, "result": result})
        return
    if method == "tools/call":
        send_stub_tool_call(handler, mode, body)
        return
    handler.send_error(404)


def make_stub_handler(mode: str) -> type[http.server.BaseHTTPRequestHandler]:
    """Create a stub BaseHTTPRequestHandler class configured for mode."""

    class StubHandler(http.server.BaseHTTPRequestHandler):
        def log_message(self, fmt: str, *args: object) -> None:
            return

        def do_GET(self) -> None:
            if self.headers.get("Authorization") != f"Bearer {STUB_TOKEN}":
                self.send_error(401)
                return
            if self.path != "/api/v1/system/status":
                self.send_error(404)
                return
            served = STUB_WRONG_COMMIT if mode == "mismatch" else STUB_SERVED_SHORT
            raw = json.dumps({"commit": served}).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(raw)

        def do_POST(self) -> None:
            if self.headers.get("Authorization") != f"Bearer {STUB_TOKEN}":
                self.send_error(401)
                return
            if self.path != "/api/v1/mcp":
                self.send_error(404)
                return
            length = int(self.headers.get("Content-Length", "0"))
            body = json.loads(self.rfile.read(length).decode("utf-8"))
            handle_stub_mcp(self, mode, body)

    return StubHandler


SELF_TEST_CASES = (
    (
        "pass with read-only tools",
        "pass",
        "cluster",
        0,
        "MCP dogfood on cluster: 2 read-only calls passed",
    ),
    (
        "served commit mismatch",
        "mismatch",
        "cluster",
        3,
        "MCP dogfood on cluster: commit mismatch",
    ),
    (
        "a tool returning isError",
        "tool_error",
        "cluster",
        3,
        "MCP dogfood on cluster: 2 read-only calls, 1 failed",
    ),
    (
        "zero callable read-only tools",
        "zero_readonly",
        "cluster",
        3,
        "MCP dogfood on cluster: 0 read-only calls, 0 failed",
    ),
    (
        "pass names another instance",
        "pass",
        "dev-compose",
        0,
        "MCP dogfood on dev-compose: 2 read-only calls passed",
    ),
    (
        "failure names another instance",
        "mismatch",
        "dev-compose",
        3,
        "MCP dogfood on dev-compose: commit mismatch",
    ),
)
REFUSED_LABEL_CASES = (
    ("missing --instance refused", []),
    ("malformed --instance refused", ["--instance", "Bad Label"]),
)


def run_stub_case(
    label: str, mode: str, instance: str, want_exit: int, want_desc: str
) -> None:
    """Start a local HTTP stub and assert the exit code and recorded description."""
    server = http.server.HTTPServer(("127.0.0.1", 0), make_stub_handler(mode))
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    output = io.StringIO()
    try:
        port = server.server_address[1]
        with contextlib.redirect_stdout(output):
            got_exit = run_dogfood(
                "lusoris/SnatchArr",
                STUB_COMMIT,
                instance,
                f"http://127.0.0.1:{port}",
                STUB_TOKEN,
                post=False,
            )
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
    if got_exit != want_exit:
        raise SelfTestError(f"{label}: expected exit {want_exit}, got {got_exit}")
    if f"description={want_desc}" not in output.getvalue():
        raise SelfTestError(f"{label}: no status description {want_desc!r}")
    if f"instance={instance} " not in output.getvalue():
        raise SelfTestError(f"{label}: output does not name instance {instance!r}")
    print(f"ok   {label}: exit {got_exit}")


def run_refused_label(label: str, instance_args: list[str]) -> None:
    """Assert that the command line refuses a missing or malformed instance label."""
    arguments = ["--repository", "lusoris/SnatchArr", "--commit", STUB_COMMIT]
    with contextlib.redirect_stderr(io.StringIO()) as errors:
        got_exit = main(arguments + instance_args)
    if got_exit != 1 or "invalid instance label" not in errors.getvalue():
        raise SelfTestError(f"{label}: expected refusal, got exit {got_exit}")
    print(f"ok   {label}: exit {got_exit}")


def self_test() -> None:
    """Run planted positive and negative cases against a local HTTP stub."""
    for label, mode, instance, want_exit, want_desc in SELF_TEST_CASES:
        run_stub_case(label, mode, instance, want_exit, want_desc)
    for label, instance_args in REFUSED_LABEL_CASES:
        run_refused_label(label, instance_args)
    total = len(SELF_TEST_CASES) + len(REFUSED_LABEL_CASES)
    print(f"dogfood pass self-test: {total} cases behaved as required")


def main(arguments: list[str] | None = None) -> int:
    """CLI entrypoint for recording an MCP dogfood pass."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", default="")
    parser.add_argument("--commit", default="")
    parser.add_argument(
        "--instance",
        default="",
        help="deployment instance label (e.g. cluster, dev-compose)",
    )
    parser.add_argument("--post", action="store_true")
    parser.add_argument("--self-test", action="store_true")
    options = parser.parse_args(arguments)
    try:
        if options.self_test:
            self_test()
            return 0
        return run_dogfood(
            options.repository,
            options.commit,
            options.instance,
            os.environ.get("SNATCHARR_URL", ""),
            os.environ.get("SNATCHARR_API_KEY", ""),
            options.post,
        )
    except SelfTestError as error:
        print(f"dogfood pass self-test failed: {error}", file=sys.stderr)
        return 1
    except CheckError as error:
        print(f"dogfood pass error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
