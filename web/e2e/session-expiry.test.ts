// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test } from '@playwright/test';
import { mockSignedInApi } from './mocks.js';

test.describe('session expiry polling', () => {
	test('redirects to /login and stops polling when data endpoints return 401', async ({ page }) => {
		await page.clock.install();
		await mockSignedInApi(page);

		let hourlyCapsRequests = 0;
		await page.route('**/api/v1/hourly-caps', (route) => {
			hourlyCapsRequests++;
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			});
		});

		await page.goto('/');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

		const problem401 = {
			status: 401,
			contentType: 'application/problem+json',
			body: JSON.stringify({
				type: 'https://tools.ietf.org/html/rfc9457',
				title: 'Unauthorized',
				status: 401,
				detail: 'Session expired'
			})
		};

		hourlyCapsRequests = 0;
		await page.route('**/api/v1/hourly-caps', (route) => {
			hourlyCapsRequests++;
			return route.fulfill(problem401);
		});
		await page.route('**/api/v1/prowlarr/status', (route) => route.fulfill(problem401));
		await page.route('**/api/v1/auth/session', (route) => route.fulfill(problem401));

		// Advance 30 s: refetch interval triggers
		await page.clock.runFor(30_000);

		// Within one refetch interval, URL becomes /login
		await expect(page).toHaveURL(/\/login/);

		const countAfterExpiry = hourlyCapsRequests;
		expect(countAfterExpiry).toBeGreaterThanOrEqual(1);

		// Advance 60 s more: no further /hourly-caps requests
		await page.clock.runFor(60_000);
		expect(hourlyCapsRequests).toBe(countAfterExpiry);
	});
});
