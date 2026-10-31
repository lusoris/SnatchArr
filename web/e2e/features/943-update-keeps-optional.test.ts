// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #943

import { expect, test, type Page } from '@playwright/test';

test.use({ storageState: { cookies: [], origins: [] } });

interface ChannelConfig {
	server_url?: string;
	topic?: string;
	token?: string;
}

interface ChannelPayload {
	id: string;
	name: string;
	kind: string;
	enabled: boolean;
	has_secret: boolean;
	config: ChannelConfig;
}

interface InstancePayload {
	id: string;
	name: string;
	kind: string;
	base_url: string;
	enabled: boolean;
}

interface McpRpcResponse {
	result?: {
		isError?: boolean;
		structuredContent?: unknown;
		content?: Array<{ type?: string; text?: string }>;
	};
	error?: { code?: number; message?: string };
}

const NOTIFY_WRITE_TOOLS = [
	'snatcharr_create_notification_channel',
	'snatcharr_update_notification_channel',
	'snatcharr_delete_notification_channel'
];

async function loginAndGetCsrf(page: Page): Promise<string> {
	await page.context().clearCookies();
	const login = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(login.ok(), `login status: ${login.status()}`).toBeTruthy();
	return ((await login.json()) as { csrf_token: string }).csrf_token;
}

async function enableMcpWithNotifyTools(page: Page, csrf: string): Promise<void> {
	const settingsRes = await page.request.get('/api/v1/settings');
	expect(settingsRes.ok()).toBeTruthy();
	const settings = (await settingsRes.json()) as {
		llm_enabled?: boolean;
		llm_write_tools?: string[];
		[key: string]: unknown;
	};

	let changed = false;
	if (!settings.llm_enabled) {
		settings.llm_enabled = true;
		changed = true;
	}

	const existing = new Set(settings.llm_write_tools ?? []);
	for (const tool of NOTIFY_WRITE_TOOLS) {
		if (!existing.has(tool)) {
			existing.add(tool);
			changed = true;
		}
	}

	if (!changed) return;

	settings.llm_write_tools = [...existing];
	const putRes = await page.request.put('/api/v1/settings', {
		data: settings,
		headers: { 'X-CSRF-Token': csrf }
	});
	expect(putRes.ok()).toBeTruthy();
}

async function createNtfyChannel(page: Page, csrf: string, name: string): Promise<ChannelPayload> {
	const res = await page.request.post('/api/v1/notifications/channels', {
		headers: { 'X-CSRF-Token': csrf },
		data: {
			name,
			kind: 'ntfy',
			enabled: false,
			config: {
				server_url: 'https://ntfy.example.invalid',
				topic: 'e2e-943',
				token: 'e2e-943-token'
			}
		}
	});
	expect(res.status()).toBe(201);
	return (await res.json()) as ChannelPayload;
}

async function deleteChannel(page: Page, csrf: string, id: string): Promise<void> {
	await page.request.delete(`/api/v1/notifications/channels/${id}`, {
		headers: { 'X-CSRF-Token': csrf }
	});
}

async function deleteApiKey(page: Page, csrf: string, id: string): Promise<void> {
	await page.request.delete(`/api/v1/auth/api-keys/${id}`, {
		headers: { 'X-CSRF-Token': csrf }
	});
}

async function initMcpHeaders(page: Page, secret: string): Promise<Record<string, string>> {
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
				clientInfo: { name: 'e2e-943', version: '1.0.0' }
			}
		}
	});
	expect(initRes.status()).toBe(200);
	const sessionId = initRes.headers()['mcp-session-id'];
	if (sessionId) {
		headers['Mcp-Session-Id'] = sessionId;
	}
	return headers;
}

async function callMcpTool(
	page: Page,
	headers: Record<string, string>,
	id: number,
	name: string,
	args: Record<string, unknown>
): Promise<McpRpcResponse> {
	const res = await page.request.post('/api/v1/mcp', {
		headers,
		data: {
			jsonrpc: '2.0',
			id,
			method: 'tools/call',
			params: { name, arguments: args }
		}
	});
	expect(res.status()).toBe(200);
	return (await res.json()) as McpRpcResponse;
}

function parseChannelPayload(raw: unknown): ChannelPayload {
	if (typeof raw === 'string') {
		return JSON.parse(raw) as ChannelPayload;
	}
	if (raw && typeof raw === 'object') {
		return raw as ChannelPayload;
	}
	throw new Error('invalid channel payload');
}

test('943: HTTP notification channel update without enabled keeps stored enabled=false, sent enabled=true replaces', async ({
	page
}) => {
	const csrf = await loginAndGetCsrf(page);
	const suffix = crypto.randomUUID().slice(0, 8);
	const initialName = `e2e-943-http-${suffix}`;
	const renamedName = `e2e-943-http-renamed-${suffix}`;
	let channelId = '';

	try {
		const created = await createNtfyChannel(page, csrf, initialName);
		channelId = created.id;
		expect(created.enabled).toBe(false);

		// Rename only (omit enabled)
		const putRes = await page.request.put(`/api/v1/notifications/channels/${channelId}`, {
			headers: { 'X-CSRF-Token': csrf },
			data: { name: renamedName, kind: 'ntfy' }
		});
		expect(putRes.status()).toBe(200);

		// Read back: enabled stays false
		const getRes = await page.request.get(`/api/v1/notifications/channels/${channelId}`);
		expect(getRes.status()).toBe(200);
		const readBack = (await getRes.json()) as ChannelPayload;
		expect(readBack.name).toBe(renamedName);
		expect(readBack.enabled).toBe(false);

		// Send enabled: true
		const putEnabledRes = await page.request.put(`/api/v1/notifications/channels/${channelId}`, {
			headers: { 'X-CSRF-Token': csrf },
			data: { name: renamedName, kind: 'ntfy', enabled: true }
		});
		expect(putEnabledRes.status()).toBe(200);

		// Read back: enabled is now true
		const getEnabledRes = await page.request.get(`/api/v1/notifications/channels/${channelId}`);
		expect(getEnabledRes.status()).toBe(200);
		const readBackEnabled = (await getEnabledRes.json()) as ChannelPayload;
		expect(readBackEnabled.enabled).toBe(true);
	} finally {
		if (channelId) {
			await deleteChannel(page, csrf, channelId);
		}
	}
});

test('943: MCP notification channel update without enabled keeps stored enabled=false, sent enabled=true replaces', async ({
	page
}) => {
	const csrf = await loginAndGetCsrf(page);
	await enableMcpWithNotifyTools(page, csrf);
	const suffix = crypto.randomUUID().slice(0, 8);
	const initialName = `e2e-943-mcp-${suffix}`;
	const renamedName = `e2e-943-mcp-renamed-${suffix}`;
	let channelId = '';
	let keyId = '';

	try {
		const keyRes = await page.request.post('/api/v1/auth/api-keys', {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				name: `e2e-943-key-${suffix}`,
				scopes: ['core:read', 'notifications:read', 'notifications:write']
			}
		});
		expect(keyRes.status()).toBe(201);
		const createdKey = (await keyRes.json()) as { id: string; secret: string };
		keyId = createdKey.id;

		const mcpHeaders = await initMcpHeaders(page, createdKey.secret);

		const created = await createNtfyChannel(page, csrf, initialName);
		channelId = created.id;
		expect(created.enabled).toBe(false);

		// Call tool with rename only (omit enabled)
		const updateRpc = await callMcpTool(
			page,
			mcpHeaders,
			2,
			'snatcharr_update_notification_channel',
			{
				channelId,
				body: {
					name: renamedName,
					kind: 'ntfy'
				}
			}
		);
		expect(updateRpc.result?.isError).toBeFalsy();

		// Read back via snatcharr_get_notification_channel: enabled is false
		const getRpc = await callMcpTool(page, mcpHeaders, 3, 'snatcharr_get_notification_channel', {
			channelId
		});
		expect(getRpc.result?.isError).toBeFalsy();
		const rawStructured = getRpc.result?.structuredContent ?? getRpc.result?.content?.[0]?.text;
		const readBack = parseChannelPayload(rawStructured);
		expect(readBack.name).toBe(renamedName);
		expect(readBack.enabled).toBe(false);

		// Call tool with enabled: true
		const updateRpc2 = await callMcpTool(
			page,
			mcpHeaders,
			4,
			'snatcharr_update_notification_channel',
			{
				channelId,
				body: {
					name: renamedName,
					kind: 'ntfy',
					enabled: true
				}
			}
		);
		expect(updateRpc2.result?.isError).toBeFalsy();

		// Read back: enabled is true
		const getRpc2 = await callMcpTool(page, mcpHeaders, 5, 'snatcharr_get_notification_channel', {
			channelId
		});
		expect(getRpc2.result?.isError).toBeFalsy();
		const rawStructured2 = getRpc2.result?.structuredContent ?? getRpc2.result?.content?.[0]?.text;
		const readBack2 = parseChannelPayload(rawStructured2);
		expect(readBack2.enabled).toBe(true);
	} finally {
		if (channelId) {
			await deleteChannel(page, csrf, channelId);
		}
		if (keyId) {
			await deleteApiKey(page, csrf, keyId);
		}
	}
});

test('943: HTTP instance update without enabled keeps stored enabled=false, sent enabled=true replaces', async ({
	page
}) => {
	const csrf = await loginAndGetCsrf(page);
	const suffix = crypto.randomUUID().slice(0, 8);
	const initialName = `e2e-943-inst-${suffix}`;
	const renamedName = `e2e-943-inst-renamed-${suffix}`;
	let instanceId = '';

	try {
		const createRes = await page.request.post('/api/v1/instances', {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				kind: 'sonarr',
				name: initialName,
				base_url: 'http://127.0.0.1:8989',
				api_key: 'fake-api-key-0123456789',
				enabled: false
			}
		});
		expect(createRes.status()).toBe(201);
		const created = (await createRes.json()) as InstancePayload;
		instanceId = created.id;
		expect(created.enabled).toBe(false);

		// PUT with name change only (omitted enabled; never start a run)
		const putRes = await page.request.put(`/api/v1/instances/${instanceId}`, {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				kind: 'sonarr',
				name: renamedName,
				base_url: 'http://127.0.0.1:8989'
			}
		});
		expect(putRes.status()).toBe(200);

		// Read back: enabled stays false
		const getRes = await page.request.get(`/api/v1/instances/${instanceId}`);
		expect(getRes.status()).toBe(200);
		const readBack = (await getRes.json()) as InstancePayload;
		expect(readBack.name).toBe(renamedName);
		expect(readBack.enabled).toBe(false);

		// Send enabled: true
		const putEnabledRes = await page.request.put(`/api/v1/instances/${instanceId}`, {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				kind: 'sonarr',
				name: renamedName,
				base_url: 'http://127.0.0.1:8989',
				enabled: true
			}
		});
		expect(putEnabledRes.status()).toBe(200);

		// Read back: enabled is now true
		const getEnabledRes = await page.request.get(`/api/v1/instances/${instanceId}`);
		expect(getEnabledRes.status()).toBe(200);
		const readBackEnabled = (await getEnabledRes.json()) as InstancePayload;
		expect(readBackEnabled.enabled).toBe(true);
	} finally {
		if (instanceId) {
			await page.request.delete(`/api/v1/instances/${instanceId}`, {
				headers: { 'X-CSRF-Token': csrf }
			});
		}
	}
});
