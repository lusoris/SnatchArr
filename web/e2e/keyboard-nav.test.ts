// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test } from '@playwright/test';
import { mockSignedInApi } from './mocks.js';

test.describe('keyboard navigation', () => {
	test.beforeEach(async ({ page }) => {
		await mockSignedInApi(page);
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

		// Tab from top: Skip link -> Brand link -> search trigger -> first nav item (Dashboard)
		await page.keyboard.press('Tab'); // skip link
		await page.keyboard.press('Tab'); // brand link in sidebar
		await page.keyboard.press('Tab'); // search trigger button
		await page.keyboard.press('Tab'); // first nav item (Dashboard)

		const dashboardLink = page.getByRole('link', { name: /Dashboard|Übersicht/i }).first();
		await expect(dashboardLink).toBeFocused();

		// Check every nav entry is reachable by tabbing sequentially
		const expectedHrefs = [
			'/',
			'/activity',
			'/instances',
			'/automation',
			'/connections',
			'/system'
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
		// The admin entries join the nav once the session loads, so a link
		// count read up front can be stale; assert that no link lacks an icon.
		await expect(page.locator('nav a.nav-link:not(:has(svg))')).toHaveCount(0);
	});

	test('desktop: palette opens via keyboard shortcut, filters commands, and Escape restores focus', async ({
		page
	}) => {
		await page.setViewportSize({ width: 1280, height: 800 });
		await page.goto('/');
		await page.waitForLoadState('networkidle');

		const searchBtn = page.getByRole('button', { name: /Search|Suche/i }).first();
		await searchBtn.focus();
		await expect(searchBtn).toBeFocused();

		// Press Control+k to open palette
		await page.keyboard.press('Control+k');

		const input = page.locator('#ssentio-cmd-input');
		await expect(input).toBeVisible();
		await expect(input).toBeFocused();

		// Type search query to filter destinations
		await page.keyboard.type('Instances');
		const option = page.getByRole('option', { name: /Instances|Instanzen/i }).first();
		await expect(option).toBeVisible();

		// Press Escape to dismiss palette
		await page.keyboard.press('Escape');
		await expect(input).toBeHidden();

		// Focus returns to opener
		await expect(searchBtn).toBeFocused();
	});

	test('phone: bottom bar is keyboard-navigable and More opens drawer', async ({ page }) => {
		await page.setViewportSize({ width: 390, height: 844 });
		await page.goto('/');
		await page.waitForLoadState('networkidle');

		const bottomNav = page
			.locator('nav')
			.filter({ has: page.getByRole('button', { name: /More|Mehr/i }) });
		await expect(bottomNav).toBeVisible();

		const moreButton = bottomNav.getByRole('button', { name: /More|Mehr/i });
		await expect(moreButton).toBeVisible();

		// Focus and activate More button via Enter key
		await moreButton.focus();
		await expect(moreButton).toBeFocused();
		await page.keyboard.press('Enter');

		const dialog = page.locator('dialog#mobile-nav-dialog');
		await expect(dialog).toBeVisible();

		const closeButton = page.getByRole('button', { name: /Close menu|Menü schließen/i });
		await expect(closeButton).toBeFocused();

		await page.keyboard.press('Escape');
		await expect(dialog).toBeHidden();
		await expect(moreButton).toBeFocused();
	});

	test('authenticated: desktop: on /system, Tab from active tab reaches panel with visible focus ring', async ({
		page
	}) => {
		await page.setViewportSize({ width: 1280, height: 800 });
		await page.goto('/system');
		await page.waitForLoadState('networkidle');

		const activeTab = page.getByRole('tab', { selected: true });
		await expect(activeTab).toBeVisible();
		await activeTab.focus();
		await expect(activeTab).toBeFocused();

		await page.keyboard.press('Tab');

		const panel = page.getByRole('tabpanel');
		await expect(panel).toBeFocused();

		const outlineStyle = await panel.evaluate((el) => window.getComputedStyle(el).outlineStyle);
		expect(outlineStyle).not.toBe('none');
	});
});
