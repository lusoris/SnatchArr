<!-- markdownlint-capture -->
<!-- markdownlint-disable MD013 -->
# lusoris/SnatchArr Agent Operating Harness

<!-- praetor:head -->

Before concluding any turn:

```bash
make verify-all
```

`make verify-all` = repository-owned gate; adoption kept it unread + unexecuted. Steps live in `Makefile`: read there, never restate. Footer commands = project markers only.
Pass = exit 0. Signed Ed25519 Exit-0 receipt only from `praetorctl gate run`; report no receipt it did not mint. Fail -> SARIF diagnostic distillation (<= 1500 tokens).

## Core Directives & Invariants

Adopted check = coverage state + catalog path; else audit scanner; else `unknown`. Gates = (none: no generated `make verify-all`, no praetor `lefthook.yml`); edits not reflected.

| Invariant | Rule | Adopted check | On fail |
| :--- | :--- | :--- | :--- |
| **HISS-01** control flow | recursion prohibited; call graph = DAG; Go: zero `goto` | `praetorctl audit` HISS scan: Go `goto`, recursion + plain-function call cycles; Rust, Python direct recursion; C `goto` | advisory |
| **HISS-02** loops, I/O | scalar upper bound on every loop; explicit deadline on every I/O call; Go: I/O takes `context.Context` deadline | `praetorctl audit` HISS scan: unbounded loop shapes in Go, C, Rust, Python; I/O deadlines unchecked | advisory |
| **HISS-03** memory | Go, Rust: zero heap allocation in hot simulation/tick loops | unknown | advisory |
| **HISS-04** complexity | McCabe cyclomatic <= 12, cognitive <= 15, statements <= 55; func LOC <= 60 (audit ceiling) | `praetorctl audit` HISS scan: function length only; other caps need repository linter | advisory |
| **HISS-05** scoping | declare every identifier in smallest lexical scope serving it | unknown | advisory |
| **HISS-06** concurrency | explicit scalar upper bound on every worker pool + concurrent fan-out | unknown | advisory |
| **HISS-07** errors | every error handled or wrapped with context; Go: zero unchecked `error` return; Rust: zero `.unwrap()` / `.expect()` outside tests | `praetorctl audit` HISS scan: partial in Go, Rust, Python | advisory |
| **HISS-08** determinism | zero dynamic code execution (`eval` / `exec`); a process signal's target is bounded above 1 (`killpg`, `kill(-pid)`, Go `syscall.Kill`) | `praetorctl audit` HISS scan: C banned calls, Python `eval` / `exec`, Go `syscall.Kill` / `unix.Kill` | advisory |
| **HISS-09** reference safety | Go, Rust: `// SAFETY:` proof before every `unsafe` block | `praetorctl audit` HISS scan: Go, Rust `unsafe` without proof; C, Python unchecked | advisory |
| **HISS-10** warnings | zero warnings: compiler, linter, format sweeps | unknown | advisory |
| **HISS-11** supply chain | pinned lockfiles; zero floating tags; signed provenance | unknown | advisory |
| **HISS-12** secrets | zero credentials in Git history | unknown | advisory |
| **HISS-13** debt ratchet | recorded infractions never grow vs committed baseline | `praetorctl audit` ratchet vs `.standards-baseline.json` | advisory |
| **HISS-14** append-only ABI | public API append-only; breaking change = `!` subject + `Migration:` footer | unknown | advisory |
| **HISS-15** 3D testing | positive + negative + boundary tests, every public interface | unknown | advisory |
| **HISS-16** context integrity | single canonical `AGENTS.md`; vendor files compiled via `praetorctl compile-context` | unknown | advisory |
| **HISS-17** state ledger | turn start `praetorctl state status`; turn end `praetorctl state sync .` | unknown | advisory |
| **HISS-18** CI efficiency | diff-aware gating via `praetorctl ci filter` | unknown | advisory |
| **HISS-19** reuse before writing | one behavior = one implementation; extend or call existing code | unknown | advisory |
| **HISS-20** replayable evidence | every enforcement claim backed by fixtures replayed both directions | unknown | advisory |
| **HISS-21** platform neutrality | gates, hooks, emitted templates run on Linux, macOS, Windows, or skip with stated reason | unknown | advisory |

## Operational Rules

1. **Act on verified state.** Read source files, run real commands before hypothesis or edit. Never guess flag names, library signatures, repo configuration from memory.

2. **Lead with output.** Direct answers, diffs, commands. No filler preamble, no "Based on", no restatement, no chatter.

3. **Context transpiler first.** Never edit `CLAUDE.md`, `.gemini/GEMINI.md`, `.codex/rules.md` manually. All agent instruction updates -> `AGENTS.md`, then:

   ```bash
   praetorctl compile-context
   ```

   - `AGENTS.md` = agent-only text -> caveman (internal register). `praetorctl compile-context --verify` + `praetorctl audit` run caveman lint; findings fail gate; no opt-out. Check first: `praetorctl caveman check --kind=context AGENTS.md`.

4. **SARIF diagnostic distillation.** Compiler/linter errors -> distill to $\le 1,500$ tokens ($< 60$ lines): top 3 root-cause failures with file/line pointers; full SARIF logs -> ephemeral storage.

5. **No evasion.** Never attempt `--no-verify`, `LEFTHOOK=0`, or modifying `.git/hooks`. Adoption installed no hooks here (`lefthook.yml` not praetor-written, or `git-hooks` declined). Adoption adds no server-side `praetorctl` gate run. Scaffolded CI: `.github/workflows/praetor-api.yml` runs step `Stop on a draft pull request`, `go run tools/apicompat/gate/main.go -base="$BASE"`.

6. **Anti-loop interception.** Same AST diff + error category repeats $\ge 3$ times -> halt immediately. Re-evaluate design; no micro-textual retries.

<!-- praetor:config -->

## Text Register

<!-- praetor:register:start -->
Register follows the audience, then the task label of your brief (`register:` in `.standards.yaml`; labels are the router's `target_tasks`).

| Register | Where | Form |
| :--- | :--- | :--- |
| social | forge: issues, PR bodies, review comments, commit bodies | `social-text` skill: BLUF, full sentences, scannable, enough and no more; conventional commit subject unchanged |
| docs | docs/, README, ADR bodies | complete without bloat: newcomer path first, expert reference after; every claim points at a file, command or test; no restated code |
| internal | briefs, agent-to-agent traffic, research fan-outs, workflow returns | `caveman` skill: fragments, no filler, verbatim code/paths/errors; facts, paths, commands, verdict |

- Task rows: social = commit_message_synthesis, waiver_signoff; docs = architecture_synthesis, function_docstrings; every other label and any unlabeled text = internal. Subagent launch brief: `caveman` brief shape with `task:` = routing label.
- Evidence above 58 lines or 1500 tokens leaves the message as a file under `.workingdir/evidence/`; return `evidence: <path> sha256:<12 hex> lines:<n>` and fetch it only when a decision needs it.
- An internal return carries verdict, changed paths, commands run, evidence pointers and open questions, nothing else.
<!-- praetor:register:end -->

<!-- praetor:tail -->

## Primary Verification Commands

```bash
# Fast local test suite
# Declared commands only; run them before claiming application verification.
'go' 'build' '-v' './...'
'cargo' 'build' '--locked'
'go' 'test' '-v' '-race' './...'
'cargo' 'test' '--locked'

# Recompile and verify cross-agent context outputs
praetorctl compile-context --verify

# Audit repository against declared HISS standards
praetorctl audit

# Repository gate; steps live in Makefile
make verify-all
```

<!-- markdownlint-restore -->
<!-- markdownlint-disable MD025 -->
<!-- praetor:harness:end -->

---

# SnatchArr repository guide

Purpose: snatch missing + cutoff-unmet media across *arr instances; respect per-instance hourly caps. Three components, one contract each:

| Path | Role | Verify |
| :--- | :--- | :--- |
| `api/` | Go 1.27 control plane, golusoris fx. Owns durable state + every "when / how much" decision. Serves OpenAPI 3.1 HTTP, SSE, gRPC `WorkerService`. Embeds built SPA. | `make api-verify` |
| `worker/` | Rust snatch data-plane (tokio + tonic). Stateless: leases runs from api, pages *arr wanted lists, dispatches searches, reports events. | `make worker-verify` |
| `web/` | SvelteKit SPA (adapter-static) on `@sveltesentio/*`. Pure client of Go API; never touches *arr. | `make web-verify`, `make web-e2e` |
| `proto/` | `snatcharr.v1.WorkerService`: only Go<->Rust contract. | `make proto-verify` |
| `deploy/` | Helm chart, kind overlay, ArgoCD, compose. | `make deploy-verify` |

`make verify-all` = all gates. Run before concluding any turn.

## Contracts

- OpenAPI first. Source of truth: `api/openapi/openapi.yaml`. `make api-gen` -> ogen server (`api/internal/build/oas`); `make web-gen` -> `web/src/lib/api/schema.d.ts`. Never hand-edit outputs.
- Generated Go lives under `api/internal/build/` on purpose: praetor HISS scanner has no `// Code generated` exemption but skips any `build` directory segment. `api/internal/gen` (protobuf) stays put; `make proto-gen` adds `// SAFETY:` lines there.
- RFC 9457 everywhere. Every non-2xx HTTP response = `application/problem+json`. SPA `problemMiddleware` depends on that media type.
- gRPC lease model. Worker PULLS: `LeaseRun` -> `Heartbeat` -> `FilterCandidates` -> `AcquireBudget` -> `ReportEvents` -> `CompleteRun`. Api = only writer of processed memory + rate buckets; worker never exceeds granted budget.
- Hourly cap counts searches *arr sends per indexer, not HTTP requests, not items: 1 per episode / season / movie / album / book; artist or author search = 1 (#180). Debit per dispatch; warn at 80 %.
- Processed memory: per instance x snatch kind x entity, per-row `expires_at`.
- SSE event names = `snatcharr.v1.EventType` names, lower snake case (`search_dispatched`, ...).
- Periodic loops (planner tick, Seerr sync, Configarr poll) never start from their own module: register in fx group `leader_loops` via `gate.Provide[*T]()`; golusoris leader/gate runs them on the elected replica (`APP_LEADER_ENABLED`, pg advisory lock).
- Configarr instances: read-only in UI except snatch policy; `source=configarr` rows re-import on file change.

## Language rules

- Go: golangci-lint v2, root `.golangci.yml` (HISS-04 caps: cyclomatic <= 10, cognitive <= 15, <= 60 lines / 50 statements per function). gosec zero exclusions; every `#nosec` carries `Gxxx -- <reason>`. `time.Now()` banned outside golusoris `core/clock`. Every I/O call: `context.Context` with deadline. Wrap errors with `%w`. Table-driven tests: positive, negative, boundary.
- Go under `tools/`: same gate as `api/` (`make tools-verify`, part of `make api-verify`); runs without + with build tag `apicompatgate`. Hand-written per-file exclusion in `.golangci.yml`: none. Finding in file not editable here -> entry in generated block of `.golangci.yml` (`praetorctl linters exceptions generate`, category A, issue = cordanaLLM/praetor#N); never hand-edit block; `tools/ci/lint_exceptions.sh` fails on new finding, expired entry, stale entry. `tools/apicompat/gate/main.go` = locked praetor asset: never edit.
- Scanners (CI jobs `secret-scan`, `semgrep`, `trivy`; scripts `tools/ci/*_scan.py` replay planted case first): gitleaks over full history; exception = fingerprint in `.gitleaksignore` under `# expires YYYY-MM-DD: <reason>`. Semgrep rules = `.semgrep.yml` only, one fixture per rule in `tools/ci/semgrep/testdata`; fix finding at source, generated code via its generator. Trivy: HIGH/CRITICAL with fix fails; no `.trivyignore`.
- Rust: `#![forbid(unsafe_code)]`, `cargo clippy -D warnings`, `clippy::unwrap_used` / `clippy::expect_used` denied outside `#[cfg(test)]`. Bounded retries with jitter; every request has timeout. `snatch-core` stays pure (no I/O), property-tested.
- TypeScript / Svelte: sveltesentio section 2 rules via shared ESLint config (complexity 10, <= 60 lines per function, `no-direct-time`, `no-unsanitised-html`, `chart-a11y-wrapper`). Svelte 5 runes only. Zod v4 schemas pinned to OpenAPI types with `satisfies`.
- Accessibility = gate: Playwright + axe-core, zero violations on every route (WCAG 2.2 AA / EN 301 549). Never suppress with `|| true`.
- Colour: oklch only. Locales: en + de via Paraglide; every user-facing string through messages.

## Don'ts

- No SvelteKit server routes, hooks, `+page.server.ts`. SPA static; API = Go.
- No `latest` image tags; no unpinned `uses:` in workflows.
- Never edit generated code (`api/internal/build/oas`, `api/internal/gen`, `api/internal/store/sqlcgen`, `web/src/lib/api/schema.d.ts`, `worker/crates/snatch-proto/src/gen`) or compiled vendor context (`CLAUDE.md`, `.gemini/**`, `.codex/**`). Edit `AGENTS.md`, run `praetorctl compile-context`.
- Never store or return *arr API keys in plaintext: encrypt at rest (`core/crypto` AES-GCM); write-only in API.
- Queue management (stalled, slow, failed, import-blocked, unwanted) acts only
  through the *arr queue API, observe mode first (ADR-0012, #362); never call a
  download client directly (ADR-0006), except per-item priority, queue-order,
  pause and resume writes, off by default and gated by `priority_mode=act`
  (ADR-0046). Observe exception: resume of items whose ownership record says
  SnatchArr paused them, after act or attribution withdrawn (ADR-0046 sections
  2, 4).

## Local development

```bash
make setup                     # hooks + praetor state
make dev                       # postgres via compose, then api (air), worker (cargo watch), web (vite)
APP_SNATCHARR_PROFILE=dev      # relaxed cookies + built-in dev secrets; still Postgres
```

Reference sources: remote only. golusoris, goenvoy, sveltesentio (github.com/golusoris); praetor (github.com/cordanaLLM). Verify symbol against remote tree before use.
