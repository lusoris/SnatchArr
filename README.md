<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: CC-BY-SA-4.0
-->

# SnatchArr

<img src="web/static/brand/mark.svg" alt="" width="72" align="right" />

SnatchArr keeps your *arr libraries complete without hammering your indexers. It asks Sonarr, Radarr, Lidarr, Readarr and Whisparr (v2 and v3) to search for **missing** items and **cutoff-unmet upgrades** in small, randomised batches, remembers what it already searched, and enforces a per-instance hourly cap.

It is a from-scratch, cloud-native rewrite of the Huntarr lineage ([newtarr](https://github.com/elfhosted/newtarr)), built on the lusoris stack:

| Component | Stack |
| --- | --- |
| `api/` | Go 1.27 control plane on [golusoris](https://github.com/golusoris/golusoris) with [goenvoy](https://github.com/golusoris/goenvoy) *arr clients, OpenAPI 3.1, RFC 9457, SSE |
| `worker/` | Rust hunt-worker (tokio + tonic) that leases hunt runs from the API over gRPC |
| `web/` | SvelteKit SPA on [sveltesentio](https://github.com/golusoris/sveltesentio), WCAG 2.2 AA / EN 301 549, en + de |
| `deploy/` | Distroless OCI images, Helm chart, CloudNativePG, ArgoCD |

Governance: [praetor](https://github.com/cordanaLLM/praetor) (HISS invariants, canonical `AGENTS.md`).

## Status

Pre-alpha. Nothing is released yet.

## Instances from Configarr

If you already run [Configarr](https://github.com/raydak-labs/configarr), point SnatchArr at its `config.yml` and `secrets.yml` and every instance is imported (and kept in sync) instead of being configured twice.

## Running Cleanuparr too?

[Cleanuparr](https://github.com/Cleanuparr/Cleanuparr)'s *Seeker* feature also hunts missing and cutoff-unmet items. Running both against the same instance doubles the searches your indexers see. Disable Seeker for instances SnatchArr manages, or the other way round. A Cleanuparr integration that warns about this is planned for v1.1.

## License

Code: [EUPL-1.2](LICENSES/EUPL-1.2.txt). Documentation: [CC BY-SA 4.0](LICENSES/CC-BY-SA-4.0.txt). REUSE-compliant.
