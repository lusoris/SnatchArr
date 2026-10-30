// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #854

import fs from 'node:fs';
import path from 'node:path';
import { expect, test, type Page } from '@playwright/test';

test.use({ storageState: { cookies: [], origins: [] } });

interface McpServerConfig {
	type?: string;
	url?: string;
	headers?: Record<string, string>;
}

interface McpConfig {
	mcpServers?: Record<string, McpServerConfig>;
}

async function loginAndGetCsrf(page: Page): Promise<string> {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok(), `login status: ${login.status()}`).toBeTruthy();
	return ((await login.json()) as { csrf_token: string }).csrf_token;
}

// mcp-sweep.test.ts also switches the MCP route on and leaves it on; restoring the flag here
// would race with that project, which runs beside the feature tests.
async function enableMcp(page: Page, csrf: string): Promise<void> {
	const settingsRes = await page.request.get('/api/v1/settings');
	expect(settingsRes.ok()).toBeTruthy();
	const settings = (await settingsRes.json()) as Record<string, unknown>;
	if (settings.llm_enabled) return;
	settings.llm_enabled = true;
	const putRes = await page.request.put('/api/v1/settings', {
		data: settings,
		headers: { 'X-CSRF-Token': csrf }
	});
	expect(putRes.ok()).toBeTruthy();
}

test('854: project .mcp.json declares the snatcharr http server pointing to a live route', async ({
	page
}) => {
	const rootMcpPath = path.resolve(import.meta.dirname, '../../../.mcp.json');
	const raw = fs.readFileSync(rootMcpPath, 'utf-8');
	const config = JSON.parse(raw) as McpConfig;

	expect(config.mcpServers).toBeDefined();
	const server = config.mcpServers?.snatcharr;
	expect(server).toBeDefined();
	expect(server?.type).toBe('http');
	expect(server?.url).toBeDefined();

	const urlMatch = /\/(api\/v1\/[a-z0-9_-]+)/.exec(server?.url ?? '');
	expect(urlMatch).not.toBeNull();
	const endpointPath = `/${urlMatch?.[1]}`;
	expect(endpointPath).toBe('/api/v1/mcp');

	const csrf = await loginAndGetCsrf(page);
	await enableMcp(page, csrf);
	await page.context().clearCookies();
	const res = await page.request.get(endpointPath);
	expect(res.status()).not.toBe(404);
	expect(res.status()).toBe(401);
	expect(res.headers()['www-authenticate']).toContain('Bearer');
});
