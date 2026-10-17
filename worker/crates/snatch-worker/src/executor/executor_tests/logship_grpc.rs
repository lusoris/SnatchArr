// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Worker log records reach the API over real gRPC (#349): layer, channel, shipper, client.

use tracing_subscriber::layer::SubscriberExt;

use super::*;

#[tokio::test]
async fn shipped_records_reach_report_logs_with_the_worker_id() -> TestResult {
    let api = FakeApi::default();
    let reported = api.reported_logs.clone();
    let (client, _) = start_fake_api(api).await?;
    let (layer, rx, dropped) = crate::logship::channel(4);
    let subscriber = tracing_subscriber::registry().with(layer);
    tracing::subscriber::with_default(subscriber, || {
        for i in 0..6 {
            tracing::warn!(target: "snatch_worker::executor", i, "planted record");
        }
    });
    let stop = CancellationToken::new();
    stop.cancel();
    let shipper = crate::spawn_shipper(&client, "w-grpc".to_owned(), rx, dropped, stop);
    tokio::time::timeout(Duration::from_secs(10), shipper).await??;
    let reported = reported.lock().await;
    let batch = some(reported.first(), "a ReportLogs batch")?;
    assert_eq!(batch.worker_id, "w-grpc");
    // Capacity 4: four records travel, the other two arrive as the dropped count.
    assert_eq!(batch.records.len(), 4);
    assert_eq!(batch.dropped, 2);
    let first = some(batch.records.first(), "a record")?;
    assert_eq!(
        (first.message.as_str(), first.target.as_str(), first.level),
        (
            "planted record",
            "snatch_worker::executor",
            Level::Warn as i32
        )
    );
    Ok(())
}
