use super::*;
use snatch_core::Selection as CoreSelection;
use wiremock::matchers::query_param;

mod filter_failure;
mod latest_read;
mod merge;
mod pages;
mod revalidation;

pub(super) fn busy_queue_episodes(ids: &[i64]) -> serde_json::Value {
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
pub(super) async fn mount_busy_queue_reads(server: &MockServer, bodies: &[serde_json::Value]) {
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

pub(super) fn has_warn_title(events: &[SnatchEvent], needle: &str) -> bool {
    events.iter().any(|ev| {
        ev.level == Level::Warn as i32
            && ev.r#type == EventType::CandidatesFiltered as i32
            && ev.title.contains(needle)
    })
}

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
    let mut pages = Vec::new();
    for request in requests {
        if request.method != "GET" || request.url.path() != wanted {
            continue;
        }
        let (_, value) = request
            .url
            .query_pairs()
            .find(|(key, _)| key == "page")
            .ok_or_else(|| format!("GET {wanted} without page parameter"))?;
        pages.push(value.parse::<u32>()?);
    }
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

async fn send_raw_get(server: &MockServer, path: &str) -> TestResult {
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    let response = tokio::time::timeout(std::time::Duration::from_secs(5), async {
        let mut stream = tokio::net::TcpStream::connect(server.address()).await?;
        let request =
            format!("GET {path} HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n");
        stream.write_all(request.as_bytes()).await?;
        let mut response = Vec::new();
        stream.read_to_end(&mut response).await?;
        Ok::<_, Box<dyn std::error::Error>>(response)
    })
    .await
    .map_err(|_| "raw GET timed out after 5 seconds")??;
    let status = String::from_utf8_lossy(&response);
    assert!(
        status.contains("200 OK") || status.starts_with("HTTP/1.1 200"),
        "unexpected response for {path}: {status}"
    );
    Ok(())
}

async fn mount_one_get_send_raw(
    mock_path: &str,
    request_path: &str,
) -> Result<Vec<wiremock::Request>, Box<dyn std::error::Error>> {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path(mock_path))
        .respond_with(ResponseTemplate::new(200))
        .mount(&server)
        .await;
    send_raw_get(&server, request_path).await?;
    some(server.received_requests().await, "recorded requests")
}

#[tokio::test]
async fn wanted_pages_at_requires_page_query_positive() -> TestResult {
    let requests =
        mount_one_get_send_raw("/api/v3/wanted/missing", "/api/v3/wanted/missing?page=2").await?;
    assert_eq!(wanted_pages_at(&requests, "/api/v3")?, [2]);
    Ok(())
}

#[tokio::test]
async fn wanted_pages_at_requires_page_query_negative() -> TestResult {
    let requests =
        mount_one_get_send_raw("/api/v3/wanted/missing", "/api/v3/wanted/missing").await?;
    assert!(
        wanted_pages_at(&requests, "/api/v3").is_err(),
        "missing page parameter should error"
    );
    Ok(())
}

#[tokio::test]
async fn wanted_pages_at_requires_page_query_boundary() -> TestResult {
    let requests = mount_one_get_send_raw("/api/v3/queue", "/api/v3/queue").await?;
    assert_eq!(wanted_pages_at(&requests, "/api/v3")?, Vec::<u32>::new());
    Ok(())
}
