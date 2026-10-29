// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test } from '@playwright/test';
import { mockAboutApi, mockInstancePolicyApi, mockSettingsApi, mockSignedInApi } from './mocks.js';

const testInstanceId = '00000000-0000-0000-0000-000000000001';

test.describe('nonblocking', () => {
	for (const initialPath of [
		'/',
		'/activity',
		'/automation',
		'/connections',
		'/system',
		'/system?tab=modules'
	]) {
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
					name: initialPath === '/activity' ? /Automation|Automatisierung/i : /Activity|Aktivität/i
				})
				.first();
			await targetLink.click();
			await expect(page).toHaveURL(initialPath === '/activity' ? /\/automation/ : /\/activity/, {
				timeout: 1000
			});

			// 3. The 503 renders a ProblemNote or skeleton, never a frozen blank page
			await expect(
				page
					.locator(
						'.problem-note, [role="alert"], [data-testid="skeleton"], [data-testid="live-skeleton"], a.card'
					)
					.first()
			).toBeVisible({
				timeout: 12_000
			});
		});
	}

	test('policy tab: delayed 10s API response shows loading and remains interactive', async ({
		page
	}) => {
		await mockInstancePolicyApi(page, testInstanceId, { delayMs: 10_000 });
		await page.goto(`/instances/${testInstanceId}/policy`);

		// Loading indicator visible within 1000 ms
		const loading = page.locator('[aria-live="polite"]').first();
		await expect(loading).toBeVisible({ timeout: 1000 });

		// Navigation stays interactive: click system link within 1000 ms
		const systemLink = page.getByRole('link', { name: /System/i }).first();
		await systemLink.click();
		await expect(page).toHaveURL(/\/system/, { timeout: 1000 });
	});

	test('policy tab: 503 renders problem note and form recovers', async ({ page }) => {
		await mockInstancePolicyApi(page, testInstanceId, { status: 503 });
		await page.goto(`/instances/${testInstanceId}/policy`);

		// Problem note visible after retries exhaust
		const problem = page.locator('.problem-note, [role="alert"]').first();
		await expect(problem).toBeVisible({ timeout: 12_000 });

		// API recovers, reload brings usable form
		await mockInstancePolicyApi(page, testInstanceId);
		await page.reload();
		const field = page.locator('[data-field="missing_per_cycle"]');
		await expect(field).toBeVisible({ timeout: 3000 });
		await field.fill('25');
		await expect(field).toHaveValue('25');
	});

	test('policy tab: save action shows pending state and recovers on 503', async ({ page }) => {
		await mockInstancePolicyApi(page, testInstanceId, { saveDelayMs: 400, saveStatus: 503 });
		await page.goto(`/instances/${testInstanceId}/policy`);

		const saveBtn = page.getByRole('button', { name: /Save|Speichern/i }).first();
		await expect(saveBtn).toBeVisible({ timeout: 3000 });

		await saveBtn.click();
		// Button shows pending state (disabled)
		await expect(saveBtn).toBeDisabled({ timeout: 500 });
		// Recovers after 503 response
		await expect(saveBtn).toBeEnabled({ timeout: 3000 });
	});

	test('system: delayed 10s API response shows loading and remains interactive', async ({
		page
	}) => {
		await mockSettingsApi(page, { delayMs: 10_000 });
		await page.goto('/system');

		const loading = page.locator('[aria-live="polite"]').first();
		await expect(loading).toBeVisible({ timeout: 1000 });

		const instancesLink = page.getByRole('link', { name: /Instances|Instanzen/i }).first();
		await instancesLink.click();
		await expect(page).toHaveURL(/\/instances/, { timeout: 1000 });
	});

	async function assertProblemNoteRecovers(
		page: Page,
		fieldSelector: string,
		value: string
	): Promise<void> {
		const problem = page.locator('.problem-note, [role="alert"]').first();
		await expect(problem).toBeVisible({ timeout: 12_000 });

		// API recovers, reload brings usable form
		await mockSettingsApi(page);
		await page.reload();
		const field = page.locator(`[data-field="${fieldSelector}"]`);
		await expect(field).toBeVisible({ timeout: 3000 });
		await field.fill(value);
		await expect(field).toHaveValue(value);
	}

	test('system: 503 renders problem note and form recovers', async ({ page }) => {
		await mockSettingsApi(page, { status: 503 });
		await page.goto('/system');
		await assertProblemNoteRecovers(page, 'history_retention_days', '60');
	});

	test('about: delayed 10s licences response shows loading and remains interactive', async ({
		page
	}) => {
		await mockAboutApi(page, { delayMs: 10_000 });
		await page.goto('/system?tab=about');

		// Loading indicator visible within 1000 ms
		const loading = page.locator('[aria-live="polite"]').first();
		await expect(loading).toBeVisible({ timeout: 1000 });

		// Navigation stays interactive: click instances link within 1000 ms
		const instancesLink = page.getByRole('link', { name: /Instances|Instanzen/i }).first();
		await instancesLink.click();
		await expect(page).toHaveURL(/\/instances/, { timeout: 1000 });
	});

	test('about: licences 503 renders problem note and tab recovers on reload', async ({ page }) => {
		await mockAboutApi(page, { status: 503 });
		await page.goto('/system?tab=about');

		// Problem note visible after retries exhaust
		const problem = page.locator('.problem-note, [role="alert"]').first();
		await expect(problem).toBeVisible({ timeout: 12_000 });

		// Licences recover, reload shows the ecosystem tabs
		await mockAboutApi(page);
		await page.reload();
		await expect(page.getByRole('tab', { name: /Go \(1\)/ })).toBeVisible({ timeout: 3000 });
	});

	test('automation caps: delayed 10s API response shows loading, remains interactive, and 503 renders problem note', async ({
		page
	}) => {
		await mockSettingsApi(page);
		let requestCount = 0;
		await page.route('**/api/v1/settings*', async (route) => {
			requestCount++;
			if (requestCount === 1) {
				await new Promise((r) => setTimeout(r, 10_000));
			}
			await route.fulfill({
				status: 503,
				contentType: 'application/problem+json',
				body: JSON.stringify({
					type: 'https://tools.ietf.org/html/rfc9457',
					title: 'Service Unavailable',
					status: 503,
					detail: 'Settings unavailable'
				})
			});
		});

		await page.goto('/automation?tab=caps');

		const nav = page.getByRole('navigation', { name: /SnatchArr/i }).first();
		await expect(nav).toBeVisible({ timeout: 1000 });

		const loading = page.locator('[aria-live="polite"]').first();
		await expect(loading).toBeVisible({ timeout: 1000 });

		const instancesLink = page.getByRole('link', { name: /Instances|Instanzen/i }).first();
		await instancesLink.click();
		await expect(page).toHaveURL(/\/instances/, { timeout: 1000 });

		await page.goto('/automation?tab=caps');
		await assertProblemNoteRecovers(page, 'global_hourly_cap', '75');
	});

	test('system: save action shows pending state and recovers on 503', async ({ page }) => {
		await mockSettingsApi(page, { saveDelayMs: 400, saveStatus: 503 });
		await page.goto('/system');

		const saveBtn = page.getByRole('button', { name: /Save|Speichern/i }).first();
		await expect(saveBtn).toBeVisible({ timeout: 3000 });

		await saveBtn.click();
		await expect(saveBtn).toBeDisabled({ timeout: 500 });
		await expect(saveBtn).toBeEnabled({ timeout: 3000 });
	});

	test('activity events: delayed 10s API response shows skeleton, remains interactive, and 503 renders problem note', async ({
		page
	}) => {
		await mockSettingsApi(page);
		let requestCount = 0;
		await page.route('**/api/v1/events*', async (route) => {
			requestCount++;
			if (requestCount === 1) {
				await new Promise((r) => setTimeout(r, 10_000));
			}
			await route.fulfill({
				status: 503,
				contentType: 'application/problem+json',
				body: JSON.stringify({
					type: 'https://tools.ietf.org/html/rfc9457',
					title: 'Service Unavailable',
					status: 503,
					detail: 'Events store unavailable'
				})
			});
		});

		await page.goto('/activity');

		const nav = page.getByRole('navigation', { name: /SnatchArr/i }).first();
		await expect(nav).toBeVisible({ timeout: 1000 });

		const skeleton = page.locator('[data-testid="activity-skeleton"]').first();
		await expect(skeleton).toBeVisible({ timeout: 1000 });

		const automationLink = page.getByRole('link', { name: /Automation|Automatisierung/i }).first();
		await automationLink.click();
		await expect(page).toHaveURL(/\/automation/, { timeout: 1000 });

		await page.goto('/activity');
		const problem = page.getByRole('alert').first();
		await expect(problem).toBeVisible({ timeout: 12_000 });
	});

	test('activity logs: delayed 10s API response shows skeleton, remains interactive, and 503 renders problem note', async ({
		page
	}) => {
		await mockSettingsApi(page, { role: 'admin' });
		let requestCount = 0;
		await page.route('**/api/v1/logs*', async (route) => {
			requestCount++;
			if (requestCount === 1) {
				await new Promise((r) => setTimeout(r, 10_000));
			}
			await route.fulfill({
				status: 503,
				contentType: 'application/problem+json',
				body: JSON.stringify({
					type: 'https://tools.ietf.org/html/rfc9457',
					title: 'Service Unavailable',
					status: 503,
					detail: 'Logs unavailable'
				})
			});
		});

		await page.goto('/activity?source=log');

		const skeleton = page.locator('[data-testid="skeleton"]').first();
		await expect(skeleton).toBeVisible({ timeout: 1000 });

		const instancesLink = page.getByRole('link', { name: /Instances|Instanzen/i }).first();
		await instancesLink.click();
		await expect(page).toHaveURL(/\/instances/, { timeout: 1000 });

		await page.goto('/activity?source=log');
		const problem = page.getByRole('alert').first();
		await expect(problem).toBeVisible({ timeout: 12_000 });
	});

	test('connections hub: planted 10s delay then 503 on one service status endpoint leaves nav responsive and other cards render', async ({
		page
	}) => {
		await mockSignedInApi(page, 'admin');

		let requestCount = 0;
		await page.route('**/api/v1/prowlarr/status*', async (route) => {
			requestCount++;
			if (requestCount === 1) {
				await new Promise((r) => setTimeout(r, 10_000));
			}
			await route.fulfill({
				status: 503,
				contentType: 'application/problem+json',
				body: JSON.stringify({
					type: 'https://tools.ietf.org/html/rfc9457',
					title: 'Service Unavailable',
					status: 503,
					detail: 'Prowlarr status unreachable'
				})
			});
		});

		await page.goto('/connections');

		// 1. Shell nav and header are responsive within 1000 ms
		const nav = page.getByRole('navigation', { name: /SnatchArr/i }).first();
		await expect(nav).toBeVisible({ timeout: 1000 });

		// 2. Other cards render while prowlarr/status is delayed
		const dlHeading = page.getByRole('heading', { name: /Download clients/i });
		await expect(dlHeading).toBeVisible({ timeout: 1000 });

		// 3. Nav click changes URL within 1000 ms
		const instancesLink = page.getByRole('link', { name: /Instances|Instanzen/i }).first();
		await instancesLink.click();
		await expect(page).toHaveURL(/\/instances/, { timeout: 1000 });

		// 4. Return to /connections: 503 renders ProblemNote on Prowlarr card while others remain rendered
		await page.goto('/connections');
		const problem = page.locator('.problem-note, [role="alert"]').first();
		await expect(problem).toBeVisible({ timeout: 12_000 });
		await expect(dlHeading).toBeVisible();
	});
});
