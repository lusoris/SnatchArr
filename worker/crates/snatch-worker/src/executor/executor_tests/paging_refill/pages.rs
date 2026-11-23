use super::*;

#[tokio::test]
async fn sonarr_refills_random_pages_until_per_cycle_is_met() -> TestResult {
    for seed in 0u32..4 {
        let ids: [i64; 3] = [11, 12, 13].map(|id| id + i64::from(seed) * 10);
        let api = FakeApi {
            filter_unprocessed: ids.to_vec(),
            ..Default::default()
        };
        let (client, _) = start_fake_api(api.clone()).await?;
        let server = MockServer::start().await;
        mount_paged(
            &server,
            &[
                (1, episodes(&[(ids[0], 100)])),
                (2, episodes(&[(ids[1], 200)])),
                (3, episodes(&[(ids[2], 300)])),
            ],
            30,
        )
        .await;
        let mut run = default_run(&server.uri());
        run.seed = u64::from(seed);
        policy(&mut run, 3, CoreSelection::Random, "1")?;
        run_short_polling(client, run).await;
        let requests = some(server.received_requests().await, "recorded requests")?;
        assert_eq!(dispatched_ids(&requests, "episodeIds")?, ids);
        let mut pages = wanted_pages(&requests)?;
        pages.sort_unstable();
        assert_eq!(pages, [1, 2, 3]);
    }
    Ok(())
}

#[tokio::test]
async fn radarr_refills_random_pages_until_per_cycle_is_met() -> TestResult {
    let ids = [41, 42, 43];
    let api = FakeApi {
        filter_unprocessed: ids.to_vec(),
        ..Default::default()
    };
    let (client, _) = start_fake_api(api).await?;
    let server = MockServer::start().await;
    let movies =
        |id| serde_json::json!([{"id": id, "title": format!("Film {id}"), "monitored": true}]);
    mount_paged(
        &server,
        &[
            (1, movies(ids[0])),
            (2, movies(ids[1])),
            (3, movies(ids[2])),
        ],
        30,
    )
    .await;
    let mut run = default_run(&server.uri());
    run.app = AppKind::Radarr as i32;
    run.seed = 3;
    policy(&mut run, 3, CoreSelection::Random, "1")?;
    run_short_polling(client, run).await;
    let requests = some(server.received_requests().await, "recorded requests")?;
    assert_eq!(dispatched_ids(&requests, "movieIds")?, ids);
    let mut pages = wanted_pages(&requests)?;
    pages.sort_unstable();
    assert_eq!(pages, [1, 2, 3]);
    Ok(())
}

#[tokio::test]
async fn refill_stops_at_eight_distinct_pages_and_completes() -> TestResult {
    let pages: Vec<_> = (1..=20).map(|page| (page, serde_json::json!([]))).collect();
    let api = FakeApi {
        filter_unprocessed: vec![],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_paged(&server, &pages, 200).await;
    let mut run = default_run(&server.uri());
    run.seed = 1;
    policy(&mut run, 2, CoreSelection::Random, "1")?;
    run_short_polling(client, run).await;
    let requests = some(server.received_requests().await, "recorded requests")?;
    let read = wanted_pages(&requests)?;
    assert_eq!(read.len(), 8, "read pages: {read:?}");
    assert_eq!(
        read.iter().copied().collect::<BTreeSet<_>>().len(),
        read.len()
    );
    let events = api.reported_events.lock().await;
    assert_eq!(
        events
            .iter()
            .filter(|event| event.r#type == EventType::PageFetched as i32)
            .count(),
        2
    );
    assert_eq!(api.completed_requests.lock().await.len(), 1);
    Ok(())
}

#[tokio::test]
async fn refill_deduplicates_items_when_wanted_pages_shift() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![1, 30],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_paged(
        &server,
        &[
            (1, episodes(&[(1, 10)])),
            (2, episodes(&[(1, 10), (2, 20)])),
            (3, episodes(&[(30, 30)])),
        ],
        30,
    )
    .await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 5, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    let requests = some(server.received_requests().await, "recorded requests")?;
    let ids = dispatched_ids(&requests, "episodeIds")?;
    assert_eq!(ids, [1, 30]);
    let events = api.reported_events.lock().await;
    let filtered = events
        .iter()
        .find(|event| event.r#type == EventType::CandidatesFiltered as i32);
    assert!(
        some(filtered, "CandidatesFiltered event")?
            .title
            .starts_with("2 candidate(s)")
    );
    Ok(())
}

#[tokio::test]
async fn sequential_refill_persists_the_cursor_after_the_last_page() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![50],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    // 20 pages stops at page 8 (cursor 9) instead of wrapping to 1 like an unmoved cursor.
    let pages: Vec<_> = (1..=20)
        .map(|page| {
            let records = if page == 5 {
                episodes(&[(50, 50)])
            } else {
                episodes(&[])
            };
            (page, records)
        })
        .collect();
    mount_paged(&server, &pages, 200).await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(
        some(
            api.completed_requests.lock().await.first(),
            "CompleteRun request"
        )?
        .cursor,
        "9"
    );
    Ok(())
}

#[tokio::test]
async fn every_app_refills_until_per_cycle_is_met() -> TestResult {
    let ids = [11, 12, 13];
    for row in &APPS {
        let api = FakeApi {
            filter_unprocessed: ids.to_vec(),
            ..Default::default()
        };
        let (client, _) = start_fake_api(api.clone()).await?;
        let server = MockServer::start().await;
        mount_paged_at(
            &server,
            row.prefix,
            &[
                (1, row_records(row, &[ids[0]])),
                (2, row_records(row, &[ids[1]])),
                (3, row_records(row, &[ids[2]])),
            ],
            30,
        )
        .await;
        let mut run = default_run(&server.uri());
        run.app = row.app as i32;
        run.seed = 3;
        policy(&mut run, 3, CoreSelection::Random, "1")?;
        run_short_polling(client, run).await;
        let requests = some(server.received_requests().await, "recorded requests")?;
        assert_eq!(completed_searched_ids(&api).await?, ids, "{}", row.name);
        let mut pages = wanted_pages_at(&requests, row.prefix)?;
        pages.sort_unstable();
        assert_eq!(pages, [1, 2, 3], "{}", row.name);
    }
    Ok(())
}

#[tokio::test]
async fn every_app_reads_at_most_eight_pages() -> TestResult {
    let pages: Vec<_> = (1..=20).map(|page| (page, serde_json::json!([]))).collect();
    for row in &APPS {
        let api = FakeApi {
            filter_unprocessed: vec![],
            ..Default::default()
        };
        let (client, _) = start_fake_api(api.clone()).await?;
        let server = MockServer::start().await;
        mount_paged_at(&server, row.prefix, &pages, 200).await;
        let mut run = default_run(&server.uri());
        run.app = row.app as i32;
        run.seed = 1;
        policy(&mut run, 2, CoreSelection::Random, "1")?;
        run_short_polling(client, run).await;
        let requests = some(server.received_requests().await, "recorded requests")?;
        let read = wanted_pages_at(&requests, row.prefix)?;
        assert_eq!(read.len(), 8, "{}: {read:?}", row.name);
        assert_eq!(
            read.iter().copied().collect::<BTreeSet<_>>().len(),
            8,
            "{}: {read:?}",
            row.name
        );
        assert!(
            completed_searched_ids(&api).await?.is_empty(),
            "{}",
            row.name
        );
    }
    Ok(())
}

#[tokio::test]
async fn every_app_searches_a_shifted_item_once() -> TestResult {
    for row in &APPS {
        let api = FakeApi {
            filter_unprocessed: vec![1, 2],
            ..Default::default()
        };
        let (client, _) = start_fake_api(api.clone()).await?;
        let server = MockServer::start().await;
        mount_paged_at(
            &server,
            row.prefix,
            &[(1, row_records(row, &[1])), (2, row_records(row, &[1, 2]))],
            20,
        )
        .await;
        let mut run = default_run(&server.uri());
        run.app = row.app as i32;
        policy(&mut run, 6, CoreSelection::Sequential, "1")?;
        run_short_polling(client, run).await;
        let requests = some(server.received_requests().await, "recorded requests")?;
        assert_eq!(
            wanted_pages_at(&requests, row.prefix)?,
            [1, 2],
            "{}",
            row.name
        );
        assert_eq!(completed_searched_ids(&api).await?, [1, 2], "{}", row.name);
    }
    Ok(())
}

#[tokio::test]
async fn every_app_persists_the_sequential_cursor() -> TestResult {
    for row in &APPS {
        let api = FakeApi {
            filter_unprocessed: vec![50],
            ..Default::default()
        };
        let (client, _) = start_fake_api(api.clone()).await?;
        let server = MockServer::start().await;
        // 20 pages stops at page 8 (cursor 9) instead of wrapping to 1 like an unmoved cursor.
        let pages: Vec<_> = (1..=20)
            .map(|page| {
                let ids: &[i64] = if page == 5 { &[50] } else { &[] };
                (page, row_records(row, ids))
            })
            .collect();
        mount_paged_at(&server, row.prefix, &pages, 200).await;
        let mut run = default_run(&server.uri());
        run.app = row.app as i32;
        policy(&mut run, 2, CoreSelection::Sequential, "1")?;
        run_short_polling(client, run).await;
        let requests = some(server.received_requests().await, "recorded requests")?;
        assert_eq!(
            wanted_pages_at(&requests, row.prefix)?,
            [1, 2, 3, 4, 5, 6, 7, 8],
            "{}",
            row.name
        );
        assert_eq!(completed_searched_ids(&api).await?, [50], "{}", row.name);
        let cursor = some(
            api.completed_requests.lock().await.first(),
            "CompleteRun request",
        )?
        .cursor
        .clone();
        assert_eq!(cursor, "9", "{}", row.name);
    }
    Ok(())
}
