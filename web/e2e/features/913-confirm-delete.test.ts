// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #913

import { expect, test, type Locator, type Page } from '@playwright/test';
import { VIEWPORTS } from '../layout.js';

// The feature run sets these; a missing one is a broken harness, not a reason to guess.
function requireEnv(name: string): string {
	const value = process.env[name];
	if (!value) {
		throw new Error(`${name} unset: run through tools/e2e/web-e2e-api.sh`);
	}
	return value;
}

async function loginAndGetCsrf(page: Page): Promise<string> {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok()).toBeTruthy();
	return ((await login.json()) as { csrf_token: string }).csrf_token;
}

async function createInstance(
	page: Page,
	csrf: string,
	name: string,
	fakearrUrl: string
): Promise<string> {
	const res = await page.request.post('/api/v1/instances', {
		headers: { 'X-CSRF-Token': csrf },
		data: {
			kind: 'sonarr',
			name,
			base_url: `${fakearrUrl}/${name}`,
			api_key: 'fake-api-key-0123456789',
			enabled: true
		}
	});
	expect(res.status()).toBe(201);
	return ((await res.json()) as { id: string }).id;
}

async function cancelThenConfirm(
	container: Locator,
	deleteBtn: Locator,
	name: string,
	verifyAlive: () => Promise<void>
): Promise<void> {
	const group = container.getByRole('group');
	await expect(group).toHaveCount(0);

	await deleteBtn.click();
	await expect(group).toBeVisible();
	await expect(group).toContainText(name);
	await verifyAlive();

	await group.locator('button:not(.btn-primary)').click();
	await expect(group).toHaveCount(0);
	await verifyAlive();

	await deleteBtn.click();
	await expect(group).toBeVisible();
	await group.locator('button.btn-primary').click();
	await expect(container).toHaveCount(0);
}

test('913: instance table row and phone card ask for confirmation before deleting and keep the row on cancel', async ({
	page
}) => {
	const fakearrUrl = requireEnv('SNATCHARR_E2E_FAKEARR');
	const suffix = crypto.randomUUID().slice(0, 8);
	const rowName = `e2e-913-row-${suffix}`;
	const cardName = `e2e-913-card-${suffix}`;
	const csrf = await loginAndGetCsrf(page);
	let rowId = '';
	let cardId = '';

	try {
		rowId = await createInstance(page, csrf, rowName, fakearrUrl);
		cardId = await createInstance(page, csrf, cardName, fakearrUrl);

		await page.setViewportSize(VIEWPORTS.desktop);
		await page.goto('/instances');
		const row = page.locator('tbody tr').filter({ hasText: rowName });
		await expect(row).toBeVisible();

		await cancelThenConfirm(row, row.getByRole('button'), rowName, async () => {
			const check = await page.request.get(`/api/v1/instances/${rowId}`);
			expect(check.status()).toBe(200);
		});
		expect((await page.request.get(`/api/v1/instances/${rowId}`)).status()).toBe(404);
		rowId = '';

		await page.setViewportSize(VIEWPORTS.phone);
		const card = page.locator('li.card.target').filter({ hasText: cardName });
		await expect(card).toBeVisible();

		await cancelThenConfirm(card, card.getByRole('button'), cardName, async () => {
			const check = await page.request.get(`/api/v1/instances/${cardId}`);
			expect(check.status()).toBe(200);
		});
		expect((await page.request.get(`/api/v1/instances/${cardId}`)).status()).toBe(404);
		cardId = '';
	} finally {
		for (const id of [rowId, cardId].filter(Boolean)) {
			await page.request.delete(`/api/v1/instances/${id}`, {
				headers: { 'X-CSRF-Token': csrf }
			});
		}
	}
});

test('913: notification channel and rule tables ask for confirmation before deleting and keep records on cancel', async ({
	page
}) => {
	const suffix = crypto.randomUUID().slice(0, 8);
	const channelName = `e2e-913-chan-${suffix}`;
	const ruleName = `e2e-913-rule-${suffix}`;
	const csrf = await loginAndGetCsrf(page);
	let channelId = '';
	let ruleId = '';

	try {
		const chanRes = await page.request.post('/api/v1/notifications/channels', {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				name: channelName,
				kind: 'webhook',
				enabled: true,
				config: {
					url: 'http://127.0.0.1:9/webhook',
					allow_private_network: true,
					allow_insecure_http: true
				}
			}
		});
		expect(chanRes.status()).toBe(201);
		channelId = ((await chanRes.json()) as { id: string }).id;

		const ruleRes = await page.request.post('/api/v1/notifications/rules', {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				name: ruleName,
				event_kind: 'run_failure_streak',
				threshold: 2,
				enabled: true,
				channel_ids: [channelId]
			}
		});
		expect(ruleRes.status()).toBe(201);
		ruleId = ((await ruleRes.json()) as { id: string }).id;

		await page.goto('/system?tab=notifications');
		const ruleRow = page.locator('tbody tr').filter({ hasText: ruleName });
		await expect(ruleRow).toBeVisible();

		await cancelThenConfirm(ruleRow, ruleRow.getByRole('button'), ruleName, async () => {
			const check = await page.request.get(`/api/v1/notifications/rules/${ruleId}`);
			expect(check.status()).toBe(200);
		});
		expect((await page.request.get(`/api/v1/notifications/rules/${ruleId}`)).status()).toBe(404);
		ruleId = '';

		const chanRow = page.locator('tbody tr').filter({ hasText: channelName });
		await expect(chanRow).toBeVisible();
		await cancelThenConfirm(chanRow, chanRow.getByRole('button').nth(1), channelName, async () => {
			const list = (await (
				await page.request.get('/api/v1/notifications/channels')
			).json()) as Array<{ id: string }>;
			expect(list.some((c) => c.id === channelId)).toBe(true);
		});
		const afterList = (await (
			await page.request.get('/api/v1/notifications/channels')
		).json()) as Array<{ id: string }>;
		expect(afterList.some((c) => c.id === channelId)).toBe(false);
		channelId = '';
	} finally {
		if (ruleId) {
			await page.request.delete(`/api/v1/notifications/rules/${ruleId}`, {
				headers: { 'X-CSRF-Token': csrf }
			});
		}
		if (channelId) {
			await page.request.delete(`/api/v1/notifications/channels/${channelId}`, {
				headers: { 'X-CSRF-Token': csrf }
			});
		}
	}
});
