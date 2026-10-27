// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #836

import http from 'node:http';
import type net from 'node:net';
import { expect, test, type Page } from '@playwright/test';

async function getCsrfToken(page: Page): Promise<string> {
	const res = await page.request.get('/api/v1/auth/session');
	expect(res.ok()).toBeTruthy();
	const data = (await res.json()) as { csrf_token: string };
	return data.csrf_token;
}

async function startDummyStub(): Promise<{ url: string; close: () => Promise<void> }> {
	const server = http.createServer((_req, res) => {
		res.writeHead(200, { 'Content-Type': 'application/json' });
		res.end(JSON.stringify({ version: '1.0.0' }));
	});
	await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', () => resolve()));
	const port = (server.address() as net.AddressInfo).port;
	return {
		url: `http://127.0.0.1:${port}`,
		close: () =>
			new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())))
	};
}

test('positive: instances list renders product display names (Sonarr, Whisparr v3, Readarr (legacy), Stash)', async ({
	page
}) => {
	const stub = await startDummyStub();
	const csrfToken = await getCsrfToken(page);

	const createdIds: string[] = [];
	const testInstances = [
		{ kind: 'sonarr', name: 'e2e-836-sonarr', expected: 'Sonarr' },
		{ kind: 'whisparr_v3', name: 'e2e-836-whisparr-v3', expected: 'Whisparr v3' },
		{ kind: 'readarr', name: 'e2e-836-readarr', expected: 'Readarr (legacy)' },
		{ kind: 'stash', name: 'e2e-836-stash', expected: 'Stash' }
	] as const;

	try {
		for (const item of testInstances) {
			const res = await page.request.post('/api/v1/instances', {
				headers: { 'X-CSRF-Token': csrfToken },
				data: {
					kind: item.kind,
					name: item.name,
					// Base URLs are unique per instance; one stub serves every path.
					base_url: `${stub.url}/${item.name}`,
					api_key: 'fake-api-key-0123456789',
					enabled: true
				}
			});
			expect(res.status(), `create ${item.kind}`).toBe(201);
			const created = (await res.json()) as { id: string };
			createdIds.push(created.id);
		}

		await page.goto('/instances');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

		for (const item of testInstances) {
			const row = page.getByRole('row').filter({ hasText: item.name });
			await expect(row).toBeVisible();
			// In desktop table, column 1 is instance kind
			await expect(row.getByRole('cell').nth(1)).toHaveText(item.expected);
		}
	} finally {
		for (const id of createdIds) {
			await page.request.delete(`/api/v1/instances/${id}`, {
				headers: { 'X-CSRF-Token': csrfToken }
			});
		}
		await stub.close();
	}
});

test('negative: instances list does not render raw API ids for formatted or capitalized kinds', async ({
	page
}) => {
	const stub = await startDummyStub();
	const csrfToken = await getCsrfToken(page);

	const createdIds: string[] = [];
	const testInstances = [
		{ kind: 'sonarr', name: 'e2e-836-neg-sonarr', rawId: 'sonarr' },
		{ kind: 'whisparr_v3', name: 'e2e-836-neg-whisparr', rawId: 'whisparr_v3' },
		{ kind: 'readarr', name: 'e2e-836-neg-readarr', rawId: 'readarr (legacy)' }
	] as const;

	try {
		for (const item of testInstances) {
			const res = await page.request.post('/api/v1/instances', {
				headers: { 'X-CSRF-Token': csrfToken },
				data: {
					kind: item.kind,
					name: item.name,
					base_url: `${stub.url}/${item.name}`,
					api_key: 'fake-api-key-0123456789',
					enabled: true
				}
			});
			expect(res.status()).toBe(201);
			const created = (await res.json()) as { id: string };
			createdIds.push(created.id);
		}

		await page.goto('/instances');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

		for (const item of testInstances) {
			const row = page.getByRole('row').filter({ hasText: item.name });
			await expect(row).toBeVisible();
			// Assert row does not retain the raw unformatted/lowercase identifier in the kind cell
			await expect(row.getByRole('cell').nth(1)).not.toHaveText(item.rawId);
		}
	} finally {
		for (const id of createdIds) {
			await page.request.delete(`/api/v1/instances/${id}`, {
				headers: { 'X-CSRF-Token': csrfToken }
			});
		}
		await stub.close();
	}
});

test('boundary: legacy qualification presence and non-arr display name boundaries', async ({
	page
}) => {
	const stub = await startDummyStub();
	const csrfToken = await getCsrfToken(page);

	const createdIds: string[] = [];
	const instancesToTest = [
		{ kind: 'readarr', name: 'e2e-836-bound-readarr', isLegacy: true },
		{ kind: 'sonarr', name: 'e2e-836-bound-sonarr', isLegacy: false },
		{ kind: 'stash', name: 'e2e-836-bound-stash', isLegacy: false }
	] as const;

	try {
		for (const inst of instancesToTest) {
			const res = await page.request.post('/api/v1/instances', {
				headers: { 'X-CSRF-Token': csrfToken },
				data: {
					kind: inst.kind,
					name: inst.name,
					base_url: `${stub.url}/${inst.name}`,
					api_key: 'fake-api-key-0123456789',
					enabled: true
				}
			});
			expect(res.status()).toBe(201);
			const created = (await res.json()) as { id: string };
			createdIds.push(created.id);
		}

		await page.goto('/instances');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

		// Readarr is the sole legacy kind: boundary check for (legacy) label qualification
		const readarrRow = page.getByRole('row').filter({ hasText: 'e2e-836-bound-readarr' });
		await expect(readarrRow.getByRole('cell').nth(1)).toContainText('(legacy)');

		// Sonarr and Stash are active non-legacy kinds: must not contain (legacy)
		const sonarrRow = page.getByRole('row').filter({ hasText: 'e2e-836-bound-sonarr' });
		await expect(sonarrRow.getByRole('cell').nth(1)).not.toContainText('(legacy)');

		const stashRow = page.getByRole('row').filter({ hasText: 'e2e-836-bound-stash' });
		await expect(stashRow.getByRole('cell').nth(1)).not.toContainText('(legacy)');
		await expect(stashRow.getByRole('cell').nth(1)).toHaveText('Stash');
	} finally {
		for (const id of createdIds) {
			await page.request.delete(`/api/v1/instances/${id}`, {
				headers: { 'X-CSRF-Token': csrfToken }
			});
		}
		await stub.close();
	}
});
