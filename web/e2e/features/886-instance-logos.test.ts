// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #886 #887
import { expect, test, type Locator, type Page } from '@playwright/test';
import { VIEWPORTS } from '../layout.js';
import { createInstance, createRow, deleteRows, signIn, uniqueSuffix } from './layout-fixtures.js';

const APP_KINDS = ['sonarr', 'radarr', 'lidarr', 'readarr', 'whisparr_v2', 'whisparr_v3'];
// rTorrent is a declared text-only exception: its row shows the category icon, no vendor logo.
const CLIENT_LOGO_KINDS = ['qbittorrent', 'transmission', 'deluge', 'sabnzbd', 'nzbget'];

// A logo counts as shown when the browser fetched the served file and decoded it.
async function expectLogoLoaded(logo: Locator): Promise<void> {
	await expect(logo).toHaveCount(1);
	await logo.scrollIntoViewIfNeeded();
	const src = await logo.getAttribute('src');
	await expect
		.poll(() => logo.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth > 0), {
			message: `logo ${src} did not load`
		})
		.toBe(true);
}

function rowOf(page: Page, name: string): Locator {
	return page.getByRole('row').filter({ hasText: name });
}

test('886: every instance row shows the logo of its own app', async ({ page }) => {
	const csrf = await signIn(page);
	const suffix = uniqueSuffix();
	const ids: string[] = [];
	try {
		const names = APP_KINDS.map((kind) => ({ kind, name: `e2e-886-${suffix}-${kind}` }));
		for (const { kind, name } of names) ids.push(await createInstance(page, csrf, name, kind));
		await page.setViewportSize(VIEWPORTS.desktop);
		await page.goto('/instances');
		for (const { kind, name } of names) {
			await expectLogoLoaded(rowOf(page, name).locator(`img[src$="logos/${kind}.svg"]`));
		}
	} finally {
		await deleteRows(page, csrf, 'instances', ids);
	}
});

test('886: download client rows show vendor logos and rTorrent its category icon', async ({
	page
}) => {
	const csrf = await signIn(page);
	const suffix = uniqueSuffix();
	const ids: string[] = [];
	try {
		const kinds = [...CLIENT_LOGO_KINDS, 'rtorrent'];
		for (const kind of kinds) {
			const name = `e2e-886-${suffix}-${kind}`;
			ids.push(
				await createRow(page, csrf, 'download-clients', {
					name,
					kind,
					base_url: `http://127.0.0.1:9091/${name}`,
					enabled: true
				})
			);
		}
		await page.setViewportSize(VIEWPORTS.desktop);
		await page.goto('/connections/download-clients');
		for (const kind of CLIENT_LOGO_KINDS) {
			const row = rowOf(page, `e2e-886-${suffix}-${kind}`);
			await expectLogoLoaded(row.locator(`img[src$="logos/${kind}.svg"]`));
		}
		const rtorrent = rowOf(page, `e2e-886-${suffix}-rtorrent`);
		await expect(rtorrent).toBeVisible();
		await expect(rtorrent.locator('img[src*="logos/"]')).toHaveCount(0);
		await expect(rtorrent.locator('svg[aria-hidden="true"]').first()).toBeVisible();
	} finally {
		await deleteRows(page, csrf, 'download-clients', ids);
	}
});

test('887: the Seerr logo on the connections hub is the square app icon', async ({ page }) => {
	await signIn(page);
	await page.setViewportSize(VIEWPORTS.desktop);
	await page.goto('/connections');
	const logo = page.locator('img[src$="logos/seerr.svg"]').filter({ visible: true });
	await expectLogoLoaded(logo);
	// The old wordmark was 322x96 and shrank to a thin strip in the square logo box.
	const size = await logo.evaluate((el: HTMLImageElement) => ({
		width: el.naturalWidth,
		height: el.naturalHeight
	}));
	expect(size.width, `Seerr logo is ${size.width}x${size.height}, not square`).toBe(size.height);
});
