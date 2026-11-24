<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->
<!-- markdownlint-disable MD013 -->

# ADR-0050: Media execution moves to torcul; SnatchArr triggers jobs and shows their state

- **Status:** Accepted
- **Date:** 2026-11-23
- **Deciders:** lusoris
- **Supersedes:** the execution parts of [ADR-0041](0041-rule-driven-library-processing.md), listed in section 3 below; the rest of ADR-0041 still binds
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
| GPU pre-encode processing and its telemetry | Pelorus, as the media handoff names it | ledger `media-execution-ownership-20261123` |
| `*arr` refresh and rename, media server refresh after an apply | SnatchArr | ADR-0041 section 8, last paragraph (`OQ-0041-15`) |
| Custom Format cutoff guard, `snatcharr-processed` `*arr` tag, planner `RuleNotAnUpgrade` | SnatchArr | ADR-0041 section 12 |
| `SNATCHARR_PROCESSED_FROM` container tag written into the processed file | torcul | ADR-0041 section 12 and this record, section 2 |
| Rule builder, dry-run preview, queue view, verification verdicts, rollback state and restore action | SnatchArr | ADR-0041 section 11 |

SnatchArr starts, links and ships no media tool and no media library. torcul is the one implementation of every step that reads or writes a media file.

### 2. Trigger and state surface

- **Client side.** The SnatchArr `api` is the torcul client, the same way it is the client of every other integration. The Rust `worker` stays the stateless snatch data plane of [ADR-0001](0001-go-control-plane-rust-snatch-worker.md) and [ADR-0010](0010-keep-api-worker-split.md); it gains no media capability, no library mount and no processing lease.
- **What SnatchArr sends.** Per library item: the file reference as torcul resolves it, the rule after scoping, the metadata inputs from section 1, the source quality from `*arr`, and the operation (plan, run, apply, rollback). torcul joins that source quality with the content hash of its plan-time source fingerprint (torcul ADR-0002 decision 4, apply step 3) into the `<orig_hash_and_quality>` value of the `SNATCHARR_PROCESSED_FROM` container tag (ADR-0041 section 12) and writes the tag.
- **What SnatchArr keeps.** Per job: the torcul job id, the state and verdicts torcul reports, and the plan report. SnatchArr stores them in PostgreSQL next to the item, streams changes over SSE and shows them in the web app and the Jellyfin plugin.
- **After an apply.** When torcul reports an apply as done, SnatchArr triggers the `*arr` refresh and optional rename and the media server item refresh through their APIs, as ADR-0041 section 8 states.
- **Dry run.** The ADR-0041 `RuleTrace` preview shows torcul's plan report (its `plan` stage writes a dry-run report, torcul ADR-0002 decision 6) instead of SnatchArr-built command lines.
- **Shapes.** Field names, transport and job states come from torcul's job interface. torcul records that interface; SnatchArr adds the client after the record merges, generates or pins it from torcul's published schema, and hand-writes no shape torcul publishes.
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

Everything else in ADR-0041 still binds: the module switch, the rule scoping hierarchy and schema, the UI, the Custom Format guard and the planner integration. [ADR-0038](0038-vmafx-library-quality-scoring.md) is not changed by this record.

### 4. Slice issues

| ADR-0041 slice | Issue | Scope after this record |
| :--- | :--- | :--- |
| S1 | #789 | Unchanged: module switch, rule schema, scoping; the dry run shows torcul's plan report |
| S2 | #790 | Execution part (worker lease, `remux_io` and audio pools, playback yield) moves to torcul; schedule windows stay as trigger windows |
| S3 | #791 | Stream and track rule options, trigger of torcul plan and remux jobs, display of the plan and the result |
| S4 | #792 | Audio rule options only; transcode and loudness tagging run in torcul |
| S5 | #793 | Trigger of torcul apply and rollback, display of the verification verdict and the rollback copy, `*arr` and media server refresh after an apply |
| S6 | #794 | torcul job client in the `api`, replacing the VMAFx encode-job client |
| S7 | #795 | Unchanged: Custom Format guard and planner integration |
| S8 | #796 | Unchanged in kind; views read torcul job states |
| S9 | #797 | Display of the VMAFx results torcul reports (scores, comparison); no VMAFx call from SnatchArr |

S3, S5, S6 and S9 wait on torcul's job interface record.

## Consequences

- **One implementation.** Every step that touches a media file lives in torcul, so verification, apply and rollback have one owner and one set of tests (HISS-19).
- **Smaller SnatchArr images.** The api and worker images carry no FFmpeg, MKVToolNix or GPL-licensed media binary.
- **Private media stays near storage.** torcul jobs run next to the library storage (torcul ADR-0002 decision 6); SnatchArr sends references and rules, not media.
- **Waiting on torcul.** Library processing in SnatchArr cannot ship before torcul publishes its job interface and an apply and rollback slice. Until then the SnatchArr side is limited to rules, queue and preview.
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
- Ledger `media-execution-ownership-20261123` (operator decision, 2026-11-23, paraphrased above), `fleet-decided-media-execution-torcul-20261123`, `snatcharr-media-execution-rescope-20261123`, `torcul-media-execution-owner-20261123`, `snatcharr-media-track-policy-20261117-withdrawn`.
