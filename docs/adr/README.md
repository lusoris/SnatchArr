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
| [0011](0011-product-definition.md) | 2026-10-17 | Product definition: parity with Huntarr and newtarr | Accepted |
| [0012](0012-queue-management-replaces-cleanuparr-link.md) | 2026-10-17 | SnatchArr manages the download queue; the Cleanuparr link goes | Accepted |
| [0013](0013-force-import-id-matched-releases.md) | 2026-10-18 | SnatchArr force-imports ID-matched releases and owns the queue alone | Accepted; amended by 0014 |
| [0014](0014-shadow-parity-week-for-id-matched.md) | 2026-10-18 | The id_matched parity week runs in shadow beside the live unblocker | Accepted |
