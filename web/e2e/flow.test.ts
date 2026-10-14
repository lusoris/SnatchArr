// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

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
