// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import fs from 'node:fs';
import http from 'node:http';
import type net from 'node:net';
import path from 'node:path';
import AxeBuilder from '@axe-core/playwright';
import { expect, request, test, type Page } from '@playwright/test';

const WCAG_22_AA = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

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

test('schedules: create, edit, delete through UI and verify axe clean', async ({ page }) => {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok()).toBeTruthy();

	await page.goto('/schedules');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	const axe = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
	expect(axe.violations).toEqual([]);

	// 1. Create schedule
	const scheduleName = 'e2e-quiet-hours';
	await page.locator('#schedule-name').fill(scheduleName);
	await page.locator('#schedule-start').fill('23:00');
	await page.locator('#schedule-end').fill('07:00');
	await page.locator('#schedule-tz').fill('UTC');
	await page.getByRole('button', { name: /add schedule|zeitplan hinzufügen/i }).click();

	await expect(page.getByRole('table')).toContainText(scheduleName);

	const getRes = await page.request.get('/api/v1/schedules');
	expect(getRes.ok()).toBeTruthy();
	const schedules = (await getRes.json()) as Array<{ id: string; name: string }>;
	const created = schedules.find((s) => s.name === scheduleName);
	expect(created).toBeDefined();

	// 2. Edit schedule
	const updatedName = 'e2e-quiet-hours-updated';
	await page
		.getByRole('button', { name: /^(edit|bearbeiten)$/i })
		.first()
		.click();
	await page.locator('#schedule-name').fill(updatedName);
	await page.getByRole('button', { name: /save schedule|zeitplan speichern/i }).click();

	await expect(page.getByRole('table')).toContainText(updatedName);

	const getRes2 = await page.request.get('/api/v1/schedules');
	expect(getRes2.ok()).toBeTruthy();
	const schedules2 = (await getRes2.json()) as Array<{ id: string; name: string }>;
	expect(schedules2.some((s) => s.name === updatedName)).toBeTruthy();

	// 3. Delete schedule with explicit in-page confirmation
	await page
		.getByRole('button', { name: /^(delete|löschen)$/i })
		.first()
		.click();
	await expect(
		page.getByRole('button', { name: /confirm delete|löschen bestätigen/i })
	).toBeVisible();
	await page.getByRole('button', { name: /confirm delete|löschen bestätigen/i }).click();

	await expect(page.locator('main')).not.toContainText(updatedName);

	const getRes3 = await page.request.get('/api/v1/schedules');
	expect(getRes3.ok()).toBeTruthy();
	const schedules3 = (await getRes3.json()) as Array<{ id: string; name: string }>;
	expect(schedules3.some((s) => s.name === updatedName)).toBeFalsy();
});

test('account: create API key, read back with Bearer token, revoke key, check 401 and axe clean', async ({
	page
}) => {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok()).toBeTruthy();

	await page.goto('/account');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	const axeResults = await new AxeBuilder({ page })
		.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
		.analyze();
	expect(axeResults.violations).toEqual([]);

	// 1. Create API key through UI
	const keyName = 'e2e-api-key';
	await page.locator('#key-name').fill(keyName);
	await page.getByRole('button', { name: /create api key|api-schlüssel erstellen/i }).click();

	// Dialog should display secret
	const dialog = page.getByRole('dialog');
	await expect(dialog).toBeVisible();
	const secretInput = dialog.locator('#created-secret-input');
	await expect(secretInput).toBeVisible();
	const secret = await secretInput.inputValue();
	expect(secret).toBeTruthy();

	// The modal dialog holds focus and passes axe; Escape closes it and drops the secret
	expect(await dialog.evaluate((el) => el.contains(document.activeElement))).toBe(true);
	const dialogAxe = await new AxeBuilder({ page })
		.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
		.analyze();
	expect(dialogAxe.violations).toEqual([]);
	await page.keyboard.press('Escape');
	await expect(dialog).not.toBeVisible();
	await expect(page.locator('#created-secret-input')).toHaveCount(0);

	// Key appears in the table
	await expect(page.getByRole('table')).toContainText(keyName);

	// 2. Read back from API using Bearer token per OpenAPI security scheme
	const unauthedRequest = await request.newContext({
		baseURL: 'http://127.0.0.1:4173',
		storageState: { cookies: [], origins: [] }
	});
	const sessionRes = await unauthedRequest.get('/api/v1/auth/session', {
		headers: {
			Authorization: `Bearer ${secret}`
		}
	});
	expect(sessionRes.ok()).toBeTruthy();
	const sessionData = (await sessionRes.json()) as { user?: { username?: string } };
	expect(sessionData.user?.username).toBe('admin');

	// 3. Revoke key through UI with in-page confirmation
	const row = page.getByRole('row').filter({ hasText: keyName });
	await row.getByRole('button', { name: /revoke|widerrufen/i }).click();
	await row.getByRole('button', { name: /revoke key|schlüssel widerrufen/i }).click();

	// Verify key is removed
	await expect(page.getByRole('cell', { name: keyName, exact: true })).not.toBeVisible();

	// 4. Request using the revoked key now gets 401
	const revokedRes = await unauthedRequest.get('/api/v1/auth/session', {
		headers: {
			Authorization: `Bearer ${secret}`
		}
	});
	expect(revokedRes.status()).toBe(401);
});

/** The instance the auth setup seeded; a11y.test.ts reads the same file. */
function seededInstanceId(): string {
	const seedFile = path.resolve(import.meta.dirname, '.auth/seed.json');
	const data = JSON.parse(fs.readFileSync(seedFile, 'utf-8')) as { instanceId?: string };
	if (!data.instanceId) throw new Error(`missing instanceId in seed file ${seedFile}`);
	return data.instanceId;
}

/** C9-04 runs in parallel and logs out, ending the shared session: start one of this test's own. */
async function freshSession(page: Page): Promise<void> {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok()).toBeTruthy();
}

test('instance actions: edit the name, read it back, reset processed memory with confirm', async ({
	page
}) => {
	await freshSession(page);
	const instanceId = seededInstanceId();
	const newName = 'e2e-renamed-sonarr';

	await page.goto(`/instances/${instanceId}`);
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	// 1. Edit the name; the API key stays empty, so the stored key is kept.
	const form = page.getByRole('form', { name: /edit instance|instanz bearbeiten/i });
	await expect(form.locator('#edit-api_key')).toHaveValue('');
	await form.locator('#edit-name').fill(newName);
	await form.getByRole('button', { name: /^(save|speichern)$/i }).click();
	await expect(page.getByRole('heading', { level: 1 })).toHaveText(newName);

	const res = await page.request.get(`/api/v1/instances/${instanceId}`);
	expect(res.ok()).toBeTruthy();
	expect(((await res.json()) as { name: string }).name).toBe(newName);

	// 2. Reset this instance's processed memory: nothing happens before the confirm step.
	const resetLabel = /^(reset processed memory|verarbeitungsgedächtnis zurücksetzen)$/i;
	await page.getByRole('button', { name: resetLabel }).click();
	const ask = page.getByRole('group', { name: resetLabel });
	await expect(ask).toContainText(newName);
	await ask.getByRole('button', { name: /^(confirm|bestätigen)$/i }).click();
	await expect(
		page.getByText(/\d+ (processed items forgotten|verarbeitete Einträge vergessen)/i)
	).toBeVisible();

	// 3. Reset every instance's memory from the instance list, again behind a confirm.
	await page.goto('/instances');
	const resetAll = /reset processed memory of all instances|aller instanzen zurücksetzen/i;
	await page.getByRole('button', { name: resetAll }).click();
	const askAll = page.getByRole('group', { name: resetAll });
	await expect(askAll).toContainText(/every instance|allen instanzen/i);
	await askAll.getByRole('button', { name: /^(confirm|bestätigen)$/i }).click();
	await expect(
		page.getByText(/\d+ (processed items forgotten|verarbeitete Einträge vergessen)/i)
	).toBeVisible();
});

test('download clients: create, edit and delete one through the UI', async ({ page }) => {
	// C9-04 runs in parallel and logs out; start a session of this test's own.
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok()).toBeTruthy();

	const name = 'e2e-qbittorrent';
	const renamed = 'e2e-qbittorrent-renamed';
	const save = page.getByRole('button', { name: /^(save|speichern)$/i });
	const card = (title: string) =>
		page
			.getByRole('listitem')
			.filter({ has: page.getByRole('heading', { level: 2, name: title }) });
	type Stored = { id: string; name: string; username: string; secret?: string };
	async function stored(): Promise<Stored[]> {
		const res = await page.request.get('/api/v1/download-clients');
		expect(res.ok()).toBeTruthy();
		return (await res.json()) as Stored[];
	}

	await page.goto('/download-clients');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	// 1. Create
	await page.locator('#dlclient-kind').selectOption('qbittorrent');
	await page.locator('#dlclient-name').fill(name);
	await page.locator('#dlclient-base-url').fill('http://127.0.0.1:9');
	await page.locator('#dlclient-username').fill('admin');
	await page.locator('#dlclient-secret').fill('e2e-password-1234');
	// Disabled, so the status poll never dials the made-up address.
	await page.getByRole('checkbox', { name: /^(enabled|aktiv)$/i }).uncheck();
	await save.click();
	await expect(card(name)).toBeVisible();
	const created = (await stored()).find((c) => c.name === name);
	expect(created?.username).toBe('admin');
	expect(created && 'secret' in created).toBe(false);

	// 2. Edit: the stored secret is never shown, and an empty field keeps it.
	await card(name)
		.getByRole('button', { name: /^(edit|bearbeiten)$/i })
		.click();
	await expect(page.locator('#dlclient-name')).toHaveValue(name);
	await expect(page.locator('#dlclient-secret')).toHaveValue('');
	await page.locator('#dlclient-name').fill(renamed);
	await save.click();
	await expect(card(renamed)).toBeVisible();
	expect((await stored()).find((c) => c.id === created?.id)?.name).toBe(renamed);

	// 3. Delete, through the in-page confirmation; the open question is axe-clean too.
	await card(renamed)
		.getByRole('button', { name: /^(delete|löschen)$/i })
		.click();
	await expect(card(renamed).getByRole('group')).toBeVisible();
	const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
	expect(results.violations).toEqual([]);
	await card(renamed)
		.getByRole('button', { name: /yes, delete|ja, löschen/i })
		.click();
	await expect(card(renamed)).toHaveCount(0);
	expect((await stored()).some((c) => c.id === created?.id)).toBe(false);
});
