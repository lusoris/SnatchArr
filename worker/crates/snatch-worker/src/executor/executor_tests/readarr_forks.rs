//! Readarr forks: runs against what Bookshelf, Chaptarr and the Faustvii fork answered
//! (docs/readarr.md names the images). On 2026-10-16 each library held one author, added
//! through the fork's own metadata source, with wanted books and no file: the wanted lists,
//! the `AuthorSearch` the worker posts and its status until it ended are the forks' answers.
//! A book record keeps only the fields the worker reads (`ChildLike` and `AuthorParent` in
//! `arr-client`). The queue answers and the failed search of an author the fork did not
//! have are from 2026-10-11, when the libraries were empty.

use super::*;

/// The author each fork gave the added author, and the one every `AuthorSearch` names.
const AUTHOR: i64 = 1;

/// Book ids are the forks' own: one book each on Bookshelf and Faustvii, two on Chaptarr.
const BOOKSHELF_MISSING: &str = r#"{"page":1,"pageSize":10,"sortKey":"Books.Id","sortDirection":"descending","totalRecords":1,"records":[
    {"id":1,"title":"Livia Leu: Unsere Botschafterin in Iran","monitored":true,"releaseDate":"2013-01-01T00:00:00Z","authorId":1,"author":{"monitored":true,"tags":[],"qualityProfileId":1}}]}"#;
const FAUSTVII_MISSING: &str = r#"{"page":1,"pageSize":10,"sortKey":"Books.Id","sortDirection":"descending","totalRecords":1,"records":[
    {"id":1,"title":"Project Hail Mary","monitored":true,"releaseDate":"2021-05-04T07:00:00Z","authorId":1,"author":{"monitored":true,"tags":[],"qualityProfileId":1}}]}"#;
/// Chaptarr names the profiles and tags of an author per media type.
const CHAPTARR_MISSING: &str = r#"{"page":1,"pageSize":10,"sortKey":"Books.Id","sortDirection":"default","totalRecords":2,"records":[
    {"id":1,"title":"The Martian","monitored":true,"releaseDate":"2013-03-22T00:00:00Z","authorId":1,"author":{"monitored":true,"tags":[],"audiobookTags":[],"ebookTags":[],"audiobookQualityProfileId":1,"ebookQualityProfileId":1}},
    {"id":2,"title":"Project Hail Mary","monitored":true,"releaseDate":"2021-05-04T00:00:00Z","authorId":1,"author":{"monitored":true,"tags":[],"audiobookTags":[],"ebookTags":[],"audiobookQualityProfileId":1,"ebookQualityProfileId":1}}]}"#;

/// No book had a file, so no cutoff list held a book.
const CUTOFF_NEWEST_FIRST: &str = r#"{"page":1,"pageSize":10,"sortKey":"Books.Id","sortDirection":"descending","totalRecords":0,"records":[]}"#;
const QUEUE_LONGEST_FIRST: &str = r#"{"page":1,"pageSize":1,"sortKey":"timeleft","sortDirection":"descending","totalRecords":0,"records":[]}"#;

/// One search command a fork accepted: its id, when it was queued and when it ended. The
/// forks queue and start a command in the same second.
struct Command {
    id: i64,
    queued: &'static str,
    ended: &'static str,
}

/// What one fork answered.
struct Fork {
    name: &'static str,
    missing: &'static str,
    cutoff: &'static str,
    queue: &'static str,
    /// Ids of the books on the missing list.
    books: &'static [i64],
    /// The `AuthorSearch` of the author in the library, 2026-10-16.
    search: Command,
    /// The `AuthorSearch` of an author the empty library did not have, 2026-10-11.
    absent: Command,
}

const FORKS: [Fork; 3] = [
    Fork {
        name: "Bookshelf 0.4.21.182",
        missing: BOOKSHELF_MISSING,
        cutoff: CUTOFF_NEWEST_FIRST,
        queue: QUEUE_LONGEST_FIRST,
        books: &[1],
        search: Command {
            id: 51,
            queued: "2026-10-16T21:27:20Z",
            ended: "2026-10-16T21:27:20Z",
        },
        absent: Command {
            id: 1,
            queued: "2026-10-11T13:42:08Z",
            ended: "2026-10-11T13:42:08Z",
        },
    },
    // Chaptarr sorts by "default" and names no sort key on the cutoff list.
    Fork {
        name: "Chaptarr 0.9.965.0",
        missing: CHAPTARR_MISSING,
        cutoff: r#"{"page":1,"pageSize":10,"sortDirection":"default","totalRecords":0,"records":[]}"#,
        queue: r#"{"page":1,"pageSize":1,"sortKey":"timeleft","sortDirection":"ascending","totalRecords":0,"records":[]}"#,
        books: &[1, 2],
        search: Command {
            id: 70,
            queued: "2026-10-16T21:27:22Z",
            ended: "2026-10-16T21:27:23Z",
        },
        absent: Command {
            id: 2,
            queued: "2026-10-11T13:42:37Z",
            ended: "2026-10-11T13:42:37Z",
        },
    },
    Fork {
        name: "Readarr 0.10.0.0 (Faustvii)",
        missing: FAUSTVII_MISSING,
        cutoff: CUTOFF_NEWEST_FIRST,
        queue: QUEUE_LONGEST_FIRST,
        books: &[1],
        search: Command {
            id: 47,
            queued: "2026-10-16T21:27:25Z",
            ended: "2026-10-16T21:27:25Z",
        },
        absent: Command {
            id: 1,
            queued: "2026-10-11T13:42:58Z",
            ended: "2026-10-11T13:42:58Z",
        },
    },
];

impl Command {
    /// The answer to `POST /api/v1/command` with an `AuthorSearch`. The three forks answered
    /// alike but for the id and the times.
    fn accepted(&self) -> serde_json::Value {
        let at = self.queued;
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
            "id": self.id
        })
    }

    /// The answer to `GET /api/v1/command/{id}` once the command ended: the accepted answer
    /// with `fields` set. `duration` is left out.
    fn ended(
        &self,
        fields: [(&str, &str); 4],
    ) -> Result<serde_json::Value, Box<dyn std::error::Error>> {
        let mut command = self.accepted();
        let answer = some(command.as_object_mut(), "a command answer")?;
        answer.insert("ended".to_owned(), serde_json::json!(self.ended));
        for (key, value) in fields {
            answer.insert(key.to_owned(), serde_json::json!(value));
        }
        Ok(command)
    }

    /// The search of an author in the library: no indexer was configured, so it found nothing.
    fn completed(&self) -> Result<serde_json::Value, Box<dyn std::error::Error>> {
        self.ended([
            ("status", "completed"),
            ("result", "successful"),
            ("message", "Author search completed. 0 reports downloaded."),
            ("stateChangeTime", self.queued),
        ])
    }

    /// The search of an author the library did not have. The stack trace under the first line
    /// of `exception` is left out.
    fn failed(&self) -> Result<serde_json::Value, Box<dyn std::error::Error>> {
        self.ended([
            ("status", "failed"),
            ("message", "Failed"),
            (
                "exception",
                "NzbDrone.Core.Datastore.ModelNotFoundException: Author with ID 1 does not exist",
            ),
            ("stateChangeTime", self.ended),
        ])
    }
}

/// One wanted list: the snatch kind, the path the worker reads and the fork's answer.
type WantedList = (SnatchKind, &'static str, serde_json::Value);

impl Fork {
    /// The two wanted lists. The cutoff list holds the books of the missing list, as no
    /// recorded cutoff list held one.
    fn lists(&self) -> Result<[WantedList; 2], Box<dyn std::error::Error>> {
        Ok([
            (
                SnatchKind::Missing,
                "/api/v1/wanted/missing",
                serde_json::from_str(self.missing)?,
            ),
            (
                SnatchKind::Upgrade,
                "/api/v1/wanted/cutoff",
                with_records(self.cutoff, self.missing)?,
            ),
        ])
    }
}

/// The list answer `envelope` with the records of the list answer `from`: `records` and
/// `totalRecords` change, the rest stays as the fork sent it.
fn with_records(
    envelope: &str,
    from: &str,
) -> Result<serde_json::Value, Box<dyn std::error::Error>> {
    let mut page: serde_json::Value = serde_json::from_str(envelope)?;
    let source: serde_json::Value = serde_json::from_str(from)?;
    let fields = some(page.as_object_mut(), "a list answer")?;
    for key in ["records", "totalRecords"] {
        let value = some(source.get(key), "a list answer field")?;
        fields.insert(key.to_owned(), value.clone());
    }
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

/// One run: the fork, the wanted list it answers, the command it accepts and the answer to
/// the poll of that command.
struct Setup<'a> {
    fork: &'a Fork,
    snatch: SnatchKind,
    list: &'a str,
    wanted: serde_json::Value,
    command: &'a Command,
    polled: serde_json::Value,
}

/// Mounts what the fork of `setup` answered.
async fn mount_fork(server: &MockServer, setup: &Setup<'_>) -> TestResult {
    let command = format!("/api/v1/command/{}", setup.command.id);
    for (endpoint, body) in [
        (setup.list, setup.wanted.clone()),
        ("/api/v1/queue", serde_json::from_str(setup.fork.queue)?),
        ("/api/v1/history/since", serde_json::json!([])),
        (command.as_str(), setup.polled.clone()),
    ] {
        Mock::given(method("GET"))
            .and(path(endpoint))
            .respond_with(ResponseTemplate::new(200).set_body_json(body))
            .mount(server)
            .await;
    }
    Mock::given(method("POST"))
        .and(path("/api/v1/command"))
        .respond_with(ResponseTemplate::new(201).set_body_json(setup.command.accepted()))
        .mount(server)
        .await;
    Ok(())
}

/// Runs one Readarr cycle of `setup`.
async fn fork_run(
    setup: Setup<'_>,
    await_command: bool,
) -> Result<Seen, Box<dyn std::error::Error>> {
    let api = FakeApi {
        filter_unprocessed: setup.fork.books.to_vec(),
        ..Default::default()
    };
    let (client, _) = start_fake_api(api.clone()).await?;
    let server = MockServer::start().await;
    mount_fork(&server, &setup).await?;

    let mut run = default_run(&server.uri());
    run.app = AppKind::Readarr as i32;
    run.snatch = setup.snatch as i32;
    let policy = some(run.policy.as_mut(), "run policy")?;
    policy.await_command = await_command;
    // The recorded books and their authors are monitored; the run reads both flags.
    policy.monitored_only = true;
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
            .filter(|r| r.url.path() == setup.list)
            .map(|r| r.url.query().unwrap_or_default().to_owned())
            .collect(),
        commands,
        searched,
        events: api.reported_events.lock().await.clone(),
    })
}

/// The search of the author in the library, on the missing list of `fork`.
fn searched_missing(fork: &Fork) -> Result<Setup<'_>, Box<dyn std::error::Error>> {
    Ok(Setup {
        fork,
        snatch: SnatchKind::Missing,
        list: "/api/v1/wanted/missing",
        wanted: serde_json::from_str(fork.missing)?,
        command: &fork.search,
        polled: fork.search.completed()?,
    })
}

/// Positive: on every fork and both wanted lists, the wanted books of the author go out as one
/// `AuthorSearch` with the author's id, under `/api/v1`, and every book counts as searched.
#[tokio::test]
async fn wanted_books_of_an_author_are_one_author_search() -> TestResult {
    for fork in &FORKS {
        for (snatch, list, wanted) in fork.lists()? {
            let what = format!("{} {list}", fork.name);
            let setup = Setup {
                snatch,
                list,
                wanted,
                ..searched_missing(fork)?
            };
            let seen = fork_run(setup, false).await?;
            assert_eq!(
                seen.commands,
                vec![serde_json::json!({"name": "AuthorSearch", "authorId": AUTHOR})],
                "{what}: commands posted"
            );
            assert_eq!(seen.searched, fork.books, "{what}: books searched");
            let command = format!("command {}", fork.search.id);
            assert_eq!(
                seen.details(EventType::SearchDispatched),
                vec![command.as_str(); fork.books.len()],
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

/// Negative: the cutoff lists exactly as the forks sent them hold no book, so nothing is
/// searched.
#[tokio::test]
async fn an_empty_cutoff_list_is_not_searched() -> TestResult {
    for fork in &FORKS {
        let list = "/api/v1/wanted/cutoff";
        let setup = Setup {
            snatch: SnatchKind::Upgrade,
            list,
            wanted: serde_json::from_str(fork.cutoff)?,
            ..searched_missing(fork)?
        };
        let seen = fork_run(setup, false).await?;
        let what = format!("{} {list}", fork.name);
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
    Ok(())
}

/// Positive: a fork that ends the author search as completed is reported as such.
#[tokio::test]
async fn a_completed_author_search_is_reported() -> TestResult {
    for fork in &FORKS {
        let seen = fork_run(searched_missing(fork)?, true).await?;
        assert_eq!(
            seen.titles(EventType::SearchCompleted),
            vec![format!("command {} completed", fork.search.id).as_str()],
            "{}",
            fork.name
        );
        assert!(
            seen.titles(EventType::SearchFailed).is_empty(),
            "{}: nothing failed",
            fork.name
        );
        assert_eq!(seen.searched, fork.books, "{}", fork.name);
    }
    Ok(())
}

/// Negative: a fork that reports the author search as failed is reported as such. The books
/// still count as searched, as the fork accepted the command.
#[tokio::test]
async fn a_failed_author_search_is_reported() -> TestResult {
    for fork in &FORKS {
        let setup = Setup {
            command: &fork.absent,
            polled: fork.absent.failed()?,
            ..searched_missing(fork)?
        };
        let seen = fork_run(setup, true).await?;
        assert_eq!(
            seen.titles(EventType::SearchFailed),
            vec![format!("command {} failed", fork.absent.id).as_str()],
            "{}",
            fork.name
        );
        assert!(
            seen.titles(EventType::SearchCompleted).is_empty(),
            "{}: nothing completed",
            fork.name
        );
        assert_eq!(seen.searched, fork.books, "{}", fork.name);
    }
    Ok(())
}
