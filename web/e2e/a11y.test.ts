// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// In no-API mode, preview runs without an API: the single chromium project
// sweeps routes coverage, /login, and /setup.
//
// In API mode (SNATCHARR_E2E_API set), preview proxies the Go API. Projects run in dependency order:
// (public /setup + routes, auth-setup, anonymous /login, authenticated sweep, flow).
import fs from 'node:fs';
import path from 'node:path';
import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import {
	json,
	mockAuthSetupRoutes,
	mockInstanceNotFound,
	mockInstanceQueueApi,
	mockModulesApi,
	mockSettingsApi,
	mockSignedInApi,
	samplePolicyData
} from './mocks.js';
import { settleToasts } from './settle.js';

const WCAG_22_AA = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

async function expectNoAxeViolations(page: Page): Promise<void> {
	await settleToasts(page);
	const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
	expect(results.violations).toEqual([]);
}

const ALL_ROUTES = [
	'/',
	'/account',
	'/activity',
	'/adult/image-sets',
	'/adult/performers',
	'/adult/sources',
	'/adult/studios',
	'/automation',
	'/connections',
	'/connections/configarr',
	'/connections/download-clients',
	'/connections/prowlarr',
	'/connections/seerr',
	'/instances',
	'/instances/[id]',
	'/instances/[id]/activity',
	'/instances/[id]/memory',
	'/instances/[id]/policy',
	'/instances/[id]/queue',
	'/login',
	'/settings/requests',
	'/setup',
	'/system'
] as const;

const AUTH_ROUTES = ALL_ROUTES.filter((r) => r !== '/setup' && r !== '/login');

function discoverRoutes(): string[] {
	const routesDir = path.resolve(import.meta.dirname, '../src/routes');
	const entries = fs.readdirSync(routesDir, { recursive: true }) as string[];
	return entries
		.filter((f) => f.endsWith('+page.svelte'))
		.map((f) => {
			const dir = path.dirname(f);
			return dir === '.' ? '/' : `/${dir.replace(/\\/g, '/')}`;
		})
		.sort();
}

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
		throw new Error(`missing instanceId in seed file ${seedFile}`);
	}
	return data.instanceId;
}

function resolvePath(pathPattern: string): string {
	if (pathPattern.includes('[id]')) {
		return pathPattern.replace('[id]', getSeededInstanceId());
	}
	return pathPattern;
}

test('routes: every +page.svelte is in the route list', () => {
	expect(ALL_ROUTES.slice().sort()).toEqual(discoverRoutes());
});

if (!process.env.SNATCHARR_E2E_API) {
	for (const route of ['/login', '/setup']) {
		test(`${route} has no axe violations`, async ({ page }) => {
			await page.goto(route);
			await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
			await expectNoAxeViolations(page);
		});
	}

	async function setupAuthRoutes(page: Page) {
		await mockAuthSetupRoutes(page);
	}

	async function setupInstanceDetailRoutes(page: Page, id: string) {
		await page.route(`**/api/v1/instances/${id}`, (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					id,
					name: 'Library Sonarr',
					kind: 'sonarr',
					base_url: 'http://sonarr.local:8989',
					source: 'manual',
					enabled: true,
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z'
				})
			})
		);
		await page.route(`**/api/v1/instances/${id}/policy`, (route) =>
			route.fulfill(json(samplePolicyData))
		);
		await page.route(`**/api/v1/instances/${id}/policy/filter-options`, (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ tags: [], quality_profiles: [] })
			})
		);
	}

	async function setupInstanceQueueRoutes(page: Page, id: string) {
		await mockInstanceQueueApi(page, id);
	}

	async function setupInstanceActivityRoutes(page: Page, id: string) {
		await page.route('**/api/v1/runs?**', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);
		await page.route(`**/api/v1/instances/${id}/preview?**`, (route) =>
			route.fulfill({
				status: 202,
				contentType: 'application/json',
				body: JSON.stringify({
					run_id: 'preview-run-001',
					seed: 42
				})
			})
		);
		await page.route('**/api/v1/runs/preview-run-001', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					id: 'preview-run-001',
					instance_id: id,
					kind: 'missing',
					status: 'done',
					queued_at: '2026-09-01T12:00:00Z',
					started_at: '2026-09-01T12:00:01Z',
					finished_at: '2026-09-01T12:00:05Z',
					searched_count: 0,
					dry_run: true,
					seed: 42,
					previewed_targets: [
						{ entity_type: 'movie', id: 101, title: 'Inception', cost: 1 },
						{ entity_type: 'series', id: 202, title: 'Breaking Bad', cost: 3 }
					]
				})
			})
		);
	}

	async function setupInstanceMocks(page: Page, id: string) {
		await setupAuthRoutes(page);
		await setupInstanceDetailRoutes(page, id);
		await setupInstanceQueueRoutes(page, id);
		await setupInstanceActivityRoutes(page, id);
	}

	async function setupLiveActivityRoutes(
		page: Page,
		options: {
			instances?: unknown[];
			systemStatus?: Record<string, unknown>;
			events?: unknown[];
		} = {}
	): Promise<void> {
		await setupAuthRoutes(page);
		await page.route('**/api/v1/instances', (route) =>
			route.fulfill(json(options.instances ?? []))
		);
		await page.route('**/api/v1/schedules', (route) => route.fulfill(json([])));
		await page.route('**/api/v1/system/status', (route) =>
			route.fulfill(
				json(
					options.systemStatus ?? {
						name: 'SnatchArr',
						version: '0.1.0',
						commit: 'abc1234',
						build_date: '2026-10-20',
						profile: 'dev',
						setup_complete: true
					}
				)
			)
		);
		await page.route('**/api/v1/events?*', (route) => route.fulfill(json(options.events ?? [])));
		await page.route('**/api/v1/events/stream', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'text/event-stream',
				headers: {
					'Cache-Control': 'no-cache',
					Connection: 'keep-alive'
				},
				body: ': keepalive\n\n'
			})
		);
	}

	const instanceId = '00000000-0000-0000-0000-000000000001';

	test('/instances/[id] (overview) has no axe violations', async ({ page }) => {
		await setupInstanceMocks(page, instanceId);
		await page.goto(`/instances/${instanceId}`);
		await expect(page.getByRole('heading', { level: 1, name: 'Library Sonarr' })).toBeVisible();
		await expect(
			page.getByRole('heading', { level: 2, name: /Edit instance|Instanz bearbeiten/i })
		).toBeVisible();

		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/instances/[id]/policy has no axe violations', async ({ page }) => {
		await setupInstanceMocks(page, instanceId);
		await page.goto(`/instances/${instanceId}/policy`);
		await expect(page.getByRole('heading', { level: 1, name: 'Library Sonarr' })).toBeVisible();
		await expect(
			page.getByRole('heading', { level: 2, name: /Snatch policy|Snatch-Regeln/i })
		).toBeVisible();

		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/system has no axe violations', async ({ page }) => {
		await mockSettingsApi(page);
		await page.goto('/system');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		await expect(page.locator('[data-field="history_retention_days"]')).toBeVisible();

		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/automation (schedules) has no axe violations', async ({ page }) => {
		await mockSignedInApi(page);
		await page.route('**/api/v1/schedules*', (route) =>
			route.fulfill(
				json([
					{
						id: '00000000-0000-0000-0000-000000000020',
						name: 'Night Quiet Hours',
						days: [1, 2, 3, 4, 5],
						start: '23:00',
						end: '07:00',
						tz: 'UTC',
						action: 'pause',
						enabled: true,
						active: false,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					}
				])
			)
		);
		await page.goto('/automation');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		await expect(page.getByRole('table')).toBeVisible();

		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/connections/download-clients has no axe violations', async ({ page }) => {
		await mockSignedInApi(page);
		await page.route('**/api/v1/download-clients/status*', (route) =>
			route.fulfill(
				json([
					{
						client_id: '00000000-0000-0000-0000-000000000040',
						name: 'qBittorrent Primary',
						kind: 'qbittorrent',
						reachable: true,
						paused: false,
						checked_at: '2026-09-01T12:00:00Z',
						active: 2,
						queued: 1,
						download_rate_bps: 204800,
						upload_rate_bps: 102400
					}
				])
			)
		);
		await page.route('**/api/v1/download-clients*', (route) =>
			route.fulfill(
				json([
					{
						id: '00000000-0000-0000-0000-000000000040',
						name: 'qBittorrent Primary',
						kind: 'qbittorrent',
						protocol: 'torrent',
						base_url: 'http://qbittorrent.local:8080',
						username: 'admin',
						enabled: true,
						source: 'manual',
						max_active: 5,
						max_queued: 0,
						bandwidth_budget_bps: 0,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					}
				])
			)
		);
		await page.goto('/connections/download-clients');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		await expect(page.getByRole('table')).toBeVisible();

		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/system?tab=environment has no axe violations', async ({ page }) => {
		await mockSettingsApi(page);
		await page.goto('/system?tab=environment');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		await expect(
			page.getByRole('heading', { level: 2, name: 'Environment settings' })
		).toBeVisible();

		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/automation?tab=caps has no axe violations', async ({ page }) => {
		await mockSettingsApi(page);
		await page.goto('/automation?tab=caps');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		await expect(page.locator('[data-field="global_hourly_cap"]')).toBeVisible();

		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/automation?tab=season-gate has no axe violations', async ({ page }) => {
		await mockSignedInApi(page);
		await mockModulesApi(page, [{ id: 'watchaware', enabled: true }]);
		await page.route('**/api/v1/instances*', (route) =>
			route.fulfill(
				json([
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
			)
		);
		await page.route('**/api/v1/watchaware/gate/series*', (route) =>
			route.fulfill(
				json([
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
			)
		);
		await page.route('**/api/v1/watchaware/gate/decisions*', (route) =>
			route.fulfill(
				json([
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
			)
		);
		await page.goto('/automation?tab=season-gate');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		await expect(page.getByRole('table').first()).toBeVisible();

		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/instances/[id]/queue has no axe violations', async ({ page }) => {
		await setupInstanceMocks(page, instanceId);
		await page.goto(`/instances/${instanceId}/queue`);
		await expect(page.getByRole('heading', { level: 1, name: 'Library Sonarr' })).toBeVisible();
		await expect(
			page.getByRole('heading', { level: 2, name: /Queue management|Warteschlangenverwaltung/i })
		).toBeVisible();

		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/instances/[id]/activity with preview result has no axe violations', async ({ page }) => {
		await setupInstanceMocks(page, instanceId);
		await page.goto(`/instances/${instanceId}/activity`);
		await expect(page.getByRole('heading', { level: 1, name: 'Library Sonarr' })).toBeVisible();
		await expect(
			page.getByRole('heading', { level: 2, name: /Preview|Probelauf-Vorschau/i })
		).toBeVisible();

		await page.getByRole('button', { name: /preview missing|fehlende vorschauen/i }).click();

		await expect(
			page.getByRole('table', {
				name: /previewed search targets|in der vorschau gefundene suchziele/i
			})
		).toBeVisible();
		await expect(page.getByText('Inception')).toBeVisible();

		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/instances/[id]/memory has no axe violations', async ({ page }) => {
		await setupInstanceMocks(page, instanceId);
		await page.goto(`/instances/${instanceId}/memory`);
		await expect(page.getByRole('heading', { level: 1, name: 'Library Sonarr' })).toBeVisible();
		await expect(
			page.getByRole('heading', { level: 2, name: /Processed memory|Speicher/i })
		).toBeVisible();

		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/instances/[id] overview at 390 px has no axe violations', async ({ page }) => {
		await page.setViewportSize({ width: 390, height: 844 });
		await setupInstanceMocks(page, instanceId);
		await page.goto(`/instances/${instanceId}`);
		await expect(page.getByRole('heading', { level: 1, name: 'Library Sonarr' })).toBeVisible();

		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/instances/[id] not-found (404 problem+json) has no axe violations', async ({ page }) => {
		const unknownId = '00000000-0000-0000-0000-000000000404';
		await setupAuthRoutes(page);
		await mockInstanceNotFound(page, unknownId);

		await page.goto(`/instances/${unknownId}`);
		await expect(
			page.getByRole('heading', { level: 1, name: /Instance not found|Instanz nicht gefunden/i })
		).toBeVisible();

		await expectNoAxeViolations(page);
	});

	test('mobile navigation drawer open has no axe violations', async ({ page }) => {
		await page.setViewportSize({ width: 390, height: 844 });
		await setupAuthRoutes(page);
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

		await page.goto('/');
		await page.waitForLoadState('networkidle');

		const menuButton = page.getByRole('button', { name: /Open menu|Menü öffnen/i });
		await menuButton.click();

		const dialog = page.locator('dialog#mobile-nav-dialog');
		await expect(dialog).toBeVisible();

		await expectNoAxeViolations(page);
	});

	test('/activity?source=live empty state has no axe violations', async ({ page }) => {
		await setupLiveActivityRoutes(page);

		await page.goto('/activity?source=live');
		await expect(
			page.getByRole('heading', {
				level: 2,
				name: /No live events yet|Noch keine Live-Ereignisse/i
			})
		).toBeVisible();

		await expectNoAxeViolations(page);
	});

	test('/activity?source=live with backlog rows has no axe violations', async ({ page }) => {
		await setupLiveActivityRoutes(page, {
			instances: [
				{
					id: '00000000-0000-0000-0000-000000000001',
					name: 'Library Sonarr',
					kind: 'sonarr',
					base_url: 'http://sonarr.local:8989',
					source: 'manual',
					enabled: true,
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z'
				}
			],
			systemStatus: {
				name: 'SnatchArr',
				version: '0.1.0',
				commit: 'abc1234',
				build_date: '2026-10-20',
				profile: 'dev',
				setup_complete: true,
				worker_last_contact_at: '2026-10-20T12:30:00Z'
			},
			events: [
				{
					id: 101,
					instance_id: '00000000-0000-0000-0000-000000000001',
					ts: '2026-10-20T12:00:00Z',
					level: 'info',
					type: 'search_dispatched',
					title: 'Searching Sonarr',
					detail: '10 items'
				}
			]
		});

		await page.goto('/activity?source=live');
		await expect(page.getByRole('log')).toBeVisible();

		await expectNoAxeViolations(page);
	});
} else {
	test.describe('public', () => {
		test('public: /setup has no axe violations', async ({ page }) => {
			await page.goto('/setup');
			await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
			await expectNoAxeViolations(page);
		});
	});

	test.describe('anonymous', () => {
		test('anonymous: /login has no axe violations', async ({ page }) => {
			await page.goto('/login');
			await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
			await expectNoAxeViolations(page);
		});
	});

	test.describe('authenticated', () => {
		for (const routePattern of AUTH_ROUTES) {
			test(`authenticated: ${routePattern} has no axe violations`, async ({ page }) => {
				const target = resolvePath(routePattern);
				await page.goto(target);
				await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
				await expectNoAxeViolations(page);
			});
		}
	});
}
