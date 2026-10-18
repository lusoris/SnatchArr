<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0014: The id_matched parity week runs in shadow beside the live unblocker

- Status: accepted
- Date: 2026-10-18
- Amends: [ADR-0013](0013-force-import-id-matched-releases.md), point 7 only. Points 1 to 6
  stand: SnatchArr is meant to own the queue alone once parity holds.

## Context

ADR-0013 point 7 planned the parity week the other way round: SnatchArr's `id_matched` rule
in act mode, and the cluster's import unblocker in dry-run (`APPLY_CHANGES=false`), so that
the unblocker's "WOULD import" lines are the reference set. That puts the only writer on
the new code during the week that is meant to prove it.

Per operator direction (paraphrased), the week runs the safer way: the unblocker keeps
importing, SnatchArr's rule stays in observe mode, the default, and records what it would
import. The unblocker is removed only after seven green days.

The two sides do not see the queue at the same moments. SnatchArr sweeps every 10 minutes
(`queue.SweepInterval`); the unblocker runs hourly (`38 * * * *`). An item can be blocked
and imported by the unblocker before any sweep sees it. The unblocker's SKIP lines name the
release title only, not the download id.

## Decision

1. **Shadow, not act.** During the week `id_matched` runs in observe mode on the reference
   stack and the unblocker imports as before. SnatchArr sends no import.
2. **One shadow line per download.** At the sweep in which an `id_matched` strike reaches
   its threshold, SnatchArr logs `queue: would force import` (`instance`, `download_id`,
   `title`) and counts `snatcharr_queue_imports_total{result="would_import"}` once. The
   first sweep of any rule's strike logs `queue: item struck` (`instance`, `rule`,
   `download_id`, `title`). Tests: `api/internal/queue/shadow_test.go`.
3. **A daily parity row.** `tools/idmatchparity` reads one UTC day of the api log and of
   `kubectl logs --timestamps` of the unblocker runs, and joins them on the lowercased
   download id within a slack of two hours:
   - a would-import with an unblocker import near it is **matched**;
   - a would-import with an unblocker SKIP of the same title near it is **skipped**, which
     is neutral, because observe mode does not run the candidate check;
   - any other would-import is a **mismatch**: SnatchArr would import what the unblocker
     did not;
   - an unblocker import that SnatchArr struck under any rule in the 24 hours before,
     without a would-import, is a **mismatch**;
   - an unblocker import SnatchArr never struck is **unseen**, which is neutral.
4. **Red day.** A day is red when it has a mismatch, or more unseen downloads than matched
   ones. A sweep every 10 minutes sees most items before the hourly run, so a majority of
   unseen downloads means the shadow is not watching, including a shadow that logs nothing
   at all. The command exits 0 for a green day, 1 for a red one and 2 for unreadable input.
5. **The week starts at the deploy read-back** of a SnatchArr release that carries point 2,
   with the shadow counter present. After seven consecutive green rows the cluster removes
   the unblocker, and the rule is switched to act. Collecting the unblocker's logs with
   timestamps, the removal and the switch belong to the cluster repository.

## Consequences

- **Positive:**
  - No item depends on new code during the week; the proven writer keeps writing.
  - Each mismatch is listed with its download id, time, reason and title, so a red day can
    be traced to one queue item.
- **Negative:**
  - The act path itself (read-back, per-sweep bound) is not exercised on the reference
    stack during the week. It rests on `make arr-forceimport-live` (ADR-0013 point 6).
  - Observe mode does not run the candidate check, so a release the unblocker refuses
    shows as skipped, not as a SnatchArr refusal. Agreement on refusals is not measured.
  - An item SnatchArr saw but struck under no rule leaves no line and counts as unseen.
    The unseen guard of point 4 bounds that gap; it does not close it.
