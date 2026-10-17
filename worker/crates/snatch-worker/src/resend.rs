// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Sends a unary call once more when the connection it was queued on closed before the call
//! left the worker. The API rotates gRPC connections (`max_connection_age`); a call queued on
//! the old connection in the moment it closes is refused by hyper with a cancel ("connection was
//! not ready", "connection closed") before any byte reaches the API, so sending it again on the
//! next connection is safe for every call. The soak (`make e2e-soak`, C14-01) counted these as
//! failed API calls. The `ReportEvents` stream is not covered: its request stream is consumed.

use std::future::Future;

use tonic::{Code, Status};

/// Upper bound on the error sources walked to find the hyper error (HISS-02).
const MAX_SOURCE_DEPTH: usize = 8;

/// Whether `status` is hyper's refusal of a request it never handed to a connection. hyper
/// reports only that case as canceled on the client side; tonic maps it to `Cancelled` and keeps
/// the hyper error as the source. A `Cancelled` the API sent has no hyper source.
pub fn unsent(status: &Status) -> bool {
    if status.code() != Code::Cancelled {
        return false;
    }
    let mut source = std::error::Error::source(status);
    for _ in 0..MAX_SOURCE_DEPTH {
        let Some(err) = source else {
            return false;
        };
        if let Some(hyper_err) = err.downcast_ref::<hyper::Error>() {
            return hyper_err.is_canceled();
        }
        source = err.source();
    }
    false
}

/// Runs `call`, and runs it once more when the first attempt was never sent. Any other
/// outcome, and the outcome of the second attempt, is returned as is.
pub async fn once_more<T, F, Fut>(mut call: F) -> Result<T, Status>
where
    F: FnMut() -> Fut,
    Fut: Future<Output = Result<T, Status>>,
{
    match call().await {
        Err(status) if unsent(&status) => {
            tracing::debug!(%status, "call not sent on a closing connection; sending it again");
            call().await
        }
        other => other,
    }
}

#[cfg(test)]
mod tests {
    use std::sync::atomic::{AtomicU32, Ordering};

    use bytes::Bytes;
    use http_body_util::Empty;
    use hyper_util::rt::{TokioExecutor, TokioIo};

    use super::*;

    /// A real hyper refusal of an unsent request: an HTTP/2 connection whose peer went away
    /// before the request was queued, as at a connection rotation.
    async fn unsent_status() -> Status {
        let (client_io, server_io) = tokio::io::duplex(1 << 16);
        let (mut sender, conn) =
            hyper::client::conn::http2::handshake(TokioExecutor::new(), TokioIo::new(client_io))
                .await
                .expect("handshake");
        drop(server_io);
        let _ = conn.await;
        let req = hyper::Request::builder()
            .uri("http://api/snatcharr.v1.WorkerService/LeaseRun")
            .body(Empty::<Bytes>::new())
            .expect("request");
        let err = sender
            .send_request(req)
            .await
            .expect_err("the connection is closed");
        assert!(err.is_canceled(), "want a cancel, got {err}");
        Status::from_error(Box::new(err))
    }

    #[tokio::test]
    async fn unsent_finds_the_hyper_cancel() {
        let status = unsent_status().await;
        assert_eq!(status.code(), Code::Cancelled);
        assert!(unsent(&status));
    }

    #[test]
    fn unsent_refuses_answers_from_the_api() {
        // Cancelled without a hyper source: the API (or a proxy) answered with it.
        assert!(!unsent(&Status::cancelled("context canceled")));
        assert!(!unsent(&Status::unavailable("connection refused")));
        assert!(!unsent(&Status::ok("")));
    }

    /// Runs `once_more` over `answers`, one per attempt; returns the result and the attempts.
    async fn run(answers: Vec<Result<u32, Status>>) -> (Result<u32, Status>, u32) {
        let attempts = AtomicU32::new(0);
        let result = once_more(|| {
            let n = attempts.fetch_add(1, Ordering::SeqCst);
            let answer = answers
                .get(usize::try_from(n).expect("index"))
                .cloned()
                .expect("an answer per attempt");
            async move { answer }
        })
        .await;
        (result, attempts.load(Ordering::SeqCst))
    }

    #[tokio::test]
    async fn once_more_resends_an_unsent_call() {
        let (result, attempts) = run(vec![Err(unsent_status().await), Ok(7)]).await;
        assert_eq!(result.expect("second attempt answers"), 7);
        assert_eq!(attempts, 2);
    }

    #[tokio::test]
    async fn once_more_keeps_other_outcomes() {
        let (result, attempts) = run(vec![Ok(3)]).await;
        assert_eq!(result.expect("first attempt answers"), 3);
        assert_eq!(attempts, 1);
        let (result, attempts) = run(vec![Err(Status::unavailable("down"))]).await;
        assert_eq!(result.expect_err("refused").code(), Code::Unavailable);
        assert_eq!(attempts, 1);
    }

    #[tokio::test]
    async fn once_more_resends_only_once() {
        let unsent_twice = vec![Err(unsent_status().await), Err(unsent_status().await)];
        let (result, attempts) = run(unsent_twice).await;
        assert!(unsent(&result.expect_err("second attempt unsent as well")));
        assert_eq!(attempts, 2);
    }
}
