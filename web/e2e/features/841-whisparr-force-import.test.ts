// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #841

import { expect, request, test, type Page } from '@playwright/test';

const whisparrUrl = process.env.SNATCHARR_E2E_FAKEARR_WHISPARR ?? 'http://127.0.0.1:18991';
const proxySecret = process.env.SNATCHARR_E2E_PROXY_SECRET ?? '';
const proxyUserHeader = process.env.SNATCHARR_E2E_PROXY_HEADER ?? 'Remote-User';
const proxyGroupsHeader = process.env.SNATCHARR_E2E_PROXY_GROUPS_HEADER ?? 'X-authentik-groups';
const proxySecretHeader =
	process.env.SNATCHARR_E2E_PROXY_SECRET_HEADER ?? 'X-SnatchArr-Proxy-Secret';

interface FakeCommandFile {
	movieId?: number;
	indexerFlags?: number;
}

interface FakeCommand {
	name: string;
	body?: {
		files?: FakeCommandFile[];
	};
}

async function getCsrfToken(page: Page): Promise<string> {
	const res = await page.request.get('/api/v1/auth/session');
	expect(res.ok()).toBeTruthy();
	const data = (await res.json()) as { csrf_token: string };
	return data.csrf_token;
}

async function createWhisparrInstance(
	page: Page,
	csrfToken: string,
	name: string
): Promise<{ id: string }> {
	// Base URLs are unique: drop an instance a failed attempt left on the fake Whisparr.
	const listRes = await page.request.get('/api/v1/instances');
	expect(listRes.ok()).toBeTruthy();
	for (const left of (await listRes.json()) as Array<{ id: string; base_url: string }>) {
		if (left.base_url === whisparrUrl) {
			await deleteInstance(page, csrfToken, left.id);
		}
	}
	const res = await page.request.post('/api/v1/instances', {
		headers: { 'X-CSRF-Token': csrfToken },
		data: {
			name,
			kind: 'whisparr_v3',
			base_url: whisparrUrl,
			api_key: 'fake-api-key-0123456789',
			enabled: true
		}
	});
	expect(res.status()).toBe(201);
	return (await res.json()) as { id: string };
}

async function deleteInstance(page: Page, csrfToken: string, id: string): Promise<void> {
	const res = await page.request.delete(`/api/v1/instances/${id}`, {
		headers: { 'X-CSRF-Token': csrfToken }
	});
	expect([204, 404]).toContain(res.status());
}

async function enableImportFixablePolicy(
	page: Page,
	csrfToken: string,
	instanceId: string
): Promise<void> {
	const getRes = await page.request.get(`/api/v1/instances/${instanceId}/queue-policy`);
	expect(getRes.status()).toBe(200);
	const policy = (await getRes.json()) as {
		enabled: boolean;
		grace_seconds: number;
		rules: Record<string, { mode: string; threshold: number; blocklist?: boolean }>;
	};
	policy.enabled = true;
	policy.grace_seconds = 0;
	policy.rules.import_fixable = { mode: 'act', threshold: 1 };

	const putRes = await page.request.put(`/api/v1/instances/${instanceId}/queue-policy`, {
		headers: { 'X-CSRF-Token': csrfToken },
		data: policy
	});
	expect(putRes.status()).toBe(200);
}

async function fetchManualImportCommands(page: Page): Promise<FakeCommand[]> {
	const res = await page.request.get(`${whisparrUrl}/_fake/commands`, {
		headers: { 'X-Api-Key': 'fake-api-key-0123456789' }
	});
	expect(res.status()).toBe(200);
	const cmds = ((await res.json()) as FakeCommand[] | null) ?? [];
	return cmds.filter((c) => c.name === 'ManualImport');
}

// Every test registers the one fake Whisparr, and base URLs are unique per instance.
test.describe.configure({ mode: 'serial' });

test('positive and boundary: sweep imports whisparr_v3 blocked item with movieId and indexerFlags 0, second sweep does not re-import', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);
	const inst = await createWhisparrInstance(page, csrfToken, 'e2e-whisparr-841');

	try {
		await enableImportFixablePolicy(page, csrfToken, inst.id);
		const before = (await fetchManualImportCommands(page)).length;

		const sweepRes = await page.request.post(`/api/v1/instances/${inst.id}/queue/sweep`, {
			headers: { 'X-CSRF-Token': csrfToken }
		});
		expect(sweepRes.status()).toBe(204);

		const imports = await fetchManualImportCommands(page);
		expect(imports.length).toBe(before + 1);
		const file = imports.at(-1)?.body?.files?.[0];
		expect(file?.movieId).toBe(10);
		expect(file?.indexerFlags).toBe(0);

		// Boundary: second sweep after import is 204 and does not dispatch a second ManualImport.
		const secondRes = await page.request.post(`/api/v1/instances/${inst.id}/queue/sweep`, {
			headers: { 'X-CSRF-Token': csrfToken }
		});
		expect(secondRes.status()).toBe(204);

		const importsAfter = await fetchManualImportCommands(page);
		expect(importsAfter.length).toBe(before + 1);
	} finally {
		await deleteInstance(page, csrfToken, inst.id);
	}
});

test('negative: viewer POST queue/sweep returns 403 problem+json and unknown instance returns 404 problem+json', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);
	const inst = await createWhisparrInstance(page, csrfToken, 'e2e-whisparr-neg-841');

	try {
		const unknownRes = await page.request.post(
			'/api/v1/instances/00000000-0000-0000-0000-000000000000/queue/sweep',
			{ headers: { 'X-CSRF-Token': csrfToken } }
		);
		expect(unknownRes.status()).toBe(404);
		expect(unknownRes.headers()['content-type']).toContain('application/problem+json');

		const viewerCtx = await request.newContext({ storageState: { cookies: [], origins: [] } });
		try {
			const loginRes = await viewerCtx.post('/api/v1/auth/proxy-session', {
				headers: {
					[proxyUserHeader]: 'e2e-viewer-841',
					[proxyGroupsHeader]: 'e2e-viewers',
					[proxySecretHeader]: proxySecret
				}
			});
			expect(loginRes.status()).toBe(200);
			const loginBody = (await loginRes.json()) as { csrf_token: string };

			const viewerSweep = await viewerCtx.post(`/api/v1/instances/${inst.id}/queue/sweep`, {
				headers: { 'X-CSRF-Token': loginBody.csrf_token }
			});
			expect(viewerSweep.status()).toBe(403);
			expect(viewerSweep.headers()['content-type']).toContain('application/problem+json');
		} finally {
			await viewerCtx.dispose();
		}
	} finally {
		await deleteInstance(page, csrfToken, inst.id);
	}
});

test('ui: admin clicks Sweep now on instance queue tab and sees success toast', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);
	const inst = await createWhisparrInstance(page, csrfToken, 'e2e-whisparr-ui-841');

	try {
		await page.goto(`/instances/${inst.id}/queue`);
		const sweepBtn = page.getByRole('button', { name: 'Sweep now' });
		await expect(sweepBtn).toBeVisible();
		await sweepBtn.click();
		await expect(page.getByText('Queue sweep completed')).toBeVisible();
	} finally {
		await deleteInstance(page, csrfToken, inst.id);
	}
});
