// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Worker liveness (gap audit A1-01): when the worker last heard from the API, how many of its
//! calls to the API failed, and a small HTTP listener that shows both.
//!
//! `GET /livez` answers 200 while the last successful API call is at most the stale limit old,
//! and 503 once it is older, so the liveness probe restarts a worker that can no longer lease or
//! heartbeat instead of it looking healthy. Before the first successful call the age counts from
//! the start of the process, which gives a fresh worker the stale limit to reach the API.
//! `GET /metrics` serves the counters in the Prometheus text format.

use std::convert::Infallible;
use std::net::SocketAddr;
use std::sync::Arc;
use std::sync::atomic::{AtomicI64, AtomicU64, Ordering};
use std::time::Duration;

use bytes::Bytes;
use http_body_util::Full;
use hyper::header::{CONTENT_TYPE, HeaderValue};
use hyper::server::conn::http1;
use hyper::service::service_fn;
use hyper::{Method, Response, StatusCode};
use hyper_util::rt::TokioIo;
use prometheus_client::encoding::EncodeLabelSet;
use prometheus_client::encoding::text::encode;
use prometheus_client::metrics::counter::Counter;
use prometheus_client::metrics::family::Family;
use prometheus_client::metrics::gauge::Gauge;
use prometheus_client::metrics::histogram::Histogram;
use prometheus_client::registry::{Registry, Unit};
use tokio::net::TcpListener;
use tokio::sync::Semaphore;
use tokio::task::JoinSet;
use tokio::time::Instant;
use tokio_util::sync::CancellationToken;

/// Connections served at once (HISS-06); a connection over the bound is closed unanswered.
const MAX_CONNECTIONS: usize = 16;
/// Deadline for one connection, from accept to the last response (HISS-02). Probes and scrapes
/// send one short request; a client that keeps the connection open reconnects after it.
const CONNECTION_DEADLINE: Duration = Duration::from_secs(10);
/// Pause after a failed accept, so a full file table does not spin the loop.
const ACCEPT_BACKOFF: Duration = Duration::from_millis(100);

/// Labels for worker API failures.
#[derive(Clone, Debug, Hash, PartialEq, Eq, EncodeLabelSet)]
pub struct ApiFailureLabels {
    /// Call name.
    pub call: &'static str,
}

/// Labels for snatch run duration.
#[derive(Clone, Debug, Hash, PartialEq, Eq, EncodeLabelSet)]
pub struct RunDurationLabels {
    /// Snatch kind.
    pub kind: &'static str,
    /// Outcome of the run.
    pub outcome: &'static str,
}

/// Labels for *arr HTTP request duration.
#[derive(Clone, Debug, Hash, PartialEq, Eq, EncodeLabelSet)]
pub struct ArrDurationLabels {
    /// Application family.
    pub app: &'static str,
    /// Operation name.
    pub operation: &'static str,
}

/// Labels for *arr HTTP request errors.
#[derive(Clone, Debug, Hash, PartialEq, Eq, EncodeLabelSet)]
pub struct ArrErrorLabels {
    /// Application family.
    pub app: &'static str,
    /// Operation name.
    pub operation: &'static str,
    /// Error class (timeout, connect, status_4xx, status_5xx, decode).
    pub class: &'static str,
}

/// Labels for circuit breaker openings.
#[derive(Clone, Debug, Hash, PartialEq, Eq, EncodeLabelSet)]
pub struct CircuitLabels {
    /// Snatch kind.
    pub kind: &'static str,
}

/// An API call of the worker; the `call` label of the failure counter.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Call {
    /// `LeaseRun`.
    LeaseRun,
    /// `Heartbeat`.
    Heartbeat,
    /// `ReportEvents`.
    ReportEvents,
    /// `CompleteRun`.
    CompleteRun,
}

impl Call {
    /// All defined API calls.
    pub const ALL: [Self; 4] = [
        Self::LeaseRun,
        Self::Heartbeat,
        Self::ReportEvents,
        Self::CompleteRun,
    ];

    fn label(self) -> &'static str {
        match self {
            Self::LeaseRun => "lease_run",
            Self::Heartbeat => "heartbeat",
            Self::ReportEvents => "report_events",
            Self::CompleteRun => "complete_run",
        }
    }
}

/// Last API contact and failure counts, shared by the lease loop, the heartbeats, the runs and
/// the listener.
#[derive(Clone, Debug)]
pub struct Health {
    start: Instant,
    stale: Duration,
    /// Milliseconds after `start` of the last successful call, plus one; 0 = none yet.
    last_contact: Arc<AtomicU64>,
    registry: Arc<Registry>,
    api_failures: Family<ApiFailureLabels, Counter>,
    run_duration: Family<RunDurationLabels, Histogram>,
    arr_request_duration: Family<ArrDurationLabels, Histogram>,
    arr_request_errors: Family<ArrErrorLabels, Counter>,
    circuit_opened: Family<CircuitLabels, Counter>,
    contact_age: Gauge<f64, AtomicU64>,
    #[cfg_attr(not(test), allow(dead_code))]
    contact_stale: Gauge<i64, AtomicI64>,
}

const RUN_DURATION_BUCKETS: [f64; 10] = [0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0, 300.0];
fn new_run_duration_histogram() -> Histogram {
    Histogram::new(RUN_DURATION_BUCKETS)
}

const ARR_DURATION_BUCKETS: [f64; 10] = [0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0];
fn new_arr_duration_histogram() -> Histogram {
    Histogram::new(ARR_DURATION_BUCKETS)
}

struct ApiMetrics {
    failures: Family<ApiFailureLabels, Counter>,
    contact_age: Gauge<f64, AtomicU64>,
    contact_stale: Gauge<i64, AtomicI64>,
}

struct DomainMetrics {
    run_duration: Family<RunDurationLabels, Histogram>,
    arr_request_duration: Family<ArrDurationLabels, Histogram>,
    arr_request_errors: Family<ArrErrorLabels, Counter>,
    circuit_opened: Family<CircuitLabels, Counter>,
}

fn init_api_metrics(registry: &mut Registry, stale: Duration) -> ApiMetrics {
    let failures = Family::<ApiFailureLabels, Counter>::default();
    for call in Call::ALL {
        let _ = failures.get_or_create(&ApiFailureLabels { call: call.label() });
    }
    registry.register(
        "snatcharr_worker_api_failures",
        "Calls from this worker to the SnatchArr API that failed, by call",
        failures.clone(),
    );

    let contact_age = Gauge::<f64, AtomicU64>::default();
    registry.register_with_unit(
        "snatcharr_worker_api_contact_age",
        "Seconds since the last successful call to the SnatchArr API, or since the start before the first one",
        Unit::Seconds,
        contact_age.clone(),
    );

    let contact_stale = Gauge::<i64, AtomicI64>::default();
    contact_stale.set(i64::try_from(stale.as_secs()).unwrap_or(i64::MAX));
    registry.register_with_unit(
        "snatcharr_worker_api_contact_stale",
        "The age after which /livez fails",
        Unit::Seconds,
        contact_stale.clone(),
    );

    ApiMetrics {
        failures,
        contact_age,
        contact_stale,
    }
}

fn init_domain_metrics(registry: &mut Registry) -> DomainMetrics {
    let run_duration =
        Family::<RunDurationLabels, Histogram>::new_with_constructor(new_run_duration_histogram);
    registry.register_with_unit(
        "snatcharr_worker_run_duration",
        "Duration of snatch runs by snatch kind and outcome",
        Unit::Seconds,
        run_duration.clone(),
    );

    let arr_request_duration =
        Family::<ArrDurationLabels, Histogram>::new_with_constructor(new_arr_duration_histogram);
    registry.register_with_unit(
        "snatcharr_worker_arr_request_duration",
        "Duration of *arr REST API requests by app and operation",
        Unit::Seconds,
        arr_request_duration.clone(),
    );

    let arr_request_errors = Family::<ArrErrorLabels, Counter>::default();
    registry.register(
        "snatcharr_worker_arr_request_errors",
        "Failed *arr REST API requests by app, operation and error class",
        arr_request_errors.clone(),
    );

    let circuit_opened = Family::<CircuitLabels, Counter>::default();
    let _ = circuit_opened.get_or_create(&CircuitLabels { kind: "missing" });
    let _ = circuit_opened.get_or_create(&CircuitLabels { kind: "upgrade" });
    registry.register(
        "snatcharr_worker_circuit_opened",
        "Number of times the consecutive failure circuit breaker opened in a run",
        circuit_opened.clone(),
    );

    DomainMetrics {
        run_duration,
        arr_request_duration,
        arr_request_errors,
        circuit_opened,
    }
}

impl Health {
    /// No contact yet; stale once `stale` passes without one.
    pub fn new(stale: Duration) -> Self {
        let mut registry = Registry::default();
        let api = init_api_metrics(&mut registry, stale);
        let domain = init_domain_metrics(&mut registry);

        Self {
            start: Instant::now(),
            stale,
            last_contact: Arc::new(AtomicU64::new(0)),
            registry: Arc::new(registry),
            api_failures: api.failures,
            run_duration: domain.run_duration,
            arr_request_duration: domain.arr_request_duration,
            arr_request_errors: domain.arr_request_errors,
            circuit_opened: domain.circuit_opened,
            contact_age: api.contact_age,
            contact_stale: api.contact_stale,
        }
    }

    fn elapsed_ms(&self) -> u64 {
        u64::try_from(self.start.elapsed().as_millis()).unwrap_or(u64::MAX)
    }

    /// Records a successful API call.
    pub fn contact(&self) {
        let stamp = self.elapsed_ms().saturating_add(1);
        self.last_contact.fetch_max(stamp, Ordering::Relaxed);
    }

    /// Records a failed API call.
    pub fn failure(&self, call: Call) {
        self.api_failures
            .get_or_create(&ApiFailureLabels { call: call.label() })
            .inc();
    }

    /// Failed calls of one kind so far.
    #[cfg_attr(not(test), allow(dead_code))]
    pub fn failures(&self, call: Call) -> u64 {
        self.api_failures
            .get_or_create(&ApiFailureLabels { call: call.label() })
            .get()
    }

    /// Records the duration of a run.
    pub fn record_run_duration(
        &self,
        kind: &'static str,
        outcome: &'static str,
        duration: Duration,
    ) {
        self.run_duration
            .get_or_create(&RunDurationLabels { kind, outcome })
            .observe(duration.as_secs_f64());
    }

    /// Records a circuit breaker opening.
    pub fn circuit_opened(&self, kind: &'static str) {
        self.circuit_opened
            .get_or_create(&CircuitLabels { kind })
            .inc();
    }

    /// The underlying registry (used for verification against documentation).
    #[cfg_attr(not(test), allow(dead_code))]
    pub fn registry(&self) -> &Registry {
        &self.registry
    }

    /// True once any API call succeeded.
    pub fn contacted(&self) -> bool {
        self.last_contact.load(Ordering::Relaxed) > 0
    }

    /// Time since the last successful call, or since the start before the first one.
    pub fn contact_age(&self) -> Duration {
        let last = self.last_contact.load(Ordering::Relaxed).saturating_sub(1);
        Duration::from_millis(self.elapsed_ms().saturating_sub(last))
    }

    /// True once the last successful call is older than the stale limit.
    pub fn is_stale(&self) -> bool {
        self.contact_age() > self.stale
    }

    /// The counters in the Prometheus text exposition format.
    pub fn metrics(&self) -> Result<String, std::fmt::Error> {
        self.contact_age.set(self.contact_age().as_secs_f64());
        let mut out = String::new();
        encode(&mut out, &self.registry)?;
        Ok(out)
    }
}

impl arr_client::RequestObserver for Health {
    fn on_request(
        &self,
        app: arr_client::Kind,
        operation: &'static str,
        duration: Duration,
        error: Option<&arr_client::Error>,
    ) {
        self.arr_request_duration
            .get_or_create(&ArrDurationLabels {
                app: app.app_label(),
                operation,
            })
            .observe(duration.as_secs_f64());

        if let Some(err) = error {
            self.arr_request_errors
                .get_or_create(&ArrErrorLabels {
                    app: app.app_label(),
                    operation,
                    class: err.error_class(),
                })
                .inc();
        }
    }
}

fn text(status: StatusCode, content_type: &'static str, body: String) -> Response<Full<Bytes>> {
    let mut resp = Response::new(Full::new(Bytes::from(body)));
    *resp.status_mut() = status;
    resp.headers_mut()
        .insert(CONTENT_TYPE, HeaderValue::from_static(content_type));
    resp
}

/// The answer to one request.
pub(crate) fn respond(method: &Method, path: &str, health: &Health) -> Response<Full<Bytes>> {
    const PLAIN: &str = "text/plain; charset=utf-8";
    if path != "/livez" && path != "/metrics" {
        return text(StatusCode::NOT_FOUND, PLAIN, "not found\n".to_owned());
    }
    if method != Method::GET {
        let mut resp = text(
            StatusCode::METHOD_NOT_ALLOWED,
            PLAIN,
            "GET only\n".to_owned(),
        );
        resp.headers_mut()
            .insert(hyper::header::ALLOW, HeaderValue::from_static("GET"));
        return resp;
    }
    if path == "/metrics" {
        return match health.metrics() {
            Ok(body) => text(
                StatusCode::OK,
                "application/openmetrics-text; version=1.0.0; charset=utf-8",
                body,
            ),
            Err(e) => {
                tracing::error!(error = %e, "metrics encoding failed");
                text(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    PLAIN,
                    "metrics encoding failed\n".to_owned(),
                )
            }
        };
    }
    let age = health.contact_age().as_secs();
    let seen = if health.contacted() {
        format!("last successful API call {age} s ago")
    } else {
        format!("no successful API call in the {age} s since start")
    };
    if health.is_stale() {
        text(
            StatusCode::SERVICE_UNAVAILABLE,
            PLAIN,
            format!("stale: {seen}\n"),
        )
    } else {
        text(StatusCode::OK, PLAIN, format!("ok: {seen}\n"))
    }
}

/// Binds the listener; a worker that cannot bind fails at start instead of running unprobed.
pub async fn bind(addr: SocketAddr) -> std::io::Result<TcpListener> {
    TcpListener::bind(addr).await
}

/// Serves `/livez` and `/metrics` until `stop` is cancelled.
pub async fn serve(listener: TcpListener, health: Arc<Health>, stop: CancellationToken) {
    let slots = Arc::new(Semaphore::new(MAX_CONNECTIONS));
    let mut conns = JoinSet::new();
    while !stop.is_cancelled() {
        let accepted = tokio::select! {
            a = listener.accept() => a,
            () = stop.cancelled() => break,
        };
        let stream = match accepted {
            Ok((stream, _)) => stream,
            Err(e) => {
                tracing::warn!(error = %e, "health listener: accept failed");
                tokio::time::sleep(ACCEPT_BACKOFF).await;
                continue;
            }
        };
        let Ok(slot) = slots.clone().try_acquire_owned() else {
            tracing::warn!(
                max = MAX_CONNECTIONS,
                "health listener: connection bound reached"
            );
            continue;
        };
        let health = health.clone();
        conns.spawn(async move {
            let _slot = slot;
            let service = service_fn(move |req| {
                let resp = respond(req.method(), req.uri().path(), &health);
                async move { Ok::<_, Infallible>(resp) }
            });
            let conn = http1::Builder::new().serve_connection(TokioIo::new(stream), service);
            match tokio::time::timeout(CONNECTION_DEADLINE, conn).await {
                Ok(Ok(())) | Err(_) => {}
                Ok(Err(e)) => tracing::debug!(error = %e, "health listener: connection failed"),
            }
        });
        while conns.try_join_next().is_some() {}
    }
    conns.abort_all();
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};

    const STALE: Duration = Duration::from_secs(180);

    async fn body(resp: Response<Full<Bytes>>) -> (StatusCode, String) {
        use http_body_util::BodyExt;
        let status = resp.status();
        let bytes = match resp.into_body().collect().await {
            Ok(collected) => collected.to_bytes(),
            Err(never) => match never {},
        };
        (status, String::from_utf8_lossy(&bytes).into_owned())
    }

    #[tokio::test(start_paused = true)]
    async fn fresh_worker_is_live_until_the_stale_limit() {
        let h = Health::new(STALE);
        assert!(!h.contacted());
        assert!(!h.is_stale());
        tokio::time::advance(STALE).await;
        assert!(!h.is_stale(), "exactly the limit is still live");
        tokio::time::advance(Duration::from_millis(1)).await;
        assert!(h.is_stale(), "past the limit without contact is stale");
    }

    #[tokio::test(start_paused = true)]
    async fn contact_resets_the_age() {
        let h = Health::new(STALE);
        tokio::time::advance(STALE + Duration::from_secs(5)).await;
        assert!(h.is_stale());
        h.contact();
        assert!(h.contacted());
        assert!(!h.is_stale());
        assert_eq!(h.contact_age(), Duration::ZERO);
        tokio::time::advance(STALE).await;
        assert!(!h.is_stale());
        tokio::time::advance(Duration::from_millis(1)).await;
        assert!(h.is_stale());
    }

    #[tokio::test(start_paused = true)]
    async fn failures_count_per_call() {
        let h = Health::new(STALE);
        h.failure(Call::Heartbeat);
        h.failure(Call::Heartbeat);
        h.failure(Call::CompleteRun);
        assert_eq!(h.failures(Call::LeaseRun), 0);
        assert_eq!(h.failures(Call::Heartbeat), 2);
        assert_eq!(h.failures(Call::ReportEvents), 0);
        assert_eq!(h.failures(Call::CompleteRun), 1);
        assert!(!h.contacted(), "a failure is no contact");
        assert_eq!(h.contact_stale.get(), 180);
        let m = h.metrics().expect("metrics");
        assert!(m.contains("snatcharr_worker_api_failures_total{call=\"lease_run\"} 0\n"));
        assert!(m.contains("snatcharr_worker_api_failures_total{call=\"heartbeat\"} 2\n"));
        assert!(m.contains("snatcharr_worker_api_failures_total{call=\"complete_run\"} 1\n"));
        assert!(m.contains("snatcharr_worker_api_contact_stale_seconds 180\n"));
    }

    #[test]
    fn registry_is_accessible() {
        let h = Health::new(STALE);
        assert!(!std::ptr::eq(h.registry(), std::ptr::null()));
    }

    #[tokio::test(start_paused = true)]
    async fn livez_answers_503_once_stale() {
        let h = Health::new(STALE);
        let (status, text) = body(respond(&Method::GET, "/livez", &h)).await;
        assert_eq!(status, StatusCode::OK, "{text}");
        tokio::time::advance(STALE + Duration::from_secs(1)).await;
        let (status, text) = body(respond(&Method::GET, "/livez", &h)).await;
        assert_eq!(status, StatusCode::SERVICE_UNAVAILABLE);
        assert_eq!(
            text,
            "stale: no successful API call in the 181 s since start\n"
        );
        // After a contact, /livez names the age of the last call, then fails once it passes.
        h.contact();
        tokio::time::advance(Duration::from_secs(2)).await;
        let (status, text) = body(respond(&Method::GET, "/livez", &h)).await;
        assert_eq!(status, StatusCode::OK);
        assert_eq!(text, "ok: last successful API call 2 s ago\n");
        tokio::time::advance(STALE).await;
        let (status, text) = body(respond(&Method::GET, "/livez", &h)).await;
        assert_eq!(status, StatusCode::SERVICE_UNAVAILABLE);
        assert_eq!(text, "stale: last successful API call 182 s ago\n");
    }

    #[tokio::test(start_paused = true)]
    async fn unknown_paths_and_methods_are_refused() {
        let h = Health::new(STALE);
        assert_eq!(
            body(respond(&Method::GET, "/", &h)).await.0,
            StatusCode::NOT_FOUND
        );
        assert_eq!(
            body(respond(&Method::GET, "/livez/", &h)).await.0,
            StatusCode::NOT_FOUND
        );
        let post = respond(&Method::POST, "/livez", &h);
        assert_eq!(
            post.headers().get(hyper::header::ALLOW),
            Some(&HeaderValue::from_static("GET"))
        );
        assert_eq!(body(post).await.0, StatusCode::METHOD_NOT_ALLOWED);
    }

    /// Sends one HTTP/1.1 request to the listener and returns the raw response.
    async fn get(addr: SocketAddr, path: &str) -> String {
        let mut s = tokio::net::TcpStream::connect(addr).await.expect("connect");
        let req = format!("GET {path} HTTP/1.1\r\nHost: worker\r\nConnection: close\r\n\r\n");
        s.write_all(req.as_bytes()).await.expect("write");
        let mut out = String::new();
        s.read_to_string(&mut out).await.expect("read");
        out
    }

    #[tokio::test]
    async fn listener_serves_livez_and_metrics_until_stopped() {
        let listener = bind("127.0.0.1:0".parse().expect("addr"))
            .await
            .expect("bind");
        let addr = listener.local_addr().expect("addr");
        let h = Arc::new(Health::new(STALE));
        h.failure(Call::LeaseRun);
        let stop = CancellationToken::new();
        let server = tokio::spawn(serve(listener, h.clone(), stop.clone()));

        let live = get(addr, "/livez").await;
        assert!(live.starts_with("HTTP/1.1 200 OK\r\n"), "{live}");
        let metrics = get(addr, "/metrics").await;
        assert!(
            metrics.contains(
                "content-type: application/openmetrics-text; version=1.0.0; charset=utf-8"
            ),
            "{metrics}"
        );
        assert!(
            metrics.trim_end().ends_with("# EOF"),
            "OpenMetrics exposition must end with # EOF, got: {metrics}"
        );
        assert!(
            metrics.contains("snatcharr_worker_api_failures_total{call=\"lease_run\"} 1"),
            "{metrics}"
        );

        stop.cancel();
        tokio::time::timeout(Duration::from_secs(5), server)
            .await
            .expect("listener stops")
            .expect("listener task");
        assert!(
            tokio::net::TcpStream::connect(addr).await.is_err(),
            "the port is closed after stop"
        );
    }

    #[tokio::test]
    async fn run_duration_and_circuit_metrics_render_correctly() {
        let h = Health::new(STALE);
        h.record_run_duration("missing", "done", Duration::from_millis(750));
        h.circuit_opened("missing");

        let m = h.metrics().expect("metrics");
        assert!(
            m.contains(
                "snatcharr_worker_run_duration_seconds_count{kind=\"missing\",outcome=\"done\"} 1"
            ),
            "{m}"
        );
        assert!(
            m.contains("snatcharr_worker_circuit_opened_total{kind=\"missing\"} 1"),
            "{m}"
        );
        assert!(
            m.contains("snatcharr_worker_circuit_opened_total{kind=\"upgrade\"} 0"),
            "{m}"
        );
    }

    #[test]
    fn no_help_line_ends_with_double_period() {
        let h = Health::new(STALE);
        let m = h.metrics().expect("metrics");
        let mut help_count = 0;
        for line in m.lines() {
            if line.starts_with("# HELP") {
                help_count += 1;
                assert!(
                    !line.ends_with(".."),
                    "help line ends with double period: {line}"
                );
            }
        }
        assert!(help_count > 0, "no help lines found");
    }

    #[tokio::test]
    async fn arr_observer_metrics_render_correctly() {
        use arr_client::{Error, Kind, RequestObserver};
        let h = Health::new(STALE);

        // Success call
        h.on_request(Kind::Sonarr, "wanted", Duration::from_millis(120), None);

        // Error call
        let err = Error::Status {
            method: "GET",
            path: "wanted".to_owned(),
            status: 500,
            body: String::new(),
        };
        h.on_request(
            Kind::Sonarr,
            "wanted",
            Duration::from_millis(250),
            Some(&err),
        );

        let m = h.metrics().expect("metrics");
        assert!(
            m.contains("snatcharr_worker_arr_request_duration_seconds_count{app=\"sonarr\",operation=\"wanted\"} 2"),
            "{m}"
        );
        assert!(
            m.contains("snatcharr_worker_arr_request_errors_total{app=\"sonarr\",operation=\"wanted\",class=\"status_5xx\"} 1"),
            "{m}"
        );
    }

    fn parse_worker_doc_table(doc_content: &str) -> std::collections::HashMap<String, Vec<String>> {
        let mut map = std::collections::HashMap::new();
        let mut in_worker = false;
        let mut in_table = false;
        for line in doc_content.lines() {
            let trimmed = line.trim();
            if trimmed.starts_with("## Worker Metrics") {
                in_worker = true;
                continue;
            }
            if in_worker && trimmed.starts_with("## ") {
                break;
            }
            if !in_worker || !trimmed.starts_with('|') {
                if in_table {
                    break;
                }
                continue;
            }
            let parts: Vec<&str> = trimmed
                .trim_matches('|')
                .split('|')
                .map(|p| p.trim().trim_matches('`').trim())
                .collect();
            let name = match parts.first() {
                Some(&n) if n != "Metric" && !n.starts_with("---") => n,
                _ => {
                    in_table = true;
                    continue;
                }
            };
            in_table = true;
            let mut labels = Vec::new();
            if let Some(&raw_labels) = parts.get(2) {
                let has_labels =
                    raw_labels != "none" && raw_labels != "-" && !raw_labels.is_empty();
                if has_labels {
                    for label in raw_labels.split(',') {
                        let cleaned = label.trim().trim_matches('`').trim();
                        if !cleaned.is_empty() {
                            labels.push(cleaned.to_owned());
                        }
                    }
                }
            }
            labels.sort();
            map.insert(name.to_owned(), labels);
        }
        map
    }

    fn gather_worker_code_metrics(h: &Health) -> std::collections::HashMap<String, Vec<String>> {
        use arr_client::{Error, Kind, RequestObserver};
        h.failure(Call::LeaseRun);
        h.record_run_duration("missing", "done", Duration::from_millis(50));
        h.circuit_opened("missing");
        h.on_request(Kind::Sonarr, "wanted", Duration::from_millis(10), None);
        let err = Error::Status {
            method: "GET",
            path: "test".into(),
            status: 500,
            body: String::new(),
        };
        h.on_request(
            Kind::Sonarr,
            "wanted",
            Duration::from_millis(10),
            Some(&err),
        );

        let text = h.metrics().expect("metrics");
        let mut map = std::collections::HashMap::new();
        for line in text.lines() {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            if let Some((prefix, rest)) = line.split_once('{') {
                let mut base = prefix.trim();
                if let Some(stripped) = base.strip_suffix("_bucket") {
                    base = stripped;
                } else if let Some(stripped) = base.strip_suffix("_sum") {
                    base = stripped;
                } else if let Some(stripped) = base.strip_suffix("_count") {
                    base = stripped;
                }
                if let Some((label_content, _)) = rest.split_once('}') {
                    let mut labels = Vec::new();
                    for pair in label_content.split(',') {
                        if let Some((k, _)) = pair.split_once('=') {
                            let k = k.trim();
                            if k != "le" {
                                labels.push(k.to_owned());
                            }
                        }
                    }
                    labels.sort();
                    labels.dedup();
                    map.insert(base.to_owned(), labels);
                }
            } else if let Some((name, _val)) = line.split_once(' ') {
                map.entry(name.trim().to_owned()).or_insert_with(Vec::new);
            }
        }
        map
    }

    fn check_worker_metrics_sync(
        code: &std::collections::HashMap<String, Vec<String>>,
        doc: &std::collections::HashMap<String, Vec<String>>,
    ) -> Result<(), String> {
        for (name, code_labels) in code {
            match doc.get(name) {
                None => {
                    return Err(format!(
                        "metric {name} is registered in code but not documented in docs/metrics.md"
                    ));
                }
                Some(doc_labels) => {
                    if code_labels != doc_labels {
                        return Err(format!(
                            "metric {name} label mismatch: code {code_labels:?}, doc {doc_labels:?}"
                        ));
                    }
                }
            }
        }
        for name in doc.keys() {
            if !code.contains_key(name) {
                return Err(format!(
                    "metric {name} is documented in docs/metrics.md but not registered in code"
                ));
            }
        }
        Ok(())
    }

    #[test]
    fn test_docs_sync_worker_metrics() {
        let manifest_dir = env!("CARGO_MANIFEST_DIR");
        let doc_path = std::path::Path::new(manifest_dir).join("../../../docs/metrics.md");
        let doc_content = std::fs::read_to_string(&doc_path);
        assert!(
            doc_content.is_ok(),
            "failed to read {}: {:?}",
            doc_path.display(),
            doc_content.as_ref().err()
        );
        let doc_content = match doc_content {
            Ok(c) => c,
            Err(_) => return,
        };
        let doc_metrics = parse_worker_doc_table(&doc_content);
        assert!(!doc_metrics.is_empty(), "parsed 0 worker metrics from docs");

        let h = Health::new(STALE);
        let code_metrics = gather_worker_code_metrics(&h);
        let sync_result = check_worker_metrics_sync(&code_metrics, &doc_metrics);
        assert!(
            sync_result.is_ok(),
            "Worker metrics and docs/metrics.md out of sync: {:?}",
            sync_result.err()
        );
    }

    #[test]
    fn test_docs_sync_worker_metrics_planted_negatives() {
        let mut valid_code = std::collections::HashMap::new();
        valid_code.insert("snatcharr_worker_test".to_owned(), vec!["call".to_owned()]);

        let mut valid_doc = std::collections::HashMap::new();
        valid_doc.insert("snatcharr_worker_test".to_owned(), vec!["call".to_owned()]);

        // Planted missing in doc
        let empty_doc = std::collections::HashMap::new();
        let err1 = match check_worker_metrics_sync(&valid_code, &empty_doc) {
            Err(e) => e,
            Ok(()) => String::new(),
        };
        assert!(err1.contains("not documented in docs/metrics.md"), "{err1}");

        // Planted extra in doc
        let mut extra_doc = valid_doc.clone();
        extra_doc.insert("snatcharr_worker_ghost".to_owned(), vec![]);
        let err2 = match check_worker_metrics_sync(&valid_code, &extra_doc) {
            Err(e) => e,
            Ok(()) => String::new(),
        };
        assert!(err2.contains("not registered in code"), "{err2}");

        // Planted label mismatch
        let mut wrong_labels_doc = std::collections::HashMap::new();
        wrong_labels_doc.insert("snatcharr_worker_test".to_owned(), vec!["wrong".to_owned()]);
        let err3 = match check_worker_metrics_sync(&valid_code, &wrong_labels_doc) {
            Err(e) => e,
            Ok(()) => String::new(),
        };
        assert!(err3.contains("label mismatch"), "{err3}");
    }
}
