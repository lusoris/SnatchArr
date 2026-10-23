// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Rendered layout of every authenticated route at desktop and phone width (API mode). With
// SNATCHARR_SERVED_URL set, the served-layout project runs the same checks against a deployed
// site and stores one full-page screenshot per route and viewport.
import fs from 'node:fs';
import path from 'node:path';
import { expect, test, type Page } from '@playwright/test';
import {
	assertRenderedLayout,
	AUTH_ROUTES,
	collectLists,
	expectedTables,
	VIEWPORTS
} from './layout.js';
import { json, mockSettingsApi } from './mocks.js';

const served = !!process.env.SNATCHARR_SERVED_URL;
if (served && !process.env.SNATCHARR_SERVED_STATE) {
	throw new Error('SNATCHARR_SERVED_STATE is required with a served URL');
}
// Screenshots: always against a served site, locally on request (SNATCHARR_LAYOUT_SHOTS=1).
const shots = served || !!process.env.SNATCHARR_LAYOUT_SHOTS;

function seededInstanceId(): string {
	const seedFile = path.resolve(import.meta.dirname, '.auth/seed.json');
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

function instanceId(): string {
	const fromEnv = process.env.SNATCHARR_SERVED_INSTANCE_ID;
	if (served) {
		if (!fromEnv) throw new Error('SNATCHARR_SERVED_INSTANCE_ID is required with a served URL');
		return fromEnv;
	}
	return seededInstanceId();
}

// History rows as the served page showed them, plus one long Detail: the row that used to size
// its own columns and push the others out of line.
const unbrokenToken120 = 'x'.repeat(120);
const historyEvents = [
	{
		id: 3,
		instance_id: '00000000-0000-0000-0000-000000000001',
		ts: '2026-10-21T06:19:00Z',
		level: 'debug',
		type: 'candidates_filtered',
		detail:
			'43 candidate(s) after filters and afterglow; 2 skipped as queued or grabbed within 24 h; 0 skipped as not yet available; 13 requested via Seerr ' +
			unbrokenToken120
	},
	{
		id: 2,
		instance_id: '00000000-0000-0000-0000-000000000001',
		ts: '2026-10-21T06:19:00Z',
		level: 'warn',
		type: 'stamina_exhausted',
		title: 'Stamina exhausted',
		detail: 'hourly cap (15) exhausted'
	},
	{
		id: 1,
		instance_id: '00000000-0000-0000-0000-000000000001',
		ts: '2026-10-21T06:19:00Z',
		level: 'info',
		type: 'search_dispatched',
		title: 'S11E04 The Lost Art of Forehead Sweat',
		detail: 'command 703361'
	}
];

for (const [viewportName, viewport] of Object.entries(VIEWPORTS)) {
	for (const pattern of AUTH_ROUTES) {
		test(`authenticated: layout ${pattern} ${viewportName}`, async ({ page }, testInfo) => {
			await page.setViewportSize(viewport);
			if (pattern === '/history' && !served) {
				await page.route('**/api/v1/events*', (route) =>
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(historyEvents)
					})
				);
			} else if (pattern === '/instances/[id]/activity' && !served) {
				await page.route('**/api/v1/runs?*', (route) =>
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify([
							{
								id: '00000000-0000-0000-0000-000000000010',
								instance_id: instanceId(),
								kind: 'missing',
								status: 'completed',
								dry_run: true,
								started_at: '2026-10-21T06:19:00Z',
								finished_at: '2026-10-21T06:20:00Z',
								searched_count: 42
							}
						])
					})
				);
			} else if (pattern === '/instances/[id]/queue' && !served) {
				await page.route('**/api/v1/instances/*/queue-strikes*', (route) =>
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify([
							{
								download_id: 'mock-download-101',
								queue_id: 1,
								rule: 'stalled',
								count: 3,
								threshold: 5,
								last_seen: '2026-10-21T06:19:00Z'
							}
						])
					})
				);
			} else if (pattern === '/prowlarr' && !served) {
				await page.route('**/api/v1/prowlarr/status*', (route) =>
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify([
							{
								link_id: '00000000-0000-0000-0000-000000000001',
								name: 'mock-prowlarr',
								reachable: true,
								checked_at: '2026-10-21T06:19:00Z',
								indexers: [
									{
										id: 1,
										name: 'Mock Indexer',
										limits_unit: 1,
										used_queries: 5,
										query_limit: 100,
										used_grabs: 2,
										grab_limit: 50,
										exhausted: false
									}
								]
							}
						])
					})
				);
			} else if (pattern === '/seerr' && !served) {
				await page.route('**/api/v1/seerr/requests*', (route) =>
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify([
							{
								link_id: '11111111-1111-1111-1111-111111111111',
								link_name: 'mock-seerr-link',
								request_id: 101,
								media_type: 'movie',
								tmdb_id: 12345,
								tvdb_id: 0,
								title: 'Mock Seerr Request Title',
								request_status: 2,
								media_status: 3,
								is_4k: false,
								requested_by: 'user@example.com',
								instance_name: 'seeded-sonarr',
								seasons: [],
								resolved: true,
								last_seen_at: '2026-10-21T06:19:00Z',
								last_snatched_at: '2026-10-21T06:19:00Z'
							}
						])
					})
				);
			}
			const route = pattern.replace('[id]', instanceId());
			const lists = collectLists(page);
			await page.goto(route);
			expect(page.url(), `${pattern}: landed on /login (unauthenticated)`).not.toMatch(
				/\/login(?:\?|$)/
			);
			await assertRenderedLayout(page, pattern, lists);
			if (shots) {
				const name = `${pattern.replace(/[/[\]]+/g, '-').replace(/^-|-$/g, '') || 'root'}-${viewportName}.png`;
				await page.screenshot({ path: testInfo.outputPath(name), fullPage: true });
			}
		});
	}
}

test('authenticated: layout expected tables follow role and data', () => {
	// Admin with the CI seed: one table per non-empty list.
	expect(expectedTables('/schedules', 'admin', { '/api/v1/schedules': 1 })).toBe(1);
	expect(
		expectedTables('/settings', 'admin', {
			'/api/v1/notifications/channels': 1,
			'/api/v1/notifications/rules': 0
		})
	).toBe(1);
	expect(expectedTables('/account', 'admin', { '/api/v1/auth/api-keys': 2 })).toBe(1);
	// Viewer: the same lists, empty data shows no table.
	expect(expectedTables('/schedules', 'viewer', { '/api/v1/schedules': 0 })).toBe(0);
	expect(expectedTables('/account', 'viewer', { '/api/v1/auth/api-keys': 0 })).toBe(0);
	// A viewer owning an API key, or a list the page never received, fails.
	expect(() => expectedTables('/account', 'viewer', { '/api/v1/auth/api-keys': 1 })).toThrow(
		/ADR-0023/
	);
	expect(() => expectedTables('/settings', 'viewer', {})).toThrow(/gave the page no JSON list/);
});

// Viewer runs in API mode (the served viewer run uses the served-layout project): the session
// reports role viewer, and the lists carry either the served deployment's empty data or one
// row each. The table count must follow role and data in both.
const STAMP = '2026-10-21T06:19:00Z';
const viewerData = {
	empty: { schedules: [], channels: [], rules: [] },
	filled: {
		schedules: [
			{
				id: '00000000-0000-0000-0000-000000000021',
				name: 'Night pause',
				days: [1, 2, 3, 4, 5],
				start: '00:00',
				end: '06:00',
				tz: 'UTC',
				action: 'pause',
				enabled: true,
				created_at: STAMP,
				updated_at: STAMP
			}
		],
		channels: [
			{
				id: '00000000-0000-0000-0000-000000000031',
				name: 'ops-webhook',
				kind: 'webhook',
				enabled: true,
				has_secret: false,
				config: { allow_private_network: false, allow_insecure_http: false },
				created_at: STAMP,
				updated_at: STAMP
			}
		],
		rules: [
			{
				id: '00000000-0000-0000-0000-000000000041',
				name: 'run failures',
				event_kind: 'run_failure_streak',
				threshold: 2,
				enabled: true,
				channel_ids: ['00000000-0000-0000-0000-000000000031'],
				created_at: STAMP,
				updated_at: STAMP
			}
		]
	}
} as const;

async function mockViewerApi(page: Page, data: (typeof viewerData)[keyof typeof viewerData]) {
	await mockSettingsApi(page, { role: 'viewer' });
	// Registered later, so these answer before the empty lists of mockSettingsApi.
	await page.route('**/api/v1/schedules', (r) => r.fulfill(json(data.schedules)));
	await page.route('**/api/v1/notifications/channels', (r) => r.fulfill(json(data.channels)));
	await page.route('**/api/v1/notifications/rules', (r) => r.fulfill(json(data.rules)));
	await page.route('**/api/v1/auth/api-keys', (r) => r.fulfill(json([])));
}

if (!served) {
	for (const [viewportName, viewport] of Object.entries(VIEWPORTS)) {
		for (const [dataName, data] of Object.entries(viewerData)) {
			for (const pattern of ['/schedules', '/settings', '/account']) {
				test(`authenticated: layout viewer ${dataName} ${pattern} ${viewportName}`, async ({
					page
				}) => {
					await page.setViewportSize(viewport);
					await mockViewerApi(page, data);
					const lists = collectLists(page);
					await page.goto(pattern);
					await assertRenderedLayout(page, pattern, lists);
				});
			}
		}
	}
}
