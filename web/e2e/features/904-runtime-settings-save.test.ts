// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #907 #908
import { expect, test, type Page } from '@playwright/test';

test.use({ storageState: { cookies: [], origins: [] } });

const KEY = 'snatcharr.item_search.cooldown_seconds';

interface RuntimeSetting {
	key: string;
	class: string;
	source: string;
	value?: string;
}

async function signIn(page: Page): Promise<string> {
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok(), `login status ${login.status()}`).toBeTruthy();
	return ((await login.json()) as { csrf_token: string }).csrf_token;
}

async function setting(page: Page, key: string): Promise<RuntimeSetting | undefined> {
	const res = await page.request.get('/api/v1/settings/runtime');
	expect(res.status()).toBe(200);
	return ((await res.json()) as RuntimeSetting[]).find((s) => s.key === key);
}

async function put(page: Page, csrf: string, key: string, value: string) {
	return page.request.put('/api/v1/settings/runtime', {
		headers: { 'X-CSRF-Token': csrf },
		data: { values: { [key]: value } }
	});
}

// The stack runs with APP_HTTP_ADDR and APP_GRPC_LISTEN set, as every deployment does.
test('907: a runtime setting saves while framework environment variables are set', async ({
	page
}) => {
	const csrf = await signIn(page);
	try {
		const res = await put(page, csrf, KEY, '345');
		expect(res.status(), await res.text()).toBe(200);
		const stored = await setting(page, KEY);
		expect(stored?.source).toBe('db');
		expect(stored?.value).toBe('345');
	} finally {
		const cleared = await put(page, csrf, KEY, '');
		expect(cleared.status()).toBe(200);
	}
	expect((await setting(page, KEY))?.source).toBe('default');
});

test('908: telemetry settings read as bootstrap and refuse a write', async ({ page }) => {
	const csrf = await signIn(page);
	expect((await setting(page, 'otel.enabled'))?.class).toBe('bootstrap');
	const res = await put(page, csrf, 'otel.enabled', 'true');
	expect(res.status()).toBe(422);
	expect(await res.text()).toContain('read-only');
});
