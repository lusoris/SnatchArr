<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0010: Keep the api and worker split; no single-container mode

- Status: accepted
- Date: 2026-10-17
- Amends: [ADR-0001](0001-go-control-plane-rust-snatch-worker.md), with the measured scaling numbers below. The split it accepted stands.

## Context

ADR-0001 split SnatchArr into a Go control plane (`api/`) and a Rust data plane (`worker/`), so that searching scales apart from the API. That split costs two *arr clients: goenvoy in Go, used in 22 api Go files, and the Rust `arr-client` crate, about 2600 lines in `worker/crates/arr-client`. Every *arr API change touches both.

Issue #44 asked whether homelab installs need a single container, with three options:

- Keep the split.
- Add an in-process Go executor on goenvoy beside the Rust worker, for single-container installs.
- Move execution to Go and retire the worker.

Measured since ADR-0001 (issue #43):

- Planner tick (`api/internal/snatch/planner_scale_test.go`, PR #315; [Concepts](../concepts.md)): one tick took 28 ms with 10 instances due, 189 ms with 100 and 815 ms with 500, on a 16-core desktop. The test fails above 30 s, half the tick interval.
- Worker autoscaling on queued runs (`make kind-keda-proof`, PR #331, KEDA 2.21.0, released 0.2.1 images, 50 runs queued). The worker went from 1 to 4 replicas 14 s after the queue query was restored, and back to 1 replica 537 s after every run was cancelled. The CPU HPA did not follow queued runs.
- The homelab bundle (`deploy/homelab/compose.yaml`, PR #294) already runs three containers: postgres, api and worker. A single-container mode would save one of the three.

## Decision

- Keep the api and worker split of ADR-0001. There is no single-container mode, no Go executor beside the worker, and the worker is not retired.
- The compose bundle stays at three containers. The Helm chart keeps one Deployment each for api and worker, with optional CPU HPA or KEDA scaling of the worker on queued runs ([Deployment](../deployment.md#worker-autoscaling)).
- The duplicate *arr client cost is contained, not removed. The Rust `arr-client` surface stays as small as ADR-0001 set it (wanted lists, commands, queue, system status). `make contracts-verify` tests `arr-client` against the upstream specs pinned in `tools/upstreamspec/specs.yaml`, so a pin bump that changes an operation the worker uses fails there instead of drifting silently.

## Consequences

- **Positive**: one execution path for leases, budget and processed memory, tested once. Worker scaling on queued runs works as measured. Homelab users keep the bundle they already run.
- **Negative**: every *arr API change still needs an edit in goenvoy and in `arr-client`. A homelab install runs two SnatchArr containers instead of one.
- **Revisit when**: the compose bundle has to shrink below three containers for a supported platform, or keeping both *arr clients in step costs more than one change per *arr release.
