// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #903 #909

import { expect, test, type Page } from '@playwright/test';
import { targetProblems } from '../../src/lib/targets.js';
import { VIEWPORTS } from '../layout.js';

const proxySecret = process.env.SNATCHARR_E2E_PROXY_SECRET ?? '';
const proxyUserHeader = process.env.SNATCHARR_E2E_PROXY_HEADER ?? 'Remote-User';
const proxyGroupsHeader = process.env.SNATCHARR_E2E_PROXY_GROUPS_HEADER ?? 'X-authentik-groups';
const proxySecretHeader =
	process.env.SNATCHARR_E2E_PROXY_SECRET_HEADER ?? 'X-SnatchArr-Proxy-Secret';

interface Created {
	path: string;
	id: string;
}

async function loginAsAdmin(page: Page): Promise<string> {
	await page.context().clearCookies();
	const res = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(res.ok()).toBeTruthy();
	return ((await res.json()) as { csrf_token: string }).csrf_token;
}

async function loginAsViewer(page: Page, username: string): Promise<void> {
	expect(proxySecret, 'SNATCHARR_E2E_PROXY_SECRET unset: run through web-e2e-api.sh').not.toBe('');
	await page.context().clearCookies();
	const res = await page.request.post('/api/v1/auth/proxy-session', {
		headers: {
			[proxyUserHeader]: username,
			[proxyGroupsHeader]: 'e2e-viewers',
			[proxySecretHeader]: proxySecret
		}
	});
	expect(res.status()).toBe(200);
}

async function create(
	page: Page,
	csrf: string,
	created: Created[],
	path: string,
	data: object
): Promise<string> {
	const res = await page.request.post(path, { headers: { 'X-CSRF-Token': csrf }, data });
	expect(res.status(), `create ${path}`).toBe(201);
	const id = ((await res.json()) as { id: string }).id;
	created.push({ path, id });
	return id;
}

// Deletes in reverse order, so a client or schedule goes before the instance it points at.
async function cleanUp(page: Page, created: Created[]): Promise<void> {
	const csrf = await loginAsAdmin(page);
	for (const item of created.reverse()) {
		await page.request.delete(`${item.path}/${item.id}`, { headers: { 'X-CSRF-Token': csrf } });
	}
}

async function tintOf(row: ReturnType<Page['locator']>): Promise<string> {
	return row.evaluate((el) => {
		const cs = window.getComputedStyle(el);
		return `${cs.backgroundColor}|${cs.borderColor}`;
	});
}

test('903: an instance row and a phone card are one click target that tints on hover and opens the instance', async ({
	page
}) => {
	const suffix = crypto.randomUUID().slice(0, 8);
	const name = `e2e-903-inst-${suffix}`;
	const created: Created[] = [];
	const csrf = await loginAsAdmin(page);
	try {
		const id = await create(page, csrf, created, '/api/v1/instances', {
			kind: 'sonarr',
			name,
			base_url: `http://127.0.0.1:19999/${name}`,
			api_key: 'fake-api-key-0123456789',
			enabled: true
		});

		await page.setViewportSize(VIEWPORTS.desktop);
		await page.goto('/instances');
		const row = page.locator('tr.target', { hasText: name });
		await expect(row).toBeVisible();
		expect(await page.evaluate(targetProblems)).toEqual([]);
		await expect(row.locator('a.target-link')).toHaveAttribute('href', `/instances/${id}`);

		const resting = await tintOf(row);
		await row.hover();
		expect(await tintOf(row)).not.toBe(resting);
		// A click away from the name link still follows it.
		await row.click({ position: { x: 4, y: 4 } });
		await expect(page).toHaveURL(new RegExp(`/instances/${id}$`));

		await page.setViewportSize(VIEWPORTS.phone);
		await page.goto('/instances');
		const card = page.locator('li.card.target', { hasText: name });
		await expect(card).toBeVisible();
		expect(await page.evaluate(targetProblems)).toEqual([]);
		await card.click({ position: { x: 4, y: 4 } });
		await expect(page).toHaveURL(new RegExp(`/instances/${id}$`));
	} finally {
		await cleanUp(page, created);
	}
});

test('909: download client rows, schedule rows and Prowlarr cards open their edit form for admins and are no target for viewers', async ({
	page
}) => {
	const suffix = crypto.randomUUID().slice(0, 8);
	const names = {
		inst: `e2e-909-inst-${suffix}`,
		dl: `e2e-909-dl-${suffix}`,
		sched: `e2e-909-sched-${suffix}`,
		prowlarr: `e2e-909-prowlarr-${suffix}`
	};
	const created: Created[] = [];
	const csrf = await loginAsAdmin(page);
	try {
		const instanceId = await create(page, csrf, created, '/api/v1/instances', {
			kind: 'sonarr',
			name: names.inst,
			base_url: `http://127.0.0.1:19999/${names.inst}`,
			api_key: 'fake-api-key-0123456789',
			enabled: true
		});
		await create(page, csrf, created, '/api/v1/download-clients', {
			name: names.dl,
			kind: 'qbittorrent',
			base_url: `http://127.0.0.1:19999/${names.dl}`,
			enabled: true,
			instance_id: instanceId,
			max_active: 0,
			max_queued: 0,
			bandwidth_budget_bps: 0
		});
		await create(page, csrf, created, '/api/v1/schedules', {
			name: names.sched,
			instance_id: instanceId,
			days: [1, 2, 3, 4, 5],
			start: '23:00',
			end: '06:00',
			tz: 'UTC',
			action: 'pause',
			enabled: true
		});
		await create(page, csrf, created, '/api/v1/prowlarr', {
			name: names.prowlarr,
			base_url: `http://127.0.0.1:19999/${names.prowlarr}`,
			api_key: 'fake-api-key-0123456789',
			enabled: true,
			hourly_cap: 15
		});

		await expectEditOpens(
			page,
			'/connections/download-clients',
			'tr.target',
			names.dl,
			'#dlclient-name'
		);
		await expectEditOpens(page, '/automation', 'tr.target', names.sched, '#schedule-name');
		await expectEditOpens(
			page,
			'/connections/prowlarr',
			'li.card.target',
			names.prowlarr,
			'#prowlarr-name'
		);

		await loginAsViewer(page, `e2e-viewer-909-${suffix}`);
		for (const [route, name] of [
			['/connections/download-clients', names.dl],
			['/automation', names.sched],
			['/connections/prowlarr', names.prowlarr]
		]) {
			await page.goto(route);
			await expect(page.getByText(name).first()).toBeVisible();
			await expect(page.locator('.target')).toHaveCount(0);
		}
	} finally {
		await cleanUp(page, created);
	}
});

async function expectEditOpens(
	page: Page,
	route: string,
	selector: string,
	name: string,
	nameField: string
): Promise<void> {
	await page.goto(route);
	const target = page.locator(selector, { hasText: name });
	await expect(target).toBeVisible();
	expect(await page.evaluate(targetProblems)).toEqual([]);
	await target.click({ position: { x: 4, y: 4 } });
	await expect(page.locator(nameField)).toHaveValue(name);
}
