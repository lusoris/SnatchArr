// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #775

import { expect, test, type Page } from '@playwright/test';

async function getCsrfToken(page: Page): Promise<string> {
	const res = await page.request.get('/api/v1/auth/session');
	expect(res.ok()).toBeTruthy();
	const data = (await res.json()) as { csrf_token: string };
	return data.csrf_token;
}

async function getFakearrInstance(
	page: Page,
	csrfToken: string
): Promise<{ id: string; cleanup: () => Promise<void> }> {
	const fakearrUrl = process.env.SNATCHARR_E2E_FAKEARR;
	if (!fakearrUrl) {
		throw new Error('missing required environment variable: SNATCHARR_E2E_FAKEARR');
	}
	// An instance of its own: the seeded one can hold a queued run from another test, which
	// refuses a new item search with 409. Base URLs are unique per instance; the item search
	// only queues a run, so the path suffix is never requested.
	const suffix = crypto.randomUUID().slice(0, 8);
	const createRes = await page.request.post('/api/v1/instances', {
		headers: { 'X-CSRF-Token': csrfToken },
		data: {
			kind: 'sonarr',
			name: `e2e-cooldown-sonarr-${suffix}`,
			base_url: `${fakearrUrl}/e2e-775-${suffix}`,
			api_key: 'fake-api-key-0123456789',
			enabled: true
		}
	});
	expect(createRes.status()).toBe(201);
	const created = (await createRes.json()) as { id: string };
	return {
		id: created.id,
		cleanup: async () => {
			await page.request.delete(`/api/v1/instances/${created.id}`, {
				headers: { 'X-CSRF-Token': csrfToken }
			});
		}
	};
}

test('positive and negative: first item search accepted, immediate repeat by same user rejected with 429 cooldown', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);
	const inst = await getFakearrInstance(page, csrfToken);

	try {
		// First search call: accepted and queued
		const firstRes = await page.request.post(`/api/v1/instances/${inst.id}/items/search`, {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				kind: 'missing',
				entity_id: 1000
			}
		});
		expect(firstRes.status()).toBe(201);
		const run = (await firstRes.json()) as {
			id: string;
			status: string;
			focus_entity_id?: number;
		};
		expect(run.status).toBe('queued');
		expect(run.focus_entity_id).toBe(1000);

		// Immediate second search call by the same user: cooldown refusal
		const secondRes = await page.request.post(`/api/v1/instances/${inst.id}/items/search`, {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				kind: 'missing',
				entity_id: 1001
			}
		});
		expect(secondRes.status()).toBe(429);
		expect(secondRes.headers()['content-type']).toContain('application/problem+json');

		const retryAfterHeader = secondRes.headers()['retry-after'];
		expect(retryAfterHeader).toBeDefined();
		const retryAfter = Number(retryAfterHeader);
		expect(Number.isNaN(retryAfter)).toBe(false);
		expect(retryAfter).toBeGreaterThan(0);

		const problem = (await secondRes.json()) as {
			status: number;
			title?: string;
			detail?: string;
		};
		expect(problem.status).toBe(429);
		expect(problem.title ?? problem.detail).toMatch(/too many requests/i);
	} finally {
		if (inst.cleanup) {
			await inst.cleanup();
		}
	}
});

test('boundary: invalid entity and group combinations rejected with 400 problem, unknown instance with 404', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);
	const inst = await getFakearrInstance(page, csrfToken);

	try {
		// Boundary: both entity_id and group_id
		const bothRes = await page.request.post(`/api/v1/instances/${inst.id}/items/search`, {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				kind: 'missing',
				entity_id: 1000,
				group_id: 50
			}
		});
		expect(bothRes.status()).toBe(400);
		expect(bothRes.headers()['content-type']).toContain('application/problem+json');

		// Boundary: neither entity_id nor group_id
		const neitherRes = await page.request.post(`/api/v1/instances/${inst.id}/items/search`, {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				kind: 'missing'
			}
		});
		expect(neitherRes.status()).toBe(400);
		expect(neitherRes.headers()['content-type']).toContain('application/problem+json');

		// Boundary: non-existent instance UUID
		const dummyId = '00000000-0000-0000-0000-000000000000';
		const missingInstRes = await page.request.post(`/api/v1/instances/${dummyId}/items/search`, {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				kind: 'missing',
				entity_id: 1000
			}
		});
		expect(missingInstRes.status()).toBe(404);
		expect(missingInstRes.headers()['content-type']).toContain('application/problem+json');
	} finally {
		if (inst.cleanup) {
			await inst.cleanup();
		}
	}
});
