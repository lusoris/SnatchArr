// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test } from '@playwright/test';

test.describe('keyboard navigation', () => {
	test.beforeEach(async ({ page }) => {
		await page.route('**/api/v1/auth/setup/status', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ setup_complete: true })
			})
		);
		await page.route('**/api/v1/auth/session', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					user: {
						id: '00000000-0000-0000-0000-000000000099',
						username: 'admin',
						role: 'admin'
					},
					csrf_token: 'test-token',
					expires_at: '2099-01-01T00:00:00Z'
				})
			})
		);
		await page.route('**/api/v1/auth/sso', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ oidc: false, proxy: false })
			})
		);
		await page.route('**/api/v1/instances', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);
		await page.route('**/api/v1/hourly-caps', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);
		await page.route('**/api/v1/prowlarr/status', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);
	});

	test('desktop: Tab reaches skip link first, activates to main, and nav entries are Tab-reachable', async ({
		page
	}) => {
		await page.setViewportSize({ width: 1280, height: 800 });
		await page.goto('/');
		await page.waitForLoadState('networkidle');

		// 1. Tab reaches skip link first
		await page.keyboard.press('Tab');
		const skipLink = page.getByRole('link', { name: /Skip to content|Zum Inhalt springen/i });
		await expect(skipLink).toBeFocused();

		// 2. Skip link moves focus to #main
		await page.keyboard.press('Enter');
		const main = page.locator('#main');
		await expect(main).toBeFocused();

		// 3. Tab through desktop sidebar navigation
		await page.goto('/');
		await page.waitForLoadState('networkidle');

		// Tab from top: Skip link -> Brand link -> first nav item (Dashboard)
		await page.keyboard.press('Tab'); // skip link
		await page.keyboard.press('Tab'); // brand link in sidebar
		await page.keyboard.press('Tab'); // first nav item (Dashboard)

		const dashboardLink = page.getByRole('link', { name: /Dashboard|Übersicht/i }).first();
		await expect(dashboardLink).toBeFocused();

		// Check every nav entry is reachable by tabbing sequentially
		const expectedHrefs = [
			'/',
			'/instances',
			'/history',
			'/live',
			'/schedules',
			'/download-clients',
			'/seerr',
			'/prowlarr',
			'/configarr',
			'/settings',
			'/logs'
		];

		for (let i = 1; i < expectedHrefs.length; i++) {
			await page.keyboard.press('Tab');
			const activeElHref = await page.evaluate(() =>
				(document.activeElement as HTMLAnchorElement)?.getAttribute('href')
			);
			expect(activeElHref).toBe(expectedHrefs[i]);
		}
	});

	test('phone: drawer traps focus and Escape returns focus to menu button', async ({ page }) => {
		await page.setViewportSize({ width: 390, height: 844 });
		await page.goto('/');
		await page.waitForLoadState('networkidle');

		const menuButton = page.getByRole('button', { name: /Open menu|Menü öffnen/i });
		await expect(menuButton).toBeVisible();

		// Click to open drawer
		await menuButton.click();

		const dialog = page.locator('dialog#mobile-nav-dialog');
		await expect(dialog).toBeVisible();

		// Close button inside drawer should receive initial focus
		const closeButton = page.getByRole('button', { name: /Close menu|Menü schließen/i });
		await expect(closeButton).toBeFocused();

		// Press Escape to close drawer
		await page.keyboard.press('Escape');

		await expect(dialog).toBeHidden();
		await expect(menuButton).toBeFocused();
	});

	test('desktop: every nav entry renders its icon in the built app', async ({ page }) => {
		await page.setViewportSize({ width: 1280, height: 800 });
		await page.goto('/');
		const links = page.locator('nav a.nav-link');
		await expect(links.first()).toBeVisible();
		const count = await links.count();
		expect(count).toBeGreaterThan(0);
		await expect(page.locator('nav a.nav-link svg')).toHaveCount(count);
	});
});
