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
	assertDesktopWidth,
	assertPhoneFit,
	assertGridRowAlignment,
	assertRenderedLayout,
	assertScrollBudget,
	assertTableFits,
	assertTouchTargetHeights,
	AUTH_ROUTES,
	collectLists,
	expectedTables,
	measureContentSpan,
	PHONE_FIT_ROUTES,
	measureTouchTargets,
	SCROLL_BUDGET_ROUTES,
	VIEWPORTS,
	WIDTH_PENDING
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

// A transport error carries the full client URL in its text (seen on main with the seeded
// download client); /logs and /live must wrap it at URL delimiters, never mid-word.
const urlErrorText =
	'Post "http://127.0.0.1:9091/transmission/rpc": dial tcp 127.0.0.1:9091: connect: connection refused';
const urlRowFixtures: Partial<
	Record<(typeof AUTH_ROUTES)[number], { glob: string; body: unknown }>
> = {
	'/activity?source=log': {
		glob: '**/api/v1/logs?*',
		body: {
			records: [
				{
					seq: 1,
					time: '2026-10-21T06:19:00Z',
					level: 'error',
					source: 'downloadclients',
					message: urlErrorText,
					attrs: [{ key: 'url', value: 'http://127.0.0.1:9091/transmission/rpc' }]
				}
			],
			capacity: 500,
			dropped: 0
		}
	},
	'/activity?source=live': {
		glob: '**/api/v1/events?*',
		body: [
			{
				id: 1,
				ts: '2026-10-21T06:19:00Z',
				level: 'error',
				type: 'run_failed',
				title: 'Download client unreachable',
				detail: urlErrorText
			}
		]
	}
};

// A fixture keyed by a path the sweep no longer visits would leave its URL check silently unrun.
test('authenticated: URL-row fixtures name swept routes', () => {
	const swept: readonly string[] = AUTH_ROUTES;
	const stale = Object.keys(urlRowFixtures).filter((key) => !swept.includes(key));
	expect(stale).toEqual([]);
});

async function mockUrlRows(page: Page, pattern: (typeof AUTH_ROUTES)[number]): Promise<void> {
	const fixture = urlRowFixtures[pattern];
	if (!fixture || served) return;
	await page.route(fixture.glob, (route) => route.fulfill(json(fixture.body)));
}

const overflowDashboardInstance = [
	{
		id: '00000000-0000-0000-0000-000000000001',
		name: 'sonarr Instance',
		kind: 'sonarr' as const,
		base_url: 'http://sonarr.anime.remux.media.svc.cluster.local:8989',
		source: 'manual' as const,
		enabled: true,
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	}
];

const hyphenatedInstancesList = [
	{
		id: '00000000-0000-0000-0000-000000000001',
		name: 'radarr-4k',
		kind: 'radarr' as const,
		base_url: 'http://radarr-4k.media.svc.cluster.local:7878',
		source: 'configarr' as const,
		enabled: true,
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	},
	{
		id: '00000000-0000-0000-0000-000000000002',
		name: 'sonarr-4k',
		kind: 'sonarr' as const,
		base_url: 'http://sonarr-4k.anime.svc.cluster.local:8989',
		source: 'manual' as const,
		enabled: true,
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	}
];

const dashboardHourlyCaps = [
	{
		scope: 'instance' as const,
		instance_id: '00000000-0000-0000-0000-000000000001',
		used: 12,
		cap: 15,
		resets_at: '2026-10-21T07:00:00Z'
	}
];

const dashboardProwlarrStatus = [
	{
		link_id: '00000000-0000-0000-0000-000000000001',
		name: 'Prowlarr',
		reachable: true,
		checked_at: '2026-10-21T06:19:00Z',
		indexers: Array.from({ length: 31 }, (_, i) => ({
			id: i + 1,
			name: `Indexer ${i + 1}`,
			limits_unit: 1,
			used_queries: i < 3 ? 100 : 5,
			query_limit: 100,
			used_grabs: 0,
			grab_limit: 50,
			exhausted: i < 3
		}))
	}
];

async function mockDashboardRoute(page: Page): Promise<void> {
	await page.route('**/api/v1/instances', (route) => {
		if (route.request().method() !== 'GET') {
			return route.fallback();
		}
		return route.fulfill(json(overflowDashboardInstance));
	});
	await page.route('**/api/v1/hourly-caps', (route) => {
		if (route.request().method() !== 'GET') {
			return route.fallback();
		}
		return route.fulfill(json(dashboardHourlyCaps));
	});
	await page.route('**/api/v1/prowlarr/status*', (route) => {
		if (route.request().method() !== 'GET') {
			return route.fallback();
		}
		return route.fulfill(json(dashboardProwlarrStatus));
	});
	await page.route('**/api/v1/runs*', (route) => {
		if (route.request().method() !== 'GET') {
			return route.fallback();
		}
		return route.fulfill(json([]));
	});
	await page.route('**/api/v1/events*', (route) => {
		if (route.request().method() !== 'GET') {
			return route.fallback();
		}
		return route.fulfill(json([]));
	});
	await page.route('**/api/v1/schedules*', (route) => {
		if (route.request().method() !== 'GET') {
			return route.fallback();
		}
		return route.fulfill(json([]));
	});
}

for (const [viewportName, viewport] of Object.entries(VIEWPORTS)) {
	for (const pattern of AUTH_ROUTES) {
		test(`authenticated: layout ${pattern} ${viewportName}`, async ({ page }, testInfo) => {
			await page.setViewportSize(viewport);
			await mockUrlRows(page, pattern);
			if (pattern === '/' && !served) {
				await mockDashboardRoute(page);
			} else if (pattern === '/instances' && !served) {
				await page.route('**/api/v1/instances', (route) => {
					if (route.request().method() !== 'GET') {
						return route.fallback();
					}
					return route.fulfill(json(hyphenatedInstancesList));
				});
			} else if (pattern === '/automation?tab=caps' && !served) {
				await mockSettingsApi(page);
			} else if (
				(pattern.startsWith('/activity') || pattern === '/instances/[id]/activity') &&
				!served
			) {
				await page.route('**/api/v1/events*', (route) =>
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(historyEvents)
					})
				);
				if (pattern === '/instances/[id]/activity') {
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
				}
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
			} else if (pattern === '/connections/prowlarr' && !served) {
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
			} else if (pattern === '/connections/seerr' && !served) {
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

			if (
				pattern === '/automation?tab=caps' &&
				(viewportName === 'desktop' || viewportName === 'phone')
			) {
				await assertGridRowAlignment(page, pattern);
			}

			if (viewportName === 'desktop' || viewportName === 'desktop1920') {
				const pending = WIDTH_PENDING[pattern];
				if (pending) {
					const today = new Date().toISOString().slice(0, 10);
					expect(
						today <= pending.expires,
						`WIDTH_PENDING entry for ${pattern} expired on ${pending.expires} (today: ${today})`
					).toBe(true);
					// A pending route must still fail the width gate; once it passes, its entry is stale.
					const passed = await assertDesktopWidth(page, pattern).then(
						() => true,
						() => false
					);
					expect(passed, `route ${pattern} now passes; remove it from WIDTH_PENDING`).toBe(false);
				} else {
					await assertDesktopWidth(page, pattern);
				}
			}

			if (viewport.width < 768 && (PHONE_FIT_ROUTES as readonly string[]).includes(pattern)) {
				await assertPhoneFit(page, pattern);
			}

			if (viewportName === 'phone' || viewportName === 'phone375') {
				await assertTouchTargetHeights(page, pattern);
			}

			if (shots) {
				const name = `${pattern.replace(/[/[\]]+/g, '-').replace(/^-|-$/g, '') || 'root'}-${viewportName}.png`;
				await page.screenshot({ path: testInfo.outputPath(name), fullPage: true });
			}
		});
	}
}

test('authenticated: layout expected tables follow role and data', () => {
	// Admin with the CI seed: one table per non-empty list.
	expect(expectedTables('/automation', 'admin', { '/api/v1/schedules': 1 })).toBe(1);
	expect(
		expectedTables('/system', 'admin', {
			'/api/v1/notifications/channels': 1,
			'/api/v1/notifications/rules': 0
		})
	).toBe(1);
	expect(expectedTables('/account', 'admin', { '/api/v1/auth/api-keys': 2 })).toBe(1);
	// Viewer: the same lists, empty data shows no table.
	expect(expectedTables('/automation', 'viewer', { '/api/v1/schedules': 0 })).toBe(0);
	expect(expectedTables('/account', 'viewer', { '/api/v1/auth/api-keys': 0 })).toBe(0);
	// A viewer owning an API key, or a list the page never received, fails.
	expect(() => expectedTables('/account', 'viewer', { '/api/v1/auth/api-keys': 1 })).toThrow(
		/ADR-0023/
	);
	expect(() => expectedTables('/system', 'viewer', {})).toThrow(/gave the page no JSON list/);
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
			for (const pattern of ['/automation', '/system', '/account']) {
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

for (const pattern of SCROLL_BUDGET_ROUTES) {
	test(`authenticated: layout scroll budget ${pattern} at 1280x800`, async ({ page }) => {
		await page.setViewportSize({ width: 1280, height: 800 });
		if (pattern === '/' && !served) {
			await mockDashboardRoute(page);
		}
		if (pattern in urlRowFixtures) {
			await mockUrlRows(page, pattern as (typeof AUTH_ROUTES)[number]);
		}
		const route = pattern.replace('[id]', instanceId());
		await page.goto(route);
		await page.waitForLoadState('networkidle');
		await assertScrollBudget(page, pattern);
	});
}

test('authenticated: layout scroll budget planted negative', async ({ page }) => {
	await page.setViewportSize({ width: 1280, height: 800 });
	await page.setContent('<div style="height: 1000px">tall content</div>');
	await expect(assertScrollBudget(page, '/activity')).rejects.toThrow(
		/\/activity: scroll height \(\d+px\) exceeds budget/
	);
});

test('authenticated: desktop width gate fixtures and planted negative', async ({ page }) => {
	// Planted negative: centered narrow column (max-w-md = 28rem = 448px in 1000px main)
	await page.setContent(
		'<main style="width:1000px;padding:0"><section style="max-width:28rem;margin-inline:auto">x</section></main>'
	);
	const measured = await page.evaluate(measureContentSpan);
	expect(measured.ratio).toBeLessThan(0.75);
	await expect(assertDesktopWidth(page, '/test-negative')).rejects.toThrow(
		`/test-negative: widest content block ${measured.widest}px is narrower than 75% of main (1000px)`
	);

	await page.setContent(
		'<main style="width:1000px;padding:0"><section style="width:1000px">x</section></main>'
	);
	await expect(assertDesktopWidth(page, '/test-positive')).resolves.not.toThrow();

	await page.setContent(
		'<main style="width:1000px;padding:0"><section style="width:750px">x</section></main>'
	);
	await expect(assertDesktopWidth(page, '/test-boundary-pass')).resolves.not.toThrow();

	await page.setContent(
		'<main style="width:1000px;padding:0"><section style="width:749px">x</section></main>'
	);
	await expect(assertDesktopWidth(page, '/test-boundary-fail')).rejects.toThrow(
		'/test-boundary-fail: widest content block 749px is narrower than 75% of main (1000px)'
	);

	// role=log fixture (/activity): wide role=log passes, narrow role=log fails
	await page.setContent(
		'<main style="width:1000px;padding:0"><div role="log" style="width:1000px">logs</div></main>'
	);
	await expect(assertDesktopWidth(page, '/test-log-pass')).resolves.not.toThrow();

	await page.setContent(
		'<main style="width:1000px;padding:0"><div role="log" style="max-width:28rem;margin-inline:auto">logs</div></main>'
	);
	const measuredLog = await page.evaluate(measureContentSpan);
	expect(measuredLog.ratio).toBeLessThan(0.75);
	await expect(assertDesktopWidth(page, '/test-log-negative')).rejects.toThrow(
		`/test-log-negative: widest content block ${measuredLog.widest}px is narrower than 75% of main (1000px)`
	);

	// role=grid fixture: wide role=grid passes, narrow role=grid fails
	await page.setContent(
		'<main style="width:1000px;padding:0"><div role="grid" style="width:1000px">grid</div></main>'
	);
	await expect(assertDesktopWidth(page, '/test-grid-pass')).resolves.not.toThrow();

	await page.setContent(
		'<main style="width:1000px;padding:0"><div role="grid" style="max-width:28rem;margin-inline:auto">grid</div></main>'
	);
	const measuredGrid = await page.evaluate(measureContentSpan);
	expect(measuredGrid.ratio).toBeLessThan(0.75);
	await expect(assertDesktopWidth(page, '/test-grid-negative')).rejects.toThrow(
		`/test-grid-negative: widest content block ${measuredGrid.widest}px is narrower than 75% of main (1000px)`
	);

	// Empty fixture: no matching block -> fails
	await page.setContent(
		'<main style="width:1000px;padding:0"><div>no matching elements</div></main>'
	);
	await expect(assertDesktopWidth(page, '/test-empty')).rejects.toThrow(
		'/test-empty: no content block measured'
	);
});

test('authenticated: phone fit gate fixtures and planted negative', async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 844 });

	await page.setContent(
		'<main style="width:390px;padding:0"><table style="width:600px"><tr><td>wide</td></tr></table></main>'
	);
	await expect(assertPhoneFit(page, '/test-negative')).rejects.toThrow(
		/\/test-negative: content wider than main at phone width \(1 block\(s\)\)/
	);

	await page.setContent(
		'<main style="width:390px;padding:0"><div style="overflow-x:auto;width:390px"><div style="width:600px">scroll</div></div></main>'
	);
	await expect(assertPhoneFit(page, '/test-negative-scroll')).rejects.toThrow(
		/\/test-negative-scroll: content wider than main at phone width \(1 block\(s\)\)/
	);

	await page.setContent(
		'<main style="width:390px;padding:0"><table style="width:100%"><tr><td>fits</td></tr></table></main>'
	);
	await expect(assertPhoneFit(page, '/test-positive')).resolves.not.toThrow();

	await page.setContent(
		'<main style="width:390px;padding:0"><section style="width:390px">x</section></main>'
	);
	await expect(assertPhoneFit(page, '/test-boundary')).resolves.not.toThrow();
});

test('authenticated: layout touch target height gate fixtures and planted negative', async ({
	page
}) => {
	// Planted negative: 32px button (.btn) must fail the check
	await page.setContent(
		'<button type="button" class="btn" style="min-height: 0; height: 32px">Action</button>'
	);
	const measured = await page.evaluate(measureTouchTargets);
	expect(measured.length).toBeGreaterThan(0);
	expect(measured[0].height).toBe(32);
	await expect(assertTouchTargetHeights(page, '/test-negative')).rejects.toThrow(
		/\/test-negative: touch target height < 44px on phone \(1 element\(s\)\)/
	);

	// Positive fixture: 44px button passes
	await page.setContent(
		'<button type="button" class="btn" style="min-height: 44px; height: 44px">Action</button>'
	);
	await expect(assertTouchTargetHeights(page, '/test-positive')).resolves.not.toThrow();

	// Boundary fixture: 43.5px fails
	await page.setContent(
		'<button type="button" class="btn" style="min-height: 0; height: 43.5px">Action</button>'
	);
	await expect(assertTouchTargetHeights(page, '/test-boundary-fail')).rejects.toThrow(
		/\/test-boundary-fail: touch target height < 44px/
	);

	// Hidden element fixture: hidden 32px button is ignored
	await page.setContent(
		'<button type="button" class="btn" style="display: none; height: 32px">Action</button>'
	);
	await expect(assertTouchTargetHeights(page, '/test-hidden')).resolves.not.toThrow();
});

test('authenticated: layout /connections/download-clients table fits at 1280', async ({ page }) => {
	await page.setViewportSize({ width: 1280, height: 800 });
	await page.goto('/connections/download-clients');
	await page.waitForLoadState('networkidle');

	const table = page.locator('table');
	await expect(table).toBeVisible();
	await expect(table.getByText(/connection refused/i).first()).toBeVisible();

	await assertTableFits(page, 'table');
});

test('authenticated: grid row alignment fixtures and planted negative', async ({ page }) => {
	await page.setContent(`
		<main style="width:600px">
			<div class="grid" style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px">
				<div style="display:flex;flex-direction:column">
					<label style="line-height:20px;height:20px">Short label</label>
					<input style="height:32px" />
				</div>
				<div style="display:flex;flex-direction:column">
					<label style="line-height:20px;height:40px">Longer label<br>that wraps</label>
					<input style="height:32px" />
				</div>
				<div style="display:flex;flex-direction:column">
					<label style="line-height:20px;height:20px">Short label</label>
					<input style="height:32px" />
				</div>
			</div>
		</main>
	`);
	await expect(assertGridRowAlignment(page, '/test-negative')).rejects.toThrow(
		/\/test-negative: inputs misaligned in grid row \(1 row\(s\)\)/
	);

	await page.setContent(`
		<main style="width:600px">
			<div class="grid items-end" style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px;align-items:end">
				<div style="display:flex;flex-direction:column">
					<label style="line-height:20px;height:20px">Short label</label>
					<input style="height:32px" />
				</div>
				<div style="display:flex;flex-direction:column">
					<label style="line-height:20px;height:40px">Longer label<br>that wraps</label>
					<input style="height:32px" />
				</div>
				<div style="display:flex;flex-direction:column">
					<label style="line-height:20px;height:20px">Short label</label>
					<input style="height:32px" />
				</div>
			</div>
		</main>
	`);
	await expect(assertGridRowAlignment(page, '/test-positive')).resolves.not.toThrow();

	await page.setContent(`
		<main style="width:600px">
			<div class="grid" style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px">
				<div style="display:flex;flex-direction:column">
					<label style="line-height:20px;height:40px">Single child</label>
					<input style="height:32px" />
				</div>
			</div>
		</main>
	`);
	await expect(assertGridRowAlignment(page, '/test-single-child')).resolves.not.toThrow();

	await page.setContent(`
		<main style="width:600px">
			<div class="grid" style="display:grid;grid-template-columns:repeat(2,1fr);gap:12px">
				<div style="display:flex;flex-direction:column">
					<label style="line-height:20px;height:20px">Label 1</label>
					<input style="height:32px" />
				</div>
				<div style="display:flex;flex-direction:column">
					<label style="line-height:21px;height:21px">Label 2</label>
					<input style="height:32px" />
				</div>
			</div>
		</main>
	`);
	await expect(assertGridRowAlignment(page, '/test-spread-1px')).resolves.not.toThrow();
});
