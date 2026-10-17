// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Ships the worker's own log records to the API's in-app log viewer (#349). A tracing layer
//! copies each event into a bounded channel and never waits: a full channel drops the record
//! and counts it. A task sends the records in batches with `ReportLogs`; the API redacts them.
//! Nothing is stored here, so a restart loses the unsent records, as it loses stdout.

use std::fmt;
use std::future::Future;
use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use snatch_proto::{Level as ProtoLevel, LogField, LogRecord, ReportLogsRequest};
use tokio::sync::mpsc;
use tokio_util::sync::CancellationToken;
use tonic::{Code, Status};
use tracing::field::{Field, Visit};
use tracing::{Event, Level, Subscriber};
use tracing_subscriber::layer::{Context, Layer};

/// Records the channel holds before the layer drops new ones.
pub const CAPACITY: usize = 1024;
/// Records sent in one batch; a full batch is sent at once.
pub const BATCH: usize = 200;
/// Longest wait before buffered records are sent.
pub const FLUSH_EVERY: Duration = Duration::from_secs(2);
/// Deadline of one `ReportLogs` call.
pub const CALL_TIMEOUT: Duration = Duration::from_secs(5);
/// Attempts per batch, and the longest wait after the first failed one (doubling).
const ATTEMPTS: u32 = 3;
const BACKOFF: Duration = Duration::from_millis(500);

/// Size caps applied before a record enters the channel (bytes, fields).
const MAX_MESSAGE_BYTES: usize = 8 << 10;
const MAX_VALUE_BYTES: usize = 2 << 10;
const MAX_FIELDS: usize = 64;

/// Records dropped since the last batch the API accepted: shared by the layer and the shipper.
#[derive(Debug, Default)]
pub struct Dropped(AtomicU64);

impl Dropped {
    fn add(&self, n: u64) {
        self.0.fetch_add(n, Ordering::Relaxed);
    }

    fn take(&self) -> u64 {
        self.0.swap(0, Ordering::Relaxed)
    }

    fn pending(&self) -> bool {
        self.0.load(Ordering::Relaxed) > 0
    }

    /// The count not yet reported.
    #[cfg(test)]
    pub fn get(&self) -> u64 {
        self.0.load(Ordering::Relaxed)
    }
}

/// The tracing layer that copies events into the channel.
pub struct ShipLayer {
    tx: mpsc::Sender<LogRecord>,
    dropped: Arc<Dropped>,
}

/// A layer with its receiving end and drop counter.
pub fn channel(capacity: usize) -> (ShipLayer, mpsc::Receiver<LogRecord>, Arc<Dropped>) {
    let (tx, rx) = mpsc::channel(capacity.max(1));
    let dropped = Arc::new(Dropped::default());
    (
        ShipLayer {
            tx,
            dropped: dropped.clone(),
        },
        rx,
        dropped,
    )
}

impl<S: Subscriber> Layer<S> for ShipLayer {
    fn on_event(&self, event: &Event<'_>, _ctx: Context<'_, S>) {
        let meta = event.metadata();
        let mut rec = LogRecord {
            ts_unix_ms: now_ms(),
            level: proto_level(*meta.level()) as i32,
            target: meta.target().to_owned(),
            message: String::new(),
            fields: Vec::new(),
        };
        event.record(&mut FieldVisitor { rec: &mut rec });
        if self.tx.try_send(rec).is_err() {
            self.dropped.add(1);
        }
    }
}

fn now_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .ok()
        .and_then(|d| i64::try_from(d.as_millis()).ok())
        .unwrap_or(0)
}

fn proto_level(level: Level) -> ProtoLevel {
    match level {
        Level::ERROR => ProtoLevel::Error,
        Level::WARN => ProtoLevel::Warn,
        Level::INFO => ProtoLevel::Info,
        _ => ProtoLevel::Debug,
    }
}

/// `s` cut at `max` bytes, on a character boundary.
fn capped(mut s: String, max: usize) -> String {
    if s.len() > max {
        // A UTF-8 character is at most four bytes, so at most three steps back.
        let mut at = max;
        for _ in 0..4 {
            if s.is_char_boundary(at) {
                break;
            }
            at = at.saturating_sub(1);
        }
        s.truncate(at);
    }
    s
}

struct FieldVisitor<'a> {
    rec: &'a mut LogRecord,
}

impl FieldVisitor<'_> {
    fn put(&mut self, field: &Field, value: String) {
        if field.name() == "message" {
            self.rec.message = capped(value, MAX_MESSAGE_BYTES);
        } else if self.rec.fields.len() < MAX_FIELDS {
            self.rec.fields.push(LogField {
                key: field.name().to_owned(),
                value: capped(value, MAX_VALUE_BYTES),
            });
        }
    }
}

impl Visit for FieldVisitor<'_> {
    fn record_str(&mut self, field: &Field, value: &str) {
        self.put(field, value.to_owned());
    }

    fn record_debug(&mut self, field: &Field, value: &dyn fmt::Debug) {
        self.put(field, format!("{value:?}"));
    }
}

/// What became of one batch.
#[derive(Debug, PartialEq, Eq)]
enum Sent {
    Accepted,
    /// Lost after the last attempt; counted as dropped.
    Lost,
    /// The API has no `ReportLogs`: shipping stops.
    Unsupported,
}

/// Codes a later attempt may get past.
fn transient(code: Code) -> bool {
    matches!(
        code,
        Code::Unavailable | Code::DeadlineExceeded | Code::ResourceExhausted | Code::Aborted
    )
}

/// A random wait between half of `d` and `d`.
fn jitter(d: Duration) -> Duration {
    use rand::RngExt;
    let half = d.checked_div(2).unwrap_or(Duration::ZERO);
    rand::rng().random_range(half..=d)
}

/// Sends one batch with bounded retries; records the API never took count as dropped.
async fn send_batch<F, Fut>(
    send: &mut F,
    worker_id: &str,
    records: Vec<LogRecord>,
    dropped: &Dropped,
) -> Sent
where
    F: FnMut(ReportLogsRequest) -> Fut,
    Fut: Future<Output = Result<(), Status>>,
{
    let req = ReportLogsRequest {
        worker_id: worker_id.to_owned(),
        dropped: dropped.take(),
        records,
    };
    let lost = u64::try_from(req.records.len())
        .unwrap_or(u64::MAX)
        .saturating_add(req.dropped);
    for attempt in 1..=ATTEMPTS {
        let status = match send(req.clone()).await {
            Ok(()) => return Sent::Accepted,
            Err(status) => status,
        };
        if status.code() == Code::Unimplemented {
            return Sent::Unsupported;
        }
        if attempt == ATTEMPTS || !transient(status.code()) {
            break;
        }
        let wait = BACKOFF.saturating_mul(2u32.saturating_pow(attempt.saturating_sub(1)));
        tokio::time::sleep(jitter(wait)).await;
    }
    dropped.add(lost);
    Sent::Lost
}

/// Why the shipper woke up.
#[derive(Debug, PartialEq, Eq)]
enum Wake {
    Records,
    Tick,
    Stop,
}

/// Batches sent at shutdown at most: a full channel plus the batch already buffered.
const FINAL_BATCHES: usize = CAPACITY / BATCH + 2;

/// Upper bound on shipper wake-ups (HISS-02): 2^48 at a million per second is about nine years.
const MAX_WAKES: u64 = 1 << 48;

/// Sends buffered records every `FLUSH_EVERY`, or at once when `BATCH` are waiting, until
/// `stop` is cancelled or every layer is gone; then sends what is left. `send` makes one
/// `ReportLogs` call with its own deadline. Stops for good when the API has no `ReportLogs`.
pub async fn ship<F, Fut>(
    mut send: F,
    worker_id: String,
    mut rx: mpsc::Receiver<LogRecord>,
    dropped: Arc<Dropped>,
    stop: CancellationToken,
) where
    F: FnMut(ReportLogsRequest) -> Fut,
    Fut: Future<Output = Result<(), Status>>,
{
    let mut ticker = tokio::time::interval(FLUSH_EVERY);
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    ticker.tick().await;
    let mut buf: Vec<LogRecord> = Vec::with_capacity(BATCH);
    for _ in 0..MAX_WAKES {
        let room = BATCH.saturating_sub(buf.len()).max(1);
        let wake = tokio::select! {
            n = rx.recv_many(&mut buf, room) => if n == 0 { Wake::Stop } else { Wake::Records },
            _ = ticker.tick() => Wake::Tick,
            () = stop.cancelled() => Wake::Stop,
        };
        if wake == Wake::Stop {
            break;
        }
        if wake == Wake::Records && buf.len() < BATCH {
            continue;
        }
        if buf.is_empty() && !dropped.pending() {
            continue;
        }
        let batch = std::mem::replace(&mut buf, Vec::with_capacity(BATCH));
        if send_batch(&mut send, &worker_id, batch, &dropped).await == Sent::Unsupported {
            tracing::info!(
                "API takes no worker log records (ReportLogs unimplemented); not shipping logs"
            );
            return;
        }
    }
    drain(&mut send, &worker_id, buf, &mut rx, &dropped).await;
}

/// Sends `buf` and every record still in the channel, `BATCH` at a time.
async fn drain<F, Fut>(
    send: &mut F,
    worker_id: &str,
    mut buf: Vec<LogRecord>,
    rx: &mut mpsc::Receiver<LogRecord>,
    dropped: &Dropped,
) where
    F: FnMut(ReportLogsRequest) -> Fut,
    Fut: Future<Output = Result<(), Status>>,
{
    for _ in 0..FINAL_BATCHES {
        while buf.len() < BATCH {
            let Ok(rec) = rx.try_recv() else { break };
            buf.push(rec);
        }
        if buf.is_empty() && !dropped.pending() {
            return;
        }
        let batch = std::mem::replace(&mut buf, Vec::with_capacity(BATCH));
        if send_batch(send, worker_id, batch, dropped).await == Sent::Unsupported {
            return;
        }
    }
}

#[cfg(test)]
mod logship_tests;
