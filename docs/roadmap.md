<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Roadmap

This page lists the planned work after 0.6 and where to follow it. Each theme is an
epic issue on GitHub. Each epic lists its child issues, and each child issue says what
already ships and what is still missing. The issues and milestones on GitHub are the
current state; this page names them.

## Milestones

| Milestone | Scope | Epics |
| :--- | :--- | :--- |
| [0.7](https://github.com/lusoris/SnatchArr/milestone/12) | Search and priorities, Settings and the Live page. Reported problems come first. | [#568](https://github.com/lusoris/SnatchArr/issues/568), [#569](https://github.com/lusoris/SnatchArr/issues/569) |
| 0.8 | Module design and switches. | [#570](https://github.com/lusoris/SnatchArr/issues/570) |
| 0.9 | The first optional modules and the LLM access module. | [#570](https://github.com/lusoris/SnatchArr/issues/570), [#571](https://github.com/lusoris/SnatchArr/issues/571) |
| [Backlog](https://github.com/lusoris/SnatchArr/milestone/10) | Work without a version yet. | [#572](https://github.com/lusoris/SnatchArr/issues/572) |

The [milestone list](https://github.com/lusoris/SnatchArr/milestones) shows every
milestone with its open and closed issues.

## Epics

### Search and priorities (0.7)

Epic [#568](https://github.com/lusoris/SnatchArr/issues/568). Search smarter, and in
the order you set.

- [#558](https://github.com/lusoris/SnatchArr/issues/558): choose the search shape per
  \*arr family (season packs, seasons, episode groups or episodes).
- [#559](https://github.com/lusoris/SnatchArr/issues/559): instance priority with named
  presets and separate search and download orders
  ([ADR-0022](adr/0022-instance-priority.md)).
- [#560](https://github.com/lusoris/SnatchArr/issues/560): apply instance priority
  inside download-client queues.

### Settings and Live page (0.7)

Epic [#569](https://github.com/lusoris/SnatchArr/issues/569). Every setting reachable in
the UI in plain words, and a Live page that is useful as soon as it opens.

- [#561](https://github.com/lusoris/SnatchArr/issues/561): every API setting in the UI,
  with presets served by the API
  ([ADR-0020](adr/0020-snatch-policy-defaults-and-presets.md)).
- [#562](https://github.com/lusoris/SnatchArr/issues/562): confirm the Live page backlog,
  empty state and live delivery on a served build.
- The open findings of the 2026-10-19 frontend audit, listed in the epic.

### Optional modules (0.8 and 0.9)

Epic [#570](https://github.com/lusoris/SnatchArr/issues/570). The product architecture is
settled in [ADR-0018](adr/0018-product-front-door.md).

- [#563](https://github.com/lusoris/SnatchArr/issues/563) (0.8): one switch mechanism
  that turns every optional module on or off, with hard boundaries.
- [#565](https://github.com/lusoris/SnatchArr/issues/565) (0.9): a request portal module
  for media types Seerr does not cover.
- [#566](https://github.com/lusoris/SnatchArr/issues/566) (0.9): an adult content module
  (stash-box sync, favourites, scene image sets, Jellyfin integration).

Both modules are off by default.

### LLM integration (0.9)

Epic [#571](https://github.com/lusoris/SnatchArr/issues/571). The MCP server already
ships ([ADR-0021](adr/0021-llm-access-module.md)).

- [#564](https://github.com/lusoris/SnatchArr/issues/564): an agent skills bundle,
  `llms.txt` and connect guides for each client.

### Jellyfin companion plugin

Epic [#572](https://github.com/lusoris/SnatchArr/issues/572), in the backlog.

- [#567](https://github.com/lusoris/SnatchArr/issues/567): the ADR for the plugin.
  Implementation issues follow once it is accepted.
