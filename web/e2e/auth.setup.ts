// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import fs from 'node:fs';
import path from 'node:path';
import { expect, test as setup, type Page } from '@playwright/test';

const authDir = path.resolve(import.meta.dirname, '.auth');
const authFile = path.join(authDir, 'admin.json');
const seedFile = path.join(authDir, 'seed.json');

async function seedData(page: Page, csrfToken: string): Promise<string> {
	const csrfHeaders = { 'X-CSRF-Token': csrfToken };
	const fakearrUrl = process.env.SNATCHARR_E2E_FAKEARR;
	if (!fakearrUrl) {
		throw new Error('missing required environment variable: SNATCHARR_E2E_FAKEARR');
	}

	const instRes = await page.request.post('/api/v1/instances', {
		data: {
			kind: 'sonarr',
			name: 'seeded-sonarr',
			base_url: fakearrUrl,
			api_key: 'fake-api-key-0123456789',
			enabled: true
		},
		headers: csrfHeaders
	});
	expect(instRes.ok()).toBeTruthy();
	const inst = (await instRes.json()) as { id: string };

	const dlRes = await page.request.post('/api/v1/download-clients', {
		data: {
			kind: 'transmission',
			name: 'seeded-transmission',
			base_url: 'http://127.0.0.1:9091',
			enabled: true
		},
		headers: csrfHeaders
	});
	expect(dlRes.ok()).toBeTruthy();

	const schedRes = await page.request.post('/api/v1/schedules', {
		data: {
			name: 'seeded-schedule',
			days: [1, 2, 3, 4, 5],
			start: '08:00',
			end: '17:00',
			tz: 'UTC',
			action: 'pause',
			enabled: true
		},
		headers: csrfHeaders
	});
	expect(schedRes.ok()).toBeTruthy();

	return inst.id;
}

setup('complete setup and seed data', async ({ page }) => {
	await page.goto('/setup');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	await page.locator('#username').fill('admin');
	await page.locator('#password').fill('smoke-password-1234');
	await page.locator('button[type="submit"]').click();

	await expect(page).toHaveURL(/.*\/$/);
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

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
