// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test } from '@playwright/test';

test.describe('nonblocking', () => {
	for (const initialPath of ['/instances', '/live']) {
		test(`delayed 10s API responses on ${initialPath} do not block shell render or navigation, and 503 renders ProblemNote or skeleton`, async ({
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

			await page.goto(initialPath);

			// 1. Nav and header band or heading are visible within 1000 ms
			const nav = page.getByRole('navigation', { name: /SnatchArr/i }).first();
			await expect(nav).toBeVisible({ timeout: 1000 });

			const headerBand = page
				.locator('.page-header-band, [data-testid="header-band"], header, h1')
				.locator('visible=true')
				.first();
			await expect(headerBand).toBeVisible({ timeout: 1000 });

			// 2. Nav click changes URL within 1000 ms while requests are pending
			const targetLink = page
				.getByRole('link', {
					name: initialPath === '/live' ? /History|Ereignisverlauf/i : /Schedules|Zeitpläne/i
				})
				.first();
			await targetLink.click();
			await expect(page).toHaveURL(initialPath === '/live' ? /\/history/ : /\/schedules/, {
				timeout: 1000
			});

			// 3. The 503 renders a ProblemNote or skeleton, never a frozen blank page
			await expect(
				page
					.locator(
						'.problem-note, [role="alert"], [data-testid="skeleton"], [data-testid="live-skeleton"]'
					)
					.first()
			).toBeVisible({
				timeout: 12_000
			});
		});
	}
});
