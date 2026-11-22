use super::*;
use snatch_core::Selection as CoreSelection;
use wiremock::matchers::query_param;

const DATE: &str = "2020-01-02T03:04:05Z";

struct AppRow {
    name: &'static str,
    app: AppKind,
    prefix: &'static str,
    record: fn(i64) -> serde_json::Value,
}

fn sonarr_record(id: i64) -> serde_json::Value {
    serde_json::json!({
        "id": id, "seriesId": id.saturating_mul(10), "seasonNumber": 1, "episodeNumber": 1,
        "title": format!("Ep {id}"), "airDateUtc": DATE, "monitored": true,
        "series": {"monitored": true, "tags": [1], "qualityProfileId": 1}
    })
}

fn radarr_record(id: i64) -> serde_json::Value {
    serde_json::json!({
        "id": id, "title": format!("Film {id}"), "year": 2020, "monitored": true,
        "physicalRelease": DATE, "digitalRelease": DATE, "inCinemas": DATE,
        "isAvailable": true, "tags": [1], "qualityProfileId": 1
    })
}

fn lidarr_record(id: i64) -> serde_json::Value {
    serde_json::json!({
        "id": id, "title": format!("Album {id}"), "monitored": true, "releaseDate": DATE,
        "artistId": id.saturating_mul(10), "artist": {"monitored": true, "tags": [1], "qualityProfileId": 1}
    })
}

fn readarr_record(id: i64) -> serde_json::Value {
    serde_json::json!({
        "id": id, "title": format!("Book {id}"), "monitored": true, "releaseDate": DATE,
        "authorId": id.saturating_mul(10), "author": {"monitored": true, "tags": [1], "qualityProfileId": 1}
    })
}

fn whisparr_v2_record(id: i64) -> serde_json::Value {
    serde_json::json!({
        "id": id, "seriesId": id.saturating_mul(10), "seasonNumber": 1, "title": format!("Scene {id}"),
        "releaseDate": {"year": 2020, "month": 1, "day": 2}, "monitored": true,
        "series": {"monitored": true, "tags": [1], "qualityProfileId": 1}
    })
}

fn whisparr_v3_record(id: i64) -> serde_json::Value {
    serde_json::json!({
        "id": id, "title": format!("Clip {id}"), "year": 2020, "monitored": true,
        "releaseDate": DATE, "tags": [1], "qualityProfileId": 1
    })
}

const APPS: [AppRow; 6] = [
    AppRow {
        name: "sonarr",
        app: AppKind::Sonarr,
        prefix: "/api/v3",
        record: sonarr_record,
    },
    AppRow {
        name: "radarr",
        app: AppKind::Radarr,
        prefix: "/api/v3",
        record: radarr_record,
    },
    AppRow {
        name: "lidarr",
        app: AppKind::Lidarr,
        prefix: "/api/v1",
        record: lidarr_record,
    },
    AppRow {
        name: "readarr",
        app: AppKind::Readarr,
        prefix: "/api/v1",
        record: readarr_record,
    },
    AppRow {
        name: "whisparr_v2",
        app: AppKind::WhisparrV2,
        prefix: "/api/v3",
        record: whisparr_v2_record,
    },
    AppRow {
        name: "whisparr_v3",
        app: AppKind::WhisparrV3,
        prefix: "/api/v3",
        record: whisparr_v3_record,
    },
];

fn row_records(row: &AppRow, ids: &[i64]) -> serde_json::Value {
    serde_json::Value::Array(ids.iter().map(|&id| (row.record)(id)).collect())
}

async fn mount_command_at(
    server: &MockServer,
    prefix: &str,
    response: impl wiremock::Respond + 'static,
) {
    Mock::given(method("POST"))
        .and(path(format!("{prefix}/command")))
        .respond_with(response)
        .mount(server)
        .await;
}

async fn mount_paged_at(
    server: &MockServer,
    prefix: &str,
    pages: &[(u32, serde_json::Value)],
    total: u32,
) {
    let wanted = format!("{prefix}/wanted/missing");
    for (page, records) in pages {
        Mock::given(method("GET"))
            .and(path(wanted.as_str()))
            .and(query_param("page", page.to_string()))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "page": page,
                "pageSize": 10,
                "totalRecords": total,
                "records": records,
            })))
            .mount(server)
            .await;
    }
    Mock::given(method("GET"))
        .and(path(format!("{prefix}/queue")))
        .respond_with(ResponseTemplate::new(200).set_body_json(queue_page()))
        .mount(server)
        .await;
    Mock::given(method("GET"))
        .and(path(format!("{prefix}/history/since")))
        .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!([])))
        .mount(server)
        .await;
    mount_command_at(server, prefix, queued_command()).await;
}

async fn mount_paged(server: &MockServer, pages: &[(u32, serde_json::Value)], total: u32) {
    mount_paged_at(server, "/api/v3", pages, total).await;
}

fn wanted_pages_at(
    requests: &[wiremock::Request],
    prefix: &str,
) -> Result<Vec<u32>, Box<dyn std::error::Error>> {
    let wanted = format!("{prefix}/wanted/missing");
    let pages = requests
        .iter()
        .filter(|request| request.method == "GET" && request.url.path() == wanted)
        .filter_map(|request| request.url.query_pairs().find(|(key, _)| key == "page"))
        .map(|(_, value)| value.parse::<u32>())
        .collect::<Result<Vec<_>, _>>()?;
    Ok(pages)
}

fn wanted_pages(requests: &[wiremock::Request]) -> Result<Vec<u32>, Box<dyn std::error::Error>> {
    wanted_pages_at(requests, "/api/v3")
}

async fn completed_searched_ids(api: &FakeApi) -> Result<Vec<i64>, Box<dyn std::error::Error>> {
    let completed = api.completed_requests.lock().await;
    let req = some(completed.first(), "CompleteRun request")?;
    assert_eq!(req.outcome, RunOutcome::Done as i32);
    let mut ids: Vec<i64> = req.searched.iter().map(|item| item.entity_id).collect();
    ids.sort_unstable();
    Ok(ids)
}

fn policy(run: &mut Run, per_cycle: u32, selection: CoreSelection, cursor: &str) -> TestResult {
    let policy = some(run.policy.as_mut(), "policy")?;
    policy.per_cycle = per_cycle;
    policy.page_size = 10;
    policy.selection = match selection {
        CoreSelection::Random => snatch_proto::Selection::Random as i32,
        CoreSelection::Sequential => snatch_proto::Selection::Sequential as i32,
        CoreSelection::Recent => snatch_proto::Selection::Recent as i32,
    };
    policy.cursor = cursor.to_owned();
    Ok(())
}

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
    assert_eq!(
        some(
            api.completed_requests.lock().await.first(),
            "CompleteRun request"
        )?
        .cursor,
        "3"
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
