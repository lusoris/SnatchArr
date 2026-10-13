// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render as svelteRender } from '@testing-library/svelte';
import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';
import type { Component } from 'svelte';
import { vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';

export type Instance = components['schemas']['Instance'];
export type SnatchPolicy = components['schemas']['SnatchPolicy'];
export type CapStatus = components['schemas']['CapStatus'];

/** A query client per test: no retry, so a refused request shows at once; nothing cached. */
export function createTestQueryClient(): QueryClient {
	return new QueryClient({
		defaultOptions: {
			queries: {
				retry: false,
				gcTime: 0
			}
		}
	});
}

/** Renders a view inside its own query client. */
export function renderWithQuery<Props extends Record<string, unknown>>(
	component: Component<Props>,
	options?: Props,
	client: QueryClient = createTestQueryClient()
) {
	return svelteRender(component, options, {
		wrapper: QueryClientProvider,
		wrapperProps: { client }
	});
}

/**
 * Answers `api.GET` from a table of path templates, at the typed client boundary. A value that is
 * an Error is thrown, as the problem middleware does for a problem document. A path outside the
 * table throws too, so a view that starts to read another endpoint fails its test.
 */
export function stubGet(table: Record<string, unknown>) {
	return vi.spyOn(api, 'GET').mockImplementation(async (path: string) => {
		if (!Object.hasOwn(table, path)) throw new Error(`GET ${path} has no stubbed answer`);
		const answer = table[path];
		if (answer instanceof Error) throw answer;
		return { data: answer } as never;
	});
}

/** A Readarr-kind instance whose last connection test passed and found the Bookshelf fork. */
export const forkInstance: Instance = {
	id: '00000000-0000-0000-0000-000000000001',
	name: 'Library Readarr',
	kind: 'readarr',
	base_url: 'http://readarr.local:8787',
	source: 'manual',
	enabled: true,
	last_seen_product: 'bookshelf',
	last_seen_version: '0.4.21.182',
	last_check_at: '2026-09-01T12:00:00Z',
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

/** An instance whose last connection test failed: the API stores its time and the error. */
export const failedInstance: Instance = {
	id: '00000000-0000-0000-0000-000000000002',
	name: 'Broken Sonarr',
	kind: 'sonarr',
	base_url: 'http://sonarr.local:8989',
	source: 'manual',
	enabled: true,
	last_check_at: '2026-09-01T12:00:00Z',
	last_error: 'Connection refused (ECONNREFUSED)',
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

/** An instance no connection test has reached yet. */
export const untestedInstance: Instance = {
	id: '00000000-0000-0000-0000-000000000003',
	name: 'Fresh Radarr',
	kind: 'radarr',
	base_url: 'http://radarr.local:7878',
	source: 'manual',
	enabled: true,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

/** The policy the instance detail view loads next to the instance. */
export const samplePolicy: SnatchPolicy = {
	missing_per_cycle: 10,
	upgrade_per_cycle: 5,
	cycle_interval_s: 300,
	hourly_cap: 50,
	selection: 'recent',
	monitored_only: true,
	skip_future_releases: true,
	radarr_release_type: 'physical',
	sonarr_missing_mode: 'episodes',
	sonarr_upgrade_mode: 'episodes',
	lidarr_missing_mode: 'album',
	processed_ttl_h: 24,
	max_queue_size: 100,
	await_command: false,
	page_size: 50,
	recent_grab_window_h: 48,
	afterglow_max_h: 168
};
