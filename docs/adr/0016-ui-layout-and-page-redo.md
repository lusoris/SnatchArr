<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0016: Modern operations layout: collapsible navigation, unified page headers, mobile bottom bar and dialog sheets

- Status: accepted
- Date: 2026-10-20
- Milestone: [UI and design](https://github.com/lusoris/SnatchArr/milestone/11)
- Supersedes: ADR-0015 sections 3, 4, 6 (keeps sections 1, 2, 5)

## Context

ADR-0015 established SnatchArr's visual identity: palette A ("Night Prowler" violet hue 285), the M3 vesica mark, WCAG 2.2 AA / EN 301 549 compliance gates (`A11Y1`), non-blocking UI guarantees (`UIF1`), and strict design tokens (`TOK1`). However, ADR-0015 preserved the fundamental layout structure inherited from the upstream `newtarr` template.

A design audit conducted on 2026-10-20 across all 19 application routes captured visual baselines at desktop (1280x800) and mobile (390x844) viewports in both dark and light modes (100 screenshots from a one-off local capture run; the run is not committed, and the gates in section 6 extend the existing e2e suites instead). The visual evidence revealed several systemic architectural defects in the user interface:

1. **Cluttered flat navigation hierarchy**: The sidebar exposed 12 flat routes without logical domain grouping. Navigating between instances, monitoring runs, automation schedules, and external services required linear visual scanning rather than grouped mental models.
2. **Absence of rapid command navigation**: The interface lacked global quick-search and command-palette mechanisms (`Cmd+K`). In multi-instance deployments, switching instances or finding specific tools required manual sidebar hunting.
3. **Ergonomic failure on mobile devices**: The mobile hamburger drawer required two taps to access core navigation destinations, and navigation controls remained placed at the top of the viewport—the most difficult area to reach with one hand.
4. **Disjointed page headers and action bars**: Page titles (`h1`), status indicators, and actions lacked a unified structural container. Some pages aligned actions horizontally, others stacked them vertically, while status indicators (e.g. SSE stream connectivity on `/live` and `/logs`) were inconsistently positioned.
5. **Inline forms causing severe vertical sprawl**: Resource creation forms (schedules, download clients, Seerr links) were appended directly beneath data lists. Users were forced to scroll past dozens of table rows or cards just to reach form inputs, obscuring list context while editing.
6. **Mobile viewport table overflow**: Multi-column tables (`/history`, `/live`, `/seerr`, `/download-clients`) forced horizontal scrolling on 390px screens without card fallbacks or contextual metadata summaries.
7. **Monolithic instance layout**: The instance view lacked clear operational boundaries, leaving users without a concise overview of stamina, search budgets, and queue states.

Current operations dashboards (Linear, Grafana, Vercel, GitHub Primer, and self-hosted tools such as Sonarr v4, Jellyfin and Overseerr) separate global navigation, entity routing, list manipulation and form entry into distinct, predictable layers.

This ADR defines a layout and information architecture of SnatchArr's own that replaces the inherited newtarr structure while keeping ADR-0015 rules `A11Y1`, `UIF1`, `TOK1`, palette A, and the M3 mark.

## Decision

### 1. Information architecture and navigation model

The navigation architecture shifts from a flat link list to a domain-grouped, multi-modal navigation system with three tiers:

- **Desktop responsive sidebar (`Sidebar.svelte`)**: Grouped into five distinct operational domains (`Fleet`, `Activity`, `Automation`, `Integrations`, `System`). Carries a search trigger showing the platform keyboard shortcut (`⌘K` / `Ctrl+K`) and a footer with the language switcher, account link and log out.
- **Global Command Dialog (`CommandDialog.svelte`)**: Activated via `Cmd+K` / `Ctrl+K` or the sidebar/topbar search triggers. A thin wrapper over `@sveltesentio/ui/cmd/palette` (no own palette, filtering or key handling); it navigates to every route and every configured instance. It never starts an action itself: a snatch run spends search budget, so it stays behind the explicit button on the dashboard or instance page, where `ConfirmAction` guards it.
- **Mobile bottom navigation bar (`BottomBar.svelte`)**: A fixed bar at the bottom of the viewport below the `md` breakpoint with one-tap access to the four primary views (`Dashboard`, `Instances`, `History`, `Live`) plus a "More" item that opens the full drawer (`NavDrawer.svelte`) for the other routes. The content area reserves the bar's height plus `env(safe-area-inset-bottom)` as bottom padding, so the bar never covers the last row of a page or a focused control (WCAG 2.2 SC 2.4.11).

```text
+-------------------------------------------------------------------------------+
| TopBar (Mobile: Brand + Search + NavDrawer toggle; Desktop: Breadcrumb/Search)|
+-------------------+-----------------------------------------------------------+
| Sidebar           | PageHeader (Title + Status Badge + Action Toolbar)        |
| - Search (Cmd+K)  +-----------------------------------------------------------+
| - Fleet (Inst, ..)|                                                           |
| - Activity (Live) | Page Content Area (Responsive Grid / DataTable / Cards)   |
| - Automation      |                                                           |
| - Integrations    |                                                           |
| - System          |                                                           |
|                   |                                                           |
| - Footer (Lang/Out| (Mobile: BottomBar thumb rail for top 4 destinations)     |
+-------------------+-----------------------------------------------------------+
```

#### Navigation taxonomy

| Group | Route | Label key | Icon | Mobile surface | Role requirement |
| :--- | :--- | :--- | :--- | :--- | :--- |
| *Root* | `/` | `nav_dashboard` | `layout-dashboard` | Bottom bar (primary) | Viewer |
| **Fleet** | `/instances` | `nav_instances` | `server` | Bottom bar (primary) | Viewer |
| **Fleet** | `/instances/[id]` | `instance_overview` | `server` | Drawer (`/instances`) | Viewer |
| **Fleet** | `/instances/[id]/policy` | `instance_policy` | `shield-check` | Subtab | Operator |
| **Fleet** | `/instances/[id]/queue` | `instance_queue` | `list-ordered` | Subtab | Operator |
| **Fleet** | `/instances/[id]/activity` | `instance_activity` | `history` | Subtab | Viewer |
| **Fleet** | `/instances/[id]/memory` | `instance_memory` | `cpu` | Subtab | Operator |
| **Activity** | `/history` | `nav_history` | `history` | Bottom bar (primary) | Viewer |
| **Activity** | `/live` | `nav_live` | `radio` | Bottom bar (primary) | Viewer |
| **Automation** | `/schedules` | `nav_schedules` | `calendar` | Drawer | Operator |
| **Automation** | `/download-clients` | `nav_download_clients` | `hard-drive` | Drawer | Operator |
| **Integrations** | `/seerr` | `nav_seerr` | `link` | Drawer | Operator |
| **Integrations** | `/prowlarr` | `nav_prowlarr` | `search` | Drawer | Viewer |
| **Integrations** | `/configarr` | `nav_configarr` | `file-code` | Drawer | Admin |
| **System** | `/settings` | `nav_settings` | `settings` | Drawer | Admin |
| **System** | `/logs` | `nav_logs` | `file-text` | Drawer | Admin |
| **System** | `/account` | `nav_account` | `user` | Drawer | Viewer |
| **Auth** | `/login` | `login_title` | `log-in` | Standalone | Public |
| **Auth** | `/setup` | `setup_title` | `sparkles` | Standalone | Public (initial setup) |

### 2. Standardized page layout templates

Every route follows one of five layout templates:

1. **Dashboard / Fleet Overview**:
   - Primary heading band via `PageHeader` with aggregate fleet health badge (`dashboard_fleet_healthy` / `dashboard_fleet_degraded`).
   - High-level metric telemetry cards (Global Stamina gauge, Prowlarr indexer headroom, active instance counts).
   - Multi-column responsive card grid displaying per-instance status, hourly quota consumption, and immediate Quick Snatch action triggers (`Missing` / `Upgrade`).
2. **List + Detail Toolbar**:
   - `PageHeader` featuring action snippet toolbar (e.g. "+ Add Schedule", "+ Add Client", "Trigger Discover").
   - Filter and search bar (`PolicyFilters`, level selectors, search queries).
   - Responsive presentation: desktop multi-column `DataTable` or card grid; mobile stacked card representation with explicit metadata badges.
   - Standard empty state (`card` with brand icon, descriptive text, and primary call to action).
3. **Subtab Detail Layout (`/instances/[id]/*`)**:
   - Persistent parent layout (`/instances/[id]/+layout.svelte`) rendering instance title, kind/role badge, error alerts, and horizontal navigation pills (`Overview`, `Policy`, `Queue`, `Activity`, `Memory`).
   - `/instances/[id]` itself renders the Overview tab inside that layout; there is no redirect, so the bare instance URL stays a stable deep link.
   - Subpages load directly into the container without full page re-render.
   - Deep-linkable URLs with browser history support.
4. **Modal Form Dialog / Sheet**:
   - Resource creation and editing forms (`ScheduleForm`, `DownloadClientForm`, `SeerrLinkForm`, `InstanceEditForm`) move out of document flow into modal dialogs built on `@sveltesentio/ui/dialog` (native `<dialog>` with `.showModal()` underneath); SnatchArr adds no own dialog primitive.
   - Keyboard accessible: focus auto-placed on first field, `Escape` key cancels and restores focus to opener element, backdrop click dismisses.
5. **Centered Card Layout (System / Auth)**:
   - Max-width constrained card layout (`max-w-xl` for settings; `max-w-sm` for login/setup) centered horizontally.
   - Clear field groups, label associations (`for`/`id`), and RFC 9457 error summaries.

### 3. Per-page layout specification

| Route | Primary task | Primary action | Template | Key components |
| :--- | :--- | :--- | :--- | :--- |
| `/` | Fleet health & quota glance | Quick snatch (`missing` / `upgrade`) | Overview / Dashboard | `PageHeader`, `StaminaGauge`, instance cards, `BrandMark` |
| `/instances` | Instance fleet management | Add instance (`+ Add`) | List + Detail | `PageHeader`, instance card grid, `CheckState`, `ConfirmDelete` |
| `/instances/[id]` | Inspect single instance health | Trigger manual run | Detail Subtab | `InstanceOverview`, `StaminaGauge`, status badges |
| `/instances/[id]/policy` | Configure snatch filters & caps | Save instance policy | Detail Subtab | `InstancePolicyTab`, `PolicyFilters`, `QueuePolicy` |
| `/instances/[id]/queue` | Monitor and unblock queues | Strike / unblock item | Detail Subtab | `InstanceQueueTab`, `QueueStrikes`, queue item table |
| `/instances/[id]/activity` | Audit historical runs for instance | Filter runs | Detail Subtab | `InstanceActivityTab`, `RunList` |
| `/instances/[id]/memory` | Manage processed release cache | Reset memory | Detail Subtab | `InstanceMemoryTab`, `ResetMemory`, `ConfirmAction` |
| `/history` | Review snatch audit trail | Filter by instance/status | List + Detail | `PageHeader`, `RunList`, filter controls |
| `/live` | Real-time event stream observation | Pause / resume stream | List + Detail | `PageHeader`, SSE connection badge, live log cards |
| `/schedules` | Automation cron management | Add schedule (`+ Add`) | List + Detail | `PageHeader`, `ScheduleTable`, `ScheduleForm` modal dialog |
| `/download-clients` | Client backpressure config | Add client / Discover | List + Detail | `PageHeader`, client status cards, `DownloadClientForm` dialog |
| `/seerr` | Overseerr/Jellyseerr requests | Snatch now / Sync | List + Detail | `PageHeader`, request table, `SeerrLinkForm` dialog |
| `/prowlarr` | Indexer query/grab limits | Test link | List + Detail | `PageHeader`, indexer limit table, `LinkForm` dialog |
| `/configarr` | Declarative config sync | Run import | Centered Card | `PageHeader`, status summary, `FormCard`, `TextField` |
| `/settings` | Global caps & notifications | Save settings | Centered Card | `PageHeader`, `NotificationChannels`, `NotificationRules` |
| `/logs` | Ring buffer & stream audit | Filter level / query | List + Detail | `PageHeader`, SSE stream indicator, terminal-style log feed |
| `/account` | User credentials & API tokens | Create API key | Centered Card | `PageHeader`, `ChangePassword`, `ApiKeys`, `ApiKeyDialog` |
| `/login` | Authenticate session | Sign in / SSO OIDC | Auth Card | `BrandMark`, credentials form, SSO button |
| `/setup` | Initialize first admin account | Complete setup | Auth Card | `BrandMark`, initial setup form, secret input |

### 4. What stays and what goes: repository inventory

Every file under `web/src/routes` and `web/src/lib/components` is classified:

| File path | Status | Architectural decision & rationale |
| :--- | :--- | :--- |
| `web/src/routes/+layout.svelte` | modified | Replaced flat frame with `Sidebar`, `TopBar`, `BottomBar`, `CommandDialog`, and skip link. |
| `web/src/routes/+layout.ts` | kept | Declares `prerender = true` and `ssr = false` for static SPA mode (ADR-0005). |
| `web/src/routes/+page.svelte` | modified | Modernized with `PageHeader`, fleet health badge, summary metrics, and token classes. |
| `web/src/routes/instances/+page.svelte` | modified | Modernized with `PageHeader` and responsive card layout. |
| `web/src/routes/instances/[id]/+layout.svelte` | modified | Modernized with `PageHeader`, instance role badges, and sticky subtab pill navigation. |
| `web/src/routes/instances/[id]/+page.svelte` | kept | Hosts `InstanceOverview` component. |
| `web/src/routes/instances/[id]/policy/+page.svelte` | kept | Hosts `InstancePolicyTab` component. |
| `web/src/routes/instances/[id]/queue/+page.svelte` | kept | Hosts `InstanceQueueTab` component. |
| `web/src/routes/instances/[id]/activity/+page.svelte` | kept | Hosts `InstanceActivityTab` component. |
| `web/src/routes/instances/[id]/memory/+page.svelte` | kept | Hosts `InstanceMemoryTab` component. |
| `web/src/routes/history/+page.svelte` | modified | Modernized with `PageHeader` and filter bar. |
| `web/src/routes/live/+page.svelte` | modified | Modernized with `PageHeader`, token classes for SSE status, and responsive stream cards. |
| `web/src/routes/schedules/+page.svelte` | modified | Modernized with `PageHeader` and modal dialog for schedule creation/editing. |
| `web/src/routes/download-clients/+page.svelte` | modified | Modernized with `PageHeader` and client status cards. |
| `web/src/routes/seerr/+page.svelte` | modified | Modernized with `PageHeader` and request table action toolbar. |
| `web/src/routes/prowlarr/+page.svelte` | modified | Modernized with `PageHeader` and indexer limit table. |
| `web/src/routes/configarr/+page.svelte` | modified | Modernized with `PageHeader` and declarative status cards. |
| `web/src/routes/settings/+page.svelte` | modified | Modernized with `PageHeader` and clean field groupings. |
| `web/src/routes/settings/NotificationChannels.svelte` | kept | Channel management sub-component. |
| `web/src/routes/settings/NotificationRules.svelte` | kept | Notification rule trigger sub-component. |
| `web/src/routes/logs/+page.svelte` | modified | Modernized with `PageHeader`, token classes (`bg-brand`, `bg-danger`), and live stream badge. |
| `web/src/routes/account/+page.svelte` | modified | Modernized with `PageHeader` and clean section containers. |
| `web/src/routes/account/ApiKeyDialog.svelte` | kept | Accessible modal dialog for displaying one-time created secret token. |
| `web/src/routes/account/ApiKeys.svelte` | kept | API key table with copy and revoke confirmations. |
| `web/src/routes/account/ChangePassword.svelte` | kept | Standard password change form. |
| `web/src/routes/login/+page.svelte` | kept | Clean centered card with brand mark, credentials form, and SSO trigger. |
| `web/src/routes/setup/+page.svelte` | kept | Initial admin setup card with secret token authentication. |
| `web/src/lib/components/PageHeader.svelte` | new | Canonical header component for all routes (`title`, `description`, `badge`, `actions`). |
| `web/src/lib/components/shell/BottomBar.svelte` | new | Mobile thumb-level bottom navigation rail for top 4 destinations + drawer trigger. |
| `web/src/lib/components/shell/CommandDialog.svelte` | new | Global keyboard command palette (`Cmd+K`): route and instance entries handed to `@sveltesentio/ui/cmd/palette`. |
| `web/src/lib/components/shell/Sidebar.svelte` | modified | Enhanced with search trigger, grouped navigation categories, and badge counters. |
| `web/src/lib/components/shell/TopBar.svelte` | modified | Added mobile search trigger and modernized layout. |
| `web/src/lib/components/shell/NavDrawer.svelte` | kept | Accessible mobile sliding drawer for secondary destinations. |
| `web/src/lib/components/shell/NavIcon.svelte` | kept | Dynamic Lucide icon renderer. |
| `web/src/lib/components/shell/nav.ts` | kept | Navigation hierarchy definition and active route matcher. |
| `web/src/lib/components/shell/icons.ts` | modified | Added required icons (`ellipsis`, `command`, `plus`, `check`, `circle-alert`, `activity`). |
| `web/src/lib/components/BrandMark.svelte` | kept | M3 vesica brand mark renderer. |
| `web/src/lib/components/CheckState.svelte` | kept | State indicator for instance connectivity. |
| `web/src/lib/components/ConfirmAction.svelte` | kept | Inline 2-step action confirmation wrapper. |
| `web/src/lib/components/ConfirmDelete.svelte` | kept | In-place delete confirmation button. |
| `web/src/lib/components/DownloadClientForm.svelte` | kept | Client configuration form; moved into modal dialog. |
| `web/src/lib/components/Field.svelte` | kept | Standard labeled form field container. |
| `web/src/lib/components/FormCard.svelte` | kept | Form card wrapper. |
| `web/src/lib/components/InstanceEditForm.svelte` | kept | Instance editing form; moved into modal dialog. |
| `web/src/lib/components/InstanceSelect.svelte` | kept | Dropdown selector for instances. |
| `web/src/lib/components/KindSelect.svelte` | kept | Dropdown selector over every API instance kind from `APP_KINDS` in `web/src/lib/kinds.ts` (`sonarr`, `radarr`, `lidarr`, `readarr`, `whisparr_v2`, `whisparr_v3`); the redo removes none. |
| `web/src/lib/components/LinkForm.svelte` | kept | Generic link configuration form. |
| `web/src/lib/components/PolicyFilters.svelte` | kept | Filter rules form for snatch policies. |
| `web/src/lib/components/PreviewPanel.svelte` | kept | Dry-run preview modal sheet. |
| `web/src/lib/components/ProblemNote.svelte` | kept | RFC 9457 problem alert card. |
| `web/src/lib/components/QueuePolicy.svelte` | kept | Queue monitoring policy configuration. |
| `web/src/lib/components/QueueStrikes.svelte` | kept | Queue strikes inspector. |
| `web/src/lib/components/ResetMemory.svelte` | kept | Processed cache reset action card. |
| `web/src/lib/components/RunList.svelte` | kept | Snatch execution history table/list. |
| `web/src/lib/components/ScheduleForm.svelte` | kept | Cron schedule editor form; moved into modal dialog. |
| `web/src/lib/components/ScheduleTable.svelte` | kept | Schedule list table with toggle controls. |
| `web/src/lib/components/SecretField.svelte` | kept | Password/API key field with visibility toggle. |
| `web/src/lib/components/SeerrLinkForm.svelte` | kept | Overseerr/Jellyseerr link configuration form. |
| `web/src/lib/components/StaminaGauge.svelte` | modified | Replaced literal oklch styles with design token custom property `var(--stamina-color)`. |
| `web/src/lib/components/TextField.svelte` | kept | Reusable text input field. |
| `web/src/lib/components/instance/InstanceActivityTab.svelte` | kept | Instance run history tab component. |
| `web/src/lib/components/instance/InstanceMemoryTab.svelte` | kept | Instance cache memory tab component. |
| `web/src/lib/components/instance/InstanceOverview.svelte` | kept | Instance status and stamina overview component. |
| `web/src/lib/components/instance/InstancePolicyTab.svelte` | kept | Instance snatch policy tab component. |
| `web/src/lib/components/instance/InstanceQueueTab.svelte` | kept | Instance queue monitoring tab component. |
| `web/src/lib/components/instance/tabs.ts` | kept | Tab route definitions and metadata. |

### 5. Implementation order

The redesign is delivered across seven discrete, reviewable pull requests. Each PR must pass all CI gates (`make web-verify`, `make web-e2e`) independently:

1. **PR 1: Shell & Mobile Navigation Infrastructure**
   - Introduces `PageHeader.svelte`, `BottomBar.svelte`, `CommandDialog.svelte`.
   - Updates `Sidebar.svelte`, `TopBar.svelte`, and `+layout.svelte`.
   - Adds required Lucide icons in `icons.ts`.
   - Verified by: existing `web/e2e/visual.test.ts`, `a11y.test.ts`, `keyboard-nav.test.ts` and `phone-overflow.test.ts` extended for the shell, plus a new unit test for `icons.ts`.
2. **PR 2: Dashboard & Fleet Telemetry (`/`)**
   - Upgrades root dashboard with fleet health summary badge, global stamina metric, Prowlarr indexer headroom bar, and Quick Snatch triggers.
   - Replaces all inline styles and literal colors in `StaminaGauge.svelte`.
   - Verified by: `src/routes/page.svelte.test.ts`, axe 0 violations.
3. **PR 3: Activity & Live Event Stream (`/history`, `/live`)**
   - Applies `PageHeader` and filter bar to `/history`.
   - Modernizes `/live` with token classes (`bg-brand`, `bg-danger`) for SSE connection indicator and live event cards.
   - Verified by: `src/routes/history/page.svelte.test.ts`, `src/routes/live/page.svelte.test.ts`, axe 0 violations.
4. **PR 4: Automation & Download Backpressure (`/schedules`, `/download-clients`)**
   - Implements modal dialogs for `ScheduleForm` and `DownloadClientForm`.
   - Modernizes `/schedules` and `/download-clients` with `PageHeader` and clean status cards.
   - Verified by: `src/routes/schedules/page.svelte.test.ts`, `src/routes/download-clients/page.svelte.test.ts`, axe 0 violations.
5. **PR 5: Integrations (`/seerr`, `/prowlarr`, `/configarr`)**
   - Applies `PageHeader` to `/seerr`, `/prowlarr`, and `/configarr`.
   - Converts `SeerrLinkForm` and `LinkForm` into modal dialog workflows.
   - Verified by: `src/routes/seerr/page.svelte.test.ts`, `src/routes/prowlarr/page.svelte.test.ts`, `src/routes/configarr/page.svelte.test.ts`, axe 0 violations.
6. **PR 6: System & Identity (`/settings`, `/logs`, `/account`, `/login`, `/setup`)**
   - Standardizes `/settings`, `/logs`, `/account` with `PageHeader`.
   - Tokenizes SSE status indicators on `/logs` (`bg-brand`, `bg-danger`).
   - Cleans card structure on `/login` and `/setup`.
   - Verified by: `src/routes/settings/page.svelte.test.ts`, `src/routes/logs/page.svelte.test.ts`, `src/routes/account/page.svelte.test.ts`, `src/routes/login/page.svelte.test.ts`, `src/routes/setup/page.svelte.test.ts`, axe 0 violations.
7. **PR 7: Instances (`/instances`, `/instances/[id]/*`)**
   - Scheduled after the Whisparr kind detection fix (pull request 467) lands, so both touch the instance pages in sequence.
   - Implements `InstanceEditForm` modal dialog, subtab URL pill navigation, and refreshed `InstanceOverview`.
   - Verified by: `src/routes/instances/page.svelte.test.ts`, instance subtab tests, axe 0 violations.

### 6. Verification gates

Every implementation PR extends the existing Playwright suites under `web/e2e/` instead of adding parallel ones, and supplies this proof before merging:

- **Screenshots**: `web/e2e/visual.test.ts` (pinned Playwright image, mocked API, planted visual changes that must fail) grows from the public pages to the pages the PR touches, at 1280x800 and 390x844 in light and dark. The PR body carries before and after screenshots of its route group (`dashboard`, `fleet`, `activity`, `automation`, `integrations`, `system`, `auth`).
- **Accessibility**: `web/e2e/a11y.test.ts` sweeps all 19 routes with axe-core against WCAG 2.2 AA and EN 301 549; zero violations, never suppressed. The prototype measured zero on all 19 routes.
- **Nonblocking UI**: `web/e2e/nonblocking.test.ts` passes; a slow or failing API leaves the shell responsive.
- **Keyboard and phone width**: `web/e2e/keyboard-nav.test.ts` covers the palette, the bottom bar, the More drawer and every dialog (open, focus on the first field, `Escape` closes and returns focus to the opener); `web/e2e/phone-overflow.test.ts` shows no horizontal overflow at 390 px and no control hidden under the bottom bar.
- **INP**: `web/e2e/inp.test.ts` keeps Interaction to Next Paint at or below 200 ms on tab switches, dialog opening and palette keystrokes.

## Consequences

### Positive

- **Faster navigation**: desktop users reach any route or instance through the `Cmd+K` palette; mobile users reach the four primary views with one tap on the bottom bar.
- **List stays in view while editing**: creation and edit forms open in a dialog over the list instead of below it.
- **Accessibility stays gated**: every route runs through axe-core (WCAG 2.2 AA / EN 301 549) with zero violations allowed, as ADR-0015 `A11Y1` requires.
- **Tokens only**: pages use semantic design tokens (`bg-brand`, `bg-danger`, `text-subtle`, `border-line`) instead of raw Tailwind colour classes or inline `oklch(...)` styles (ADR-0015 `TOK1`).

### Negative and neutral

- **More shell code**: `BottomBar` and `CommandDialog` add two components to the shell layer.
- **Dialog state synchronization**: Creation forms rendered in modal dialogs require explicit open/close state management and focus restoration.

## Alternatives considered

### 1. Linear-style top navigation tabs

A top-anchored horizontal tab navigation bar (common in developer tools like Linear and Vercel) was evaluated.
*Why not*: On standard 1280x800 displays, top horizontal bars consume valuable vertical viewport height. Furthermore, top horizontal tabs do not translate naturally to mobile screens without introducing an entirely separate mobile navigation drawer, whereas a responsive sidebar plus mobile bottom bar shares unified state and icons cleanly.

### 2. Mobile Floating Action Button (FAB) navigation

A floating circular action button positioned in the lower-right corner to open navigation or creation menus was evaluated.
*Why not*: FABs persistently obscure underlying tabular and card content, frequently trigger accidental touches near submit buttons, and violate WCAG 2.2 target size and spacing criteria when rendered in complex layouts. A fixed, structured bottom bar (`BottomBar.svelte`) provides predictable, accessible thumb targets.

### 3. Accordion-based instance detail page

Collapsing instance policy, queue, activity, and memory into expandable accordion panels on a single `/instances/[id]` page was evaluated.
*Why not*: Single-page accordions destroy deep-linking capabilities (e.g. sharing a direct URL to an instance's failing queue), complicate browser back/forward history navigation, and degrade keyboard navigation when navigating long sections. Subtabs (`/instances/[id]/queue`) provide clear URLs, isolated data queries, and clean browser history.
