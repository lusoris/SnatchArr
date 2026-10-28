// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
// SPDX-License-Identifier: EUPL-1.2
import { expect, test, type Page } from '@playwright/test';
import { json, mockInstancePolicyApi } from './mocks.js';

const instanceId = '00000000-0000-0000-0000-000000000001';
const runId = '10000000-0000-0000-0000-000000000001';
const events = ['run_started', 'search_dispatched', 'run_completed'].map((type, index) => ({
	id: index + 1,
	instance_id: instanceId,
	run_id: runId,
	type,
	title: `Event ${index + 1}`,
	level: 'info',
	ts: '2026-10-28T12:00:00Z'
}));

/** Every matched item must open a detail dialog or navigate; identify dead items by index. */
export async function assertAllOpenDetail(page: Page, selector: string): Promise<void> {
	const items = page.locator(selector);
	await expect(items.first()).toBeVisible();
	const count = await items.count();
	expect(count, `${selector}: expected at least one item`).toBeGreaterThanOrEqual(1);
	for (let index = 0; index < count; index += 1) {
		const before = page.url();
		const type = await items.nth(index).getAttribute('data-event-type');
		await items.nth(index).click();
		await expect
			.poll(async () => (await page.getByRole('dialog').isVisible()) || page.url() !== before, {
				message: `${selector} item index ${index}: click opened no dialog and changed no URL`
			})
			.toBe(true);
		if (type) await expect(page.getByRole('dialog'), `event index ${index}`).toBeVisible();
		if (page.url() !== before) {
			await page.goBack();
			await expect(items.nth(index)).toBeVisible();
		} else {
			const dialog = page.getByRole('dialog');
			await expect(dialog).toBeVisible();
			if (type) await expect(dialog.getByText(type, { exact: true })).toBeVisible();
			await page.keyboard.press('Escape');
			await expect(dialog).toBeHidden();
		}
	}
}

async function mockEntityApi(page: Page): Promise<void> {
	await mockInstancePolicyApi(page, instanceId);
	await page.route('**/api/v1/instances', (route) =>
		route.fulfill(
			json([
				{
					id: instanceId,
					name: 'Library Sonarr',
					kind: 'sonarr',
					source: 'manual',
					enabled: true,
					base_url: 'http://sonarr.local:8989',
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z'
				}
			])
		)
	);
	await page.route('**/api/v1/events*', (route) => route.fulfill(json(events)));
	await page.route(`**/api/v1/runs/${runId}`, (route) =>
		route.fulfill(
			json({
				id: runId,
				instance_id: instanceId,
				kind: 'missing',
				status: 'done',
				queued_at: '2026-10-28T12:00:00Z',
				searched_count: 2
			})
		)
	);
}

test('authenticated: every dashboard event opens its detail dialog', async ({ page }) => {
	await mockEntityApi(page);
	await page.goto('/');
	const rows = page.getByTestId('dashboard-event');
	await expect(rows).toHaveCount(events.length);
	await assertAllOpenDetail(page, '[data-testid=dashboard-event]');
});

test('authenticated: event instance link opens instance detail', async ({ page }) => {
	await mockEntityApi(page);
	await page.goto('/');
	await page.getByTestId('dashboard-event').first().click();
	await page.getByRole('dialog').getByRole('link', { name: instanceId }).click();
	await expect(page).toHaveURL(new RegExp(`/instances/${instanceId}$`));
});
