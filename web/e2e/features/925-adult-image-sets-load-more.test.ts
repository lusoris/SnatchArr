// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #925

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { expect, test, type Page } from '@playwright/test';

const png1x1Base64 =
	'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==';
const pngBytes = Buffer.from(png1x1Base64, 'base64');

async function loginAndGetCsrf(page: Page): Promise<string> {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok()).toBeTruthy();
	const sess = (await login.json()) as { csrf_token: string };
	return sess.csrf_token;
}

async function setAdultModuleState(
	page: Page,
	csrf: string,
	enabled: boolean,
	imageSetDir: string
): Promise<void> {
	const res = await page.request.put('/api/v1/system/modules/adult', {
		headers: { 'X-CSRF-Token': csrf },
		data: {
			enabled,
			config: { image_set_dir: imageSetDir, sync_interval_minutes: 360 }
		}
	});
	expect(res.ok()).toBeTruthy();
}

// A rescan drops the sets whose directory vanished; the index keeps one set so the scan accepts it.
async function dropAllButFirstSet(page: Page, csrf: string, dir: string): Promise<void> {
	for (const entry of fs.readdirSync(dir)) {
		if (entry !== 'set-001') {
			fs.rmSync(path.join(dir, entry), { recursive: true, force: true });
		}
	}
	const res = await page.request.post('/api/v1/adult/scan', { headers: { 'X-CSRF-Token': csrf } });
	expect(res.ok()).toBeTruthy();
}

test('925: adult image sets page loads 50 sets initially and appends remaining sets via Load more button', async ({
	page
}) => {
	const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'snatcharr-925-'));
	let csrf = '';

	try {
		for (let i = 1; i <= 52; i++) {
			const setName = `set-${String(i).padStart(3, '0')}`;
			const setDir = path.join(tmpDir, setName);
			fs.mkdirSync(setDir, { recursive: true });
			fs.writeFileSync(path.join(setDir, '001.png'), pngBytes);
		}

		csrf = await loginAndGetCsrf(page);
		await setAdultModuleState(page, csrf, true, tmpDir);

		const scanRes = await page.request.post('/api/v1/adult/scan', {
			headers: { 'X-CSRF-Token': csrf }
		});
		expect(scanRes.ok()).toBeTruthy();
		const summary = (await scanRes.json()) as { sets_found: number; items_found: number };
		expect(summary.sets_found).toBe(52);

		await page.goto('/adult/image-sets');
		const listSection = page.locator('section[aria-labelledby="adult-image-sets-list-title"]');
		const listItems = listSection.locator('ul').first().locator('> li');
		await expect(listItems).toHaveCount(50);

		const shownLine = listSection.locator('p[aria-live="polite"]');
		await expect(shownLine).toContainText('50');
		await expect(shownLine).toContainText('52');

		await expect(listSection.getByRole('heading', { name: 'set-052', exact: true })).toBeVisible();
		await expect(listSection.getByRole('heading', { name: 'set-003', exact: true })).toBeVisible();
		await expect(listSection.getByRole('heading', { name: 'set-001', exact: true })).toHaveCount(0);

		const loadMoreBtn = listSection.locator('button.btn:not(.btn-sm)');
		await expect(loadMoreBtn).toBeVisible();
		await loadMoreBtn.click();

		await expect(listItems).toHaveCount(52);
		await expect(shownLine).toContainText('52');
		await expect(listSection.getByRole('heading', { name: 'set-001', exact: true })).toBeVisible();
		await expect(listSection.getByRole('heading', { name: 'set-002', exact: true })).toBeVisible();
		await expect(loadMoreBtn).toHaveCount(0);
	} finally {
		if (csrf) {
			await dropAllButFirstSet(page, csrf, tmpDir);
			await setAdultModuleState(page, csrf, false, tmpDir);
		}
		fs.rmSync(tmpDir, { recursive: true, force: true });
	}
});
