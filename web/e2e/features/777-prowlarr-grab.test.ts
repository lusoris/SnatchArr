// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #777

import http from 'node:http';
import { expect, test, type Page } from '@playwright/test';
import { listenStub, type StubHandle } from './feature-helpers.js';

// The Prowlarr stub accepts any key; this one is only a placeholder.
const STUB_KEY = 'fake-api-key-0123456789';

async function getCsrfToken(page: Page): Promise<string> {
	const res = await page.request.get('/api/v1/auth/session');
	expect(res.ok()).toBeTruthy();
	const data = (await res.json()) as { csrf_token: string };
	return data.csrf_token;
}

// Start a stub Prowlarr server on an ephemeral port.
async function startProwlarrStub(): Promise<StubHandle> {
	const server = http.createServer((req, res) => {
		res.writeHead(200, { 'Content-Type': 'application/json' });
		if (req.url?.startsWith('/api/v1/system/status')) {
			res.end(JSON.stringify({ appName: 'Prowlarr', version: '1.15.0' }));
		} else if (req.url?.startsWith('/api/v1/indexerstats')) {
			res.end(
				JSON.stringify({
					indexers: [{ indexerId: 1, numberOfQueries: 5, numberOfRssQueries: 1, numberOfGrabs: 2 }]
				})
			);
		} else if (req.url?.startsWith('/api/v1/indexerstatus')) {
			res.end(JSON.stringify([]));
		} else if (req.url?.startsWith('/api/v1/applications')) {
			res.end(JSON.stringify([]));
		} else if (req.url?.startsWith('/api/v1/indexer')) {
			res.end(
				JSON.stringify([
					{
						id: 1,
						name: 'Prowlarr Idx 1',
						enable: true,
						fields: [
							{ name: 'baseSettings.queryLimit', value: 100 },
							{ name: 'baseSettings.grabLimit', value: 50 },
							{ name: 'baseSettings.limitsUnit', value: 0 }
						]
					}
				])
			);
		} else {
			res.end(JSON.stringify([]));
		}
	});
	return listenStub(server);
}

test('positive: create prowlarr link with default hourly_cap 15 and custom cap, probe stub, and verify UI form', async ({
	page
}) => {
	const stub = await startProwlarrStub();
	const csrfToken = await getCsrfToken(page);

	try {
		const createRes = await page.request.post('/api/v1/prowlarr', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				name: 'prowlarr-default-cap',
				base_url: stub.url,
				api_key: STUB_KEY,
				enabled: true
			}
		});
		expect(createRes.status()).toBe(201);
		const created = (await createRes.json()) as { id: string; hourly_cap: number; name: string };
		expect(created.hourly_cap).toBe(15);

		const readRes = await page.request.get(`/api/v1/prowlarr/${created.id}`);
		expect(readRes.status()).toBe(200);
		const read = (await readRes.json()) as { id: string; hourly_cap: number };
		expect(read.hourly_cap).toBe(15);

		const updateRes = await page.request.put(`/api/v1/prowlarr/${created.id}`, {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				name: 'prowlarr-default-cap',
				base_url: stub.url,
				hourly_cap: 35,
				enabled: true
			}
		});
		expect(updateRes.status()).toBe(200);
		const updated = (await updateRes.json()) as { id: string; hourly_cap: number };
		expect(updated.hourly_cap).toBe(35);

		const probeRes = await page.request.post(`/api/v1/prowlarr/${created.id}/test`, {
			headers: { 'X-CSRF-Token': csrfToken }
		});
		expect(probeRes.status()).toBe(200);
		const probeData = (await probeRes.json()) as { app_name: string; version: string };
		expect(probeData.app_name).toBe('Prowlarr');
		expect(probeData.version).toBe('1.15.0');

		await page.goto('/connections/prowlarr');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		const addBtn = page.getByRole('button', { name: /add link|verbindung hinzufügen/i });
		if (await addBtn.isVisible()) {
			await addBtn.click();
		}
		const capInput = page.locator('#prowlarr-hourly-cap');
		await expect(capInput).toBeVisible();
		await expect(capInput).toHaveAttribute('type', 'number');
		await expect(capInput).toHaveAttribute('min', '1');
		await expect(capInput).toHaveAttribute('max', '500');

		await page.request.delete(`/api/v1/prowlarr/${created.id}`, {
			headers: { 'X-CSRF-Token': csrfToken }
		});
	} finally {
		await stub.close();
	}
});

test('negative: hourly cap outside 1..500 rejected with problem', async ({ page }) => {
	const stub = await startProwlarrStub();
	const csrfToken = await getCsrfToken(page);

	try {
		const capZeroRes = await page.request.post('/api/v1/prowlarr', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				name: 'prowlarr-zero-cap',
				base_url: stub.url,
				api_key: STUB_KEY,
				hourly_cap: 0,
				enabled: true
			}
		});
		expect([400, 422]).toContain(capZeroRes.status());
		expect(capZeroRes.headers()['content-type']).toContain('application/problem+json');
		const p0 = (await capZeroRes.json()) as { status: number };
		expect([400, 422]).toContain(p0.status);

		const capOverRes = await page.request.post('/api/v1/prowlarr', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				name: 'prowlarr-over-cap',
				base_url: stub.url,
				api_key: STUB_KEY,
				hourly_cap: 501,
				enabled: true
			}
		});
		expect([400, 422]).toContain(capOverRes.status());
		expect(capOverRes.headers()['content-type']).toContain('application/problem+json');
		const p501 = (await capOverRes.json()) as { status: number };
		expect([400, 422]).toContain(p501.status);
	} finally {
		await stub.close();
	}
});

test('boundary: minimum cap 1 and maximum cap 500 accepted on creation', async ({ page }) => {
	const stub = await startProwlarrStub();
	const csrfToken = await getCsrfToken(page);

	try {
		const minRes = await page.request.post('/api/v1/prowlarr', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				name: 'prowlarr-boundary-min',
				base_url: stub.url,
				api_key: STUB_KEY,
				hourly_cap: 1,
				enabled: true
			}
		});
		expect(minRes.status()).toBe(201);
		const minLink = (await minRes.json()) as { id: string; hourly_cap: number };
		expect(minLink.hourly_cap).toBe(1);

		const maxRes = await page.request.post('/api/v1/prowlarr', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				name: 'prowlarr-boundary-max',
				base_url: stub.url,
				api_key: STUB_KEY,
				hourly_cap: 500,
				enabled: true
			}
		});
		expect(maxRes.status()).toBe(201);
		const maxLink = (await maxRes.json()) as { id: string; hourly_cap: number };
		expect(maxLink.hourly_cap).toBe(500);

		await page.request.delete(`/api/v1/prowlarr/${minLink.id}`, {
			headers: { 'X-CSRF-Token': csrfToken }
		});
		await page.request.delete(`/api/v1/prowlarr/${maxLink.id}`, {
			headers: { 'X-CSRF-Token': csrfToken }
		});
	} finally {
		await stub.close();
	}
});
