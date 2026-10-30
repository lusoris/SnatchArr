// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Seeding helpers of the layout and logo feature tests. Not a test file: the features project
// matches `*.test.ts` only. Every row a test creates carries a unique name and is deleted again,
// because the other feature tests share the database.
import { expect, type Page } from '@playwright/test';

export type Collection = 'instances' | 'download-clients' | 'schedules';

// The feature run sets these; a missing one is a broken harness, not a reason to skip.
export function requireEnv(name: string): string {
	const value = process.env[name];
	if (!value) {
		throw new Error(`${name} unset: run through tools/e2e/web-e2e-api.sh`);
	}
	return value;
}

// A short random suffix that keeps names and base URLs unique across tests and retries.
export function uniqueSuffix(): string {
	return crypto.randomUUID().slice(0, 8);
}

// Real sign-in through the API; the page context keeps the session cookie. Returns the CSRF token.
export async function signIn(page: Page): Promise<string> {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok(), `login status ${login.status()}`).toBeTruthy();
	return ((await login.json()) as { csrf_token: string }).csrf_token;
}

export async function createRow(
	page: Page,
	csrf: string,
	collection: Collection,
	data: Record<string, unknown>
): Promise<string> {
	const res = await page.request.post(`/api/v1/${collection}`, {
		headers: { 'X-CSRF-Token': csrf },
		data
	});
	expect(res.status(), `create ${collection}: ${await res.text()}`).toBe(201);
	return ((await res.json()) as { id: string }).id;
}

export async function deleteRows(
	page: Page,
	csrf: string,
	collection: Collection,
	ids: readonly string[]
): Promise<void> {
	for (const id of ids) {
		const res = await page.request.delete(`/api/v1/${collection}/${id}`, {
			headers: { 'X-CSRF-Token': csrf }
		});
		expect([204, 404], `delete ${collection}/${id}`).toContain(res.status());
	}
}

// An instance on the fake *arr. The base URL is unique per instance (a duplicate is refused);
// creating one never contacts the fake, so the path suffix is never requested.
export async function createInstance(
	page: Page,
	csrf: string,
	name: string,
	kind = 'sonarr'
): Promise<string> {
	return createRow(page, csrf, 'instances', {
		kind,
		name,
		base_url: `${requireEnv('SNATCHARR_E2E_FAKEARR')}/${name}`,
		api_key: 'fake-api-key-0123456789',
		enabled: true
	});
}
