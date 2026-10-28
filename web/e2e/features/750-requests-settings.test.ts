// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #750

import { expect, request, test, type Page } from '@playwright/test';

const proxySecret = process.env.SNATCHARR_E2E_PROXY_SECRET ?? '';
const proxyUserHeader = process.env.SNATCHARR_E2E_PROXY_HEADER ?? 'Remote-User';
const proxyGroupsHeader = process.env.SNATCHARR_E2E_PROXY_GROUPS_HEADER ?? 'X-authentik-groups';
const proxySecretHeader =
	process.env.SNATCHARR_E2E_PROXY_SECRET_HEADER ?? 'X-SnatchArr-Proxy-Secret';

async function getCsrfToken(page: Page): Promise<string> {
	const res = await page.request.get('/api/v1/auth/session');
	expect(res.ok()).toBeTruthy();
	const data = (await res.json()) as { csrf_token: string };
	return data.csrf_token;
}

const defaultSettings = {
	voting_mode: 'upvotes_only',
	allow_viewer_votes: true,
	visibility: 'public_attributed',
	voting_ttl_days: 0
};

test.describe.configure({ mode: 'serial' });

test('positive: get defaults, update settings via API and UI form, persist across reload', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);

	try {
		// 1. GET default values
		const getRes = await page.request.get('/api/v1/requests/settings');
		expect(getRes.status()).toBe(200);
		const initial = await getRes.json();
		expect(initial).toEqual(defaultSettings);

		// 2. PUT changed values via API -> GET returns them
		const changedApi = {
			voting_mode: 'net_score',
			allow_viewer_votes: false,
			visibility: 'public_anonymous',
			voting_ttl_days: 14
		};
		const putRes = await page.request.put('/api/v1/requests/settings', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: changedApi
		});
		expect(putRes.status()).toBe(200);
		expect(await putRes.json()).toEqual(changedApi);

		const getAfterPut = await page.request.get('/api/v1/requests/settings');
		expect(getAfterPut.status()).toBe(200);
		expect(await getAfterPut.json()).toEqual(changedApi);

		// 3. UI page shows the stored values
		await page.goto('/settings/requests');
		const votingModeSelect = page.locator('select[data-field="voting_mode"]');
		const allowViewerCheckbox = page.locator('input[data-field="allow_viewer_votes"]');
		const visibilitySelect = page.locator('select[data-field="visibility"]');
		const ttlInput = page.locator('input[data-field="voting_ttl_days"]');

		await expect(votingModeSelect).toHaveValue('net_score');
		await expect(allowViewerCheckbox).not.toBeChecked();
		await expect(visibilitySelect).toHaveValue('public_anonymous');
		await expect(ttlInput).toHaveValue('14');

		// 4. Saving through the UI form persists
		await votingModeSelect.selectOption('upvotes_only');
		await allowViewerCheckbox.check();
		await visibilitySelect.selectOption('private_to_requester_and_admin');
		await ttlInput.fill('21');

		const saveBtn = page.getByRole('button', { name: 'Save' });
		await saveBtn.click();
		await expect(page.getByText('Request settings saved')).toBeVisible();

		// 5. Reload shows the persisted values
		await page.reload();
		await expect(page.locator('select[data-field="voting_mode"]')).toHaveValue('upvotes_only');
		await expect(page.locator('input[data-field="allow_viewer_votes"]')).toBeChecked();
		await expect(page.locator('select[data-field="visibility"]')).toHaveValue(
			'private_to_requester_and_admin'
		);
		await expect(page.locator('input[data-field="voting_ttl_days"]')).toHaveValue('21');
	} finally {
		// Restore defaults at the end (retry-safe)
		await page.request.put('/api/v1/requests/settings', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: defaultSettings
		});
	}
});

test('negative: invalid voting_mode returns 4xx problem+json, viewer PUT returns 403 problem+json', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);

	try {
		// 1. Invalid voting_mode returns 4xx problem+json
		const badModeRes = await page.request.put('/api/v1/requests/settings', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				voting_mode: 'unsupported_mode',
				allow_viewer_votes: true,
				visibility: 'public_attributed',
				voting_ttl_days: 0
			}
		});
		expect([400, 422]).toContain(badModeRes.status());
		expect(badModeRes.headers()['content-type']).toContain('application/problem+json');

		// 2. Viewer PUT returns 403 problem+json
		const viewerCtx = await request.newContext({ storageState: { cookies: [], origins: [] } });
		try {
			const loginRes = await viewerCtx.post('/api/v1/auth/proxy-session', {
				headers: {
					[proxyUserHeader]: 'e2e-viewer-750',
					[proxyGroupsHeader]: 'e2e-viewers',
					[proxySecretHeader]: proxySecret
				}
			});
			expect(loginRes.status()).toBe(200);
			const loginBody = (await loginRes.json()) as { csrf_token: string };

			const viewerPutRes = await viewerCtx.put('/api/v1/requests/settings', {
				headers: { 'X-CSRF-Token': loginBody.csrf_token },
				data: {
					voting_mode: 'net_score',
					allow_viewer_votes: false,
					visibility: 'public_anonymous',
					voting_ttl_days: 7
				}
			});
			expect(viewerPutRes.status()).toBe(403);
			expect(viewerPutRes.headers()['content-type']).toContain('application/problem+json');
		} finally {
			await viewerCtx.dispose();
		}
	} finally {
		// Restore defaults at the end (retry-safe)
		await page.request.put('/api/v1/requests/settings', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: defaultSettings
		});
	}
});
