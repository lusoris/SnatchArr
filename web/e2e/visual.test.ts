// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Visual baseline of the public pages (gap audit C15-03), after the sveltesentio recipe
// docs/compose/playwright-visual.md: fonts ready and animations off before every shot, both
// colour schemes per page. It runs only as the "visual" project inside the pinned Playwright
// image (tools/e2e/web_visual.sh), so the pixels never depend on the host's fonts or GPU.
// The API is mocked: the pages show the state of a fresh install and of a signed-out visitor.
import { expect, test, type Page } from '@playwright/test';
import { mockInstancePolicyApi, mockSettingsApi, mockSignedInApi } from './mocks.js';

const PAGES = [
	{ name: 'login', path: '/login', setupComplete: true },
	{ name: 'setup', path: '/setup', setupComplete: false }
] as const;
// The signed-in shell (ADR-0016): sidebar on desktop, top bar and bottom bar on a phone.
const SHELL_VIEWPORTS = [
	{ name: 'desktop', width: 1280, height: 800 },
	{ name: 'phone', width: 390, height: 844 }
] as const;
const LOCALES = ['en', 'de'] as const;
const SCHEMES = ['light', 'dark'] as const;

// Planted visual changes (SNATCHARR_VISUAL_PLANT, make web-visual): each must fail the
// comparison, or the baseline proves nothing.
const PLANTS: Record<string, string> = {
	shift: 'body { padding-top: 24px !important; }',
	brand: '.btn-primary { background: oklch(0.6 0.2 30) !important; }',
	// Only the signed-in shell marks a current page, so this case proves the shell shots.
	nav: '[aria-current="page"] { background: oklch(0.6 0.2 30) !important; }'
};

function json(body: unknown): { status: number; contentType: string; body: string } {
	return { status: 200, contentType: 'application/json', body: JSON.stringify(body) };
}

async function mockPublicApi(page: Page, setupComplete: boolean): Promise<void> {
	await page.route('**/api/v1/auth/setup/status', (route) =>
		route.fulfill(json({ setup_complete: setupComplete, secret_required: false }))
	);
	await page.route('**/api/v1/auth/session', (route) =>
		route.fulfill({
			status: 401,
			contentType: 'application/problem+json',
			body: JSON.stringify({ type: 'about:blank', title: 'Unauthorized', status: 401 })
		})
	);
	await page.route('**/api/v1/auth/sso', (route) =>
		route.fulfill(json({ oidc: false, proxy: false }))
	);
}

async function prep(page: Page, path: string): Promise<void> {
	await page.goto(path);
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await page.evaluate(() => document.fonts.ready);
	await page.addStyleTag({
		content:
			'*, *::before, *::after { animation-duration: 0s !important; transition-duration: 0s !important; }'
	});
	const plant = process.env.SNATCHARR_VISUAL_PLANT;
	if (plant) {
		const css = PLANTS[plant];
		if (!css) throw new Error(`unknown SNATCHARR_VISUAL_PLANT ${plant}`);
		await page.addStyleTag({ content: css });
	}
}

for (const target of PAGES) {
	for (const locale of LOCALES) {
		for (const scheme of SCHEMES) {
			test(`${target.name} ${locale} ${scheme}`, async ({ page, baseURL }) => {
				await page
					.context()
					.addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: baseURL ?? '' }]);
				await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
				await mockPublicApi(page, target.setupComplete);
				await prep(page, target.path);
				await expect(page).toHaveScreenshot(`${target.name}-${locale}-${scheme}.png`, {
					fullPage: true
				});
			});
		}
	}
}

for (const viewport of SHELL_VIEWPORTS) {
	for (const locale of LOCALES) {
		for (const scheme of SCHEMES) {
			test(`shell ${viewport.name} ${locale} ${scheme}`, async ({ page, baseURL }) => {
				await page
					.context()
					.addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: baseURL ?? '' }]);
				await page.setViewportSize({ width: viewport.width, height: viewport.height });
				await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
				await mockSignedInApi(page);
				await prep(page, '/');
				await expect(page).toHaveScreenshot(`shell-${viewport.name}-${locale}-${scheme}.png`);
			});
		}
	}
}

const POLICY_TARGETS = [
	{ kind: 'sonarr' as const, id: '00000000-0000-0000-0000-000000000001' },
	{ kind: 'radarr' as const, id: '00000000-0000-0000-0000-000000000002' }
] as const;

for (const target of POLICY_TARGETS) {
	for (const viewport of SHELL_VIEWPORTS) {
		for (const locale of LOCALES) {
			for (const scheme of SCHEMES) {
				test(`policy ${target.kind} ${viewport.name} ${locale} ${scheme}`, async ({
					page,
					baseURL
				}) => {
					await page
						.context()
						.addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: baseURL ?? '' }]);
					await page.setViewportSize({ width: viewport.width, height: viewport.height });
					await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
					await mockInstancePolicyApi(page, target.id, { instanceKind: target.kind });
					await prep(page, `/instances/${target.id}/policy`);
					await expect(page.locator('[data-field="missing_per_cycle"]')).toBeVisible();
					await expect(page).toHaveScreenshot(
						`policy-${target.kind}-${viewport.name}-${locale}-${scheme}.png`,
						{ fullPage: true }
					);
				});
			}
		}
	}
}

for (const viewport of SHELL_VIEWPORTS) {
	for (const locale of LOCALES) {
		for (const scheme of SCHEMES) {
			test(`settings ${viewport.name} ${locale} ${scheme}`, async ({ page, baseURL }) => {
				await page
					.context()
					.addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: baseURL ?? '' }]);
				await page.setViewportSize({ width: viewport.width, height: viewport.height });
				await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
				await mockSettingsApi(page);
				await prep(page, '/settings');
				await expect(page.locator('[data-field="global_hourly_cap"]')).toBeVisible();
				await expect(page).toHaveScreenshot(`settings-${viewport.name}-${locale}-${scheme}.png`, {
					fullPage: true
				});
			});
		}
	}
}
