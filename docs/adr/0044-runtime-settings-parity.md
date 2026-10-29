<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>

SPDX-License-Identifier: CC-BY-SA-4.0
-->
<!-- markdownlint-disable MD013 -->

# ADR-0044: Runtime Settings Parity Between Environment, API and UI

- **Status**: Accepted (item 6 pending an operator decision)
- **Date**: 2026-10-29
- **Deciders**: lusoris

## Context

**SnatchArr** (meaning: an automated media-snatching coordinator for `*arr` instances; origin: English *snatch* + the `*arr` suffix of the Servarr ecosystem; pronunciation: `/ˈsnætʃ.ɑːr/`) reads its configuration once at start-up. `config.Load` (`api/internal/config/config.go:504`) decodes the `snatcharr.*` koanf tree, where **koanf** (meaning: the Go configuration library golusoris loads settings with; origin: the open-source `knadh/koanf` project; pronunciation: as spelled, `/koʊ.ænf/`) maps `APP_SNATCHARR_*` environment variables onto keys, and **golusoris** (meaning: shared Go foundation library for the lusoris fleet; origin: *Go* + Latin *lusoris*, "of the player"; pronunciation: `/ˌɡoʊ.luːˈsɔːr.ɪs/`) adds the `APP_HTTP_*`, `APP_DB_*` and `APP_CRYPTO_KEY` keys.

An inventory of every variable the API, worker and chart read found 131 variables in three classes:

| Class | Count | Meaning |
| :--- | ---: | :--- |
| bootstrap | 55 | needed before the store opens or the server listens (database DSNs, listen addresses, TLS paths, HTTP limits, profile) |
| runtime | 63 | read by a running feature (sign-in providers, session lifetime, proxy trust, Configarr paths, log ring size, cooldowns) |
| secret | 13 | credentials and signing keys |

57 of the 63 runtime variables have no field in the UI, and many have no read-back over the HTTP API. The only database-backed settings are the singleton `settings` row (`api/internal/settings/service.go`) for history retention, user agent, global hourly cap, priority presets and the LLM switch. An administrator therefore has to edit the Helm values or the compose file to change, for example, the OIDC (OpenID Connect) group mapping, and cannot see from the UI which value is in force.

The operator asked for parity (paraphrased): every non-secret setting gets a UI field; a value set in the environment wins and the UI shows it locked as set by the environment; secrets are write-only; bootstrap values are shown read-only.

## Decision

1. **One registry of settings.** A Go registry in `api/internal/config` lists every key with its environment name, class (`bootstrap`, `runtime`, `secret`), type, default, validation and whether a change applies live or on restart. The registry is the single source; the docs table, the OpenAPI enum of keys and the UI form schema are generated from it.
2. **Precedence: environment > database > default.** Runtime and storable secret values may be stored in a new `runtime_settings` table (key, value, updated_at, updated_by). At start-up the API opens the store with the bootstrap values, then merges the stored overrides under the environment and runs the existing `Options.Validate` on the result. A key whose environment variable is set reports `source: env` and refuses writes with a `409` problem.
3. **Bootstrap values are read-only.** They are returned with `source` and value (never a DSN password, which is redacted) and cannot be written through the API, because they are needed before the store is open.
4. **Secrets are write-only.** A storable secret is encrypted at rest with the existing `core/crypto` AES-GCM encryptor (`api/internal/app/crypto.go`). Reads return only `{set: bool, source}`; writes replace or clear the value. Secrets the store or its encryption depends on stay environment-only: `APP_DB_*`, `APP_CRYPTO_KEY`, the worker token and the API key signing secret.
5. **Writes are validated as a whole.** `PUT /api/v1/settings/runtime` merges the patch into the effective options and runs `Options.Validate`; a failing merge returns a `422` problem naming the key, and nothing is stored. Admin role only; every write records the key, the acting user and the time in `runtime_settings`, never a secret value, and logs the key name.
6. **Apply timing is open.** When a changed value takes effect is an open operator decision. Options under review: apply on restart, live apply for all runtime settings, or live apply for a named set. The first implementation applies stored values at the next start, because most consumers (OIDC provider, session manager, proxy trust) are built once by fx; that path is the base the other options build on, not the final behaviour. Until the decision is recorded, a write returns `pending_restart: true` for every key and the UI says so.
7. **Parity is a test.** A generated parity table (`docs/configuration.md` section and a committed TSV) lists each key with its class, UI field and docs row. A Go test fails when a registry key lacks a UI field (non-secret runtime), a docs row, or a class, and when a variable read by the code is missing from the registry. Its negative case is a planted key without a UI field.

## Consequences

- Administrators see every setting and its source in the UI and can change runtime settings without editing deployment files.
- Deployments that set everything through the environment keep working unchanged: every such field shows as locked.
- Until the apply-timing decision is recorded, changes take effect at the next start; this record is amended when it is.
- Adding a setting now means a registry entry; the parity test refuses a key without a UI field and docs row.
- A database restore also restores runtime overrides; the environment still wins over them.
