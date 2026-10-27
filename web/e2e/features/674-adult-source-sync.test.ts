// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #674
import fs from 'node:fs';
import http from 'node:http';
import type net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { expect, test, type Page } from '@playwright/test';

interface StubServer {
	url: string;
	close: () => Promise<void>;
}

function buildGraphQLResponse(query: string): Record<string, unknown> {
	if (query.includes('queryPerformers')) {
		return {
			data: {
				query_performers: {
					count: 1,
					performers: [
						{
							id: 'perf-ext-674',
							name: 'Performer SixSevenFour',
							disambiguation: 'Series A',
							aliases: [],
							urls: [
								{
									url: 'https://profile.example/perf-674',
									site: { id: 's1', name: 'Profile', url: 'https://profile.example' }
								}
							]
						}
					]
				}
			}
		};
	}
	return {
		data: {
			query_studios: {
				count: 1,
				studios: [
					{
						id: 'studio-ext-674',
						name: 'Studio SixSevenFour',
						aliases: [],
						urls: [
							{
								url: 'https://studio.example/studio-674',
								site: { id: 's2', name: 'Studio', url: 'https://studio.example' }
							}
						]
					}
				]
			}
		}
	};
}

async function startGraphQLStub(): Promise<StubServer> {
	const server = http.createServer((req, res) => {
		let raw = '';
		req.on('data', (chunk: Buffer) => {
			raw += chunk.toString();
		});
		req.on('end', () => {
			const parsed = raw ? (JSON.parse(raw) as { query?: string }) : {};
			const payload = buildGraphQLResponse(parsed.query ?? '');
			res.writeHead(200, { 'Content-Type': 'application/json' });
			res.end(JSON.stringify(payload));
		});
	});
	await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', () => resolve()));
	const port = (server.address() as net.AddressInfo).port;
	return {
		url: `http://127.0.0.1:${port}/graphql`,
		close: () => new Promise<void>((resolve) => server.close(() => resolve()))
	};
}

async function loginAndGetCsrf(page: Page): Promise<string> {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok()).toBeTruthy();
	const sess = (await login.json()) as { csrf_token: string };
	return sess.csrf_token;
}

async function setAdultModuleState(
	page: Page,
	csrf: string,
	enabled: boolean,
	imageSetDir: string
): Promise<void> {
	const res = await page.request.put('/api/v1/system/modules/adult', {
		headers: { 'X-CSRF-Token': csrf },
		data: {
			enabled,
			config: { image_set_dir: imageSetDir, sync_interval_minutes: 360 }
		}
	});
	expect(res.ok()).toBeTruthy();
}

test('674: adult catalogue source CRUD, favourite sync, module-off 404, and disabled boundary', async ({
	page
}) => {
	const stub = await startGraphQLStub();
	const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'snatcharr-674-'));

	try {
		const csrf = await loginAndGetCsrf(page);

		// Negative: module off -> 404
		await setAdultModuleState(page, csrf, false, tmpDir);
		const offRes = await page.request.get('/api/v1/adult/sources');
		expect(offRes.status()).toBe(404);

		// Enable adult module
		await setAdultModuleState(page, csrf, true, tmpDir);

		// Positive: create source pointing at local GraphQL stub
		const createRes = await page.request.post('/api/v1/adult/sources', {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				name: 'e2e-catalogue-674',
				endpoint: stub.url,
				api_key: 'e2e-secret-key-674',
				enabled: true,
				sync_favorites: true,
				priority: 10
			}
		});
		expect(createRes.status()).toBe(201);
		const created = (await createRes.json()) as { id: string; name: string };

		// Trigger sync and verify status counts > 0
		const syncRes = await page.request.post('/api/v1/adult/sync', {
			headers: { 'X-CSRF-Token': csrf }
		});
		expect(syncRes.ok()).toBeTruthy();

		const statusRes = await page.request.get('/api/v1/adult/sync/status');
		expect(statusRes.ok()).toBeTruthy();
		const status = (await statusRes.json()) as {
			total_sources: number;
			enabled_sources: number;
			performers_synced: number;
			studios_synced: number;
		};
		expect(status.total_sources).toBeGreaterThan(0);
		expect(status.enabled_sources).toBeGreaterThan(0);
		expect(status.performers_synced).toBeGreaterThan(0);
		expect(status.studios_synced).toBeGreaterThan(0);

		// UI page lists the source
		await page.goto('/adult/sources');
		await expect(page.getByText('e2e-catalogue-674')).toBeVisible();

		// Boundary: source disabled -> sync summary 0
		const disableSrcRes = await page.request.put(`/api/v1/adult/sources/${created.id}`, {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				name: 'e2e-catalogue-674',
				endpoint: stub.url,
				enabled: false,
				sync_favorites: true,
				priority: 10
			}
		});
		expect(disableSrcRes.ok()).toBeTruthy();

		const boundarySync = await page.request.post('/api/v1/adult/sync', {
			headers: { 'X-CSRF-Token': csrf }
		});
		expect(boundarySync.ok()).toBeTruthy();
		const summary = (await boundarySync.json()) as {
			sources_synced: number;
			performers_synced: number;
			studios_synced: number;
		};
		expect(summary.sources_synced).toBe(0);
		expect(summary.performers_synced).toBe(0);
		expect(summary.studios_synced).toBe(0);

		// Clean up created source and restore module off state
		await page.request.delete(`/api/v1/adult/sources/${created.id}`, {
			headers: { 'X-CSRF-Token': csrf }
		});
		await setAdultModuleState(page, csrf, false, tmpDir);
	} finally {
		await stub.close();
		fs.rmSync(tmpDir, { recursive: true, force: true });
	}
});
