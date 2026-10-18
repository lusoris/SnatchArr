<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0013: SnatchArr force-imports ID-matched releases and owns the queue alone

- Status: accepted
- Date: 2026-10-18
- Extends: [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md). Its removal rules,
  their strike counts and observe-first rollout stand.
- Bounds: [ADR-0006](0006-download-client-backpressure.md) stands. SnatchArr still never
  talks to a download client directly.

## Context

Sonarr and Radarr sometimes grab a release whose title their parser cannot read, for
example a German release named after the episode title instead of its number. The *arr
still knows the release, because grab history maps the download id to the series and
episode or to the movie. It refuses the automatic import, though, and the queue item shows:

- `trackedDownloadStatus` = `warning`;
- `trackedDownloadState` = `importBlocked` (or `importPending` before it gives up);
- the status message "… matched to series by ID. Automatic import is not possible."
  (Radarr says "movie").

A manual import clears such an item, because the manual-import candidate already carries
the mapping from grab history. On the reference stack a separate job, the cluster's import
unblocker, does this hourly. SnatchArr's sweep (ADR-0012) leaves these items alone so that
it does not remove a release the unblocker is about to import (#383,
`isUnblockerCase` in `api/internal/queue/sweep.go`). Two tools therefore act on one queue,
and an operator outside the reference stack has to write and run that job alone.

The upstream APIs (Sonarr and Radarr `develop`, checked 2026-10-18) offer:

- `GET /api/v3/manualimport?downloadId=…&filterExistingFiles=true`, which lists the
  candidate files of a download. Each candidate carries its `rejections`, and the mapping
  from grab history: `series` and `episodes` on Sonarr, `movie` on Radarr.
- `POST /api/v3/command` with `name=ManualImport`, `importMode` and `files[]`
  (`ManualImportCommand`, `ManualImportFile`), which imports the files.
- `POST /api/v3/manualimport` (`ReprocessItems`), which only re-evaluates candidates and
  imports nothing. It is not the import call.

## Decision

1. **SnatchArr is the only component that acts on the *arr queue.** It removes items under
   the ADR-0012 rules and force-imports ID-matched releases under this record. The
   cluster's import unblocker is retired once SnatchArr reaches parity (point 7).
2. **A new rule, `id_matched`, force-imports instead of removing.** It matches exactly the
   items `isUnblockerCase` matches today: status `warning`, state `importBlocked` or
   `importPending`, and every status line is either the ID-match note or the item's own
   release title. Any other line leaves the item to the other rules. The rule covers
   Sonarr and Radarr, whose candidates carry the mapping. Lidarr, Readarr and Whisparr
   items stay untouched and need a manual import, as `docs/queue-management.md` says today.
3. **Only clean, mapped candidates are imported.** A candidate qualifies when it has no
   rejection of its own and is mapped: a series and at least one episode on Sonarr, a
   movie on Radarr. The import sends those candidates with `importMode=auto` through the
   `ManualImport` command. An item without a qualifying candidate is left for the operator.
   It is never removed by this rule.
4. **Bounded and observed.** The rule starts in observe mode with a threshold of one
   sweep. In observe mode it records and reports the import it would make. In act mode
   each sweep imports at most a per-instance bound of items, 25 by default (the
   unblocker's `MAX_PER_INSTANCE`). Further items wait for the next sweep.
5. **Read-back, not trust.** The command is asynchronous, so a 201 proves nothing. The next
   sweep checks that an imported download has left the queue. A download still ID-matched
   after an import is reported as a failed import (event and metric). It is not imported
   again until its strike row expires.
6. **Proven on real applications, never on the live stack.** The write path is tested
   against real Sonarr and Radarr releases started for the test, next to a real torrent
   client, in the way `make dlclients-live` tests download clients. The test reproduces
   the ID-match block through grab history. It then checks the import, and refuses planted
   cases: a candidate with a rejection, an unmapped candidate and a second status line must
   not import. No test writes to an *arr it did not start.
7. **Parity, then retirement.** Until parity the current setup stays: the unblocker
   imports, and SnatchArr skips those items and observes. For the parity week the rule runs
   in act mode on the reference stack, and the unblocker runs with `APPLY_CHANGES=false`, so
   its "WOULD import" lines are the reference set and only SnatchArr writes. Parity means
   that for one week SnatchArr imports every item the unblocker would, makes no import the
   unblocker would refuse, and leaves no failed read-back unexplained. The cluster then
   removes the unblocker. The dry-run switch and the removal belong to the cluster
   repository and are not part of this record.

## Consequences

- **Positive:**
  - One application owns the queue, so no second job writes to it behind SnatchArr's back.
  - Operators outside the reference stack get the fix without writing a job of their own.
  - The import is bounded, observed first and confirmed by read-back. The unblocker had the
    same safety rules, but SnatchArr also records and shows each import.
- **Negative:**
  - SnatchArr gains a second write call on the *arr, `ManualImport`. Its safety rests on
    the candidate checks of point 3, and each check has a planted case.
  - The proof needs real Sonarr and Radarr containers, a torrent client and a metadata
    lookup for the test series and movie. Like `make dlclients-live`, it runs in CI and
    stays out of `make verify-all`.
  - The parity week needs a step in the cluster repository (the unblocker in dry-run)
    before SnatchArr's rule is switched to act there. If both write at once, an item may be
    imported by whichever comes first, and parity cannot be measured.
