// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Shared helpers for e2e feature tests (auth, MCP toggling, stub servers, proxy auth).
// Not a test file: the features project matches `*.test.ts` only.
import type http from 'node:http';
import type net from 'node:net';
import { expect, type Page } from '@playwright/test';

export interface StubHandle {
	url: string;
	close: () => Promise<void>;
}

// Switches the MCP route on in runtime settings when it is off. mcp-sweep.test.ts also switches
// it on and leaves it on; restoring the flag afterwards would race with that project, which runs
// beside the feature tests.
export async function enableMcp(page: Page, csrf: string): Promise<void> {
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

// Bind a server to 127.0.0.1:0, resolve assigned port on listening, or reject on error.
export function listenLocal(server: net.Server | http.Server): Promise<number> {
	return new Promise<number>((resolve, reject) => {
		// Handle successful server bind by resolving the assigned port.
		const onListening = () => {
			server.off('error', onError);
			const addr = server.address() as net.AddressInfo;
			resolve(addr.port);
		};
		// Handle server bind failure by rejecting with the error.
		const onError = (err: Error) => {
			server.off('listening', onListening);
			reject(err);
		};
		server.once('listening', onListening);
		server.once('error', onError);
		server.listen(0, '127.0.0.1');
	});
}

// Bind an HTTP server to 127.0.0.1:0 via listenLocal and return its URL and close handle.
export async function listenStub(server: http.Server, path = ''): Promise<StubHandle> {
	const port = await listenLocal(server);
	const suffix = path && !path.startsWith('/') ? `/${path}` : path;
	// Close the server and resolve once all connections terminate.
	const close = () =>
		new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
	return {
		url: `http://127.0.0.1:${port}${suffix}`,
		close
	};
}

export const proxySecret = process.env.SNATCHARR_E2E_PROXY_SECRET ?? '';
export const proxyUserHeader = process.env.SNATCHARR_E2E_PROXY_HEADER ?? 'Remote-User';
export const proxyGroupsHeader =
	process.env.SNATCHARR_E2E_PROXY_GROUPS_HEADER ?? 'X-authentik-groups';
export const proxySecretHeader =
	process.env.SNATCHARR_E2E_PROXY_SECRET_HEADER ?? 'X-SnatchArr-Proxy-Secret';

// Build HTTP headers for proxy authentication tests.
export function proxyHeaders(
	user: string,
	groups: string,
	secret = proxySecret
): Record<string, string> {
	return {
		[proxyUserHeader]: user,
		[proxyGroupsHeader]: groups,
		[proxySecretHeader]: secret
	};
}
