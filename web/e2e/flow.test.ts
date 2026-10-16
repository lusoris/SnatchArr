// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import http from 'node:http';
import type net from 'node:net';
import { expect, test } from '@playwright/test';

test('C9-04: create instance through UI, read back from API, logout and login', async ({
	page
}) => {
	const fakearrFlowUrl = process.env.SNATCHARR_E2E_FAKEARR_FLOW;
	if (!fakearrFlowUrl) {
		throw new Error('missing required environment variable: SNATCHARR_E2E_FAKEARR_FLOW');
	}
	const instanceName = 'c904-sonarr';

	await page.goto('/instances');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	await page.locator('#name').fill(instanceName);
	await page.locator('#base_url').fill(fakearrFlowUrl);
	await page.locator('#api_key').fill('fake-api-key-0123456789');
	await page.locator('button[type="submit"]').click();

	await expect(page.getByRole('table')).toContainText(instanceName);

	const res = await page.request.get('/api/v1/instances');
	expect(res.ok()).toBeTruthy();
	const instances = (await res.json()) as Array<{ name: string; base_url: string }>;
	const created = instances.find((i) => i.name === instanceName);
	expect(created).toBeDefined();
	expect(created?.name).toBe(instanceName);
	expect(created?.base_url).toBe(fakearrFlowUrl);

	await page.getByRole('button', { name: /log out|abmelden/i }).click();
	await expect(page).toHaveURL(/.*\/login/);
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	await page.locator('#username').fill('admin');
	await page.locator('#password').fill('smoke-password-1234');
	await page.locator('button[type="submit"]').click();

	await expect(page).toHaveURL(/.*\/$/);
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
});

test('notifications: create channel, test delivery, create rule, read back from API', async ({
	page
}) => {
	const server = http.createServer((_req, res) => {
		res.writeHead(200, { 'Content-Type': 'application/json' });
		res.end('{}');
	});
	await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', () => resolve()));
	const port = (server.address() as net.AddressInfo).port;
	const webhookUrl = `http://127.0.0.1:${port}/webhook`;

	try {
		// C9-04 runs in parallel and logs out, which ends the session this project shares
		// through storageState; drop that cookie and start a session of this test's own.
		await page.context().clearCookies();
		const login = await page.request.post('/api/v1/auth/login', {
			data: { username: 'admin', password: 'smoke-password-1234' }
		});
		expect(login.ok()).toBeTruthy();

		await page.goto('/settings');
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		await expect(
			page.getByRole('heading', { level: 2, name: /notifications|benachrichtigungen/i })
		).toBeVisible();

		// 1. Create channel
		const channelName = 'e2e-alert-webhook';
		await page.locator('#channel-name').fill(channelName);
		await page.locator('#channel-kind').selectOption('webhook');
		await page.locator('#channel-url').fill(webhookUrl);
		await page.getByLabel(/allow private networks|private netzwerke erlauben/i).check();
		await page.getByLabel(/allow insecure http|unsicheres http erlauben/i).check();
		await page.getByRole('button', { name: /add channel|kanal hinzufügen/i }).click();

		await expect(page.getByRole('table').first()).toContainText(channelName);

		// 2. Test channel
		await page
			.getByRole('button', { name: /test|testen/i })
			.first()
			.click();
		await expect(
			page.getByText(
				/test notification delivered successfully|test-benachrichtigung erfolgreich zugestellt/i
			)
		).toBeVisible();

		// 3. Create rule
		const ruleName = 'e2e-streak-rule';
		await page.locator('#rule-name').fill(ruleName);
		await page.locator('#rule-event').selectOption('run_failure_streak');
		await page.locator('#rule-threshold').fill('2');
		await page.getByLabel(new RegExp(channelName)).check();
		await page.getByRole('button', { name: /add rule|regel hinzufügen/i }).click();

		await expect(page.getByRole('table').nth(1)).toContainText(ruleName);

		// 4. Read back from API
		const chanRes = await page.request.get('/api/v1/notifications/channels');
		expect(chanRes.ok()).toBeTruthy();
		const channels = (await chanRes.json()) as Array<{ name: string; kind: string }>;
		expect(channels.some((c) => c.name === channelName)).toBeTruthy();

		const ruleRes = await page.request.get('/api/v1/notifications/rules');
		expect(ruleRes.ok()).toBeTruthy();
		const rules = (await ruleRes.json()) as Array<{ name: string; event_kind: string }>;
		expect(rules.some((r) => r.name === ruleName)).toBeTruthy();
	} finally {
		await new Promise<void>((resolve) => server.close(() => resolve()));
	}
});
