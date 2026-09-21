# SnatchArr repository guide

SnatchArr hunts missing and cutoff-unmet media across *arr instances while respecting
per-instance hourly caps. Three components, one contract each way:

| Path | What it is | Verify |
| :--- | :--- | :--- |
| `api/` | Go 1.27 control plane on golusoris (fx). Owns all durable state and every "when / how much" decision. Serves the OpenAPI 3.1 HTTP API, SSE, and the gRPC `WorkerService`. Embeds the built SPA. | `make api-verify` |
| `worker/` | Rust hunt data-plane (tokio + tonic). Stateless: leases runs from the API, pages *arr wanted lists, dispatches searches, reports events. | `make worker-verify` |
| `web/` | SvelteKit SPA (adapter-static) on `@sveltesentio/*`. Pure client of the Go API; never talks to *arr. | `make web-verify`, `make web-e2e` |
| `proto/` | `snatcharr.v1.WorkerService` — the only Go↔Rust contract. | `make proto-verify` |
| `deploy/` | Helm chart, kind overlay, ArgoCD, compose. | `make deploy-verify` |

`make verify-all` runs every gate. Run it before concluding any turn.

## Contracts

- **OpenAPI first.** `api/openapi/openapi.yaml` is the source of truth. `make api-gen` regenerates the ogen server (`api/internal/oas`), `make web-gen` regenerates `web/src/lib/api/schema.d.ts`. Never hand-edit either output.
- **RFC 9457 everywhere.** Every non-2xx HTTP response is `application/problem+json`. The SPA's `problemMiddleware` depends on that media type.
- **gRPC lease model.** The worker PULLS: `LeaseRun` → `Heartbeat` → `FilterCandidates` → `AcquireBudget` → `ReportEvents` → `CompleteRun`. The API is the only writer of processed memory and rate buckets; the worker never exceeds a granted budget.
- **Hourly cap counts items searched**, not HTTP requests. Debit per dispatch, warn at 80 %.
- **Processed memory** is per instance × hunt kind × entity with a per-row `expires_at`.
- **SSE event names** are the `snatcharr.v1.EventType` names in lower snake case (`search_dispatched`, …).
- **Configarr instances are read-only** in the UI except their hunt policy; `source=configarr` rows are re-imported on file change.

## Language rules

- **Go**: golangci-lint v2 with the root `.golangci.yml` (HISS-04 caps: cyclomatic ≤ 10, cognitive ≤ 15, ≤ 60 lines / 50 statements per function). gosec runs with zero exclusions; every `#nosec` carries `Gxxx -- <reason>`. `time.Now()` is banned outside golusoris `core/clock`. Every I/O call takes a `context.Context` with a deadline. Wrap errors with `%w`. Table-driven tests with positive, negative and boundary cases.
- **Rust**: `#![forbid(unsafe_code)]`, `cargo clippy -D warnings`, `clippy::unwrap_used` / `clippy::expect_used` denied outside `#[cfg(test)]`. Bounded retries with jitter; every request has a timeout. `hunt-core` stays pure (no I/O) and is property-tested.
- **TypeScript / Svelte**: sveltesentio §2 rules via the shared ESLint config (complexity 10, ≤ 60 lines per function, `no-direct-time`, `no-unsanitised-html`, `chart-a11y-wrapper`). Svelte 5 runes only. Zod v4 schemas pinned to OpenAPI types with `satisfies`.
- **Accessibility is a gate**: Playwright + axe-core must report zero violations on every route (WCAG 2.2 AA / EN 301 549). Never suppress it with `|| true`.
- **Colour**: oklch only. **Locales**: en + de via Paraglide; every user-facing string goes through messages.

## Don'ts

- No SvelteKit server routes, hooks or `+page.server.ts` — the SPA is static and the API is Go.
- No `latest` image tags, no unpinned `uses:` in workflows.
- Never edit generated code (`api/internal/oas`, `api/internal/gen`, `web/src/lib/api/schema.d.ts`, `worker/crates/snatch-proto/src/gen`) or compiled vendor context files (`CLAUDE.md`, `.cursor/**`, `.windsurfrules`, `.gemini/**`, `.codex/**`, `.github/copilot-instructions.md`); edit `AGENTS.md` and run `praetorctl compile-context`.
- Never store or return *arr API keys in plaintext: encrypt at rest (`core/crypto` AES-GCM), write-only in the API.
- No Swaparr / stalled-download handling — Cleanuparr owns that. Cleanuparr integration is v1.1.

## Local development

```bash
make setup                     # hooks + praetor state
make dev                       # postgres via compose, then api (air), worker (cargo watch), web (vite)
APP_PROFILE=dev                # SQLite + in-process scheduler instead of Postgres + river
```

Reference sources are remote: golusoris, goenvoy, sveltesentio (github.com/golusoris), praetor (github.com/cordanaLLM). Verify a symbol against the remote tree before using it.
