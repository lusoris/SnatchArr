// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Executes one leased run: page the wanted list, filter, ask the API which ids are
//! unprocessed, select, group into search targets, acquire budget, dispatch, report.

use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

use arr_client::{ArrClient, CommandStatus, Kind, Options, RadarrRelease, Wanted, WantedPage};
use rand::rngs::SmallRng;
use rand::{RngExt, SeedableRng};
use snatch_core::{Candidate, Filter, IdRule, LidarrMode, PagePlan, Selection, SonarrMode, Target};
use snatch_proto::worker_service_client::WorkerServiceClient;
use snatch_proto::{
    AcquireBudgetRequest, AppKind, CompleteRunRequest, EventType, FilterCandidatesRequest, Level,
    Policy, ReportEventsRequest, Run, RunOutcome, SearchedItem, SnatchEvent, SnatchKind,
};
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::Channel;
use tonic::{Code, Request, Status};

use crate::auth::AuthInterceptor;
use crate::health::{Call, Health};
use crate::resend;

/// Client type with the auth interceptor applied.
pub type Client = WorkerServiceClient<InterceptedService<Channel, AuthInterceptor>>;

/// Bounds (HISS-02).
const MAX_COMMAND_POLLS: u32 = 150;
const COMMAND_POLL_INTERVAL: Duration = Duration::from_secs(2);
const MAX_EVENTS_BUFFER: usize = 512;
const COMPLETE_RETRY: Retry = Retry {
    attempts: 3,
    backoff: Duration::from_secs(1),
};

/// How long a dispatched search command is polled for a terminal state: at most `max` polls,
/// `interval` apart. Production is MAX_COMMAND_POLLS x COMMAND_POLL_INTERVAL (5 minutes); tests
/// pass a short budget through [`execute_with`].
#[derive(Clone, Copy)]
pub(crate) struct Polling {
    pub max: u32,
    pub interval: Duration,
}

/// How a refused `CompleteRun` is retried: `attempts` calls at most, the wait before the n-th
/// retry a random time between half and all of `backoff` x 2^(n-1) (HISS-02). Production is
/// COMPLETE_RETRY; tests pass a short one through [`execute_with`].
#[derive(Clone, Copy)]
pub(crate) struct Retry {
    pub attempts: u32,
    pub backoff: Duration,
}

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
    /// Filtered pool before the per-cycle limit and hourly budget.
    pub eligible: Option<u32>,
    /// Sequential cursor to persist.
    pub cursor: String,
    /// Set when the run was cut short (circuit breaker); the items already searched still
    /// count, but the run is reported as failed so the API backs the instance off.
    pub error: Option<String>,
}

impl Outcome {
    /// An outcome with nothing searched yet that persists `cursor`.
    fn at(cursor: String, eligible: u32) -> Self {
        Self {
            cursor,
            eligible: Some(eligible),
            ..Self::default()
        }
    }
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
    /// Every event emitted, dropped ones included; CompleteRun reports those not accepted.
    /// A u64 so the count cannot wrap within any run's lifetime.
    emitted: AtomicU64,
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
        self.emitted.fetch_add(1, Ordering::Relaxed);
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

/// A run limited to one entity (movie) or group (series): a quickie for a Seerr request.
#[derive(Debug, Clone, Copy)]
struct Focus {
    entity: Option<i64>,
    group: Option<i64>,
}

impl Focus {
    fn of(run: &Run) -> Option<Self> {
        let entity = (run.focus_entity_id > 0).then_some(run.focus_entity_id);
        let group = (run.focus_group_id > 0).then_some(run.focus_group_id);
        (entity.is_some() || group.is_some()).then_some(Self { entity, group })
    }

    fn matches(self, c: &Candidate) -> bool {
        self.entity == Some(c.id) || self.group == Some(c.group)
    }
}

/// Page size a focused run sweeps the wanted list with.
const FOCUS_PAGE_SIZE: u32 = 500;

/// What candidate collection hands to selection.
struct Collected {
    keep: Vec<Candidate>,
    busy: usize,
    /// Dropped only because the app reports them as not yet available (#350).
    unavailable: usize,
    priority_ids: Vec<i64>,
    priority_groups: Vec<i64>,
}

/// Everything derived from the lease before any I/O.
struct Prepared {
    kind: Kind,
    wanted: Wanted,
    policy: Policy,
    arr: ArrClient,
    page_size: u32,
    focus: Option<Focus>,
    polling: Polling,
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

/// The candidate filter of a policy at `now_unix`.
fn filter_of(p: &Policy, now_unix: i64) -> Filter {
    Filter {
        monitored_only: p.monitored_only,
        skip_future: p.skip_future_releases,
        release_delay_secs: i64::from(p.release_delay_hours).saturating_mul(3600),
        now_unix,
        tags: IdRule {
            include: p.include_tags.clone(),
            exclude: p.exclude_tags.clone(),
        },
        profiles: IdRule {
            include: p.include_quality_profiles.clone(),
            exclude: p.exclude_quality_profiles.clone(),
        },
        skip_unavailable: p.skip_unavailable_movies,
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

fn prepare(
    run: &Run,
    arr_timeout: Duration,
    polling: Polling,
    health: &Health,
) -> Result<Prepared, ExecError> {
    let kind = kind_of(run.app)?;
    let wanted = wanted_of(run.snatch)?;
    let policy = run
        .policy
        .clone()
        .ok_or_else(|| ExecError::Lease("policy missing".to_owned()))?;
    let opts = Options {
        timeout: arr_timeout,
        user_agent: run.user_agent.clone(),
        observer: Some(std::sync::Arc::new(health.clone())),
        ..Options::default()
    };
    let arr = ArrClient::new(kind, &run.base_url, &run.api_key, opts)?;
    let focus = Focus::of(run);
    let page_size = if focus.is_some() {
        FOCUS_PAGE_SIZE
    } else {
        policy.page_size.clamp(10, 1000)
    };
    Ok(Prepared {
        kind,
        wanted,
        policy,
        arr,
        page_size,
        focus,
        polling,
    })
}

/// Runs one lease to completion and reports the outcome to the API.
pub async fn execute(
    client: Client,
    run: Run,
    arr_timeout: Duration,
    worker_id: &str,
    health: &Health,
) {
    let polling = Polling {
        max: MAX_COMMAND_POLLS,
        interval: COMMAND_POLL_INTERVAL,
    };
    execute_with(
        client,
        run,
        arr_timeout,
        worker_id,
        (polling, COMPLETE_RETRY),
        health,
    )
    .await
}

/// [`execute`] with an explicit command-polling budget and `CompleteRun` retry.
pub(crate) async fn execute_with(
    mut client: Client,
    run: Run,
    arr_timeout: Duration,
    worker_id: &str,
    (polling, retry): (Polling, Retry),
    health: &Health,
) {
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
        emitted: AtomicU64::new(0),
    };
    let run_start = std::time::Instant::now();
    let (outcome, searched, cursor, error, eligible_items) =
        handle_snatch(&mut client, &run, arr_timeout, &events, polling, health).await;
    record_run_metrics(health, run.snatch, outcome, run_start.elapsed());
    let emitted = u32::try_from(events.emitted.load(Ordering::Relaxed)).unwrap_or(u32::MAX);
    drop(events);
    let events_unconfirmed = emitted.saturating_sub(record_report(report.await, health));
    if events_unconfirmed > 0 {
        tracing::warn!(run = %run.run_id, emitted, events_unconfirmed, "events not confirmed; the run's history may be incomplete");
    }
    let req = CompleteRunRequest {
        run_id: run.run_id.clone(),
        worker_id: worker_id.to_owned(),
        outcome: outcome.into(),
        searched,
        cursor,
        error,
        events_unconfirmed,
        eligible_items,
    };
    complete(&mut client, &req, retry, health).await;
}

fn record_run_metrics(health: &Health, snatch_kind: i32, outcome: RunOutcome, duration: Duration) {
    let outcome_str = match outcome {
        RunOutcome::Done => "done",
        RunOutcome::Failed => "failed",
        RunOutcome::Cancelled => "cancelled",
        RunOutcome::Unspecified => "unspecified",
    };
    let kind_str = match snatch_proto::SnatchKind::try_from(snatch_kind) {
        Ok(snatch_proto::SnatchKind::Upgrade) => "upgrade",
        _ => "missing",
    };
    health.record_run_duration(kind_str, outcome_str, duration);
}

async fn handle_snatch(
    client: &mut Client,
    run: &Run,
    arr_timeout: Duration,
    events: &Events,
    polling: Polling,
    health: &Health,
) -> (RunOutcome, Vec<SearchedItem>, String, String, Option<u32>) {
    match snatch(client, run, arr_timeout, events, polling, health).await {
        Ok(Outcome {
            searched,
            cursor,
            eligible,
            error: None,
        }) => (RunOutcome::Done, searched, cursor, String::new(), eligible),
        Ok(Outcome {
            searched,
            cursor,
            eligible,
            error: Some(e),
        }) => {
            tracing::warn!(run = %run.run_id, error = %e, "run cut short");
            (RunOutcome::Failed, searched, cursor, e, eligible)
        }
        Err(e) => {
            tracing::warn!(run = %run.run_id, error = %e, "run failed");
            events
                .emit(
                    Ev::run(Level::Error, EventType::RunFinished, "snatch failed")
                        .detail(e.to_string()),
                )
                .await;
            (
                RunOutcome::Failed,
                Vec::new(),
                String::new(),
                e.to_string(),
                None,
            )
        }
    }
}

/// Calls `CompleteRun`, retrying a transient refusal within `retry`. Every refused call counts
/// as a failure; a run that still is not completed is failed by the API once its lease expires.
async fn complete(client: &mut Client, req: &CompleteRunRequest, retry: Retry, health: &Health) {
    let attempts = retry.attempts.max(1);
    for attempt in 1..=attempts {
        let sent = resend::once_more(|| {
            let mut c = client.clone();
            let req = req.clone();
            async move { c.complete_run(Request::new(req)).await }
        })
        .await;
        let status = match sent {
            Ok(_) => {
                health.contact();
                return;
            }
            Err(status) => status,
        };
        health.failure(Call::CompleteRun);
        if attempt == attempts || !transient(status.code()) {
            tracing::error!(run = %req.run_id, attempt, %status, "CompleteRun failed; lease will expire");
            return;
        }
        tracing::warn!(run = %req.run_id, attempt, %status, "CompleteRun failed; retrying");
        tokio::time::sleep(jitter(retry.wait(attempt))).await;
    }
}

impl Retry {
    /// The longest wait after the failed attempt `attempt` (from 1): `backoff` x 2^(attempt-1).
    fn wait(self, attempt: u32) -> Duration {
        let factor = 2u32.saturating_pow(attempt.saturating_sub(1));
        self.backoff.saturating_mul(factor)
    }
}

/// Codes a later call may get past: the API, or the path to it, briefly could not answer.
fn transient(code: Code) -> bool {
    matches!(
        code,
        Code::Unavailable | Code::DeadlineExceeded | Code::ResourceExhausted | Code::Aborted
    )
}

/// A random wait between half of `d` and `d`, so workers refused together do not retry together.
fn jitter(d: Duration) -> Duration {
    let half = d.checked_div(2).unwrap_or(Duration::ZERO);
    rand::rng().random_range(half..=d)
}

/// Records the end of the event stream: an answer is contact, a refusal or a panic a failure.
/// Returns the events the API accepted; none count as accepted when the stream failed.
fn record_report(
    joined: Result<
        Result<tonic::Response<snatch_proto::ReportEventsResponse>, Status>,
        tokio::task::JoinError,
    >,
    health: &Health,
) -> u32 {
    match joined {
        Ok(Ok(resp)) => {
            health.contact();
            let accepted = resp.into_inner().accepted;
            tracing::debug!(accepted, "events reported");
            accepted
        }
        Ok(Err(status)) => {
            health.failure(Call::ReportEvents);
            tracing::warn!(%status, "event stream rejected");
            0
        }
        Err(join) => {
            health.failure(Call::ReportEvents);
            tracing::warn!(error = %join, "event reporter panicked");
            0
        }
    }
}

async fn snatch(
    client: &mut Client,
    run: &Run,
    arr_timeout: Duration,
    events: &Events,
    polling: Polling,
    health: &Health,
) -> Result<Outcome, ExecError> {
    let p = prepare(run, arr_timeout, polling, health)?;
    let focused = if p.focus.is_some() {
        " (focused on one Seerr request)"
    } else {
        ""
    };
    events
        .emit(Ev::run(
            Level::Info,
            EventType::RunStarted,
            format!("{:?} snatch started{focused}", p.wanted),
        ))
        .await;
    let mut rng = seeded_rng(run.seed);
    let (plan, first) = plan_pages(&p, &mut rng).await?;
    let msg = format!(
        "{} wanted item(s), fetching {} page(s)",
        first.total_records,
        plan.pages.len()
    );
    events
        .emit(Ev::run(Level::Debug, EventType::PageFetched, msg))
        .await;
    let c = collect_candidates(client, &p, run, &plan.pages, first).await?;
    let eligible = u32::try_from(c.keep.len()).unwrap_or(u32::MAX);
    let msg = format!(
        "{} candidate(s) after filters and afterglow; {} skipped as queued or grabbed within {} h; {} skipped as not yet available; {} requested via Seerr",
        c.keep.len(),
        c.busy,
        p.policy.recent_grab_hours,
        c.unavailable,
        c.priority_ids.len().saturating_add(c.priority_groups.len())
    );
    events
        .emit(Ev::run(Level::Debug, EventType::CandidatesFiltered, msg))
        .await;
    let selected = snatch_core::select_prioritised(
        &c.keep,
        p.policy.per_cycle as usize,
        selection_of(&p.policy),
        &c.priority_ids,
        &c.priority_groups,
        &mut rng,
    );
    let targets = group(p.kind, &p.policy, &selected);
    let dispatch = acquire(client, run, targets, events).await?;
    let by_id: HashMap<i64, Candidate> = selected.iter().map(|c| (c.id, c.clone())).collect();
    if run.dry_run {
        preview_all(&p, &dispatch, &by_id, events).await;
        return Ok(Outcome::at(p.policy.cursor.clone(), eligible));
    }
    let mut outcome = Outcome::at(plan.next_cursor.to_string(), eligible);
    dispatch_all(&p, &dispatch, &by_id, events, &mut outcome, health).await;
    Ok(outcome)
}

/// A run's RNG: seeded when the run carries a seed, so a preview and the real run it previews
/// pick the same targets; from OS entropy when the seed is 0.
fn seeded_rng(seed: u64) -> SmallRng {
    if seed != 0 {
        SmallRng::seed_from_u64(seed)
    } else {
        SmallRng::from_rng(&mut rand::rng())
    }
}

/// Emits SEARCH_PREVIEWED events for each target granted in a dry-run / preview run.
async fn preview_all(
    p: &Prepared,
    dispatch: &[Target],
    by_id: &HashMap<i64, Candidate>,
    events: &Events,
) {
    let entity_type = p.kind.entity_type();
    for target in dispatch {
        for (id, cost) in preview_costs(target) {
            let title = by_id.get(&id).map(|c| c.title.clone()).unwrap_or_default();
            let ev = Ev::run(Level::Info, EventType::SearchPreviewed, title)
                .entity(entity_type, id)
                .detail(cost.to_string());
            events.emit(ev).await;
        }
    }
}

/// Pairs each item of a target with the stamina it adds. An atomic target is one search for
/// the whole parent, so its first item carries the charge and the rest 0: the previewed costs
/// sum to what a real run would spend.
fn preview_costs(target: &Target) -> Vec<(i64, u32)> {
    let atomic = target.atomic();
    target
        .items()
        .iter()
        .enumerate()
        .map(|(i, id)| {
            let cost = match (atomic, i) {
                (true, 0) => target.query_cost(),
                (true, _) => 0,
                (false, _) => 1,
            };
            (*id, cost)
        })
        .collect()
}

/// Dispatches every target, opening the circuit after MAX_CONSECUTIVE_FAILURES failed
/// commands in a row: a dead indexer or instance must not burn the rest of the stamina.
async fn dispatch_all(
    p: &Prepared,
    dispatch: &[Target],
    by_id: &HashMap<i64, Candidate>,
    events: &Events,
    outcome: &mut Outcome,
    health: &Health,
) {
    let mut failures: u32 = 0;
    for target in dispatch {
        if dispatch_target(p, target, by_id, events, outcome).await {
            failures = 0;
            continue;
        }
        failures = failures.saturating_add(1);
        if failures >= MAX_CONSECUTIVE_FAILURES {
            let msg = format!("circuit open after {failures} consecutive search failures");
            events
                .emit(Ev::run(Level::Warn, EventType::CircuitOpened, msg.clone()))
                .await;
            events
                .emit(Ev::run(Level::Error, EventType::RunFinished, msg.clone()))
                .await;
            outcome.error = Some(msg);
            let kind_str = match p.wanted {
                Wanted::Missing => "missing",
                Wanted::Cutoff => "upgrade",
            };
            health.circuit_opened(kind_str);
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
    let plan = if p.focus.is_some() {
        PagePlan::sweep(first.total_records, p.page_size)
    } else {
        PagePlan::plan(
            first.total_records,
            p.page_size,
            p.policy.per_cycle,
            selection_of(&p.policy),
            cursor,
            rng,
        )
    };
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
) -> Result<Collected, ExecError> {
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
    if let Some(focus) = p.focus {
        candidates.retain(|c| focus.matches(c));
    }
    let filter = filter_of(&p.policy, chrono::Utc::now().timestamp());
    let filtered = snatch_core::filter(&candidates, &filter);
    let unavailable = snatch_core::unavailable_count(&candidates, &filter);
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
    let resp = resend::once_more(|| {
        let mut c = client.clone();
        let req = req.clone();
        async move { c.filter_candidates(Request::new(req)).await }
    })
    .await?
    .into_inner();
    let keep = filtered
        .into_iter()
        .filter(|c| resp.unprocessed_ids.contains(&c.id))
        .collect();
    Ok(Collected {
        keep,
        busy: skipped,
        unavailable,
        priority_ids: resp.priority_ids,
        priority_groups: resp.priority_group_ids,
    })
}

/// Asks the API for stamina per target and keeps what was granted: a target searched item by
/// item may be granted in part, one command for a whole parent only whole (#142).
async fn acquire(
    client: &mut Client,
    run: &Run,
    targets: Vec<Target>,
    events: &Events,
) -> Result<Vec<Target>, ExecError> {
    let items: Vec<snatch_proto::BudgetItem> = targets
        .iter()
        .map(|t| snatch_proto::BudgetItem {
            cost: t.query_cost(),
            atomic: t.atomic(),
        })
        .collect();
    let requested: u32 = items.iter().map(|item| item.cost).sum();
    let req = AcquireBudgetRequest {
        run_id: run.run_id.clone(),
        requested,
        items,
    };
    let grant = resend::once_more(|| {
        let mut c = client.clone();
        let req = req.clone();
        async move { c.acquire_budget(Request::new(req)).await }
    })
    .await?
    .into_inner();
    let msg = format!(
        "stamina: {} of {} indexer searches granted ({} left in the last 60 minutes)",
        grant.granted, requested, grant.remaining_in_window
    );
    events
        .emit(Ev::run(Level::Info, EventType::BudgetAcquired, msg))
        .await;
    // An API from before #142 answers without per-target grants.
    let (dispatch, deferred) = if grant.granted_items.is_empty() {
        snatch_core::cap(targets, grant.granted)
    } else {
        snatch_core::apply_grants(targets, &grant.granted_items)
    };
    if !deferred.is_empty() {
        let msg = format!(
            "{} target(s) deferred: out of stamina for the last 60 minutes",
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
    by_id: &HashMap<i64, Candidate>,
    events: &Events,
    outcome: &mut Outcome,
) -> bool {
    let entity_type = p.kind.entity_type();
    let cmd = match p.arr.search(target).await {
        Ok(cmd) => cmd,
        Err(e) => {
            for id in target.items() {
                let title = by_id.get(id).map(|c| c.title.clone()).unwrap_or_default();
                let ev =
                    Ev::run(Level::Warn, EventType::SearchFailed, title).entity(entity_type, *id);
                events.emit(ev.detail(e.to_string())).await;
            }
            return false;
        }
    };
    for id in target.items() {
        let (title, group_id) = by_id
            .get(id)
            .map(|c| (c.title.clone(), c.group))
            .unwrap_or_default();
        let ev = Ev::run(Level::Info, EventType::SearchDispatched, title.clone())
            .entity(entity_type, *id);
        events.emit(ev.detail(format!("command {}", cmd.id))).await;
        outcome.searched.push(SearchedItem {
            entity_type: entity_type.to_owned(),
            entity_id: *id,
            title,
            group_id,
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
    while !cmd.is_terminal() && polls < p.polling.max {
        polls = polls.saturating_add(1);
        tokio::time::sleep(p.polling.interval).await;
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

#[cfg(test)]
mod executor_tests;
