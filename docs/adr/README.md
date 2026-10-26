<!-- markdownlint-disable MD013 -->
# Architectural Decision Records (ADRs)

This directory documents key architectural decisions following the HISS-14 immutable numbering lattice.

| Number | Date | Title | Status |
| :--- | :--- | :--- | :--- |
| [0000](0000-template.md) | 2026-09-11 | ADR Architecture Decision Template | Accepted |
| [0001](0001-go-control-plane-rust-snatch-worker.md) | 2026-09-20 | Go control plane, Rust snatch-worker | Accepted; amended by 0010 |
| [0002](0002-lease-pull-work-distribution.md) | 2026-09-20 | Lease/pull work distribution over gRPC | Accepted |
| [0003](0003-configarr-as-instance-source.md) | 2026-09-20 | Configarr as an instance source | Accepted |
| [0004](0004-no-swaparr-cleanuparr-deferred.md) | 2026-09-20 | No Swaparr; Cleanuparr complementary | Superseded by 0012 |
| [0005](0005-static-spa-embedded-in-api-image.md) | 2026-09-20 | Static SPA embedded in the API image | Accepted |
| [0006](0006-download-client-backpressure.md) | 2026-09-21 | Download clients observed for backpressure, never driven | Accepted |
| [0007](0007-read-only-cleanuparr-link.md) | 2026-10-09 | Read-only Cleanuparr link in v1 | Superseded by 0012 |
| [0008](0008-readarr-frozen-legacy-target.md) | 2026-10-11 | Readarr stays a supported target, frozen | Accepted |
| [0009](0009-detect-cleanuparr-seeker.md) | 2026-10-16 | Detect Cleanuparr Seeker and warn on double searching | Superseded by 0012 |
| [0010](0010-keep-api-worker-split.md) | 2026-10-17 | Keep the api and worker split; no single-container mode | Accepted |
| [0011](0011-product-definition.md) | 2026-10-17 | Product definition: parity with Huntarr and newtarr | Superseded by 0018 |
| [0012](0012-queue-management-replaces-cleanuparr-link.md) | 2026-10-17 | SnatchArr manages the download queue; the Cleanuparr link goes | Accepted |
| [0013](0013-force-import-id-matched-releases.md) | 2026-10-18 | SnatchArr force-imports ID-matched releases and owns the queue alone | Accepted; amended by 0014 |
| [0014](0014-shadow-parity-week-for-id-matched.md) | 2026-10-18 | The id_matched parity week runs in shadow beside the live unblocker | Accepted |
| [0015](0015-ui-design-system-and-layout.md) | 2026-10-19 | SnatchArr's own UI: palette A, the M3 mark, a task-flow navigation and a UI that never blocks | Accepted; mark superseded by 0017 |
| [0016](0016-ui-layout-and-page-redo.md) | 2026-10-20 | Modern operations layout: collapsible navigation, unified page headers, mobile bottom bar and dialog sheets | Accepted |
| [0017](0017-anatomical-brand-mark.md) | 2026-10-20 | An anatomical vulva mark replaces the M3 vesica | Accepted |
| [0018](0018-product-front-door.md) | 2026-10-20 | Product front door of the media stack | Proposed |
| [0019](0019-foreign-title-search.md) | 2026-10-20 | Foreign-title search and release push without proxy indexers | Proposed |
| [0020](0020-snatch-policy-defaults-and-presets.md) | 2026-10-20 | Snatch policy defaults and presets | Proposed |
| [0021](0021-llm-access-module.md) | 2026-10-20 | LLM access module | Proposed |
| [0022](0022-instance-priority.md) | 2026-10-21 | Per-instance priority for searches and downloads | Proposed |
| [0023](0023-sso-viewer-role.md) | 2026-10-21 | Roles from single sign-on groups | Accepted |
| [0024](0024-watch-aware-modes.md) | 2026-10-23 | Watch-aware modes: remove when seen, and the season gate | Proposed |
| [0025](0025-module-switches.md) | 2026-10-24 | Architecture and mechanism of optional module switches | Proposed |
| [0026](0026-accounts-and-roles.md) | 2026-10-24 | Accounts, roles and Jellyfin sign-in | Proposed |
| [0027](0027-adult-module.md) | 2026-10-24 | Adult module: stash-box sources, Whisparr requests and image sets | Proposed |
| [0028](0028-jellyfin-plugin.md) | 2026-10-24 | Jellyfin companion plugin and its contract | Proposed |
| [0029](0029-task-flow-ui.md) | 2026-10-24 | Task-flow navigation, one activity view, desktop-first density and context help | Accepted |
| [0033](0033-adult-identity-and-model.md) | 2026-10-26 | Adult identity resolution and normalized data model | Accepted |
