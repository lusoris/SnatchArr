// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! `snatch-worker healthcheck`: the container health check.
//!
//! The worker image is distroless (no shell, no curl), so a compose or Docker health check
//! runs the binary itself. It asks the running worker's `/livez` on the loopback address of
//! `SNATCH_HTTP_ADDR` and succeeds only on 200, so the container turns unhealthy exactly when
//! the liveness probe of the chart would restart it.

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr, SocketAddr};
use std::time::Duration;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;

/// The first argument that selects the probe instead of the worker.
pub const COMMAND: &str = "healthcheck";

/// Bound on one probe (HISS-02); compose runs the next one after its own interval.
pub const TIMEOUT: Duration = Duration::from_secs(3);

/// Bytes of the answer read before the connection is dropped.
const MAX_ANSWER: u64 = 4096;

const REQUEST: &[u8] = b"GET /livez HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n";

/// Why a probe failed.
#[derive(Debug, thiserror::Error)]
pub enum ProbeError {
    #[error("no answer from {addr} within {timeout:?}")]
    Timeout { addr: SocketAddr, timeout: Duration },
    #[error("ask {addr}: {source}")]
    Io {
        addr: SocketAddr,
        source: std::io::Error,
    },
    #[error("{addr}/livez answered {status:?}")]
    NotLive { addr: SocketAddr, status: String },
}

/// The address to probe for a listen address: a wildcard host becomes the loopback address of
/// the same family.
pub fn target(listen: SocketAddr) -> SocketAddr {
    let ip = match listen.ip() {
        IpAddr::V4(v4) if v4.is_unspecified() => IpAddr::V4(Ipv4Addr::LOCALHOST),
        IpAddr::V6(v6) if v6.is_unspecified() => IpAddr::V6(Ipv6Addr::LOCALHOST),
        ip => ip,
    };
    SocketAddr::new(ip, listen.port())
}

/// Asks `addr` for `/livez` once and fails unless the answer is 200 within `timeout`.
pub async fn check(addr: SocketAddr, timeout: Duration) -> Result<(), ProbeError> {
    tokio::time::timeout(timeout, ask(addr))
        .await
        .map_err(|_| ProbeError::Timeout { addr, timeout })?
}

async fn ask(addr: SocketAddr) -> Result<(), ProbeError> {
    let io = |source| ProbeError::Io { addr, source };
    let mut stream = TcpStream::connect(addr).await.map_err(io)?;
    stream.write_all(REQUEST).await.map_err(io)?;
    let mut answer = Vec::new();
    stream
        .take(MAX_ANSWER)
        .read_to_end(&mut answer)
        .await
        .map_err(io)?;
    let text = String::from_utf8_lossy(&answer);
    let status = text.lines().next().unwrap_or_default();
    if status.starts_with("HTTP/1.1 200 ") {
        Ok(())
    } else {
        Err(ProbeError::NotLive {
            addr,
            status: status.to_owned(),
        })
    }
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use tokio::net::TcpListener;
    use tokio_util::sync::CancellationToken;

    use super::*;
    use crate::health::{self, Health};

    #[test]
    fn target_replaces_only_wildcards() {
        let cases = [
            ("0.0.0.0:8081", "127.0.0.1:8081"),
            ("[::]:8081", "[::1]:8081"),
            ("10.0.0.5:8081", "10.0.0.5:8081"),
            ("127.0.0.1:65535", "127.0.0.1:65535"),
        ];
        for (listen, want) in cases {
            let got = target(listen.parse().expect("listen"));
            assert_eq!(got, want.parse::<SocketAddr>().expect("want"), "{listen}");
        }
    }

    async fn worker(stale: Duration) -> (SocketAddr, CancellationToken) {
        let listener = health::bind("127.0.0.1:0".parse().expect("addr"))
            .await
            .expect("bind");
        let addr = listener.local_addr().expect("addr");
        let stop = CancellationToken::new();
        tokio::spawn(health::serve(
            listener,
            Arc::new(Health::new(stale)),
            stop.clone(),
        ));
        (addr, stop)
    }

    #[tokio::test]
    async fn live_worker_passes() {
        let (addr, stop) = worker(Duration::from_secs(60)).await;
        check(addr, TIMEOUT).await.expect("live worker");
        stop.cancel();
    }

    #[tokio::test]
    async fn stale_worker_fails() {
        // A zero stale limit makes /livez answer 503 at once.
        let (addr, stop) = worker(Duration::ZERO).await;
        tokio::time::sleep(Duration::from_millis(5)).await;
        let err = check(addr, TIMEOUT).await.expect_err("stale worker");
        assert!(
            matches!(&err, ProbeError::NotLive { status, .. } if status.starts_with("HTTP/1.1 503")),
            "{err}"
        );
        stop.cancel();
    }

    #[tokio::test]
    async fn closed_port_fails() {
        let listener = TcpListener::bind("127.0.0.1:0").await.expect("bind");
        let addr = listener.local_addr().expect("addr");
        drop(listener);
        let err = check(addr, TIMEOUT).await.expect_err("closed port");
        assert!(matches!(err, ProbeError::Io { .. }), "{err}");
    }

    #[tokio::test]
    async fn silent_listener_times_out() {
        // Accepts the connection, never answers: the probe gives up at its bound.
        let listener = TcpListener::bind("127.0.0.1:0").await.expect("bind");
        let addr = listener.local_addr().expect("addr");
        let hold = tokio::spawn(async move {
            let conn = listener.accept().await;
            tokio::time::sleep(Duration::from_secs(5)).await;
            drop(conn);
        });
        let err = check(addr, Duration::from_millis(200))
            .await
            .expect_err("silent listener");
        assert!(matches!(err, ProbeError::Timeout { .. }), "{err}");
        hold.abort();
    }
}
