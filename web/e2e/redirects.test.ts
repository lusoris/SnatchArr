// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test } from '@playwright/test';
import { mockSettingsApi } from './mocks.js';

interface RedirectCase {
	from: string;
	targetPath: string;
	targetSearch: string;
	heading: RegExp;
}

export const REDIRECT_TABLE: RedirectCase[] = [
	{
		from: '/history',
		targetPath: '/activity',
		targetSearch: '',
		heading: /Activity|Aktivität/i
	},
	{
		from: '/live',
		targetPath: '/activity',
		targetSearch: '?source=live',
		heading: /Activity|Aktivität/i
	},
	{
		from: '/logs',
		targetPath: '/activity',
		targetSearch: '?source=log',
		heading: /Activity|Aktivität/i
	},
	{
		from: '/schedules',
		targetPath: '/automation',
		targetSearch: '?tab=schedules',
		heading: /Automation|Automatisierung/i
	},
	{
		from: '/download-clients',
		targetPath: '/connections/download-clients',
		targetSearch: '',
		heading: /Download clients|Download-Clients/i
	},
	{
		from: '/seerr',
		targetPath: '/connections/seerr',
		targetSearch: '',
		heading: /Seerr/i
	},
	{
		from: '/prowlarr',
		targetPath: '/connections/prowlarr',
		targetSearch: '',
		heading: /Prowlarr/i
	},
	{
		from: '/configarr',
		targetPath: '/connections/configarr',
		targetSearch: '',
		heading: /Configarr/i
	},
	{
		from: '/settings',
		targetPath: '/system',
		targetSearch: '?tab=general',
		heading: /System/i
	},
	{
		from: '/about',
		targetPath: '/system',
		targetSearch: '?tab=about',
		heading: /System/i
	}
];

test.describe('authenticated: client-side legacy redirects', () => {
	for (const { from, targetPath, targetSearch, heading } of REDIRECT_TABLE) {
		test(`legacy ${from} redirects to ${targetPath}${targetSearch}`, async ({ page }) => {
			await page.goto(from);
			await expect(page).toHaveURL(`${targetPath}${targetSearch}`);
			const url = new URL(page.url());
			expect(url.pathname).toBe(targetPath);
			expect(url.search).toBe(targetSearch);
			await expect(page.getByRole('heading', { level: 1, name: heading })).toBeVisible();
		});
	}

	test('preserves incoming query parameters through redirect', async ({ page }) => {
		await page.goto('/history?instance=seeded-sonarr&custom=123');
		await expect(page).toHaveURL('/activity?instance=seeded-sonarr&custom=123');
		const url = new URL(page.url());
		expect(url.pathname).toBe('/activity');
		expect(url.searchParams.get('instance')).toBe('seeded-sonarr');
		expect(url.searchParams.get('custom')).toBe('123');
		await expect(
			page.getByRole('heading', { level: 1, name: /Activity|Aktivität/i })
		).toBeVisible();
	});

	test('viewer visiting /logs redirects to /activity with history tab and no log tab', async ({
		page
	}) => {
		await mockSettingsApi(page, { role: 'viewer' });
		await page.goto('/logs');
		await expect(page).toHaveURL('/activity?source=log');
		const url = new URL(page.url());
		expect(url.pathname).toBe('/activity');
		expect(url.search).toBe('?source=log');
		await expect(
			page.getByRole('heading', { level: 1, name: /Activity|Aktivität/i })
		).toBeVisible();

		// Log tab is omitted for viewer; events/history tab is active
		await expect(
			page.getByRole('tab', { name: /Logs|Protokolle|Server log|Serverprotokoll/i })
		).toHaveCount(0);
		await expect(
			page.getByRole('tab', { name: /Events|Ereignisse|History|Ereignisverlauf/i, selected: true })
		).toBeVisible();
	});
});
