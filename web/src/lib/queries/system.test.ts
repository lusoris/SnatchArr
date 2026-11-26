// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { keys } from '#lib/query.js';
import { session } from '#lib/session.svelte.js';
import { formatVersionLabel, systemModulesQuery, type ModuleState } from './system.js';

function mountModulesQuery(options?: { requireSession?: boolean }): QueryClient {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false } }
	});
	const children = createRawSnippet(() => ({
		render: () => {
			systemModulesQuery(options);
			return '<div></div>';
		}
	}));
	const { body } = render(QueryClientProvider, { props: { client, children } });
	expect(body).toContain('<div></div>');
	return client;
}

describe('formatVersionLabel', () => {
	it('positive: formats version and first 7 characters of commit', () => {
		expect(formatVersionLabel('0.1.0', 'abcdef123456')).toBe('v0.1.0 (abcdef1)');
		expect(formatVersionLabel('v1.2.3', 'fedcba654321')).toBe('v1.2.3 (fedcba6)');
	});

	it('negative: missing version renders nothing', () => {
		expect(formatVersionLabel(undefined, undefined)).toBe('');
		expect(formatVersionLabel(null, null)).toBe('');
		expect(formatVersionLabel('', 'abcdef123456')).toBe('');
	});

	it('boundary: empty or missing commit renders without commit part and dev build as-is', () => {
		expect(formatVersionLabel('dev', '')).toBe('dev');
		expect(formatVersionLabel('dev', undefined)).toBe('dev');
		expect(formatVersionLabel('0.1.0', '')).toBe('v0.1.0');
		expect(formatVersionLabel('0.1.0', undefined)).toBe('v0.1.0');
	});
});

describe('systemModulesQuery', () => {
	afterEach(() => {
		session.state = 'unknown';
		vi.restoreAllMocks();
	});

	it('positive: authenticated session fetches and returns the module list', async () => {
		const modules: ModuleState[] = [{ id: 'watchaware', enabled: true }];
		session.state = 'authenticated';
		const getSpy = vi.spyOn(api, 'GET').mockResolvedValue({ data: modules } as never);

		const client = mountModulesQuery();

		await vi.waitFor(() => {
			expect(client.getQueryData(keys.modules)).toEqual(modules);
		});
		expect(getSpy).toHaveBeenCalledWith('/system/modules');
	});

	it('negative: unauthenticated session does not fetch modules', () => {
		session.state = 'anonymous';
		const getSpy = vi.spyOn(api, 'GET');

		const client = mountModulesQuery();

		expect(getSpy).not.toHaveBeenCalled();
		expect(client.getQueryData(keys.modules)).toBeUndefined();
	});

	it('boundary: empty API response body falls back to an empty list', async () => {
		session.state = 'authenticated';
		const getSpy = vi.spyOn(api, 'GET').mockResolvedValue({ data: undefined } as never);

		const client = mountModulesQuery();

		await vi.waitFor(() => {
			expect(client.getQueryData(keys.modules)).toEqual([]);
		});
		expect(getSpy).toHaveBeenCalledWith('/system/modules');
	});

	it('boundary: requireSession false fetches while the session is anonymous', async () => {
		const modules: ModuleState[] = [{ id: 'watchaware', enabled: true }];
		session.state = 'anonymous';
		const getSpy = vi.spyOn(api, 'GET').mockResolvedValue({ data: modules } as never);

		const client = mountModulesQuery({ requireSession: false });

		await vi.waitFor(() => {
			expect(client.getQueryData(keys.modules)).toEqual(modules);
		});
		expect(getSpy).toHaveBeenCalledWith('/system/modules');
	});
});
