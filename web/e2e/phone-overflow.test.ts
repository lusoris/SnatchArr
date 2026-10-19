// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import fs from 'node:fs';
import path from 'node:path';
import { expect, test, type Page } from '@playwright/test';

test.use({
	viewport: { width: 390, height: 844 },
	colorScheme: 'light'
});

function getSeededInstanceId(): string {
	const seedFile = path.resolve(import.meta.dirname, '.auth/seed.json');
	if (!fs.existsSync(seedFile)) {
		throw new Error(`seed file does not exist: ${seedFile}`);
	}
	let data: { instanceId?: string };
	try {
		data = JSON.parse(fs.readFileSync(seedFile, 'utf-8')) as { instanceId?: string };
	} catch (err) {
		throw new Error(
			`unreadable seed file ${seedFile}: ${err instanceof Error ? err.message : String(err)}`,
			{ cause: err }
		);
	}
	if (!data.instanceId) {
		throw new Error(`seed file ${seedFile} missing instanceId field`);
	}
	return data.instanceId;
}

async function assertNoHorizontalOverflow(page: Page, route: string) {
	await page.waitForLoadState('networkidle');
	const { scrollWidth, innerWidth } = await page.evaluate(() => ({
		scrollWidth: document.documentElement.scrollWidth,
		innerWidth: window.innerWidth
	}));
	expect(
		scrollWidth,
		`Route ${route} has horizontal overflow: scrollWidth (${scrollWidth}px) > innerWidth (${innerWidth}px)`
	).toBeLessThanOrEqual(innerWidth);
}

test.beforeAll(async ({ request }) => {
	// Seed API key for /account and notification channel/rule for /settings
	const sessionRes = await request.get('/api/v1/auth/session');
	if (sessionRes.ok()) {
		const sessionData = (await sessionRes.json()) as { csrf_token?: string };
		if (sessionData.csrf_token) {
			const csrfHeaders = { 'X-CSRF-Token': sessionData.csrf_token };

			// Seed an API key
			await request.post('/api/v1/auth/api-keys', {
				headers: csrfHeaders,
				data: { name: 'phone-test-key' }
			});

			// Seed a notification channel
			const chanRes = await request.post('/api/v1/notifications/channels', {
				headers: csrfHeaders,
				data: {
					name: 'phone-webhook-channel',
					kind: 'webhook',
					config: {
						url: 'http://127.0.0.1:9091/webhook',
						allow_private_networks: true,
						allow_insecure_http: true
					},
					enabled: true
				}
			});

			if (chanRes.ok()) {
				const chan = (await chanRes.json()) as { id: string };
				await request.post('/api/v1/notifications/rules', {
					headers: csrfHeaders,
					data: {
						name: 'phone-streak-rule',
						event_kind: 'run_failure_streak',
						threshold: 2,
						channel_ids: [chan.id]
					}
				});
			}
		}
	}
});

test('authenticated: phone overflow /instances', async ({ page }) => {
	await page.goto('/instances');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('table')).toBeVisible();
	await assertNoHorizontalOverflow(page, '/instances');
});

test('authenticated: phone overflow /account', async ({ page }) => {
	await page.goto('/account');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('table')).toBeVisible();
	await assertNoHorizontalOverflow(page, '/account');
});

test('authenticated: phone overflow /schedules', async ({ page }) => {
	await page.goto('/schedules');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('table')).toBeVisible();
	await assertNoHorizontalOverflow(page, '/schedules');
});

test('authenticated: phone overflow /seerr', async ({ page }) => {
	await page.route('**/api/v1/seerr/requests', async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([
				{
					link_id: '11111111-1111-1111-1111-111111111111',
					link_name: 'test-seerr-link',
					request_id: 101,
					media_type: 'movie',
					tmdb_id: 12345,
					tvdb_id: 0,
					title: 'A Very Long Movie Title For Seerr Request Testing',
					request_status: 2,
					media_status: 3,
					is_4k: true,
					requested_by: 'requester@example.com',
					seasons: [],
					resolved: false,
					unresolved_reason: 'missing',
					last_seen_at: '2026-10-19T10:00:00Z',
					last_snatched_at: '2026-10-19T10:00:00Z'
				}
			])
		});
	});

	await page.goto('/seerr');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('table')).toBeVisible();
	await assertNoHorizontalOverflow(page, '/seerr');
});

test('authenticated: phone overflow /settings', async ({ page }) => {
	await page.goto('/settings');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('table').first()).toBeVisible();
	await assertNoHorizontalOverflow(page, '/settings');
});

test('authenticated: phone overflow /logs', async ({ page }) => {
	await page.route('**/api/v1/logs?*', async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({
				records: [
					{
						seq: 1,
						time: '2026-10-19T10:00:00Z',
						level: 'info',
						source: 'phone-test',
						message:
							'long_unbroken_message_text_without_spaces_that_must_wrap_properly_at_phone_viewport_390px_0123456789_abcdefghijklmnopqrstuvwxyz',
						attrs: []
					}
				],
				capacity: 500,
				dropped: 0
			})
		});
	});

	await page.goto('/logs');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await assertNoHorizontalOverflow(page, '/logs');
});

test('authenticated: phone overflow /instances/[id]', async ({ page }) => {
	const instanceId = getSeededInstanceId();

	await page.route('**/api/v1/runs?*', async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([
				{
					id: '00000000-0000-0000-0000-000000000010',
					instance_id: instanceId,
					kind: 'missing',
					status: 'completed',
					dry_run: true,
					started_at: '2026-10-19T10:00:00Z',
					finished_at: '2026-10-19T10:05:00Z',
					searched_count: 42
				}
			])
		});
	});

	await page.route('**/api/v1/instances/*/preview?*', async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ run_id: 'seeded-preview-run' })
		});
	});

	await page.route('**/api/v1/runs/seeded-preview-run', async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({
				id: 'seeded-preview-run',
				instance_id: instanceId,
				kind: 'missing',
				status: 'done',
				dry_run: true,
				previewed_targets: [
					{
						id: 1001,
						title: 'A Very Long Series Target Title For Overflow Verification',
						entity_type: 'episode',
						cost: 1
					}
				]
			})
		});
	});

	await page.goto(`/instances/${instanceId}`);
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await assertNoHorizontalOverflow(page, `/instances/${instanceId}`);

	// Activity tab: trigger preview so PreviewPanel table renders
	await page.goto(`/instances/${instanceId}/activity`);
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	const previewBtn = page.getByRole('button', { name: /preview missing|fehlende vorschauen/i });
	await expect(previewBtn).toBeVisible();
	await previewBtn.click();
	await expect(page.getByRole('table').first()).toBeVisible();

	await assertNoHorizontalOverflow(page, `/instances/${instanceId}/activity`);
});
