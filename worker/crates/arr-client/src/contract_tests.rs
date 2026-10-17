// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Contract tests: the wire types and the requests of this client against the `OpenAPI` specs
//! that Sonarr, Radarr, Lidarr and the three supported Readarr forks (Bookshelf, Chaptarr and
//! the Faustvii fork) publish (`docs/upstream-contracts.md`).
//!
//! `upstreamspec fetch` writes one contract file per pinned spec into a cache directory that is
//! never committed. A contract lists the operations the client calls, each with the documented
//! query parameter names and an example response in which every documented property is present.
//! [`check`] compares the client with one contract:
//!
//! - a field a wire type reads that the documented response lacks,
//! - a request the client sends that the spec does not describe (path or query parameter),
//! - a client method that fails on the documented example response,
//! - an operation of the contract the client does not request,
//! - a command body that differs from the upstream C# command class (a field the class lacks, a
//!   property of the class the body lacks, a command without a class) and a pinned class whose
//!   command the client never sends.
//!
//! The specs cannot describe command bodies: `CommandResource` lists the generic properties of a
//! command and forbids others. `upstreamspec fetch` therefore reads the public properties of the
//! pinned command classes into the contract (`commands`, keyed by command name) and the local
//! server compares every body it receives with them.
//!
//! The tests named `upstream_contract_*` read the real contracts and are ignored by default;
//! `make contracts-verify` runs them and fails when one left no receipt. Every other test runs
//! [`check`] on a contract written by hand here, with and without a planted difference.

use std::cell::RefCell;
use std::collections::{BTreeMap, BTreeSet};
use std::path::Path;
use std::time::Duration;

use serde::Deserialize;
use serde::de::value::BorrowedStrDeserializer;
use serde::de::{self, DeserializeOwned, DeserializeSeed, Deserializer, MapAccess, SeqAccess};
use serde_json::{Value, json};
use snatch_core::Target;
use wiremock::matchers::any;
use wiremock::{Mock, MockServer, Request, ResponseTemplate};

use super::{
    ArrClient, BusyLike, ChildLike, CommandStatus, EpisodeLike, Error, Kind, MovieLike, Options,
    Paged, RadarrRelease, SceneLike, SystemStatus, Wanted, WhisparrMovieLike,
};

/// The environment variable that names the cache directory.
const ENV_CACHE: &str = "SNATCHARR_SPEC_CACHE";

/// One contract file (`<spec>.contract.json`).
#[derive(Debug, Clone, Deserialize)]
struct Contract {
    spec: String,
    commit: String,
    /// Keyed by `<METHOD> <path>`, path parameters in braces.
    operations: BTreeMap<String, Operation>,
    /// The properties of each upstream command class, by command name (`EpisodeSearch`), as the
    /// upstream serializer writes them (camel case). A body has `name` and exactly these.
    #[serde(default)]
    commands: BTreeMap<String, Vec<String>>,
}

#[derive(Debug, Clone, Deserialize)]
struct Operation {
    parameters: Vec<String>,
    response: Value,
}

/// Walks an example the way serde walks a response and records what the wire type reads and
/// the example does not provide: a field the object lacks (`records[].seriesId`), a value that
/// is null (`records[].series: null`) and an array without an element (`records: empty`).
struct Probe<'de, 'f> {
    value: &'de Value,
    at: String,
    found: &'f RefCell<Vec<String>>,
}

impl Probe<'_, '_> {
    fn note(&self, what: &str) {
        let at = if self.at.is_empty() {
            "(root)"
        } else {
            &self.at
        };
        self.found.borrow_mut().push(format!("{at}: {what}"));
    }
}

fn child(at: &str, name: &str) -> String {
    if at.is_empty() {
        name.to_owned()
    } else {
        format!("{at}.{name}")
    }
}

impl<'de> Deserializer<'de> for Probe<'de, '_> {
    type Error = serde_json::Error;

    fn deserialize_any<V: de::Visitor<'de>>(self, visitor: V) -> Result<V::Value, Self::Error> {
        if self.value.is_null() {
            self.note("null");
        }
        self.value.deserialize_any(visitor)
    }

    fn deserialize_option<V: de::Visitor<'de>>(self, visitor: V) -> Result<V::Value, Self::Error> {
        if self.value.is_null() {
            self.note("null");
            return visitor.visit_none();
        }
        visitor.visit_some(self)
    }

    fn deserialize_seq<V: de::Visitor<'de>>(self, visitor: V) -> Result<V::Value, Self::Error> {
        let Value::Array(items) = self.value else {
            return self.value.deserialize_any(visitor);
        };
        if items.is_empty() {
            self.note("empty");
        }
        visitor.visit_seq(Elements {
            items: items.iter(),
            at: format!("{}[]", self.at),
            found: self.found,
        })
    }

    fn deserialize_struct<V: de::Visitor<'de>>(
        self,
        _name: &'static str,
        fields: &'static [&'static str],
        visitor: V,
    ) -> Result<V::Value, Self::Error> {
        let Value::Object(object) = self.value else {
            return self.value.deserialize_any(visitor);
        };
        for field in fields {
            if !object.contains_key(*field) {
                self.found.borrow_mut().push(child(&self.at, field));
            }
        }
        visitor.visit_map(Fields {
            entries: object.iter(),
            pending: None,
            at: self.at,
            found: self.found,
        })
    }

    fn deserialize_newtype_struct<V: de::Visitor<'de>>(
        self,
        _name: &'static str,
        visitor: V,
    ) -> Result<V::Value, Self::Error> {
        visitor.visit_newtype_struct(self)
    }

    fn deserialize_ignored_any<V: de::Visitor<'de>>(
        self,
        visitor: V,
    ) -> Result<V::Value, Self::Error> {
        visitor.visit_unit()
    }

    serde::forward_to_deserialize_any! {
        bool i8 i16 i32 i64 i128 u8 u16 u32 u64 u128 f32 f64 char str string bytes byte_buf
        unit unit_struct tuple tuple_struct map enum identifier
    }
}

/// The members of one example object.
struct Fields<'de, 'f> {
    entries: serde_json::map::Iter<'de>,
    pending: Option<(&'de String, &'de Value)>,
    at: String,
    found: &'f RefCell<Vec<String>>,
}

impl<'de> MapAccess<'de> for Fields<'de, '_> {
    type Error = serde_json::Error;

    fn next_key_seed<K: DeserializeSeed<'de>>(
        &mut self,
        seed: K,
    ) -> Result<Option<K::Value>, Self::Error> {
        let Some((key, value)) = self.entries.next() else {
            return Ok(None);
        };
        self.pending = Some((key, value));
        seed.deserialize(BorrowedStrDeserializer::new(key))
            .map(Some)
    }

    fn next_value_seed<S: DeserializeSeed<'de>>(
        &mut self,
        seed: S,
    ) -> Result<S::Value, Self::Error> {
        let Some((key, value)) = self.pending.take() else {
            return Err(de::Error::custom("a value was read before its key"));
        };
        seed.deserialize(Probe {
            value,
            at: child(&self.at, key),
            found: self.found,
        })
    }
}

/// The elements of one example array. They share the path `<array>[]`.
struct Elements<'de, 'f> {
    items: std::slice::Iter<'de, Value>,
    at: String,
    found: &'f RefCell<Vec<String>>,
}

impl<'de> SeqAccess<'de> for Elements<'de, '_> {
    type Error = serde_json::Error;

    fn next_element_seed<S: DeserializeSeed<'de>>(
        &mut self,
        seed: S,
    ) -> Result<Option<S::Value>, Self::Error> {
        let Some(value) = self.items.next() else {
            return Ok(None);
        };
        seed.deserialize(Probe {
            value,
            at: self.at.clone(),
            found: self.found,
        })
        .map(Some)
    }
}

/// What the wire type `T` reads and `example` does not provide, sorted. An example that `T`
/// does not decode adds one line with the decoder's message.
fn missing<T: DeserializeOwned>(example: &Value) -> Vec<String> {
    let found = RefCell::new(Vec::new());
    let decoded = T::deserialize(Probe {
        value: example,
        at: String::new(),
        found: &found,
    });
    let mut found = found.into_inner();
    if let Err(err) = decoded {
        found.push(format!("does not decode: {err}"));
    }
    found.sort();
    found.dedup();
    found
}

type Reader = fn(&Value) -> Vec<String>;

/// Every operation the client requests, with the wire type its response is decoded into.
fn readers(kind: Kind) -> [(&'static str, &'static str, Reader); 7] {
    let wanted: Reader = match kind {
        Kind::Sonarr => missing::<Paged<EpisodeLike>>,
        Kind::WhisparrV2 => missing::<Paged<SceneLike>>,
        Kind::Radarr => missing::<Paged<MovieLike>>,
        Kind::WhisparrV3 => missing::<Paged<WhisparrMovieLike>>,
        Kind::Lidarr | Kind::Readarr => missing::<Paged<ChildLike>>,
    };
    [
        ("GET", "system/status", missing::<SystemStatus>),
        ("GET", "queue", missing::<Paged<BusyLike>>),
        ("GET", "history/since", missing::<Vec<BusyLike>>),
        ("GET", "wanted/missing", wanted),
        ("GET", "wanted/cutoff", wanted),
        ("POST", "command", missing::<CommandStatus>),
        ("GET", "command/{id}", missing::<CommandStatus>),
    ]
}

/// What the wire types read for another *arr family. `BusyLike` and `ChildLike` serve several
/// families, so the spec of one family does not document the id fields of the others, and
/// `AuthorParent` reads Chaptarr's per-format tags and profiles, which the other forks lack. An
/// entry the spec does document fails the check: the list cannot go stale.
fn foreign(kind: Kind, spec: &str) -> BTreeSet<String> {
    let prefix = kind.api_prefix();
    let own = match kind {
        Kind::Sonarr | Kind::WhisparrV2 => "episodeId",
        Kind::Radarr | Kind::WhisparrV3 => "movieId",
        Kind::Lidarr => "albumId",
        Kind::Readarr => "bookId",
    };
    let mut absent = BTreeSet::new();
    for id in ["episodeId", "movieId", "albumId", "bookId"] {
        if id != own {
            absent.insert(format!("GET {prefix}/queue: records[].{id}"));
            absent.insert(format!("GET {prefix}/history/since: [].{id}"));
        }
    }
    let other_parent: &[&str] = match kind {
        Kind::Lidarr => &["authorId", "author"],
        Kind::Readarr => &["artistId", "artist"],
        Kind::Sonarr | Kind::Radarr | Kind::WhisparrV2 | Kind::WhisparrV3 => &[],
    };
    let per_format: &[&str] = if kind == Kind::Readarr && spec != "chaptarr" {
        &[
            "author.audiobookTags",
            "author.ebookTags",
            "author.audiobookQualityProfileId",
            "author.ebookQualityProfileId",
        ]
    } else {
        &[]
    };
    for field in other_parent.iter().chain(per_format) {
        for list in ["missing", "cutoff"] {
            absent.insert(format!("GET {prefix}/wanted/{list}: records[].{field}"));
        }
    }
    absent
}

/// The differences between the wire types and the documented responses.
fn shapes(kind: Kind, contract: &Contract) -> Vec<String> {
    let mut problems = Vec::new();
    let mut found = BTreeSet::new();
    for (method, path, reader) in readers(kind) {
        let key = format!("{method} {}/{path}", kind.api_prefix());
        match contract.operations.get(&key) {
            Some(operation) => {
                found.extend(
                    reader(&operation.response)
                        .into_iter()
                        .map(|at| format!("{key}: {at}")),
                );
            }
            None => problems.push(format!("{key}: the contract lacks the operation")),
        }
    }
    let absent = foreign(kind, &contract.spec);
    problems.extend(
        found
            .difference(&absent)
            .map(|at| format!("{at}: the client reads it and the spec does not document it")),
    );
    problems.extend(
        absent
            .difference(&found)
            .map(|at| format!("{at}: listed as absent and the spec documents it")),
    );
    problems
}

/// Reports whether `path` is `template` with each `{name}` segment replaced by one segment.
fn matches(template: &str, path: &str) -> bool {
    let (want, got): (Vec<&str>, Vec<&str>) =
        (template.split('/').collect(), path.split('/').collect());
    want.len() == got.len()
        && want.iter().zip(&got).all(|(segment, value)| {
            let parameter = segment.starts_with('{') && segment.ends_with('}');
            (parameter && !value.is_empty()) || segment == value
        })
}

/// The operation of the contract that `request` asks for.
fn operation_of<'a>(
    operations: &'a BTreeMap<String, Operation>,
    request: &Request,
) -> Option<(&'a String, &'a Operation)> {
    operations.iter().find(|(key, _)| {
        key.split_once(' ').is_some_and(|(method, template)| {
            method == request.method.as_str() && matches(template, request.url.path())
        })
    })
}

fn note<T>(problems: &mut Vec<String>, call: &str, result: Result<T, Error>) -> Option<T> {
    match result {
        Ok(value) => Some(value),
        Err(err) => {
            problems.push(format!("{call} on the documented response: {err}"));
            None
        }
    }
}

/// Every target the worker sends a search command for to a client of `kind`, one per command.
fn targets(kind: Kind) -> Vec<Target> {
    let items = vec![1];
    let mut targets = vec![Target::Items {
        items: items.clone(),
    }];
    match kind {
        Kind::Sonarr | Kind::WhisparrV2 => targets.extend([
            Target::Episodes {
                series_id: 1,
                items: items.clone(),
            },
            Target::Season {
                series_id: 1,
                season: 1,
                items: items.clone(),
            },
            Target::Series {
                series_id: 1,
                seasons: 1,
                items,
            },
        ]),
        Kind::Lidarr => targets.push(Target::Artist {
            artist_id: 1,
            items,
        }),
        Kind::Readarr => targets.push(Target::Author {
            author_id: 1,
            items,
        }),
        Kind::Radarr | Kind::WhisparrV3 => {}
    }
    targets
}

/// Runs every client method against `server` and reports the ones that fail or read nothing.
async fn calls(client: &ArrClient, kind: Kind) -> Vec<String> {
    let mut problems = Vec::new();
    note(&mut problems, "system_status", client.system_status().await);
    note(&mut problems, "queue_total", client.queue_total().await);
    let busy = note(&mut problems, "busy_ids", client.busy_ids(1).await);
    if busy.is_some_and(|ids| ids.is_empty()) {
        problems.push("busy_ids read no id from the documented responses".to_owned());
    }
    for wanted in [Wanted::Missing, Wanted::Cutoff] {
        let call = format!("wanted_page of {}", wanted.path());
        let page = client.wanted_page(wanted, 1, 10, RadarrRelease::Physical);
        if note(&mut problems, &call, page.await).is_some_and(|page| page.records.is_empty()) {
            problems.push(format!(
                "{call} read no record from the documented response"
            ));
        }
    }
    for target in targets(kind) {
        note(&mut problems, "search", client.search(&target).await);
    }
    note(
        &mut problems,
        "command_status",
        client.command_status(1).await,
    );
    problems
}

/// The differences between the body of a command request and the upstream class of its command.
/// `sent` collects the names of the commands the client sent.
fn body_problems(contract: &Contract, body: &[u8], sent: &mut BTreeSet<String>) -> Vec<String> {
    let Ok(Value::Object(fields)) = serde_json::from_slice::<Value>(body) else {
        return vec!["POST command: the body is not a JSON object".to_owned()];
    };
    let Some(name) = fields.get("name").and_then(Value::as_str) else {
        return vec!["POST command: the body has no string \"name\"".to_owned()];
    };
    sent.insert(name.to_owned());
    let Some(properties) = contract.commands.get(name) else {
        return vec![format!(
            "command {name}: the contract pins no upstream class for it"
        )];
    };
    let mut problems = Vec::new();
    for field in fields.keys().filter(|field| *field != "name") {
        if !properties.contains(field) {
            problems.push(format!(
                "command {name}: field \"{field}\" is not a property of the upstream class"
            ));
        }
    }
    for property in properties {
        if !fields.contains_key(property) {
            problems.push(format!(
                "command {name}: the upstream class has the property \"{property}\" and the body lacks it"
            ));
        }
    }
    problems
}

/// The requests the spec does not describe and the operations no request reached.
fn traffic(contract: &Contract, received: &[Request]) -> Vec<String> {
    let mut problems = Vec::new();
    let mut requested = BTreeSet::new();
    let mut sent = BTreeSet::new();
    for request in received {
        let Some((key, operation)) = operation_of(&contract.operations, request) else {
            problems.push(format!(
                "{} {}: no such operation in the contract",
                request.method,
                request.url.path()
            ));
            continue;
        };
        requested.insert(key);
        if request.method.as_str() == "POST" && key.ends_with("/command") {
            problems.extend(body_problems(contract, &request.body, &mut sent));
        }
        for (name, _) in request.url.query_pairs() {
            if !operation.parameters.iter().any(|known| *known == name) {
                problems.push(format!(
                    "{key}: query parameter \"{name}\" is not in the spec"
                ));
            }
        }
    }
    problems.extend(
        contract
            .commands
            .keys()
            .filter(|name| !sent.contains(*name))
            .map(|name| {
                format!("command {name}: the contract pins its upstream class and the client never sends it")
            }),
    );
    problems.extend(
        contract
            .operations
            .keys()
            .filter(|key| !requested.contains(key))
            .map(|key| {
                format!(
                    "{key}: the contract lists the operation and the client does not request it"
                )
            }),
    );
    problems
}

/// Everything in which the client for `kind` and `contract` differ, one line each, sorted.
async fn check(kind: Kind, contract: &Contract) -> Vec<String> {
    let mut problems = shapes(kind, contract);
    let server = MockServer::start().await;
    let operations = contract.operations.clone();
    Mock::given(any())
        .respond_with(
            move |request: &Request| match operation_of(&operations, request) {
                Some((_, operation)) => {
                    ResponseTemplate::new(200).set_body_json(&operation.response)
                }
                None => ResponseTemplate::new(404),
            },
        )
        .mount(&server)
        .await;
    let options = Options {
        timeout: Duration::from_secs(5),
        attempts: 1,
        ..Options::default()
    };
    match ArrClient::new(kind, &server.uri(), "key", options) {
        Ok(client) => problems.extend(calls(&client, kind).await),
        Err(err) => problems.push(format!("client: {err}")),
    }
    let received = server.received_requests().await.unwrap_or_default();
    problems.extend(traffic(contract, &received));
    problems.sort();
    problems.dedup();
    problems
}

/// The id field of a queue or history record, a record of the wanted lists and the query parameter
/// that asks for its parent, as the pinned specs have them. Written by hand: nothing of a spec is
/// copied.
fn shape(kind: Kind) -> (&'static str, Value, Option<&'static str>) {
    let date = "2020-01-02T03:04:05Z";
    match kind {
        Kind::Sonarr => (
            "episodeId",
            json!({"id": 1, "seriesId": 1, "seasonNumber": 1, "episodeNumber": 1, "title": "string",
                   "airDateUtc": date, "monitored": true,
                   "series": {"monitored": true, "tags": [1], "qualityProfileId": 1}}),
            Some("includeSeries"),
        ),
        Kind::WhisparrV2 => (
            "episodeId",
            json!({"id": 1, "seriesId": 1, "seasonNumber": 1, "title": "string",
                   "releaseDate": {"year": 2020, "month": 1, "day": 2}, "monitored": true,
                   "series": {"monitored": true, "tags": [1], "qualityProfileId": 1}}),
            Some("includeSeries"),
        ),
        Kind::Radarr => (
            "movieId",
            json!({"id": 1, "title": "string", "year": 1, "monitored": true,
                   "physicalRelease": date, "digitalRelease": date, "inCinemas": date,
                   "isAvailable": true, "tags": [1], "qualityProfileId": 1}),
            None,
        ),
        Kind::WhisparrV3 => (
            "movieId",
            json!({"id": 1, "title": "string", "year": 1, "monitored": true, "releaseDate": date,
                   "tags": [1], "qualityProfileId": 1}),
            None,
        ),
        Kind::Lidarr => (
            "albumId",
            json!({"id": 1, "title": "string", "monitored": true, "releaseDate": date,
                   "artistId": 1, "artist": {"monitored": true, "tags": [1], "qualityProfileId": 1}}),
            Some("includeArtist"),
        ),
        Kind::Readarr => (
            "bookId",
            json!({"id": 1, "title": "string", "monitored": true, "releaseDate": date,
                   "authorId": 1, "author": {"monitored": true, "tags": [1], "qualityProfileId": 1}}),
            Some("includeAuthor"),
        ),
    }
}

/// The commands the client sends for `kind` and the properties of their upstream classes, written
/// by hand: nothing of a class is copied.
fn commands(kind: Kind) -> BTreeMap<String, Vec<String>> {
    let list: &[(&str, &[&str])] = match kind {
        Kind::Sonarr | Kind::WhisparrV2 => &[
            ("EpisodeSearch", &["episodeIds"]),
            ("SeasonSearch", &["seasonNumber", "seriesId"]),
            ("SeriesSearch", &["seriesId"]),
        ],
        Kind::Radarr | Kind::WhisparrV3 => &[("MoviesSearch", &["movieIds"])],
        Kind::Lidarr => &[
            ("AlbumSearch", &["albumIds"]),
            ("ArtistSearch", &["artistId"]),
        ],
        Kind::Readarr => &[
            ("AuthorSearch", &["authorId"]),
            ("BookSearch", &["bookIds"]),
        ],
    };
    list.iter()
        .map(|(name, properties)| {
            (
                (*name).to_owned(),
                properties.iter().map(|&p| p.to_owned()).collect(),
            )
        })
        .collect()
}

fn operation(parameters: &[&str], response: Value) -> Operation {
    Operation {
        parameters: parameters.iter().map(|&name| name.to_owned()).collect(),
        response,
    }
}

/// A contract with the shape the pinned specs have for the operations of the client.
fn sample(kind: Kind) -> Contract {
    let (id, record, include) = shape(kind);
    let paged =
        |record: Value| json!({"page": 1, "pageSize": 1, "totalRecords": 1, "records": [record]});
    let status = json!({"appName": "string", "version": "string"});
    let command = json!({"id": 1, "name": "string", "status": "string"});
    let paging = ["page", "pageSize"];
    let mut listing = paging.to_vec();
    listing.extend(include);
    let prefix = format!(" {}/", kind.api_prefix());
    let operations = [
        ("GET system/status", operation(&[], status)),
        ("GET queue", operation(&paging, paged(json!({ id: 1 })))),
        (
            "GET history/since",
            operation(&["date", "eventType"], json!([{ id: 1 }])),
        ),
        (
            "GET wanted/missing",
            operation(&listing, paged(record.clone())),
        ),
        ("GET wanted/cutoff", operation(&listing, paged(record))),
        ("POST command", operation(&[], command.clone())),
        ("GET command/{id}", operation(&[], command)),
    ]
    .into_iter()
    .map(|(key, operation)| (key.replacen(' ', &prefix, 1), operation))
    .collect();
    Contract {
        spec: "sample".to_owned(),
        commit: "0123abc".to_owned(),
        operations,
        commands: commands(kind),
    }
}

const ALL_KINDS: [Kind; 6] = [
    Kind::Sonarr,
    Kind::Radarr,
    Kind::Lidarr,
    Kind::Readarr,
    Kind::WhisparrV2,
    Kind::WhisparrV3,
];

#[tokio::test]
async fn the_documented_shape_has_no_difference_for_any_kind() {
    for kind in ALL_KINDS {
        let problems = check(kind, &sample(kind)).await;
        assert!(problems.is_empty(), "{kind:?}: {problems:#?}");
    }
}

/// Changes the Sonarr sample and returns what [`check`] reports.
async fn planted(change: impl FnOnce(&mut Contract)) -> Vec<String> {
    let mut contract = sample(Kind::Sonarr);
    change(&mut contract);
    check(Kind::Sonarr, &contract).await
}

/// The response of one operation of a sample. An operation the sample lacks is added with a null
/// response, which [`check`] reports, so a misspelled key does not pass as a planted difference.
fn response<'a>(contract: &'a mut Contract, operation: &str) -> &'a mut Value {
    let added = || Operation {
        parameters: Vec::new(),
        response: Value::Null,
    };
    &mut contract
        .operations
        .entry(operation.to_owned())
        .or_insert_with(added)
        .response
}

fn assert_reports(problems: &[String], wanted: &[&str]) {
    for want in wanted {
        assert!(
            problems.iter().any(|problem| problem.contains(want)),
            "{problems:#?} lacks {want:?}"
        );
    }
}

#[tokio::test]
async fn a_field_that_leaves_the_spec_is_reported() {
    let problems = planted(|contract| {
        let record = &mut response(contract, "GET /api/v3/wanted/missing")["records"][0];
        record.as_object_mut().unwrap().remove("airDateUtc");
        record["series"] = json!({});
    })
    .await;
    assert_eq!(
        problems,
        [
            "GET /api/v3/wanted/missing: records[].airDateUtc: the client reads it and the spec does not document it",
            "GET /api/v3/wanted/missing: records[].series.monitored: the client reads it and the spec does not document it",
            "GET /api/v3/wanted/missing: records[].series.qualityProfileId: the client reads it and the spec does not document it",
            "GET /api/v3/wanted/missing: records[].series.tags: the client reads it and the spec does not document it",
        ]
    );
}

#[tokio::test]
async fn a_required_field_that_leaves_the_spec_fails_the_client_too() {
    let problems = planted(|contract| {
        *response(contract, "GET /api/v3/command/{id}") = json!({"name": "string"});
    })
    .await;
    assert_reports(
        &problems,
        &[
            "GET /api/v3/command/{id}: id: the client reads it and the spec does not document it",
            "GET /api/v3/command/{id}: status: the client reads it",
            "GET /api/v3/command/{id}: does not decode: missing field `id`",
            "command_status on the documented response: arr: decode command/1",
        ],
    );
}

#[tokio::test]
async fn a_field_of_another_family_that_enters_the_spec_is_reported() {
    let problems = planted(|contract| {
        response(contract, "GET /api/v3/queue")["records"][0]["movieId"] = json!(1);
    })
    .await;
    assert_eq!(
        problems,
        ["GET /api/v3/queue: records[].movieId: listed as absent and the spec documents it"]
    );
}

#[tokio::test]
async fn a_query_parameter_that_leaves_the_spec_is_reported() {
    let problems = planted(|contract| {
        let operation = contract.operations.get_mut("GET /api/v3/wanted/cutoff");
        operation
            .unwrap()
            .parameters
            .retain(|name| name != "includeSeries");
    })
    .await;
    assert_eq!(
        problems,
        ["GET /api/v3/wanted/cutoff: query parameter \"includeSeries\" is not in the spec"]
    );
}

#[tokio::test]
async fn an_operation_that_leaves_the_spec_is_reported() {
    let problems = planted(|contract| {
        contract.operations.remove("POST /api/v3/command");
    })
    .await;
    assert_reports(
        &problems,
        &[
            "POST /api/v3/command: the contract lacks the operation",
            "POST /api/v3/command: no such operation in the contract",
            "search on the documented response: arr: POST command: HTTP 404",
        ],
    );
}

#[tokio::test]
async fn an_operation_the_client_does_not_request_is_reported() {
    let problems = planted(|contract| {
        let extra = Operation {
            parameters: Vec::new(),
            response: json!([]),
        };
        contract
            .operations
            .insert("GET /api/v3/series".to_owned(), extra);
    })
    .await;
    assert_eq!(
        problems,
        ["GET /api/v3/series: the contract lists the operation and the client does not request it"]
    );
}

#[tokio::test]
async fn an_example_that_proves_nothing_is_reported() {
    let problems = planted(|contract| {
        response(contract, "GET /api/v3/wanted/missing")["records"] = json!([]);
        response(contract, "GET /api/v3/queue")["records"][0]["episodeId"] = Value::Null;
        response(contract, "GET /api/v3/history/since")[0]["episodeId"] = Value::Null;
        response(contract, "GET /api/v3/wanted/cutoff")["records"][0]["airDateUtc"] =
            json!("string");
        response(contract, "GET /api/v3/system/status")["version"] = Value::Null;
    })
    .await;
    assert_reports(
        &problems,
        &[
            "GET /api/v3/wanted/missing: records: empty: the client reads it",
            "wanted_page of wanted/missing read no record from the documented response",
            "GET /api/v3/queue: records[].episodeId: null: the client reads it",
            "busy_ids read no id from the documented responses",
            "GET /api/v3/wanted/cutoff: does not decode:",
            "wanted_page of wanted/cutoff on the documented response: arr: decode wanted/cutoff",
            "GET /api/v3/system/status: version: null: the client reads it",
            "GET /api/v3/system/status: does not decode: invalid type: null",
        ],
    );
}

#[tokio::test]
async fn a_renamed_body_field_is_reported() {
    // The planted rename seen from the contract: upstream calls the property `episodeId`, the
    // client still sends `episodeIds`.
    let problems = planted(|contract| {
        contract
            .commands
            .insert("EpisodeSearch".to_owned(), vec!["episodeId".to_owned()]);
    })
    .await;
    assert_eq!(
        problems,
        [
            "command EpisodeSearch: field \"episodeIds\" is not a property of the upstream class",
            "command EpisodeSearch: the upstream class has the property \"episodeId\" and the body lacks it",
        ]
    );
}

#[tokio::test]
async fn a_property_the_body_lacks_is_reported() {
    let problems = planted(|contract| {
        contract
            .commands
            .get_mut("SeasonSearch")
            .unwrap()
            .push("monitored".to_owned());
    })
    .await;
    assert_eq!(
        problems,
        [
            "command SeasonSearch: the upstream class has the property \"monitored\" and the body lacks it"
        ]
    );
}

#[tokio::test]
async fn a_command_without_a_pinned_class_is_reported() {
    let problems = planted(|contract| {
        contract.commands.remove("SeriesSearch");
    })
    .await;
    assert_eq!(
        problems,
        ["command SeriesSearch: the contract pins no upstream class for it"]
    );
}

#[tokio::test]
async fn a_pinned_class_the_client_never_sends_is_reported() {
    let problems = planted(|contract| {
        contract
            .commands
            .insert("RefreshSeries".to_owned(), vec!["seriesId".to_owned()]);
    })
    .await;
    assert_eq!(
        problems,
        [
            "command RefreshSeries: the contract pins its upstream class and the client never sends it"
        ]
    );
}

#[test]
fn a_body_that_is_no_command_is_reported() {
    let contract = sample(Kind::Sonarr);
    let mut sent = BTreeSet::new();
    for (body, want) in [
        (&b"[]"[..], "the body is not a JSON object"),
        (&b"not json"[..], "the body is not a JSON object"),
        (
            &b"{\"episodeIds\":[1]}"[..],
            "the body has no string \"name\"",
        ),
        (&b"{\"name\":7}"[..], "the body has no string \"name\""),
    ] {
        let problems = body_problems(&contract, body, &mut sent);
        assert_eq!(problems.len(), 1, "{problems:?}");
        assert!(problems[0].contains(want), "{problems:?} lacks {want:?}");
    }
    assert!(sent.is_empty(), "{sent:?}");
    // Boundary: the name alone is a body whose class has properties.
    let problems = body_problems(&contract, b"{\"name\":\"SeriesSearch\"}", &mut sent);
    assert_eq!(
        problems,
        [
            "command SeriesSearch: the upstream class has the property \"seriesId\" and the body lacks it"
        ]
    );
}

#[test]
fn matches_compares_segment_by_segment() {
    for (template, path, want) in [
        ("/api/v3/queue", "/api/v3/queue", true),
        ("/api/v3/command/{id}", "/api/v3/command/17", true),
        // Boundary: a parameter stands for exactly one segment that is not empty.
        ("/api/v3/command/{id}", "/api/v3/command/", false),
        ("/api/v3/command/{id}", "/api/v3/command", false),
        ("/api/v3/command/{id}", "/api/v3/command/17/extra", false),
        ("/api/v3/queue", "/api/v1/queue", false),
        ("/api/v3/queue", "/api/v3/queue/", false),
    ] {
        assert_eq!(matches(template, path), want, "{template} against {path}");
    }
}

/// Why an upstream contract test failed. The test harness prints the error of a failed test
/// with `Debug`, so `Debug` writes the lines as they are.
struct Failure(String);

impl std::fmt::Debug for Failure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

/// Checks the client for `kind` against the contract of the pinned spec `spec` and leaves the
/// receipt `upstreamspec receipts` asks for: the commit the contract was built from.
async fn upstream(kind: Kind, spec: &str) -> Result<(), Failure> {
    let cache = std::env::var(ENV_CACHE).map_err(|_| {
        Failure(format!(
            "{ENV_CACHE} is not set; run `make contracts-verify`"
        ))
    })?;
    let cache = Path::new(&cache);
    let text =
        std::fs::read_to_string(cache.join(format!("{spec}.contract.json"))).map_err(|err| {
            Failure(format!(
                "contract of {spec}: {err}; run `make contracts-verify`"
            ))
        })?;
    let contract: Contract =
        serde_json::from_str(&text).map_err(|err| Failure(format!("contract of {spec}: {err}")))?;
    if contract.spec != spec || contract.commit.is_empty() {
        return Err(Failure(format!(
            "the contract file of {spec} names the spec {:?} at commit {:?}",
            contract.spec, contract.commit
        )));
    }
    if contract.operations.is_empty() {
        return Err(Failure(format!(
            "the contract of {spec} lists no operation"
        )));
    }
    if contract.commands.is_empty() {
        return Err(Failure(format!(
            "the contract of {spec} pins no command class, so no body would be compared"
        )));
    }
    let problems = check(kind, &contract).await;
    if !problems.is_empty() {
        return Err(Failure(format!(
            "{spec} at {}:\n  {}",
            contract.commit,
            problems.join("\n  ")
        )));
    }
    std::fs::write(
        cache.join(format!("{spec}.checked")),
        format!("{}\n", contract.commit),
    )
    .map_err(|err| Failure(format!("receipt of {spec}: {err}")))
}

#[tokio::test]
#[ignore = "needs the spec cache; run `make contracts-verify`"]
async fn upstream_contract_sonarr() -> Result<(), Failure> {
    upstream(Kind::Sonarr, "sonarr").await
}

#[tokio::test]
#[ignore = "needs the spec cache; run `make contracts-verify`"]
async fn upstream_contract_radarr() -> Result<(), Failure> {
    upstream(Kind::Radarr, "radarr").await
}

#[tokio::test]
#[ignore = "needs the spec cache; run `make contracts-verify`"]
async fn upstream_contract_lidarr() -> Result<(), Failure> {
    upstream(Kind::Lidarr, "lidarr").await
}

#[tokio::test]
#[ignore = "needs the spec cache; run `make contracts-verify`"]
async fn upstream_contract_bookshelf() -> Result<(), Failure> {
    upstream(Kind::Readarr, "bookshelf").await
}

#[tokio::test]
#[ignore = "needs the spec cache; run `make contracts-verify`"]
async fn upstream_contract_chaptarr() -> Result<(), Failure> {
    upstream(Kind::Readarr, "chaptarr").await
}

#[tokio::test]
#[ignore = "needs the spec cache; run `make contracts-verify`"]
async fn upstream_contract_readarr_faustvii() -> Result<(), Failure> {
    upstream(Kind::Readarr, "readarr-faustvii").await
}

#[tokio::test]
#[ignore = "needs the spec cache; run `make contracts-verify`"]
async fn upstream_contract_whisparr_v2() -> Result<(), Failure> {
    upstream(Kind::WhisparrV2, "whisparr-v2").await
}

#[tokio::test]
#[ignore = "needs the spec cache; run `make contracts-verify`"]
async fn upstream_contract_whisparr_v3() -> Result<(), Failure> {
    upstream(Kind::WhisparrV3, "whisparr-v3").await
}

/// Chaptarr's per-format author fields are read only from the Chaptarr spec: another fork that
/// documents them fails as a stale absence, and Chaptarr without them fails as a missing field.
#[tokio::test]
async fn per_format_author_fields_are_expected_only_from_chaptarr() {
    let per_format = json!({"monitored": true, "tags": [1], "qualityProfileId": 1,
        "audiobookTags": [1], "ebookTags": [1],
        "audiobookQualityProfileId": 1, "ebookQualityProfileId": 1});
    let mut chaptarr = sample(Kind::Readarr);
    chaptarr.spec = "chaptarr".to_owned();
    for list in ["missing", "cutoff"] {
        response(&mut chaptarr, &format!("GET /api/v1/wanted/{list}"))["records"][0]["author"] =
            per_format.clone();
    }
    let problems = check(Kind::Readarr, &chaptarr).await;
    assert!(problems.is_empty(), "{problems:#?}");

    let mut bare = sample(Kind::Readarr);
    bare.spec = "chaptarr".to_owned();
    assert_reports(
        &check(Kind::Readarr, &bare).await,
        &["records[].author.audiobookTags: the client reads it and the spec does not document it"],
    );

    let mut bookshelf = sample(Kind::Readarr);
    bookshelf.spec = "bookshelf".to_owned();
    response(&mut bookshelf, "GET /api/v1/wanted/missing")["records"][0]["author"] = per_format;
    assert_reports(
        &check(Kind::Readarr, &bookshelf).await,
        &["records[].author.ebookTags: listed as absent and the spec documents it"],
    );
}
