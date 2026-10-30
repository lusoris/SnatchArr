// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #889
import { expect, test, type Page } from '@playwright/test';
import { assertReflow } from '../screen-range.js';
import { createInstance, deleteRows, signIn, uniqueSuffix } from './layout-fixtures.js';

// 320 CSS px is the WCAG 1.4.10 reflow width; the screen-range matrix names it phone-reflow.
const REFLOW = { width: 320, height: 640 } as const;
// md:min-h-[28rem] on the activity page keeps the feed usable on a short landscape screen.
const ACTIVITY_MIN_PX = 28 * 16;

async function openSettled(page: Page, route: string): Promise<void> {
	await page.goto(route);
	await expect(page.getByRole('heading', { level: 1 }).first()).toBeVisible();
	await page.waitForLoadState('networkidle');
}

test('889: instance pages reflow at 320 px and their subtabs wrap', async ({ page }) => {
	const csrf = await signIn(page);
	const id = await createInstance(page, csrf, `e2e-889-${uniqueSuffix()}`);
	try {
		await page.setViewportSize(REFLOW);
		for (const route of [
			`/instances/${id}`,
			`/instances/${id}/queue`,
			`/instances/${id}/policy`,
			'/connections/download-clients',
			'/system?tab=modules'
		]) {
			await openSettled(page, route);
			await assertReflow(page);
		}

		// Below md the subtab bar wraps its tabs instead of scrolling them sideways.
		await openSettled(page, `/instances/${id}`);
		const bar = page.locator('nav.subtab-bar');
		await expect(bar).toBeVisible();
		await expect(bar).toHaveCSS('flex-wrap', 'wrap');
		const fits = await bar.evaluate((el) => el.scrollWidth <= el.clientWidth);
		expect(fits, 'the subtabs need no sideways scroll').toBe(true);
	} finally {
		await deleteRows(page, csrf, 'instances', [id]);
	}
});

test('889: the activity log takes keyboard focus and the feed keeps its height on a short screen', async ({
	page
}) => {
	await signIn(page);
	// A phone in landscape is wider than md and shorter than the feed needs.
	await page.setViewportSize({ width: 900, height: 480 });
	await openSettled(page, '/activity');

	const log = page.getByRole('log');
	await expect(log).toHaveAttribute('tabindex', '0');
	await log.focus();
	await expect(log).toBeFocused();

	const feed = await page.locator('.page-column').boundingBox();
	expect(feed, 'the activity page column has no box').not.toBeNull();
	expect(feed!.height, 'the feed collapsed on a short screen').toBeGreaterThanOrEqual(
		ACTIVITY_MIN_PX - 1
	);
});

test('889: the preset descriptions on the policy tab are not clamped', async ({ page }) => {
	const csrf = await signIn(page);
	const id = await createInstance(page, csrf, `e2e-889p-${uniqueSuffix()}`);
	try {
		await page.setViewportSize(REFLOW);
		await openSettled(page, `/instances/${id}/policy`);
		const presets = page.locator('div.card:has([data-testid="estimate-text"]) button[title]');
		await expect(presets.first()).toBeVisible();
		const clamps = await presets.evaluateAll((buttons) =>
			buttons.map((b) => getComputedStyle(b.querySelector('span.text-subtle')!).webkitLineClamp)
		);
		expect(clamps.length, 'the picker offers presets').toBeGreaterThan(0);
		expect(clamps.filter((clamp) => clamp !== 'none')).toEqual([]);
	} finally {
		await deleteRows(page, csrf, 'instances', [id]);
	}
});
