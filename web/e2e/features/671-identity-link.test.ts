// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #671

import { expect, request, test, type Page } from '@playwright/test';

async function getCsrfToken(page: Page): Promise<string> {
	const res = await page.request.get('/api/v1/auth/session');
	expect(res.ok()).toBeTruthy();
	const data = (await res.json()) as { csrf_token: string };
	return data.csrf_token;
}

test('positive: list external identities returns 200 with items array', async ({ page }) => {
	const res = await page.request.get('/api/v1/auth/identities');
	expect(res.status()).toBe(200);
	expect(res.headers()['content-type']).toContain('application/json');

	const data = (await res.json()) as { items: Array<{ provider: string; linked_at: string }> };
	expect(Array.isArray(data.items)).toBe(true);
});

test('negative: unconfigured jellyfin and proxy identity linking endpoints return 404 problem', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);

	// In the test harness, the real API binary is launched without APP_SNATCHARR_JELLYFIN_ENABLED
	// or APP_SNATCHARR_PROXY_AUTH_ENABLED. Unconfigured providers return 404 (domain.ErrNotFound).
	const jfLinkRes = await page.request.post('/api/v1/auth/identities/jellyfin', {
		headers: { 'X-CSRF-Token': csrfToken },
		data: { username: 'jf-test-user', password: 'jf-test-password' }
	});
	expect(jfLinkRes.status()).toBe(404);
	expect(jfLinkRes.headers()['content-type']).toContain('application/problem+json');
	const jfProblem = (await jfLinkRes.json()) as { status: number };
	expect(jfProblem.status).toBe(404);

	const jfUnlinkRes = await page.request.delete('/api/v1/auth/identities/jellyfin', {
		headers: { 'X-CSRF-Token': csrfToken }
	});
	expect(jfUnlinkRes.status()).toBe(404);
	expect(jfUnlinkRes.headers()['content-type']).toContain('application/problem+json');

	const proxyLinkRes = await page.request.post('/api/v1/auth/identities/proxy', {
		headers: { 'X-CSRF-Token': csrfToken }
	});
	expect(proxyLinkRes.status()).toBe(404);
	expect(proxyLinkRes.headers()['content-type']).toContain('application/problem+json');

	const proxyUnlinkRes = await page.request.delete('/api/v1/auth/identities/proxy', {
		headers: { 'X-CSRF-Token': csrfToken }
	});
	expect(proxyUnlinkRes.status()).toBe(404);
	expect(proxyUnlinkRes.headers()['content-type']).toContain('application/problem+json');
});

test('boundary: unauthenticated access rejected with 401 problem and account page renders without error', async ({
	page
}) => {
	const anon = await request.newContext({ storageState: { cookies: [], origins: [] } });
	try {
		const res = await anon.get('/api/v1/auth/identities');
		expect(res.status()).toBe(401);
		expect(res.headers()['content-type']).toContain('application/problem+json');
		const problem = (await res.json()) as { status: number };
		expect(problem.status).toBe(401);
	} finally {
		await anon.dispose();
	}

	// Verify the UI side on /account when SSO is configured
	await page.route('**/api/v1/auth/sso', (route) =>
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({
				oidc: false,
				proxy: false,
				jellyfin: true,
				jellyfinQuickConnect: false
			})
		})
	);
	await page.route('**/api/v1/auth/identities', (route) =>
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({
				items: [{ provider: 'jellyfin', linked_at: '2026-01-01T00:00:00Z' }]
			})
		})
	);

	await page.goto('/account');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	const section = page.locator('section').filter({ hasText: /jellyfin/i });
	await expect(section.first()).toBeVisible();
});
