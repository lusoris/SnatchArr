<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Upstream contracts

SnatchArr talks to Sonarr, Radarr, Lidarr, three Readarr forks and Seerr through clients
whose wire types are written by hand. When one of those projects renames or drops a field,
a hand-written type keeps compiling and the value is silently empty. The contract tests catch that before a
release: they compare each client with the OpenAPI spec its upstream project publishes.

This page is for contributors. Running SnatchArr needs none of it.

## Run it

```bash
make contracts-verify
```

The target downloads the pinned specs on the first run (about 2.3 MB in total, into
`.cache/upstream-specs/`), so a cold cache needs network access. It is part of
`make verify-all` and runs in CI as the job `contracts` (`.github/workflows/ci.yml`).

## What a failure means

| Message | Meaning | What to do |
| --- | --- | --- |
| `<path> at commit <commit> has sha256 <new>, the manifest pins <old>` | The spec file changed between two pinned commits, or the pin is wrong. | [Renew the pin](#renewing-a-pin). |
| `<operation>: ... <field> ... the spec does not document ...` | The client reads a field the pinned spec does not have. | Fix the wire type, or the code that depends on the field. |
| `... query parameter "<name>" is not in the spec` or `no such operation in the contract` | The client sends a request the spec does not describe. | Fix the request. |
| `... listed as absent and the spec documents it` | An entry of a known-gap list is out of date. | Remove the entry. |
| `no contract test left a receipt` | A contract test was skipped, renamed or deleted. | Restore the test; see [Receipts](#receipts). |
| `... is in the git index`, `.gitignore does not ignore ...` or `.dockerignore has no rule ...` | A spec or a file built from one is about to be committed, or could enter an image build. | Unstage the file or restore the rule; see [Licence rule](#licence-rule). |

## How it works

1. `tools/upstreamspec/specs.yaml` pins each spec: the upstream repository, a release tag,
   the commit of that tag, the path of the spec file and its sha256. A project that tags no
   release is pinned with a branch in the place of the tag (Bookshelf, branch `develop`);
   an entry has exactly one of the two. It also lists the operations the client calls.
2. `upstreamspec fetch` downloads each file at its commit and refuses one whose checksum
   differs. For each spec it writes a contract file: per listed operation the documented
   query parameter names and one example response in which every documented property is
   present (`tools/upstreamspec/openapi.go`).
3. `upstreamspec guard` checks that nothing of the cache is committed or can enter an
   image build context.
4. The contract tests run the real client against a local server that answers with the
   example responses:
    - Go, Seerr: `TestUpstreamContract` in `api/internal/seerrclient/contract_test.go`.
    - Rust, Sonarr, Radarr, Lidarr and the Readarr forks Bookshelf, Chaptarr and
      Faustvii/Readarr: `upstream_contract_*` in
      `worker/crates/arr-client/src/contract_tests.rs`. [Readarr](readarr.md#forks) lists
      the fork versions.
5. `upstreamspec receipts` checks that each of those tests passed.

A contract test reports four kinds of difference: a field a wire type reads that the
documented response lacks, a request the spec does not describe (path or query
parameter), a client method that fails on the documented example, and a listed
operation the client never requests.

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

## Renewing a pin

Renovate reads repository, tag and commit of each manifest entry (`customManagers` in
`renovate.json`) and opens one pull request, `upstream specs`, when an upstream project
has a newer tag. For an entry pinned on a branch a second pattern reads repository, branch
and commit, and Renovate proposes the newest commit of that branch (datasource
`github-digest`, which dates the commit, so the waiting time for new releases applies).
Renovate cannot renew the sha256, so these pull requests are never merged automatically.

1. If the `contracts` job is green, the spec file is unchanged between the two commits.
   Merge.
2. If `fetch` fails on the checksum, the spec changed. Read the upstream diff of the spec
   file between the old and the new commit.
3. Put the sha256 from the failure message into `tools/upstreamspec/specs.yaml` and run
   `make contracts-verify`.
4. A difference the contract tests report now is the finding this gate exists for: fix
   the client in the same pull request.

A repository can hold release tags of another project: `Sonarr/Sonarr` carries the Radarr
tags from v4.1 to v6, which point at Radarr commits. A package rule in `renovate.json` reads
Sonarr through its published releases (`github-releases`) instead of its tags, so those tags
are never proposed. The manifest refuses one commit under two repositories, so a Radarr tag
that still gets in at the commit of the Radarr pin fails the `contracts` job with a message
that names it; any other foreign commit fails the spec download.

`TestRenovateKeepsEveryPinCurrent` in `tools/upstreamspec/renovate_test.go` applies the
two patterns of `renovate.json` to the manifest and fails when they no longer capture
every pin, for example after the fields of an entry are reordered. `make contracts-verify` runs
the tests of `tools/upstreamspec` first, so the `contracts` job covers a change to
`renovate.json` as well.

## Limits

The check says "the spec documents what the client uses". It does not say more than that.

- The example of an array has one element, an enumeration its first value, `anyOf` and
  `oneOf` their first alternative. `required` and `nullable` are not read, so a field the
  spec documents as optional counts as present.
- Request bodies are not compared. This concerns `POST /api/v3/command` and
  `POST /api/v1/command`.
- A spec can be wrong about its own server. Five fields of the Seerr request list are
  returned by the server and missing from the spec; they are listed with their source in
  `decodes` in `api/internal/seerrclient/contract_test.go` and stay covered by the
  fixture tests in `client_test.go`.

## Gaps

| Client | State |
| --- | --- |
| `api/internal/cleanuparrclient` | No contract test. The Cleanuparr repository holds no OpenAPI file (checked at commit `520dde2c`, 2026-10-11). The fixture tests in `client_test.go` remain the only check. |
| `worker/crates/arr-client`, the retired Readarr and Whisparr | No pinned spec. Their wire types are the ones the Readarr forks, Lidarr, Sonarr and Radarr use, and the hand-written contracts in `contract_tests.rs` cover all six kinds. The spec of Faustvii/Readarr at `v0.10.0` is byte for byte the one Readarr published at `v0.4.18.2805`. |
| `api/internal/arrclient` | Not in scope: it has no wire types of its own and calls the typed clients of [goenvoy](https://github.com/golusoris/goenvoy). |
