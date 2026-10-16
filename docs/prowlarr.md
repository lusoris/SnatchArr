<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Prowlarr

[Prowlarr](https://github.com/Prowlarr/Prowlarr) (pronounced *PROWL-er*) is an indexer proxy and manager for the \*arr ecosystem. It syncs indexer configurations to Sonarr, Radarr, Lidarr and Readarr, tracks query limits, and reports hourly and daily indexer usage.

SnatchArr connects to Prowlarr in **read-only mode** to monitor indexer stamina and query limits ([issue #38](https://github.com/lusoris/SnatchArr/issues/38)). When every indexer an \*arr instance searches through has used up its Prowlarr query limit, SnatchArr defers that instance's searches instead of sending queries the indexers would refuse. Nothing is ever written to Prowlarr. API keys are stored encrypted at rest (`core/crypto` AES-GCM) and are write-only in the API.

## Setup

1. In Prowlarr, copy your API key from **Settings → General → Security → API Key**.
2. In SnatchArr, navigate to **Prowlarr** in the sidebar, click **Add link**, and enter:
   - **Name**: A label for this Prowlarr link (e.g. `Primary Prowlarr`).
   - **URL**: The base URL of your Prowlarr instance (e.g. `http://prowlarr:9696`).
   - **API Key**: The API key copied from Prowlarr.
3. Click **Test** to verify connectivity, then click **Save**.

### Checking the Prowlarr Server URL

For SnatchArr to match an \*arr instance's indexers to the corresponding Prowlarr indexers, the indexer URL configured in the \*arr must point back to Prowlarr.

In Prowlarr, navigate to **Settings → Apps**, select each application, and inspect the **Prowlarr Server** field. This URL must match what the \*arr instance uses to contact Prowlarr (for example, `http://prowlarr:9696` within a container network).

SnatchArr builds matching prefixes from both the link's base URL and each registered application's `prowlarrUrl` (`api/internal/prowlarr/service.go`). If an \*arr instance has indexers with automatic search enabled but none match any linked Prowlarr, SnatchArr logs a warning:

```text
no *arr indexer points at a linked Prowlarr; check the application's Prowlarr Server URL
```

## Gate Semantics

Before queueing a snatch run for an instance, the planner consults the Prowlarr gate (`api/internal/snatch/planner.go`).

The planner defers an instance's snatch with reason `indexer_limit` (`snatcharr_planner_deferrals_total{reason="indexer_limit"}`) **only when all** of the following conditions hold:

1. **Every** indexer of that \*arr instance with automatic search enabled (`enableAutomaticSearch: true`) routes through a linked Prowlarr instance.
2. **Every** such indexer is either:
   - At or above the exhaustion threshold of its Prowlarr query limit (`used_queries >= threshold * query_limit` in its daily or hourly window), OR
   - Disabled (`enable: false` or `disabledTill` in the future).
3. **No** unlimited indexer (`query_limit` unset or `0`) is currently alive.

### Partial Headroom

If some indexers have exhausted their budget while other automatic-search indexers still have headroom or are unlimited, the gate **never defers**. The snatch run proceeds, and indexer headroom is displayed on the dashboard card.

### Combining Stamina and Indexer Limits

Instance stamina and Prowlarr indexer limits operate in tandem:

| Metric | What it counts | Window | Scope |
| --- | --- | --- | --- |
| **Instance stamina** | Items searched on an \*arr instance | Each unit returns 60 to 61 minutes after it was spent ([Concepts](concepts.md#stamina)) | Per \*arr instance (`hourly_cap`) |
| **Prowlarr indexer limit** | Queries + RSS queries sent to an indexer | Limit window (1 hour or 1 day) | Per indexer in Prowlarr (`query_limit`) |

Both gates must allow the search: an instance with ample hourly stamina will still be deferred if all its indexers have reached their Prowlarr query ceiling, and vice-versa.

### Fail-Open Behavior

The Prowlarr gate fails open (allows snatching to continue) when:

- Prowlarr is unreachable or returns an HTTP error.
- An \*arr indexer is unknown to Prowlarr.
- An indexer is configured directly in the \*arr without going through Prowlarr.

This ensures unexpected network disruptions or unmanaged indexers do not permanently block library automation.

## Configuration

The threshold at which an indexer is treated as exhausted is configurable:

| Setting | Environment variable | Default | Range |
| --- | --- | --- | --- |
| `snatcharr.prowlarr.limit_threshold` | `APP_SNATCHARR_PROWLARR_LIMIT_THRESHOLD` | `0.9` | `(0, 1]` |

Validated on startup in `api/internal/config/config.go`. At the default value of `0.9`, SnatchArr treats an indexer with a limit of 1000 daily queries as exhausted once it reaches 900 queries.

## Observability

- **API endpoints**:
  - `GET /api/v1/prowlarr`: List configured links (`api/openapi/openapi.yaml`).
  - `POST /api/v1/prowlarr`: Add a link.
  - `PUT /api/v1/prowlarr/{linkId}`: Edit link URL, name, or update API key.
  - `DELETE /api/v1/prowlarr/{linkId}`: Remove a link.
  - `POST /api/v1/prowlarr/{linkId}/test`: Probe a stored link.
  - `GET /api/v1/prowlarr/status`: Live cached view (30-second TTL) of indexer status, limits, and used queries.
- **Metrics**:
  - Counter `snatcharr_planner_deferrals_total{reason="indexer_limit"}` records each deferral caused by exhausted indexers (`api/internal/metrics/metrics.go`).
