<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0042: Brand mark choice (vulva, penis, hand) and SFW mode

- **Status:** Accepted
- **Date:** 2026-10-26
- **Extends:** [ADR-0017](0017-anatomical-brand-mark.md)
- **Related:** [ADR-0025](0025-module-switches.md), [ADR-0026](0026-accounts-and-roles.md), [ADR-0027](0027-adult-module.md), [ADR-0028](0028-jellyfin-plugin.md), [ADR-0039](0039-metadata-single-source.md)

## Context

[ADR-0017](0017-anatomical-brand-mark.md) established the anatomical vulva study (`web/static/brand/mark.svg` and `web/src/lib/components/BrandMark.svelte`) as SnatchArr's single brand mark across the web application, documentation site, favicons, and OpenGraph image. Since ADR-0017 was accepted, three product requirements have emerged:

1. **Safe-for-work (`SFW`) and general-audience surfaces**: Adopters running SnatchArr for general household media (**Radarr**, **Sonarr**, **Lidarr**, **Readarr**) with the `adult` module disabled ([ADR-0025](0025-module-switches.md)), or opening the web interface on shared displays, work laptops, or living-room screens, need an unintrusive, general-audience mark that still plays directly on the verb *snatch* (a hand snatching a media disc).
2. **Anatomical parity**: Where anatomical branding is active, offering a companion anatomical penis mark alongside the existing vulva mark lets each user or household choose the anatomical study they prefer while keeping the vulva mark as the default anatomical mark.
3. **External client surfaces**: The **Jellyfin** companion plugin ([ADR-0028](0028-jellyfin-plugin.md), `lusoris/jellyfin-plugin-snatcharr`) runs inside a general-audience media server shared by household members of all ages and parental-rating policies; embedding adult anatomical vector assets in the plugin binary or catalogue thumbnail is inappropriate for that distribution channel.

### Glossary of referenced projects (meaning, origin, pronunciation)

- **SnatchArr** (`/ˈsnætʃ.ɑːr/`): this repository's media acquisition and library controller; compounds English *snatch* (to grab a release from an indexer, and in British slang anatomical *snatch*, the double entendre behind [ADR-0017](0017-anatomical-brand-mark.md)) with the pirate-themed `-arr` suffix of the `*arr` ecosystem.
- **Radarr** (`/ˈreɪ.dɑːr/`), **Sonarr** (`/ˈsoʊ.nɑːr/`), **Lidarr** (`/ˈlaɪ.dɑːr/`), **Readarr** (`/ˈriː.dɑːr/`), **Whisparr** (`/ˈwɪs.pɑːr/`), **Prowlarr** (`/ˈpraʊ.lɑːr/`): the `*arr` suite managing movies, television series, music, books, adult video, and indexers; coined as puns on *radar*, *sonar*, *lidar*, *reader*, *whisper*, and *prowler* plus `-arr`.
- **Stash** (`/stæʃ/`): self-hosted metadata and streaming organizer for personal and adult video collections; from English *stash* (a private store).
- **Jellyfin** (`/ˈdʒɛl.i.fɪn/`): community-driven free-software media server forked from Emby in 2018; combines *jellyfish* and *fin*.
- **golusoris** (`/ˌɡoʊ.luːˈsɔː.rɪs/`), **goenvoy** (`/ˌɡoʊˈɛn.vɔɪ/`), **sveltesentio** (`/ˌsvɛltˈsɛn.ti.oʊ/`): `lusoris` shared libraries for Go `uber-go/fx` lifecycle gates (Go + Latin *lūsōris*, "of the player"), Go outbound HTTP/webhook clients (Go + *envoy*), and Svelte 5 accessible UI components (Svelte + Latin *sentiō*, "I perceive").
- **Paraglide** (`/ˈpær.ə.ɡlaɪd/`): compile-time internationalisation library from the Inlang project (`@inlang/paraglide-js`), named after the lightweight glider wing.

## Decision

### 1. Three brand marks: `vulva` (default), `penis`, and `hand` (SFW)

SnatchArr defines three brand marks sharing the [ADR-0017](0017-anatomical-brand-mark.md) visual grammar (`viewBox="0 0 100 100"`, rounded caps and joins, `oklch` four-token palette `--o`, `--b`, `--p`, `--s` with `@media (prefers-color-scheme: dark)` in standalone SVG and `.dark` CSS custom properties in `web/src/lib/theme.css`, plus a simplified 5-to-6-path cut rendered below `40 px`):

| Mark id | Category | Role |
| :--- | :--- | :--- |
| `vulva` | Anatomical (default) | Existing anatomical vulva study from [ADR-0017](0017-anatomical-brand-mark.md); kept unchanged as the default anatomical mark. |
| `penis` | Anatomical | Companion anatomical penis and scrotum study in the same line weight, groin framing (`--b`), glans fill (`--p`), and meatus shadow gradient (`url(#s)`). |
| `hand` | Safe-for-work (`SFW`) | Hand snatching a media disc; literal visual pun on *snatch* ("grab a release"). Shown whenever `SFW` mode is active, when `hand` is explicitly selected, and unconditionally in the **Jellyfin** companion plugin ([ADR-0028](0028-jellyfin-plugin.md)). |

### 2. Candidate variants for operator selection

All candidate SVGs live in `.workingdir/marks/candidates/<name>.svg` (full detail for `>= 40 px`) and `.workingdir/marks/candidates/<name>-cut.svg` (simplified cut for `< 40 px`), with rendered `16 px`, `32 px`, and `180 px` light and dark PNGs and contact sheets in `.workingdir/evidence/marks-candidates-20261026/`. The operator picked `hand-snatch` (`OQ-0042-1`) and `penis-upright` (`OQ-0042-2`); section 7 records the result.

#### 2.1 SFW `hand` candidates (3 variants)

| Candidate | Base work & provenance | Licence | Changes from base |
| :--- | :--- | :--- | :--- |
| `hand-snatch` | Derived from [`File:Hand_reaching_down_Pinhead_icon.svg`](https://commons.wikimedia.org/wiki/File:Hand_reaching_down_Pinhead_icon.svg) on Wikimedia Commons (author: **Quincy Morgan** / `User:Quincylvania`, upstream `waysidemapping/pinhead` v12.0.0; licence: **CC0 1.0** public-domain dedication; uploaded **2026-03-06**, verified on the Commons file page 2026-10-26). | `CC-BY-SA-4.0` (derived from `CC0-1.0` base, copyright `2026 Quincy Morgan`, `2026 lusoris`) | Converted the solid black filled hand silhouette (`256 x 256` viewBox) into open anatomical outline strokes (`--o`) and wrist cuff/crease strokes (`--b`) in `0 0 100 100`, added inter-digital creases, placed a media disc (`fill="var(--p)"` outer ring + `fill="url(#s)"` inner spindle core) beneath the reaching thumb and fingers with motion arcs (`--b`), and created the simplified `< 40 px` cut (`hand-snatch-cut.svg`). |
| `hand-pinch` | Own vector line drawing by `lusoris` (checked against the anatomical gesture of [`File:Icon-grabbing-hand.svg`](https://commons.wikimedia.org/wiki/File:Icon-grabbing-hand.svg), author: **Oleksandr Panasovskyi**, licence: **CC BY 3.0**, uploaded **2024-10-20**, verified on the Commons file page 2026-10-26; drawn from scratch as clean cubic Bézier centerlines). | `CC-BY-SA-4.0` (`2026 lusoris`) | Side-profile hand entering from upper right (`--b` wrist contours, `--o` index, middle, ring, and opposed thumb contours) pinching and lifting a media disc (`--p` ring, `url(#s)` spindle core) at lower left, with a simplified `< 40 px` cut (`hand-pinch-cut.svg`). |
| `hand-claw` | Own vector line drawing by `lusoris`. | `CC-BY-SA-4.0` (`2026 lusoris`) | Symmetrical overhead claw-grip hand descending vertically from top wrist contours (`--b`) with arched thumb and outer finger (`--o`) and two central curled fingers poised over a media disc (`--p` ring, `url(#s)` core) and lift arcs (`--b`), with a simplified `< 40 px` cut (`hand-claw-cut.svg`). |

#### 2.2 Anatomical `penis` candidates (2 variants)

| Candidate | Base work & provenance | Licence | Changes from base |
| :--- | :--- | :--- | :--- |
| `penis-kebert` | Derived from [`File:Male_reproductive_frontal_without_labels.svg`](https://commons.wikimedia.org/wiki/File:Male_reproductive_frontal_without_labels.svg) on Wikimedia Commons (author: **T. Kebert** / `User:Nefronus` — the same illustrator and same day as `File:Female_vulva.svg` used in [ADR-0017](0017-anatomical-brand-mark.md); licence: **CC BY-SA 4.0**; uploaded **2024-02-23** at `20:02 UTC`, verified on the Commons file page 2026-10-26). | `CC-BY-SA-4.0` (`2024 T. Kebert`, `2026 lusoris`) | Stripped all internal anatomical structures (urinary bladder, ureters, ductus deferens, seminal vesicles, prostate, bulbourethral glands, internal corpora cavernosa cross-section, and dashed subcutis lines); isolated the external groin and thigh contours (`path48`/`path49`, `--b`), external scrotal contour (`path44`/`path45`, `--o`), penile shaft (`path41`, `--o`), prepuce/coronal sulcus folds (`path42`/`path43`, `--o`), glans penis (`fill="var(--p)"`, `stroke="var(--o)"`), and urethral meatus (`fill="url(#s)"`, `stroke="var(--o)"`); normalised into `0 0 100 100`, widened strokes to `1.8`–`2.4` (`3.0`–`3.2` in `penis-kebert-cut.svg`), and applied the ADR-0017 `oklch` palette. |
| `penis-upright` | Own anatomical vector study by `lusoris`, composed as a direct ventral upright counterpart to `mark.svg` ([ADR-0017](0017-anatomical-brand-mark.md)). | `CC-BY-SA-4.0` (`2026 lusoris`) | Frames an upright ventral study of the glans (`fill="var(--p)"`), urethral meatus (`fill="url(#s)"`), frenulum delta, corpus spongiosum / median raphe lines, and asymmetrically hanging scrotum (`--o`) inside the same curved groin/thigh contours (`--b`) as the vulva mark, plus a simplified `< 40 px` cut (`penis-upright-cut.svg`). |

#### 2.3 Rendering method for candidate evidence

`rsvg-convert` (`librsvg` 2.63.4) does not evaluate CSS custom properties (`var(--o)`) or CSS Color 4 `oklch(...)` tokens inside an SVG `<style>` block (rendering unresolved variables as black `#000000`). To produce deterministic light and dark PNGs at `16 px` (`*-cut.svg`), `32 px` (`*-cut.svg`), and `180 px` (`*.svg`) matching the browser's `prefers-color-scheme: light` and `prefers-color-scheme: dark` rendering:

- **Light render (`*-light.png`)**: resolves `:root` `oklch` tokens to sRGB hex (`--o: #6b1d26`, `--b: #61370d`, `--p: #ba5661`, `--s: #3d0813`) and passes `-b #fafafa` (`oklch(0.985 0 0)` white background) to `rsvg-convert`.
- **Dark render (`*-dark.png`)**: resolves `@media (prefers-color-scheme: dark) { :root { ... } }` tokens to sRGB hex (`--o: #eab3b6`, `--b: #ccb195`, `--p: #e2808d`, `--s: #2f020b`) and passes `-b #080717` (`oklch(0.14 0.035 285)` from `web/src/lib/theme.css` `.dark { --color-background: oklch(0.14 0.035 285); }`) to `rsvg-convert`.

### 3. Palette and CSS tokens

All three marks share the exact [ADR-0017](0017-anatomical-brand-mark.md) token contract in `web/src/lib/theme.css` and standalone SVG `<style>` blocks:

| Token | Standalone SVG variable | Light (`:root`) | Dark (`.dark` / `prefers-color-scheme: dark`) | Role across `vulva` / `penis` / `hand` |
| :--- | :--- | :--- | :--- | :--- |
| `--brand-mark-outline` | `--o` | `oklch(0.28 0.09 15)` | `oklch(0.82 0.06 12)` | Primary anatomical or hand/disc line art |
| `--brand-mark-body` | `--b` | `oklch(0.32 0.05 55)` | `oklch(0.78 0.04 55)` | Groin/thigh framing contours (`vulva`, `penis`) or wrist/motion contours (`hand`) |
| `--brand-mark-primary` | `--p` | `oklch(0.55 0.14 15)` | `oklch(0.70 0.14 12)` | Glans clitoris (`vulva`), glans penis (`penis`), or snatched media disc body (`hand`) |
| `--brand-mark-shadow` | `--s` | `oklch(0.18 0.07 15)` | `oklch(0.14 0.06 15)` | Introitus gradient (`vulva`), urethral meatus gradient (`penis`), or disc spindle core gradient (`hand`) |

### 4. User setting and relation between SFW mode and the `adult` module switch ([ADR-0025](0025-module-switches.md))

#### 4.1 Settings model

Two preferences govern which mark renders in the web UI:

- `brand_mark`: `'vulva' | 'penis' | 'hand'` (default `'vulva'`). Selects the preferred mark when `SFW` mode is not overriding it.
- `sfw_mode`: controls whether anatomical marks (`vulva`, `penis`) are replaced by the `hand` mark across the UI shell, login screen, and dynamic `<link rel="icon">` / document title badge.

#### 4.2 Options for coupling `sfw_mode` to the `adult` module switch ([ADR-0025](0025-module-switches.md))

Under [ADR-0025](0025-module-switches.md), the `adult` module (**Whisparr** + **Stash**) is off by default (`modules.adult.enabled = false`). Three options exist for how `sfw_mode` relates to `modules.adult.enabled`:

1. **Option a — Forced SFW when `adult` module is off**:
   - *Behaviour*: Whenever `modules.adult.enabled == false`, `SFW` mode is forced on (`hand` mark always shown; `vulva` and `penis` choices are hidden or disabled). When `modules.adult.enabled == true`, the user can pick `vulva`, `penis`, or `hand`.
   - *Risk*: Removes the default [ADR-0017](0017-anatomical-brand-mark.md) anatomical identity for adopters who only run **Radarr**/**Sonarr**/**Lidarr**/**Readarr** (no **Whisparr**/**Stash**) yet deliberately want SnatchArr's anatomical brand mark, and breaks existing single-module deployments that expect `vulva` as the default unless they enable an unused `adult` module.
2. **Option b — Independent toggle**:
   - *Behaviour*: `sfw_mode` (`boolean`, default `false`) and `brand_mark` (`'vulva' | 'penis' | 'hand'`, default `'vulva'`) are completely independent of `modules.adult.enabled`. Turning `sfw_mode` on forces `hand`; turning `sfw_mode` off displays `brand_mark` (`vulva` by default) regardless of whether `modules.adult.enabled` is `true` or `false`.
   - *Risk*: Fresh installs with `modules.adult.enabled = false` show the anatomical `vulva` mark on first boot until an administrator or user toggles `sfw_mode` on (or sets `APP_SNATCHARR_SFW_MODE=true`), which can surprise general-media-only households if they did not expect an anatomical mark when `adult` is off.
3. **Option c — Both (coupled `auto` default with explicit override, recommended)**:
   - *Behaviour*: `sfw_mode` is a tri-state setting `'auto' | 'on' | 'off'` (default `'auto'`), or equivalently a boolean toggle whose default tracks `!modules.adult.enabled` until explicitly overridden:
     - `sfw_mode = 'auto'`: effective `SFW` is `true` when `modules.adult.enabled == false` (or when the signed-in user lacks adult visibility under [ADR-0026](0026-accounts-and-roles.md) / [ADR-0027](0027-adult-module.md)), and `false` when `modules.adult.enabled == true` (displaying `brand_mark`, default `'vulva'`).
     - `sfw_mode = 'on'`: always shows the `hand` mark, even when `modules.adult.enabled == true` (useful for screen-sharing or shared household displays).
     - `sfw_mode = 'off'`: displays the chosen `brand_mark` (`'vulva'`, `'penis'`, or `'hand'`), even when `modules.adult.enabled == false` (preserving [ADR-0017](0017-anatomical-brand-mark.md) anatomical branding for general-media adopters who want it without enabling **Whisparr**/**Stash**).
   - *Risk*: Slightly richer state matrix (`3 x 3` effective combinations in unit tests) than a two-state boolean, mitigated by a single pure selector function `resolveEffectiveBrandMark(brandMark, sfwMode, adultModuleEnabled, userAdultAllowed) -> 'vulva' | 'penis' | 'hand'`.

- **Decision** (`OQ-0042-3`, answered c): **Option c (both — `auto` default coupled to the `adult` module switch with explicit `on` / `off` override)**. It gives general-audience installs (`modules.adult.enabled = false`) and non-adult household accounts a safe-for-work default out of the box without forcing **Whisparr**/**Stash** on operators who want the anatomical mark (`sfw_mode = 'off'`), while still letting `adult`-enabled operators switch `sfw_mode = 'on'` at any time.

### 5. Jellyfin companion plugin ([ADR-0028](0028-jellyfin-plugin.md)) ships `hand` only

The **Jellyfin** companion plugin (`lusoris/jellyfin-plugin-snatcharr`, [ADR-0028](0028-jellyfin-plugin.md)) embeds and serves **only the SFW `hand` mark**:

- The plugin catalogue thumbnail (`thumb.png` / `mark-hand.svg`) and any in-client badge inside **Jellyfin** web/TV clients never bundle `vulva` or `penis` SVG/PNG assets.
- Rationale: **Jellyfin** plugin catalogue pages and client script bundles are readable by all household profiles before per-user parental controls are evaluated.

### 6. Licence and `REUSE.toml` plan per file

Every mark asset and component carries an explicit SPDX header or `REUSE.toml` annotation so `reuse lint` passes with zero warnings:

| File path (when wired after operator pick) | Licence | Copyright holders | Attribution mechanism |
| :--- | :--- | :--- | :--- |
| `.workingdir/marks/candidates/*.svg` (current candidate stage, git-ignored) | `CC-BY-SA-4.0` | `2024 T. Kebert` (on `penis-kebert*`), `2026 Quincy Morgan` (on `hand-snatch*`), `2026 lusoris` | Inline XML comment with REUSE copyright and licence tags + derivation note |
| `web/src/lib/components/BrandMark.svelte` | `CC-BY-SA-4.0` | `2024 T. Kebert`, `2026 Quincy Morgan` (if `hand-snatch` picked), `2026 lusoris` | Top HTML comment block with REUSE copyright and licence tags + derivation notes for each mark variant |
| `web/static/brand/mark.svg` (`vulva`), `web/static/brand/mark-penis.svg`, `web/static/brand/mark-hand.svg` | `CC-BY-SA-4.0` | Respective base author + `2026 lusoris` | Top XML comment block in each `.svg` file + `[[annotations]]` entry in `REUSE.toml` |
| `docs/assets/mark.svg` (and optional `docs/assets/mark-hand.svg`, `docs/assets/mark-penis.svg`) | `CC-BY-SA-4.0` | Respective base author + `2026 lusoris` | Top XML comment block in each `.svg` file + `[[annotations]]` entry in `REUSE.toml` |
| `web/static/favicon-32x32.png`, `web/static/apple-touch-icon.png`, `docs/assets/og.png` | `CC-BY-SA-4.0` | Respective base author + `2026 lusoris` | `[[annotations]]` entry in `REUSE.toml` (binary PNGs have no inline comment) |
| `docs/adr/0042-brand-mark-choice-and-sfw-mode.md` | `CC-BY-SA-4.0` | `2026 lusoris <lusoris@pm.me>` | Inline HTML comment header + `docs/**` stanza in `REUSE.toml` |

Because `LICENSES/CC-BY-SA-4.0.txt` is already present in the repository and both `CC0-1.0` (`Hand_reaching_down_Pinhead_icon.svg`) and `CC-BY-SA-4.0` (`Male_reproductive_frontal_without_labels.svg`) permit licensing adapted works under `CC-BY-SA-4.0`, all three marks share a single uniform `CC-BY-SA-4.0` licence across `REUSE.toml` and the about page.

## Consequences

### Positive

- Gives general-audience households, shared screens, and the **Jellyfin** companion plugin ([ADR-0028](0028-jellyfin-plugin.md)) a distinctive, on-theme `hand` mark ("snatching a release") while keeping [ADR-0017](0017-anatomical-brand-mark.md)'s anatomical identity intact.
- Provides anatomical symmetry (`vulva` and `penis`) in the exact same `oklch` palette, stroke hierarchy, and `< 40 px` cut architecture.
- Keeps the runtime bundle cost negligible: all three marks are inline `<svg>` branches inside `BrandMark.svelte` (`< 4 KB` total uncompressed, zero external font or icon-pack dependency).

### Negative

- Adds a `mark` prop (`'vulva' | 'penis' | 'hand'`) to `BrandMark.svelte` and a user/instance preference lookup once wired.
- Static `<link rel="icon">` and `apple-touch-icon.png` in `web/src/app.html` must pick one build-time default (or switch `<link rel="icon">` dynamically at runtime once the session/config loads).

### 7. Picked marks and wiring

- `hand` = `hand-snatch`, derived from Quincy Morgan's `CC0-1.0` reaching hand. `CC0` asks for no credit; SnatchArr credits the base author anyway, in the SVG comment, `BrandMark.svelte` and the about page.
- `penis` = `penis-upright`, the own ventral study next to the vulva mark.
- `vulva` stays the default `brand_mark`.
- One pure selector, `resolveBrandMark(brandMark, sfwMode, adultModuleEnabled, userAdultAllowed)` in `web/src/lib/brand-mark.ts`, decides the effective mark; a table test covers every combination of its inputs.
- Until the session carries `adult_allowed` ([ADR-0026](0026-accounts-and-roles.md) section 4), the web passes `userAdultAllowed = false`, so `auto` fails closed to `hand`.
- `web/static/brand/mark-hand.svg` is the standalone hand asset; the Jellyfin plugin copies it.

## Answered questions

1. **OQ-0042-1** (`snatcharr-marks-pick-20261026`, answered a): `hand-snatch`.
2. **OQ-0042-2** (`snatcharr-marks-pick-20261026`, answered a): `penis-upright`.
3. **OQ-0042-3** (`snatcharr-marks-sfw-coupling`, answered c): tri-state `sfw_mode = 'auto' | 'on' | 'off'`, default `'auto'`: SFW when the `adult` module is off or the user lacks adult visibility; one pure selector with a table test over every state.
