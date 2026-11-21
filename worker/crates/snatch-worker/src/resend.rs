// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Sends a unary call once more when the connection it was queued on closed before the call
//! left the worker. The API rotates gRPC connections (`max_connection_age`); a call queued on
//! the old connection in the moment it closes is refused by hyper with a cancel ("connection was
//! not ready", "connection closed") before any byte reaches the API, so sending it again on the
//! next connection is safe for every call. The soak (`make e2e-soak`, C14-01) counted these as
//! failed API calls. [`stream_once_more`] opens a client stream again the same way, on the same
//! receiver, so no event emitted meanwhile is lost.

use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};
use std::time::Duration;

use tokio::sync::{mpsc, oneshot};
use tokio_stream::Stream;
use tonic::{Code, Status};

/// Upper bound on the error sources walked to find the hyper error (HISS-02).
const MAX_SOURCE_DEPTH: usize = 8;

/// Upper bound on waiting for a dropped call attempt to hand its receiver back: hyper drops an
/// unsent body before it answers, so this is a HISS-02 bound.
const RETURN_WAIT: Duration = Duration::from_secs(1);

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

/// A request stream lent to one call attempt that hands its receiver back when the attempt
/// drops it.
pub struct Lent<T> {
    rx: Option<mpsc::Receiver<T>>,
    back: Option<oneshot::Sender<mpsc::Receiver<T>>>,
}

impl<T> Stream for Lent<T> {
    type Item = T;

    fn poll_next(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Option<Self::Item>> {
        match self.rx.as_mut() {
            Some(rx) => rx.poll_recv(cx),
            None => Poll::Ready(None),
        }
    }
}

impl<T> Drop for Lent<T> {
    fn drop(&mut self) {
        if let (Some(rx), Some(back)) = (self.rx.take(), self.back.take()) {
            let _ = back.send(rx);
        }
    }
}

/// Runs `call` with `rx` lent as a request stream, and opens it once more on the same receiver
/// when the first attempt was never sent and dropped its stream within `RETURN_WAIT`. Any other
/// outcome, and the outcome of the second attempt, is returned as is.
pub async fn stream_once_more<T, R, F, Fut>(rx: mpsc::Receiver<T>, mut call: F) -> Result<R, Status>
where
    F: FnMut(Lent<T>) -> Fut,
    Fut: Future<Output = Result<R, Status>>,
{
    let (back_tx, back_rx) = oneshot::channel();
    let first = Lent {
        rx: Some(rx),
        back: Some(back_tx),
    };
    match call(first).await {
        Err(status) if unsent(&status) => {
            let Ok(Ok(rx)) = tokio::time::timeout(RETURN_WAIT, back_rx).await else {
                return Err(status);
            };
            tracing::debug!(
                %status,
                "event stream not sent on a closing connection; opening it again"
            );
            call(Lent {
                rx: Some(rx),
                back: None,
            })
            .await
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
    use tokio_stream::StreamExt;

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

    #[tokio::test]
    async fn stream_once_more_reopens_an_unsent_stream() {
        let (tx, rx) = mpsc::channel::<u32>(8);
        tx.send(1).await.expect("first item");
        tx.send(2).await.expect("second item");
        let (refused_tx, refused_rx) = oneshot::channel::<()>();
        let producer = tokio::spawn(async move {
            refused_rx.await.expect("first attempt refused");
            let sent = tx.send(3).await.is_ok();
            drop(tx);
            sent
        });
        let mut signal = Some(refused_tx);
        let attempts = AtomicU32::new(0);
        let result = stream_once_more(rx, |events| {
            let n = attempts.fetch_add(1, Ordering::SeqCst);
            let refused = signal.take();
            async move {
                if n == 0 {
                    drop(events);
                    let status = unsent_status().await;
                    if let Some(sig) = refused {
                        let _ = sig.send(());
                    }
                    return Err(status);
                }
                Ok(events.collect::<Vec<_>>().await)
            }
        })
        .await;
        assert_eq!(result.expect("second attempt answers"), vec![1, 2, 3]);
        assert!(producer.await.expect("producer task"), "third item sent");
        assert_eq!(attempts.load(Ordering::SeqCst), 2);
    }

    #[tokio::test]
    async fn stream_once_more_keeps_other_outcomes() {
        let (_tx, rx) = mpsc::channel::<u32>(1);
        let attempts = AtomicU32::new(0);
        let result = stream_once_more(rx, |events| {
            drop(events);
            attempts.fetch_add(1, Ordering::SeqCst);
            async { Err::<u32, _>(Status::unavailable("down")) }
        })
        .await;
        let err = result.expect_err("refused");
        assert_eq!(err.code(), Code::Unavailable);
        assert_eq!(err.message(), "down");
        assert_eq!(attempts.load(Ordering::SeqCst), 1);

        let (_tx, rx) = mpsc::channel::<u32>(1);
        let attempts = AtomicU32::new(0);
        let result = stream_once_more(rx, |_events| {
            attempts.fetch_add(1, Ordering::SeqCst);
            async { Ok::<u32, Status>(5) }
        })
        .await;
        assert_eq!(result.expect("first attempt answers"), 5);
        assert_eq!(attempts.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn stream_once_more_reopens_only_once() {
        let (_tx, rx) = mpsc::channel::<u32>(1);
        let attempts = AtomicU32::new(0);
        let result = stream_once_more(rx, |events| {
            drop(events);
            attempts.fetch_add(1, Ordering::SeqCst);
            async { Err::<u32, _>(unsent_status().await) }
        })
        .await;
        assert!(unsent(&result.expect_err("second attempt unsent as well")));
        assert_eq!(attempts.load(Ordering::SeqCst), 2);
    }

    #[tokio::test]
    async fn stream_once_more_gives_up_when_the_stream_is_kept() {
        let status = unsent_status().await;
        tokio::time::pause();
        let start = tokio::time::Instant::now();
        let (_tx, rx) = mpsc::channel::<u32>(1);
        let mut kept: Option<Lent<u32>> = None;
        let attempts = AtomicU32::new(0);
        let result = stream_once_more(rx, |events| {
            attempts.fetch_add(1, Ordering::SeqCst);
            kept = Some(events);
            let status = status.clone();
            async move { Err::<u32, _>(status) }
        })
        .await;
        assert!(unsent(&result.expect_err("kept stream not reopened")));
        assert_eq!(attempts.load(Ordering::SeqCst), 1);
        assert!(start.elapsed() >= RETURN_WAIT);
        drop(kept);
    }

    #[tokio::test]
    async fn lent_hands_its_receiver_back_on_drop() {
        let (tx, rx) = mpsc::channel::<u32>(2);
        tx.send(42).await.expect("send before lend");
        let (back_tx, back_rx) = oneshot::channel();
        let lent = Lent {
            rx: Some(rx),
            back: Some(back_tx),
        };
        drop(lent);
        let mut returned = back_rx.await.expect("receiver handed back");
        assert_eq!(returned.recv().await, Some(42));
    }
}
