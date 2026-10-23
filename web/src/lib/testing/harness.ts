// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render as svelteRender } from '@testing-library/svelte';
import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';
import type { Component } from 'svelte';
import { vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { session } from '#lib/session.svelte.js';

export type Instance = components['schemas']['Instance'];
export type SnatchPolicy = components['schemas']['SnatchPolicy'];
export type Run = components['schemas']['Run'];
export type PolicyFilterOptions = components['schemas']['PolicyFilterOptions'];
export type QueuePolicy = components['schemas']['QueuePolicy'];
export type QueueStrike = components['schemas']['QueueStrike'];
export type InstancePriority = components['schemas']['InstancePriority'];

/** A query client per test: no retry, so a refused request shows at once; nothing cached. */
function createTestQueryClient(): QueryClient {
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
	if (session.user === null && session.state === 'unknown') {
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
	}
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
	afterglow_max_h: 168,
	skip_unavailable_movies: true,
	dry_run: false
};

/** The tags and quality profiles an instance offers to the policy filters, sorted by name. */
export const sampleFilterOptions: PolicyFilterOptions = {
	tags: [
		{ id: 2, name: 'anime' },
		{ id: 5, name: 'kids' }
	],
	quality_profiles: [{ id: 4, name: 'HD-1080p' }]
};

/** An instance the Configarr import owns: only its policy is editable in the UI. */
export const configarrInstance: Instance = {
	id: '00000000-0000-0000-0000-000000000004',
	name: 'Configarr Sonarr',
	kind: 'sonarr',
	base_url: 'http://sonarr-cfg.local:8989',
	source: 'configarr',
	configarr_key: 'sonarr-main',
	enabled: true,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

/** Runs of one instance, newest first: one waiting, one at the worker, one finished. */
export const sampleRuns: Run[] = [
	{
		id: '10000000-0000-0000-0000-000000000001',
		instance_id: forkInstance.id,
		kind: 'missing',
		status: 'queued',
		queued_at: '2026-09-01T12:10:00Z',
		searched_count: 0
	},
	{
		id: '10000000-0000-0000-0000-000000000002',
		instance_id: forkInstance.id,
		kind: 'upgrade',
		status: 'leased',
		queued_at: '2026-09-01T12:00:00Z',
		started_at: '2026-09-01T12:01:00Z',
		searched_count: 3
	},
	{
		id: '10000000-0000-0000-0000-000000000003',
		instance_id: forkInstance.id,
		kind: 'missing',
		status: 'done',
		queued_at: '2026-09-01T11:00:00Z',
		started_at: '2026-09-01T11:01:00Z',
		finished_at: '2026-09-01T11:05:00Z',
		searched_count: 12
	}
];

/** The queue policy the instance detail view loads next to the snatch policy. */
export const sampleQueuePolicy: QueuePolicy = {
	enabled: true,
	rules: {
		stalled: { mode: 'observe', threshold: 3, blocklist: true },
		metadata_stuck: { mode: 'observe', threshold: 180, blocklist: true },
		slow: { mode: 'observe', threshold: 5, blocklist: true },
		failed: { mode: 'act', threshold: 1, blocklist: true },
		import_blocked: { mode: 'act', threshold: 2, blocklist: true },
		id_matched: { mode: 'observe', threshold: 1, blocklist: false },
		import_fixable: { mode: 'observe', threshold: 1, blocklist: false },
		import_broken: { mode: 'observe', threshold: 2, blocklist: true },
		import_not_wanted: { mode: 'observe', threshold: 1, blocklist: true },
		unwanted_score: { mode: 'observe', threshold: 3, blocklist: true },
		not_an_upgrade: { mode: 'observe', threshold: 1, blocklist: false },
		duplicate: { mode: 'observe', threshold: 1, blocklist: false },
		unwanted_unmonitored: { mode: 'observe', threshold: 3, blocklist: false }
	},
	grace_seconds: 3600,
	min_bytes_per_second: 1024,
	max_removals_per_sweep: 5,
	max_imports_per_sweep: 10,
	match_fixable_min_percent: 75,
	match_broken_below_percent: 40,
	track_fixable_min_percent: 50,
	track_broken_below_percent: 40
};

/** Active queue strikes for an instance. */
export const sampleQueueStrikes: QueueStrike[] = [
	{
		download_id: 'nzb-test-123',
		queue_id: 42,
		rule: 'stalled',
		count: 2,
		threshold: 3,
		last_seen: '2026-09-01T12:00:00Z'
	}
];

/** Sample instance priority matching default preset values. */
export const samplePriority: InstancePriority = {
	search_priority: null,
	download_priority: null,
	effective_search_priority: 30,
	effective_download_priority: 30,
	search_source: 'preset',
	download_source: 'preset'
};
