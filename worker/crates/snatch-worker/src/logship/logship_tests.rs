// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Log shipping tests: the layer stays bounded and counts every drop; the shipper batches,
//! retries within its bound and reports what it lost.

use std::sync::Mutex;

use tracing_subscriber::layer::SubscriberExt;

use super::*;

type Calls = Arc<Mutex<Vec<ReportLogsRequest>>>;

/// A `send` that records every request and answers with the next scripted code (Ok past the
/// end of the script).
fn recorder(
    script: Vec<Code>,
) -> (
    Calls,
    impl FnMut(ReportLogsRequest) -> std::future::Ready<Result<(), Status>>,
) {
    let calls: Calls = Arc::new(Mutex::new(Vec::new()));
    let seen = calls.clone();
    let mut script = script.into_iter();
    let send = move |req| {
        // A poisoned lock loses the request; the call-count assertions then fail.
        if let Ok(mut seen) = seen.lock() {
            seen.push(req);
        }
        std::future::ready(match script.next() {
            Some(code) => Err(Status::new(code, "planted")),
            None => Ok(()),
        })
    };
    (calls, send)
}

fn record(n: usize) -> LogRecord {
    LogRecord {
        message: format!("r{n}"),
        ..LogRecord::default()
    }
}

#[test]
fn layer_stays_bounded_and_counts_every_drop_under_a_burst() {
    let (layer, mut rx, dropped) = channel(CAPACITY);
    let subscriber = tracing_subscriber::registry().with(layer);
    tracing::subscriber::with_default(subscriber, || {
        for i in 0..5000 {
            tracing::info!(i, "burst");
        }
    });
    let held = std::iter::from_fn(|| rx.try_recv().ok()).count();
    assert_eq!(held, CAPACITY);
    // 5000 events, 1024 held: 3976 dropped and counted.
    assert_eq!(dropped.get(), 3976);
}

#[test]
fn layer_boundary_capacity_one_keeps_the_first_record() {
    let (layer, mut rx, dropped) = channel(1);
    tracing::subscriber::with_default(tracing_subscriber::registry().with(layer), || {
        tracing::warn!("first");
        tracing::warn!("second");
    });
    assert_eq!(rx.try_recv().unwrap().message, "first");
    assert!(rx.try_recv().is_err());
    assert_eq!(dropped.get(), 1);
}

#[test]
fn layer_copies_level_target_message_and_capped_fields() {
    let (layer, mut rx, _) = channel(4);
    let long = "x".repeat(MAX_VALUE_BYTES + 10);
    tracing::subscriber::with_default(tracing_subscriber::registry().with(layer), || {
        tracing::error!(target: "snatch_worker::executor", run = "r-1", api_key = "planted", detail = %long, "search {} failed", 7);
    });
    let rec = rx.try_recv().unwrap();
    assert_eq!(rec.level, ProtoLevel::Error as i32);
    assert_eq!(rec.target, "snatch_worker::executor");
    assert_eq!(rec.message, "search 7 failed");
    let fields: Vec<(&str, usize)> = rec
        .fields
        .iter()
        .map(|f| (f.key.as_str(), f.value.len()))
        .collect();
    // Redaction is the API's job: the worker ships the field as it is.
    assert_eq!(
        fields,
        [("run", 3), ("api_key", 7), ("detail", MAX_VALUE_BYTES)]
    );
    assert!(rec.ts_unix_ms > 0);
}

#[test]
fn capped_cuts_on_a_character_boundary() {
    assert_eq!(capped("abc".to_owned(), 3), "abc");
    assert_eq!(capped("abcd".to_owned(), 3), "abc");
    // "é" is two bytes; a cut inside it keeps the character out.
    assert_eq!(capped("aé".to_owned(), 2), "a");
}

#[tokio::test(start_paused = true)]
async fn send_batch_retries_a_transient_refusal() {
    let (calls, mut send) = recorder(vec![Code::Unavailable]);
    let dropped = Dropped::default();
    dropped.add(4);
    let sent = send_batch(&mut send, "w1", vec![record(1)], &dropped).await;
    assert_eq!(sent, Sent::Accepted);
    let calls = calls.lock().unwrap();
    assert_eq!(calls.len(), 2);
    let retry = calls.get(1).unwrap();
    assert_eq!((retry.worker_id.as_str(), retry.dropped), ("w1", 4));
    assert_eq!(dropped.get(), 0);
}

#[tokio::test(start_paused = true)]
async fn send_batch_counts_a_lost_batch_exactly() {
    let cases = [
        // Every attempt refused: the bound ends the retries.
        ("transient", vec![Code::Unavailable; 5], ATTEMPTS),
        // A refusal no retry gets past ends at once.
        ("permanent", vec![Code::InvalidArgument], 1),
    ];
    for (name, script, want_calls) in cases {
        let (calls, mut send) = recorder(script);
        let dropped = Dropped::default();
        dropped.add(5);
        let batch = (0..3).map(record).collect();
        let sent = send_batch(&mut send, "w1", batch, &dropped).await;
        assert_eq!(sent, Sent::Lost, "{name}");
        assert_eq!(calls.lock().unwrap().len(), want_calls as usize, "{name}");
        // The three records plus the five dropped before are reported with the next batch.
        assert_eq!(dropped.get(), 8, "{name}");
    }
}

#[tokio::test(start_paused = true)]
async fn send_batch_stops_on_an_api_without_report_logs() {
    let (calls, mut send) = recorder(vec![Code::Unimplemented]);
    let sent = send_batch(&mut send, "w1", vec![record(1)], &Dropped::default()).await;
    assert_eq!(sent, Sent::Unsupported);
    assert_eq!(calls.lock().unwrap().len(), 1);
}

#[tokio::test(start_paused = true)]
async fn ship_sends_a_full_batch_at_once_and_the_rest_on_the_tick() {
    let (tx, rx) = mpsc::channel(CAPACITY);
    let (calls, send) = recorder(Vec::new());
    let dropped = Arc::new(Dropped::default());
    let stop = CancellationToken::new();
    let task = tokio::spawn(ship(
        send,
        "w1".to_owned(),
        rx,
        dropped.clone(),
        stop.clone(),
    ));
    for i in 0..BATCH + 5 {
        tx.try_send(record(i)).unwrap();
    }
    tokio::time::sleep(FLUSH_EVERY / 4).await;
    let sizes =
        |c: &Calls| -> Vec<usize> { c.lock().unwrap().iter().map(|r| r.records.len()).collect() };
    assert_eq!(
        sizes(&calls),
        [BATCH],
        "a full batch leaves before the tick"
    );
    tokio::time::sleep(FLUSH_EVERY).await;
    assert_eq!(sizes(&calls), [BATCH, 5], "the rest leaves on the tick");
    // A drop count alone is reported on the next tick.
    dropped.add(2);
    tokio::time::sleep(FLUSH_EVERY).await;
    let last = calls.lock().unwrap().last().cloned().unwrap();
    assert_eq!((last.records.len(), last.dropped), (0, 2));
    // An idle tick sends nothing.
    tokio::time::sleep(FLUSH_EVERY).await;
    assert_eq!(calls.lock().unwrap().len(), 3);
    stop.cancel();
    task.await.unwrap();
}

#[tokio::test(start_paused = true)]
async fn ship_sends_everything_left_when_stopped() {
    let (tx, rx) = mpsc::channel(CAPACITY);
    let (calls, send) = recorder(Vec::new());
    for i in 0..CAPACITY {
        tx.try_send(record(i)).unwrap();
    }
    let stop = CancellationToken::new();
    stop.cancel();
    ship(
        send,
        "w1".to_owned(),
        rx,
        Arc::new(Dropped::default()),
        stop,
    )
    .await;
    let total: usize = calls.lock().unwrap().iter().map(|r| r.records.len()).sum();
    assert_eq!(total, CAPACITY);
    assert!(
        calls
            .lock()
            .unwrap()
            .iter()
            .all(|r| r.records.len() <= BATCH)
    );
}

#[tokio::test(start_paused = true)]
async fn ship_ends_for_good_on_an_api_without_report_logs() {
    let (tx, rx) = mpsc::channel(CAPACITY);
    let (calls, send) = recorder(vec![Code::Unimplemented]);
    let task = tokio::spawn(ship(
        send,
        "w1".to_owned(),
        rx,
        Arc::new(Dropped::default()),
        CancellationToken::new(),
    ));
    tx.try_send(record(1)).unwrap();
    tokio::time::sleep(FLUSH_EVERY * 2).await;
    assert!(task.is_finished());
    assert_eq!(calls.lock().unwrap().len(), 1);
}
