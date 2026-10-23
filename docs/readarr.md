<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Readarr (legacy)

Readarr was retired by its maintainers on 2025-06-27 and its repository is archived
([announcement](https://github.com/Readarr/Readarr#readme)). SnatchArr still supports
Readarr instances. That support is frozen
([ADR-0008](adr/0008-readarr-frozen-legacy-target.md)).

## If you run Readarr today

Nothing changes. Add the instance with the kind `readarr`, or import it from Configarr,
as before. SnatchArr goes after missing books and cutoff-unmet upgrades, sends one search
per author, and respects stamina, afterglow and foreplay like for every other instance
([Concepts](concepts.md)).

The instance form, the instance list, the dashboard and the instance page show the kind as
`readarr (legacy)`. The form adds a one-line note when you choose it. Once a connection
test has reached a fork, the list, the dashboard and the instance page name the fork
instead ([Where the name is kept](#where-the-name-is-kept)).

## What frozen means

| Kept | Not done |
| --- | --- |
| Everything Readarr support does in v0.1.5. | Features for Readarr alone. |
| Fixes for a regression in SnatchArr. The Readarr tests stay and must pass. | Fixes for a Readarr quirk that no supported fork shares. |
| The kind `readarr` in the API and in Configarr imports. No removal is scheduled. | Repairs to Readarr itself. SnatchArr sends search commands; it cannot fix what Readarr finds, for example when its metadata lookups fail. |

## Forks

Three forks of Readarr are tested targets (2026-10-11). Add one with the kind `readarr`,
like Readarr itself: there is no kind per fork. The connection test names the fork it
reached ([What the connection test names](#what-the-connection-test-names)).

| Fork | Tested version | Spec pinned at | Last commit |
| --- | --- | --- | --- |
| [Bookshelf](https://github.com/pennydreadful/bookshelf) | 0.4.21.182 | branch `develop`, commit `3ac41f7e3142` | 2026-09-11 |
| [Chaptarr](https://github.com/Chaptarr/chaptarr) | 0.9.965.0 | tag `v0.9.965`, commit `e67374f4b3fe` | 2026-09-27 |
| [Readarr (Faustvii fork)](https://github.com/Faustvii/Readarr) | 0.10.0.0 | tag `v0.10.0`, commit `7f02ffa52997` | 2026-09-02 |

No fork failed the tests, so none is listed as unsupported. A fork that is not in the
table is untested: the connection test names it "not recognised", and adding it with the
kind `readarr` is at your own risk.

### What tested means

Each fork passes two kinds of test.

- **Contract.** `upstream_contract_bookshelf`, `upstream_contract_chaptarr` and
  `upstream_contract_readarr_faustvii` in `worker/crates/arr-client/src/contract_tests.rs`
  run the worker's client against the OpenAPI spec the fork publishes at the pinned commit:
  system status, queue, history, both wanted lists, the search command and its status. Run
  them with `make contracts-verify` ([Upstream contracts](upstream-contracts.md)); the pins
  are in `tools/upstreamspec/specs.yaml`.
- **Worker run.** `worker/crates/snatch-worker/src/executor/executor_tests/readarr_forks.rs`
  runs a whole snatch, for missing books and for upgrades, against what each fork answered
  on 2026-10-16. Run it with `cargo test -p snatch-worker readarr_forks`.

The answers were recorded from these images. On 2026-10-11 each library was empty; on
2026-10-16 each held one author, added through the fork's own metadata source, with wanted
books and no file:

- Bookshelf: `ghcr.io/pennydreadful/bookshelf@sha256:67498dd5ece516867d72ee642abd6c1a66b36a135c8f7da0127109564372beb1`.
  Its status answer names the pinned commit as the package version.
- Chaptarr: `chaptarr/chaptarr@sha256:bb00d67cbe1485d1cd61ef178362a29b42aa08f86b290841aab4b115ad2647a6`.
- Readarr (Faustvii fork): `ghcr.io/faustvii/readarr@sha256:23b3dfeac0a60d3395bee862e6b954dc14bc6bd13411a89e36f92e9bbaf7bdfa`.

### What a run sends

A run sends one `AuthorSearch` command per author who has a selected wanted book, and no
`BookSearch` (`group_readarr` in `worker/crates/snatch-core/src/lib.rs`; ADR-0008 names
both commands, a run sends only the first). All three forks accepted the command with
HTTP 201.

The fork then searches every monitored book of that author, not only the books the run
selected (`AuthorSearch` in `src/NzbDrone.Core/IndexerSearch/ReleaseSearchService.cs` of
each fork at the pinned commit; Chaptarr limits it to the media profiles configured for
the author). Stamina is debited per selected book all the same
([#180](https://github.com/lusoris/SnatchArr/issues/180)).

### Limits of the tests

- The missing lists in the worker test are the forks' answers of 2026-10-16. A book record
  keeps only the fields the worker reads; the covers, links and descriptions the forks
  return are left out.
- Each fork ran the `AuthorSearch` of its author and ended it as `completed`, with
  "0 reports downloaded": no indexer was configured, so no search reached an indexer and
  nothing was grabbed. The search of an author the library did not have, recorded on
  2026-10-11, ended as `failed` on every fork; the test keeps it as its negative case.
- No book had a file, so every cutoff list was empty. The upgrade run in the worker test
  reads the books of the missing list from the fork's cutoff envelope.
- Bookshelf could not look up an author by name on 2026-10-16: its metadata source
  (`hardcover.bookinfo.pro`) did not answer the search within 100 seconds. Adding the
  author by its Hardcover id worked. Chaptarr and the Faustvii fork found the author by
  name.
- The contract tests compare what the client reads and which requests it sends. They do
  not compare the body of the search command with the spec
  ([Limits](upstream-contracts.md#limits)).
- Chaptarr's wanted lists take an optional `mediaType` (`ebook`, `audiobook` or `all`).
  SnatchArr does not send it, and Chaptarr then lists both media types
  (`GetBookMonitoringFilter` in `src/NzbDrone.Core/Books/Extensions/AuthorExtensions.cs`
  at the pinned commit). A snatch for one media type only is not possible.
- A newer version of a fork is covered once its pin is renewed
  ([Renewing a pin](upstream-contracts.md#renewing-a-pin)); the worker test keeps the
  answers of the versions in the table.

### What the connection test names

The connection test of an instance with the kind `readarr` says which product answered.
Most forks still report the name `Readarr`, so the test reads three fields of
`/api/v1/system/status`. The first row that matches decides; upper and lower case make no
difference.

| `appName` | `packageAuthor` | `version` | Named as |
| --- | --- | --- | --- |
| `Chaptarr` | any | any | Chaptarr |
| not `Readarr` | any | any | not recognised |
| `Readarr` | contains `pennydreadful` | any | Bookshelf |
| `Readarr` | contains `Faustvii` | any | Readarr (Faustvii fork) |
| `Readarr` | anything else | 0.4.19 or lower | Readarr |
| `Readarr` | anything else | higher, or not a version | not recognised |

The retired Readarr never built a version above 0.4.19, which is what the last two rows
rest on.

Two limits follow from the fields:

- `packageAuthor` is written by whoever builds the image. Bookshelf or the Faustvii fork
  from a build without that field is named "not recognised".
- A fork that reports `Readarr`, a version up to 0.4.19 and no known packager cannot be
  told from Readarr.

A name is not a statement of support: the test succeeds for every row, also for "not
recognised". The tested forks are the three in the table under [Forks](#forks). The rule
is `readarrProduct` in
`api/internal/arrclient/product.go`; `TestProbe_Product` in the same directory holds the
answers each product gave on 2026-10-11. The API carries the name as `product` of
`ProbeResult` (`api/openapi/openapi.yaml`).

### Where the name is kept

The connection test of a stored instance (`POST /instances/{instanceId}/test`, the test
button on the instance page) keeps the name with the instance. The API returns it as
`last_seen_product` of `Instance`. The instance list, the dashboard and the instance page
show it next to the kind:

| Last successful test found | Shown as |
| --- | --- |
| Bookshelf | `readarr · Bookshelf` |
| Chaptarr | `readarr · Chaptarr` |
| Readarr (Faustvii fork) | `readarr · Readarr (Faustvii fork)` |
| not recognised | `readarr · unknown product` |
| Readarr, or no test yet | `readarr (legacy)` |

Enabled instances are checked every 5 minutes on the leader (updating
`last_check_at`), and an on-demand test runs whenever you test the connection.
The test of unsaved form values (`POST /instances/test`) names the product
in its answer and keeps nothing.

- A test that fails keeps the name: an instance that does not answer is still that product.
- A change of the kind or the base URL forgets the name until the next test, whether you
  edit the instance or a Configarr import changes it. Any other edit keeps it.
- An instance of a kind that one product serves has no name.

The column is `last_seen_product` of the table `instances`
(`api/internal/store/migrations/postgres/000008_instance_product.up.sql`); the three rules
are in `api/internal/store/queries/instances.sql` and checked by
`api/internal/instances/service_integration_test.go`. The label is `instanceKindLabel` in
`web/src/lib/products.ts`.
