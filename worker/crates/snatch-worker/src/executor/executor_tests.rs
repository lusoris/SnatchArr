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

mod completion;
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
    /// Ends the event stream with an answer after this many events; later ones are dropped.
    pub report_cut_after: Option<u32>,
    /// The n-th CompleteRun call is refused with the n-th code; calls past the list succeed.
    pub complete_refusals: Vec<tonic::Code>,
    /// Every CompleteRun call, refused or not.
    pub complete_calls: Arc<std::sync::atomic::AtomicU32>,
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
            report_cut_after: None,
            complete_refusals: Vec::new(),
            complete_calls: Arc::new(std::sync::atomic::AtomicU32::new(0)),
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
            if self.report_cut_after == Some(accepted) {
                break;
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
        let call = self
            .complete_calls
            .fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        if let Some(code) = usize::try_from(call)
            .ok()
            .and_then(|n| self.complete_refusals.get(n))
        {
            return Err(Status::new(*code, "planted CompleteRun failure"));
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
        dry_run: false,
        seed: 0,
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
            ..Policy::default()
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

/// Mounts standard Sonarr mock endpoints (wanted, empty queue, empty history).
async fn mount_sonarr_wanted(server: &MockServer, wanted_records: serde_json::Value) {
    mount_sonarr(
        server,
        wanted_page(wanted_records),
        queue_page(),
        serde_json::json!([]),
    )
    .await;
}

/// Mounts standard Sonarr mock endpoints (wanted, empty queue, empty history) and the queued command.
async fn mount_sonarr_default(server: &MockServer, wanted_records: serde_json::Value) {
    mount_sonarr_wanted(server, wanted_records).await;
    mount_command(server, queued_command()).await;
}

struct SonarrRun {
    // Kept to hold the mock server alive for the duration of the test.
    _server: MockServer,
    requests: Vec<wiremock::Request>,
}

async fn run_sonarr_cycle(
    api: &FakeApi,
    wanted: serde_json::Value,
    per_cycle: Option<u32>,
) -> Result<SonarrRun, Box<dyn std::error::Error>> {
    let (client, _) = start_fake_api(api.clone()).await?;
    let mock_server = MockServer::start().await;
    mount_sonarr_default(&mock_server, wanted).await;

    let mut run = default_run(&mock_server.uri());
    if let Some(pc) = per_cycle {
        some(run.policy.as_mut(), "run policy")?.per_cycle = pc;
    }
    run_short_polling(client, run).await;

    let requests = some(mock_server.received_requests().await, "recorded requests")?;
    Ok(SonarrRun {
        _server: mock_server,
        requests,
    })
}

fn command_posts(requests: &[wiremock::Request]) -> Vec<&wiremock::Request> {
    requests
        .iter()
        .filter(|r| r.method == "POST" && r.url.path() == "/api/v3/command")
        .collect()
}

fn has_circuit_open_event(events: &[SnatchEvent]) -> bool {
    events
        .iter()
        .any(|e| e.level == Level::Error as i32 && e.title.contains("circuit open"))
}

async fn assert_single_completed_outcome(
    api: &FakeApi,
    outcome: RunOutcome,
) -> Result<(), Box<dyn std::error::Error>> {
    let complete = api.completed_requests.lock().await;
    assert_eq!(complete.len(), 1);
    assert_eq!(
        some(complete.first(), "a CompleteRun call")?.outcome,
        outcome as i32
    );
    Ok(())
}

/// CompleteRun retry for tests: 3 attempts, 5 ms base wait.
const SHORT_RETRY: crate::executor::Retry = crate::executor::Retry {
    attempts: 3,
    backoff: Duration::from_millis(5),
};

/// Runs `run` with a short command-polling budget (3 polls, 10 ms apart) and [`SHORT_RETRY`];
/// returns what the run recorded about its API calls.
async fn run_short_polling(client: Client, run: Run) -> crate::health::Health {
    run_with_retry(client, run, SHORT_RETRY).await
}

/// [`run_short_polling`] with the CompleteRun retry `retry`.
async fn run_with_retry(
    client: Client,
    run: Run,
    retry: crate::executor::Retry,
) -> crate::health::Health {
    let health = crate::health::Health::new(Duration::from_secs(180));
    crate::executor::execute_with(
        client,
        run,
        Duration::from_secs(1),
        "w-1",
        (
            crate::executor::Polling {
                max: 3,
                interval: Duration::from_millis(10),
            },
            retry,
        ),
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
    let run = run_sonarr_cycle(&api, episodes(&[(1, 10), (2, 11)]), None).await?;

    let commands = search_posts(&run.requests);
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
    let run = run_sonarr_cycle(&api, episodes(&[(1, 10), (2, 10), (3, 10)]), Some(3)).await?;

    assert_eq!(
        search_posts(&run.requests),
        1,
        "one EpisodeSearch for the part that fits"
    );
    let post = some(
        command_posts(&run.requests).into_iter().next(),
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
    mount_sonarr_wanted(&mock_server, episodes(&[(1, 10)])).await;

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
    let posts = command_posts(&requests);
    assert_eq!(posts.len(), 1, "Only ID 3 should be dispatched");

    let body = std::str::from_utf8(&some(posts.first().copied(), "a search POST")?.body)?;
    assert!(
        body.contains("\"episodeIds\":[3]"),
        "Dispatch should be for episode 3 only"
    );
    Ok(())
}

/// Episodes of series 10 (tag 1, profile 4), 20 (tag 2, profile 5) and 30 (no tag, profile 4).
fn labelled_episodes() -> serde_json::Value {
    let row = |id: i64, series: i64, tags: serde_json::Value, profile: i32| {
        serde_json::json!({
            "id": id, "seriesId": series, "seasonNumber": 1, "episodeNumber": id,
            "title": format!("Ep {id}"), "monitored": true, "airDateUtc": "2001-01-01T00:00:00Z",
            "series": { "monitored": true, "tags": tags, "qualityProfileId": profile }
        })
    };
    serde_json::json!([
        row(1, 10, serde_json::json!([1]), 4),
        row(2, 20, serde_json::json!([2]), 5),
        row(3, 30, serde_json::json!([]), 4),
    ])
}

/// Runs a policy change against `labelled_episodes` and returns the searched episode ids, sorted.
async fn dispatched_with(
    change: impl FnOnce(&mut Policy),
) -> Result<Vec<i64>, Box<dyn std::error::Error>> {
    let api = FakeApi {
        filter_unprocessed: vec![1, 2, 3],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api).await?;
    let mock_server = MockServer::start().await;
    mount_sonarr_wanted(&mock_server, labelled_episodes()).await;
    mount_command(&mock_server, queued_command()).await;
    let mut run = default_run(&mock_server.uri());
    change(some(run.policy.as_mut(), "policy")?);
    run_short_polling(client, run).await;
    let requests = some(mock_server.received_requests().await, "recorded requests")?;
    let mut ids = Vec::new();
    for post in command_posts(&requests) {
        let body: serde_json::Value = serde_json::from_slice(&post.body)?;
        let searched = some(
            body.get("episodeIds").and_then(|v| v.as_array()),
            "episodeIds",
        )?;
        ids.extend(searched.iter().filter_map(serde_json::Value::as_i64));
    }
    ids.sort_unstable();
    Ok(ids)
}

#[tokio::test]
async fn policy_tag_and_profile_rules_reach_the_filter() -> TestResult {
    assert_eq!(
        dispatched_with(|_| {}).await?,
        [1, 2, 3],
        "no rule keeps all"
    );
    assert_eq!(dispatched_with(|p| p.include_tags = vec![1]).await?, [1]);
    assert_eq!(dispatched_with(|p| p.exclude_tags = vec![2]).await?, [1, 3]);
    let profile = |p: &mut Policy| p.include_quality_profiles = vec![4];
    assert_eq!(dispatched_with(profile).await?, [1, 3]);
    let profile = |p: &mut Policy| p.exclude_quality_profiles = vec![4];
    assert_eq!(dispatched_with(profile).await?, [2]);
    // Aired in 2001: a delay of a year has passed, one of a century has not.
    let delay = |p: &mut Policy| p.release_delay_hours = 24 * 365;
    assert_eq!(dispatched_with(delay).await?, [1, 2, 3]);
    let delay = |p: &mut Policy| p.release_delay_hours = 24 * 365 * 100;
    assert_eq!(dispatched_with(delay).await?, Vec::<i64>::new());
    Ok(())
}

#[test]
fn filter_of_converts_the_release_delay_to_seconds() {
    let policy = Policy {
        release_delay_hours: 2,
        include_tags: vec![7],
        exclude_quality_profiles: vec![9],
        ..Policy::default()
    };
    let f = filter_of(&policy, 100_000);
    assert_eq!(f.release_delay_secs, 7200);
    assert_eq!(f.now_unix, 100_000);
    assert_eq!(f.tags.include, vec![7]);
    assert_eq!(f.profiles.exclude, vec![9]);
    assert!(
        !f.skip_unavailable,
        "an API that never sets the field skips nothing"
    );
    let on = Policy {
        skip_unavailable_movies: true,
        ..Policy::default()
    };
    assert!(filter_of(&on, 0).skip_unavailable);
    // Boundary: the largest hour count does not overflow.
    let max = Policy {
        release_delay_hours: u32::MAX,
        ..Policy::default()
    };
    assert_eq!(
        filter_of(&max, 0).release_delay_secs,
        i64::from(u32::MAX) * 3600
    );
}

/// A Radarr missing run over movie 1 (available), 2 (not yet available) and 3 (no flag): the
/// searched movie ids, the budget asked for, and the candidates_filtered title.
async fn radarr_availability_run(
    skip_unavailable: bool,
) -> Result<(Vec<i64>, usize, String), Box<dyn std::error::Error>> {
    let api = FakeApi {
        filter_unprocessed: vec![1, 2, 3],
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let mock_server = MockServer::start().await;
    let movie = |id: i64, available: Option<bool>| {
        let mut m = serde_json::json!({"id": id, "title": format!("Film {id}"), "monitored": true});
        if let (Some(m), Some(a)) = (m.as_object_mut(), available) {
            m.insert("isAvailable".to_owned(), serde_json::Value::Bool(a));
        }
        m
    };
    let records = serde_json::json!([movie(1, Some(true)), movie(2, Some(false)), movie(3, None)]);
    mount_sonarr_wanted(&mock_server, records).await;
    mount_command(&mock_server, queued_command()).await;
    let mut run = default_run(&mock_server.uri());
    run.app = AppKind::Radarr as i32;
    some(run.policy.as_mut(), "policy")?.skip_unavailable_movies = skip_unavailable;
    run_short_polling(client, run).await;
    let requests = some(mock_server.received_requests().await, "recorded requests")?;
    let mut ids = Vec::new();
    for post in command_posts(&requests) {
        let body: serde_json::Value = serde_json::from_slice(&post.body)?;
        let searched = some(body.get("movieIds").and_then(|v| v.as_array()), "movieIds")?;
        ids.extend(searched.iter().filter_map(serde_json::Value::as_i64));
    }
    ids.sort_unstable();
    let asked = api
        .budget_calls
        .lock()
        .await
        .iter()
        .flat_map(|(items, _)| items.iter().map(|i| i.cost as usize))
        .sum();
    let detail = api
        .reported_events
        .lock()
        .await
        .iter()
        .find(|e| e.r#type == EventType::CandidatesFiltered as i32)
        .map(|e| e.title.clone())
        .unwrap_or_default();
    Ok((ids, asked, detail))
}

// Radarr minimum availability (#350): a movie Radarr reports as not yet available is neither
// searched nor charged, a movie without the flag is kept, and the skip is named in the run's
// events. Negative: with the switch off every movie is searched.
#[tokio::test]
async fn unavailable_movies_are_skipped_before_budget() -> TestResult {
    let (ids, asked, detail) = radarr_availability_run(true).await?;
    assert_eq!(ids, [1, 3]);
    assert_eq!(asked, 2, "the unavailable movie asks for no budget");
    assert!(
        detail.contains("1 skipped as not yet available"),
        "title: {detail}"
    );
    let (ids, asked, detail) = radarr_availability_run(false).await?;
    assert_eq!(ids, [1, 2, 3]);
    assert_eq!(asked, 3);
    assert!(
        detail.contains("0 skipped as not yet available"),
        "title: {detail}"
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
    mount_sonarr_default(&mock_server, episodes(&[(1, 10)])).await;

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
    mount_sonarr_wanted(
        &mock_server,
        episodes(&[(1, 10), (2, 11), (3, 12), (4, 13)]),
    )
    .await;

    mount_command(&mock_server, ResponseTemplate::new(500)).await;

    let mut run = default_run(&mock_server.uri());
    some(run.policy.as_mut(), "run policy")?.per_cycle = 4;

    run_short_polling(client, run).await;

    let requests = some(mock_server.received_requests().await, "recorded requests")?;
    let posts = command_posts(&requests);
    assert_eq!(
        posts.len(),
        9,
        "Should open circuit after exactly 3 consecutive failures (3 retries each)"
    );

    let events = api.reported_events.lock().await;
    assert!(
        has_circuit_open_event(&events),
        "Should report circuit open event"
    );
    assert!(
        events
            .iter()
            .any(|e| e.r#type == EventType::CircuitOpened as i32),
        "Should report CircuitOpened event type"
    );

    assert_single_completed_outcome(&api, RunOutcome::Failed).await?;
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
    let _run = run_sonarr_cycle(&api, episodes(&[(1, 10), (2, 11)]), Some(2)).await?;

    let events = api.reported_events.lock().await;
    let deferred = events
        .iter()
        .find(|e| e.level == Level::Warn as i32 && e.title.contains("deferred"));
    assert!(
        deferred.is_none(),
        "Should not warn about deferred targets if grant == requested"
    );
    assert_single_completed_outcome(&api, RunOutcome::Done).await?;
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
    mount_sonarr_wanted(
        &mock_server,
        episodes(&[(1, 10), (2, 11), (3, 12), (4, 13)]),
    )
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
    assert!(
        !has_circuit_open_event(&events),
        "Circuit should stay closed because 3rd attempt succeeded"
    );
    assert_single_completed_outcome(&api, RunOutcome::Done).await?;
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
    let _run = run_sonarr_cycle(&api, episodes(&[(1, 10), (2, 11)]), Some(2)).await?;

    let complete = api.completed_requests.lock().await;
    assert_eq!(complete.len(), 1, "Run should complete");
    assert_eq!(
        some(complete.first(), "a CompleteRun call")?.outcome,
        RunOutcome::Done as i32,
        "Run should be Done, not cancelled"
    );
    assert_eq!(
        some(complete.first(), "a CompleteRun call")?.searched.len(),
        2
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
    mount_sonarr_default(&mock_server, two_season_series()).await;

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
    let _run = run_sonarr_cycle(&api, episodes(&[(1, 10), (2, 10), (3, 11)]), Some(3)).await?;

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
    let _run = run_sonarr_cycle(&api, episodes(&[(1, 10), (2, 10), (3, 10)]), Some(3)).await?;

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
        complete_refusals: vec![tonic::Code::Unavailable; 3],
        ..FakeApi::default()
    };
    let (client, _) = start_fake_api(api).await?;
    let health = run_short_polling(client, default_run(&sonarr.uri())).await;
    // Every attempt of SHORT_RETRY is refused and counted.
    assert_eq!(health.failures(crate::health::Call::CompleteRun), 3);
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

async fn wait_until<F, Fut>(
    stop: &CancellationToken,
    task: tokio::task::JoinHandle<()>,
    mut check: F,
) -> TestResult
where
    F: FnMut() -> Fut,
    Fut: std::future::Future<Output = bool>,
{
    for _ in 0..500 {
        if check().await {
            break;
        }
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
    stop.cancel();
    tokio::time::timeout(Duration::from_secs(5), task).await??;
    Ok(())
}

/// Waits up to 5 s for `done`, then stops the task behind `stop`.
async fn until(
    health: &crate::health::Health,
    done: impl Fn(&crate::health::Health) -> bool,
    stop: &CancellationToken,
    task: tokio::task::JoinHandle<()>,
) -> TestResult {
    wait_until(stop, task, || std::future::ready(done(health))).await
}

struct LeaseLoopHandle {
    health: Arc<crate::health::Health>,
    stop: CancellationToken,
    task: tokio::task::JoinHandle<()>,
}

async fn spawn_test_lease_loop(
    api: FakeApi,
) -> Result<LeaseLoopHandle, Box<dyn std::error::Error>> {
    let (client, _) = start_fake_api(api).await?;
    let health = Arc::new(crate::health::Health::new(Duration::from_secs(180)));
    let stop = CancellationToken::new();
    let task = tokio::spawn(crate::lease_loop(
        loop_config(),
        client,
        health.clone(),
        stop.clone(),
    ));
    Ok(LeaseLoopHandle { health, stop, task })
}

async fn spawn_test_heartbeat(api: FakeApi) -> Result<LeaseLoopHandle, Box<dyn std::error::Error>> {
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
    Ok(LeaseLoopHandle { health, stop, task })
}

#[tokio::test]
async fn refused_lease_is_counted_and_is_no_contact() -> TestResult {
    let handle = spawn_test_lease_loop(FakeApi::default()).await?;
    until(
        &handle.health,
        |h| h.failures(crate::health::Call::LeaseRun) > 0,
        &handle.stop,
        handle.task,
    )
    .await?;
    assert!(handle.health.failures(crate::health::Call::LeaseRun) >= 1);
    assert!(
        !handle.health.contacted(),
        "a refused lease must not refresh /livez"
    );
    Ok(())
}

#[tokio::test]
async fn answered_lease_records_contact() -> TestResult {
    let handle = spawn_test_lease_loop(FakeApi {
        lease_empty: true,
        ..FakeApi::default()
    })
    .await?;
    until(
        &handle.health,
        crate::health::Health::contacted,
        &handle.stop,
        handle.task,
    )
    .await?;
    assert!(handle.health.contacted());
    assert_eq!(handle.health.failures(crate::health::Call::LeaseRun), 0);
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
    let handle = spawn_test_lease_loop(api).await?;
    wait_until(&handle.stop, handle.task, || {
        let comp = completed.clone();
        async move { !comp.lock().await.is_empty() }
    })
    .await?;
    let guard = completed.lock().await;
    assert_eq!(guard.first().map(|c| c.run_id.as_str()), Some("test_run"));
    assert_eq!(handle.health.failures(crate::health::Call::CompleteRun), 0);
    Ok(())
}

#[tokio::test]
async fn heartbeat_failure_is_counted_and_success_is_contact() -> TestResult {
    for (fail, want_failures, want_contact) in [(true, true, false), (false, false, true)] {
        let api = FakeApi {
            fail_heartbeat: fail,
            ..FakeApi::default()
        };
        let handle = spawn_test_heartbeat(api).await?;
        until(
            &handle.health,
            |h| h.contacted() || h.failures(crate::health::Call::Heartbeat) > 0,
            &handle.stop,
            handle.task,
        )
        .await?;
        assert_eq!(
            handle.health.failures(crate::health::Call::Heartbeat) > 0,
            want_failures,
            "fail={fail}"
        );
        assert_eq!(handle.health.contacted(), want_contact, "fail={fail}");
    }
    Ok(())
}

fn many_episodes(count: i64) -> serde_json::Value {
    let rows: Vec<serde_json::Value> = (1..=count)
        .map(|id| {
            serde_json::json!({
                "id": id, "seriesId": id, "seasonNumber": 1, "episodeNumber": 1,
                "title": format!("Episode {id}"), "monitored": true, "airDateUtc": "2020-01-01T00:00:00Z",
                "series": { "monitored": true, "tags": [], "qualityProfileId": 1 }
            })
        })
        .collect();
    serde_json::Value::Array(rows)
}

/// One run over `count` wanted episodes with random selection; returns the fake api and the
/// *arr mock so the test can read what was reported and requested.
async fn seeded_run(
    count: i64,
    seed: u64,
    dry_run: bool,
    per_cycle: u32,
) -> Result<(FakeApi, MockServer), Box<dyn std::error::Error>> {
    let api = FakeApi {
        filter_unprocessed: (1..=count).collect(),
        budget_granted: 100,
        ..FakeApi::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let mock_server = MockServer::start().await;
    mount_sonarr_default(&mock_server, many_episodes(count)).await;
    let mut run = default_run(&mock_server.uri());
    run.dry_run = dry_run;
    run.seed = seed;
    let pol = some(run.policy.as_mut(), "policy")?;
    pol.selection = snatch_proto::Selection::Random as i32;
    pol.per_cycle = per_cycle;
    pol.cursor = "incoming_cursor_42".to_owned();
    run_short_polling(client, run).await;
    Ok((api, mock_server))
}

/// Entity ids of the reported events of type `ty`, in report order.
async fn ids_of(api: &FakeApi, ty: EventType) -> Vec<i64> {
    let events = api.reported_events.lock().await;
    events
        .iter()
        .filter(|e| e.r#type == ty as i32)
        .map(|e| e.entity_id)
        .collect()
}

#[tokio::test]
async fn dry_run_preview_matches_real_run_targets_with_same_seed_and_never_calls_search()
-> TestResult {
    let (api_real, _) = seeded_run(10, 4242, false, 4).await?;
    let dispatched_ids = ids_of(&api_real, EventType::SearchDispatched).await;
    assert_eq!(dispatched_ids.len(), 4);

    let (api_dry, mock_dry) = seeded_run(10, 4242, true, 4).await?;
    let previewed_ids = ids_of(&api_dry, EventType::SearchPreviewed).await;
    assert_eq!(
        previewed_ids, dispatched_ids,
        "same seed must yield exact same targets in order"
    );
    let dry_requests = some(mock_dry.received_requests().await, "dry requests")?;
    assert_eq!(
        search_posts(&dry_requests),
        0,
        "dry run must never call search endpoint"
    );

    let completed = api_dry.completed_requests.lock().await;
    assert_eq!(completed.len(), 1);
    let comp = some(completed.first(), "completed request")?;
    assert_eq!(comp.outcome, RunOutcome::Done as i32);
    assert!(
        comp.searched.is_empty(),
        "dry run searched list must be empty"
    );
    assert_eq!(
        comp.cursor, "incoming_cursor_42",
        "dry run must return incoming cursor unchanged"
    );
    Ok(())
}

#[tokio::test]
async fn different_seed_differs_under_random_selection() -> TestResult {
    let (api_1, _) = seeded_run(20, 101, true, 5).await?;
    let (api_2, _) = seeded_run(20, 999, true, 5).await?;
    let ids_1 = ids_of(&api_1, EventType::SearchPreviewed).await;
    let ids_2 = ids_of(&api_2, EventType::SearchPreviewed).await;
    assert_eq!(ids_1.len(), 5);
    assert_eq!(ids_2.len(), 5);
    assert_ne!(
        ids_1, ids_2,
        "different seeds must produce different random selections"
    );
    Ok(())
}

#[tokio::test]
async fn seed_zero_uses_os_entropy_and_completes_dry_run() -> TestResult {
    let (api, mock_server) = seeded_run(5, 0, true, 3).await?;
    let previewed_ids = ids_of(&api, EventType::SearchPreviewed).await;
    assert_eq!(
        previewed_ids.len(),
        3,
        "should preview 3 targets with seed 0"
    );
    let dry_requests = some(mock_server.received_requests().await, "dry requests")?;
    assert_eq!(
        search_posts(&dry_requests),
        0,
        "seed 0 dry run must never call search endpoint"
    );
    let completed = api.completed_requests.lock().await;
    assert_eq!(completed.len(), 1);
    assert_eq!(
        some(completed.first(), "completed request")?.outcome,
        RunOutcome::Done as i32
    );
    Ok(())
}

/// Positive, negative and boundary: previewed costs sum to the target's stamina charge.
#[test]
fn preview_costs_sum_to_the_query_cost() {
    let cases = [
        (
            Target::Series {
                series_id: 1,
                seasons: 3,
                items: vec![1, 2, 3, 4],
            },
            vec![(1, 3), (2, 0), (3, 0), (4, 0)],
        ),
        (
            Target::Episodes {
                series_id: 1,
                items: vec![5, 6],
            },
            vec![(5, 1), (6, 1)],
        ),
        (
            Target::Season {
                series_id: 2,
                season: 1,
                items: vec![7],
            },
            vec![(7, 1)],
        ),
    ];
    for (target, want) in cases {
        let got = preview_costs(&target);
        assert_eq!(got, want, "{target:?}");
        let sum: u32 = got.iter().map(|(_, c)| c).sum();
        assert_eq!(sum, target.query_cost(), "{target:?}");
    }
}
