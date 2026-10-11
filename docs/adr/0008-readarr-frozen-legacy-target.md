<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0008: Readarr stays a supported target, frozen

- Status: accepted
- Date: 2026-10-11

## Context

The Servarr team retired Readarr. The last commit of
[Readarr/Readarr](https://github.com/Readarr/Readarr), `0b79d3000d` of 2025-06-27, is the
retirement announcement, and the repository is archived. The announcement names the
reason: the project's book metadata became unusable and nobody took the project over.

Existing installs keep running, and Readarr's `/api/v1` cannot change any more. SnatchArr
reads the wanted lists and the queue and sends the `AuthorSearch` and `BookSearch`
commands.

SnatchArr supports the instance kind `readarr` today:

- the connectivity probe (`api/internal/arrclient/probe.go`),
- the Configarr import (`api/internal/configarr/parse.go`),
- the worker's client (`Kind::Readarr` in `worker/crates/arr-client/src/lib.rs`) and the
  grouping of wanted books into one search per author (`group_readarr` in
  `worker/crates/snatch-core/src/lib.rs`).

Seerr requests are not matched to Readarr items: SnatchArr has no TMDB or TVDB lookup for
Readarr (`api/internal/arrclient/resolve.go`).

Forks of Readarr exist that keep the v1 API layout ([Readarr (legacy)](../readarr.md)
names them). Whether SnatchArr works with them is a separate question
([#22](https://github.com/lusoris/SnatchArr/issues/22)).

## Decision

- `readarr` stays a supported instance kind. Its behaviour is frozen as released in
  v0.1.5.
- No feature is added for Readarr alone.
- A regression against the frozen behaviour is a bug and is fixed. The existing Readarr
  tests stay unchanged and keep passing; they are what "frozen" is measured against.
- Any other fix to the Readarr paths is made only when it also applies to a supported
  fork.
- The README, the docs and the instance form label Readarr as legacy
  ([Readarr (legacy)](../readarr.md)).
- A fork is its own target with its own tests. It is never assumed to behave like
  Readarr because it shares the API layout.
- No removal is scheduled. Removing the kind changes the public `AppKind` enumeration, so
  it needs an ADR that supersedes this one and a breaking-change commit.

## Consequences

- People who still run Readarr lose nothing.
- The Readarr paths cost little: the worker shares the Lidarr wire types, and the tests
  run in the normal gates.
- No Readarr release can break SnatchArr. A broken Readarr install, for example one
  without working metadata, is outside SnatchArr's reach: it sends search commands and
  cannot repair what Readarr finds.
- Nobody upstream answers a Readarr API question any more. A defect that needs a change
  in Readarr stays open.
