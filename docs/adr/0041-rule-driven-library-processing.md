<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0041: Rule-driven library processing (remux and re-encode by rules)

- **Status:** Accepted (open questions OQ-0041-1 to OQ-0041-22 answered 2026-10-26, ledger ids snatcharr-oq-0041-1 to snatcharr-oq-0041-22)
- **Date:** 2026-10-26
- **Related:** [ADR-0001](0001-go-control-plane-rust-snatch-worker.md), [ADR-0002](0002-lease-pull-work-distribution.md), [ADR-0006](0006-download-client-backpressure.md), [ADR-0010](0010-keep-api-worker-split.md), [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md), [ADR-0025](0025-module-switches.md), [ADR-0028](0028-jellyfin-plugin.md), [ADR-0035](0035-jellyfin-integration.md), ADR-0038 (VMAFx library quality scoring, pull request #746), ADR-0040 (Audiobookshelf integration, pull request #749)

## Context

### Problem statement

Media files imported across a user's libraries rarely match the long-term storage, language, container, and playback baseline of the household:

1. **Bloated multi-language streams and broken dispositions**: Blu-ray and WEB-DL imports frequently carry 5 to 15 audio tracks and 20 to 40 subtitle tracks in languages no household user speaks, alongside unlabelled commentary tracks, misflagged forced subtitles, or default-track flags pointing at a dubbed track instead of the work's original language.
2. **Container mismatches requiring purely lossless fixes**: Files arriving in MPEG-4 Part 14 (`MP4`), Audio Video Interleave (`AVI`), or MPEG transport stream (`TS`) containers—or Matroska (`MKV`) files carrying Dolby Vision Profile 7 dual-layer streams whose Enhancement Layer (`EL`) causes playback fallback on streaming clients—often need only a fast, bit-exact lossless remux or Reference Processing Unit (`RPU`) conversion rather than a lossy video re-encode.
3. **Storage efficiency vs. perceptual quality**: High-bitrate Advanced Video Coding (`H.264` / `AVC`) or High Efficiency Video Coding (`H.265` / `HEVC`) releases can often be re-encoded to AOMedia Video 1 (`AV1`) or `HEVC` at 30 % to 60 % of the original byte size while preserving perceptual transparency, provided the encoder is guided by a perceptual quality target rather than a blind fixed Constant Rate Factor (`CRF`) that either wastes bits on simple scenes or starves complex grain.
4. **High Dynamic Range (`HDR`) and audio fidelity hazards**: Naive transcoding pipelines strip SMPTE ST 2086 static `HDR10` metadata, discard SMPTE ST 2094-40 (`HDR10+`) dynamic Supplemental Enhancement Information (`SEI`) messages, corrupt Dolby Vision Profile 5 (`IPT-PQ-C2` proprietary colour space) or Profile 7 (`FEL` / `MEL`) streams, or flatten lossless object audio (`Dolby TrueHD` with Atmos, `DTS-HD Master Audio`) without keeping a compatible surround fallback.
5. **Upgrade loops with `*arr` and SnatchArr's own planner**: When an external tool re-encodes a file on disk (for example from a `25 GB` `Remux-1080p` to a `5 GB` `AV1-1080p`) or renames its codec token, **Radarr**, **Sonarr**, **Lidarr**, **Readarr**, or **Whisparr** can see the modified file as falling below the instance's quality profile or Custom Format (`CF`) cutoff—triggering an endless cycle where SnatchArr and `*arr` re-snatch the original large release, re-import it, and re-encode it again.

### Glossary of referenced projects (meaning, origin, pronunciation)

- **SnatchArr** (`/ˈsnætʃ.ɑːr/`): this repository's media acquisition and library controller; compounds English *snatch* (to grab a release from an indexer) with the pirate-themed `-arr` suffix of the `*arr` ecosystem.
- **Radarr** (`/ˈreɪ.dɑːr/`), **Sonarr** (`/ˈsoʊ.nɑːr/`), **Lidarr** (`/ˈlaɪ.dɑːr/`), **Readarr** (`/ˈriː.dɑːr/`), **Whisparr** (`/ˈwɪs.pɑːr/`), **Prowlarr** (`/ˈpraʊ.lɑːr/`), **Bazarr** (`/bəˈzɑːr/`): the `*arr` suite managing movies, television series, music, books, adult video, indexers, and subtitles; coined as puns on *radar*, *sonar*, *lidar*, *reader*, *whisper*, *prowler*, and *bazaar* plus `-arr`.
- **Configarr** (`/kənˈfɪɡ.ɑːr/`): declarative synchronisation tool for Sonarr and Radarr quality profiles and TRaSH Guides custom formats (*config* + `-arr`).
- **Seerr** (`/ˈsiː.ər/`): media request and discovery portal unifying Overseerr and Jellyseerr; from English *seer* with doubled `r`.
- **Jellyfin** (`/ˈdʒɛl.i.fɪn/`): community-driven free-software media server forked from Emby in 2018; combines *jellyfish* and *fin*.
- **Stash** (`/stæʃ/`): self-hosted metadata and streaming organizer for personal and adult video collections; from English *stash* (a private store).
- **Audiobookshelf** (`/ˈɔː.di.oʊ.bʊk.ʃɛlf/`): self-hosted audiobook and podcast server (*audiobook* + *bookshelf*).
- **VMAF** (`/ˈviː.mæf/`): Video Multi-Method Assessment Fusion, the Emmy-winning full-reference perceptual video quality metric developed by Netflix with academic partners.
- **VMAFx** (`/ˈviː.mæf.ɛks/`): `lusoris` companion project (`VMAFx/vmafx`) providing hardware-accelerated perceptual quality scoring (`vmafx`) and quality-aware encoder tuning (`vmaf-tune`); appends `x` (extended/execution) to `VMAF`.
- **golusoris** (`/ˌɡoʊ.luːˈsɔː.rɪs/`), **goenvoy** (`/ˌɡoʊˈɛn.vɔɪ/`), **sveltesentio** (`/ˌsvɛltˈsɛn.ti.oʊ/`): `lusoris` shared libraries for Go `uber-go/fx` lifecycle gates (Go + Latin *lūsōris*, "of the player"), Go outbound HTTP/webhook clients (Go + *envoy*), and Svelte 5 accessible UI components (Svelte + Latin *sentiō*, "I perceive").
- **Tdarr** (`/ˈtiː.dɑːr/`): distributed transcode and health-check system by HaveAGitGat (*Transcode Distributed Arr*).
- **Unmanic** (`/ʌnˈmæn.ɪk/`): linear per-library plugin runner by Josh.5 (*un-* + *manic*).
- **FileFlows** (`/ˈfaɪl.floʊz/`): directed-acyclic-graph file processing automation system by revenz (*file* + *flows*).
- **ab-av1** (`/ˌeɪ.biː.eɪ.viːˈwʌn/`): Rust CLI by Alex Butler (`alexheretic`) for sample-based VMAF CRF binary search (`AB` + `AV1`).
- **Av1an** (`/ˈeɪ.viː.ən/`, like *avian*): Rust scene-chunked parallel video encoding framework (`rust-av/Av1an`, blending `AV1` + *avian*).
- **HandBrake** (`/ˈhænd.breɪk/`): open-source multithreaded video transcoder founded by Eric Petit in 2003.
- **FFmpeg** (`/ˌɛf.ɛfˈɛm.pɛɡ/`): cross-platform multimedia framework founded by Fabrice Bellard (*Fast Forward* + `MPEG`).
- **MKVToolNix** (`/ˌɛm.keɪ.viːˈtuːl.nɪks/`): Matroska (`MKV`) inspection and multiplexing suite by Moritz Bunkus (`MKV` + *Tool* + `-nix`).
- **dovi_tool** (`/ˈdoʊ.vi.tuːl/`): Rust CLI and `libdovi` library by `quietvoid` for Dolby Vision (`DoVi`) `RPU` extraction, conversion, and injection.
- **hdr10plus_tool** (`/ˌeɪtʃ.diː.ɑːr.tɛnˈplʌs.tuːl/`): Rust CLI by `quietvoid` for SMPTE ST 2094-40 `HDR10+` dynamic SEI extraction and injection.
- **dovi_convert** (`/ˈdoʊ.vi.kənˈvɜːrt/`): Python tool by `cryptochrome` for lossless Dolby Vision Profile 7 to Profile 8.1 conversion with Enhancement Layer (`EL`) backup.
- **SVT Encore** (`/ˌɛs.viː.tiː ˈɑːŋ.kɔːr/`): Sveriges Television (`SVT`) Spring Boot and Redis distributed FFmpeg transcoding service (French/English *encore*).
- **Transcodarr** (`/trænzˈkoʊ.dɑːr/`): Python watch-folder and library transcode orchestrator by ReedyLab (*transcode* + `-arr`).
- **jellyfin-plugin-pre-transcode** (`/ˈdʒɛl.i.fɪn ˈplʌɡ.ɪn ˌpriː.trænzˈkoʊd/`): native `.NET 10` Jellyfin plugin by `mugurc` for background compatibility pre-transcoding.
- **HBBatchBeast** (`/ˌeɪtʃ.biːˈbætʃ.biːst/`): legacy batch wrapper for HandBrake and FFmpeg by HaveAGitGat (*HandBrake* + *Batch Beast*).
- **video_transcoding** (`/ˈvɪd.i.oʊ trænzˈkoʊ.dɪŋ/`): Ruby CLI scripts (`transcode-video.rb`) by Don and Lisa Melton.
- **sickbeard_mp4_automator** (`SMA`, `/ˈsɪk.bɪərd ˌɛm.piːˈfɔːr ˈɔː.tə.meɪ.tər/`): Python post-processing conversion and tagging script by `mdhiggins`.

### Why Jellyfin's built-in transcoding is not this

In `jellyfin/jellyfin` (`v12.3`, `2026-10-23`, commit `06de749d435e`, `GPL-2.0-only`), built-in transcoding is a live, per-playback-session streaming mechanism rather than a permanent library processor:

1. **Session-bound ephemeral output**: `MediaBrowser.Controller/MediaEncoding/TranscodingJobType.cs:6-22` defines only `Progressive`, `Hls`, and `Dash` streaming jobs negotiated against the active playback client's `DeviceProfile` (`MediaBrowser.Controller/MediaEncoding/EncodingHelper.cs`).
2. **24-hour scratch eviction**: `Emby.Server.Implementations/ScheduledTasks/Tasks/DeleteTranscodeFileTask.cs:81-89` deletes all generated transcode segments older than 24 hours (`DateTime.UtcNow.AddDays(-1)`) from `GetTranscodePath()`. If ten users play an incompatible file across a week, Jellyfin live-transcodes it ten times.
3. **No stream pruning, VMAF targeting, or `*arr` coordination**: Jellyfin core never rewrites or replaces library files on disk, never prunes unwanted audio or subtitle languages from containers, never runs a target-VMAF search or Dolby Vision Profile 7 -> 8.1 RPU conversion, and never notifies Radarr, Sonarr, Lidarr, or Whisparr of file changes (`TrickplayManager.cs:33-90` only extracts `.jpg` scrubbing sprites).

### Ecosystem survey summary (verified 2026-10-26)

Releases were read from each project's forge on 2026-10-26; the `file:line` references point into each project's tree at the listed release or commit.

| Tool | Latest release / commit | SPDX licence | Rule model | Codec & HW support | Quality-target / VMAF | HDR10 / HDR10+ / DoVi | `*arr` integration | Distributed workers | Licence & integration fit |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Tdarr** (`HaveAGitGat/Tdarr` + `Tdarr_Plugins`) | Server `3.02.00` (`2026-10-25`, `09b50f03b841`); Plugins `master` (`2026-10-15`, `87893f3f5c2f`) | Core: `LicenseRef-proprietary` (`LICENSE.md:1-52`); Plugins: `GPL-3.0-only` | Classic linear stack or DAG Flow plugins (`FlowPluginsTs/**`, 130 plugins) | `hevc`, `h264`, `av1` (`ffmpegCommandSetVideoEncoder/1.0.0/index.ts:32-38`); `nvenc`, `qsv`, `vaapi`, `amf`, `videotoolbox`, `rkmpp` probed via 1s `lavfi` encode (`hardwareUtils.ts:23-102`) | None in `FlowPluginsTs` | `checkHdr` (`index.ts:45-57`), `ffmpegCommandHdrToSdr` (`index.ts:38-42`); no built-in `dovi_tool` RPU flow plugin | `notifyRadarrOrSonarr/3.0.0/index.ts:144-251` (`RefreshMovie`/`RefreshSeries` + optional unmonitor); `applyRadarrOrSonarrNamingPolicy/1.0.0/index.ts:115-160` | `Tdarr_Server` + `Tdarr_Node` with 4 CPU/GPU pools (`README.md:45-48`) | Proprietary core forbids redistribution/embedding (`LICENSE.md:18-45`); external REST only |
| **Unmanic** (`Unmanic/unmanic`) | `0.4.1` (`2026-08-14`, `1c324b8fc397`) | `GPL-3.0-only` (`LICENSE:1-15`) | Per-library sequential Python plugin list across 4 hooks (`executor.py:58-115`) | Delegated to Python plugins invoking `ffmpeg` (`foreman.py:48-215`) | None in core | None in core | Community postprocessor plugins (`postprocessor.py:180-240`) | Remote link workers (`foreman.py:75`); limits gated by supporter tier (`foreman.py:154,174`) | `GPL-3.0-only` Python daemon; supporter-tier limits on multi-library/links |
| **FileFlows** (`fileflows/community-repository`) | Core proprietary (`26.x`); scripts `ed81a1a95ecc` (`2026-09-15`) | Core: `LicenseRef-proprietary`; Scripts: `NONE` | Visual DAG flow editor + JS scripts (`Scripts/Flow/**`) | `h264`, `hevc`, `av1`, `vp9` across CPU, NVENC, QSV, AMF, VAAPI, VideoToolbox | JS scripts for `ab-av1` (`Video - Run ab-av1.js:38-99`) and `libvmaf` (`Video - VMAF.js:42-91`) | JS scripts for `dovi_tool --crop --mode 2` (`Video - Dolby Vision - Fix crop and compatibility.js:104-120`) and `hdr10plus_tool` (`Video - Inject HDR10+ metadata.js:41-79`) | JS scripts for Radarr original language (`Radarr - Get Original Language.js:25-32`) and refresh (`Radarr - Refresh.js:43-56`) | Server + remote flow runners (paid tier limits) | Proprietary freemium core + unlicensed script repo; cannot be embedded |
| **ab-av1** (`alexheretic/ab-av1`) | `v0.11.7` (`2026-08-21`, `18c2c027b8dd`) | `MIT` (`LICENSE:1-22`) | Single-file CLI flags (`crf_search.rs:52-127`) | Any FFmpeg encoder (`libsvtav1`, `libx265`, `*_nvenc`, `*_qsv`, `*_vaapi`, `*_videotoolbox`, `crf_search.rs:74-102`) | Interpolated binary search on 20s samples for `--min-vmaf` / `--min-xpsnr` + `--max-encoded-percent` (`crf_search.rs:25-127`, `vmaf.rs:11-99`) | Passes `-pix_fmt` / `-vf` (`ffmpeg.rs:100-102`); no DoVi RPU or HDR10+ SEI re-injection | None | None | `MIT`: clean subprocess (`--stdout-format json`) or Rust crate adaptation |
| **Av1an** (`rust-av/Av1an`) | `v0.6.0` (`2026-10-26`, `641ce62d70e3`) | `GPL-3.0-only` (`LICENSE.md:1-30`) | CLI flags + scene zones (`zones.rs`) | Software encoders: `aom`, `rav1e`, `vpx`, `svt-av1`, `x264`, `x265` (`encoder/mod.rs:84-92`) | Per-scene chunk target quality search via VMAF, XPSNR, Butteraugli, SSIMULACRA2 (`target_quality.rs:30-95`) | VapourSynth/FFmpeg pipe; no built-in DoVi P7->P8.1 conversion | None | Local multi-worker chunk broker (`broker.rs`) | `GPL-3.0-only`: subprocess execution only (cannot link into `EUPL-1.2` worker) |
| **HandBrake CLI** (`HandBrake/HandBrake`) | `1.11.2` (`2026-06-07`, `9eb6c936803e`) | `GPL-2.0-only` (`LICENSE:3-11`) | JSON preset files | `SVT-AV1` (`encsvtav1.c:32-40`), `x265`, `x264`, NVENC, QSV/oneVPL, AMF, VideoToolbox | Fixed RF/CQ or bitrate only | Native DoVi (8.1/5/7->8.1) and HDR10+ passthrough (`dovi_common.c:1-90`, `hdr10plus.c:14-90`) | None | None | `GPL-2.0-only` subprocess; cannot do lossless video remux (`-c:v copy`) |
| **FFmpeg** (`FFmpeg/FFmpeg`) | `n9.0.2` (`2026-09-18`, `946fcce07b6d`) / `n8.1.3` (`2026-09-21`, `253db2a7b0a8`) | `LGPL-2.1-or-later` / `GPL-2.0-or-later` / `GPL-3.0-or-later` (`LICENSE.md:1-90`) | CLI flags (`-map`, `-c`, `-disposition`, `-vf`) | Full CPU + GPU matrix (`libsvtav1`, `libx265`, `libvvenc`, `*_nvenc`, `*_qsv`, `*_amf`, `*_vaapi`, `*_videotoolbox`, `*_vulkan`, `Changelog:1-90`) | `libvmaf` / `xpsnr` filters (no built-in CRF search loop) | HDR10 side data, `dovi_rpudec` (`Changelog:23`), `tonemapx`/`libplacebo` | None | None | Universal CLI subprocess (`ffmpeg` / `ffprobe`) with zero licence coupling |
| **MKVToolNix** (`mbunkus/mkvtoolnix`) | `release-103.0` (`2026-10-18`, `6e16746073fb`) | `GPL-2.0-only` (`COPYING:1-25`) | CLI flags (`mkvmerge -J`, `--audio-tracks`, `--track-order`, `mkvpropedit`) | Lossless Matroska muxer and in-place header editor | N/A (bit-exact lossless) | Native DoVi RPU + BL/EL combiner (`dovi_meta.cpp:21-80`, `hevc_dovi_layer_combiner_packet_converter.cpp:1-44`) | None | None | `GPL-2.0-only` CLI subprocess (`mkvmerge`, `mkvpropedit`) |
| **dovi_tool** & **hdr10plus_tool** (`quietvoid/*`) | `dovi_tool` `2.3.4` (`2026-09-10`, `614c816b6446`); `hdr10plus_tool` `1.7.2` (`2025-12-27`, `faa437e52b31`) | `MIT` (`LICENSE:1-21` in both) | CLI subcommands (`extract-rpu`, `inject-rpu`, `extract`, `inject`) | HEVC Annex B bitstreams & MKV/AV1 metadata | N/A (bitstream metadata tools) | Lossless DoVi P7/P5 -> P8.1 RPU conversion (`--mode 2 --crop`) and HDR10+ SEI round-trip | None | None | `MIT`: permissive CLI subprocess or Rust library crates |
| **dovi_convert** (`cryptochrome/dovi_convert`) | `v8.2.0` (`2026-03-16`, `258281e69f6c`) | `GPL-3.0-only` (`LICENSE:1-25`) | CLI batch scanner for DoVi P7 MKVs | Wraps `dovi_tool`, `ffmpeg`, `mkvmerge` (`README.md:40-42`) | N/A (lossless P7 -> P8.1) | Safe DoVi P7 -> P8.1 conversion with EL backup & restore (`README.md:14-19`) | None | None | `GPL-3.0-only` standalone Python script |
| **SVT Encore** (`svt/encore`) | `v1.0.0` (`2026-06-26`, `c42c6e804159`) | `EUPL-1.2` (`LICENSE:1-25`) | YAML profiles + REST API (`README.md:11-14`) | Wraps `ffmpeg` | External | Broadcast flags | None | Spring Boot + Redis priority queues (`README.md:11-12`) | `EUPL-1.2`, but requires JVM + Redis and lacks `*arr`/remux rules |
| **Transcodarr** (`reedylab/transcodarr`) | `v1.3.0` (`2026-07-17`, `78f9dd0d249b`) | `MIT` (`LICENSE:1-21`) | Presets + "Auto preset" rules by resolution/codec/type (`README.md:35-38`) | `ffmpeg` (`H.264`, `H.265`, `VP9`, `AV1`, CPU/GPU) | Fixed CRF; verifies duration/streams/size (`README.md:42`) | Basic FFmpeg flags | Updates Radarr/Sonarr paths, refreshes Jellyfin (`README.md:55-58`) | Single-host dual worker pool (`README.md:39`) | `MIT` Python app coupled to watch-folders and external subtitle scraping |
| **jellyfin-plugin-pre-transcode** (`mugurc/jellyfin-plugin-pre-transcode`) | `v0.11.1.0` (`2026-10-20`, `88ec1b10c29a`) | `GPL-3.0-only` (`LICENSE:1-25`) | AND/OR condition rules + per-library overrides (`RuleEvaluator.cs`, `README.md:61-64`) | Probes server `ffmpeg` (`-codecs`, `-hwaccels`, `FfmpegCapabilitiesService.cs`) | Fixed CRF or bitrate | Keeps 10-bit or `tonemap` filter (`README.md:50-54`) | None (updates Jellyfin only; zero `*arr` awareness) | In-process inside Jellyfin with OS-level window freeze (`ProcessSuspender.cs`, `README.md:67-72`) | `GPL-3.0-only`; mutates files without notifying `*arr` and copies all audio tracks |

### VMAFx integration context

ADR-0038 (pull request #746) proposes integrating `VMAFx/vmafx` (`v1.0.0-rc.3`, commit `40e87b159d37d312066fef83047ae7c64d4ac3b4`, `EUPL-1.2` + `BSD-2-Clause-Patent`) for library quality scoring, dynamic multi-GPU pool scheduling (`--backend auto`), and unified 64-bit `oshash` + DCT `phash` cut verification.

### VMAFx `vmaf-tune` facts (for review by the VMAFx session)

> Review marker: this section was read from the VMAFx tree by the SnatchArr session, read-only, at tag `v1.0.0-rc.3` (commit `40e87b159d37d312066fef83047ae7c64d4ac3b4`, released 2026-10-07, pre-release). It awaits review by the VMAFx session (ledger `snatcharr-reencode-vmaf-tune-facts`). Paths below are relative to the VMAFx tree at that tag.

VMAFx (from "VMAF", Netflix's Video Multi-method Assessment Fusion metric, plus "x" for the extended fork, pronounced "vee-maf-ex") ships `vmaf-tune`, a quality-aware encode automation tool. Facts that bear on rule-driven re-encoding:

| Fact | Source at `v1.0.0-rc.3` |
| :--- | :--- |
| `vmaf-tune` drives FFmpeg to find encode parameters for a quality target, a bitrate budget or a Pareto request; hybrid C + Python under `tools/vmaf-tune/`. ADR-0237 is Accepted for Phase A only; Phases B to F stay Proposed in that ADR, each later phase has its own ADR. | `docs/adr/0237-quality-aware-encode-automation.md:4`, `:41-60` |
| The encode harness is codec-agnostic: `encode.py` asks a per-codec adapter for its FFmpeg arguments; no codec special-casing in the search loop. | `docs/adr/0297-vmaf-tune-encode-multi-codec.md:31-61` |
| Codec adapters at the tag: `libx264`, `libx265`, SVT-AV1, libaom, libvpx, VVenC; NVIDIA NVENC (H.264, HEVC, AV1); Intel QSV (H.264, HEVC, AV1); AMD AMF (H.264, HEVC, AV1); Apple VideoToolbox (H.264, HEVC, AV1, ProRes). | `tools/vmaf-tune/src/vmaftune/codec_adapters/` (file list); `docs/adr/0282-vmaf-tune-amf-adapters.md`, `docs/adr/0288-vmaf-tune-codec-adapter-x265.md`, `docs/adr/0290-vmaf-tune-nvenc-adapters.md` |
| `vmaf-tune recommend` finds the lowest-bitrate encode whose VMAF reaches `--target-vmaf`, coarse to fine. | `tools/vmaf-tune/src/vmaftune/cli.py:1676-1683` |
| `vmaf-tune fast` is an opt-in proxy plus Bayesian plus GPU-verify path; the slow grid stays canonical and is never replaced automatically. | `docs/adr/0276-vmaf-tune-fast-path.md:3-25` |
| `--sample-clip-seconds N` encodes and scores a centre window of N seconds on both sides; sampled VMAF drifts about 1 to 2 points from full-clip VMAF on diverse content, and the ADR advises a full-source rescore of the chosen cell. | `docs/adr/0301-vmaf-tune-sample-clip.md:33-47`, `:72-78` |
| Container sources (mkv, mp4) are accepted: `EncodeRequest.source_is_container` skips the raw-YUV demuxer flags and leaves the input to FFmpeg auto-detection. | `tools/vmaf-tune/src/vmaftune/encode.py:70-74`, `:321-326` |
| The encode harness handles the video stream only: `encode.py` carries no `-map`, `-c:a`, `-c:s` or `-sn` arguments, so audio and subtitle selection, ordering and default flags are outside `vmaf-tune`. | `tools/vmaf-tune/src/vmaftune/encode.py` (absence, grep at the tag) |
| Model choice by height: below 2160 p `vmaf_v1.0.16_3d0h` (the fork default), from 2160 p `vmaf_v1.0.16_1d5h_2160`. ADR-0289 still names `vmaf_v0.6.1`; the code follows the later ADR-1169. This matches the SnatchArr answer `snatcharr-oq-0038-9` (default `vmaf_v1.0.16_3d0h` everywhere). | `tools/vmaf-tune/src/vmaftune/resolution.py:8-10`, `:95-101`; `docs/adr/0289-vmaf-tune-resolution-aware.md` (Decision); `docs/adr/README.md:1012` |
| HDR: `hdr.py` classifies PQ and HLG with a strict BT.2020 primaries gate (malformed signalling falls back to SDR) and emits per-encoder HDR flags. No HDR VMAF model ships yet; HDR sources are scored against the SDR model with a warning. Dolby Vision has no specific handling ("future Dolby-Vision-specific models would extend this map"). | `tools/vmaf-tune/src/vmaftune/hdr.py:3-24`, `:420-428`; `docs/adr/README.md:335` (ADR-0300 row) |
| `vmaf-tune ladder` builds a per-title bitrate ladder (Pareto hull, HLS, DASH or JSON manifest); it is aimed at streaming renditions, not at single-file library re-encodes. | `docs/adr/0295-vmaf-tune-phase-e-bitrate-ladder.md` (Decision) |
| `vmaf-tune auto` composes the phases into one deterministic decision tree (HDR detection, resolution, codec shortlist, predictor with coarse-to-fine fallback). Its design ADR-0397 is Proposed. | `tools/vmaf-tune/src/vmaftune/auto.py:3-29`; `tools/vmaf-tune/src/vmaftune/cli.py:1486-1497`; `docs/adr/README.md:425` |
| Licences: fork-local `vmaf-tune` files carry `EUPL-1.2`; the tree holds further licences for upstream and third-party parts (BSD-2-Clause-Patent for upstream Netflix VMAF among them). | `tools/vmaf-tune/src/vmaftune/hdr.py:1-2`, `auto.py:1-2`; `LICENSES/` |

What `vmaf-tune` does not cover at this tag, so a rule engine has to own or delegate it elsewhere: stream selection and order (audio, subtitles, commentary, forced subtitles, default flags), container remux without re-encode, keeping or replacing the original file, scheduling, resource limits and library-wide queues.

---

## Decision drivers

1. **Works for every SnatchArr adopter ([ADR-0035](0035-jellyfin-integration.md), [ADR-0028](0028-jellyfin-plugin.md))**: Must operate identically on a single-host Docker Compose homelab and a multi-node Kubernetes cluster, and must be fully usable both in the standalone SnatchArr SvelteKit web application (`web/`) and inside Jellyfin via the `jellysin/plugin-snatcharr` companion plugin without requiring an external Identity Provider (`IdP`) or reverse proxy.
2. **Observe / dry-run mode first ([ADR-0012](0012-queue-management-replaces-cleanuparr-link.md))**: Any workflow that rewrites, replaces, or deletes library files on disk must default to a non-destructive `observe` (dry-run preview) mode that computes the exact stream diff, command plan, and predicted space savings before a user enables active replacement.
3. **Never touch download clients directly ([ADR-0006](0006-download-client-backpressure.md))**: Library processing operates on imported library files or coordinates exclusively through `*arr` APIs (`Radarr`, `Sonarr`, `Lidarr`, `Readarr`, `Whisparr`), **Jellyfin**, **Stash**, and **Audiobookshelf**; it must never hook into qBittorrent, SABnzbd, or NZBGet post-processing scripts.
4. **Optional module switch, off by default ([ADR-0025](0025-module-switches.md))**: Adopters who only want SnatchArr's core snatch scheduler must pay zero background CPU, GPU, or I/O cost when library processing is disabled.
5. **Control-plane vs. data-plane separation and HISS invariants ([ADR-0001](0001-go-control-plane-rust-snatch-worker.md), [ADR-0002](0002-lease-pull-work-distribution.md), [ADR-0010](0010-keep-api-worker-split.md))**: Durable state, rule evaluation, budgets, and scheduling live in PostgreSQL; long-running media I/O runs under explicit `context.Context` deadlines (`HISS-02`) and explicit scalar worker-pool concurrency bounds (`HISS-06`).
6. **Dynamic GPU pool without static device pinning (ADR-0038)**: Whether a worker host has zero GPUs, one Intel Arc (`oneVPL`/`QSV`) iGPU, an AMD (`VAAPI`/`AMF`) card, Apple Silicon (`VideoToolbox`), or multiple NVIDIA (`NVENC`) GPUs, hardware acceleration must be discovered dynamically and pooled without hardcoded device indices.
7. **Zero `*arr` re-snatch upgrade loops**: Re-encoding or remuxing a file must never trick `*arr` or SnatchArr's own `Planner` into treating the newly processed file as a downgrade that triggers an infinite re-snatch loop.
8. **Licence hygiene (`EUPL-1.2`)**: SnatchArr cannot embed proprietary binaries (`Tdarr_Server`, `FileFlows`) and must isolate `GPL-2.0-only` / `GPL-3.0-only` tools (`HandBrakeCLI`, `mkvmerge`, `Av1an`, `ffmpeg` GPL builds) behind clean CLI subprocess boundaries.

---

## Decision

### Ownership split with VMAFx

Per the ownership split decided on 2026-10-26 (ledger id `snatcharr-vmafx-video-split-20261026`):

- **VMAFx** ([github.com/VMAFx/vmafx](https://github.com/VMAFx/vmafx)) owns the complete video stream: target codec and encoder choice, hardware probing with software/hardware fallback, HDR10 / HDR10+ / Dolby Vision carry-through and conversion, resolution scaling, deinterlacing, perceptual quality planning (`vmaf-tune auto`), and GPU worker pools (`vmafx-node`). This builds on existing VMAFx architecture records [ADR-0237: Quality-aware encode automation](https://github.com/VMAFx/vmafx/blob/main/docs/adr/0237-quality-aware-encode-automation.md) and [ADR-0297: vmaf-tune encode multi-codec](https://github.com/VMAFx/vmafx/blob/main/docs/adr/0297-vmaf-tune-encode-multi-codec.md). The VMAFx encode-job API ADR is currently pending in the VMAFx repository; implementation slice S6 will add the link when it lands.
- **SnatchArr** owns library policies and declarative rules, stream and track selection, container remuxing and in-place header edits, audio transcoding and loudness normalization, file staging and pre-replace verification gates, quarantine and rollback, service notifications (`*arr`, Jellyfin, Stash, Audiobookshelf), and task scheduling.
- SnatchArr calls VMAFx for every lossy video encode and ships zero video encoder, HDR conversion, resolution scaling, or GPU device code.
- SnatchArr uses the whole VMAFx surface through the VMAFx encode-job API, phased: the auto encode first (slice S6), then scoring, compare, bitrate ladder, per-shot tuning, saliency, HDR handling, sample clips, and node workers as later slices (slice S9). SnatchArr re-implements none of these functions and has no fixed CRF or bitrate path of its own (ledger id `snatcharr-vmafx-full-toolkit-20261026`).
- Answers to questions `OQ-0041-8`, `OQ-0041-9`, `OQ-0041-11`, `OQ-0041-12`, and `OQ-0041-17` (option a) stand as architectural requirements on VMAFx rather than SnatchArr code.

### 1. Component architecture, orchestration, and module gating (OQ-0041-1, OQ-0041-2, OQ-0041-3)

SnatchArr `api` (Go control plane) manages rule definitions, library item state, processing queues, and `*arr`/Jellyfin REST API coordination in PostgreSQL (`OQ-0041-1`). SnatchArr `worker` (Rust data plane) leases processing jobs over gRPC (`snatcharr.v1.WorkerService`), mounts the local media filesystem, and executes lossless CLI operations (`mkvpropedit`, `mkvmerge`, and `ffmpeg` audio filters). SnatchArr orchestrates open-source CLI primitives directly for remux and audio tasks without embedding or depending on external proprietary daemons (`OQ-0041-2`).

The feature is registered under a dedicated module key `library_processing` in the PostgreSQL `modules` table ([ADR-0025](0025-module-switches.md)), disabled (`enabled = false`) by default, and gated by `x-module` middleware and `leader_loops` (`OQ-0041-3`). When enabled without the `vmafx` module, SnatchArr executes only lossless operations (Tier 0 header updates and Tier 1 container remuxes).

### 2. Stream selection, disposition flags, and track normalization (OQ-0041-4, OQ-0041-5, OQ-0041-6)

Audio stream pruning preserves all user-configured preferred languages (ISO 639-2/B or 639-3) and automatically retains the work's original production language resolved from `*arr` or Jellyfin metadata (`OQ-0041-4`). If the original language is undetermined (`und`) or unmatched, all audio streams are preserved to prevent accidental dialogue loss.

Per-rule policies explicitly manage auxiliary tracks (`OQ-0041-5`): commentary tracks (detected via Matroska flags, `comment` disposition, or title regex) may be stripped or kept; visually impaired audio is handled independently; forced subtitles are unconditionally preserved for kept audio streams; and SDH tracks are preserved or pruned when non-SDH subtitles exist.

Streams are normalized into canonical order (`OQ-0041-6`): original language high-channel audio first, followed by dubbed languages, compatibility tracks, and commentary; subtitles are ordered by preferred language, forced, full, and SDH. Stale default dispositions are cleared so at most one audio track receives `default=1`, `forced=1` is set only when the audio language differs from the primary household language, and track titles are normalized. When only track flags or titles change, in-place `mkvpropedit` executes without rewriting media data.

### 3. Container targets and lossless remux-first pipeline (OQ-0041-7)

Processing follows a three-tier lossless-first escalation (`OQ-0041-7`):

1. **Tier 0 (header-only)**: Applies in-place `mkvpropedit` within milliseconds if an MKV file requires only flag, disposition, or track title modifications.
2. **Tier 1 (lossless remux)**: Executes bit-exact `mkvmerge` or `ffmpeg -c copy` stream copying without video decoding when resolving container conversions (`MP4`/`TS`/`AVI` -> `MKV`), audio/subtitle pruning, or stream reordering.
3. **Tier 2 (encode)**: Delegates to VMAFx `vmaf-tune` auto mode only when rule constraints require lossy video re-encoding.

### 4. Target video codecs, software encoders, and hardware acceleration (OQ-0041-8, OQ-0041-9)

Requirements on VMAFx: VMAFx curates and drives video encoders (`AV1` via `libsvtav1` as primary software encoder, `HEVC` via `libx265`, `H.264` via `libx264`; `VVC` via `libvvenc` remains experimental and disabled by default) (`OQ-0041-8`). VMAFx probes hardware encoder capabilities at worker startup using synthetic encode tests across NVIDIA `NVENC`, Intel `QSV`/`oneVPL`, AMD `AMF`/Linux `VAAPI`, and Apple Silicon `VideoToolbox`, respecting per-rule choices (`software-only`, `hardware-preferred-with-cpu-fallback`, or `hardware-only`) without static device pinning (`OQ-0041-9`). SnatchArr contains no video encoder implementations or hardware probe logic.

### 5. Quality targeting, size ceiling, and VMAFx auto mode (OQ-0041-10)

Lossy video re-encoding is planned exclusively through VMAFx `vmaf-tune` auto mode (`OQ-0041-10`). A SnatchArr rule supplies the target perceptual quality score (`min_vmaf`), a hard maximum encoded size cap (`max_encoded_percent`), and the set of allowed target codecs. VMAFx determines the per-shot encoding parameters, including spatial and temporal saliency, HDR metadata handling, sample clipping, and codec selection.

The original file is preserved on disk unless the re-encoded output is strictly smaller than the source and meets the size percentage ceiling. SnatchArr provides no fixed CRF/CQ mode, no target bitrate mode, and no `ab-av1` or `Av1an` integration. If the `vmafx` module is disabled, video re-encoding is bypassed and only lossless Tiers 0 and 1 execute.

### 6. HDR10, HDR10+, and Dolby Vision metadata preservation (OQ-0041-11)

Requirements on VMAFx: Profile-aware video handling preserves static SMPTE ST 2086 `HDR10` metadata, extracts and re-injects SMPTE ST 2094-40 `HDR10+` dynamic SEI messages, preserves Dolby Vision Profile 8.1 RPU metadata, converts Profile 7 (`FEL`/`MEL`) to Profile 8.1, and skips re-encoding on proprietary `IPT-PQ-C2` Profile 5 streams to avoid colour distortion (`OQ-0041-11`). Tone-mapping to SDR occurs only if explicitly specified by a compatibility rule. SnatchArr performs no HDR metadata parsing or conversion.

### 7. Resolution, frame rate, audio transcoding, and loudness normalization (OQ-0041-12, OQ-0041-13)

Resolution, frame rate, cropping, and deinterlacing stand as requirements on VMAFx (`OQ-0041-12`): never upscale; downscale only when an explicit `max_height` ceiling is configured; preserve source frame rate with passthrough timing; deinterlace (`bwdif`) only when interlaced cadence is detected; and preserve letterbox black bars uncropped by default to prevent PGS graphic subtitle misalignment and Dolby Vision L5 active-area corruption.

Audio transcoding and loudness are owned by SnatchArr (`OQ-0041-13`): rules configure whether to keep lossless surround audio (`TrueHD`, `DTS-HD MA`, `FLAC`), keep lossless with an added secondary stereo/surround compatibility track (`Opus`, `E-AC-3`, `AAC`), or transcode surround streams. Non-destructive `EBU R128` loudness scanning and metadata tagging are preferred over destructive sample compression, reserving `loudnorm` dynamic compression for opt-in night-mode compatibility tracks.

### 8. File staging, pre-replace verification gates, rollback, and service notification (OQ-0041-14, OQ-0041-15)

Processed outputs are written to temporary staging files (`<name>.snatcharr-tmp.<ext>`) (`OQ-0041-14`). Before replacing the original file, a mandatory verification battery must pass:

1. Container and stream count match the planned disposition.
2. Duration delta satisfies $|\Delta\text{duration}| \le 1.0\text{s}$.
3. Zero decode errors across start, middle, and end packet probes.
4. Encoded file size is strictly less than `max_encoded_percent` of the source file.
5. When `vmafx` is enabled, VMAFx perceptual score meets or exceeds `min_vmaf` and DCT `phash` Hamming distance satisfies $d \le 8$.

Passed outputs are swapped atomically via a configurable rollback policy: moving the original to `.snatcharr-trash/` with a retention TTL (default 7 days) and one-click UI restoration, performing in-place replacement, or producing a multi-version sibling.

Following replacement, SnatchArr coordinates exclusively through official REST APIs (`OQ-0041-15`): triggering `*arr` (`Radarr`, `Sonarr`, `Lidarr`, `Readarr`, `Whisparr`) `RefreshMovie`/`RefreshSeries` endpoints, polling completion, optionally executing `/api/v3/rename` to keep media info tokens truthful in filenames, and dispatching targeted item refreshes to Jellyfin, Stash, or Audiobookshelf.

### 9. Scheduling windows, playback yield, and bounded worker pools (OQ-0041-16, OQ-0041-17)

Processing jobs honour cron/daily time windows (`OQ-0041-16`). SnatchArr automatically yields or pauses active worker jobs (via `SIGSTOP`/`SIGCONT` on Unix) when concurrent Jellyfin playback sessions exceed configured thresholds or when GPU resources are required for user streaming. Real-time playback takes strict precedence over background optimization.

Concurrency is constrained by explicit scalar bounds (`HISS-06`) (`OQ-0041-17`): SnatchArr workers lease jobs against bounded slots for disk-bound `remux_io` and audio transcoding, prioritizing manual requests and fast lossless remuxes ahead of long-running re-encodes. Video encode worker pools and GPU allocation (`encode_gpu`, `vmafx-node`) are managed by VMAFx per the ownership split.

### 10. Rule scoping hierarchy, declarative schema, and dry-run preview (OQ-0041-18, OQ-0041-19)

Rules evaluate within a deterministic precedence hierarchy (`OQ-0041-18`): per-item override -> per-user request language preference -> per-library / root folder -> per-`*arr` instance -> per-media-type (`movie`, `episode`, `anime`, `music`, `audiobook`, `stash_scene`) -> global default.

Rules are defined using a typed, declarative YAML/JSON schema validated via OpenAPI and Zod v4, forbidding arbitrary runtime code execution (`HISS-08`) (`OQ-0041-19`). An observe-first dry-run engine generates a `RuleTrace` diff for each file, illustrating stream modifications, execution tier, CLI commands, and estimated size changes before active processing is enabled.

### 11. User interface across SnatchArr SPA and Jellyfin plugin (OQ-0041-20)

The rule builder, dry-run preview table, active queue monitor, verification inspector, and rollback controls are implemented in the SnatchArr SvelteKit SPA (`web/`) using `@sveltesentio/*` components, `oklch` colour tokens, Paraglide `en` and `de` messages, and zero WCAG 2.2 AA accessibility violations (`OQ-0041-20`). The exact same responsive views and item-level preview/run actions are surfaced inside Jellyfin via `jellysin/plugin-snatcharr` ([ADR-0028](0028-jellyfin-plugin.md), [ADR-0035](0035-jellyfin-integration.md)).

### 12. Custom Format cutoff protection and planner upgrade integration (OQ-0041-21, OQ-0041-22)

To prevent infinite upgrade loops with `*arr` instances, SnatchArr preserves source release and quality tokens in filenames, conducts pre-flight Custom Format score simulations during dry-run, tags processed files (`SNATCHARR_PROCESSED_FROM` container tag and `snatcharr-processed` `*arr` tag), and offers a per-rule toggle to mark items terminal or unmonitor in `*arr` once quality targets are satisfied (`OQ-0041-21`).

SnatchArr's snatch `Planner` integrates with processed memory: items processed under library rules are tagged `RuleNotAnUpgrade`, preventing the planner from treating local codec and size reductions as quality drops (`OQ-0041-22`). The planner can also be configured to prioritize high-bitrate remux releases from indexers when a rule specifies automatic local transcoding to AV1 upon import.

---

## Options per question

### 1. Component ownership, build-vs-integrate, and module gating (`OQ-0041-1`..`OQ-0041-3`)

- **Owning component (`OQ-0041-1`)**:
  - *Option a*: `SnatchArr api` (Go control plane) owns rule definitions, library state, job queue, and `*arr`/Jellyfin API coordination in PostgreSQL, while `SnatchArr worker` (Rust data plane) leases processing jobs over gRPC (`snatcharr.v1.WorkerService`), mounts the media filesystem, and runs local CLI subprocesses.
  - *Option b*: `VMAFx` (`VMAFx/vmafx`) owns the entire encoding/remuxing pipeline as an extended media-processing daemon, and `SnatchArr api` delegates job specs to `VMAFx`.
  - *Option c*: A dedicated new microservice (`snatch-transcoder` binary/container) separate from `snatch-worker` leases jobs from `SnatchArr api` so snatch workers never need media volume mounts or GPU device passthrough.
- **Build-vs-integrate strategy (`OQ-0041-2`)**:
  - *Option a*: Own the rule engine and job state machine in SnatchArr and directly orchestrate best-of-breed open-source CLI primitives (`ffprobe`, `ffmpeg`, `mkvmerge`, `mkvpropedit`, `dovi_tool`, `hdr10plus_tool`, and `VMAFx` / `ab-av1`).
  - *Option b*: Drive an external third-party orchestrator (`Tdarr`, `Unmanic`, or `SVT Encore`) via its REST API.
  - *Option c*: Hybrid—own the native pipeline using CLI primitives as the primary path, plus an optional webhook/REST bridge for adopters who already run an external `Tdarr` or `SVT Encore` cluster.
- **Module activation (`OQ-0041-3`)**:
  - *Option a*: Register a dedicated module key (e.g. `library_processing` or `reencode`) in the `modules` table ([ADR-0025](0025-module-switches.md)), disabled (`enabled = false`) by default, gated by `x-module` OpenAPI middleware and `leader_loops`.
  - *Option b*: Bundle under the `vmafx` module switch from ADR-0038.
  - *Option c*: Enable in `observe` mode by default on every installation.

### 2. Scope 1: Stream selection, ordering, flags, and titles (`OQ-0041-4`..`OQ-0041-6`)

- **Audio language selection & original-language preservation (`OQ-0041-4`)**:
  - *Option a*: Keep user-configured preferred audio languages (ISO 639-2/B or 639-3 list) **plus** automatically preserve the work's original production language resolved from `*arr` (`movie.originalLanguage` / `series.originalLanguage`), Jellyfin (`ProductionLocations` / TMDb / TVDb metadata), or the source track marked `[Original]`, falling back to keeping all audio tracks if original language is unknown (`und`) or no track matches.
  - *Option b*: Keep only tracks matching the static ISO language allow-list configured on the rule, with no external original-language lookup.
  - *Option c*: Keep only the single first audio track plus one user language.
- **Commentary, descriptive audio, SDH, and forced subtitles (`OQ-0041-5`)**:
  - *Option a*: Separate per-rule policies for (i) commentary tracks (matched by Matroska `--commentary-flag`, disposition `comment`, or title regex `(?i)\b(commentary|director|cast)\b`: `keep`, `strip`, or `keep-for-favorites`), (ii) visually impaired / audio description (`visual_impaired`), (iii) subtitle language allow-list, (iv) forced subtitles (`disposition.forced` or title `(?i)\bforced\b`: always keep for kept audio languages), and (v) Subtitles for the Deaf and Hard-of-Hearing (`SDH` / `hearing_impaired`: keep alongside or strip when non-SDH exists).
  - *Option b*: Treat all audio and subtitle tracks of a given ISO language identically regardless of commentary, forced, or SDH flags.
  - *Option c*: Strip all internal subtitles and rely exclusively on external `.srt` files managed by Bazarr.
- **Stream ordering, default/forced dispositions, and track titles (`OQ-0041-6`)**:
  - *Option a*: Deterministic canonical ordering (1. original language lossless/highest-channel audio, 2. preferred dubbed audio, 3. stereo compatibility fallback, 4. commentary; subtitles ordered by preferred language -> forced -> full -> SDH), clearing stale `default` flags so at most one audio track receives `default=1` and `forced=1` is set only when audio language differs from user's primary language, plus normalized human-readable track titles (or in-place `mkvpropedit` when only flags/titles change).
  - *Option b*: Preserve upstream stream order and dispositions untouched.
  - *Option c*: Strip all track titles (`--track-name tid:`) to avoid release-group spam.

### 3. Scope 2: Container targets and lossless remux-first paths (`OQ-0041-7`)

- **Container & lossless-first execution (`OQ-0041-7`)**:
  - *Option a*: Three-tier lossless-first escalation:
    1. **Tier 0 (header-only)**: If file is already Matroska (`MKV`) and only track flags, dispositions, or titles violate the rule, apply `mkvpropedit` in-place in milliseconds.
    2. **Tier 1 (lossless remux)**: If video/audio codecs already satisfy the rule (or the rule is remux-only) and only container (`MP4`/`TS`/`AVI` -> `MKV`, or `MKV` -> `MP4` with `faststart`), stream pruning, stream reordering, or lossless Dolby Vision Profile 7 -> 8.1 RPU conversion (`dovi_tool` + `mkvmerge`) is needed, execute a bit-exact lossless remux without decoding video.
    3. **Tier 2 (encode)**: Decode and re-encode video/audio only when codec, bitrate/size, or resolution rules require it.
  - *Option b*: Single unified `ffmpeg` transcode pass for all rule violations.
  - *Option c*: Remux-only (`mkvmerge`); forbid lossy video re-encoding entirely.

### 4. Scope 3: Video codecs, software encoders, and hardware acceleration (`OQ-0041-8`..`OQ-0041-9`)

*Owned by VMAFx per the ownership split (ledger snatcharr-vmafx-video-split-20261026); option a stands as requirements on VMAFx.*

- **Target video codecs and software encoders (`OQ-0041-8`)**:
  - *Option a*: Support `AV1` (`SVT-AV1` / `libsvtav1` as primary software encoder, `libaom-av1` and `librav1e` as advanced options) and `HEVC` (`libx265`) as primary archival targets, `H.264` (`libx264`) for legacy client compatibility profiles, and keep Versatile Video Coding (`VVC` / `H.266` via `libvvenc`) disabled/experimental until hardware decode support exists in mainstream Jellyfin clients.
  - *Option b*: Restrict re-encoding strictly to `AV1` (`libsvtav1`).
  - *Option c*: Expose every video encoder compiled into the host `ffmpeg` without curation.
- **Hardware encoder support per vendor and CPU fallback (`OQ-0041-9`)**:
  - *Option a*: Probe hardware encoders at worker startup using a 1-second synthetic `lavfi` test encode (similar to `Tdarr` `hardwareUtils.ts:23-102` and `jellyfin-plugin-pre-transcode` `FfmpegCapabilitiesService.cs`) across NVIDIA `NVENC` (`av1_nvenc`, `hevc_nvenc`, `h264_nvenc`), Intel `QSV` / `oneVPL` on Arc and iGPUs (`av1_qsv`, `hevc_qsv`, `h264_qsv`), AMD `AMF` / Linux `VAAPI` (`av1_vaapi`, `hevc_vaapi`, `av1_amf`, `hevc_amf`), and Apple Silicon `VideoToolbox` (`hevc_videotoolbox`, `h264_videotoolbox`). Per rule, allow choosing `software-only` (archival quality), `hardware-preferred-with-cpu-fallback`, or `hardware-only` (fail/requeue if no GPU slot is available rather than saturating CPU).
  - *Option b*: Silently fall back from GPU to CPU software encoding whenever a GPU device is busy or fails initialization.
  - *Option c*: Support CPU software encoding only (`libsvtav1` / `libx265`) and use GPUs exclusively for decoding (`-hwaccel`) and `VMAFx` scoring.

### 5. Scope 4: Quality target vs. size (`OQ-0041-10`)

*Owned by VMAFx per the ownership split (ledger snatcharr-vmafx-video-split-20261026). Decided: lossy re-encoding is planned only by VMAFx `vmaf-tune` auto mode (ledger snatcharr-oq-0041-10); fixed CRF/CQ, ab-av1 crf-search, Av1an, and bitrate targets are not chosen for SnatchArr.*

- **Quality targeting model (`OQ-0041-10`)**:
  - *Option a (not chosen as written; VMAFx `vmaf-tune` auto mode selected)*: Support three selectable modes per rule, all guarded by a mandatory **maximum encoded size percentage cap** (e.g. `max_encoded_percent = 85%`; if predicted or actual output exceeds the cap, abort and keep the original file):
    1. **Target perceptual quality search** (`VMAFx vmaf-tune` or `ab-av1 crf-search` interpolated sample search for `min_vmaf` e.g. `95.0` or `min_xpsnr`, bounded by `[min_crf, max_crf]`). *(VMAFx `vmaf-tune` auto mode selected; `ab-av1` not chosen).*
    2. **Scene-chunked target quality** (optional `Av1an` subprocess mode for software `SVT-AV1`/`x265` archival rules). *(Not chosen).*
    3. **Fixed CRF / Constant Quality (`CQ` / `ICQ`) preset** per resolution and codec tier. *(Not chosen).*
  - *Option b (not chosen)*: Fixed CRF / CQ presets only; run `VMAFx` only once *after* the full encode finishes.
  - *Option c (not chosen)*: Target average bitrate (`-b:v`) formulas scaled by pixel count.

### 6. Scope 5: HDR10, HDR10+, and Dolby Vision handling (`OQ-0041-11`)

*Owned by VMAFx per the ownership split (ledger snatcharr-vmafx-video-split-20261026); option a stands as requirements on VMAFx.*

- **High Dynamic Range and Dolby Vision profiles (`OQ-0041-11`)**:
  - *Option a*: Profile-aware HDR pipeline:
    - **Static `HDR10`**: Preserve 10-bit pixel format (`yuv420p10le` / `p010le`), `bt2020nc` / `bt2020` / `smpte2084` colour signalling, Mastering Display Colour Volume (`master-display`), and `max-cll`.
    - **`HDR10+` (SMPTE ST 2094-40)**: Extract dynamic SEI before video re-encode via `hdr10plus_tool extract` (or HandBrake/libx265 native pass) and re-inject via `hdr10plus_tool inject` (`quietvoid/hdr10plus_tool/README.md:23-64`).
    - **Dolby Vision Profile 8.1 (`BL` + `RPU`, `HDR10` base)**: Preserve RPU across re-encode (via `dovi_tool extract-rpu` + `inject-rpu` or native `libsvtav1`/`libx265` RPU injection), updating active-area crop offsets (`dovi_tool --crop`) if black bars are cropped.
    - **Dolby Vision Profile 7 (UHD Blu-ray dual-layer `BL` + `EL` + `RPU`, `FEL`/`MEL`)**: Convert RPU to Profile 8.1 (`dovi_tool --mode 2 extract-rpu`, optionally backing up the `EL` per `dovi_convert` for `MEL`/safe `FEL` or skipping complex brightness-expanding `FEL` titles when configured).
    - **Dolby Vision Profile 5 (streaming proprietary `IPT-PQ-C2` colour space, no `HDR10` fallback)**: Default to **skip video re-encode** (allow lossless stream/container remux only) or require explicit libplacebo/dovi_tool reshape to Profile 8.1 / SDR, because naive re-encoding without `IPT-PQ-C2` reshaping produces purple/green colour corruption.
    - **Tone-mapping (`HDR` -> `SDR`)**: Disabled by default on primary archival files (since Jellyfin live-tonemaps for SDR clients); available only when a rule explicitly targets an SDR-only compatibility library or secondary version.
  - *Option b*: Convert all HDR and Dolby Vision sources to 8-bit `BT.709` SDR via `tonemapx` / `libplacebo`.
  - *Option c*: Skip all HDR10+ and Dolby Vision files unconditionally.

### 7. Scope 6: Resolution, frame rate, audio transcode, and loudness (`OQ-0041-12`..`OQ-0041-13`)

*Resolution scaling, cropping, deinterlacing, and frame rate (OQ-0041-12) are owned by VMAFx per the ownership split (ledger snatcharr-vmafx-video-split-20261026); option a stands as requirements on VMAFx. Audio transcoding and loudness (OQ-0041-13) are owned by SnatchArr.*

- **Resolution, aspect ratio / cropping, and frame rate (`OQ-0041-12`)**:
  - *Option a*: Never upscale; downscale only when a rule sets an explicit ceiling (e.g. `max_height: 1080`) using Lanczos/Spline scaling; keep original Variable or Constant Frame Rate (`-fps_mode passthrough`, `ab-av1/src/ffmpeg.rs:98`) unless deinterlacing (`bwdif`) is triggered on interlaced (`tt`/`bb`/`tb`/`bt`) sources; keep black bars uncropped by default so Dolby Vision L5 active-area offsets and PGS graphic subtitle coordinates remain valid unless crop is explicitly enabled.
  - *Option b*: Auto-crop black bars (`cropdetect`) and force constant frame rate (`CFR`) on every encode.
  - *Option c*: Forbid any resolution scaling or deinterlacing.
- **Audio codec transcoding, lossless retention, and loudness (`OQ-0041-13`)**:
  - *Option a*: Per-rule audio policy supporting: (i) keep lossless (`TrueHD`, `DTS-HD MA`, `FLAC`, `PCM`) untouched, (ii) keep lossless primary track **plus** generate a secondary compatibility track (`Opus`, `E-AC-3`, or `AAC` stereo/5.1) when none exists, or (iii) transcode lossy/lossless surround to `Opus` (in `MKV`) or `E-AC-3` / `AAC` (in `MP4`). For loudness, prefer non-destructive `EBU R128` ReplayGain/loudness metadata tagging (`ebur128` scan written to track tags) over destructive dynamic-range compression (`loudnorm` filter), with `loudnorm` opt-in only for dedicated night-mode compatibility tracks.
  - *Option b*: Transcode all audio tracks to stereo `AAC` with two-pass `loudnorm` baked into the samples.
  - *Option c*: Copy all kept audio streams bit-for-bit without any audio encoding or loudness scanning.

### 8. Scope 7: Staging, pre-replace verification, rollback, and `*arr` rescan (`OQ-0041-14`..`OQ-0041-15`)

- **Keep vs. replace, verification gates, and rollback (`OQ-0041-14`)**:
  - *Option a*: Write output to a staging file (`<name>.snatcharr-tmp.<ext>`), then run a mandatory multi-gate verification battery before touching the original:
    1. Container & stream count match the planned disposition (`ffprobe` / `mkvmerge -J`).
    2. Duration delta $|\Delta\text{duration}| \le 1.0\text{s}$ (ADR-0038 cut gate).
    3. Zero decode errors on start/middle/end packet probes.
    4. Encoded size is strictly smaller than `max_encoded_percent` of input.
    5. When `VMAFx` is enabled (ADR-0038), DCT `phash` cut verification (`Hamming <= 8`) and perceptual score $\ge \text{min\_vmaf}$.
    Only when all gates pass, atomically swap via a quarantine/rollback policy: either move the original to a configurable `.snatcharr-trash/` quarantine folder with a TTL (e.g. 7 days) + one-click rollback in the UI (or `EL` backup for DoVi P7), Replace in-place after atomic `.old` rename (`Tdarr` `replaceOriginalFile/1.0.0/index.ts:65-145`), or output as a Jellyfin multi-version sibling (`Movie (1080p) - [AV1].mkv`).
  - *Option b*: Overwrite original files immediately upon `ffmpeg` exit code `0` without a quarantine window or perceptual verification.
  - *Option c*: Never replace originals; only write second copies alongside every source file.
- **`*arr` and media-server notification / rename path (`OQ-0041-15`)**:
  - *Option a*: Coordinate strictly through the owning service's official REST API ([ADR-0006](0006-download-client-backpressure.md)): for `*arr` (`Radarr`, `Sonarr`, `Lidarr`, `Readarr`, `Whisparr`), trigger `RefreshMovie` / `RefreshSeries` / `RefreshArtist` (`POST /api/v3/command`), poll completion, and optionally invoke `/api/v3/rename` (`RenameMovie` / `RenameSeries`, as in `Tdarr` `applyRadarrOrSonarrNamingPolicy/1.0.0/index.ts:115-160`) so filename tokens like `{MediaInfo VideoCodec}` or `{MediaInfo AudioCodec}` stay accurate; then trigger a targeted path/item refresh on **Jellyfin**, **Stash**, or **Audiobookshelf**.
  - *Option b*: Mutate files silently on disk and wait for `*arr`'s 12-hour daily disk scan task to notice.
  - *Option c*: Update `*arr`'s SQLite/PostgreSQL database directly.

### 9. Scope 8: Scheduling, resource limits, queue, and concurrency bounds (`OQ-0041-16`..`OQ-0041-17`)

- **Time windows, playback yield, and resource limits (`OQ-0041-16`)**:
  - *Option a*: Support cron/daily processing windows (e.g. `23:00–07:00`), automatic pause or OS-level process suspension (`SIGSTOP`/`SIGCONT` on Unix, similar to `jellyfin-plugin-pre-transcode` `ProcessSuspender.cs`) or job yield when active Jellyfin playback sessions exceed a threshold (or when a user starts live-transcoding on the same GPU), plus explicit CPU thread caps (`--threads` / `SVT-AV1 --lp`) and systemd/cgroup memory bounds.
  - *Option b*: Run encodes 24/7 at default encoder thread counts regardless of active Jellyfin streams.
  - *Option c*: Only run jobs when triggered manually by an administrator.
- **Worker pools, GPU pooling, and scalar concurrency bounds (`OQ-0041-17`)**:
  *GPU worker pooling and video encode execution are owned by VMAFx per the ownership split (ledger snatcharr-vmafx-video-split-20261026); option a GPU pooling stands as requirements on VMAFx. SnatchArr worker pools cover remux and audio.*
  - *Option a*: Enforce explicit scalar upper bounds (`HISS-06`) across separate worker slots for `remux_io` (disk-bound), `encode_cpu` (CPU-bound), and `encode_gpu` (sharing the dynamic GPU pool from ADR-0038 without static GPU ordinal pinning), with priority ordering (1. manual single-item requests, 2. lossless remuxes, 3. high-savings re-encodes).
  - *Option b*: Single FIFO queue where one 6-hour software `AV1` encode blocks ten 15-second lossless `MKV` track-pruning remuxes.
  - *Option c*: Unbounded parallel job execution per worker.

### 10. Scope 9: Rule scoping, declarative format, and dry-run preview (`OQ-0041-18`..`OQ-0041-19`)

- **Rule hierarchy and target scoping (`OQ-0041-18`)**:
  - *Option a*: Scoped rule hierarchy evaluated in deterministic precedence order: per-item override -> per-user request/preference tag (e.g. keeping a requester's preferred language from Seerr/Jellyfin request metadata) -> per-library / root folder -> per-`*arr` instance -> per-media-type (`movie`, `episode`, `anime`, `music`, `audiobook`, `stash_scene`) -> global default.
  - *Option b*: Flat per-folder rules only, with no awareness of `*arr` instances, anime vs. live-action, or user language preferences.
  - *Option c*: Single global rule applied to every library file.
- **Rule language/format and dry-run preview (`OQ-0041-19`)**:
  - *Option a*: Declarative typed YAML/JSON schema (validated via OpenAPI + Zod v4, exportable/importable for GitOps alongside Configarr) with a structured condition-and-action builder in the UI (avoiding arbitrary user JS code execution per `HISS-08`), paired with an **observe-first dry-run preview** ([ADR-0012](0012-queue-management-replaces-cleanuparr-link.md)) that produces a per-file trace (`RuleTrace`) showing kept/removed streams, tier (`noop`, `header-edit`, `remux`, `reencode`), exact CLI command, and estimated byte delta.
  - *Option b*: Node-and-wire visual Turing-complete JavaScript plugin graph (`Tdarr` / `FileFlows` style).
  - *Option c*: Raw custom `ffmpeg` argument strings per library.

### 11. Scope 10: UI surface across SnatchArr web and Jellyfin plugin (`OQ-0041-20`)

- **UI surface (`OQ-0041-20`)**:
  - *Option a*: Build the full rule editor, dry-run preview table, queue monitor, verification/VMAFx diff inspector, and one-click rollback controls in the SnatchArr SvelteKit SPA (`web/` using `@sveltesentio/*`, `oklch` tokens, `en` + `de` Paraglide messages, and zero axe-core WCAG 2.2 AA violations) and expose the exact same views inside Jellyfin via `jellysin/plugin-snatcharr` ([ADR-0028](0028-jellyfin-plugin.md), [ADR-0035](0035-jellyfin-integration.md)), plus per-item "Preview / Run rule processing" actions on Jellyfin item detail dialogs.
  - *Option b*: Expose configuration and queue views only in the standalone SnatchArr web UI (`web/`), omitting them from the Jellyfin plugin surface.
  - *Option c*: Build a separate C#/HTML configuration page inside Jellyfin (`configPage.html` style) duplicating the SvelteKit UI.

### 12. Scope 11: `*arr` Custom Formats, cutoff protection, and SnatchArr planner (`OQ-0041-21`..`OQ-0041-22`)

- **Preventing `*arr` Custom Format / quality profile upgrade loops (`OQ-0041-21`)**:
  - *Option a*: Three-layer defence against upgrade loops:
    1. **Filename & tag preservation**: Preserve release-group, source quality (`Bluray-1080p`, `WEBDL-2160p`), and Custom Format tokens in the filename (or apply `*arr` rename policy) and write a Matroska/MP4 tag (`SNATCHARR_PROCESSED_FROM=<orig_hash_and_quality>`).
    2. **Pre-flight CF simulation**: Before replacing a file, evaluate whether the post-encode media info (codec change + smaller file size) would cause `*arr`'s quality/CF score to drop below the profile cutoff or trigger a TRaSH Custom Format penalty (e.g. `x265 (HD)` no-golden-rule penalty); warn in dry-run and either keep the release's original quality/CF score marker or apply an `*arr` tag (`snatcharr-processed`).
    3. **Optional unmonitor or snatch-ignore**: Offer a per-rule toggle to either mark the item as `processed_terminal` in SnatchArr's processed memory or unmonitor in `*arr` (like `Tdarr` `notifyRadarrOrSonarr/3.0.0/index.ts:212-248`) once target quality and `VMAFx` score are met.
  - *Option b*: Unmonitor every processed movie or episode in `*arr` unconditionally.
  - *Option c*: Do nothing and rely on the user to manually edit their TRaSH / Configarr Custom Formats.
- **Interaction with SnatchArr's own snatch `Planner` (`OQ-0041-22`)**:
  - *Option a*: Integrate with SnatchArr's processed memory and `Planner`: record processed items in `SnatchArr api` so the snatch planner never treats a SnatchArr-re-encoded file as needing a codec/size re-snatch (`RuleNotAnUpgrade`), and optionally allow the planner to prefer snatching high-bitrate remuxes/sources when a library rule is configured to locally transcode them to `AV1` after import.
  - *Option b*: Keep the snatch `Planner` and library processor completely unaware of each other's state tables.
  - *Option c*: Pause all SnatchArr indexer searches across the entire instance whenever any library processing job is queued.

---

## Answered questions

The open questions `OQ-0041-1` to `OQ-0041-22` were answered on 2026-10-26. Each answer is recorded in the decision ledger under the id shown; the wording is a paraphrase. Where an answer differs from the recommendation in the question list that follows, the answer binds.

| ID | Question | Options (each with primary risk) | Recommended | Answer | Ledger id |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `OQ-0041-1` | Which component owns rule-driven library processing state and execution? | **a)** `SnatchArr api` owns rules, queue, and `*arr`/Jellyfin coordination in PostgreSQL; `SnatchArr worker` leases jobs via gRPC and executes CLI tools (risk: `snatch-worker` containers that run processing jobs need media volume mounts and optional GPU passthrough). **b)** `VMAFx` owns the entire remux/re-encode pipeline as an all-in-one daemon (risk: turns a perceptual scoring tool into a stateful `*arr`/library orchestrator, duplicating SnatchArr's API clients; `vmaf-tune` handles the video stream only at `v1.0.0-rc.3`, so stream selection, remux and replacement would be new VMAFx scope). **c)** New standalone `snatch-transcoder` service binary/container leasing from `SnatchArr api` (risk: adds a fourth deployable component to build, version, and chart). | **a** (or **a** with worker capability labels so only media-mounted workers lease processing runs)—reuses the proven [ADR-0001](0001-go-control-plane-rust-snatch-worker.md) / [ADR-0002](0002-lease-pull-work-distribution.md) lease model without a new control plane. | **a** | `snatcharr-oq-0041-1` |
| `OQ-0041-2` | Should SnatchArr build its own rule orchestrator over CLI primitives or drive an external tool (`Tdarr`, `Unmanic`, `SVT Encore`)? | **a)** Own the declarative rule engine in SnatchArr and invoke open-source CLI primitives (`ffprobe`, `ffmpeg`, `mkvmerge`, `mkvpropedit`, `dovi_tool`, `hdr10plus_tool`, `VMAFx`/`ab-av1`) directly (risk: SnatchArr maintains the CLI command builders and output parsers). **b)** Drive `Tdarr`, `Unmanic`, or `SVT Encore` via REST (risk: `Tdarr` core is proprietary and cannot be bundled; `Unmanic` gates link/library counts by supporter tier; `SVT Encore` needs JVM+Redis and lacks stream-pruning/DoVi rules). **c)** Hybrid: native CLI pipeline + external `Tdarr`/`Encore` REST adapter (risk: doubles test and support surface for two divergent execution engines). | **a**—only direct orchestration of open-source CLI primitives satisfies the any-adopter, `EUPL-1.2`, and Jellyfin-embedded requirements without proprietary dependencies. | **a** | `snatcharr-oq-0041-2` |
| `OQ-0041-3` | How is the feature gated in SnatchArr's module system ([ADR-0025](0025-module-switches.md))? | **a)** Dedicated `library_processing` module in the `modules` table, disabled (`enabled = false`) by default, gated via `x-module` and `leader_loops` (risk: requires one explicit toggle before first use). **b)** Coupled to the `vmafx` module switch from ADR-0038 (risk: adopters who only want lossless track pruning or `mkvmerge` remuxing must enable `vmafx` too). **c)** Always enabled in `observe` mode on every install (risk: runs background `ffprobe` scans on libraries for users who only want indexer snatching). | **a**—honours [ADR-0025](0025-module-switches.md) zero-overhead guarantee while allowing lossless remuxing with or without `VMAFx`. | **a** | `snatcharr-oq-0041-3` |
| `OQ-0041-4` | How should audio language selection handle the work's original language? | **a)** Keep user-configured language list **plus** auto-preserve the work's original production language queried from `*arr` / Jellyfin / `[Original]` track metadata, falling back to keeping all audio if unknown (`und`) or unmatched (risk: depends on accurate original-language metadata in `*arr` or Jellyfin). **b)** Keep only the static ISO language list on the rule (risk: strips the original Japanese/Korean/French audio from foreign films if the user only listed `eng`/`deu`). **c)** Keep only the first audio track (risk: breaks multi-lingual households and commentary/compatibility tracks). | **a**—prevents accidental destruction of original-language performances in multi-national libraries while failing safe (`keep all`) when metadata is `und`. | **a** | `snatcharr-oq-0041-4` |
| `OQ-0041-5` | How should commentary tracks, audio descriptions, forced subtitles, and SDH subtitles be selected? | **a)** Explicit per-rule policies for commentary (by flag + title regex), visual-impaired audio, subtitle language list, forced subtitles (always kept for kept audio languages), and SDH (keep or strip when standard sub exists) (risk: heuristic title matching needed when upstream muxers omit Matroska flags). **b)** Filter strictly by ISO language code, ignoring commentary/forced/SDH distinctions (risk: either keeps unwanted commentary tracks as main audio or deletes forced foreign-dialogue subtitles). **c)** Strip all embedded subtitles (risk: breaks PGS/ASS anime styling and offline self-contained MKV playback). | **a**—separating commentary, forced, and SDH tracks is required to avoid making a commentary track the primary audio stream or losing forced foreign-part subtitles. | **a** | `snatcharr-oq-0041-5` |
| `OQ-0041-6` | How should stream order, `default`/`forced` flags, and track titles be normalized? | **a)** Canonical stream ordering + strict single-`default` audio disposition + conditional `forced` subtitle flag + normalized track titles, using in-place `mkvpropedit` when no remux/encode is needed (risk: overwrites custom release-group track titles if user prefers them). **b)** Leave stream order, flags, and titles untouched (risk: players select dubbed or commentary tracks by default when track 1 was stripped). **c)** Wipe all track titles blank (risk: removes useful labels like `"Director's Commentary"` or `"Signs & Songs"`). | **a**—prevents broken default track selection after stream pruning and uses `mkvpropedit` for sub-second header fixes on Matroska files. | **a** | `snatcharr-oq-0041-6` |
| `OQ-0041-7` | How should container conversion and lossless remux paths be prioritized relative to re-encoding? | **a)** Three-tier lossless-first pipeline: Tier 0 `mkvpropedit` header-only -> Tier 1 lossless `mkvmerge` / `ffmpeg -c copy` remux (including DoVi P7->P8.1 RPU conversion) -> Tier 2 lossy encode only when codec/size/resolution rules demand it (risk: orchestration state machine has three execution tiers instead of one). **b)** Always run a full `ffmpeg` decode/encode pass (risk: wastes hours of GPU/CPU time and degrades quality when a 20-second lossless remux would suffice). **c)** Support lossless remuxing only (risk: cannot reclaim disk space from oversized H.264/MPEG-2 streams). | **a**—maximizes speed and preserves bit-exact quality whenever a rule can be satisfied without re-encoding video. | **a** (Dolby Vision RPU conversion is a requirement on VMAFx) | `snatcharr-oq-0041-7` |
| `OQ-0041-8` | Which target video codecs and software encoders should be supported? | **a)** Curated support for `AV1` (`libsvtav1` primary, `libaom-av1`/`librav1e` advanced), `HEVC` (`libx265`), and `H.264` (`libx264`), keeping `VVC` (`libvvenc`) experimental/off by default (risk: `VVC` adopters must opt in explicitly). **b)** `AV1` (`libsvtav1`) only (risk: older Apple TV / TV hardware clients without `AV1` decode would force Jellyfin live transcoding). **c)** Unfiltered pass-through of all `ffmpeg -encoders` (risk: users select broken or obsolete encoders that fail verification). | **a**—covers modern archival (`AV1`), universal HDR hardware decode (`HEVC`), and legacy compatibility (`H.264`) without exposing footgun codecs. | **a** (requirement on VMAFx) | `snatcharr-oq-0041-8` |
| `OQ-0041-9` | How should hardware encoders (`NVENC`, `QSV`/`oneVPL`, `AMF`/`VAAPI`, `VideoToolbox`) and CPU fallback work? | **a)** Probe working hardware encoders at worker startup via a 1s synthetic `lavfi` test encode; per rule, allow `software-only`, `hardware-preferred-with-cpu-fallback`, or `hardware-only` (queue/wait rather than silently pegging CPU) (risk: startup probe adds ~1–2s per candidate hardware backend). **b)** Silently fall back to CPU software encoding whenever a GPU encode fails (risk: a transient GPU driver reset silently saturates all host CPU cores with `SVT-AV1`). **c)** CPU software encoding only (risk: unusable for adopters who rely on Intel Arc `QSV` or NVIDIA `NVENC` throughput). | **a**—validates real hardware availability up front (catching missing `/dev/dri` or driver mismatches) and respects operator choice on CPU fallback. | **a** (requirement on VMAFx) | `snatcharr-oq-0041-9` |
| `OQ-0041-10` | How should video quality vs. file size be targeted? | **a)** Selectable per rule between target perceptual quality search (`VMAFx vmaf-tune` / `ab-av1 crf-search` for `min_vmaf` or `min_xpsnr`) and fixed CRF/CQ presets, both strictly bounded by `max_encoded_percent` (abort and keep original if output is not smaller by the required margin) (risk: sample CRF search adds ~5–15% pre-encode probing time). **b)** Fixed CRF/CQ only (risk: fixed CRF either bloats grainy films larger than the source or crushes dark scenes). **c)** Fixed target bitrate by resolution (risk: starves high-complexity action and wastes bits on animation). | **a**—perceptual target search combined with a hard `max_encoded_percent` ceiling guarantees both visual fidelity and positive storage savings. | VMAFx `vmaf-tune` auto mode: rule gives target score, size cap, and allowed codecs; VMAFx chooses per-shot settings; keep original unless smaller; no fixed CRF/bitrate or ab-av1 | `snatcharr-oq-0041-10` |
| `OQ-0041-11` | How should `HDR10`, `HDR10+`, and Dolby Vision profiles (`5`, `7`, `8.1`) be handled? | **a)** Preserve `HDR10` static metadata; round-trip `HDR10+` SEI via `hdr10plus_tool`; preserve DoVi Profile 8.1 RPU; convert DoVi Profile 7 (`FEL`/`MEL`) to Profile 8.1 via `dovi_tool --mode 2` (with optional `EL` backup or `FEL` skip policy); skip video re-encode on proprietary `IPT-PQ-C2` Profile 5 unless explicit reshape is configured; tone-map to SDR only when explicitly requested (risk: requires bundling `dovi_tool` and `hdr10plus_tool` in worker images). **b)** Tone-map all HDR/DoVi to 8-bit SDR (risk: permanently destroys HDR and Dolby Vision mastering across the user's library). **c)** Skip all HDR/DoVi files (risk: leaves the largest 4K files in the library completely unprocessed). | **a**—preserves dynamic HDR metadata for modern displays and fixes Blu-ray Profile 7 compatibility without colour corruption on Profile 5. | **a** (requirement on VMAFx) | `snatcharr-oq-0041-11` |
| `OQ-0041-12` | How should resolution scaling, cropping, deinterlacing, and frame rate be handled? | **a)** Never upscale; optional max-resolution downscale (`Lanczos`/`Spline`); `-fps_mode passthrough` preserving source CFR/VFR; auto-deinterlace (`bwdif`) only on interlaced sources; do not auto-crop black bars by default so DoVi L5 offsets and PGS subtitles stay aligned (risk: keeps letterbox black bars unless crop is explicitly enabled on a rule). **b)** Auto-crop (`cropdetect`) and force CFR on all files (risk: misaligns graphic PGS/VobSub subtitles and invalidates Dolby Vision L5 active-area metadata). **c)** Forbid all scaling and deinterlacing (risk: cannot downscale 4K to 1080p or fix 1080i broadcast captures). | **a**—avoids the PGS subtitle and Dolby Vision L5 breakage caused by blind auto-cropping while supporting explicit downscale rules. | **a** (requirement on VMAFx) | `snatcharr-oq-0041-12` |
| `OQ-0041-13` | How should audio codecs and loudness normalization be handled? | **a)** Configurable per rule: keep lossless (`TrueHD`/`DTS-HD MA`/`FLAC`), keep lossless + add a secondary `Opus`/`E-AC-3`/`AAC` compatibility track, or transcode to `Opus`/`E-AC-3`/`AAC`; prefer non-destructive `EBU R128` loudness tags over destructive sample-altering `loudnorm` (with `loudnorm` opt-in for night-mode tracks) (risk: adding a compatibility track slightly increases container size when lossless audio is kept). **b)** Re-encode all audio to stereo `AAC` with baked-in `loudnorm` (risk: destroys surround sound and Atmos/DTS:X spatial metadata). **c)** Stream-copy audio only (risk: cannot convert legacy `AC-3`/`DTS` to `Opus` or add stereo compatibility tracks). | **a**—protects lossless home-theatre audio while optionally providing direct-play stereo/surround fallback and non-destructive loudness tags. | **a** | `snatcharr-oq-0041-13` |
| `OQ-0041-14` | What staging, pre-replace verification, and rollback mechanism should guard original files? | **a)** Stage to `.snatcharr-tmp`, verify container/streams, $\|\Delta\text{duration}\| \le 1.0\text{s}$, zero decode errors, size $< \text{max\_encoded\_percent}$, and optional `VMAFx` `phash` + score gate; then apply configurable disposition: quarantine original in `.snatcharr-trash/` with TTL + one-click rollback, atomic in-place replace, or Jellyfin multi-version sibling (risk: quarantine TTL temporarily holds disk space until expiry). **b)** Immediate in-place overwrite on `ffmpeg` exit `0` (risk: truncated or corrupted encodes permanently destroy user media). **c)** Keep both files permanently (risk: doubles library storage usage). | **a**—enforces a two-barrier verification gate (`ffprobe`/decode + `VMAFx` cut/quality check) and reversible quarantine before permanent deletion. | **a** | `snatcharr-oq-0041-14` |
| `OQ-0041-15` | How should `*arr` (`Radarr`, `Sonarr`, `Lidarr`, `Readarr`, `Whisparr`) and media servers (`Jellyfin`, `Stash`, `Audiobookshelf`) be notified after file changes? | **a)** Strictly via each service's REST API ([ADR-0006](0006-download-client-backpressure.md)): trigger `RefreshMovie`/`RefreshSeries`/`RefreshArtist`, poll completion, optionally run `/api/v3/rename` (`RenameMovie`/`RenameSeries`) so codec tokens in filenames stay truthful, then refresh the item in Jellyfin/Stash/Audiobookshelf (risk: adds 2–4 API calls per completed file). **b)** Rely on scheduled 12-hour background disk scans (risk: `*arr` and Jellyfin show stale codec/size metadata or broken file paths for hours). **c)** Mutate `*arr` databases directly (risk: corrupts upstream state and violates service boundaries). | **a**—keeps `*arr` media info, naming tokens, and Jellyfin playback metadata immediately consistent through official APIs. | **a** | `snatcharr-oq-0041-15` |
| `OQ-0041-16` | How should scheduling windows, active Jellyfin playback yield, and host resource limits work? | **a)** Support daily/cron time windows, automatic pause or OS-level process freeze (`SIGSTOP`/`SIGCONT`) when active Jellyfin playback or live-transcode sessions are running, and explicit thread/memory caps (risk: background queue throughput pauses during peak evening viewing hours). **b)** Run jobs continuously at full hardware capacity (risk: causes frame drops and buffering for users watching Jellyfin). **c)** Manual execution only (risk: requires constant operator babysitting). | **a**—Playback always takes priority over background library optimization on shared homelab hardware. | **a** | `snatcharr-oq-0041-16` |
| `OQ-0041-17` | How should worker pools, GPU allocation, and concurrency bounds (`HISS-06`) be structured? | **a)** Separate bounded worker pools for `remux_io`, `encode_cpu`, and `encode_gpu` (using ADR-0038's dynamic `--backend auto` GPU pool without static ordinal pinning), with priority scheduling so fast lossless remuxes never starve behind multi-hour encodes (risk: requires separate concurrency counters in the lease protocol). **b)** Single FIFO worker slot per node (risk: head-of-line blocking stalls fast remux jobs for hours). **c)** Unbounded concurrency (risk: violates `HISS-06` and triggers host OOM or GPU VRAM exhaustion). | **a**—satisfies `HISS-06` scalar bounds, prevents head-of-line blocking, and shares GPUs cleanly with `VMAFx`. | **a** (remux/audio pools in SnatchArr; GPU pooling is requirement on VMAFx) | `snatcharr-oq-0041-17` |
| `OQ-0041-18` | At what granularity can processing rules be scoped? | **a)** Hierarchical scoping: per-item -> per-user request language preference -> per-library / root folder -> per-`*arr` instance -> per-media-type (`movie`, `episode`, `anime`, `music`, `audiobook`, `stash_scene`) -> global default (risk: UI must clearly display which rule won via a trace badge). **b)** Per-filesystem-folder only (risk: cannot distinguish anime from live-action in a shared TV library or honour requester languages). **c)** Single global rule (risk: forces identical settings on 4K archival movies, daily TV soaps, and anime). | **a**—matches SnatchArr's existing multi-instance `*arr` and Jellyfin domain model and surfaces rule precedence in `RuleTrace`. | **a** | `snatcharr-oq-0041-18` |
| `OQ-0041-19` | What rule format and dry-run preview model should be used? | **a)** Declarative YAML/JSON schema (GitOps-friendly, OpenAPI + Zod v4 validated, zero `eval`/JS execution per `HISS-08`) with an `observe`-first dry-run preview (`RuleTrace`) showing stream diffs, command plan, and predicted savings before enabling `active` mode (risk: users migrating from `Tdarr` JS plugins express rules declaratively rather than in custom JS code). **b)** User-authored JavaScript flow plugins (risk: violates `HISS-08` determinism/no-dynamic-code invariant and creates security/sandbox hazards). **c)** Raw `ffmpeg` CLI strings (risk: brittle, unsafe, and impossible to validate or preview stream diffs). | **a**—enforces [ADR-0012](0012-queue-management-replaces-cleanuparr-link.md) observe-first safety and `HISS-08` determinism while enabling GitOps configuration sync. | **a** | `snatcharr-oq-0041-19` |
| `OQ-0041-20` | Where is the user interface surfaced? | **a)** First-class views in SnatchArr's SvelteKit SPA (`web/`) **and** embedded inside Jellyfin via `jellysin/plugin-snatcharr` ([ADR-0028](0028-jellyfin-plugin.md), [ADR-0035](0035-jellyfin-integration.md)), including item-level preview/enqueue actions inside Jellyfin (risk: every view must pass Playwright + axe-core WCAG 2.2 AA gates in both standalone and embedded contexts). **b)** Standalone SnatchArr web UI only (risk: violates [ADR-0035](0035-jellyfin-integration.md) Jellyfin-only user direction). **c)** Separate C#/HTML plugin UI inside Jellyfin (risk: duplicates frontend code and violates `HISS-19` reuse). | **a**—aligns with [ADR-0028](0028-jellyfin-plugin.md) and [ADR-0035](0035-jellyfin-integration.md) by serving one accessible Svelte 5 UI in both surfaces. | **a** | `snatcharr-oq-0041-20` |
| `OQ-0041-21` | How should SnatchArr prevent `*arr` Custom Format / quality profile cutoff drops and infinite upgrade loops after re-encoding? | **a)** Preserve release/quality tokens (or apply `*arr` rename), run pre-flight `*arr` CF/cutoff simulation in dry-run, tag processed items (`SNATCHARR_PROCESSED_FROM` + optional `*arr` tag), and provide a per-rule toggle to record terminal state or unmonitor in `*arr` when target quality is met (risk: pre-flight CF check requires syncing `*arr` custom format specifications). **b)** Unmonitor every processed item in `*arr` unconditionally (risk: stops legitimate future upgrades such as `WEBDL-1080p` -> `Bluray-2160p` when the user still wanted a resolution upgrade). **c)** Ignore `*arr` Custom Formats (risk: causes infinite re-snatch and re-encode loops on `x265`/`AV1` size or codec rules). | **a**—prevents infinite re-snatch loops without blinding `*arr` to legitimate future resolution or source-tier upgrades. | **a** | `snatcharr-oq-0041-21` |
| `OQ-0041-22` | How should library processing interact with SnatchArr's own snatch `Planner`? | **a)** Record processed entities in `SnatchArr api` so the snatch `Planner` never re-snatches the old codec/size for the same quality tier (`RuleNotAnUpgrade`), and optionally let the planner prefer high-bitrate remux candidates when a rule will locally transcode them to `AV1` (risk: couples processed-item ledger to `Planner` candidate filtering). **b)** Keep `Planner` and library processor completely separate (risk: SnatchArr's own cutoff-unmet loop may snatch a replacement for a file it just re-encoded). **c)** Disable cutoff-unmet snatching globally when library processing is enabled (risk: breaks core SnatchArr functionality). | **a**—unifies acquisition and post-import optimization under one control plane so SnatchArr never fights its own library rules. | **a** | `snatcharr-oq-0041-22` |

---

## Verification plan

Implementation slices S1 through S8 are verified by unit, property, CLI fixture, and integration tests under HISS-15 (positive, negative, and boundary cases) and HISS-20 (replayable fixtures):

1. **Positive cases (synthetic `ffmpeg -f lavfi -i testsrc2` fixtures only, zero copyrighted media)**:
   - Lossless Tier 0 (`mkvpropedit`) flag/title normalization and Tier 1 (`mkvmerge`) multi-language audio/subtitle pruning preserving original language (`jpn`) + user language (`eng`, `deu`) and stripping commentary tracks (Slice S3).
   - Dolby Vision Profile 7 -> Profile 8.1 RPU conversion and HDR10+ SEI extraction/injection round-trip on synthetic Annex B bitstreams (Slice S6 requirement on VMAFx).
   - VMAFx `vmaf-tune` auto encode bounded by `max_encoded_percent` with pre-replace verification (`ffprobe` stream check, $|\Delta\text{duration}| \le 1.0\text{s}$, `VMAFx` `oshash`/`phash` cut gate) and `*arr` `RefreshMovie` / `RenameMovie` API mock verification (Slices S5, S6).
2. **Negative and boundary cases**:
   - **Size cap boundary**: Encoded output at `85.1%` of source size when `max_encoded_percent = 85.0%` must abort replacement, discard staging file, and record `skipped_size_cap` in processed memory (Slice S5).
   - **Unknown language fallback**: Source with all audio tracks tagged `und` must keep all audio tracks rather than producing a silent video file (Slice S3).
   - **Dolby Vision Profile 5 guard**: Source with `dv_profile = 5` (`IPT-PQ-C2`) under a standard `HEVC`/`AV1` re-encode rule without reshape must skip video re-encode rather than corrupting colour space (Slice S6 requirement on VMAFx).
   - **Upgrade-loop guard**: Re-encoded file whose post-encode codec/size would fall below `*arr`'s cutoff must trigger the configured `OQ-0041-21` barrier so `Planner` returns `RuleNotAnUpgrade` (Slice S7).
3. **Planted negative replay (`HISS-20`)**:
   - A planted truncated staging file ($|\Delta\text{duration}| = 3.5\text{s}$) and a planted spliced frame mismatch (`phash` Hamming distance $> 8$) must be rejected by the pre-replace verification gate before any rename or quarantine operation touches the original file (Slice S5).
4. **UI & accessibility gates**:
   - Playwright + `@axe-core/playwright` zero-violation sweep (`make web-e2e`) across the rule editor, `RuleTrace` dry-run preview table, and rollback views in both standalone `web/` and the Jellyfin plugin host view (`en` and `de` locales) (Slice S8).

---

## Follow-up slices and milestones

Follow-up implementation work is tracked under the version milestone **0.13 (rule-driven library processing)**:

### Slice table

| Slice | Scope | Target milestone | Dependencies |
| :--- | :--- | :--- | :--- |
| **S1** ([#789](https://github.com/lusoris/SnatchArr/issues/789)) | Module `library_processing` ([ADR-0025](0025-module-switches.md) switch, default off) + declarative rule schema (YAML/JSON, OpenAPI + Zod v4) + scoping hierarchy + `RuleTrace` dry-run preview (`OQ-0041-3`, `OQ-0041-18`, `OQ-0041-19`). | **0.13** | [ADR-0025](0025-module-switches.md) |
| **S2** ([#790](https://github.com/lusoris/SnatchArr/issues/790)) | Worker lease capability + bounded `remux_io` and audio worker pools + schedule windows / Jellyfin playback yield (`OQ-0041-1`, `OQ-0041-16`). | **0.13** | S1, [ADR-0002](0002-lease-pull-work-distribution.md) |
| **S3** ([#791](https://github.com/lusoris/SnatchArr/issues/791)) | Lossless Tiers 0 and 1 owned by SnatchArr: stream and track selection, flags, titles, container remux (`OQ-0041-4`..`OQ-0041-7`). | **0.13** | S1, S2 |
| **S4** ([#792](https://github.com/lusoris/SnatchArr/issues/792)) | Audio handling and loudness normalization (`OQ-0041-13`). | **0.13** | S3 |
| **S5** ([#793](https://github.com/lusoris/SnatchArr/issues/793)) | Staging, pre-replace verification gates, quarantine and rollback, `*arr` refresh/rename, and media server refresh via REST API (`OQ-0041-14`, `OQ-0041-15`). | **0.13** | S3, S4, [ADR-0006](0006-download-client-backpressure.md) |
| **S6** ([#794](https://github.com/lusoris/SnatchArr/issues/794)) | VMAFx encode-job client: SnatchArr sends input file + stream map + rule targets, receives verified video stream + provenance (`OQ-0041-8`..`OQ-0041-12`, `OQ-0041-17` as requirements on VMAFx). | **0.13** | S5, issue [#761](https://github.com/lusoris/SnatchArr/issues/761), VMAFx encode-job API ADR (VMAFx repository, pending) |
| **S7** ([#795](https://github.com/lusoris/SnatchArr/issues/795)) | `*arr` Custom Format / cutoff guard + planner `RuleNotAnUpgrade` (`OQ-0041-21`, `OQ-0041-22`). | **0.13** | S5 |
| **S8** ([#796](https://github.com/lusoris/SnatchArr/issues/796)) | UI in web SPA + Jellyfin companion plugin surface, Playwright + axe-core accessibility gate (`OQ-0041-20`). | **0.13** | S1, S5, [ADR-0028](0028-jellyfin-plugin.md) |
| **S9** ([#797](https://github.com/lusoris/SnatchArr/issues/797)) | Remaining VMAFx surfaces through the encode-job API: scoring, compare, bitrate ladder, per-shot tuning, saliency, HDR handling, sample clips, node workers; one issue per surface once the VMAFx encode-job API ADR names its phases. | **0.14** | S6, VMAFx encode-job API ADR (VMAFx repository, pending) |

---

## References

- Ledger `snatcharr-rule-reencode-research-20261026`: the order for this research and ADR draft; plan only, every choice an open question.
- Ledger `snatcharr-reencode-vmaf-tune-facts`: read the VMAFx tree read-only at its newest tag and mark the VMAFx section for review by the VMAFx session.
- Ledger `snatcharr-oq-0038-9`: VMAF model `vmaf_v1.0.16_3d0h` everywhere, cited in the VMAFx section.
- Ledger range `snatcharr-oq-0041-1` to `snatcharr-oq-0041-22`: answers to open questions OQ-0041-1 through OQ-0041-22, recorded 2026-10-26.
- Ledger `snatcharr-vmafx-video-split-20261026`: ownership split between SnatchArr and VMAFx decided 2026-10-26 (VMAFx owns the video stream; SnatchArr owns rules, remux, audio, safety, and coordination).
- Ledger `snatcharr-vmafx-full-toolkit-20261026`: SnatchArr uses every VMAFx function through the encode-job API, auto encode first and the other surfaces as later slices, decided 2026-10-26.
