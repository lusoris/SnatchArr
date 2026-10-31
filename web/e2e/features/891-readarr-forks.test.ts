// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #891 #896

import http from 'node:http';
import type net from 'node:net';
import { expect, test, type Page } from '@playwright/test';

interface StoredInstance {
	last_seen_product?: string;
}

interface StubStatus {
	appName: string;
	version: string;
	packageAuthor?: string;
}

const statusRoutes: Record<string, StubStatus> = {
	rresurrected: {
		appName: 'Readarr',
		version: '11.3.1.0',
		packageAuthor: '[ricetim](https://github.com/ricetim)'
	},
	fork: {
		appName: 'Readarr',
		version: '11.3.1.0',
		packageAuthor: ''
	},
	'boundary-readarr': {
		appName: 'Readarr',
		version: '0.4.19.99999',
		packageAuthor: ''
	},
	'boundary-fork': {
		appName: 'Readarr',
		version: '0.4.20.0',
		packageAuthor: ''
	},
	'unknown-author': {
		appName: 'Readarr',
		version: '11.3.1.0',
		packageAuthor: '[ricetimber](https://github.com/ricetimber)'
	}
};

async function startReadarrStub(): Promise<{
	baseUrl: (segment: string) => string;
	close: () => Promise<void>;
}> {
	const servers: http.Server[] = [];
	const baseUrls: Record<string, string> = {};

	for (const [segment, payload] of Object.entries(statusRoutes)) {
		const server = http.createServer((_req, res) => {
			res.writeHead(200, { 'Content-Type': 'application/json' });
			res.end(JSON.stringify(payload));
		});
		await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', () => resolve()));
		const port = (server.address() as net.AddressInfo).port;
		baseUrls[segment] = `http://127.0.0.1:${port}`;
		servers.push(server);
	}

	return {
		baseUrl: (segment: string) => baseUrls[segment] ?? '',
		close: async () => {
			await Promise.all(
				servers.map(
					(s) =>
						new Promise<void>((resolve, reject) =>
							s.close((err) => (err ? reject(err) : resolve()))
						)
				)
			);
		}
	};
}

async function getCsrfToken(page: Page): Promise<string> {
	const res = await page.request.get('/api/v1/auth/session');
	expect(res.ok()).toBeTruthy();
	const data = (await res.json()) as { csrf_token: string };
	return data.csrf_token;
}

async function probeProduct(
	page: Page,
	csrf: string,
	baseUrl: string,
	segment: string
): Promise<string> {
	const res = await page.request.post('/api/v1/instances/test', {
		headers: { 'X-CSRF-Token': csrf },
		data: {
			kind: 'readarr',
			name: `e2e-probe-${segment}`,
			base_url: baseUrl,
			api_key: 'fake-api-key-0123456789'
		}
	});
	expect(res.ok(), await res.text()).toBeTruthy();
	const body = (await res.json()) as { product: string };
	return body.product;
}

test('891/896: Readarr rresurrected and unbranded post-retirement forks resolve in probe API and render distinct product labels in UI', async ({
	page
}) => {
	const csrf = await getCsrfToken(page);
	const stub = await startReadarrStub();
	const suffix = crypto.randomUUID().slice(0, 8);
	const rresName = `e2e-rresurrected-${suffix}`;
	const forkName = `e2e-readarr-fork-${suffix}`;
	const createdIds: string[] = [];

	try {
		expect(await probeProduct(page, csrf, stub.baseUrl('rresurrected'), 'rresurrected')).toBe(
			'readarr_rresurrected'
		);
		expect(await probeProduct(page, csrf, stub.baseUrl('fork'), 'fork')).toBe('readarr_fork');
		expect(
			await probeProduct(page, csrf, stub.baseUrl('boundary-readarr'), 'boundary-readarr')
		).toBe('readarr');
		expect(await probeProduct(page, csrf, stub.baseUrl('boundary-fork'), 'boundary-fork')).toBe(
			'readarr_fork'
		);
		expect(await probeProduct(page, csrf, stub.baseUrl('unknown-author'), 'unknown-author')).toBe(
			'unknown'
		);

		for (const [name, seg] of [
			[rresName, 'rresurrected'],
			[forkName, 'fork']
		] as const) {
			const createRes = await page.request.post('/api/v1/instances', {
				headers: { 'X-CSRF-Token': csrf },
				data: {
					kind: 'readarr',
					name,
					base_url: stub.baseUrl(seg),
					api_key: 'fake-api-key-0123456789',
					enabled: true
				}
			});
			expect(createRes.ok()).toBeTruthy();
			const id = ((await createRes.json()) as { id: string }).id;
			createdIds.push(id);

			const testRes = await page.request.post(`/api/v1/instances/${id}/test`, {
				headers: { 'X-CSRF-Token': csrf }
			});
			expect(testRes.ok()).toBeTruthy();
		}

		// The stored instance keeps the product the test named.
		const stored = async (id: string) =>
			((await (await page.request.get(`/api/v1/instances/${id}`)).json()) as StoredInstance)
				.last_seen_product;
		expect(await stored(createdIds[0])).toBe('readarr_rresurrected');
		expect(await stored(createdIds[1])).toBe('readarr_fork');

		// The list names a fork next to the kind; the rresurrected name is the same in en and de.
		await page.goto('/instances');
		const rresKind = page.getByRole('row').filter({ hasText: rresName }).getByRole('cell').nth(1);
		await expect(rresKind).toContainText('(rresurrected)');
		const forkKind = page.getByRole('row').filter({ hasText: forkName }).getByRole('cell').nth(1);
		await expect(forkKind).toContainText('·');
		await expect(forkKind).not.toContainText('(rresurrected)');
	} finally {
		for (const id of createdIds) {
			await page.request.delete(`/api/v1/instances/${id}`, {
				headers: { 'X-CSRF-Token': csrf }
			});
		}
		await stub.close();
	}
});
