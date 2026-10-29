// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import fs from 'node:fs/promises';
import path from 'node:path';
import { expect, test, type Page, type TestInfo } from '@playwright/test';
import { AUTH_ROUTES, assertTouchTargetHeights } from './layout.js';
import {
	json,
	mockAboutApi,
	mockInstancePolicyApi,
	mockSettingsApi,
	problemJson
} from './mocks.js';
import { settleToasts } from './settle.js';
import {
	SCREEN_CLASSES,
	applyTextSpacing,
	assertFocusVisible,
	assertLandmarksAndNames,
	assertNoHorizontalScroll,
	assertReflow,
	assertScalesWithViewport,
	assertTextNotClipped,
	axeScan
} from './screen-range.js';

const apiMode = !!process.env.SNATCHARR_E2E_API;
const mockInstanceId = '00000000-0000-0000-0000-000000000001';
type AuthRoute = (typeof AUTH_ROUTES)[number];

// All AUTH_ROUTES are served by the successful fixtures below. Future exceptions
// must be explicit here, with a reason; the coverage test rejects missing entries.
const MOCK_UNSERVED_ROUTES: readonly { route: AuthRoute; reason: string }[] = [];
const MOCK_ROUTES = AUTH_ROUTES.filter(
	(route) => !MOCK_UNSERVED_ROUTES.some((entry) => entry.route === route)
);
const MATRIX_ROUTES = [
	'/',
	'/instances',
	'/connections/download-clients',
	'/system',
	'/account'
] as const;
const unmocked = new WeakMap<Page, string[]>();

// CSP itself is covered by security.test.ts. Spacing overrides and planted styles
// must actually apply in these contexts (the same harness choice as visual tests).
test.use({ bypassCSP: true, launchOptions: { args: ['--disable-gpu'] } });

async function instanceId(): Promise<string> {
	if (!apiMode) return mockInstanceId;
	const seedPath = path.resolve(import.meta.dirname, '.auth/seed.json');
	const seed = JSON.parse(await fs.readFile(seedPath, 'utf8')) as { instanceId?: string };
	if (!seed.instanceId) throw new Error(`${seedPath}: missing seeded instanceId`);
	return seed.instanceId;
}

async function mockInstanceRoutes(page: Page): Promise<void> {
	await mockInstancePolicyApi(page, mockInstanceId);
	await page.route(`**/api/v1/instances/${mockInstanceId}/priority`, (route) =>
		route.fulfill(
			json({
				search_priority: null,
				download_priority: null,
				effective_search_priority: 10,
				effective_download_priority: 10,
				search_source: 'preset',
				download_source: 'preset'
			})
		)
	);
	await page.route(`**/api/v1/instances/${mockInstanceId}/queue-policy`, (route) =>
		route.fulfill(
			json({
				enabled: true,
				rules: Object.fromEntries(
					[
						'stalled',
						'metadata_stuck',
						'slow',
						'failed',
						'id_matched',
						'import_blocked',
						'import_fixable',
						'import_broken',
						'import_not_wanted',
						'unwanted_score',
						'not_an_upgrade',
						'duplicate',
						'unwanted_unmonitored'
					].map((key) => [key, { mode: 'observe', threshold: 3, blocklist: false }])
				),
				grace_seconds: 3600,
				min_bytes_per_second: 1024,
				max_removals_per_sweep: 5,
				max_imports_per_sweep: 10
			})
		)
	);
	await page.route(`**/api/v1/instances/${mockInstanceId}/queue-strikes`, (route) =>
		route.fulfill(json([]))
	);
}

async function mockScreenRangeApi(page: Page): Promise<void> {
	const missing: string[] = [];
	unmocked.set(page, missing);
	await page.route('**/api/v1/**', (route) => {
		missing.push(route.request().url());
		return route.fulfill(problemJson(501, 'Missing screen-range fixture'));
	});
	await mockSettingsApi(page);
	await mockAboutApi(page);
	await mockInstanceRoutes(page);
	await page.route('**/api/v1/me/preferences', (route) => route.fulfill(json({})));
	await page.route('**/api/v1/auth/api-keys', (route) => route.fulfill(json([])));
	await page.route('**/api/v1/seerr/requests*', (route) => route.fulfill(json([])));
	await page.route('**/api/v1/logs?*', (route) => route.fulfill(json({ records: [] })));
	for (const stream of ['events', 'logs']) {
		await page.route(`**/api/v1/${stream}/stream*`, (route) =>
			route.fulfill({
				status: 200,
				contentType: 'text/event-stream',
				body: ': keepalive\n\n'
			})
		);
	}
}

test.beforeEach(async ({ page }) => {
	if (!apiMode) await mockScreenRangeApi(page);
});

test.afterEach(async ({ page }) => {
	expect(unmocked.get(page) ?? [], 'every requested API endpoint needs a fixture').toEqual([]);
});

async function visit(page: Page, pattern: AuthRoute): Promise<void> {
	const route = pattern.includes('[id]') ? pattern.replace('[id]', await instanceId()) : pattern;
	await page.goto(route);
	await expect(page).toHaveURL(new RegExp(`${route.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`));
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.locator('[aria-busy="true"], [data-testid="skeleton"]')).toHaveCount(0);
	await page.waitForLoadState('networkidle');
	await settleToasts(page);
	await expect(page.locator('[data-testid="not-found-view"], [role="alert"]')).toHaveCount(0);
}

async function screenshot(
	page: Page,
	info: TestInfo,
	screen: string,
	route: string
): Promise<void> {
	const slug = route === '/' ? 'overview' : route.slice(1).replace(/[^a-z0-9]+/gi, '-');
	const directory = path.resolve(import.meta.dirname, '../test-results/screen-range', screen);
	await fs.mkdir(directory, { recursive: true });
	const file = path.join(directory, `${slug}.png`);
	await page.screenshot({ path: file, fullPage: false, animations: 'disabled' });
	await info.attach(`${screen}/${slug}`, { path: file, contentType: 'image/png' });
	const span = await assertScalesWithViewport(page);
	const measurement = path.join(directory, `${slug}.json`);
	await fs.writeFile(measurement, JSON.stringify(span, null, 2));
	await info.attach('content-span', { path: measurement, contentType: 'application/json' });
}

test('authenticated: screen-range route coverage', () => {
	const excluded = MOCK_UNSERVED_ROUTES.map(({ route, reason }) => {
		expect(reason.trim(), `${route}: missing exclusion reason`).not.toBe('');
		return route;
	});
	expect([...MOCK_ROUTES, ...excluded].sort()).toEqual([...AUTH_ROUTES].sort());
	expect(MATRIX_ROUTES.every((route) => MOCK_ROUTES.includes(route))).toBe(true);
});

for (const screen of SCREEN_CLASSES) {
	test.describe(`screen-range ${screen.name}`, () => {
		test.use({
			viewport: { width: screen.width, height: screen.height },
			deviceScaleFactor: screen.dpr
		});
		for (const route of MATRIX_ROUTES) {
			test(`authenticated: layout ${route}`, async ({ page }, info) => {
				await visit(page, route);
				await assertNoHorizontalScroll(page);
				if (screen.width <= 640) await assertReflow(page);
				if (screen.width <= 414) await assertTouchTargetHeights(page, route);
				await screenshot(page, info, screen.name, route);
			});
		}
	});
}

// A 1280x800 viewport at 200% and 400% gives these CSS viewport dimensions.
// DPR preserves the device-pixel footprint; it does not itself emulate browser zoom.
const ACCESSIBILITY_MODES = [
	{ name: 'reflow-portrait', width: 320, height: 640, dpr: 2 },
	{ name: 'zoom-200', width: 640, height: 400, dpr: 2 },
	{ name: 'zoom-400', width: 320, height: 200, dpr: 4 },
	{ name: 'phone-landscape', width: 800, height: 320, dpr: 2 },
	{ name: 'desktop', width: 1280, height: 800, dpr: 1 }
] as const;

for (const mode of ACCESSIBILITY_MODES) {
	test.describe(`screen-range ${mode.name}`, () => {
		test.use({ viewport: { width: mode.width, height: mode.height }, deviceScaleFactor: mode.dpr });
		for (const route of apiMode ? AUTH_ROUTES : MOCK_ROUTES) {
			test(`authenticated: accessibility ${route}`, async ({ page }, info) => {
				// The live log tail appends records the other workers cause, so the text never
				// stands still to be measured. Hold the stream; the list keeps its loaded records.
				if (route.includes('source=log')) await page.route('**/api/v1/logs/stream*', () => {});
				await visit(page, route);
				await assertReflow(page);
				const aria = await assertLandmarksAndNames(page);
				await info.attach('landmarks-and-names', { body: aria, contentType: 'text/plain' });
				await axeScan(page);
				await applyTextSpacing(page);
				await assertReflow(page);
				await assertTextNotClipped(page);
				await assertFocusVisible(page);
			});
		}
	});
}

test.describe('screen-range planted negatives', () => {
	test.use({ viewport: { width: 320, height: 640 }, deviceScaleFactor: 2 });
	test('authenticated: planted negative reflow', async ({ page }) => {
		await visit(page, '/');
		await assertReflow(page);
		await page.addStyleTag({ content: 'main{min-width:1200px}' });
		await expect(assertReflow(page)).rejects.toThrow(/horizontal page scroll|main exceeds reflow/);
	});
	test('authenticated: planted negative accessible name', async ({ page }) => {
		await visit(page, '/');
		await assertLandmarksAndNames(page);
		await page.locator('main').evaluate((main) => {
			const button = document.createElement('button');
			button.type = 'button';
			const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
			icon.setAttribute('aria-hidden', 'true');
			button.append(icon);
			main.append(button);
		});
		await expect(assertLandmarksAndNames(page)).rejects.toThrow(/missing accessible name/);
	});
	test('authenticated: planted negative focus under overlay', async ({ page }) => {
		await visit(page, '/');
		await assertFocusVisible(page, 3);
		await page.locator('body').evaluate((body) => {
			const cover = document.createElement('div');
			cover.style.cssText = 'position:fixed;inset:0;z-index:9999;background:white';
			body.append(cover);
		});
		await expect(assertFocusVisible(page, 3)).rejects.toThrow(/focus hidden under an overlay/);
	});
	test('authenticated: focus past the viewport edge fails beyond one pixel', async ({ page }) => {
		await visit(page, '/');
		// A fixed control first in the tab order; focus scrolling cannot move it into view.
		const plant = async (bottom: string) =>
			page.locator('body').evaluate((body, offset) => {
				document.getElementById('planted-edge')?.remove();
				const button = document.createElement('button');
				button.id = 'planted-edge';
				button.type = 'button';
				button.textContent = 'Planted edge';
				button.style.cssText = `position:fixed;left:8px;bottom:${offset};height:40px;z-index:9999`;
				body.prepend(button);
			}, bottom);
		await plant('-0.5px');
		await assertFocusVisible(page, 1);
		await plant('-4px');
		await expect(assertFocusVisible(page, 1)).rejects.toThrow(/focus outside viewport/);
	});
	test('authenticated: planted negative text overlap', async ({ page }) => {
		await visit(page, '/');
		await assertTextNotClipped(page);
		await page.locator('main').evaluate((main) => {
			const box = document.createElement('div');
			box.style.cssText = 'position:relative;height:40px';
			for (const name of ['Planted one', 'Planted two']) {
				const button = document.createElement('button');
				button.type = 'button';
				button.textContent = name;
				button.style.cssText = 'position:absolute;left:0;top:0';
				box.append(button);
			}
			main.prepend(box);
		});
		await expect(assertTextNotClipped(page)).rejects.toThrow(/text overlaps/);
	});
	// Boundary: a button scrolled out of its scroll box shares no visible area with the
	// heading its box rect would cover, so it is no collision.
	test('authenticated: scrolled-out text is no overlap', async ({ page }) => {
		await visit(page, '/');
		await page.locator('main').evaluate((main) => {
			const heading = document.createElement('h2');
			heading.textContent = 'Planted heading';
			const box = document.createElement('div');
			box.style.cssText = 'height:40px;overflow-y:auto';
			const button = document.createElement('button');
			button.type = 'button';
			button.textContent = 'Scrolled away';
			const pad = document.createElement('div');
			pad.style.height = '400px';
			box.append(button, pad);
			main.prepend(heading, box);
			// Scroll the button exactly onto the heading's place above the box.
			box.scrollTop = Math.ceil(
				box.getBoundingClientRect().top - heading.getBoundingClientRect().top
			);
		});
		await assertTextNotClipped(page);
	});
});
