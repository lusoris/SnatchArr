<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: CC-BY-SA-4.0
-->

# SnatchArr

<img src="web/static/brand/mark.svg" alt="" width="72" align="right" />

SnatchArr keeps your *arr libraries complete without hammering your indexers. It asks Sonarr, Radarr, Lidarr, Readarr and Whisparr (v2 and v3) to search for **missing** items and **cutoff-unmet upgrades** in small batches. Every search is a *snatch*: it remembers what it already snatched (the *afterglow*), never exceeds an instance's hourly *stamina*, and slows down (*foreplay*) while your download client is busy, paused or gone. Yes, the name means what you think it means.

Docs: <https://lusoris.github.io/SnatchArr/>

It is a from-scratch, cloud-native rewrite of the Huntarr lineage ([newtarr](https://github.com/elfhosted/newtarr)), built on the lusoris stack:

| Component | Stack |
| --- | --- |
| `api/` | Go 1.27 control plane on [golusoris](https://github.com/golusoris/golusoris) with [goenvoy](https://github.com/golusoris/goenvoy) *arr clients, OpenAPI 3.1, RFC 9457, SSE |
| `worker/` | Rust snatch-worker (tokio + tonic) that leases snatch runs from the API over gRPC |
| `web/` | SvelteKit SPA on [sveltesentio](https://github.com/golusoris/sveltesentio), WCAG 2.2 AA / EN 301 549, en + de |
| `deploy/` | Distroless OCI images, Helm chart, CloudNativePG, ArgoCD |

Governance: [praetor](https://github.com/cordanaLLM/praetor) (HISS invariants, canonical `AGENTS.md`).

## Status

Pre-alpha. Nothing is released yet.

## Instances from Configarr

If you already run [Configarr](https://github.com/raydak-labs/configarr), point SnatchArr at its `config.yml` and `secrets.yml` and every instance is imported (and kept in sync) instead of being configured twice.

## Running Cleanuparr too?

[Cleanuparr](https://github.com/Cleanuparr/Cleanuparr) cleans up what your download clients choke on; SnatchArr only decides what to search for next. They complement each other. A read-only Cleanuparr integration (status, recent strikes, skipping struck items) is part of v1.

## License

Code: [EUPL-1.2](LICENSES/EUPL-1.2.txt). Documentation: [CC BY-SA 4.0](LICENSES/CC-BY-SA-4.0.txt). REUSE-compliant.

<!-- praetor:readme-governance:start -->
[![HISS Adopted](https://img.shields.io/badge/Standards-HISS%20Adopted-blue)](AGENTS.md)

Praetor manages this repository's declared governance policy. This managed block records adoption state; it is not a verification certificate.

| Gate | Command | Contract |
| :--- | :--- | :--- |
| **Verification** | `make verify-all` | Runs the repository's configured verification cascade |
| **HISS Audit** | `praetorctl audit` | Enforces policy, generated-surface integrity, and the debt ratchet |
| **Context Sync** | `praetorctl compile-context --verify` | Verifies every generated agent context against `AGENTS.md` |
| **Debt Baseline** | `.standards-baseline.json` | 0 recorded infractions; audit forbids growth |
<!-- praetor:readme-governance:end -->
