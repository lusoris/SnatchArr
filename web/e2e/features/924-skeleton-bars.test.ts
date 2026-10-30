// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #924
import { expect, test } from '@playwright/test';
import { signIn } from './layout-fixtures.js';

const TRANSPARENT = new Set(['rgba(0, 0, 0, 0)', 'transparent']);
const INSTANCES = '**/api/v1/instances';

test('924: the dashboard skeleton bars are filled while the fleet loads', async ({ page }) => {
	await signIn(page);

	// Only the load request of the instance list is held, so the page stays in its loading state;
	// the held request is let through, and its handler awaited, before the test ends.
	let release: () => void = () => undefined;
	const held = new Promise<void>((resolve) => {
		release = resolve;
	});
	await page.route(INSTANCES, async (route) => {
		await held;
		await route.continue();
	});
	try {
		await page.goto('/');
		const bars = page.locator('[data-testid="skeleton"] .animate-pulse');
		await expect(bars.first()).toBeVisible();

		const fills = await bars.evaluateAll((els) =>
			els.map((el) => getComputedStyle(el).backgroundColor)
		);
		expect(fills.length, 'the loading card draws its bars').toBeGreaterThanOrEqual(3);
		expect(
			fills.filter((fill) => TRANSPARENT.has(fill)).length,
			`skeleton bars without a fill: ${fills.join(' | ')}`
		).toBe(0);
	} finally {
		release();
		await page.unrouteAll({ behavior: 'wait' });
	}
});
