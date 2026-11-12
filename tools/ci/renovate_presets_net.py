# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Bounded HTTPS reads and offline network self-tests for Renovate preset closure."""

from __future__ import annotations

import http.client
import io
import json
import re
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from collections import deque
from collections.abc import Callable
from typing import Protocol, Self

from renovate_managers import no_duplicate_keys
from renovate_regex import ConfigError

HTTP_TIMEOUT_SECONDS = 20

MAX_HTTP_BYTES = 1 << 20

READ_CHUNK_BYTES = 65536

HORISTES_REF = "github>plurano/horistes"

GITHUB_API_HOST = "api.github.com"

HORISTES_COMMITS_URL = "https://api.github.com/repos/plurano/horistes/commits/HEAD"

HORISTES_CONTENTS_URL = "https://api.github.com/repos/plurano/horistes/contents"

SHA_RE = re.compile(r"^[0-9a-f]{40}$")

OpenerFunc = Callable[[urllib.request.Request, float], bytes]


class Readable(Protocol):
    """Minimal byte-stream interface used by bounded response readers."""

    def read(self, size: int = -1) -> bytes:
        """Read up to SIZE bytes."""


class HttpsRedirectHandler(urllib.request.HTTPRedirectHandler):
    """Allow only HTTPS redirects and strip `Authorization` on any cross-host redirect."""

    def redirect_request(  # type: ignore[override]
        self,
        req: urllib.request.Request,
        fp: object,
        code: int,
        msg: str,
        headers: object,
        newurl: str,
    ) -> urllib.request.Request | None:
        """Validate redirect scheme and drop credentials when leaving `api.github.com`."""
        parsed = urllib.parse.urlsplit(newurl)
        if parsed.scheme != "https" or not parsed.hostname:
            raise urllib.error.URLError(f"refused non-https redirect to {parsed.scheme!r}")
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)  # type: ignore[arg-type]
        if redirected is not None and parsed.hostname.lower() != GITHUB_API_HOST:
            redirected.headers.pop("Authorization", None)
            redirected.unredirected_hdrs.pop("Authorization", None)
        return redirected


def load_json_object(raw: bytes, label: str) -> dict[str, object]:
    """Decode UTF-8 JSON bytes RAW with duplicate-key refusal and require a top-level object."""
    try:
        text = raw.decode("utf-8")
        parsed = json.loads(text, object_pairs_hook=no_duplicate_keys)
    except (UnicodeDecodeError, ValueError) as exc:
        raise ConfigError(f"{label}: invalid JSON: {exc}") from exc
    if not isinstance(parsed, dict):
        raise ConfigError(f"{label}: top-level JSON value must be an object")
    return parsed


def read_with_deadline(
    resp: Readable,
    limit: int,
    timeout: float,
    clock: Callable[[], float] = time.monotonic,
) -> bytes:
    """Read at most LIMIT + 1 bytes from RESP within one total TIMEOUT."""
    deadline = clock() + timeout
    chunks: list[bytes] = []
    remaining = limit + 1
    max_reads = limit // READ_CHUNK_BYTES + 2
    for _ in range(max_reads):
        if clock() > deadline:
            raise TimeoutError(f"response read exceeded {timeout}s")
        chunk = resp.read(min(READ_CHUNK_BYTES, remaining))
        if clock() > deadline:
            raise TimeoutError(f"response read exceeded {timeout}s")
        if not chunk:
            return b"".join(chunks)
        saved = chunk[:remaining]
        chunks.append(saved)
        remaining -= len(saved)
        if remaining == 0:
            return b"".join(chunks)
    raise TimeoutError(f"response read exceeded {max_reads} reads")


def open_https_response(
    opener: urllib.request.OpenerDirector,
    req: urllib.request.Request,
    timeout: float,
    bodies: list[bytes],
    errors: list[Exception],
) -> None:
    """Open REQ and read its capped body, recording one result or the original exception."""
    try:
        with opener.open(req, timeout=timeout) as resp:  # nosec B310  # fixed https://api.github.com URL verified above; redirects restricted to https
            bodies.append(read_with_deadline(resp, MAX_HTTP_BYTES, timeout))
    except (OSError, http.client.HTTPException, ValueError) as exc:  # what urllib raises; default_https_opener re-raises it unchanged
        errors.append(exc)


def default_https_opener(
    req: urllib.request.Request,
    timeout: float,
    build_opener: Callable[..., urllib.request.OpenerDirector] = urllib.request.build_opener,
) -> bytes:
    """Execute HTTPS request REQ against `api.github.com` and read up to MAX_HTTP_BYTES + 1."""
    parsed = urllib.parse.urlsplit(req.full_url)
    if parsed.scheme != "https" or parsed.hostname != GITHUB_API_HOST:
        raise ConfigError(f"refused non-GitHub-API URL: {req.full_url!r}")
    opener = build_opener(HttpsRedirectHandler())
    bodies: list[bytes] = []
    errors: list[Exception] = []
    worker = threading.Thread(
        target=open_https_response,
        args=(opener, req, timeout, bodies, errors),
        daemon=True,
    )
    worker.start()
    worker.join(timeout=timeout)
    if worker.is_alive():
        raise TimeoutError(f"response read exceeded {timeout}s")
    if errors:
        raise errors[0]
    if not bodies:
        raise ConfigError("HTTPS response worker ended without a body or a urllib error; its traceback is on stderr")
    return bodies[0]


def github_api_get(
    url: str, accept: str, token: str | None, opener: OpenerFunc
) -> bytes:
    """GET one `https://api.github.com` URL with deadline, byte cap and scoped bearer header."""
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != "https" or parsed.hostname != GITHUB_API_HOST:
        raise ConfigError(f"refused URL outside https://{GITHUB_API_HOST}: {url!r}")
    headers = {
        "Accept": accept,
        "User-Agent": "snatcharr-renovate-presets",
    }
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, headers=headers, method="GET")
    data = opener(req, HTTP_TIMEOUT_SECONDS)
    if len(data) > MAX_HTTP_BYTES:
        raise ConfigError(f"{url}: response exceeds {MAX_HTTP_BYTES} bytes")
    return data


def format_net_exc(exc: BaseException) -> str:
    """Format a network/HTTP exception without exposing headers or request metadata."""
    return f"HTTP {exc.code}" if isinstance(exc, urllib.error.HTTPError) else str(exc)


def fetch_horistes_head(token: str | None, opener: OpenerFunc) -> str:
    """Resolve `plurano/horistes` default-branch HEAD commit SHA."""
    try:
        raw_sha = github_api_get(
            HORISTES_COMMITS_URL, "application/vnd.github.sha", token, opener
        )
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise ConfigError(f"horistes HEAD lookup failed: {format_net_exc(exc)}") from exc
    sha = raw_sha.decode("utf-8", errors="replace").strip()
    if SHA_RE.fullmatch(sha) is None:
        raise ConfigError(f"horistes HEAD returned non-SHA {sha!r}")
    return sha


def fetch_horistes(
    token: str | None = None, opener: OpenerFunc = default_https_opener
) -> tuple[str, dict[str, object]]:
    """Resolve `plurano/horistes` HEAD SHA, then read `default.json` (404 -> `renovate.json`)."""
    sha = fetch_horistes_head(token, opener)
    default_url = f"{HORISTES_CONTENTS_URL}/default.json?ref={sha}"
    fallback_url = f"{HORISTES_CONTENTS_URL}/renovate.json?ref={sha}"
    try:
        raw_body = github_api_get(
            default_url, "application/vnd.github.raw+json", token, opener
        )
    except urllib.error.HTTPError as exc:
        code = exc.code
        exc.close()
        if code != 404:
            raise ConfigError(f"horistes default.json failed: HTTP {code}") from exc
        try:
            raw_body = github_api_get(
                fallback_url, "application/vnd.github.raw+json", token, opener
            )
        except (urllib.error.URLError, TimeoutError, OSError) as exc2:
            raise ConfigError(
                f"horistes renovate.json fallback failed: {format_net_exc(exc2)}"
            ) from exc2
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise ConfigError(f"horistes default.json failed: {exc}") from exc

    return sha, load_json_object(raw_body, HORISTES_REF)


def make_http_error(url: str, code: int) -> urllib.error.HTTPError:
    """Construct an `HTTPError` with an empty closable body for `--self-test`."""
    return urllib.error.HTTPError(url, code, f"HTTP {code}", hdrs=None, fp=io.BytesIO(b""))  # type: ignore[arg-type]


def self_test_default_json(sha: str) -> None:
    """Verify `default.json` HTTP 200 path and bearer header attachment."""
    calls: list[tuple[str, str | None]] = []

    def ok_opener(req: urllib.request.Request, _timeout: int) -> bytes:
        """Serve `HEAD` and `default.json` while recording `(url, Authorization)`."""
        calls.append((req.full_url, req.get_header("Authorization")))
        if req.full_url == HORISTES_COMMITS_URL:
            return f"{sha}\n".encode("ascii")
        if req.full_url == f"{HORISTES_CONTENTS_URL}/default.json?ref={sha}":
            return b'{"extends": ["config:recommended"]}'
        raise AssertionError(f"unexpected URL {req.full_url}")

    got_sha, got_body = fetch_horistes(
        token="tok-test", opener=ok_opener
    )  # nosec B106  # fixed self-test value for a fake opener, never sent
    if got_sha != sha or got_body != {"extends": ["config:recommended"]}:
        raise AssertionError("default.json body mismatch")
    if len(calls) != 2 or any(auth != "Bearer tok-test" for _, auth in calls):
        raise AssertionError("Authorization header mismatch on default.json path")


def self_test_renovate_json_fallback(sha: str) -> None:
    """Verify `default.json` HTTP 404 -> `renovate.json` fallback with anonymous headers."""
    calls: list[tuple[str, str | None]] = []

    def fallback_opener(req: urllib.request.Request, _timeout: int) -> bytes:
        """Return 404 on `default.json` and 200 on `renovate.json`."""
        calls.append((req.full_url, req.get_header("Authorization")))
        if req.full_url == HORISTES_COMMITS_URL:
            return sha.encode("ascii")
        if req.full_url == f"{HORISTES_CONTENTS_URL}/default.json?ref={sha}":
            raise make_http_error(req.full_url, 404)
        if req.full_url == f"{HORISTES_CONTENTS_URL}/renovate.json?ref={sha}":
            return b'{"description": "fallback"}'
        raise AssertionError(f"unexpected URL {req.full_url}")

    got_sha, got_body = fetch_horistes(token=None, opener=fallback_opener)
    if got_sha != sha or got_body != {"description": "fallback"}:
        raise AssertionError("renovate.json fallback body mismatch")
    if len(calls) != 3 or any(auth is not None for _, auth in calls):
        raise AssertionError("anonymous fallback call mismatch")


def self_test_horistes_errors(sha: str) -> None:
    """Verify non-404 HTTP errors, oversized responses, bad SHAs and redirect credential stripping."""
    def http_500_opener(req: urllib.request.Request, _timeout: int) -> bytes:
        """Return SHA on HEAD and HTTP 500 on `default.json`."""
        if req.full_url == HORISTES_COMMITS_URL:
            return sha.encode("ascii")
        raise make_http_error(req.full_url, 500)

    def oversized_opener(req: urllib.request.Request, _timeout: int) -> bytes:
        """Return SHA on HEAD and MAX_HTTP_BYTES + 1 bytes on `default.json`."""
        if req.full_url == HORISTES_COMMITS_URL:
            return sha.encode("ascii")
        return b" " * (MAX_HTTP_BYTES + 1)

    def bad_sha_opener(_req: urllib.request.Request, _timeout: int) -> bytes:
        """Return a non-hex ref instead of a 40-char SHA."""
        return b"not-a-40-hex-sha"

    for bad_opener in (http_500_opener, oversized_opener, bad_sha_opener):
        try:
            fetch_horistes(token=None, opener=bad_opener)
        except ConfigError:
            pass
        else:
            raise AssertionError(f"{bad_opener.__name__} did not raise ConfigError")


def self_test_redirects() -> None:
    """Verify `HttpsRedirectHandler` strips cross-host auth and refuses plain HTTP redirects."""
    handler = HttpsRedirectHandler()
    orig = urllib.request.Request(
        HORISTES_COMMITS_URL, headers={"Authorization": "Bearer secret"}
    )
    redir = handler.redirect_request(
        orig, None, 302, "Found", {}, "https://objects.githubusercontent.com/x"
    )
    if redir is None or "Authorization" in redir.headers:
        raise AssertionError("cross-host redirect retained Authorization header")
    try:
        handler.redirect_request(orig, None, 302, "Found", {}, "http://api.github.com/x")
    except urllib.error.URLError:
        return
    raise AssertionError("http redirect was not refused")


def self_test_read_with_deadline() -> None:
    """Verify response read success, total-deadline refusal and LIMIT + 1 boundary."""
    class FakeResponse:
        """Return caller-provided chunks while respecting the requested read size."""

        def __init__(self, chunks: list[bytes]) -> None:
            """Queue the chunks the fake body returns in order."""
            self.chunks = deque(chunks)

        def read(self, size: int = -1) -> bytes:
            """Return the next chunk, split at `size` like a real response."""
            chunk = self.chunks.popleft() if self.chunks else b""
            if size >= 0 and len(chunk) > size:
                self.chunks.appendleft(chunk[size:])
                return chunk[:size]
            return chunk

    if read_with_deadline(FakeResponse([b"abc"]), 8, 1, clock=lambda: 0.0) != b"abc":
        raise AssertionError("under-deadline response mismatch")
    times = iter((0.0, 0.0, 0.0, 2.0))
    try:
        read_with_deadline(
            FakeResponse([b"a", b"b"]), 8, 1, clock=lambda: next(times)
        )
    except TimeoutError as exc:
        if str(exc) != "response read exceeded 1s":
            raise
    else:
        raise AssertionError("mid-body deadline was not refused")
    boundary = read_with_deadline(
        FakeResponse([b"abcdef"]), 4, 1, clock=lambda: 0.0
    )
    if boundary != b"abcde":
        raise AssertionError("LIMIT + 1 boundary mismatch")
    try:
        read_with_deadline(FakeResponse([b"a"] * 4), 8, 1, clock=lambda: 0.0)
    except TimeoutError as exc:
        if str(exc) != "response read exceeded 2 reads":
            raise
    else:
        raise AssertionError("read count bound was not refused")


def self_test_https_opener() -> None:
    """Verify the live opener reads the body through `read_with_deadline`, not one short read."""
    class TrickleResponse:
        """Return one byte per read, like a body arriving in small pieces."""

        def __init__(self) -> None:
            """Queue a three-byte body delivered one byte per read."""
            self.left = deque([b"a", b"b", b"c"])

        def __enter__(self) -> Self:
            """Act as its own context manager, like `urlopen` responses."""
            return self

        def __exit__(self, *_exc: object) -> None:
            """Hold no resource, so leaving the context does nothing."""

        def read(self, _size: int = -1) -> bytes:
            """Return one byte, then an empty read at end of body."""
            return self.left.popleft() if self.left else b""

    class FakeOpener:
        """Stand in for `urllib.request.OpenerDirector` without network access."""

        def open(self, _req: object, timeout: float) -> TrickleResponse:
            """Refuse a call without a deadline, else return the trickling body."""
            if timeout <= 0:
                raise AssertionError("opener called without a deadline")
            return TrickleResponse()

    req = urllib.request.Request(f"https://{GITHUB_API_HOST}/repos/x/y")
    body = default_https_opener(req, 5, build_opener=lambda *_h: FakeOpener())
    if body != b"abc":
        raise AssertionError(f"live opener read {body!r}, want b'abc'")


def self_test_https_opener_deadline() -> None:
    """Verify one deadline bounds blocked opener and response reads."""
    class BlockingResponse:
        """Block its only body read until the daemon worker is abandoned."""

        def __enter__(self) -> Self:
            """Act as its own context manager."""
            return self

        def __exit__(self, *_exc: object) -> None:
            """Hold no resource after the timed-out daemon worker exits."""

        def read(self, _size: int = -1) -> bytes:
            """Block beyond the outer response deadline."""
            threading.Event().wait()
            return b""

    class BlockingOpen:
        """Block opening the response beyond the outer response deadline."""

        def open(self, _req: object, timeout: float) -> BlockingResponse:
            """Block before a response can be returned."""
            if timeout <= 0:
                raise AssertionError("opener called without a deadline")
            threading.Event().wait()
            return BlockingResponse()

    class BlockingRead:
        """Return a response whose body read blocks beyond the deadline."""

        def open(self, _req: object, timeout: float) -> BlockingResponse:
            """Return a blocking response."""
            if timeout <= 0:
                raise AssertionError("opener called without a deadline")
            return BlockingResponse()

    req = urllib.request.Request(f"https://{GITHUB_API_HOST}/repos/x/y")
    for opener in (BlockingOpen(), BlockingRead()):
        started = time.monotonic()
        try:
            default_https_opener(req, 0.2, build_opener=lambda *_h, o=opener: o)
        except TimeoutError as exc:
            if str(exc) != "response read exceeded 0.2s":
                raise
            if time.monotonic() - started > 1.2:
                raise AssertionError("blocked response outlived deadline tolerance")
        else:
            raise AssertionError("blocked response did not time out")


def self_test_https_opener_exception() -> None:
    """Verify worker HTTP errors retain their identity and message."""
    req = urllib.request.Request(f"https://{GITHUB_API_HOST}/repos/x/y")
    expected = make_http_error(req.full_url, 503)

    class FailingOpener:
        """Raise the prebuilt HTTP error from the worker thread."""

        def open(self, _req: object, timeout: float) -> Readable:
            """Propagate the exact HTTP error object."""
            if timeout <= 0:
                raise AssertionError("opener called without a deadline")
            raise expected

    try:
        default_https_opener(req, 1, build_opener=lambda *_h: FailingOpener())
    except urllib.error.HTTPError as exc:
        if exc is not expected:
            raise AssertionError("worker HTTP error identity changed") from exc
    else:
        raise AssertionError("worker HTTP error was not propagated")


def run_self_test() -> None:
    """Execute offline unit tests for `horistes` HTTP fetch and redirect handling."""
    sha = "1075f6323bffb41e599fae6d7113a19029b33f2d"
    self_test_default_json(sha)
    self_test_renovate_json_fallback(sha)
    self_test_horistes_errors(sha)
    self_test_redirects()
    self_test_read_with_deadline()
    self_test_https_opener()
    self_test_https_opener_deadline()
    self_test_https_opener_exception()
