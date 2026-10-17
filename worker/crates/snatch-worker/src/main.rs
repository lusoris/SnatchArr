// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! SnatchArr snatch-worker: leases snatch runs from the API over gRPC and executes them
//! against *arr instances (ADR-0001, ADR-0002). Stateless; safe to scale horizontally.

#![cfg_attr(test, allow(clippy::unwrap_used, clippy::expect_used))]

mod auth;
mod config;
mod executor;
mod health;
mod logship;
mod probe;
mod resend;

use std::sync::Arc;
use std::time::Duration;

use snatch_proto::worker_service_client::WorkerServiceClient;
use snatch_proto::{
    HeartbeatDirective, HeartbeatRequest, LeaseRunRequest, LeaseRunResponse, LogRecord,
    ReportLogsRequest,
};
use tokio::sync::{Semaphore, mpsc};
use tokio_util::sync::CancellationToken;
use tonic::transport::Channel;
use tonic::{Code, Request, Status};
use tracing_subscriber::EnvFilter;
use tracing_subscriber::layer::SubscriberExt;
use tracing_subscriber::util::SubscriberInitExt;

use crate::auth::AuthInterceptor;
use crate::config::Config;
use crate::executor::Client;
use crate::health::{Call, Health};

/// Upper bound on heartbeats per run (HISS-02): 2^20 * 5 s is ~60 days.
const MAX_HEARTBEATS: u32 = 1 << 20;

/// Backoff bounds for API connectivity problems (HISS-02).
const MIN_BACKOFF: Duration = Duration::from_secs(1);
const MAX_BACKOFF: Duration = Duration::from_secs(30);

/// Longest wait at shutdown for the last log records to reach the API.
const LOG_DRAIN: Duration = Duration::from_secs(10);

fn env_filter() -> EnvFilter {
    EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"))
}

fn init_tracing() {
    tracing_subscriber::fmt()
        .json()
        .with_env_filter(env_filter())
        .with_target(false)
        .init();
}

/// Logs to stdout as `init_tracing` does and copies every record into the log shipper's
/// channel (#349).
fn init_shipped_tracing() -> (mpsc::Receiver<LogRecord>, Arc<logship::Dropped>) {
    let (layer, rx, dropped) = logship::channel(logship::CAPACITY);
    tracing_subscriber::registry()
        .with(env_filter())
        .with(tracing_subscriber::fmt::layer().json().with_target(false))
        .with(layer)
        .init();
    (rx, dropped)
}

/// Ships the worker's log records to the API until `stop`, each call within its deadline.
fn spawn_shipper(
    client: &Client,
    worker_id: String,
    rx: mpsc::Receiver<LogRecord>,
    dropped: Arc<logship::Dropped>,
    stop: CancellationToken,
) -> tokio::task::JoinHandle<()> {
    let client = client.clone();
    let send = move |req: ReportLogsRequest| {
        let client = client.clone();
        async move {
            resend::once_more(|| {
                let mut c = client.clone();
                let mut r = Request::new(req.clone());
                r.set_timeout(logship::CALL_TIMEOUT);
                async move { c.report_logs(r).await }
            })
            .await
            .map(|_| ())
        }
    };
    tokio::spawn(logship::ship(send, worker_id, rx, dropped, stop))
}

async fn connect(cfg: &Config) -> Result<Client, Box<dyn std::error::Error + Send + Sync>> {
    let channel = Channel::from_shared(cfg.api_grpc.clone())?
        .connect_timeout(Duration::from_secs(10))
        .timeout(Duration::from_secs(60))
        .tcp_keepalive(Some(Duration::from_secs(30)))
        .connect_lazy();
    let auth = AuthInterceptor::new(cfg.token.as_deref())?;
    Ok(WorkerServiceClient::with_interceptor(channel, auth))
}

async fn shutdown_signal() {
    let ctrl_c = tokio::signal::ctrl_c();
    #[cfg(unix)]
    {
        let mut term =
            match tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate()) {
                Ok(s) => s,
                Err(e) => {
                    tracing::warn!(error = %e, "SIGTERM handler unavailable");
                    let _ = ctrl_c.await;
                    return;
                }
            };
        tokio::select! {
            _ = ctrl_c => {},
            _ = term.recv() => {},
        }
    }
    #[cfg(not(unix))]
    {
        let _ = ctrl_c.await;
    }
}

/// Heartbeats a run until the run finishes or the API asks for cancellation.
async fn heartbeat(
    client: Client,
    run_id: String,
    worker_id: String,
    every: Duration,
    health: Arc<Health>,
    cancel: CancellationToken,
) {
    let mut ticker = tokio::time::interval(every);
    ticker.tick().await;
    for _ in 0..MAX_HEARTBEATS {
        tokio::select! {
            _ = cancel.cancelled() => return,
            _ = ticker.tick() => {
                let req = HeartbeatRequest { run_id: run_id.clone(), worker_id: worker_id.clone() };
                let resp = match resend::once_more(|| {
                    let mut c = client.clone();
                    let req = req.clone();
                    async move { c.heartbeat(Request::new(req)).await }
                })
                .await
                {
                    Ok(resp) => resp,
                    Err(status) => {
                        health.failure(Call::Heartbeat);
                        tracing::warn!(run = %run_id, %status, "heartbeat failed");
                        continue;
                    }
                };
                health.contact();
                if resp.get_ref().directive() == HeartbeatDirective::Cancel {
                    tracing::info!(run = %run_id, "run cancelled by API");
                    cancel.cancel();
                    return;
                }
            }
        }
    }
}

/// One `LeaseRun` long-poll, sent once more when a closing connection refused it unsent.
async fn lease(
    client: &Client,
    cfg: &Config,
) -> Result<tonic::Response<LeaseRunResponse>, tonic::Status> {
    let req = LeaseRunRequest {
        worker_id: cfg.worker_id.clone(),
        wait_seconds: cfg.lease_wait_secs,
    };
    resend::once_more(|| {
        let mut c = client.clone();
        let req = req.clone();
        async move { c.lease_run(Request::new(req)).await }
    })
    .await
}

/// Whether a failed lease is logged as a warning (#177). A stopping API answers `Unavailable`
/// and the next call reaches the new replica, so the first `Unavailable` of a series (the backoff
/// still at its minimum) is logged at info. A second failure in a row, which follows after
/// `MIN_BACKOFF`, warns; so does every other code from the first failure.
fn lease_failure_warns(code: Code, backoff: Duration) -> bool {
    code != Code::Unavailable || backoff > MIN_BACKOFF
}

fn log_lease_failure(status: &Status, backoff: Duration) {
    let backoff_ms = u64::try_from(backoff.as_millis()).unwrap_or(u64::MAX);
    if lease_failure_warns(status.code(), backoff) {
        tracing::warn!(%status, backoff_ms, "lease failed");
    } else {
        tracing::info!(%status, backoff_ms, "lease refused, API stopping or unreachable; retrying");
    }
}

async fn lease_loop(
    cfg: Arc<Config>,
    client: Client,
    health: Arc<Health>,
    stop: CancellationToken,
) {
    let permits = Arc::new(Semaphore::new(cfg.concurrency));
    let mut backoff = MIN_BACKOFF;
    let mut tasks = tokio::task::JoinSet::new();
    while !stop.is_cancelled() {
        let Ok(permit) = permits.clone().acquire_owned().await else {
            break;
        };
        let lease = tokio::select! {
            r = lease(&client, &cfg) => r,
            _ = stop.cancelled() => break,
        };
        match lease {
            Ok(resp) => {
                health.contact();
                backoff = MIN_BACKOFF;
                let Some(run) = resp.into_inner().run else {
                    continue;
                };
                tracing::info!(run = %run.run_id, instance = %run.instance_id, "leased run");
                let cancel = CancellationToken::new();
                let hb = tokio::spawn(heartbeat(
                    client.clone(),
                    run.run_id.clone(),
                    cfg.worker_id.clone(),
                    cfg.heartbeat,
                    health.clone(),
                    cancel.clone(),
                ));
                let exec_client = client.clone();
                let cfg2 = cfg.clone();
                let health2 = health.clone();
                tasks.spawn(async move {
                    let _permit = permit;
                    tokio::select! {
                        () = executor::execute(exec_client, run, cfg2.arr_timeout, &cfg2.worker_id, &health2) => {},
                        () = cancel.cancelled() => tracing::info!("run execution aborted"),
                    }
                    cancel.cancel();
                    hb.abort();
                });
            }
            Err(status) => {
                health.failure(Call::LeaseRun);
                log_lease_failure(&status, backoff);
                tokio::select! {
                    _ = tokio::time::sleep(backoff) => {},
                    _ = stop.cancelled() => break,
                }
                backoff = backoff.saturating_mul(2).min(MAX_BACKOFF);
            }
        }
    }
    tracing::info!(in_flight = tasks.len(), "draining");
    while tasks.join_next().await.is_some() {}
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    // `snatch-worker healthcheck` probes a running worker instead of starting one (container
    // health check in a distroless image).
    if std::env::args().nth(1).as_deref() == Some(probe::COMMAND) {
        init_tracing();
        let cfg = Config::from_env()?;
        if let Err(e) = probe::check(probe::target(cfg.http_addr), probe::TIMEOUT).await {
            tracing::error!(error = %e, "healthcheck failed");
            std::process::exit(1);
        }
        return Ok(());
    }
    let (log_rx, log_dropped) = init_shipped_tracing();
    let cfg = Arc::new(Config::from_env()?);
    tracing::info!(api = %cfg.api_grpc, worker = %cfg.worker_id, concurrency = cfg.concurrency, "snatch-worker starting");
    let client = connect(&cfg).await?;
    let ship_stop = CancellationToken::new();
    let shipper = spawn_shipper(
        &client,
        cfg.worker_id.clone(),
        log_rx,
        log_dropped,
        ship_stop.clone(),
    );
    let health = Arc::new(Health::new(cfg.stale));
    let listener = health::bind(cfg.http_addr).await?;
    tracing::info!(addr = %listener.local_addr()?, stale_secs = cfg.stale.as_secs(), "health listener on /livez, /readyz and /metrics");
    // The listener outlives the drain below: a terminating worker still answers its probe.
    let http_stop = CancellationToken::new();
    let http = tokio::spawn(health::serve(listener, health.clone(), http_stop.clone()));
    let stop = CancellationToken::new();
    let stopper = stop.clone();
    tokio::spawn(async move {
        shutdown_signal().await;
        tracing::info!("shutdown requested");
        stopper.cancel();
    });
    lease_loop(cfg, client, health, stop).await;
    http_stop.cancel();
    if let Err(e) = http.await {
        tracing::warn!(error = %e, "health listener ended abnormally");
    }
    tracing::info!("snatch-worker stopped");
    ship_stop.cancel();
    if tokio::time::timeout(LOG_DRAIN, shipper).await.is_err() {
        tracing::warn!("last log records not sent before the drain deadline");
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn lease_failure_warns_after_the_first_unavailable() {
        let cases = [
            // A replica going away: one Unavailable at the minimum backoff stays below warn.
            ("first unavailable", Code::Unavailable, MIN_BACKOFF, false),
            // The API is still away one backoff later: warn.
            (
                "second unavailable",
                Code::Unavailable,
                MIN_BACKOFF * 2,
                true,
            ),
            (
                "unavailable at the ceiling",
                Code::Unavailable,
                MAX_BACKOFF,
                true,
            ),
            // Every other code warns from the first failure.
            ("first internal", Code::Internal, MIN_BACKOFF, true),
            (
                "first unauthenticated",
                Code::Unauthenticated,
                MIN_BACKOFF,
                true,
            ),
            (
                "first permission denied",
                Code::PermissionDenied,
                MIN_BACKOFF,
                true,
            ),
            ("first cancelled", Code::Cancelled, MIN_BACKOFF, true),
        ];
        for (name, code, backoff, want) in cases {
            assert_eq!(lease_failure_warns(code, backoff), want, "{name}");
        }
    }
}
