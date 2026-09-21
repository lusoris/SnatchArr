// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Executes one leased run: page the wanted list, filter, ask the API which ids are
//! unprocessed, select, group into search targets, acquire budget, dispatch, report.

use std::collections::HashMap;
use std::time::Duration;

use arr_client::{ArrClient, CommandStatus, Kind, Options, RadarrRelease, Wanted, WantedPage};
use rand::SeedableRng;
use rand::rngs::SmallRng;
use snatch_core::{Candidate, Filter, LidarrMode, PagePlan, Selection, SonarrMode, Target};
use snatch_proto::worker_service_client::WorkerServiceClient;
use snatch_proto::{
    AcquireBudgetRequest, AppKind, CompleteRunRequest, EventType, FilterCandidatesRequest, Level,
    Policy, ReportEventsRequest, Run, RunOutcome, SearchedItem, SnatchEvent, SnatchKind,
};
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::Channel;
use tonic::{Request, Status};

use crate::auth::AuthInterceptor;

/// Client type with the auth interceptor applied.
pub type Client = WorkerServiceClient<InterceptedService<Channel, AuthInterceptor>>;

/// Bounds (HISS-02).
const MAX_COMMAND_POLLS: u32 = 150;
const COMMAND_POLL_INTERVAL: Duration = Duration::from_secs(2);
const MAX_EVENTS_BUFFER: usize = 512;

/// Execution errors.
#[derive(Debug, thiserror::Error)]
pub enum ExecError {
    /// API RPC failed.
    #[error("api: {0}")]
    Rpc(#[from] Status),
    /// *arr call failed.
    #[error(transparent)]
    Arr(#[from] arr_client::Error),
    /// Lease payload was inconsistent.
    #[error("lease: {0}")]
    Lease(String),
}

/// What the run produced.
#[derive(Debug, Default)]
pub struct Outcome {
    /// Items whose search command was accepted.
    pub searched: Vec<SearchedItem>,
    /// Sequential cursor to persist.
    pub cursor: String,
    /// Set when the run was cut short (circuit breaker); the items already searched still
    /// count, but the run is reported as failed so the API backs the instance off.
    pub error: Option<String>,
}

/// Consecutive search-command failures that open the circuit (HISS-02 bound on retries).
const MAX_CONSECUTIVE_FAILURES: u32 = 3;

/// One event to report; a small struct keeps call sites short.
struct Ev {
    level: Level,
    ty: EventType,
    entity_type: &'static str,
    entity_id: i64,
    title: String,
    detail: String,
}

impl Ev {
    fn run(level: Level, ty: EventType, title: impl Into<String>) -> Self {
        Self {
            level,
            ty,
            entity_type: "",
            entity_id: 0,
            title: title.into(),
            detail: String::new(),
        }
    }

    fn detail(mut self, detail: impl Into<String>) -> Self {
        self.detail = detail.into();
        self
    }

    fn entity(mut self, entity_type: &'static str, id: i64) -> Self {
        self.entity_type = entity_type;
        self.entity_id = id;
        self
    }
}

struct Events {
    tx: mpsc::Sender<ReportEventsRequest>,
    run_id: String,
}

impl Events {
    async fn emit(&self, e: Ev) {
        let ev = SnatchEvent {
            run_id: self.run_id.clone(),
            ts_unix_ms: chrono::Utc::now().timestamp_millis(),
            level: e.level.into(),
            r#type: e.ty.into(),
            entity_type: e.entity_type.to_owned(),
            entity_id: e.entity_id,
            title: e.title,
            detail: e.detail,
        };
        if self
            .tx
            .send(ReportEventsRequest { event: Some(ev) })
            .await
            .is_err()
        {
            tracing::warn!(run = %self.run_id, "event channel closed; dropping event");
        }
    }
}

/// Everything derived from the lease before any I/O.
struct Prepared {
    kind: Kind,
    wanted: Wanted,
    policy: Policy,
    arr: ArrClient,
    page_size: u32,
}

fn kind_of(app: i32) -> Result<Kind, ExecError> {
    let app =
        AppKind::try_from(app).map_err(|_| ExecError::Lease(format!("unknown app kind {app}")))?;
    match app {
        AppKind::Sonarr => Ok(Kind::Sonarr),
        AppKind::Radarr => Ok(Kind::Radarr),
        AppKind::Lidarr => Ok(Kind::Lidarr),
        AppKind::Readarr => Ok(Kind::Readarr),
        AppKind::WhisparrV2 => Ok(Kind::WhisparrV2),
        AppKind::WhisparrV3 => Ok(Kind::WhisparrV3),
        AppKind::Unspecified => Err(ExecError::Lease("app kind unspecified".to_owned())),
    }
}

fn wanted_of(snatch: i32) -> Result<Wanted, ExecError> {
    let snatch = SnatchKind::try_from(snatch)
        .map_err(|_| ExecError::Lease(format!("unknown snatch kind {snatch}")))?;
    match snatch {
        SnatchKind::Missing => Ok(Wanted::Missing),
        SnatchKind::Upgrade => Ok(Wanted::Cutoff),
        SnatchKind::Unspecified => Err(ExecError::Lease("snatch kind unspecified".to_owned())),
    }
}

fn selection_of(p: &Policy) -> Selection {
    match p.selection() {
        snatch_proto::Selection::Sequential => Selection::Sequential,
        snatch_proto::Selection::Recent => Selection::Recent,
        snatch_proto::Selection::Random | snatch_proto::Selection::Unspecified => Selection::Random,
    }
}

fn release_of(p: &Policy) -> RadarrRelease {
    match p.radarr_release_type() {
        snatch_proto::RadarrReleaseType::Digital => RadarrRelease::Digital,
        snatch_proto::RadarrReleaseType::Cinema => RadarrRelease::Cinema,
        snatch_proto::RadarrReleaseType::Physical
        | snatch_proto::RadarrReleaseType::Unspecified => RadarrRelease::Physical,
    }
}

fn sonarr_mode(p: &Policy) -> SonarrMode {
    match p.sonarr_mode() {
        snatch_proto::SonarrMode::SeasonPacks => SonarrMode::SeasonPacks,
        snatch_proto::SonarrMode::Shows => SonarrMode::Shows,
        snatch_proto::SonarrMode::Episodes | snatch_proto::SonarrMode::Unspecified => {
            SonarrMode::Episodes
        }
    }
}

fn lidarr_mode(p: &Policy) -> LidarrMode {
    match p.lidarr_mode() {
        snatch_proto::LidarrMode::Album => LidarrMode::Album,
        snatch_proto::LidarrMode::Artist | snatch_proto::LidarrMode::Unspecified => {
            LidarrMode::Artist
        }
    }
}

fn group(kind: Kind, p: &Policy, selected: &[Candidate]) -> Vec<Target> {
    match kind {
        Kind::Sonarr | Kind::WhisparrV2 => snatch_core::group_sonarr(selected, sonarr_mode(p)),
        Kind::Lidarr => snatch_core::group_lidarr(selected, lidarr_mode(p)),
        Kind::Readarr => snatch_core::group_readarr(selected),
        Kind::Radarr | Kind::WhisparrV3 => snatch_core::group_flat(selected),
    }
}

fn prepare(run: &Run, arr_timeout: Duration) -> Result<Prepared, ExecError> {
    let kind = kind_of(run.app)?;
    let wanted = wanted_of(run.snatch)?;
    let policy = run
        .policy
        .clone()
        .ok_or_else(|| ExecError::Lease("policy missing".to_owned()))?;
    let opts = Options {
        timeout: arr_timeout,
        user_agent: run.user_agent.clone(),
        ..Options::default()
    };
    let arr = ArrClient::new(kind, &run.base_url, &run.api_key, opts)?;
    let page_size = policy.page_size.clamp(10, 1000);
    Ok(Prepared {
        kind,
        wanted,
        policy,
        arr,
        page_size,
    })
}

/// Runs one lease to completion and reports the outcome to the API.
pub async fn execute(mut client: Client, run: Run, arr_timeout: Duration, worker_id: &str) {
    let (tx, rx) = mpsc::channel::<ReportEventsRequest>(MAX_EVENTS_BUFFER);
    let mut reporter = client.clone();
    let report = tokio::spawn(async move {
        reporter
            .report_events(Request::new(ReceiverStream::new(rx)))
            .await
    });
    let events = Events {
        tx,
        run_id: run.run_id.clone(),
    };
    let (outcome, searched, cursor, error) =
        match snatch(&mut client, &run, arr_timeout, &events).await {
            Ok(Outcome {
                searched,
                cursor,
                error: None,
            }) => (RunOutcome::Done, searched, cursor, String::new()),
            Ok(Outcome {
                searched,
                cursor,
                error: Some(e),
            }) => {
                tracing::warn!(run = %run.run_id, error = %e, "run cut short");
                (RunOutcome::Failed, searched, cursor, e)
            }
            Err(e) => {
                tracing::warn!(run = %run.run_id, error = %e, "run failed");
                events
                    .emit(
                        Ev::run(Level::Error, EventType::RunFinished, "snatch failed")
                            .detail(e.to_string()),
                    )
                    .await;
                (RunOutcome::Failed, Vec::new(), String::new(), e.to_string())
            }
        };
    drop(events);
    match report.await {
        Ok(Ok(resp)) => tracing::debug!(accepted = resp.into_inner().accepted, "events reported"),
        Ok(Err(status)) => tracing::warn!(%status, "event stream rejected"),
        Err(join) => tracing::warn!(error = %join, "event reporter panicked"),
    }
    let req = CompleteRunRequest {
        run_id: run.run_id.clone(),
        worker_id: worker_id.to_owned(),
        outcome: outcome.into(),
        searched,
        cursor,
        error,
    };
    if let Err(status) = client.complete_run(Request::new(req)).await {
        tracing::error!(run = %run.run_id, %status, "CompleteRun failed; lease will expire");
    }
}

async fn snatch(
    client: &mut Client,
    run: &Run,
    arr_timeout: Duration,
    events: &Events,
) -> Result<Outcome, ExecError> {
    let p = prepare(run, arr_timeout)?;
    events
        .emit(Ev::run(
            Level::Info,
            EventType::RunStarted,
            format!("{:?} snatch started", p.wanted),
        ))
        .await;
    let mut rng = SmallRng::from_rng(&mut rand::rng());
    let (plan, first) = plan_pages(&p, &mut rng).await?;
    let msg = format!(
        "{} wanted item(s), fetching {} page(s)",
        first.total_records,
        plan.pages.len()
    );
    events
        .emit(Ev::run(Level::Debug, EventType::PageFetched, msg))
        .await;
    let (keep, busy) = collect_candidates(client, &p, run, &plan.pages, first).await?;
    let msg = format!(
        "{} candidate(s) after filters and afterglow; {busy} skipped as queued or grabbed within {} h",
        keep.len(),
        p.policy.recent_grab_hours
    );
    events
        .emit(Ev::run(Level::Debug, EventType::CandidatesFiltered, msg))
        .await;
    let selected = snatch_core::select(
        &keep,
        p.policy.per_cycle as usize,
        selection_of(&p.policy),
        &mut rng,
    );
    let targets = group(p.kind, &p.policy, &selected);
    let dispatch = acquire(client, run, targets, events).await?;
    let titles: HashMap<i64, String> = selected.iter().map(|c| (c.id, c.title.clone())).collect();
    let mut outcome = Outcome {
        searched: Vec::new(),
        cursor: plan.next_cursor.to_string(),
        error: None,
    };
    dispatch_all(&p, &dispatch, &titles, events, &mut outcome).await;
    Ok(outcome)
}

/// Dispatches every target, opening the circuit after MAX_CONSECUTIVE_FAILURES failed
/// commands in a row: a dead indexer or instance must not burn the rest of the stamina.
async fn dispatch_all(
    p: &Prepared,
    dispatch: &[Target],
    titles: &HashMap<i64, String>,
    events: &Events,
    outcome: &mut Outcome,
) {
    let mut failures: u32 = 0;
    for target in dispatch {
        if dispatch_target(p, target, titles, events, outcome).await {
            failures = 0;
            continue;
        }
        failures = failures.saturating_add(1);
        if failures >= MAX_CONSECUTIVE_FAILURES {
            let msg = format!("circuit open after {failures} consecutive search failures");
            events
                .emit(Ev::run(Level::Error, EventType::RunFinished, msg.clone()))
                .await;
            outcome.error = Some(msg);
            return;
        }
    }
}

async fn plan_pages(p: &Prepared, rng: &mut SmallRng) -> Result<(PagePlan, WantedPage), ExecError> {
    let first = p
        .arr
        .wanted_page(p.wanted, 1, p.page_size, release_of(&p.policy))
        .await?;
    let cursor: u32 = p.policy.cursor.parse().unwrap_or(1);
    let plan = PagePlan::plan(
        first.total_records,
        p.page_size,
        p.policy.per_cycle,
        selection_of(&p.policy),
        cursor,
        rng,
    );
    Ok((plan, first))
}

/// Fetches the planned pages, applies the local filters, drops what is already queued or
/// freshly grabbed, and asks the API which ids are not in afterglow. Returns the
/// candidates and how many were skipped as busy.
async fn collect_candidates(
    client: &mut Client,
    p: &Prepared,
    run: &Run,
    pages: &[u32],
    first: WantedPage,
) -> Result<(Vec<Candidate>, usize), ExecError> {
    let mut candidates = Vec::new();
    for page in pages {
        let fetched = if *page == 1 {
            first.clone()
        } else {
            p.arr
                .wanted_page(p.wanted, *page, p.page_size, release_of(&p.policy))
                .await?
        };
        candidates.extend(fetched.records);
    }
    let filter = Filter {
        monitored_only: p.policy.monitored_only,
        skip_future: p.policy.skip_future_releases,
        now_unix: chrono::Utc::now().timestamp(),
    };
    let filtered = snatch_core::filter(&candidates, &filter);
    let busy = p.arr.busy_ids(p.policy.recent_grab_hours).await?;
    let before = filtered.len();
    let filtered: Vec<Candidate> = filtered
        .into_iter()
        .filter(|c| busy.binary_search(&c.id).is_err())
        .collect();
    let skipped = before.saturating_sub(filtered.len());
    let req = FilterCandidatesRequest {
        run_id: run.run_id.clone(),
        entity_type: p.kind.entity_type().to_owned(),
        entity_ids: filtered.iter().map(|c| c.id).collect(),
    };
    let unprocessed = client
        .filter_candidates(Request::new(req))
        .await?
        .into_inner()
        .unprocessed_ids;
    let keep = filtered
        .into_iter()
        .filter(|c| unprocessed.contains(&c.id))
        .collect();
    Ok((keep, skipped))
}

/// Asks the API for budget and trims the targets to what was granted.
async fn acquire(
    client: &mut Client,
    run: &Run,
    targets: Vec<Target>,
    events: &Events,
) -> Result<Vec<Target>, ExecError> {
    let requested: u32 = targets.iter().map(Target::query_cost).sum();
    let req = AcquireBudgetRequest {
        run_id: run.run_id.clone(),
        requested,
    };
    let grant = client.acquire_budget(Request::new(req)).await?.into_inner();
    let msg = format!(
        "stamina: {} of {} indexer queries granted ({} left this hour)",
        grant.granted, requested, grant.remaining_in_window
    );
    events
        .emit(Ev::run(Level::Info, EventType::BudgetAcquired, msg))
        .await;
    let (dispatch, deferred) = snatch_core::cap(targets, grant.granted);
    if !deferred.is_empty() {
        let msg = format!(
            "{} target(s) deferred: out of stamina this hour",
            deferred.len()
        );
        events
            .emit(Ev::run(Level::Warn, EventType::BudgetAcquired, msg))
            .await;
    }
    Ok(dispatch)
}

/// Dispatches one target; returns whether the *arr accepted the search command.
async fn dispatch_target(
    p: &Prepared,
    target: &Target,
    titles: &HashMap<i64, String>,
    events: &Events,
    outcome: &mut Outcome,
) -> bool {
    let entity_type = p.kind.entity_type();
    let cmd = match p.arr.search(target).await {
        Ok(cmd) => cmd,
        Err(e) => {
            for id in target.items() {
                let title = titles.get(id).cloned().unwrap_or_default();
                let ev =
                    Ev::run(Level::Warn, EventType::SearchFailed, title).entity(entity_type, *id);
                events.emit(ev.detail(e.to_string())).await;
            }
            return false;
        }
    };
    for id in target.items() {
        let title = titles.get(id).cloned().unwrap_or_default();
        let ev = Ev::run(Level::Info, EventType::SearchDispatched, title.clone())
            .entity(entity_type, *id);
        events.emit(ev.detail(format!("command {}", cmd.id))).await;
        outcome.searched.push(SearchedItem {
            entity_type: entity_type.to_owned(),
            entity_id: *id,
            title,
        });
    }
    if p.policy.await_command {
        await_command(p, cmd, events).await;
    }
    true
}

/// Polls a command until it reaches a terminal state or the bounded poll budget ends.
async fn await_command(p: &Prepared, mut cmd: CommandStatus, events: &Events) {
    let mut polls: u32 = 0;
    while !cmd.is_terminal() && polls < MAX_COMMAND_POLLS {
        polls = polls.saturating_add(1);
        tokio::time::sleep(COMMAND_POLL_INTERVAL).await;
        match p.arr.command_status(cmd.id).await {
            Ok(c) => cmd = c,
            Err(e) => {
                tracing::debug!(error = %e, "command poll failed");
                break;
            }
        }
    }
    let ty = if cmd.status == "completed" {
        EventType::SearchCompleted
    } else {
        EventType::SearchFailed
    };
    events
        .emit(Ev::run(
            Level::Debug,
            ty,
            format!("command {} {}", cmd.id, cmd.status),
        ))
        .await;
}
