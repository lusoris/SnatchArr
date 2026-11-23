use super::*;

fn busy_queue_episodes(ids: &[i64]) -> serde_json::Value {
    serde_json::json!({
        "page": 1,
        "pageSize": 10,
        "totalRecords": ids.len(),
        "records": ids
            .iter()
            .map(|id| serde_json::json!({"episodeId": id}))
            .collect::<Vec<_>>(),
    })
}

/// Sonarr queue mocks at priority 1, mounted before [`mount_paged`]'s default queue.
async fn mount_busy_queue_reads(server: &MockServer, bodies: &[serde_json::Value]) {
    let last = bodies.len().saturating_sub(1);
    for (idx, body) in bodies.iter().enumerate() {
        let mut mock = Mock::given(method("GET"))
            .and(path("/api/v3/queue"))
            .respond_with(ResponseTemplate::new(200).set_body_json(body.clone()))
            .with_priority(1);
        if idx < last {
            mock = mock.up_to_n_times(1);
        }
        mock.mount(server).await;
    }
}

fn has_debug_title(events: &[SnatchEvent], needle: &str) -> bool {
    events.iter().any(|ev| {
        ev.level == Level::Debug as i32
            && ev.r#type == EventType::CandidatesFiltered as i32
            && ev.title.contains(needle)
    })
}

fn has_warn_title(events: &[SnatchEvent], needle: &str) -> bool {
    events.iter().any(|ev| {
        ev.level == Level::Warn as i32
            && ev.r#type == EventType::CandidatesFiltered as i32
            && ev.title.contains(needle)
    })
}

#[tokio::test]
async fn revalidation_refills_vacated_slots_positive() -> TestResult {
    let api = FakeApi {
        filter_script: vec![vec![10, 20, 30], vec![20], vec![30]],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100), (20, 200), (30, 300)]))],
        10,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [20, 30]);
    assert_eq!(
        *api.filter_calls.lock().await,
        vec![vec![10, 20, 30], vec![10, 20], vec![30]]
    );
    let events = api.reported_events.lock().await;
    assert!(has_debug_title(
        &events,
        "replacement round filled 1 of 1 vacated slot(s)"
    ));
    Ok(())
}

#[tokio::test]
async fn revalidation_refills_vacated_slots_negative() -> TestResult {
    let api = FakeApi {
        filter_script: vec![vec![10, 20, 30], vec![20], vec![]],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100), (20, 200), (30, 300)]))],
        10,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [20]);
    assert_eq!(api.filter_calls.lock().await.len(), 3);
    let events = api.reported_events.lock().await;
    assert!(!has_debug_title(&events, "replacement round filled"));
    Ok(())
}

#[tokio::test]
async fn revalidation_refills_vacated_slots_boundary() -> TestResult {
    let api = FakeApi {
        filter_script: vec![vec![10, 20], vec![20]],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_paged(&server, &[(1, episodes(&[(10, 100), (20, 200)]))], 10).await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [20]);
    assert_eq!(
        *api.filter_calls.lock().await,
        vec![vec![10, 20], vec![10, 20]]
    );
    Ok(())
}

#[tokio::test]
async fn busy_recheck_before_acquire_positive() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![10, 20],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_busy_queue_reads(&server, &[queue_page(), busy_queue_episodes(&[20])]).await;
    mount_paged(&server, &[(1, episodes(&[(10, 100), (20, 200)]))], 10).await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [10]);
    let requests = some(server.received_requests().await, "recorded requests")?;
    assert_eq!(dispatched_ids(&requests, "episodeIds")?, [10]);
    let events = api.reported_events.lock().await;
    assert!(has_debug_title(
        &events,
        "busy re-check dropped 1 candidate(s)"
    ));
    Ok(())
}

#[tokio::test]
async fn busy_recheck_before_acquire_negative() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![10, 20],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(200).set_body_json(queue_page()))
        .up_to_n_times(1)
        .with_priority(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(401))
        .with_priority(1)
        .mount(&server)
        .await;
    mount_paged(&server, &[(1, episodes(&[(10, 100), (20, 200)]))], 10).await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_single_completed_outcome(&api, RunOutcome::Failed).await?;
    assert!(api.budget_calls.lock().await.is_empty());
    let requests = some(server.received_requests().await, "recorded requests")?;
    assert!(dispatched_ids(&requests, "episodeIds")?.is_empty());
    Ok(())
}

#[tokio::test]
async fn busy_recheck_before_acquire_boundary() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![10, 20, 30, 40],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_busy_queue_reads(&server, &[queue_page(), busy_queue_episodes(&[20, 30])]).await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100), (20, 200), (30, 300), (40, 400)]))],
        10,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [10, 40]);
    Ok(())
}

#[tokio::test]
async fn busy_recheck_before_acquire_after_failed_refresh() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![10, 20],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(200).set_body_json(queue_page()))
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
        .respond_with(ResponseTemplate::new(200).set_body_json(busy_queue_episodes(&[20])))
        .with_priority(1)
        .mount(&server)
        .await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100)])), (2, episodes(&[(20, 200)]))],
        20,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [10]);
    let events = api.reported_events.lock().await;
    assert!(has_warn_title(&events, "busy re-read after refill failed"));
    assert!(has_debug_title(
        &events,
        "busy re-check dropped 1 candidate(s)"
    ));
    Ok(())
}

#[tokio::test]
async fn replacement_busy_recheck_after_processed_positive() -> TestResult {
    let api = FakeApi {
        filter_script: vec![vec![10, 20, 30], vec![20], vec![30]],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_busy_queue_reads(
        &server,
        &[
            serde_json::json!({
                "page": 1, "pageSize": 10, "totalRecords": 0, "records": [],
            }),
            serde_json::json!({
                "page": 1, "pageSize": 10, "totalRecords": 0, "records": [],
            }),
            busy_queue_episodes(&[30]),
        ],
    )
    .await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100), (20, 200), (30, 300)]))],
        10,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [20]);
    let events = api.reported_events.lock().await;
    assert!(has_debug_title(
        &events,
        "busy re-check dropped 1 replacement candidate(s)"
    ));
    Ok(())
}

#[tokio::test]
async fn replacement_busy_recheck_after_processed_negative() -> TestResult {
    let api = FakeApi {
        filter_script: vec![vec![10, 20, 30], vec![20], vec![30]],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_busy_queue_reads(
        &server,
        &[
            serde_json::json!({
                "page": 1, "pageSize": 10, "totalRecords": 0, "records": [],
            }),
            serde_json::json!({
                "page": 1, "pageSize": 10, "totalRecords": 0, "records": [],
            }),
            serde_json::json!({
                "page": 1, "pageSize": 10, "totalRecords": 0, "records": [],
            }),
        ],
    )
    .await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100), (20, 200), (30, 300)]))],
        10,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [20, 30]);
    let events = api.reported_events.lock().await;
    assert!(!has_debug_title(
        &events,
        "busy re-check dropped 1 replacement candidate(s)"
    ));
    Ok(())
}

#[tokio::test]
async fn replacement_busy_recheck_after_processed_boundary() -> TestResult {
    let api = FakeApi {
        filter_script: vec![vec![10, 20, 30], vec![20], vec![30]],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_busy_queue_reads(
        &server,
        &[
            serde_json::json!({
                "page": 1, "pageSize": 10, "totalRecords": 0, "records": [],
            }),
            serde_json::json!({
                "page": 1, "pageSize": 10, "totalRecords": 0, "records": [],
            }),
            busy_queue_episodes(&[99]),
        ],
    )
    .await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100), (20, 200), (30, 300)]))],
        10,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [20, 30]);
    let events = api.reported_events.lock().await;
    assert!(!has_debug_title(
        &events,
        "busy re-check dropped 1 replacement candidate(s)"
    ));
    Ok(())
}

#[tokio::test]
async fn refill_fresh_busy_filter_positive() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![10, 20],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_busy_queue_reads(
        &server,
        &[busy_queue_episodes(&[10, 20]), busy_queue_episodes(&[10])],
    )
    .await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100)])), (2, episodes(&[(20, 200)]))],
        20,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 1, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [20]);
    let requests = some(server.received_requests().await, "recorded requests")?;
    assert_eq!(wanted_pages(&requests)?, [1, 2]);
    Ok(())
}

#[tokio::test]
async fn refill_fresh_busy_filter_negative() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![10, 20],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_busy_queue_reads(&server, &[busy_queue_episodes(&[10, 20])]).await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100)])), (2, episodes(&[(20, 200)]))],
        20,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 1, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, Vec::<i64>::new());
    let requests = some(server.received_requests().await, "recorded requests")?;
    assert_eq!(wanted_pages(&requests)?, [1, 2]);
    Ok(())
}

#[tokio::test]
async fn refill_fresh_busy_filter_boundary() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![10, 20],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(200).set_body_json(busy_queue_episodes(&[10, 20])))
        .up_to_n_times(1)
        .with_priority(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(500))
        .with_priority(1)
        .mount(&server)
        .await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100)])), (2, episodes(&[(20, 200)]))],
        20,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 1, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, Vec::<i64>::new());
    let events = api.reported_events.lock().await;
    assert!(has_warn_title(&events, "busy re-read after refill failed"));
    Ok(())
}
