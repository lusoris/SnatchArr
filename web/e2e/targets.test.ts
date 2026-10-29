// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test, type Page } from '@playwright/test';
import { json, mockSignedInApi } from './mocks.js';

const instance1 = {
	id: '00000000-0000-0000-0000-000000000001',
	name: 'Library Sonarr',
	kind: 'sonarr',
	source: 'manual',
	enabled: true,
	base_url: 'http://sonarr.local:8989',
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

const instance2 = {
	id: '00000000-0000-0000-0000-000000000002',
	name: 'Library Radarr',
	kind: 'radarr',
	source: 'manual',
	enabled: true,
	base_url: 'http://radarr.local:7878',
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

const testInstances = [instance1, instance2];

async function mockTargetsApi(page: Page): Promise<void> {
	await mockSignedInApi(page);
	await page.route('**/api/v1/instances*', (route) => {
		const url = route.request().url();
		if (url.includes(instance1.id)) {
			return route.fulfill(json(instance1));
		}
		if (url.includes(instance2.id)) {
			return route.fulfill(json(instance2));
		}
		return route.fulfill(json(testInstances));
	});
}

/** In-browser evaluator validating target contracts on the live DOM tree. */
function evalTargetProblems(root: Element | Document = document): string[] {
	function isValidTargetLink(el: Element): boolean {
		if (el.tagName === 'A') {
			return el.hasAttribute('href');
		}
		if (
			el.tagName === 'BUTTON' &&
			el.getAttribute('type') === 'button' &&
			!el.hasAttribute('disabled') &&
			!(el as HTMLButtonElement).disabled
		) {
			return true;
		}
		return false;
	}

	const problems: string[] = [];
	const targets: Element[] = [];

	if (root instanceof Element && root.matches('.target')) {
		targets.push(root);
	}
	targets.push(...Array.from(root.querySelectorAll('.target')));

	for (const target of targets) {
		const nested = target.querySelectorAll('.target');
		if (nested.length > 0) {
			problems.push(
				`Nested .target elements are prohibited: found ${nested.length} nested target(s)`
			);
		}

		const allTargetLinks = Array.from(target.querySelectorAll('.target-link')).filter(
			(el) => el.closest('.target') === target
		);

		const validLinks = allTargetLinks.filter(isValidTargetLink);

		if (validLinks.length === 0) {
			problems.push(
				'Target element must contain exactly one a.target-link[href] or button.target-link[type=button]:not([disabled]), found 0'
			);
		} else if (validLinks.length > 1) {
			problems.push(
				`Target element must contain exactly one a.target-link[href] or button.target-link[type=button]:not([disabled]), found ${validLinks.length}`
			);
		}
	}

	return problems;
}

async function verifyTargetHoverAndFocus(page: Page, index: number): Promise<void> {
	const target = page.locator('.target:visible').nth(index);
	const initial = await target.evaluate((el) => {
		const cs = window.getComputedStyle(el);
		return { bg: cs.backgroundColor, border: cs.borderColor };
	});

	await target.hover();
	const hovered = await target.evaluate((el) => {
		const cs = window.getComputedStyle(el);
		return { bg: cs.backgroundColor, border: cs.borderColor };
	});

	const changed = initial.bg !== hovered.bg || initial.border !== hovered.border;
	expect(changed, `target index ${index} hover style must differ`).toBe(true);

	const link = target.locator('a.target-link');
	await link.focus();
	await page.keyboard.press('Shift+Tab');
	await page.keyboard.press('Tab');

	const outline = await link.evaluate((el) => window.getComputedStyle(el).outlineStyle);
	expect(outline, `target index ${index} link must display its own focus outline`).not.toBe('none');
}

async function verifyTargetClickNavigation(page: Page, index: number): Promise<void> {
	const target = page.locator('.target:visible').nth(index);
	const link = target.locator('a.target-link');
	const href = await link.getAttribute('href');
	expect(href, `target index ${index} link href must be present`).toBeTruthy();

	await target.click();
	await page.waitForURL((url) => url.pathname.includes(href!));
	await page.goBack();
	await page.waitForLoadState('networkidle');
	await expect(page.locator('.target:visible').first()).toBeVisible();
}

for (const path of ['/', '/instances']) {
	test(`authenticated: interactive targets on ${path}`, async ({ page }) => {
		await mockTargetsApi(page);
		await page.goto(path);
		await page.waitForLoadState('networkidle');

		const problems = await page.evaluate(evalTargetProblems);
		expect(problems).toEqual([]);

		const totalTargets = await page.locator('.target:visible').count();
		const cap = Math.min(totalTargets, 5);
		expect(cap, `page ${path} must have at least one visible .target`).toBeGreaterThan(0);

		for (let i = 0; i < cap; i++) {
			await verifyTargetHoverAndFocus(page, i);
		}

		for (let i = 0; i < cap; i++) {
			await verifyTargetClickNavigation(page, i);
		}
	});
}

test('authenticated: planted negative target without target-link reports problem', async ({
	page
}) => {
	await mockTargetsApi(page);
	await page.goto('/instances');
	await page.waitForLoadState('networkidle');

	await page.evaluate(() => {
		const orphan = document.createElement('div');
		orphan.className = 'card target';
		orphan.textContent = 'Orphan target without link';
		document.body.appendChild(orphan);
	});

	const problems = await page.evaluate(evalTargetProblems);
	expect(problems.length).toBe(1);
	expect(problems[0]).toContain('found 0');
});

const dlClient = {
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
};

const dlStatus = {
	client_id: dlClient.id,
	name: 'qBittorrent Primary',
	kind: 'qbittorrent',
	reachable: true,
	paused: false,
	checked_at: '2026-09-01T12:00:00Z',
	active: 2,
	queued: 1,
	download_rate_bps: 204800,
	upload_rate_bps: 102400
};

const sampleSchedule = {
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
};

test('authenticated: admin on /connections/download-clients opens edit form on row click and has no target problems', async ({
	page
}) => {
	await mockSignedInApi(page, 'admin');
	await page.route('**/api/v1/download-clients/status*', (route) =>
		route.fulfill(json([dlStatus]))
	);
	await page.route('**/api/v1/download-clients*', (route) => route.fulfill(json([dlClient])));
	await page.route('**/api/v1/instances*', (route) => route.fulfill(json([])));

	await page.goto('/connections/download-clients');
	await page.waitForLoadState('networkidle');

	const problems = await page.evaluate(evalTargetProblems);
	expect(problems).toEqual([]);

	const row = page.locator('tr.target').first();
	await expect(row).toBeVisible();

	await row.click({ position: { x: 10, y: 10 } });
	await expect(page.locator('#dlclient-name')).toBeFocused();
});

test('authenticated: viewer on /connections/download-clients has zero .target elements', async ({
	page
}) => {
	await mockSignedInApi(page, 'viewer');
	await page.route('**/api/v1/download-clients/status*', (route) =>
		route.fulfill(json([dlStatus]))
	);
	await page.route('**/api/v1/download-clients*', (route) => route.fulfill(json([dlClient])));
	await page.route('**/api/v1/instances*', (route) => route.fulfill(json([])));

	await page.goto('/connections/download-clients');
	await page.waitForLoadState('networkidle');

	await expect(page.locator('.target')).toHaveCount(0);
});

test('authenticated: admin on /automation opens edit form on row click and has no target problems', async ({
	page
}) => {
	await mockSignedInApi(page, 'admin');
	await page.route('**/api/v1/schedules*', (route) => route.fulfill(json([sampleSchedule])));
	await page.route('**/api/v1/instances*', (route) => route.fulfill(json([])));

	await page.goto('/automation');
	await page.waitForLoadState('networkidle');

	const problems = await page.evaluate(evalTargetProblems);
	expect(problems).toEqual([]);

	const row = page.locator('tr.target').first();
	await expect(row).toBeVisible();

	await row.click({ position: { x: 10, y: 10 } });
	await expect(
		page.getByRole('heading', { level: 2, name: /Edit schedule|Zeitplan bearbeiten/i })
	).toBeVisible();
});

test('authenticated: viewer on /automation has zero .target elements', async ({ page }) => {
	await mockSignedInApi(page, 'viewer');
	await page.route('**/api/v1/schedules*', (route) => route.fulfill(json([sampleSchedule])));
	await page.route('**/api/v1/instances*', (route) => route.fulfill(json([])));

	await page.goto('/automation');
	await page.waitForLoadState('networkidle');

	await expect(page.locator('.target')).toHaveCount(0);
});

test('authenticated: a configarr row is no target and does not cover the editable row above it', async ({
	page
}) => {
	const imported = {
		...dlClient,
		id: '00000000-0000-0000-0000-000000000041',
		name: 'Imported Client',
		source: 'configarr'
	};
	await mockSignedInApi(page, 'admin');
	await page.route('**/api/v1/download-clients/status*', (route) => route.fulfill(json([])));
	await page.route('**/api/v1/download-clients*', (route) =>
		route.fulfill(json([dlClient, imported]))
	);
	await page.route('**/api/v1/instances*', (route) => route.fulfill(json([])));

	await page.goto('/connections/download-clients');
	await expect(page.locator('tbody tr')).toHaveCount(2);

	await expect(page.locator('tbody tr').nth(1)).not.toHaveClass(/\btarget\b/);
	expect(await page.evaluate(evalTargetProblems)).toEqual([]);

	await page
		.locator('tbody tr')
		.nth(0)
		.click({ position: { x: 10, y: 10 } });
	await expect(page.locator('#dlclient-name')).toBeFocused();
});
