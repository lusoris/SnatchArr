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

### What the connection test names

The connection test of an instance with the kind `readarr` says which product answered.
Most forks still report the name `Readarr`, so the test reads three fields of
`/api/v1/system/status`. The first row that matches decides; upper and lower case make no
difference.

| `appName` | `packageAuthor` | `version` | Named as |
| --- | --- | --- | --- |
| `Chaptarr` | any | any | Chaptarr |
| not `Readarr` | any | any | not recognised |
| `Readarr` | contains `pennydreadful` | any | Bookshelf |
| `Readarr` | contains `Faustvii` | any | Readarr (Faustvii fork) |
| `Readarr` | anything else | 0.4.19 or lower | Readarr |
| `Readarr` | anything else | higher, or not a version | not recognised |

The third fork in the table is [Faustvii/Readarr](https://github.com/Faustvii/Readarr),
last commit 2026-09-02. The retired Readarr never built a version above 0.4.19, which is
what the last two rows rest on.

Two limits follow from the fields:

- `packageAuthor` is written by whoever builds the image. Bookshelf or the Faustvii fork
  from a build without that field is named "not recognised".
- A fork that reports `Readarr`, a version up to 0.4.19 and no known packager cannot be
  told from Readarr.

A name is not a statement of support: the test still succeeds for every row, and the first
paragraph of this section holds. The rule is `readarrProduct` in
`api/internal/arrclient/product.go`; `TestProbe_Product` in the same directory holds the
answers each product gave on 2026-10-11. The API carries the name as `product` of
`ProbeResult` (`api/openapi/openapi.yaml`).
