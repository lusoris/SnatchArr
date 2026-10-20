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

const WCAG_22_AA = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

const ALL_ROUTES = [
	'/',
	'/account',
	'/configarr',
	'/download-clients',
	'/history',
	'/instances',
	'/instances/[id]',
	'/instances/[id]/activity',
	'/instances/[id]/memory',
	'/instances/[id]/policy',
	'/instances/[id]/queue',
	'/live',
	'/login',
	'/logs',
	'/prowlarr',
	'/schedules',
	'/seerr',
	'/settings',
	'/setup'
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
			const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
			expect(results.violations).toEqual([]);
		});
	}

	async function setupAuthRoutes(page: Page) {
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
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					missing_per_cycle: 10,
					upgrade_per_cycle: 5,
					cycle_interval_s: 300,
					hourly_cap: 50,
					selection: 'recent',
					monitored_only: true,
					skip_future_releases: true,
					radarr_release_type: 'physical',
					sonarr_missing_mode: 'episodes',
					sonarr_upgrade_mode: 'episodes',
					lidarr_missing_mode: 'album',
					processed_ttl_h: 24,
					max_queue_size: 100,
					await_command: false,
					page_size: 50,
					recent_grab_window_h: 48,
					afterglow_max_h: 168,
					dry_run: false
				})
			})
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
		await page.route(`**/api/v1/instances/${id}/queue-policy`, (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					enabled: true,
					rules: {
						stalled: { mode: 'observe', threshold: 3 },
						slow: { mode: 'observe', threshold: 5 },
						failed: { mode: 'act', threshold: 1 },
						import_blocked: { mode: 'act', threshold: 2 },
						id_matched: { mode: 'observe', threshold: 1 },
						unwanted_score: { mode: 'observe', threshold: 3 },
						unwanted_unmonitored: { mode: 'observe', threshold: 3 }
					},
					grace_seconds: 3600,
					min_bytes_per_second: 1024,
					min_custom_format_score: null,
					max_removals_per_sweep: 5,
					max_imports_per_sweep: 10
				})
			})
		);
		await page.route(`**/api/v1/instances/${id}/queue-strikes`, (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);
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

	const instanceId = '00000000-0000-0000-0000-000000000001';

	test('/instances/[id] (overview) has no axe violations', async ({ page }) => {
		await setupInstanceMocks(page, instanceId);
		await page.goto(`/instances/${instanceId}`);
		await expect(page.getByRole('heading', { level: 1, name: 'Library Sonarr' })).toBeVisible();
		await expect(
			page.getByRole('heading', { level: 2, name: /Edit instance|Instanz bearbeiten/i })
		).toBeVisible();

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

		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/instances/[id] overview at 390 px has no axe violations', async ({ page }) => {
		await page.setViewportSize({ width: 390, height: 844 });
		await setupInstanceMocks(page, instanceId);
		await page.goto(`/instances/${instanceId}`);
		await expect(page.getByRole('heading', { level: 1, name: 'Library Sonarr' })).toBeVisible();

		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/instances/[id] not-found (404 problem+json) has no axe violations', async ({ page }) => {
		const unknownId = '00000000-0000-0000-0000-000000000404';
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
		await page.route(`**/api/v1/instances/${unknownId}`, (route) =>
			route.fulfill({
				status: 404,
				contentType: 'application/problem+json',
				body: JSON.stringify({
					type: 'about:blank',
					title: 'Not Found',
					status: 404,
					detail: 'Instance not found'
				})
			})
		);

		await page.goto(`/instances/${unknownId}`);
		await expect(
			page.getByRole('heading', { level: 1, name: /Instance not found|Instanz nicht gefunden/i })
		).toBeVisible();

		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('mobile navigation drawer open has no axe violations', async ({ page }) => {
		await page.setViewportSize({ width: 390, height: 844 });
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

		await page.goto('/');
		await page.waitForLoadState('networkidle');

		const menuButton = page.getByRole('button', { name: /Open menu|Menü öffnen/i });
		await menuButton.click();

		const dialog = page.locator('dialog#mobile-nav-dialog');
		await expect(dialog).toBeVisible();

		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/live empty state has no axe violations', async ({ page }) => {
		await setupAuthRoutes(page);
		await page.route('**/api/v1/instances', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);
		await page.route('**/api/v1/schedules', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);
		await page.route('**/api/v1/system/status', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					name: 'SnatchArr',
					version: '0.1.0',
					commit: 'abc1234',
					build_date: '2026-10-20',
					profile: 'dev',
					setup_complete: true
				})
			})
		);
		await page.route('**/api/v1/events?*', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);
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

		await page.goto('/live');
		await expect(
			page.getByRole('heading', {
				level: 2,
				name: /No live events yet|Noch keine Live-Ereignisse/i
			})
		).toBeVisible();

		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});

	test('/live with backlog rows has no axe violations', async ({ page }) => {
		await setupAuthRoutes(page);
		await page.route('**/api/v1/instances', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([
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
				])
			})
		);
		await page.route('**/api/v1/schedules', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);
		await page.route('**/api/v1/system/status', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					name: 'SnatchArr',
					version: '0.1.0',
					commit: 'abc1234',
					build_date: '2026-10-20',
					profile: 'dev',
					setup_complete: true,
					worker_last_contact_at: '2026-10-20T12:30:00Z'
				})
			})
		);
		await page.route('**/api/v1/events?*', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([
					{
						id: 101,
						instance_id: '00000000-0000-0000-0000-000000000001',
						ts: '2026-10-20T12:00:00Z',
						level: 'info',
						type: 'search_dispatched',
						title: 'Searching Sonarr',
						detail: '10 items'
					}
				])
			})
		);
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

		await page.goto('/live');
		await expect(page.getByRole('log')).toBeVisible();

		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});
} else {
	test.describe('public', () => {
		test('public: /setup has no axe violations', async ({ page }) => {
			await page.goto('/setup');
			await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
			const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
			expect(results.violations).toEqual([]);
		});
	});

	test.describe('anonymous', () => {
		test('anonymous: /login has no axe violations', async ({ page }) => {
			await page.goto('/login');
			await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
			const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
			expect(results.violations).toEqual([]);
		});
	});

	test.describe('authenticated', () => {
		for (const routePattern of AUTH_ROUTES) {
			test(`authenticated: ${routePattern} has no axe violations`, async ({ page }) => {
				const target = resolvePath(routePattern);
				await page.goto(target);
				await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
				const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
				expect(results.violations).toEqual([]);
			});
		}
	});
}
