<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Roadmap

This page lists the planned work after 0.6, one version milestone at a time. Every open issue sits in exactly one
version milestone, and the order inside a milestone is the blocking order: an item waits on the items above it.
The milestones on GitHub are the current state; this page is rendered from them.

## Milestones

| Milestone | Scope | Open issues |
| :--- | :--- | ---: |
| [0.7](https://github.com/lusoris/SnatchArr/milestone/12) | Jellyfin sign-in as the only user path, then reported problems, then search and priorities. | 21 |
| [0.8](https://github.com/lusoris/SnatchArr/milestone/13) | Settings and the Live page, UI consistency, brand, code health, dependencies and deployment. | 20 |
| [0.9](https://github.com/lusoris/SnatchArr/milestone/14) | Module switches, Jellyfin-facing API surfaces, community requests, the request portal and the remaining research items. | 16 |
| [0.10](https://github.com/lusoris/SnatchArr/milestone/15) | The adult module: identity model, Stash integration, image sets, matching and adult requests. | 8 |
| [0.11](https://github.com/lusoris/SnatchArr/milestone/16) | LLM integration. | 3 |
| [0.12](https://github.com/lusoris/SnatchArr/milestone/17) | The Jellyfin companion plugin and every SnatchArr function surface inside Jellyfin. | 26 |

The [milestone list](https://github.com/lusoris/SnatchArr/milestones) shows every milestone with its open and closed issues.

## 0.7

Jellyfin sign-in as the only user path, then reported problems, then search and priorities.

1. [#681](https://github.com/lusoris/SnatchArr/issues/681): Jellyfin sign-in as the only user path on the login page
1. [#682](https://github.com/lusoris/SnatchArr/issues/682): Jellyfin default role setting and admin mapping
1. [#683](https://github.com/lusoris/SnatchArr/issues/683): Link a second sign-in identity from the account page
1. [#684](https://github.com/lusoris/SnatchArr/issues/684): Map Seerr requests to the requesting Jellyfin user
1. [#685](https://github.com/lusoris/SnatchArr/issues/685): Sign-in for any adopter without a reverse proxy or identity provider
1. [#705](https://github.com/lusoris/SnatchArr/issues/705): Jellyfin surface: API keys
1. [#686](https://github.com/lusoris/SnatchArr/issues/686): Close out stale active units in the plan graph with evidence
1. [#493](https://github.com/lusoris/SnatchArr/issues/493): Security Scan fails on main
1. [#522](https://github.com/lusoris/SnatchArr/issues/522): test(httpapi): race tests hit auth deadlines on a heavily loaded host
1. [#428](https://github.com/lusoris/SnatchArr/issues/428): ui: /instances: Delete removes an instance without confirmation
1. [#429](https://github.com/lusoris/SnatchArr/issues/429): ui: /settings: Delete removes a notification channel without confirmation
1. [#430](https://github.com/lusoris/SnatchArr/issues/430): ui: /settings: Delete removes a notification rule without confirmation
1. [#436](https://github.com/lusoris/SnatchArr/issues/436): ui: Dashboard hides load errors of capabilities and Prowlarr
1. [#438](https://github.com/lusoris/SnatchArr/issues/438): ui: /settings: enabled column is read as a bare symbol by screen readers
1. [#439](https://github.com/lusoris/SnatchArr/issues/439): ui: /account: password field errors are not linked to their inputs
1. [#641](https://github.com/lusoris/SnatchArr/issues/641): fix(arrclient): readarr instance on 11.3.1.0 detected as unknown product
1. [#687](https://github.com/lusoris/SnatchArr/issues/687): Queue handling end-to-end tests across every *arr kind
1. [#568](https://github.com/lusoris/SnatchArr/issues/568): [EPIC] Search and priorities (0.7)
1. [#558](https://github.com/lusoris/SnatchArr/issues/558): Choose the search shape per *arr family: season packs, seasons, episode groups or episodes
1. [#559](https://github.com/lusoris/SnatchArr/issues/559): Instance priority: named presets and separate search and download orders
1. [#560](https://github.com/lusoris/SnatchArr/issues/560): Apply instance priority inside download-client queues

## 0.8

Settings and the Live page, UI consistency, brand, code health, dependencies and deployment.

1. [#569](https://github.com/lusoris/SnatchArr/issues/569): [EPIC] Settings and Live page (0.7)
1. [#561](https://github.com/lusoris/SnatchArr/issues/561): Settings: every API setting in the UI, with plain labels, name pickers and presets
1. [#562](https://github.com/lusoris/SnatchArr/issues/562): Live page: confirm backlog on open, empty state and live delivery on a served build
1. [#431](https://github.com/lusoris/SnatchArr/issues/431): ui: /instances: no empty state when no instance exists
1. [#432](https://github.com/lusoris/SnatchArr/issues/432): ui: /live: no empty state before the first event
1. [#434](https://github.com/lusoris/SnatchArr/issues/434): ui: /settings duplicates the change-password form of /account
1. [#435](https://github.com/lusoris/SnatchArr/issues/435): ui: Hand-rolled forms bypass FormCard
1. [#437](https://github.com/lusoris/SnatchArr/issues/437): ui: ProblemNote has no retry action
1. [#440](https://github.com/lusoris/SnatchArr/issues/440): ui: /live and /logs use raw Tailwind colours instead of tokens
1. [#445](https://github.com/lusoris/SnatchArr/issues/445): ui: "+ Add" toolbar with a dialog instead of inline create forms
1. [#447](https://github.com/lusoris/SnatchArr/issues/447): ui: sveltesentio DataTable on all major lists
1. [#688](https://github.com/lusoris/SnatchArr/issues/688): Brand marks, palette candidates and README screenshots
1. [#689](https://github.com/lusoris/SnatchArr/issues/689): Milestone and roadmap replan
1. [#147](https://github.com/lusoris/SnatchArr/issues/147): CI: draft pull requests still run every component job
1. [#690](https://github.com/lusoris/SnatchArr/issues/690): Code health: reuse rows, gap audit and cross-repository dedupe
1. [#691](https://github.com/lusoris/SnatchArr/issues/691): Reduce code clones across web, worker and tools
1. [#55](https://github.com/lusoris/SnatchArr/issues/55): Dependency Dashboard
1. [#148](https://github.com/lusoris/SnatchArr/issues/148): Remove the workarounds that wait on upstream fixes
1. [#149](https://github.com/lusoris/SnatchArr/issues/149): Adopt the six open major dependency updates, SvelteKit 3 first
1. [#692](https://github.com/lusoris/SnatchArr/issues/692): Deployment: chart rollout and cluster live proof

## 0.9

Module switches, Jellyfin-facing API surfaces, community requests, the request portal and the remaining research items.

1. [#570](https://github.com/lusoris/SnatchArr/issues/570): [EPIC] Optional modules (0.8 and 0.9)
1. [#693](https://github.com/lusoris/SnatchArr/issues/693): Community requests with voting and admin approval rules
1. [#731](https://github.com/lusoris/SnatchArr/issues/731): Register requests module switch and schema migrations
1. [#732](https://github.com/lusoris/SnatchArr/issues/732): Implement deterministic approval rule and quota engine
1. [#733](https://github.com/lusoris/SnatchArr/issues/733): Add OpenAPI endpoints for submitting, listing, and voting on requests
1. [#734](https://github.com/lusoris/SnatchArr/issues/734): Add handoff adapters for music, books, comics, and games wishlist
1. [#735](https://github.com/lusoris/SnatchArr/issues/735): Extend notification engine with community request lifecycle events
1. [#736](https://github.com/lusoris/SnatchArr/issues/736): Add community requests and voting view in SnatchArr SPA
1. [#565](https://github.com/lusoris/SnatchArr/issues/565): Request portal module for media types Seerr does not cover
1. [#694](https://github.com/lusoris/SnatchArr/issues/694): Watch-aware modes: season gate observe run, then remove-when-seen
1. [#695](https://github.com/lusoris/SnatchArr/issues/695): Product rename candidates
1. [#696](https://github.com/lusoris/SnatchArr/issues/696): Foreign-title matching and Sonarr push-title import check
1. [#574](https://github.com/lusoris/SnatchArr/issues/574): Re-review the five dated standards exceptions before 2027-01-06
1. [#575](https://github.com/lusoris/SnatchArr/issues/575): Re-decide ADR-0012 when its queue-management bounds change
1. [#697](https://github.com/lusoris/SnatchArr/issues/697): Watch the re-decide triggers of ADR-0010 and ADR-0015
1. [#698](https://github.com/lusoris/SnatchArr/issues/698): CUE sheet splitter

## 0.10

The adult module: identity model, Stash integration, image sets, matching and adult requests.

1. [#566](https://github.com/lusoris/SnatchArr/issues/566): Adult content module: stash-box sync, favourites, scene image sets and Jellyfin integration
1. [#737](https://github.com/lusoris/SnatchArr/issues/737): Add adult scene, performer, and studio request kinds and Whisparr handoff
1. [#738](https://github.com/lusoris/SnatchArr/issues/738): Isolate adult request visibility, voting, and sanitized notifications
1. [#699](https://github.com/lusoris/SnatchArr/issues/699): Adult request kind on the community request model
1. [#700](https://github.com/lusoris/SnatchArr/issues/700): Adult identity model: implementation slices of ADR-0033
1. [#701](https://github.com/lusoris/SnatchArr/issues/701): Stash integration slices of ADR-0032
1. [#702](https://github.com/lusoris/SnatchArr/issues/702): Image-set engine follow-ups after #679
1. [#703](https://github.com/lusoris/SnatchArr/issues/703): Matching engine slices of ADR-0034

## 0.11

LLM integration.

1. [#571](https://github.com/lusoris/SnatchArr/issues/571): [EPIC] LLM integration (0.9)
1. [#564](https://github.com/lusoris/SnatchArr/issues/564): LLM access module: agent skills bundle, llms.txt and per-client connect guides
1. [#730](https://github.com/lusoris/SnatchArr/issues/730): Jellyfin surface: LLM and MCP tool surface

## 0.12

The Jellyfin companion plugin and every SnatchArr function surface inside Jellyfin.

1. [#567](https://github.com/lusoris/SnatchArr/issues/567): ADR: Jellyfin companion plugin
1. [#572](https://github.com/lusoris/SnatchArr/issues/572): [EPIC] Jellyfin companion plugin
1. [#706](https://github.com/lusoris/SnatchArr/issues/706): Jellyfin surface: Search a missing item and manage runs
1. [#707](https://github.com/lusoris/SnatchArr/issues/707): Jellyfin surface: Snatch activity, hourly caps, and processed memory
1. [#708](https://github.com/lusoris/SnatchArr/issues/708): Jellyfin surface: Seerr request sync and dispatch
1. [#709](https://github.com/lusoris/SnatchArr/issues/709): Jellyfin surface: Seerr connection settings
1. [#710](https://github.com/lusoris/SnatchArr/issues/710): Jellyfin surface: Instance settings
1. [#711](https://github.com/lusoris/SnatchArr/issues/711): Jellyfin surface: Snatch policy and instance priority
1. [#712](https://github.com/lusoris/SnatchArr/issues/712): Jellyfin surface: Queue view and strike policy
1. [#713](https://github.com/lusoris/SnatchArr/issues/713): Jellyfin surface: Schedules
1. [#714](https://github.com/lusoris/SnatchArr/issues/714): Jellyfin surface: Notifications
1. [#715](https://github.com/lusoris/SnatchArr/issues/715): Jellyfin surface: Prowlarr indexer links and health
1. [#716](https://github.com/lusoris/SnatchArr/issues/716): Jellyfin surface: Download client health and connections
1. [#717](https://github.com/lusoris/SnatchArr/issues/717): Jellyfin surface: Configarr GitOps import
1. [#718](https://github.com/lusoris/SnatchArr/issues/718): Jellyfin surface: Watch-aware gate status and manual release
1. [#719](https://github.com/lusoris/SnatchArr/issues/719): Jellyfin surface: Watch-aware source settings
1. [#720](https://github.com/lusoris/SnatchArr/issues/720): Jellyfin surface: Module switches
1. [#721](https://github.com/lusoris/SnatchArr/issues/721): Jellyfin surface: System status, settings, and logs
1. [#722](https://github.com/lusoris/SnatchArr/issues/722): Jellyfin surface: Request a title (native request portal)
1. [#723](https://github.com/lusoris/SnatchArr/issues/723): Jellyfin surface: View my requests and approvals
1. [#724](https://github.com/lusoris/SnatchArr/issues/724): Jellyfin surface: Queue action (retry, remove, blocklist)
1. [#725](https://github.com/lusoris/SnatchArr/issues/725): Jellyfin surface: Watch-aware remove-when-seen policies
1. [#726](https://github.com/lusoris/SnatchArr/issues/726): Jellyfin surface: Adult favourites and subscriptions
1. [#727](https://github.com/lusoris/SnatchArr/issues/727): Jellyfin surface: Adult image-set galleries
1. [#728](https://github.com/lusoris/SnatchArr/issues/728): Jellyfin surface: Adult matching audit queue
1. [#729](https://github.com/lusoris/SnatchArr/issues/729): Jellyfin surface: Stash companion instance settings

## Alternatives

Until the watch-aware remove-when-seen mode ships, [Maintainerr](https://github.com/Maintainerr/Maintainerr)
removes watched media.
