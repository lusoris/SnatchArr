<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: EUPL-1.2
-->

# SnatchArr Metadata Proxy (`metadata-proxy`)

`snatcharr-metadata-proxy` is the standalone outbound metadata proxy for SnatchArr ("snatch" means acquire a missing or cutoff-unmet media item, and "Arr" refers to the \*arr family of media applications; pronounce it "snatch-arr"), specified in [ADR-0049](../docs/adr/0049-metadata-proxy.md). Slice 1 implements the `CONNECT` forward-proxy binary in `log` and `enforce` modes, exposing one dedicated listener port per participating application (so the bounded `app` metric label requires no proxy credentials), refusing non-`CONNECT` plain-HTTP requests, seeded indexer/download-client hosts, and private/loopback/link-local/cluster-service addresses at dial time, and serving Kubernetes health probes (`/livez`, `/readyz`, `/startupz`) and Prometheus metrics (`/metrics`) on a separate admin HTTP listener.

## Configuration

Settings load through `golusoris/core/config` under the `metadata_proxy` key tree from `/etc/snatcharr/metadata-proxy.yaml` (if present), an optional file named by `APP_METADATA_PROXY_CONFIG_FILE`, and `APP_*` environment variables (highest precedence).

| YAML key | Environment variable | Default | Description |
| :--- | :--- | :--- | :--- |
| `http.addr` | `APP_HTTP_ADDR` | `":8080"` | Admin HTTP listener address serving `/livez`, `/readyz`, `/startupz`, and `/metrics`. |
| `metadata_proxy.mode` | `APP_METADATA_PROXY_MODE` | `"log"` | `log` tunnels hosts outside `allow_hosts` (after hard refusals) and logs a warning; `enforce` refuses them with `403 Forbidden`. |
| `metadata_proxy.listeners` | `APP_METADATA_PROXY_LISTENERS` | *(required)* | List of `{app, port}` entries in YAML (or comma-separated `app:port` pairs in env). Ports must be unique and within `1..65535`. |
| `metadata_proxy.allow_hosts` | `APP_METADATA_PROXY_ALLOW_HOSTS` | `[]` | Metadata-provider host allow-list. Supports case-insensitive exact hosts (`skyhook.sonarr.tv`) and leading-dot subdomain suffixes (`.sonarr.tv`); a single trailing dot is ignored. Required when `mode` is `enforce`. |
| `metadata_proxy.refuse_hosts` | `APP_METADATA_PROXY_REFUSE_HOSTS` | `[]` | Seeded indexer and download-client hosts refused with `403` in both modes before any dial; same pattern syntax as `allow_hosts`. |
| `metadata_proxy.ports` | `APP_METADATA_PROXY_PORTS` | `[443]` | Allowed `CONNECT` destination ports (`1..65535`). |
| `metadata_proxy.denied_prefixes` | `APP_METADATA_PROXY_DENIED_PREFIXES` | `[]` | Extra IPv4/IPv6 CIDRs refused at dial time in addition to loopback, RFC 1918 private, link-local, CGNAT, and non-global-unicast IPv6 ranges. |
| `metadata_proxy.dial_timeout` | `APP_METADATA_PROXY_DIAL_TIMEOUT` | `10s` | Positive deadline for outbound TCP resolution and connection establishment. |
| `metadata_proxy.idle_timeout` | `APP_METADATA_PROXY_IDLE_TIMEOUT` | `2m` | Closes a tunnel after this positive duration with no bytes transferred in either direction. |
| `metadata_proxy.max_tunnel` | `APP_METADATA_PROXY_MAX_TUNNEL` | `30m` | Positive maximum lifetime of any single established `CONNECT` tunnel. |
| `metadata_proxy.max_tunnels` | `APP_METADATA_PROXY_MAX_TUNNELS` | `256` | Positive upper bound on concurrent open tunnels per listener (`503` when full). |

Startup fails immediately with a wrapped error when no listener is configured, an app name or port is duplicated, any port is outside `1..65535`, `mode` is not `log` or `enforce`, `mode` is `enforce` with an empty `allow_hosts`, a host pattern or CIDR is malformed, or any timeout or concurrency bound is non-positive.

## Running locally

Run from the repository root with the example configuration file:

```bash
APP_METADATA_PROXY_CONFIG_FILE=metadata-proxy/config.example.yaml \
  go run ./metadata-proxy/cmd/snatcharr-metadata-proxy
```

Or run purely from environment variables:

```bash
APP_HTTP_ADDR=127.0.0.1:18080 \
APP_METADATA_PROXY_MODE=log \
APP_METADATA_PROXY_LISTENERS=sonarr:18081,radarr:18082 \
APP_METADATA_PROXY_ALLOW_HOSTS=skyhook.sonarr.tv,api.radarr.video \
  go run ./metadata-proxy/cmd/snatcharr-metadata-proxy
```

Verify probes and metrics on the admin listener:

```bash
curl -fsS http://127.0.0.1:18080/readyz
curl -fsS http://127.0.0.1:18080/metrics | grep '^metadata_proxy_'
```

Readiness (`/readyz`) reflects only whether configuration is loaded and every configured `CONNECT` listener is bound; it never dials an upstream metadata provider.

## `result` metric label values

`metadata_proxy_tunnels_total` (`app`, `host`, `result`, `mode`) and `metadata_proxy_tunnel_duration_seconds` (`app`, `host`, `result`) bound the `result` label to the following fixed allow-list:

| `result` | HTTP status | Meaning |
| :--- | :--- | :--- |
| `ok` | `200` | `CONNECT` tunnel established and closed cleanly or after idle/lifetime expiry. |
| `refused_method` | `405` | Non-`CONNECT` HTTP method refused (`Allow: CONNECT` returned; target/query/headers never logged). |
| `refused_target` | `400` | Malformed `CONNECT` authority (`host:port` missing or invalid). |
| `refused_host` | `403` | Target host matched `refuse_hosts`, or fell outside `allow_hosts` in `enforce` mode. |
| `refused_port` | `403` | Target port is not in `metadata_proxy.ports`. |
| `refused_ip` | `403` | Resolved IP address was blocked at dial time by the SSRF guard or `denied_prefixes`. |
| `refused_limit` | `503` | Listener reached `metadata_proxy.max_tunnels` concurrent open tunnels. |
| `dial_timeout` | `504` | Outbound TCP dial exceeded `metadata_proxy.dial_timeout`. |
| `dial_error` | `502` | Outbound TCP dial failed for another network error (for example, connection refused or DNS lookup failure). |

All pre-tunnel refusals (`400`, `403`, `405`, `503`) also increment `metadata_proxy_refused_tunnels_total{app}`. Hosts matching `allow_hosts` record their normalized `allow_hosts` entry on `host`; unlisted hosts fold into `other`.

## Slice 2 roadmap

Slice 2 adds provider-aware reverse-proxy caching for metadata providers whose base URL an application lets the operator point at the proxy (Lidarr, Readarr and its forks, Whisparr metadata base, Jellyfin MusicBrainz, and allow-listed read-only StashBox GraphQL queries), backed by `golusoris/cache/twotier` (in-process L1 plus Dragonfly L2) and the reverse-proxy metric family (`metadata_proxy_requests_total`, `metadata_proxy_duplicate_requests_total`, and `metadata_proxy_request_duration_seconds`).
