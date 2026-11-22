// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Sends a unary call once more when the connection it was queued on closed before the call
//! left the worker; the second attempt waits a random time up to `RESEND_WAIT`. The API rotates
//! gRPC connections (`max_connection_age`); a call queued on the old connection in the moment it
//! closes is refused by hyper with a cancel ("connection was not ready", "connection closed")
//! before any byte reaches the API, so sending it again on the next connection is safe for every
//! call. The soak (`make e2e-soak`, C14-01) counted these as failed API calls.
//! [`stream_once_more`] opens a client stream again the same way, on the same receiver, so no
//! event emitted meanwhile is lost; an attempt that took an item is not reopened and the first
//! refusal is returned.

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

/// Upper bound on the random wait before the second attempt (HISS-02): spreads workers refused
/// in the same connection rotation so they do not retry in lockstep.
const RESEND_WAIT: Duration = Duration::from_millis(250);

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

/// Runs `call`, and runs it once more when the first attempt was never sent; the second attempt
/// waits a random time up to `RESEND_WAIT`. Any other outcome, and the outcome of the second
/// attempt, is returned as is.
pub async fn once_more<T, F, Fut>(mut call: F) -> Result<T, Status>
where
    F: FnMut() -> Fut,
    Fut: Future<Output = Result<T, Status>>,
{
    match call().await {
        Err(status) if unsent(&status) => {
            tracing::debug!(%status, "call not sent on a closing connection; sending it again");
            tokio::time::sleep(crate::backoff::jitter(RESEND_WAIT)).await;
            call().await
        }
        other => other,
    }
}

/// A request stream lent to one call attempt that hands its receiver back when the attempt
/// drops it. An attempt that took an item is not reopened and the first refusal is returned.
pub struct Lent<T> {
    rx: Option<mpsc::Receiver<T>>,
    back: Option<oneshot::Sender<mpsc::Receiver<T>>>,
    /// Set once `poll_next` handed an item to the attempt.
    taken: bool,
}

impl<T> Stream for Lent<T> {
    type Item = T;

    fn poll_next(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Option<Self::Item>> {
        let poll = match self.rx.as_mut() {
            Some(rx) => rx.poll_recv(cx),
            None => Poll::Ready(None),
        };
        if matches!(poll, Poll::Ready(Some(_))) {
            self.taken = true;
        }
        poll
    }
}

/// Hands the receiver back only when no item was taken: taken items cannot be replayed, so the
/// caller must not reopen the stream.
impl<T> Drop for Lent<T> {
    fn drop(&mut self) {
        if !self.taken
            && let (Some(rx), Some(back)) = (self.rx.take(), self.back.take())
        {
            let _ = back.send(rx);
        }
    }
}

/// Runs `call` with `rx` lent as a request stream, and opens it once more on the same receiver
/// when the first attempt was never sent and dropped its stream within `RETURN_WAIT`; the second
/// attempt waits a random time up to `RESEND_WAIT`. Any other outcome, and the outcome of the
/// second attempt, is returned as is. An attempt that took an item is not reopened and the first
/// refusal is returned.
pub async fn stream_once_more<T, R, F, Fut>(rx: mpsc::Receiver<T>, mut call: F) -> Result<R, Status>
where
    F: FnMut(Lent<T>) -> Fut,
    Fut: Future<Output = Result<R, Status>>,
{
    let (back_tx, back_rx) = oneshot::channel();
    let first = Lent {
        rx: Some(rx),
        back: Some(back_tx),
        taken: false,
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
            tokio::time::sleep(crate::backoff::jitter(RESEND_WAIT)).await;
            call(Lent {
                rx: Some(rx),
                back: None,
                taken: false,
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

    /// Runs `once_more` over `answers`, one per attempt; returns the result and the start of each
    /// attempt.
    async fn run_timed(
        answers: Vec<Result<u32, Status>>,
    ) -> (Result<u32, Status>, Vec<tokio::time::Instant>) {
        let mut starts = Vec::new();
        let result = once_more(|| {
            let n = starts.len();
            starts.push(tokio::time::Instant::now());
            let answer = answers.get(n).cloned().expect("an answer per attempt");
            async move { answer }
        })
        .await;
        (result, starts)
    }

    /// Runs `once_more` over `answers`, one per attempt; returns the result and the attempts.
    async fn run(answers: Vec<Result<u32, Status>>) -> (Result<u32, Status>, u32) {
        let (result, starts) = run_timed(answers).await;
        (result, u32::try_from(starts.len()).expect("attempts"))
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
    async fn stream_once_more_keeps_a_polled_stream_closed() {
        let status = unsent_status().await;
        tokio::time::pause();
        let start = tokio::time::Instant::now();
        let (tx, rx) = mpsc::channel::<u32>(4);
        tx.send(1).await.expect("first item");
        tx.send(2).await.expect("second item");
        let attempts = AtomicU32::new(0);
        let result = stream_once_more(rx, |mut events| {
            let n = attempts.fetch_add(1, Ordering::SeqCst);
            let status = status.clone();
            async move {
                if n == 0 {
                    assert_eq!(events.next().await, Some(1));
                }
                drop(events);
                Err::<u32, _>(status)
            }
        })
        .await;
        assert!(unsent(&result.expect_err("polled stream not reopened")));
        assert_eq!(attempts.load(Ordering::SeqCst), 1);
        assert!(start.elapsed() < RETURN_WAIT);
    }

    #[tokio::test]
    async fn lent_hands_its_receiver_back_on_drop() {
        let (tx, rx) = mpsc::channel::<u32>(2);
        tx.send(42).await.expect("send before lend");
        let (back_tx, back_rx) = oneshot::channel();
        let lent = Lent {
            rx: Some(rx),
            back: Some(back_tx),
            taken: false,
        };
        drop(lent);
        let mut returned = back_rx.await.expect("receiver handed back");
        assert_eq!(returned.recv().await, Some(42));
    }

    #[tokio::test]
    async fn lent_keeps_its_receiver_once_an_item_was_taken() {
        let (tx, rx) = mpsc::channel::<u32>(2);
        tx.send(1).await.expect("first item");
        tx.send(2).await.expect("second item");
        let (back_tx, back_rx) = oneshot::channel();
        let mut lent = Lent {
            rx: Some(rx),
            back: Some(back_tx),
            taken: false,
        };
        assert_eq!(lent.next().await, Some(1));
        drop(lent);
        assert!(back_rx.await.is_err(), "receiver not handed back");
    }

    #[tokio::test]
    async fn lent_hands_back_after_an_empty_poll() {
        let (tx, rx) = mpsc::channel::<u32>(2);
        let (back_tx, back_rx) = oneshot::channel();
        let mut lent = Lent {
            rx: Some(rx),
            back: Some(back_tx),
            taken: false,
        };
        let poll = tokio::time::timeout(Duration::from_millis(10), lent.next()).await;
        assert!(poll.is_err(), "empty open channel stays pending");
        drop(lent);
        let mut returned = back_rx.await.expect("receiver handed back");
        tx.send(99).await.expect("send after return");
        assert_eq!(returned.recv().await, Some(99));
    }

    // The clock starts paused so `start` sits on the timer tick and the upper bound holds.
    #[tokio::test(start_paused = true)]
    async fn once_more_waits_before_the_second_attempt() {
        let status = unsent_status().await;
        let start = tokio::time::Instant::now();
        let (result, starts) = run_timed(vec![Err(status), Ok(7)]).await;
        let elapsed = start.elapsed();
        assert_eq!(result.expect("second attempt answers"), 7);
        assert_eq!(starts.len(), 2);
        let [first, second]: [_; 2] = starts.try_into().expect("two attempts");
        assert_eq!(first.checked_duration_since(start), Some(Duration::ZERO));
        let before_second = second
            .checked_duration_since(start)
            .expect("second attempt not before start");
        assert!(
            before_second >= RESEND_WAIT / 2 && before_second <= RESEND_WAIT,
            "once_more second attempt at {before_second:?} outside {:?}..={RESEND_WAIT:?}",
            RESEND_WAIT / 2
        );
        assert!(
            elapsed >= RESEND_WAIT / 2 && elapsed <= RESEND_WAIT,
            "elapsed {elapsed:?} outside {:?}..={RESEND_WAIT:?}",
            RESEND_WAIT / 2
        );
    }

    #[tokio::test(start_paused = true)]
    async fn stream_once_more_waits_before_the_second_attempt() {
        let status = unsent_status().await;
        let (tx, rx) = mpsc::channel::<u32>(4);
        tx.send(1).await.expect("first item");
        tx.send(2).await.expect("second item");
        drop(tx);
        let start = tokio::time::Instant::now();
        let mut starts = Vec::new();
        let result = stream_once_more(rx, |events| {
            let n = starts.len();
            starts.push(tokio::time::Instant::now());
            let status = status.clone();
            async move {
                if n == 0 {
                    drop(events);
                    return Err(status);
                }
                Ok(events.collect::<Vec<_>>().await)
            }
        })
        .await;
        let elapsed = start.elapsed();
        assert_eq!(result.expect("second attempt answers"), vec![1, 2]);
        assert_eq!(starts.len(), 2);
        let [first, second]: [_; 2] = starts.try_into().expect("two attempts");
        assert_eq!(first.checked_duration_since(start), Some(Duration::ZERO));
        let before_second = second
            .checked_duration_since(start)
            .expect("second attempt not before start");
        assert!(
            before_second >= RESEND_WAIT / 2 && before_second <= RESEND_WAIT,
            "stream second attempt at {before_second:?} outside {:?}..={RESEND_WAIT:?}",
            RESEND_WAIT / 2
        );
        assert!(
            elapsed >= RESEND_WAIT / 2 && elapsed <= RESEND_WAIT,
            "elapsed {elapsed:?} outside {:?}..={RESEND_WAIT:?}",
            RESEND_WAIT / 2
        );
    }

    #[tokio::test(start_paused = true)]
    async fn once_more_does_not_wait_after_an_answer() {
        let status = Status::unavailable("down");
        let start = tokio::time::Instant::now();
        let (result, attempts) = run(vec![Err(status)]).await;
        assert_eq!(result.expect_err("refused").code(), Code::Unavailable);
        assert_eq!(attempts, 1);
        assert_eq!(start.elapsed(), Duration::ZERO);
    }
}
