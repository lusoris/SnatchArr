// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// #342: the boundary that identifies a control needs 3:1 (WCAG 2.2 SC 1.4.11) against the
// colour next to it, in both themes. Reads the generated tokens, never a copy of them.
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { contrastRatio } from './theme-contrast';

const themeCss = readFileSync(new URL('./theme.css', import.meta.url), 'utf8');
const appCss = readFileSync(new URL('../app.css', import.meta.url), 'utf8');

const MIN = 3;
// Surfaces an input or button can sit on: the page, a card (.card, /login), a popover.
const SURFACES = ['background', 'card', 'popover'] as const;

function block(css: string, selector: string): Record<string, string> {
	const start = css.indexOf(`${selector} {`);
	if (start < 0) throw new Error(`no ${selector} block in theme.css`);
	const body = css.slice(css.indexOf('{', start) + 1, css.indexOf('}', start));
	return Object.fromEntries(
		[...body.matchAll(/--color-([\w-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()])
	);
}

const themes = { light: block(themeCss, ':root'), dark: block(themeCss, '.dark') };

/** Declarations of the first rule whose selector list contains `selector`. */
function rule(css: string, selector: string): string {
	const m = new RegExp(
		`(?:^|[\\s,}])${selector.replace('.', '\\.')}\\s*(?:,[^{]*)?\\{([^}]*)\\}`
	).exec(css);
	if (!m) throw new Error(`no ${selector} rule in app.css`);
	return m[1];
}

/** Follow `--color-x: var(--color-y)` aliases of the @theme block to a theme token. */
function resolveAlias(name: string): string {
	const theme = /@theme\s*\{([^}]*)\}/.exec(appCss)?.[1] ?? '';
	const aliases = Object.fromEntries(
		[...theme.matchAll(/--color-([\w-]+):\s*var\(--color-([\w-]+)\)/g)].map((m) => [m[1], m[2]])
	);
	const seen = new Set<string>();
	let cur = name;
	while (cur in aliases) {
		if (seen.has(cur)) throw new Error(`--color-${cur} aliases itself`);
		seen.add(cur);
		if (aliases[cur] === cur) throw new Error(`--color-${cur} aliases itself`);
		cur = aliases[cur];
	}
	return cur;
}

describe('contrast helper', () => {
	it('matches known WCAG values', () => {
		expect(contrastRatio('oklch(1 0 0)', 'oklch(0 0 0)')).toBeCloseTo(21, 1);
		expect(contrastRatio('oklch(1 0 0)', 'oklch(1 0 0)')).toBeCloseTo(1, 5);
		// #767676 is the lightest grey that reaches 4.54:1 on white; oklch(0.5681 0 0) = #767676.
		expect(contrastRatio('oklch(0.5681 0 0)', 'oklch(1 0 0)')).toBeCloseTo(4.54, 1);
	});

	it('accounts for chroma, not only lightness', () => {
		// Same lightness, different luminance once the colour is saturated.
		expect(contrastRatio('oklch(0.6 0.2 30)', 'oklch(1 0 0)')).not.toBeCloseTo(
			contrastRatio('oklch(0.6 0 0)', 'oklch(1 0 0)'),
			1
		);
	});

	it("refuses today's pre-fix grey (1.26:1) and accepts a 3:1 grey", () => {
		expect(contrastRatio('oklch(0.922 0 0)', 'oklch(1 0 0)')).toBeLessThan(MIN);
		expect(contrastRatio('oklch(0.6 0 0)', 'oklch(1 0 0)')).toBeGreaterThanOrEqual(MIN);
	});
});

describe.each(Object.entries(themes))('%s theme input boundary (#342)', (_name, tokens) => {
	it.each(SURFACES)('--color-input reaches 3:1 on --color-%s', (surface) => {
		expect(contrastRatio(tokens.input, tokens[surface])).toBeGreaterThanOrEqual(MIN);
	});
});

describe('the stylesheet draws the boundary from the control token', () => {
	it.each(['.input', '.select', '.btn'])('%s border colour resolves to --color-input', (sel) => {
		const m = /border(?:-color)?:\s*[^;]*var\(--color-([\w-]+)\)/.exec(rule(appCss, sel));
		expect(m, `${sel} has no var() border colour`).not.toBeNull();
		expect(resolveAlias(m![1])).toBe('input');
	});

	it('no @theme alias in app.css refers to itself (an unresolved var() drops the whole border)', () => {
		for (const name of ['line', 'surface', 'control', 'bg', 'fg', 'subtle', 'brand', 'danger']) {
			expect(() => resolveAlias(name)).not.toThrow();
		}
		for (const name of ['line', 'surface', 'control']) {
			expect(Object.keys(themes.light)).toContain(resolveAlias(name));
		}
	});
});
