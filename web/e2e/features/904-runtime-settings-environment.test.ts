// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #904 #907 #908

import fs from 'node:fs';
import path from 'node:path';
import { expect, test, type Page } from '@playwright/test';

interface RuntimeSetting {
	key: string;
	env: string;
	class: 'bootstrap' | 'runtime' | 'secret';
	type: string;
	default: string;
	value?: string;
	is_set?: boolean;
	source: 'env' | 'db' | 'default';
	locked: boolean;
	apply: 'restart' | 'live';
}

test.describe.configure({ mode: 'serial' });

async function loginAndGetCsrf(page: Page): Promise<string> {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok()).toBeTruthy();
	return ((await login.json()) as { csrf_token: string }).csrf_token;
}

async function fetchRuntimeSettings(page: Page): Promise<RuntimeSetting[]> {
	const res = await page.request.get('/api/v1/settings/runtime');
	expect(res.status()).toBe(200);
	return (await res.json()) as RuntimeSetting[];
}

test('904/907: runtime settings registry matches docs/settings-parity.tsv and API rejects locked, bootstrap, and invalid overrides', async ({
	page
}) => {
	const csrf = await loginAndGetCsrf(page);
	const settings = await fetchRuntimeSettings(page);

	const parityPath = path.resolve(import.meta.dirname, '../../../docs/settings-parity.tsv');
	const parityLines = fs.readFileSync(parityPath, 'utf-8').trim().split('\n').slice(1);
	const actualLines = settings.map((s) => `${s.env}\t${s.key}\t${s.class}\t${s.apply}\tyes`);
	expect(actualLines).toEqual(parityLines);

	const apiKeySecret = settings.find((s) => s.key === 'snatcharr.apikey.secret');
	expect(apiKeySecret?.class).toBe('secret');
	expect(apiKeySecret?.locked).toBe(true);

	// Env-locked runtime setting -> 409 Conflict
	const lockedRes = await page.request.put('/api/v1/settings/runtime', {
		headers: { 'X-CSRF-Token': csrf },
		data: { values: { 'snatcharr.session.secure': 'true' } }
	});
	expect(lockedRes.status()).toBe(409);
	expect(lockedRes.headers()['content-type']).toContain('application/problem+json');

	// Bootstrap setting -> 422 Unprocessable Entity
	const bootstrapRes = await page.request.put('/api/v1/settings/runtime', {
		headers: { 'X-CSRF-Token': csrf },
		data: { values: { 'snatcharr.web.dev': 'true' } }
	});
	expect(bootstrapRes.status()).toBe(422);
	expect(bootstrapRes.headers()['content-type']).toContain('application/problem+json');

	// Environment-only secret -> 422 Unprocessable Entity
	const envSecretRes = await page.request.put('/api/v1/settings/runtime', {
		headers: { 'X-CSRF-Token': csrf },
		data: { values: { 'snatcharr.apikey.secret': 'x'.repeat(32) } }
	});
	expect(envSecretRes.status()).toBe(422);
	expect(envSecretRes.headers()['content-type']).toContain('application/problem+json');

	// Unknown setting key -> 422 Unprocessable Entity
	const unknownRes = await page.request.put('/api/v1/settings/runtime', {
		headers: { 'X-CSRF-Token': csrf },
		data: { values: { 'snatcharr.nonexistent.key': '1' } }
	});
	expect(unknownRes.status()).toBe(422);
	expect(unknownRes.headers()['content-type']).toContain('application/problem+json');
});

test('908: Environment tab groups settings by class, locks bootstrap, env-set and env-only secret fields and leaves runtime and stored secret fields editable', async ({
	page
}) => {
	await loginAndGetCsrf(page);
	const settings = await fetchRuntimeSettings(page);
	const lockedKeys = settings.filter((s) => s.class === 'runtime' && s.locked);
	const freeRuntime = settings.find((s) => s.class === 'runtime' && !s.locked);
	const freeSecret = settings.find((s) => s.class === 'secret' && !s.locked);
	expect(lockedKeys.length, 'a runtime setting locked by the environment').toBeGreaterThan(0);
	expect(freeRuntime, 'an unlocked runtime setting').toBeDefined();
	expect(freeSecret, 'an unlocked secret setting').toBeDefined();

	await page.goto('/system?tab=environment');
	await expect(page.locator('#environment-main-title')).toBeVisible();
	for (const cls of ['bootstrap', 'runtime', 'secret']) {
		await expect(page.locator(`#class-${cls}-heading`)).toBeVisible();
	}
	// One input per registered setting, none of them for a key the registry does not know.
	await expect(page.locator('input[id^="setting-"]')).toHaveCount(settings.length);

	await expect(page.locator(`input[id="setting-${lockedKeys[0].key}"]`)).toBeDisabled();
	await expect(page.locator('input[id="setting-snatcharr.web.dev"]')).toBeDisabled();
	await expect(page.locator('input[id="setting-snatcharr.apikey.secret"]')).toBeDisabled();
	await expect(page.locator(`input[id="setting-${freeRuntime?.key}"]`)).toBeEnabled();
	await expect(page.locator(`input[id="setting-${freeSecret?.key}"]`)).toBeEnabled();
	// A stored secret is never sent back to the page.
	await expect(page.locator(`input[id="setting-${freeSecret?.key}"]`)).toHaveValue('');
});

test('907/908: Environment tab saves runtime and secret overrides in DB, persists across reload, and resets to default', async ({
	page
}) => {
	await loginAndGetCsrf(page);
	await page.goto('/system?tab=environment');
	await expect(page.locator('#environment-main-title')).toBeVisible();

	const customUrl = 'http://127.0.0.1:4173/custom-public-url';
	const urlInput = page.locator('input[id="setting-snatcharr.public.url"]');
	await expect(urlInput).toBeEnabled();
	await urlInput.fill(customUrl);

	await page.locator('form button[type="submit"]').click();
	await expect(page.locator('form [role="status"]')).toBeVisible();

	await page.reload();
	await expect(page.locator('input[id="setting-snatcharr.public.url"]')).toHaveValue(customUrl);

	const urlRow = (await fetchRuntimeSettings(page)).find((s) => s.key === 'snatcharr.public.url');
	expect(urlRow?.source).toBe('db');

	const emptied = page.locator('input[id="setting-snatcharr.public.url"]');
	await emptied.fill('');
	await page.locator('form button[type="submit"]').click();
	await expect(page.locator('form [role="status"]')).toBeVisible();
	await expect
		.poll(async () => {
			const rows = await fetchRuntimeSettings(page);
			return rows.find((s) => s.key === 'snatcharr.public.url')?.source;
		})
		.toBe('default');
});
