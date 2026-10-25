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
	await expect(page.getByRole('list', { name: 'Instances' })).toBeVisible();
	await assertNoHorizontalOverflow(page, '/instances');
});

test('authenticated: phone overflow /instances at 375px', async ({ page }) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await page.goto('/instances');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('list', { name: 'Instances' })).toBeVisible();
	await assertNoHorizontalOverflow(page, '/instances');
});

test('authenticated: phone overflow /instances at 414px', async ({ page }) => {
	await page.setViewportSize({ width: 414, height: 896 });
	await page.goto('/instances');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('list', { name: 'Instances' })).toBeVisible();
	await assertNoHorizontalOverflow(page, '/instances');
});

test('authenticated: phone overflow /account', async ({ page }) => {
	await page.goto('/account');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('table')).toBeVisible();
	await assertNoHorizontalOverflow(page, '/account');
});

test('authenticated: phone overflow /automation', async ({ page }) => {
	await page.goto('/automation');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('table')).toBeVisible();
	await assertNoHorizontalOverflow(page, '/automation');
});

test('authenticated: phone overflow /automation?tab=caps', async ({ page }) => {
	await page.goto('/automation?tab=caps');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.locator('[data-field="global_hourly_cap"]')).toBeVisible();
	await assertNoHorizontalOverflow(page, '/automation?tab=caps');
});

test('authenticated: phone overflow /automation?tab=season-gate', async ({ page }) => {
	await page.route('**/api/v1/system/modules', (route) =>
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([{ id: 'watchaware', enabled: true }])
		})
	);
	await page.route('**/api/v1/instances*', (route) =>
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([
				{
					id: '00000000-0000-0000-0000-000000000001',
					name: 'Sonarr 1',
					kind: 'sonarr',
					base_url: 'http://sonarr.local',
					enabled: true,
					source: 'manual',
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z'
				}
			])
		})
	);
	await page.route('**/api/v1/watchaware/gate/series*', (route) =>
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([
				{
					instance_id: '00000000-0000-0000-0000-000000000001',
					series_id: 1,
					tvdb_id: 101,
					title: 'Example Series',
					gate_season: 1,
					state: 'gated',
					added_before_gate: false,
					held_seasons: [2, 3],
					updated_at: '2026-01-01T00:00:00Z'
				}
			])
		})
	);
	await page.route('**/api/v1/watchaware/gate/decisions*', (route) =>
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([
				{
					id: '00000000-0000-0000-0000-000000000002',
					instance_id: '00000000-0000-0000-0000-000000000001',
					series_id: 1,
					season_number: 2,
					action: 'unmonitor',
					would_act: false,
					reason: 'season after gate',
					decided_at: '2026-01-01T00:00:00Z'
				}
			])
		})
	);
	await page.goto('/automation?tab=season-gate');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await assertNoHorizontalOverflow(page, '/automation?tab=season-gate');
});

test('authenticated: phone overflow /connections/seerr', async ({ page }) => {
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

	await page.goto('/connections/seerr');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('table')).toBeVisible();
	await assertNoHorizontalOverflow(page, '/connections/seerr');
});

test('authenticated: phone overflow /system', async ({ page }) => {
	await page.goto('/system');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await assertNoHorizontalOverflow(page, '/system');
});

test('authenticated: phone overflow /system?tab=modules', async ({ page }) => {
	await page.goto('/system?tab=modules');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await assertNoHorizontalOverflow(page, '/system?tab=modules');
});

test('authenticated: phone overflow /system?tab=notifications', async ({ page }) => {
	await page.goto('/system?tab=notifications');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('table').first()).toBeVisible();
	await assertNoHorizontalOverflow(page, '/system?tab=notifications');
});

test('authenticated: phone overflow /activity?source=log', async ({ page }) => {
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
						source:
							'phone-test-long-source-without-spaces-that-must-wrap-properly-0123456789abcdefghijklmnopqrstuvwxyz',
						message:
							'long_unbroken_message_text_without_spaces_that_must_wrap_properly_at_phone_viewport_390px_0123456789_abcdefghijklmnopqrstuvwxyz',
						attrs: [
							{
								key: 'url',
								value:
									'https://example.com/very/long/unbroken/path/that/exceeds/two/hundred/characters/without/any/spaces/012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789'
							}
						]
					}
				],
				capacity: 500,
				dropped: 0
			})
		});
	});

	await page.goto('/activity?source=log');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await assertNoHorizontalOverflow(page, '/activity?source=log');
});

test('authenticated: desktop overflow /activity?source=log', async ({ page }) => {
	await page.setViewportSize({ width: 1280, height: 800 });
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
						source:
							'desktop-test-long-source-without-spaces-that-must-wrap-properly-0123456789abcdefghijklmnopqrstuvwxyz',
						message:
							'long_unbroken_message_text_without_spaces_that_must_wrap_properly_at_desktop_viewport_1280px_0123456789_abcdefghijklmnopqrstuvwxyz',
						attrs: [
							{
								key: 'url',
								value:
									'https://example.com/very/long/unbroken/path/that/exceeds/two/hundred/characters/without/any/spaces/012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789'
							}
						]
					}
				],
				capacity: 500,
				dropped: 0
			})
		});
	});

	await page.goto('/activity?source=log');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await assertNoHorizontalOverflow(page, '/activity?source=log');
});

test('authenticated: phone overflow /activity', async ({ page }) => {
	await page.route('**/api/v1/events?*', async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([
				{
					id: 101,
					instance_id: '00000000-0000-0000-0000-000000000001',
					ts: '2026-10-19T10:00:00Z',
					level: 'info',
					type: 'search_dispatched',
					title:
						'A Very Long Event Title For History Testing That Spans Multiple Words And Causes Horizontal Overflow If The Table Is Not Contained In A Scroll Region',
					detail: 'Searched 42 items across all indexers for this instance'
				}
			])
		});
	});

	await page.goto('/activity');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('log')).toBeVisible();
	await assertNoHorizontalOverflow(page, '/activity');
});

test('authenticated: html[lang] follows app locale switch from de to en', async ({
	page,
	baseURL
}) => {
	await page
		.context()
		.addCookies([
			{ name: 'PARAGLIDE_LOCALE', value: 'de', url: baseURL ?? 'http://127.0.0.1:4173' }
		]);
	await page.setViewportSize({ width: 1280, height: 800 });
	await page.goto('/settings');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.locator('html')).toHaveAttribute('lang', 'de');

	const langSelect = page.locator('#desktop-lang');
	await expect(langSelect).toBeVisible();
	await langSelect.selectOption('en');
	await expect(page.locator('html')).toHaveAttribute('lang', 'en');
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

test('authenticated: no control under bottom bar at 390px', async ({ page }) => {
	await page.goto('/settings');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	const bottomBar = page
		.locator('nav')
		.filter({ has: page.getByRole('button', { name: /More|Mehr/i }) });
	await expect(bottomBar).toBeVisible();
	const bottomBarBox = await bottomBar.boundingBox();
	expect(bottomBarBox).toBeTruthy();

	// Scroll to bottom
	await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));

	const controls = page.locator('#main button, #main a, #main input, #main select');
	const count = await controls.count();
	expect(count).toBeGreaterThan(0);

	for (let i = 0; i < count; i++) {
		const control = controls.nth(i);
		if (await control.isVisible()) {
			const box = await control.boundingBox();
			if (box && bottomBarBox && box.y >= 0 && box.y < 844) {
				expect(
					box.y + box.height,
					`Control at index ${i} is obscured by bottom bar: bottom edge (${box.y + box.height}) > bar top (${bottomBarBox.y})`
				).toBeLessThanOrEqual(bottomBarBox.y + 1);
			}
		}
	}
});
