// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #893
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { expect, test, type Page } from '@playwright/test';

const SOURCE_NAME = 'e2e-favourites-893';

async function loginAndGetCsrf(page: Page): Promise<string> {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok()).toBeTruthy();
	return ((await login.json()) as { csrf_token: string }).csrf_token;
}

async function setAdultModule(
	page: Page,
	csrf: string,
	enabled: boolean,
	dir: string
): Promise<void> {
	const res = await page.request.put('/api/v1/system/modules/adult', {
		headers: { 'X-CSRF-Token': csrf },
		data: { enabled, config: { image_set_dir: dir, sync_interval_minutes: 360 } }
	});
	expect(res.ok()).toBeTruthy();
}

async function storedSyncFavorites(page: Page, id: string): Promise<boolean> {
	const res = await page.request.get(`/api/v1/adult/sources/${id}`);
	expect(res.ok()).toBeTruthy();
	return ((await res.json()) as { sync_favorites: boolean }).sync_favorites;
}

// Toggles the box, waits for the saved status, reloads and checks both the page and the API.
async function toggleAndReload(page: Page, id: string, want: boolean): Promise<void> {
	const box = page.locator(`#adult-source-sync-favorites-${id}`);
	await box.setChecked(want);
	await expect(page.locator(`#adult-source-sync-favorites-${id}-status`)).toHaveAttribute(
		'data-status',
		'saved'
	);
	await page.reload();
	await expect(page.locator(`#adult-source-sync-favorites-${id}`)).toBeChecked({ checked: want });
	expect(await storedSyncFavorites(page, id)).toBe(want);
}

test('893: the synchronize-favourites checkbox on a catalogue source saves on change and survives a reload', async ({
	page
}) => {
	const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'snatcharr-893-'));
	const csrf = await loginAndGetCsrf(page);
	await setAdultModule(page, csrf, true, dir);
	let id = '';
	try {
		// A retry starts clean: a source left by a failed attempt would make the create 409.
		const left = (await (await page.request.get('/api/v1/adult/sources')).json()) as Array<{
			id: string;
			name: string;
		}>;
		for (const src of left.filter((s) => s.name === SOURCE_NAME)) {
			await page.request.delete(`/api/v1/adult/sources/${src.id}`, {
				headers: { 'X-CSRF-Token': csrf }
			});
		}
		// The endpoint is never called: nothing here starts a sync.
		const created = await page.request.post('/api/v1/adult/sources', {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				name: SOURCE_NAME,
				endpoint: 'http://127.0.0.1:1/graphql',
				api_key: 'fake-api-key-0123456789',
				enabled: true,
				sync_favorites: true,
				priority: 30
			}
		});
		expect(created.status()).toBe(201);
		id = ((await created.json()) as { id: string }).id;

		await page.goto('/adult/sources');
		await expect(page.locator(`#adult-source-sync-favorites-${id}`)).toBeChecked();
		// Off, then on again: both directions persist without a separate save.
		await toggleAndReload(page, id, false);
		await toggleAndReload(page, id, true);
	} finally {
		if (id) {
			await page.request.delete(`/api/v1/adult/sources/${id}`, {
				headers: { 'X-CSRF-Token': csrf }
			});
		}
		await setAdultModule(page, csrf, false, dir);
		fs.rmSync(dir, { recursive: true, force: true });
	}
});
