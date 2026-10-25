// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// The system colour scheme reaches the page as the class "dark" on <html> (issue #318), and the
// dark theme passes axe (contrast) like the light one. Runs in the same Playwright projects as
// the axe sweep of the public pages (title prefix "public:").
import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { settleToasts } from './settle.js';

const WCAG_22_AA = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

for (const [scheme, path] of [
	['light', '/setup'],
	['dark', '/setup'],
	['light', '/login'],
	['dark', '/login']
] as const) {
	test(`public: html.dark follows colorScheme ${scheme} on ${path}`, async ({ page }) => {
		await page.emulateMedia({ colorScheme: scheme });
		await page.goto(path);
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		const dark = await page.evaluate(() => document.documentElement.classList.contains('dark'));
		expect(dark).toBe(scheme === 'dark');
		await expect(page.locator('html')).toHaveCSS('color-scheme', scheme);
		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});
}

test('public: html.dark follows a live scheme change', async ({ page }) => {
	await page.emulateMedia({ colorScheme: 'light' });
	await page.goto('/setup');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await page.emulateMedia({ colorScheme: 'dark' });
	await expect(page.locator('html')).toHaveClass(/dark/);
	await page.emulateMedia({ colorScheme: 'light' });
	await expect(page.locator('html')).not.toHaveClass(/dark/);
});

test('public: html[lang] follows locale de and switches to en', async ({ page, baseURL }) => {
	await page
		.context()
		.addCookies([
			{ name: 'PARAGLIDE_LOCALE', value: 'de', url: baseURL ?? 'http://127.0.0.1:4173' }
		]);
	await page.goto('/login');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.locator('html')).toHaveAttribute('lang', 'de');

	await page
		.context()
		.addCookies([
			{ name: 'PARAGLIDE_LOCALE', value: 'en', url: baseURL ?? 'http://127.0.0.1:4173' }
		]);
	await page.reload();
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.locator('html')).toHaveAttribute('lang', 'en');
});

test.describe('locale context de-DE', () => {
	test.use({ locale: 'de-DE' });

	test('public: html[lang] follows context locale de-DE', async ({ page }) => {
		await page.goto('/setup');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		await expect(page.locator('html')).toHaveAttribute('lang', 'de');
	});
});
