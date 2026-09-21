// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! SnatchArr hunt-worker: leases hunt runs from the API over gRPC and executes them
//! against *arr instances (ADR-0001, ADR-0002). Stateless; safe to scale horizontally.

#![cfg_attr(test, allow(clippy::unwrap_used, clippy::expect_used))]

mod auth;
mod config;
mod executor;

use std::sync::Arc;
use std::time::Duration;

use snatch_proto::worker_service_client::WorkerServiceClient;
use snatch_proto::{HeartbeatDirective, HeartbeatRequest, LeaseRunRequest};
use tokio::sync::Semaphore;
use tokio_util::sync::CancellationToken;
use tonic::Request;
use tonic::transport::Channel;
use tracing_subscriber::EnvFilter;

use crate::auth::AuthInterceptor;
use crate::config::Config;
use crate::executor::Client;

/// Upper bound on heartbeats per run (HISS-02): 2^20 * 5 s is ~60 days.
const MAX_HEARTBEATS: u32 = 1 << 20;

/// Backoff bounds for API connectivity problems (HISS-02).
const MIN_BACKOFF: Duration = Duration::from_secs(1);
const MAX_BACKOFF: Duration = Duration::from_secs(30);

fn init_tracing() {
    let filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"));
    tracing_subscriber::fmt()
        .json()
        .with_env_filter(filter)
        .with_target(false)
        .init();
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
    mut client: Client,
    run_id: String,
    worker_id: String,
    every: Duration,
    cancel: CancellationToken,
) {
    let mut ticker = tokio::time::interval(every);
    ticker.tick().await;
    for _ in 0..MAX_HEARTBEATS {
        tokio::select! {
            _ = cancel.cancelled() => return,
            _ = ticker.tick() => {
                let req = HeartbeatRequest { run_id: run_id.clone(), worker_id: worker_id.clone() };
                match client.heartbeat(Request::new(req)).await {
                    Ok(resp) if resp.get_ref().directive() == HeartbeatDirective::Cancel => {
                        tracing::info!(run = %run_id, "run cancelled by API");
                        cancel.cancel();
                        return;
                    }
                    Ok(_) => {}
                    Err(status) => tracing::warn!(run = %run_id, %status, "heartbeat failed"),
                }
            }
        }
    }
}

async fn lease_loop(cfg: Arc<Config>, mut client: Client, stop: CancellationToken) {
    let permits = Arc::new(Semaphore::new(cfg.concurrency));
    let mut backoff = MIN_BACKOFF;
    let mut tasks = tokio::task::JoinSet::new();
    while !stop.is_cancelled() {
        let Ok(permit) = permits.clone().acquire_owned().await else {
            break;
        };
        let req = LeaseRunRequest {
            worker_id: cfg.worker_id.clone(),
            wait_seconds: cfg.lease_wait_secs,
        };
        let lease = tokio::select! {
            r = client.lease_run(Request::new(req)) => r,
            _ = stop.cancelled() => break,
        };
        match lease {
            Ok(resp) => {
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
                    cancel.clone(),
                ));
                let exec_client = client.clone();
                let cfg2 = cfg.clone();
                tasks.spawn(async move {
                    let _permit = permit;
                    tokio::select! {
                        () = executor::execute(exec_client, run, cfg2.arr_timeout, &cfg2.worker_id) => {},
                        () = cancel.cancelled() => tracing::info!("run execution aborted"),
                    }
                    cancel.cancel();
                    hb.abort();
                });
            }
            Err(status) => {
                tracing::warn!(%status, backoff_ms = u64::try_from(backoff.as_millis()).unwrap_or(u64::MAX), "lease failed");
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
    init_tracing();
    let cfg = Arc::new(Config::from_env()?);
    tracing::info!(api = %cfg.api_grpc, worker = %cfg.worker_id, concurrency = cfg.concurrency, "snatch-worker starting");
    let client = connect(&cfg).await?;
    let stop = CancellationToken::new();
    let stopper = stop.clone();
    tokio::spawn(async move {
        shutdown_signal().await;
        tracing::info!("shutdown requested");
        stopper.cancel();
    });
    lease_loop(cfg, client, stop).await;
    tracing::info!("snatch-worker stopped");
    Ok(())
}
