// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #854

import fs from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import { enableMcp } from './feature-helpers.js';
import { signIn } from './layout-fixtures.js';

test.use({ storageState: { cookies: [], origins: [] } });

interface McpServerConfig {
	type?: string;
	url?: string;
	headers?: Record<string, string>;
}

interface McpConfig {
	mcpServers?: Record<string, McpServerConfig>;
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

	const csrf = await signIn(page);
	await enableMcp(page, csrf);
	await page.context().clearCookies();
	const res = await page.request.get(endpointPath);
	expect(res.status()).not.toBe(404);
	expect(res.status()).toBe(401);
	expect(res.headers()['www-authenticate']).toContain('Bearer');
});
