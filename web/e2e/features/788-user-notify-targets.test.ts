// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #788

import { expect, request, test, type Page } from '@playwright/test';

async function getCsrfToken(page: Page): Promise<string> {
	const res = await page.request.get('/api/v1/auth/session');
	expect(res.ok()).toBeTruthy();
	const data = (await res.json()) as { csrf_token: string };
	return data.csrf_token;
}

test('positive: create, read, update and delete personal notification target', async ({ page }) => {
	const csrfToken = await getCsrfToken(page);

	const createRes = await page.request.post('/api/v1/me/notify-targets', {
		headers: { 'X-CSRF-Token': csrfToken },
		data: {
			kind: 'webhook',
			enabled: true,
			config: {
				url: 'http://127.0.0.1:9091/webhook',
				allow_private_networks: true,
				allow_insecure_http: true
			}
		}
	});
	expect(createRes.status()).toBe(201);
	const created = (await createRes.json()) as { id: string; kind: string; enabled: boolean };
	expect(created.id).toMatch(/^[0-9a-f-]{36}$/i);
	expect(created.kind).toBe('webhook');
	expect(created.enabled).toBe(true);

	const listRes = await page.request.get('/api/v1/me/notify-targets');
	expect(listRes.status()).toBe(200);
	const list = (await listRes.json()) as Array<{ id: string }>;
	expect(list.some((t) => t.id === created.id)).toBe(true);

	const getRes = await page.request.get(`/api/v1/me/notify-targets/${created.id}`);
	expect(getRes.status()).toBe(200);
	const single = (await getRes.json()) as { id: string; kind: string; enabled: boolean };
	expect(single.id).toBe(created.id);
	expect(single.kind).toBe('webhook');

	const putRes = await page.request.put(`/api/v1/me/notify-targets/${created.id}`, {
		headers: { 'X-CSRF-Token': csrfToken },
		data: {
			kind: 'webhook',
			enabled: false
		}
	});
	expect(putRes.status()).toBe(200);
	const updated = (await putRes.json()) as { id: string; enabled: boolean };
	expect(updated.id).toBe(created.id);
	expect(updated.enabled).toBe(false);

	const delRes = await page.request.delete(`/api/v1/me/notify-targets/${created.id}`, {
		headers: { 'X-CSRF-Token': csrfToken }
	});
	expect(delRes.status()).toBe(204);

	const afterDelRes = await page.request.get(`/api/v1/me/notify-targets/${created.id}`);
	expect(afterDelRes.status()).toBe(404);
	expect(afterDelRes.headers()['content-type']).toContain('application/problem+json');
});

test('negative: invalid kind rejected with 422, missing configuration with 400 problem', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);

	const badKindRes = await page.request.post('/api/v1/me/notify-targets', {
		headers: { 'X-CSRF-Token': csrfToken },
		data: {
			kind: 'carrier_pigeon',
			enabled: true,
			config: {}
		}
	});
	expect(badKindRes.status()).toBe(422);
	expect(badKindRes.headers()['content-type']).toContain('application/problem+json');
	const badKindProblem = (await badKindRes.json()) as { status: number };
	expect(badKindProblem.status).toBe(422);

	const missingConfigRes = await page.request.post('/api/v1/me/notify-targets', {
		headers: { 'X-CSRF-Token': csrfToken },
		data: {
			kind: 'webhook',
			enabled: true,
			config: {}
		}
	});
	expect(missingConfigRes.status()).toBe(400);
	expect(missingConfigRes.headers()['content-type']).toContain('application/problem+json');
	const missingConfigProblem = (await missingConfigRes.json()) as { status: number };
	expect(missingConfigProblem.status).toBe(400);
});

test('boundary: non-existent target id returns 404 problem and anonymous access returns 401 problem', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);
	const dummyId = '00000000-0000-0000-0000-000000000000';

	const getMissingRes = await page.request.get(`/api/v1/me/notify-targets/${dummyId}`);
	expect(getMissingRes.status()).toBe(404);
	expect(getMissingRes.headers()['content-type']).toContain('application/problem+json');

	const delMissingRes = await page.request.delete(`/api/v1/me/notify-targets/${dummyId}`, {
		headers: { 'X-CSRF-Token': csrfToken }
	});
	expect(delMissingRes.status()).toBe(404);
	expect(delMissingRes.headers()['content-type']).toContain('application/problem+json');

	const anon = await request.newContext({ storageState: { cookies: [], origins: [] } });
	try {
		const res = await anon.get('/api/v1/me/notify-targets');
		expect(res.status()).toBe(401);
		expect(res.headers()['content-type']).toContain('application/problem+json');
		const problem = (await res.json()) as { status: number };
		expect(problem.status).toBe(401);
	} finally {
		await anon.dispose();
	}
});
