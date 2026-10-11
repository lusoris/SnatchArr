//! Readarr forks: a run against what Bookshelf, Chaptarr and the Faustvii fork answered on
//! 2026-10-11 (docs/readarr.md names the images). The list envelopes, the history and the
//! command answers are the forks' own. Their libraries were empty, so the two book records
//! are written by hand from the fields the worker reads, and the author search the forks
//! accepted names an author they did not have.

use super::*;

/// The author the recorded `AuthorSearch` named.
const AUTHOR: i64 = 1;
/// The hand-written wanted books of that author.
const BOOKS: [i64; 2] = [11, 12];

const BOOKS_NEWEST_FIRST: &str = r#"{"page":1,"pageSize":1,"sortKey":"Books.Id","sortDirection":"descending","totalRecords":0,"records":[]}"#;
const QUEUE_LONGEST_FIRST: &str = r#"{"page":1,"pageSize":1,"sortKey":"timeleft","sortDirection":"descending","totalRecords":0,"records":[]}"#;

/// What one fork answered with an empty library.
struct Fork {
    name: &'static str,
    missing: &'static str,
    cutoff: &'static str,
    queue: &'static str,
    /// Id the fork gave the search command.
    command_id: i64,
    /// When it queued, started and ended the command.
    command_at: &'static str,
}

const FORKS: [Fork; 3] = [
    Fork {
        name: "Bookshelf 0.4.21.182",
        missing: BOOKS_NEWEST_FIRST,
        cutoff: BOOKS_NEWEST_FIRST,
        queue: QUEUE_LONGEST_FIRST,
        command_id: 1,
        command_at: "2026-10-11T13:42:08Z",
    },
    // Chaptarr sorts by "default" and names no sort key on the cutoff list.
    Fork {
        name: "Chaptarr 0.9.965.0",
        missing: r#"{"page":1,"pageSize":1,"sortKey":"Books.Id","sortDirection":"default","totalRecords":0,"records":[]}"#,
        cutoff: r#"{"page":1,"pageSize":1,"sortDirection":"default","totalRecords":0,"records":[]}"#,
        queue: r#"{"page":1,"pageSize":1,"sortKey":"timeleft","sortDirection":"ascending","totalRecords":0,"records":[]}"#,
        command_id: 2,
        command_at: "2026-10-11T13:42:37Z",
    },
    Fork {
        name: "Readarr 0.10.0.0 (Faustvii)",
        missing: BOOKS_NEWEST_FIRST,
        cutoff: BOOKS_NEWEST_FIRST,
        queue: QUEUE_LONGEST_FIRST,
        command_id: 1,
        command_at: "2026-10-11T13:42:58Z",
    },
];

impl Fork {
    /// The two wanted lists: the snatch kind, the path the worker reads and the answer.
    fn lists(&self) -> [(SnatchKind, &'static str, &'static str); 2] {
        [
            (SnatchKind::Missing, "/api/v1/wanted/missing", self.missing),
            (SnatchKind::Upgrade, "/api/v1/wanted/cutoff", self.cutoff),
        ]
    }

    /// The answer to `POST /api/v1/command` with an `AuthorSearch`. The three forks answered
    /// alike but for the id and the times.
    fn accepted(&self) -> serde_json::Value {
        let at = self.command_at;
        serde_json::json!({
            "name": "AuthorSearch", "commandName": "Author Search",
            "body": {
                "authorId": AUTHOR, "sendUpdatesToClient": true, "updateScheduledTask": true,
                "requiresDiskAccess": false, "isExclusive": false, "isTypeExclusive": false,
                "isLongRunning": false, "name": "AuthorSearch", "trigger": "manual",
                "suppressMessages": false, "clientUserAgent": "SnatchArr-probe"
            },
            "priority": "normal", "status": "started", "result": "unknown", "queued": at,
            "started": at, "trigger": "manual", "clientUserAgent": "SnatchArr-probe",
            "stateChangeTime": at, "sendUpdatesToClient": true, "updateScheduledTask": true,
            "id": self.command_id
        })
    }

    /// The answer to `GET /api/v1/command/{id}` after the command ran: it failed, because the
    /// library had no author. `duration` and the stack trace under the first line of
    /// `exception` are left out.
    fn failed(&self) -> Result<serde_json::Value, Box<dyn std::error::Error>> {
        let mut command = self.accepted();
        let fields = some(command.as_object_mut(), "a command answer")?;
        for (key, value) in [
            ("status", "failed"),
            ("message", "Failed"),
            ("ended", self.command_at),
            (
                "exception",
                "NzbDrone.Core.Datastore.ModelNotFoundException: Author with ID 1 does not exist",
            ),
        ] {
            fields.insert(key.to_owned(), serde_json::json!(value));
        }
        Ok(command)
    }
}

/// A recorded list answer that holds the two hand-written books: `records` and `totalRecords`
/// change, the rest stays as the fork sent it.
fn with_books(recorded: &str) -> Result<serde_json::Value, Box<dyn std::error::Error>> {
    let mut page: serde_json::Value = serde_json::from_str(recorded)?;
    let fields = some(page.as_object_mut(), "a list answer")?;
    let records: Vec<serde_json::Value> = BOOKS
        .iter()
        .map(|id| {
            serde_json::json!({
                "id": id, "title": format!("Book {id}"), "authorId": AUTHOR, "monitored": true,
                "releaseDate": "2001-01-01T00:00:00Z", "author": { "monitored": true }
            })
        })
        .collect();
    fields.insert("totalRecords".to_owned(), serde_json::json!(records.len()));
    fields.insert("records".to_owned(), serde_json::Value::Array(records));
    Ok(page)
}

/// What one run left behind.
struct Seen {
    /// Path of every request the fork received.
    paths: Vec<String>,
    /// Query of every wanted-list request.
    wanted_queries: Vec<String>,
    /// Body of every command the worker posted.
    commands: Vec<serde_json::Value>,
    /// Book ids the worker reported as searched.
    searched: Vec<i64>,
    events: Vec<SnatchEvent>,
}

impl Seen {
    /// Detail of every event of one type, in the order reported.
    fn details(&self, ty: EventType) -> Vec<&str> {
        self.events
            .iter()
            .filter(|e| e.r#type == ty as i32)
            .map(|e| e.detail.as_str())
            .collect()
    }

    /// Title of every event of one type, in the order reported.
    fn titles(&self, ty: EventType) -> Vec<&str> {
        self.events
            .iter()
            .filter(|e| e.r#type == ty as i32)
            .map(|e| e.title.as_str())
            .collect()
    }
}

/// Mounts what `fork` answered, with `wanted` on the list at `list`.
async fn mount_fork(
    server: &MockServer,
    fork: &Fork,
    list: &str,
    wanted: serde_json::Value,
) -> TestResult {
    let command = format!("/api/v1/command/{}", fork.command_id);
    for (endpoint, body) in [
        (list, wanted),
        ("/api/v1/queue", serde_json::from_str(fork.queue)?),
        ("/api/v1/history/since", serde_json::json!([])),
        (command.as_str(), fork.failed()?),
    ] {
        Mock::given(method("GET"))
            .and(path(endpoint))
            .respond_with(ResponseTemplate::new(200).set_body_json(body))
            .mount(server)
            .await;
    }
    Mock::given(method("POST"))
        .and(path("/api/v1/command"))
        .respond_with(ResponseTemplate::new(201).set_body_json(fork.accepted()))
        .mount(server)
        .await;
    Ok(())
}

/// Runs one Readarr cycle of `snatch` against `fork`.
async fn fork_run(
    fork: &Fork,
    (snatch, list, wanted): (SnatchKind, &str, serde_json::Value),
    await_command: bool,
) -> Result<Seen, Box<dyn std::error::Error>> {
    let api = FakeApi {
        filter_unprocessed: BOOKS.to_vec(),
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_fork(&server, fork, list, wanted).await?;

    let mut run = default_run(&server.uri());
    run.app = AppKind::Readarr as i32;
    run.snatch = snatch as i32;
    some(run.policy.as_mut(), "run policy")?.await_command = await_command;
    run_short_polling(client, run).await;

    let requests = some(server.received_requests().await, "recorded requests")?;
    let mut commands = Vec::new();
    for request in requests.iter().filter(|r| r.method == "POST") {
        commands.push(serde_json::from_slice(&request.body)?);
    }
    let completed = api.completed_requests.lock().await;
    let searched = some(completed.first(), "a CompleteRun call")?
        .searched
        .iter()
        .map(|item| item.entity_id)
        .collect();
    Ok(Seen {
        paths: requests.iter().map(|r| r.url.path().to_owned()).collect(),
        wanted_queries: requests
            .iter()
            .filter(|r| r.url.path() == list)
            .map(|r| r.url.query().unwrap_or_default().to_owned())
            .collect(),
        commands,
        searched,
        events: api.reported_events.lock().await.clone(),
    })
}

/// Positive: on every fork and both wanted lists, the wanted books of one author go out as one
/// `AuthorSearch` with the author's id, under `/api/v1`, and both books count as searched.
#[tokio::test]
async fn wanted_books_of_an_author_are_one_author_search() -> TestResult {
    for fork in &FORKS {
        for (snatch, list, recorded) in fork.lists() {
            let what = format!("{} {list}", fork.name);
            let seen = fork_run(fork, (snatch, list, with_books(recorded)?), false).await?;
            assert_eq!(
                seen.commands,
                vec![serde_json::json!({"name": "AuthorSearch", "authorId": AUTHOR})],
                "{what}: commands posted"
            );
            assert_eq!(seen.searched, BOOKS, "{what}: books searched");
            let command = format!("command {}", fork.command_id);
            assert_eq!(
                seen.details(EventType::SearchDispatched),
                vec![command.as_str(); BOOKS.len()],
                "{what}: one dispatch event per book, with the fork's command id"
            );
            assert!(
                seen.paths.iter().all(|p| p.starts_with("/api/v1/")),
                "{what}: a request left /api/v1: {:?}",
                seen.paths
            );
            assert!(
                !seen.wanted_queries.is_empty()
                    && seen
                        .wanted_queries
                        .iter()
                        .all(|q| q.contains("includeAuthor=true")),
                "{what}: the list is read with its authors: {:?}",
                seen.wanted_queries
            );
        }
    }
    Ok(())
}

/// Negative: the lists exactly as the forks sent them hold no book, so nothing is searched.
#[tokio::test]
async fn an_empty_fork_library_is_not_searched() -> TestResult {
    for fork in &FORKS {
        for (snatch, list, recorded) in fork.lists() {
            let what = format!("{} {list}", fork.name);
            let seen =
                fork_run(fork, (snatch, list, serde_json::from_str(recorded)?), false).await?;
            assert_eq!(seen.commands, Vec::<serde_json::Value>::new(), "{what}");
            assert_eq!(seen.searched, Vec::<i64>::new(), "{what}");
            assert!(
                seen.details(EventType::SearchDispatched).is_empty(),
                "{what}: nothing dispatched"
            );
            assert_eq!(
                seen.wanted_queries.len(),
                1,
                "{what}: the list is read once"
            );
        }
    }
    Ok(())
}

/// Negative: a fork that reports the author search as failed is reported as such. The books
/// still count as searched, as the fork accepted the command.
#[tokio::test]
async fn a_failed_author_search_is_reported() -> TestResult {
    for fork in &FORKS {
        let (snatch, list, recorded) = fork.lists()[0];
        let seen = fork_run(fork, (snatch, list, with_books(recorded)?), true).await?;
        assert_eq!(
            seen.titles(EventType::SearchFailed),
            vec![format!("command {} failed", fork.command_id).as_str()],
            "{}",
            fork.name
        );
        assert!(
            seen.titles(EventType::SearchCompleted).is_empty(),
            "{}: nothing completed",
            fork.name
        );
        assert_eq!(seen.searched, BOOKS, "{}", fork.name);
    }
    Ok(())
}
