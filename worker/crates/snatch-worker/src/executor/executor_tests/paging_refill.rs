use super::*;
use snatch_core::Selection as CoreSelection;
use wiremock::matchers::query_param;

async fn mount_paged(server: &MockServer, pages: &[(u32, serde_json::Value)], total: u32) {
    for (page, records) in pages {
        Mock::given(method("GET"))
            .and(path("/api/v3/wanted/missing"))
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
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(200).set_body_json(queue_page()))
        .mount(server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/v3/history/since"))
        .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!([])))
        .mount(server)
        .await;
    mount_command(server, queued_command()).await;
}

fn wanted_pages(requests: &[wiremock::Request]) -> Result<Vec<u32>, Box<dyn std::error::Error>> {
    let mut pages = requests
        .iter()
        .filter(|request| request.method == "GET" && request.url.path() == "/api/v3/wanted/missing")
        .filter_map(|request| request.url.query_pairs().find(|(key, _)| key == "page"))
        .map(|(_, value)| value.parse::<u32>())
        .collect::<Result<Vec<_>, _>>()?;
    pages.sort_unstable();
    Ok(pages)
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
        assert_eq!(wanted_pages(&requests)?, [1, 2, 3]);
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
    assert_eq!(wanted_pages(&requests)?, [1, 2, 3]);
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
    run.seed = 5;
    policy(&mut run, 2, CoreSelection::Random, "1")?;
    run_short_polling(client, run).await;
    let requests = some(server.received_requests().await, "recorded requests")?;
    let read = wanted_pages(&requests)?;
    assert!((8..=9).contains(&read.len()), "read pages: {read:?}");
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
    let pages: Vec<_> = (1..=5)
        .map(|page| {
            let records = if page == 5 {
                episodes(&[(50, 50)])
            } else {
                episodes(&[])
            };
            (page, records)
        })
        .collect();
    mount_paged(&server, &pages, 50).await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 2, CoreSelection::Sequential, "1")?;
    run_short_polling(client, run).await;
    assert_eq!(
        some(
            api.completed_requests.lock().await.first(),
            "CompleteRun request"
        )?
        .cursor,
        "1"
    );
    Ok(())
}
