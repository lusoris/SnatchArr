use super::*;

#[tokio::test]
async fn refill_error_keeps_the_collected_candidates() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![10, 30],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100)])), (3, episodes(&[(30, 300)]))],
        30,
    )
    .await;
    Mock::given(method("GET"))
        .and(path("/api/v3/wanted/missing"))
        .and(query_param("page", "2"))
        .respond_with(ResponseTemplate::new(400).set_body_string("bad page"))
        .mount(&server)
        .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 3, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [10]);
    let events = api.reported_events.lock().await;
    let warn = events
        .iter()
        .find(|ev| ev.level == Level::Warn as i32 && ev.r#type == EventType::PageFetched as i32);
    let title = &some(warn, "Warn PageFetched event")?.title;
    assert!(title.contains("400"), "unexpected title: {title}");
    assert!(
        title.contains("1 candidate(s)"),
        "unexpected title: {title}"
    );
    Ok(())
}

#[tokio::test]
async fn refill_rereads_busy_items() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![10, 20],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    for (page, records) in [(1u32, episodes(&[(10, 100)])), (2, episodes(&[(20, 200)]))] {
        Mock::given(method("GET"))
            .and(path("/api/v3/wanted/missing"))
            .and(query_param("page", page.to_string()))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "page": page, "pageSize": 10, "totalRecords": 20, "records": records,
            })))
            .mount(&server)
            .await;
    }
    Mock::given(method("GET"))
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(200).set_body_json(queue_page()))
        .up_to_n_times(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
            "page": 1, "pageSize": 10, "totalRecords": 1, "records": [{"episodeId": 20}],
        })))
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/v3/history/since"))
        .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!([])))
        .mount(&server)
        .await;
    mount_command(&server, queued_command()).await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [10]);
    let events = api.reported_events.lock().await;
    let filtered = events
        .iter()
        .find(|ev| ev.r#type == EventType::CandidatesFiltered as i32);
    let title = &some(filtered, "CandidatesFiltered event")?.title;
    assert!(
        title.contains("1 skipped as queued or grabbed"),
        "unexpected title: {title}"
    );
    Ok(())
}

#[tokio::test]
async fn filter_batches_stay_within_the_api_limit() -> TestResult {
    let eligible: Vec<i64> = (1..=8000).collect();
    let api = FakeApi {
        filter_unprocessed: eligible.clone(),
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    let pages: Vec<_> = (1u32..=8)
        .map(|page| {
            let start = i64::from(page.saturating_sub(1))
                .saturating_mul(1000)
                .saturating_add(1);
            let end = i64::from(page).saturating_mul(1000);
            let rows: Vec<_> = (start..=end).map(|id| (id, id)).collect();
            (page, episodes(&rows))
        })
        .collect();
    mount_paged(&server, &pages, 8000).await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 10, CoreSelection::Recent, "1")?;
    some(run.policy.as_mut(), "policy")?.page_size = 1000;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?.len(), 10);
    let calls = api.filter_calls.lock().await;
    assert!(!calls.is_empty(), "expected FilterCandidates calls");
    assert!(
        calls.iter().all(|batch| batch.len() <= MAX_FILTER_BATCH),
        "a FilterCandidates batch exceeded {MAX_FILTER_BATCH}"
    );
    let sent: BTreeSet<i64> = calls.iter().flatten().copied().collect();
    let expected: BTreeSet<i64> = eligible.into_iter().collect();
    assert_eq!(sent, expected);
    Ok(())
}

#[tokio::test]
async fn revalidation_drops_items_processed_during_refill() -> TestResult {
    let api = FakeApi {
        filter_script: vec![vec![10], vec![20], vec![20]],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_paged(
        &server,
        &[(1, episodes(&[(10, 100)])), (2, episodes(&[(20, 200)]))],
        20,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    let requests = some(server.received_requests().await, "recorded requests")?;
    assert_eq!(dispatched_ids(&requests, "episodeIds")?, [20]);
    assert_eq!(completed_searched_ids(&api).await?, [20]);
    let events = api.reported_events.lock().await;
    assert!(
        events.iter().any(|ev| {
            ev.level == Level::Debug as i32
                && ev.r#type == EventType::CandidatesFiltered as i32
                && ev.title.contains("dropped 1 candidate(s)")
        }),
        "expected CandidatesFiltered event naming the drop: {events:?}"
    );
    Ok(())
}

fn radarr_movie(id: i64, available: bool) -> serde_json::Value {
    serde_json::json!({
        "id": id, "title": format!("Film {id}"), "monitored": true, "isAvailable": available
    })
}

#[tokio::test]
async fn merge_counts_a_repeated_skip_once() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![1, 2, 3],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    let first = serde_json::json!([radarr_movie(1, true), radarr_movie(2, false)]);
    let second = serde_json::json!([
        radarr_movie(1, true),
        radarr_movie(2, false),
        radarr_movie(3, true)
    ]);
    for (page, records) in [(1u32, first), (2, second)] {
        let body = serde_json::json!({"page": page, "pageSize": 10, "totalRecords": 20, "records": records});
        Mock::given(method("GET"))
            .and(path("/api/v3/wanted/missing"))
            .and(query_param("page", page.to_string()))
            .respond_with(ResponseTemplate::new(200).set_body_json(body))
            .mount(&server)
            .await;
    }
    let queue = serde_json::json!({"page": 1, "pageSize": 10, "totalRecords": 1, "records": [{"movieId": 1}]});
    for (ep, body) in [
        ("/api/v3/queue", queue),
        ("/api/v3/history/since", serde_json::json!([])),
    ] {
        Mock::given(method("GET"))
            .and(path(ep))
            .respond_with(ResponseTemplate::new(200).set_body_json(body))
            .mount(&server)
            .await;
    }
    mount_command(&server, queued_command()).await;
    let mut run = default_run(&server.uri());
    run.app = AppKind::Radarr as i32;
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    some(run.policy.as_mut(), "policy")?.skip_unavailable_movies = true;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [3]);
    let events = api.reported_events.lock().await;
    let filtered = events
        .iter()
        .find(|ev| ev.r#type == EventType::CandidatesFiltered as i32);
    let title = &some(filtered, "CandidatesFiltered event")?.title;
    assert!(title.contains("1 skipped as queued or grabbed"), "{title}");
    assert!(title.contains("1 skipped as not yet available"), "{title}");
    Ok(())
}

#[tokio::test]
async fn partial_refill_keeps_fetched_pages() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![30, 31],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_paged(
        &server,
        &[
            (1, episodes(&[])),
            (2, episodes(&[])),
            (3, episodes(&[(30, 300), (31, 310)])),
        ],
        40,
    )
    .await;
    Mock::given(method("GET"))
        .and(path("/api/v3/wanted/missing"))
        .and(query_param("page", "4"))
        .respond_with(ResponseTemplate::new(500).set_body_string("boom"))
        .mount(&server)
        .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 6, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(completed_searched_ids(&api).await?, [30, 31]);
    // Sequential cursor advances past fetched refill page 3 to the failed page 4.
    assert_eq!(
        some(
            api.completed_requests.lock().await.first(),
            "CompleteRun request"
        )?
        .cursor,
        "4"
    );
    let events = api.reported_events.lock().await;
    let warn = events
        .iter()
        .find(|ev| ev.level == Level::Warn as i32 && ev.r#type == EventType::PageFetched as i32);
    let title = &some(warn, "Warn PageFetched event")?.title;
    assert!(
        title.contains("refill page 4 failed"),
        "unexpected title: {title}"
    );
    assert!(
        title.contains("keeping 2 candidate(s) from 1 refill page(s)"),
        "unexpected title: {title}"
    );
    Ok(())
}

#[tokio::test]
async fn sequential_refill_selects_cursor_pages_before_page_one() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![11, 12, 31, 41],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_paged(
        &server,
        &[
            (1, episodes(&[(11, 110), (12, 120)])),
            (2, episodes(&[])),
            (3, episodes(&[(31, 310)])),
            (4, episodes(&[(41, 410)])),
        ],
        40,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "2")?;
    run_short_polling(client, run).await;
    let requests = some(server.received_requests().await, "recorded requests")?;
    assert_eq!(dispatched_ids(&requests, "episodeIds")?, [31, 41]);
    assert_eq!(completed_searched_ids(&api).await?, [31, 41]);
    Ok(())
}

fn test_candidate(id: i64, title: &str) -> Candidate {
    Candidate {
        id,
        group: id,
        season: Some(1),
        title: title.to_owned(),
        release_unix: None,
        monitored: true,
        available: None,
        tags: Vec::new(),
        profiles: Vec::new(),
        season_episode_count: None,
        season_wanted: 0,
    }
}

#[tokio::test]
async fn merge_takes_the_latest_record_for_a_refetched_id() -> TestResult {
    let mut batch_a = Collected {
        keep: vec![
            test_candidate(1, "ep1-old"),
            test_candidate(2, "ep2-old"),
            test_candidate(4, "ep4-untouched"),
        ],
        seen_ids: BTreeSet::from([1, 2, 3, 4]),
        busy_ids: BTreeSet::from([3]),
        unavailable_ids: BTreeSet::from([2]),
        priority_ids: vec![4],
        priority_groups: Vec::new(),
        priorities_read: true,
    };
    let batch_b = Collected {
        keep: vec![test_candidate(3, "ep3"), test_candidate(2, "ep2-new")],
        seen_ids: BTreeSet::from([1, 2, 3]),
        busy_ids: BTreeSet::new(),
        unavailable_ids: BTreeSet::new(),
        priority_ids: vec![2],
        priority_groups: Vec::new(),
        priorities_read: true,
    };
    batch_a.merge(batch_b);
    assert_eq!(
        batch_a.keep,
        vec![
            test_candidate(2, "ep2-new"),
            test_candidate(4, "ep4-untouched"),
            test_candidate(3, "ep3"),
        ]
    );
    assert!(batch_a.busy_ids.is_empty());
    assert!(batch_a.unavailable_ids.is_empty());
    assert_eq!(batch_a.seen_ids, BTreeSet::from([1, 2, 3, 4]));
    assert_eq!(batch_a.priority_ids, vec![2]);

    let api = FakeApi {
        filter_unprocessed: vec![1, 3, 4],
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
    assert_eq!(completed_searched_ids(&api).await?, [3, 4]);
    Ok(())
}

#[tokio::test]
async fn refill_keeps_seerr_priority_when_the_planned_batch_is_empty() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![21, 22],
        filter_priority_ids: vec![22],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    let planned_unmonitored =
        serde_json::json!([{"id": 10, "title": "Film 10", "monitored": false}]);
    let refill_eligible = serde_json::json!([
        {"id": 21, "title": "Film 21", "monitored": true},
        {"id": 22, "title": "Film 22", "monitored": true}
    ]);
    mount_paged(
        &server,
        &[(1, planned_unmonitored), (2, refill_eligible)],
        20,
    )
    .await;
    let mut run = default_run(&server.uri());
    run.app = AppKind::Radarr as i32;
    policy(&mut run, 1, CoreSelection::Sequential, "1")?;
    some(run.policy.as_mut(), "policy")?.monitored_only = true;
    run_short_polling(client, run).await;
    let requests = some(server.received_requests().await, "recorded requests")?;
    assert_eq!(dispatched_ids(&requests, "movieIds")?, [22]);
    assert_eq!(completed_searched_ids(&api).await?, [22]);
    assert_eq!(
        *api.filter_calls.lock().await,
        vec![vec![21, 22], vec![22]],
        "planned batch with no eligible movies makes no FilterCandidates call"
    );
    Ok(())
}
