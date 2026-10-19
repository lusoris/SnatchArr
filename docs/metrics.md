# Metrics and Observability

SnatchArr exports Prometheus metrics from both its Go control plane (API) and its Rust data plane (worker) to give full operational visibility into search throughput, rate-limit stamina, run scheduling, circuit breaker activity, and upstream *arr API health.

## Scraping & Getting Started

### Helm (Kubernetes)

To scrape SnatchArr with the Prometheus Operator, enable the built-in ServiceMonitor and PodMonitor in `values.yaml`:

```yaml
serviceMonitor:
  enabled: true
  interval: 30s
  scrapeTimeout: 10s
```

This provisions:
- A `ServiceMonitor` targeting the control plane API `/metrics` endpoint on port `http` (8080).
- A `PodMonitor` targeting the worker data plane `/metrics` endpoint on port `http` (8081).

To automatically deploy the pre-packaged Grafana dashboard, enable:

```yaml
grafana:
  dashboards:
    enabled: true
    labels:
      grafana_dashboard: "1"
```

This creates a ConfigMap holding `snatcharr.json`, recognized by the Grafana sidecar or operator.

### Docker Compose

For local development or single-host deployments, an observability profile is provided in `deploy/compose/docker-compose.yml`:

```bash
docker compose -f deploy/compose/docker-compose.yml --profile observability up -d
```

This launches:
- **Prometheus** on `http://localhost:9091` scraping the API (`host.docker.internal:8080`) and worker (`host.docker.internal:8081`).
- **Grafana** on `http://localhost:3000` (anonymous Admin enabled) with Prometheus provisioned as default data source (`uid: prometheus`) and the SnatchArr dashboard pre-loaded.
- **OpenTelemetry Collector** on port 4317.

### Grafana Dashboard

The standalone dashboard JSON is located at `deploy/helm/snatcharr/dashboards/snatcharr.json`. It expects a Prometheus data source variable `${datasource}` and includes panel visualizations for:
- Searches dispatched rate by instance and snatch kind.
- Hourly stamina spent vs. remaining per instance and global.
- Run counts by lifecycle status.
- Run lease wait time percentiles (p50 and p95).
- Planner deferral rates by classification reason.
- Pending Overseerr / Jellyseerr processing requests.
- Leader loop success age and failure rates.
- Budget grant outcomes (granted, partial, denied, error).
- Worker run duration (p95) by kind and outcome.
- *arr REST API request latency (p95) and error rates by class.
- Circuit breaker trip counts.
- Worker API communication failures and last contact age.

---

## Architectural Decisions

### Data Plane Endpoint Separation

The worker serves its own `/metrics` endpoint directly on port 8081 using the `prometheus-client` crate, rather than sending metric payloads back to the API over gRPC `WorkerService`.
- **Reasoning**: Keeps the data plane stateless, avoids protobuf protocol bloat and serialization overhead, preserves per-pod time-series cardinality, and works directly with Kubernetes `PodMonitor` and Prometheus standard scraping.

### Leader-Only Domain State Gauges

State gauges (`snatcharr_stamina_used_items`, `snatcharr_stamina_remaining_items`, `snatcharr_runs`, and `snatcharr_seerr_requests_pending`) are refreshed by an internal periodic leader loop (`metrics: state`, interval 30 seconds).
- **Behavior**: Only the elected replica (`APP_LEADER_ENABLED=true`, Postgres advisory lock) queries the database to refresh these gauges. Non-leader replicas do not export these metrics.
- **Aggregation**: Queries and dashboards must aggregate these metrics using `max()` (e.g. `max by (arr_instance) (snatcharr_stamina_used_items)`).
- **Liveness**: Loop ticks feed `snatcharr_loop_last_success_timestamp_seconds{loop="metrics_state"}` and `snatcharr_loop_failures_total{loop="metrics_state"}`, making leader query health directly alertable.

### Seerr Pending Definition

`snatcharr_seerr_requests_pending` represents the total number of active processing media requests across all enabled Overseerr / Jellyseerr links currently stored in the SnatchArr database (`seerr_requests` table).

The `seerr_requests` table only ever holds open/processing requests:
- Synchronisation (`Service.Sync` in `api/internal/seerr/sync.go`) fetches only processing requests through `Client.ProcessingRequests` (`/api/v1/request?filter=processing`, `api/internal/seerrclient/client.go`).
- At the end of every sync, rows that Seerr no longer returns (fulfilled, available or declined) are deleted by `DeleteUnseenSeerrRequests` (`api/internal/store/queries/seerr.sql`), using the tolerance `staleAfter` (one second, `api/internal/seerr/service.go`).

### Circuit Breakers

The circuit breaker halts further *arr search requests within a run after consecutive failures exceed threshold (`MAX_CONSECUTIVE_FAILURES = 5`). Because circuits are scoped to individual runs, `snatcharr_worker_circuit_opened_total` is exported as a counter of circuit opening events, not a gauge.

---

## API Metrics (Control Plane)

The API serves these metrics on `/metrics`:

| Metric | Type | Labels | Leader only | Description |
| --- | --- | --- | --- | --- |
| `snatcharr_loop_failures_total` | counter | `loop` | no | Ticks of a leader loop that returned an error, by loop name. |
| `snatcharr_loop_last_success_timestamp_seconds` | gauge | `loop` | no | Unix timestamp of the last successful tick of a leader loop, by loop name. |
| `snatcharr_loop_interval_seconds` | gauge | `loop` | no | Configured interval between ticks of a leader loop, by loop name. |
| `snatcharr_planner_instance_failures_total` | counter | `arr_instance` | no | Planner passes that skipped an instance because planning failed. |
| `snatcharr_runs_queued_total` | counter | none | no | Total runs queued by the planner. |
| `snatcharr_runs_leased_total` | counter | `kind` | no | Runs leased by a worker, by snatch kind (`missing`, `upgrade`). |
| `snatcharr_runs_finished_total` | counter | `outcome` | no | Runs completed, by outcome (`done`, `failed`, `cancelled`, `lease_exhausted`). |
| `snatcharr_worker_events_total` | counter | `type` | no | Events reported by workers, by SSE event type (`search_dispatched`, etc.). |
| `snatcharr_worker_events_unconfirmed_total` | counter | none | no | Events emitted for completed runs that were not confirmed. |
| `snatcharr_budget_requests_total` | counter | `result` | no | Worker budget requests by outcome (`granted`, `partial`, `denied`, `error`). |
| `snatcharr_searches_dispatched_total` | counter | `arr_instance`, `kind` | no | Total items dispatched to *arr instances by instance name and kind. |
| `snatcharr_search_last_dispatched_timestamp_seconds` | gauge | `arr_instance` | no | Unix time of the last search dispatched to an *arr instance, by instance name. |
| `snatcharr_stamina_used_items` | gauge | `arr_instance` | yes | Hourly stamina spent in current rolling window by instance and `_global`. |
| `snatcharr_stamina_remaining_items` | gauge | `arr_instance` | yes | Hourly stamina remaining under effective cap by instance and `_global`. |
| `snatcharr_runs` | gauge | `status` | yes | Current count of runs by lifecycle status (`queued`, `leased`, `done`, `failed`, `cancelled`). |
| `snatcharr_seerr_requests_pending` | gauge | none | yes | Current count of active/pending processing Seerr requests across enabled links. |
| `snatcharr_run_lease_wait_seconds` | histogram | `kind` | no | Seconds elapsed between run enqueue and worker lease acquisition, by kind. |
| `snatcharr_planner_deferrals_total` | counter | `reason` | no | Planner decisions not to enqueue a run, by classified reason enum. |

### Label Values (API)

- `kind`: `missing`, `upgrade`
- `outcome`: `done`, `failed`, `cancelled`, `lease_exhausted`
- `result`: `granted`, `partial`, `denied`, `error`
- `status`: `queued`, `leased`, `done`, `failed`, `cancelled`
- `reason`: `schedule_closed`, `already_running`, `client_unreachable`, `client_paused`, `client_active_limit`, `bandwidth_saturated`, `gate_refusal`, `indexer_limit`, `tick_bound`

### Upgrading from 0.4.x

In SnatchArr 0.5.0+, the *arr instance metric label was renamed from `instance` to `arr_instance`. Prometheus scrape jobs attach a target label named `instance` (the scraped host:port); when target metrics also declare an `instance` label with default scrape settings (`honor_labels: false`), Prometheus renames the exported metric label to `exported_instance`. Renaming to `arr_instance` eliminates this collision.

Update existing alert rules, recording rules, and Grafana dashboards by replacing `instance=` selectors, `by (instance)` groupings, and any workarounds referencing `exported_instance` with `arr_instance`.

---

## Worker Metrics (Data Plane)

The worker data plane serves these metrics on its HTTP health port (`8081` by default) at `/metrics`:

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `snatcharr_worker_api_failures_total` | counter | `call` | Calls from worker to SnatchArr gRPC API that failed, by call type. |
| `snatcharr_worker_api_contact_age_seconds` | gauge | none | Seconds since last successful call to the SnatchArr gRPC API. |
| `snatcharr_worker_api_contact_stale_seconds` | gauge | none | Configured duration after which worker fails `/livez`. |
| `snatcharr_worker_run_duration_seconds` | histogram | `kind`, `outcome` | Duration in seconds of snatch runs by snatch kind and terminal outcome. |
| `snatcharr_worker_arr_request_duration_seconds` | histogram | `app`, `operation` | Duration in seconds of *arr REST API HTTP requests by app and operation. |
| `snatcharr_worker_arr_request_errors_total` | counter | `app`, `operation`, `class` | Failed *arr REST API requests by app, operation, and error class. |
| `snatcharr_worker_circuit_opened_total` | counter | `kind` | Number of times the consecutive failure circuit breaker opened in a run. |

### Label Values (Worker)

- `call`: `lease_run`, `heartbeat`, `report_events`, `complete_run`
- `kind`: `missing`, `upgrade`
- `outcome`: `done`, `failed`, `cancelled`
- `app`: `sonarr`, `radarr`, `lidarr`, `readarr`
- `class`: `timeout`, `connect`, `status_4xx`, `status_5xx`, `decode`
