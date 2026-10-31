// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
// SPDX-License-Identifier: EUPL-1.2
import { expect, test, type Locator, type Page } from '@playwright/test';
import { json, mockSignedInApi } from './mocks.js';

const appKinds = ['sonarr', 'radarr', 'lidarr', 'readarr', 'whisparr_v2', 'whisparr_v3'];
const clientKinds = ['qbittorrent', 'transmission', 'deluge', 'rtorrent', 'sabnzbd', 'nzbget'];
const stamp = '2026-01-01T00:00:00Z';
const id = (n: number) => `00000000-0000-0000-0000-${String(n).padStart(12, '0')}`;

async function mockLogoApi(page: Page): Promise<void> {
	await mockSignedInApi(page);
	await page.route('**/api/v1/instances', (route) =>
		route.fulfill(
			json(
				appKinds.map((kind, n) => ({
					id: id(n + 1),
					name: `Library ${n + 1}`,
					kind,
					source: 'manual',
					enabled: true,
					base_url: `http://app${n + 1}.local`,
					created_at: stamp,
					updated_at: stamp
				}))
			)
		)
	);
	await page.route('**/api/v1/download-clients', (route) =>
		route.fulfill(
			json(
				clientKinds.map((kind, n) => ({
					id: id(n + 101),
					kind,
					protocol: ['sabnzbd', 'nzbget'].includes(kind) ? 'usenet' : 'torrent',
					name: `Client ${n + 1}`,
					base_url: `http://client${n + 1}.local`,
					username: '',
					enabled: true,
					source: 'manual',
					max_active: 0,
					max_queued: 0,
					bandwidth_budget_bps: 0,
					created_at: stamp,
					updated_at: stamp
				}))
			)
		)
	);
}

/**
 * Every visible logo image in scope must have loaded from the served static directory. Routes
 * render a table and a card list and hide one per breakpoint; lazy images in the hidden one never load.
 */
async function expectLogosLoaded(scope: Page | Locator, want: number): Promise<void> {
	const logos = scope.locator('img[src*="logos/"]').filter({ visible: true });
	await expect(logos).toHaveCount(want);
	for (let index = 0; index < want; index += 1) {
		const logo = logos.nth(index);
		const src = await logo.getAttribute('src');
		await logo.scrollIntoViewIfNeeded();
		await expect
			.poll(() => logo.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth > 0), {
				message: `logo ${src} did not load`
			})
			.toBe(true);
	}
}

test('authenticated: every instance shows its app logo', async ({ page }) => {
	await mockLogoApi(page);
	await page.goto('/instances');
	await expect(page.getByRole('link', { name: 'Library 1' }).first()).toBeVisible();
	await expectLogosLoaded(page, appKinds.length);
	for (const kind of appKinds) {
		await expect(
			page.locator(`img[src$="logos/${kind}.svg"]`).filter({ visible: true }).first()
		).toBeVisible();
	}
});

test('authenticated: download clients show logos, rTorrent its category icon and name', async ({
	page
}) => {
	await mockLogoApi(page);
	await page.goto('/connections/download-clients');
	await expect(page.getByText('Client 1', { exact: true })).toBeVisible();
	// Five vendor logos in the table; rTorrent is a declared text-only exception.
	const table = page.getByRole('table');
	await expectLogosLoaded(table, 5);
	await expect(table.locator('img[src*="logos/rtorrent"]')).toHaveCount(0);
	await expect(page.getByText(/^rTorrent · torrent$/)).toBeVisible();
});

// Review screenshots of the logo surfaces, on request (SNATCHARR_LAYOUT_SHOTS=1).
const shotViewports = [
	{ name: 'laptop', width: 1280, height: 800 },
	{ name: 'desktop', width: 1920, height: 1080 },
	{ name: 'phone', width: 390, height: 844 }
] as const;
for (const route of ['/connections', '/connections/download-clients', '/instances']) {
	for (const viewport of shotViewports) {
		for (const scheme of ['light', 'dark'] as const) {
			test(`authenticated: logos screenshot ${route} ${viewport.name} ${scheme}`, async ({
				page
			}, testInfo) => {
				test.skip(!process.env.SNATCHARR_LAYOUT_SHOTS, 'screenshots on request only');
				await page.setViewportSize({ width: viewport.width, height: viewport.height });
				await page.emulateMedia({ colorScheme: scheme });
				await mockLogoApi(page);
				await page.goto(route);
				await expect(
					page.locator('img[src*="logos/"]').filter({ visible: true }).first()
				).toBeVisible();
				const slug = route.replaceAll('/', '-').slice(1);
				await page.screenshot({
					path: testInfo.outputPath(`logos-${slug}-${viewport.name}-${scheme}.png`),
					fullPage: true
				});
			});
		}
	}
}
