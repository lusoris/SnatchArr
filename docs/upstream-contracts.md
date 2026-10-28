<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Upstream contracts

SnatchArr talks to Sonarr, Radarr, Lidarr, Whisparr v2 and v3, three Readarr forks and Seerr
through clients
whose wire types are written by hand. When one of those projects renames or drops a field,
a hand-written type keeps compiling and the value is silently empty. The contract tests catch that before a
release: they compare each client with the OpenAPI spec its upstream project publishes.

This page is for contributors. Running SnatchArr needs none of it.

## Run it

```bash
make contracts-verify
```

The target downloads the pinned specs on the first run (about 2.9 MB in total, into
`.cache/upstream-specs/`), so a cold cache needs network access. It is part of
`make verify-all` and runs in CI as the job `contracts` (`.github/workflows/ci.yml`).

## What a failure means

| Message | Meaning | What to do |
| --- | --- | --- |
| `<path> at commit <commit> has sha256 <new>, the manifest pins <old>` | The spec file changed between two pinned commits, or the pin is wrong. | [Renew the pin](#renewing-a-pin). |
| `goenvoy manifest at <commit12> has sha256 <new>, specs.yaml pins <old>` | The source manifest changed between two commits, or the pin is wrong. | [Renew the pin](#renewing-a-pin). |
| `<operation>: ... <field> ... the spec does not document ...` | The client reads a field the pinned spec does not have. | Fix the wire type, or the code that depends on the field. |
| `... query parameter "<name>" is not in the spec` or `no such operation in the contract` | The client sends a request the spec does not describe. | Fix the request. |
| `... listed as absent and the spec documents it` | An entry of a known-gap list is out of date. | Remove the entry. |
| `command <Name>: field "<x>" is not a property of the upstream class` or `command <Name>: the upstream class has the property "<x>" and the body lacks it` | A command body the worker sends has a field the pinned C# class lacks, or lacks one it has. | Fix the body; see [Command bodies](#command-bodies). |
| `no contract test left a receipt` | A contract test was skipped, renamed or deleted. | Restore the test; see [Receipts](#receipts). |
| `... is in the git index`, `.gitignore does not ignore ...` or `.dockerignore has no rule ...` | A spec or a file built from one is about to be committed, or could enter an image build. | Unstage the file or restore the rule; see [Licence rule](#licence-rule). |

## How it works

1. `tools/upstreamspec/specs.yaml` pins the source manifest from `golusoris/goenvoy` (`source`
   with repository, commit, path `upstream/specs.yaml` and sha256). Goenvoy owns the upstream spec
   pins (repository, release tag or branch, commit, path, sha256, license, prefix). SnatchArr
   lists only its consumer operations and, for an entry with `POST .../command`, the upstream C#
   command classes (path and sha256 at the commit of its spec).
   A new upstream release is pinned in goenvoy, whose Renovate configuration reads
   `upstream/specs.yaml` there; SnatchArr follows when its source pin moves to that commit.
2. `upstreamspec fetch` downloads each file at its commit and refuses one whose checksum
   differs. For each spec it writes a contract file: per listed operation the documented
   query parameter names and one example response in which every documented property is
   present (`tools/upstreamspec/openapi.go`). It downloads each pinned command class the
   same way and writes the camel-cased names of its public properties into the contract file
   (`tools/upstreamspec/commands.go`).
3. `upstreamspec guard` checks that nothing of the cache is committed or can enter an
   image build context.
4. The contract tests run the real client against a local server that answers with the
   example responses:
    - Go, Seerr: `TestUpstreamContract` in `api/internal/seerrclient/contract_test.go`.
    - Rust, Sonarr, Radarr, Lidarr, Whisparr v2 and v3 and the Readarr forks Bookshelf,
      Chaptarr and Faustvii/Readarr: `upstream_contract_*` in
      `worker/crates/arr-client/src/contract_tests.rs`. [Readarr](readarr.md#forks) lists
      the fork versions.
5. `upstreamspec receipts` checks that each of those tests passed.

A contract test reports four kinds of difference: a field a wire type reads that the
documented response lacks, a request the spec does not describe (path or query
parameter), a client method that fails on the documented example, a listed
operation the client never requests, and a command body that differs from its upstream class.

### Command bodies

The OpenAPI schema of `POST .../command` lists only the generic properties of a command and
forbids others, so it cannot describe the body of a search. Those fields live in one C# class
per command (`EpisodeSearchCommand`, `MoviesSearchCommand`, `AuthorSearchCommand`, ...), and
the command name is the class name without `Command`. The manifest pins each class the worker
sends. The local server of the Rust test compares every command body it receives with the
properties of the class of its `name`: an unknown field fails, and so does a missing one. A
renamed field in `CommandBody` (`worker/crates/arr-client/src/lib.rs`) therefore fails the
`contracts` job. A class with no public get/set property fails `fetch`, so an extractor that
matches nothing cannot let every body pass.

The class files are GPL-3.0 sources like the specs: they stay in the cache and are covered by
the [licence rule](#licence-rule).

### Whisparr

Whisparr tags its v3 (Eros) releases, but the spec committed with them was last generated on
2025-06-08 and lacks `wanted/missing` and `wanted/cutoff`, which the v3 API has served since
2025-11-17 (`src/Whisparr.Api.V3/Wanted/`, checked at tag `v3.1.0.2116`). Whisparr
publishes a generated spec per generation on its own branch instead: `api-docs` for v2 and
`eros-api-docs` for v3. Both are pinned with `branch`.

Whisparr's wanted records are not Sonarr's episodes or Radarr's movies, so the client reads them
with their own wire types (`SceneLike` and `WhisparrMovieLike` in
`worker/crates/arr-client/src/lib.rs`):

- A v2 scene has no `episodeNumber` and no `airDateUtc`; it carries the day of its release as
  `releaseDate`. The spec documents that `DateOnly` as an object with `year`, `month` and
  `day`, while the server writes a string such as `"2020-01-02"`
  (`STJDateOnlyConverter`); the client reads both.
- A v3 movie has one `releaseDate` in the place of Radarr's `inCinemas`, `digitalRelease` and
  `physicalRelease`, so the Radarr release-date setting does not apply to it.

### Receipts

A test command that matches no test exits 0, so a renamed or deleted contract test would
leave the gate green. Each passing contract test therefore writes `<spec>.checked` with
the pinned commit into the cache. `fetch` removes the receipts of earlier runs and
`receipts` fails for every pin without one (`tools/upstreamspec/receipts.go`).

Outside `make contracts-verify` the contract tests do not run: the Go test skips without
the `SNATCHARR_SPEC_CACHE` variable and the Rust tests are marked `#[ignore]`. Both
packages also test the comparison itself against contracts written by hand, with and
without a planted difference; those tests run in `make api-verify` and
`make worker-verify`.

## Licence rule

Sonarr, Radarr, Lidarr and the three Readarr forks publish their specs under GPL-3.0.
SnatchArr is EUPL-1.2. The
project keeps the two apart with one rule: no spec, and nothing built from a spec, is
committed or copied into an image. That covers the contract files, fixtures and
generated client code. The clients stay hand-written, and the sample payloads in the
tests are written by hand as well. The Seerr spec is MIT; the same rule applies to it so
that there is one mechanism.

`upstreamspec guard` enforces the rule (`tools/upstreamspec/guard.go`). It fails when

- a file inside the cache directory is in the git index,
- an indexed file has the content of a cache file under another name,
- no `.gitignore` rule covers the cache directory, or
- `.dockerignore` does not exclude it, or re-includes part of it.

## Third-party logos

Every integrated app shows its own logo beside its name. The logos live in `web/static/logos/`, unmodified copies of upstream files at a pinned commit. `web/src/lib/logos/logos.json` is the one record of each logo's source repository, commit, path, SHA-256, copyright, SPDX licence and trademark owner, and `REUSE.toml` carries the same copyright and licence per file.

A few vendors' terms do not allow their mark as an integration icon. These apps are declared exceptions in the manifest: the UI shows a generic category icon (chat, torrent client, media server) next to the plain product name instead. The exceptions are Discord, Emby, Microsoft Teams, rTorrent, Slack and Telegram.

`make logos-check` (CI job `logos`) runs `tools/logos/fetch.py --check` and its planted cases in `tools/logos/fetch_test.sh`. The check reads every app kind from `api/openapi/openapi.yaml` and fails, naming the app, when:

- an app has neither a logo nor a declared exception,
- a logo file is missing or its hash differs from the manifest,
- the REUSE annotation of a logo disagrees with the manifest, or
- an exception is added that is not on the declared list.

`make logos` downloads the files again from the pinned commits; to move a logo to a newer commit, change `commit` and `sha256` in the manifest, run `make logos`, then `make logos-check`.

## Renewing a pin

Renovate reads `source.repository` and `source.commit` (`customManagers` in `renovate.json`)
and opens one pull request, `upstream specs`, when `golusoris/goenvoy` has a newer commit on branch
`main` (datasource `github-digest`, which dates the commit, so the waiting time for new releases
applies). Renovate cannot renew the sha256, so these pull requests are never merged automatically.

1. Run `go run ./tools/upstreamspec fetch` (or `make contracts-verify`).
2. If `source.sha256` differs, `fetch` fails and prints the new checksum:
   `goenvoy manifest at <commit12> has sha256 <new>, specs.yaml pins <old>`.
3. Put the new sha256 into `tools/upstreamspec/specs.yaml` under `source.sha256`.
4. If an upstream spec's commit moved and a C# command class changed at that commit, `fetch` will
   report the new checksum for the class; put the new sha256 into `commands` in
   `tools/upstreamspec/specs.yaml`.
5. Run `make contracts-verify`. A difference the contract tests report now is the finding this gate
   exists for: fix the client in the same pull request.

`TestRenovateKeepsEveryPinCurrent` in `tools/upstreamspec/renovate_test.go` checks that the pattern
in `renovate.json` captures `source.repository` and `source.commit` exactly as in `specs.yaml`.
`make contracts-verify` runs the tests of `tools/upstreamspec` first, so the `contracts` job covers
a change to `renovate.json` as well.

## Download clients

The download clients publish no specs, so `make dlclients-live` checks them against real
releases instead. It starts qBittorrent, Transmission, Deluge, rTorrent, SABnzbd and NZBGet from
`tools/e2e/dlclients/compose.yaml` (pinned by digest, kept current by Renovate), sets each up as
a user would, and runs `TestLiveDownloadClients` (`api/internal/dlclients/live_test.go`)
through the adapters production uses. Each client must be reachable with its secret, report an
empty queue and the right paused state, and refuse a wrong secret; the run repeats with SABnzbd
and NZBGet paused. Two planted cases, a wrong secret for every client and a pause that never
happened, must fail first. CI runs it as the job `dlclients-live`; it needs Docker and is not
part of `make verify-all`.

Real clients behave in ways a fake did not show:

| Client | Behaviour | Where it is handled |
| --- | --- | --- |
| qBittorrent 5 | Answers HTTP 401 when the port in the `Host` header is not the port the WebUI listens on. | The compose file publishes the WebUI on the port it listens on. |
| Deluge 2 | The web UI starts without a daemon connection and answers every daemon call with "Unknown method". | The snapshot reports `ErrDelugeNoDaemon`; the user connects a daemon in the web UI's connection manager. |
| Deluge 2 | Serves no `daemon.info`, which goenvoy asks for the version ([golusoris/goenvoy#101](https://github.com/golusoris/goenvoy/issues/101)). | The version stays empty; `unknownVersion` in `live_test.go` fails once a fixed release reports it. |
| NZBGet | Starts with downloads paused. | `tools/e2e/dlclients_live.sh` resumes it before the first run. |


## Force import on Sonarr and Radarr

The ManualImport write path of rule id_matched ([ADR-0013](adr/0013-force-import-id-matched-releases.md)) is checked against real releases by `make arr-forceimport-live`. It starts Sonarr, Radarr and qBittorrent from `tools/e2e/forceimport/compose.yaml` (pinned by digest, kept current by Renovate), adds one series and one movie, and pushes a release for each by tvdb or tmdb id under a title the parser cannot match (`tools/e2e/forceimport/arr_setup.py`). qBittorrent seeds the prepared files, so grab history is the only link and the queue item blocks with the ID-match note. `TestLiveForceImport` then runs the production queue read, sweep and `ManualImport` and waits until the download has left the queue. Four planted cases must fail first: videos without an audio track, a torrent whose two files map to no episode, a second status line and no import sent. The metadata lookups of both apps need internet access. CI runs it as the job `arr-forceimport-live`; it is not part of `make verify-all`.

What the real releases showed:

| Behaviour | Where it is handled |
| :--- | :--- |
| Radarr words the note "matched to movie by ID. Manual Import required.", not "Automatic import is not possible". | `idMatchNeedles` in `api/internal/queue/sweep.go`, case `negative/id_match_radarr_note` |
| `GET /manualimport` matches `downloadId` case-sensitively; the queue lists the info hash in upper case. | The service passes the queue's own `downloadId`. |
| Radarr maps a pushed release by tmdb id only when the title's year matches the movie. | `RADARR_TITLE` in `arr_setup.py` |
| A file without an audio track is rejected ("No audio tracks detected"). | Planted case `audio` |
| Sonarr maps the only video file of a download through grab history, whatever its name; a file it maps to an episode the series lacks is rejected ("Invalid season or episode") only when the download holds more than one. | Planted case `unmapped` puts two such files in the torrent. |
## Limits

The check says "the spec documents what the client uses". It does not say more than that.

- The example of an array has one element, an enumeration its first value, `anyOf` and
  `oneOf` their first alternative. `required` and `nullable` are not read, so a field the
  spec documents as optional counts as present.
- A command body is checked for field names only, not for value types or ranges. The class
  properties are read as text, not compiled: a property with `[JsonIgnore]`, a computed one,
  an override, a static one and one without a public setter are skipped.
- A spec can be wrong about its own server. Five fields of the Seerr request list are
  returned by the server and missing from the spec; they are listed with their source in
  `decodes` in `api/internal/seerrclient/contract_test.go` and stay covered by the
  fixture tests in `client_test.go`.

## Gaps

| Client | State |
| --- | --- |
| `worker/crates/arr-client`, the retired Readarr | No pinned spec. Its wire types are the ones the Readarr forks use, and the hand-written contracts in `contract_tests.rs` cover all six kinds. The spec of Faustvii/Readarr at `v0.10.0` is byte for byte the one Readarr published at `v0.4.18.2805`. |
| `api/internal/arrclient` | Not in scope: it has no wire types of its own and calls the typed clients of [goenvoy](https://github.com/golusoris/goenvoy). |
