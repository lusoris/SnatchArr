// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { ProblemError } from '@sveltesentio/core';
import { fireEvent, screen, waitFor } from '@testing-library/svelte';
import { toast } from 'svelte-sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { fill, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import AdultSourcesPage from './+page.svelte';

type AdultSource = components['schemas']['AdultSource'];
type AdultSyncStatus = components['schemas']['AdultSyncStatus'];

const sampleSource: AdultSource = {
	id: '018f0000-0000-7000-8000-000000000001',
	name: 'Primary Catalogue',
	endpoint: 'http://127.0.0.1:1/graphql',
	has_api_key: true,
	enabled: true,
	sync_favorites: true,
	priority: 10,
	last_sync_at: '2026-10-27T11:00:00Z',
	last_sync_error: 'dial tcp 127.0.0.1:1: connect: connection refused',
	created_at: '2026-10-27T10:00:00Z',
	updated_at: '2026-10-27T10:00:00Z'
};

const sampleStatus: AdultSyncStatus = {
	total_sources: 1,
	enabled_sources: 1,
	performers_synced: 4,
	studios_synced: 2,
	last_sync_at: '2026-10-27T11:00:00Z',
	last_error: 'dial tcp 127.0.0.1:1: connect: connection refused'
};

describe('adult catalogue sources (/adult/sources)', () => {
	afterEach(() => {
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		vi.restoreAllMocks();
	});

	it('positive: renders catalogue sources, per-source sync telemetry, and triggers manual sync', async () => {
		stubGet({
			'/adult/sources': [sampleSource],
			'/adult/sync/status': sampleStatus
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({
			data: { sources_synced: 1, performers_synced: 4, studios_synced: 2 }
		} as never);
		const toastSpy = vi.spyOn(toast, 'success');

		renderWithQuery(AdultSourcesPage);

		expect(
			await screen.findByRole('heading', { level: 1, name: m.adult_sources_title() })
		).toBeTruthy();
		expect(await screen.findByText('Primary Catalogue')).toBeTruthy();
		expect(screen.getByTitle('http://127.0.0.1:1/graphql')).toBeTruthy();
		expect(
			await screen.findByText(m.adult_sources_status_counts({ performers: 4, studios: 2 }))
		).toBeTruthy();
		expect(
			await screen.findByText(
				m.adult_sources_source_error({
					error: 'dial tcp 127.0.0.1:1: connect: connection refused'
				})
			)
		).toBeTruthy();

		const syncBtn = screen.getByRole('button', { name: m.adult_sources_sync_now() });
		await fireEvent.click(syncBtn);

		await waitFor(() => {
			expect(postSpy).toHaveBeenCalledWith('/adult/sync');
		});
		expect(toastSpy).toHaveBeenCalledWith(
			m.adult_sources_synced_toast({ sources: 1, performers: 4, studios: 2 })
		);
	});

	it('negative: hides admin controls for viewer and shows problem detail on create error', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		stubGet({
			'/adult/sources': [sampleSource],
			'/adult/sync/status': sampleStatus
		});

		const { unmount } = renderWithQuery(AdultSourcesPage);

		expect(await screen.findByText('Primary Catalogue')).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.adult_sources_sync_now() })).toBeNull();
		expect(screen.queryByRole('button', { name: m.action_save() })).toBeNull();
		unmount();

		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		stubGet({
			'/adult/sources': [],
			'/adult/sync/status': sampleStatus
		});
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 400,
				detail: 'invalid endpoint url'
			})
		);

		renderWithQuery(AdultSourcesPage);

		await screen.findByText(m.adult_sources_none());
		await fill(m.adult_sources_name(), 'Broken Source');
		await fill(m.adult_sources_endpoint(), 'http://127.0.0.1:1/graphql');
		await fill(m.adult_sources_api_key(), 'fake-api-key-0123456789');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect((await screen.findByRole('alert')).textContent).toContain('invalid endpoint url');
	});

	it('boundary: renders empty state when no catalogue sources are configured', async () => {
		stubGet({
			'/adult/sources': [],
			'/adult/sync/status': {
				total_sources: 0,
				enabled_sources: 0,
				performers_synced: 0,
				studios_synced: 0
			}
		});

		renderWithQuery(AdultSourcesPage);

		expect(await screen.findByText(m.adult_sources_none())).toBeTruthy();
		expect(await screen.findByText(m.adult_sources_status_never())).toBeTruthy();
	});
});
