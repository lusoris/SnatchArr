// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #823

import http from 'node:http';
import type net from 'node:net';
import { expect, test, type Page } from '@playwright/test';

async function getCsrfToken(page: Page): Promise<string> {
	const res = await page.request.get('/api/v1/auth/session');
	expect(res.ok()).toBeTruthy();
	const data = (await res.json()) as { csrf_token: string };
	return data.csrf_token;
}

async function startStashStub(): Promise<{ url: string; close: () => Promise<void> }> {
	const server = http.createServer((_req, res) => {
		res.writeHead(200, { 'Content-Type': 'application/json' });
		res.end(JSON.stringify({ version: 'v0.27.2' }));
	});
	await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', () => resolve()));
	const port = (server.address() as net.AddressInfo).port;
	return {
		url: `http://127.0.0.1:${port}`,
		close: () =>
			new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())))
	};
}

test('positive: create stash instance and read kind=stash back from get and list', async ({
	page
}) => {
	const stub = await startStashStub();
	const csrfToken = await getCsrfToken(page);
	const stashName = 'e2e-stash-positive';

	try {
		const createRes = await page.request.post('/api/v1/instances', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				kind: 'stash',
				name: stashName,
				base_url: stub.url,
				api_key: 'stash-secret-token-823',
				enabled: true
			}
		});
		expect(createRes.status()).toBe(201);
		const created = (await createRes.json()) as { id: string; kind: string; name: string };
		expect(created.kind).toBe('stash');
		expect(created.name).toBe(stashName);

		const readRes = await page.request.get(`/api/v1/instances/${created.id}`);
		expect(readRes.status()).toBe(200);
		const read = (await readRes.json()) as { id: string; kind: string; name: string };
		expect(read.kind).toBe('stash');
		expect(read.name).toBe(stashName);

		const listRes = await page.request.get('/api/v1/instances');
		expect(listRes.status()).toBe(200);
		const instances = (await listRes.json()) as Array<{ id: string; kind: string }>;
		expect(instances.some((i) => i.id === created.id && i.kind === 'stash')).toBe(true);

		await page.request.delete(`/api/v1/instances/${created.id}`, {
			headers: { 'X-CSRF-Token': csrfToken }
		});
	} finally {
		await stub.close();
	}
});

test('negative: snatch and wanted operations on stash instance are refused with 422 problem', async ({
	page
}) => {
	const stub = await startStashStub();
	const csrfToken = await getCsrfToken(page);

	try {
		const createRes = await page.request.post('/api/v1/instances', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				kind: 'stash',
				name: 'e2e-stash-negative',
				base_url: stub.url,
				api_key: 'stash-secret-token-823',
				enabled: true
			}
		});
		expect(createRes.status()).toBe(201);
		const created = (await createRes.json()) as { id: string };

		const previewRes = await page.request.post(
			`/api/v1/instances/${created.id}/preview?kind=missing`,
			{ headers: { 'X-CSRF-Token': csrfToken } }
		);
		expect(previewRes.status()).toBe(422);
		expect(previewRes.headers()['content-type']).toContain('application/problem+json');
		const previewProblem = (await previewRes.json()) as {
			status: number;
			title?: string;
			detail?: string;
		};
		expect(previewProblem.status).toBe(422);
		expect(previewProblem.detail ?? previewProblem.title).toMatch(
			/stash instance does not support snatching|unprocessable/i
		);

		const triggerRes = await page.request.post(`/api/v1/instances/${created.id}/runs`, {
			headers: { 'X-CSRF-Token': csrfToken },
			data: { kind: 'missing' }
		});
		expect(triggerRes.status()).toBe(422);
		expect(triggerRes.headers()['content-type']).toContain('application/problem+json');
		const triggerProblem = (await triggerRes.json()) as { status: number };
		expect(triggerProblem.status).toBe(422);

		const searchRes = await page.request.post(`/api/v1/instances/${created.id}/items/search`, {
			headers: { 'X-CSRF-Token': csrfToken },
			data: { kind: 'missing', entity_id: 12345 }
		});
		expect(searchRes.status()).toBe(422);
		expect(searchRes.headers()['content-type']).toContain('application/problem+json');
		const searchProblem = (await searchRes.json()) as { status: number };
		expect(searchProblem.status).toBe(422);

		await page.request.delete(`/api/v1/instances/${created.id}`, {
			headers: { 'X-CSRF-Token': csrfToken }
		});
	} finally {
		await stub.close();
	}
});

test('boundary: stash instance priority and hourly cap exclusions, invalid kind rejection', async ({
	page
}) => {
	const stub = await startStashStub();
	const csrfToken = await getCsrfToken(page);

	try {
		const createRes = await page.request.post('/api/v1/instances', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				kind: 'stash',
				name: 'e2e-stash-boundary',
				base_url: stub.url,
				api_key: 'stash-secret-token-823',
				enabled: true
			}
		});
		expect(createRes.status()).toBe(201);
		const created = (await createRes.json()) as { id: string };

		const priorityRes = await page.request.get(`/api/v1/instances/${created.id}/priority`);
		expect(priorityRes.status()).toBe(422);
		expect(priorityRes.headers()['content-type']).toContain('application/problem+json');
		const priorityProblem = (await priorityRes.json()) as { status: number };
		expect(priorityProblem.status).toBe(422);

		const capsRes = await page.request.get('/api/v1/hourly-caps');
		expect(capsRes.status()).toBe(200);
		const caps = (await capsRes.json()) as Array<{ instance_id: string }>;
		expect(caps.some((c) => c.instance_id === created.id)).toBe(false);

		const invalidKindRes = await page.request.post('/api/v1/instances', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				kind: 'unsupported_video_kind',
				name: 'e2e-stash-invalid-kind',
				base_url: stub.url,
				api_key: 'some-key',
				enabled: true
			}
		});
		expect(invalidKindRes.status()).toBe(422);
		expect(invalidKindRes.headers()['content-type']).toContain('application/problem+json');
		const invalidProblem = (await invalidKindRes.json()) as { status: number };
		expect(invalidProblem.status).toBe(422);

		await page.request.delete(`/api/v1/instances/${created.id}`, {
			headers: { 'X-CSRF-Token': csrfToken }
		});
	} finally {
		await stub.close();
	}
});
