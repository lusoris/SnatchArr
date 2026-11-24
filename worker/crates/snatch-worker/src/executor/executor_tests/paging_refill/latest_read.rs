use super::*;

fn unmonitored_episode(id: i64) -> serde_json::Value {
    serde_json::json!({
        "id": id,
        "seriesId": id.saturating_mul(10),
        "seasonNumber": 1,
        "episodeNumber": id,
        "title": format!("Ep {id}"),
        "monitored": false,
        "series": { "monitored": true }
    })
}

async fn run_sequential_cursor_two(
    unprocessed: &[i64],
    pages: &[(u32, serde_json::Value)],
) -> Result<(FakeApi, Vec<wiremock::Request>), Box<dyn std::error::Error>> {
    let api = FakeApi {
        filter_unprocessed: unprocessed.to_vec(),
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_paged(&server, pages, 40).await;
    let mut run = default_run(&server.uri());
    policy(&mut run, 3, CoreSelection::Sequential, "2")?;
    some(run.policy.as_mut(), "policy")?.monitored_only = true;
    run_short_polling(client, run).await;
    let requests = some(server.received_requests().await, "recorded requests")?;
    Ok((api, requests))
}

#[tokio::test]
async fn fetch_keeps_the_latest_read_of_a_repeated_id_positive() -> TestResult {
    let pages = [
        (1, episodes(&[])),
        (2, episodes(&[])),
        (3, serde_json::json!([unmonitored_episode(7)])),
        (4, episodes(&[(7, 70), (9, 90)])),
    ];
    let (api, requests) = run_sequential_cursor_two(&[7, 9], &pages).await?;
    assert_eq!(dispatched_ids(&requests, "episodeIds")?, [7, 9]);
    assert_eq!(completed_searched_ids(&api).await?, [7, 9]);
    Ok(())
}

#[tokio::test]
async fn fetch_keeps_the_latest_read_of_a_repeated_id_negative() -> TestResult {
    let page_four = serde_json::json!([
        unmonitored_episode(7),
        {
            "id": 9,
            "seriesId": 90,
            "seasonNumber": 1,
            "episodeNumber": 9,
            "title": "Ep 9",
            "monitored": true,
            "series": { "monitored": true }
        }
    ]);
    let pages = [
        (1, episodes(&[])),
        (2, episodes(&[])),
        (3, episodes(&[(7, 70)])),
        (4, page_four),
    ];
    let (api, requests) = run_sequential_cursor_two(&[7, 9], &pages).await?;
    assert_eq!(dispatched_ids(&requests, "episodeIds")?, [9]);
    assert_eq!(completed_searched_ids(&api).await?, [9]);
    Ok(())
}

#[tokio::test]
async fn fetch_keeps_the_latest_read_of_a_repeated_id_boundary() -> TestResult {
    let pages = [
        (1, episodes(&[(7, 70)])),
        (2, episodes(&[])),
        (3, serde_json::json!([unmonitored_episode(7)])),
        (4, episodes(&[(9, 90)])),
    ];
    let (api, requests) = run_sequential_cursor_two(&[7, 9], &pages).await?;
    assert_eq!(dispatched_ids(&requests, "episodeIds")?, [9]);
    assert_eq!(completed_searched_ids(&api).await?, [9]);
    Ok(())
}

#[tokio::test]
async fn refill_skips_cached_page_one_ids_seen_earlier() -> TestResult {
    let pages = [
        (1, episodes(&[(7, 70), (8, 80)])),
        (2, serde_json::json!([unmonitored_episode(7)])),
        (3, episodes(&[(31, 310)])),
        (4, episodes(&[(41, 410)])),
    ];
    let (api, requests) = run_sequential_cursor_two(&[7, 8, 31, 41], &pages).await?;
    assert_eq!(dispatched_ids(&requests, "episodeIds")?, [8, 31, 41]);
    assert_eq!(completed_searched_ids(&api).await?, [8, 31, 41]);
    assert_eq!(
        *api.filter_calls.lock().await,
        vec![vec![31, 41, 8], vec![31, 41, 8]]
    );
    Ok(())
}

/// Runs a monitored-only Radarr Sequential run from cursor 1 (20 movies in all, page size 10) over
/// `pages`, with FilterCandidates answering the per-call Seerr priority ids in `script`.
async fn run_radarr_priority_script(
    per_cycle: u32,
    script: Vec<Vec<i64>>,
    pages: &[(u32, serde_json::Value)],
) -> Result<FakeApi, Box<dyn std::error::Error>> {
    let api = FakeApi {
        filter_unprocessed: vec![10, 11, 21, 22],
        filter_priority_script: script,
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_paged(&server, pages, 20).await;
    let mut run = default_run(&server.uri());
    run.app = AppKind::Radarr as i32;
    policy(&mut run, per_cycle, CoreSelection::Sequential, "1")?;
    some(run.policy.as_mut(), "policy")?.monitored_only = true;
    run_short_polling(client, run).await;
    Ok(api)
}

/// The title of the run's CandidatesFiltered event.
async fn filtered_title(api: &FakeApi) -> Result<String, Box<dyn std::error::Error>> {
    let events = api.reported_events.lock().await;
    let filtered = events
        .iter()
        .find(|ev| ev.r#type == EventType::CandidatesFiltered as i32);
    Ok(some(filtered, "CandidatesFiltered event")?.title.clone())
}

#[tokio::test]
async fn refill_takes_the_latest_seerr_priority_positive() -> TestResult {
    let pages = [
        (1, serde_json::json!([radarr_record(10)])),
        (2, serde_json::json!([radarr_record(21), radarr_record(22)])),
    ];
    let api = run_radarr_priority_script(2, vec![vec![22], vec![]], &pages).await?;
    assert_eq!(completed_searched_ids(&api).await?, [10, 21]);
    let title = filtered_title(&api).await?;
    assert!(
        title.contains("; 0 requested via Seerr"),
        "unexpected title: {title}"
    );
    Ok(())
}

#[tokio::test]
async fn refill_takes_the_latest_seerr_priority_negative() -> TestResult {
    let pages = [
        (1, serde_json::json!([radarr_record(10)])),
        (2, serde_json::json!([radarr_record(21), radarr_record(22)])),
    ];
    let api = run_radarr_priority_script(2, vec![vec![], vec![22]], &pages).await?;
    assert_eq!(completed_searched_ids(&api).await?, [10, 22]);
    let title = filtered_title(&api).await?;
    assert!(
        title.contains("; 1 requested via Seerr"),
        "unexpected title: {title}"
    );
    Ok(())
}

#[tokio::test]
async fn refill_takes_the_latest_seerr_priority_boundary() -> TestResult {
    let pages = [
        (1, serde_json::json!([radarr_record(10), radarr_record(11)])),
        (
            2,
            serde_json::json!([{"id": 21, "title": "Film 21", "monitored": false}]),
        ),
    ];
    let api = run_radarr_priority_script(3, vec![vec![11]], &pages).await?;
    assert_eq!(completed_searched_ids(&api).await?, [10, 11]);
    let calls = api.filter_calls.lock().await.clone();
    assert_eq!(
        calls.len(),
        2,
        "a refill batch with no eligible movie makes no call: {calls:?}"
    );
    assert_eq!(calls.first(), Some(&vec![10, 11]));
    let title = filtered_title(&api).await?;
    assert!(
        title.contains("; 1 requested via Seerr"),
        "unexpected title: {title}"
    );
    Ok(())
}
