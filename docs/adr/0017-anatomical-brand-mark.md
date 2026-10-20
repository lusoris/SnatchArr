<!-- markdownlint-disable MD013 -->
<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# ADR-0017: An anatomical vulva mark replaces the M3 vesica

- Status: accepted
- Date: 2026-10-20
- Milestone: [UI and design](https://github.com/lusoris/SnatchArr/milestone/11)
- Supersedes: the M3 mark bullet of ADR-0015 section 2, and "the M3 mark" in ADR-0016

## Context

The M3 mark of ADR-0015 is a vesica (an almond shape) with a dot and a short bar in its centre. The project owner asked for a mark that reads as a vulva at first sight; the M3 almond reads as an abstract shape with a candle or the letter i in the middle. Three drawn rounds were rejected, and the owner chose a published anatomical drawing as the base.

## Decision

- Base: "Female vulva.svg" by T. Kebert, [Wikimedia Commons](https://commons.wikimedia.org/wiki/File:Female_vulva.svg), CC BY-SA 4.0 (uploaded 2024-02-23).
- Changes: cropped to the vulva (pubic hair, thighs, perineum, anus and buttock lines removed); clitoral glans enlarged 1.8 times (2.4 times in the small cut); vaginal opening filled with a radial gradient from shadow to pink; outline, labia and fill recoloured in oklch; strokes widened for icon sizes.
- Small sizes: below 40 px `BrandMark.svelte` draws a simplified cut (outer and inner labia, enlarged glans, shaded opening) with thicker strokes. The 32 px favicon PNG uses the cut, the 180 px Apple touch icon the full drawing.
- Colour: the mark no longer follows `currentColor`. It carries a light set and a dark set (`html.dark` in the SPA, `prefers-color-scheme` in the standalone SVG files), so it stays visible on the dark navigation that #443 fixed.
- Licence: every file derived from the base is CC BY-SA 4.0 with the attribution "T. Kebert, Wikimedia Commons, CC BY-SA 4.0, modified", declared in `REUSE.toml` and checked by `reuse lint`. The visual baselines show the mark and carry the same licence.

## Consequences

- **Positive**: The mark says what the name means, at every size from 16 px up; the credit and licence are machine-checked.
- **Negative**: The mark files are share-alike (CC BY-SA 4.0), unlike the EUPL-1.2 code; anyone who adapts the mark must keep that licence and the credit. The mark is two-tone and no longer a single `currentColor` glyph.
