// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #679 #821 #825

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { expect, request, test, type Page } from '@playwright/test';

const proxySecret = process.env.SNATCHARR_E2E_PROXY_SECRET ?? '';
const proxyUserHeader = process.env.SNATCHARR_E2E_PROXY_HEADER ?? 'Remote-User';
const proxyGroupsHeader = process.env.SNATCHARR_E2E_PROXY_GROUPS_HEADER ?? 'X-authentik-groups';
const proxySecretHeader =
	process.env.SNATCHARR_E2E_PROXY_SECRET_HEADER ?? 'X-SnatchArr-Proxy-Secret';

// 1x1 valid PNG image bytes
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

test('679: adult image sets scan, stashignore creation, detail retrieval, file streaming, and containment checks', async ({
	page
}) => {
	const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'snatcharr-679-'));
	const outsideFile = path.join(os.tmpdir(), 'outside-679.png');

	try {
		// Prepare test filesystem structure:
		// - set-one with two PNG files and a symlink to outsideFile
		// - set-two with one PNG file
		// - empty-set with zero files
		const set1Dir = path.join(tmpDir, 'set-one');
		const set2Dir = path.join(tmpDir, 'set-two');
		const emptyDir = path.join(tmpDir, 'empty-set');

		fs.mkdirSync(set1Dir, { recursive: true });
		fs.mkdirSync(set2Dir, { recursive: true });
		fs.mkdirSync(emptyDir, { recursive: true });

		fs.writeFileSync(path.join(set1Dir, '001.png'), pngBytes);
		fs.writeFileSync(path.join(set1Dir, '002.png'), pngBytes);
		fs.writeFileSync(path.join(set2Dir, '001.png'), pngBytes);

		fs.writeFileSync(outsideFile, pngBytes);
		fs.symlinkSync(outsideFile, path.join(set1Dir, 'symlink.png'));

		const csrf = await loginAndGetCsrf(page);

		// Negative: module off -> 404
		await setAdultModuleState(page, csrf, false, tmpDir);
		const offRes = await page.request.get('/api/v1/adult/image-sets');
		expect(offRes.status()).toBe(404);

		// Enable adult module pointing at tmpDir
		await setAdultModuleState(page, csrf, true, tmpDir);

		// Negative: viewer scan 403 via proxy-session (if proxy configured)
		if (proxySecret) {
			const viewerCtx = await request.newContext({ storageState: { cookies: [], origins: [] } });
			try {
				const loginRes = await viewerCtx.post('/api/v1/auth/proxy-session', {
					headers: {
						[proxyUserHeader]: 'e2e-viewer-679',
						[proxyGroupsHeader]: 'e2e-viewers',
						[proxySecretHeader]: proxySecret
					}
				});
				if (loginRes.status() === 200) {
					const loginBody = (await loginRes.json()) as { csrf_token: string };
					const viewerScan = await viewerCtx.post('/api/v1/adult/scan', {
						headers: { 'X-CSRF-Token': loginBody.csrf_token }
					});
					expect(viewerScan.status()).toBe(403);
					expect(viewerScan.headers()['content-type']).toContain('application/problem+json');
				}
			} finally {
				await viewerCtx.dispose();
			}
		}

		// Positive: Admin triggers scan -> 200 summary with ignore_file_present: true
		const scanRes = await page.request.post('/api/v1/adult/scan', {
			headers: { 'X-CSRF-Token': csrf }
		});
		expect(scanRes.ok()).toBeTruthy();
		const summary = (await scanRes.json()) as {
			sets_found: number;
			items_found: number;
			ignore_file_present: boolean;
		};
		expect(summary.sets_found).toBe(3);
		expect(summary.items_found).toBe(3);
		expect(summary.ignore_file_present).toBe(true);

		// Verify .stashignore exists in tmp root and starts with "# SnatchArr" (#821)
		const stashIgnorePath = path.join(tmpDir, '.stashignore');
		expect(fs.existsSync(stashIgnorePath)).toBe(true);
		const stashIgnoreContent = fs.readFileSync(stashIgnorePath, 'utf-8');
		expect(stashIgnoreContent.startsWith('# SnatchArr')).toBe(true);

		// Positive: GET /adult/image-sets lists all sets
		const listRes = await page.request.get('/api/v1/adult/image-sets');
		expect(listRes.ok()).toBeTruthy();
		const listData = (await listRes.json()) as {
			items: Array<{ id: string; title: string; item_count: number }>;
			total: number;
		};
		expect(listData.total).toBe(3);
		const setOne = listData.items.find((s) => s.title === 'set-one');
		expect(setOne).toBeDefined();
		expect(setOne?.item_count).toBe(2);

		// Positive: GET /adult/image-sets/{setId} returns detail + ordered items
		const detailRes = await page.request.get(`/api/v1/adult/image-sets/${setOne?.id}`);
		expect(detailRes.ok()).toBeTruthy();
		const detailData = (await detailRes.json()) as {
			id: string;
			title: string;
			ignore_file_present: boolean;
			items: Array<{ id: string; file_name: string }>;
		};
		expect(detailData.ignore_file_present).toBe(true);
		expect(detailData.items.length).toBe(2);
		expect(detailData.items[0].file_name).toBe('001.png');
		expect(detailData.items[1].file_name).toBe('002.png');

		// Negative: symlink pointing outside tmp root is not listed (#825)
		const hasSymlink = detailData.items.some((i) => i.file_name === 'symlink.png');
		expect(hasSymlink).toBe(false);

		// Positive: GET item file streams valid PNG bytes with correct headers
		const firstItemId = detailData.items[0].id;
		const fileRes = await page.request.get(`/api/v1/adult/image-items/${firstItemId}/file`);
		expect(fileRes.ok()).toBeTruthy();
		expect(fileRes.headers()['content-type']).toBe('image/png');
		expect(fileRes.headers()['x-content-type-options']).toBe('nosniff');
		const fileBytes = await fileRes.body();
		expect(fileBytes.equals(pngBytes)).toBe(true);

		// Negative: unknown item ID -> 404
		const unknownItemRes = await page.request.get(
			'/api/v1/adult/image-items/00000000-0000-0000-0000-000000000000/file'
		);
		expect(unknownItemRes.status()).toBe(404);

		// Negative: unknown set ID -> 404
		const unknownSetRes = await page.request.get(
			'/api/v1/adult/image-sets/00000000-0000-0000-0000-000000000000'
		);
		expect(unknownSetRes.status()).toBe(404);

		// UI: /adult/image-sets shows both sets, ignore-file status, and scan toast
		await page.goto('/adult/image-sets');
		await expect(page.getByText('set-one')).toBeVisible();
		await expect(page.getByText('set-two')).toBeVisible();
		await expect(page.getByText('.stashignore present')).toBeVisible();

		const scanBtn = page.getByRole('button', { name: 'Scan now' });
		await expect(scanBtn).toBeVisible();
		await scanBtn.click();
		await expect(page.getByText('Scan completed: 3 sets, 3 items')).toBeVisible();

		// Restore module off
		await setAdultModuleState(page, csrf, false, tmpDir);
	} finally {
		if (fs.existsSync(outsideFile)) {
			fs.unlinkSync(outsideFile);
		}
		fs.rmSync(tmpDir, { recursive: true, force: true });
	}
});
