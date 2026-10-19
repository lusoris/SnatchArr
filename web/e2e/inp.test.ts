// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test, type Page } from '@playwright/test';

interface EventTimingEntry {
	name: string;
	duration: number;
	startTime: number;
}

declare global {
	interface Window {
		__eventDurations?: EventTimingEntry[];
	}
}

async function setupMocks(page: Page) {
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
}

test.describe('INP measurements', () => {
	test('measure input latency via PerformanceObserver during nav click and drawer open (<= 200 ms)', async ({
		page
	}) => {
		await setupMocks(page);
		await page.setViewportSize({ width: 390, height: 844 });
		await page.goto('/');
		await page.waitForLoadState('networkidle');

		// Install PerformanceObserver for type 'event' with durationThreshold 16
		await page.evaluate(() => {
			window.__eventDurations = [];
			try {
				const observer = new PerformanceObserver((list) => {
					for (const entry of list.getEntries()) {
						window.__eventDurations?.push({
							name: entry.name,
							duration: entry.duration,
							startTime: entry.startTime
						});
					}
				});
				observer.observe({ type: 'event', durationThreshold: 16, buffered: true });
			} catch (err) {
				console.warn('PerformanceObserver type event not supported:', err);
			}
		});

		// 1. Drawer open interaction
		const menuButton = page.getByRole('button', { name: /Open menu|Menü öffnen/i });
		await menuButton.click();
		const dialog = page.locator('dialog#mobile-nav-dialog');
		await expect(dialog).toBeVisible();

		// 2. Nav click interaction
		const instancesLink = page.getByRole('link', { name: /Instances|Instanzen/i }).first();
		await instancesLink.click();
		await expect(page).toHaveURL(/\/instances/);

		// Allow observers to collect events
		await page.waitForTimeout(200);

		const entries = await page.evaluate(() => window.__eventDurations ?? []);
		const durations = entries.map((e) => e.duration);
		const worstDuration = durations.length > 0 ? Math.max(...durations) : 0;

		// Format output for reporting
		console.log(
			`[INP] Total interactions captured: ${entries.length}, durations: [${durations.map((d) => `${d.toFixed(1)}ms`).join(', ')}], worst: ${worstDuration.toFixed(1)}ms`
		);

		// Assert worst interaction <= 200ms
		expect(worstDuration).toBeLessThanOrEqual(200);
	});
});
