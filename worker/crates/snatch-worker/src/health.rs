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
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

use bytes::Bytes;
use http_body_util::Full;
use hyper::header::{CONTENT_TYPE, HeaderValue};
use hyper::server::conn::http1;
use hyper::service::service_fn;
use hyper::{Method, Response, StatusCode};
use hyper_util::rt::TokioIo;
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
    const ALL: [Self; 4] = [
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
#[derive(Debug)]
pub struct Health {
    start: Instant,
    stale: Duration,
    /// Milliseconds after `start` of the last successful call, plus one; 0 = none yet.
    last_contact: AtomicU64,
    lease_run: AtomicU64,
    heartbeat: AtomicU64,
    report_events: AtomicU64,
    complete_run: AtomicU64,
}

impl Health {
    /// No contact yet; stale once `stale` passes without one.
    pub fn new(stale: Duration) -> Self {
        Self {
            start: Instant::now(),
            stale,
            last_contact: AtomicU64::new(0),
            lease_run: AtomicU64::new(0),
            heartbeat: AtomicU64::new(0),
            report_events: AtomicU64::new(0),
            complete_run: AtomicU64::new(0),
        }
    }

    fn elapsed_ms(&self) -> u64 {
        u64::try_from(self.start.elapsed().as_millis()).unwrap_or(u64::MAX)
    }

    fn counter(&self, call: Call) -> &AtomicU64 {
        match call {
            Call::LeaseRun => &self.lease_run,
            Call::Heartbeat => &self.heartbeat,
            Call::ReportEvents => &self.report_events,
            Call::CompleteRun => &self.complete_run,
        }
    }

    /// Records a successful API call.
    pub fn contact(&self) {
        let stamp = self.elapsed_ms().saturating_add(1);
        self.last_contact.fetch_max(stamp, Ordering::Relaxed);
    }

    /// Records a failed API call.
    pub fn failure(&self, call: Call) {
        self.counter(call).fetch_add(1, Ordering::Relaxed);
    }

    /// Failed calls of one kind so far.
    pub fn failures(&self, call: Call) -> u64 {
        self.counter(call).load(Ordering::Relaxed)
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
    pub fn metrics(&self) -> String {
        let mut out = String::from(
            "# HELP snatcharr_worker_api_failures_total Calls from this worker to the SnatchArr API that failed, by call.\n\
             # TYPE snatcharr_worker_api_failures_total counter\n",
        );
        for call in Call::ALL {
            out.push_str(&format!(
                "snatcharr_worker_api_failures_total{{call=\"{}\"}} {}\n",
                call.label(),
                self.failures(call)
            ));
        }
        out.push_str(&format!(
            "# HELP snatcharr_worker_api_contact_age_seconds Seconds since the last successful call to the SnatchArr API, or since the start before the first one.\n\
             # TYPE snatcharr_worker_api_contact_age_seconds gauge\n\
             snatcharr_worker_api_contact_age_seconds {:.3}\n\
             # HELP snatcharr_worker_api_contact_stale_seconds The age after which /livez fails.\n\
             # TYPE snatcharr_worker_api_contact_stale_seconds gauge\n\
             snatcharr_worker_api_contact_stale_seconds {}\n",
            self.contact_age().as_secs_f64(),
            self.stale.as_secs()
        ));
        out
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
        return text(
            StatusCode::OK,
            "text/plain; version=0.0.4; charset=utf-8",
            health.metrics(),
        );
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
        let m = h.metrics();
        assert!(m.contains("snatcharr_worker_api_failures_total{call=\"lease_run\"} 0\n"));
        assert!(m.contains("snatcharr_worker_api_failures_total{call=\"heartbeat\"} 2\n"));
        assert!(m.contains("snatcharr_worker_api_failures_total{call=\"complete_run\"} 1\n"));
        assert!(m.contains("snatcharr_worker_api_contact_stale_seconds 180\n"));
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
            metrics.contains("content-type: text/plain; version=0.0.4"),
            "{metrics}"
        );
        assert!(
            metrics.contains("snatcharr_worker_api_failures_total{call=\"lease_run\"} 1\n"),
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
}
