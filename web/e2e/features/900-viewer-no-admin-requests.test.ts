// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #900 #911

import { expect, test, type Page } from '@playwright/test';

const proxySecret = process.env.SNATCHARR_E2E_PROXY_SECRET ?? '';
const proxyUserHeader = process.env.SNATCHARR_E2E_PROXY_HEADER ?? 'Remote-User';
const proxyGroupsHeader = process.env.SNATCHARR_E2E_PROXY_GROUPS_HEADER ?? 'X-authentik-groups';
const proxySecretHeader =
	process.env.SNATCHARR_E2E_PROXY_SECRET_HEADER ?? 'X-SnatchArr-Proxy-Secret';

// The feature run sets these; a missing one is a broken harness, not a reason to guess.
function requireEnv(name: string): string {
	const value = process.env[name];
	if (!value) {
		throw new Error(`${name} unset: run through tools/e2e/web-e2e-api.sh`);
	}
	return value;
}

async function loginAsAdmin(page: Page): Promise<string> {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok()).toBeTruthy();
	return ((await login.json()) as { csrf_token: string }).csrf_token;
}

async function loginAsViewer(page: Page, username: string): Promise<void> {
	if (!proxySecret) {
		throw new Error('SNATCHARR_E2E_PROXY_SECRET unset: run through tools/e2e/web-e2e-api.sh');
	}
	await page.context().clearCookies();
	const res = await page.request.post('/api/v1/auth/proxy-session', {
		headers: {
			[proxyUserHeader]: username,
			[proxyGroupsHeader]: 'e2e-viewers',
			[proxySecretHeader]: proxySecret
		}
	});
	expect(res.status()).toBe(200);
	const body = (await res.json()) as { user?: { role?: string } };
	expect(body.user?.role).toBe('viewer');
}

function watchApiTraffic(page: Page): { paths: string[]; forbidden: string[] } {
	const paths: string[] = [];
	const forbidden: string[] = [];
	page.on('request', (req) => {
		const pathname = new URL(req.url()).pathname;
		if (pathname.startsWith('/api/v1/')) {
			paths.push(pathname);
		}
	});
	page.on('response', (res) => {
		const pathname = new URL(res.url()).pathname;
		if (pathname.startsWith('/api/v1/') && res.status() === 403) {
			forbidden.push(`${res.request().method()} ${pathname}`);
		}
	});
	return { paths, forbidden };
}

test('900: viewer visiting /connections/configarr and /instances/:id/policy makes no admin-only requests and gets no 403', async ({
	page
}) => {
	const fakearrUrl = requireEnv('SNATCHARR_E2E_FAKEARR');
	const suffix = crypto.randomUUID().slice(0, 8);
	const csrf = await loginAsAdmin(page);
	let instanceId = '';

	try {
		const instRes = await page.request.post('/api/v1/instances', {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				kind: 'sonarr',
				name: `e2e-900-sonarr-${suffix}`,
				base_url: `${fakearrUrl}/e2e-900-${suffix}`,
				api_key: 'fake-api-key-0123456789',
				enabled: true
			}
		});
		expect(instRes.status()).toBe(201);
		instanceId = ((await instRes.json()) as { id: string }).id;

		await loginAsViewer(page, `e2e-viewer-900-${suffix}`);
		const traffic = watchApiTraffic(page);

		await page.goto('/connections/configarr');
		await expect(page.locator('#configarr-status-title')).toBeVisible();
		await page.waitForLoadState('networkidle');
		await expect(page.locator('#configarr-config-path')).toHaveCount(0);
		await expect(page.getByRole('alert')).toHaveCount(0);
		expect(traffic.paths).not.toContain('/api/v1/configarr/status');
		expect(traffic.forbidden).toEqual([]);

		await page.goto(`/instances/${instanceId}/policy`);
		await expect(page.locator('#policy-title')).toBeVisible();
		await page.waitForLoadState('networkidle');
		await expect(page.getByRole('alert')).toHaveCount(0);
		expect(traffic.paths).not.toContain(`/api/v1/instances/${instanceId}/policy/filter-options`);
		expect(traffic.forbidden).toEqual([]);

		// Negative: direct viewer GET to both endpoints is refused with 403 problem+json.
		const cfgRes = await page.request.get('/api/v1/configarr/status');
		expect(cfgRes.status()).toBe(403);
		expect(cfgRes.headers()['content-type']).toContain('application/problem+json');

		const filterRes = await page.request.get(
			`/api/v1/instances/${instanceId}/policy/filter-options`
		);
		expect(filterRes.status()).toBe(403);
		expect(filterRes.headers()['content-type']).toContain('application/problem+json');
	} finally {
		if (instanceId) {
			const adminCsrf = await loginAsAdmin(page);
			await page.request.delete(`/api/v1/instances/${instanceId}`, {
				headers: { 'X-CSRF-Token': adminCsrf }
			});
		}
	}
});

test('911: viewer visiting /system?tab=environment makes no request to /api/v1/settings/runtime and gets no 403', async ({
	page
}) => {
	const suffix = crypto.randomUUID().slice(0, 8);
	await loginAsViewer(page, `e2e-viewer-911-${suffix}`);
	const traffic = watchApiTraffic(page);

	await page.goto('/system?tab=environment');
	await expect(page.locator('#environment-main-title')).toBeVisible();
	await page.waitForLoadState('networkidle');

	await expect(page.locator('#class-runtime-heading')).toHaveCount(0);
	await expect(page.locator('table')).toHaveCount(0);
	await expect(page.getByRole('alert')).toHaveCount(0);
	expect(traffic.paths).not.toContain('/api/v1/settings/runtime');
	expect(traffic.forbidden).toEqual([]);

	const directRes = await page.request.get('/api/v1/settings/runtime');
	expect(directRes.status()).toBe(403);
	expect(directRes.headers()['content-type']).toContain('application/problem+json');
});
