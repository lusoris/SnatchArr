// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Completion (gap audit A1-08, A1-09): CompleteRun tells the API how many events it did not
//! confirm, and a transient refusal of CompleteRun is retried within a bound.

use super::*;
use crate::health::Call;
use std::sync::atomic::Ordering;
use tonic::Code;

/// Runs the default (failing) run against `api`; returns the API and the run's health.
async fn run_against(
    api: FakeApi,
    retry: crate::executor::Retry,
) -> Result<(FakeApi, crate::health::Health), Box<dyn std::error::Error>> {
    let sonarr = MockServer::start().await;
    let (client, _) = start_fake_api(api.clone()).await?;
    let health = run_with_retry(client, default_run(&sonarr.uri()), retry).await;
    Ok((api, health))
}

/// The `events_unconfirmed` the only CompleteRun call carried, and the events the API saw.
async fn unconfirmed(api: &FakeApi) -> Result<(u32, usize), Box<dyn std::error::Error>> {
    let complete = api.completed_requests.lock().await;
    assert_eq!(complete.len(), 1, "one CompleteRun call");
    let sent = some(complete.first(), "a CompleteRun call")?.events_unconfirmed;
    Ok((sent, api.reported_events.lock().await.len()))
}

#[tokio::test]
async fn accepted_events_leave_none_unconfirmed() -> TestResult {
    let (api, _) = run_against(FakeApi::default(), SHORT_RETRY).await?;
    let (sent, reported) = unconfirmed(&api).await?;
    assert!(reported >= 2, "the run emits at least start and finish");
    assert_eq!(sent, 0);
    Ok(())
}

#[tokio::test]
async fn refused_event_stream_leaves_every_event_unconfirmed() -> TestResult {
    let api = FakeApi {
        fail_report: true,
        ..FakeApi::default()
    };
    let (api, _) = run_against(api, SHORT_RETRY).await?;
    let (sent, reported) = unconfirmed(&api).await?;
    assert!(
        reported >= 2,
        "the fake received the events before refusing"
    );
    assert_eq!(usize::try_from(sent)?, reported);
    Ok(())
}

#[tokio::test]
async fn events_after_a_closed_stream_count_as_unconfirmed() -> TestResult {
    let (whole, _) = run_against(FakeApi::default(), SHORT_RETRY).await?;
    let emitted = whole.reported_events.lock().await.len();
    let api = FakeApi {
        report_cut_after: Some(1),
        ..FakeApi::default()
    };
    let (api, _) = run_against(api, SHORT_RETRY).await?;
    let (sent, reported) = unconfirmed(&api).await?;
    assert_eq!(reported, 1, "the stream closed after one event");
    assert_eq!(usize::try_from(sent)?, emitted.saturating_sub(1));
    Ok(())
}

#[tokio::test]
async fn transient_complete_refusal_is_retried() -> TestResult {
    let api = FakeApi {
        complete_refusals: vec![Code::Unavailable, Code::DeadlineExceeded],
        ..FakeApi::default()
    };
    let (api, health) = run_against(api, SHORT_RETRY).await?;
    assert_eq!(api.complete_calls.load(Ordering::Relaxed), 3);
    assert_eq!(api.completed_requests.lock().await.len(), 1);
    assert_eq!(health.failures(Call::CompleteRun), 2);
    Ok(())
}

#[tokio::test]
async fn each_transient_code_is_retried() -> TestResult {
    for code in [
        Code::Unavailable,
        Code::DeadlineExceeded,
        Code::ResourceExhausted,
        Code::Aborted,
    ] {
        let api = FakeApi {
            complete_refusals: vec![code],
            ..FakeApi::default()
        };
        let (api, _) = run_against(api, SHORT_RETRY).await?;
        assert_eq!(api.completed_requests.lock().await.len(), 1, "{code:?}");
    }
    Ok(())
}

#[tokio::test]
async fn permanent_complete_refusal_is_not_retried() -> TestResult {
    for code in [
        Code::InvalidArgument,
        Code::FailedPrecondition,
        Code::Unauthenticated,
        Code::Internal,
    ] {
        let api = FakeApi {
            complete_refusals: vec![code; 3],
            ..FakeApi::default()
        };
        let (api, health) = run_against(api, SHORT_RETRY).await?;
        assert_eq!(api.complete_calls.load(Ordering::Relaxed), 1, "{code:?}");
        assert_eq!(health.failures(Call::CompleteRun), 1, "{code:?}");
    }
    Ok(())
}

#[tokio::test]
async fn retry_stops_at_its_attempt_bound() -> TestResult {
    let api = FakeApi {
        complete_refusals: vec![Code::Unavailable; 5],
        ..FakeApi::default()
    };
    let (api, _) = run_against(api, SHORT_RETRY).await?;
    assert_eq!(api.complete_calls.load(Ordering::Relaxed), 3);
    assert!(api.completed_requests.lock().await.is_empty());
    Ok(())
}

#[tokio::test]
async fn zero_attempts_still_calls_once() -> TestResult {
    let api = FakeApi {
        complete_refusals: vec![Code::Unavailable],
        ..FakeApi::default()
    };
    let none = crate::executor::Retry {
        attempts: 0,
        backoff: Duration::from_millis(5),
    };
    let (api, health) = run_against(api, none).await?;
    assert_eq!(api.complete_calls.load(Ordering::Relaxed), 1);
    assert_eq!(health.failures(Call::CompleteRun), 1);
    Ok(())
}

#[tokio::test]
async fn last_refusal_does_not_wait() -> TestResult {
    let api = FakeApi {
        complete_refusals: vec![Code::Unavailable],
        ..FakeApi::default()
    };
    // A wait after the only attempt would take 15 s to 30 s.
    let one = crate::executor::Retry {
        attempts: 1,
        backoff: Duration::from_secs(30),
    };
    let (api, _) = tokio::time::timeout(Duration::from_secs(10), run_against(api, one)).await??;
    assert_eq!(api.complete_calls.load(Ordering::Relaxed), 1);
    Ok(())
}

#[test]
fn retry_wait_doubles_from_the_backoff() {
    let r = crate::executor::Retry {
        attempts: 3,
        backoff: Duration::from_millis(100),
    };
    assert_eq!(
        r.wait(0),
        Duration::from_millis(100),
        "attempt 0 reads as 1"
    );
    assert_eq!(r.wait(1), Duration::from_millis(100));
    assert_eq!(r.wait(2), Duration::from_millis(200));
    assert_eq!(r.wait(3), Duration::from_millis(400));
    let most = Duration::from_millis(100).saturating_mul(u32::MAX);
    assert_eq!(r.wait(u32::MAX), most, "the factor saturates");
}

#[test]
fn jitter_stays_between_half_and_whole() {
    assert_eq!(jitter(Duration::ZERO), Duration::ZERO);
    for d in [Duration::from_millis(1), Duration::from_secs(1)] {
        let mut below_whole = false;
        for _ in 0..200 {
            let j = jitter(d);
            assert!(j >= d / 2 && j <= d, "{j:?} outside {d:?}");
            below_whole |= j < d;
        }
        assert!(
            below_whole,
            "jitter of {d:?} never moved off the whole wait"
        );
    }
}
