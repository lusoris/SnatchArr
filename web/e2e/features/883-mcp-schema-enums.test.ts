// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #883

import { expect, test, type Page } from '@playwright/test';
import { enableMcp } from './feature-helpers.js';
import { signIn } from './layout-fixtures.js';

test.use({ storageState: { cookies: [], origins: [] } });

interface PropertyDef {
	type?: string;
	enum?: string[];
	pattern?: string;
	default?: number;
}

interface McpTool {
	name: string;
	inputSchema?: {
		properties?: Record<string, PropertyDef>;
	};
}

async function requestMcpTools(page: Page, secret: string): Promise<McpTool[]> {
	const headers: Record<string, string> = {
		Authorization: `Bearer ${secret}`,
		'Content-Type': 'application/json',
		Accept: 'application/json, text/event-stream'
	};
	const initRes = await page.request.post('/api/v1/mcp', {
		headers,
		data: {
			jsonrpc: '2.0',
			id: 1,
			method: 'initialize',
			params: {
				protocolVersion: '2025-03-26',
				capabilities: {},
				clientInfo: { name: 'e2e-883', version: '1.0.0' }
			}
		}
	});
	expect(initRes.status()).toBe(200);
	const sessionId = initRes.headers()['mcp-session-id'];
	if (sessionId) headers['Mcp-Session-Id'] = sessionId;

	const listRes = await page.request.post('/api/v1/mcp', {
		headers,
		data: { jsonrpc: '2.0', id: 2, method: 'tools/list' }
	});
	expect(listRes.status()).toBe(200);
	const payload = (await listRes.json()) as { result?: { tools?: McpTool[] } };
	return payload.result?.tools ?? [];
}

test('883: MCP tool schemas preserve parameter enums, patterns and defaults on live route', async ({
	page
}) => {
	const csrf = await signIn(page);
	await enableMcp(page, csrf);
	let keyId = '';

	try {
		const keyRes = await page.request.post('/api/v1/auth/api-keys', {
			data: { name: 'mcp-schema-enums-883', scopes: ['core:read', 'admin:read', 'admin:write'] },
			headers: { 'X-CSRF-Token': csrf }
		});
		expect(keyRes.status()).toBe(201);
		const createdKey = (await keyRes.json()) as { id: string; secret: string };
		keyId = createdKey.id;

		const tools = await requestMcpTools(page, createdKey.secret);
		const listLogsTool = tools.find((t) => t.name === 'snatcharr_list_logs');
		expect(listLogsTool).toBeDefined();

		const props = listLogsTool?.inputSchema?.properties;
		expect(props?.level?.enum).toEqual(['debug', 'info', 'warn', 'error']);
		expect(props?.source?.pattern).toBe('^(api|worker(:[^\\s:]+)?)$');
		expect(props?.limit?.default).toBe(200);
	} finally {
		if (keyId) {
			await page.request.delete(`/api/v1/auth/api-keys/${keyId}`, {
				headers: { 'X-CSRF-Token': csrf }
			});
		}
	}
});
