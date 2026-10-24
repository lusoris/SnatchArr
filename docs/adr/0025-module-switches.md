<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0025: Architecture and mechanism of optional module switches

- Status: proposed
- Date: 2026-10-24
- Related: [ADR-0018](0018-product-front-door.md), [ADR-0021](0021-llm-access-module.md), [ADR-0024](0024-watch-aware-modes.md)

## Context

SnatchArr operates as the unified front door of the media stack ([ADR-0018](0018-product-front-door.md)). ADR-0018 establishes a modular product architecture where functional domains beyond the core search and queue scheduler—such as the LLM access module ([ADR-0021](0021-llm-access-module.md)), watch-aware modes ([ADR-0024](0024-watch-aware-modes.md)), request portal extensions, and the adult media library—are designed as toggleable modules. ADR-0018 mandates that optional modules are disabled by default and maintain hard boundaries across Go packages, OpenAPI endpoints, database tables, and user interfaces (`napp-shape-20261020`).

Issue [#563](https://github.com/lusoris/SnatchArr/issues/563) requires a single, uniform switch mechanism for every optional module. Before introducing new modules, the architecture of registration, state storage, HTTP routing, background worker loops, database migrations, Model Context Protocol (MCP) tool exposure, and frontend navigation must be unified.

### Current implementation baseline

The existing codebase contains exactly one optional switch prototype: the `llm_enabled` setting introduced for the LLM access module ([ADR-0021](0021-llm-access-module.md)). A comprehensive survey of the current code reveals how module state is handled today:

1. **Storage and schema**:
   - The boolean switch is stored as a column on the singleton `settings` table: `llm_enabled BOOLEAN NOT NULL DEFAULT FALSE` (`api/internal/store/migrations/postgres/000024_llm_module.up.sql:4`).
   - SQL queries read and write the column directly: `api/internal/store/queries/settings.sql:19`, with generated code in `api/internal/store/sqlcgen/settings.sql.go:16,38,67`.
   - Domain representation resides in `domain.Settings.LLMEnabled` (`api/internal/domain/settings.go:118`).
   - `settings.Service` reads the setting via `Get` (`api/internal/settings/service.go:35-41`) and updates it via `Update` (`api/internal/settings/service.go:44-78`).
2. **REST API exposure**:
   - The setting is defined on the OpenAPI `Settings` object (`api/openapi/openapi.yaml:2535`) and converted in `api/internal/httpapi/convert.go:215,232`. Generated models reflect `LlmEnabled OptBool` in `api/internal/build/oas/oas_schemas_gen.go:7255`.
3. **Endpoint gating and refusal**:
   - The MCP HTTP endpoint `/api/v1/mcp` is mounted in Chi: `api/internal/llm/llm.go:101-103`.
   - On every request, `Service.Handler` checks `s.loadEnabledSettings(r.Context())` (`api/internal/llm/llm.go:108-112,137-148`).
   - If `!cfg.LLMEnabled`, it returns `fmt.Errorf("%w: llm module is disabled", domain.ErrNotFound)`.
   - The centralized error handler `httpapi.ErrorHandler` (`api/internal/httpapi/problem.go:45-48`) resolves `domain.ErrNotFound` via `ogenkit.WithHTTPStatus(domain.ErrNotFound, http.StatusNotFound, "")` (`api/internal/httpapi/problem.go:34`), returning an RFC 9457 HTTP 404 Problem JSON payload (`application/problem+json`).
4. **Application dependency graph**:
   - Subsystems are statically assembled into Uber fx: `app.Module` (`api/internal/app/app.go:61-110`) bundles all packages (`llm.Module` at line 97, `gate.Module` at line 99).
   - Route mounting occurs in `mountHTTP` (`api/internal/app/app.go:162-187`), where `llm.Mount` attaches at line 182 and `httpapi.Mount` attaches the generated OpenAPI server at line 183.
5. **Periodic leader loops**:
   - `AGENTS.md` Contracts line 129 specifies: `Periodic loops (planner tick, Seerr sync, Configarr poll) never start from their own module: register in fx group leader_loops via gate.Provide[*T]()`.
   - Implementations wrap `gate.Periodic` and register via fx: `api/internal/instances/module.go:35`, `api/internal/seerr/module.go:43`, `api/internal/queue/module.go:42`.
   - In upstream `golusoris` (`github.com/golusoris/golusoris@v0.20.0/leader/gate/gate.go:61-100,105-123`), `gate.Gate` collects the `leader_loops` group and starts all loops via `startAll` upon acquiring leadership, stopping them via `stopAll` upon lease expiration.
6. **Database migrations**:
   - Migrations are embedded from `migrations/postgres/*.sql` via `store.MigrationsFS()` (`api/internal/store/store.go:28-39`).
   - `app.embedMigrations` (`api/internal/app/migrate.go:18-26`) configures `dbmigrate.Options` with `o.Auto = true`, running all migrations sequentially at container boot.
7. **Frontend navigation**:
   - Navigation links are statically declared in TypeScript: `web/src/lib/components/shell/nav.ts:37-75` exports `getNavGroups(isAdmin: boolean)` returning hard-coded `AppRoute` entries (`web/src/lib/components/shell/nav.ts:7-20`), consumed by `web/src/routes/+layout.svelte:39`.
   - Navigation and routes are entirely unaware of whether an optional backend module is active.

### Problem statement

Expanding to multiple optional modules under this baseline creates three failure modes:

1. **Schema bloat**: Adding dedicated columns (`watchaware_enabled`, `requests_enabled`, `adult_enabled`) to the `settings` table causes perpetual schema churn on the singleton configuration row.
2. **Inconsistent route gating**: Hand-writing gating checks inside each HTTP handler risks ungated route leaks where an author forgets the `if !cfg.Enabled` check.
3. **Ghost UI surfaces**: Users see navigation links and dashboard widgets for modules that are disabled or unconfigured on the backend, leading to broken views.

## Decision

SnatchArr establishes a unified module switch mechanism governed by the architectural choices detailed below.

### 1. Module registration and descriptors

Each optional module package exports a declarative module descriptor registered in a central registry in the Go control plane (`api/internal/modules`).

- **Descriptor schema**:

  ```go
  type Descriptor struct {
      ID          string            // e.g. "llm", "watchaware", "requests", "adult"
      DisplayName string            // e.g. "LLM Access", "Watch-Aware Modes"
      Default     bool              // default enabled state (false for all optional modules)
      Operations  []string          // OpenAPI operation IDs owned by this module
      Tools       []string          // MCP tool names exposed by this module
      NavRoute    string            // SPA route prefix (e.g. "/watchaware")
      NavGroup    string            // SPA nav group (e.g. "integrations")
  }
  ```

- **Options considered**:
  - *Option A: Static compile-time registry in Go* (Recommended). Each module exports a descriptor; `api/internal/modules` aggregates them into an immutable registry validated during unit testing.
    - *Trade-offs*: Fully type-safe; zero reflection or dynamic loading risks (HISS-08); enables compile-time and static test verification. Adding a module requires registering its descriptor in the control plane assembly.
  - *Option B: Go plugin architecture (`plugin.Open`)*.
    - *Trade-offs*: Enables dynamic shared library loading at runtime. Highly fragile in Go across toolchain versions, incompatible with musl/distroless static compilation, and introduces severe memory safety risks.
  - *Option C: Decentralized fx group tags without a centralized descriptor*.
    - *Trade-offs*: Loose coupling, but prevents static analysis, OpenAPI consistency checks, and unified administrative querying.
- **Recommendation**: **Option A**. Modules declare their static metadata in Go, assembled into `api/internal/modules.Registry`.

### 2. Switch storage and migration

Module states are decoupled from the core `settings` singleton table into a dedicated module state table.

- **Data model**:

  ```sql
  CREATE TABLE modules (
      id VARCHAR(64) PRIMARY KEY,
      enabled BOOLEAN NOT NULL DEFAULT FALSE,
      config JSONB NOT NULL DEFAULT '{}'::jsonb,
      updated_at TIMESTAMPTZ NOT NULL
  );
  ```

- **Migration path**:
  - Migration `000028_modules.up.sql` creates the `modules` table and seeds registered modules with their defaults.
  - Data migration: `INSERT INTO modules (id, enabled, updated_at) SELECT 'llm', llm_enabled, updated_at FROM settings ON CONFLICT (id) DO UPDATE SET enabled = EXCLUDED.enabled;`.
  - Compatibility: To avoid breaking existing clients during migration, `domain.Settings` continues to mirror `llm_enabled` through a virtual getter/setter backed by the `modules` table until a planned deprecation cycle.
- **Options considered**:
  - *Option A: Dedicated `modules` table with JSONB configuration* (Recommended).
    - *Trade-offs*: Schema changes are zero when introducing new modules. Per-module settings live alongside the switch. Clear audit trail per module.
  - *Option B: Individual boolean columns on `settings` table (`llm_enabled`, `watchaware_enabled`, ...)*.
    - *Trade-offs*: Requires a database migration for every future module. Violates package isolation boundaries by coupling the core settings table to all optional features.
  - *Option C: Generic key-value store (`settings_kv`)*.
    - *Trade-offs*: Unstructured, lacks schema validation, and loses transactional integrity with relational queries.
- **Recommendation**: **Option A**. A dedicated `modules` table provides an append-only, decoupled model for current and future modules.

### 3. API gating and OpenAPI operation marking

Every HTTP route belonging to an optional module must automatically return RFC 9457 HTTP 404 Problem JSON when the module is disabled, hiding the module's presence from unprivileged discovery (per [#563](https://github.com/lusoris/SnatchArr/issues/563)).

- **OpenAPI contract marking**:

  In `api/openapi/openapi.yaml`, every operation belonging to an optional module declares the vendor extension `x-module`:

  ```yaml
  /watchaware/sources:
    get:
      operationId: listWatchSources
      x-module: watchaware
      summary: List configured watch-state sources
      tags:
        - watchaware
      responses:
        '200': ...
  ```

- **Enforcement mechanism**:

  SnatchArr's ogen server configuration (`api/internal/httpapi/module.go:38`) attaches an ogen middleware via `oas.WithMiddleware`:

  ```go
  func ModuleGateMiddleware(reg *modules.Registry, store *modules.Store) oas.Middleware {
      return func(req middleware.Request, next func(req middleware.Request) (middleware.Response, error)) (middleware.Response, error) {
          if modID, ok := reg.ModuleForOperation(req.OperationID); ok {
              if !store.IsEnabled(req.Context, modID) {
                  return nil, fmt.Errorf("%w: module %s is disabled", domain.ErrNotFound, modID)
              }
          }
          return next(req)
      }
  }
  ```

  Because ogen passes middleware errors directly to `s.cfg.ErrorHandler` (`api/internal/build/oas/oas_handlers_gen.go:191-225`), `httpapi.ErrorHandler` catches `domain.ErrNotFound` and formats it as an RFC 9457 HTTP 404 response (`application/problem+json`).
- **Options considered**:
  - *Option A: OpenAPI `x-module` vendor extension with ogen middleware* (Recommended).
    - *Trade-offs*: Declarative and centralized. Middleware intercepts by `req.OperationID` before any handler code executes. Zero boilerplate in handlers.
  - *Option B: Chi router middleware matching URL path prefixes*.
    - *Trade-offs*: Path prefixes do not always align cleanly with module ownership. URL routing rules become fragmented between Chi and OpenAPI.
  - *Option C: Manual check inside each handler method* (status quo in `llm.go:144`).
    - *Trade-offs*: Error-prone; depends on developer discipline; impossible to verify completely without brittle code-scanning linters.
- **Recommendation**: **Option A**. Vendor extension `x-module` in `openapi.yaml` combined with ogen middleware.

### 4. Leader loops lifecycle

Background worker tasks of optional modules (such as watch-state synchronization in [ADR-0024](0024-watch-aware-modes.md)) must not perform work when their module is switched off.

- **Verified upstream semantics**:
  Inspection of `golusoris` (`github.com/golusoris/golusoris@v0.20.0/leader/gate/gate.go:61-100,105-123`) verifies that `gate.Gate` operates on an immutable slice of `Loop` instances collected at startup from fx group `leader_loops`. `Gate` calls `startAll` when this replica acquires the Postgres advisory lock and `stopAll` when leadership is lost. Furthermore, `gate.Periodic.Start` (`github.com/golusoris/golusoris@v0.20.0/leader/gate/periodic.go:51-56`) explicitly refuses a second start while running. `gate.Gate` has no built-in API for dynamic loop registration or selective deregistration during an active leadership term.
- **Switch execution behavior**:
  - Module leader loops register in fx group `leader_loops` as standard `gate.Periodic` instances via `gate.Provide[*T]()`.
  - At the start of every periodic tick, the loop's tick handler inspects the module enabled cache.
  - When disabled, the tick function logs a debug message and returns immediately with `nil` without performing any database lookups, remote network I/O, or search budget debits.
  - **No restart required**: When an administrator toggles a module switch on or off via the settings UI, the loop transitions state on the very next tick without requiring a container restart.
- **Options considered**:
  - *Option A: Quiescent ticking with in-memory module enabled check* (Recommended).
    - *Trade-offs*: Seamless integration with `golusoris` `gate.Gate`; instant runtime reaction to switch toggles without restarting the container; preserves Kubernetes readiness and statuspage probe semantics (`Gate.Check`); CPU cost of an idle timer tick is sub-microsecond.
  - *Option B: Dynamic goroutine lifecycle via `events.Bus` outside `gate.Gate`*.
    - *Trade-offs*: Removes the idle timer, but bypasses the cluster-wide leadership lease lock, risking concurrent executions across multiple replicas.
  - *Option C: Boot-time conditional registration in fx*.
    - *Trade-offs*: Loops are omitted from the fx graph if disabled at start; however, enabling or disabling a module requires a mandatory process restart.
- **Recommendation**: **Option A**. Loops remain registered with `gate.Gate` and execute as quiescent no-ops while their module switch is off.

### 5. Database migrations: startup vs on-enable

Optional modules introduce dedicated database tables (for example, `watchaware_sources` and `watchaware_item_watches` in [ADR-0024](0024-watch-aware-modes.md)).

- **Execution policy**:
  All database migrations across all modules execute at application boot during standard startup migration processing (`api/internal/app/migrate.go:18-26`). When an optional module is disabled, its database tables exist in Postgres but remain entirely empty and quiescent.
- **Downgrade and rollback consequences**:
  Executing migrations linearly at startup guarantees that database schema versions (`schema_migrations`) remain single-track and deterministic. Reverting a release with `migrate down` rolls back migrations in exact reverse sequence.
  Conversely, attempting "migrate-on-enable" (running DDL dynamically when a switch is toggled in the UI) creates unacceptable operational risks:
  1. Concurrent DDL races when multiple API replicas run.
  2. Extended schema locks during user HTTP requests.
  3. Intractable downgrade semantics: toggling a switch off cannot safely execute `DROP TABLE` without destroying user data, rendering dynamic migration cleanup illusory.
- **Options considered**:
  - *Option A: Unified linear startup migrations* (Recommended).
    - *Trade-offs*: Standard `golang-migrate` workflow; zero runtime DDL; safe multi-replica startups; full compatibility with static `sqlc` query generation. Small storage overhead for empty table definitions in Postgres catalog.
  - *Option B: Migrate on module enable*.
    - *Trade-offs*: Avoids unused tables when a feature is never used, but introduces DDL execution inside HTTP request handlers, catalog locking risks, migration failure recovery complications, and non-linear schema history.
- **Recommendation**: **Option A**. All migrations execute at startup; disabled module tables remain dormant.

### 6. Single-Page Application (SPA) integration

The web interface must dynamically adapt its navigation sidebar, command palette, and route guards based on active modules without hard-coding module lists in the frontend client.

- **Dynamic configuration read**:
  The SPA layout fetches active module identifiers from the backend session or system configuration endpoint (`GET /api/v1/system/modules` or as part of `GET /api/v1/auth/session`).
- **Navigation filtering**:

  `web/src/lib/components/shell/nav.ts` is enhanced to associate `NavItem` definitions with an optional `module?: string` tag:

  ```typescript
  export interface NavItem {
      href: AppRoute;
      label: string;
      icon: string;
      module?: string;
  }
  ```

  `getNavGroups(isAdmin: boolean, activeModules: Set<string>): NavGroup[]` filters out any navigation item whose declared module is not present in `activeModules`.
- **Client route guards**:
  If a user manually navigates to a URL belonging to a disabled module (e.g. `/watchaware`), the SvelteKit client-side router intercepts the navigation and renders an RFC 9457-aligned 404 view rather than attempting API calls that will fail.
- **Options considered**:
  - *Option A: API-driven module filtering with SvelteKit route guards* (Recommended).
    - *Trade-offs*: Centralized authority in the API; dynamic UI updates immediately after saving settings; clean user experience with no ghost routes.
  - *Option B: Static feature flags in web environment variables*.
    - *Trade-offs*: Requires rebuilding or restarting web containers to change module availability; violates runtime toggle requirements.
- **Recommendation**: **Option A**. The API is the single source of truth for active modules; the SPA dynamically filters navigation and guards routes.

### 7. Model Context Protocol (MCP) tool governance

In accordance with ADR-0021 ([docs/adr/0021-llm-access-module.md](0021-llm-access-module.md)), language models connecting via `/api/v1/mcp` must only discover and invoke tools belonging to active modules.

- **Tool catalog tagging**:
  In `api/internal/llm/tools.go`, each `toolSpec` includes an optional `Module string` field matching the module's registered ID.
- **Dynamic discovery and invocation fencing**:
  1. **Discovery (`tools/list`)**: `registerTools` (`api/internal/llm/tools.go:1292-1312`) checks module enablement. Any tool whose associated module is disabled is excluded from the returned tool list.
  2. **Execution (`tools/call`)**: `checkToolCallFences` (`api/internal/llm/llm.go:225-255`) inspects incoming tool execution calls. If a tool belongs to a disabled module, the invocation is rejected immediately with a new JSON-RPC error code `-32022` ("module disabled"), distinct from the existing `ErrCodeToolNotEnabled` (`-32021`, write tool not opted in, `api/internal/llm/llm.go:35`), and a security audit warning is emitted.
- **Options considered**:
  - *Option A: Unified MCP server with dynamic tool filtering* (Recommended).
    - *Trade-offs*: Preserves a single `/api/v1/mcp` endpoint and single bearer token authentication model while cleanly hiding disabled tool surfaces.
  - *Option B: Separate MCP endpoints per module*.
    - *Trade-offs*: Forces connecting agents to manage multiple MCP server configurations, URL endpoints, and connection lifecycles.
- **Recommendation**: **Option A**. Single MCP endpoint with dynamic tool discovery and call fencing.

### 8. Testing requirements and repository verification

The module switch implementation must be verified with three-dimensional testing (HISS-15) and a dedicated repository gate check:

1. **Test dimensions**:
   - **Positive**: When a module is enabled, its OpenAPI endpoints return `200 OK`, its leader loop executes scheduled work, its MCP tools are present in `tools/list`, and its nav item appears in the SPA sidebar.
   - **Negative**: When a module is disabled, every owned endpoint returns HTTP 404 with RFC 9457 `application/problem+json`, its leader loop ticks as a silent no-op without database or network I/O, its MCP tools are absent from `tools/list`, direct `tools/call` invocations return `-32022`, and its nav item is absent from the DOM.
   - **Boundary**: Switching a module from off to on and back to off at runtime without restarting the API process immediately toggles route availability and tool visibility across consecutive requests.
   - **Planted defect**: A test case where a newly added route omits the `x-module` annotation or where an operation is assigned an unregistered module ID must fail the verification gate.
2. **Automated repository gate check**:
   A dedicated check `tools/moduleroutes` (integrated into `make api-verify`) reads `api/openapi/openapi.yaml` and asserts:
   - Every operation tagged with a non-core tag carries a valid `x-module` extension matching a registered module in `api/internal/modules`.
   - Every operation declaring `x-module` is bound to the `ModuleGateMiddleware` test suite.

## Consequences

### Positive

- **Predictable boundary enforcement**: Developers cannot accidentally leak an active route for a disabled module because gating is enforced centrally at the OpenAPI middleware layer via `x-module`.
- **Zero schema churn on settings**: Adding future optional modules (requests, adult content) requires zero structural modifications to the singleton `settings` table.
- **Consistent agent boundaries**: Language model agents discover only the capabilities of active modules, preventing hallucinated or failing tool invocations.
- **Instant operational toggling**: Operators can turn modules on or off live in the web settings UI without container restarts, service downtime, or leadership thrashing.

### Negative

- **Slight request overhead**: The ogen router evaluates the module gate middleware on every incoming request. With in-memory atomic caching, this overhead is measured in nanoseconds.
- **Quiescent database tables**: Postgres holds dormant table schemas for disabled modules. Since empty relational tables consume minimal disk space (a few disk pages for metadata), this trade-off strongly favors operational stability over dynamic DDL complexity.

## Follow-ups

Implementation of this architecture proceeds in five discrete pull requests:

- [x] **Slice 1: Module registry and storage**: Introduce `api/internal/modules`, database table `modules`, migration `000028_modules` (owned by ADR-0025 Slice 1, ranking before ADR-0024 Slice 1), config validation per module in Go, `llm_enabled` data migration, system settings API endpoints, and a boundary test verifying off-then-on returns the same rows (#604, #605).
- [x] **Slice 2: OpenAPI gating middleware and repository check**: Implement `x-module` validation, ogen `ModuleGateMiddleware`, and `check_module_routes` CI verification (#606).
- [x] **Slice 3: MCP dynamic tool gating**: Add `Module` field to `toolSpec` in `api/internal/llm/tools.go` and implement dynamic filtering in `tools/list` and `tools/call` (#619).
- [ ] **Slice 4: SPA dynamic navigation and route guards**: Update `nav.ts` and SvelteKit router layout to filter links and guard routes based on active module states.
- [x] **Slice 5: Watch-aware module migration**: Adopt the ADR-0025 switch architecture as the foundation for watch-aware modes ([ADR-0024](0024-watch-aware-modes.md) Slice 1) (this PR).

## Open Questions

1. **Storage model for module-specific configuration (`snatcharr-adr25-module-config-storage`)**:
   - *Options*: a) Store module-specific sub-configurations inside `modules.config` JSONB (Recommended). b) Require dedicated relational settings tables per module (e.g. `watchaware_settings`).
   - *Recommendation*: Option a for simplicity and isolation, moving to dedicated tables only when complex relational foreign keys are needed.
   - *Answered (2026-10-24, controller ledger `snatcharr-adr25-module-config-storage`)*: Option a. One `modules` table (id, enabled, config JSONB); each module validates its own config in Go; `settings.llm_enabled` migrates into it (matching the section 2 migration path).
2. **Route refusal status code (`snatcharr-adr25-disabled-status`)**:
   - *Options*: a) Return HTTP 404 Not Found (Recommended, per [#563](https://github.com/lusoris/SnatchArr/issues/563) to prevent feature enumeration). b) Return HTTP 403 Forbidden with `module_disabled` detail.
   - *Recommendation*: Option a (HTTP 404 with RFC 9457 Problem JSON), ensuring disabled features present no discoverable attack surface.
   - *Answered (2026-10-24, controller ledger `snatcharr-adr25-disabled-status`)*: Option a. Every route of a switched-off module returns HTTP 404 with RFC 9457 `application/problem+json` (matching the shipped LLM module in `api/internal/llm/llm.go` and issue [#563](https://github.com/lusoris/SnatchArr/issues/563)). Checked in the Slice 2 test suite: every operation of an off module answers 404 Problem JSON.
3. **Data retention on switch-off (`snatcharr-adr25-data-on-disable`)**:
   - *Options*: a) Retain existing module data in database when a module is toggled off (Recommended). b) Offer an optional administrative checkbox to purge module data upon deactivation.
   - *Recommendation*: Option a to prevent accidental data loss during temporary deactivation.
   - *Answered (2026-10-24, controller ledger `snatcharr-adr25-data-on-disable`)*: Option a. Switch-off keeps data and deletes nothing. Any purge option constitutes deletion of user data and requires its own operator decision before any slice builds it. Checked in the Slice 1 boundary test: switching a module off and then back on returns the same database rows.

## References

- `napp-shape-20261020` (2026-10-20T14:13:07) (paraphrased): Architecture of switchable modules with hard package, route, and table boundaries; settings page design; phased implementation order.
- `snatcharr-product-v1-modules-20261020` (2026-10-20T14:48:58) (paraphrased): Definition of the four initial modules for the first stable release.
- [ADR-0018](0018-product-front-door.md): Product front door of the media stack.
- [ADR-0021](0021-llm-access-module.md): LLM access module.
- [ADR-0024](0024-watch-aware-modes.md): Watch-aware modes: remove when seen, and the season gate.
- Issue [#563](https://github.com/lusoris/SnatchArr/issues/563): One switch mechanism that turns every optional module on or off, with hard boundaries.
