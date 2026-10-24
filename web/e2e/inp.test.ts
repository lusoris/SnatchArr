// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test } from '@playwright/test';
import { mockInstancePolicyApi, mockSignedInApi } from './mocks.js';

const testInstanceId = '00000000-0000-0000-0000-000000000001';

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

test.describe('INP measurements', () => {
	test('measure input latency via PerformanceObserver during nav click and drawer open (<= 200 ms)', async ({
		page
	}) => {
		await mockSignedInApi(page);
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
		const connectionsLink = dialog.getByRole('link', { name: /Connections|Verbindungen/i });
		await connectionsLink.click();
		await expect(page).toHaveURL(/\/connections/);

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
	test('measure input latency on the policy form: preset click and field input (<= 200 ms)', async ({
		page
	}) => {
		await mockInstancePolicyApi(page, testInstanceId);
		await page.goto(`/instances/${testInstanceId}/policy`);
		const field = page.locator('[data-field="hourly_cap"]');
		await expect(field).toBeVisible({ timeout: 3000 });

		await page.evaluate(() => {
			window.__eventDurations = [];
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
		});

		await page.getByRole('button', { name: /Catch up|Aufholen/i }).click();
		await field.fill('30');
		await expect(field).toHaveValue('30');
		await page.waitForTimeout(200);

		const entries = await page.evaluate(() => window.__eventDurations ?? []);
		const worst = entries.length > 0 ? Math.max(...entries.map((e) => e.duration)) : 0;
		console.log(`[INP] policy form: ${entries.length} interactions, worst ${worst.toFixed(1)}ms`);
		expect(worst).toBeLessThanOrEqual(200);
	});
});
