<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Readarr (legacy)

Readarr was retired by its maintainers on 2025-06-27 and its repository is archived
([announcement](https://github.com/Readarr/Readarr#readme)). SnatchArr still supports
Readarr instances. That support is frozen
([ADR-0008](adr/0008-readarr-frozen-legacy-target.md)).

## If you run Readarr today

Nothing changes. Add the instance with the kind `readarr`, or import it from Configarr,
as before. SnatchArr goes after missing books and cutoff-unmet upgrades, sends one search
per author, and respects stamina, afterglow and foreplay like for every other instance
([Concepts](concepts.md)).

The instance form, the instance list, the dashboard and the instance page show the kind as
`readarr (legacy)`. The form adds a one-line note when you choose it.

## What frozen means

| Kept | Not done |
| --- | --- |
| Everything Readarr support does in v0.1.5. | Features for Readarr alone. |
| Fixes for a regression in SnatchArr. The Readarr tests stay and must pass. | Fixes for a Readarr quirk that no supported fork shares. |
| The kind `readarr` in the API and in Configarr imports. No removal is scheduled. | Repairs to Readarr itself. SnatchArr sends search commands; it cannot fix what Readarr finds, for example when its metadata lookups fail. |

## Forks

No Readarr fork is a supported target yet. Two forks keep Readarr's v1 API layout,
including the wanted lists SnatchArr reads (checked 2026-10-11):

- [Bookshelf](https://github.com/pennydreadful/bookshelf), last commit 2026-09-11
- [Chaptarr](https://github.com/Chaptarr/chaptarr), last commit 2026-09-27

SnatchArr has not been tested against either. Adding one with the kind `readarr` is at
your own risk until [#22](https://github.com/lusoris/SnatchArr/issues/22) is closed. That
issue adds the forks that pass the compatibility tests as targets of their own and lists
the ones that fail here, with the reason.
