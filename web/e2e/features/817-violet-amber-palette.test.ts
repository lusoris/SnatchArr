// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #817

import { expect, test, type Page } from '@playwright/test';

// Tokens retuned / added by PR #817 for the Violet Amber palette.
// See git show 30eabfc2 -- web/src/lib/theme.css
const EXPECTED_LIGHT_TOKENS = {
	'--color-primary': 'oklch(0.38 0.22 285)',
	'--color-secondary': 'oklch(0.94 0.025 285)',
	'--color-accent': 'oklch(0.93 0.06 85)',
	'--color-accent-foreground': 'oklch(0.24 0.08 85)',
	'--color-ring': 'oklch(0.50 0.18 285)',
	'--color-brand': 'oklch(0.38 0.22 285)',
	'--color-brand-foreground': 'oklch(0.985 0.005 285)',
	'--color-nav-active': 'oklch(0.93 0.04 285)',
	'--color-nav-active-foreground': 'oklch(0.20 0.12 285)',
	'--color-running': 'oklch(0.48 0.20 285)'
} as const;

const EXPECTED_DARK_TOKENS = {
	'--color-primary': 'oklch(0.76 0.20 290)',
	'--color-accent': 'oklch(0.28 0.08 85)',
	'--color-accent-foreground': 'oklch(0.96 0.03 85)',
	'--color-ring': 'oklch(0.70 0.18 290)',
	'--color-brand': 'oklch(0.76 0.20 290)',
	'--color-brand-foreground': 'oklch(0.14 0.04 285)',
	'--color-nav-active': 'oklch(0.24 0.06 285)',
	'--color-nav-active-foreground': 'oklch(0.92 0.10 290)',
	'--color-running': 'oklch(0.76 0.20 290)'
} as const;

// The build minifies theme.css (oklch(0.38 0.22 285) is served as oklch(38% .22 285)), so each
// token is compared as the colour the browser resolves, not as source text.
async function resolvedTokens(page: Page, tokens: Readonly<Record<string, string>>) {
	return page.evaluate((entries) => {
		const probe = document.createElement('div');
		document.body.append(probe);
		const resolve = (value: string) => {
			probe.style.color = '';
			probe.style.color = value;
			return getComputedStyle(probe).color;
		};
		const result = Object.fromEntries(
			entries.map(([k, v]) => [k, { actual: resolve(`var(${k})`), expected: resolve(v) }])
		);
		probe.remove();
		return result;
	}, Object.entries(tokens));
}

async function expectTokens(page: Page, tokens: Readonly<Record<string, string>>, mode: string) {
	const resolved = await resolvedTokens(page, tokens);
	for (const [prop, { actual, expected }] of Object.entries(resolved)) {
		expect(actual, `${mode} mode token ${prop}`).toBe(expected);
	}
}

test('positive: violet amber brand and accent tokens match theme.css in light and dark mode', async ({
	page
}) => {
	// 1. Light mode
	await page.emulateMedia({ colorScheme: 'light' });
	await page.goto('/instances');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	await expectTokens(page, EXPECTED_LIGHT_TOKENS, 'light');

	// 2. Dark mode
	await page.emulateMedia({ colorScheme: 'dark' });
	await expect(page.locator('html')).toHaveClass(/dark/);

	await expectTokens(page, EXPECTED_DARK_TOKENS, 'dark');
});

test('negative: unknown and non-existent custom properties return empty string', async ({
	page
}) => {
	await page.goto('/instances');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	const unknownTokens = await page.evaluate(() => {
		const style = window.getComputedStyle(document.documentElement);
		return [
			style.getPropertyValue('--color-nonexistent').trim(),
			style.getPropertyValue('--color-unknown-violet-amber').trim(),
			style.getPropertyValue('--color-brand-legacy').trim()
		];
	});

	for (const val of unknownTokens) {
		expect(val).toBe('');
	}
});

test('boundary: forced theme handling and class-based theme boundary toggling', async ({
	page
}) => {
	await page.emulateMedia({ colorScheme: 'light' });
	await page.goto('/instances');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	// In web/src, theme is driven by html.dark via prefers-color-scheme (theme-init.js).
	// Setting data-theme does not override tokens because data-theme is not bound in theme.css.
	const brand = { '--color-brand': EXPECTED_LIGHT_TOKENS['--color-brand'] };
	const darkBrand = { '--color-brand': EXPECTED_DARK_TOKENS['--color-brand'] };
	await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'dark'));
	await expectTokens(page, brand, 'data-theme');

	// Boundary: explicitly forcing the .dark class switches custom properties to dark palette
	await page.evaluate(() => document.documentElement.classList.add('dark'));
	await expectTokens(page, darkBrand, 'forced dark');

	// Toggling back to light removes .dark and restores light palette token
	await page.evaluate(() => {
		document.documentElement.classList.remove('dark');
		document.documentElement.removeAttribute('data-theme');
	});
	await expectTokens(page, brand, 'restored light');
});
