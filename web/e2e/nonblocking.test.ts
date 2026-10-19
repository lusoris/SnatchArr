// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test } from '@playwright/test';

test.describe('nonblocking', () => {
	test('delayed 10s API responses do not block shell render or navigation, and 503 renders ProblemNote or skeleton', async ({
		page
	}) => {
		await page.route('**/api/v1/**', async (route) => {
			await new Promise((r) => setTimeout(r, 10_000));
			await route.fulfill({
				status: 503,
				contentType: 'application/problem+json',
				body: JSON.stringify({
					type: 'https://tools.ietf.org/html/rfc9457',
					title: 'Service Unavailable',
					status: 503,
					detail: 'Database unreachable'
				})
			});
		});

		await page.goto('/instances');

		// 1. Nav and header band are visible within 1000 ms
		const nav = page.getByRole('navigation', { name: /SnatchArr/i }).first();
		await expect(nav).toBeVisible({ timeout: 1000 });

		const headerBand = page
			.locator('.page-header-band, [data-testid="header-band"], header')
			.locator('visible=true')
			.first();
		await expect(headerBand).toBeVisible({ timeout: 1000 });

		// 2. Nav click changes URL within 1000 ms while requests are pending
		const schedulesLink = page.getByRole('link', { name: /Schedules|Zeitpläne/i }).first();
		await schedulesLink.click();
		await expect(page).toHaveURL(/\/schedules/, { timeout: 1000 });

		// 3. The 503 renders a ProblemNote or skeleton, never a frozen blank page
		await expect(
			page.locator('.problem-note, [role="alert"], [data-testid="skeleton"]').first()
		).toBeVisible({
			timeout: 12_000
		});
	});
});
