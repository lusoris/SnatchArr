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

fn has_refill_failed_kept(events: &[SnatchEvent], kept: u32) -> bool {
    let kept_needle = format!("keeping {kept} candidate(s)");
    events.iter().any(|ev| {
        ev.level == Level::Warn as i32
            && ev.r#type == EventType::PageFetched as i32
            && ev.title.contains("refill failed")
            && ev.title.contains(&kept_needle)
    })
}

async fn run_refill_filter_failure_fresh_busy(
    server: &MockServer,
) -> Result<(Vec<i64>, Vec<SnatchEvent>), Box<dyn std::error::Error>> {
    let api = FakeApi {
        filter_unprocessed: vec![10, 20],
        filter_refused_calls: vec![REFILL_FILTER_CALL],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    mount_paged(
        server,
        &[(1, episodes(&[(10, 100)])), (2, episodes(&[(20, 200)]))],
        20,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    let requests = some(server.received_requests().await, "recorded requests")?;
    let ids = dispatched_ids(&requests, "episodeIds")?;
    let events = api.reported_events.lock().await.clone();
    Ok((ids, events))
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

#[tokio::test]
async fn refill_filter_failure_applies_fresh_busy_positive() -> TestResult {
    let server = MockServer::start().await;
    mount_busy_queue_reads(
        &server,
        &[busy_queue_episodes(&[]), busy_queue_episodes(&[10])],
    )
    .await;
    let (ids, events) = run_refill_filter_failure_fresh_busy(&server).await?;
    assert_eq!(ids, Vec::<i64>::new());
    assert!(has_refill_failed_kept(&events, 0));
    assert!(has_refill_failed_warn(&events));
    Ok(())
}

#[tokio::test]
async fn refill_filter_failure_applies_fresh_busy_negative() -> TestResult {
    let server = MockServer::start().await;
    mount_busy_queue_reads(
        &server,
        &[busy_queue_episodes(&[]), busy_queue_episodes(&[])],
    )
    .await;
    let (ids, events) = run_refill_filter_failure_fresh_busy(&server).await?;
    assert_eq!(ids, [10]);
    assert!(has_refill_failed_kept(&events, 1));
    Ok(())
}

#[tokio::test]
async fn refill_filter_failure_applies_fresh_busy_boundary() -> TestResult {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(200).set_body_json(busy_queue_episodes(&[])))
        .up_to_n_times(1)
        .with_priority(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(401))
        .up_to_n_times(1)
        .with_priority(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(200).set_body_json(busy_queue_episodes(&[])))
        .with_priority(1)
        .mount(&server)
        .await;
    let (ids, events) = run_refill_filter_failure_fresh_busy(&server).await?;
    assert_eq!(ids, [10]);
    assert!(has_refill_failed_kept(&events, 1));
    assert!(has_warn_title(&events, "busy re-read after refill failed"));
    Ok(())
}
