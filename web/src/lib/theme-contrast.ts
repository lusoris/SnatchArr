// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// WCAG 2.2 SC 1.4.11 helper: contrast of two oklch() colours, computed through OKLab ->
// linear sRGB (Ottosson), so it stays right once a token gains chroma. axe-core does not test
// non-text contrast, so the theme test computes it from the generated tokens.

export type Rgb = readonly [number, number, number];

const OKLCH = /^oklch\(\s*([\d.]+)(%?)\s+([\d.]+)\s+([\d.]+)(?:deg)?\s*(?:\/\s*[\d.]+%?\s*)?\)$/;

/** `oklch(L C H)` (L as a fraction or a percentage) to clamped linear sRGB. */
export function oklchToLinearSrgb(value: string): Rgb {
	const m = OKLCH.exec(value.trim());
	if (!m) throw new Error(`not an oklch() colour: ${value}`);
	const l = m[2] ? Number(m[1]) / 100 : Number(m[1]);
	const c = Number(m[3]);
	const h = (Number(m[4]) * Math.PI) / 180;
	const a = c * Math.cos(h);
	const b = c * Math.sin(h);
	const l_ = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
	const m_ = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
	const s_ = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;
	const clamp = (x: number): number => Math.min(1, Math.max(0, x));
	return [
		clamp(4.0767416621 * l_ - 3.3077115913 * m_ + 0.2309699292 * s_),
		clamp(-1.2684380046 * l_ + 2.6097574011 * m_ - 0.3413193965 * s_),
		clamp(-0.0041960863 * l_ - 0.7034186147 * m_ + 1.707614701 * s_)
	];
}

/** WCAG relative luminance of an oklch() colour. */
export function relativeLuminance(value: string): number {
	const [r, g, b] = oklchToLinearSrgb(value);
	return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** WCAG contrast ratio of two oklch() colours, 1 to 21. */
export function contrastRatio(a: string, b: string): number {
	const la = relativeLuminance(a);
	const lb = relativeLuminance(b);
	return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}
