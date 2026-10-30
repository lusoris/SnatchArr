// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import fs from 'node:fs';
import path from 'node:path';
import { expect, test as setup, type Page } from '@playwright/test';
import { submitAdminLogin } from './mocks.js';

const authDir = path.resolve(import.meta.dirname, '.auth');
const authFile = path.join(authDir, 'admin.json');
const seedFile = path.join(authDir, 'seed.json');

async function postSeed(
	page: Page,
	csrfToken: string,
	url: string,
	data: Record<string, unknown>
): Promise<{ id: string }> {
	const res = await page.request.post(url, { data, headers: { 'X-CSRF-Token': csrfToken } });
	expect(res.ok(), `seed POST ${url}: status ${res.status()}`).toBeTruthy();
	return (await res.json()) as { id: string };
}

async function seedData(page: Page, csrfToken: string): Promise<string> {
	const fakearrUrl = process.env.SNATCHARR_E2E_FAKEARR;
	if (!fakearrUrl) {
		throw new Error('missing required environment variable: SNATCHARR_E2E_FAKEARR');
	}
	const inst = await postSeed(page, csrfToken, '/api/v1/instances', {
		kind: 'sonarr',
		name: 'seeded-sonarr',
		base_url: fakearrUrl,
		api_key: 'fake-api-key-0123456789',
		enabled: true
	});
	await postSeed(page, csrfToken, '/api/v1/download-clients', {
		kind: 'transmission',
		name: 'seeded-transmission',
		base_url: 'http://127.0.0.1:9091',
		enabled: true
	});
	await postSeed(page, csrfToken, '/api/v1/schedules', {
		name: 'seeded-schedule',
		days: [1, 2, 3, 4, 5],
		start: '08:00',
		end: '17:00',
		tz: 'UTC',
		action: 'pause',
		enabled: true
	});
	await postSeed(page, csrfToken, '/api/v1/auth/api-keys', { name: 'seeded-key' });
	const chan = await postSeed(page, csrfToken, '/api/v1/notifications/channels', {
		name: 'seeded-webhook-channel',
		kind: 'webhook',
		config: {
			url: 'http://127.0.0.1:9091/webhook',
			allow_private_networks: true,
			allow_insecure_http: true
		},
		enabled: true
	});
	await postSeed(page, csrfToken, '/api/v1/notifications/rules', {
		name: 'seeded-rule',
		event_kind: 'run_failure_streak',
		threshold: 2,
		channel_ids: [chan.id]
	});
	return inst.id;
}

setup('complete setup and seed data', async ({ page }) => {
	await page.goto('/setup');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await submitAdminLogin(page);

	const sessionRes = await page.request.get('/api/v1/auth/session');
	if (!sessionRes.ok()) {
		const txt = await sessionRes.text();
		throw new Error(`get /auth/session status ${sessionRes.status()}: ${txt}`);
	}
	const sessionData = (await sessionRes.json()) as { csrf_token: string };

	const instanceId = await seedData(page, sessionData.csrf_token);

	await fs.promises.mkdir(authDir, { recursive: true });
	await page.context().storageState({ path: authFile });
	await fs.promises.writeFile(seedFile, JSON.stringify({ instanceId }), 'utf-8');
});
