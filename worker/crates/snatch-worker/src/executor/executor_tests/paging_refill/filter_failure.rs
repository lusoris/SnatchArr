use super::*;

/// The planned batch is filtered on call 0; the refill page on call 1 in these fixtures.
const REFILL_FILTER_CALL: usize = 1;

fn has_refill_failed_warn(events: &[SnatchEvent]) -> bool {
    events.iter().any(|ev| {
        ev.level == Level::Warn as i32
            && ev.r#type == EventType::PageFetched as i32
            && ev.title.contains("refill failed")
    })
}

#[tokio::test]
async fn refill_filter_failure_drops_a_reread_excluded_id_positive() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![1, 3, 4],
        filter_refused_calls: vec![REFILL_FILTER_CALL],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    let unmonitored_one = serde_json::json!([
        {"id": 1, "seriesId": 10, "seasonNumber": 1, "episodeNumber": 1, "title": "Ep 1", "monitored": false, "series": {"monitored": true}},
        {"id": 3, "seriesId": 30, "seasonNumber": 1, "episodeNumber": 3, "title": "Ep 3", "monitored": true, "series": {"monitored": true}}
    ]);
    mount_paged(
        &server,
        &[(1, episodes(&[(1, 10), (4, 40)])), (2, unmonitored_one)],
        20,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 3, CoreSelection::Sequential, "1")?;
    some(run.policy.as_mut(), "policy")?.monitored_only = true;
    run_short_polling(client, run).await;
    let calls = api.filter_calls.lock().await;
    assert_eq!(calls.get(REFILL_FILTER_CALL), Some(&vec![3]));
    let requests = some(server.received_requests().await, "recorded requests")?;
    assert_eq!(dispatched_ids(&requests, "episodeIds")?, [4]);
    let events = api.reported_events.lock().await;
    assert!(has_refill_failed_warn(&events));
    Ok(())
}

#[tokio::test]
async fn refill_filter_failure_drops_a_reread_excluded_id_negative() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![1, 3, 4],
        filter_refused_calls: vec![REFILL_FILTER_CALL],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    let monitored_one = serde_json::json!([
        {"id": 1, "seriesId": 10, "seasonNumber": 1, "episodeNumber": 1, "title": "Ep 1", "monitored": true, "series": {"monitored": true}},
        {"id": 3, "seriesId": 30, "seasonNumber": 1, "episodeNumber": 3, "title": "Ep 3", "monitored": true, "series": {"monitored": true}}
    ]);
    mount_paged(
        &server,
        &[(1, episodes(&[(1, 10), (4, 40)])), (2, monitored_one)],
        20,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 3, CoreSelection::Sequential, "1")?;
    some(run.policy.as_mut(), "policy")?.monitored_only = true;
    run_short_polling(client, run).await;
    let calls = api.filter_calls.lock().await;
    assert_eq!(calls.get(REFILL_FILTER_CALL), Some(&vec![1, 3]));
    let requests = some(server.received_requests().await, "recorded requests")?;
    assert_eq!(dispatched_ids(&requests, "episodeIds")?, [1, 4]);
    Ok(())
}

#[tokio::test]
async fn refill_filter_failure_drops_a_reread_excluded_id_boundary() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![1, 3, 4],
        filter_refused_calls: vec![REFILL_FILTER_CALL],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    let page_three_only = serde_json::json!([
        {"id": 3, "seriesId": 30, "seasonNumber": 1, "episodeNumber": 3, "title": "Ep 3", "monitored": true, "series": {"monitored": true}}
    ]);
    mount_paged(
        &server,
        &[(1, episodes(&[(1, 10), (4, 40)])), (2, page_three_only)],
        20,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 3, CoreSelection::Sequential, "1")?;
    some(run.policy.as_mut(), "policy")?.monitored_only = true;
    run_short_polling(client, run).await;
    let calls = api.filter_calls.lock().await;
    assert_eq!(calls.get(REFILL_FILTER_CALL), Some(&vec![3]));
    let requests = some(server.received_requests().await, "recorded requests")?;
    assert_eq!(dispatched_ids(&requests, "episodeIds")?, [1, 4]);
    Ok(())
}
