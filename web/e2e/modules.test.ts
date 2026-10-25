// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test } from '@playwright/test';
import { mockSignedInApi } from './mocks.js';

test.describe('modules fail closed and nonblocking', () => {
	test('fail closed: /system/modules returns 503 -> shell renders, six nav items, no block', async ({
		page
	}) => {
		await mockSignedInApi(page);
		await page.route('**/api/v1/system/modules', (route) =>
			route.fulfill({
				status: 503,
				contentType: 'application/problem+json',
				body: JSON.stringify({
					type: 'urn:problem:service-unavailable',
					status: 503,
					title: 'Service Unavailable',
					detail: 'modules unavailable'
				})
			})
		);

		await page.setViewportSize({ width: 1280, height: 800 });
		await page.goto('/');
		await page.waitForLoadState('networkidle');

		// Desktop sidebar renders exactly 6 primary nav items in ADR-0029 order
		const navLinks = page.locator('aside nav ul a');
		await expect(navLinks).toHaveCount(6);

		const expectedHrefs = [
			'/',
			'/activity',
			'/instances',
			'/automation',
			'/connections',
			'/system'
		];
		for (const href of expectedHrefs) {
			await expect(page.locator(`aside nav a[href="${href}"]`)).toBeVisible();
		}
	});

	test('nonblocking: /system/modules delayed 10 s -> nav visible immediately', async ({ page }) => {
		await mockSignedInApi(page);
		await page.route('**/api/v1/system/modules', async (route) => {
			await new Promise((resolve) => setTimeout(resolve, 10000));
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: '[]'
			});
		});

		await page.setViewportSize({ width: 1280, height: 800 });
		await page.goto('/', { waitUntil: 'commit' });

		// Navigation links render immediately without waiting for the delayed modules endpoint
		const navLinks = page.locator('aside nav ul a');
		await expect(navLinks).toHaveCount(6, { timeout: 3000 });
	});
});
