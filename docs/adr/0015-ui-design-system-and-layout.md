<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0015: SnatchArr's own UI: palette A, the M3 mark, a task-flow navigation and a UI that never blocks

- Status: accepted
- Date: 2026-10-19
- Milestone: [UI and design](https://github.com/lusoris/SnatchArr/milestone/11), issues #417 to #447

## Context

The web UI started from the newtarr template: twelve flat sidebar links, an inline create
form under every list, one long scrolling page per instance, neutral greys, and a root
layout that renders nothing but "Loading" until the session request returns
(`web/src/routes/+layout.svelte`, the `ready` flag). An audit on 2026-10-19 filed 31
issues against it (milestone 11): contrast failures (#417 to #419), phone overflow (#420
to #427, #441), missing empty states and confirmations (#428 to #433), error handling
(#436, #437), accessibility (#438, #439) and structure (#442 to #447).

Per operator direction (paraphrased), newtarr was only the starting template. SnatchArr
gets its own information architecture, navigation and page layouts, derived from what its
users do. Two design rounds were compared on screenshots. The operator picked palette A
("Night Prowler", violet, hue 285) and the M3 mark (a vesica, the almond shape where two
circles overlap). Page pattern a was also picked: instance detail as subtabs, a "+ Add"
toolbar that opens a dialog, the brand mark on `/login`, and the sveltesentio `DataTable`
on every major list. Two further directions bind every UI round. The UI never blocks
input while it waits for the API. It also follows current Svelte 5 and SvelteKit idioms,
checked against the docs of the installed versions.

The design evidence (task flows, route map, before and after screenshots at 1280x800 and
375x812, light and dark, axe results) is a local record of the design lane run
`snatcharr-ui-ia2`; this ADR carries its conclusions.

## Decision

### 1. Rules every UI change follows

| Id | Rule | Checked by |
| :--- | :--- | :--- |
| A11Y1 | Target EN 301 549 V4.1.1; the legal floor is V3.2.1, which references WCAG 2.1 AA, and SnatchArr meets WCAG 2.2 AA. axe-core runs on every route in CI with zero violations allowed. Each new or restructured page gets a keyboard pass (tab order, visible focus, Escape closes overlays, focus returns to the opener) and a screen-reader pass (landmarks, names, live regions), recorded in its pull request. | `web/e2e/a11y.test.ts` in `make web-e2e`; pull request checklist |
| UIF1 | Never freeze. No computation on the UI thread beyond one frame's work; every API call is async with a deadline and a visible state (skeleton, pending, error with retry). A test delays the API by 10 s and then answers 503: navigation and form inputs stay usable throughout. A planted blocking `await` in the root layout must turn that test red. The input-latency budget (section 5) is measured and recorded. | `web/e2e/nonblocking.test.ts` (added with the first implementation round) |
| TOK1 | Colours come only from the palette preset in `@sveltesentio/ui/presets` (`presetSnatchArr`), generated into `web/src/lib/theme.css` by `web/scripts/gen-theme.ts`. No hex and no literal oklch values in components or route files; Tailwind palette classes such as `text-red-600` are replaced by token classes (#440). The preset moves into the shared W3C design tokens file when sveltesentio cuts it. | `web/src/lib/theme-contrast.test.ts`; sveltesentio `packages/ui/test/palette.test.ts` |
| RS-svelte-ai-tools | The 2026-10-16 Svelte tooling evaluation is read before choosing a library. Component work is checked with the official Svelte MCP server (`@sveltejs/mcp`, autofixer and live docs). The two picks it named for SnatchArr are not adopted: `@workflow/sveltekit` and `@sentry/sveltekit` both need a SvelteKit server or `hooks.server.ts`, which this static SPA does not have (ADR-0005). Background work stays in the Go API and the worker; errors reach the user as RFC 9457 problems. | review |

### 2. Palette and brand

- Palette A in light and dark, as `presetSnatchArr` in golusoris/sveltesentio
  (`packages/ui/src/presets`, beside the sizing presets of sveltesentio ADR-0047). The
  sveltesentio test checks every text pair at 4.5:1 and every non-text pair at 3:1
  (WCAG 2.2 1.4.3 and 1.4.11), and a planted low-contrast pair fails it.
- The M3 mark is one SVG that uses `currentColor`, so it follows the theme. That fixes
  the invisible mark on the dark navigation (#443). It appears in the sidebar header,
  on `/login` and `/setup` (#446), in empty states, and as the favicon (SVG plus 32 px PNG
  and 180 px Apple touch icon).
- Wordmark: system sans, weight 600, letter spacing -0.01em.

### 3. Information architecture and navigation

The navigation is grouped by what the user is doing, most frequent first:

| Group | Entries | Task flow |
| :--- | :--- | :--- |
| Fleet | Dashboard `/`, Instances `/instances` | see fleet health and stamina; add, tune and inspect *arr instances |
| Activity | History `/history`, Live `/live` | audit runs; watch dispatches as they happen |
| Automation | Schedules `/schedules` | pause windows and cap changes |
| Integrations | Download clients, Seerr, Prowlarr, Configarr | connect the systems around the *arr apps |
| System | Settings `/settings`, Logs `/logs` (admins only) | notifications, retention, diagnostics |
| Footer | Account `/account`, language, log out | personal settings, out of the task list |

- Every entry carries an icon through `@sveltesentio/ui/icons` (Lucide set via the
  `@lucide/svelte` peer), never inline SVG copies.
- Desktop (1024 px and wider): fixed 240 px sidebar with group labels; the active entry
  gets a tinted background and a 3 px brand-colour left border.
- Below 1024 px: a top bar with the mark, the name and a menu button; the navigation
  opens as a modal drawer (focus trapped, Escape and backdrop close it, focus returns to
  the menu button). The page content starts on the first screen (#442).
- Every route keeps its URL. `/account` moves from the main list to the footer.

### 4. Page layouts

- Content column: max width 80rem, 1.5 rem gutter (1 rem on phones), 8 px spacing grid.
- Header band on every page: title, one-line description, and the page's primary action
  on the right ("+ Add instance", "+ Add schedule", ...).
- Create flows open a dialog from that action (#445). The dialog is a native modal
  `<dialog>` opened with `showModal()`, or `@sveltesentio/ui/dialog`, which wraps the
  same behaviour: backdrop, inert page, focus moved into the dialog, focus back to the
  opener on close. An inline panel that overlaps the page is not a dialog and fails review.
- Major lists use `@sveltesentio/ui/data/table` (#447): instances, history, schedules,
  download clients, Seerr requests, notification channels and rules, API keys, logs.
  Wide tables scroll inside their card; the page never scrolls sideways.
- Instance detail is split into subtabs that are child routes (#444):
  `/instances/[id]` (overview and edit), `/instances/[id]/policy`, `/instances/[id]/queue`,
  `/instances/[id]/activity`, `/instances/[id]/memory`. Child routes give each tab a
  deep link, working back and forward buttons and its own code chunk; the static
  adapter's fallback page serves them like any other route. An unknown id shows a
  not-found state (#433).
- Dashboard: cards in a grid, one column on phones, two from 768 px, three from 1280 px.
- Every list has an empty state with the mark artwork, one sentence and the primary
  action (#431, #432). Every delete asks for confirmation (#428 to #430).
- Errors render as `ProblemNote` with a retry action (#437), in the region that failed,
  never as a full-page replacement (#436).

### 5. A UI that never blocks

Installed versions (from `web/pnpm-lock.yaml` on 2026-10-19), against whose docs these
idioms were checked: svelte 5.57.2, @sveltejs/kit 3.0.1, @sveltejs/adapter-static
4.0.0, @tanstack/svelte-query 6.3.2, @sveltesentio/ui 0.7.0, @sveltesentio/query 0.5.0,
@playwright/test 1.64.0, @axe-core/playwright 4.13.0.

- The shell (sidebar or top bar, header band, content frame) renders on the first frame.
  The root layout's `ready` gate goes. The session request runs in the background; until
  it answers, data regions show skeletons and no protected data is fetched. A 401 sends
  the user to `/login`. No authentication hint is stored in `localStorage`: the session
  cookie stays the only source of truth.
- Server state goes through TanStack Query (`@sveltesentio/query`): cached data renders
  at once and refreshes in the background; first loads show a skeleton of the region's
  shape. Mutations update the cache optimistically, roll back on error, and report the
  outcome as a toast (`@sveltesentio/ui/toast`).
- Long work (instance probe, Configarr import, run dispatch) is a background request with
  a pending state on the button that started it and a toast when it ends; SSE events
  update the views that show it.
- Each data region sits in a `<svelte:boundary>` with a `failed` snippet, so a render
  error replaces that region only (Svelte docs, `svelte-boundary`).
- Not adopted: Svelte's `await` in markup and `$derived`. It still needs the
  `experimental.async` compiler flag, and its details may change outside a semver major
  release (Svelte docs, `await-expressions`). Revisit when the flag is removed.
- Not available: SvelteKit form actions and remote functions. Both run on a SvelteKit
  server, which an `adapter-static` SPA does not deploy (SvelteKit docs,
  `single-page-apps`). Forms submit through query mutations to the Go API.
- Budgets: Lighthouse CI on the built SPA, Total Blocking Time at most 150 ms, Cumulative
  Layout Shift at most 0.05 and Largest Contentful Paint at most 1.2 s. A Lighthouse
  navigation run has no interactions and so reports no INP. web.dev names TBT as the lab
  stand-in. INP is measured directly in Playwright during scripted flows (nav click,
  dialog open, form typing) through the Event Timing API: the worst interaction must
  stay at or under 200 ms, web.dev's "good" threshold, and the measured values are
  recorded in the pull request.

### 6. What stays and what goes from the newtarr template

| Part | Decision | Reason |
| :--- | :--- | :--- |
| Flat sidebar in `+layout.svelte` | replace | grouped navigation with icons and a phone drawer (section 3) |
| `ready` loading gate in `+layout.svelte` | replace | blocks the whole UI on one request (section 5) |
| Language select and log out in the middle of the nav | replace | moved to the sidebar footer |
| Inline create forms under lists (instances, schedules, download clients, Seerr links, notification channels and rules) | replace | "+ Add" dialog (section 4) |
| Hand-written tables in route files and `RunList`, `ScheduleTable`, `PreviewPanel` | replace | `DataTable` with a scroll container (section 4) |
| `InstanceDetail.svelte` as one long page | replace | child-route subtabs; its sections become one component per tab |
| Neutral grey theme in `gen-theme.ts` | replace | palette A preset (TOK1) |
| Duplicate change-password form on `/settings` (#434) | replace | `/account` keeps the only copy |
| Raw Tailwind colours on `/live` and `/logs` (#440) | replace | token classes (TOK1) |
| `/login`, `/setup` plain forms | keep the forms, replace the frame | centred card with the mark (#446) |
| `FormCard`, `Field`, `TextField`, `SecretField`, `KindSelect`, `InstanceSelect` | keep | already token-based and labelled; reused inside dialogs; hand-rolled forms move onto them (#435) |
| `ConfirmDelete`, `ConfirmAction` | keep | in-place confirm step beside the action; added where deletes lack it (#428 to #430) |
| `ProblemNote` | keep, extend | RFC 9457 display; gains a retry action (#437) |
| `CheckState`, `StaminaGauge` | keep, fix | fit the new cards; gauge colours move to tokens (#418) |
| `PolicyFilters`, `QueuePolicy`, `QueueStrikes`, `ResetMemory`, `LinkForm`, `SeerrLinkForm`, `DownloadClientForm`, `InstanceEditForm`, `ScheduleForm` | keep | domain forms; they move into the policy, queue and memory subtabs or into create dialogs unchanged |
| `ApiKeyDialog` on `/account` | keep | already a native modal dialog; the model for section 4 |
| `session.svelte.ts` | keep, change | stays the session store; loses the blocking first refresh |
| Paraglide messages (en, de) | keep | every new string goes through them |

### 7. How implementation rounds are checked

Each round closes milestone issues and shows, in its pull request:

- Playwright screenshots before and after at 1280x800 and at phone width (390x844),
  light and dark, wherever navigation or layout visibly changes.
- axe at zero violations on every touched route, and the keyboard and screen-reader
  passes of A11Y1.
- For the round that removes the `ready` gate: `nonblocking.test.ts` red on the old
  layout and green on the new one; later rounds keep it green.

## Consequences

- SnatchArr pins a released `@sveltesentio/ui` that contains `presetSnatchArr`. Until
  that release exists, `gen-theme.ts` keeps its current tokens, and no round adds local
  colour values in the meantime.
- Lighthouse CI and the INP measurement add one CI job; its runtime is part of the
  first implementation round's pull request.
- Child-route subtabs change the instance detail URLs; the old `/instances/[id]` URL
  still opens the overview tab, so existing links keep working.
- The not-adopted items (`@workflow/sveltekit`, `@sentry/sveltekit`, Svelte async
  `await`) are revisited if SnatchArr ever gains a SvelteKit server or Svelte removes
  the experimental flag.
