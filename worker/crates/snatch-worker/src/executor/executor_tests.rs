//! Executor tests: a fake snatcharr.v1.WorkerService (tonic on 127.0.0.1:0) and a wiremock
//! Sonarr drive `execute` / `execute_with` end to end (#18).
#![cfg(test)]

use super::*;
use snatch_proto::worker_service_server::{WorkerService, WorkerServiceServer};
use snatch_proto::*;
use std::sync::Arc;
use tokio::net::TcpListener;
use tokio::sync::Mutex;
use tokio_stream::StreamExt;
use tokio_util::sync::CancellationToken;
use tonic::transport::Server;
use wiremock::matchers::{method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

mod readarr_forks;

type TestResult = Result<(), Box<dyn std::error::Error>>;

/// The value, or a test error naming what was missing (no unwrap: HISS-07).
fn some<T>(value: Option<T>, what: &str) -> Result<T, Box<dyn std::error::Error>> {
    value.ok_or_else(|| format!("missing: {what}").into())
}

#[derive(Clone)]
struct FakeApi {
    pub cancel_heartbeat: bool,
    pub budget_granted: u32,
    /// False answers like an API from before #142: a total, no per-target grants.
    pub per_target_grants: bool,
    /// Grants to answer instead of the API's own rule, to tell them apart from a recount.
    pub scripted_grants: Option<Vec<u32>>,
    /// Every AcquireBudget call: the items asked for and the grants answered.
    pub budget_calls: Arc<Mutex<Vec<BudgetCall>>>,
    pub filter_unprocessed: Vec<i64>,
    pub reported_events: Arc<Mutex<Vec<SnatchEvent>>>,
    pub completed_requests: Arc<Mutex<Vec<CompleteRunRequest>>>,
    /// True answers LeaseRun with no run; false refuses it as unimplemented.
    pub lease_empty: bool,
    /// A run to answer every LeaseRun with; takes precedence over `lease_empty`.
    pub leased: Option<Run>,
    pub fail_heartbeat: bool,
    pub fail_report: bool,
    pub fail_complete: bool,
}

impl Default for FakeApi {
    fn default() -> Self {
        Self {
            cancel_heartbeat: false,
            budget_granted: 1000,
            per_target_grants: true,
            scripted_grants: None,
            budget_calls: Arc::new(Mutex::new(Vec::new())),
            filter_unprocessed: vec![1, 2, 3, 4, 5],
            reported_events: Arc::new(Mutex::new(Vec::new())),
            completed_requests: Arc::new(Mutex::new(Vec::new())),
            lease_empty: false,
            leased: None,
            fail_heartbeat: false,
            fail_report: false,
            fail_complete: false,
        }
    }
}

/// One AcquireBudget call: the items asked for and the grants answered.
type BudgetCall = (Vec<BudgetItem>, Vec<u32>);

/// The API's grant rule: in order, an item that fits gets its cost, a divisible one gets what
/// is left, an atomic one that does not fit gets nothing.
fn fake_grants(mut left: u32, items: &[BudgetItem]) -> Vec<u32> {
    items
        .iter()
        .map(|item| {
            let grant = if item.cost <= left {
                item.cost
            } else if item.atomic {
                0
            } else {
                left
            };
            left = left.saturating_sub(grant);
            grant
        })
        .collect()
}

#[tonic::async_trait]
impl WorkerService for FakeApi {
    async fn lease_run(
        &self,
        _: tonic::Request<LeaseRunRequest>,
    ) -> Result<tonic::Response<LeaseRunResponse>, Status> {
        if let Some(run) = &self.leased {
            return Ok(tonic::Response::new(LeaseRunResponse {
                run: Some(run.clone()),
            }));
        }
        if self.lease_empty {
            return Ok(tonic::Response::new(LeaseRunResponse { run: None }));
        }
        Err(Status::unimplemented("unimplemented"))
    }

    async fn heartbeat(
        &self,
        _: tonic::Request<HeartbeatRequest>,
    ) -> Result<tonic::Response<HeartbeatResponse>, Status> {
        if self.fail_heartbeat {
            return Err(Status::unavailable("planted heartbeat failure"));
        }
        let directive = if self.cancel_heartbeat {
            HeartbeatDirective::Cancel
        } else {
            HeartbeatDirective::Continue
        };
        Ok(tonic::Response::new(HeartbeatResponse {
            directive: directive as i32,
            lease_expires_unix: 0,
        }))
    }

    async fn filter_candidates(
        &self,
        request: tonic::Request<FilterCandidatesRequest>,
    ) -> Result<tonic::Response<FilterCandidatesResponse>, Status> {
        let req = request.into_inner();
        let mut unprocessed = Vec::new();
        for id in req.entity_ids {
            if self.filter_unprocessed.contains(&id) {
                unprocessed.push(id);
            }
        }
        Ok(tonic::Response::new(FilterCandidatesResponse {
            unprocessed_ids: unprocessed,
            priority_ids: vec![],
            priority_group_ids: vec![],
        }))
    }

    async fn acquire_budget(
        &self,
        request: tonic::Request<AcquireBudgetRequest>,
    ) -> Result<tonic::Response<AcquireBudgetResponse>, Status> {
        // Leaves the heartbeat time to cancel first in heartbeat_cancel_terminates_run.
        tokio::time::sleep(Duration::from_millis(200)).await;
        let req = request.into_inner();
        let (granted, granted_items) = if self.per_target_grants {
            let grants = self
                .scripted_grants
                .clone()
                .unwrap_or_else(|| fake_grants(self.budget_granted, &req.items));
            (grants.iter().sum(), grants)
        } else {
            (
                std::cmp::min(self.budget_granted, req.requested),
                Vec::new(),
            )
        };
        self.budget_calls
            .lock()
            .await
            .push((req.items, granted_items.clone()));
        Ok(tonic::Response::new(AcquireBudgetResponse {
            granted,
            remaining_in_window: 1000,
            window_resets_unix: 0,
            granted_items,
        }))
    }

    async fn report_events(
        &self,
        request: tonic::Request<tonic::Streaming<ReportEventsRequest>>,
    ) -> Result<tonic::Response<ReportEventsResponse>, Status> {
        let mut stream = request.into_inner();
        let mut accepted: u32 = 0;
        while let Some(req) = stream.next().await {
            if let Ok(Some(ev)) = req.map(|r| r.event) {
                self.reported_events.lock().await.push(ev);
                accepted = accepted.saturating_add(1);
            }
        }
        if self.fail_report {
            return Err(Status::unavailable("planted ReportEvents failure"));
        }
        Ok(tonic::Response::new(ReportEventsResponse { accepted }))
    }

    async fn complete_run(
        &self,
        request: tonic::Request<CompleteRunRequest>,
    ) -> Result<tonic::Response<CompleteRunResponse>, Status> {
        if self.fail_complete {
            return Err(Status::unavailable("planted CompleteRun failure"));
        }
        self.completed_requests
            .lock()
            .await
            .push(request.into_inner());
        Ok(tonic::Response::new(CompleteRunResponse {}))
    }
}

async fn start_fake_api(api: FakeApi) -> Result<(Client, String), Box<dyn std::error::Error>> {
    let listener = TcpListener::bind("127.0.0.1:0").await?;
    let port = listener.local_addr()?.port();
    let addr = format!("http://127.0.0.1:{}", port);
    tokio::spawn(async move {
        let served = Server::builder()
            .add_service(WorkerServiceServer::new(api))
            .serve_with_incoming(tokio_stream::wrappers::TcpListenerStream::new(listener))
            .await;
        if let Err(e) = served {
            tracing::warn!(error = %e, "fake WorkerService stopped");
        }
    });
    tokio::time::sleep(Duration::from_millis(10)).await;

    let channel = tonic::transport::Channel::from_shared(addr.clone())?.connect_lazy();
    let auth = crate::auth::AuthInterceptor::new(None)?;
    let client =
        snatch_proto::worker_service_client::WorkerServiceClient::with_interceptor(channel, auth);
    Ok((client, addr))
}

fn default_run(api_url: &str) -> Run {
    Run {
        run_id: "test_run".to_owned(),
        lease_expires_unix: 0,
        instance_id: "inst".to_owned(),
        app: AppKind::Sonarr as i32,
        snatch: SnatchKind::Missing as i32,
        base_url: api_url.to_owned(),
        api_key: "abc".to_owned(),
        user_agent: "test".to_owned(),
        focus_entity_id: 0,
        focus_group_id: 0,
        policy: Some(Policy {
            per_cycle: 10,
            page_size: 10,
            monitored_only: false,
            skip_future_releases: false,
            recent_grab_hours: 12,
            selection: snatch_proto::Selection::Sequential as i32,
            radarr_release_type: snatch_proto::RadarrReleaseType::Digital as i32,
            sonarr_mode: snatch_proto::SonarrMode::Episodes as i32,
            lidarr_mode: snatch_proto::LidarrMode::Album as i32,
            await_command: false,
            cursor: "1".to_owned(),
        }),
    }
}

fn wanted_page(records: serde_json::Value) -> serde_json::Value {
    serde_json::json!({
        "page": 1,
        "pageSize": 10,
        "totalRecords": 5,
        "records": records
    })
}

/// Mounts the Sonarr endpoints a run reads: wanted/missing, the download queue and history.
async fn mount_sonarr(
    server: &MockServer,
    wanted: serde_json::Value,
    queue: serde_json::Value,
    history: serde_json::Value,
) {
    for (endpoint, body) in [
        ("/api/v3/wanted/missing", wanted),
        ("/api/v3/queue", queue),
        ("/api/v3/history/since", history),
    ] {
        Mock::given(method("GET"))
            .and(path(endpoint))
            .respond_with(ResponseTemplate::new(200).set_body_json(body))
            .mount(server)
            .await;
    }
}

/// Answers every search command POST with `response`.
async fn mount_command(server: &MockServer, response: ResponseTemplate) {
    Mock::given(method("POST"))
        .and(path("/api/v3/command"))
        .respond_with(response)
        .mount(server)
        .await;
}

/// Answers status polls for command `id` with `response`.
async fn mount_command_status(server: &MockServer, id: u32, response: ResponseTemplate) {
    Mock::given(method("GET"))
        .and(path(format!("/api/v3/command/{id}")))
        .respond_with(response)
        .mount(server)
        .await;
}

/// A search command Sonarr accepted and queued (id 100).
fn queued_command() -> ResponseTemplate {
    ResponseTemplate::new(200)
        .set_body_json(serde_json::json!({"id": 100, "name": "EpisodeSearch", "status": "queued"}))
}

/// Runs `run` with a short command-polling budget (3 polls, 10 ms apart); returns what the
/// run recorded about its API calls.
async fn run_short_polling(client: Client, run: Run) -> crate::health::Health {
    let health = crate::health::Health::new(Duration::from_secs(180));
    crate::executor::execute_with(
        client,
        run,
        Duration::from_secs(1),
        "w-1",
        crate::executor::Polling {
            max: 3,
            interval: Duration::from_millis(10),
        },
        &health,
    )
    .await;
    health
}

/// Counts the search command POSTs Sonarr received.
fn search_posts(requests: &[wiremock::Request]) -> usize {
    requests
        .iter()
        .filter(|r| r.method == "POST" && r.url.path() == "/api/v3/command")
        .count()
}

/// Monitored season-1 episodes as wanted/missing records, one per (episode id, series id);
/// episode number and title follow the id.
fn episodes(rows: &[(i64, i64)]) -> serde_json::Value {
    serde_json::Value::Array(
        rows.iter()
            .map(|&(id, series)| {
                serde_json::json!({
                    "id": id, "seriesId": series, "seasonNumber": 1, "episodeNumber": id,
                    "title": format!("Ep {id}"), "monitored": true, "series": { "monitored": true }
                })
            })
            .collect(),
    )
}

fn queue_page() -> serde_json::Value {
    serde_json::json!({
        "page": 1,
        "pageSize": 10,
        "totalRecords": 0,
        "records": []
    })
}

#[tokio::test]
async fn cap_defers_over_budget() -> TestResult {
    let api = FakeApi {
        budget_granted: 1, // Only 1 granted
        filter_unprocessed: vec![1, 2],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;

    let mock_server = MockServer::start().await;
    mount_sonarr(
        &mock_server,
        wanted_page(episodes(&[(1, 10), (2, 11)])),
        queue_page(),
        serde_json::json!([]),
    )
    .await;

    mount_command(&mock_server, queued_command()).await;

    let run = default_run(&mock_server.uri());
    run_short_polling(client, run).await;

    let requests = some(mock_server.received_requests().await, "recorded requests")?;
    let commands = search_posts(&requests);
    let events = api.reported_events.lock().await;
    assert_eq!(commands, 1, "Only 1 target should be dispatched");
    let warn_ev = events
        .iter()
        .find(|e| e.level == Level::Warn as i32 && e.r#type == EventType::BudgetAcquired as i32);
    assert!(warn_ev.is_some(), "Should report deferred targets");
    Ok(())
}

/// A grant smaller than one series' episodes still searches as many episodes as granted
/// (#136): the target is split at the grant instead of waiting whole.
#[tokio::test]
async fn cap_splits_one_series_to_the_grant() -> TestResult {
    let api = FakeApi {
        budget_granted: 2,
        filter_unprocessed: vec![1, 2, 3],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let mock_server = MockServer::start().await;
    mount_sonarr(
        &mock_server,
        wanted_page(episodes(&[(1, 10), (2, 10), (3, 10)])),
        queue_page(),
        serde_json::json!([]),
    )
    .await;
    mount_command(&mock_server, queued_command()).await;

    let mut run = default_run(&mock_server.uri());
    some(run.policy.as_mut(), "run policy")?.per_cycle = 3;
    run_short_polling(client, run).await;

    let requests = some(mock_server.received_requests().await, "recorded requests")?;
    assert_eq!(
        search_posts(&requests),
        1,
        "one EpisodeSearch for the part that fits"
    );
    let post = some(
        requests
            .iter()
            .find(|r| r.method == "POST" && r.url.path() == "/api/v3/command"),
        "a search POST",
    )?;
    let body: serde_json::Value = serde_json::from_slice(&post.body)?;
    assert_eq!(
        body.get("episodeIds")
            .and_then(|ids| ids.as_array())
            .map(Vec::len),
        Some(2),
        "two of the three episodes are searched: {body}"
    );
    let complete = api.completed_requests.lock().await;
    assert_eq!(
        some(complete.first(), "a CompleteRun call")?.searched.len(),
        2
    );
    Ok(())
}

#[tokio::test]
async fn heartbeat_cancel_terminates_run() -> TestResult {
    let api = FakeApi {
        cancel_heartbeat: true,
        filter_unprocessed: vec![1],
        budget_granted: 100,
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;

    let mock_server = MockServer::start().await;
    mount_sonarr(
        &mock_server,
        wanted_page(episodes(&[(1, 10)])),
        queue_page(),
        serde_json::json!([]),
    )
    .await;

    mount_command(
        &mock_server,
        queued_command().set_delay(Duration::from_millis(100)),
    )
    .await;

    let run = default_run(&mock_server.uri());
    let cancel = CancellationToken::new();
    let hb = {
        let (hb_c, run_id, c) = (client.clone(), run.run_id.clone(), cancel.clone());
        tokio::spawn(async move {
            crate::heartbeat(
                hb_c,
                run_id,
                "worker-1".to_string(),
                Duration::from_millis(10),
                Arc::new(crate::health::Health::new(Duration::from_secs(180))),
                c,
            )
            .await;
        })
    };

    tokio::select! {
        _ = run_short_polling(client, run) => {
        },
        () = cancel.cancelled() => {
        },
    }

    hb.abort();

    assert_eq!(
        search_posts(&mock_server.received_requests().await.unwrap_or_default()),
        0
    );
    assert_eq!(api.completed_requests.lock().await.len(), 0);
    Ok(())
}

#[tokio::test]
async fn skips_queued_and_recently_grabbed() -> TestResult {
    // ID 1 queued, ID 2 dropped by FilterCandidates, ID 3 kept
    let api = FakeApi {
        filter_unprocessed: vec![3],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;

    let mock_server = MockServer::start().await;
    mount_sonarr(
        &mock_server,
        wanted_page(episodes(&[(1, 10), (2, 10), (3, 10)])),
        serde_json::json!({"page":1,"pageSize":10,"totalRecords":1,"records":[{"episodeId":1}]}),
        serde_json::json!([]),
    )
    .await;

    mount_command(&mock_server, queued_command()).await;

    let run = default_run(&mock_server.uri());
    run_short_polling(client, run).await;

    let requests = some(mock_server.received_requests().await, "recorded requests")?;
    let posts: Vec<_> = requests
        .iter()
        .filter(|r| r.method == "POST" && r.url.path() == "/api/v3/command")
        .collect();
    assert_eq!(posts.len(), 1, "Only ID 3 should be dispatched");

    let body = std::str::from_utf8(&some(posts.first(), "a search POST")?.body)?;
    assert!(
        body.contains("\"episodeIds\":[3]"),
        "Dispatch should be for episode 3 only"
    );
    Ok(())
}

#[tokio::test]
async fn await_command_exits_after_bound() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![1],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;

    let mock_server = MockServer::start().await;
    mount_sonarr(
        &mock_server,
        wanted_page(episodes(&[(1, 10)])),
        queue_page(),
        serde_json::json!([]),
    )
    .await;

    mount_command(&mock_server, queued_command()).await;

    mount_command_status(&mock_server, 100, queued_command()).await;
    mount_command_status(&mock_server, 100, queued_command()).await;

    let mut run = default_run(&mock_server.uri());
    some(run.policy.as_mut(), "run policy")?.await_command = true;

    run_short_polling(client, run).await;

    let events = api.reported_events.lock().await;
    let fail_ev = events
        .iter()
        .find(|e| e.r#type == EventType::SearchFailed as i32);
    assert!(
        fail_ev.is_some(),
        "Should report SearchFailed after exceeding MAX_COMMAND_POLLS"
    );

    let requests = some(mock_server.received_requests().await, "recorded requests")?;
    let gets: Vec<_> = requests
        .iter()
        .filter(|r| r.method == "GET" && r.url.path() == "/api/v3/command/100")
        .collect();
    assert_eq!(gets.len(), 3, "Should poll exactly 'max' times");
    Ok(())
}

#[tokio::test]
async fn circuit_opens_after_consecutive_failures() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![1, 2, 3, 4],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;

    let mock_server = MockServer::start().await;
    mount_sonarr(
        &mock_server,
        wanted_page(episodes(&[(1, 10), (2, 10), (3, 10), (4, 10)])),
        queue_page(),
        serde_json::json!([]),
    )
    .await;

    mount_command(&mock_server, ResponseTemplate::new(500)).await;

    let mut run = default_run(&mock_server.uri());
    some(run.policy.as_mut(), "run policy")?.per_cycle = 4;
    // The policy will group them individually if SonarrMode is Episodes and they are from the same series...
    // Actually snatch_core groups episodes of the same series. We want individual POSTs, so let's give them different seriesId.
    mock_server.reset().await;
    mount_sonarr(
        &mock_server,
        wanted_page(episodes(&[(1, 10), (2, 11), (3, 12), (4, 13)])),
        queue_page(),
        serde_json::json!([]),
    )
    .await;

    mount_command(&mock_server, ResponseTemplate::new(500)).await;

    run_short_polling(client, run).await;

    let requests = some(mock_server.received_requests().await, "recorded requests")?;
    let posts: Vec<_> = requests
        .iter()
        .filter(|r| r.method == "POST" && r.url.path() == "/api/v3/command")
        .collect();
    assert_eq!(
        posts.len(),
        9,
        "Should open circuit after exactly 3 consecutive failures (3 retries each)"
    );

    let events = api.reported_events.lock().await;
    let circuit_ev = events
        .iter()
        .find(|e| e.level == Level::Error as i32 && e.title.contains("circuit open"));
    assert!(circuit_ev.is_some(), "Should report circuit open event");

    let complete = api.completed_requests.lock().await;
    assert_eq!(complete.len(), 1, "CompleteRun should still be called");
    assert_eq!(
        some(complete.first(), "a CompleteRun call")?.outcome,
        RunOutcome::Failed as i32
    );
    Ok(())
}

#[test]
fn poll_bound_duration() {
    assert_eq!(
        super::COMMAND_POLL_INTERVAL * super::MAX_COMMAND_POLLS,
        std::time::Duration::from_secs(300)
    );
}

#[tokio::test]
async fn cap_boundary_exact() -> TestResult {
    let api = FakeApi {
        budget_granted: 2, // exactly the number of items
        filter_unprocessed: vec![1, 2],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;

    let mock_server = MockServer::start().await;
    mount_sonarr(
        &mock_server,
        wanted_page(episodes(&[(1, 10), (2, 11)])),
        queue_page(),
        serde_json::json!([]),
    )
    .await;

    mount_command(&mock_server, queued_command()).await;

    let mut run = default_run(&mock_server.uri());
    some(run.policy.as_mut(), "run policy")?.per_cycle = 2;

    run_short_polling(client, run).await;

    let events = api.reported_events.lock().await;
    let deferred = events
        .iter()
        .find(|e| e.level == Level::Warn as i32 && e.title.contains("deferred"));
    assert!(
        deferred.is_none(),
        "Should not warn about deferred targets if grant == requested"
    );
    let complete = api.completed_requests.lock().await;
    assert_eq!(complete.len(), 1);
    assert_eq!(
        some(complete.first(), "a CompleteRun call")?.outcome,
        RunOutcome::Done as i32
    );
    Ok(())
}

use std::sync::atomic::{AtomicI32, Ordering};
#[derive(Clone)]
struct FailThenSucceed {
    failures: Arc<AtomicI32>,
}
impl wiremock::Respond for FailThenSucceed {
    fn respond(&self, _request: &wiremock::Request) -> ResponseTemplate {
        if self.failures.fetch_sub(1, Ordering::SeqCst) > 0 {
            ResponseTemplate::new(500)
        } else {
            ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "id": 100,
                "name": "EpisodeSearch",
                "status": "queued"
            }))
        }
    }
}

#[tokio::test]
async fn circuit_boundary_recovers() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![1, 2, 3, 4],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;

    let mock_server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/v3/wanted/missing"))
        .respond_with(ResponseTemplate::new(200).set_body_json(wanted_page(serde_json::json!([
            { "id": 1, "seriesId": 10, "seasonNumber": 1, "episodeNumber": 1, "title": "Ep 1", "monitored": true, "series": { "monitored": true } },
            { "id": 2, "seriesId": 11, "seasonNumber": 1, "episodeNumber": 2, "title": "Ep 2", "monitored": true, "series": { "monitored": true } },
            { "id": 3, "seriesId": 12, "seasonNumber": 1, "episodeNumber": 3, "title": "Ep 3", "monitored": true, "series": { "monitored": true } },
            { "id": 4, "seriesId": 13, "seasonNumber": 1, "episodeNumber": 4, "title": "Ep 4", "monitored": true, "series": { "monitored": true } }
        ]))))
        .mount(&mock_server).await;

    Mock::given(method("GET"))
        .and(path("/api/v3/queue"))
        .respond_with(ResponseTemplate::new(200).set_body_json(queue_page()))
        .mount(&mock_server)
        .await;

    Mock::given(method("GET"))
        .and(path("/api/v3/history/since"))
        .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!([])))
        .mount(&mock_server)
        .await;

    Mock::given(method("POST"))
        .and(path("/api/v3/command"))
        .respond_with(FailThenSucceed {
            failures: Arc::new(AtomicI32::new(6)),
        })
        .mount(&mock_server)
        .await;

    let mut run = default_run(&mock_server.uri());
    some(run.policy.as_mut(), "run policy")?.per_cycle = 4;

    run_short_polling(client, run).await;

    let events = api.reported_events.lock().await;
    let circuit_ev = events
        .iter()
        .find(|e| e.level == Level::Error as i32 && e.title.contains("circuit open"));
    assert!(
        circuit_ev.is_none(),
        "Circuit should stay closed because 3rd attempt succeeded"
    );
    let complete = api.completed_requests.lock().await;
    assert_eq!(complete.len(), 1);
    assert_eq!(
        some(complete.first(), "a CompleteRun call")?.outcome,
        RunOutcome::Done as i32
    );
    Ok(())
}

#[tokio::test]
async fn heartbeat_negative_without_cancel() -> TestResult {
    let api = FakeApi {
        filter_unprocessed: vec![1, 2],
        budget_granted: 100,
        cancel_heartbeat: false, // Heartbeat continues
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;

    let mock_server = MockServer::start().await;
    mount_sonarr(
        &mock_server,
        wanted_page(episodes(&[(1, 10), (2, 11)])),
        queue_page(),
        serde_json::json!([]),
    )
    .await;

    mount_command(&mock_server, queued_command()).await;

    let mut run = default_run(&mock_server.uri());
    some(run.policy.as_mut(), "run policy")?.per_cycle = 2;

    run_short_polling(client, run).await;

    let complete = api.completed_requests.lock().await;
    assert_eq!(complete.len(), 1, "Run should complete");
    assert_eq!(
        some(complete.first(), "a CompleteRun call")?.outcome,
        RunOutcome::Done as i32,
        "Run should be Done, not cancelled"
    );
    assert_eq!(
        some(complete.first(), "a CompleteRun call")?.searched.len(),
        2,
        "All granted targets should be dispatched"
    );
    Ok(())
}

/// Two wanted episodes of series 10 in two seasons: one SeriesSearch that costs 2.
fn two_season_series() -> serde_json::Value {
    serde_json::json!([
        {
            "id": 1, "seriesId": 10, "seasonNumber": 1, "episodeNumber": 1,
            "title": "Ep 1", "monitored": true, "series": { "monitored": true }
        },
        {
            "id": 2, "seriesId": 10, "seasonNumber": 2, "episodeNumber": 1,
            "title": "Ep 2", "monitored": true, "series": { "monitored": true }
        },
    ])
}

/// Runs one whole-series (Shows mode) cycle over `two_season_series` with `budget` stamina
/// left; returns the search commands sent and the AcquireBudget calls the API saw.
async fn series_run(budget: u32) -> Result<(usize, Vec<BudgetCall>), Box<dyn std::error::Error>> {
    let api = FakeApi {
        budget_granted: budget,
        filter_unprocessed: vec![1, 2],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let mock_server = MockServer::start().await;
    mount_sonarr(
        &mock_server,
        wanted_page(two_season_series()),
        queue_page(),
        serde_json::json!([]),
    )
    .await;
    mount_command(&mock_server, queued_command()).await;

    let mut run = default_run(&mock_server.uri());
    some(run.policy.as_mut(), "run policy")?.sonarr_mode = snatch_proto::SonarrMode::Shows as i32;
    run_short_polling(client, run).await;

    let requests = some(mock_server.received_requests().await, "recorded requests")?;
    let calls = api.budget_calls.lock().await.clone();
    Ok((search_posts(&requests), calls))
}

/// Negative: a whole-series search that does not fit waits whole and is charged nothing
/// (#142). The worker marks it atomic, so the API grants zero instead of a share it could
/// never spend.
#[tokio::test]
async fn whole_series_that_does_not_fit_is_not_charged() -> TestResult {
    let (commands, calls) = series_run(1).await?;
    assert_eq!(commands, 0, "a series that costs 2 is not searched on 1");
    let whole = BudgetItem {
        cost: 2,
        atomic: true,
    };
    assert_eq!(calls, vec![(vec![whole], vec![0])]);
    Ok(())
}

/// Positive and boundary: with exactly its cost left, the whole series is searched once and
/// charged its cost.
#[tokio::test]
async fn whole_series_that_fits_is_searched_and_charged() -> TestResult {
    let (commands, calls) = series_run(2).await?;
    assert_eq!(commands, 1, "one SeriesSearch");
    let whole = BudgetItem {
        cost: 2,
        atomic: true,
    };
    assert_eq!(calls, vec![(vec![whole], vec![2])]);
    Ok(())
}

/// The worker dispatches what the API granted per target and does not recount the total: with
/// grants of 0 and 1 for two series, only the second one is searched (a recount of the total 1
/// would search an episode of the first).
#[tokio::test]
async fn per_target_grants_decide_what_is_searched() -> TestResult {
    let api = FakeApi {
        scripted_grants: Some(vec![0, 1]),
        filter_unprocessed: vec![1, 2, 3],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let mock_server = MockServer::start().await;
    mount_sonarr(
        &mock_server,
        wanted_page(episodes(&[(1, 10), (2, 10), (3, 11)])),
        queue_page(),
        serde_json::json!([]),
    )
    .await;
    mount_command(&mock_server, queued_command()).await;

    let mut run = default_run(&mock_server.uri());
    some(run.policy.as_mut(), "run policy")?.per_cycle = 3;
    run_short_polling(client, run).await;

    let complete = api.completed_requests.lock().await;
    let searched: Vec<i64> = some(complete.first(), "a CompleteRun call")?
        .searched
        .iter()
        .map(|item| item.entity_id)
        .collect();
    assert_eq!(
        searched,
        vec![3],
        "only the episode of the second series was granted"
    );
    Ok(())
}

/// An API from before #142 answers with a total only; the worker then caps the targets
/// itself, as it did before.
#[tokio::test]
async fn api_without_per_target_grants_is_capped_by_the_total() -> TestResult {
    let api = FakeApi {
        budget_granted: 2,
        per_target_grants: false,
        filter_unprocessed: vec![1, 2, 3],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let mock_server = MockServer::start().await;
    mount_sonarr(
        &mock_server,
        wanted_page(episodes(&[(1, 10), (2, 10), (3, 10)])),
        queue_page(),
        serde_json::json!([]),
    )
    .await;
    mount_command(&mock_server, queued_command()).await;

    let mut run = default_run(&mock_server.uri());
    some(run.policy.as_mut(), "run policy")?.per_cycle = 3;
    run_short_polling(client, run).await;

    let complete = api.completed_requests.lock().await;
    assert_eq!(
        some(complete.first(), "a CompleteRun call")?.searched.len(),
        2,
        "two of three episodes fit the total of 2"
    );
    Ok(())
}

// Liveness (gap audit A1-01): every API call the worker makes either records a contact or
// counts a failure, so /livez and /metrics see an API the worker cannot use.

#[tokio::test]
async fn completed_run_records_contact_and_no_failure() -> TestResult {
    let sonarr = MockServer::start().await;
    let (client, _) = start_fake_api(FakeApi::default()).await?;
    let health = run_short_polling(client, default_run(&sonarr.uri())).await;
    assert!(health.contacted(), "ReportEvents and CompleteRun answered");
    for call in [
        crate::health::Call::ReportEvents,
        crate::health::Call::CompleteRun,
    ] {
        assert_eq!(health.failures(call), 0, "{call:?}");
    }
    Ok(())
}

#[tokio::test]
async fn failed_complete_run_is_counted() -> TestResult {
    let sonarr = MockServer::start().await;
    let api = FakeApi {
        fail_complete: true,
        ..FakeApi::default()
    };
    let (client, _) = start_fake_api(api).await?;
    let health = run_short_polling(client, default_run(&sonarr.uri())).await;
    assert_eq!(health.failures(crate::health::Call::CompleteRun), 1);
    assert_eq!(health.failures(crate::health::Call::ReportEvents), 0);
    // The answered event stream is the only successful call of this run.
    assert!(health.contacted());
    Ok(())
}

#[tokio::test]
async fn refused_event_stream_is_counted() -> TestResult {
    let sonarr = MockServer::start().await;
    let api = FakeApi {
        fail_report: true,
        ..FakeApi::default()
    };
    let (client, _) = start_fake_api(api).await?;
    let health = run_short_polling(client, default_run(&sonarr.uri())).await;
    assert_eq!(health.failures(crate::health::Call::ReportEvents), 1);
    assert_eq!(health.failures(crate::health::Call::CompleteRun), 0);
    Ok(())
}

fn loop_config() -> Arc<crate::config::Config> {
    Arc::new(crate::config::Config {
        api_grpc: String::new(),
        token: None,
        worker_id: "w-1".to_owned(),
        concurrency: 1,
        lease_wait_secs: 1,
        heartbeat: Duration::from_secs(5),
        arr_timeout: Duration::from_secs(1),
        http_addr: std::net::SocketAddr::from(([127, 0, 0, 1], 0)),
        stale: Duration::from_secs(180),
    })
}

/// Waits up to 5 s for `done`, then stops the task behind `stop`.
async fn until(
    health: &crate::health::Health,
    done: impl Fn(&crate::health::Health) -> bool,
    stop: &CancellationToken,
    task: tokio::task::JoinHandle<()>,
) -> TestResult {
    for _ in 0..500 {
        if done(health) {
            break;
        }
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
    stop.cancel();
    tokio::time::timeout(Duration::from_secs(5), task).await??;
    Ok(())
}

#[tokio::test]
async fn refused_lease_is_counted_and_is_no_contact() -> TestResult {
    let (client, _) = start_fake_api(FakeApi::default()).await?;
    let health = Arc::new(crate::health::Health::new(Duration::from_secs(180)));
    let stop = CancellationToken::new();
    let task = tokio::spawn(crate::lease_loop(
        loop_config(),
        client,
        health.clone(),
        stop.clone(),
    ));
    until(
        &health,
        |h| h.failures(crate::health::Call::LeaseRun) > 0,
        &stop,
        task,
    )
    .await?;
    assert!(health.failures(crate::health::Call::LeaseRun) >= 1);
    assert!(
        !health.contacted(),
        "a refused lease must not refresh /livez"
    );
    Ok(())
}

#[tokio::test]
async fn answered_lease_records_contact() -> TestResult {
    let api = FakeApi {
        lease_empty: true,
        ..FakeApi::default()
    };
    let (client, _) = start_fake_api(api).await?;
    let health = Arc::new(crate::health::Health::new(Duration::from_secs(180)));
    let stop = CancellationToken::new();
    let task = tokio::spawn(crate::lease_loop(
        loop_config(),
        client,
        health.clone(),
        stop.clone(),
    ));
    until(&health, crate::health::Health::contacted, &stop, task).await?;
    assert!(health.contacted());
    assert_eq!(health.failures(crate::health::Call::LeaseRun), 0);
    Ok(())
}

#[tokio::test]
async fn leased_run_is_executed_and_completed() -> TestResult {
    // The *arr address refuses connections: the run fails fast and must still be completed.
    let api = FakeApi {
        leased: Some(default_run("http://127.0.0.1:1")),
        ..FakeApi::default()
    };
    let completed = api.completed_requests.clone();
    let (client, _) = start_fake_api(api).await?;
    let health = Arc::new(crate::health::Health::new(Duration::from_secs(180)));
    let stop = CancellationToken::new();
    let task = tokio::spawn(crate::lease_loop(
        loop_config(),
        client,
        health.clone(),
        stop.clone(),
    ));
    let mut done = false;
    for _ in 0..500 {
        if !completed.lock().await.is_empty() {
            done = true;
            break;
        }
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
    stop.cancel();
    tokio::time::timeout(Duration::from_secs(5), task).await??;
    assert!(done, "the leased run was never completed");
    assert_eq!(
        completed.lock().await.first().map(|c| c.run_id.as_str()),
        Some("test_run")
    );
    assert_eq!(health.failures(crate::health::Call::CompleteRun), 0);
    Ok(())
}

#[tokio::test]
async fn heartbeat_failure_is_counted_and_success_is_contact() -> TestResult {
    for (fail, want_failures, want_contact) in [(true, true, false), (false, false, true)] {
        let api = FakeApi {
            fail_heartbeat: fail,
            ..FakeApi::default()
        };
        let (client, _) = start_fake_api(api).await?;
        let health = Arc::new(crate::health::Health::new(Duration::from_secs(180)));
        let stop = CancellationToken::new();
        let task = tokio::spawn(crate::heartbeat(
            client,
            "run-1".to_owned(),
            "w-1".to_owned(),
            Duration::from_millis(10),
            health.clone(),
            stop.clone(),
        ));
        until(
            &health,
            |h| h.contacted() || h.failures(crate::health::Call::Heartbeat) > 0,
            &stop,
            task,
        )
        .await?;
        assert_eq!(
            health.failures(crate::health::Call::Heartbeat) > 0,
            want_failures,
            "fail={fail}"
        );
        assert_eq!(health.contacted(), want_contact, "fail={fail}");
    }
    Ok(())
}
