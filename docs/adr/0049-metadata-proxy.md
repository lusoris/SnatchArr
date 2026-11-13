<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: CC-BY-SA-4.0
-->
<!-- markdownlint-disable MD013 -->

# ADR-0049: Metadata proxy

- **Status**: Accepted (option a, product name and automatic kill switch answered by the operator on 2026-11-13, ledger ids `snatcharr-metadata-proxy-tls-interception`, `snatcharr-metadata-proxy-product-name`, `snatcharr-metadata-proxy-auto-killswitch`; build gated on the remaining open decisions)
- **Date**: 2026-11-13
- **Deciders**: lusoris

## Context

SnatchArr is the project name for the media-stack coordinator: “snatch” means acquire a missing or cutoff-unmet item, and “Arr” refers to the family of \*arr applications; pronounce it “snatch-arr”. The media stack makes overlapping metadata calls from Sonarr, Radarr, Lidarr, Readarr and its forks, Whisparr, Seerr, Jellyfin, Emby, Stash and Bazarr. Prowlarr was examined too and makes no metadata-provider calls (see the matrix). The component keeps the plain name `metadata-proxy`, with no separate product name (see the decision record).

Seerr evidence is pinned to `seerr-team/seerr` tag `v3.5.2` (commit `18a90b4e`, released 2026-11-12, read from the GitHub releases API on 2026-11-13).

The proxy is its own binary, image, Deployment, Service and NetworkPolicy, with a separate compose service. It routes eligible metadata calls and never routes indexers, download clients, notifications, \*arr-to-\*arr calls or media-server traffic. This ADR is planning only: it adds no Go, Helm or compose code.

Every provider call found in the pinned sources is HTTPS. RFC 9110 section 9.3.6 defines CONNECT as a tunnel that then blindly forwards bytes. In CONNECT mode the proxy sees the destination host, byte counts and timing, never the request path or body, so it cannot cache or deduplicate. Caching needs either a provider base URL the app lets an operator point at the proxy (reverse-proxy mode) or TLS termination with a CA every participating app trusts.

## Decision

### Deployable shape

Add a top-level `metadata-proxy/` component so its lifecycle, image and release gate stay independent of the Go control plane and the Rust worker. Follow the existing binary and image patterns in `api/cmd/snatcharr`, `worker/Dockerfile`, `deploy/helm/snatcharr/Chart.yaml` and `deploy/homelab/compose.yaml`; the implementation adds a chart Deployment, Service, NetworkPolicy, PodDisruptionBudget and compose service. The binary owns routing, provider-aware caching in reverse-proxy mode, health endpoints and metrics. It holds no SnatchArr durable state.

Caching uses the shared `golusoris/cache/twotier` package: an in-process L1 plus a rueidis-backed, Redis-compatible L2 (Dragonfly) with per-prefix TTLs. The golusoris outbound caching transport and the Dragonfly deployment are parallel work and dependencies of the implementation, not designs made here.

### Routing method

Two routes put an app's metadata traffic on the proxy. Both keep everything else direct.

1. **App proxy setting.** The \*arr family (Settings > General > Proxy, host config API), Seerr (Settings > Network, `/api/v1/settings/network`), Stash (`proxy`, `no_proxy`) and Bazarr (proxy settings) have a built-in HTTP/SOCKS proxy with a bypass list. SnatchArr sets it declaratively through the app API where the matrix shows a writable resource. Kill switch: write `proxyEnabled=false` (or the app's equivalent) through the same API; no restart.
2. **Container environment proxy.** .NET uses `HttpClient.DefaultProxy` when a handler sets no proxy of its own; on Linux that reads `http_proxy`/`HTTP_PROXY`, `https_proxy`/`HTTPS_PROXY`, `all_proxy`/`ALL_PROXY` and `no_proxy`/`NO_PROXY` (lowercase first). `NO_PROXY` has no `*` wildcard, and a leading dot matches subdomains ([Microsoft docs](https://learn.microsoft.com/en-us/dotnet/api/system.net.http.httpclient.defaultproxy), retrieved 2026-11-13). The shared \*arr dispatcher assigns `handler.Proxy` only when the app proxy is set and never sets `UseProxy = false` (`Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:159,174-176`; each \*arr row in Appendix A cites its own lines), so with the app proxy off the \*arr apps honour these variables. Jellyfin builds its `SocketsHttpHandler`s without a proxy (`jellyfin/jellyfin@83d0d0e7f76e23e2e085c8ff67d28b772d35967d:Jellyfin.Server/Startup.cs:88-99`) and so does its TVDB plugin (`jellyfin/jellyfin-plugin-tvdb@f1bcc9edf37d5ee65cf3683e89b76226c09a22eb:Jellyfin.Plugin.Tvdb/TvdbClientManager.cs:759-769`). The environment applies to the whole process, so `NO_PROXY` must name every internal host the app calls. Kill switch: remove the variables and restart the container.

The app proxy setting is the default for every app that has one: it carries per-app bypass syntax (wildcards, IP ranges, local-address bypass) and flips without a restart. The environment route is for apps without an app setting (Jellyfin, and Emby as a log-mode trial).

App bypass lists and `NO_PROXY` are deny-lists: they say what stays direct. The proxy keeps the matching allow-list of metadata provider hosts, so a missing bypass entry shows up instead of silently sending an indexer through the proxy:

- **Log mode** tunnels every CONNECT, counts it, and logs each host outside the allow-list with the app it came from. A host that appears there is a missing bypass entry to fix in that app.
- **Enforce mode** refuses CONNECT to hosts outside the allow-list with `403 Forbidden` and counts the refusal. An app enters enforce mode only after its log-mode window shows no host outside the allow-list.

Reverse-proxy caching is allowed only for a provider whose base URL the app lets an operator change (Lidarr, Readarr and its forks, Whisparr, Jellyfin MusicBrainz, Stash StashBox). The proxy never rewrites provider URLs, and CONNECT mode never claims path-level cache hits.

Each app connects to its own proxy listener port, which sets the bounded `app` label without proxy credentials.

### Options and recommendation

| Option | Scope | Risk |
| :--- | :--- | :--- |
| a | TLS-terminating forward proxy plus a fleet CA mounted into each app trust store | CA key custody, per-container trust configuration, pinned clients, provider terms. Not built: the operator declined it for now (see the decision record). |
| b | CONNECT pass-through everywhere; cache only in reverse-proxy mode where a base URL is configurable | No caching for apps without a configurable base URL; host-level measurement only for them. |
| c | Per app: CONNECT where nothing better exists, reverse-proxy cache where a base URL is configurable, direct where no route is proven | Less uniform; each app has its own route and kill switch. |

Option a terminates TLS at the proxy and re-signs each provider certificate with a fleet CA, so every participating app must trust that CA. This ADR names the methods and does not choose one; they stay on record for the revisit trigger in the decision record. Trust injection per runtime, from pinned source:

| Runtime (apps) | Trust method | Source |
| :--- | :--- | :--- |
| .NET on Linux (Sonarr, Radarr, Lidarr, Readarr, Whisparr, Jellyfin; Emby unverified) | CA in the OpenSSL machine root store, or `SSL_CERT_FILE` / `SSL_CERT_DIR` | `dotnet/runtime@a6f96e58c1163961f04d4218c824160ade1cf3f2:src/libraries/System.Security.Cryptography/tests/X509Certificates/X509StoreTests.Unix.cs:38-39,53` |
| Node.js (Seerr) | `NODE_EXTRA_CA_CERTS` extends the bundled roots | `nodejs/node@40b49b6c6b2a97e9384da15e81ad3209e736f77f:doc/api/cli.md:4218-4224` |
| Go (Stash) | `SSL_CERT_FILE` / `SSL_CERT_DIR` | `golang/go@f2858e6cd2ce4c43a1f18bc081ba620b09dcefa5:src/crypto/x509/root.go:127,132` |
| Python requests (Bazarr) | `REQUESTS_CA_BUNDLE` or `CURL_CA_BUNDLE` | `psf/requests@3c7baaa05ab4aa49cc819ce3a819d260de19c157:src/requests/sessions.py:857-858` |

CA custody, each with its risk:

- **Cluster-held CA key** in a Kubernetes Secret read by the proxy. Simplest to run; anyone who can read that Secret can impersonate every metadata provider to every participating app.
- **Offline root with an in-cluster intermediate** limited to the provider allow-list and rotated on a schedule. Smaller blast radius if the cluster is compromised; needs a root custody process and rotation, and whether every client enforces name constraints is unverified.

Both share three risks: clients that pin provider certificates fail closed, the trust change is process-wide (every TLS call the app makes trusts the fleet CA), and provider terms on caching are unverified per provider.

Route per app (option c; option a is not built, see the decision record):

| App | Route | Cache | Before-rollout measurement |
| :--- | :--- | :--- | :--- |
| Sonarr | App proxy, CONNECT | None (base URL fixed in source) | Host counts, bytes, latency |
| Radarr | App proxy, CONNECT | None (base URL fixed in source) | Host counts, bytes, latency |
| Lidarr | App proxy, CONNECT, plus `MetadataSource` pointed at the proxy | Reverse-proxy cache for the metadata base | Path-level counts and duplicate share in reverse mode |
| Readarr and forks | App proxy, CONNECT, plus `MetadataSource` pointed at the proxy | Reverse-proxy cache in front of the configured metadata server | Path-level counts and duplicate share in reverse mode |
| Whisparr v2, v3 | App proxy, CONNECT, plus config-file `WhisparrMetadata` pointed at the proxy | Reverse-proxy cache for the Whisparr metadata base; TMDb and StashDB stay CONNECT (v3) | Path-level for the metadata base; host counts for the rest |
| Prowlarr | Excluded: indexer traffic only | None | None |
| Seerr | App proxy, CONNECT | None (TVDB base URL is a source constant; Seerr already caches TVDB answers itself) | Host counts, bytes, latency |
| Jellyfin | Environment proxy, CONNECT; MusicBrainz server pointed at the proxy | Reverse-proxy cache for MusicBrainz only | Host counts; path-level for MusicBrainz |
| Emby | Environment proxy trial, CONNECT | None | Host counts if the trial shows Emby honours the variables |
| Stash | App proxy, CONNECT; StashBox endpoint pointed at the proxy | Reverse-proxy candidate; StashBox is GraphQL over POST, so body-keyed cache rules wait for measurement | Path-level for StashBox; host counts for scrapers |
| Bazarr | App proxy, CONNECT | None (dynamic subtitle providers) | Host counts, bytes, latency |

### Measurement plan

Before rollout, every app runs in log mode for a fixed window. What that window can measure depends on the route:

- **CONNECT (all apps):** per app and provider host, tunnels opened, failures, bytes each way and tunnel duration. No path, so no duplicate share.
- **Reverse-proxy mode (Lidarr, Readarr, Whisparr metadata base, Jellyfin MusicBrainz, Stash StashBox):** per provider and endpoint family, requests, upstream status and latency, plus the duplicate share. A duplicate is the same provider, method, normalized request target and relevant query or body identity inside the normalization window.
- **Jellyfin and Emby (dedupe question):** CONNECT gives per-host call counts for TMDb, OMDb, TVDB, Fanart and AudioDB. Their duplicate share needs the Jellyfin/Emby server logs (or option a). MusicBrainz is the one Jellyfin provider with path-level numbers from reverse mode.

After rollout, the reverse-mode rows add cache hit ratio, upstream calls avoided and latency change; the CONNECT rows keep tunnel counts and timing.

Metrics are declared once in a `metricdef.Catalog` and registered through `golusoris/k8s/metrics/prom`. Names follow the `metricdef` rules at `golusoris/golusoris@914a3db52118676d1af386835ddf65bd8d861bfc:observability/metricdef/metricdef.go`: counters end in `_total` (:30, :152-161), a unit is a base unit carried in the name (`_seconds`, `_bytes`; :74-77, :104-108, :171-184), `_bucket`/`_count`/`_sum`/`_created` are reserved (:102), and every label has a `LabelLimit` with either an `Allow` list or `MaxDistinct` (:60-66, :222-228). The `metadata_proxy_` prefix follows the component name, which the operator kept (see the decision record).

| Name | Kind | Labels (limit) |
| :--- | :--- | :--- |
| `metadata_proxy_tunnels_total` | counter | `app` (Allow: configured apps), `host` (Allow: provider allow-list; others fold into `other`), `result` (Allow), `mode` (Allow: log, enforce) |
| `metadata_proxy_tunnel_bytes_total` | counter, unit bytes | `app`, `host`, `direction` (Allow: up, down) |
| `metadata_proxy_tunnel_duration_seconds` | histogram, unit seconds | `app`, `host`, `result` |
| `metadata_proxy_refused_tunnels_total` | counter | `app` (hosts outside the allow-list go to the log, not a label) |
| `metadata_proxy_requests_total` | counter | `provider` (Allow), `endpoint_family` (Allow per provider), `outcome` (Allow: hit, miss, stale, bypass) |
| `metadata_proxy_duplicate_requests_total` | counter | `provider`, `endpoint_family` |
| `metadata_proxy_request_duration_seconds` | histogram, unit seconds | `provider`, `outcome` |

### Failure modes and kill switch

Proxy outage must not stop the media stack. The bypass lists keep indexers, download clients, \*arr-to-\*arr calls, notifications and media serving direct, so an outage degrades metadata lookups only: adding new series, movies, artists or books, metadata refreshes and image fetches fail or retry. Searches, grabs, downloads, imports and playback continue.

While its proxy is set, an \*arr app has no direct fallback: proxy and timeout errors surface from the dispatcher (`Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:130-144`). To keep the degraded window short:

- Run at least two replicas behind a PodDisruptionBudget, with readiness tied to upstream reachability, so a single pod loss never empties the Service.
- Keep one kill switch per route:
  - App proxy route: `proxyEnabled=false` through `/api/v3/config/host` (Sonarr, Radarr, Whisparr) or `/api/v1/config/host` (Lidarr, Readarr), and the network settings write for Seerr.
  - Environment route (Jellyfin, Emby): drop the variables and restart.
  - Reverse-proxy routes: reset the base URL (`MetadataSource`, `WhisparrMetadata`, the Jellyfin MusicBrainz server, the Stash StashBox endpoint) to the provider default.
  - Stash and Bazarr: their app configuration write; the exact API resource is unverified.
- The runbook disables every app route before the proxy Deployment is removed.

The kill switch is manual for the first rollout: an alert on proxy health pages the operator, who follows the runbook. SnatchArr never writes \*arr, Seerr or Jellyfin proxy settings, so ADR-0039's ownership split stays unchanged (see the decision record).

### Rollout

Rollout is a cluster order, issued after this ADR lands and the image is published:

1. Deploy the proxy in log mode, with the proxy-health alert and the kill-switch runbook in place.
2. Configure one low-risk app (Bazarr or Seerr).
3. Read its log-mode window: no host outside the allow-list, no indexer or download-client host.
4. Expand app by app, then switch each app to enforce mode.
5. Enable reverse-proxy caching per proven base-URL route.
6. Compare before and after metrics.

This planning unit performs no rollout.

### Compact routing matrix

| App | Providers called | Base URL configurable | App proxy setting and API | Environment proxy | Mode |
| :--- | :--- | :--- | :--- | :--- | :--- |
| Sonarr [S1] | `skyhook.sonarr.tv`, `services.sonarr.tv` | No | Settings > General > Proxy; `/api/v3/config/host` | Yes, .NET default proxy when the app proxy is off | CONNECT |
| Radarr [S2] | TMDb, `api.radarr.video` | No | Settings > General > Proxy; `/api/v3/config/host` | Yes, same | CONNECT |
| Lidarr [S3] | `api.lidarr.audio` | Yes: `MetadataSource`, `/api/v1/config/metadataprovider` | Settings > General > Proxy; `/api/v1/config/host` | Yes, same | CONNECT + reverse cache |
| Readarr and forks [S4] | `api.bookinfo.club` (upstream, retired); fork or replacement servers | Yes: `MetadataSource`, `/api/v1/config/development` (hidden UI page `/settings/development`) | Settings > General > Proxy; `/api/v1/config/host` | Yes, same | CONNECT + reverse cache |
| Whisparr v2 [S5] | `api.whisparr.com/v3` | Yes: config-file key `WhisparrMetadata`; no API resource found | Settings > General > Proxy; `/api/v3/config/host` | Yes, same | CONNECT + reverse cache |
| Whisparr v3 [S6] | `api.whisparr.com/v4`, TMDb, StashDB | Yes for the Whisparr base: config-file key `WhisparrMetadata` | Settings > General > Proxy; `/api/v3/config/host` | Yes, same | CONNECT + reverse cache |
| Prowlarr [S7] | None (indexers, `prowlarr.servarr.com`, `releases.servarr.com`) | Not applicable | Settings > General > Proxy; `/api/v1/config/host` | Yes, same | Excluded |
| Seerr [S8] | TMDb, TVDB, image hosts | No: TVDB base is a source constant | Settings > Network; `/api/v1/settings/network` | Custom agents; standard variables not read | CONNECT |
| Jellyfin + plugins [S9] | TMDb, OMDb, MusicBrainz, AudioDB, TVDB, Fanart | MusicBrainz only | None | Yes, .NET default proxy (server and TVDB plugin handlers set none) | Environment CONNECT + MusicBrainz reverse cache |
| Emby [S10] | TVDB, TMDb and others (docs and forum) | Unverified (closed source) | None reported | Unverified; trial in log mode | Environment CONNECT trial |
| Stash [S11] | StashBox GraphQL, scraper and image URLs | Yes: StashBox and scraper URLs are configuration | `proxy`, `no_proxy`; API resource unverified | Yes, the app exports them | CONNECT + StashBox reverse candidate |
| Bazarr [S12] | Dynamic subtitle providers; \*arr APIs | No single base | Proxy settings; API resource unverified | Yes, the app exports them | CONNECT |

[S1]: https://github.com/Sonarr/Sonarr/blob/cab419ade8ac7fcab5bf80394ee492abd35d5f5a/src/Sonarr.Api.V3/Config/HostConfigResource.cs#L38-L132
[S2]: https://github.com/Radarr/Radarr/blob/a96bf7c3ecb23cb76993770727285dc17a613fbc/src/Radarr.Api.V3/Config/HostConfigResource.cs#L38-L93
[S3]: https://github.com/Lidarr/Lidarr/blob/da7b4dfb1a9e7e1d6625c2dbc3fff96971ab26bd/src/NzbDrone.Core/MetadataSource/IMetadataRequestBuilder.cs#L25-L29
[S4]: https://github.com/Readarr/Readarr/blob/0b79d3000d4e5f8425f499970b0190e2c421fceb/src/NzbDrone.Core/MetadataSource/MetadataRequestBuilder.cs#L25-L29
[S5]: https://github.com/Whisparr/Whisparr/blob/face3be8956e4c3798a9364a0f2ea95948e34c1c/src/NzbDrone.Core/Configuration/ConfigFileProvider.cs#L95-L107
[S6]: https://github.com/Whisparr/Whisparr/blob/94480761ec412cae40ba84e55c70b723851340cf/src/NzbDrone.Core/Configuration/ConfigFileProvider.cs#L178-L190
[S7]: https://github.com/Prowlarr/Prowlarr/blob/0dc20d8495bad9951c3fafe53f32b6e1c70fcf3b/src/NzbDrone.Common/Cloud/ProwlarrCloudRequestBuilder.cs#L15-L18
[S8]: https://github.com/seerr-team/seerr/blob/18a90b4ed962cc56abc9087e305da5c18a3c7275/server/api/tvdb/index.ts#L22-L33
[S9]: https://github.com/jellyfin/jellyfin/blob/83d0d0e7f76e23e2e085c8ff67d28b772d35967d/Jellyfin.Server/Startup.cs#L88-L124
[S10]: https://emby.media/community/topic/114442-how-set-http-proxy-in-config/
[S11]: https://github.com/stashapp/stash/blob/9e9ad972643320e737c711884f10e4285f0f3a88/internal/manager/config/config.go#L150-L1867
[S12]: https://github.com/morpheus65535/bazarr/blob/8c68f16a1e12f8e7985baeb73fb4199b5149108a/bazarr/app/config.py#L301-L1053

## Decision record

The operator answered three decisions on 2026-11-13 (paraphrased; ledger ids in the first column).

| Decision | Answer | Revisit trigger |
| :--- | :--- | :--- |
| TLS interception (option a) and CA custody, `snatcharr-metadata-proxy-tls-interception` | No TLS interception now. Build option c in log mode first and decide after its window, with measured host counts. | Log-mode host counts show a duplicate share for the CONNECT-only apps: a new decision with those numbers. |
| Product name, `snatcharr-metadata-proxy-product-name` | No separate product name. Image `snatcharr-metadata-proxy`, chart values key `metadataProxy`, metrics `metadata_proxy_*`. | None. |
| Automatic kill switch, `snatcharr-metadata-proxy-auto-killswitch` | Manual kill switch only for the first rollout: an alert plus the runbook. ADR-0039 stays unchanged. | The log-mode window shows the proxy outage rate: a new decision with those numbers. |

## Open decisions

- Jellyfin and Emby environment route: approval to set process-wide proxy variables on the media servers, and whether Emby stays direct if the trial shows it ignores them.
- Whisparr: the reverse-proxy route needs a `config.xml` edit and a restart, because no API resource writes `WhisparrMetadata`.
- Cache TTLs, provider allow-list, log retention and the duplicate-normalization window, once the golusoris caching transport is available.

## Consequences

The default path keeps provider TLS end to end and needs no new trust anchor. CONNECT mode alone gives host-level numbers only; caching and duplicate measurement come from the reverse-proxy routes (Lidarr, Readarr, Whisparr, Jellyfin MusicBrainz, Stash StashBox) or from app logs. Each app has its own route and kill switch, so failure handling is tested and documented per app; apps without a proven route stay direct and listed, never silently proxied.

The separate binary and deployment add an image, Service, readiness, PDB, metrics, NetworkPolicy and upgrade surface. Enforce mode turns a missing bypass entry into a visible refusal, which is why each app moves to enforce mode only after a clean log-mode window.

## Alternatives

- DNS override: no request-aware routing, no reliable bypass semantics and no safe HTTPS caching; it changes every caller of a provider hostname.
- Sidecar per app: more replicas and configuration, a cache per app, and still no HTTPS path visibility without per-app trust changes.
- No proxy and per-app caches: keeps app ownership but cannot deduplicate across apps or give one measurement surface.

## References

- [ADR-0019](0019-foreign-title-search.md), [ADR-0039](0039-metadata-single-source.md), and ADR-0048 (TVDB provider, in flight).
- `snatcharr-metadata-cache-proxy-20261113` (paraphrased, operator, 2026-11-13, shared metadata cache proxy).
- `snatcharr-metadata-proxy-separate-deployable-20261113` (paraphrased, operator, 2026-11-13, separate deployable shape).
- `snatcharr-metadata-proxy-forward-proxy-route-20261113` (paraphrased, operator, 2026-11-13, forward-proxy routing).
- `snatcharr-metadata-proxy-jellyfin-dedupe-measure-20261113` (paraphrased, operator, 2026-11-13, Jellyfin deduplication measurement).
- `snatcharr-metadata-cache-placement-correction-20261113` (paraphrased, operator, 2026-11-13, cache placement correction).
- [RFC 9110 §9.3.6](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.3.6) (retrieved 2026-11-13).
- [`HttpClient.DefaultProxy`](https://learn.microsoft.com/en-us/dotnet/api/system.net.http.httpclient.defaultproxy) (retrieved 2026-11-13).
- `golusoris/golusoris@914a3db52118676d1af386835ddf65bd8d861bfc:observability/metricdef/metricdef.go` (metric naming rules).
- [Emby network documentation](https://www.emby.media/support/articles/Hosting-Settings.html) and [Emby proxy forum answer](https://emby.media/community/topic/114442-how-set-http-proxy-in-config/) (retrieved 2026-11-13; docs and forum only).

## Appendix A: routing matrix

Each cell carries a citation in `repository@commit:path:line` form. “Unverified” means the pinned source or docs did not prove the behavior.

The environment-proxy rows for .NET apps rely on one platform rule: a handler that sets no proxy uses `HttpClient.DefaultProxy`, which on Linux reads `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY` and `NO_PROXY` ([Microsoft docs](https://learn.microsoft.com/en-us/dotnet/api/system.net.http.httpclient.defaultproxy), retrieved 2026-11-13).

### Sonarr

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `skyhook.sonarr.tv` TVDB routes and `services.sonarr.tv`; `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Common/Cloud/SonarrCloudRequestBuilder.cs:15-18` | No; hosts are constants in `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Common/Cloud/SonarrCloudRequestBuilder.cs:15-18` | Settings > General > Proxy: `proxyEnabled`, `proxyType`, `proxyHostname`, `proxyPort`, `proxyBypassFilter`, `proxyBypassLocalAddresses`; `/api/v3/config/host`; `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/Sonarr.Api.V3/Config/HostConfigResource.cs:38-45,85-93,122-132` | Yes when the app proxy is off: handler built at :159, `handler.Proxy` set only when proxy settings exist at :174-176, `UseProxy` never cleared; `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:159,174-176` | Custom validation callback; default roots and pinning unverified; `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Core/Security/X509CertificateValidationService.cs:23-74` | No direct fallback; proxy and timeout errors surface; `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:130-144` | Indexers, download clients, other \*arr, notifications, media server; bypass supports wildcards and IP ranges; `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Core/Http/HttpProxySettingsProvider.cs:52-65` |

### Radarr

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| TMDb and `api.radarr.video`; `Radarr/Radarr@a96bf7c3ecb23cb76993770727285dc17a613fbc:src/NzbDrone.Common/Cloud/RadarrCloudRequestBuilder.cs:16-23` | No; constants in the same file | Settings > General > Proxy, same fields; `/api/v3/config/host`; `Radarr/Radarr@a96bf7c3ecb23cb76993770727285dc17a613fbc:src/Radarr.Api.V3/Config/HostConfigResource.cs:38-45,85-93` | Yes when the app proxy is off; `Radarr/Radarr@a96bf7c3ecb23cb76993770727285dc17a613fbc:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:156,171-173` | Custom validation callback; roots and pinning unverified; `Radarr/Radarr@a96bf7c3ecb23cb76993770727285dc17a613fbc:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:167` | No direct fallback; `Radarr/Radarr@a96bf7c3ecb23cb76993770727285dc17a613fbc:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:128-141` | Indexers, download clients, other \*arr, notifications, media server; bypass semantics from the shared provider; `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Core/Http/HttpProxySettingsProvider.cs:52-65` |

### Lidarr

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `api.lidarr.audio/api/v0.4` by default; `Lidarr/Lidarr@da7b4dfb1a9e7e1d6625c2dbc3fff96971ab26bd:src/NzbDrone.Common/Cloud/LidarrCloudRequestBuilder.cs:16-19` | Yes: `MetadataSource` setting at `Lidarr/Lidarr@da7b4dfb1a9e7e1d6625c2dbc3fff96971ab26bd:src/NzbDrone.Core/Configuration/ConfigService.cs:287-291`, used by `MetadataRequestBuilder` when set at `Lidarr/Lidarr@da7b4dfb1a9e7e1d6625c2dbc3fff96971ab26bd:src/NzbDrone.Core/MetadataSource/IMetadataRequestBuilder.cs:25-29`; API `/api/v1/config/metadataprovider` at `Lidarr/Lidarr@da7b4dfb1a9e7e1d6625c2dbc3fff96971ab26bd:src/Lidarr.Api.V1/Config/MetadataProviderConfigController.cs:9` and `Lidarr/Lidarr@da7b4dfb1a9e7e1d6625c2dbc3fff96971ab26bd:src/Lidarr.Api.V1/Config/MetadataProviderConfigResource.cs:8` | Settings > General > Proxy; `/api/v1/config/host`; `Lidarr/Lidarr@da7b4dfb1a9e7e1d6625c2dbc3fff96971ab26bd:src/Lidarr.Api.V1/Config/HostConfigResource.cs:38-45,85-93` | Yes when the app proxy is off; `Lidarr/Lidarr@da7b4dfb1a9e7e1d6625c2dbc3fff96971ab26bd:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:159,174-176` | Custom validation callback; roots and pinning unverified; `Lidarr/Lidarr@da7b4dfb1a9e7e1d6625c2dbc3fff96971ab26bd:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:170` | No direct fallback; `Lidarr/Lidarr@da7b4dfb1a9e7e1d6625c2dbc3fff96971ab26bd:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:130-144`; reverse route: resetting `MetadataSource` to empty restores the default | Indexers, download clients, other \*arr, notifications, media server; `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Core/Http/HttpProxySettingsProvider.cs:52-65` |

### Readarr and forks

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| Upstream default `api.bookinfo.club/v1`; `Readarr/Readarr@0b79d3000d4e5f8425f499970b0190e2c421fceb:src/NzbDrone.Common/Cloud/ReadarrCloudRequestBuilder.cs:16-19`. Upstream is retired (archived; `https://api.github.com/repos/Readarr/Readarr`, retrieved 2026-11-13). Live installs point `MetadataSource` at a replacement server such as `blampe/rreading-glasses` (hosted `https://api.bookinfo.pro`; `blampe/rreading-glasses@a2939b625d91389d3f0a3e58cbc3bfa7ebb8390a:README.md:58`) | Yes: `MetadataSource` at `Readarr/Readarr@0b79d3000d4e5f8425f499970b0190e2c421fceb:src/NzbDrone.Core/Configuration/ConfigService.cs:265-269`, used at `Readarr/Readarr@0b79d3000d4e5f8425f499970b0190e2c421fceb:src/NzbDrone.Core/MetadataSource/MetadataRequestBuilder.cs:25-29`; API `/api/v1/config/development` at `Readarr/Readarr@0b79d3000d4e5f8425f499970b0190e2c421fceb:src/Readarr.Api.V1/Config/DevelopmentConfigController.cs:14` and `Readarr/Readarr@0b79d3000d4e5f8425f499970b0190e2c421fceb:src/Readarr.Api.V1/Config/DevelopmentConfigResource.cs:8`; hidden UI page `/settings/development` per `blampe/rreading-glasses@a2939b625d91389d3f0a3e58cbc3bfa7ebb8390a:README.md:65-72` | Settings > General > Proxy; `/api/v1/config/host`; `Readarr/Readarr@0b79d3000d4e5f8425f499970b0190e2c421fceb:src/Readarr.Api.V1/Config/HostConfigResource.cs:35-42,79-87` | Yes when the app proxy is off; `Readarr/Readarr@0b79d3000d4e5f8425f499970b0190e2c421fceb:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:160,175-177` | Custom validation callback; roots and pinning unverified; `Readarr/Readarr@0b79d3000d4e5f8425f499970b0190e2c421fceb:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:171` | No direct fallback; `Readarr/Readarr@0b79d3000d4e5f8425f499970b0190e2c421fceb:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:131-145` | Indexers, download clients, other \*arr, notifications, media server; `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Core/Http/HttpProxySettingsProvider.cs:52-65` |

Forks and replacements, each pinned on 2026-11-13:

- `pennydreadful/bookshelf` (GitHub fork of `Readarr/Readarr`, last push 2026-09-11): keeps `MetadataSource` and adds a `METADATA_URL` environment fallback (`pennydreadful/bookshelf@3ac41f7e31427684a37138e8e539b7e6c7b73869:src/NzbDrone.Core/Configuration/ConfigService.cs:266-273`), uses it at `pennydreadful/bookshelf@3ac41f7e31427684a37138e8e539b7e6c7b73869:src/NzbDrone.Core/MetadataSource/MetadataRequestBuilder.cs:23`, keeps `/api/v1/config/development` (`pennydreadful/bookshelf@3ac41f7e31427684a37138e8e539b7e6c7b73869:src/Readarr.Api.V1/Config/DevelopmentConfigController.cs:14`), and ships Goodreads (`softcover`) and Hardcover (`hardcover`) image tags (`pennydreadful/bookshelf@3ac41f7e31427684a37138e8e539b7e6c7b73869:README.md:17-27`). Same routes as upstream.
- `blampe/rreading-glasses` (replacement metadata server, last push 2026-11-12): sits behind `MetadataSource`; the proxy's reverse mode goes in front of it.
- `ricetim/readarr-rresurrected` (standalone repository, not a GitHub fork, last push 2026-10-31, `ricetim/readarr-rresurrected@0b4067eb568c8b1f9c79a3ae1ef96cbceb45aed4:README.md`): bundles a metadata service inside the container. Its outbound provider hosts are unverified; log mode measures them.

### Whisparr v2

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `api.whisparr.com/v3`; `Whisparr/Whisparr@face3be8956e4c3798a9364a0f2ea95948e34c1c:src/NzbDrone.Common/Cloud/WhisparrCloudRequestBuilder.cs:19` | Yes, through the config file: key `WhisparrMetadata`, default `https://api.whisparr.com/v3/{route}`, at `Whisparr/Whisparr@face3be8956e4c3798a9364a0f2ea95948e34c1c:src/NzbDrone.Core/Configuration/ConfigFileProvider.cs:95-107`, used at `Whisparr/Whisparr@face3be8956e4c3798a9364a0f2ea95948e34c1c:src/NzbDrone.Core/MetadataSource/SkyHook/SkyHookProxy.cs:38,54`. No API resource found: a code search of the default branch `v2-develop` (2026-11-13) finds the key only in `ConfigFileProvider.cs`, `WhisparrCloudRequestBuilder.cs`, `SkyHookProxy.cs` and `TPDbImportProxy.cs` | Settings > General > Proxy; `/api/v3/config/host`; `Whisparr/Whisparr@face3be8956e4c3798a9364a0f2ea95948e34c1c:src/Whisparr.Api.V3/Config/HostConfigResource.cs:36-43,80-88` | Yes when the app proxy is off; `Whisparr/Whisparr@face3be8956e4c3798a9364a0f2ea95948e34c1c:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:152,167-169` | Custom validation callback; roots and pinning unverified; `Whisparr/Whisparr@face3be8956e4c3798a9364a0f2ea95948e34c1c:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:163` | No direct fallback; `Whisparr/Whisparr@face3be8956e4c3798a9364a0f2ea95948e34c1c:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:128-130` | Indexers, download clients, other \*arr, notifications, media server; `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Core/Http/HttpProxySettingsProvider.cs:52-65` |

### Whisparr v3

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `api.whisparr.com/v4`, TMDb, `stashdb.org/graphql`; `Whisparr/Whisparr@94480761ec412cae40ba84e55c70b723851340cf:src/NzbDrone.Common/Cloud/WhisparrCloudRequestBuilder.cs:17-27` | Yes for the Whisparr base, through the config file: key `WhisparrMetadata` at `Whisparr/Whisparr@94480761ec412cae40ba84e55c70b723851340cf:src/NzbDrone.Core/Configuration/ConfigFileProvider.cs:178-190`, used at `Whisparr/Whisparr@94480761ec412cae40ba84e55c70b723851340cf:src/NzbDrone.Core/MetadataSource/SkyHook/SkyHookProxy.cs:45-52`; TMDb and StashDB are constants; v3 API resources not searched | Settings > General > Proxy; `/api/v3/config/host`; `Whisparr/Whisparr@94480761ec412cae40ba84e55c70b723851340cf:src/Whisparr.Api.V3/Config/HostConfigResource.cs:36-43,94-102` | Yes when the app proxy is off; `Whisparr/Whisparr@94480761ec412cae40ba84e55c70b723851340cf:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:159,174-176` | Custom validation callback; roots and pinning unverified; `Whisparr/Whisparr@94480761ec412cae40ba84e55c70b723851340cf:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:170` | No direct fallback; `Whisparr/Whisparr@94480761ec412cae40ba84e55c70b723851340cf:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:130-144` | Indexers, download clients, other \*arr, notifications, media server; `Sonarr/Sonarr@cab419ade8ac7fcab5bf80394ee492abd35d5f5a:src/NzbDrone.Core/Http/HttpProxySettingsProvider.cs:52-65` |

### Prowlarr (excluded)

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| None. Outbound traffic is configured indexers plus `prowlarr.servarr.com` and `releases.servarr.com`; `Prowlarr/Prowlarr@0dc20d8495bad9951c3fafe53f32b6e1c70fcf3b:src/NzbDrone.Common/Cloud/ProwlarrCloudRequestBuilder.cs:15-18`; `Prowlarr/Prowlarr@0dc20d8495bad9951c3fafe53f32b6e1c70fcf3b:src/Prowlarr.Core/Indexers/IndexerFactory.cs:22` | Not applicable | Has the \*arr proxy setting (`/api/v1/config/host`; `Prowlarr/Prowlarr@0dc20d8495bad9951c3fafe53f32b6e1c70fcf3b:src/Prowlarr.Api.V1/Config/HostConfigResource.cs:38-45,86-94`); not pointed at this proxy | Honours the variables like the other \*arr apps (`Prowlarr/Prowlarr@0dc20d8495bad9951c3fafe53f32b6e1c70fcf3b:src/NzbDrone.Common/Http/Dispatchers/ManagedHttpDispatcher.cs:178,194-196`); never set them on Prowlarr | Not applicable | Not applicable | All of it |

### Seerr

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| TMDb API and images, TVDB API and images; `seerr-team/seerr@18a90b4ed962cc56abc9087e305da5c18a3c7275:server/api/themoviedb/index.ts:186,528`; `seerr-team/seerr@18a90b4ed962cc56abc9087e305da5c18a3c7275:server/api/tvdb/index.ts:22-33,53,530` | No: the TVDB base is the constant `https://api4.thetvdb.com/v4` in `DEFAULT_CONFIG` (`seerr-team/seerr@18a90b4ed962cc56abc9087e305da5c18a3c7275:server/api/tvdb/index.ts:22-33`); settings only choose the provider per media type (`seerr-team/seerr@18a90b4ed962cc56abc9087e305da5c18a3c7275:server/lib/settings/index.ts:111-119`). Seerr caches TVDB answers itself (12 h default TTL, `seerr-team/seerr@18a90b4ed962cc56abc9087e305da5c18a3c7275:server/api/tvdb/index.ts:45`) | Settings > Network: proxy fields; GET/POST `/api/v1/settings/network`; `seerr-team/seerr@18a90b4ed962cc56abc9087e305da5c18a3c7275:server/lib/settings/index.ts:121-187`; `seerr-team/seerr@18a90b4ed962cc56abc9087e305da5c18a3c7275:src/components/Settings/SettingsNetwork/index.tsx:65-72,154-176` | Custom axios and undici agents; standard variable names not read; `seerr-team/seerr@18a90b4ed962cc56abc9087e305da5c18a3c7275:server/utils/customProxyAgent.ts:31-59,119-145` | Node default trust; custom CA and pinning unverified; `seerr-team/seerr@18a90b4ed962cc56abc9087e305da5c18a3c7275:server/utils/customProxyAgent.ts:119-145` | Agent construction at startup falls back to the default agent; behavior on a later proxy outage unverified; `seerr-team/seerr@18a90b4ed962cc56abc9087e305da5c18a3c7275:server/utils/customProxyAgent.ts:148-165` | \*arr, media server, notifications, other internal services; bypass matcher takes comma-separated entries and wildcard suffixes; `seerr-team/seerr@18a90b4ed962cc56abc9087e305da5c18a3c7275:server/utils/customProxyAgent.ts:75-100` |

### Jellyfin server and plugins

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| TMDb, OMDb, MusicBrainz, AudioDB, TVDB, Fanart; `jellyfin/jellyfin@83d0d0e7f76e23e2e085c8ff67d28b772d35967d:MediaBrowser.Providers/Plugins/Omdb/OmdbProvider.cs:269,335-378`; `jellyfin/jellyfin@83d0d0e7f76e23e2e085c8ff67d28b772d35967d:MediaBrowser.Providers/Plugins/AudioDb/AudioDbArtistProvider.cs:33,64,93`; `jellyfin/jellyfin-plugin-tvdb@f1bcc9edf37d5ee65cf3683e89b76226c09a22eb:Jellyfin.Plugin.Tvdb/TvdbUtils.cs:19`; `jellyfin/jellyfin-plugin-fanart@933264e19f2c1d713234f8c15b7049c2b9e371fe:Jellyfin.Plugin.Fanart/Plugin.cs:27-32` | MusicBrainz server only; `jellyfin/jellyfin@83d0d0e7f76e23e2e085c8ff67d28b772d35967d:MediaBrowser.Providers/Plugins/MusicBrainz/Configuration/PluginConfiguration.cs:13-30,42` | None found in server or provider source | Yes, .NET default proxy. Server handlers set no proxy (`jellyfin/jellyfin@83d0d0e7f76e23e2e085c8ff67d28b772d35967d:Jellyfin.Server/Startup.cs:88-99`) and back the named clients `Default` and `MusicBrainz` (:101-124). Fanart uses `NamedClient.Default` (`jellyfin/jellyfin-plugin-fanart@933264e19f2c1d713234f8c15b7049c2b9e371fe:Jellyfin.Plugin.Fanart/Providers/AlbumProvider.cs:233`, `Providers/MovieProvider.cs:229`, `Providers/SeriesProvider.cs:248`). The TVDB plugin builds its own `SocketsHttpHandler`, also without a proxy (`jellyfin/jellyfin-plugin-tvdb@f1bcc9edf37d5ee65cf3683e89b76226c09a22eb:Jellyfin.Plugin.Tvdb/TvdbClientManager.cs:759-769`). Confirm in log mode | No custom pinning in the inspected handlers; roots unverified; `jellyfin/jellyfin@83d0d0e7f76e23e2e085c8ff67d28b772d35967d:Jellyfin.Server/Startup.cs:88-99` | Metadata and image fetches fail while the proxy is down; playback and library serving stay direct. Kill switch: drop the variables and restart | Clients, library and media traffic, \*arr, notifications, every internal host in `NO_PROXY` (process-wide) |

### Emby

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| TVDB, TMDb and other providers named in docs and forum, not source-enumerated (closed source); [Emby network documentation](https://www.emby.media/support/articles/Hosting-Settings.html) (retrieved 2026-11-13) | Unverified | No application outbound proxy option, per an Emby team forum answer; [forum](https://emby.media/community/topic/114442-how-set-http-proxy-in-config/) (retrieved 2026-11-13) | Unverified. Emby is a .NET server, so the same default-proxy rule may apply; a log-mode trial with the variables set shows whether it does | Unverified | Unverified; same kill switch as Jellyfin if the trial succeeds | Clients, library and media traffic, \*arr, notifications, every internal host in `NO_PROXY` |

### Stash

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| Configured StashBox GraphQL and scraper or image URLs; `stashapp/stash@9e9ad972643320e737c711884f10e4285f0f3a88:pkg/stashbox/graphql/generated_client.go:30-31`; `stashapp/stash@9e9ad972643320e737c711884f10e4285f0f3a88:pkg/scraper/url.go:112-115` | Yes: StashBox and scraper URLs are configuration; `stashapp/stash@9e9ad972643320e737c711884f10e4285f0f3a88:pkg/stashbox/graphql/generated_client.go:30-31` | `proxy` and `no_proxy` settings; settings path and API resource unverified; `stashapp/stash@9e9ad972643320e737c711884f10e4285f0f3a88:internal/manager/config/config.go:150-167,1849-1867` | Yes: the app exports `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` and its clients use `ProxyFromEnvironment`; `stashapp/stash@9e9ad972643320e737c711884f10e4285f0f3a88:internal/manager/init.go:237-243`; `stashapp/stash@9e9ad972643320e737c711884f10e4285f0f3a88:pkg/utils/image.go:64-75` | Some image and scraper clients set `InsecureSkipVerify`; `stashapp/stash@9e9ad972643320e737c711884f10e4285f0f3a88:pkg/utils/image.go:66-70`; `stashapp/stash@9e9ad972643320e737c711884f10e4285f0f3a88:pkg/scraper/cache.go:127-134` | Direct fallback and retry unverified; proxy configuration is global; `stashapp/stash@9e9ad972643320e737c711884f10e4285f0f3a88:internal/manager/config/config.go:1849-1867` | Local services, media files, download clients, notifications; `no_proxy` takes Go syntax and defaults include private ranges; `stashapp/stash@9e9ad972643320e737c711884f10e4285f0f3a88:internal/manager/config/config.go:150-160,1864-1867` |

### Bazarr

| Metadata providers called | Base URL configurable? | Built-in HTTP/SOCKS proxy setting | Environment proxy | TLS trust | Proxy-down behavior | Traffic that must stay off proxy |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| Dynamic subtitle-provider registry plus Sonarr and Radarr APIs; `morpheus65535/bazarr@8c68f16a1e12f8e7985baeb73fb4199b5149108a:bazarr/app/get_providers.py:198-224` | No single base; provider URLs are runtime configuration; `morpheus65535/bazarr@8c68f16a1e12f8e7985baeb73fb4199b5149108a:bazarr/app/config.py:301-308` | Proxy type, URL, port, credentials and exclude settings; settings path and API resource unverified; `morpheus65535/bazarr@8c68f16a1e12f8e7985baeb73fb4199b5149108a:bazarr/app/config.py:301-308` | Yes: the app exports `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`; `morpheus65535/bazarr@8c68f16a1e12f8e7985baeb73fb4199b5149108a:bazarr/app/config.py:1036-1046` | Global provider SSL-disable switch; roots and pinning unverified; `morpheus65535/bazarr@8c68f16a1e12f8e7985baeb73fb4199b5149108a:bazarr/app/config.py:1049-1053`; `morpheus65535/bazarr@8c68f16a1e12f8e7985baeb73fb4199b5149108a:bazarr/sonarr/rootfolder.py:20-24` | Direct fallback and retry unverified; proxy errors handled in provider throttling; `morpheus65535/bazarr@8c68f16a1e12f8e7985baeb73fb4199b5149108a:bazarr/app/get_providers.py:82-94` | \*arr APIs, download clients, notifications, media server; exclude list goes to the requests environment handling, wildcard and range behavior unverified; `morpheus65535/bazarr@8c68f16a1e12f8e7985baeb73fb4199b5149108a:bazarr/app/config.py:1036-1046` |

<!-- markdownlint-enable MD013 -->
