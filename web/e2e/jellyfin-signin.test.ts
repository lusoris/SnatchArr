// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { json, mockAuthenticatedApi } from './mocks.js';

const WCAG_22_AA = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

test.describe('Jellyfin sign-in and Seerr user mapping', () => {
	test('Jellyfin sign-in succeeds, lands on /, and /login passes axe with zero violations', async ({
		page
	}) => {
		let signedIn = false;
		await mockAuthenticatedApi(page);
		await page.route('**/api/v1/auth/sso', (route) =>
			route.fulfill(json({ oidc: false, proxy: false, jellyfin: true, jellyfinQuickConnect: true }))
		);
		await page.route('**/api/v1/auth/session', (route) => {
			if (!signedIn) {
				return route.fulfill({
					status: 401,
					contentType: 'application/problem+json',
					body: JSON.stringify({
						type: 'about:blank',
						title: 'Unauthorized',
						status: 401,
						detail: 'not signed in'
					})
				});
			}
			return route.fulfill(
				json({
					user: {
						id: '00000000-0000-0000-0000-000000000077',
						username: 'jf-alice',
						role: 'requester'
					},
					csrf_token: 'test-token',
					expires_at: '2099-01-01T00:00:00Z'
				})
			);
		});
		await page.route('**/api/v1/auth/jellyfin', (route) => {
			signedIn = true;
			return route.fulfill(
				json({
					user: {
						id: '00000000-0000-0000-0000-000000000077',
						username: 'jf-alice',
						role: 'requester'
					},
					csrf_token: 'test-token',
					expires_at: '2099-01-01T00:00:00Z'
				})
			);
		});

		await page.goto('/login');
		await expect(page.locator('#jellyfin-username')).toBeVisible();
		await expect(page.locator('#username')).toHaveCount(0);

		const resultsCollapsed = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(resultsCollapsed.violations).toEqual([]);

		await page.locator('#jellyfin-username').fill('alice');
		await page.locator('#jellyfin-password').fill('correct-horse-battery');
		await page.getByRole('button', { name: /Sign in with Jellyfin/i }).click();

		await expect(page).toHaveURL(/\/$/);
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	});

	test('wrong Jellyfin credentials show 401 problem alert and stay on /login', async ({ page }) => {
		await mockAuthenticatedApi(page);
		await page.route('**/api/v1/auth/sso', (route) =>
			route.fulfill(json({ oidc: false, proxy: false, jellyfin: true, jellyfinQuickConnect: false }))
		);
		await page.route('**/api/v1/auth/session', (route) =>
			route.fulfill({
				status: 401,
				contentType: 'application/problem+json',
				body: JSON.stringify({
					type: 'about:blank',
					title: 'Unauthorized',
					status: 401,
					detail: 'not signed in'
				})
			})
		);
		await page.route('**/api/v1/auth/jellyfin', (route) =>
			route.fulfill({
				status: 401,
				contentType: 'application/problem+json',
				body: JSON.stringify({
					type: 'about:blank',
					title: 'Unauthorized',
					status: 401,
					detail: 'Invalid Jellyfin username or password'
				})
			})
		);

		await page.goto('/login');
		await page.locator('#jellyfin-username').fill('alice');
		await page.locator('#jellyfin-password').fill('wrong-password');
		await page.getByRole('button', { name: /Sign in with Jellyfin/i }).click();

		const alert = page.getByRole('alert');
		await expect(alert).toBeVisible();
		await expect(alert).toContainText('Invalid Jellyfin username or password');
		await expect(page).toHaveURL(/\/login$/);
	});

	test('Seerr request list shows mapped SnatchArr username and falls back to Seerr name', async ({
		page
	}) => {
		await mockAuthenticatedApi(page);
		await page.route('**/api/v1/seerr/requests*', (route) =>
			route.fulfill(
				json([
					{
						link_id: '00000000-0000-0000-0000-000000000030',
						link_name: 'Main Jellyseerr',
						request_id: 101,
						media_type: 'movie',
						tmdb_id: 550,
						tvdb_id: 0,
						title: 'Fight Club',
						request_status: 2,
						media_status: 3,
						is_4k: false,
						requested_by: 'seerr-alice',
						requestedByUserId: '00000000-0000-0000-0000-000000000077',
						requestedByUsername: 'snatcharr-alice',
						seasons: [],
						instance_id: '00000000-0000-0000-0000-000000000001',
						instance_name: 'Main Radarr',
						entity_id: 42,
						resolved: true,
						last_seen_at: '2026-10-20T12:00:00Z'
					},
					{
						link_id: '00000000-0000-0000-0000-000000000030',
						link_name: 'Main Jellyseerr',
						request_id: 102,
						media_type: 'movie',
						tmdb_id: 551,
						tvdb_id: 0,
						title: 'Unmapped Movie',
						request_status: 2,
						media_status: 3,
						is_4k: false,
						requested_by: 'seerr-unmapped-bob',
						seasons: [],
						resolved: false,
						unresolved_reason: 'no matching instance',
						last_seen_at: '2026-10-20T12:00:00Z'
					}
				])
			)
		);

		await page.goto('/connections/seerr');
		const table = page.getByRole('table');
		await expect(table).toBeVisible();
		await expect(table).toContainText('Fight Club');
		await expect(table).toContainText('snatcharr-alice');
		await expect(table).not.toContainText('seerr-alice');
		await expect(table).toContainText('seerr-unmapped-bob');
	});
});
