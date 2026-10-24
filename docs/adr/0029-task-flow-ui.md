<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0029: Task-flow navigation, one activity view, desktop-first density and context help

- Status: accepted (operator order snatcharr-ui-redo-resume-20261024, paraphrased)
- Date: 2026-10-24
- Milestone: [UI and design](https://github.com/lusoris/SnatchArr/milestone/11)
- Supersedes: ADR-0016 sections 1 to 5 (keeps section 6, extended in section 8 here)

## Context

ADR-0016 pull request 1 (#470) shipped the modern operations shell (`web/src/lib/components/shell/Sidebar.svelte`, `TopBar.svelte`, `BottomBar.svelte`, `CommandDialog.svelte`). However, ADR-0016 sections 2 to 4 restyled the page set inherited from the `newtarr` template route by route rather than replacing the underlying layout structure.

An operator review on 2026-10-24 (paraphrased) identified that the user interface still retains newtarr's fragmented structure and suffers from significant usability, density, and workflow defects:

1. **Inherited flat page structure**: Historical and realtime event monitoring are artificially divided across separate routes (`/history`, `/live`, and `/logs`). In practice, `/history` and `/live` consume the same event models:
   - `/history` polls `GET /events` (`api/openapi/openapi.yaml:777`, `page_size: 200`) every 30 seconds (`web/src/routes/history/+page.svelte:22-24`).
   - `/live` connects to the Server-Sent Events stream at `/api/v1/events/stream` (`api/internal/httpapi/module.go:54`, `LIVE_EVENTS` from `web/src/lib/live-events.ts:9-36`) while fetching an initial backlog via `GET /events` (`page_size: 100`, `web/src/routes/live/+page.svelte:28,60`).
   - `/logs` queries `GET /logs` (`api/openapi/openapi.yaml:867`) and listens to `/api/v1/logs/stream` (`api/openapi/openapi.yaml:876`), restricted to administrators (`web/src/routes/logs/+page.svelte:35,51`).
   Dividing these streams across three separate pages degrades troubleshooting workflows.
2. **Weak overview dashboard**: The root dashboard (`web/src/routes/+page.svelte`) shows one global stamina gauge, Prowlarr headroom meters and one card per instance with quickie buttons. It answers no fleet-level question at a glance (failing instances, searches spent today, grabs and imports, queue size) and gives no path into recent activity.
3. **Phone-width columns on desktop viewports**: Across multiple authenticated pages, forms and cards sit in narrow fixed-width containers that do not grow with the viewport. The capture run measured the widest content block at 576 px on `/settings`, `/configarr`, `/instances/[id]`, `/instances/[id]/memory` and `/instances/[id]/policy`, and at 672 px on `/about`, unchanged between 1280 and 1920 px viewports (see the measured baseline below). The sources:
   - `web/src/routes/+layout.svelte:127`: `<div class="mx-auto max-w-[80rem]">`
   - `web/src/routes/about/+page.svelte:30`: `<div class="flex max-w-2xl flex-col gap-6">`
   - `web/src/routes/account/ApiKeyDialog.svelte:44`: `class="card m-auto w-full max-w-lg shadow-xl backdrop:bg-black/60"`
   - `web/src/routes/account/ApiKeys.svelte:165`: `class="card flex max-w-xl flex-col gap-4"`
   - `web/src/routes/account/ChangePassword.svelte:72`: `<form class="card flex max-w-xl flex-col gap-4" onsubmit={handleSubmit}>`
   - `web/src/routes/configarr/+page.svelte:53`: `<section class="card mb-6 max-w-xl" aria-labelledby="configarr-status-title">`
   - `web/src/routes/instances/+page.svelte:131`: `<section class="card max-w-xl" aria-labelledby="add-instance">`
   - `web/src/routes/instances/[id]/+layout.svelte:38`: `<div class="card max-w-xl">`
   - `web/src/routes/settings/+page.svelte:322`: `<div class="mx-auto flex max-w-xl flex-col gap-6">`
   - `web/src/lib/components/AuthCard.svelte:19`: `<div class="mx-auto w-full max-w-sm">`
   - `web/src/lib/components/CreateForm.svelte:27`: `<form class="card flex max-w-xl flex-col gap-4" onsubmit={handleSubmit}>`
   - `web/src/lib/components/FormCard.svelte:51`: `<section class="card max-w-xl" aria-labelledby={id}>`
   - `web/src/lib/components/InstanceEditForm.svelte:55`: `class="card mb-6 flex max-w-xl flex-col gap-3"`
   - `web/src/lib/components/instance/InstancePolicyTab.svelte:82`: `class="mx-auto flex max-w-xl flex-col gap-6"`
   - `web/src/lib/components/instance/InstancePriorityCard.svelte:102`: `class="card mb-6 flex max-w-xl flex-col gap-3"`
   - `web/src/lib/components/QueuePolicy.svelte:172`: `class="card grid max-w-3xl gap-4 md:grid-cols-2"`
   - `web/src/lib/components/ResetMemory.svelte:36`: `<section class="card mb-6 flex max-w-xl flex-col gap-3" aria-labelledby="memory-title">`
   - `web/src/lib/components/ScheduleForm.svelte:146`: `class="card flex max-w-xl flex-col gap-4"`
4. **Absence of colored or animated status signaling**: Status indicators rely on static text or monochromatic badges without semantic hue or stateful animation (such as running pulses).
5. **Excessive vertical scrolling**: Stacked multi-card layouts require continuous scrolling to locate settings or list items rather than organized tabs or spatial grids.
6. **Missing documentation integration**: The web application contains no contextual links pointing to the official documentation site (`site_url` in `mkdocs.yml:7`: `https://lusoris.github.io/SnatchArr/`).

### Measured baseline

Document scroll height per route before the redo, captured on 2026-10-24 from `origin/main` at `776cea4a` in API mode (`tools/e2e/web-e2e-api.sh` with the e2e seed: fakearr instances, one notification channel and rule), light scheme, reduced motion. "Widest content block" is the widest `section`, `.card`, `table`, `form`, `ul` or `dl` inside `main`. Routes at the viewport height (800, 1080, 900) do not scroll. Screenshots for every row at all three viewports, light and dark, form the before set of delivery pull request A (section 8), which shows them beside its after set.

| Route | 1280x800 scroll height (px) | 1920x1080 | 390x844 | widest content block at 1280 / 1920 (px) |
| :--- | ---: | ---: | ---: | ---: |
| `/` | 800 | 1080 | 900 | 992 / 1280 |
| `/about` | 28215 | 28215 | 35310 | 672 / 672 |
| `/account` | 924 | 1080 | 1054 | 992 / 1280 |
| `/configarr` | 800 | 1080 | 900 | 576 / 576 |
| `/download-clients` | 1252 | 1252 | 1412 | 992 / 1280 |
| `/history` | 800 | 1080 | 900 | 0 / 0 |
| `/instances` | 864 | 1080 | 1038 | 992 / 1280 |
| `/instances/[id]` | 1086 | 1086 | 1302 | 576 / 576 |
| `/instances/[id]/activity` | 800 | 1080 | 900 | 992 / 1280 |
| `/instances/[id]/memory` | 800 | 1080 | 900 | 576 / 576 |
| `/instances/[id]/policy` | 2382 | 2382 | 3586 | 576 / 576 |
| `/instances/[id]/queue` | 1930 | 1930 | 2602 | 992 / 1280 |
| `/live` | 800 | 1080 | 900 | 992 / 1280 |
| `/logs` | 46014 | 39692 | 71842 | 992 / 1280 |
| `/prowlarr` | 800 | 1080 | 900 | 992 / 1280 |
| `/schedules` | 874 | 1080 | 1058 | 992 / 1280 |
| `/seerr` | 918 | 1080 | 1030 | 992 / 1280 |
| `/settings` | 4766 | 4766 | 5566 | 576 / 576 |

`/logs` grows with the log ring buffer because the page scrolls as a whole; `/about` renders the full licence list inline; `/settings` stacks every settings form in one 576 px column. `/history` renders its table in a container the metric does not match (0); its width follows the main column.

## Decision

### 1. Task flows drive navigation

Navigation architecture is re-anchored directly around primary operator task flows. The six core operational flows are defined below:

| Task flow | Question the operator asks | Where answered | Primary action |
| :--- | :--- | :--- | :--- |
| **Check health now** | Is the media fleet healthy, stamina unexhausted, and queue clear? | Overview (`/`) | Quick snatch (`Missing` / `Upgrade`) behind `ConfirmAction` |
| **Find out what happened** | Why was a release snatched or skipped? What caused a warning? | Activity (`/activity`) | Filter event stream, toggle live tail, inspect payload |
| **Add or tune an *arr instance** | How do I register an instance or adjust snatch policies and priorities? | Instances (`/instances`, `/instances/[id]/*`) | "+ Add instance", edit policy, strike/unblock queue |
| **Decide when and how much** | When are runs scheduled, and what are global search caps and priorities? | Automation (`/automation`) | "+ Add schedule", edit global hourly cap, edit priority presets |
| **Connect services** | Are Prowlarr, download clients, Seerr, and Configarr linked and healthy? | Connections (`/connections`, `/connections/<kind>`) | "+ Add link", test connection, sync requests |
| **Administer** | How do I manage retention, toggle modules, manage accounts, and view about? | System (`/system`) | Save general settings, toggle module, manage accounts |
| **Manage my own account** | How do I change my password or create an API key? | Account (`/account`, from the account menu) | Change password, create API key |

#### Navigation structure

Top-level navigation consists of exactly six primary entries in fixed order, eliminating categorical group headings:

1. **Overview** (`/`)
2. **Activity** (`/activity`)
3. **Instances** (`/instances`)
4. **Automation** (`/automation`)
5. **Connections** (`/connections`)
6. **System** (`/system`)

- **Account and utility menu**: Shifted to the user avatar/footer section in the sidebar. Houses the own-account page (`/account`, open to every signed-in role, because viewers manage their own password and API keys too), language selection (`LocaleSelect.svelte`), theme toggle, log out, and documentation help links.
- **Mobile bottom bar (`BottomBar.svelte`)**: Presents four destinations on mobile viewports (<768 px): `Overview`, `Activity`, `Instances`, and `More`. Tapping `More` opens `NavDrawer.svelte` containing `Automation`, `Connections`, `System`, and account options.
- **Global Command Dialog (`CommandDialog.svelte`)**: Kept from ADR-0016; routes and instance entries are regenerated to match the new task-flow route table. It never triggers mutative actions directly.
- **Module gating**: Per ADR-0025 section 6, navigation items declare an optional `module?: string` tag. When a module is disabled in backend state, its corresponding navigation entries are omitted, and direct URL access renders an RFC 9457-aligned 404 view.

#### Route table

| New route | Label key | Icon | Role requirement | Module tag | Replaces old route |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `/` | `nav_overview` | `layout-dashboard` | Viewer | none | `/` (restructured) |
| `/activity` | `nav_activity` | `activity` | Viewer (logs tab: Admin) | none | `/history`, `/live`, `/logs` |
| `/instances` | `nav_instances` | `server` | Viewer (create/edit: Admin) | none | `/instances` |
| `/instances/[id]` | `instance_overview` | `server` | Viewer | none | `/instances/[id]` |
| `/instances/[id]/policy` | `instance_policy` | `shield-check` | Admin | none | `/instances/[id]/policy` |
| `/instances/[id]/queue` | `instance_queue` | `list-ordered` | Admin | none | `/instances/[id]/queue` |
| `/instances/[id]/activity` | `instance_activity` | `history` | Viewer | none | `/instances/[id]/activity` |
| `/instances/[id]/memory` | `instance_memory` | `cpu` | Admin | none | `/instances/[id]/memory` |
| `/automation` | `nav_automation` | `clock` | Admin | none | `/schedules`, `/settings` (caps & presets) |
| `/connections` | `nav_connections` | `link` | Viewer (edit: Admin) | none | Hub for integration routes |
| `/connections/download-clients` | `nav_download_clients` | `cloud-download` | Admin | none | `/download-clients` |
| `/connections/seerr` | `nav_seerr` | `film` | Admin | none | `/seerr` |
| `/connections/prowlarr` | `nav_prowlarr` | `search` | Viewer (edit: Admin) | none | `/prowlarr` |
| `/connections/configarr` | `nav_configarr` | `file-code` | Admin | none | `/configarr` |
| `/system` | `nav_system` | `settings` | Admin | none | `/settings` (general, notifications), `/about` |
| `/account` | `nav_account` | `user` | Viewer | none | `/account` (kept, reached from the account menu instead of the sidebar) |

Module-owned entries (for example media servers for the watch-aware module) join `/connections` with their module tag when ADR-0025 slice 4 lands; no current route carries a module tag.

#### Client-side redirects for legacy routes

The Single-Page Application (SPA) remains purely static without server-side routing (ADR-0005). Client-side redirects in `+page.svelte` / `+page.ts` intercept legacy URLs and translate them with query preservation:

- `/history` &rarr; `/activity`
- `/live` &rarr; `/activity` (follow live is on by default)
- `/logs` &rarr; `/activity?source=log`
- `/schedules` &rarr; `/automation?tab=schedules`
- `/download-clients` &rarr; `/connections/download-clients`
- `/seerr` &rarr; `/connections/seerr`
- `/prowlarr` &rarr; `/connections/prowlarr`
- `/configarr` &rarr; `/connections/configarr`
- `/settings` &rarr; `/system?tab=general`
- `/about` &rarr; `/system?tab=about`

`/account` keeps its path.

These client-side redirects are retained for exactly one minor release and then removed.

### 2. One activity view (`/activity`)

A single, unified `ActivityFeed.svelte` component powers both `/activity` and the instance-scoped `/instances/[id]/activity` tab (where the instance filter is fixed).

- **Unified data sources**:
  - **Events stream**: Merges historical events (`GET /events`, `api/openapi/openapi.yaml:777`, backlog `page_size: 100`) and the realtime Server-Sent Events stream (`/api/v1/events/stream`, `api/internal/httpapi/module.go:54`), deduplicated by `Event.id` (`api/openapi/openapi.yaml:2412`).
  - **Server log stream**: Merges ring buffer logs (`GET /logs`, `api/openapi/openapi.yaml:867`) and realtime logs (`/api/v1/logs/stream`, `api/openapi/openapi.yaml:876`), gated strictly for `admin` role (`enabled: session.isAdmin`). For `viewer` users, the "Server log" tab is omitted from the UI.
- **Interactive controls**:
  - **Source segmented control**: Toggle between `Events` and `Server log`.
  - **Follow live toggle**: Active by default. Automatically follows incoming stream frames. If the user scrolls upward into history, auto-scroll pauses and displays a floating pill indicating `n new events`. Tapping the pill snaps back to bottom and resumes following.
  - **Query filters**: Instance selector, log severity (`debug`, `info`, `warn`, `error`), event type filter, date/time range, and free-text search (`q`). All filter parameters are bound to the URL query string (`$page.url.searchParams`) to ensure deep-linking and browser history functionality.
- **Row presentation and grouping**:
  - Each row displays a semantic `StatusIcon.svelte`, relative timestamp (`formatWhen`), instance badge, and event summary. Clicking a row expands an inline detail tray with formatted JSON metadata; no navigation away from the feed is required.
  - Consecutive events belonging to the same snatch execution collapse into a single run summary card based on `run_id` (`api/openapi/openapi.yaml:2415`), displaying item counts and dispatches. Where `run_id` is omitted in event records, grouping falls back to `instance_id` + `type` within a 60-second window (a backend API follow-up is planned to ensure `run_id` is emitted across all snatch events).
- **Virtualization and memory bounds**:
  - DOM virtualization is applied when the feed exceeds 500 rows. The SSE ring frame budget of 500 frames is maintained (`historyLimit: 500` from `web/src/routes/live/+page.svelte:32` and `logs/+page.svelte:56`). `ConnectionStatus.svelte` remains present to communicate realtime SSE connection health.

### 3. Overview redo

On desktop screens (>=1280 px), `/` renders as a high-density 12-column operational grid:

- **Row 1: Key Performance Indicator (KPI) tiles (4 to 6 tiles)**:
  1. *Searches used this hour / global hourly cap*: Measured via `GET /hourly-caps` (`api/openapi/openapi.yaml:913`).
  2. *Fleet instance health*: Healthy vs. total registered instances from `GET /instances` (`api/openapi/openapi.yaml:344`).
  3. *Active queue items*: Aggregate items currently queued across instances. *(Note: OpenAPI currently provides only per-instance queue endpoints `GET /instances/{instanceId}/queue` at line 595; a global summary endpoint `GET /queue/summary` is listed as an API follow-up).*
  4. *Grabs and imports (last 24 hours)*: Derived from `queue_item_imported` and `seerr_request_snatched` event counts. *(Note: General search release grabs are executed directly by *arr instances; an aggregate 24-hour grab summary endpoint is listed as an API follow-up).*
  5. *Prowlarr indexer headroom*: Count of reachable indexers with remaining query and grab capacity via `GET /prowlarr/status` (`api/openapi/openapi.yaml:1431`).
  - Key values are rendered in semantic accent tokens; each tile includes a status indicator icon.
- **Row 2 Left (8 columns): Instance fleet table**:
  - Compact multi-column table displaying one row per instance: connectivity `StatusIcon`, instance name, kind badge, stamina meter (`StaminaGauge`), last execution timestamp, next scheduled window, and Quick Snatch triggers (`Missing` / `Upgrade`) protected by `ConfirmAction.svelte`. (The command palette never initiates snatch executions directly).
- **Row 2 Right (4 columns): Operational context**:
  - Mini `ActivityFeed` showing the 10 most recent events with a direct link to `/activity`.
  - Next schedule countdown timer.
  - Prowlarr indexer headroom meters.
- **Spatial density**: Designed to fit an 8-instance fleet on a 1280x800 viewport without requiring window scrolling.
- **Responsive adaptation**: At 768–1279 px, layout adapts to a 2-column grid; below 768 px, layout stacks into a single column with KPI tiles displayed 2-up.
- **Empty fleet onboarding**: When zero instances exist, the dashboard displays a 3-step onboarding guide (1: Add an *arr instance, 2: Link Prowlarr indexers, 3: Open documentation) in place of a single-sentence empty state.
- **Failure isolation (`UIF1`)**: Every telemetry source runs in an independent Svelte query lifecycle with its own skeleton loader and `ProblemNote.svelte` error card. A failure in one endpoint never blanks the page or blocks neighboring widgets.

### 4. Desktop width and density rules (normative, testable)

1. **Content area width**:
   - Content area spans full width minus the fixed sidebar, bounded at `max-w-[100rem]` (1600 px), aligned to the left against the sidebar gutter.
   - **Normative constraint**: No `mx-auto max-w-*` wrapper narrower than 64rem (1024 px) may exist on any authenticated route at viewports >=1280 px.
   - Forms requiring constrained reading line lengths must utilize a 2-column grid (`Form` + `Context Help/Preview Panel`) instead of a narrow, centered column.
2. **Breakpoints**:
   - `<768px`: Mobile phone (single column, thumb-level bottom navigation).
   - `768px–1279px`: Tablet / compact laptop (2-column layout).
   - `>=1280px`: Desktop multi-column (12-column grid).
   - `>=1600px`: High-density widescreen (expanded table columns, bounded at 100rem).
3. **Density and spacing**:
   - Table row height is standardized to 2.25rem (36 px) at >=1280 px desktop. On mobile devices (<768 px), touch targets maintain a 2.75rem (44 px) height, fully satisfying WCAG 2.5.8 (min 24 px spacing).
   - Sticky `PageHeader`: Title, status badges, and action bars remain pinned to the top of the viewport during content scrolling.
   - Form organization: Multi-card forms are organized into tabbed views (via a local component until the library ships one) rather than vertically stacked card monoliths (e.g. `/system` tabs for General, Modules, Notifications, Accounts, and About).
4. **Scroll budget**:
   - At 1280x800 viewport using the `web-e2e-api` seed dataset, the following routes must fit within 1.25x viewport height (<=1000 px document height): `Overview`, `Instances`, `Instance overview`, `Automation`, `Connections hub`, and `System (General)`.
   - Data tables and activity feeds scroll within dedicated internal containers while the outer page header remains fixed.
5. **Automated layout gate**:
   - `web/e2e/layout.ts` and `web/e2e/layout.test.ts` are extended with `assertDesktopWidth(page, route)`: at 1280 and 1920 viewports, the primary content bounding box must span >= 75% of the `<main>` container width.
   - Scroll budget assertions are executed for each route listed above.
   - **Planted negative**: A test fixture route rendering a centered `max-w-md` column at 1280 px must fail the check (mutation test proving enforcement, mirroring `tools/e2e/planted.sh`).

### 5. Visual language and design tokens

Per controller direction `snatcharr-ui-redo-concordia-scope` (paraphrased), every design token must explicitly declare its authoritative source and lifecycle status.

Palette A ("Night Prowler" violet hue 285) and strict OKLCH colour specifications remain mandatory per ADR-0015 (`TOK1`).

#### Token inventory and sources

| Token | Role | Source | Light mode value | Dark mode value |
| :--- | :--- | :--- | :--- | :--- |
| `--color-destructive` | Critical error, failure, delete | `@sveltesentio/ui` preset via `paletteCss` | `oklch(0.55 0.22 25)` | `oklch(0.55 0.22 25)` |
| `--color-warning` | Warning, rate limit, stamina caution | Local (`web/scripts/gen-theme.ts`, #417) | `oklch(0.55 0.16 80)` | `oklch(0.75 0.16 80)` |
| `--ui-min-target-size`| Accessible touch target floor | `@sveltesentio/ui` | `24px` | `24px` |
| `--ui-focus-emphasis` | Focus ring thickness offset | `@sveltesentio/ui` | `0` | `0` |
| `--color-success` | Healthy, completed, active | Local (`web/scripts/gen-theme.ts`), upstream golusoris/sveltesentio#453 | `oklch(0.50 0.13 150)` | `oklch(0.78 0.14 150)` |
| `--color-info` | Information, secondary status | Local (`web/scripts/gen-theme.ts`), upstream golusoris/sveltesentio#453 | `oklch(0.50 0.12 240)` | `oklch(0.78 0.11 240)` |
| `--color-running` | Active in-progress execution | Local (`web/scripts/gen-theme.ts`), upstream golusoris/sveltesentio#453 | `oklch(0.50 0.16 285)` | `oklch(0.76 0.16 290)` |
| `--motion-duration-short` | Micro-transition duration | Local (`web/scripts/gen-theme.ts`), upstream golusoris/sveltesentio#453 | `150ms` | `150ms` |
| `--motion-easing` | Standard motion transition curve | Local (`web/scripts/gen-theme.ts`), upstream golusoris/sveltesentio#453 | `cubic-bezier(0.2, 0, 0, 1)` | `cubic-bezier(0.2, 0, 0, 1)` |

*Upstream status note*: Checked `@sveltesentio/ui` 0.8.1 source code; the package currently provides no motion tokens and no `@media (prefers-reduced-motion)` rules.

*Contrast check requirement*: Proposed local OKLCH tokens must be contrast-checked (`>= 4.5:1` for text, `>= 3:1` for non-text graphics) against `--color-card` and `--color-background` in both light and dark modes by the PR that introduces them (`web/src/lib/theme-contrast.test.ts`). Values must adjust if any contrast assertion fails.

*Upstream swap rule*: Once `@sveltesentio/ui` publishes official success, info, running, and motion tokens, the local extension in `web/scripts/gen-theme.ts` (and the regenerated `web/src/lib/theme.css`) is removed in that dependency update PR.

#### Component and motion rules

- **`StatusIcon.svelte`**:
  - Implements states: `ok`, `warn`, `danger`, `info`, `running`, `idle`, and `disabled`.
  - Multimodal representation: Uses distinct icon glyphs, semantic colors, and visible text or `.sr-only` screen-reader labels. Status is never conveyed through color alone (WCAG 1.4.1).
  - Animation and reduced motion: In-progress operations (`running`) exhibit a slow pulse or spin; new feed records apply a 150 ms slide/fade; KPI counter updates animate briefly. All animations are wrapped within `@media (prefers-reduced-motion: no-preference)`. When reduced motion is preferred, status icons remain completely static.
  - Motion containment: At most one animated element may run per card or tile to prevent visual fatigue.
- **Accents**: Hero values (KPI numbers, capacity metrics, error counts) utilize status or brand color tokens, while surrounding body copy remains in neutral foreground/muted tokens.

### 6. Documentation and context help

- **Documentation base URL**: Derived from `site_url` in `mkdocs.yml:7` (`https://lusoris.github.io/SnatchArr/`), overridable at build time via `PUBLIC_DOCS_URL` in web environment configuration.
- **Global shell access**:
  - Account menu and sidebar footer include an explicit "Documentation" link (`rel="noopener"`, opens the docs site in a new browser tab).
  - Command palette (`CommandDialog.svelte`) indexes docs pages as searchable entries (e.g. `Docs: Snatch Policies`).
- **Contextual page help**:
  - `PageHeader.svelte` accepts an optional `help` prop: `{ summaryKey: string; docsPath: string }`.
  - An accessible "?" button in the header triggers a side sheet (built on `@sveltesentio/ui/dialog`, non-modal on desktop, modal bottom sheet on mobile).
  - The sheet presents a 2-4 sentence overview from Paraglide message keys (`en` and `de`) and provides a "Read the full guide" link pointing directly to the relevant documentation section anchor.
- **Route-to-documentation mapping**:

Anchors are the MkDocs slugs of headings that exist on `main` today; "write" marks a page or heading the delivery PR for that route adds.

| Route | Summary key | Docs path | Status |
| :--- | :--- | :--- | :--- |
| `/` | `help_overview` | `concepts.md` | exists |
| `/activity` | `help_activity` | `concepts.md#history-and-live-events`, `logs.md#using-the-page` | exists |
| `/instances` | `help_instances` | `concepts.md#snatch` | exists |
| `/instances/[id]` | `help_instance_overview` | `concepts.md#stamina` | exists |
| `/instances/[id]/policy` | `help_instance_policy` | `configuration.md#snatch-policy` | exists |
| `/instances/[id]/queue` | `help_instance_queue` | `queue-management.md` | exists |
| `/instances/[id]/activity` | `help_instance_activity` | `concepts.md#history-and-live-events` | exists |
| `/instances/[id]/memory` | `help_instance_memory` | `concepts.md#afterglow` | exists |
| `/automation` | `help_automation` | `concepts.md#schedules`, `configuration.md#runtime-settings` | exists |
| `/connections` | `help_connections` | `connections.md` | write (hub page linking the per-service pages) |
| `/connections/download-clients` | `help_connections_download_clients` | `download-clients.md` | exists |
| `/connections/seerr` | `help_connections_seerr` | `seerr.md` | exists |
| `/connections/prowlarr` | `help_connections_prowlarr` | `prowlarr.md` | exists |
| `/connections/configarr` | `help_connections_configarr` | `configuration.md#configarr` | exists |
| `/system` | `help_system` | `configuration.md#runtime-settings` | exists |
| `/system?tab=modules` | `help_system_modules` | `modules.md` | write (with ADR-0025 slice 4) |
| `/system?tab=notifications` | `help_system_notifications` | `notifications.md` | exists |
| `/system?tab=accounts` | `help_system_accounts` | `sso.md` | exists; local accounts section written with ADR-0026 |
| `/system?tab=about` | `help_system_about` | `index.md` | exists |
| `/account` | `help_account` | `api.md#authentication` | exists; password section to write |
| `/login` | `help_login` | `sso.md` | exists |
| `/setup` | `help_setup` | `install-compose.md`, `deployment.md#helm` | exists |

- **Field help**: Form fields across policy, scheduling, and system configurations preserve the label/unit/effect structure defined in ADR-0020, supplemented with anchor links to docs headings where available.
- **Automated verification**: A unit test in `web/src/lib/components/PageHeader.svelte.test.ts` validates that every declared route in `nav.ts` provides a valid `help` definition that maps to a real documentation file in the repository.

### 7. Keep/replace table: component and route inventory

Every source file in `web/src/routes` and `web/src/lib/components` is cataloged below (test files are grouped with their corresponding component):

| File path | Verdict | New home | Reason |
| :--- | :--- | :--- | :--- |
| `web/src/routes/+layout.svelte` | kept, restyled | same | Shell layout; updated with 6 top nav items, account menu, and mobile bottom bar |
| `web/src/routes/+layout.ts` | kept | same | Static SPA configuration (`prerender = true`, `ssr = false`, ADR-0005) |
| `web/src/routes/+page.svelte` (and `page.svelte.test.ts`) | replaced | same | Replaced by 12-column Overview grid with KPI tiles, compact table, and mini activity feed |
| `web/src/routes/about/+page.svelte` (and `page.svelte.test.ts`) | replaced | `/system?tab=about` | Standalone narrow `max-w-2xl` page moved into System tabbed interface; redirect added |
| `web/src/routes/account/+page.svelte` (and `page.svelte.test.ts`) | kept, restyled | same, reached from the account menu | Own-account flow for every role; leaves the sidebar, two-column layout (password, API keys) |
| `web/src/routes/account/ApiKeyDialog.svelte` | kept | same | Reusable modal dialog for revealing created API tokens |
| `web/src/routes/account/ApiKeys.svelte` | kept, restyled | same | API key management table; `max-w-xl` removed |
| `web/src/routes/account/ChangePassword.svelte` | kept, restyled | same | Password update form; `max-w-xl` removed, placed in the two-column grid |
| `web/src/routes/configarr/+page.svelte` (and `page.svelte.test.ts`) | replaced | `/connections/configarr` | Flat card stack replaced by Connections sub-route; redirect added |
| `web/src/routes/download-clients/+page.svelte` (and `page.svelte.test.ts`) | replaced | `/connections/download-clients` | Standalone route replaced by Connections sub-route; redirect added |
| `web/src/routes/history/+page.svelte` (and `page.svelte.test.ts`) | replaced | `/activity` | Replaced by unified `ActivityFeed.svelte` events tab; redirect added |
| `web/src/routes/instances/+page.svelte` (and `page.svelte.test.ts`) | kept, restyled | same | Instance fleet list; restyled for high desktop density, eliminating `max-w-xl` add container |
| `web/src/routes/instances/[id]/+layout.svelte` | kept, restyled | same | Persistent instance layout; subtabs updated, `max-w-xl` wrapper eliminated |
| `web/src/routes/instances/[id]/+page.svelte` | kept | same | Instance overview tab host |
| `web/src/routes/instances/[id]/activity/+page.svelte` | replaced | same | Replaced by unified `ActivityFeed.svelte` with fixed `instance_id` filter |
| `web/src/routes/instances/[id]/memory/+page.svelte` | kept | same | Instance processed memory tab host |
| `web/src/routes/instances/[id]/policy/+page.svelte` | kept | same | Instance snatch policy tab host |
| `web/src/routes/instances/[id]/queue/+page.svelte` | kept | same | Instance queue inspection and strike tab host |
| `web/src/routes/live/+page.svelte` (and `page.svelte.test.ts`) | replaced | `/activity?live=true` | Replaced by unified `ActivityFeed.svelte` with follow-live active; redirect added |
| `web/src/routes/login/+page.svelte` (and `page.svelte.test.ts`) | kept | same | Public authentication card with brand mark and SSO trigger |
| `web/src/routes/logs/+page.svelte` (and `page.svelte.test.ts`) | replaced | `/activity?source=log` | Replaced by unified `ActivityFeed.svelte` server log tab; redirect added |
| `web/src/routes/prowlarr/+page.svelte` (and `page.svelte.test.ts`) | replaced | `/connections/prowlarr` | Replaced by Connections sub-route; redirect added |
| `web/src/routes/schedules/+page.svelte` (and `page.svelte.test.ts`) | replaced | `/automation?tab=schedules` | Replaced by Automation view; redirect added |
| `web/src/routes/seerr/+page.svelte` (and `page.svelte.test.ts`) | replaced | `/connections/seerr` | Replaced by Connections sub-route; redirect added |
| `web/src/routes/settings/+page.svelte` (and `page.svelte.test.ts`) | replaced | `/system` and `/automation` | Monolithic `max-w-xl` page split: caps/presets to `/automation`, remainder to `/system` |
| `web/src/routes/settings/NotificationChannels.svelte` | kept, restyled | `/system` | Channel manager; restyled into System Notifications tab |
| `web/src/routes/settings/NotificationRules.svelte` | kept, restyled | `/system` | Rule manager; restyled into System Notifications tab |
| `web/src/routes/setup/+page.svelte` (and `page.svelte.test.ts`) | kept | same | First-run setup form |
| `web/src/lib/components/AdminFormSlot.svelte` (and `.test.ts`) | kept | same | Role-gated wrapper for form inputs |
| `web/src/lib/components/AdminSaveButton.svelte` (and `.test.ts`) | kept | same | Role-gated save button |
| `web/src/lib/components/AuthCard.svelte` (and `.test.ts`) | kept | same | Centered container for unauthenticated views (`/login`, `/setup`) |
| `web/src/lib/components/BrandMark.svelte` (and `.test.ts`) | kept | same | SVG anatomical brand mark (ADR-0017) |
| `web/src/lib/components/CheckState.svelte` (and `.test.ts`) | merged into `StatusIcon` | `web/src/lib/components` | Subsumed by unified multimodal `StatusIcon.svelte` |
| `web/src/lib/components/ConfirmAction.svelte` (and `.test.ts`) | kept | same | 2-step confirmation wrapper for mutative actions |
| `web/src/lib/components/ConfirmDelete.svelte` (and `.test.ts`) | kept | same | In-place delete confirmation control |
| `web/src/lib/components/ConnectionStatus.svelte` (and `.test.ts`) | kept | same | Realtime SSE connection status badge |
| `web/src/lib/components/CreateForm.svelte` (and `.test.ts`) | replaced | `web/src/lib/components` | Replaced by modal dialog sheets without `max-w-xl` wrapper |
| `web/src/lib/components/DownloadClientForm.svelte` | kept | `web/src/lib/components` | Download client configuration dialog form |
| `web/src/lib/components/Field.svelte` (and `.test.ts`) | kept | same | Standard accessible labeled field container |
| `web/src/lib/components/FormCard.svelte` | replaced | `web/src/lib/components` | Removed narrow `max-w-xl` wrapper in favor of 2-column form grids |
| `web/src/lib/components/InstanceEditForm.svelte` (and `.test.ts`) | kept | `web/src/lib/components` | Modal dialog form for instance configuration |
| `web/src/lib/components/InstanceSelect.svelte` | kept | same | Dropdown instance selector |
| `web/src/lib/components/KindSelect.svelte` (and `.test.ts`) | kept | same | Instance kind selector supporting all `APP_KINDS` |
| `web/src/lib/components/LinkCard.svelte` (and `.test.ts`) | kept, restyled | `/connections` | Integration link summary card for Connections hub |
| `web/src/lib/components/LinkForm.svelte` | kept | `web/src/lib/components` | Generic integration connection form |
| `web/src/lib/components/NewPasswordField.svelte` (and `.test.ts`) | kept | same | Password input with strength indicator |
| `web/src/lib/components/PageHeader.svelte` (and `.test.ts`) | modified | same | Unified route header; enhanced with sticky positioning and `help` prop |
| `web/src/lib/components/PolicyFilters.svelte` | kept | same | Snatch policy filter rule manager |
| `web/src/lib/components/PreviewPanel.svelte` (and `.test.ts`) | kept | same | Dry-run preview modal sheet |
| `web/src/lib/components/PriorityPresetFields.svelte` | kept | `/automation` | Priority preset sliders; moved from Settings to Automation |
| `web/src/lib/components/ProblemNote.svelte` (and `.test.ts`) | kept | same | RFC 9457 error card (`UIF1` compliance) |
| `web/src/lib/components/QueryGuard.svelte` (and `.test.ts`) | kept | same | Asynchronous query wrapper handling loading/error states |
| `web/src/lib/components/QueuePolicy.svelte` (and `.test.ts`) | kept, restyled | same | Queue management configuration; restyled without `max-w-3xl` constraint |
| `web/src/lib/components/QueueStrikes.svelte` (and `.test.ts`) | kept | same | Struck release inspector table |
| `web/src/lib/components/ResetMemory.svelte` (and `.test.ts`) | kept, restyled | same | Processed memory reset card; restyled without `max-w-xl` constraint |
| `web/src/lib/components/RunList.svelte` (and `.test.ts`) | merged into `ActivityFeed` | `web/src/lib/components` | Subsumed by unified `ActivityFeed.svelte` |
| `web/src/lib/components/ScheduleForm.svelte` | kept | `/automation` | Cron schedule creation dialog form |
| `web/src/lib/components/ScheduleTable.svelte` | kept | `/automation` | Schedule list table |
| `web/src/lib/components/SecretField.svelte` (and `.test.ts`) | kept | same | Sensitive credential field with visibility toggle |
| `web/src/lib/components/SeerrLinkForm.svelte` | kept | `/connections/seerr` | Seerr connection dialog form |
| `web/src/lib/components/StaminaGauge.svelte` (and `.test.ts`) | kept | same | Hourly capacity gauge using design token variables |
| `web/src/lib/components/TextField.svelte` | kept | same | Labeled text input |
| `web/src/lib/components/UrlText.svelte` (and `.test.ts`) | kept | same | Breakable URL renderer |
| `web/src/lib/components/instance/InstanceActivityTab.svelte` (and `.test.ts`) | replaced | same | Replaced by `ActivityFeed.svelte` scoped to instance |
| `web/src/lib/components/instance/InstanceMemoryTab.svelte` (and `.test.ts`) | kept | same | Instance cache reset container |
| `web/src/lib/components/instance/InstanceOverview.svelte` (and `.test.ts`) | kept, restyled | same | Instance health and stamina overview |
| `web/src/lib/components/instance/InstancePolicyTab.svelte` (and `.test.ts`) | kept, restyled | same | Instance policy tab; eliminated `mx-auto max-w-xl` centered wrapper |
| `web/src/lib/components/instance/InstancePriorityCard.svelte` | kept, restyled | same | Priority override card; restyled without `max-w-xl` constraint |
| `web/src/lib/components/instance/InstanceQueryHarness.svelte` | kept | same | Query harness for instance testing |
| `web/src/lib/components/instance/instance-query.svelte.ts` (and `.test.ts`) | kept | same | Query stores for instance views |
| `web/src/lib/components/instance/InstanceQueueTab.svelte` (and `.test.ts`) | kept | same | Queue management and strike container |
| `web/src/lib/components/instance/tabs.ts` (and `.test.ts`) | kept | same | Instance subtab route definitions |
| `web/src/lib/components/policy/AppSpecificSection.svelte` (and `.test.ts`) | kept | same | Policy section for app-specific filters |
| `web/src/lib/components/policy/LimitsSection.svelte` (and `.test.ts`) | kept | same | Policy section for stamina caps |
| `web/src/lib/components/policy/PresetPicker.svelte` (and `.test.ts`) | kept | same | Snatch policy preset selector |
| `web/src/lib/components/policy/presets.ts` (and `.test.ts`) | kept | same | Pinned policy preset configurations |
| `web/src/lib/components/policy/SafetySection.svelte` (and `.test.ts`) | kept | same | Policy section for safety thresholds |
| `web/src/lib/components/policy/SearchSection.svelte` (and `.test.ts`) | kept | same | Policy section for search dispatch behavior |
| `web/src/lib/components/policy/section.ts` (and `.test.ts`) | kept | same | Policy section helper types |
| `web/src/lib/components/policy/TimingSection.svelte` (and `.test.ts`) | kept | same | Policy section for interval schedules |
| `web/src/lib/components/shell/BottomBar.svelte` (and `.test.ts`) | kept, modified | same | Bottom navigation bar updated to Overview, Activity, Instances, More |
| `web/src/lib/components/shell/CommandDialog.svelte` (and `.test.ts`) | kept, modified | same | Command palette entries updated to match new route table |
| `web/src/lib/components/shell/icons.ts` (and `.test.ts`) | kept, modified | same | Dynamic Lucide icon registrations |
| `web/src/lib/components/shell/KofiLink.svelte` | kept | same | Sponsorship link in account menu |
| `web/src/lib/components/shell/LocaleSelect.svelte` (and `.test.ts`) | kept | same | Language selector dropdown in account menu |
| `web/src/lib/components/shell/NavDrawer.svelte` | kept, modified | same | Mobile secondary navigation drawer |
| `web/src/lib/components/shell/NavFooter.svelte` (and `.test.ts`) | kept, modified | same | Sidebar footer housing user account, docs help, and settings |
| `web/src/lib/components/shell/NavGroups.svelte` (and `.test.ts`) | replaced | `web/src/lib/components/shell` | Replaced by flat 6-item navigation list without group dividers |
| `web/src/lib/components/shell/NavIcon.svelte` | kept | same | Dynamic icon renderer |
| `web/src/lib/components/shell/nav.ts` (and `.test.ts`) | modified | same | Route definitions updated to new 6-flow structure with module tags |
| `web/src/lib/components/shell/Sidebar.svelte` | kept, modified | same | Responsive desktop sidebar updated with new navigation list |
| `web/src/lib/components/shell/TopBar.svelte` | kept | same | Mobile top bar with search trigger and drawer toggle |
| `web/src/lib/components/shell/VersionLabel.svelte` | kept | same | Build version indicator |

### 8. Delivery sequence and verification gates

Delivery is structured across seven discrete, reviewable pull requests. Each PR must independently achieve a clean git status, pass all CI verification gates, and maintain zero regression:

1. **PR A: Shell, Navigation, Redirects, Layout Gate, and Status Tokens**
   Delivered in two parts:
   - **PR A1**: Layout width gate (`assertDesktopWidth`, scroll budget, planted negative in `layout.test.ts`), local status and motion tokens in `gen-theme.ts` / `theme.css`, contrast tests in `theme-contrast.test.ts`, and `StatusIcon.svelte`.
   - **PR A2**: Reconfigures top-level navigation in `nav.ts` to the 6 primary entries, updates `Sidebar.svelte` and `BottomBar.svelte`, establishes client-side redirects for legacy routes in SvelteKit router, and route hosts.
2. **PR B: Overview Redo (`/`)**
   - Implements 12-column operational desktop layout, 5 KPI telemetry tiles, compact instance table, and mini activity stream.
   - Introduces 3-step structured empty fleet onboarding checklist.
3. **PR C: Unified Activity View (`/activity`)**
   - Implements `ActivityFeed.svelte` merging events and server logs with follow-live toggle, query filters, and virtualized list.
   - Refactors `/instances/[id]/activity` to consume `ActivityFeed.svelte`.
4. **PR D: Instances Fleet and Subtabs (`/instances`, `/instances/[id]/*`)**
   - Eliminates `max-w-xl` constraints across instance forms and subtabs.
   - Implements dense desktop instance management grid.
5. **PR E: Automation (`/automation`)**
   - Implements `/automation` route combining cron schedules, global hourly cap configuration, and priority preset sliders.
6. **PR F: Connections Hub (`/connections/*`)**
   - Implements Connections overview hub and dedicated sub-routes for Download Clients, Seerr, Prowlarr, and Configarr.
7. **PR G: System Administration and Documentation Help (`/system/*`)**
   - Implements tabbed System administration view (General, Modules, Notifications, Accounts, About); the Accounts tab holds the account management of ADR-0026 once that lands, and the own-account page stays at `/account`.
   - Integrates `PageHeader.svelte` help slide-over side sheet and links all routes to docs anchors.

#### Verification gates per pull request

Every implementation PR extends existing test suites under `web/e2e/` (ADR-0016 section 6) and must satisfy:

- **Screenshot baselines**: Captures rendered views at 1280x800, 1920x1080, and 390x844 in both light and dark themes against the `web-e2e-api` seed dataset. PR description must include before and after comparison images for touched routes.
- **Scroll height audit**: PR description includes the before-and-after scroll height table for touched routes, verifying compliance with the 1.25x viewport budget.
- **Accessibility gate**: `web/e2e/a11y.test.ts` sweeps all routes with axe-core; zero violations permitted against WCAG 2.2 AA and EN 301 549 (`A11Y1`).
- **Interaction and viewport tests**: `keyboard-nav.test.ts` (focus trapping and Escape behavior), `phone-overflow.test.ts` (zero horizontal overflow at 390 px), and `inp.test.ts` (INP <= 200 ms).
- **Non-blocking UI resilience (`UIF1`)**: `web/e2e/nonblocking.test.ts` tests touched routes against a planted 10-second API delay followed by a 503 response; shell navigation must remain responsive within 1 second while displaying an appropriate skeleton or `ProblemNote.svelte`.
- **Visual regression**: Snapshot diffs verified via `make web-visual-update`.
- **Served-surface gate**: The served-site check (`layout.test.ts` against `SNATCHARR_SERVED_URL`) runs and passes following production release deployment.

## Consequences

### Positive

- **Intuitive domain alignment**: The six task flows directly match operator intent, removing cognitive friction when diagnosing problems or adjusting parameters.
- **Streamlined event observability**: Merging events and logs into a single `ActivityFeed` eliminates tab-switching during troubleshooting.
- **High information density**: Full-width desktop grids utilize large viewports effectively without awkward centered phone columns.
- **Integrated help**: Operators can immediately open relevant documentation sections directly from any page header.
- **Strict token governance**: All status colors and motion curves adhere to verified design tokens with clean upstream migration paths.

### Negative and neutral

- **Temporary redirect overhead**: Maintaining legacy client-side redirects creates transitional routing entries that must be deprecated after one minor release.
- **Local token maintenance**: Operating local status and motion tokens in `theme.css` requires tracking upstream `golusoris/sveltesentio` releases for replacement.
- **API gaps**: Complete KPI telemetry requires follow-up API endpoints for fleet-aggregate queue counts and 24-hour grab totals.

## Alternatives considered

### 1. Incremental cosmetic restyling of ADR-0016 pages

Continuing to restyle inherited newtarr pages without altering route taxonomy or layout structures was evaluated.
*Why not*: Operator review demonstrated that cosmetic updates cannot resolve fundamental architectural defects: separate history, live, and log pages force repetitive navigation, and stacked card layouts cannot achieve desktop density without structural redesign.

### 2. Separate dedicated routes for Events and Logs

Preserving separate `/events` and `/logs` top-level routes was evaluated.
*Why not*: Operational investigations require viewing application logs and snatch events side-by-side or in close correlation. A segmented control inside a single `/activity` view provides instant switching while maintaining URL-addressable filter state.

### 3. Unbounded fluid full-width layout

Allowing tables and text blocks to expand infinitely across 2560px+ ultrawide displays was evaluated.
*Why not*: Unconstrained text lines on wide monitors violate accessibility readability standards (WCAG reading measure recommendations of 80 characters per line). Capping content area width at `100rem` (1600 px) ensures optimal information density without unreadable line lengths.

## References

- [ADR-0015: SnatchArr's own UI: palette A, the M3 mark, a task-flow navigation and a UI that never blocks](0015-ui-design-system-and-layout.md)
- [ADR-0016: Modern operations layout: collapsible navigation, unified page headers, mobile bottom bar and dialog sheets](0016-ui-layout-and-page-redo.md)
- [ADR-0020: Snatch policy defaults and presets](0020-snatch-policy-defaults-and-presets.md)
- [ADR-0023: Roles from single sign-on groups](0023-sso-viewer-role.md)
- [ADR-0025: Architecture and mechanism of optional module switches](0025-module-switches.md)
- Operator order `snatcharr-ui-redo-20261020` (paraphrased)
- Operator order `snatcharr-ui-redo-resume-20261024` (paraphrased)
- Controller answer `snatcharr-ui-redo-concordia-scope` (paraphrased)
