<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->
<!-- markdownlint-disable MD013 -->

# ADR-0050: Media execution moves to torcul; SnatchArr triggers jobs and shows their state

- **Status:** Accepted
- **Date:** 2026-11-23
- **Deciders:** lusoris
- **Supersedes:** the execution parts of [ADR-0041](0041-rule-driven-library-processing.md), listed in section 3 below; the rest of ADR-0041 still binds. It also supersedes where two [ADR-0038](0038-vmafx-library-quality-scoring.md) answers place their library file reads, `OQ-0038-1` and `OQ-0038-2`, by the answer to `OQ-0050-1` in section 5; the rest of ADR-0038 still binds
- **Related:** [ADR-0001](0001-go-control-plane-rust-snatch-worker.md), [ADR-0010](0010-keep-api-worker-split.md), [ADR-0038](0038-vmafx-library-quality-scoring.md), [ADR-0041](0041-rule-driven-library-processing.md); torcul ADR-0001 and ADR-0002; issues #789 to #797

## Context

[ADR-0041](0041-rule-driven-library-processing.md) (2026-10-26) split rule-driven library processing between two owners. SnatchArr was to run every lossless step itself: the Rust worker would mount the library, run `mkvpropedit`, `mkvmerge` and `ffmpeg` audio filters, write staging files, run the pre-replace verification battery, swap the files and keep a trash folder for rollback. VMAFx would run every lossy video encode behind an encode-job API that SnatchArr calls.

Three things changed after that record:

1. **The encode-job contract left VMAFx.** It started as ADR-3378 in the VMAFx repository and moved on 2026-10-27 to torcul, the encode service of the Vidiko organisation (torcul `README.md`, section "What it does"). The "VMAFx encode-job API ADR" that ADR-0041 waits for will not appear in VMAFx.
2. **Track policy and remux left SnatchArr.** On 2026-11-17 the operator placed library track selection and remux with torcul, not SnatchArr (paraphrased; ledger `snatcharr-media-track-policy-20261117-withdrawn`, 2026-11-17, topic: track policy owner). The same day torcul accepted its ADR-0002, which runs the whole pipeline in process: probe, plan, demux, decode, filter, encode, mux, verify, apply.
3. **All media execution, apply and rollback included, now belongs to torcul.** The operator decided on 2026-11-23 that torcul owns all media execution, including apply and rollback; SnatchArr keeps the rules, the integrations, the queue and the UI, triggers torcul jobs and shows their state (paraphrased; ledger `media-execution-ownership-20261123`, 2026-11-23, topic: media execution owner).

No execution code was ever written in SnatchArr. At `main` commit `1ae38363` no file under `api/`, `worker/`, `web/src/` or `deploy/` names `mkvmerge`, `mkvpropedit`, `ffprobe` or `loudnorm`, and the branches opened for this work hold only documents (`docs/adr-0038-vmafx`, `docs/adr-0041-rule-reencode`) or no commits (`feat/track-policy`).

### Names used in this record

- **torcul**: the video encode and media execution service. Meaning: a press, as in a wine or oil press: a source file goes in and a processed file comes out. Origin: Latin *torcular* (press), from *torquere* (to twist), shortened. Pronunciation: TOR-kul (/ˈtɔr.kʊl/). Source: torcul `README.md`, section "The name". Repository: `github.com/vidiko/torcul` (public, EUPL-1.2).
- **Vidiko**: the GitHub organisation (`github.com/vidiko`) that holds torcul, VMAFx and Pelorus. Its repositories record no meaning, origin or pronunciation for the organisation name as of 2026-11-23; this record names it only as the owner of those repositories.
- **VMAFx**: the fork of Netflix VMAF that torcul uses for quality scoring, `vmaf-tune auto` and encoder comparison. Meaning, origin and pronunciation ("vee-maf-ex") as recorded in [ADR-0038](0038-vmafx-library-quality-scoring.md), section "Verified Interface Baseline".
- **Pelorus**: the GPU pre-encode pipeline (Vulkan compute filters on frames in video memory) that sits beside VMAFx. Meaning, origin (Latin *Pelorus*, a ship navigation sighting instrument) and pronunciation ("puh-lor-uhs") as recorded in [ADR-0038](0038-vmafx-library-quality-scoring.md), row "Output Schema"; repository `github.com/vidiko/pelorus`.

## Decision

### 1. Ownership

| Concern | Owner | Recorded in |
| :--- | :--- | :--- |
| Rule data: preferred languages, commentary, forced and SDH policies, audio options, quality target, size cap, allowed codecs, rollback retention | SnatchArr | ADR-0041 sections 2, 5, 7, 10 (rule options only) |
| Rule scoping hierarchy, typed schema, validation | SnatchArr | ADR-0041 section 10 |
| Metadata only SnatchArr holds: original language from `*arr` or Jellyfin, media type, library and instance scope | SnatchArr | ADR-0041 section 2 |
| `library_processing` module switch | SnatchArr | ADR-0041 section 1, [ADR-0025](0025-module-switches.md) |
| Queue of library items, schedule windows that decide when a job is triggered | SnatchArr | this record, section 2 |
| Probe, stream plan, header edit, remux, audio transcode and loudness tags, video encode | torcul | torcul ADR-0002 decisions 1 to 3; torcul issues 2 to 10 |
| Staging, verification before write, apply, rollback copy, restore | torcul | torcul ADR-0002 decision 4; ADR-0041 section 8 gates kept as requirements in section 3 below; apply and rollback extension of torcul ADR-0002 ordered in ledger `torcul-media-execution-owner-20261123` (open order, not written at torcul commit `f3929d4d`) |
| Bounded execution pools, yield to live playback, GPU placement | torcul | torcul ADR-0002 decision 6; torcul issue 6 |
| Quality scores of encodes | VMAFx, called by torcul | torcul ADR-0001 |
| Score of a staged upgrade candidate against the incumbent library copy it would replace (ADR-0038 Case 2, queue rule `RuleNotAnUpgrade`) | torcul, as a read-only score job; VMAFx scores inside its verdict | this record, section 2; torcul issue 22 |
| GPU pre-encode processing and its telemetry | Pelorus, as the media handoff names it | ledger `media-execution-ownership-20261123` |
| `*arr` refresh and rename, media server refresh after an apply or a rollback | SnatchArr | ADR-0041 section 8, last paragraph (`OQ-0041-15`) |
| Custom Format cutoff guard, `snatcharr-processed` `*arr` tag, planner `RuleNotAnUpgrade` | SnatchArr | ADR-0041 section 12 |
| `SNATCHARR_PROCESSED_FROM` container tag written into the processed file | torcul | ADR-0041 section 12 and this record, section 2 |
| Rule builder, dry-run preview, queue view, verification verdicts, rollback state and restore action | SnatchArr | ADR-0041 section 11 |

SnatchArr starts, links and ships no media tool and no media library. torcul is the one implementation of every step that reads or writes a media file; that includes the ADR-0038 steps that read library files, the shared hasher and VMAFx scoring of library copies, which torcul runs as jobs (answer to `OQ-0050-1`, section 5), and the ADR-0038 score of a staged upgrade candidate, a completed `*arr` download not yet imported, against the incumbent library copy it would replace, which torcul runs as a read-only score job (section 2, staged candidates).

### 2. Trigger and state surface

- **Client side.** The SnatchArr `api` is the torcul client, the same way it is the client of every other integration. The Rust `worker` stays the stateless snatch data plane of [ADR-0001](0001-go-control-plane-rust-snatch-worker.md) and [ADR-0010](0010-keep-api-worker-split.md); it gains no media capability, no library mount and no processing lease.
- **What SnatchArr sends.** Per library item: the file reference (the `*arr` file path rewritten by the path mapping in the next item), the file size `*arr` reports, the rule after scoping, the metadata inputs from section 1, the source quality from `*arr`, and the operation (plan, run, apply, rollback, or the score job of the staged candidates item below). torcul joins that source quality with the content hash of its plan-time source fingerprint (torcul ADR-0002 decision 4, apply step 3) into the `<orig_hash_and_quality>` value of the `SNATCHARR_PROCESSED_FROM` container tag (ADR-0041 section 12) and writes the tag.
- **File references.** Each torcul connection configured in SnatchArr carries a path mapping with one entry per `*arr` instance and library root: each entry maps the pair of a SnatchArr `*arr` instance and a root folder path that instance reports through its `rootfolder` endpoint to the path of the same storage inside torcul, the way [ADR-0047](0047-list-sync-across-instances.md) maps a root alias to one path per target. One connection can serve several `*arr` instances; two instances that report the same root folder path, such as `/movies`, for different libraries keep separate entries, and SnatchArr refuses a second entry for the same instance and root folder path. SnatchArr resolves a file reference only through the entries of the instance that reported the file, and triggers no job for a file outside every root mapped for that instance or under more than one of them. The same mapping holds one download entry per `*arr` instance and download folder: each maps the pair of a SnatchArr `*arr` instance and a folder under which that instance reports the output path of a completed queue item to the path of the same storage inside torcul. SnatchArr resolves such an output path only through the download entries of the instance that reported the item, and only as the candidate of a score job (staged candidates, below); every other reference keeps the library root rule above. As a requirement on torcul's job interface, its `plan` stage refuses a reference that does not resolve to a regular file under a library root configured in torcul, or whose size differs from the size `*arr` reports, and its score job refuses a candidate that does not resolve to a regular file under a download root configured in torcul or an incumbent that does not resolve to a regular file under a library root; its apply already refuses when the source fingerprint taken at plan time (device, inode, size, modification time, content hash) has changed (torcul ADR-0002 decision 4, apply step 3).
- **What SnatchArr keeps.** Per job: the torcul job id, the submission key, the state, verdicts, hashes and scores torcul reports with the highest report revision seen, the plan report, and whether torcul confirmed SnatchArr's acknowledgement of a terminal state. SnatchArr stores them in PostgreSQL next to the item, streams changes over SSE and shows them in the web app and the Jellyfin plugin.
- **After an apply or a rollback.** When SnatchArr stores a done state for an apply or a rollback, from a torcul report or from the lookup in the next item, it triggers the `*arr` refresh and optional rename and the media server item refresh through their APIs, as ADR-0041 section 8 states. SnatchArr writes one refresh row per target service in the same transaction that stores the done state, at most once per job and target service; a loop in the `leader_loops` fx group retries open rows with bounded, jittered backoff up to a retry limit, and a row that reaches the limit shows as failed in the queue view with a retry action, so neither a restart between the apply or rollback and the refreshes nor a report SnatchArr missed loses a refresh. The rows of one item keep the ADR-0041 section 8 order: its media server row starts only after its `*arr` row is done, and that row covers the `*arr` refresh and, when enabled, the rename, so no retry lets the media server refresh finish on the old file path; with the rename off, the media server row still waits for the `*arr` refresh, and a media server row whose `*arr` row failed waits until a retry of that row succeeds. A rollback puts the original file back, which is a replacement under ADR-0041 section 8 like an apply.
- **Missed reports.** A loop in the `leader_loops` fx group reads, through torcul's job lookup, the full current report of every job SnatchArr stores without a terminal state: once when the loop starts on the elected replica, then at a bounded interval. SnatchArr applies each answer like a report, under the revision rule of the ordered reports below, so a terminal apply or rollback state that torcul reached while SnatchArr was stopped, or whose report was lost, is stored with its refresh rows on the next pass. A job that torcul no longer returns before SnatchArr acknowledged it, which the job lookup requirement below leaves only to a fault on the torcul side such as lost job storage, shows as unknown in the queue view, with a refresh action, instead of a guessed outcome.
- **Staged candidates.** When an `*arr` queue item completes to an output path that a download entry resolves and ADR-0038 quality scoring is enabled, SnatchArr triggers a torcul score job while the completed download is still in that folder, before `*arr` imports it (ADR-0038 queue rule `RuleNotAnUpgrade`). The request carries the candidate reference, the incumbent library file `*arr` reports for the same item resolved through the library entries, and the `OQ-0038-5` gate: the pooled statistic, the minimum score and the largest allowed regression against the incumbent. torcul opens both files read-only, writes no file, takes no lock that blocks the `*arr` import, refuses the job while a `run`, `apply` or `rollback` job of the incumbent file is open, and returns its verdict with the pooled VMAFx scores inside it, under the same report, revision, lookup and acknowledgement rules as every other job. A candidate that `*arr` imported or removed before torcul opened it fails the job without a verdict, and the queue rule takes no action for that item. The check is therefore best effort, the timing risk that `OQ-0038-3` answer a names: no `*arr` API holds the import of one queue item (the Sonarr and Radarr v3 queue APIs list, remove and grab items, and completed download handling is one switch for the whole instance), so the score job races the import. The queue rule acts only on a verdict that arrives while the item is still in the `*arr` queue; an item that `*arr` imports first, a score job that fails or that torcul refuses, and a torcul that does not answer leave the item without a verdict, and act mode removes no item without one. SnatchArr stores the verdict and acts on it only through the queue rule, observe mode first ([ADR-0012](0012-queue-management-replaces-cleanuparr-link.md)). [torcul issue 22](https://github.com/vidiko/torcul/issues/22) records the job interface and the download folder reach; until torcul ships that job, SnatchArr triggers no score job.
- **Dry run.** The ADR-0041 `RuleTrace` preview shows torcul's plan report (its `plan` stage writes a dry-run report, torcul ADR-0002 decision 6) instead of SnatchArr-built command lines.
- **Shapes.** Field names, transport and job states come from torcul's job interface. torcul records that interface; SnatchArr adds the client after the record merges, generates or pins it from torcul's published schema, and hand-writes no shape torcul publishes.
- **Requirements on torcul's job interface.** Whatever shapes torcul publishes, SnatchArr relies on the four properties in the next items; torcul's job interface record states how it meets them.
- **One mutating job per file.** torcul runs at most one `run`, `apply` or `rollback` job per source file at a time and refuses an operation that is not a valid next step for that file, for example an `apply` without a passed verdict or a `rollback` without a rollback copy, so two requests for one item cannot race their file swaps.
- **Idempotent submission.** Every request carries a submission key that SnatchArr stores in PostgreSQL before it sends the request, and torcul returns the existing job for a key it has seen. After a timeout or a lost response SnatchArr looks the key up and submits again only when torcul holds no job for it.
- **Ordered reports.** Every job report carries a revision that rises with each change of that job. SnatchArr keeps the highest revision per job and drops a report whose revision is not higher, so a late report never replaces a newer state.
- **Job lookup.** torcul returns the full report of a job's current revision by its job id and by its submission key: the revision, the state, the verdicts, the hashes and scores, and the plan report, and keeps a job in a terminal state readable until SnatchArr acknowledges it, so SnatchArr can recover a state whose report it never received however long it was stopped. SnatchArr acknowledges a terminal job only after the transaction that stores that full report, with its refresh rows for an apply or a rollback, has committed; torcul treats a repeated acknowledgement as a no-op, SnatchArr records an acknowledgement once torcul confirms it, and the loop in the missed reports item acknowledges again every stored terminal job without a recorded acknowledgement. torcul's job interface record states how long it keeps a job after its acknowledgement.
- **Module switch.** `library_processing` stays off by default. With the switch on and no torcul connection configured, SnatchArr shows rules and the queue but triggers nothing.

### 3. Parts of ADR-0041 this record supersedes

| ADR-0041 part | Was | Now |
| :--- | :--- | :--- |
| Ownership split, SnatchArr bullet | SnatchArr runs remux, header edits, audio transcode, staging, verification, quarantine and rollback | torcul runs them; SnatchArr keeps the rule options and the integrations |
| Ownership split, VMAFx bullets | SnatchArr calls the VMAFx encode-job API for every lossy encode and every further VMAFx surface | torcul calls VMAFx; SnatchArr calls no VMAFx encode surface |
| Section 1, worker paragraph | the worker leases processing jobs, mounts the library and runs CLI tools | the `api` triggers torcul jobs; the worker is unchanged |
| Section 2, last sentence | SnatchArr runs `mkvpropedit` for flag and title edits | torcul plans and runs header edits from the rule |
| Section 3, tiers 0 to 2 | SnatchArr runs tiers 0 and 1 and delegates tier 2 to VMAFx | torcul plans the cheapest change that meets the rule |
| Sections 4 to 7, "requirements on VMAFx" | requirements on VMAFx | requirements on torcul, which uses VMAFx (torcul issues 2 to 9) |
| Section 7, audio execution | SnatchArr transcodes audio and writes loudness tags | torcul does; the audio options stay rule data |
| Section 8, staging, battery, swap, trash | SnatchArr stages, verifies, swaps and keeps `.snatcharr-trash/` | torcul stages, verifies, swaps and keeps the rollback copy (torcul ADR-0002 decision 4); the ADR-0041 section 8 battery stays the pass condition before any replacement; at torcul commit `f3929d4d`, ADR-0002 decisions 3 and 4 cover items 1 and 2 for a remux (input container kept, kept streams matching the plan, per-packet equality with the source with timing equal within one tick, stricter than both) and cover only the VMAFx score for an encode; payload equality shows that a remux decodes as its source does, not that it decodes without errors, so item 3 (zero decode errors on start, middle and end packet probes) is a requirement on torcul's verify verdict for a remux and an encode alike, and for an encode so are items 1, 2 and 4 (container and stream count matching the plan, duration delta at most 1.0 s, size below `max_encoded_percent` of the source) and the DCT `phash` Hamming distance of item 5 (at most 8), to be stated in the apply and rollback extension of torcul ADR-0002 ordered in ledger `torcul-media-execution-owner-20261123` (not written at `f3929d4d`); apply refuses replacement unless the verdict passed (decision 4, apply step 1) |
| Section 9, playback yield and pools | SnatchArr freezes worker processes and runs `remux_io` and audio pools | torcul runs bounded pools and yields; SnatchArr schedule windows decide when to trigger |
| Section 10, `RuleTrace` command column | SnatchArr shows the CLI commands it would run | SnatchArr shows torcul's plan report |
| Context item 8, licence hygiene | SnatchArr isolates GPL tools behind subprocesses | SnatchArr ships none; torcul carries its own licence record (torcul ADR-0002 decision 7) |

Everything else in ADR-0041 still binds: the module switch, the rule scoping hierarchy and schema, the UI, the Custom Format guard and the planner integration. [ADR-0038](0038-vmafx-library-quality-scoring.md) still binds except for two answers that place steps reading library files, which section 1 gives to torcul: the shared hasher in the Rust crate `worker/crates/snatch-vhash` (`OQ-0038-1`) and the optional VMAFx scoring container that the SnatchArr Compose file and Helm chart would deploy (`OQ-0038-2`). Neither exists at `main` commit `1ae38363`. The answer to `OQ-0050-1` (section 5) supersedes both placements: torcul runs the hasher (`oshash`, DCT `phash`, duration and frame-rate probe) and VMAFx scoring of library copies and of staged upgrade candidates (section 2, staged candidates) as jobs, VMAFx stays the scoring implementation, and SnatchArr builds neither the crate nor the container. SnatchArr triggers the jobs, stores the reported hashes and scores, and keeps importing hashes from a linked Stash.

### 4. Slice issues

| ADR-0041 slice | Issue | Scope after this record |
| :--- | :--- | :--- |
| S1 | #789 | Unchanged: module switch, rule schema, scoping; the dry run shows torcul's plan report |
| S2 | #790 | Execution part (worker lease, `remux_io` and audio pools, playback yield) moves to torcul; schedule windows stay as trigger windows |
| S3 | #791 | Stream and track rule options, trigger of torcul plan and remux jobs, display of the plan and the result |
| S4 | #792 | Audio rule options only; transcode and loudness tagging run in torcul |
| S5 | #793 | Trigger of torcul apply and rollback, display of the verification verdict and the rollback copy, `*arr` and media server refresh after an apply or a rollback |
| S6 | #794 | torcul job client in the `api`, replacing the VMAFx encode-job client |
| S7 | #795 | Unchanged: Custom Format guard and planner integration |
| S8 | #796 | Unchanged in kind; views read torcul job states |
| S9 | #797 | Display of the VMAFx results torcul reports (scores, comparison); no VMAFx call from SnatchArr |

S3, S5, S6, S8 and S9 wait on torcul's job interface record, and so does the dry-run preview of S1; the S1 module switch, rule schema and scoping do not.

### 5. Answered question

| ID | Question | Options | Ledger |
| :--- | :--- | :--- | :--- |
| `OQ-0050-1` | Who runs the ADR-0038 steps that read library files: the shared hasher (`oshash`, DCT `phash`, duration and frame-rate probe) and VMAFx scoring of library copies? | a) torcul runs both as jobs; SnatchArr stores the reported hashes and scores and keeps importing hashes from a linked Stash. b) Both stay SnatchArr parts, as read-only exceptions to section 1. c) The hasher moves to torcul and the scoring container stays in the SnatchArr deploy. | `snatcharr-adr-0038-file-reads-owner`, answered 2026-11-24: **a** |

The job scope of answer a covers library copies and staged upgrade candidates: torcul runs the ADR-0038 Case 2 score of a completed `*arr` download against the incumbent library copy it would replace as the read-only score job of section 2, staged candidates (torcul issue 22).

## Consequences

- **One implementation.** Every step that touches a media file lives in torcul, so verification, apply and rollback have one owner and one set of tests (HISS-19).
- **Smaller SnatchArr images.** The api and worker images carry no FFmpeg, MKVToolNix or GPL-licensed media binary.
- **Private media stays near storage.** torcul jobs run next to the library storage (torcul ADR-0002 decision 6); SnatchArr sends references and rules, not media.
- **Waiting on torcul.** Library processing in SnatchArr cannot ship before torcul publishes its job interface and an apply and rollback slice. Until then the SnatchArr side is limited to rules, queue and preview, and the staged candidate score of section 2 waits on torcul issue 22.
- **Two repositories per feature.** A processing feature now needs a torcul change and a SnatchArr change in step.
- **Revisit trigger.** Reopen this split if torcul cannot reach a shippable apply and rollback slice, or if a SnatchArr feature waits on torcul for more than one milestone (ledger `fleet-decided-media-execution-torcul-20261123`).

## Verification

- `make verify-all` passes on the commit that adds this record (documentation lint and REUSE headers).
- `grep -rn -E 'mkvmerge|mkvpropedit|ffprobe|loudnorm' api worker web/src deploy` prints nothing at `main` commit `1ae38363`: no execution code exists to remove.
- Issues #791, #793, #794 and #797 state the scope from section 4 and link this record.

## References

- [ADR-0041](0041-rule-driven-library-processing.md): rule-driven library processing, 2026-10-26.
- torcul `README.md` and `docs/adr/0001-vmafx-integration.md`, `docs/adr/0002-pipeline-architecture.md` at `github.com/vidiko/torcul` commit `f3929d4ded5b3bb0c07ad2aff053e93927ee86a1` (2026-11-17).
- torcul open issues 2 to 10 (encode job slices, pools, pipeline and remux slice), read 2026-11-23.
- [torcul issue 22](https://github.com/vidiko/torcul/issues/22): score job for a staged upgrade candidate against the incumbent library copy, and the download folder reach, opened 2026-11-24.
- Ledger `media-execution-ownership-20261123` (operator decision, 2026-11-23, paraphrased above), `fleet-decided-media-execution-torcul-20261123`, `snatcharr-media-execution-rescope-20261123`, `torcul-media-execution-owner-20261123`, `snatcharr-media-track-policy-20261117-withdrawn`, `snatcharr-adr-0038-file-reads-owner` (2026-11-24, `OQ-0050-1`, answered with option a, derived from the 2026-11-23 ownership decision: torcul runs the ADR-0038 library file reads as jobs, VMAFx keeps scoring, SnatchArr triggers the jobs and stores the results), `snatcharr-1315-r12-staged-candidate` (2026-11-24, staged upgrade candidates: torcul scores a completed `*arr` download against the incumbent library copy in a read-only score job, and the path mapping reaches the `*arr` download folder). Ledger ids name entries of the operator's decision ledger, which is not published; this record paraphrases each decision it relies on where it cites it, with date and topic, as [ADR-0047](0047-list-sync-across-instances.md) does in its references, and the torcul documents and issues above are the published sources for every torcul claim.
