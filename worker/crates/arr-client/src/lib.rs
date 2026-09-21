// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Minimal *arr REST client for snatching. Only what a snatch run needs: the wanted lists
//! (`wanted/missing`, `wanted/cutoff`), search commands, command status, the queue size and
//! `system/status`. Every request has a timeout, bounded retries, and the product User-Agent.

#![cfg_attr(
    test,
    allow(
        clippy::unwrap_used,
        clippy::expect_used,
        clippy::indexing_slicing,
        clippy::cast_possible_truncation,
        clippy::cast_sign_loss,
        clippy::arithmetic_side_effects
    )
)]

use std::sync::OnceLock;
use std::time::Duration;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use snatch_core::{Candidate, Target};
use url::Url;

/// *arr application family and API generation.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Kind {
    /// Sonarr v4 (`/api/v3`, episodes).
    Sonarr,
    /// Radarr v5 (`/api/v3`, movies).
    Radarr,
    /// Lidarr (`/api/v1`, albums).
    Lidarr,
    /// Readarr (`/api/v1`, books).
    Readarr,
    /// Whisparr v2 (Sonarr-shaped, `/api/v3`).
    WhisparrV2,
    /// Whisparr v3 / Eros (Radarr-shaped, `/api/v3`).
    WhisparrV3,
}

impl Kind {
    /// API path prefix for this kind.
    #[must_use]
    pub const fn api_prefix(self) -> &'static str {
        match self {
            Self::Lidarr | Self::Readarr => "/api/v1",
            Self::Sonarr | Self::Radarr | Self::WhisparrV2 | Self::WhisparrV3 => "/api/v3",
        }
    }

    /// Entity type name stored in processed memory and history.
    #[must_use]
    pub const fn entity_type(self) -> &'static str {
        match self {
            Self::Sonarr | Self::WhisparrV2 => "episode",
            Self::Radarr | Self::WhisparrV3 => "movie",
            Self::Lidarr => "album",
            Self::Readarr => "book",
        }
    }
}

/// Which wanted list to walk.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Wanted {
    /// Monitored items without a file.
    Missing,
    /// Items below their quality cutoff.
    Cutoff,
}

impl Wanted {
    const fn path(self) -> &'static str {
        match self {
            Self::Missing => "wanted/missing",
            Self::Cutoff => "wanted/cutoff",
        }
    }
}

/// Which Radarr release date decides "future" for the skip-unreleased filter.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RadarrRelease {
    /// `physicalRelease` (newtarr default).
    Physical,
    /// `digitalRelease`.
    Digital,
    /// `inCinemas`.
    Cinema,
}

/// Client errors.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// Transport-level failure (DNS, connect, timeout, TLS setup). The message carries the
    /// whole source chain because `reqwest::Error` alone prints only "builder error".
    #[error("arr: transport: {}", error_chain(.0))]
    Transport(#[from] reqwest::Error),
    /// Non-2xx after retries.
    #[error("arr: {method} {path}: HTTP {status}: {body}")]
    Status {
        /// HTTP method.
        method: &'static str,
        /// Request path.
        path: String,
        /// Status code.
        status: u16,
        /// Truncated response body.
        body: String,
    },
    /// Body could not be decoded.
    #[error("arr: decode {path}: {source}")]
    Decode {
        /// Request path.
        path: String,
        /// Underlying error.
        source: serde_json::Error,
    },
    /// Base URL invalid.
    #[error("arr: invalid base url: {0}")]
    BaseUrl(#[from] url::ParseError),
}

/// Client options.
#[derive(Debug, Clone)]
pub struct Options {
    /// Per-request timeout.
    pub timeout: Duration,
    /// Attempts per request (>= 1) on 5xx/429/transport errors.
    pub attempts: u32,
    /// Base backoff between attempts (doubles each retry, capped at 8x).
    pub backoff: Duration,
    /// User-Agent header.
    pub user_agent: String,
}

impl Default for Options {
    fn default() -> Self {
        Self {
            timeout: Duration::from_secs(30),
            attempts: 3,
            backoff: Duration::from_millis(500),
            user_agent: "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)".to_owned(),
        }
    }
}

/// One paged wanted-list response, normalised to [`Candidate`]s.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WantedPage {
    /// 1-based page number echoed by the server.
    pub page: u32,
    /// Page size echoed by the server.
    pub page_size: u32,
    /// Total records across all pages.
    pub total_records: u32,
    /// Items on this page.
    pub records: Vec<Candidate>,
}

/// Result of `POST /command` or `GET /command/{id}`.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CommandStatus {
    /// Command id.
    pub id: i64,
    /// Command name.
    #[serde(default)]
    pub name: String,
    /// `queued`, `started`, `completed`, `failed`, `aborted`, `cancelled`, `orphaned`.
    #[serde(default)]
    pub status: String,
}

impl CommandStatus {
    /// Whether the command reached a terminal state.
    #[must_use]
    pub fn is_terminal(&self) -> bool {
        matches!(
            self.status.as_str(),
            "completed" | "failed" | "aborted" | "cancelled" | "orphaned"
        )
    }
}

/// `GET /system/status` subset.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SystemStatus {
    /// Application name.
    #[serde(default)]
    pub app_name: String,
    /// Version string.
    #[serde(default)]
    pub version: String,
}

/// An authenticated client for one instance.
#[derive(Debug, Clone)]
pub struct ArrClient {
    http: reqwest::Client,
    base: Url,
    api_key: String,
    kind: Kind,
    opts: Options,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Paged<T> {
    page: u32,
    page_size: u32,
    total_records: u32,
    records: Vec<T>,
}

/// Bounds for the queue walk (HISS-02).
const QUEUE_PAGE_SIZE: u32 = 500;
const MAX_QUEUE_PAGES: u32 = 10;

/// The entity id fields a queue or history record may carry, one per *arr family.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct BusyLike {
    episode_id: Option<i64>,
    movie_id: Option<i64>,
    album_id: Option<i64>,
    book_id: Option<i64>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct MonitoredParent {
    #[serde(default)]
    monitored: bool,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct EpisodeLike {
    id: i64,
    series_id: i64,
    season_number: i32,
    episode_number: i32,
    #[serde(default)]
    title: String,
    air_date_utc: Option<DateTime<Utc>>,
    #[serde(default)]
    monitored: bool,
    series: Option<MonitoredParent>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct MovieLike {
    id: i64,
    #[serde(default)]
    title: String,
    #[serde(default)]
    year: i32,
    #[serde(default)]
    monitored: bool,
    physical_release: Option<DateTime<Utc>>,
    digital_release: Option<DateTime<Utc>>,
    in_cinemas: Option<DateTime<Utc>>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ChildLike {
    id: i64,
    #[serde(default)]
    title: String,
    #[serde(default)]
    monitored: bool,
    release_date: Option<DateTime<Utc>>,
    artist_id: Option<i64>,
    author_id: Option<i64>,
    artist: Option<MonitoredParent>,
    author: Option<MonitoredParent>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CommandBody<'a> {
    name: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    episode_ids: Option<&'a [i64]>,
    #[serde(skip_serializing_if = "Option::is_none")]
    series_id: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    season_number: Option<i32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    movie_ids: Option<&'a [i64]>,
    #[serde(skip_serializing_if = "Option::is_none")]
    album_ids: Option<&'a [i64]>,
    #[serde(skip_serializing_if = "Option::is_none")]
    artist_id: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    book_ids: Option<&'a [i64]>,
    #[serde(skip_serializing_if = "Option::is_none")]
    author_id: Option<i64>,
}

impl<'a> CommandBody<'a> {
    const fn empty(name: &'static str) -> Self {
        Self {
            name,
            episode_ids: None,
            series_id: None,
            season_number: None,
            movie_ids: None,
            album_ids: None,
            artist_id: None,
            book_ids: None,
            author_id: None,
        }
    }

    fn for_target(kind: Kind, t: &'a Target) -> Self {
        match t {
            Target::Episodes {
                series_id: _,
                items,
            } => Self {
                episode_ids: Some(items),
                ..Self::empty("EpisodeSearch")
            },
            Target::Season {
                series_id, season, ..
            } => Self {
                series_id: Some(*series_id),
                season_number: Some(*season),
                ..Self::empty("SeasonSearch")
            },
            Target::Series { series_id, .. } => Self {
                series_id: Some(*series_id),
                ..Self::empty("SeriesSearch")
            },
            Target::Items { items } => match kind {
                Kind::Lidarr => Self {
                    album_ids: Some(items),
                    ..Self::empty("AlbumSearch")
                },
                Kind::Readarr => Self {
                    book_ids: Some(items),
                    ..Self::empty("BookSearch")
                },
                Kind::Sonarr | Kind::WhisparrV2 => Self {
                    episode_ids: Some(items),
                    ..Self::empty("EpisodeSearch")
                },
                Kind::Radarr | Kind::WhisparrV3 => Self {
                    movie_ids: Some(items),
                    ..Self::empty("MoviesSearch")
                },
            },
            Target::Artist { artist_id, .. } => Self {
                artist_id: Some(*artist_id),
                ..Self::empty("ArtistSearch")
            },
            Target::Author { author_id, .. } => Self {
                author_id: Some(*author_id),
                ..Self::empty("AuthorSearch")
            },
        }
    }
}

const MAX_BODY_IN_ERROR: usize = 256;

/// Renders an error followed by every `source()` below it, joined with ": ".
fn error_chain(err: &(dyn std::error::Error + 'static)) -> String {
    let mut out = err.to_string();
    let mut cur = err.source();
    // Bounded: a pathological cycle must not spin forever.
    for _ in 0..16 {
        let Some(next) = cur else { break };
        out.push_str(": ");
        out.push_str(&next.to_string());
        cur = next.source();
    }
    out
}

/// Mozilla's root bundle, parsed once. It is added as *extra* roots, so the OS store is
/// still honoured when present, but a container without `ca-certificates` (or a scratch
/// image) no longer fails at client construction with "No CA certificates were loaded".
fn bundled_roots() -> &'static [reqwest::Certificate] {
    static ROOTS: OnceLock<Vec<reqwest::Certificate>> = OnceLock::new();
    ROOTS.get_or_init(|| {
        webpki_root_certs::TLS_SERVER_ROOT_CERTS
            .iter()
            .filter_map(|der| reqwest::Certificate::from_der(der).ok())
            .collect()
    })
}

impl ArrClient {
    /// Builds a client for one instance.
    pub fn new(kind: Kind, base_url: &str, api_key: &str, opts: Options) -> Result<Self, Error> {
        let base = Url::parse(base_url.trim_end_matches('/'))?;
        let mut builder = reqwest::Client::builder()
            .timeout(opts.timeout)
            .user_agent(opts.user_agent.clone());
        for cert in bundled_roots() {
            builder = builder.add_root_certificate(cert.clone());
        }
        let http = builder.build()?;
        Ok(Self {
            http,
            base,
            api_key: api_key.to_owned(),
            kind,
            opts,
        })
    }

    /// The instance kind.
    #[must_use]
    pub const fn kind(&self) -> Kind {
        self.kind
    }

    fn url(&self, path: &str, query: &[(&str, String)]) -> Result<Url, Error> {
        let mut u = self.base.clone();
        let joined = format!(
            "{}{}/{}",
            u.path().trim_end_matches('/'),
            self.kind.api_prefix(),
            path
        );
        u.set_path(&joined);
        if !query.is_empty() {
            let mut q = u.query_pairs_mut();
            for (k, v) in query {
                q.append_pair(k, v);
            }
        }
        Ok(u)
    }

    async fn send(
        &self,
        req: reqwest::RequestBuilder,
        method: &'static str,
        path: &str,
    ) -> Result<String, Error> {
        let mut delay = self.opts.backoff;
        let attempts = self.opts.attempts.max(1);
        let mut last: Option<Error> = None;
        for attempt in 1..=attempts {
            let Some(cloned) = req.try_clone() else { break };
            match cloned.header("X-Api-Key", &self.api_key).send().await {
                Ok(resp) => {
                    let status = resp.status();
                    let body = resp.text().await.unwrap_or_default();
                    if status.is_success() {
                        return Ok(body);
                    }
                    let err = Error::Status {
                        method,
                        path: path.to_owned(),
                        status: status.as_u16(),
                        body: body.chars().take(MAX_BODY_IN_ERROR).collect(),
                    };
                    let retryable = status.as_u16() == 429 || status.is_server_error();
                    if !retryable || attempt == attempts {
                        return Err(err);
                    }
                    last = Some(err);
                }
                Err(e) => {
                    if attempt == attempts {
                        return Err(Error::Transport(e));
                    }
                    last = Some(Error::Transport(e));
                }
            }
            tracing::debug!(method, path, attempt, "arr: retrying");
            tokio::time::sleep(delay).await;
            delay = delay
                .saturating_mul(2)
                .min(self.opts.backoff.saturating_mul(8));
        }
        Err(last.unwrap_or(Error::Status {
            method,
            path: path.to_owned(),
            status: 0,
            body: String::new(),
        }))
    }

    async fn get_json<T: serde::de::DeserializeOwned>(
        &self,
        path: &str,
        query: &[(&str, String)],
    ) -> Result<T, Error> {
        let url = self.url(path, query)?;
        let body = self.send(self.http.get(url), "GET", path).await?;
        serde_json::from_str(&body).map_err(|source| Error::Decode {
            path: path.to_owned(),
            source,
        })
    }

    /// `GET system/status`.
    pub async fn system_status(&self) -> Result<SystemStatus, Error> {
        self.get_json("system/status", &[]).await
    }

    /// Total items in the download queue (`GET queue?page=1&pageSize=1`).
    pub async fn queue_total(&self) -> Result<u32, Error> {
        let p: Paged<serde_json::Value> = self
            .get_json(
                "queue",
                &[("page", "1".to_owned()), ("pageSize", "1".to_owned())],
            )
            .await?;
        Ok(p.total_records)
    }

    /// Ids that are already on their way and must not be searched again: everything in
    /// the download queue plus, when `grab_hours > 0`, everything the *arr grabbed within
    /// that window (`GET history/since?eventType=1`). Sorted and deduplicated.
    pub async fn busy_ids(&self, grab_hours: u32) -> Result<Vec<i64>, Error> {
        let mut ids = self.queue_ids().await?;
        if grab_hours > 0 {
            ids.extend(self.grabbed_since(grab_hours).await?);
        }
        ids.sort_unstable();
        ids.dedup();
        Ok(ids)
    }

    async fn queue_ids(&self) -> Result<Vec<i64>, Error> {
        let mut ids = Vec::new();
        for page in 1..=MAX_QUEUE_PAGES {
            let query = [
                ("page", page.to_string()),
                ("pageSize", QUEUE_PAGE_SIZE.to_string()),
            ];
            let p: Paged<BusyLike> = self.get_json("queue", &query).await?;
            let fetched = p.records.len();
            ids.extend(p.records.iter().filter_map(|r| self.busy_id(r)));
            if fetched < QUEUE_PAGE_SIZE as usize {
                break;
            }
        }
        Ok(ids)
    }

    async fn grabbed_since(&self, hours: u32) -> Result<Vec<i64>, Error> {
        let window = chrono::TimeDelta::try_hours(i64::from(hours)).unwrap_or_default();
        let since = Utc::now()
            .checked_sub_signed(window)
            .unwrap_or_else(Utc::now)
            .to_rfc3339_opts(chrono::SecondsFormat::Secs, true);
        let query = [("date", since), ("eventType", "1".to_owned())];
        let records: Vec<BusyLike> = self.get_json("history/since", &query).await?;
        Ok(records.iter().filter_map(|r| self.busy_id(r)).collect())
    }

    fn busy_id(&self, r: &BusyLike) -> Option<i64> {
        match self.kind {
            Kind::Sonarr | Kind::WhisparrV2 => r.episode_id,
            Kind::Radarr | Kind::WhisparrV3 => r.movie_id,
            Kind::Lidarr => r.album_id,
            Kind::Readarr => r.book_id,
        }
    }

    /// One page of a wanted list, normalised to candidates.
    pub async fn wanted_page(
        &self,
        wanted: Wanted,
        page: u32,
        page_size: u32,
        release: RadarrRelease,
    ) -> Result<WantedPage, Error> {
        let mut query = vec![
            ("page", page.to_string()),
            ("pageSize", page_size.to_string()),
        ];
        if matches!(self.kind, Kind::Sonarr | Kind::WhisparrV2) {
            query.push(("includeSeries", "true".to_owned()));
        }
        if matches!(self.kind, Kind::Lidarr) {
            query.push(("includeArtist", "true".to_owned()));
        }
        if matches!(self.kind, Kind::Readarr) {
            query.push(("includeAuthor", "true".to_owned()));
        }
        let path = wanted.path();
        match self.kind {
            Kind::Sonarr | Kind::WhisparrV2 => {
                let p: Paged<EpisodeLike> = self.get_json(path, &query).await?;
                Ok(page_of(p, episode_candidate))
            }
            Kind::Radarr | Kind::WhisparrV3 => {
                let p: Paged<MovieLike> = self.get_json(path, &query).await?;
                Ok(page_of(p, |m| movie_candidate(m, release)))
            }
            Kind::Lidarr | Kind::Readarr => {
                let p: Paged<ChildLike> = self.get_json(path, &query).await?;
                Ok(page_of(p, child_candidate))
            }
        }
    }

    /// Dispatches the search command for a target (`POST command`).
    pub async fn search(&self, target: &Target) -> Result<CommandStatus, Error> {
        let body = CommandBody::for_target(self.kind, target);
        let url = self.url("command", &[])?;
        let text = self
            .send(self.http.post(url).json(&body), "POST", "command")
            .await?;
        serde_json::from_str(&text).map_err(|source| Error::Decode {
            path: "command".to_owned(),
            source,
        })
    }

    /// Polls a command (`GET command/{id}`).
    pub async fn command_status(&self, id: i64) -> Result<CommandStatus, Error> {
        self.get_json(&format!("command/{id}"), &[]).await
    }
}

fn page_of<T>(p: Paged<T>, map: impl Fn(T) -> Candidate) -> WantedPage {
    WantedPage {
        page: p.page,
        page_size: p.page_size,
        total_records: p.total_records,
        records: p.records.into_iter().map(map).collect(),
    }
}

fn episode_candidate(e: EpisodeLike) -> Candidate {
    let parent_monitored = e.series.as_ref().is_none_or(|s| s.monitored);
    Candidate {
        id: e.id,
        group: e.series_id,
        season: Some(e.season_number),
        title: format!(
            "S{:02}E{:02} {}",
            e.season_number, e.episode_number, e.title
        ),
        release_unix: e.air_date_utc.map(|d| d.timestamp()),
        monitored: e.monitored && parent_monitored,
    }
}

fn movie_candidate(m: MovieLike, release: RadarrRelease) -> Candidate {
    let date = match release {
        RadarrRelease::Physical => m.physical_release,
        RadarrRelease::Digital => m.digital_release,
        RadarrRelease::Cinema => m.in_cinemas,
    };
    Candidate {
        id: m.id,
        group: m.id,
        season: None,
        title: if m.year > 0 {
            format!("{} ({})", m.title, m.year)
        } else {
            m.title
        },
        release_unix: date.map(|d| d.timestamp()),
        monitored: m.monitored,
    }
}

fn child_candidate(c: ChildLike) -> Candidate {
    let parent_monitored = c
        .artist
        .as_ref()
        .or(c.author.as_ref())
        .is_none_or(|p| p.monitored);
    Candidate {
        id: c.id,
        group: c.artist_id.or(c.author_id).unwrap_or(c.id),
        season: None,
        title: c.title,
        release_unix: c.release_date.map(|d| d.timestamp()),
        monitored: c.monitored && parent_monitored,
    }
}

#[cfg(test)]
mod tests {
    #[derive(Debug, thiserror::Error)]
    #[error("outer")]
    struct Outer(#[source] Inner);

    #[derive(Debug, thiserror::Error)]
    #[error("inner cause")]
    struct Inner;

    #[test]
    fn error_chain_joins_sources() {
        assert_eq!(super::error_chain(&Outer(Inner)), "outer: inner cause");
        assert!(super::bundled_roots().len() > 100);
    }

    use super::*;
    use wiremock::matchers::{body_partial_json, header, method, path, query_param};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    fn opts() -> Options {
        Options {
            timeout: Duration::from_secs(2),
            attempts: 3,
            backoff: Duration::from_millis(5),
            ..Options::default()
        }
    }

    #[tokio::test]
    async fn sonarr_wanted_missing_maps_episodes_and_parent_monitoring() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/v3/wanted/missing"))
            .and(query_param("includeSeries", "true"))
            .and(header("X-Api-Key", "k"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "page": 1, "pageSize": 2, "totalRecords": 7,
                "records": [
                    {"id": 10, "seriesId": 3, "seasonNumber": 1, "episodeNumber": 2, "title": "Pilot", "airDateUtc": "2020-01-01T00:00:00Z", "monitored": true, "series": {"monitored": true}},
                    {"id": 11, "seriesId": 3, "seasonNumber": 1, "episodeNumber": 3, "title": "Two", "monitored": true, "series": {"monitored": false}}
                ]
            })))
            .mount(&server)
            .await;
        let c = ArrClient::new(Kind::Sonarr, &server.uri(), "k", opts()).unwrap();
        let page = c
            .wanted_page(Wanted::Missing, 1, 2, RadarrRelease::Physical)
            .await
            .unwrap();
        assert_eq!(page.total_records, 7);
        assert_eq!(page.records.len(), 2);
        assert_eq!(page.records[0].title, "S01E02 Pilot");
        assert_eq!(page.records[0].release_unix, Some(1_577_836_800));
        assert!(page.records[0].monitored);
        assert!(
            !page.records[1].monitored,
            "unmonitored series must taint the episode"
        );
    }

    #[tokio::test]
    async fn radarr_release_type_selects_the_date() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/v3/wanted/cutoff"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "page": 1, "pageSize": 10, "totalRecords": 1,
                "records": [{"id": 5, "title": "Film", "year": 2021, "monitored": true,
                              "physicalRelease": "2021-06-01T00:00:00Z", "digitalRelease": "2021-05-01T00:00:00Z"}]
            })))
            .mount(&server)
            .await;
        let c = ArrClient::new(Kind::Radarr, &server.uri(), "k", opts()).unwrap();
        let phys = c
            .wanted_page(Wanted::Cutoff, 1, 10, RadarrRelease::Physical)
            .await
            .unwrap();
        let digi = c
            .wanted_page(Wanted::Cutoff, 1, 10, RadarrRelease::Digital)
            .await
            .unwrap();
        let cine = c
            .wanted_page(Wanted::Cutoff, 1, 10, RadarrRelease::Cinema)
            .await
            .unwrap();
        assert_eq!(phys.records[0].title, "Film (2021)");
        assert!(phys.records[0].release_unix > digi.records[0].release_unix);
        assert_eq!(
            cine.records[0].release_unix, None,
            "missing date stays unknown"
        );
    }

    #[tokio::test]
    async fn lidarr_uses_v1_prefix_and_artist_grouping() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/v1/wanted/missing"))
            .and(query_param("includeArtist", "true"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "page": 1, "pageSize": 10, "totalRecords": 1,
                "records": [{"id": 8, "title": "Album", "monitored": true, "artistId": 2, "artist": {"monitored": true}}]
            })))
            .mount(&server)
            .await;
        let c = ArrClient::new(Kind::Lidarr, &server.uri(), "k", opts()).unwrap();
        let page = c
            .wanted_page(Wanted::Missing, 1, 10, RadarrRelease::Physical)
            .await
            .unwrap();
        assert_eq!(page.records[0].group, 2);
    }

    #[tokio::test]
    async fn search_posts_the_right_command_per_kind() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/api/v3/command"))
            .and(body_partial_json(
                serde_json::json!({"name": "SeasonSearch", "seriesId": 3, "seasonNumber": 2}),
            ))
            .respond_with(ResponseTemplate::new(201).set_body_json(
                serde_json::json!({"id": 42, "name": "SeasonSearch", "status": "queued"}),
            ))
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path("/api/v1/command"))
            .and(body_partial_json(
                serde_json::json!({"name": "BookSearch", "bookIds": [1, 2]}),
            ))
            .respond_with(
                ResponseTemplate::new(201)
                    .set_body_json(serde_json::json!({"id": 7, "status": "started"})),
            )
            .mount(&server)
            .await;
        let sonarr = ArrClient::new(Kind::Sonarr, &server.uri(), "k", opts()).unwrap();
        let st = sonarr
            .search(&Target::Season {
                series_id: 3,
                season: 2,
                items: vec![1],
            })
            .await
            .unwrap();
        assert_eq!(st.id, 42);
        assert!(!st.is_terminal());
        let readarr = ArrClient::new(Kind::Readarr, &server.uri(), "k", opts()).unwrap();
        let st = readarr
            .search(&Target::Items { items: vec![1, 2] })
            .await
            .unwrap();
        assert_eq!(st.id, 7);
    }

    #[tokio::test]
    async fn retries_on_503_then_succeeds_and_gives_up_on_401() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/v3/system/status"))
            .respond_with(ResponseTemplate::new(503))
            .up_to_n_times(2)
            .mount(&server)
            .await;
        Mock::given(method("GET"))
            .and(path("/api/v3/system/status"))
            .respond_with(
                ResponseTemplate::new(200)
                    .set_body_json(serde_json::json!({"appName": "Sonarr", "version": "4.0.0"})),
            )
            .mount(&server)
            .await;
        Mock::given(method("GET"))
            .and(path("/api/v3/queue"))
            .respond_with(ResponseTemplate::new(401).set_body_string("nope"))
            .expect(1)
            .mount(&server)
            .await;
        let c = ArrClient::new(Kind::Sonarr, &server.uri(), "k", opts()).unwrap();
        let st = c.system_status().await.unwrap();
        assert_eq!(st.version, "4.0.0");
        let err = c.queue_total().await.unwrap_err();
        assert!(
            matches!(err, Error::Status { status: 401, .. }),
            "401 must not retry: {err}"
        );
    }

    #[tokio::test]
    async fn gives_up_after_bounded_attempts() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/v3/queue"))
            .respond_with(ResponseTemplate::new(500))
            .expect(3)
            .mount(&server)
            .await;
        let c = ArrClient::new(Kind::Radarr, &server.uri(), "k", opts()).unwrap();
        assert!(matches!(
            c.queue_total().await.unwrap_err(),
            Error::Status { status: 500, .. }
        ));
    }

    #[test]
    fn boundary_invalid_base_url() {
        assert!(matches!(
            ArrClient::new(Kind::Sonarr, "not a url", "k", opts()),
            Err(Error::BaseUrl(_))
        ));
    }

    #[tokio::test]
    async fn busy_ids_merge_queue_and_recent_grabs() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/v3/queue"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "page": 1, "pageSize": 500, "totalRecords": 2,
                "records": [{"id": 1, "episodeId": 7}, {"id": 2, "episodeId": 5}]
            })))
            .mount(&server)
            .await;
        Mock::given(method("GET"))
            .and(path("/api/v3/history/since"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!([
                {"id": 10, "episodeId": 9, "eventType": "grabbed"},
                {"id": 11, "episodeId": 5, "eventType": "grabbed"}
            ])))
            .mount(&server)
            .await;
        let c = ArrClient::new(Kind::Sonarr, &server.uri(), "k", Options::default()).unwrap();
        assert_eq!(c.busy_ids(24).await.unwrap(), vec![5, 7, 9]);
        assert_eq!(
            c.busy_ids(0).await.unwrap(),
            vec![5, 7],
            "0 hours skips history"
        );
    }
}
