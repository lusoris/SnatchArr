// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #671

import { expect, request, test, type Page } from '@playwright/test';

const proxySecret = process.env.SNATCHARR_E2E_PROXY_SECRET ?? '';
const proxyUserHeader = process.env.SNATCHARR_E2E_PROXY_HEADER ?? 'Remote-User';
const proxyGroupsHeader = process.env.SNATCHARR_E2E_PROXY_GROUPS_HEADER ?? 'X-authentik-groups';
const proxySecretHeader =
	process.env.SNATCHARR_E2E_PROXY_SECRET_HEADER ?? 'X-SnatchArr-Proxy-Secret';

function proxyHeaders(user: string, groups: string, secret = proxySecret): Record<string, string> {
	return {
		[proxyUserHeader]: user,
		[proxyGroupsHeader]: groups,
		[proxySecretHeader]: secret
	};
}

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

test('positive: link proxy identity with viewer group returns 204, list shows provider proxy, unlink returns 204, list empty', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);

	// Ensure any prior proxy link is cleared
	await page.request.delete('/api/v1/auth/identities/proxy', {
		headers: { 'X-CSRF-Token': csrfToken }
	});

	// Link proxy identity with non-admin role (viewer group e2e-viewers) -> 204
	const linkRes = await page.request.post('/api/v1/auth/identities/proxy', {
		headers: {
			'X-CSRF-Token': csrfToken,
			...proxyHeaders('e2e-viewer-user', 'e2e-viewers')
		}
	});
	expect(linkRes.status()).toBe(204);

	// List identities shows provider proxy
	const listRes = await page.request.get('/api/v1/auth/identities');
	expect(listRes.status()).toBe(200);
	const listData = (await listRes.json()) as {
		items: Array<{ provider: string; linked_at: string }>;
	};
	expect(listData.items.some((i) => i.provider === 'proxy')).toBe(true);

	// Unlink proxy identity -> 204
	const unlinkRes = await page.request.delete('/api/v1/auth/identities/proxy', {
		headers: { 'X-CSRF-Token': csrfToken }
	});
	expect(unlinkRes.status()).toBe(204);

	// List identities is empty
	const emptyRes = await page.request.get('/api/v1/auth/identities');
	expect(emptyRes.status()).toBe(200);
	const emptyData = (await emptyRes.json()) as {
		items: Array<{ provider: string; linked_at: string }>;
	};
	expect(emptyData.items.some((i) => i.provider === 'proxy')).toBe(false);
});

test('negative: unconfigured jellyfin identity linking endpoints return 404 problem', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);

	// In the test harness, the real API binary is launched without APP_SNATCHARR_JELLYFIN_ENABLED.
	// Unconfigured jellyfin provider returns 404 (domain.ErrNotFound).
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
});

test('negative: proxy link refuses admin group (409), no groups (403), wrong secret (401), and anonymous (401)', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);

	// Admin group cannot be linked -> 409 Conflict (handlers_identities.go lines 95-101)
	const adminRes = await page.request.post('/api/v1/auth/identities/proxy', {
		headers: {
			'X-CSRF-Token': csrfToken,
			...proxyHeaders('e2e-admin-user', 'e2e-admins')
		}
	});
	expect(adminRes.status()).toBe(409);
	expect(adminRes.headers()['content-type']).toContain('application/problem+json');
	const adminProblem = (await adminRes.json()) as { status: number };
	expect(adminProblem.status).toBe(409);

	// No groups or unrecognized groups -> 403 Forbidden (handlers_identities.go lines 86-94)
	const noGroupsRes = await page.request.post('/api/v1/auth/identities/proxy', {
		headers: {
			'X-CSRF-Token': csrfToken,
			[proxyUserHeader]: 'e2e-viewer-user',
			[proxySecretHeader]: proxySecret
		}
	});
	expect(noGroupsRes.status()).toBe(403);
	expect(noGroupsRes.headers()['content-type']).toContain('application/problem+json');
	const noGroupsProblem = (await noGroupsRes.json()) as { status: number };
	expect(noGroupsProblem.status).toBe(403);

	// Wrong proxy secret -> 401 Unauthorized (sso.go lines 108-111, problem.go line 31)
	const wrongSecretRes = await page.request.post('/api/v1/auth/identities/proxy', {
		headers: {
			'X-CSRF-Token': csrfToken,
			...proxyHeaders(
				'e2e-viewer-user',
				'e2e-viewers',
				'wrong-proxy-secret-00000000000000000000000000000000'
			)
		}
	});
	expect(wrongSecretRes.status()).toBe(401);
	expect(wrongSecretRes.headers()['content-type']).toContain('application/problem+json');
	const wrongSecretProblem = (await wrongSecretRes.json()) as { status: number };
	expect(wrongSecretProblem.status).toBe(401);

	// Anonymous request -> 401 Unauthorized (security.go lines 64-67, handlers_identities.go lines 77-80)
	const anon = await request.newContext({ storageState: { cookies: [], origins: [] } });
	try {
		const anonRes = await anon.post('/api/v1/auth/identities/proxy', {
			headers: proxyHeaders('e2e-viewer-user', 'e2e-viewers')
		});
		expect(anonRes.status()).toBe(401);
		expect(anonRes.headers()['content-type']).toContain('application/problem+json');
		const anonProblem = (await anonRes.json()) as { status: number };
		expect(anonProblem.status).toBe(401);
	} finally {
		await anon.dispose();
	}
});

test('boundary: link twice returns 204 (idempotent), different subject returns 409, and unlink when none returns 404', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);

	// Ensure clean state before boundary test
	await page.request.delete('/api/v1/auth/identities/proxy', {
		headers: { 'X-CSRF-Token': csrfToken }
	});

	// First link -> 204
	const firstLink = await page.request.post('/api/v1/auth/identities/proxy', {
		headers: {
			'X-CSRF-Token': csrfToken,
			...proxyHeaders('e2e-boundary-viewer', 'e2e-viewers')
		}
	});
	expect(firstLink.status()).toBe(204);

	// Second link with exact same proxy identity -> 204 (idempotent, identity.go lines 36-39)
	const secondLink = await page.request.post('/api/v1/auth/identities/proxy', {
		headers: {
			'X-CSRF-Token': csrfToken,
			...proxyHeaders('e2e-boundary-viewer', 'e2e-viewers')
		}
	});
	expect(secondLink.status()).toBe(204);

	// Second link with DIFFERENT proxy identity without unlinking -> 409 Conflict (identity.go lines 68-75)
	const diffLink = await page.request.post('/api/v1/auth/identities/proxy', {
		headers: {
			'X-CSRF-Token': csrfToken,
			...proxyHeaders('e2e-other-viewer', 'e2e-viewers')
		}
	});
	expect(diffLink.status()).toBe(409);
	expect(diffLink.headers()['content-type']).toContain('application/problem+json');
	const diffProblem = (await diffLink.json()) as { status: number };
	expect(diffProblem.status).toBe(409);

	// Unlink -> 204
	const unlinkRes = await page.request.delete('/api/v1/auth/identities/proxy', {
		headers: { 'X-CSRF-Token': csrfToken }
	});
	expect(unlinkRes.status()).toBe(204);

	// Unlink when none linked -> 404 Not Found (identity.go line 177, handlers_identities.go line 119)
	const unlinkNoneRes = await page.request.delete('/api/v1/auth/identities/proxy', {
		headers: { 'X-CSRF-Token': csrfToken }
	});
	expect(unlinkNoneRes.status()).toBe(404);
	expect(unlinkNoneRes.headers()['content-type']).toContain('application/problem+json');
	const unlinkNoneProblem = (await unlinkNoneRes.json()) as { status: number };
	expect(unlinkNoneProblem.status).toBe(404);
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
