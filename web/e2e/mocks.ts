// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Signed-in admin over an empty fleet: the API state the mocked e2e suites share.
import type { Page } from '@playwright/test';

function json(body: unknown): { status: number; contentType: string; body: string } {
	return { status: 200, contentType: 'application/json', body: JSON.stringify(body) };
}

export async function mockSignedInApi(page: Page): Promise<void> {
	await page.route('**/api/v1/auth/setup/status', (route) =>
		route.fulfill(json({ setup_complete: true }))
	);
	await page.route('**/api/v1/auth/session', (route) =>
		route.fulfill(
			json({
				user: { id: '00000000-0000-0000-0000-000000000099', username: 'admin', role: 'admin' },
				csrf_token: 'test-token',
				expires_at: '2099-01-01T00:00:00Z'
			})
		)
	);
	await page.route('**/api/v1/auth/sso', (route) =>
		route.fulfill(json({ oidc: false, proxy: false }))
	);
	for (const path of ['instances', 'hourly-caps', 'prowlarr/status']) {
		await page.route(`**/api/v1/${path}`, (route) => route.fulfill(json([])));
	}
}
