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
import { expect, test } from '@playwright/test';

const WCAG_22_AA = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

const ALL_ROUTES = [
	'/',
	'/account',
	'/cleanuparr',
	'/configarr',
	'/download-clients',
	'/history',
	'/instances',
	'/instances/[id]',
	'/live',
	'/login',
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
