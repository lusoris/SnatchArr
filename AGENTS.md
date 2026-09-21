<!-- markdownlint-disable MD013 MD025 -->
# SnatchArr Agent Operating Harness

Before concluding any turn:

```bash
make verify-all
```

`make verify-all` = `praetorctl audit` + `praetorctl compile-context --verify` + repository tests. All pass -> Ed25519 Exit-0 receipt. Fail -> SARIF diagnostic distillation (<= 1500 tokens).

## Core Directives & Invariants (Modernized NASA JPL Power-of-10)

| Invariant | Scope | NASA Rule | Enforcement Mechanism | Failure Action |
| :--- | :--- | :--- | :--- | :--- |
| **HISS-01** | Control Flow | Rule 1 | Recursion strictly prohibited; call graph must be DAG; zero `goto`. | Immediate build failure |
| **HISS-02** | Loops & I/O | Rule 2 | Scalar upper bound on all loops; explicit `context.Context` timeout on all I/O. | Semgrep / AST error |
| **HISS-03** | Memory | Rule 3 | Zero dynamic heap allocation (`malloc` / `free`) in hot simulation/tick loops. | Allocation audit sweep |
| **HISS-04** | Complexity | Rule 4 | Function length $\le 60$ LOC, McCabe Cyclomatic $\le 10$, Statements $\le 50$. | AST sweep blocker |
| **HISS-07** | Error Handling | Rule 7 | Zero `.unwrap()` / `.expect()`; all errors handled or wrapped with context. | Linter / Compiler error |
| **HISS-08** | Determinism | Rule 8 | Zero dynamic execution (`eval` / `exec`); zero banned unsafe libc (`gets` / `strcpy` / `sprintf`). | AST / Linter error |
| **HISS-09** | Reference Safety | Rule 9 | Mandatory `// SAFETY:` proofs for all pointer arithmetic and `unsafe` blocks. | AST check blocker |
| **HISS-10** | Warning Hygiene | Rule 10 | Zero-warning tolerance across compiler, linter, and format sweeps. | Exit code 1 |
| **HISS-15** | 3D Testing | Rule 5 | Positive, negative, and boundary tests mandatory for all public interfaces. | CI coverage gate |
| **HISS-16** | Context Integrity | Fleet | Single canonical `AGENTS.md`; vendor files compiled via `praetorctl compile-context`. | Pre-commit blocker |

## Operational Rules

1. **Act on verified state.** Read source files, run real commands before hypothesis or edit. Never guess flag names, library signatures, repo configuration from memory.

2. **Lead with output.** Direct answers, diffs, commands. No filler preamble, no "Based on", no restatement, no chatter.

3. **Context transpiler first.** Never edit `CLAUDE.md`, `.cursor/rules/*.mdc`, `.windsurfrules`, `.github/copilot-instructions.md` manually. All agent instruction updates -> `AGENTS.md`, then:

   ```bash
   praetorctl compile-context
   ```

   - `AGENTS.md` = agent-only text -> caveman (internal register). `praetorctl compile-context --verify` + `praetorctl audit` run caveman lint; findings fail gate; no opt-out. Check first: `praetorctl caveman check --kind=context AGENTS.md`.

4. **SARIF diagnostic distillation.** Compiler/linter errors -> distill to $\le 1,500$ tokens ($< 60$ lines): top 3 root-cause failures with file/line pointers; full SARIF logs -> ephemeral storage.

5. **No evasion.** Never attempt `--no-verify`, `LEFTHOOK=0`, or modifying `.git/hooks`. `cordana-standards[bot]` re-checks every pull request in ephemeral isolated sandbox.

6. **Anti-loop interception.** Same AST diff + error category repeats $\ge 3$ times -> halt immediately. Re-evaluate design; no micro-textual retries.

## Text Register

<!-- praetor:register:start -->
Register follows the audience, then the task label of your brief (`register:` in `.standards.yaml`; labels are the router's `target_tasks`).

| Register | Where | Form |
| :--- | :--- | :--- |
| social | forge: issues, PR bodies, review comments, commit bodies | `social-text` skill: BLUF, full sentences, scannable, enough and no more; PR template, receipt fence, conventional commit subject and changelog fragment unchanged |
| docs | docs/, README, ADR bodies | complete without bloat: newcomer path first, expert reference after; every claim points at a file, command or test; no restated code |
| internal | briefs, agent-to-agent traffic, research fan-outs, workflow returns | `caveman` skill: fragments, no filler, verbatim code/paths/errors; facts, paths, commands, verdict |

- Task rows: social = commit_message_synthesis, waiver_signoff; docs = architecture_synthesis, function_docstrings; every other label and any brief without one = internal.
- Evidence above 58 lines or 1500 tokens leaves the message as a file under `.workingdir/evidence/`; return `evidence: <path> sha256:<12 hex> lines:<n>` and fetch it only when a decision needs it.
- An internal return carries verdict, changed paths, commands run, evidence pointers and open questions, nothing else.
<!-- praetor:register:end -->

## Primary Verification Commands

```bash
# Fast local test suite
# Declared commands only; run them before claiming application verification.
'make' 'verify-all'

# Recompile and verify cross-agent context outputs
praetorctl compile-context --verify

# Audit repository against declared HISS standards
praetorctl audit

# Run all formatting, linting, and security gates
make verify-all
```

<!-- praetor:harness:end -->

---

# SnatchArr repository guide

Purpose: hunt missing + cutoff-unmet media across *arr instances; respect per-instance hourly caps. Three components, one contract each:

| Path | Role | Verify |
| :--- | :--- | :--- |
| `api/` | Go 1.27 control plane, golusoris fx. Owns durable state + every "when / how much" decision. Serves OpenAPI 3.1 HTTP, SSE, gRPC `WorkerService`. Embeds built SPA. | `make api-verify` |
| `worker/` | Rust hunt data-plane (tokio + tonic). Stateless: leases runs from api, pages *arr wanted lists, dispatches searches, reports events. | `make worker-verify` |
| `web/` | SvelteKit SPA (adapter-static) on `@sveltesentio/*`. Pure client of Go API; never touches *arr. | `make web-verify`, `make web-e2e` |
| `proto/` | `snatcharr.v1.WorkerService`: only Go<->Rust contract. | `make proto-verify` |
| `deploy/` | Helm chart, kind overlay, ArgoCD, compose. | `make deploy-verify` |

`make verify-all` = all gates. Run before concluding any turn.

## Contracts

- OpenAPI first. Source of truth: `api/openapi/openapi.yaml`. `make api-gen` -> ogen server (`api/internal/build/oas`); `make web-gen` -> `web/src/lib/api/schema.d.ts`. Never hand-edit outputs.
- Generated Go lives under `api/internal/build/` on purpose: praetor HISS scanner has no `// Code generated` exemption but skips any `build` directory segment. `api/internal/gen` (protobuf) stays put; `make proto-gen` adds `// SAFETY:` lines there.
- RFC 9457 everywhere. Every non-2xx HTTP response = `application/problem+json`. SPA `problemMiddleware` depends on that media type.
- gRPC lease model. Worker PULLS: `LeaseRun` -> `Heartbeat` -> `FilterCandidates` -> `AcquireBudget` -> `ReportEvents` -> `CompleteRun`. Api = only writer of processed memory + rate buckets; worker never exceeds granted budget.
- Hourly cap counts items searched, not HTTP requests. Debit per dispatch; warn at 80 %.
- Processed memory: per instance x hunt kind x entity, per-row `expires_at`.
- SSE event names = `snatcharr.v1.EventType` names, lower snake case (`search_dispatched`, ...).
- Configarr instances: read-only in UI except hunt policy; `source=configarr` rows re-import on file change.

## Language rules

- Go: golangci-lint v2, root `.golangci.yml` (HISS-04 caps: cyclomatic <= 10, cognitive <= 15, <= 60 lines / 50 statements per function). gosec zero exclusions; every `#nosec` carries `Gxxx -- <reason>`. `time.Now()` banned outside golusoris `core/clock`. Every I/O call: `context.Context` with deadline. Wrap errors with `%w`. Table-driven tests: positive, negative, boundary.
- Rust: `#![forbid(unsafe_code)]`, `cargo clippy -D warnings`, `clippy::unwrap_used` / `clippy::expect_used` denied outside `#[cfg(test)]`. Bounded retries with jitter; every request has timeout. `hunt-core` stays pure (no I/O), property-tested.
- TypeScript / Svelte: sveltesentio section 2 rules via shared ESLint config (complexity 10, <= 60 lines per function, `no-direct-time`, `no-unsanitised-html`, `chart-a11y-wrapper`). Svelte 5 runes only. Zod v4 schemas pinned to OpenAPI types with `satisfies`.
- Accessibility = gate: Playwright + axe-core, zero violations on every route (WCAG 2.2 AA / EN 301 549). Never suppress with `|| true`.
- Colour: oklch only. Locales: en + de via Paraglide; every user-facing string through messages.

## Don'ts

- No SvelteKit server routes, hooks, `+page.server.ts`. SPA static; API = Go.
- No `latest` image tags; no unpinned `uses:` in workflows.
- Never edit generated code (`api/internal/build/oas`, `api/internal/gen`, `api/internal/store/sqlcgen`, `web/src/lib/api/schema.d.ts`, `worker/crates/snatch-proto/src/gen`) or compiled vendor context (`CLAUDE.md`, `.cursor/**`, `.windsurfrules`, `.gemini/**`, `.codex/**`, `.github/copilot-instructions.md`). Edit `AGENTS.md`, run `praetorctl compile-context`.
- Never store or return *arr API keys in plaintext: encrypt at rest (`core/crypto` AES-GCM); write-only in API.
- No Swaparr / stalled-download handling: Cleanuparr owns that. Cleanuparr integration = v1.1.

## Local development

```bash
make setup                     # hooks + praetor state
make dev                       # postgres via compose, then api (air), worker (cargo watch), web (vite)
APP_PROFILE=dev                # SQLite + in-process scheduler instead of Postgres + river
```

Reference sources: remote only. golusoris, goenvoy, sveltesentio (github.com/golusoris); praetor (github.com/cordanaLLM). Verify symbol against remote tree before use.
