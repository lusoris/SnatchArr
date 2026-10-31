// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #824 #745
import fs from 'node:fs';
import http from 'node:http';
import type net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { expect, test, type Page } from '@playwright/test';

interface StubServer {
	baseUrl: string;
	close: () => Promise<void>;
}

function buildSourceAResponse(query: string): Record<string, unknown> {
	if (query.includes('queryPerformers') || query.includes('query_performers')) {
		return {
			data: {
				query_performers: {
					count: 2,
					performers: [
						{
							id: 'perf-shared-a-824',
							name: 'Performer Shared-824',
							disambiguation: '',
							aliases: [],
							urls: [
								{
									url: 'https://profile.example/shared-824',
									site: { id: 's1', name: 'Profile', url: 'https://profile.example' }
								}
							]
						},
						{
							id: 'perf-name-a-745',
							name: 'Performer Namesake-745',
							disambiguation: '',
							aliases: [],
							urls: [
								{
									url: 'https://profile.example/namesake-a',
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
						id: 'studio-a-824',
						name: 'Studio Alpha-824',
						aliases: [],
						urls: [
							{
								url: 'https://studio.example/studio-a',
								site: { id: 's2', name: 'Studio', url: 'https://studio.example' }
							}
						]
					}
				]
			}
		}
	};
}

function buildSourceBResponse(query: string): Record<string, unknown> {
	if (query.includes('queryPerformers') || query.includes('query_performers')) {
		return {
			data: {
				query_performers: {
					count: 2,
					performers: [
						{
							id: 'perf-shared-b-824',
							name: 'Performer Shared-824',
							disambiguation: '',
							aliases: [],
							urls: [
								{
									url: 'https://profile.example/shared-824',
									site: { id: 's1', name: 'Profile', url: 'https://profile.example' }
								}
							]
						},
						{
							id: 'perf-name-b-745',
							name: 'Performer Namesake-745',
							disambiguation: '',
							aliases: [],
							urls: [
								{
									url: 'https://profile.example/namesake-b',
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
						id: 'studio-b-824',
						name: 'Studio Beta-824',
						aliases: [],
						urls: [
							{
								url: 'https://studio.example/studio-b',
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
			const isSourceA = req.url?.startsWith('/a/graphql');
			const payload = isSourceA
				? buildSourceAResponse(parsed.query ?? '')
				: buildSourceBResponse(parsed.query ?? '');
			res.writeHead(200, { 'Content-Type': 'application/json' });
			res.end(JSON.stringify(payload));
		});
	});
	await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', () => resolve()));
	const port = (server.address() as net.AddressInfo).port;
	return {
		baseUrl: `http://127.0.0.1:${port}`,
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

test('824: adult performers and studios catalog, url merge, namesake split, detail refs, module-off 404', async ({
	page
}) => {
	const stub = await startGraphQLStub();
	const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'snatcharr-824-'));

	try {
		const csrf = await loginAndGetCsrf(page);

		// Negative: module off -> /adult/performers 404 and unknown id 404
		await setAdultModuleState(page, csrf, false, tmpDir);
		const offRes = await page.request.get('/api/v1/adult/performers');
		expect(offRes.status()).toBe(404);
		const offDetailRes = await page.request.get(
			'/api/v1/adult/performers/00000000-0000-0000-0000-000000000000'
		);
		expect(offDetailRes.status()).toBe(404);

		// Enable adult module
		await setAdultModuleState(page, csrf, true, tmpDir);

		// Negative when enabled: unknown id -> 404
		const unknownRes = await page.request.get(
			'/api/v1/adult/performers/00000000-0000-0000-0000-000000000000'
		);
		expect(unknownRes.status()).toBe(404);

		// Clean up leftover test sources ending with -824
		const leftRes = await page.request.get('/api/v1/adult/sources');
		expect(leftRes.ok()).toBeTruthy();
		for (const left of (await leftRes.json()) as Array<{ id: string; name: string }>) {
			if (left.name.endsWith('-824')) {
				await page.request.delete(`/api/v1/adult/sources/${left.id}`, {
					headers: { 'X-CSRF-Token': csrf }
				});
			}
		}

		// Create Source A
		const srcARes = await page.request.post('/api/v1/adult/sources', {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				name: 'e2e-source-a-824',
				endpoint: `${stub.baseUrl}/a/graphql`,
				api_key: 'fake-api-key-0123456789',
				enabled: true,
				sync_favorites: true,
				priority: 10
			}
		});
		expect(srcARes.status()).toBe(201);
		const srcA = (await srcARes.json()) as { id: string };

		// Create Source B
		const srcBRes = await page.request.post('/api/v1/adult/sources', {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				name: 'e2e-source-b-824',
				endpoint: `${stub.baseUrl}/b/graphql`,
				api_key: 'fake-api-key-0123456789',
				enabled: true,
				sync_favorites: true,
				priority: 20
			}
		});
		expect(srcBRes.status()).toBe(201);
		const srcB = (await srcBRes.json()) as { id: string };

		// Trigger synchronization
		const syncRes = await page.request.post('/api/v1/adult/sync', {
			headers: { 'X-CSRF-Token': csrf }
		});
		expect(syncRes.ok()).toBeTruthy();

		// Case merge-by-url (#824 canonical + refs):
		// "Performer Shared-824" has identical profile URL in both sources -> merged into exactly 1 performer
		const perfListRes = await page.request.get('/api/v1/adult/performers?limit=200');
		expect(perfListRes.ok()).toBeTruthy();
		const perfPage = (await perfListRes.json()) as {
			items: Array<{ id: string; name: string; ref_count?: number }>;
		};

		const sharedPerf = perfPage.items.filter((p) => p.name === 'Performer Shared-824');
		expect(sharedPerf).toHaveLength(1);

		// Detail GET /adult/performers/{performerId} has 2 external_refs
		const sharedDetailRes = await page.request.get(`/api/v1/adult/performers/${sharedPerf[0].id}`);
		expect(sharedDetailRes.ok()).toBeTruthy();
		const sharedDetail = (await sharedDetailRes.json()) as {
			id: string;
			name: string;
			external_refs: Array<{
				source_kind: string;
				foreign_id: string;
				is_favorite: boolean;
				url?: string;
			}>;
		};
		expect(sharedDetail.external_refs).toHaveLength(2);
		expect(sharedDetail.external_refs[0].source_kind).toBe('stashbox');
		expect(sharedDetail.external_refs[0].is_favorite).toBe(true);

		// Case name-only (#745):
		// "Performer Namesake-745" has different URLs in both sources -> 2 separate performers with that name, 1 ref each
		const namesakePerfs = perfPage.items.filter((p) => p.name === 'Performer Namesake-745');
		expect(namesakePerfs).toHaveLength(2);

		for (const ns of namesakePerfs) {
			const nsDetailRes = await page.request.get(`/api/v1/adult/performers/${ns.id}`);
			expect(nsDetailRes.ok()).toBeTruthy();
			const nsDetail = (await nsDetailRes.json()) as {
				external_refs: Array<{ foreign_id: string }>;
			};
			expect(nsDetail.external_refs).toHaveLength(1);
		}

		// Studios: one studio per source -> assert GET /adult/studios + detail
		const studioListRes = await page.request.get('/api/v1/adult/studios?limit=200');
		expect(studioListRes.ok()).toBeTruthy();
		const studioPage = (await studioListRes.json()) as {
			items: Array<{ id: string; name: string; ref_count?: number }>;
		};
		const studioAItem = studioPage.items.find((s) => s.name === 'Studio Alpha-824');
		const studioBItem = studioPage.items.find((s) => s.name === 'Studio Beta-824');
		expect(studioAItem).toBeTruthy();
		expect(studioBItem).toBeTruthy();

		const studioADetailRes = await page.request.get(
			`/api/v1/adult/studios/${studioAItem?.id ?? ''}`
		);
		expect(studioADetailRes.ok()).toBeTruthy();
		const studioADetail = (await studioADetailRes.json()) as {
			id: string;
			name: string;
			external_refs: Array<{ foreign_id: string }>;
		};
		expect(studioADetail.external_refs).toHaveLength(1);
		expect(studioADetail.external_refs[0].foreign_id).toBe('studio-a-824');

		// UI: /adult/performers shows both namesakes; /adult/studios shows the studio
		await page.goto('/adult/performers');
		await expect(page.getByText('Performer Shared-824')).toBeVisible();
		await expect(page.getByText('Performer Namesake-745').first()).toBeVisible();

		await page.goto('/adult/studios');
		await expect(page.getByText('Studio Alpha-824')).toBeVisible();

		// Clean up created sources
		await page.request.delete(`/api/v1/adult/sources/${srcA.id}`, {
			headers: { 'X-CSRF-Token': csrf }
		});
		await page.request.delete(`/api/v1/adult/sources/${srcB.id}`, {
			headers: { 'X-CSRF-Token': csrf }
		});

		// Restore module off state
		await setAdultModuleState(page, csrf, false, tmpDir);
	} finally {
		await stub.close();
		fs.rmSync(tmpDir, { recursive: true, force: true });
	}
});
